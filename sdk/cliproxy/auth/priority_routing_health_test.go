package auth

import (
	"context"
	"testing"
	"time"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

const priorityHealthRoutingModel = "priority-health-model"

func newPriorityHealthRoutingManager(t *testing.T, selector Selector) (*Manager, *Auth, *Auth) {
	t.Helper()
	manager := NewManager(nil, selector, nil)
	manager.RegisterExecutor(schedulerTestExecutor{provider: "codex"})
	manager.RegisterExecutor(schedulerTestExecutor{provider: "gemini"})
	high := &Auth{ID: t.Name() + "-high", Provider: "codex", Status: StatusActive, Attributes: map[string]string{"priority": "10"}}
	low := &Auth{ID: t.Name() + "-low", Provider: "gemini", Status: StatusActive, Attributes: map[string]string{"priority": "0", AttributeVirtualSource: t.Name() + "-file.json"}}
	for _, auth := range []*Auth{high, low} {
		registry.GetGlobalRegistry().RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{{ID: priorityHealthRoutingModel}})
		auth := auth
		t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(auth.ID) })
		if _, errRegister := manager.Register(WithSkipPersist(context.Background()), auth); errRegister != nil {
			t.Fatalf("Register(%s): %v", auth.ID, errRegister)
		}
	}
	registeredHigh, _ := manager.GetByID(high.ID)
	registeredLow, _ := manager.GetByID(low.ID)
	return manager, registeredHigh, registeredLow
}

func pickPriorityHealthMixed(t *testing.T, manager *Manager, opts cliproxyexecutor.Options, tried map[string]struct{}) (*Auth, error) {
	t.Helper()
	selected, _, _, errPick := manager.pickNextMixed(context.Background(), []string{"codex", "gemini"}, priorityHealthRoutingModel, opts, tried)
	return selected, errPick
}

func markPriorityHealthFailure(manager *Manager, auth *Auth, opts cliproxyexecutor.Options) {
	ctx := cliproxyexecutor.WithUpstreamAttemptTracker(context.Background())
	cliproxyexecutor.MarkUpstreamAttempt(ctx)
	manager.MarkResult(ctx, Result{
		AuthID: auth.ID, Provider: auth.Provider, Model: priorityHealthRoutingModel,
		Options: opts, Error: &Error{Code: ErrorCodeTransientTransport, Retryable: true},
	})
}

func TestManagerPriorityHealthStrictAcrossStrategiesAndProviders(t *testing.T) {
	strategies := map[string]func() Selector{
		"round-robin":          func() Selector { return &RoundRobinSelector{} },
		"fill-first":           func() Selector { return &FillFirstSelector{} },
		"weighted-round-robin": func() Selector { return &WeightedRoundRobinSelector{} },
		"adaptive": func() Selector {
			return NewAdaptiveSelector(internalconfig.AdaptiveRoutingConfig{TopK: 1, Weights: internalconfig.AdaptiveRoutingWeights{Load: 1}})
		},
	}
	for name, newSelector := range strategies {
		t.Run(name, func(t *testing.T) {
			manager, high, low := newPriorityHealthRoutingManager(t, newSelector())
			selected, errPick := pickPriorityHealthMixed(t, manager, cliproxyexecutor.Options{}, nil)
			if errPick != nil || selected == nil || selected.ID != high.ID {
				t.Fatalf("initial selection = %v, error=%v, want high provider credential", selected, errPick)
			}
			markPriorityHealthFailure(manager, high, cliproxyexecutor.Options{})
			selected, errPick = pickPriorityHealthMixed(t, manager, cliproxyexecutor.Options{}, nil)
			if errPick != nil || selected == nil || selected.ID != high.ID {
				t.Fatalf("fifth-or-fewer failure selection = %v, error=%v, want high credential", selected, errPick)
			}
			for range 5 {
				markPriorityHealthFailure(manager, high, cliproxyexecutor.Options{})
			}
			selected, errPick = pickPriorityHealthMixed(t, manager, cliproxyexecutor.Options{}, nil)
			if errPick != nil || selected == nil || selected.ID != low.ID {
				t.Fatalf("sixth failure selection = %v, error=%v, want lower file credential %s", selected, errPick, low.ID)
			}
		})
	}
}

func TestManagerPriorityHealthCoolingTierDoesNotLeakBeforeDemotion(t *testing.T) {
	manager, high, low := newPriorityHealthRoutingManager(t, &RoundRobinSelector{})
	high.Unavailable = true
	high.ModelStates = map[string]*ModelState{priorityHealthRoutingModel: {Unavailable: true, NextRetryAfter: time.Now().Add(time.Hour)}}
	if _, errUpdate := manager.Update(WithSkipPersist(context.Background()), high); errUpdate != nil {
		t.Fatal(errUpdate)
	}
	selected, errPick := pickPriorityHealthMixed(t, manager, cliproxyexecutor.Options{}, nil)
	if errPick == nil || selected != nil {
		t.Fatalf("cooling preferred tier selection = %v, error=%v, want no lower-tier selection", selected, errPick)
	}
	for range 6 {
		markPriorityHealthFailure(manager, high, cliproxyexecutor.Options{})
	}
	selected, errPick = pickPriorityHealthMixed(t, manager, cliproxyexecutor.Options{}, nil)
	if errPick != nil || selected == nil || selected.ID != low.ID {
		t.Fatalf("demoted cooling tier selection = %v, error=%v, want %s", selected, errPick, low.ID)
	}
}

func TestManagerPriorityHealthTriedHighTierDoesNotFallThrough(t *testing.T) {
	manager, high, _ := newPriorityHealthRoutingManager(t, &RoundRobinSelector{})
	selected, errPick := pickPriorityHealthMixed(t, manager, cliproxyexecutor.Options{}, map[string]struct{}{high.ID: {}})
	if errPick == nil || selected != nil {
		t.Fatalf("tried healthy high tier selection = %v, error=%v, want no lower-tier fallback", selected, errPick)
	}
}

func TestManagerPriorityHealthConcurrencyCapAndRPMBoundaries(t *testing.T) {
	manager, high, low := newPriorityHealthRoutingManager(t, &FillFirstSelector{})
	high.Metadata = map[string]any{"concurrency_limit": 1}
	if _, errUpdate := manager.Update(WithSkipPersist(context.Background()), high); errUpdate != nil {
		t.Fatal(errUpdate)
	}
	manager.requestLimiter.states = map[string]*requestLimitState{"auth:" + high.ID: {active: 1}}
	selected, errPick := pickPriorityHealthMixed(t, manager, cliproxyexecutor.Options{}, nil)
	if errPick != nil || selected == nil || selected.ID != low.ID {
		t.Fatalf("at-cap selection = %v, error=%v, want %s", selected, errPick, low.ID)
	}
	manager.requestLimiter.states["auth:"+high.ID].active = 0
	selected, errPick = pickPriorityHealthMixed(t, manager, cliproxyexecutor.Options{}, nil)
	if errPick != nil || selected == nil || selected.ID != high.ID {
		t.Fatalf("below-cap selection = %v, error=%v, want %s", selected, errPick, high.ID)
	}
	high.Metadata["concurrency_limit"] = 0
	if _, errUpdate := manager.Update(WithSkipPersist(context.Background()), high); errUpdate != nil {
		t.Fatal(errUpdate)
	}
	manager.requestLimiter.states["auth:"+high.ID].active = 1
	selected, errPick = pickPriorityHealthMixed(t, manager, cliproxyexecutor.Options{}, nil)
	if errPick != nil || selected == nil || selected.ID != high.ID {
		t.Fatalf("unlimited selection = %v, error=%v, want %s", selected, errPick, high.ID)
	}
	high.Metadata = map[string]any{"rpm_limit": 1}
	if _, errUpdate := manager.Update(WithSkipPersist(context.Background()), high); errUpdate != nil {
		t.Fatal(errUpdate)
	}
	manager.requestLimiter.states["auth:"+high.ID] = &requestLimitState{starts: []time.Time{time.Now()}}
	selected, errPick = pickPriorityHealthMixed(t, manager, cliproxyexecutor.Options{}, nil)
	if errPick != nil || selected == nil || selected.ID != high.ID {
		t.Fatalf("RPM-saturated selection = %v, error=%v, want %s", selected, errPick, high.ID)
	}
}

func priorityHealthClientOptions(group string, allowAll bool) cliproxyexecutor.Options {
	return cliproxyexecutor.Options{Metadata: map[string]any{
		cliproxyexecutor.ClientKeyIDMetadataKey:          "client-" + group,
		cliproxyexecutor.ClientGroupIDsMetadataKey:       group,
		cliproxyexecutor.ClientAllowAllGroupsMetadataKey: allowAll,
	}}
}

func TestManagerPriorityHealthGroupIsolationAndAdminAggregate(t *testing.T) {
	manager, high, low := newPriorityHealthRoutingManager(t, &RoundRobinSelector{})
	high.Index = "priority-health-high-index"
	low.Index = "priority-health-low-index"
	if _, errUpdate := manager.Update(WithSkipPersist(context.Background()), high); errUpdate != nil {
		t.Fatal(errUpdate)
	}
	if _, errUpdate := manager.Update(WithSkipPersist(context.Background()), low); errUpdate != nil {
		t.Fatal(errUpdate)
	}
	manager.SetCredentialGroupResolver(testCredentialGroupResolver{memberships: map[string]map[int64]int{
		high.Index: {1: 10, 2: 10}, low.Index: {1: 0, 2: 0},
	}})
	groupOne := priorityHealthClientOptions("1", false)
	for range 6 {
		markPriorityHealthFailure(manager, high, groupOne)
	}
	groupTwo := priorityHealthClientOptions("2", false)
	selected, errPick := pickPriorityHealthMixed(t, manager, groupTwo, nil)
	if errPick != nil || selected == nil || selected.ID != high.ID {
		t.Fatalf("isolated group selection = %v, error=%v, want %s", selected, errPick, high.ID)
	}
	admin := priorityHealthClientOptions("1", true)
	selected, errPick = pickPriorityHealthMixed(t, manager, admin, nil)
	if errPick != nil || selected == nil || selected.ID != low.ID {
		t.Fatalf("admin aggregate selection = %v, error=%v, want %s", selected, errPick, low.ID)
	}
}
