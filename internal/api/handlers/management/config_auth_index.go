package management

import (
	"fmt"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/watcher/synthesizer"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

type geminiKeyWithAuthIndex struct {
	config.GeminiKey
	AuthIndex string `json:"auth-index,omitempty"`
}

type claudeKeyWithAuthIndex struct {
	config.ClaudeKey
	AuthIndex string `json:"auth-index,omitempty"`
}

type codexKeyWithAuthIndex struct {
	config.CodexKey
	AuthIndex string `json:"auth-index,omitempty"`
}

type xaiKeyWithAuthIndex struct {
	config.XAIKey
	AuthIndex string `json:"auth-index,omitempty"`
}

type vertexCompatKeyWithAuthIndex struct {
	config.VertexCompatKey
	AuthIndex string `json:"auth-index,omitempty"`
}

type openAICompatibilityAPIKeyWithAuthIndex struct {
	config.OpenAICompatibilityAPIKey
	AuthIndex string `json:"auth-index,omitempty"`
}

type openAICompatibilityWithAuthIndex struct {
	Name           string                                   `json:"name"`
	Priority       int                                      `json:"priority,omitempty"`
	Disabled       bool                                     `json:"disabled"`
	Prefix         string                                   `json:"prefix,omitempty"`
	BaseURL        string                                   `json:"base-url"`
	APIKeyEntries  []openAICompatibilityAPIKeyWithAuthIndex `json:"api-key-entries,omitempty"`
	Models         []config.OpenAICompatibilityModel        `json:"models,omitempty"`
	Headers        map[string]string                        `json:"headers,omitempty"`
	DisableCooling bool                                     `json:"disable-cooling"`
	AuthIndex      string                                   `json:"auth-index,omitempty"`
}

func (h *Handler) liveAuthIndexByID() map[string]string {
	out := map[string]string{}
	if h == nil {
		return out
	}
	h.mu.Lock()
	manager := h.authManager
	h.mu.Unlock()
	if manager == nil {
		return out
	}
	// authManager.List() returns clones, so EnsureIndex only affects these copies.
	for _, auth := range manager.List() {
		if auth == nil {
			continue
		}
		id := strings.TrimSpace(auth.ID)
		if id == "" {
			continue
		}
		idx := strings.TrimSpace(auth.Index)
		if idx == "" {
			idx = auth.EnsureIndex()
		}
		if idx == "" {
			continue
		}
		out[id] = idx
	}
	return out
}

type apiKeyConfigIdentity interface {
	GetAPIKey() string
	GetBaseURL() string
}

func apiKeyConfigAuthIndices[T apiKeyConfigIdentity](entries []T, kind string, liveIndexByID map[string]string) []string {
	indices := make([]string, len(entries))
	idGen := synthesizer.NewStableIDGenerator()
	provider := strings.TrimSuffix(kind, ":apikey")
	for i := range entries {
		key := strings.TrimSpace(entries[i].GetAPIKey())
		if key == "" {
			continue
		}
		id, _ := idGen.Next(kind, key, entries[i].GetBaseURL())
		indices[i] = configAuthIndex(liveIndexByID, &coreauth.Auth{
			ID:       id,
			Provider: provider,
			Attributes: map[string]string{
				"api_key":  key,
				"base_url": strings.TrimSpace(entries[i].GetBaseURL()),
			},
		})
	}
	return indices
}

func vertexConfigAuthIndices(entries []config.VertexCompatKey, liveIndexByID map[string]string) []string {
	indices := make([]string, len(entries))
	idGen := synthesizer.NewStableIDGenerator()
	for i := range entries {
		entry := entries[i]
		id, _ := idGen.Next("vertex:apikey", entry.APIKey, entry.BaseURL, entry.ProxyURL)
		indices[i] = configAuthIndex(liveIndexByID, &coreauth.Auth{
			ID:       id,
			Provider: "vertex",
			Attributes: map[string]string{
				"api_key":  strings.TrimSpace(entry.APIKey),
				"base_url": strings.TrimSpace(entry.BaseURL),
			},
		})
	}
	return indices
}

func configAuthIndex(liveIndexByID map[string]string, auth *coreauth.Auth) string {
	if auth == nil {
		return ""
	}
	if authIndex := strings.TrimSpace(liveIndexByID[auth.ID]); authIndex != "" {
		return authIndex
	}
	return strings.TrimSpace(auth.EnsureIndex())
}

type openAICompatibilityAuthIndexSet struct {
	AuthIndex     string
	APIKeyIndices []string
}

func openAICompatibilityAuthIndexSets(entries []config.OpenAICompatibility, liveIndexByID map[string]string) []openAICompatibilityAuthIndexSet {
	normalized := normalizedOpenAICompatibilityEntries(entries)
	sets := make([]openAICompatibilityAuthIndexSet, len(normalized))
	idGen := synthesizer.NewStableIDGenerator()
	for i := range normalized {
		entry := normalized[i]
		providerName := strings.ToLower(strings.TrimSpace(entry.Name))
		if providerName == "" {
			providerName = "openai-compatibility"
		}
		idKind := fmt.Sprintf("openai-compatibility:%s", providerName)
		if len(entry.APIKeyEntries) == 0 {
			id, _ := idGen.Next(idKind, entry.BaseURL)
			sets[i].AuthIndex = configAuthIndex(liveIndexByID, &coreauth.Auth{
				ID:       id,
				Provider: "openai-compatibility",
				Attributes: map[string]string{
					"base_url":    strings.TrimSpace(entry.BaseURL),
					"compat_name": entry.Name,
				},
			})
			continue
		}
		sets[i].APIKeyIndices = make([]string, len(entry.APIKeyEntries))
		for j := range entry.APIKeyEntries {
			apiKeyEntry := entry.APIKeyEntries[j]
			id, _ := idGen.Next(idKind, apiKeyEntry.APIKey, entry.BaseURL, apiKeyEntry.ProxyURL)
			sets[i].APIKeyIndices[j] = configAuthIndex(liveIndexByID, &coreauth.Auth{
				ID:       id,
				Provider: "openai-compatibility",
				Attributes: map[string]string{
					"api_key":     strings.TrimSpace(apiKeyEntry.APIKey),
					"base_url":    strings.TrimSpace(entry.BaseURL),
					"compat_name": entry.Name,
				},
			})
		}
	}
	return sets
}

func (s openAICompatibilityAuthIndexSet) all() []string {
	out := make([]string, 0, len(s.APIKeyIndices)+1)
	if s.AuthIndex != "" {
		out = append(out, s.AuthIndex)
	}
	for _, authIndex := range s.APIKeyIndices {
		if authIndex != "" {
			out = append(out, authIndex)
		}
	}
	return out
}

func findAPIKeyConfigIndexByAuthIndex[T apiKeyConfigIdentity](entries []T, kind string, target string, liveIndexByID map[string]string) int {
	target = strings.TrimSpace(target)
	if target == "" || len(entries) == 0 || len(liveIndexByID) == 0 {
		return -1
	}
	idGen := synthesizer.NewStableIDGenerator()
	for i := range entries {
		key := strings.TrimSpace(entries[i].GetAPIKey())
		if key == "" {
			continue
		}
		id, _ := idGen.Next(kind, key, entries[i].GetBaseURL())
		if strings.TrimSpace(liveIndexByID[id]) == target {
			return i
		}
	}
	return -1
}

func findVertexConfigIndexByAuthIndex(entries []config.VertexCompatKey, target string, liveIndexByID map[string]string) int {
	target = strings.TrimSpace(target)
	if target == "" || len(entries) == 0 || len(liveIndexByID) == 0 {
		return -1
	}
	idGen := synthesizer.NewStableIDGenerator()
	for i := range entries {
		entry := entries[i]
		id, _ := idGen.Next("vertex:apikey", entry.APIKey, entry.BaseURL, entry.ProxyURL)
		if strings.TrimSpace(liveIndexByID[id]) == target {
			return i
		}
	}
	return -1
}

func (h *Handler) geminiKeysWithAuthIndex() []geminiKeyWithAuthIndex {
	if h == nil {
		return nil
	}
	liveIndexByID := h.liveAuthIndexByID()

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cfg == nil {
		return nil
	}

	authIndices := apiKeyConfigAuthIndices(h.cfg.GeminiKey, "gemini:apikey", liveIndexByID)
	out := make([]geminiKeyWithAuthIndex, len(h.cfg.GeminiKey))
	for i := range h.cfg.GeminiKey {
		entry := h.cfg.GeminiKey[i]
		out[i] = geminiKeyWithAuthIndex{
			GeminiKey: entry,
			AuthIndex: authIndices[i],
		}
	}
	return out
}

func (h *Handler) interactionsKeysWithAuthIndex() []geminiKeyWithAuthIndex {
	if h == nil {
		return nil
	}
	liveIndexByID := h.liveAuthIndexByID()

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cfg == nil {
		return nil
	}

	authIndices := apiKeyConfigAuthIndices(h.cfg.InteractionsKey, "gemini-interactions:apikey", liveIndexByID)
	out := make([]geminiKeyWithAuthIndex, len(h.cfg.InteractionsKey))
	for i := range h.cfg.InteractionsKey {
		entry := h.cfg.InteractionsKey[i]
		out[i] = geminiKeyWithAuthIndex{
			GeminiKey: entry,
			AuthIndex: authIndices[i],
		}
	}
	return out
}

func (h *Handler) claudeKeysWithAuthIndex() []claudeKeyWithAuthIndex {
	if h == nil {
		return nil
	}
	liveIndexByID := h.liveAuthIndexByID()

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cfg == nil {
		return nil
	}

	authIndices := apiKeyConfigAuthIndices(h.cfg.ClaudeKey, "claude:apikey", liveIndexByID)
	out := make([]claudeKeyWithAuthIndex, len(h.cfg.ClaudeKey))
	for i := range h.cfg.ClaudeKey {
		entry := h.cfg.ClaudeKey[i]
		out[i] = claudeKeyWithAuthIndex{
			ClaudeKey: entry,
			AuthIndex: authIndices[i],
		}
	}
	return out
}

func (h *Handler) codexKeysWithAuthIndex() []codexKeyWithAuthIndex {
	if h == nil {
		return nil
	}
	liveIndexByID := h.liveAuthIndexByID()

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cfg == nil {
		return nil
	}

	authIndices := apiKeyConfigAuthIndices(h.cfg.CodexKey, "codex:apikey", liveIndexByID)
	out := make([]codexKeyWithAuthIndex, len(h.cfg.CodexKey))
	for i := range h.cfg.CodexKey {
		entry := h.cfg.CodexKey[i]
		out[i] = codexKeyWithAuthIndex{
			CodexKey:  entry,
			AuthIndex: authIndices[i],
		}
	}
	return out
}

func (h *Handler) xaiKeysWithAuthIndex() []xaiKeyWithAuthIndex {
	if h == nil {
		return nil
	}
	liveIndexByID := h.liveAuthIndexByID()

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cfg == nil {
		return nil
	}

	authIndices := apiKeyConfigAuthIndices(h.cfg.XAIKey, "xai:apikey", liveIndexByID)
	out := make([]xaiKeyWithAuthIndex, len(h.cfg.XAIKey))
	for i := range h.cfg.XAIKey {
		entry := h.cfg.XAIKey[i]
		out[i] = xaiKeyWithAuthIndex{
			XAIKey:    entry,
			AuthIndex: authIndices[i],
		}
	}
	return out
}

func (h *Handler) vertexCompatKeysWithAuthIndex() []vertexCompatKeyWithAuthIndex {
	if h == nil {
		return nil
	}
	liveIndexByID := h.liveAuthIndexByID()

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cfg == nil {
		return nil
	}

	authIndices := vertexConfigAuthIndices(h.cfg.VertexCompatAPIKey, liveIndexByID)
	out := make([]vertexCompatKeyWithAuthIndex, len(h.cfg.VertexCompatAPIKey))
	for i := range h.cfg.VertexCompatAPIKey {
		entry := h.cfg.VertexCompatAPIKey[i]
		out[i] = vertexCompatKeyWithAuthIndex{
			VertexCompatKey: entry,
			AuthIndex:       authIndices[i],
		}
	}
	return out
}

func (h *Handler) openAICompatibilityWithAuthIndex() []openAICompatibilityWithAuthIndex {
	if h == nil {
		return nil
	}
	liveIndexByID := h.liveAuthIndexByID()

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cfg == nil {
		return nil
	}

	normalized := normalizedOpenAICompatibilityEntries(h.cfg.OpenAICompatibility)
	authIndexSets := openAICompatibilityAuthIndexSets(normalized, liveIndexByID)
	out := make([]openAICompatibilityWithAuthIndex, len(normalized))
	for i := range normalized {
		entry := normalized[i]

		response := openAICompatibilityWithAuthIndex{
			Name:           entry.Name,
			Priority:       entry.Priority,
			Disabled:       entry.Disabled,
			Prefix:         entry.Prefix,
			BaseURL:        entry.BaseURL,
			Models:         entry.Models,
			Headers:        entry.Headers,
			DisableCooling: entry.DisableCooling,
			AuthIndex:      authIndexSets[i].AuthIndex,
		}
		if len(entry.APIKeyEntries) > 0 {
			response.APIKeyEntries = make([]openAICompatibilityAPIKeyWithAuthIndex, len(entry.APIKeyEntries))
			for j := range entry.APIKeyEntries {
				apiKeyEntry := entry.APIKeyEntries[j]
				response.APIKeyEntries[j] = openAICompatibilityAPIKeyWithAuthIndex{
					OpenAICompatibilityAPIKey: apiKeyEntry,
					AuthIndex:                 authIndexSets[i].APIKeyIndices[j],
				}
			}
		}
		out[i] = response
	}
	return out
}
