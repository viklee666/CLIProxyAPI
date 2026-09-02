package management

import (
	"strings"

	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

// authIndicesForPath resolves every auth index backed by an auth file so client-access
// group bindings can be removed together with the credential. It must run before the
// file is deleted, while the manager can still resolve the indices.
func (h *Handler) authIndicesForPath(path string, fallbackID string) []string {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	manager := h.authManager
	h.mu.Unlock()
	if manager == nil {
		return nil
	}

	seen := make(map[string]struct{})
	out := make([]string, 0, 1)
	add := func(auth *coreauth.Auth) {
		if auth == nil {
			return
		}
		authIndex := strings.TrimSpace(auth.Index)
		if authIndex == "" {
			authIndex = strings.TrimSpace(auth.EnsureIndex())
		}
		if authIndex == "" {
			return
		}
		if _, ok := seen[authIndex]; ok {
			return
		}
		seen[authIndex] = struct{}{}
		out = append(out, authIndex)
	}

	for _, auth := range manager.List() {
		if auth == nil {
			continue
		}
		if sameAuthFilePath(authAttribute(auth, "path"), path) || sameAuthFilePath(authAttribute(auth, coreauth.AttributeVirtualSource), path) {
			add(auth)
		}
	}
	if len(out) == 0 && strings.TrimSpace(fallbackID) != "" {
		if auth, ok := manager.GetByID(strings.TrimSpace(fallbackID)); ok {
			add(auth)
		}
	}
	if len(out) == 0 {
		if auth, errBuild := h.buildAuthFromFileData(path, nil); errBuild == nil {
			add(auth)
		}
	}
	return out
}
