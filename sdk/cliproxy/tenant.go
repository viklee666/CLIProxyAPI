package cliproxy

import (
	"context"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/watcher"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func (s *Service) syncTenantAuths(ctx context.Context) error {
	if s == nil || s.tenant == nil || s.coreManager == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.tenantAuthSyncMu.Lock()
	defer s.tenantAuthSyncMu.Unlock()

	auths, errSynthesize := s.tenant.SynthesizeAuths(ctx)
	if errSynthesize != nil {
		return errSynthesize
	}
	current := make(map[string]struct{})
	for _, auth := range s.coreManager.List() {
		if isTenantRuntimeAuth(auth) {
			current[auth.ID] = struct{}{}
		}
	}
	next := make(map[string]struct{}, len(auths))
	for _, auth := range auths {
		if auth == nil || auth.ID == "" {
			continue
		}
		next[auth.ID] = struct{}{}
		s.handleAuthUpdate(coreauth.WithSkipPersist(ctx), watcher.AuthUpdate{Action: watcher.AuthUpdateActionModify, ID: auth.ID, Auth: auth})
	}
	for id := range current {
		if _, exists := next[id]; exists {
			continue
		}
		s.handleAuthUpdate(coreauth.WithSkipPersist(ctx), watcher.AuthUpdate{Action: watcher.AuthUpdateActionDelete, ID: id})
	}
	return nil
}

func isTenantRuntimeAuth(auth *coreauth.Auth) bool {
	if auth == nil || auth.Attributes == nil {
		return false
	}
	value, exists := auth.Attributes[coreauth.AttributeTenantID]
	if !exists || value == "" {
		return false
	}
	return true
}
