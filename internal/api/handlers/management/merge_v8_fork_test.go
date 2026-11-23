package management

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/watcher/synthesizer"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func TestConfigAuthIndexFallbackMatchesV8Synthesis(t *testing.T) {
	pool := &config.ProxyConnectionPoolConfig{IdleConnTimeout: "1m"}
	cfg := &config.Config{
		GeminiKey:          []config.GeminiKey{{APIKey: "gemini", BaseURL: "https://gemini.example", Prefix: "team", ProxyURL: "http://proxy.example", Headers: map[string]string{"X-Test": "value"}, ConnectionPool: pool}},
		ClaudeKey:          []config.ClaudeKey{{APIKey: "claude", BaseURL: "https://claude.example", Prefix: "team", ConnectionPool: pool}},
		CodexKey:           []config.CodexKey{{APIKey: "codex", BaseURL: "https://codex.example", Prefix: "team", ConnectionPool: pool}},
		VertexCompatAPIKey: []config.VertexCompatKey{{APIKey: "vertex", BaseURL: "https://vertex.example", ProxyURL: "http://proxy.example", ConnectionPool: pool}},
		OpenAICompatibility: []config.OpenAICompatibility{
			{Name: "keyless", BaseURL: "https://keyless.example", Prefix: "team", ConnectionPool: pool},
			{Name: "custom", BaseURL: "https://custom.example", Prefix: "team", APIKeyEntries: []config.OpenAICompatibilityAPIKey{{APIKey: "custom", ProxyURL: "http://proxy.example", ConnectionPool: pool}}},
		},
	}
	auths, err := synthesizer.NewConfigSynthesizer().Synthesize(&synthesizer.SynthesisContext{Config: cfg, IDGenerator: synthesizer.NewStableIDGenerator()})
	if err != nil {
		t.Fatal(err)
	}
	manager := coreauth.NewManager(nil, nil, nil)
	want := make(map[string]bool)
	for _, auth := range auths {
		want[auth.EnsureIndex()] = true
		if _, err = manager.Register(context.Background(), auth); err != nil {
			t.Fatal(err)
		}
	}
	for _, live := range []map[string]string{nil, liveAuthIndexFromManager(manager)} {
		got := append(apiKeyConfigAuthIndices(cfg.GeminiKey, "gemini:apikey", live), apiKeyConfigAuthIndices(cfg.ClaudeKey, "claude:apikey", live)...)
		got = append(got, apiKeyConfigAuthIndices(cfg.CodexKey, "codex:apikey", live)...)
		got = append(got, vertexConfigAuthIndices(cfg.VertexCompatAPIKey, live)...)
		got = append(got, allOpenAICompatibilityAuthIndices(cfg.OpenAICompatibility, live)...)
		if len(got) != len(want) {
			t.Fatalf("indices = %v; want %d unique runtime indices", got, len(want))
		}
		for _, index := range got {
			if index == "" || !want[index] {
				t.Fatalf("index %q does not match runtime synthesis", index)
			}
		}
	}
}

func TestConfigV8DeleteRemovesClientAccessBindings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(`config-version: 8
api-keys:
  openai-compatibility:
    - name: keyless
      base-url: https://example.com/v1
      keys: []
`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	indices := allConfigAuthIndices(cfg, nil)
	if len(indices) != 1 || indices[0] == "" {
		t.Fatalf("indices = %v", indices)
	}
	service := newBoundClientAccessService(t, indices)
	h := &Handler{cfg: cfg, configFilePath: path, clientAccess: service}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodDelete, "/v8/management/config/api-keys/openai-compatibility", nil)
	c.Params = gin.Params{{Key: "path", Value: "/api-keys/openai-compatibility"}}
	h.ConfigV8(c)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d, body=%s", rec.Code, rec.Body.String())
	}
	assertNoClientAccessCredentialBindings(t, service)
}

func TestListAuthFilesV8HidesTenantRuntimeAuth(t *testing.T) {
	authDir := t.TempDir()
	manager := coreauth.NewManager(nil, nil, nil)
	registerTenantRuntimeAuth(t, manager, filepath.Join(authDir, "tenant.json"))
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: authDir}, manager)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Set(ConfigV8ContextKey, true)
	c.Request = httptest.NewRequest(http.MethodGet, "/v8/management/credentials?page=1&page_size=10", nil)
	h.ListAuthFiles(c)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "tenant-private") || strings.Contains(rec.Body.String(), "tenant.json") {
		t.Fatalf("tenant runtime auth exposed: status=%d body=%s", rec.Code, rec.Body.String())
	}
}
