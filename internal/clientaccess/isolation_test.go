package clientaccess

import (
	"context"
	"testing"
)

func TestMissingClientAccessServiceDeniesCredentialAccess(t *testing.T) {
	var service *Service
	if allowed, _, _ := service.ResolveCredentialAccess("credential", []int64{1}, false, true); allowed {
		t.Fatal("a typed-nil credential resolver allowed access")
	}
}

func TestExplicitGroupAssignmentDoesNotDefaultToAllResources(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	group, errGroup := service.CreateGroup(ctx, GroupCreate{Name: "restricted"})
	if errGroup != nil {
		t.Fatal(errGroup)
	}
	key, errKey := service.CreateKey(ctx, KeyCreate{Name: "group-only", GroupIDs: []int64{group.ID}})
	if errKey != nil {
		t.Fatal(errKey)
	}
	if key.AllowAllGroups {
		t.Error("a key with an explicit group silently received all-group access")
	}
	ungroupedKey, errUngrouped := service.CreateKey(ctx, KeyCreate{Name: "ungrouped-only", AllowUngrouped: true})
	if errUngrouped != nil {
		t.Fatal(errUngrouped)
	}
	if ungroupedKey.AllowAllGroups {
		t.Error("an ungrouped-only key silently received all-group access")
	}
	globalKey, errGlobal := service.CreateKey(ctx, KeyCreate{Name: "legacy-all"})
	if errGlobal != nil {
		t.Fatal(errGlobal)
	}
	groups := []int64{group.ID}
	updated, errUpdate := service.UpdateKey(ctx, globalKey.ID, KeyUpdate{GroupIDs: &groups})
	if errUpdate != nil {
		t.Fatal(errUpdate)
	}
	if updated.AllowAllGroups {
		t.Error("assigning groups without an explicit allow-all flag retained all-group access")
	}
}

func TestZeroPriorityGroupReplacementRevokesOldMembership(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	group, errGroup := service.CreateGroup(ctx, GroupCreate{Name: "replace-zero"})
	if errGroup != nil {
		t.Fatal(errGroup)
	}
	for _, authIndex := range []string{"old-resource", "new-resource"} {
		if _, errReplace := service.ReplaceGroupCredentialBindings(ctx, group.ID, GroupCredentialBindingBatch{AuthIndices: []string{authIndex}}); errReplace != nil {
			t.Fatal(errReplace)
		}
	}
	if allowed, _, _ := service.ResolveCredentialAccess("old-resource", []int64{group.ID}, false, false); allowed {
		t.Error("replaced credential retained access at priority zero")
	}
	if allowed, _, _ := service.ResolveCredentialAccess("new-resource", []int64{group.ID}, false, false); !allowed {
		t.Error("replacement credential did not receive its membership")
	}
}

func TestDisabledGroupCredentialsAreNotUngrouped(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	group, errGroup := service.CreateGroup(ctx, GroupCreate{Name: "disabled-group"})
	if errGroup != nil {
		t.Fatal(errGroup)
	}
	if errBind := service.ReplaceCredentialBindings(ctx, CredentialBindingBatch{
		AuthIndices: []string{"private-resource"}, Groups: []CredentialGroupInput{{GroupID: group.ID}},
	}); errBind != nil {
		t.Fatal(errBind)
	}
	disabled := false
	if _, errDisable := service.UpdateGroup(ctx, group.ID, GroupUpdate{Enabled: &disabled}); errDisable != nil {
		t.Fatal(errDisable)
	}
	if allowed, _, _ := service.ResolveCredentialAccess("private-resource", []int64{group.ID}, false, true); allowed {
		t.Fatal("disabling a group exposed its resources through allow_ungrouped")
	}
	if allowed, _, _ := service.ResolveCredentialAccess("actually-ungrouped", nil, false, true); !allowed {
		t.Fatal("an actually ungrouped resource should remain accessible")
	}
}
