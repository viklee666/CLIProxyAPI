package auth

import (
	"context"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

// shouldEscapeAuth reports whether the fallback selector considers a bound
// credential unhealthy enough to break session affinity. Only adaptive
// selectors implement the health probe, so other strategies never escape.
func (s *SessionAffinitySelector) shouldEscapeAuth(authID string) bool {
	if s == nil || s.fallback == nil {
		return false
	}
	health, ok := s.fallback.(interface{ ShouldEscapeAuth(string) bool })
	return ok && health.ShouldEscapeAuth(authID)
}

func (s *SessionAffinitySelector) pickAvoiding(ctx context.Context, provider, model string, opts cliproxyexecutor.Options, auths []*Auth, avoidID string) (*Auth, bool) {
	if s == nil || s.fallback == nil || len(auths) < 2 {
		return nil, false
	}
	alternatives := make([]*Auth, 0, len(auths)-1)
	for _, auth := range auths {
		if auth != nil && auth.ID != avoidID {
			alternatives = append(alternatives, auth)
		}
	}
	if len(alternatives) == 0 {
		return nil, false
	}
	replacement, errPick := s.fallback.Pick(ctx, provider, model, opts, alternatives)
	return replacement, errPick == nil && replacement != nil
}

// BeginAttempt forwards active concurrency tracking to the fallback selector.
func (s *SessionAffinitySelector) BeginAttempt(authID string) func() {
	if s == nil || s.fallback == nil {
		return func() {}
	}
	tracker, ok := s.fallback.(interface{ BeginAttempt(string) func() })
	if !ok {
		return func() {}
	}
	return tracker.BeginAttempt(authID)
}

// ObserveResult forwards execution outcomes to the fallback selector.
func (s *SessionAffinitySelector) ObserveResult(result Result) {
	if s == nil || s.fallback == nil {
		return
	}
	if observer, ok := s.fallback.(interface{ ObserveResult(Result) }); ok {
		observer.ObserveResult(result)
	}
}

// AdaptiveScores forwards score snapshots to an adaptive fallback selector.
func (s *SessionAffinitySelector) AdaptiveScores(provider, model string, auths []*Auth) ([]AdaptiveScore, internalconfig.AdaptiveRoutingConfig, bool) {
	if s == nil || s.fallback == nil {
		return nil, internalconfig.AdaptiveRoutingConfig{}, false
	}
	source, ok := s.fallback.(interface {
		AdaptiveScores(string, string, []*Auth) ([]AdaptiveScore, internalconfig.AdaptiveRoutingConfig, bool)
	})
	if !ok {
		return nil, internalconfig.AdaptiveRoutingConfig{}, false
	}
	return source.AdaptiveScores(provider, model, auths)
}
