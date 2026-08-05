package tenant

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/clientaccess"
	tenantservice "github.com/router-for-me/CLIProxyAPI/v7/internal/tenant"
)

func newTenantTestEngine(t *testing.T) (*gin.Engine, *tenantservice.Service, *clientaccess.Service) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	tenants, errTenants := tenantservice.New(filepath.Join(dir, "tenant.sqlite"))
	if errTenants != nil {
		t.Fatalf("tenant.New() error = %v", errTenants)
	}
	t.Cleanup(func() { _ = tenants.Close() })
	clientAccess, errClientAccess := clientaccess.New(filepath.Join(dir, "client-access.sqlite"))
	if errClientAccess != nil {
		t.Fatalf("clientaccess.New() error = %v", errClientAccess)
	}
	t.Cleanup(func() { _ = clientAccess.Close() })
	handler := NewHandler(tenants, clientAccess, func(ctx context.Context) error {
		_, errSynthesize := tenants.SynthesizeAuths(ctx)
		return errSynthesize
	})
	engine := gin.New()
	handler.Register(engine)
	return engine, tenants, clientAccess
}

func tenantRequest(t *testing.T, engine http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		encoded, errMarshal := json.Marshal(body)
		if errMarshal != nil {
			t.Fatalf("marshal request body: %v", errMarshal)
		}
		reader = bytes.NewReader(encoded)
	}
	request := httptest.NewRequest(method, path, reader)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	return response
}

func loginTenant(t *testing.T, engine http.Handler, password string) string {
	t.Helper()
	response := tenantRequest(t, engine, http.MethodPost, "/v0/tenant/login", "", map[string]string{"password": password})
	if response.Code != http.StatusOK {
		t.Fatalf("tenant login status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload struct {
		Token string `json:"token"`
	}
	if errDecode := json.Unmarshal(response.Body.Bytes(), &payload); errDecode != nil {
		t.Fatalf("decode login: %v", errDecode)
	}
	if payload.Token == "" {
		t.Fatal("login returned empty token")
	}
	return payload.Token
}

func TestTenantAPIUsesSessionScopeAndSanitizesQuota(t *testing.T) {
	engine, tenants, _ := newTenantTestEngine(t)
	ctx := context.Background()
	first, _, errFirst := tenants.Create(ctx, tenantservice.CreateInput{DisplayName: "first", Password: "first-password"})
	if errFirst != nil {
		t.Fatalf("Create(first) error = %v", errFirst)
	}
	second, _, errSecond := tenants.Create(ctx, tenantservice.CreateInput{DisplayName: "second", Password: "second-password"})
	if errSecond != nil {
		t.Fatalf("Create(second) error = %v", errSecond)
	}
	firstToken := loginTenant(t, engine, "first-password")
	secondToken := loginTenant(t, engine, "second-password")

	firstGroupResponse := tenantRequest(t, engine, http.MethodPost, "/v0/tenant/groups", firstToken, map[string]any{
		"name":      "first-group",
		"tenant_id": second.ID,
	})
	if firstGroupResponse.Code != http.StatusCreated {
		t.Fatalf("create tenant group status = %d, body = %s", firstGroupResponse.Code, firstGroupResponse.Body.String())
	}
	var firstGroup struct {
		ID       int64 `json:"id"`
		TenantID int64 `json:"tenant_id"`
	}
	if errDecode := json.Unmarshal(firstGroupResponse.Body.Bytes(), &firstGroup); errDecode != nil {
		t.Fatalf("decode first group: %v", errDecode)
	}
	if firstGroup.TenantID != first.ID {
		t.Fatalf("forged tenant_id was accepted: group tenant = %d, want %d", firstGroup.TenantID, first.ID)
	}
	secondGroupResponse := tenantRequest(t, engine, http.MethodPost, "/v0/tenant/groups", secondToken, map[string]any{"name": "second-group"})
	if secondGroupResponse.Code != http.StatusCreated {
		t.Fatalf("create second tenant group status = %d, body = %s", secondGroupResponse.Code, secondGroupResponse.Body.String())
	}
	var secondGroup struct {
		ID int64 `json:"id"`
	}
	if errDecode := json.Unmarshal(secondGroupResponse.Body.Bytes(), &secondGroup); errDecode != nil {
		t.Fatalf("decode second group: %v", errDecode)
	}

	crossGroup := tenantRequest(t, engine, http.MethodGet, "/v0/tenant/groups/"+jsonNumber(secondGroup.ID), firstToken, nil)
	if crossGroup.Code != http.StatusNotFound {
		t.Fatalf("cross tenant group status = %d, body = %s", crossGroup.Code, crossGroup.Body.String())
	}

	keyResponse := tenantRequest(t, engine, http.MethodPost, "/v0/tenant/keys", firstToken, map[string]any{
		"name":                "first-key",
		"group_ids":           []int64{firstGroup.ID},
		"concurrency_limit":   3,
		"rpm_limit":           99,
		"request_limit_total": 88,
		"token_limit_total":   77,
		"tenant_id":           second.ID,
	})
	if keyResponse.Code != http.StatusCreated {
		t.Fatalf("create tenant key status = %d, body = %s", keyResponse.Code, keyResponse.Body.String())
	}
	var key struct {
		ID                int64  `json:"id"`
		TenantID          int64  `json:"tenant_id"`
		Secret            string `json:"secret"`
		ConcurrencyLimit  int    `json:"concurrency_limit"`
		RPMLimit          int    `json:"rpm_limit"`
		RequestLimitTotal int64  `json:"request_limit_total"`
		TokenLimitTotal   int64  `json:"token_limit_total"`
	}
	if errDecode := json.Unmarshal(keyResponse.Body.Bytes(), &key); errDecode != nil {
		t.Fatalf("decode tenant key: %v", errDecode)
	}
	if key.TenantID != first.ID || key.Secret == "" || key.ConcurrencyLimit != 3 || key.RPMLimit != 0 || key.RequestLimitTotal != 0 || key.TokenLimitTotal != 0 {
		t.Fatalf("tenant key response = %+v", key)
	}
	getKey := tenantRequest(t, engine, http.MethodGet, "/v0/tenant/keys/"+jsonNumber(key.ID), firstToken, nil)
	if getKey.Code != http.StatusOK || bytes.Contains(getKey.Body.Bytes(), []byte(key.Secret)) {
		t.Fatalf("get tenant key leaked secret or failed: status=%d body=%s", getKey.Code, getKey.Body.String())
	}
	crossKey := tenantRequest(t, engine, http.MethodGet, "/v0/tenant/keys/"+jsonNumber(key.ID), secondToken, nil)
	if crossKey.Code != http.StatusNotFound {
		t.Fatalf("cross tenant key status = %d, body = %s", crossKey.Code, crossKey.Body.String())
	}
}

func TestTenantProviderAPIScopesSecretsAndTestTarget(t *testing.T) {
	engine, tenants, _ := newTenantTestEngine(t)
	ctx := context.Background()
	_, _, errFirst := tenants.Create(ctx, tenantservice.CreateInput{DisplayName: "provider-first", Password: "provider-first-password"})
	if errFirst != nil {
		t.Fatalf("Create(first) error = %v", errFirst)
	}
	_, _, errSecond := tenants.Create(ctx, tenantservice.CreateInput{DisplayName: "provider-second", Password: "provider-second-password"})
	if errSecond != nil {
		t.Fatalf("Create(second) error = %v", errSecond)
	}
	firstToken := loginTenant(t, engine, "provider-first-password")
	secondToken := loginTenant(t, engine, "provider-second-password")

	hits := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		hits++
		if request.URL.Path != "/v1/models" {
			t.Errorf("provider test path = %q", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer provider-secret" {
			t.Errorf("provider test authorization = %q", request.Header.Get("Authorization"))
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	providerResponse := tenantRequest(t, engine, http.MethodPost, "/v0/tenant/providers", firstToken, map[string]any{
		"channel":  "openai-compat",
		"name":     "private-provider",
		"base_url": upstream.URL,
		"api_key":  "provider-secret",
	})
	if providerResponse.Code != http.StatusCreated {
		t.Fatalf("create provider status = %d, body = %s", providerResponse.Code, providerResponse.Body.String())
	}
	if bytes.Contains(providerResponse.Body.Bytes(), []byte("provider-secret")) {
		t.Fatalf("provider response leaked API key: %s", providerResponse.Body.String())
	}
	var provider struct {
		ID        int64  `json:"id"`
		AuthIndex string `json:"auth_index"`
	}
	if errDecode := json.Unmarshal(providerResponse.Body.Bytes(), &provider); errDecode != nil {
		t.Fatalf("decode provider: %v", errDecode)
	}
	if provider.ID == 0 || provider.AuthIndex == "" {
		t.Fatalf("provider was not synthesized: %+v", provider)
	}
	crossProvider := tenantRequest(t, engine, http.MethodGet, "/v0/tenant/providers/"+jsonNumber(provider.ID), secondToken, nil)
	if crossProvider.Code != http.StatusNotFound {
		t.Fatalf("cross tenant provider status = %d, body = %s", crossProvider.Code, crossProvider.Body.String())
	}
	testResponse := tenantRequest(t, engine, http.MethodPost, "/v0/tenant/providers/"+jsonNumber(provider.ID)+"/test", firstToken, map[string]string{"url": "http://ignored.invalid"})
	if testResponse.Code != http.StatusOK || hits != 1 {
		t.Fatalf("provider test status=%d hits=%d body=%s", testResponse.Code, hits, testResponse.Body.String())
	}
}

func TestTenantProviderModelDiscoveryUsesStoredProviderAndScopesTenant(t *testing.T) {
	engine, tenants, _ := newTenantTestEngine(t)
	ctx := context.Background()
	_, _, errFirst := tenants.Create(ctx, tenantservice.CreateInput{DisplayName: "discovery-first", Password: "discovery-first-password"})
	if errFirst != nil {
		t.Fatalf("Create(first) error = %v", errFirst)
	}
	_, _, errSecond := tenants.Create(ctx, tenantservice.CreateInput{DisplayName: "discovery-second", Password: "discovery-second-password"})
	if errSecond != nil {
		t.Fatalf("Create(second) error = %v", errSecond)
	}
	firstToken := loginTenant(t, engine, "discovery-first-password")
	secondToken := loginTenant(t, engine, "discovery-second-password")

	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/v1/models":
			if got := request.Header.Get("Authorization"); got != "Bearer discovery-secret" {
				t.Errorf("OpenAI discovery authorization = %q", got)
			}
			_, _ = writer.Write([]byte(`{"data":[{"id":"gpt-private","display_name":"Private GPT","description":"tenant model"}]}`))
		case "/v1beta/models":
			if got := request.Header.Get("x-goog-api-key"); got != "gemini-discovery-secret" {
				t.Errorf("Gemini discovery authorization = %q", got)
			}
			if request.URL.Query().Get("pageToken") == "second" {
				_, _ = writer.Write([]byte(`{"models":[{"name":"models/gemini-second"}]}`))
				return
			}
			_, _ = writer.Write([]byte(`{"models":[{"name":"models/gemini-first","displayName":"Gemini First"}],"nextPageToken":"second"}`))
		default:
			t.Errorf("unexpected discovery path = %q", request.URL.Path)
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer upstream.Close()

	createProvider := func(channel, name, apiKey, baseURL string) int64 {
		response := tenantRequest(t, engine, http.MethodPost, "/v0/tenant/providers", firstToken, map[string]any{
			"channel":  channel,
			"name":     name,
			"base_url": baseURL,
			"api_key":  apiKey,
		})
		if response.Code != http.StatusCreated {
			t.Fatalf("create %s provider status = %d, body = %s", channel, response.Code, response.Body.String())
		}
		var provider struct {
			ID int64 `json:"id"`
		}
		if errDecode := json.Unmarshal(response.Body.Bytes(), &provider); errDecode != nil {
			t.Fatalf("decode %s provider: %v", channel, errDecode)
		}
		return provider.ID
	}

	openAIProviderID := createProvider(tenantservice.ChannelOpenAICompat, "openai-discovery", "discovery-secret", upstream.URL+"/v1")
	openAIModels := tenantRequest(t, engine, http.MethodPost, "/v0/tenant/providers/"+jsonNumber(openAIProviderID)+"/models", firstToken, nil)
	if openAIModels.Code != http.StatusOK {
		t.Fatalf("OpenAI discovery status = %d, body = %s", openAIModels.Code, openAIModels.Body.String())
	}
	if bytes.Contains(openAIModels.Body.Bytes(), []byte("discovery-secret")) {
		t.Fatalf("OpenAI discovery leaked API key: %s", openAIModels.Body.String())
	}
	var openAIPayload providerModelsResponse
	if errDecode := json.Unmarshal(openAIModels.Body.Bytes(), &openAIPayload); errDecode != nil {
		t.Fatalf("decode OpenAI discovery response: %v", errDecode)
	}
	if len(openAIPayload.Models) != 1 || openAIPayload.Models[0] != (providerDiscoveredModel{Name: "gpt-private", Alias: "Private GPT", Description: "tenant model"}) {
		t.Fatalf("OpenAI discovery models = %+v", openAIPayload.Models)
	}

	crossTenant := tenantRequest(t, engine, http.MethodPost, "/v0/tenant/providers/"+jsonNumber(openAIProviderID)+"/models", secondToken, nil)
	if crossTenant.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant discovery status = %d, body = %s", crossTenant.Code, crossTenant.Body.String())
	}

	geminiProviderID := createProvider(tenantservice.ChannelGemini, "gemini-discovery", "gemini-discovery-secret", upstream.URL+"/v1beta")
	geminiModels := tenantRequest(t, engine, http.MethodPost, "/v0/tenant/providers/"+jsonNumber(geminiProviderID)+"/models", firstToken, nil)
	if geminiModels.Code != http.StatusOK {
		t.Fatalf("Gemini discovery status = %d, body = %s", geminiModels.Code, geminiModels.Body.String())
	}
	var geminiPayload providerModelsResponse
	if errDecode := json.Unmarshal(geminiModels.Body.Bytes(), &geminiPayload); errDecode != nil {
		t.Fatalf("decode Gemini discovery response: %v", errDecode)
	}
	if len(geminiPayload.Models) != 2 || geminiPayload.Models[0].Name != "gemini-first" || geminiPayload.Models[0].Alias != "Gemini First" || geminiPayload.Models[1].Name != "gemini-second" {
		t.Fatalf("Gemini discovery models = %+v", geminiPayload.Models)
	}
}

func TestTenantLogoutInvalidatesOnlyPresentedSession(t *testing.T) {
	engine, tenants, _ := newTenantTestEngine(t)
	_, _, errCreate := tenants.Create(context.Background(), tenantservice.CreateInput{DisplayName: "logout", Password: "logout-password"})
	if errCreate != nil {
		t.Fatalf("Create() error = %v", errCreate)
	}
	token := loginTenant(t, engine, "logout-password")
	logout := tenantRequest(t, engine, http.MethodPost, "/v0/tenant/logout", token, nil)
	if logout.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, body = %s", logout.Code, logout.Body.String())
	}
	me := tenantRequest(t, engine, http.MethodGet, "/v0/tenant/me", token, nil)
	if me.Code != http.StatusUnauthorized {
		t.Fatalf("logged-out token status = %d, body = %s", me.Code, me.Body.String())
	}
}

func TestTenantCredentialBindingsHonorGroupFilter(t *testing.T) {
	engine, tenants, _ := newTenantTestEngine(t)
	ctx := context.Background()
	if _, _, errCreate := tenants.Create(ctx, tenantservice.CreateInput{
		DisplayName: "binding-filter",
		Password:    "binding-filter-password",
	}); errCreate != nil {
		t.Fatalf("Create() error = %v", errCreate)
	}
	token := loginTenant(t, engine, "binding-filter-password")

	createProvider := func(name string) int64 {
		response := tenantRequest(t, engine, http.MethodPost, "/v0/tenant/providers", token, map[string]any{
			"channel": "openai-compat",
			"name":    name,
			"api_key": name + "-secret",
		})
		if response.Code != http.StatusCreated {
			t.Fatalf("create provider %s status = %d, body = %s", name, response.Code, response.Body.String())
		}
		var provider struct {
			ID        int64  `json:"id"`
			AuthIndex string `json:"auth_index"`
		}
		if errDecode := json.Unmarshal(response.Body.Bytes(), &provider); errDecode != nil {
			t.Fatalf("decode provider %s: %v", name, errDecode)
		}
		if provider.ID == 0 || provider.AuthIndex == "" {
			t.Fatalf("provider %s was not created: %+v", name, provider)
		}
		return provider.ID
	}

	providerOne := createProvider("provider-one")
	providerTwo := createProvider("provider-two")
	providers := tenantRequest(t, engine, http.MethodGet, "/v0/tenant/providers", token, nil)
	if providers.Code != http.StatusOK {
		t.Fatalf("list providers status = %d, body = %s", providers.Code, providers.Body.String())
	}
	var providerRows []struct {
		ID        int64  `json:"id"`
		AuthIndex string `json:"auth_index"`
	}
	if errDecode := json.Unmarshal(providers.Body.Bytes(), &providerRows); errDecode != nil {
		t.Fatalf("decode providers: %v", errDecode)
	}
	authIndices := make(map[int64]string, len(providerRows))
	for _, provider := range providerRows {
		authIndices[provider.ID] = provider.AuthIndex
	}

	createGroup := func(name string) int64 {
		response := tenantRequest(t, engine, http.MethodPost, "/v0/tenant/groups", token, map[string]string{"name": name})
		if response.Code != http.StatusCreated {
			t.Fatalf("create group %s status = %d, body = %s", name, response.Code, response.Body.String())
		}
		var group struct {
			ID int64 `json:"id"`
		}
		if errDecode := json.Unmarshal(response.Body.Bytes(), &group); errDecode != nil {
			t.Fatalf("decode group %s: %v", name, errDecode)
		}
		return group.ID
	}

	firstGroup := createGroup("first-group")
	secondGroup := createGroup("second-group")
	for _, binding := range []struct {
		groupID   int64
		authIndex string
	}{
		{groupID: firstGroup, authIndex: authIndices[providerOne]},
		{groupID: secondGroup, authIndex: authIndices[providerTwo]},
	} {
		response := tenantRequest(
			t,
			engine,
			http.MethodPut,
			"/v0/tenant/groups/"+jsonNumber(binding.groupID)+"/credential-bindings",
			token,
			map[string]any{"auth_indices": []string{binding.authIndex}, "priority": 0},
		)
		if response.Code != http.StatusOK {
			t.Fatalf("replace bindings for group %d status = %d, body = %s", binding.groupID, response.Code, response.Body.String())
		}
	}

	response := tenantRequest(
		t,
		engine,
		http.MethodGet,
		"/v0/tenant/credential-bindings?group_ids="+jsonNumber(firstGroup),
		token,
		nil,
	)
	if response.Code != http.StatusOK {
		t.Fatalf("list filtered bindings status = %d, body = %s", response.Code, response.Body.String())
	}
	var page struct {
		Items []struct {
			GroupID   int64  `json:"group_id"`
			AuthIndex string `json:"auth_index"`
		} `json:"items"`
	}
	if errDecode := json.Unmarshal(response.Body.Bytes(), &page); errDecode != nil {
		t.Fatalf("decode filtered bindings: %v", errDecode)
	}
	if len(page.Items) != 1 || page.Items[0].GroupID != firstGroup || page.Items[0].AuthIndex != authIndices[providerOne] {
		t.Fatalf("filtered bindings = %+v, want group=%d auth_index=%q", page.Items, firstGroup, authIndices[providerOne])
	}
}

func TestTenantClientAccessDisabledReturnsServiceUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	tenants, errTenants := tenantservice.New(filepath.Join(dir, "tenant.sqlite"))
	if errTenants != nil {
		t.Fatalf("tenant.New() error = %v", errTenants)
	}
	t.Cleanup(func() { _ = tenants.Close() })
	if _, _, errCreate := tenants.Create(context.Background(), tenantservice.CreateInput{
		DisplayName: "disabled-client-access",
		Password:    "disabled-client-access-password",
	}); errCreate != nil {
		t.Fatalf("Create() error = %v", errCreate)
	}

	engine := gin.New()
	NewHandler(tenants, nil, nil).Register(engine)
	token := loginTenant(t, engine, "disabled-client-access-password")
	for _, path := range []string{"/v0/tenant/groups", "/v0/tenant/credential-bindings", "/v0/tenant/keys"} {
		response := tenantRequest(t, engine, http.MethodGet, path, token, nil)
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s status = %d, want %d body=%s", path, response.Code, http.StatusServiceUnavailable, response.Body.String())
		}
	}
}

func jsonNumber(value int64) string {
	return strconv.FormatInt(value, 10)
}
