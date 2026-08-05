package tenant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/watcher/synthesizer"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

type providerExtra struct {
	ExcludedModels          []string
	DisableCooling          bool
	Websockets              bool
	RebuildMidSystemMessage bool
	Cloak                   *config.CloakConfig
	ExperimentalCCHSigning  bool
	Weight                  *int
}

func (s *Service) SynthesizeAuths(ctx context.Context) ([]*coreauth.Auth, error) {
	if s == nil || s.store == nil || s.protector == nil {
		return nil, errors.New("tenant service is unavailable")
	}
	providers, errList := s.store.listProvidersForSynthesis(ctx)
	if errList != nil {
		return nil, errList
	}
	auths := make([]*coreauth.Auth, 0, len(providers))
	for _, provider := range providers {
		apiKey, errDecrypt := s.protector.UnprotectString(provider.apiKeyEnc)
		if errDecrypt != nil {
			return nil, fmt.Errorf("decrypt tenant provider %d: %w", provider.ID, errDecrypt)
		}
		auth, errSynthesize := synthesizeProvider(provider, apiKey)
		if errSynthesize != nil {
			return nil, fmt.Errorf("synthesize tenant provider %d: %w", provider.ID, errSynthesize)
		}
		oldIndex := strings.TrimSpace(provider.AuthIndex)
		newIndex := auth.EnsureIndex()
		if newIndex == "" {
			return nil, errors.New("tenant provider has no auth index")
		}
		if oldIndex != newIndex {
			if oldIndex != "" {
				if errDelete := s.store.deleteCredentialOwnership(ctx, oldIndex); errDelete != nil {
					return nil, errDelete
				}
			}
			if errUpdate := s.store.updateProviderAuthIndex(ctx, provider.ID, provider.TenantID, newIndex); errUpdate != nil {
				return nil, errUpdate
			}
		}
		if errOwnership := s.store.putCredentialOwnership(ctx, newIndex, provider.TenantID); errOwnership != nil {
			return nil, errOwnership
		}
		auths = append(auths, auth)
	}
	if errReload := s.reloadOwnership(ctx); errReload != nil {
		return nil, errReload
	}
	return auths, nil
}

func synthesizeProvider(provider providerRecord, apiKey string) (*coreauth.Auth, error) {
	configValue, errConfig := providerConfig(provider, apiKey)
	if errConfig != nil {
		return nil, errConfig
	}
	ctx := &synthesizer.SynthesisContext{
		Config:      configValue,
		Now:         time.Now().UTC(),
		IDGenerator: synthesizer.NewStableIDGenerator(),
	}
	auths, errSynthesize := synthesizer.NewConfigSynthesizer().Synthesize(ctx)
	if errSynthesize != nil {
		return nil, errSynthesize
	}
	if len(auths) != 1 || auths[0] == nil {
		return nil, errors.New("tenant provider did not produce exactly one auth")
	}
	auth := auths[0]
	if auth.Attributes == nil {
		auth.Attributes = make(map[string]string)
	}
	auth.ID = fmt.Sprintf("tenant-provider:%d:%d", provider.TenantID, provider.ID)
	auth.Index = ""
	auth.Prefix = tenantProviderPrefix(provider.TenantID, provider.Prefix)
	auth.Disabled = provider.Disabled
	if provider.Disabled {
		auth.Status = coreauth.StatusDisabled
	} else {
		auth.Status = coreauth.StatusActive
	}
	auth.Attributes["source"] = fmt.Sprintf("tenant:%d:%s[%d]", provider.TenantID, provider.Channel, provider.ID)
	auth.Attributes[coreauth.AttributeRuntimeOnly] = "true"
	auth.Attributes[coreauth.AttributeTenantID] = strconv.FormatInt(provider.TenantID, 10)
	auth.Attributes[coreauth.AttributeAuthIndexSeed] = fmt.Sprintf("tenant:%d:provider:%d:%s", provider.TenantID, provider.ID, provider.Channel)
	auth.Attributes[coreauth.AttributeTenantModels] = provider.modelsJSON
	auth.Attributes[coreauth.AttributeTenantChannel] = provider.Channel
	if errExtra := addTenantProviderExtraAttributes(auth, provider.extraJSON); errExtra != nil {
		return nil, errExtra
	}
	return auth, nil
}

func tenantProviderPrefix(tenantID int64, providerPrefix string) string {
	prefix := fmt.Sprintf("t%d", tenantID)
	providerPrefix = strings.Trim(strings.TrimSpace(providerPrefix), "/")
	if providerPrefix == "" {
		return prefix
	}
	return prefix + "/" + providerPrefix
}

func addTenantProviderExtraAttributes(auth *coreauth.Auth, rawExtra string) error {
	if auth == nil || auth.Attributes == nil {
		return nil
	}
	extra, errExtra := decodeProviderExtra(rawExtra)
	if errExtra != nil {
		return errExtra
	}
	if extra.Cloak != nil {
		data, errMarshal := json.Marshal(extra.Cloak)
		if errMarshal != nil {
			return fmt.Errorf("encode tenant provider cloak: %w", errMarshal)
		}
		auth.Attributes[coreauth.AttributeTenantClaudeCloak] = string(data)
	}
	if extra.ExperimentalCCHSigning {
		auth.Attributes[coreauth.AttributeTenantExperimentalCCHSigning] = "true"
	}
	if extra.Weight != nil {
		auth.Attributes[coreauth.AttributeWeight] = strconv.Itoa(*extra.Weight)
	}
	return nil
}

func providerConfig(provider providerRecord, apiKey string) (*config.Config, error) {
	extra, errExtra := decodeProviderExtra(provider.extraJSON)
	if errExtra != nil {
		return nil, errExtra
	}
	cfg := &config.Config{}
	switch provider.Channel {
	case ChannelClaude:
		models := make([]config.ClaudeModel, 0)
		if errModels := decodeProviderModels(provider.modelsJSON, &models); errModels != nil {
			return nil, errModels
		}
		cfg.ClaudeKey = []config.ClaudeKey{{
			Name:                    provider.Name,
			APIKey:                  apiKey,
			Priority:                provider.Priority,
			Prefix:                  provider.Prefix,
			BaseURL:                 provider.BaseURL,
			ProxyURL:                provider.ProxyURL,
			Models:                  models,
			Headers:                 provider.Headers,
			ExcludedModels:          extra.ExcludedModels,
			DisableCooling:          extra.DisableCooling,
			RebuildMidSystemMessage: extra.RebuildMidSystemMessage,
			Cloak:                   extra.Cloak,
			ExperimentalCCHSigning:  extra.ExperimentalCCHSigning,
		}}
	case ChannelCodex, ChannelXAI:
		models := make([]config.CodexModel, 0)
		if errModels := decodeProviderModels(provider.modelsJSON, &models); errModels != nil {
			return nil, errModels
		}
		entry := config.CodexKey{
			Name:           provider.Name,
			APIKey:         apiKey,
			Priority:       provider.Priority,
			Prefix:         provider.Prefix,
			BaseURL:        provider.BaseURL,
			ProxyURL:       provider.ProxyURL,
			Models:         models,
			Headers:        provider.Headers,
			ExcludedModels: extra.ExcludedModels,
			DisableCooling: extra.DisableCooling,
			Websockets:     extra.Websockets,
		}
		if provider.Channel == ChannelCodex {
			cfg.CodexKey = []config.CodexKey{entry}
		} else {
			cfg.XAIKey = []config.XAIKey{entry}
		}
	case ChannelGemini:
		models := make([]config.GeminiModel, 0)
		if errModels := decodeProviderModels(provider.modelsJSON, &models); errModels != nil {
			return nil, errModels
		}
		cfg.GeminiKey = []config.GeminiKey{{
			Name:           provider.Name,
			APIKey:         apiKey,
			Priority:       provider.Priority,
			Prefix:         provider.Prefix,
			BaseURL:        provider.BaseURL,
			ProxyURL:       provider.ProxyURL,
			Models:         models,
			Headers:        provider.Headers,
			ExcludedModels: extra.ExcludedModels,
			DisableCooling: extra.DisableCooling,
		}}
	case ChannelOpenAICompat:
		models := make([]config.OpenAICompatibilityModel, 0)
		if errModels := decodeProviderModels(provider.modelsJSON, &models); errModels != nil {
			return nil, errModels
		}
		cfg.OpenAICompatibility = []config.OpenAICompatibility{{
			Name:           provider.Name,
			Priority:       provider.Priority,
			Prefix:         provider.Prefix,
			BaseURL:        provider.BaseURL,
			APIKeyEntries:  []config.OpenAICompatibilityAPIKey{{APIKey: apiKey, ProxyURL: provider.ProxyURL}},
			Models:         models,
			Headers:        provider.Headers,
			DisableCooling: extra.DisableCooling,
		}}
	case ChannelVertex:
		models := make([]config.VertexCompatModel, 0)
		if errModels := decodeProviderModels(provider.modelsJSON, &models); errModels != nil {
			return nil, errModels
		}
		cfg.VertexCompatAPIKey = []config.VertexCompatKey{{
			Name:           provider.Name,
			APIKey:         apiKey,
			Priority:       provider.Priority,
			Prefix:         provider.Prefix,
			Weight:         extra.Weight,
			BaseURL:        provider.BaseURL,
			ProxyURL:       provider.ProxyURL,
			Models:         models,
			Headers:        provider.Headers,
			ExcludedModels: extra.ExcludedModels,
		}}
	default:
		return nil, errors.New("unsupported tenant provider channel")
	}
	return cfg, nil
}

func decodeProviderModels[T any](rawValue string, destination *[]T) error {
	rawValue = strings.TrimSpace(rawValue)
	if rawValue == "" || rawValue == "null" {
		return nil
	}
	if errUnmarshal := json.Unmarshal([]byte(rawValue), destination); errUnmarshal != nil {
		return errors.New("invalid tenant provider models")
	}
	return nil
}

func decodeProviderExtra(rawValue string) (providerExtra, error) {
	rawValue = strings.TrimSpace(rawValue)
	if rawValue == "" || rawValue == "null" || rawValue == "{}" {
		return providerExtra{}, nil
	}
	var values map[string]json.RawMessage
	if errUnmarshal := json.Unmarshal([]byte(rawValue), &values); errUnmarshal != nil {
		return providerExtra{}, errors.New("invalid tenant provider extra")
	}
	extra := providerExtra{}
	if value := firstJSONValue(values, "excluded_models", "excluded-models"); value != nil {
		if errUnmarshal := json.Unmarshal(value, &extra.ExcludedModels); errUnmarshal != nil {
			return providerExtra{}, errors.New("invalid tenant provider excluded models")
		}
	}
	if value := firstJSONValue(values, "disable_cooling", "disable-cooling"); value != nil {
		if errUnmarshal := json.Unmarshal(value, &extra.DisableCooling); errUnmarshal != nil {
			return providerExtra{}, errors.New("invalid tenant provider disable cooling")
		}
	}
	if value := firstJSONValue(values, "websockets"); value != nil {
		if errUnmarshal := json.Unmarshal(value, &extra.Websockets); errUnmarshal != nil {
			return providerExtra{}, errors.New("invalid tenant provider websockets")
		}
	}
	if value := firstJSONValue(values, "rebuild_mid_system_message", "rebuild-mid-system-message"); value != nil {
		if errUnmarshal := json.Unmarshal(value, &extra.RebuildMidSystemMessage); errUnmarshal != nil {
			return providerExtra{}, errors.New("invalid tenant provider rebuild system message")
		}
	}
	if value := firstJSONValue(values, "cloak"); value != nil {
		var cloak config.CloakConfig
		if errUnmarshal := json.Unmarshal(value, &cloak); errUnmarshal != nil {
			return providerExtra{}, errors.New("invalid tenant provider cloak")
		}
		extra.Cloak = &cloak
	}
	if value := firstJSONValue(values, "experimental_cch_signing", "experimental-cch-signing"); value != nil {
		if errUnmarshal := json.Unmarshal(value, &extra.ExperimentalCCHSigning); errUnmarshal != nil {
			return providerExtra{}, errors.New("invalid tenant provider experimental CCH signing")
		}
	}
	if value := firstJSONValue(values, "weight"); value != nil {
		var weight int
		if errUnmarshal := json.Unmarshal(value, &weight); errUnmarshal != nil {
			return providerExtra{}, errors.New("invalid tenant provider weight")
		}
		extra.Weight = &weight
	}
	return extra, nil
}

func firstJSONValue(values map[string]json.RawMessage, keys ...string) json.RawMessage {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			return value
		}
	}
	return nil
}
