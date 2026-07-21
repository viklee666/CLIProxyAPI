package management

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/util"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

const (
	latestReleaseURL             = "https://api.github.com/repos/router-for-me/CLIProxyAPI/releases/latest"
	latestReleaseUserAgent       = "CLIProxyAPI"
	maxLatestReleaseBytes        = 256 * 1024
	maxConfigYAMLBytes           = 16 * 1024 * 1024
	maxConfigFullResponseBytes   = 32 * 1024 * 1024
	maxConfigUIResponseBytes     = 2 * 1024 * 1024
	maxConfigUICollectionEntries = 200
)

func (h *Handler) GetConfig(c *gin.Context) {
	if h == nil || h.cfg == nil {
		c.JSON(http.StatusOK, gin.H{})
		return
	}
	view := strings.ToLower(strings.TrimSpace(c.Query("view")))
	if view == "" {
		view = "full"
	}
	if view != "full" && view != "ui" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_view", "message": "view must be full or ui"})
		return
	}

	h.mu.Lock()
	var (
		data []byte
		err  error
	)
	if view == "ui" {
		data, err = h.marshalUIConfigLocked()
	} else {
		data, err = json.Marshal(h.cfg)
	}
	h.mu.Unlock()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "encode_failed", "message": err.Error()})
		return
	}
	maxBytes := int64(maxConfigFullResponseBytes)
	if view == "ui" {
		maxBytes = maxConfigUIResponseBytes
	}
	if int64(len(data)) > maxBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{
			"error":   "response_too_large",
			"message": fmt.Sprintf("config %s response must not exceed %d bytes", view, maxBytes),
		})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("X-Config-View", view)
	c.Data(http.StatusOK, "application/json; charset=utf-8", data)
}

// marshalUIConfigLocked returns the bounded bootstrap projection used by the
// management UI. Large editable collections have dedicated paginated APIs;
// this view carries only the first lightweight identity records needed for
// navigation, labels, and compatibility while reporting every omitted total.
// The caller must hold h.mu.
func (h *Handler) marshalUIConfigLocked() ([]byte, error) {
	snapshot := *h.cfg
	snapshot.SDKConfig = h.cfg.SDKConfig
	snapshot.APIKeys = nil
	snapshot.GeminiKey = nil
	snapshot.InteractionsKey = nil
	snapshot.CodexKey = nil
	snapshot.XAIKey = nil
	snapshot.ClaudeKey = nil
	snapshot.VertexCompatAPIKey = nil
	snapshot.OpenAICompatibility = nil
	snapshot.OAuthExcludedModels = nil
	snapshot.OAuthModelAlias = nil
	snapshot.Payload = config.PayloadConfig{}
	snapshot.Plugins = h.cfg.Plugins
	snapshot.Plugins.StoreSources = nil
	snapshot.Plugins.StoreAuth = nil
	snapshot.Plugins.Configs = nil

	base, err := json.Marshal(&snapshot)
	if err != nil {
		return nil, err
	}
	body := map[string]any{}
	if err = json.Unmarshal(base, &body); err != nil {
		return nil, err
	}
	delete(body, "payload")
	if plugins, ok := body["plugins"].(map[string]any); ok {
		delete(plugins, "store-sources")
		delete(plugins, "store-auth")
		delete(plugins, "configs")
	}

	totals := map[string]int{
		"api-keys":             len(h.cfg.APIKeys),
		"gemini-api-key":       len(h.cfg.GeminiKey),
		"interactions-api-key": len(h.cfg.InteractionsKey),
		"codex-api-key":        len(h.cfg.CodexKey),
		"xai-api-key":          len(h.cfg.XAIKey),
		"claude-api-key":       len(h.cfg.ClaudeKey),
		"vertex-api-key":       len(h.cfg.VertexCompatAPIKey),
		"openai-compatibility": len(h.cfg.OpenAICompatibility),
	}
	truncated := make([]string, 0, len(totals))
	for key, total := range totals {
		if total > maxConfigUICollectionEntries {
			truncated = append(truncated, key)
		}
	}
	sort.Strings(truncated)

	body["api-keys"] = append([]string(nil), h.cfg.APIKeys[:boundedConfigCollectionLength(len(h.cfg.APIKeys))]...)
	body["gemini-api-key"] = projectGeminiConfigIdentities(h.cfg.GeminiKey)
	body["interactions-api-key"] = projectGeminiConfigIdentities(h.cfg.InteractionsKey)
	body["codex-api-key"] = projectCodexConfigIdentities(h.cfg.CodexKey)
	body["xai-api-key"] = projectCodexConfigIdentities(h.cfg.XAIKey)
	body["claude-api-key"] = projectClaudeConfigIdentities(h.cfg.ClaudeKey)
	body["vertex-api-key"] = projectVertexConfigIdentities(h.cfg.VertexCompatAPIKey)
	body["openai-compatibility"] = projectOpenAIConfigIdentities(h.cfg.OpenAICompatibility)
	body["collection_totals"] = totals
	body["collections_truncated"] = truncated
	body["projection"] = "ui"

	return json.Marshal(body)
}

func boundedConfigCollectionLength(total int) int {
	if total > maxConfigUICollectionEntries {
		return maxConfigUICollectionEntries
	}
	return total
}

func projectProviderConfigIdentity(name, apiKey, prefix, baseURL string, excludedModels []string, disableCooling bool) gin.H {
	item := gin.H{"api-key": apiKey}
	if name != "" {
		item["name"] = name
	}
	if prefix != "" {
		item["prefix"] = prefix
	}
	if baseURL != "" {
		item["base-url"] = baseURL
	}
	if len(excludedModels) > 0 {
		item["excluded-models"] = append([]string(nil), excludedModels...)
	}
	if disableCooling {
		item["disable-cooling"] = true
	}
	return item
}

func projectGeminiConfigIdentities(items []config.GeminiKey) []gin.H {
	items = items[:boundedConfigCollectionLength(len(items))]
	out := make([]gin.H, 0, len(items))
	for i := range items {
		item := items[i]
		out = append(out, projectProviderConfigIdentity(item.Name, item.APIKey, item.Prefix, item.BaseURL, item.ExcludedModels, item.DisableCooling))
	}
	return out
}

func projectCodexConfigIdentities(items []config.CodexKey) []gin.H {
	items = items[:boundedConfigCollectionLength(len(items))]
	out := make([]gin.H, 0, len(items))
	for i := range items {
		item := items[i]
		out = append(out, projectProviderConfigIdentity(item.Name, item.APIKey, item.Prefix, item.BaseURL, item.ExcludedModels, item.DisableCooling))
	}
	return out
}

func projectClaudeConfigIdentities(items []config.ClaudeKey) []gin.H {
	items = items[:boundedConfigCollectionLength(len(items))]
	out := make([]gin.H, 0, len(items))
	for i := range items {
		item := items[i]
		out = append(out, projectProviderConfigIdentity(item.Name, item.APIKey, item.Prefix, item.BaseURL, item.ExcludedModels, item.DisableCooling))
	}
	return out
}

func projectVertexConfigIdentities(items []config.VertexCompatKey) []gin.H {
	items = items[:boundedConfigCollectionLength(len(items))]
	out := make([]gin.H, 0, len(items))
	for i := range items {
		item := items[i]
		out = append(out, projectProviderConfigIdentity(item.Name, item.APIKey, item.Prefix, item.BaseURL, item.ExcludedModels, false))
	}
	return out
}

func projectOpenAIConfigIdentities(items []config.OpenAICompatibility) []gin.H {
	items = items[:boundedConfigCollectionLength(len(items))]
	out := make([]gin.H, 0, len(items))
	for i := range items {
		item := items[i]
		entry := gin.H{
			"name":            item.Name,
			"base-url":        item.BaseURL,
			"disabled":        item.Disabled,
			"disable-cooling": item.DisableCooling,
		}
		if item.Prefix != "" {
			entry["prefix"] = item.Prefix
		}
		keys := item.APIKeyEntries[:boundedConfigCollectionLength(len(item.APIKeyEntries))]
		keyEntries := make([]gin.H, 0, len(keys))
		for j := range keys {
			keyEntries = append(keyEntries, gin.H{"api-key": keys[j].APIKey})
		}
		entry["api-key-entries"] = keyEntries
		entry["api-key-entries-total"] = len(item.APIKeyEntries)
		entry["api-key-entries-truncated"] = len(item.APIKeyEntries) > len(keys)
		out = append(out, entry)
	}
	return out
}

type releaseInfo struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
}

// GetLatestVersion returns the latest release version from GitHub without downloading assets.
func (h *Handler) GetLatestVersion(c *gin.Context) {
	client := &http.Client{Timeout: 10 * time.Second}
	proxyURL := ""
	if h != nil && h.cfg != nil {
		proxyURL = strings.TrimSpace(h.cfg.ProxyURL)
	}
	if proxyURL != "" {
		sdkCfg := &sdkconfig.SDKConfig{ProxyURL: proxyURL}
		util.SetProxy(sdkCfg, client)
	}

	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, latestReleaseURL, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "request_create_failed", "message": err.Error()})
		return
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", latestReleaseUserAgent)

	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "request_failed", "message": err.Error()})
		return
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.WithError(errClose).Debug("failed to close latest version response body")
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		c.JSON(http.StatusBadGateway, gin.H{"error": "unexpected_status", "message": fmt.Sprintf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))})
		return
	}

	info, errRead := readLatestReleaseInfo(resp.Body)
	if errRead != nil {
		if isManagementRequestTooLarge(errRead) {
			c.JSON(http.StatusBadGateway, gin.H{"error": "response_too_large", "message": fmt.Sprintf("latest release response exceeds %d bytes", maxLatestReleaseBytes)})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": "read_failed", "message": errRead.Error()})
		return
	}
	version := strings.TrimSpace(info.TagName)
	if version == "" {
		version = strings.TrimSpace(info.Name)
	}
	if version == "" {
		c.JSON(http.StatusBadGateway, gin.H{"error": "invalid_response", "message": "missing release version"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"latest-version": version})
}

func readLatestReleaseInfo(body io.Reader) (releaseInfo, error) {
	data, err := readManagementResponseBody(body, maxLatestReleaseBytes)
	if err != nil {
		return releaseInfo{}, err
	}
	var info releaseInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return releaseInfo{}, err
	}
	return info, nil
}

func WriteConfig(path string, data []byte) error {
	data = config.NormalizeCommentIndentation(data)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	if _, errWrite := f.Write(data); errWrite != nil {
		_ = f.Close()
		return errWrite
	}
	if errSync := f.Sync(); errSync != nil {
		_ = f.Close()
		return errSync
	}
	return f.Close()
}

func (h *Handler) PutConfigYAML(c *gin.Context) {
	body, err := readManagementRequestBody(c, maxConfigYAMLBytes)
	if err != nil {
		if isManagementRequestTooLarge(err) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{
				"error":   "request_too_large",
				"message": fmt.Sprintf("config YAML must not exceed %d bytes", maxConfigYAMLBytes),
			})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_yaml", "message": "cannot read request body"})
		return
	}
	var cfg config.Config
	if err = yaml.Unmarshal(body, &cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_yaml", "message": err.Error()})
		return
	}
	// Validate config using LoadConfigOptional with optional=false to enforce parsing
	tmpDir := filepath.Dir(h.configFilePath)
	tmpFile, err := os.CreateTemp(tmpDir, "config-validate-*.yaml")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "write_failed", "message": err.Error()})
		return
	}
	tempFile := tmpFile.Name()
	if _, errWrite := tmpFile.Write(body); errWrite != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tempFile)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "write_failed", "message": errWrite.Error()})
		return
	}
	if errClose := tmpFile.Close(); errClose != nil {
		_ = os.Remove(tempFile)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "write_failed", "message": errClose.Error()})
		return
	}
	defer func() {
		_ = os.Remove(tempFile)
	}()
	_, err = config.LoadConfigOptional(tempFile, false)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "invalid_config", "message": err.Error()})
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if WriteConfig(h.configFilePath, body) != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "write_failed", "message": "failed to write config"})
		return
	}
	// Reload into handler to keep memory in sync
	newCfg, err := config.LoadConfig(h.configFilePath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "reload_failed", "message": err.Error()})
		return
	}
	h.cfg = newCfg
	c.JSON(http.StatusOK, gin.H{"ok": true, "changed": []string{"config"}})
}

// GetConfigYAML returns the raw config.yaml file bytes without re-encoding.
// It preserves comments and original formatting/styles.
func (h *Handler) GetConfigYAML(c *gin.Context) {
	file, err := os.Open(h.configFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found", "message": "config file not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "read_failed", "message": err.Error()})
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "stat_failed", "message": err.Error()})
		return
	}
	if info.Size() > maxConfigYAMLBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "response_too_large", "message": fmt.Sprintf("config YAML must not exceed %d bytes", maxConfigYAMLBytes)})
		return
	}
	c.Header("Content-Type", "application/yaml; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.DataFromReader(http.StatusOK, info.Size(), "application/yaml; charset=utf-8", file, nil)
}

// Debug
func (h *Handler) GetDebug(c *gin.Context) { c.JSON(200, gin.H{"debug": h.cfg.Debug}) }
func (h *Handler) PutDebug(c *gin.Context) { h.updateBoolField(c, func(v bool) { h.cfg.Debug = v }) }

// UsageStatisticsEnabled
func (h *Handler) GetUsageStatisticsEnabled(c *gin.Context) {
	c.JSON(200, gin.H{"usage-statistics-enabled": h.cfg.UsageStatisticsEnabled})
}
func (h *Handler) PutUsageStatisticsEnabled(c *gin.Context) {
	h.updateBoolField(c, func(v bool) { h.cfg.UsageStatisticsEnabled = v })
}

// UsageStatisticsEnabled
func (h *Handler) GetLoggingToFile(c *gin.Context) {
	c.JSON(200, gin.H{"logging-to-file": h.cfg.LoggingToFile})
}
func (h *Handler) PutLoggingToFile(c *gin.Context) {
	h.updateBoolField(c, func(v bool) { h.cfg.LoggingToFile = v })
}

// LogsMaxTotalSizeMB
func (h *Handler) GetLogsMaxTotalSizeMB(c *gin.Context) {
	c.JSON(200, gin.H{"logs-max-total-size-mb": h.cfg.LogsMaxTotalSizeMB})
}
func (h *Handler) PutLogsMaxTotalSizeMB(c *gin.Context) {
	var body struct {
		Value *int `json:"value"`
	}
	if errBindJSON := c.ShouldBindJSON(&body); errBindJSON != nil || body.Value == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	value := *body.Value
	if value < 0 {
		value = 0
	}
	h.cfg.LogsMaxTotalSizeMB = value
	h.persist(c)
}

// ErrorLogsMaxFiles
func (h *Handler) GetErrorLogsMaxFiles(c *gin.Context) {
	c.JSON(200, gin.H{"error-logs-max-files": h.cfg.ErrorLogsMaxFiles})
}
func (h *Handler) PutErrorLogsMaxFiles(c *gin.Context) {
	var body struct {
		Value *int `json:"value"`
	}
	if errBindJSON := c.ShouldBindJSON(&body); errBindJSON != nil || body.Value == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	value := *body.Value
	if value < 0 {
		value = 10
	}
	h.cfg.ErrorLogsMaxFiles = value
	h.persist(c)
}

// Request log
func (h *Handler) GetRequestLog(c *gin.Context) { c.JSON(200, gin.H{"request-log": h.cfg.RequestLog}) }
func (h *Handler) PutRequestLog(c *gin.Context) {
	h.updateBoolField(c, func(v bool) { h.cfg.RequestLog = v })
}

// Websocket auth
func (h *Handler) GetWebsocketAuth(c *gin.Context) {
	c.JSON(200, gin.H{"ws-auth": h.cfg.WebsocketAuth})
}
func (h *Handler) PutWebsocketAuth(c *gin.Context) {
	h.updateBoolField(c, func(v bool) { h.cfg.WebsocketAuth = v })
}

// Request retry
func (h *Handler) GetRequestRetry(c *gin.Context) {
	c.JSON(200, gin.H{"request-retry": h.cfg.RequestRetry})
}
func (h *Handler) PutRequestRetry(c *gin.Context) {
	h.updateIntField(c, func(v int) { h.cfg.RequestRetry = v })
}

// Max retry interval
func (h *Handler) GetMaxRetryInterval(c *gin.Context) {
	c.JSON(200, gin.H{"max-retry-interval": h.cfg.MaxRetryInterval})
}
func (h *Handler) PutMaxRetryInterval(c *gin.Context) {
	h.updateIntField(c, func(v int) { h.cfg.MaxRetryInterval = v })
}

// ForceModelPrefix
func (h *Handler) GetForceModelPrefix(c *gin.Context) {
	c.JSON(200, gin.H{"force-model-prefix": h.cfg.ForceModelPrefix})
}
func (h *Handler) PutForceModelPrefix(c *gin.Context) {
	h.updateBoolField(c, func(v bool) { h.cfg.ForceModelPrefix = v })
}

func normalizeRoutingStrategy(strategy string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(strategy))
	switch normalized {
	case "", "round-robin", "roundrobin", "rr":
		return "round-robin", true
	case "fill-first", "fillfirst", "ff":
		return "fill-first", true
	case "adaptive":
		return "adaptive", true
	default:
		return "", false
	}
}

// RoutingStrategy
func (h *Handler) GetRoutingStrategy(c *gin.Context) {
	strategy, ok := normalizeRoutingStrategy(h.cfg.Routing.Strategy)
	if !ok {
		c.JSON(200, gin.H{"strategy": strings.TrimSpace(h.cfg.Routing.Strategy)})
		return
	}
	c.JSON(200, gin.H{"strategy": strategy})
}
func (h *Handler) PutRoutingStrategy(c *gin.Context) {
	var body struct {
		Value *string `json:"value"`
	}
	if errBindJSON := c.ShouldBindJSON(&body); errBindJSON != nil || body.Value == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	normalized, ok := normalizeRoutingStrategy(*body.Value)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid strategy"})
		return
	}
	h.cfg.Routing.Strategy = normalized
	h.persist(c)
}

// Proxy URL
func (h *Handler) GetProxyURL(c *gin.Context) { c.JSON(200, gin.H{"proxy-url": h.cfg.ProxyURL}) }
func (h *Handler) PutProxyURL(c *gin.Context) {
	h.updateStringField(c, func(v string) { h.cfg.ProxyURL = v })
}
func (h *Handler) DeleteProxyURL(c *gin.Context) {
	h.cfg.ProxyURL = ""
	h.persist(c)
}
