package management

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestPutConfigYAMLRejectsOversizedBody(t *testing.T) {
	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	ginContext.Request = httptest.NewRequest(
		http.MethodPut,
		"/v0/management/config.yaml",
		strings.NewReader(strings.Repeat("x", maxConfigYAMLBytes+1)),
	)

	(&Handler{}).PutConfigYAML(ginContext)

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d body=%s", recorder.Code, http.StatusRequestEntityTooLarge, recorder.Body.String())
	}
}

func TestGetConfigUIProjectionBoundsLargeCollections(t *testing.T) {
	cfg := &config.Config{Debug: true}
	for i := 0; i < maxConfigUICollectionEntries+5; i++ {
		cfg.APIKeys = append(cfg.APIKeys, "client-key-"+strings.Repeat("x", 8))
		cfg.GeminiKey = append(cfg.GeminiKey, config.GeminiKey{
			APIKey:         "gemini-key",
			Prefix:         "team",
			BaseURL:        "https://gemini.example/v1",
			Models:         []config.GeminiModel{{Name: "large-model", Alias: "alias"}},
			Headers:        map[string]string{"Authorization": "secret"},
			ExcludedModels: []string{"disabled-model"},
		})
	}
	cfg.Plugins.Configs = map[string]config.PluginInstanceConfig{"secret-plugin": {}}

	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	ginContext.Request = httptest.NewRequest(http.MethodGet, "/v0/management/config?view=ui", nil)
	(&Handler{cfg: cfg}).GetConfig(ginContext)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if got := recorder.Header().Get("X-Config-View"); got != "ui" {
		t.Fatalf("X-Config-View = %q, want ui", got)
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["debug"] != true || body["projection"] != "ui" {
		t.Fatalf("unexpected projection metadata: %#v", body)
	}
	apiKeys, _ := body["api-keys"].([]any)
	gemini, _ := body["gemini-api-key"].([]any)
	if len(apiKeys) != maxConfigUICollectionEntries || len(gemini) != maxConfigUICollectionEntries {
		t.Fatalf("projected lengths api_keys=%d gemini=%d, want %d", len(apiKeys), len(gemini), maxConfigUICollectionEntries)
	}
	firstGemini, _ := gemini[0].(map[string]any)
	if _, exists := firstGemini["models"]; exists {
		t.Fatalf("UI identity projection leaked models: %#v", firstGemini)
	}
	if _, exists := firstGemini["headers"]; exists {
		t.Fatalf("UI identity projection leaked headers: %#v", firstGemini)
	}
	plugins, _ := body["plugins"].(map[string]any)
	if _, exists := plugins["configs"]; exists {
		t.Fatalf("UI projection leaked plugin configs: %#v", plugins)
	}
	totals, _ := body["collection_totals"].(map[string]any)
	if totals["api-keys"] != float64(maxConfigUICollectionEntries+5) || totals["gemini-api-key"] != float64(maxConfigUICollectionEntries+5) {
		t.Fatalf("unexpected collection totals: %#v", totals)
	}
}

func TestGetConfigFullViewRemainsCompatible(t *testing.T) {
	cfg := &config.Config{SDKConfig: config.SDKConfig{APIKeys: []string{"key-1", "key-2"}}}
	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	ginContext.Request = httptest.NewRequest(http.MethodGet, "/v0/management/config", nil)
	(&Handler{cfg: cfg}).GetConfig(ginContext)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var body struct {
		APIKeys []string `json:"api-keys"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.APIKeys) != 2 || body.APIKeys[1] != "key-2" {
		t.Fatalf("full response api keys = %#v", body.APIKeys)
	}
}
