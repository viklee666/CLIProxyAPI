package handlers

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

const (
	defaultFirstEventTimeoutSeconds = 0
	defaultFirstEventTimeoutRetries = 0
)

// StreamingFirstEventTimeout returns the maximum wait for the first non-empty upstream stream event.
// Returning 0 disables the timeout (default when unset).
func StreamingFirstEventTimeout(cfg *config.SDKConfig) time.Duration {
	seconds := defaultFirstEventTimeoutSeconds
	if cfg != nil {
		seconds = cfg.Streaming.FirstEventTimeoutSeconds
	}
	if seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

// StreamingFirstEventTimeoutRetries returns how many identical retries may follow first-event timeouts.
func StreamingFirstEventTimeoutRetries(cfg *config.SDKConfig) int {
	retries := defaultFirstEventTimeoutRetries
	if cfg != nil {
		retries = cfg.Streaming.FirstEventTimeoutRetries
	}
	if retries < 0 {
		return 0
	}
	return retries
}

// accessMetadataFromGinContext lifts the client-access identity resolved by the
// auth middleware into executor metadata keys.
func accessMetadataFromGinContext(c *gin.Context) map[string]any {
	meta := make(map[string]any)
	if c == nil {
		return meta
	}
	rawAccessMetadata, exists := c.Get("accessMetadata")
	if !exists {
		return meta
	}
	accessMetadata, ok := rawAccessMetadata.(map[string]string)
	if !ok {
		return meta
	}
	for _, metadataKey := range []string{
		coreexecutor.ClientKeyIDMetadataKey,
		coreexecutor.ClientTenantIDMetadataKey,
		coreexecutor.ClientGroupIDsMetadataKey,
		coreexecutor.ClientAllowAllGroupsMetadataKey,
		coreexecutor.ClientAllowUngroupedMetadataKey,
		coreexecutor.ClientReservationIDMetadataKey,
	} {
		value := strings.TrimSpace(accessMetadata[metadataKey])
		if value == "" {
			continue
		}
		if metadataKey == coreexecutor.ClientAllowAllGroupsMetadataKey || metadataKey == coreexecutor.ClientAllowUngroupedMetadataKey {
			if parsed, errParse := strconv.ParseBool(value); errParse == nil {
				meta[metadataKey] = parsed
				continue
			}
		}
		meta[metadataKey] = value
	}
	return meta
}

// ModelsForRequest returns a tenant-scoped catalog for client-access tenant
// keys and the existing global catalog for all other callers.
func (h *BaseAPIHandler) ModelsForRequest(c *gin.Context, handlerType string) []map[string]any {
	modelRegistry := registry.GetGlobalRegistry()
	metadata := accessMetadataFromGinContext(c)
	if h == nil || h.AuthManager == nil || coreauth.TenantIDFromMetadata(metadata) <= 0 {
		return modelRegistry.GetAvailableModels(handlerType)
	}
	return modelRegistry.GetAvailableModelsForClients(handlerType, h.AuthManager.TenantModelClientIDs(metadata))
}

// IsTenantRequest reports whether the authenticated request belongs to a tenant
// client-access key. It is used by protocol-specific catalog branches that
// would otherwise bypass ModelsForRequest.
func (h *BaseAPIHandler) IsTenantRequest(c *gin.Context) bool {
	if h == nil || h.AuthManager == nil {
		return false
	}
	return coreauth.TenantIDFromMetadata(accessMetadataFromGinContext(c)) > 0
}
