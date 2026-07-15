package management

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// GetConfigSummary returns only non-secret configuration counts needed by
// overview pages. It avoids serializing API keys and complete provider records.
func (h *Handler) GetConfigSummary(c *gin.Context) {
	if h == nil || h.cfg == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "configuration unavailable"})
		return
	}

	h.mu.Lock()
	apiKeys := len(h.cfg.APIKeys)
	gemini := len(h.cfg.GeminiKey)
	interactions := len(h.cfg.InteractionsKey)
	codex := len(h.cfg.CodexKey)
	claude := len(h.cfg.ClaudeKey)
	xai := len(h.cfg.XAIKey)
	vertex := len(h.cfg.VertexCompatAPIKey)
	openAI := len(h.cfg.OpenAICompatibility)
	h.mu.Unlock()

	c.JSON(http.StatusOK, gin.H{
		"api_keys":              apiKeys,
		"gemini_api_keys":       gemini,
		"interactions_api_keys": interactions,
		"codex_api_keys":        codex,
		"claude_api_keys":       claude,
		"xai_api_keys":          xai,
		"vertex_api_keys":       vertex,
		"openai_compatibility":  openAI,
		"provider_credentials":  gemini + interactions + codex + claude + xai + vertex + openAI,
	})
}
