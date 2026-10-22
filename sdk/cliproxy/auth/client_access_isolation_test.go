package auth

import (
	"context"
	"net/http"
	"testing"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executionregistry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

type accessIsolationHomeDispatcher struct {
	response []byte
}

func (*accessIsolationHomeDispatcher) HeartbeatOK() bool { return true }
func (d *accessIsolationHomeDispatcher) RPopAuth(context.Context, string, string, http.Header, int) ([]byte, error) {
	return d.response, nil
}
func (*accessIsolationHomeDispatcher) AbortAmbiguousDispatch() {}

func TestCreditsFallbackCannotBypassClientAccess(t *testing.T) {
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	manager.RegisterExecutor(schedulerTestExecutor{provider: "antigravity"})
	manager.SetCredentialGroupResolver(testCredentialGroupResolver{memberships: map[string]map[int64]int{"credits-private": {2: 0}}})
	registered, errRegister := manager.Register(context.Background(), &Auth{ID: "credits-isolation", Index: "credits-private", Provider: "antigravity", Status: StatusActive})
	if errRegister != nil {
		t.Fatal(errRegister)
	}
	registry.GetGlobalRegistry().RegisterClient(registered.ID, "antigravity", []*registry.ModelInfo{{ID: "claude-isolation"}})
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(registered.ID) })
	for _, opts := range []cliproxyexecutor.Options{tenantOptions(1, true), clientGroupOptions("1", false)} {
		candidates, errCandidates := manager.findAllAntigravityCreditsCandidateAuths(context.Background(), "claude-isolation", opts)
		if errCandidates != nil || len(candidates) != 0 {
			t.Errorf("fallback selected resources outside the caller's access: candidates=%v err=%v", candidates, errCandidates)
		}
	}
}

func TestPartialTenantMetadataDoesNotDisableGroupChecks(t *testing.T) {
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	manager.RegisterExecutor(schedulerTestExecutor{})
	manager.SetCredentialOwnershipResolver(testCredentialOwnershipResolver{owners: map[string]int64{"tenant-private": 1}})
	manager.SetCredentialGroupResolver(testCredentialGroupResolver{memberships: map[string]map[int64]int{"tenant-private": {2: 0}}})
	if _, errRegister := manager.Register(context.Background(), tenantAuth("tenant-partial", "tenant-private", 1)); errRegister != nil {
		t.Fatal(errRegister)
	}
	opts := clientGroupOptions("1", false)
	delete(opts.Metadata, cliproxyexecutor.ClientKeyIDMetadataKey)
	opts.Metadata[cliproxyexecutor.ClientTenantIDMetadataKey] = "1"
	if _, _, errPick := manager.pickNext(context.Background(), "test", "", opts, nil); errPick == nil {
		t.Fatal("missing key ID disabled group checks for a tenant-scoped request")
	}
}

func TestTenantCannotSelectGlobalCredentials(t *testing.T) {
	for _, withOwners := range []bool{false, true} {
		manager := NewManager(nil, &RoundRobinSelector{}, nil)
		manager.RegisterExecutor(schedulerTestExecutor{})
		if withOwners {
			manager.SetCredentialOwnershipResolver(testCredentialOwnershipResolver{owners: map[string]int64{"other-index": 2}})
		}
		if _, errRegister := manager.Register(context.Background(), &Auth{ID: "global", Index: "global-index", Provider: "test", Status: StatusActive}); errRegister != nil {
			t.Fatal(errRegister)
		}
		for _, allowAll := range []bool{false, true} {
			opts := tenantOptions(1, allowAll)
			opts.Metadata[cliproxyexecutor.ClientAllowUngroupedMetadataKey] = true
			if _, _, errPick := manager.pickNext(context.Background(), "test", "", opts, nil); errPick == nil {
				t.Errorf("single-provider selection accepted global credential (owners=%v, allowAll=%v)", withOwners, allowAll)
			}
			if _, _, _, errPick := manager.pickNextMixed(context.Background(), []string{"test"}, "", opts, nil); errPick == nil {
				t.Errorf("mixed-provider selection accepted global credential (owners=%v, allowAll=%v)", withOwners, allowAll)
			}
		}
	}
}

func TestClientGroupSelectionFailsClosedWithoutResolver(t *testing.T) {
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	manager.RegisterExecutor(schedulerTestExecutor{})
	if _, errRegister := manager.Register(context.Background(), &Auth{ID: "global", Index: "global-index", Provider: "test", Status: StatusActive}); errRegister != nil {
		t.Fatal(errRegister)
	}
	opts := clientGroupOptions("1", true)
	if _, _, errPick := manager.pickNext(context.Background(), "test", "", opts, nil); errPick == nil {
		t.Error("single-provider selection accepted a group-restricted request without its resolver")
	}
	if _, _, _, errPick := manager.pickNextMixed(context.Background(), []string{"test"}, "", opts, nil); errPick == nil {
		t.Error("mixed-provider selection accepted a group-restricted request without its resolver")
	}
}

func TestHomeDispatchEnforcesClientAccess(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		opts     cliproxyexecutor.Options
		resolver CredentialGroupResolver
		allowed  bool
	}{
		{name: "tenant", opts: tenantOptions(1, true)},
		{name: "foreign group", opts: clientGroupOptions("1", false), resolver: testCredentialGroupResolver{memberships: map[string]map[int64]int{"home-index": {2: 0}}}},
		{name: "missing resolver", opts: clientGroupOptions("1", true)},
		{name: "matching group", opts: clientGroupOptions("1", false), resolver: testCredentialGroupResolver{memberships: map[string]map[int64]int{"home-index": {1: 0}}}, allowed: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			manager := NewManager(nil, &RoundRobinSelector{}, nil)
			manager.SetConfig(&internalconfig.Config{Home: internalconfig.HomeConfig{Enabled: true}})
			manager.RegisterExecutor(schedulerTestExecutor{})
			manager.SetCredentialGroupResolver(testCase.resolver)
			manager.PublishHomeDispatch(&accessIsolationHomeDispatcher{response: []byte(`{"auth":{"id":"home-auth","index":"home-index","provider":"test","status":"active"},"auth_index":"home-index"}`)}, executionregistry.New(), 1)
			selection, errSelection := manager.pickHomeDispatchSelection(context.Background(), "test-model", testCase.opts)
			if selection != nil {
				selection.End("test_complete")
			}
			if testCase.allowed && errSelection != nil {
				t.Fatalf("allowed Home dispatch failed: %v", errSelection)
			}
			if !testCase.allowed && errSelection == nil {
				t.Fatal("Home dispatch bypassed client access restrictions")
			}
		})
	}
}

func TestTenantExecutionCannotUseHomeGlobalDispatch(t *testing.T) {
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	manager.SetConfig(&internalconfig.Config{Home: internalconfig.HomeConfig{Enabled: true}})
	manager.RegisterExecutor(&homeExecutionExecutor{})
	manager.PublishHomeDispatch(homeExecutionDispatcher{}, executionregistry.New(), 1)
	for _, count := range []bool{false, true} {
		var errExecute error
		if count {
			_, errExecute = manager.ExecuteCount(context.Background(), []string{"home-execution"}, cliproxyexecutor.Request{Model: "test-model"}, tenantOptions(1, true))
		} else {
			_, errExecute = manager.Execute(context.Background(), []string{"home-execution"}, cliproxyexecutor.Request{Model: "test-model"}, tenantOptions(1, true))
		}
		if errExecute == nil {
			t.Errorf("tenant used global Home execution (count=%v)", count)
		}
	}
}
