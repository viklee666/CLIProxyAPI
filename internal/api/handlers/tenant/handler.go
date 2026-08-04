// Package tenant implements the non-administrator tenant control-plane API.
package tenant

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/clientaccess"
	tenantservice "github.com/router-for-me/CLIProxyAPI/v7/internal/tenant"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/proxyutil"
)

const (
	tenantContextKey       = "tenant_api_subject"
	tenantTokenContextKey  = "tenant_api_token"
	tenantLoginMaxFailures = 5
	tenantLoginBlockFor    = 30 * time.Minute
	tenantTestTimeout      = 60 * time.Second
	tenantTestBodyLimit    = 1 << 20
)

type failedLogin struct {
	count        int
	blockedUntil time.Time
	lastActivity time.Time
}

// Handler owns only the tenant-facing API surface. It intentionally does not
// share the management middleware or any management credentials.
type Handler struct {
	tenants      *tenantservice.Service
	clientAccess *clientaccess.Service
	syncAuths    func(context.Context) error

	attemptsMu sync.Mutex
	attempts   map[string]*failedLogin
}

func NewHandler(tenants *tenantservice.Service, clientAccess *clientaccess.Service, syncAuths func(context.Context) error) *Handler {
	return &Handler{
		tenants:      tenants,
		clientAccess: clientAccess,
		syncAuths:    syncAuths,
		attempts:     make(map[string]*failedLogin),
	}
}

func (h *Handler) Register(engine *gin.Engine) {
	if h == nil || engine == nil {
		return
	}
	engine.POST("/v0/tenant/login", h.Login)
	tenantAPI := engine.Group("/v0/tenant")
	tenantAPI.Use(h.SessionMiddleware())
	{
		tenantAPI.POST("/logout", h.Logout)
		tenantAPI.GET("/me", h.Me)
		tenantAPI.POST("/change-password", h.ChangePassword)

		tenantAPI.GET("/providers", h.ListProviders)
		tenantAPI.POST("/providers", h.CreateProvider)
		tenantAPI.GET("/providers/:id", h.GetProvider)
		tenantAPI.PATCH("/providers/:id", h.UpdateProvider)
		tenantAPI.DELETE("/providers/:id", h.DeleteProvider)
		tenantAPI.POST("/providers/:id/test", h.TestProvider)

		tenantAPI.GET("/groups", h.ListGroups)
		tenantAPI.POST("/groups", h.CreateGroup)
		tenantAPI.GET("/groups/:id", h.GetGroup)
		tenantAPI.PATCH("/groups/:id", h.UpdateGroup)
		tenantAPI.DELETE("/groups/:id", h.DeleteGroup)
		tenantAPI.PUT("/groups/:id/credential-bindings", h.ReplaceGroupCredentialBindings)
		tenantAPI.GET("/credential-bindings", h.ListCredentialBindings)

		tenantAPI.GET("/keys", h.ListKeys)
		tenantAPI.POST("/keys", h.CreateKey)
		tenantAPI.GET("/keys/:id", h.GetKey)
		tenantAPI.PATCH("/keys/:id", h.UpdateKey)
		tenantAPI.DELETE("/keys/:id", h.DeleteKey)
	}
}

func (h *Handler) SessionMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if h == nil || h.tenants == nil {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "tenant service is unavailable"})
			return
		}
		token := bearerToken(c.Request)
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "tenant session is required"})
			return
		}
		subject, errAuthenticate := h.tenants.AuthenticateSession(c.Request.Context(), token)
		if errAuthenticate != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid tenant session"})
			return
		}
		c.Set(tenantContextKey, subject)
		c.Set(tenantTokenContextKey, token)
		c.Next()
	}
}

func bearerToken(request *http.Request) string {
	if request == nil {
		return ""
	}
	value := strings.TrimSpace(request.Header.Get("Authorization"))
	if value == "" {
		return ""
	}
	parts := strings.Fields(value)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func tenantFromContext(c *gin.Context) (tenantservice.Tenant, bool) {
	if c == nil {
		return tenantservice.Tenant{}, false
	}
	value, exists := c.Get(tenantContextKey)
	if !exists {
		return tenantservice.Tenant{}, false
	}
	subject, ok := value.(tenantservice.Tenant)
	return subject, ok && subject.ID > 0
}

func tenantID(c *gin.Context) (int64, bool) {
	subject, ok := tenantFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid tenant session"})
		return 0, false
	}
	return subject.ID, true
}

type loginRequest struct {
	Password string `json:"password"`
}

type loginResponse struct {
	Token     string               `json:"token"`
	Tenant    tenantservice.Tenant `json:"tenant"`
	ExpiresAt time.Time            `json:"expires_at"`
}

func (h *Handler) Login(c *gin.Context) {
	if h == nil || h.tenants == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "tenant service is unavailable"})
		return
	}
	clientIP := c.ClientIP()
	if h.isLoginBlocked(clientIP) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many failed login attempts"})
		return
	}
	var input loginRequest
	if errBind := c.ShouldBindJSON(&input); errBind != nil || strings.TrimSpace(input.Password) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "password is required"})
		return
	}
	subject, errAuthenticate := h.tenants.AuthenticatePassword(c.Request.Context(), input.Password)
	if errAuthenticate != nil {
		h.recordLoginFailure(clientIP)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid tenant credentials"})
		return
	}
	token, session, errIssue := h.tenants.IssueSession(c.Request.Context(), subject.ID, tenantservice.DefaultSessionTTL)
	if errIssue != nil {
		writeTenantError(c, errIssue)
		return
	}
	h.clearLoginFailures(clientIP)
	c.JSON(http.StatusOK, loginResponse{Token: token, Tenant: subject, ExpiresAt: session.ExpiresAt})
}

func (h *Handler) Logout(c *gin.Context) {
	if h == nil || h.tenants == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "tenant service is unavailable"})
		return
	}
	token, _ := c.Get(tenantTokenContextKey)
	if errRevoke := h.tenants.RevokeSession(c.Request.Context(), stringValue(token)); errRevoke != nil {
		writeTenantError(c, errRevoke)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) Me(c *gin.Context) {
	subject, ok := tenantFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid tenant session"})
		return
	}
	c.JSON(http.StatusOK, subject)
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func (h *Handler) ChangePassword(c *gin.Context) {
	tenantID, ok := tenantID(c)
	if !ok {
		return
	}
	var input changePasswordRequest
	if errBind := c.ShouldBindJSON(&input); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	if errChange := h.tenants.ChangePassword(c.Request.Context(), tenantID, input.CurrentPassword, input.NewPassword); errChange != nil {
		if errors.Is(errChange, tenantservice.ErrInvalidCredentials) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid tenant credentials"})
			return
		}
		writeTenantError(c, errChange)
		return
	}
	c.Status(http.StatusNoContent)
}

type providerCreateRequest struct {
	Channel  string            `json:"channel"`
	Name     string            `json:"name"`
	BaseURL  string            `json:"base_url"`
	APIKey   string            `json:"api_key"`
	ProxyURL string            `json:"proxy_url"`
	Priority int               `json:"priority"`
	Disabled bool              `json:"disabled"`
	Headers  map[string]string `json:"headers"`
	Models   json.RawMessage   `json:"models"`
	Extra    json.RawMessage   `json:"extra"`
}

func (input providerCreateRequest) toInput() tenantservice.ProviderCreateInput {
	return tenantservice.ProviderCreateInput{
		Channel: input.Channel, Name: input.Name, BaseURL: input.BaseURL, APIKey: input.APIKey,
		ProxyURL: input.ProxyURL, Priority: input.Priority, Disabled: input.Disabled,
		Headers: input.Headers, Models: input.Models, Extra: input.Extra,
	}
}

type providerUpdateRequest struct {
	Name     *string            `json:"name,omitempty"`
	BaseURL  *string            `json:"base_url,omitempty"`
	APIKey   *string            `json:"api_key,omitempty"`
	ProxyURL *string            `json:"proxy_url,omitempty"`
	Priority *int               `json:"priority,omitempty"`
	Disabled *bool              `json:"disabled,omitempty"`
	Headers  *map[string]string `json:"headers,omitempty"`
	Models   *json.RawMessage   `json:"models,omitempty"`
	Extra    *json.RawMessage   `json:"extra,omitempty"`
}

func (input providerUpdateRequest) toInput() tenantservice.ProviderUpdateInput {
	return tenantservice.ProviderUpdateInput{
		Name: input.Name, BaseURL: input.BaseURL, APIKey: input.APIKey, ProxyURL: input.ProxyURL,
		Priority: input.Priority, Disabled: input.Disabled, Headers: input.Headers, Models: input.Models, Extra: input.Extra,
	}
}

func providerID(c *gin.Context) (int64, bool) { return positiveID(c, "id") }

func (h *Handler) ListProviders(c *gin.Context) {
	tenantID, ok := tenantID(c)
	if !ok {
		return
	}
	providers, errList := h.tenants.ListProviders(c.Request.Context(), tenantID)
	if errList != nil {
		writeTenantError(c, errList)
		return
	}
	c.JSON(http.StatusOK, providers)
}

func (h *Handler) CreateProvider(c *gin.Context) {
	tenantID, ok := tenantID(c)
	if !ok {
		return
	}
	var request providerCreateRequest
	if errBind := c.ShouldBindJSON(&request); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	provider, errCreate := h.tenants.CreateProvider(c.Request.Context(), tenantID, request.toInput())
	if errCreate != nil {
		writeTenantError(c, errCreate)
		return
	}
	if errSync := h.syncTenantAuths(c.Request.Context()); errSync != nil {
		writeTenantError(c, errSync)
		return
	}
	provider, errGet := h.tenants.GetProvider(c.Request.Context(), tenantID, provider.ID)
	if errGet != nil {
		writeTenantError(c, errGet)
		return
	}
	c.JSON(http.StatusCreated, provider)
}

func (h *Handler) GetProvider(c *gin.Context) {
	tenantID, ok := tenantID(c)
	if !ok {
		return
	}
	id, valid := providerID(c)
	if !valid {
		return
	}
	provider, errGet := h.tenants.GetProvider(c.Request.Context(), tenantID, id)
	if errGet != nil {
		writeTenantError(c, errGet)
		return
	}
	c.JSON(http.StatusOK, provider)
}

func (h *Handler) UpdateProvider(c *gin.Context) {
	tenantID, ok := tenantID(c)
	if !ok {
		return
	}
	id, valid := providerID(c)
	if !valid {
		return
	}
	var request providerUpdateRequest
	if errBind := c.ShouldBindJSON(&request); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	provider, errUpdate := h.tenants.UpdateProvider(c.Request.Context(), tenantID, id, request.toInput())
	if errUpdate != nil {
		writeTenantError(c, errUpdate)
		return
	}
	if errSync := h.syncTenantAuths(c.Request.Context()); errSync != nil {
		writeTenantError(c, errSync)
		return
	}
	provider, errGet := h.tenants.GetProvider(c.Request.Context(), tenantID, provider.ID)
	if errGet != nil {
		writeTenantError(c, errGet)
		return
	}
	c.JSON(http.StatusOK, provider)
}

func (h *Handler) DeleteProvider(c *gin.Context) {
	tenantID, ok := tenantID(c)
	if !ok {
		return
	}
	id, valid := providerID(c)
	if !valid {
		return
	}
	if errDelete := h.tenants.DeleteProvider(c.Request.Context(), tenantID, id); errDelete != nil {
		writeTenantError(c, errDelete)
		return
	}
	if errSync := h.syncTenantAuths(c.Request.Context()); errSync != nil {
		writeTenantError(c, errSync)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) syncTenantAuths(ctx context.Context) error {
	if h == nil || h.syncAuths == nil {
		return errors.New("tenant provider runtime synchronization is unavailable")
	}
	return h.syncAuths(ctx)
}

type providerTestResponse struct {
	StatusCode int    `json:"status_code"`
	Body       string `json:"body"`
}

func (h *Handler) TestProvider(c *gin.Context) {
	tenantID, ok := tenantID(c)
	if !ok {
		return
	}
	id, valid := providerID(c)
	if !valid {
		return
	}
	provider, errConfig := h.tenants.ProviderTestConfig(c.Request.Context(), tenantID, id)
	if errConfig != nil {
		writeTenantError(c, errConfig)
		return
	}
	endpoint, errEndpoint := providerTestEndpoint(provider)
	if errEndpoint != nil {
		writeTenantError(c, errEndpoint)
		return
	}
	requestCtx, cancel := context.WithTimeout(c.Request.Context(), tenantTestTimeout)
	defer cancel()
	request, errRequest := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if errRequest != nil {
		writeTenantError(c, errRequest)
		return
	}
	for key, value := range provider.Headers {
		request.Header.Set(key, value)
	}
	setProviderTestAuthorization(request, provider)
	transport, errTransport := tenantProviderTransport(provider.ProxyURL)
	if errTransport != nil {
		writeTenantError(c, errTransport)
		return
	}
	response, errDo := (&http.Client{Timeout: tenantTestTimeout, Transport: transport}).Do(request)
	if errDo != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "provider test request failed"})
		return
	}
	defer response.Body.Close()
	body, errRead := io.ReadAll(io.LimitReader(response.Body, tenantTestBodyLimit))
	if errRead != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "provider test response failed"})
		return
	}
	c.JSON(http.StatusOK, providerTestResponse{StatusCode: response.StatusCode, Body: string(body)})
}

func providerTestEndpoint(provider tenantservice.ProviderTestConfig) (string, error) {
	baseURL := strings.TrimSpace(provider.BaseURL)
	if baseURL == "" {
		switch provider.Channel {
		case tenantservice.ChannelClaude:
			baseURL = "https://api.anthropic.com"
		case tenantservice.ChannelGemini:
			baseURL = "https://generativelanguage.googleapis.com"
		case tenantservice.ChannelXAI:
			baseURL = "https://api.x.ai"
		case tenantservice.ChannelVertex:
			baseURL = "https://aiplatform.googleapis.com"
		default:
			baseURL = "https://api.openai.com"
		}
	}
	baseURL = strings.TrimRight(baseURL, "/")
	parsed, errParse := url.Parse(baseURL)
	if errParse != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return "", errors.New("tenant provider base URL must be an absolute HTTP URL")
	}
	path := "/v1/models"
	if provider.Channel == tenantservice.ChannelGemini {
		path = "/v1beta/models"
	}
	return baseURL + path, nil
}

func setProviderTestAuthorization(request *http.Request, provider tenantservice.ProviderTestConfig) {
	if request == nil {
		return
	}
	switch provider.Channel {
	case tenantservice.ChannelClaude:
		request.Header.Set("x-api-key", provider.APIKey)
		if request.Header.Get("anthropic-version") == "" {
			request.Header.Set("anthropic-version", "2023-06-01")
		}
	case tenantservice.ChannelGemini:
		request.Header.Set("x-goog-api-key", provider.APIKey)
	default:
		request.Header.Set("Authorization", "Bearer "+provider.APIKey)
	}
}

func tenantProviderTransport(proxyURL string) (http.RoundTripper, error) {
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL != "" {
		transport, _, errBuild := proxyutil.BuildHTTPTransport(proxyURL)
		if errBuild != nil {
			return nil, errBuild
		}
		return transport, nil
	}
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok || transport == nil {
		return &http.Transport{Proxy: nil}, nil
	}
	clone := transport.Clone()
	clone.Proxy = nil
	return clone, nil
}

func (h *Handler) ListGroups(c *gin.Context) {
	tenantID, ok := tenantID(c)
	if !ok {
		return
	}
	clientAccess, ok := h.requireClientAccess(c)
	if !ok {
		return
	}
	page, errList := clientAccess.ListTenantGroups(c.Request.Context(), tenantID, clientListOptions(c))
	if errList != nil {
		writeTenantError(c, errList)
		return
	}
	c.JSON(http.StatusOK, page)
}

func (h *Handler) CreateGroup(c *gin.Context) {
	tenantID, ok := tenantID(c)
	if !ok {
		return
	}
	var input clientaccess.GroupCreate
	if errBind := c.ShouldBindJSON(&input); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	clientAccess, ok := h.requireClientAccess(c)
	if !ok {
		return
	}
	group, errCreate := clientAccess.CreateTenantGroup(c.Request.Context(), tenantID, input)
	if errCreate != nil {
		writeTenantError(c, errCreate)
		return
	}
	c.JSON(http.StatusCreated, group)
}

func (h *Handler) GetGroup(c *gin.Context) {
	tenantID, ok := tenantID(c)
	if !ok {
		return
	}
	id, valid := positiveID(c, "id")
	if !valid {
		return
	}
	clientAccess, ok := h.requireClientAccess(c)
	if !ok {
		return
	}
	group, errGet := clientAccess.GetTenantGroup(c.Request.Context(), tenantID, id)
	if errGet != nil {
		writeTenantError(c, errGet)
		return
	}
	c.JSON(http.StatusOK, group)
}

func (h *Handler) UpdateGroup(c *gin.Context) {
	tenantID, ok := tenantID(c)
	if !ok {
		return
	}
	id, valid := positiveID(c, "id")
	if !valid {
		return
	}
	var input clientaccess.GroupUpdate
	if errBind := c.ShouldBindJSON(&input); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	clientAccess, ok := h.requireClientAccess(c)
	if !ok {
		return
	}
	group, errUpdate := clientAccess.UpdateTenantGroup(c.Request.Context(), tenantID, id, input)
	if errUpdate != nil {
		writeTenantError(c, errUpdate)
		return
	}
	c.JSON(http.StatusOK, group)
}

func (h *Handler) DeleteGroup(c *gin.Context) {
	tenantID, ok := tenantID(c)
	if !ok {
		return
	}
	id, valid := positiveID(c, "id")
	if !valid {
		return
	}
	clientAccess, ok := h.requireClientAccess(c)
	if !ok {
		return
	}
	if errDelete := clientAccess.DeleteTenantGroup(c.Request.Context(), tenantID, id); errDelete != nil {
		writeTenantError(c, errDelete)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) ReplaceGroupCredentialBindings(c *gin.Context) {
	tenantID, ok := tenantID(c)
	if !ok {
		return
	}
	id, valid := positiveID(c, "id")
	if !valid {
		return
	}
	var input clientaccess.GroupCredentialBindingBatch
	if errBind := c.ShouldBindJSON(&input); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	clientAccess, ok := h.requireClientAccess(c)
	if !ok {
		return
	}
	stats, errReplace := clientAccess.ReplaceTenantGroupCredentialBindings(c.Request.Context(), tenantID, id, input, h.tenants)
	if errReplace != nil {
		writeTenantError(c, errReplace)
		return
	}
	c.JSON(http.StatusOK, stats)
}

func (h *Handler) ListCredentialBindings(c *gin.Context) {
	tenantID, ok := tenantID(c)
	if !ok {
		return
	}
	clientAccess, ok := h.requireClientAccess(c)
	if !ok {
		return
	}
	page, errList := clientAccess.ListTenantCredentialBindings(c.Request.Context(), tenantID, clientListOptions(c))
	if errList != nil {
		writeTenantError(c, errList)
		return
	}
	c.JSON(http.StatusOK, page)
}

func (h *Handler) ListKeys(c *gin.Context) {
	tenantID, ok := tenantID(c)
	if !ok {
		return
	}
	clientAccess, ok := h.requireClientAccess(c)
	if !ok {
		return
	}
	page, errList := clientAccess.ListTenantKeys(c.Request.Context(), tenantID, clientListOptions(c))
	if errList != nil {
		writeTenantError(c, errList)
		return
	}
	for index := range page.Items {
		page.Items[index].Secret = ""
	}
	c.JSON(http.StatusOK, page)
}

func (h *Handler) CreateKey(c *gin.Context) {
	tenantID, ok := tenantID(c)
	if !ok {
		return
	}
	var input clientaccess.KeyCreate
	if errBind := c.ShouldBindJSON(&input); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	clientAccess, ok := h.requireClientAccess(c)
	if !ok {
		return
	}
	key, errCreate := clientAccess.CreateTenantKey(c.Request.Context(), tenantID, input)
	if errCreate != nil {
		writeTenantError(c, errCreate)
		return
	}
	c.JSON(http.StatusCreated, key)
}

func (h *Handler) GetKey(c *gin.Context) {
	tenantID, ok := tenantID(c)
	if !ok {
		return
	}
	id, valid := positiveID(c, "id")
	if !valid {
		return
	}
	clientAccess, ok := h.requireClientAccess(c)
	if !ok {
		return
	}
	key, errGet := clientAccess.GetTenantKey(c.Request.Context(), tenantID, id)
	if errGet != nil {
		writeTenantError(c, errGet)
		return
	}
	key.Secret = ""
	c.JSON(http.StatusOK, key)
}

func (h *Handler) UpdateKey(c *gin.Context) {
	tenantID, ok := tenantID(c)
	if !ok {
		return
	}
	id, valid := positiveID(c, "id")
	if !valid {
		return
	}
	var input clientaccess.KeyUpdate
	if errBind := c.ShouldBindJSON(&input); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	clientAccess, ok := h.requireClientAccess(c)
	if !ok {
		return
	}
	key, errUpdate := clientAccess.UpdateTenantKey(c.Request.Context(), tenantID, id, input)
	if errUpdate != nil {
		writeTenantError(c, errUpdate)
		return
	}
	key.Secret = ""
	c.JSON(http.StatusOK, key)
}

func (h *Handler) DeleteKey(c *gin.Context) {
	tenantID, ok := tenantID(c)
	if !ok {
		return
	}
	id, valid := positiveID(c, "id")
	if !valid {
		return
	}
	clientAccess, ok := h.requireClientAccess(c)
	if !ok {
		return
	}
	if errDelete := clientAccess.DeleteTenantKey(c.Request.Context(), tenantID, id); errDelete != nil {
		writeTenantError(c, errDelete)
		return
	}
	c.Status(http.StatusNoContent)
}

func clientListOptions(c *gin.Context) clientaccess.ListOptions {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	options := clientaccess.ListOptions{Page: page, PageSize: pageSize, Search: strings.TrimSpace(c.Query("search"))}
	if rawEnabled := strings.TrimSpace(c.Query("enabled")); rawEnabled != "" {
		if enabled, errParse := strconv.ParseBool(rawEnabled); errParse == nil {
			options.Enabled = &enabled
		}
	}
	seenAuthIndices := make(map[string]struct{})
	for _, raw := range append(c.QueryArray("auth_index"), strings.Split(c.Query("auth_indices"), ",")...) {
		authIndex := strings.TrimSpace(raw)
		if authIndex == "" {
			continue
		}
		if _, exists := seenAuthIndices[authIndex]; exists {
			continue
		}
		seenAuthIndices[authIndex] = struct{}{}
		options.AuthIndices = append(options.AuthIndices, authIndex)
	}
	seenGroupIDs := make(map[int64]struct{})
	for _, raw := range append(c.QueryArray("group_id"), strings.Split(c.Query("group_ids"), ",")...) {
		groupID, errParse := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if errParse != nil || groupID <= 0 {
			continue
		}
		if _, exists := seenGroupIDs[groupID]; exists {
			continue
		}
		seenGroupIDs[groupID] = struct{}{}
		options.GroupIDs = append(options.GroupIDs, groupID)
	}
	return options
}

func (h *Handler) requireClientAccess(c *gin.Context) (*clientaccess.Service, bool) {
	if h == nil || h.clientAccess == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "client access is disabled"})
		return nil, false
	}
	return h.clientAccess, true
}

func positiveID(c *gin.Context, key string) (int64, bool) {
	id, errParse := strconv.ParseInt(strings.TrimSpace(c.Param(key)), 10, 64)
	if errParse != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return 0, false
	}
	return id, true
}

func stringValue(value any) string {
	stringValue, _ := value.(string)
	return strings.TrimSpace(stringValue)
}

func writeTenantError(c *gin.Context, err error) {
	if err == nil || c == nil {
		return
	}
	switch {
	case errors.Is(err, sql.ErrNoRows):
		c.JSON(http.StatusNotFound, gin.H{"error": "record not found"})
	case errors.Is(err, tenantservice.ErrInvalidCredentials), errors.Is(err, tenantservice.ErrInvalidSession):
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid tenant credentials"})
	case strings.Contains(strings.ToLower(err.Error()), "required"),
		strings.Contains(strings.ToLower(err.Error()), "must be"),
		strings.Contains(strings.ToLower(err.Error()), "invalid"),
		strings.Contains(strings.ToLower(err.Error()), "unsupported"):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case strings.Contains(strings.ToLower(err.Error()), "unique constraint"):
		c.JSON(http.StatusConflict, gin.H{"error": "record already exists"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "tenant request failed"})
	}
}

func (h *Handler) isLoginBlocked(clientIP string) bool {
	h.attemptsMu.Lock()
	defer h.attemptsMu.Unlock()
	attempt := h.attempts[clientIP]
	if attempt == nil {
		return false
	}
	now := time.Now().UTC()
	attempt.lastActivity = now
	if attempt.blockedUntil.After(now) {
		return true
	}
	if !attempt.blockedUntil.IsZero() {
		attempt.count = 0
		attempt.blockedUntil = time.Time{}
	}
	return false
}

func (h *Handler) recordLoginFailure(clientIP string) {
	h.attemptsMu.Lock()
	defer h.attemptsMu.Unlock()
	now := time.Now().UTC()
	attempt := h.attempts[clientIP]
	if attempt == nil {
		attempt = &failedLogin{}
		h.attempts[clientIP] = attempt
	}
	attempt.count++
	attempt.lastActivity = now
	if attempt.count >= tenantLoginMaxFailures {
		attempt.blockedUntil = now.Add(tenantLoginBlockFor)
	}
}

func (h *Handler) clearLoginFailures(clientIP string) {
	h.attemptsMu.Lock()
	delete(h.attempts, clientIP)
	h.attemptsMu.Unlock()
}
