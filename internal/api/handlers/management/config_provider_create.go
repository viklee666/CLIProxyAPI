package management

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

// The legacy PUT endpoints replace an entire provider section. These POST
// handlers provide record-level creation so the management UI never needs to
// download and upload the full config merely to add one provider.

func (h *Handler) PostGeminiKey(c *gin.Context) {
	var entry config.GeminiKey
	if !bindConfigCollectionJSON(c, &entry) {
		return
	}
	entry.APIKey = strings.TrimSpace(entry.APIKey)
	entry.BaseURL = strings.TrimSpace(entry.BaseURL)
	if entry.APIKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "api-key is required"})
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	for i := range h.cfg.GeminiKey {
		if strings.TrimSpace(h.cfg.GeminiKey[i].APIKey) == entry.APIKey && strings.TrimSpace(h.cfg.GeminiKey[i].BaseURL) == entry.BaseURL {
			c.JSON(http.StatusConflict, gin.H{"error": "item already exists"})
			return
		}
	}
	if !validateConfigCollectionSize(c, len(h.cfg.GeminiKey)+1) {
		return
	}
	h.cfg.GeminiKey = append(h.cfg.GeminiKey, entry)
	h.cfg.SanitizeGeminiKeys()
	h.persistLocked(c)
}

func (h *Handler) PostInteractionsKey(c *gin.Context) {
	var entry config.GeminiKey
	if !bindConfigCollectionJSON(c, &entry) {
		return
	}
	entry.APIKey = strings.TrimSpace(entry.APIKey)
	entry.BaseURL = strings.TrimSpace(entry.BaseURL)
	if entry.APIKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "api-key is required"})
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	for i := range h.cfg.InteractionsKey {
		if strings.TrimSpace(h.cfg.InteractionsKey[i].APIKey) == entry.APIKey && strings.TrimSpace(h.cfg.InteractionsKey[i].BaseURL) == entry.BaseURL {
			c.JSON(http.StatusConflict, gin.H{"error": "item already exists"})
			return
		}
	}
	if !validateConfigCollectionSize(c, len(h.cfg.InteractionsKey)+1) {
		return
	}
	h.cfg.InteractionsKey = append(h.cfg.InteractionsKey, entry)
	h.cfg.SanitizeInteractionsKeys()
	h.persistLocked(c)
}

func (h *Handler) PostClaudeKey(c *gin.Context) {
	var entry config.ClaudeKey
	if !bindConfigCollectionJSON(c, &entry) {
		return
	}
	normalizeClaudeKey(&entry)
	if entry.APIKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "api-key is required"})
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	for i := range h.cfg.ClaudeKey {
		if strings.TrimSpace(h.cfg.ClaudeKey[i].APIKey) == entry.APIKey && strings.TrimSpace(h.cfg.ClaudeKey[i].BaseURL) == entry.BaseURL {
			c.JSON(http.StatusConflict, gin.H{"error": "item already exists"})
			return
		}
	}
	if !validateConfigCollectionSize(c, len(h.cfg.ClaudeKey)+1) {
		return
	}
	h.cfg.ClaudeKey = append(h.cfg.ClaudeKey, entry)
	h.cfg.SanitizeClaudeKeys()
	h.persistLocked(c)
}

func (h *Handler) PostCodexKey(c *gin.Context) {
	var entry config.CodexKey
	if !bindConfigCollectionJSON(c, &entry) {
		return
	}
	normalizeCodexKey(&entry)
	if entry.BaseURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "base-url is required"})
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	for i := range h.cfg.CodexKey {
		if strings.TrimSpace(h.cfg.CodexKey[i].APIKey) == entry.APIKey && strings.TrimSpace(h.cfg.CodexKey[i].BaseURL) == entry.BaseURL {
			c.JSON(http.StatusConflict, gin.H{"error": "item already exists"})
			return
		}
	}
	if !validateConfigCollectionSize(c, len(h.cfg.CodexKey)+1) {
		return
	}
	h.cfg.CodexKey = append(h.cfg.CodexKey, entry)
	h.cfg.SanitizeCodexKeys()
	h.persistLocked(c)
}

func (h *Handler) PostXAIKey(c *gin.Context) {
	var entry config.XAIKey
	if !bindConfigCollectionJSON(c, &entry) {
		return
	}
	normalizeCodexKey(&entry)
	if entry.BaseURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "base-url is required"})
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	for i := range h.cfg.XAIKey {
		if strings.TrimSpace(h.cfg.XAIKey[i].APIKey) == entry.APIKey && strings.TrimSpace(h.cfg.XAIKey[i].BaseURL) == entry.BaseURL {
			c.JSON(http.StatusConflict, gin.H{"error": "item already exists"})
			return
		}
	}
	if !validateConfigCollectionSize(c, len(h.cfg.XAIKey)+1) {
		return
	}
	h.cfg.XAIKey = append(h.cfg.XAIKey, entry)
	h.cfg.SanitizeXAIKeys()
	h.persistLocked(c)
}

func (h *Handler) PostVertexCompatKey(c *gin.Context) {
	var entry config.VertexCompatKey
	if !bindConfigCollectionJSON(c, &entry) {
		return
	}
	normalizeVertexCompatKey(&entry)
	if entry.APIKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "api-key is required"})
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	for i := range h.cfg.VertexCompatAPIKey {
		if strings.TrimSpace(h.cfg.VertexCompatAPIKey[i].APIKey) == entry.APIKey && strings.TrimSpace(h.cfg.VertexCompatAPIKey[i].BaseURL) == entry.BaseURL {
			c.JSON(http.StatusConflict, gin.H{"error": "item already exists"})
			return
		}
	}
	if !validateConfigCollectionSize(c, len(h.cfg.VertexCompatAPIKey)+1) {
		return
	}
	h.cfg.VertexCompatAPIKey = append(h.cfg.VertexCompatAPIKey, entry)
	h.cfg.SanitizeVertexCompatKeys()
	h.persistLocked(c)
}

func (h *Handler) PostOpenAICompat(c *gin.Context) {
	var entry config.OpenAICompatibility
	if !bindConfigCollectionJSON(c, &entry) {
		return
	}
	normalizeOpenAICompatibilityEntry(&entry)
	entry.Name = strings.TrimSpace(entry.Name)
	if entry.Name == "" || entry.BaseURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name and base-url are required"})
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	for i := range h.cfg.OpenAICompatibility {
		if strings.EqualFold(strings.TrimSpace(h.cfg.OpenAICompatibility[i].Name), entry.Name) {
			c.JSON(http.StatusConflict, gin.H{"error": "item already exists"})
			return
		}
	}
	if !validateConfigCollectionSize(c, len(h.cfg.OpenAICompatibility)+1) {
		return
	}
	h.cfg.OpenAICompatibility = append(h.cfg.OpenAICompatibility, entry)
	h.cfg.SanitizeOpenAICompatibility()
	h.persistLocked(c)
}
