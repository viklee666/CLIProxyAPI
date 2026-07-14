package auth

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

type blockingAdaptiveExecutor struct {
	schedulerTestExecutor
	started chan struct{}
	release chan struct{}
}

func (e *blockingAdaptiveExecutor) Identifier() string { return "codex" }

func (e *blockingAdaptiveExecutor) Execute(_ context.Context, _ *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	close(e.started)
	<-e.release
	return cliproxyexecutor.Response{Payload: []byte(`{"ok":true}`)}, nil
}

func adaptiveTestAuth(id string, priority int) *Auth {
	return &Auth{
		ID:       id,
		Index:    id + "-index",
		Provider: "codex",
		Status:   StatusActive,
		Attributes: map[string]string{
			"priority": strconv.Itoa(priority),
		},
	}
}

func TestAdaptiveSelectorScoresPriorityLoadSuccessAndTTFT(t *testing.T) {
	authA := adaptiveTestAuth("auth-a", 1)
	authB := adaptiveTestAuth("auth-b", 9)

	prioritySelector := NewAdaptiveSelector(internalconfig.AdaptiveRoutingConfig{
		TopK: 1,
		Weights: internalconfig.AdaptiveRoutingWeights{
			Priority: 1,
		},
	})
	selected, errPick := prioritySelector.Pick(context.Background(), "codex", "gpt-test", cliproxyexecutor.Options{}, []*Auth{authA, authB})
	if errPick != nil || selected.ID != authB.ID {
		t.Fatalf("priority Pick() = auth=%v err=%v", selected, errPick)
	}

	loadSelector := NewAdaptiveSelector(internalconfig.AdaptiveRoutingConfig{
		TopK: 1,
		Weights: internalconfig.AdaptiveRoutingWeights{
			Load: 1,
		},
	})
	release := loadSelector.BeginAttempt(authA.ID)
	selected, errPick = loadSelector.Pick(context.Background(), "codex", "gpt-test", cliproxyexecutor.Options{}, []*Auth{authA, authB})
	if errPick != nil || selected.ID != authB.ID {
		t.Fatalf("load Pick() = auth=%v err=%v", selected, errPick)
	}
	release()

	successSelector := NewAdaptiveSelector(internalconfig.AdaptiveRoutingConfig{
		TopK: 1,
		Weights: internalconfig.AdaptiveRoutingWeights{
			SuccessRate: 1,
		},
	})
	successSelector.ObserveResult(Result{AuthID: authA.ID, Success: false})
	successSelector.ObserveResult(Result{AuthID: authB.ID, Success: true})
	selected, errPick = successSelector.Pick(context.Background(), "codex", "gpt-test", cliproxyexecutor.Options{}, []*Auth{authA, authB})
	if errPick != nil || selected.ID != authB.ID {
		t.Fatalf("success Pick() = auth=%v err=%v", selected, errPick)
	}

	ttftSelector := NewAdaptiveSelector(internalconfig.AdaptiveRoutingConfig{
		TopK:         1,
		TTFTTargetMS: 10_000,
		Weights: internalconfig.AdaptiveRoutingWeights{
			TTFT: 1,
		},
	})
	ttftSelector.HandleUsage(context.Background(), coreusage.Record{AuthID: authA.ID, TTFT: 40 * time.Second})
	ttftSelector.HandleUsage(context.Background(), coreusage.Record{AuthID: authB.ID, TTFT: time.Second})
	selected, errPick = ttftSelector.Pick(context.Background(), "codex", "gpt-test", cliproxyexecutor.Options{}, []*Auth{authA, authB})
	if errPick != nil || selected.ID != authB.ID {
		t.Fatalf("TTFT Pick() = auth=%v err=%v", selected, errPick)
	}
}

func TestAdaptiveSelectorWeightedTopKAndScores(t *testing.T) {
	authA := adaptiveTestAuth("auth-a", 9)
	authB := adaptiveTestAuth("auth-b", 5)
	authC := adaptiveTestAuth("auth-c", 1)
	selector := NewAdaptiveSelector(internalconfig.AdaptiveRoutingConfig{
		TopK: 2,
		Weights: internalconfig.AdaptiveRoutingWeights{
			Priority: 1,
		},
	})
	selector.rand = func() float64 { return 0.999999 }
	selected, errPick := selector.Pick(context.Background(), "codex", "gpt-test", cliproxyexecutor.Options{}, []*Auth{authA, authB, authC})
	if errPick != nil {
		t.Fatalf("Pick() error = %v", errPick)
	}
	if selected.ID != authB.ID {
		t.Fatalf("weighted top-K selected %s, want %s", selected.ID, authB.ID)
	}

	release := selector.BeginAttempt(authA.ID)
	selector.ObserveResult(Result{AuthID: authA.ID, Success: false})
	selector.HandleUsage(context.Background(), coreusage.Record{AuthID: authA.ID, TTFT: 2 * time.Second})
	scores := selector.Scores("codex", "gpt-test", []*Auth{authA, authB, authC})
	if len(scores) != 3 || scores[0].Rank == 0 {
		t.Fatalf("Scores() = %+v", scores)
	}
	var scoreA *AdaptiveScore
	for i := range scores {
		if scores[i].AuthID == authA.ID {
			scoreA = &scores[i]
			break
		}
	}
	if scoreA == nil || scoreA.ActiveRequests != 1 || scoreA.OutcomeSamples != 1 || scoreA.TTFTSamples != 1 {
		t.Fatalf("auth-a score = %+v", scoreA)
	}
	release()
}

func TestSessionAffinityEscapesUnhealthyAdaptiveBinding(t *testing.T) {
	authA := adaptiveTestAuth("auth-a", 1)
	authB := adaptiveTestAuth("auth-b", 1)
	adaptive := NewAdaptiveSelector(internalconfig.AdaptiveRoutingConfig{
		TopK: 1,
		Weights: internalconfig.AdaptiveRoutingWeights{
			SuccessRate: 1,
		},
		StickyEscape: internalconfig.AdaptiveStickyEscape{
			Enabled:            true,
			MinSamples:         1,
			ErrorRateThreshold: 0.5,
			TTFTThresholdMS:    30_000,
		},
	})
	selector := NewSessionAffinitySelectorWithConfig(SessionAffinityConfig{Fallback: adaptive, TTL: time.Hour})
	t.Cleanup(selector.Stop)
	opts := cliproxyexecutor.Options{Headers: http.Header{"X-Session-ID": []string{"session-1"}}}

	first, errFirst := selector.Pick(context.Background(), "codex", "gpt-test", opts, []*Auth{authA, authB})
	if errFirst != nil || first.ID != authA.ID {
		t.Fatalf("first Pick() = auth=%v err=%v", first, errFirst)
	}
	adaptive.ObserveResult(Result{AuthID: authA.ID, Success: false})
	second, errSecond := selector.Pick(context.Background(), "codex", "gpt-test", opts, []*Auth{authA, authB})
	if errSecond != nil || second.ID != authB.ID {
		t.Fatalf("escaped Pick() = auth=%v err=%v", second, errSecond)
	}
}

func TestManagerTracksAdaptiveActiveExecutionLifecycle(t *testing.T) {
	adaptive := NewAdaptiveSelector(internalconfig.AdaptiveRoutingConfig{TopK: 1})
	manager := NewManager(nil, adaptive, nil)
	executor := &blockingAdaptiveExecutor{started: make(chan struct{}), release: make(chan struct{})}
	manager.RegisterExecutor(executor)
	auth := adaptiveTestAuth("auth-live", 1)
	if _, errRegister := manager.Register(context.Background(), auth); errRegister != nil {
		t.Fatalf("Register() error = %v", errRegister)
	}
	registry.GetGlobalRegistry().RegisterClient(auth.ID, "codex", []*registry.ModelInfo{{ID: "gpt-test"}})
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(auth.ID) })

	done := make(chan error, 1)
	go func() {
		_, errExecute := manager.Execute(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: "gpt-test"}, cliproxyexecutor.Options{})
		done <- errExecute
	}()
	select {
	case <-executor.started:
	case <-time.After(2 * time.Second):
		t.Fatal("executor did not start")
	}
	scores := adaptive.Scores("codex", "gpt-test", []*Auth{auth})
	if len(scores) != 1 || scores[0].ActiveRequests != 1 {
		t.Fatalf("active scores = %+v", scores)
	}
	close(executor.release)
	if errExecute := <-done; errExecute != nil {
		t.Fatalf("Execute() error = %v", errExecute)
	}
	scores = adaptive.Scores("codex", "gpt-test", []*Auth{auth})
	if len(scores) != 1 || scores[0].ActiveRequests != 0 || scores[0].OutcomeSamples != 1 {
		t.Fatalf("completed scores = %+v", scores)
	}
}
