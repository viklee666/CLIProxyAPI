package auth

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// RequestAuthRecoverer lets an executor repair request-scoped auth metadata
// after an upstream authentication failure. Manager serializes recovery per
// auth ID, persists returned updates, and retries the request at most once.
type RequestAuthRecoverer interface {
	ShouldRecoverRequestAuth(auth *Auth, execErr error) bool
	RequestAuthRecoveryState(auth *Auth) string
	RecoverRequestAuth(ctx context.Context, auth *Auth, execErr error) (*Auth, error)
}

// RequestAuthRecoveryObserver is notified after recovered auth metadata has
// been persisted so executors can invalidate stale runtime resources.
type RequestAuthRecoveryObserver interface {
	RequestAuthRecovered(auth *Auth)
}

// executorSupportsRequestAuthRecovery reports whether the executor owns a request-scoped
// auth repair path for this credential, such as Codex Agent Identity assertions.
func executorSupportsRequestAuthRecovery(executor ProviderExecutor, auth *Auth) bool {
	if executor == nil || auth == nil {
		return false
	}
	recoverer, ok := executor.(RequestAuthRecoverer)
	return ok && recoverer != nil && auth.AuthKind() == AuthKindAgentIdentity
}

func (m *Manager) tryRecoverRequestAuth(ctx context.Context, executor ProviderExecutor, auth *Auth, execErr error, alreadyTried bool) (*Auth, bool, error) {
	if m == nil || alreadyTried || execErr == nil || !executorSupportsRequestAuthRecovery(executor, auth) {
		return auth, false, nil
	}
	recoverer, ok := executor.(RequestAuthRecoverer)
	if !ok || recoverer == nil {
		return auth, false, nil
	}
	// Conductor wraps post-connect failures in upstreamExecutionAttemptError (Error/Unwrap
	// only). Recoverers such as Codex Agent Identity type-assert StatusCode(), so unwrap
	// the execution boundary before ShouldRecoverRequestAuth / RecoverRequestAuth.
	execErr = unwrapExecutionBoundaryError(execErr)
	if execErr == nil || !recoverer.ShouldRecoverRequestAuth(auth, execErr) {
		return auth, false, nil
	}

	id := strings.TrimSpace(auth.ID)
	failedState := recoverer.RequestAuthRecoveryState(auth)
	if id == "" {
		updated, errRecover := recoverer.RecoverRequestAuth(ctx, auth.Clone(), execErr)
		return updated, true, errRecover
	}

	lockValue, _ := m.requestPrepareLocks.LoadOrStore(id, &requestAuthPrepareLock{})
	lock, ok := lockValue.(*requestAuthPrepareLock)
	if !ok || lock == nil {
		updated, errRecover := recoverer.RecoverRequestAuth(ctx, auth.Clone(), execErr)
		return updated, true, errRecover
	}

	lock.mu.Lock()
	defer lock.mu.Unlock()

	target := auth.Clone()
	m.mu.RLock()
	if current := m.auths[id]; current != nil {
		target = current.Clone()
	}
	m.mu.RUnlock()

	if currentState := recoverer.RequestAuthRecoveryState(target); currentState != failedState {
		return target, true, nil
	}
	if !recoverer.ShouldRecoverRequestAuth(target, execErr) {
		return target, false, nil
	}

	updated, errRecover := recoverer.RecoverRequestAuth(ctx, target, execErr)
	if errRecover != nil {
		return auth, true, errRecover
	}
	if updated == nil {
		return target, true, nil
	}

	saved, errUpdate := m.Update(ctx, updated)
	if errUpdate != nil {
		return updated, true, errUpdate
	}
	if saved == nil {
		saved = updated
	}
	if observer, okObserver := executor.(RequestAuthRecoveryObserver); okObserver && observer != nil {
		observer.RequestAuthRecovered(saved.Clone())
	}
	return saved, true, nil
}

func makeHTTPRequestReplayable(req *http.Request) error {
	if req == nil || req.Body == nil || req.Body == http.NoBody || req.GetBody != nil {
		return nil
	}
	body, errRead := io.ReadAll(req.Body)
	if errClose := req.Body.Close(); errRead == nil && errClose != nil {
		errRead = errClose
	}
	if errRead != nil {
		return fmt.Errorf("read http request body for auth recovery: %w", errRead)
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
	return nil
}

func cloneHTTPRequestForAttempt(ctx context.Context, req *http.Request) (*http.Request, error) {
	if req == nil {
		return nil, &Error{Code: "invalid_request", Message: "http request is nil"}
	}
	if ctx == nil {
		ctx = req.Context()
	}
	attemptReq := req.Clone(ctx)
	attemptReq.Header = req.Header.Clone()
	if req.Body == nil || req.Body == http.NoBody {
		return attemptReq, nil
	}
	if req.GetBody == nil {
		return nil, &Error{Code: "invalid_request", Message: "http request body is not replayable"}
	}
	body, errGetBody := req.GetBody()
	if errGetBody != nil {
		return nil, fmt.Errorf("clone http request body for auth recovery: %w", errGetBody)
	}
	attemptReq.Body = body
	return attemptReq, nil
}

func httpResponseRequestAuthError(resp *http.Response) error {
	if resp == nil || resp.StatusCode != http.StatusUnauthorized || resp.Body == nil {
		return nil
	}
	body, errRead := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(body))
	if errRead != nil {
		return nil
	}
	return &Error{
		Code:       requestScopedErrorCode,
		Message:    string(body),
		HTTPStatus: resp.StatusCode,
	}
}
