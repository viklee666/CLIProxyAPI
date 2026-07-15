package management

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/watcher/synthesizer"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestPostGeminiKeyAppendsOneRecord(t *testing.T) {
	h := &Handler{
		cfg: &config.Config{GeminiKey: []config.GeminiKey{{
			APIKey:  "existing",
			BaseURL: "https://existing.example",
		}}},
		configFilePath: writeTestConfigFile(t),
	}

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v0/management/gemini-api-key", strings.NewReader(`{
		"api-key":"new-key",
		"base-url":"https://new.example",
		"priority":7,
		"disable-cooling":true
	}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	h.PostGeminiKey(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if len(h.cfg.GeminiKey) != 2 {
		t.Fatalf("len(GeminiKey) = %d, want 2", len(h.cfg.GeminiKey))
	}
	if h.cfg.GeminiKey[0].APIKey != "existing" {
		t.Fatalf("existing record changed: %#v", h.cfg.GeminiKey[0])
	}
	created := h.cfg.GeminiKey[1]
	if created.APIKey != "new-key" || created.Priority != 7 || !created.DisableCooling {
		t.Fatalf("created record = %#v", created)
	}
}

func TestPatchCodexKeyMatchesAuthIndexAndUpdatesAllExecutionFields(t *testing.T) {
	const (
		apiKey    = "codex-key"
		baseURL   = "https://codex.example/v1"
		authIndex = "auth-index-1"
	)
	idGen := synthesizer.NewStableIDGenerator()
	authID, _ := idGen.Next("codex:apikey", apiKey, baseURL)
	manager := coreauth.NewManager(nil, nil, nil)
	if _, err := manager.Register(context.Background(), &coreauth.Auth{
		ID:       authID,
		Index:    authIndex,
		Provider: "codex",
	}); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	h := &Handler{
		cfg: &config.Config{CodexKey: []config.CodexKey{{
			APIKey:         apiKey,
			BaseURL:        baseURL,
			Priority:       1,
			Websockets:     true,
			DisableCooling: false,
			Headers:        map[string]string{"X-Old": "keep-until-replaced"},
		}}},
		configFilePath: writeTestConfigFile(t),
		authManager:    manager,
	}

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPatch, "/v0/management/codex-api-key", strings.NewReader(`{
		"match":"",
		"match-auth-index":"auth-index-1",
		"value":{
			"priority":9,
			"prefix":"team-a",
			"websockets":false,
			"disable-cooling":true,
			"headers":{},
			"models":[],
			"excluded-models":[]
		}
	}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	h.PatchCodexKey(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	entry := h.cfg.CodexKey[0]
	if entry.Priority != 9 || entry.Prefix != "team-a" || entry.Websockets || !entry.DisableCooling {
		t.Fatalf("patched record = %#v", entry)
	}
	if len(entry.Headers) != 0 || len(entry.Models) != 0 || len(entry.ExcludedModels) != 0 {
		t.Fatalf("clearable fields were not replaced: %#v", entry)
	}
}

func TestGetOpenAICompatIncludesFalseDisableCooling(t *testing.T) {
	h := NewHandlerWithoutConfigFilePath(&config.Config{
		OpenAICompatibility: []config.OpenAICompatibility{{
			Name:           "provider",
			BaseURL:        "https://provider.example/v1",
			DisableCooling: false,
		}},
	}, nil)

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/openai-compatibility", nil)
	h.GetOpenAICompat(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"disable-cooling":false`) {
		t.Fatalf("response omits false disable-cooling: %s", rec.Body.String())
	}
}
