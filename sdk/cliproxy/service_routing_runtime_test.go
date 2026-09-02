package cliproxy

import (
	"context"
	"errors"
	"testing"
	"time"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

type stubUsagePlugin struct{}

func (stubUsagePlugin) HandleUsage(context.Context, usage.Record) {}

type failingCooldownStore struct{}

func (failingCooldownStore) Load(context.Context) ([]coreauth.CooldownStateRecord, error) {
	return nil, nil
}

func (failingCooldownStore) Save(context.Context, []coreauth.CooldownStateRecord) error {
	return errors.New("persist failed")
}

func TestNewRoutingSelectorUnregistersAdaptiveUsagePlugin(t *testing.T) {
	dropped := stubUsagePlugin{}
	usage.RegisterNamedPlugin(adaptiveUsagePluginName, dropped)
	t.Cleanup(func() {
		usage.UnregisterNamedPlugin(adaptiveUsagePluginName)
	})

	selector := newRoutingSelector(routingRuntimeState{strategy: "weighted-round-robin", sessionAffinityTTL: time.Hour})
	if _, ok := selector.(*coreauth.WeightedRoundRobinSelector); !ok {
		t.Fatalf("selector type = %T, want *WeightedRoundRobinSelector", selector)
	}
	if got := usage.UnregisterNamedPlugin(adaptiveUsagePluginName); got != nil {
		t.Fatal("adaptive usage plugin still registered after switching to WRR")
	}
}

func TestApplyManagerConfigUnregistersAdaptiveUsagePluginWhenLeavingAdaptive(t *testing.T) {
	dropped := stubUsagePlugin{}
	usage.RegisterNamedPlugin(adaptiveUsagePluginName, dropped)
	t.Cleanup(func() {
		usage.UnregisterNamedPlugin(adaptiveUsagePluginName)
	})

	adaptiveState := normalizedRoutingRuntimeState(&internalconfig.Config{
		Routing: internalconfig.RoutingConfig{Strategy: "adaptive"},
	})
	service := &Service{
		coreManager:         coreauth.NewManager(nil, coreauth.NewAdaptiveSelector(internalconfig.AdaptiveRoutingConfig{}), nil),
		appliedRoutingState: &adaptiveState,
	}
	commit := configCommit{
		cfg:      &internalconfig.Config{Routing: internalconfig.RoutingConfig{Strategy: "fill-first"}},
		sequence: 1,
	}
	if !service.applyManagerConfig(context.Background(), commit) {
		t.Fatal("applyManagerConfig failed")
	}
	if _, ok := service.coreManager.Selector().(*coreauth.FillFirstSelector); !ok {
		t.Fatalf("selector type = %T, want *FillFirstSelector", service.coreManager.Selector())
	}
	if got := usage.UnregisterNamedPlugin(adaptiveUsagePluginName); got != nil {
		t.Fatal("adaptive usage plugin still registered after switching to fill-first")
	}
}

func TestApplyManagerConfigDoesNotSwapSelectorWhenCooldownPersistFails(t *testing.T) {
	initial := &trackingStoppableSelector{}
	manager := coreauth.NewManager(nil, initial, nil)
	manager.SetCooldownStateStore(failingCooldownStore{})
	service := &Service{coreManager: manager}

	commit := configCommit{
		cfg:      &internalconfig.Config{Routing: internalconfig.RoutingConfig{Strategy: "fill-first"}},
		sequence: 1,
	}
	if service.applyManagerConfig(context.Background(), commit) {
		t.Fatal("applyManagerConfig succeeded despite cooldown persist failure")
	}
	if initial.stopped {
		t.Fatal("selector was stopped before cooldown persist succeeded")
	}
	if got := manager.Selector(); got != initial {
		t.Fatalf("selector = %p, want original %p", got, initial)
	}
	if service.appliedRoutingState != nil {
		t.Fatal("appliedRoutingState updated after failed cooldown persist")
	}
}

func TestApplyManagerConfigIgnoresUnusedAdaptiveAndTTL(t *testing.T) {
	initial := &trackingStoppableSelector{}
	state := routingRuntimeState{
		strategy:           "round-robin",
		sessionAffinityTTL: time.Hour,
		adaptive:           internalconfig.AdaptiveRoutingConfig{TopK: 3},
	}
	service := &Service{
		coreManager:         coreauth.NewManager(nil, initial, nil),
		appliedRoutingState: &state,
	}

	commit := configCommit{
		cfg: &internalconfig.Config{Routing: internalconfig.RoutingConfig{
			Strategy:           "round-robin",
			SessionAffinityTTL: "2h",
			Adaptive:           internalconfig.AdaptiveRoutingConfig{TopK: 9, EWMAAlpha: 0.5},
		}},
		sequence: 1,
	}
	if !service.applyManagerConfig(context.Background(), commit) {
		t.Fatal("applyManagerConfig failed")
	}
	if initial.stopped {
		t.Fatal("unused adaptive/TTL YAML rebuild stopped the selector")
	}
	if got := service.coreManager.Selector(); got != initial {
		t.Fatalf("selector = %p, want original %p", got, initial)
	}
}
