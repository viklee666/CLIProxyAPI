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
		"name":"  Primary route  ",
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
	if created.Name != "Primary route" || created.APIKey != "new-key" || created.Priority != 7 || !created.DisableCooling {
		t.Fatalf("created record = %#v", created)
	}
}

func TestPatchProviderRemarkNames(t *testing.T) {
	tests := []struct {
		name       string
		newHandler func(string) *Handler
		patch      func(*Handler, *gin.Context)
		getName    func(*Handler) string
	}{
		{
			name: "gemini",
			newHandler: func(path string) *Handler {
				return &Handler{cfg: &config.Config{GeminiKey: []config.GeminiKey{{APIKey: "key"}}}, configFilePath: path}
			},
			patch:   (*Handler).PatchGeminiKey,
			getName: func(h *Handler) string { return h.cfg.GeminiKey[0].Name },
		},
		{
			name: "interactions",
			newHandler: func(path string) *Handler {
				return &Handler{cfg: &config.Config{InteractionsKey: []config.GeminiKey{{APIKey: "key"}}}, configFilePath: path}
			},
			patch:   (*Handler).PatchInteractionsKey,
			getName: func(h *Handler) string { return h.cfg.InteractionsKey[0].Name },
		},
		{
			name: "codex",
			newHandler: func(path string) *Handler {
				return &Handler{cfg: &config.Config{CodexKey: []config.CodexKey{{APIKey: "key", BaseURL: "https://codex.example/v1"}}}, configFilePath: path}
			},
			patch:   (*Handler).PatchCodexKey,
			getName: func(h *Handler) string { return h.cfg.CodexKey[0].Name },
		},
		{
			name: "claude",
			newHandler: func(path string) *Handler {
				return &Handler{cfg: &config.Config{ClaudeKey: []config.ClaudeKey{{APIKey: "key", BaseURL: "https://claude.example/v1"}}}, configFilePath: path}
			},
			patch:   (*Handler).PatchClaudeKey,
			getName: func(h *Handler) string { return h.cfg.ClaudeKey[0].Name },
		},
		{
			name: "vertex",
			newHandler: func(path string) *Handler {
				return &Handler{cfg: &config.Config{VertexCompatAPIKey: []config.VertexCompatKey{{APIKey: "key", BaseURL: "https://vertex.example/v1"}}}, configFilePath: path}
			},
			patch:   (*Handler).PatchVertexCompatKey,
			getName: func(h *Handler) string { return h.cfg.VertexCompatAPIKey[0].Name },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := tt.newHandler(writeTestConfigFile(t))
			patchName := func(name string) *httptest.ResponseRecorder {
				rec := httptest.NewRecorder()
				ctx, _ := gin.CreateTestContext(rec)
				ctx.Request = httptest.NewRequest(http.MethodPatch, "/v0/management/provider", strings.NewReader(`{"index":0,"value":{"name":"`+name+`"}}`))
				ctx.Request.Header.Set("Content-Type", "application/json")
				tt.patch(h, ctx)
				return rec
			}

			if rec := patchName("  Primary route  "); rec.Code != http.StatusOK {
				t.Fatalf("set status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
			}
			if got := tt.getName(h); got != "Primary route" {
				t.Fatalf("name = %q, want Primary route", got)
			}

			if rec := patchName("   "); rec.Code != http.StatusOK {
				t.Fatalf("clear status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
			}
			if got := tt.getName(h); got != "" {
				t.Fatalf("cleared name = %q, want empty", got)
			}
		})
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
			"name":"Display only",
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
	if entry.Name != "Display only" || entry.Priority != 9 || entry.Prefix != "team-a" || entry.Websockets || !entry.DisableCooling {
		t.Fatalf("patched record = %#v", entry)
	}
	if len(entry.Headers) != 0 || len(entry.Models) != 0 || len(entry.ExcludedModels) != 0 {
		t.Fatalf("clearable fields were not replaced: %#v", entry)
	}
	projected := h.codexKeysWithAuthIndex()
	if len(projected) != 1 || projected[0].AuthIndex != authIndex {
		t.Fatalf("auth index changed after remark update: %#v", projected)
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
