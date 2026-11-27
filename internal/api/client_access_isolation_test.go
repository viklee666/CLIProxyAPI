package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/clientaccess"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	sdkaccess "github.com/router-for-me/CLIProxyAPI/v7/sdk/access"
	sdkhandlers "github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers/claude"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers/gemini"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers/openai"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

type isolatedCatalogExecutor struct {
	calls int
}

func (*isolatedCatalogExecutor) Identifier() string { return "isolation-test" }
func (e *isolatedCatalogExecutor) Execute(context.Context, *cliproxyauth.Auth, coreexecutor.Request, coreexecutor.Options) (coreexecutor.Response, error) {
	e.calls++
	return coreexecutor.Response{Payload: []byte(`{"id":"test","choices":[]}`)}, nil
}
func (e *isolatedCatalogExecutor) ExecuteStream(context.Context, *cliproxyauth.Auth, coreexecutor.Request, coreexecutor.Options) (*coreexecutor.StreamResult, error) {
	e.calls++
	chunks := make(chan coreexecutor.StreamChunk)
	close(chunks)
	return &coreexecutor.StreamResult{Chunks: chunks}, nil
}
func (*isolatedCatalogExecutor) Refresh(_ context.Context, a *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
	return a, nil
}
func (e *isolatedCatalogExecutor) CountTokens(ctx context.Context, a *cliproxyauth.Auth, req coreexecutor.Request, opts coreexecutor.Options) (coreexecutor.Response, error) {
	return e.Execute(ctx, a, req, opts)
}
func (*isolatedCatalogExecutor) HttpRequest(context.Context, *cliproxyauth.Auth, *http.Request) (*http.Response, error) {
	return nil, nil
}

type denyCatalogGroupAccess struct{}

func (denyCatalogGroupAccess) ResolveCredentialAccess(string, []int64, bool, bool) (bool, int, bool) {
	return false, 0, false
}

func TestCodexAlphaSearchCannotBypassClientAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tenantID := range []string{"", "42", "legacy"} {
		manager := cliproxyauth.NewManager(nil, &cliproxyauth.RoundRobinSelector{}, nil)
		manager.SetCredentialGroupResolver(denyCatalogGroupAccess{})
		executor := &codexSearchCaptureExecutor{responseBody: io.NopCloser(strings.NewReader(`{"data":[]}`))}
		manager.RegisterExecutor(executor)
		candidate, errRegister := manager.Register(context.Background(), &cliproxyauth.Auth{ID: "search-isolation", Index: "search-isolation-index", Provider: "codex", Status: cliproxyauth.StatusActive, Metadata: map[string]any{"access_token": "synthetic-token"}})
		if errRegister != nil {
			t.Fatal(errRegister)
		}
		registry.GetGlobalRegistry().RegisterClient(candidate.ID, "codex", []*registry.ModelInfo{{ID: "gpt-search-isolation"}})
		base := sdkhandlers.NewBaseAPIHandlers(&config.SDKConfig{}, manager)
		server := &Server{cfg: &config.Config{}, handlers: base}
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/alpha/search", strings.NewReader(`{"model":"gpt-search-isolation","query":"test"}`))
		metadata := map[string]string{
			coreexecutor.ClientKeyIDMetadataKey:          "1",
			coreexecutor.ClientGroupIDsMetadataKey:       "1",
			coreexecutor.ClientAllowAllGroupsMetadataKey: "false",
		}
		if tenantID != "" && tenantID != "legacy" {
			metadata[coreexecutor.ClientTenantIDMetadataKey] = tenantID
			metadata[coreexecutor.ClientAllowAllGroupsMetadataKey] = "true"
		}
		if tenantID != "legacy" {
			c.Set("accessMetadata", metadata)
		}
		server.codexAlphaSearch(c)
		registry.GetGlobalRegistry().UnregisterClient(candidate.ID)
		if tenantID == "legacy" {
			if recorder.Code != http.StatusOK || executor.httpCalls != 1 {
				t.Errorf("legacy search fixture failed: status=%d calls=%d body=%s", recorder.Code, executor.httpCalls, recorder.Body.String())
			}
		} else if recorder.Code < http.StatusBadRequest || executor.httpCalls != 0 {
			t.Errorf("tenant=%q search bypassed restrictions: status=%d upstreamCalls=%d body=%s", tenantID, recorder.Code, executor.httpCalls, recorder.Body.String())
		}
	}
}

func TestClientKeyCatalogAndExecutionStayWithinAssignedGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	service, errNew := clientaccess.New(filepath.Join(t.TempDir(), "client-access.sqlite"))
	if errNew != nil {
		t.Fatal(errNew)
	}
	t.Cleanup(func() {
		if errClose := service.Close(); errClose != nil {
			t.Error(errClose)
		}
	})
	group, errGroup := service.CreateGroup(ctx, clientaccess.GroupCreate{Name: "allowed"})
	if errGroup != nil {
		t.Fatal(errGroup)
	}
	otherGroup, errOther := service.CreateGroup(ctx, clientaccess.GroupCreate{Name: "other"})
	if errOther != nil {
		t.Fatal(errOther)
	}
	allowAll := false
	key, errKey := service.CreateKey(ctx, clientaccess.KeyCreate{Name: "restricted", AllowAllGroups: &allowAll, GroupIDs: []int64{group.ID}})
	if errKey != nil {
		t.Fatal(errKey)
	}
	manager := cliproxyauth.NewManager(nil, &cliproxyauth.RoundRobinSelector{}, nil)
	manager.SetCredentialGroupResolver(service)
	executor := &isolatedCatalogExecutor{}
	manager.RegisterExecutor(executor)
	for _, entry := range []struct {
		id      string
		model   string
		groupID int64
	}{
		{id: "isolation-allowed", model: "isolation-visible-model", groupID: group.ID},
		{id: "isolation-denied", model: "isolation-hidden-model", groupID: otherGroup.ID},
	} {
		registered, errRegister := manager.Register(ctx, &cliproxyauth.Auth{ID: entry.id, Index: entry.id, Provider: executor.Identifier(), Status: cliproxyauth.StatusActive})
		if errRegister != nil {
			t.Fatal(errRegister)
		}
		registry.GetGlobalRegistry().RegisterClient(registered.ID, executor.Identifier(), []*registry.ModelInfo{{ID: entry.model, Object: "model", OwnedBy: "test"}})
		t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(registered.ID) })
		if errBind := service.ReplaceCredentialBindings(ctx, clientaccess.CredentialBindingBatch{AuthIndices: []string{registered.Index}, Groups: []clientaccess.CredentialGroupInput{{GroupID: entry.groupID}}}); errBind != nil {
			t.Fatal(errBind)
		}
	}
	accessManager := sdkaccess.NewManager()
	accessManager.SetProviders([]sdkaccess.Provider{service})
	base := sdkhandlers.NewBaseAPIHandlers(&config.SDKConfig{}, manager)
	openAI := openai.NewOpenAIAPIHandler(base)
	claudeAPI := claude.NewClaudeCodeAPIHandler(base)
	geminiAPI := gemini.NewGeminiAPIHandler(base)
	server := &Server{cfg: &config.Config{Home: config.HomeConfig{Enabled: true}}}
	engine := gin.New()
	engine.Use(AuthMiddleware(accessManager))
	engine.GET("/v1/models", server.unifiedModelsHandler(openAI, claudeAPI))
	engine.GET("/v1beta/models", server.geminiModelsHandler(geminiAPI))
	engine.GET("/v1beta/models/*action", server.geminiGetHandler(geminiAPI))
	engine.POST("/v1/chat/completions", openAI.ChatCompletions)
	for _, entry := range []struct {
		path   string
		header string
		value  string
		home   bool
	}{
		{path: "/v1/models"},
		{path: "/v1/models?client_version=1"},
		{path: "/v1/models", header: "Anthropic-Version", value: "2023-06-01"},
		{path: "/v1/models", header: "User-Agent", value: "grok-shell/0.2.119 (macos; aarch64)"},
		{path: "/v1beta/models"},
		{path: "/v1/models", home: true},
		{path: "/v1/models?client_version=1", home: true},
		{path: "/v1/models", header: "Anthropic-Version", value: "2023-06-01", home: true},
		{path: "/v1/models", header: "User-Agent", value: "grok-shell/0.2.119 (macos; aarch64)", home: true},
		{path: "/v1beta/models", home: true},
	} {
		t.Run(entry.path+entry.header+"/home="+strconv.FormatBool(entry.home), func(t *testing.T) {
			server.cfg.Home.Enabled = entry.home
			request := httptest.NewRequest(http.MethodGet, entry.path, nil)
			request.Header.Set("Authorization", "Bearer "+key.Secret)
			if entry.header != "" {
				request.Header.Set(entry.header, entry.value)
			}
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, request)
			body := recorder.Body.String()
			if recorder.Code != http.StatusOK || !strings.Contains(body, "isolation-visible-model") || strings.Contains(body, "isolation-hidden-model") {
				t.Errorf("unscoped model catalog: status=%d body=%s", recorder.Code, body)
			}
		})
	}
	for _, model := range []string{"isolation-visible-model", "isolation-hidden-model"} {
		request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"`+model+`","messages":[{"role":"user","content":"test"}]}`))
		request.Header.Set("Authorization", "Bearer "+key.Secret)
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		before := executor.calls
		engine.ServeHTTP(recorder, request)
		if model == "isolation-visible-model" {
			if recorder.Code != http.StatusOK || executor.calls != before+1 {
				t.Errorf("allowed resource failed: status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		} else if recorder.Code < http.StatusBadRequest || executor.calls != before {
			t.Errorf("forbidden resource executed: status=%d calls=%d body=%s", recorder.Code, executor.calls-before, recorder.Body.String())
		}
	}
	request := httptest.NewRequest(http.MethodGet, "/v1beta/models/isolation-hidden-model", nil)
	request.Header.Set("Authorization", "Bearer "+key.Secret)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Errorf("model detail leaked: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
