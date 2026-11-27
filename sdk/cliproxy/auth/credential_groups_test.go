package auth

import (
	"context"
	"testing"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

type testCredentialGroupResolver struct {
	memberships map[string]map[int64]int
}

func (r testCredentialGroupResolver) ResolveCredentialAccess(authIndex string, allowedGroupIDs []int64, allowAllGroups, allowUngrouped bool) (bool, int, bool) {
	if allowAllGroups {
		return true, 0, false
	}
	groups := r.memberships[authIndex]
	if len(groups) == 0 {
		return allowUngrouped, 0, false
	}
	best := 0
	found := false
	for _, groupID := range allowedGroupIDs {
		priority, ok := groups[groupID]
		if !ok {
			continue
		}
		if !found || priority > best {
			best = priority
		}
		found = true
	}
	return found, best, found
}

func clientGroupOptions(groupIDs string, allowUngrouped bool) cliproxyexecutor.Options {
	return cliproxyexecutor.Options{Metadata: map[string]any{
		cliproxyexecutor.ClientKeyIDMetadataKey:          "1",
		cliproxyexecutor.ClientGroupIDsMetadataKey:       groupIDs,
		cliproxyexecutor.ClientAllowAllGroupsMetadataKey: false,
		cliproxyexecutor.ClientAllowUngroupedMetadataKey: allowUngrouped,
	}}
}

func TestManagerCredentialGroupFiltersCandidates(t *testing.T) {
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	manager.RegisterExecutor(schedulerTestExecutor{})
	manager.SetCredentialGroupResolver(testCredentialGroupResolver{memberships: map[string]map[int64]int{
		"index-a": {1: 5},
		"index-b": {2: 9},
	}})
	for _, auth := range []*Auth{
		{ID: "a", Index: "index-a", Provider: "test", Status: StatusActive},
		{ID: "b", Index: "index-b", Provider: "test", Status: StatusActive},
	} {
		if _, errRegister := manager.Register(context.Background(), auth); errRegister != nil {
			t.Fatalf("Register(%s) error = %v", auth.ID, errRegister)
		}
	}
	selected, errSelect := manager.SelectAuth(context.Background(), "test", "", clientGroupOptions("1", false))
	if errSelect != nil {
		t.Fatalf("SelectAuth() error = %v", errSelect)
	}
	if selected.ID != "a" {
		t.Fatalf("SelectAuth() ID = %q, want a", selected.ID)
	}
}

func TestManagerCredentialGroupPriorityOverridesBasePriority(t *testing.T) {
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	manager.RegisterExecutor(schedulerTestExecutor{})
	manager.SetCredentialGroupResolver(testCredentialGroupResolver{memberships: map[string]map[int64]int{
		"index-a": {1: 2},
		"index-b": {1: 20},
	}})
	for _, auth := range []*Auth{
		{ID: "a", Index: "index-a", Provider: "test", Status: StatusActive, Attributes: map[string]string{"priority": "100"}},
		{ID: "b", Index: "index-b", Provider: "test", Status: StatusActive, Attributes: map[string]string{"priority": "0"}},
	} {
		if _, errRegister := manager.Register(context.Background(), auth); errRegister != nil {
			t.Fatalf("Register(%s) error = %v", auth.ID, errRegister)
		}
	}
	selected, errSelect := manager.SelectAuth(context.Background(), "test", "", clientGroupOptions("1", false))
	if errSelect != nil {
		t.Fatalf("SelectAuth() error = %v", errSelect)
	}
	if selected.ID != "b" {
		t.Fatalf("SelectAuth() ID = %q, want b", selected.ID)
	}
}

func TestManagerCredentialGroupAllowsUngrouped(t *testing.T) {
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	manager.RegisterExecutor(schedulerTestExecutor{})
	manager.SetCredentialGroupResolver(testCredentialGroupResolver{memberships: map[string]map[int64]int{}})
	if _, errRegister := manager.Register(context.Background(), &Auth{ID: "ungrouped", Index: "index-u", Provider: "test", Status: StatusActive}); errRegister != nil {
		t.Fatalf("Register() error = %v", errRegister)
	}
	selected, errSelect := manager.SelectAuth(context.Background(), "test", "", clientGroupOptions("1", true))
	if errSelect != nil {
		t.Fatalf("SelectAuth() error = %v", errSelect)
	}
	if selected.ID != "ungrouped" {
		t.Fatalf("SelectAuth() ID = %q", selected.ID)
	}
}
