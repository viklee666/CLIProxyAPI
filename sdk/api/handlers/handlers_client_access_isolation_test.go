package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

func TestCallerScopePreservesAuthenticatedPrincipalAfterHomeRewrite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set("userApiKey", "shared-home-usage-key")
	c.Set("accessPrincipal", "caller-a")
	first := requestCallerScope(c)
	c.Set("accessPrincipal", "caller-b")
	second := requestCallerScope(c)
	if first == "" || second == "" || first == second {
		t.Fatal("a shared Home usage key merged different authenticated caller scopes")
	}
}

func TestScopedClientCannotBypassCredentialSelectionWithDirectPlugin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tenantID := range []string{"", "1"} {
		ginContext, _ := gin.CreateTestContext(httptest.NewRecorder())
		ginContext.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		metadata := map[string]string{
			coreexecutor.ClientKeyIDMetadataKey:          "1",
			coreexecutor.ClientGroupIDsMetadataKey:       "1",
			coreexecutor.ClientAllowAllGroupsMetadataKey: "false",
		}
		if tenantID != "" {
			metadata[coreexecutor.ClientTenantIDMetadataKey] = tenantID
			metadata[coreexecutor.ClientAllowAllGroupsMetadataKey] = "true"
		}
		ginContext.Set("accessMetadata", metadata)
		ctx := context.WithValue(context.Background(), "gin", ginContext)
		host := &handlerDirectExecutorRouteHost{}
		h := NewBaseAPIHandlers(&sdkconfig.SDKConfig{}, nil)
		h.ModelRouterHost = host
		_, _, errExecute := h.executeWithPluginExecutor(ctx, "openai", "openai", "test", "test", nil, "", "global-plugin", modelExecutionOptions{})
		if errExecute == nil || errExecute.StatusCode != http.StatusForbidden {
			t.Errorf("tenant=%q direct execution error=%v, want 403", tenantID, errExecute)
		}
		_, _, errCount := h.countWithPluginExecutor(ctx, "openai", "test", "test", nil, "", "global-plugin", modelExecutionOptions{})
		if errCount == nil || errCount.StatusCode != http.StatusForbidden {
			t.Errorf("tenant=%q count error=%v, want 403", tenantID, errCount)
		}
		_, _, errors := h.streamWithPluginExecutor(ctx, "openai", "openai", "test", "test", nil, "", "global-plugin", modelExecutionOptions{})
		if errors == nil {
			t.Fatal("missing stream rejection")
		}
		if errStream := <-errors; errStream == nil || errStream.StatusCode != http.StatusForbidden {
			t.Errorf("tenant=%q stream error=%v, want 403", tenantID, errStream)
		}
		if host.lastPluginID != "" {
			t.Fatal("a restricted request reached the direct plugin executor")
		}
	}
}

type wideningCatalogInterceptor struct {
	handlerModelRouterTestHost
}

func (*wideningCatalogInterceptor) InterceptResponse(context.Context, pluginapi.ResponseInterceptRequest) pluginapi.ResponseInterceptResponse {
	return pluginapi.ResponseInterceptResponse{Body: []byte(`{"data":[{"id":"forbidden-model"}]}`)}
}

func TestScopedCatalogCannotBeWidenedByGlobalInterceptor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	c.Set("accessMetadata", map[string]string{coreexecutor.ClientKeyIDMetadataKey: "1"})
	h := NewBaseAPIHandlers(&sdkconfig.SDKConfig{}, nil)
	h.PluginHost = &wideningCatalogInterceptor{}
	h.WriteModelListResponse(c, "openai", gin.H{"data": []gin.H{{"id": "permitted-model"}}})
	if body := recorder.Body.String(); strings.Contains(body, "forbidden-model") || !strings.Contains(body, "permitted-model") {
		t.Fatalf("global interceptor bypassed scoped model listing: %s", body)
	}
}

func TestScopedCatalogFailsClosedWithoutAuthManager(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ginContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	ginContext.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	ginContext.Set("accessMetadata", map[string]string{coreexecutor.ClientKeyIDMetadataKey: "1"})
	h := NewBaseAPIHandlers(&sdkconfig.SDKConfig{}, nil)
	if !h.IsModelCatalogScoped(ginContext) {
		t.Fatal("a missing auth manager disabled catalog scoping")
	}
	if models := h.ModelsForRequest(ginContext, "openai"); len(models) != 0 {
		t.Fatalf("missing manager returned global models: %v", models)
	}
	if infos := h.ModelInfosForRequest(ginContext); len(infos) != 0 {
		t.Fatalf("missing manager returned global model metadata: %v", infos)
	}
}
