package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	sdkhandlers "github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers/claude"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers/gemini"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers/openai"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

type tenantCatalogOwnershipResolver map[string]int64

func (r tenantCatalogOwnershipResolver) OwnerOf(authIndex string) int64 {
	return r[authIndex]
}

func (r tenantCatalogOwnershipResolver) HasOwnedCredentials() bool {
	return len(r) > 0
}

func TestTenantModelCatalogIgnoresHomeAndScopesEveryFormat(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const tenantClientID = "tenant-catalog-client"
	const tenantModelID = "tenant-catalog-visible"
	const foreignClientID = "global-catalog-client"
	const foreignModelID = "global-catalog-hidden"

	modelRegistry := registry.GetGlobalRegistry()
	modelRegistry.RegisterClient(tenantClientID, "openai", []*registry.ModelInfo{{
		ID: tenantModelID, Object: "model", OwnedBy: "tenant",
	}})
	modelRegistry.RegisterClient(foreignClientID, "openai", []*registry.ModelInfo{{
		ID: foreignModelID, Object: "model", OwnedBy: "global",
	}})
	t.Cleanup(func() {
		modelRegistry.UnregisterClient(tenantClientID)
		modelRegistry.UnregisterClient(foreignClientID)
	})

	manager := cliproxyauth.NewManager(nil, &cliproxyauth.RoundRobinSelector{}, nil)
	registered, errRegister := manager.Register(context.Background(), &cliproxyauth.Auth{
		ID:       tenantClientID,
		Index:    "tenant-catalog-index",
		Provider: "openai",
		Status:   cliproxyauth.StatusActive,
		Attributes: map[string]string{
			cliproxyauth.AttributeTenantID: "1",
		},
	})
	if errRegister != nil {
		t.Fatalf("register tenant auth: %v", errRegister)
	}
	manager.SetCredentialOwnershipResolver(tenantCatalogOwnershipResolver{registered.Index: 1})

	baseHandler := sdkhandlers.NewBaseAPIHandlers(nil, manager)
	openAIHandler := openai.NewOpenAIAPIHandler(baseHandler)
	claudeHandler := claude.NewClaudeCodeAPIHandler(baseHandler)
	geminiHandler := gemini.NewGeminiAPIHandler(baseHandler)
	server := &Server{cfg: &config.Config{Home: config.HomeConfig{Enabled: true}}}

	tenantContext := func(target string, header http.Header) (*httptest.ResponseRecorder, *gin.Context) {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodGet, target, nil)
		ctx.Request.Header = header
		ctx.Set("accessMetadata", map[string]string{
			cliproxyexecutor.ClientKeyIDMetadataKey:          "tenant-key",
			cliproxyexecutor.ClientTenantIDMetadataKey:       "1",
			cliproxyexecutor.ClientAllowAllGroupsMetadataKey: "true",
		})
		return recorder, ctx
	}
	assertScopedResponse := func(name string, recorder *httptest.ResponseRecorder) {
		t.Helper()
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s status = %d, body = %s", name, recorder.Code, recorder.Body.String())
		}
		body := recorder.Body.String()
		if !strings.Contains(body, tenantModelID) {
			t.Fatalf("%s missing tenant model: %s", name, body)
		}
		if strings.Contains(body, foreignModelID) {
			t.Fatalf("%s leaked foreign model: %s", name, body)
		}
	}

	modelsHandler := server.unifiedModelsHandler(openAIHandler, claudeHandler)
	recorder, ctx := tenantContext("/v1/models", make(http.Header))
	modelsHandler(ctx)
	assertScopedResponse("OpenAI", recorder)

	recorder, ctx = tenantContext("/v1/models?client_version=1", make(http.Header))
	modelsHandler(ctx)
	assertScopedResponse("Codex client", recorder)

	anthropicHeaders := make(http.Header)
	anthropicHeaders.Set("Anthropic-Version", "2023-06-01")
	recorder, ctx = tenantContext("/v1/models", anthropicHeaders)
	modelsHandler(ctx)
	assertScopedResponse("Claude", recorder)

	recorder, ctx = tenantContext("/v1beta/models", make(http.Header))
	server.geminiModelsHandler(geminiHandler)(ctx)
	assertScopedResponse("Gemini", recorder)
}
