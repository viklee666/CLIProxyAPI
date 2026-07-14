package handlers

import (
	"bytes"
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
)

type timeoutThenPayloadStreamExecutor struct {
	mu              sync.Mutex
	hangingAttempts int
	calls           int
	canceled        int
	payloads        [][]byte
}

func (e *timeoutThenPayloadStreamExecutor) Identifier() string { return "codex" }

func (e *timeoutThenPayloadStreamExecutor) Execute(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (coreexecutor.Response, error) {
	return coreexecutor.Response{}, &coreauth.Error{Code: "not_implemented", Message: "Execute not implemented"}
}

func (e *timeoutThenPayloadStreamExecutor) ExecuteStream(ctx context.Context, _ *coreauth.Auth, req coreexecutor.Request, _ coreexecutor.Options) (*coreexecutor.StreamResult, error) {
	e.mu.Lock()
	e.calls++
	call := e.calls
	e.payloads = append(e.payloads, bytes.Clone(req.Payload))
	e.mu.Unlock()

	if call <= e.hangingAttempts {
		chunks := make(chan coreexecutor.StreamChunk)
		go func() {
			<-ctx.Done()
			e.mu.Lock()
			e.canceled++
			e.mu.Unlock()
			close(chunks)
		}()
		return &coreexecutor.StreamResult{Chunks: chunks}, nil
	}

	chunks := make(chan coreexecutor.StreamChunk, 1)
	chunks <- coreexecutor.StreamChunk{Payload: []byte("ok")}
	close(chunks)
	return &coreexecutor.StreamResult{Chunks: chunks}, nil
}

func (e *timeoutThenPayloadStreamExecutor) Refresh(_ context.Context, auth *coreauth.Auth) (*coreauth.Auth, error) {
	return auth, nil
}

func (e *timeoutThenPayloadStreamExecutor) CountTokens(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (coreexecutor.Response, error) {
	return coreexecutor.Response{}, &coreauth.Error{Code: "not_implemented", Message: "CountTokens not implemented"}
}

func (e *timeoutThenPayloadStreamExecutor) HttpRequest(context.Context, *coreauth.Auth, *http.Request) (*http.Response, error) {
	return nil, &coreauth.Error{Code: "not_implemented", Message: "HttpRequest not implemented"}
}

func (e *timeoutThenPayloadStreamExecutor) snapshot() (int, int, [][]byte) {
	e.mu.Lock()
	defer e.mu.Unlock()
	payloads := make([][]byte, len(e.payloads))
	for i := range e.payloads {
		payloads[i] = bytes.Clone(e.payloads[i])
	}
	return e.calls, e.canceled, payloads
}

func TestExecuteStreamWithAuthManager_FirstEventTimeoutRetriesIdenticalRequest(t *testing.T) {
	executor := &timeoutThenPayloadStreamExecutor{hangingAttempts: 1}
	manager := coreauth.NewManager(nil, nil, nil)
	manager.RegisterExecutor(executor)
	auth := &coreauth.Auth{ID: "first-event-timeout-auth", Provider: "codex", Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("manager.Register(): %v", err)
	}
	registry.GetGlobalRegistry().RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{{ID: "test-model"}})
	t.Cleanup(func() {
		registry.GetGlobalRegistry().UnregisterClient(auth.ID)
	})

	handler := NewBaseAPIHandlers(&sdkconfig.SDKConfig{
		Streaming: sdkconfig.StreamingConfig{
			FirstEventTimeoutSeconds: 1,
			FirstEventTimeoutRetries: 1,
		},
	}, manager)
	rawRequest := []byte(`{"model":"test-model","messages":[{"role":"user","content":"hello"}]}`)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	started := time.Now()
	dataChan, _, errChan := handler.ExecuteStreamWithAuthManager(ctx, "openai", "test-model", rawRequest, "")

	var got []byte
	for chunk := range dataChan {
		got = append(got, chunk...)
	}
	for msg := range errChan {
		if msg != nil {
			t.Fatalf("unexpected stream error: %+v", msg)
		}
	}
	if string(got) != "ok" {
		t.Fatalf("payload = %q, want ok", got)
	}
	if elapsed := time.Since(started); elapsed < 900*time.Millisecond || elapsed > 4*time.Second {
		t.Fatalf("elapsed = %s, want one timeout followed by a prompt retry", elapsed)
	}

	deadline := time.Now().Add(time.Second)
	for {
		calls, canceled, payloads := executor.snapshot()
		if calls == 2 && canceled == 1 {
			if len(payloads) != 2 || !bytes.Equal(payloads[0], rawRequest) || !bytes.Equal(payloads[1], rawRequest) {
				t.Fatalf("retry payloads = %q, want two identical requests", payloads)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("calls = %d, canceled = %d, want 2 and 1", calls, canceled)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestExecuteStreamThroughFirstEvent_ExhaustsConfiguredRetries(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	canceled := 0
	open := func(ctx context.Context) (*coreexecutor.StreamResult, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		chunks := make(chan coreexecutor.StreamChunk)
		go func() {
			<-ctx.Done()
			mu.Lock()
			canceled++
			mu.Unlock()
			close(chunks)
		}()
		return &coreexecutor.StreamResult{Chunks: chunks}, nil
	}

	result, pending, closed, cancel, err := executeStreamThroughFirstEvent(
		context.Background(), 20*time.Millisecond, 2, "openai", "test-model", open,
	)
	if result != nil || pending != nil || closed || cancel != nil {
		t.Fatalf("unexpected successful stream result: result=%v pending=%v closed=%v cancel=%v", result, pending, closed, cancel)
	}
	if err == nil {
		t.Fatal("expected first-event timeout error")
	}
	statusErr, ok := err.(interface{ StatusCode() int })
	if !ok || statusErr.StatusCode() != http.StatusGatewayTimeout {
		t.Fatalf("error = %T %v, want HTTP 504 status error", err, err)
	}

	deadline := time.Now().Add(time.Second)
	for {
		mu.Lock()
		gotCalls, gotCanceled := calls, canceled
		mu.Unlock()
		if gotCalls == 3 && gotCanceled == 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("calls = %d, canceled = %d, want 3 and 3", gotCalls, gotCanceled)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestStreamingFirstEventTimeoutConfigDefaultsAndClamps(t *testing.T) {
	if got := StreamingFirstEventTimeout(nil); got != 0 {
		t.Fatalf("StreamingFirstEventTimeout(nil) = %s, want 0", got)
	}
	if got := StreamingFirstEventTimeoutRetries(nil); got != 0 {
		t.Fatalf("StreamingFirstEventTimeoutRetries(nil) = %d, want 0", got)
	}
	cfg := &sdkconfig.SDKConfig{Streaming: sdkconfig.StreamingConfig{
		FirstEventTimeoutSeconds: -1,
		FirstEventTimeoutRetries: -1,
	}}
	if got := StreamingFirstEventTimeout(cfg); got != 0 {
		t.Fatalf("negative timeout = %s, want 0", got)
	}
	if got := StreamingFirstEventTimeoutRetries(cfg); got != 0 {
		t.Fatalf("negative retries = %d, want 0", got)
	}
	cfg.Streaming.FirstEventTimeoutSeconds = 20
	cfg.Streaming.FirstEventTimeoutRetries = 3
	if got := StreamingFirstEventTimeout(cfg); got != 20*time.Second {
		t.Fatalf("timeout = %s, want 20s", got)
	}
	if got := StreamingFirstEventTimeoutRetries(cfg); got != 3 {
		t.Fatalf("retries = %d, want 3", got)
	}
}
