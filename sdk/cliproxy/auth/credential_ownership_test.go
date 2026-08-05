package auth

import (
	"context"
	"strconv"
	"testing"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

type testCredentialOwnershipResolver struct {
	owners map[string]int64
}

func (r testCredentialOwnershipResolver) OwnerOf(authIndex string) int64 {
	return r.owners[authIndex]
}

func (r testCredentialOwnershipResolver) HasOwnedCredentials() bool {
	return len(r.owners) > 0
}

func tenantOptions(tenantID int64, allowAll bool) cliproxyexecutor.Options {
	metadata := map[string]any{
		cliproxyexecutor.ClientKeyIDMetadataKey:          "1",
		cliproxyexecutor.ClientAllowAllGroupsMetadataKey: allowAll,
	}
	if tenantID > 0 {
		metadata[cliproxyexecutor.ClientTenantIDMetadataKey] = strconv.FormatInt(tenantID, 10)
	}
	return cliproxyexecutor.Options{Metadata: metadata}
}

func tenantAuth(id, index string, tenantID int64) *Auth {
	return &Auth{
		ID:       id,
		Index:    index,
		Provider: "test",
		Status:   StatusActive,
		Attributes: map[string]string{
			AttributeTenantID: strconv.FormatInt(tenantID, 10),
		},
	}
}

func TestManagerCredentialOwnershipRejectsNonOwnersEvenWhenGroupsAllowAll(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		resolver CredentialOwnershipResolver
		opts     cliproxyexecutor.Options
	}{
		{
			name:     "admin caller",
			resolver: testCredentialOwnershipResolver{owners: map[string]int64{"tenant-index": 1}},
			opts:     tenantOptions(0, true),
		},
		{
			name:     "other tenant caller",
			resolver: testCredentialOwnershipResolver{owners: map[string]int64{"tenant-index": 1}},
			opts:     tenantOptions(2, true),
		},
		{
			name:     "missing ownership resolver",
			resolver: nil,
			opts:     tenantOptions(1, true),
		},
		{
			name:     "missing ownership snapshot entry",
			resolver: testCredentialOwnershipResolver{owners: map[string]int64{}},
			opts:     tenantOptions(1, true),
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			manager := NewManager(nil, &RoundRobinSelector{}, nil)
			manager.RegisterExecutor(schedulerTestExecutor{})
			manager.SetCredentialOwnershipResolver(testCase.resolver)
			if _, errRegister := manager.Register(context.Background(), tenantAuth("tenant", "tenant-index", 1)); errRegister != nil {
				t.Fatalf("Register() error = %v", errRegister)
			}
			if _, errSelect := manager.SelectAuth(context.Background(), "test", "", testCase.opts); errSelect == nil {
				t.Fatal("SelectAuth() error = nil, want tenant credential rejection")
			}
		})
	}
}

func TestManagerCredentialOwnershipAllowsMatchingTenantAndGlobalCredentials(t *testing.T) {
	t.Parallel()
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	manager.RegisterExecutor(schedulerTestExecutor{})
	manager.SetCredentialOwnershipResolver(testCredentialOwnershipResolver{owners: map[string]int64{"tenant-index": 1}})
	if _, errRegister := manager.Register(context.Background(), tenantAuth("tenant", "tenant-index", 1)); errRegister != nil {
		t.Fatalf("Register() error = %v", errRegister)
	}
	selected, errSelect := manager.SelectAuth(context.Background(), "test", "", tenantOptions(1, true))
	if errSelect != nil {
		t.Fatalf("SelectAuth() error = %v", errSelect)
	}
	if selected.ID != "tenant" {
		t.Fatalf("SelectAuth() ID = %q, want tenant", selected.ID)
	}

	globalManager := NewManager(nil, &RoundRobinSelector{}, nil)
	globalManager.RegisterExecutor(schedulerTestExecutor{})
	globalManager.SetCredentialOwnershipResolver(testCredentialOwnershipResolver{owners: map[string]int64{"tenant-index": 1}})
	if _, errRegister := globalManager.Register(context.Background(), &Auth{ID: "global", Index: "global-index", Provider: "test", Status: StatusActive}); errRegister != nil {
		t.Fatalf("Register() global error = %v", errRegister)
	}
	global, errGlobal := globalManager.SelectAuth(context.Background(), "test", "", tenantOptions(2, true))
	if errGlobal != nil {
		t.Fatalf("SelectAuth() global error = %v", errGlobal)
	}
	if global.ID != "global" {
		t.Fatalf("SelectAuth() global ID = %q, want global", global.ID)
	}
}

func TestManagerTenantModelClientIDsHonorsOwnershipAndGroupBindings(t *testing.T) {
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	manager.RegisterExecutor(schedulerTestExecutor{})
	manager.SetCredentialOwnershipResolver(testCredentialOwnershipResolver{owners: map[string]int64{
		"tenant-a": 1,
		"tenant-b": 1,
		"other":    2,
	}})
	manager.SetCredentialGroupResolver(testCredentialGroupResolver{memberships: map[string]map[int64]int{
		"tenant-a": {10: 0},
		"tenant-b": {20: 0},
		"other":    {10: 0},
	}})
	for _, candidate := range []*Auth{
		tenantAuth("client-a", "tenant-a", 1),
		tenantAuth("client-b", "tenant-b", 1),
		tenantAuth("client-other", "other", 2),
	} {
		if _, errRegister := manager.Register(context.Background(), candidate); errRegister != nil {
			t.Fatalf("Register(%s) error = %v", candidate.ID, errRegister)
		}
	}

	metadata := map[string]any{
		cliproxyexecutor.ClientKeyIDMetadataKey:          "tenant-key",
		cliproxyexecutor.ClientTenantIDMetadataKey:       "1",
		cliproxyexecutor.ClientGroupIDsMetadataKey:       "10",
		cliproxyexecutor.ClientAllowAllGroupsMetadataKey: false,
		cliproxyexecutor.ClientAllowUngroupedMetadataKey: false,
	}
	clientIDs := manager.TenantModelClientIDs(metadata)
	if len(clientIDs) != 1 || clientIDs[0] != "client-a" {
		t.Fatalf("TenantModelClientIDs() = %v, want [client-a]", clientIDs)
	}
}

func TestManagerHomeEnabledForMetadataExcludesTenantKeys(t *testing.T) {
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	manager.SetConfig(&internalconfig.Config{Home: internalconfig.HomeConfig{Enabled: true}})

	if !manager.HomeEnabledForMetadata(nil) {
		t.Fatal("HomeEnabledForMetadata(nil) = false, want true")
	}
	if manager.HomeEnabledForMetadata(map[string]any{
		cliproxyexecutor.ClientTenantIDMetadataKey: "1",
	}) {
		t.Fatal("HomeEnabledForMetadata(tenant) = true, want false")
	}
}
