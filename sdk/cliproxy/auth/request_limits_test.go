package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

func TestRequestLimiterAtomicAdmissionAndRelease(t *testing.T) {
	limiter := &requestLimiter{}
	auth := &Auth{ID: "limited", Metadata: map[string]any{"concurrency_limit": 3}}
	var admitted atomic.Int32
	var ready sync.WaitGroup
	var finished sync.WaitGroup
	ready.Add(64)
	finished.Add(64)
	releaseAll := make(chan struct{})
	for range 64 {
		go func() {
			defer finished.Done()
			release, errLimit := limiter.acquire(auth)
			if errLimit == nil {
				admitted.Add(1)
			}
			ready.Done()
			<-releaseAll
			if release != nil {
				release()
				release()
			}
		}()
	}
	ready.Wait()
	if admitted.Load() != 3 {
		t.Errorf("admitted %d simultaneous requests, want 3", admitted.Load())
	}
	close(releaseAll)
	finished.Wait()
	release, errLimit := limiter.acquire(auth)
	if errLimit != nil {
		t.Fatal(errLimit)
	}
	release()
}

func TestRequestLimiterRollingRPMAndProviderPool(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	limiter := &requestLimiter{nowFunc: func() time.Time { return now }}
	authA := &Auth{ID: "key-a", Attributes: map[string]string{AttributeRequestLimitGroup: "provider-a"}, Metadata: map[string]any{"rpm_limit": 2}}
	authB := authA.Clone()
	authB.ID = "key-b"
	for _, auth := range []*Auth{authA, authB} {
		release, errLimit := limiter.acquire(auth)
		if errLimit != nil {
			t.Fatal(errLimit)
		}
		release()
		now = now.Add(10 * time.Second)
	}
	if _, errLimit := limiter.acquire(authB); errLimit == nil {
		t.Fatal("shared provider exceeded its RPM limit")
	}
	now = now.Add(40 * time.Second)
	release, errLimit := limiter.acquire(authB)
	if errLimit != nil {
		t.Fatalf("rolling window did not reopen: %v", errLimit)
	}
	release()
	if _, errLimit = limiter.acquire(authA); errLimit == nil {
		t.Fatal("window reset all starts instead of expiring only the oldest request")
	}
}

func TestRequestLimiterUnlimitedAndHotReload(t *testing.T) {
	limiter := &requestLimiter{}
	auth := &Auth{ID: "same-credential"}
	releaseFirst, errLimit := limiter.acquire(auth)
	if errLimit != nil {
		t.Fatal(errLimit)
	}
	defer releaseFirst()
	auth.Metadata = map[string]any{"concurrency_limit": 1}
	if _, errLimit = limiter.acquire(auth); errLimit == nil {
		t.Fatal("hot reload ignored an existing unlimited request")
	}
	releaseFirst()
	release, errLimit := limiter.acquire(auth)
	if errLimit != nil {
		t.Fatal(errLimit)
	}
	release()
	auth.Metadata = nil
	for range 100 {
		release, errLimit = limiter.acquire(auth)
		if errLimit != nil {
			t.Fatal(errLimit)
		}
		defer release()
	}
}

type requestLimitExecutor struct {
	retryContractHomeExecutor
	chunks chan cliproxyexecutor.StreamChunk
}

func (executor *requestLimitExecutor) ExecuteStream(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	return &cliproxyexecutor.StreamResult{Chunks: executor.chunks}, nil
}

func TestManagerRequestLimitsAcrossExecutionPaths(t *testing.T) {
	for _, path := range []string{"execute", "count", "stream"} {
		t.Run(path, func(t *testing.T) {
			manager := NewManager(nil, nil, nil)
			manager.SetRetryConfig(3, 0, 0)
			executor := &retryContractHomeExecutor{}
			manager.RegisterExecutor(executor)
			model := "limit-model-" + path
			auth := &Auth{ID: t.Name(), Provider: executor.Identifier(), Status: StatusActive, Metadata: map[string]any{"rpm_limit": 1}}
			if _, errRegister := manager.Register(context.Background(), auth); errRegister != nil {
				t.Fatal(errRegister)
			}
			registry.GetGlobalRegistry().RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{{ID: model}})
			t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(auth.ID) })
			run := func() error {
				req := cliproxyexecutor.Request{Model: model}
				switch path {
				case "execute":
					_, errExecute := manager.Execute(context.Background(), []string{auth.Provider}, req, cliproxyexecutor.Options{})
					return errExecute
				case "count":
					_, errExecute := manager.ExecuteCount(context.Background(), []string{auth.Provider}, req, cliproxyexecutor.Options{})
					return errExecute
				default:
					stream, errExecute := manager.ExecuteStream(context.Background(), []string{auth.Provider}, req, cliproxyexecutor.Options{})
					if stream != nil {
						for range stream.Chunks {
						}
					}
					return errExecute
				}
			}
			if errExecute := run(); errExecute != nil {
				t.Fatal(errExecute)
			}
			var limited *requestLimitError
			if errExecute := run(); !errors.As(errExecute, &limited) || limited.StatusCode() != http.StatusTooManyRequests {
				t.Fatalf("second request error = %v, want local 429", errExecute)
			}
			if len(executor.Calls()) != 1 {
				t.Fatalf("upstream calls = %v, want one", executor.Calls())
			}
			updated, _ := manager.GetByID(auth.ID)
			if updated.Unavailable || updated.LastError != nil {
				t.Fatal("local admission rejection poisoned credential health")
			}
		})
	}
}

func TestManagerStreamHoldsConcurrencyUntilCloseOrCancel(t *testing.T) {
	for _, cancelStream := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel=%t", cancelStream), func(t *testing.T) {
			manager := NewManager(nil, nil, nil)
			executor := &requestLimitExecutor{chunks: make(chan cliproxyexecutor.StreamChunk, 1)}
			executor.chunks <- cliproxyexecutor.StreamChunk{Payload: []byte("data: started\n\n")}
			manager.RegisterExecutor(executor)
			model := "stream-concurrency-" + t.Name()
			auth := &Auth{ID: t.Name(), Provider: executor.Identifier(), Status: StatusActive, Metadata: map[string]any{"concurrency_limit": 1}}
			if _, errRegister := manager.Register(context.Background(), auth); errRegister != nil {
				t.Fatal(errRegister)
			}
			registry.GetGlobalRegistry().RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{{ID: model}})
			t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(auth.ID) })
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			stream, errStream := manager.ExecuteStream(ctx, []string{auth.Provider}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{})
			if errStream != nil {
				t.Fatal(errStream)
			}
			if _, errExecute := manager.ExecuteCount(context.Background(), []string{auth.Provider}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{}); errExecute == nil {
				t.Fatal("active stream released concurrency early")
			}
			if cancelStream {
				cancel()
			} else {
				close(executor.chunks)
			}
			for range stream.Chunks {
			}
			if _, errExecute := manager.ExecuteCount(context.Background(), []string{auth.Provider}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{}); errExecute != nil {
				t.Fatalf("stream leaked concurrency: %v", errExecute)
			}
			if cancelStream {
				close(executor.chunks)
			}
		})
	}
}

func TestManagerLimitedCredentialFallsBackWithoutUpstreamAttempt(t *testing.T) {
	manager := NewManager(nil, nil, nil)
	executor := &retryContractHomeExecutor{}
	manager.RegisterExecutor(executor)
	model := "request-limit-fallback-model"
	for index, priority := range []string{"10", "0"} {
		auth := &Auth{ID: fmt.Sprintf("limited-fallback-%d", index), Provider: executor.Identifier(), Status: StatusActive, Attributes: map[string]string{"priority": priority}, Metadata: map[string]any{"rpm_limit": 1}}
		if _, errRegister := manager.Register(context.Background(), auth); errRegister != nil {
			t.Fatal(errRegister)
		}
		registry.GetGlobalRegistry().RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{{ID: model}})
		t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(auth.ID) })
	}
	for range 2 {
		if _, errExecute := manager.Execute(context.Background(), []string{executor.Identifier()}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{}); errExecute != nil {
			t.Fatal(errExecute)
		}
	}
	calls := executor.Calls()
	if len(calls) != 2 || calls[0] != "limited-fallback-0" || calls[1] != "limited-fallback-1" {
		t.Fatalf("limited credential failed to fall back: %v", calls)
	}
}

func TestValidateAuthRequestLimits(t *testing.T) {
	for _, value := range []any{-1, 1.5, "3", true} {
		auth := &Auth{Metadata: map[string]any{"rpm_limit": value}}
		if ValidateAuthRequestLimits(auth) == nil {
			t.Fatalf("accepted invalid limit %#v", value)
		}
	}
	if (&Auth{Metadata: map[string]any{"concurrency-limit": 2, "rpm-limit": 10}}).RequestLimits() != (internalconfig.RequestLimits{ConcurrencyLimit: 2, RPMLimit: 10}) {
		t.Fatal("legacy limit keys not recognized")
	}
}
