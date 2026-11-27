package live

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	sdkaccess "github.com/router-for-me/CLIProxyAPI/v7/sdk/access"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

// liveAccessMetadata carries only identity established by authentication middleware.
func liveAccessMetadata(c *gin.Context) map[string]any {
	metadata := make(map[string]any)
	if c == nil {
		return metadata
	}
	raw, _ := c.Get("accessMetadata")
	access, _ := raw.(map[string]string)
	for _, key := range []string{
		coreexecutor.ClientKeyIDMetadataKey,
		coreexecutor.ClientTenantIDMetadataKey,
		coreexecutor.ClientGroupIDsMetadataKey,
		coreexecutor.ClientAllowAllGroupsMetadataKey,
		coreexecutor.ClientAllowUngroupedMetadataKey,
		coreexecutor.ClientReservationIDMetadataKey,
	} {
		value := strings.TrimSpace(access[key])
		if value == "" {
			continue
		}
		if key == coreexecutor.ClientAllowAllGroupsMetadataKey || key == coreexecutor.ClientAllowUngroupedMetadataKey {
			parsed, errParse := strconv.ParseBool(value)
			metadata[key] = errParse == nil && parsed
			continue
		}
		metadata[key] = value
	}
	return metadata
}

func liveSessionMetadata(c *gin.Context, session liveSession) map[string]any {
	metadata := liveAccessMetadata(c)
	metadata[coreexecutor.PinnedAuthMetadataKey] = session.authID
	metadata[coreexecutor.ExecutionSessionMetadataKey] = session.callID
	return metadata
}

func authorizeLiveSession(c *gin.Context, session liveSession) bool {
	if principal, hasClientSecret := c.Get(ClientSecretPrincipalContextKey); hasClientSecret {
		value, _ := principal.(string)
		if session.clientSecretPrincipal == "" || value != session.clientSecretPrincipal {
			writeRealtimeError(c, http.StatusForbidden, "Realtime client secret is not valid for this call", "invalid_request_error", "realtime_client_secret_scope_mismatch")
			return false
		}
	}
	principal, provider := requestOwner(c)
	metadata := liveAccessMetadata(c)
	keyID, _ := metadata[coreexecutor.ClientKeyIDMetadataKey].(string)
	tenantID, _ := metadata[coreexecutor.ClientTenantIDMetadataKey].(string)
	if principal != session.ownerPrincipal || provider != session.ownerProvider || keyID != session.clientKeyID || tenantID != session.clientTenantID {
		writeRealtimeError(c, http.StatusForbidden, "Realtime call belongs to another API principal", "invalid_request_error", "realtime_call_scope_mismatch")
		return false
	}
	return true
}

func cloneClientSecretAccessMetadata(source map[string]string) map[string]string {
	metadata := make(map[string]string)
	for _, key := range []string{
		coreexecutor.ClientKeyIDMetadataKey,
		coreexecutor.ClientTenantIDMetadataKey,
		coreexecutor.ClientGroupIDsMetadataKey,
		coreexecutor.ClientAllowAllGroupsMetadataKey,
		coreexecutor.ClientAllowUngroupedMetadataKey,
	} {
		if value, exists := source[key]; exists {
			metadata[key] = value
		}
	}
	return metadata
}

// AuthorizeClientSecretIssuer revalidates the issuer and reserves fresh request quota.
// The caller must release the returned reservation when the request finishes.
func AuthorizeClientSecretIssuer(c *gin.Context, authorization ClientSecretAuthorization, manager *sdkaccess.Manager) (func(), bool) {
	principal := authorization.IssuerPrincipal
	provider := authorization.IssuerProvider
	if principal != "" {
		// Authenticate only the issuer credential, never credentials supplied alongside
		// the ephemeral key. A fresh request also prevents alternate query/header keys
		// from replacing the identity whose authority the secret delegates.
		request := c.Request.Clone(c.Request.Context())
		request.Header = make(http.Header)
		request.Header.Set("Authorization", "Bearer "+principal)
		request.URL.RawQuery = ""
		result, errAuthenticate := manager.Authenticate(request.Context(), request)
		if errAuthenticate != nil {
			for key, values := range errAuthenticate.Headers {
				for _, value := range values {
					c.Writer.Header().Add(key, value)
				}
			}
			writeRealtimeError(c, errAuthenticate.HTTPStatusCode(), "Realtime client secret issuer authorization failed", "authentication_error", "invalid_realtime_client_secret")
			c.Abort()
			return nil, false
		}
		if result == nil || result.Principal != principal || result.Provider != provider ||
			result.Metadata[coreexecutor.ClientKeyIDMetadataKey] != authorization.AccessMetadata[coreexecutor.ClientKeyIDMetadataKey] ||
			result.Metadata[coreexecutor.ClientTenantIDMetadataKey] != authorization.AccessMetadata[coreexecutor.ClientTenantIDMetadataKey] {
			if result != nil && result.Release != nil {
				result.Release()
			}
			writeRealtimeError(c, http.StatusUnauthorized, "Realtime client secret issuer is no longer authorized", "authentication_error", "invalid_realtime_client_secret")
			c.Abort()
			return nil, false
		}
		c.Set("userApiKey", result.Principal)
		c.Set("accessPrincipal", result.Principal)
		c.Set("accessProvider", result.Provider)
		c.Set("accessMetadata", result.Metadata)
		return result.Release, true
	}
	if len(manager.Providers()) != 0 || len(authorization.AccessMetadata) != 0 {
		writeRealtimeError(c, http.StatusUnauthorized, "Realtime client secret has no authenticated issuer", "authentication_error", "invalid_realtime_client_secret")
		c.Abort()
		return nil, false
	}
	// Preserve anonymous mode only while the server still permits anonymous access.
	c.Set("userApiKey", authorization.Principal)
	c.Set("accessPrincipal", authorization.Principal)
	c.Set("accessProvider", "realtime-client-secret")
	return nil, true
}
