package auth

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

type requestAuthRecoveryStore struct {
	mu    sync.Mutex
	saves []*Auth
}

func (s *requestAuthRecoveryStore) List(context.Context) ([]*Auth, error) { return nil, nil }

func (s *requestAuthRecoveryStore) Save(_ context.Context, auth *Auth) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saves = append(s.saves, auth.Clone())
	return auth.ID, nil
}

func (s *requestAuthRecoveryStore) Delete(context.Context, string) error { return nil }

func (s *requestAuthRecoveryStore) latest() *Auth {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.saves) == 0 {
		return nil
	}
	return s.saves[len(s.saves)-1].Clone()
}

type requestAuthRecoveryExecutor struct {
	mu sync.Mutex

	executeCalls  int
	streamCalls   int
	httpCalls     int
	prepareCalls  int
	recoverCalls  int
	refreshCalls  int
	observed      int
	alwaysInvalid bool
	httpBodies    []string
}

func (e *requestAuthRecoveryExecutor) Identifier() string { return "codex" }

func requestAuthTask(auth *Auth) string {
	if auth == nil || auth.Metadata == nil {
		return ""
	}
	value, _ := auth.Metadata["task_id"].(string)
	return value
}

func requestAuthInvalidTaskError() error {
	return &Error{HTTPStatus: http.StatusUnauthorized, Message: `{"error":{"code":"invalid_task_id"}}`}
}

func (e *requestAuthRecoveryExecutor) Execute(_ context.Context, auth *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	e.mu.Lock()
	e.executeCalls++
	alwaysInvalid := e.alwaysInvalid
	e.mu.Unlock()
	if alwaysInvalid || requestAuthTask(auth) != "task-new" {
		return cliproxyexecutor.Response{}, requestAuthInvalidTaskError()
	}
	return cliproxyexecutor.Response{Payload: []byte("task-new")}, nil
}

func (e *requestAuthRecoveryExecutor) ExecuteStream(_ context.Context, auth *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	e.mu.Lock()
	e.streamCalls++
	alwaysInvalid := e.alwaysInvalid
	e.mu.Unlock()
	chunks := make(chan cliproxyexecutor.StreamChunk, 1)
	if alwaysInvalid || requestAuthTask(auth) != "task-new" {
		chunks <- cliproxyexecutor.StreamChunk{Err: requestAuthInvalidTaskError()}
	} else {
		chunks <- cliproxyexecutor.StreamChunk{Payload: []byte("task-new")}
	}
	close(chunks)
	return &cliproxyexecutor.StreamResult{Chunks: chunks}, nil
}

func (e *requestAuthRecoveryExecutor) Refresh(_ context.Context, auth *Auth) (*Auth, error) {
	e.mu.Lock()
	e.refreshCalls++
	e.mu.Unlock()
	return auth, nil
}

func (e *requestAuthRecoveryExecutor) CountTokens(ctx context.Context, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return e.Execute(ctx, auth, req, opts)
}

func (e *requestAuthRecoveryExecutor) HttpRequest(_ context.Context, auth *Auth, req *http.Request) (*http.Response, error) {
	var body []byte
	if req != nil && req.Body != nil {
		body, _ = io.ReadAll(req.Body)
		_ = req.Body.Close()
	}
	e.mu.Lock()
	e.httpCalls++
	e.httpBodies = append(e.httpBodies, string(body))
	alwaysInvalid := e.alwaysInvalid
	e.mu.Unlock()
	if alwaysInvalid || requestAuthTask(auth) != "task-new" {
		return &http.Response{
			StatusCode: http.StatusUnauthorized,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"error":{"code":"invalid_task_id"}}`)),
		}, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("task-new")),
	}, nil
}

func (e *requestAuthRecoveryExecutor) ShouldPrepareRequestAuth(auth *Auth) bool {
	return requestAuthTask(auth) == ""
}

func (e *requestAuthRecoveryExecutor) PrepareRequestAuth(_ context.Context, auth *Auth) (*Auth, error) {
	e.mu.Lock()
	e.prepareCalls++
	e.mu.Unlock()
	updated := auth.Clone()
	if updated.Metadata == nil {
		updated.Metadata = make(map[string]any)
	}
	updated.Metadata["task_id"] = "task-new"
	return updated, nil
}

func (e *requestAuthRecoveryExecutor) ShouldRecoverRequestAuth(_ *Auth, execErr error) bool {
	return statusCodeFromError(execErr) == http.StatusUnauthorized
}

func (e *requestAuthRecoveryExecutor) RequestAuthRecoveryState(auth *Auth) string {
	return requestAuthTask(auth)
}

func (e *requestAuthRecoveryExecutor) RecoverRequestAuth(_ context.Context, auth *Auth, _ error) (*Auth, error) {
	e.mu.Lock()
	e.recoverCalls++
	e.mu.Unlock()
	time.Sleep(5 * time.Millisecond)
	updated := auth.Clone()
	if updated.Metadata == nil {
		updated.Metadata = make(map[string]any)
	}
	updated.Metadata["task_id"] = "task-new"
	return updated, nil
}

func (e *requestAuthRecoveryExecutor) RequestAuthRecovered(*Auth) {
	e.mu.Lock()
	e.observed++
	e.mu.Unlock()
}

func (e *requestAuthRecoveryExecutor) counts() (execute, stream, recover, refresh, observed int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.executeCalls, e.streamCalls, e.recoverCalls, e.refreshCalls, e.observed
}

func (e *requestAuthRecoveryExecutor) httpCounts() (httpCalls, prepareCalls int, bodies []string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.httpCalls, e.prepareCalls, append([]string(nil), e.httpBodies...)
}

func newRequestAuthRecoveryFixture(t *testing.T, alwaysInvalid bool) (*Manager, *requestAuthRecoveryExecutor, *requestAuthRecoveryStore, *Auth, string) {
	t.Helper()
	model := "gpt-5.5"
	auth := &Auth{
		ID:       "agent-auth",
		Provider: "codex",
		Metadata: map[string]any{
			"auth_kind":         AuthKindAgentIdentity,
			"agent_runtime_id":  "agent-runtime",
			"agent_private_key": "private-key",
			"task_id":           "task-old",
		},
	}
	store := &requestAuthRecoveryStore{}
	executor := &requestAuthRecoveryExecutor{alwaysInvalid: alwaysInvalid}
	manager := NewManager(store, nil, nil)
	manager.RegisterExecutor(executor)
	registry.GetGlobalRegistry().RegisterClient(auth.ID, "codex", []*registry.ModelInfo{{ID: model}})
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(auth.ID) })
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	return manager, executor, store, auth, model
}

func TestManagerExecuteRecoversRequestAuthBeforeFallback(t *testing.T) {
	manager, executor, store, auth, model := newRequestAuthRecoveryFixture(t, false)
	response, err := manager.Execute(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got := string(response.Payload); got != "task-new" {
		t.Fatalf("payload = %q, want task-new", got)
	}
	execute, _, recover, refresh, observed := executor.counts()
	if execute != 2 || recover != 1 || refresh != 0 || observed != 1 {
		t.Fatalf("counts execute=%d recover=%d refresh=%d observed=%d", execute, recover, refresh, observed)
	}
	if got := requestAuthTask(store.latest()); got != "task-new" {
		t.Fatalf("persisted task_id = %q, want task-new", got)
	}
	if current, ok := manager.GetByID(auth.ID); !ok || requestAuthTask(current) != "task-new" {
		t.Fatalf("runtime auth = %#v, want task-new", current)
	}
}

func TestManagerExecuteStreamRecoversBootstrapInvalidTask(t *testing.T) {
	manager, executor, _, _, model := newRequestAuthRecoveryFixture(t, false)
	result, err := manager.ExecuteStream(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{Stream: true})
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}
	var payload string
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			t.Fatalf("stream chunk error = %v", chunk.Err)
		}
		payload += string(chunk.Payload)
	}
	if payload != "task-new" {
		t.Fatalf("stream payload = %q, want task-new", payload)
	}
	_, stream, recover, refresh, _ := executor.counts()
	if stream != 2 || recover != 1 || refresh != 0 {
		t.Fatalf("counts stream=%d recover=%d refresh=%d", stream, recover, refresh)
	}
}

func TestManagerExecuteRecoversRequestAuthAtMostOnce(t *testing.T) {
	manager, executor, _, _, model := newRequestAuthRecoveryFixture(t, true)
	if _, err := manager.Execute(context.Background(), []string{"codex"}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{}); err == nil {
		t.Fatal("Execute() error = nil, want repeated invalid_task_id failure")
	}
	execute, _, recover, _, _ := executor.counts()
	if execute != 2 || recover != 1 {
		t.Fatalf("counts execute=%d recover=%d, want 2 and 1", execute, recover)
	}
}

func TestManagerRecoverRequestAuthCoalescesConcurrentCalls(t *testing.T) {
	manager, executor, _, auth, _ := newRequestAuthRecoveryFixture(t, false)
	errInvalid := requestAuthInvalidTaskError()
	const workers = 8
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			updated, attempted, err := manager.tryRecoverRequestAuth(context.Background(), executor, auth, errInvalid, false)
			if err != nil {
				errs <- err
				return
			}
			if !attempted || requestAuthTask(updated) != "task-new" {
				errs <- &Error{Message: "unexpected recovery result"}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent recovery error = %v", err)
	}
	_, _, recover, _, observed := executor.counts()
	if recover != 1 || observed != 1 {
		t.Fatalf("counts recover=%d observed=%d, want 1 and 1", recover, observed)
	}
}

func TestManagerHttpRequestPreparesAndPersistsMissingRequestAuth(t *testing.T) {
	manager, executor, store, auth, _ := newRequestAuthRecoveryFixture(t, false)
	missingTask := auth.Clone()
	delete(missingTask.Metadata, "task_id")
	if _, errUpdate := manager.Update(context.Background(), missingTask); errUpdate != nil {
		t.Fatalf("Update() error = %v", errUpdate)
	}

	req, errRequest := http.NewRequest(http.MethodPost, "https://example.test/usage", strings.NewReader("request-body"))
	if errRequest != nil {
		t.Fatalf("http.NewRequest() error = %v", errRequest)
	}
	resp, errDo := manager.HttpRequest(context.Background(), missingTask, req)
	if errDo != nil {
		t.Fatalf("HttpRequest() error = %v", errDo)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	httpCalls, prepareCalls, bodies := executor.httpCounts()
	if httpCalls != 1 || prepareCalls != 1 {
		t.Fatalf("http calls=%d prepare calls=%d, want 1 and 1", httpCalls, prepareCalls)
	}
	if len(bodies) != 1 || bodies[0] != "request-body" {
		t.Fatalf("request bodies = %#v, want one replayable body", bodies)
	}
	if got := requestAuthTask(store.latest()); got != "task-new" {
		t.Fatalf("persisted task_id = %q, want task-new", got)
	}
}

func TestManagerHttpRequestRecoversInvalidTaskAndReplaysBodyOnce(t *testing.T) {
	manager, executor, store, auth, _ := newRequestAuthRecoveryFixture(t, false)
	req, errRequest := http.NewRequest(http.MethodPost, "https://example.test/usage", strings.NewReader("request-body"))
	if errRequest != nil {
		t.Fatalf("http.NewRequest() error = %v", errRequest)
	}
	resp, errDo := manager.HttpRequest(context.Background(), auth, req)
	if errDo != nil {
		t.Fatalf("HttpRequest() error = %v", errDo)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	httpCalls, prepareCalls, bodies := executor.httpCounts()
	_, _, recoverCalls, refreshCalls, observed := executor.counts()
	if httpCalls != 2 || prepareCalls != 0 || recoverCalls != 1 || refreshCalls != 0 || observed != 1 {
		t.Fatalf("http=%d prepare=%d recover=%d refresh=%d observed=%d", httpCalls, prepareCalls, recoverCalls, refreshCalls, observed)
	}
	if len(bodies) != 2 || bodies[0] != "request-body" || bodies[1] != "request-body" {
		t.Fatalf("request bodies = %#v, want body replayed exactly once", bodies)
	}
	if got := requestAuthTask(store.latest()); got != "task-new" {
		t.Fatalf("persisted task_id = %q, want task-new", got)
	}
}

func TestManagerHttpRequestRecoversInvalidTaskAtMostOnce(t *testing.T) {
	manager, executor, _, auth, _ := newRequestAuthRecoveryFixture(t, true)
	req, errRequest := http.NewRequest(http.MethodGet, "https://example.test/usage", nil)
	if errRequest != nil {
		t.Fatalf("http.NewRequest() error = %v", errRequest)
	}
	resp, errDo := manager.HttpRequest(context.Background(), auth, req)
	if errDo != nil {
		t.Fatalf("HttpRequest() error = %v", errDo)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	httpCalls, _, _ := executor.httpCounts()
	_, _, recoverCalls, _, _ := executor.counts()
	if httpCalls != 2 || recoverCalls != 1 {
		t.Fatalf("http calls=%d recover calls=%d, want 2 and 1", httpCalls, recoverCalls)
	}
}
