package tenant

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	ChannelClaude       = "claude"
	ChannelCodex        = "codex"
	ChannelGemini       = "gemini"
	ChannelXAI          = "xai"
	ChannelOpenAICompat = "openai-compat"
	ChannelVertex       = "vertex"
)

type Provider struct {
	ID        int64             `json:"id"`
	TenantID  int64             `json:"tenant_id"`
	Channel   string            `json:"channel"`
	Name      string            `json:"name"`
	BaseURL   string            `json:"base_url"`
	ProxyURL  string            `json:"proxy_url"`
	Priority  int               `json:"priority"`
	Disabled  bool              `json:"disabled"`
	Headers   map[string]string `json:"headers"`
	Models    json.RawMessage   `json:"models"`
	Extra     json.RawMessage   `json:"extra"`
	AuthIndex string            `json:"auth_index"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

type ProviderCreateInput struct {
	Channel  string
	Name     string
	BaseURL  string
	APIKey   string
	ProxyURL string
	Priority int
	Disabled bool
	Headers  map[string]string
	Models   json.RawMessage
	Extra    json.RawMessage
}

type ProviderUpdateInput struct {
	Name     *string
	BaseURL  *string
	APIKey   *string
	ProxyURL *string
	Priority *int
	Disabled *bool
	Headers  *map[string]string
	Models   *json.RawMessage
	Extra    *json.RawMessage
}

// ProviderTestConfig is an internal-facing credential view used only to run a
// server-generated provider connectivity check. APIKey must never be encoded
// into an HTTP response.
type ProviderTestConfig struct {
	Provider
	APIKey string `json:"-"`
}

// ProviderAdminView is intentionally separate from Provider. It is the only
// provider DTO exposed to the management API and never carries a plaintext
// credential or encrypted ciphertext.
type ProviderAdminView struct {
	ID                int64     `json:"id"`
	TenantID          int64     `json:"tenant_id"`
	TenantDisplayName string    `json:"tenant_display_name"`
	Channel           string    `json:"channel"`
	Name              string    `json:"name"`
	BaseURL           string    `json:"base_url"`
	ProxyURL          string    `json:"proxy_url"`
	Priority          int       `json:"priority"`
	Disabled          bool      `json:"disabled"`
	AuthIndex         string    `json:"auth_index"`
	APIKey            string    `json:"api_key"`
	ModelCount        int       `json:"model_count"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type providerRecord struct {
	Provider
	apiKeyEnc   string
	headersJSON string
	modelsJSON  string
	extraJSON   string
}

func normalizeChannel(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "openai-compatibility", "openai-compat", "openai_compat", "openai":
		return ChannelOpenAICompat, nil
	case ChannelClaude, ChannelCodex, ChannelGemini, ChannelXAI, ChannelVertex:
		return value, nil
	default:
		return "", errors.New("unsupported tenant provider channel")
	}
}

func normalizeProviderCreate(input ProviderCreateInput) (ProviderCreateInput, error) {
	channel, errChannel := normalizeChannel(input.Channel)
	if errChannel != nil {
		return ProviderCreateInput{}, errChannel
	}
	input.Channel = channel
	input.Name = strings.TrimSpace(input.Name)
	input.APIKey = strings.TrimSpace(input.APIKey)
	input.BaseURL = strings.TrimSpace(input.BaseURL)
	input.ProxyURL = strings.TrimSpace(input.ProxyURL)
	if input.Name == "" {
		return ProviderCreateInput{}, errors.New("tenant provider name is required")
	}
	if input.APIKey == "" {
		return ProviderCreateInput{}, errors.New("tenant provider API key is required")
	}
	return input, nil
}

func normalizeJSON(value json.RawMessage, fallback string) (string, error) {
	if len(value) == 0 || string(value) == "null" {
		return fallback, nil
	}
	var decoded any
	if errUnmarshal := json.Unmarshal(value, &decoded); errUnmarshal != nil {
		return "", errors.New("invalid provider JSON value")
	}
	encoded, errMarshal := json.Marshal(decoded)
	if errMarshal != nil {
		return "", fmt.Errorf("encode provider JSON value: %w", errMarshal)
	}
	return string(encoded), nil
}

func normalizeHeaders(headers map[string]string) (string, error) {
	if headers == nil {
		return "{}", nil
	}
	normalized := make(map[string]string, len(headers))
	for rawKey, rawValue := range headers {
		key := strings.TrimSpace(rawKey)
		if key == "" {
			continue
		}
		normalized[key] = strings.TrimSpace(rawValue)
	}
	encoded, errMarshal := json.Marshal(normalized)
	if errMarshal != nil {
		return "", fmt.Errorf("encode provider headers: %w", errMarshal)
	}
	return string(encoded), nil
}

func providerPendingIndex() (string, error) {
	raw := make([]byte, 18)
	if _, errRead := rand.Read(raw); errRead != nil {
		return "", fmt.Errorf("generate tenant provider id: %w", errRead)
	}
	return "pending:" + base64.RawURLEncoding.EncodeToString(raw), nil
}

func (s *Service) CreateProvider(ctx context.Context, tenantID int64, input ProviderCreateInput) (Provider, error) {
	if s == nil || s.store == nil || s.protector == nil {
		return Provider{}, errors.New("tenant service is unavailable")
	}
	if tenantID <= 0 {
		return Provider{}, errors.New("tenant id is required")
	}
	input, errNormalize := normalizeProviderCreate(input)
	if errNormalize != nil {
		return Provider{}, errNormalize
	}
	if _, errGet := s.store.GetTenant(ctx, tenantID); errGet != nil {
		return Provider{}, errGet
	}
	headersJSON, errHeaders := normalizeHeaders(input.Headers)
	if errHeaders != nil {
		return Provider{}, errHeaders
	}
	modelsJSON, errModels := normalizeJSON(input.Models, "[]")
	if errModels != nil {
		return Provider{}, errModels
	}
	extraJSON, errExtra := normalizeJSON(input.Extra, "{}")
	if errExtra != nil {
		return Provider{}, errExtra
	}
	apiKeyEnc, errEncrypt := s.protector.ProtectString(input.APIKey)
	if errEncrypt != nil {
		return Provider{}, errEncrypt
	}
	pendingIndex, errPending := providerPendingIndex()
	if errPending != nil {
		return Provider{}, errPending
	}
	item, errCreate := s.store.createProvider(ctx, tenantID, input, apiKeyEnc, headersJSON, modelsJSON, extraJSON, pendingIndex)
	if errCreate != nil {
		return Provider{}, errCreate
	}
	return item.Provider, nil
}

func (s *Service) GetProvider(ctx context.Context, tenantID, providerID int64) (Provider, error) {
	if s == nil || s.store == nil {
		return Provider{}, errors.New("tenant service is unavailable")
	}
	item, errGet := s.store.getProvider(ctx, tenantID, providerID)
	if errGet != nil {
		return Provider{}, errGet
	}
	return item.Provider, nil
}

// ProviderTestConfig returns one tenant-owned provider and its decrypted key
// for internal HTTP testing. The key is deliberately excluded from JSON.
func (s *Service) ProviderTestConfig(ctx context.Context, tenantID, providerID int64) (ProviderTestConfig, error) {
	if s == nil || s.store == nil || s.protector == nil {
		return ProviderTestConfig{}, errors.New("tenant service is unavailable")
	}
	item, errGet := s.store.getProvider(ctx, tenantID, providerID)
	if errGet != nil {
		return ProviderTestConfig{}, errGet
	}
	apiKey, errDecrypt := s.protector.UnprotectString(item.apiKeyEnc)
	if errDecrypt != nil {
		return ProviderTestConfig{}, fmt.Errorf("decrypt tenant provider API key: %w", errDecrypt)
	}
	return ProviderTestConfig{Provider: item.Provider, APIKey: apiKey}, nil
}

func (s *Service) ListProviders(ctx context.Context, tenantID int64) ([]Provider, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("tenant service is unavailable")
	}
	items, errList := s.store.listProviders(ctx, tenantID)
	if errList != nil {
		return nil, errList
	}
	result := make([]Provider, 0, len(items))
	for _, item := range items {
		result = append(result, item.Provider)
	}
	return result, nil
}

// ListProviderAdminViews exposes provider diagnostics for management without
// allowing management writes or serializing the stored encrypted secret.
// A nil tenantID returns every tenant provider; a positive ID scopes the view.
func (s *Service) ListProviderAdminViews(ctx context.Context, tenantID *int64) ([]ProviderAdminView, error) {
	if s == nil || s.store == nil || s.protector == nil {
		return nil, errors.New("tenant service is unavailable")
	}
	var tenants []Tenant
	if tenantID != nil {
		if *tenantID <= 0 {
			return nil, errors.New("tenant id is required")
		}
		item, errGet := s.store.GetTenant(ctx, *tenantID)
		if errGet != nil {
			return nil, errGet
		}
		tenants = []Tenant{item}
	} else {
		items, errList := s.store.ListTenants(ctx)
		if errList != nil {
			return nil, errList
		}
		tenants = items
	}
	result := make([]ProviderAdminView, 0)
	for _, owner := range tenants {
		providers, errList := s.store.listProviders(ctx, owner.ID)
		if errList != nil {
			return nil, errList
		}
		for _, item := range providers {
			apiKey, errDecrypt := s.protector.UnprotectString(item.apiKeyEnc)
			if errDecrypt != nil {
				return nil, fmt.Errorf("decrypt tenant provider %d: %w", item.ID, errDecrypt)
			}
			result = append(result, ProviderAdminView{
				ID: item.ID, TenantID: owner.ID, TenantDisplayName: owner.DisplayName,
				Channel: item.Channel, Name: item.Name, BaseURL: item.BaseURL, ProxyURL: item.ProxyURL,
				Priority: item.Priority, Disabled: item.Disabled, AuthIndex: item.AuthIndex,
				APIKey: maskProviderAPIKey(apiKey), ModelCount: providerModelCount(item.Models),
				CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
			})
		}
	}
	return result, nil
}

func maskProviderAPIKey(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 8 {
		return "****"
	}
	return value[:4] + "..." + value[len(value)-4:]
}

func providerModelCount(value json.RawMessage) int {
	var models []json.RawMessage
	if errUnmarshal := json.Unmarshal(value, &models); errUnmarshal != nil {
		return 0
	}
	return len(models)
}

func (s *Service) UpdateProvider(ctx context.Context, tenantID, providerID int64, input ProviderUpdateInput) (Provider, error) {
	if s == nil || s.store == nil || s.protector == nil {
		return Provider{}, errors.New("tenant service is unavailable")
	}
	current, errGet := s.store.getProvider(ctx, tenantID, providerID)
	if errGet != nil {
		return Provider{}, errGet
	}
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" {
			return Provider{}, errors.New("tenant provider name is required")
		}
		current.Name = name
	}
	if input.BaseURL != nil {
		current.BaseURL = strings.TrimSpace(*input.BaseURL)
	}
	if input.ProxyURL != nil {
		current.ProxyURL = strings.TrimSpace(*input.ProxyURL)
	}
	if input.Priority != nil {
		current.Priority = *input.Priority
	}
	if input.Disabled != nil {
		current.Disabled = *input.Disabled
	}
	if input.Headers != nil {
		headersJSON, errHeaders := normalizeHeaders(*input.Headers)
		if errHeaders != nil {
			return Provider{}, errHeaders
		}
		current.headersJSON = headersJSON
	}
	if input.Models != nil {
		modelsJSON, errModels := normalizeJSON(*input.Models, "[]")
		if errModels != nil {
			return Provider{}, errModels
		}
		current.modelsJSON = modelsJSON
	}
	if input.Extra != nil {
		extraJSON, errExtra := normalizeJSON(*input.Extra, "{}")
		if errExtra != nil {
			return Provider{}, errExtra
		}
		current.extraJSON = extraJSON
	}
	if input.APIKey != nil {
		apiKey := strings.TrimSpace(*input.APIKey)
		if apiKey == "" {
			return Provider{}, errors.New("tenant provider API key is required")
		}
		apiKeyEnc, errEncrypt := s.protector.ProtectString(apiKey)
		if errEncrypt != nil {
			return Provider{}, errEncrypt
		}
		current.apiKeyEnc = apiKeyEnc
	}
	item, errUpdate := s.store.updateProvider(ctx, current)
	if errUpdate != nil {
		return Provider{}, errUpdate
	}
	return item.Provider, nil
}

func (s *Service) DeleteProvider(ctx context.Context, tenantID, providerID int64) error {
	if s == nil || s.store == nil {
		return errors.New("tenant service is unavailable")
	}
	item, errGet := s.store.getProvider(ctx, tenantID, providerID)
	if errGet != nil {
		return errGet
	}
	if errDelete := s.store.deleteProvider(ctx, tenantID, providerID); errDelete != nil {
		return errDelete
	}
	if strings.TrimSpace(item.AuthIndex) != "" {
		if errOwnership := s.store.deleteCredentialOwnership(ctx, item.AuthIndex); errOwnership != nil {
			return errOwnership
		}
	}
	return s.reloadOwnership(ctx)
}
