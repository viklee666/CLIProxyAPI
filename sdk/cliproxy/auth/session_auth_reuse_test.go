package auth

import (
	"context"
	"testing"
	"time"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

func TestManagerCanReuseSessionAuth(t *testing.T) {
	const model = "session-auth-reuse-model"
	for _, testCase := range []struct {
		name      string
		selector  func() Selector
		configure func(*Manager, *Auth, *Auth)
		opts      cliproxyexecutor.Options
		highModel string
		wantLow   bool
		wantHigh  bool
	}{
		{name: "higher priority", wantHigh: true},
		{name: "explicit never keeps strict tier", selector: func() Selector {
			return NewSessionAffinitySelectorWithConfig(SessionAffinityConfig{Fallback: &RoundRobinSelector{}, RebindOn: SessionAffinityRebindOnNever})
		}, wantHigh: true},
		{name: "explicit quota keeps strict tier", selector: func() Selector {
			return NewSessionAffinitySelectorWithConfig(SessionAffinityConfig{Fallback: &RoundRobinSelector{}, RebindOn: SessionAffinityRebindOnQuota})
		}, wantHigh: true},
		{name: "explicit never respects group revocation", selector: func() Selector {
			return NewSessionAffinitySelectorWithConfig(SessionAffinityConfig{Fallback: &RoundRobinSelector{}, RebindOn: SessionAffinityRebindOnNever})
		}, opts: clientGroupOptions("1", false), configure: func(manager *Manager, low, high *Auth) {
			manager.SetCredentialGroupResolver(testCredentialGroupResolver{memberships: map[string]map[int64]int{
				low.Index: {2: 0}, high.Index: {1: 10},
			}})
		}, wantHigh: true},
		{name: "equal priority", configure: func(_ *Manager, low, high *Auth) { high.Attributes["priority"] = low.Attributes["priority"] }, wantLow: true, wantHigh: true},
		{name: "all negative", configure: func(_ *Manager, low, high *Auth) {
			low.Attributes["priority"], high.Attributes["priority"] = "-2", "-1"
		}, wantHigh: true},
		{name: "disabled higher priority", configure: func(_ *Manager, _, high *Auth) { high.Disabled = true }, wantLow: true},
		{name: "higher priority in cooldown keeps tier closed", configure: func(_ *Manager, _, high *Auth) {
			high.Unavailable = true
			high.ModelStates = map[string]*ModelState{model: {Unavailable: true, NextRetryAfter: time.Now().Add(time.Hour)}}
		}},
		{name: "unrelated higher priority model", highModel: "unrelated-reuse-model", wantLow: true},
		{name: "weighted zero weight", selector: func() Selector { return &WeightedRoundRobinSelector{} }, configure: func(_ *Manager, _, high *Auth) {
			high.Attributes[AttributeWeight] = "0"
		}, wantLow: true},
		{name: "affinity weighted zero weight", selector: func() Selector { return NewSessionAffinitySelector(&WeightedRoundRobinSelector{}) }, configure: func(_ *Manager, _, high *Auth) {
			high.Attributes[AttributeWeight] = "0"
		}, wantLow: true},
		{name: "adaptive keeps strict tier", selector: func() Selector { return NewAdaptiveSelector(internalconfig.AdaptiveRoutingConfig{}) }, wantHigh: true},
		{name: "affinity adaptive keeps strict tier", selector: func() Selector {
			return NewSessionAffinitySelector(NewAdaptiveSelector(internalconfig.AdaptiveRoutingConfig{}))
		}, wantHigh: true},
		{name: "group priority override", opts: clientGroupOptions("1", false), configure: func(manager *Manager, low, high *Auth) {
			manager.SetCredentialGroupResolver(testCredentialGroupResolver{memberships: map[string]map[int64]int{
				low.Index: {1: 10}, high.Index: {1: 0},
			}})
		}, wantLow: true},
		{name: "higher priority outside group", opts: clientGroupOptions("1", false), configure: func(manager *Manager, low, high *Auth) {
			manager.SetCredentialGroupResolver(testCredentialGroupResolver{memberships: map[string]map[int64]int{
				low.Index: {1: 0}, high.Index: {2: 10},
			}})
		}, wantLow: true},
		{name: "group resolver missing", opts: clientGroupOptions("1", true)},
		{name: "higher priority owned by tenant", configure: func(manager *Manager, _, high *Auth) {
			high.Attributes[AttributeTenantID] = "1"
			manager.SetCredentialOwnershipResolver(testCredentialOwnershipResolver{owners: map[string]int64{high.Index: 1}})
		}, wantLow: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			selector := Selector(&RoundRobinSelector{})
			if testCase.selector != nil {
				selector = testCase.selector()
			}
			if stoppable, ok := selector.(StoppableSelector); ok {
				t.Cleanup(stoppable.Stop)
			}
			manager := NewManager(nil, selector, nil)
			providers := []string{"codex", "reuse-responses-provider"}
			for _, provider := range providers {
				manager.RegisterExecutor(schedulerTestExecutor{provider: provider})
			}
			low := &Auth{
				ID: t.Name() + "-oauth", Index: "reuse-oauth", Provider: providers[0], Status: StatusActive,
				Attributes: map[string]string{"priority": "-1", "websockets": "true"},
			}
			high := &Auth{
				ID: t.Name() + "-upstream", Index: "reuse-upstream", Provider: providers[1], Status: StatusActive,
				Attributes: map[string]string{"priority": "1", "api_key": "test-key"},
			}
			if testCase.configure != nil {
				testCase.configure(manager, low, high)
			}
			ctx := WithSkipPersist(context.Background())
			for _, auth := range []*Auth{low, high} {
				supportedModel := model
				if auth == high && testCase.highModel != "" {
					supportedModel = testCase.highModel
				}
				registry.GetGlobalRegistry().RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{{ID: supportedModel}})
				t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(auth.ID) })
				if _, errRegister := manager.Register(ctx, auth); errRegister != nil {
					t.Fatal(errRegister)
				}
			}
			for authID, want := range map[string]bool{low.ID: testCase.wantLow, high.ID: testCase.wantHigh, "missing-auth": false} {
				if got := manager.CanReuseSessionAuth(ctx, authID, providers, model+"(high)", testCase.opts); got != want {
					t.Errorf("CanReuseSessionAuth(%q) = %t, want %t", authID, got, want)
				}
			}
		})
	}
}

func TestManagerCanReuseSessionAuthUsesResolvedModelAvailability(t *testing.T) {
	const alias, upstream = "session-reuse-alias", "session-reuse-upstream"
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	manager.RegisterExecutor(schedulerTestExecutor{provider: "codex"})
	manager.SetOAuthModelAlias(map[string][]internalconfig.OAuthModelAlias{
		"codex": {{Name: upstream, Alias: alias, Fork: true}},
	})
	auth := &Auth{
		ID: t.Name(), Provider: "codex", Status: StatusActive,
		ModelStates: map[string]*ModelState{alias: {Unavailable: true, NextRetryAfter: time.Now().Add(time.Hour)}},
	}
	registry.GetGlobalRegistry().RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{{ID: upstream}})
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(auth.ID) })
	ctx := WithSkipPersist(context.Background())
	if _, errRegister := manager.Register(ctx, auth); errRegister != nil {
		t.Fatal(errRegister)
	}
	if !manager.CanReuseSessionAuth(ctx, auth.ID, []string{"codex"}, alias, cliproxyexecutor.Options{}) {
		t.Fatal("old alias cooldown prevented reuse of the healthy upstream model")
	}
}
