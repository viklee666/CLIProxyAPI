package cliproxy

import (
	"encoding/json"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
)

// registerTenantModelsForAuth publishes the model catalog carried by a tenant
// runtime auth. Tenant providers never inherit models from global configuration,
// so this short-circuits the regular registration path when it applies.
func (s *Service) registerTenantModelsForAuth(a *coreauth.Auth, provider, compatProviderKey string, excluded []string) bool {
	channel, rawModels, tenantModels := coreauth.TenantModelPayload(a)
	if !tenantModels {
		return false
	}
	forceModelPrefix := s.cfg != nil && s.cfg.ForceModelPrefix
	var models []*ModelInfo
	providerKey := provider
	switch channel {
	case "gemini":
		models = registry.GetGeminiModels()
		var configured []config.GeminiModel
		if errUnmarshal := json.Unmarshal([]byte(rawModels), &configured); errUnmarshal == nil && len(configured) > 0 {
			models = buildGeminiConfigModels(&config.GeminiKey{Models: configured})
		}
	case "claude":
		models = registry.GetClaudeModels()
		var configured []config.ClaudeModel
		if errUnmarshal := json.Unmarshal([]byte(rawModels), &configured); errUnmarshal == nil && len(configured) > 0 {
			models = buildClaudeConfigModels(&config.ClaudeKey{Models: configured})
		}
	case "codex":
		models = registry.GetCodexProModels()
		var configured []config.CodexModel
		if errUnmarshal := json.Unmarshal([]byte(rawModels), &configured); errUnmarshal == nil && len(configured) > 0 {
			models = buildCodexConfigModels(&config.CodexKey{Models: configured})
		}
	case "xai":
		models = registry.GetXAIModels()
		var configured []config.XAIModel
		if errUnmarshal := json.Unmarshal([]byte(rawModels), &configured); errUnmarshal == nil && len(configured) > 0 {
			models = buildXAIConfigModels(&config.XAIKey{Models: configured})
		}
	case "vertex":
		models = registry.GetGeminiVertexModels()
		var configured []config.VertexCompatModel
		if errUnmarshal := json.Unmarshal([]byte(rawModels), &configured); errUnmarshal == nil && len(configured) > 0 {
			models = buildVertexCompatConfigModels(&config.VertexCompatKey{Models: configured})
		}
	case "openai-compat":
		var configured []config.OpenAICompatibilityModel
		if errUnmarshal := json.Unmarshal([]byte(rawModels), &configured); errUnmarshal != nil {
			GlobalModelRegistry().UnregisterClient(a.ID)
			return true
		}
		compatName := "tenant"
		if a.Attributes != nil {
			if value := strings.TrimSpace(a.Attributes["compat_name"]); value != "" {
				compatName = value
			}
			if value := strings.TrimSpace(a.Attributes["provider_key"]); value != "" {
				providerKey = value
			}
		}
		if providerKey == "" {
			providerKey = compatProviderKey
		}
		if providerKey == "" {
			providerKey = "openai-compatibility"
		}
		models = buildOpenAICompatibilityConfigModels(&config.OpenAICompatibility{Name: compatName, Models: configured})
	default:
		GlobalModelRegistry().UnregisterClient(a.ID)
		return true
	}
	models = applyExcludedModels(models, excluded)
	if len(models) == 0 {
		GlobalModelRegistry().UnregisterClient(a.ID)
		return true
	}
	s.registerResolvedModelsForAuth(a, providerKey, applyModelPrefixes(models, a.Prefix, forceModelPrefix))
	return true
}
