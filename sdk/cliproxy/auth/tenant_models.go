package auth

import (
	"encoding/json"
	"strings"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

// TenantModelPayload returns the per-provider model definition carried by a
// runtime-only tenant auth. It never falls back to global configuration.
func TenantModelPayload(auth *Auth) (channel, raw string, ok bool) {
	if auth == nil || len(auth.Attributes) == 0 {
		return "", "", false
	}
	if strings.TrimSpace(auth.Attributes[AttributeTenantID]) == "" {
		return "", "", false
	}
	raw, exists := auth.Attributes[AttributeTenantModels]
	if !exists {
		return "", "", false
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = "[]"
	}
	channel = strings.ToLower(strings.TrimSpace(auth.Attributes[AttributeTenantChannel]))
	if channel == "" {
		channel = strings.ToLower(strings.TrimSpace(auth.Provider))
	}
	switch channel {
	case "openai-compatibility", "openai_compat", "openai":
		channel = "openai-compat"
	}
	return channel, raw, true
}

func tenantModelAliasEntries(auth *Auth) ([]modelAliasEntry, bool) {
	channel, raw, ok := TenantModelPayload(auth)
	if !ok {
		return nil, false
	}
	switch channel {
	case "claude":
		var models []internalconfig.ClaudeModel
		if err := json.Unmarshal([]byte(raw), &models); err != nil {
			return nil, true
		}
		return asModelAliasEntries(models), true
	case "codex":
		var models []internalconfig.CodexModel
		if err := json.Unmarshal([]byte(raw), &models); err != nil {
			return nil, true
		}
		return asModelAliasEntries(models), true
	case "gemini":
		var models []internalconfig.GeminiModel
		if err := json.Unmarshal([]byte(raw), &models); err != nil {
			return nil, true
		}
		return asModelAliasEntries(models), true
	case "xai":
		var models []internalconfig.XAIModel
		if err := json.Unmarshal([]byte(raw), &models); err != nil {
			return nil, true
		}
		return asModelAliasEntries(models), true
	case "vertex":
		var models []internalconfig.VertexCompatModel
		if err := json.Unmarshal([]byte(raw), &models); err != nil {
			return nil, true
		}
		return asModelAliasEntries(models), true
	case "openai-compat":
		var models []internalconfig.OpenAICompatibilityModel
		if err := json.Unmarshal([]byte(raw), &models); err != nil {
			return nil, true
		}
		return asModelAliasEntries(models), true
	default:
		return nil, true
	}
}
