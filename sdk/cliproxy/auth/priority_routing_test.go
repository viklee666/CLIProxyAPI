package auth

import (
	"context"
	"net/http"
	"testing"
	"time"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
)

func TestManagerCodexPriorityOverridesClientSessionBinding(t *testing.T) {
	for strategy, newFallback := range map[string]func() Selector{
		"round-robin":          func() Selector { return &RoundRobinSelector{} },
		"weighted-round-robin": func() Selector { return &WeightedRoundRobinSelector{} },
		"fill-first":           func() Selector { return &FillFirstSelector{} },
	} {
		for client, options := range priorityRoutingClientOptions() {
			for _, path := range []string{"single", "mixed", "cross-provider"} {
				t.Run(strategy+"/"+client+"/"+path, func(t *testing.T) {
					ctx := context.Background()
					const model = "codex-priority-model"
					selector := NewSessionAffinitySelector(newFallback())
					t.Cleanup(selector.Stop)
					manager := NewManager(nil, selector, nil)
					manager.RegisterExecutor(schedulerTestExecutor{provider: "codex"})
					oauth := &Auth{
						ID: t.Name() + "-oauth", Provider: "codex", Status: StatusActive,
						Attributes: map[string]string{"priority": "0", "websockets": "false"},
						Metadata:   map[string]any{"access_token": "oauth-token"},
					}
					apiKey := &Auth{
						ID: t.Name() + "-api-key", Provider: "codex", Status: StatusActive,
						Attributes: map[string]string{
							"priority": "10", "websockets": "false",
							"api_key": "test-key", "base_url": "https://provider.example/v1",
						},
					}
					if path == "cross-provider" {
						apiKey.Provider = "priority-responses-provider"
						manager.RegisterExecutor(schedulerTestExecutor{provider: apiKey.Provider})
					}
					register := func(candidate *Auth, supportedModel string) {
						t.Helper()
						registry.GetGlobalRegistry().RegisterClient(candidate.ID, candidate.Provider, []*registry.ModelInfo{{ID: supportedModel}})
						t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(candidate.ID) })
						if _, errRegister := manager.Register(WithSkipPersist(ctx), candidate); errRegister != nil {
							t.Fatalf("Register(%s): %v", candidate.ID, errRegister)
						}
					}
					register(oauth, model)
					register(&Auth{
						ID: t.Name() + "-other-model", Provider: "codex", Status: StatusActive,
						Attributes: map[string]string{"priority": "1000"},
					}, "unrelated-model")
					pick := func(wantID string) {
						t.Helper()
						var selected *Auth
						var errPick error
						if path == "single" {
							selected, _, errPick = manager.pickNext(ctx, "codex", model, options(), nil)
						} else {
							providers := []string{"codex"}
							if path == "cross-provider" {
								providers = append(providers, apiKey.Provider)
							}
							selected, _, _, errPick = manager.pickNextMixed(ctx, providers, model, options(), nil)
						}
						if errPick != nil || selected == nil || selected.ID != wantID {
							t.Fatalf("pick = auth=%v error=%v, want %q", selected, errPick, wantID)
						}
					}

					pick(oauth.ID)
					register(apiKey, model)
					pick(apiKey.ID)
					pick(apiKey.ID)
				})
			}
		}
	}
}

func priorityRoutingClientOptions() map[string]func() cliproxyexecutor.Options {
	return map[string]func() cliproxyexecutor.Options{
		"no-session": func() cliproxyexecutor.Options { return cliproxyexecutor.Options{} },
		"codex": func() cliproxyexecutor.Options {
			return cliproxyexecutor.Options{Headers: http.Header{"Session-Id": {"codex-session"}}}
		},
		"pi": func() cliproxyexecutor.Options {
			return cliproxyexecutor.Options{Headers: http.Header{"X-Slot-Session-Id": {"pi-session"}}}
		},
		"prompt-cache-key": func() cliproxyexecutor.Options {
			return cliproxyexecutor.Options{OriginalRequest: []byte(`{"prompt_cache_key":"stable-cache-key"}`)}
		},
		"lcp": func() cliproxyexecutor.Options {
			return cliproxyexecutor.Options{
				SourceFormat:    sdktranslator.FormatOpenAI,
				OriginalRequest: []byte(`{"messages":[{"role":"user","content":"stable prompt"}]}`),
				Metadata:        map[string]any{cliproxyexecutor.CallerScopeMetadataKey: "priority-caller"},
			}
		},
	}
}

func TestSessionAffinityPriorityOverridesParentBinding(t *testing.T) {
	for _, client := range []struct {
		name   string
		header string
		parent string
	}{
		{name: "codex", header: "Session-Id", parent: "X-Codex-Parent-Thread-Id"},
		{name: "pi", header: "X-Slot-Session-Id", parent: "X-Parent-Slot-Session-Id"},
	} {
		t.Run(client.name, func(t *testing.T) {
			selector := NewSessionAffinitySelector(&RoundRobinSelector{})
			t.Cleanup(selector.Stop)
			low := &Auth{ID: "oauth", Provider: "codex", Attributes: map[string]string{"priority": "0"}}
			high := &Auth{ID: "api-key", Provider: "codex", Attributes: map[string]string{"priority": "10"}}
			parentOpts := cliproxyexecutor.Options{Headers: http.Header{client.header: {"parent"}}}
			if _, errParent := selector.Pick(context.Background(), "codex", "model", parentOpts, []*Auth{low}); errParent != nil {
				t.Fatal(errParent)
			}
			childOpts := cliproxyexecutor.Options{Headers: http.Header{client.header: {"child"}, client.parent: {"parent"}}}
			got, errPick := selector.Pick(context.Background(), "codex", "model", childOpts, []*Auth{low, high})
			if errPick != nil || got == nil || got.ID != high.ID {
				t.Fatalf("child Pick() = auth=%v error=%v, want %s", got, errPick, high.ID)
			}
		})
	}
}

func TestSessionAffinityAdaptiveFallbackUsesAllPriorityTiers(t *testing.T) {
	for client, options := range priorityRoutingClientOptions() {
		t.Run(client, func(t *testing.T) {
			adaptive := NewAdaptiveSelector(internalconfig.AdaptiveRoutingConfig{
				TopK: 1, Weights: internalconfig.AdaptiveRoutingWeights{Load: 1},
			})
			selector := NewSessionAffinitySelector(adaptive)
			t.Cleanup(selector.Stop)
			high := adaptiveTestAuth("high-busy", 100)
			low := adaptiveTestAuth("low-idle", 0)
			release := adaptive.BeginAttempt(high.ID)
			defer release()
			got, errPick := selector.Pick(context.Background(), "codex", "model", options(), []*Auth{high, low})
			if errPick != nil || got == nil || got.ID != low.ID {
				t.Fatalf("adaptive Pick() = auth=%v error=%v, want idle lower-priority auth %s", got, errPick, low.ID)
			}
		})
	}
}

func TestSessionAffinityWeightedPrioritySkipsZeroWeight(t *testing.T) {
	ctx := context.Background()
	selector := NewSessionAffinitySelector(&WeightedRoundRobinSelector{})
	t.Cleanup(selector.Stop)
	manager := NewManager(nil, selector, nil)
	manager.RegisterExecutor(schedulerTestExecutor{provider: "codex"})
	for _, candidate := range []*Auth{
		{ID: "excluded-high", Provider: "codex", Attributes: map[string]string{"priority": "10", AttributeWeight: "0"}},
		{ID: "available-low", Provider: "codex", Attributes: map[string]string{"priority": "0", AttributeWeight: "1"}},
	} {
		if _, errRegister := manager.Register(WithSkipPersist(ctx), candidate); errRegister != nil {
			t.Fatal(errRegister)
		}
	}
	got, errPick := manager.SelectAuth(ctx, "codex", "", priorityRoutingClientOptions()["codex"]())
	if errPick != nil || got == nil || got.ID != "available-low" {
		t.Fatalf("weighted Pick() = auth=%v error=%v, want available-low", got, errPick)
	}
}

func TestSessionAffinityAdaptivePreservesHealthyBindingAcrossPriorityTiers(t *testing.T) {
	for client, options := range priorityRoutingClientOptions() {
		t.Run(client, func(t *testing.T) {
			adaptive := NewAdaptiveSelector(internalconfig.AdaptiveRoutingConfig{
				TopK: 1, Weights: internalconfig.AdaptiveRoutingWeights{Priority: 1},
			})
			selector := NewSessionAffinitySelector(adaptive)
			t.Cleanup(selector.Stop)
			low := adaptiveTestAuth("low", 0)
			high := adaptiveTestAuth("high", 100)
			if _, errPick := selector.Pick(context.Background(), "codex", "model", options(), []*Auth{low}); errPick != nil {
				t.Fatal(errPick)
			}
			want := low.ID
			if client == "no-session" {
				want = high.ID
			}
			got, errPick := selector.Pick(context.Background(), "codex", "model", options(), []*Auth{low, high})
			if errPick != nil || got == nil || got.ID != want {
				t.Fatalf("adaptive binding Pick() = auth=%v error=%v, want %s", got, errPick, want)
			}
		})
	}
}

func TestSessionAffinityAdaptiveLCPHonorsStickyEscape(t *testing.T) {
	adaptive := NewAdaptiveSelector(internalconfig.AdaptiveRoutingConfig{
		TopK: 1, EWMAAlpha: 1, Weights: internalconfig.AdaptiveRoutingWeights{SuccessRate: 1},
		StickyEscape: internalconfig.AdaptiveStickyEscape{Enabled: true, MinSamples: 1, ErrorRateThreshold: 0.5},
	})
	selector := NewSessionAffinitySelector(adaptive)
	t.Cleanup(selector.Stop)
	low := adaptiveTestAuth("low", 0)
	high := adaptiveTestAuth("high", 100)
	options := priorityRoutingClientOptions()["lcp"]
	if _, errPick := selector.Pick(context.Background(), "codex", "model", options(), []*Auth{low}); errPick != nil {
		t.Fatal(errPick)
	}
	adaptive.ObserveResult(Result{AuthID: low.ID, Success: false})
	got, errPick := selector.Pick(context.Background(), "codex", "model", options(), []*Auth{low, high})
	if errPick != nil || got == nil || got.ID != high.ID {
		t.Fatalf("adaptive LCP escape = auth=%v error=%v, want %s", got, errPick, high.ID)
	}
}

func TestManagerAdaptiveSelectionUsesResolvedAliasAvailability(t *testing.T) {
	withQuotaCooldownEnabled(t)
	for _, affinity := range []bool{false, true} {
		name := "adaptive"
		if affinity {
			name = "affinity-adaptive"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			const alias, upstream = "adaptive-priority-alias", "adaptive-priority-upstream"
			adaptive := NewAdaptiveSelector(internalconfig.AdaptiveRoutingConfig{
				TopK: 1, Weights: internalconfig.AdaptiveRoutingWeights{Load: 1},
			})
			var selector Selector = adaptive
			if affinity {
				sticky := NewSessionAffinitySelector(adaptive)
				t.Cleanup(sticky.Stop)
				selector = sticky
			}
			manager := NewManager(nil, selector, nil)
			manager.RegisterExecutor(schedulerTestExecutor{provider: "codex"})
			low := adaptiveTestAuth(t.Name()+"-low", 0)
			high := adaptiveTestAuth(t.Name()+"-high", 100)
			for _, candidate := range []*Auth{low, high} {
				registry.GetGlobalRegistry().RegisterClient(candidate.ID, "codex", []*registry.ModelInfo{{ID: alias}, {ID: upstream}})
				t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(candidate.ID) })
				if _, errRegister := manager.Register(WithSkipPersist(ctx), candidate); errRegister != nil {
					t.Fatal(errRegister)
				}
				retryAfter := time.Hour
				manager.MarkResult(ctx, Result{
					AuthID: candidate.ID, Provider: "codex", Model: alias, RetryAfter: &retryAfter,
					Error: &Error{HTTPStatus: http.StatusTooManyRequests, Message: "old alias quota"},
				})
			}
			manager.SetOAuthModelAlias(map[string][]internalconfig.OAuthModelAlias{
				"codex": {{Name: upstream, Alias: alias, Fork: true}},
			})
			release := adaptive.BeginAttempt(high.ID)
			defer release()
			got, errPick := manager.SelectAuth(ctx, "codex", alias, priorityRoutingClientOptions()["codex"]())
			if errPick != nil || got == nil || got.ID != low.ID {
				t.Fatalf("resolved alias Pick() = auth=%v error=%v, want %s", got, errPick, low.ID)
			}
		})
	}
}
