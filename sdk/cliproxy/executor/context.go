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

type upstreamAttemptTracker struct {
	attempted atomic.Bool
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
