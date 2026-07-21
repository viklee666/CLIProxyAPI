package management

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/htmlsanitize"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/pluginhost"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
	"gopkg.in/yaml.v3"
)

const (
	defaultPluginCollectionPageSize = 100
	maxPluginCollectionPageSize     = 200
)

type pluginCollectionPage struct {
	page       int
	pageSize   int
	total      int
	totalPages int
	start      int
	end        int
}

type pluginListResponse struct {
	PluginsEnabled bool              `json:"plugins_enabled"`
	PluginsDir     string            `json:"plugins_dir"`
	Plugins        []pluginListEntry `json:"plugins"`
	Page           int               `json:"page"`
	PageSize       int               `json:"page_size"`
	Total          int               `json:"total"`
	TotalPages     int               `json:"total_pages"`
	HasMore        bool              `json:"has_more"`
}

type pluginListEntry struct {
	ID               string                  `json:"id"`
	Path             string                  `json:"path"`
	Configured       bool                    `json:"configured"`
	Registered       bool                    `json:"registered"`
	Enabled          bool                    `json:"enabled"`
	EffectiveEnabled bool                    `json:"effective_enabled"`
	SupportsOAuth    bool                    `json:"supports_oauth"`
	OAuthProvider    string                  `json:"oauth_provider"`
	Logo             string                  `json:"logo"`
	ConfigFields     []pluginConfigFieldInfo `json:"config_fields"`
	Menus            []pluginMenuInfo        `json:"menus"`
	Metadata         *pluginMetadataInfo     `json:"metadata"`
}

type pluginMetadataInfo struct {
	Name             string                  `json:"name"`
	Version          string                  `json:"version"`
	Author           string                  `json:"author"`
	GitHubRepository string                  `json:"github_repository"`
	Logo             string                  `json:"logo"`
	ConfigFields     []pluginConfigFieldInfo `json:"config_fields"`
}

type pluginConfigFieldInfo struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	EnumValues  []string `json:"enum_values"`
	Description string   `json:"description"`
}

type pluginMenuInfo struct {
	Path        string `json:"path"`
	Menu        string `json:"menu"`
	Description string `json:"description"`
}

type pluginRouteListResponse struct {
	Routes     []pluginhost.ManagementRouteInfo `json:"routes"`
	Page       int                              `json:"page"`
	PageSize   int                              `json:"page_size"`
	Total      int                              `json:"total"`
	TotalPages int                              `json:"total_pages"`
	HasMore    bool                             `json:"has_more"`
}

func parsePluginCollectionPage(c *gin.Context, total int) (pluginCollectionPage, error) {
	page := pluginCollectionPage{
		page:     1,
		pageSize: defaultPluginCollectionPageSize,
	}
	if rawPage := strings.TrimSpace(c.Query("page")); rawPage != "" {
		parsed, err := strconv.Atoi(rawPage)
		if err != nil || parsed < 1 {
			return page, errors.New("page must be a positive integer")
		}
		page.page = parsed
	}
	if rawPageSize := strings.TrimSpace(c.Query("page_size")); rawPageSize != "" {
		parsed, err := strconv.Atoi(rawPageSize)
		if err != nil || parsed < 1 || parsed > maxPluginCollectionPageSize {
			return page, fmt.Errorf("page_size must be an integer between 1 and %d", maxPluginCollectionPageSize)
		}
		page.pageSize = parsed
	}
	return pluginCollectionPageForTotal(page, total), nil
}

func pluginCollectionPageForTotal(page pluginCollectionPage, total int) pluginCollectionPage {
	page.total = total
	page.totalPages = 0
	page.start = 0
	page.end = 0
	if total == 0 {
		return page
	}
	page.totalPages = (total + page.pageSize - 1) / page.pageSize
	if page.page > page.totalPages {
		page.start = total
		page.end = total
		return page
	}
	page.start = (page.page - 1) * page.pageSize
	page.end = min(page.start+page.pageSize, total)
	return page
}

func pluginCollectionPageSlice[T any](items []T, page pluginCollectionPage) []T {
	paged := make([]T, page.end-page.start)
	copy(paged, items[page.start:page.end])
	return paged
}

func writeInvalidPluginPagination(c *gin.Context, err error) {
	c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_pagination", "message": err.Error()})
}

// ListPlugins returns discovered, configured, and registered plugin entries.
func (h *Handler) ListPlugins(c *gin.Context) {
	filterID := strings.TrimSpace(c.Query("id"))
	if filterID != "" && !pluginhost.ValidatePluginID(filterID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_plugin_id", "message": "invalid plugin id"})
		return
	}
	requestedPage, errPage := parsePluginCollectionPage(c, 0)
	if errPage != nil {
		writeInvalidPluginPagination(c, errPage)
		return
	}
	if h == nil || h.cfg == nil {
		c.JSON(http.StatusOK, pluginListResponse{
			PluginsDir: "plugins",
			Plugins:    []pluginListEntry{},
			Page:       requestedPage.page,
			PageSize:   requestedPage.pageSize,
			Total:      requestedPage.total,
			TotalPages: requestedPage.totalPages,
			HasMore:    false,
		})
		return
	}

	h.mu.Lock()
	pluginsEnabled := h.cfg.Plugins.Enabled
	pluginsDir := normalizedPluginsDir(h.cfg.Plugins.Dir)
	configs := make(map[string]config.PluginInstanceConfig, len(h.cfg.Plugins.Configs))
	for id, item := range h.cfg.Plugins.Configs {
		configs[id] = item
	}
	host := h.pluginHost
	h.mu.Unlock()

	resolvedPluginsDir, errResolvePluginsDir := config.ResolvePluginsDir(pluginsDir)
	if errResolvePluginsDir != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "plugin_directory_invalid", "message": errResolvePluginsDir.Error()})
		return
	}
	pluginsDir = resolvedPluginsDir
	entries := make(map[string]pluginListEntry)
	files, errDiscover := pluginhost.DiscoverPluginFiles(pluginsDir, pluginStoreDesiredVersions(configs))
	if errDiscover != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "plugin_discovery_failed", "message": errDiscover.Error()})
		return
	}
	for _, file := range files {
		if filterID != "" && file.ID != filterID {
			continue
		}
		entries[file.ID] = pluginListEntry{
			ID:           htmlsanitize.String(file.ID),
			Path:         htmlsanitize.String(file.Path),
			Enabled:      false,
			ConfigFields: []pluginConfigFieldInfo{},
			Menus:        []pluginMenuInfo{},
		}
	}
	for id, item := range configs {
		if filterID != "" && id != filterID {
			continue
		}
		entry := entries[id]
		entry.ID = htmlsanitize.String(id)
		entry.Configured = true
		entry.Enabled = pluginInstanceEnabled(item)
		if entry.ConfigFields == nil {
			entry.ConfigFields = []pluginConfigFieldInfo{}
		}
		if entry.Menus == nil {
			entry.Menus = []pluginMenuInfo{}
		}
		entries[id] = entry
	}
	if host != nil {
		for _, info := range host.RegisteredPlugins() {
			if filterID != "" && info.ID != filterID {
				continue
			}
			entry := entries[info.ID]
			entry.ID = htmlsanitize.String(info.ID)
			entry.Registered = true
			entry.SupportsOAuth = info.SupportsOAuth
			entry.OAuthProvider = htmlsanitize.String(info.OAuthProvider)
			entry.Logo = htmlsanitize.String(info.Metadata.Logo)
			entry.ConfigFields = pluginConfigFields(info.Metadata.ConfigFields)
			entry.Menus = pluginMenus(info.Menus)
			entry.Metadata = pluginMetadata(info.Metadata)
			entries[info.ID] = entry
		}
	}

	ids := make([]string, 0, len(entries))
	for id := range entries {
		if filterID != "" && id != filterID {
			continue
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	page := pluginCollectionPageForTotal(requestedPage, len(ids))
	pagedIDs := pluginCollectionPageSlice(ids, page)
	out := make([]pluginListEntry, 0, len(pagedIDs))
	for _, id := range pagedIDs {
		entry := entries[id]
		entry.EffectiveEnabled = pluginsEnabled && entry.Enabled && entry.Registered
		if entry.ConfigFields == nil {
			entry.ConfigFields = []pluginConfigFieldInfo{}
		}
		if entry.Menus == nil {
			entry.Menus = []pluginMenuInfo{}
		}
		out = append(out, entry)
	}

	c.JSON(http.StatusOK, pluginListResponse{
		PluginsEnabled: pluginsEnabled,
		PluginsDir:     htmlsanitize.String(pluginsDir),
		Plugins:        out,
		Page:           page.page,
		PageSize:       page.pageSize,
		Total:          page.total,
		TotalPages:     page.totalPages,
		HasMore:        page.end < page.total,
	})
}

// ListPluginRoutes exposes the runtime-loaded dynamic management route set so
// route audits can verify plugin APIs that cannot be enumerated statically.
func (h *Handler) ListPluginRoutes(c *gin.Context) {
	if h == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "handler not initialized"})
		return
	}
	requestedPage, errPage := parsePluginCollectionPage(c, 0)
	if errPage != nil {
		writeInvalidPluginPagination(c, errPage)
		return
	}
	h.mu.Lock()
	host := h.pluginHost
	h.mu.Unlock()
	routes := []pluginhost.ManagementRouteInfo{}
	if host != nil {
		routes = host.ManagementRouteInventory()
	}
	page := pluginCollectionPageForTotal(requestedPage, len(routes))
	c.JSON(http.StatusOK, pluginRouteListResponse{
		Routes:     pluginCollectionPageSlice(routes, page),
		Page:       page.page,
		PageSize:   page.pageSize,
		Total:      page.total,
		TotalPages: page.totalPages,
		HasMore:    page.end < page.total,
	})
}

// GetPluginConfig returns the preserved plugins.configs.<id> object as JSON.
func (h *Handler) GetPluginConfig(c *gin.Context) {
	id, okID := pluginIDFromRequest(c)
	if !okID {
		return
	}
	if h == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "plugin_not_found", "message": "plugin not found"})
		return
	}

	h.mu.Lock()
	if h.cfg == nil {
		h.mu.Unlock()
		c.JSON(http.StatusNotFound, gin.H{"error": "plugin_not_found", "message": "plugin not found"})
		return
	}
	item, configured := h.cfg.Plugins.Configs[id]
	pluginsDir := normalizedPluginsDir(h.cfg.Plugins.Dir)
	host := h.pluginHost
	h.mu.Unlock()

	if configured {
		body, errBody := pluginConfigJSONObject(item)
		if errBody != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "plugin_config_encode_failed", "message": errBody.Error()})
			return
		}
		c.JSON(http.StatusOK, body)
		return
	}

	if pluginRegistered(host, id) {
		c.JSON(http.StatusOK, gin.H{})
		return
	}
	resolvedPluginsDir, errResolvePluginsDir := config.ResolvePluginsDir(pluginsDir)
	if errResolvePluginsDir != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "plugin_directory_invalid", "message": errResolvePluginsDir.Error()})
		return
	}
	discovered, errDiscover := pluginDiscovered(resolvedPluginsDir, id)
	if errDiscover != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "plugin_discovery_failed", "message": errDiscover.Error()})
		return
	}
	if discovered {
		c.JSON(http.StatusOK, gin.H{})
		return
	}

	c.JSON(http.StatusNotFound, gin.H{"error": "plugin_not_found", "message": "plugin not found"})
}

// PatchPluginEnabled updates plugins.configs.<id>.enabled without touching plugins.enabled.
func (h *Handler) PatchPluginEnabled(c *gin.Context) {
	id, okID := pluginIDFromRequest(c)
	if !okID {
		return
	}
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if errBindJSON := c.ShouldBindJSON(&body); errBindJSON != nil || body.Enabled == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_body", "message": "enabled is required"})
		return
	}

	h.mu.Lock()
	ensurePluginConfigMap(h.cfg)
	item := h.cfg.Plugins.Configs[id]
	node := pluginConfigNode(item)
	setYAMLMappingValue(node, "enabled", boolYAMLNode(*body.Enabled))
	updated, errConfig := pluginInstanceConfigFromNode(node)
	if errConfig != nil {
		h.mu.Unlock()
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_config", "message": errConfig.Error()})
		return
	}
	h.cfg.Plugins.Configs[id] = updated
	cfgSnapshot, okSnapshot := h.saveConfigAndSnapshotLocked(c)
	h.mu.Unlock()
	if !okSnapshot {
		return
	}

	h.reloadConfigAfterManagementSaveAsync(c.Request.Context(), cfgSnapshot)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// PutPluginConfig replaces plugins.configs.<id> with the request object.
func (h *Handler) PutPluginConfig(c *gin.Context) {
	id, okID := pluginIDFromRequest(c)
	if !okID {
		return
	}
	body, okBody := readPluginConfigObject(c)
	if !okBody {
		return
	}
	node, errNode := yamlNodeFromJSONObject(body)
	if errNode != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_body", "message": errNode.Error()})
		return
	}
	updated, errConfig := pluginInstanceConfigFromNode(node)
	if errConfig != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_config", "message": errConfig.Error()})
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	ensurePluginConfigMap(h.cfg)
	h.cfg.Plugins.Configs[id] = updated
	h.persistLocked(c)
}

// PatchPluginConfig shallow-merges plugins.configs.<id> with the request object.
func (h *Handler) PatchPluginConfig(c *gin.Context) {
	id, okID := pluginIDFromRequest(c)
	if !okID {
		return
	}
	body, okBody := readPluginConfigObject(c)
	if !okBody {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	ensurePluginConfigMap(h.cfg)
	node := pluginConfigNode(h.cfg.Plugins.Configs[id])
	keys := make([]string, 0, len(body))
	for key := range body {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := body[key]
		if value == nil {
			deleteYAMLMappingKey(node, key)
			continue
		}
		valueNode, errNode := yamlNodeFromJSONValue(value)
		if errNode != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_body", "message": errNode.Error()})
			return
		}
		setYAMLMappingValue(node, key, valueNode)
	}
	updated, errConfig := pluginInstanceConfigFromNode(node)
	if errConfig != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_config", "message": errConfig.Error()})
		return
	}
	h.cfg.Plugins.Configs[id] = updated
	h.persistLocked(c)
}

// DeletePlugin removes the selected local plugin file and its saved config.
func (h *Handler) DeletePlugin(c *gin.Context) {
	id, okID := pluginIDFromRequest(c)
	if !okID {
		return
	}
	if h == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "plugin_not_found", "message": "plugin not found"})
		return
	}

	h.mu.Lock()
	if h.cfg == nil {
		h.mu.Unlock()
		c.JSON(http.StatusNotFound, gin.H{"error": "plugin_not_found", "message": "plugin not found"})
		return
	}
	pluginsDir := normalizedPluginsDir(h.cfg.Plugins.Dir)
	item, configured := h.cfg.Plugins.Configs[id]
	host := h.pluginHost
	h.mu.Unlock()

	resolvedPluginsDir, errResolvePluginsDir := config.ResolvePluginsDir(pluginsDir)
	if errResolvePluginsDir != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "plugin_directory_invalid", "message": errResolvePluginsDir.Error()})
		return
	}
	pluginsDir = resolvedPluginsDir
	var desiredVersions map[string]string
	if configured {
		desiredVersions = pluginStoreDesiredVersions(map[string]config.PluginInstanceConfig{id: item})
	}
	path, errPath := pluginFilePath(pluginsDir, id, desiredVersions)
	if errPath != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "plugin_discovery_failed", "message": errPath.Error()})
		return
	}
	if path == "" && !configured {
		c.JSON(http.StatusNotFound, gin.H{"error": "plugin_not_found", "message": "plugin not found"})
		return
	}

	if pluginBusy(host, id) && (host == nil || !host.UnloadPlugin(id)) && pluginBusy(host, id) {
		c.JSON(http.StatusConflict, gin.H{
			"error":            "plugin_delete_requires_restart",
			"message":          "loaded plugin cannot be deleted while the server is running",
			"restart_required": true,
		})
		return
	}

	fileDeleted := false
	if path != "" {
		if errRemove := os.Remove(path); errRemove != nil {
			if !errors.Is(errRemove, os.ErrNotExist) {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "plugin_delete_failed", "message": errRemove.Error()})
				return
			}
		} else {
			fileDeleted = true
		}
	}

	h.mu.Lock()
	delete(h.cfg.Plugins.Configs, id)
	if configured {
		if errSave := config.SaveConfigPreserveComments(h.configFilePath, h.cfg); errSave != nil {
			h.mu.Unlock()
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":        "config_save_failed",
				"message":      fmt.Sprintf("plugin deleted but saving config failed: %s", errSave.Error()),
				"file_deleted": fileDeleted,
				"path":         path,
			})
			return
		}
	}
	cfgSnapshot := h.reloadSnapshotConfigLocked()
	h.mu.Unlock()

	h.reloadConfigAfterManagementSaveAsync(c.Request.Context(), cfgSnapshot)
	c.JSON(http.StatusOK, gin.H{
		"status":             "deleted",
		"id":                 htmlsanitize.String(id),
		"path":               htmlsanitize.String(path),
		"file_deleted":       fileDeleted,
		"configured_removed": configured,
		"restart_required":   false,
	})
}

func normalizedPluginsDir(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "plugins"
	}
	return dir
}

func pluginInstanceEnabled(item config.PluginInstanceConfig) bool {
	if item.Enabled == nil {
		return false
	}
	return *item.Enabled
}

func pluginRegistered(host *pluginhost.Host, id string) bool {
	if host == nil {
		return false
	}
	for _, info := range host.RegisteredPlugins() {
		if info.ID == id {
			return true
		}
	}
	return false
}

func pluginDiscovered(pluginsDir string, id string) (bool, error) {
	files, errDiscover := pluginhost.DiscoverPluginFiles(pluginsDir)
	if errDiscover != nil {
		return false, errDiscover
	}
	for _, file := range files {
		if file.ID == id {
			return true, nil
		}
	}
	return false, nil
}

func pluginFilePath(pluginsDir string, id string, desiredVersions ...map[string]string) (string, error) {
	files, errDiscover := pluginhost.DiscoverPluginFiles(pluginsDir, desiredVersions...)
	if errDiscover != nil {
		return "", errDiscover
	}
	for _, file := range files {
		if file.ID == id {
			return file.Path, nil
		}
	}
	return "", nil
}

func pluginConfigFields(fields []pluginapi.ConfigField) []pluginConfigFieldInfo {
	out := make([]pluginConfigFieldInfo, 0, len(fields))
	for _, field := range fields {
		out = append(out, pluginConfigFieldInfo{
			Name:        htmlsanitize.String(field.Name),
			Type:        htmlsanitize.String(string(field.Type)),
			EnumValues:  htmlsanitize.Strings(field.EnumValues),
			Description: htmlsanitize.String(field.Description),
		})
	}
	return out
}

func pluginMenus(menus []pluginhost.RegisteredPluginMenu) []pluginMenuInfo {
	out := make([]pluginMenuInfo, 0, len(menus))
	for _, menu := range menus {
		out = append(out, pluginMenuInfo{
			Path:        htmlsanitize.String(menu.Path),
			Menu:        htmlsanitize.String(menu.Menu),
			Description: htmlsanitize.String(menu.Description),
		})
	}
	return out
}

func pluginMetadata(meta pluginapi.Metadata) *pluginMetadataInfo {
	return &pluginMetadataInfo{
		Name:             htmlsanitize.String(meta.Name),
		Version:          htmlsanitize.String(meta.Version),
		Author:           htmlsanitize.String(meta.Author),
		GitHubRepository: htmlsanitize.String(meta.GitHubRepository),
		Logo:             htmlsanitize.String(meta.Logo),
		ConfigFields:     pluginConfigFields(meta.ConfigFields),
	}
}

func pluginIDFromRequest(c *gin.Context) (string, bool) {
	id := strings.TrimSpace(c.Param("id"))
	if !pluginhost.ValidatePluginID(id) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_plugin_id", "message": "invalid plugin id"})
		return "", false
	}
	return id, true
}

func readPluginConfigObject(c *gin.Context) (map[string]any, bool) {
	decoder := json.NewDecoder(c.Request.Body)
	decoder.UseNumber()
	var body map[string]any
	if errDecode := decoder.Decode(&body); errDecode != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_body", "message": errDecode.Error()})
		return nil, false
	}
	if body == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_body", "message": "body must be a JSON object"})
		return nil, false
	}
	return body, true
}

func ensurePluginConfigMap(cfg *config.Config) {
	if cfg == nil {
		return
	}
	cfg.NormalizePluginsConfig()
}

func pluginConfigNode(item config.PluginInstanceConfig) *yaml.Node {
	if item.Raw.Kind == yaml.MappingNode {
		return cloneYAMLNode(&item.Raw)
	}
	node := emptyYAMLMappingNode()
	if item.Enabled != nil {
		setYAMLMappingValue(node, "enabled", boolYAMLNode(*item.Enabled))
	}
	if item.Priority != 0 {
		setYAMLMappingValue(node, "priority", intYAMLNode(item.Priority))
	}
	return node
}

func pluginConfigJSONObject(item config.PluginInstanceConfig) (map[string]any, error) {
	value, errValue := yamlNodeToJSONValue(pluginConfigNode(item))
	if errValue != nil {
		return nil, errValue
	}
	body, ok := value.(map[string]any)
	if !ok || body == nil {
		return map[string]any{}, nil
	}
	return body, nil
}

func pluginInstanceConfigFromNode(node *yaml.Node) (config.PluginInstanceConfig, error) {
	if node == nil {
		node = emptyYAMLMappingNode()
	}
	var item config.PluginInstanceConfig
	if errDecode := node.Decode(&item); errDecode != nil {
		return config.PluginInstanceConfig{}, errDecode
	}
	return item, nil
}

func yamlNodeFromJSONObject(body map[string]any) (*yaml.Node, error) {
	node := emptyYAMLMappingNode()
	keys := make([]string, 0, len(body))
	for key := range body {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		valueNode, errNode := yamlNodeFromJSONValue(body[key])
		if errNode != nil {
			return nil, fmt.Errorf("%s: %w", key, errNode)
		}
		setYAMLMappingValue(node, key, valueNode)
	}
	return node, nil
}

func yamlNodeFromJSONValue(value any) (*yaml.Node, error) {
	switch typed := value.(type) {
	case nil:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}, nil
	case string:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: typed}, nil
	case bool:
		return boolYAMLNode(typed), nil
	case json.Number:
		if _, errInt64 := typed.Int64(); errInt64 == nil {
			return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: typed.String()}, nil
		}
		if _, errFloat64 := typed.Float64(); errFloat64 == nil {
			return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: typed.String()}, nil
		}
		return nil, fmt.Errorf("invalid number %q", typed.String())
	case float64:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: strconv.FormatFloat(typed, 'f', -1, 64)}, nil
	case []any:
		node := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, item := range typed {
			child, errChild := yamlNodeFromJSONValue(item)
			if errChild != nil {
				return nil, errChild
			}
			node.Content = append(node.Content, child)
		}
		return node, nil
	case map[string]any:
		return yamlNodeFromJSONObject(typed)
	default:
		return nil, fmt.Errorf("unsupported value type %T", value)
	}
}

func yamlNodeToJSONValue(node *yaml.Node) (any, error) {
	if node == nil {
		return nil, nil
	}
	switch node.Kind {
	case yaml.MappingNode:
		out := make(map[string]any, len(node.Content)/2)
		for index := 0; index+1 < len(node.Content); index += 2 {
			key := node.Content[index]
			value := node.Content[index+1]
			if key == nil {
				continue
			}
			child, errChild := yamlNodeToJSONValue(value)
			if errChild != nil {
				return nil, fmt.Errorf("%s: %w", key.Value, errChild)
			}
			out[key.Value] = child
		}
		return out, nil
	case yaml.SequenceNode:
		out := make([]any, 0, len(node.Content))
		for _, childNode := range node.Content {
			child, errChild := yamlNodeToJSONValue(childNode)
			if errChild != nil {
				return nil, errChild
			}
			out = append(out, child)
		}
		return out, nil
	case yaml.ScalarNode:
		if node.Tag == "!!str" || node.Tag == "" {
			return node.Value, nil
		}
		var value any
		if errDecode := node.Decode(&value); errDecode != nil {
			return nil, errDecode
		}
		return value, nil
	case yaml.AliasNode:
		return yamlNodeToJSONValue(node.Alias)
	default:
		return nil, fmt.Errorf("unsupported YAML node kind %d", node.Kind)
	}
}

func emptyYAMLMappingNode() *yaml.Node {
	return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
}

func boolYAMLNode(value bool) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(value)}
}

func intYAMLNode(value int) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(value)}
}

func setYAMLMappingValue(mapping *yaml.Node, key string, value *yaml.Node) {
	if mapping.Kind != yaml.MappingNode {
		*mapping = *emptyYAMLMappingNode()
	}
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if mapping.Content[index] != nil && mapping.Content[index].Value == key {
			mapping.Content[index+1] = value
			return
		}
	}
	mapping.Content = append(mapping.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value)
}

func deleteYAMLMappingKey(mapping *yaml.Node, key string) {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return
	}
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if mapping.Content[index] != nil && mapping.Content[index].Value == key {
			mapping.Content = append(mapping.Content[:index], mapping.Content[index+2:]...)
			return
		}
	}
}

func cloneYAMLNode(node *yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}
	out := *node
	if len(node.Content) > 0 {
		out.Content = make([]*yaml.Node, 0, len(node.Content))
		for _, child := range node.Content {
			out.Content = append(out.Content, cloneYAMLNode(child))
		}
	}
	return &out
}
