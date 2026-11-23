package auth

import (
	"context"
	"net/http"
	"testing"
	"time"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

type retainedStreamFailureHook struct {
	NoopHook
	results chan Result
}

func (h *retainedStreamFailureHook) OnResult(_ context.Context, result Result) {
	h.results <- result
}

func TestWrapStreamResultRecordsRetainedFailureAfterCancellation(t *testing.T) {
	for _, delayed := range []bool{false, true} {
		t.Run(map[bool]string{false: "ready-error", true: "error-after-output-close"}[delayed], func(t *testing.T) {
			hook := &retainedStreamFailureHook{results: make(chan Result, 1)}
			manager := NewManager(nil, nil, hook)
			auth, errRegister := manager.Register(WithSkipPersist(t.Context()), &Auth{
				ID: t.Name(), Provider: "aistudio", Status: StatusActive,
				Metadata: map[string]any{"concurrency_limit": 1},
			})
			if errRegister != nil {
				t.Fatal(errRegister)
			}
			release, errLimit := manager.requestLimiter.acquire(auth)
			if errLimit != nil {
				t.Fatal(errLimit)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			remaining := make(chan cliproxyexecutor.StreamChunk, 1)
			failure := &Error{Code: ErrorCodeTransientTransport, HTTPStatus: http.StatusBadGateway, Message: "MALFORMED_FUNCTION_CALL"}
			if !delayed {
				remaining <- cliproxyexecutor.StreamChunk{Err: failure}
				close(remaining)
				cancel()
			}
			stream := manager.wrapStreamResultWithCancel(ctx, auth, auth.Provider, "model", "model", nil, nil, remaining, OAuthModelAliasResult{}, false, cliproxyexecutor.Options{}, 1, release)
			cancel()
			select {
			case _, open := <-stream.Chunks:
				if open {
					t.Fatal("canceled stream forwarded a chunk")
				}
			case <-time.After(time.Second):
				t.Fatal("cancellation waited for the upstream terminal error")
			}
			releaseNext, errNext := manager.requestLimiter.acquire(auth)
			if errNext != nil {
				t.Fatalf("canceled stream held its admission slot: %v", errNext)
			}
			releaseNext()
			if delayed {
				remaining <- cliproxyexecutor.StreamChunk{Err: failure}
				close(remaining)
			}
			select {
			case result := <-hook.results:
				if result.Success || result.Error == nil || result.Error.Code != ErrorCodeTransientTransport || result.Error.HTTPStatus != http.StatusBadGateway {
					t.Fatalf("retained failure result = %+v", result)
				}
			case <-time.After(time.Second):
				t.Fatal("retained terminal failure was discarded")
			}
			current, _ := manager.GetByID(auth.ID)
			if current.Failed != 1 || current.Unavailable || current.Quota.Exceeded {
				t.Fatalf("terminal failure accounting poisoned credential availability: %+v", current)
			}
		})
	}
}
