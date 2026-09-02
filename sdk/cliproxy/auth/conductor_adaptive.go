package auth

import (
	"strings"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
)

func usesAdaptiveSelection(selector Selector) bool {
	switch typed := selector.(type) {
	case *AdaptiveSelector:
		return typed != nil
	case *SessionAffinitySelector:
		return typed != nil && usesAdaptiveSelection(typed.fallback)
	default:
		return false
	}
}

func (m *Manager) beginAdaptiveAttempt(authID string) func() {
	if m == nil || strings.TrimSpace(authID) == "" {
		return func() {}
	}
	m.mu.RLock()
	selector := m.selector
	m.mu.RUnlock()
	tracker, ok := selector.(interface{ BeginAttempt(string) func() })
	if !ok {
		return func() {}
	}
	return tracker.BeginAttempt(authID)
}

// adaptiveAttemptTracker keeps the adaptive selector's in-flight counter aligned with
// the credential currently being attempted. Releasing is idempotent, so callers can
// release at the top of a retry loop and still rely on a function-scoped defer.
type adaptiveAttemptTracker struct {
	finish func()
}

func newAdaptiveAttemptTracker() *adaptiveAttemptTracker {
	return &adaptiveAttemptTracker{}
}

func (t *adaptiveAttemptTracker) begin(m *Manager, authID string) {
	if t == nil {
		return
	}
	t.release()
	t.finish = m.beginAdaptiveAttempt(authID)
}

func (t *adaptiveAttemptTracker) release() {
	if t == nil || t.finish == nil {
		return
	}
	finish := t.finish
	t.finish = nil
	finish()
}

// handoff transfers release ownership to a long-lived consumer such as a stream reader.
func (t *adaptiveAttemptTracker) handoff() func() {
	if t == nil || t.finish == nil {
		return func() {}
	}
	finish := t.finish
	t.finish = nil
	return finish
}

func (m *Manager) observeAdaptiveResult(result Result) {
	if m == nil {
		return
	}
	m.mu.RLock()
	selector := m.selector
	m.mu.RUnlock()
	if observer, ok := selector.(interface{ ObserveResult(Result) }); ok {
		observer.ObserveResult(result)
	}
}

// AdaptiveRoutingSnapshot contains an on-demand view of live scheduling scores.
type AdaptiveRoutingSnapshot struct {
	Enabled bool                                 `json:"enabled"`
	Config  internalconfig.AdaptiveRoutingConfig `json:"config"`
	Items   []AdaptiveScore                      `json:"items"`
}

// AdaptiveRoutingScores returns live scores without mutating selector state.
func (m *Manager) AdaptiveRoutingScores(provider, model string) AdaptiveRoutingSnapshot {
	if m == nil {
		return AdaptiveRoutingSnapshot{}
	}
	m.mu.RLock()
	selector := m.selector
	auths := make([]*Auth, 0, len(m.auths))
	registryRef := registry.GetGlobalRegistry()
	for _, auth := range m.auths {
		if auth == nil {
			continue
		}
		if strings.TrimSpace(model) != "" && !m.authSupportsRouteModel(registryRef, auth, model) {
			continue
		}
		auths = append(auths, auth.Clone())
	}
	m.mu.RUnlock()
	source, ok := selector.(interface {
		AdaptiveScores(string, string, []*Auth) ([]AdaptiveScore, internalconfig.AdaptiveRoutingConfig, bool)
	})
	if !ok {
		return AdaptiveRoutingSnapshot{}
	}
	items, cfg, enabled := source.AdaptiveScores(strings.ToLower(strings.TrimSpace(provider)), strings.TrimSpace(model), auths)
	return AdaptiveRoutingSnapshot{Enabled: enabled, Config: cfg, Items: items}
}
