package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/interfaces"
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

// ClientAccessMetadata lifts the client-access identity resolved by the
// auth middleware into executor metadata keys.
func ClientAccessMetadata(c *gin.Context) map[string]any {
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

// pluginExecutorAccessError blocks direct plugin execution when it would skip
// mandatory credential selection. Provider routes still use the auth manager.
func pluginExecutorAccessError(ctx context.Context) *interfaces.ErrorMessage {
	metadata := requestExecutionMetadata(ctx)
	allowAll, _ := metadata[coreexecutor.ClientAllowAllGroupsMetadataKey].(bool)
	_, hasClientKey := metadata[coreexecutor.ClientKeyIDMetadataKey]
	if coreauth.TenantIDFromMetadata(metadata) > 0 || (hasClientKey && !allowAll) {
		return &interfaces.ErrorMessage{StatusCode: http.StatusForbidden, Error: fmt.Errorf("direct plugin execution cannot enforce the client's credential access restrictions")}
	}
	return nil
}

// ModelsForRequest returns only models backed by credentials the caller may use.
func (h *BaseAPIHandler) ModelsForRequest(c *gin.Context, handlerType string) []map[string]any {
	modelRegistry := registry.GetGlobalRegistry()
	if !h.IsModelCatalogScoped(c) {
		return modelRegistry.GetAvailableModels(handlerType)
	}
	if h == nil || h.AuthManager == nil {
		return []map[string]any{}
	}
	return modelRegistry.GetAvailableModelsForClients(handlerType, h.AuthManager.ModelClientIDs(ClientAccessMetadata(c)))
}

// ModelInfosForRequest applies request-scoped access checks to model metadata.
func (h *BaseAPIHandler) ModelInfosForRequest(c *gin.Context) []*registry.ModelInfo {
	modelRegistry := registry.GetGlobalRegistry()
	if !h.IsModelCatalogScoped(c) {
		return modelRegistry.GetAvailableModelInfos()
	}
	if h == nil || h.AuthManager == nil {
		return []*registry.ModelInfo{}
	}
	return modelRegistry.GetAvailableModelInfosForClients(h.AuthManager.ModelClientIDs(ClientAccessMetadata(c)))
}

// IsModelCatalogScoped prevents protocol-specific catalog branches from
// substituting a global or static model list for an access-controlled request.
func (h *BaseAPIHandler) IsModelCatalogScoped(c *gin.Context) bool {
	var manager *coreauth.Manager
	if h != nil {
		manager = h.AuthManager
	}
	return manager.ModelCatalogFilteringRequired(ClientAccessMetadata(c))
}

// IsTenantRequest reports whether the authenticated request belongs to a tenant
// client-access key. It is used by protocol-specific catalog branches that
// would otherwise bypass ModelsForRequest.
func (h *BaseAPIHandler) IsTenantRequest(c *gin.Context) bool {
	if h == nil || h.AuthManager == nil {
		return false
	}
	return coreauth.TenantIDFromMetadata(ClientAccessMetadata(c)) > 0
}
