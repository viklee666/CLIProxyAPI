package management

import (
	"strconv"
	"strings"

	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

// isTenantRuntimeAuth reports whether an auth record is a tenant-owned runtime
// provider. Those are managed through the dedicated tenant APIs and carry an
// execution API key, so they must never surface in generic auth-file views.
func isTenantRuntimeAuth(auth *coreauth.Auth) bool {
	if !isRuntimeOnlyAuth(auth) {
		return false
	}
	tenantID, errParse := strconv.ParseInt(strings.TrimSpace(authAttribute(auth, coreauth.AttributeTenantID)), 10, 64)
	return errParse == nil && tenantID > 0
}
