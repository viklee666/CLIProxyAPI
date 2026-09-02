package helps

import (
	"fmt"

	claudeauth "github.com/router-for-me/CLIProxyAPI/v7/internal/auth/claude"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

// ClaudeDeviceIDPool copies the credential device pool under Auth.metadataMu.
func ClaudeDeviceIDPool(auth *cliproxyauth.Auth) any {
	if auth == nil {
		return nil
	}
	return auth.ReadMetadataCopy(claudeauth.ClaudeDeviceIDsMetadataKey)
}

// StoreClaudeDeviceIDPool writes the credential device pool under Auth.metadataMu.
func StoreClaudeDeviceIDPool(auth *cliproxyauth.Auth, deviceIDs []string) {
	if auth == nil {
		return
	}
	auth.MutateMetadata(func(meta map[string]any) {
		claudeauth.StoreDeviceIDPoolIn(meta, deviceIDs)
	})
}

// EnsureClaudeDeviceIDPool repairs or creates the credential device pool under
// Auth.metadataMu. It must not take claudeDevicePoolMu.
func EnsureClaudeDeviceIDPool(auth *cliproxyauth.Auth) ([]string, bool, error) {
	if auth == nil {
		return nil, false, fmt.Errorf("ensure Claude device pool: auth is nil")
	}
	var deviceIDs []string
	var changed bool
	var err error
	auth.MutateMetadata(func(meta map[string]any) {
		deviceIDs, changed, err = claudeauth.EnsureDeviceIDPoolIn(meta)
	})
	return deviceIDs, changed, err
}
