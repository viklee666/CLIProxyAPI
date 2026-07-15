package clientaccess

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestReplaceCredentialBindingsWithStatsLargeSelection(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	groups := make([]CredentialGroupInput, 0, 3)
	for index := 0; index < 3; index++ {
		group, errGroup := service.CreateGroup(ctx, GroupCreate{Name: fmt.Sprintf("group-%d", index)})
		if errGroup != nil {
			t.Fatalf("create group: %v", errGroup)
		}
		groups = append(groups, CredentialGroupInput{GroupID: group.ID, Priority: index * 10})
	}
	authIndices := make([]string, 0, 1202)
	for index := 0; index < 1200; index++ {
		authIndices = append(authIndices, fmt.Sprintf("auth-%04d", index))
	}
	authIndices = append(authIndices, "auth-0000", " ")

	stats, errReplace := service.ReplaceCredentialBindingsWithStats(ctx, CredentialBindingBatch{AuthIndices: authIndices, Groups: groups})
	if errReplace != nil {
		t.Fatalf("replace large selection: %v", errReplace)
	}
	if stats.Matched != 1200 || stats.Updated != 1200 || stats.Unchanged != 0 {
		t.Fatalf("first stats = %+v", stats)
	}
	page, errList := service.ListCredentialBindings(ctx, ListOptions{Page: 1, PageSize: 200})
	if errList != nil {
		t.Fatalf("list bindings: %v", errList)
	}
	if page.Total != 3600 {
		t.Fatalf("binding total = %d, want 3600", page.Total)
	}

	stats, errReplace = service.ReplaceCredentialBindingsWithStats(ctx, CredentialBindingBatch{AuthIndices: authIndices, Groups: groups})
	if errReplace != nil {
		t.Fatalf("repeat large selection: %v", errReplace)
	}
	if stats.Matched != 1200 || stats.Updated != 0 || stats.Unchanged != 1200 {
		t.Fatalf("repeat stats = %+v", stats)
	}

	groups[0].Priority = 99
	stats, errReplace = service.ReplaceCredentialBindingsWithStats(ctx, CredentialBindingBatch{AuthIndices: authIndices, Groups: groups})
	if errReplace != nil {
		t.Fatalf("update large selection: %v", errReplace)
	}
	if stats.Updated != 1200 || stats.Unchanged != 0 {
		t.Fatalf("updated stats = %+v", stats)
	}
}

func TestReplaceCredentialBindingsWithStatsInvalidGroupRollsBack(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	group, errGroup := service.CreateGroup(ctx, GroupCreate{Name: "valid"})
	if errGroup != nil {
		t.Fatalf("create group: %v", errGroup)
	}
	if _, errReplace := service.ReplaceCredentialBindingsWithStats(ctx, CredentialBindingBatch{
		AuthIndices: []string{"auth-a"},
		Groups:      []CredentialGroupInput{{GroupID: group.ID, Priority: 1}},
	}); errReplace != nil {
		t.Fatalf("seed binding: %v", errReplace)
	}
	if _, errReplace := service.ReplaceCredentialBindingsWithStats(ctx, CredentialBindingBatch{
		AuthIndices: []string{"auth-a"},
		Groups:      []CredentialGroupInput{{GroupID: 999999, Priority: 2}},
	}); errReplace == nil {
		t.Fatal("invalid group error = nil")
	}
	page, errList := service.ListCredentialBindings(ctx, ListOptions{Page: 1, PageSize: 20, AuthIndices: []string{"auth-a"}})
	if errList != nil {
		t.Fatalf("list binding: %v", errList)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].GroupID != group.ID || page.Items[0].Priority != 1 {
		t.Fatalf("binding changed after rollback: %+v", page)
	}
}

func TestListGroupsAndKeysLoadBatchedRelations(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	group, errGroup := service.CreateGroup(ctx, GroupCreate{Name: "batched-relations"})
	if errGroup != nil {
		t.Fatalf("create group: %v", errGroup)
	}
	allowAll := false
	key, errKey := service.CreateKey(ctx, KeyCreate{
		Name:            "batched-key",
		CustomSecret:    "sk-cpa-batched-relations",
		AllowAllGroups:  &allowAll,
		GroupIDs:        []int64{group.ID},
		TokenLimitTotal: 1000,
	})
	if errKey != nil {
		t.Fatalf("create key: %v", errKey)
	}
	if _, _, errReserve := service.store.ReserveUsage(ctx, key.ID, "reservation-batched", 50, time.Now().UTC()); errReserve != nil {
		t.Fatalf("reserve usage: %v", errReserve)
	}
	if _, errBindings := service.ReplaceCredentialBindingsWithStats(ctx, CredentialBindingBatch{
		AuthIndices: []string{"auth-a", "auth-b"},
		Groups:      []CredentialGroupInput{{GroupID: group.ID, Priority: 5}},
	}); errBindings != nil {
		t.Fatalf("create bindings: %v", errBindings)
	}

	groupPage, errGroups := service.ListGroups(ctx, ListOptions{Page: 1, PageSize: 20, Search: "relations"})
	if errGroups != nil {
		t.Fatalf("list groups: %v", errGroups)
	}
	if groupPage.Total != 1 || len(groupPage.Items) != 1 || groupPage.Items[0].KeyCount != 1 || groupPage.Items[0].CredentialCount != 2 {
		t.Fatalf("group page = %+v", groupPage)
	}
	keyPage, errKeys := service.ListKeys(ctx, ListOptions{Page: 1, PageSize: 20, Search: "batched"})
	if errKeys != nil {
		t.Fatalf("list keys: %v", errKeys)
	}
	if keyPage.Total != 1 || len(keyPage.Items) != 1 || len(keyPage.Items[0].GroupIDs) != 1 || keyPage.Items[0].GroupIDs[0] != group.ID || keyPage.Items[0].TokenReserved != 50 {
		t.Fatalf("key page = %+v", keyPage)
	}
}
