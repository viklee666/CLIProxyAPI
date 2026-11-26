package management

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/MoeForge/moego/json"
	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/clienterror"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

type pelicanRequest struct {
	AuthID    string `json:"auth_id"`
	AuthIndex string `json:"auth_index"`
	Name      string `json:"name"`
	Model     string `json:"model"`
	Prompt    string `json:"prompt"`
	Effort    string `json:"effort"`
}

// GetPelicanTargetV8 resolves one live credential and its exact registered models.
func (h *Handler) GetPelicanTargetV8(c *gin.Context) {
	if h == nil || h.authManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "auth manager is not available"})
		return
	}
	authID := strings.TrimSpace(c.Query("auth_id"))
	name, authIndex := strings.TrimSpace(c.Query("name")), strings.TrimSpace(c.Query("auth_index"))
	if kind := strings.TrimSpace(c.Query("provider_kind")); kind != "" {
		if authID != "" || name != "" || authIndex != "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "provider and credential selectors cannot be combined"})
			return
		}
		var status int
		var message string
		authID, status, message = h.pelicanConfigAuthID(kind, c.Query("config_index"), c.Query("key_index"))
		if status != 0 {
			c.JSON(status, gin.H{"error": message})
			return
		}
	} else if c.Query("config_index") != "" || c.Query("key_index") != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "provider_kind is required for configuration selectors"})
		return
	}
	target := h.pelicanTarget(c, authID, name, authIndex)
	if target == nil {
		return
	}
	registered := registry.GetGlobalRegistry().GetModelsForClient(target.ID)
	models := make([]string, 0, len(registered))
	for _, model := range registered {
		if model != nil && model.ID != "" {
			models = append(models, model.ID)
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"auth_id": target.ID, "auth_index": target.Index,
		"name": firstNonEmpty(target.FileName, target.ID), "models": models,
	})
}

func (h *Handler) pelicanConfigAuthID(kind, rawIndex, rawKeyIndex string) (string, int, string) {
	index, err := strconv.Atoi(rawIndex)
	if err != nil || index < 0 {
		return "", http.StatusBadRequest, "config_index must be a non-negative integer"
	}
	if kind != "openai" && rawKeyIndex != "" {
		return "", http.StatusBadRequest, "key_index is only valid for openai providers"
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cfg == nil {
		return "", http.StatusNotFound, "test target not found"
	}
	// Keep each whole original array: the synthesizer's duplicate-ID counters
	// depend on preceding entries, including entries with the same key and URL.
	var cfg config.Config
	count, keyIndex := 0, 0
	switch kind {
	case "gemini":
		cfg.GeminiKey, count = h.cfg.GeminiKey, len(h.cfg.GeminiKey)
	case "interactions":
		cfg.InteractionsKey, count = h.cfg.InteractionsKey, len(h.cfg.InteractionsKey)
	case "codex":
		cfg.CodexKey, count = h.cfg.CodexKey, len(h.cfg.CodexKey)
	case "claude":
		cfg.ClaudeKey, count = h.cfg.ClaudeKey, len(h.cfg.ClaudeKey)
	case "vertex":
		cfg.VertexCompatAPIKey, count = h.cfg.VertexCompatAPIKey, len(h.cfg.VertexCompatAPIKey)
	case "xai":
		cfg.XAIKey, count = h.cfg.XAIKey, len(h.cfg.XAIKey)
	case "meta":
		cfg.MetaKey, count = h.cfg.MetaKey, len(h.cfg.MetaKey)
	case "openai":
		cfg.OpenAICompatibility, count = h.cfg.OpenAICompatibility, len(h.cfg.OpenAICompatibility)
		if index < count {
			entry := &cfg.OpenAICompatibility[index]
			if entry.Disabled {
				return "", http.StatusConflict, "test target is disabled or requires sign-in"
			}
			if len(entry.APIKeyEntries) > 0 {
				keyIndex, err = strconv.Atoi(rawKeyIndex)
				if err != nil || keyIndex < 0 || keyIndex >= len(entry.APIKeyEntries) {
					return "", http.StatusBadRequest, "key_index must identify an existing openai key"
				}
			} else if rawKeyIndex != "" {
				return "", http.StatusBadRequest, "key_index must be omitted for a provider without key entries"
			}
		}
	default:
		return "", http.StatusBadRequest, "unsupported provider_kind"
	}
	if index >= count {
		return "", http.StatusNotFound, "test target not found"
	}
	position := 0
	configIndex := strconv.Itoa(index)
	for _, auth := range synthesizeConfigIdentityAuths(&cfg) {
		if auth.Attributes["config_index"] != configIndex {
			continue
		}
		if position == keyIndex {
			return auth.ID, 0, ""
		}
		position++
	}
	return "", http.StatusNotFound, "test target not found"
}

func (h *Handler) pelicanTarget(c *gin.Context, authID, name, authIndex string) *coreauth.Auth {
	if authID == "" && name == "" && authIndex == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "test target is required"})
		return nil
	}
	var target *coreauth.Auth
	if authID != "" {
		target, _ = h.authManager.GetByID(authID)
		if !matchesAuthFileLookup(target, name, authIndex) {
			target = nil
		}
	} else {
		for _, candidate := range h.authManager.List() {
			if !matchesAuthFileLookup(candidate, name, authIndex) || !h.pelicanTargetAccessible(candidate) {
				continue
			}
			if target != nil {
				c.JSON(http.StatusConflict, gin.H{"error": "test target is ambiguous; specify auth_id"})
				return nil
			}
			target = candidate
		}
	}
	if !h.pelicanTargetAccessible(target) {
		c.JSON(http.StatusNotFound, gin.H{"error": "test target not found"})
		return nil
	}
	if target.Disabled || target.Status == coreauth.StatusDisabled || target.RequiresSignIn {
		c.JSON(http.StatusConflict, gin.H{"error": "test target is disabled or requires sign-in"})
		return nil
	}
	return target
}

func (h *Handler) pelicanTargetAccessible(target *coreauth.Auth) bool {
	if target == nil || isTenantRuntimeAuth(target) {
		return false
	}
	// Check ownership independently of availability, so disabled local targets
	// receive the same conflict response during discovery and execution.
	if target.Disabled || target.Status == coreauth.StatusDisabled {
		target = target.Clone()
		target.Disabled, target.Status = false, coreauth.StatusActive
	}
	return h.authManager.CredentialAllowedForMetadata(target, nil)
}

// PelicanTestV8 generates an operator-requested comparison artifact on exactly one
// managed credential. It never accepts upstream credentials or endpoints from the UI.
func (h *Handler) PelicanTestV8(c *gin.Context) {
	if h == nil || h.authManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "auth manager is not available"})
		return
	}
	body, errRead := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 128*1024))
	if errRead != nil {
		status := http.StatusBadRequest
		var tooLarge *http.MaxBytesError
		if errors.As(errRead, &tooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		c.JSON(status, gin.H{"error": "invalid or oversized request body"})
		return
	}
	var input pelicanRequest
	if errDecode := json.Unmarshal(body, &input); errDecode != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	input.AuthID = strings.TrimSpace(input.AuthID)
	input.AuthIndex = strings.TrimSpace(input.AuthIndex)
	input.Name = strings.TrimSpace(input.Name)
	input.Model = strings.TrimSpace(input.Model)
	if (input.AuthID == "" && input.AuthIndex == "" && input.Name == "") || input.Model == "" || len(input.Model) > 512 || strings.TrimSpace(input.Prompt) == "" || len(input.Prompt) > 100000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "target, model and prompt are required; model and prompt must fit their size limits"})
		return
	}
	switch input.Effort {
	case "":
		input.Effort = "medium"
	case "low", "medium", "high", "xhigh":
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "effort must be low, medium, high or xhigh"})
		return
	}
	target := h.pelicanTarget(c, input.AuthID, input.Name, input.AuthIndex)
	if target == nil {
		return
	}

	// Reuse the normal translation, payload rules, proxy and refresh pipeline. Only
	// the fixed target is eligible; only same-identity OAuth recovery may replay it.
	doc := ojson.NewDocString(`{"model":"","messages":[{"role":"user","content":""}],"stream":false,"store":false}`)
	_ = doc.Set("model", input.Model)
	_ = doc.Set("messages.0.content", input.Prompt)
	_ = doc.Set("reasoning_effort", input.Effort)
	payload := doc.Bytes()
	sessionID := "pelican-" + uuid.NewString()
	ctx := coreexecutor.WithRequestRetryState(c.Request.Context())
	ctx = coreexecutor.WithOAuthRecoveryAllowed(ctx)
	coreexecutor.DisableRequestRetries(ctx)
	opts := coreexecutor.Options{
		SourceFormat:    sdktranslator.FormatOpenAI,
		ResponseFormat:  sdktranslator.FormatOpenAI,
		OriginalRequest: payload,
		Headers:         http.Header{"X-Session-Id": {sessionID}},
		Metadata: map[string]any{
			coreexecutor.PinnedAuthMetadataKey:           target.ID,
			coreauth.LocalManagementExecutionMetadataKey: true,
			coreexecutor.ClientAllowAllGroupsMetadataKey: true,
			coreexecutor.CanonicalSessionIDMetadataKey:   sessionID,
			coreexecutor.RequestedModelMetadataKey:       input.Model,
			coreexecutor.ReasoningEffortMetadataKey:      input.Effort,
		},
	}
	started := time.Now()
	response, errExecute := h.authManager.Execute(ctx, []string{target.Provider}, coreexecutor.Request{
		Model: input.Model, Payload: payload, Format: sdktranslator.FormatOpenAI,
	}, opts)
	if errExecute != nil {
		upstreamStatus := clienterror.HTTPStatusFromErrorOr(errExecute, http.StatusBadGateway)
		status := upstreamStatus
		// A target's rejected credential must not sign the operator out of the UI.
		if status < 400 || status > 599 || status == http.StatusUnauthorized || status == http.StatusForbidden {
			status = http.StatusBadGateway
		}
		c.JSON(status, gin.H{"error": coreauth.SanitizeRenewalMessage(errExecute.Error()), "upstream_status": upstreamStatus})
		return
	}
	answer, responseModel, usage, errAnswer := pelicanAnswer(response)
	if errAnswer != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": errAnswer.Error()})
		return
	}
	if responseModel == "" {
		responseModel = input.Model
	}
	c.JSON(http.StatusOK, gin.H{
		"auth_id":     target.ID,
		"auth_index":  target.Index,
		"name":        firstNonEmpty(target.FileName, target.ID),
		"model":       responseModel,
		"answer":      answer,
		"duration_ms": time.Since(started).Milliseconds(),
		"usage":       usage,
	})
}

func pelicanAnswer(response coreexecutor.Response) (string, string, json.RawMessage, error) {
	if response.Incomplete {
		return "", "", nil, errors.New("test generation was incomplete")
	}
	if len(response.Payload) > 8*1024*1024 || !json.Valid(response.Payload) {
		return "", "", nil, errors.New("invalid or oversized test response")
	}
	root := ojson.ParseBytes(response.Payload)
	choice := root.Get("choices.0")
	switch choice.Get("finish_reason").String() {
	case "length", "content_filter", "tool_calls", "function_call":
		return "", "", nil, errors.New("test generation did not produce a complete answer")
	}
	content := choice.Get("message.content")
	var answer string
	if content.Type == ojson.String {
		answer = content.String()
	} else if content.IsArray() {
		var text strings.Builder
		content.ForEach(func(_, part ojson.Result) bool {
			if part.Get("type").String() == "text" {
				text.WriteString(part.Get("text").String())
			}
			return true
		})
		answer = text.String()
	}
	if strings.TrimSpace(answer) == "" {
		return "", "", nil, errors.New("test generation returned no text answer")
	}
	var usage json.RawMessage
	if value := root.Get("usage"); value.IsObject() {
		usage = json.RawMessage(value.Raw)
	}
	return answer, root.Get("model").String(), usage, nil
}
