package executor

import (
	"context"
	"sync/atomic"
)

type downstreamWebsocketContextKey struct{}
type requireUpstreamWebsocketContextKey struct{}
type upstreamAttemptTrackerContextKey struct{}
type requestRetryStateContextKey struct{}
type oauthRecoveryAllowedContextKey struct{}

type requestRetryState struct {
	disabled atomic.Bool
}
type localWebsocketTurnTrackerContextKey struct{}
type continuityWaitObserverContextKey struct{}

// ContinuityWaitObserver notifies a downstream transport when a real continuity wait starts and ends.
type ContinuityWaitObserver interface {
	OnContinuityWaitStart()
	OnContinuityWaitEnd()
}

type upstreamAttemptTracker struct {
	attempted atomic.Bool
	model     atomic.Pointer[string]
}

type localWebsocketTurnTracker struct {
	executed atomic.Bool
}

// WithDownstreamWebsocket marks the current request as coming from a downstream websocket connection.
func WithDownstreamWebsocket(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, downstreamWebsocketContextKey{}, true)
}

// DownstreamWebsocket reports whether the current request originates from a downstream websocket connection.
func DownstreamWebsocket(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	raw := ctx.Value(downstreamWebsocketContextKey{})
	enabled, ok := raw.(bool)
	return ok && enabled
}

// WithRequiredUpstreamWebsocket marks a request whose incremental context is valid only on the current upstream websocket.
func WithRequiredUpstreamWebsocket(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, requireUpstreamWebsocketContextKey{}, true)
}

// RequiredUpstreamWebsocket reports whether falling back to an HTTP upstream would lose request context.
func RequiredUpstreamWebsocket(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	raw := ctx.Value(requireUpstreamWebsocketContextKey{})
	enabled, ok := raw.(bool)
	return ok && enabled
}

// WithRequestRetryState shares retry policy across all execution layers of one request.
// Reusing the state lets a provider selection disable retries in its callers as well.
func WithRequestRetryState(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if state, ok := ctx.Value(requestRetryStateContextKey{}).(*requestRetryState); ok && state != nil {
		return ctx
	}
	return context.WithValue(ctx, requestRetryStateContextKey{}, &requestRetryState{})
}

// DisableRequestRetries prevents any further retries for the current request.
// The context must have been initialized with WithRequestRetryState.
func DisableRequestRetries(ctx context.Context) {
	if ctx == nil {
		return
	}
	if state, ok := ctx.Value(requestRetryStateContextKey{}).(*requestRetryState); ok && state != nil {
		state.disabled.Store(true)
	}
}

// RequestRetriesDisabled reports whether the selected provider requires a single attempt.
func RequestRetriesDisabled(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	state, ok := ctx.Value(requestRetryStateContextKey{}).(*requestRetryState)
	return ok && state != nil && state.disabled.Load()
}

// WithOAuthRecoveryAllowed permits same-credential OAuth recovery even when
// ordinary retries are disabled. It never enables credential or model fallback.
func WithOAuthRecoveryAllowed(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, oauthRecoveryAllowedContextKey{}, true)
}

// OAuthRecoveryAllowed reports whether a caller explicitly permits OAuth recovery.
func OAuthRecoveryAllowed(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	allowed, _ := ctx.Value(oauthRecoveryAllowedContextKey{}).(bool)
	return allowed
}

// WithUpstreamAttemptTracker installs a fresh tracker for one provider execution attempt.
func WithUpstreamAttemptTracker(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, upstreamAttemptTrackerContextKey{}, &upstreamAttemptTracker{})
}

// MarkUpstreamAttempt records that the provider execution reached an upstream transport boundary.
func MarkUpstreamAttempt(ctx context.Context) {
	if ctx == nil {
		return
	}
	tracker, ok := ctx.Value(upstreamAttemptTrackerContextKey{}).(*upstreamAttemptTracker)
	if !ok || tracker == nil {
		return
	}
	tracker.attempted.Store(true)
}

// UpstreamAttempted reports whether the tracked provider execution reached an upstream transport boundary.
func UpstreamAttempted(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	tracker, ok := ctx.Value(upstreamAttemptTrackerContextKey{}).(*upstreamAttemptTracker)
	return ok && tracker != nil && tracker.attempted.Load()
}

// SetUpstreamAttemptModel records a provider-selected model for this attempt.
func SetUpstreamAttemptModel(ctx context.Context, model string) {
	if ctx != nil {
		if tracker, ok := ctx.Value(upstreamAttemptTrackerContextKey{}).(*upstreamAttemptTracker); ok {
			tracker.model.Store(&model)
		}
	}
}

// UpstreamAttemptModel reports the provider-selected model, if any.
func UpstreamAttemptModel(ctx context.Context) string {
	if ctx != nil {
		if tracker, ok := ctx.Value(upstreamAttemptTrackerContextKey{}).(*upstreamAttemptTracker); ok {
			if model := tracker.model.Load(); model != nil {
				return *model
			}
		}
	}
	return ""
}

// WithLocalWebsocketTurnTracker installs a tracker that records when a downstream
// websocket turn was fulfilled locally over HTTP rather than establishing or
// owning an upstream websocket response.
func WithLocalWebsocketTurnTracker(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, localWebsocketTurnTrackerContextKey{}, &localWebsocketTurnTracker{})
}

// MarkLocalWebsocketTurn records that the current downstream websocket turn was
// answered locally without creating an upstream websocket response.
func MarkLocalWebsocketTurn(ctx context.Context) {
	if ctx == nil {
		return
	}
	tracker, ok := ctx.Value(localWebsocketTurnTrackerContextKey{}).(*localWebsocketTurnTracker)
	if !ok || tracker == nil {
		return
	}
	tracker.executed.Store(true)
}

// ResetLocalWebsocketTurn clears the local websocket turn marker before a new auth attempt.
func ResetLocalWebsocketTurn(ctx context.Context) {
	if ctx == nil {
		return
	}
	tracker, ok := ctx.Value(localWebsocketTurnTrackerContextKey{}).(*localWebsocketTurnTracker)
	if !ok || tracker == nil {
		return
	}
	tracker.executed.Store(false)
}

// LocalWebsocketTurnExecuted reports whether the current downstream websocket turn
// was fulfilled locally rather than on an upstream websocket.
func LocalWebsocketTurnExecuted(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	tracker, ok := ctx.Value(localWebsocketTurnTrackerContextKey{}).(*localWebsocketTurnTracker)
	return ok && tracker != nil && tracker.executed.Load()
}

// WithContinuityWaitObserver attaches an observer notified only while a real continuity wait is active.
func WithContinuityWaitObserver(ctx context.Context, observer ContinuityWaitObserver) context.Context {
	if observer == nil {
		return ctx
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, continuityWaitObserverContextKey{}, observer)
}

// NotifyContinuityWaitStart notifies any attached observer that a real continuity wait has started.
func NotifyContinuityWaitStart(ctx context.Context) {
	if ctx == nil {
		return
	}
	if observer, ok := ctx.Value(continuityWaitObserverContextKey{}).(ContinuityWaitObserver); ok && observer != nil {
		observer.OnContinuityWaitStart()
	}
}

// NotifyContinuityWaitEnd notifies any attached observer that a continuity wait has ended.
func NotifyContinuityWaitEnd(ctx context.Context) {
	if ctx == nil {
		return
	}
	if observer, ok := ctx.Value(continuityWaitObserverContextKey{}).(ContinuityWaitObserver); ok && observer != nil {
		observer.OnContinuityWaitEnd()
	}
}
