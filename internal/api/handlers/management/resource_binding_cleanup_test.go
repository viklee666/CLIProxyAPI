package management

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/clientaccess"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/watcher/synthesizer"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

type testProviderAuth struct {
	id    string
	index string
}

func newBoundClientAccessService(t *testing.T, authIndices []string) *clientaccess.Service {
	t.Helper()
	service, errNew := clientaccess.New(filepath.Join(t.TempDir(), "client-access.sqlite"))
	if errNew != nil {
		t.Fatalf("clientaccess.New() error = %v", errNew)
	}
	t.Cleanup(func() {
		if errClose := service.Close(); errClose != nil {
			t.Fatalf("client access Close() error = %v", errClose)
		}
	})
	group, errGroup := service.CreateGroup(context.Background(), clientaccess.GroupCreate{Name: "main"})
	if errGroup != nil {
		t.Fatalf("CreateGroup() error = %v", errGroup)
	}
	if errReplace := service.ReplaceCredentialBindings(context.Background(), clientaccess.CredentialBindingBatch{
		AuthIndices: authIndices,
		Groups:      []clientaccess.CredentialGroupInput{{GroupID: group.ID, Priority: 10}},
	}); errReplace != nil {
		t.Fatalf("ReplaceCredentialBindings() error = %v", errReplace)
	}
	return service
}

func assertNoClientAccessCredentialBindings(t *testing.T, service *clientaccess.Service) {
	t.Helper()
	page, errList := service.ListCredentialBindings(context.Background(), clientaccess.ListOptions{Page: 1, PageSize: 20})
	if errList != nil {
		t.Fatalf("ListCredentialBindings() error = %v", errList)
	}
	if page.Total != 0 || len(page.Items) != 0 {
		t.Fatalf("credential bindings after resource delete = %+v", page)
	}
	groups, errGroups := service.ListGroups(context.Background(), clientaccess.ListOptions{Page: 1, PageSize: 20})
	if errGroups != nil {
		t.Fatalf("ListGroups() error = %v", errGroups)
	}
	if len(groups.Items) != 1 || groups.Items[0].CredentialCount != 0 {
		t.Fatalf("groups after resource delete = %+v", groups)
	}
}

func registerTestProviderAuths(t *testing.T, manager *coreauth.Manager, auths []testProviderAuth) []string {
	t.Helper()
	indices := make([]string, 0, len(auths))
	for _, item := range auths {
		if _, errRegister := manager.Register(context.Background(), &coreauth.Auth{
			ID:       item.id,
			Index:    item.index,
			Provider: "test",
			Status:   coreauth.StatusActive,
		}); errRegister != nil {
			t.Fatalf("Register(%s) error = %v", item.id, errRegister)
		}
		indices = append(indices, item.index)
	}
	return indices
}

func TestDeleteAuthFileRemovesClientAccessCredentialBindings(t *testing.T) {
	authDir := t.TempDir()
	fileName := "assigned.json"
	filePath := filepath.Join(authDir, fileName)
	if errWrite := os.WriteFile(filePath, []byte(`{"type":"codex"}`), 0o600); errWrite != nil {
		t.Fatalf("write auth file: %v", errWrite)
	}

	manager := coreauth.NewManager(nil, nil, nil)
	authIndex := (&coreauth.Auth{
		Provider: "codex",
		Attributes: map[string]string{
			"path": filePath,
		},
		Metadata: map[string]any{"type": "codex"},
	}).EnsureIndex()
	service := newBoundClientAccessService(t, []string{authIndex})
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: authDir}, manager)
	h.tokenStore = &memoryAuthStore{}
	h.SetClientAccessService(service)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodDelete, "/v0/management/auth-files?name="+url.QueryEscape(fileName), nil)
	h.DeleteAuthFile(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	assertNoClientAccessCredentialBindings(t, service)
}

func TestDeleteAllAuthFilesRemovesClientAccessCredentialBindings(t *testing.T) {
	authDir := t.TempDir()
	manager := coreauth.NewManager(nil, nil, nil)
	authIndices := []string{"auth-alpha", "auth-beta"}
	for i, fileName := range []string{"alpha.json", "beta.json"} {
		filePath := filepath.Join(authDir, fileName)
		if errWrite := os.WriteFile(filePath, []byte(`{"type":"codex"}`), 0o600); errWrite != nil {
			t.Fatalf("write auth file: %v", errWrite)
		}
		if _, errRegister := manager.Register(context.Background(), &coreauth.Auth{
			ID:       fileName,
			Index:    authIndices[i],
			FileName: fileName,
			Provider: "codex",
			Status:   coreauth.StatusActive,
			Attributes: map[string]string{
				"path": filePath,
			},
		}); errRegister != nil {
			t.Fatalf("Register(%s) error = %v", fileName, errRegister)
		}
	}
	service := newBoundClientAccessService(t, authIndices)
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: authDir}, manager)
	h.tokenStore = &memoryAuthStore{}
	h.SetClientAccessService(service)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodDelete, "/v0/management/auth-files?all=true", nil)
	h.DeleteAuthFile(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	assertNoClientAccessCredentialBindings(t, service)
}

func TestDeleteAIProviderRemovesBindingsWithoutRuntimeAuth(t *testing.T) {
	const (
		apiKey  = "gemini-key"
		baseURL = "https://gemini.example.com"
	)
	id, _ := synthesizer.NewStableIDGenerator().Next("gemini:apikey", apiKey, baseURL)
	authIndex := (&coreauth.Auth{
		ID:       id,
		Provider: "gemini",
		Attributes: map[string]string{
			"api_key":  apiKey,
			"base_url": baseURL,
		},
	}).EnsureIndex()
	service := newBoundClientAccessService(t, []string{authIndex})
	cfg := &config.Config{GeminiKey: []config.GeminiKey{{APIKey: apiKey, BaseURL: baseURL}}}
	h := NewHandler(cfg, writeTestConfigFile(t), coreauth.NewManager(nil, nil, nil))
	h.SetClientAccessService(service)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodDelete, "/v0/management/gemini-api-key?index=0", nil)
	h.DeleteGeminiKey(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	assertNoClientAccessCredentialBindings(t, service)
}

func TestDeleteAIProviderRemovesClientAccessCredentialBindings(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*config.Config) []testProviderAuth
		target    string
		remove    func(*Handler, *gin.Context)
	}{
		{
			name: "gemini",
			configure: func(cfg *config.Config) []testProviderAuth {
				cfg.GeminiKey = []config.GeminiKey{{APIKey: "gemini-key", BaseURL: "https://gemini.example.com"}}
				id, _ := synthesizer.NewStableIDGenerator().Next("gemini:apikey", "gemini-key", "https://gemini.example.com")
				return []testProviderAuth{{id: id, index: "auth-gemini"}}
			},
			target: "/v0/management/gemini-api-key?index=0",
			remove: (*Handler).DeleteGeminiKey,
		},
		{
			name: "interactions",
			configure: func(cfg *config.Config) []testProviderAuth {
				cfg.InteractionsKey = []config.GeminiKey{{APIKey: "interactions-key", BaseURL: "https://interactions.example.com"}}
				id, _ := synthesizer.NewStableIDGenerator().Next("gemini-interactions:apikey", "interactions-key", "https://interactions.example.com")
				return []testProviderAuth{{id: id, index: "auth-interactions"}}
			},
			target: "/v0/management/interactions-api-key?index=0",
			remove: (*Handler).DeleteInteractionsKey,
		},
		{
			name: "claude",
			configure: func(cfg *config.Config) []testProviderAuth {
				cfg.ClaudeKey = []config.ClaudeKey{{APIKey: "claude-key", BaseURL: "https://claude.example.com"}}
				id, _ := synthesizer.NewStableIDGenerator().Next("claude:apikey", "claude-key", "https://claude.example.com")
				return []testProviderAuth{{id: id, index: "auth-claude"}}
			},
			target: "/v0/management/claude-api-key?index=0",
			remove: (*Handler).DeleteClaudeKey,
		},
		{
			name: "codex",
			configure: func(cfg *config.Config) []testProviderAuth {
				cfg.CodexKey = []config.CodexKey{{APIKey: "codex-key", BaseURL: "https://codex.example.com"}}
				id, _ := synthesizer.NewStableIDGenerator().Next("codex:apikey", "codex-key", "https://codex.example.com")
				return []testProviderAuth{{id: id, index: "auth-codex"}}
			},
			target: "/v0/management/codex-api-key?index=0",
			remove: (*Handler).DeleteCodexKey,
		},
		{
			name: "xai",
			configure: func(cfg *config.Config) []testProviderAuth {
				cfg.XAIKey = []config.XAIKey{{APIKey: "xai-key", BaseURL: "https://xai.example.com"}}
				id, _ := synthesizer.NewStableIDGenerator().Next("xai:apikey", "xai-key", "https://xai.example.com")
				return []testProviderAuth{{id: id, index: "auth-xai"}}
			},
			target: "/v0/management/xai-api-key?index=0",
			remove: (*Handler).DeleteXAIKey,
		},
		{
			name: "vertex",
			configure: func(cfg *config.Config) []testProviderAuth {
				cfg.VertexCompatAPIKey = []config.VertexCompatKey{{APIKey: "vertex-key", BaseURL: "https://vertex.example.com", ProxyURL: "https://proxy.example.com"}}
				id, _ := synthesizer.NewStableIDGenerator().Next("vertex:apikey", "vertex-key", "https://vertex.example.com", "https://proxy.example.com")
				return []testProviderAuth{{id: id, index: "auth-vertex"}}
			},
			target: "/v0/management/vertex-api-key?index=0",
			remove: (*Handler).DeleteVertexCompatKey,
		},
		{
			name: "openai compatibility with multiple keys",
			configure: func(cfg *config.Config) []testProviderAuth {
				cfg.OpenAICompatibility = []config.OpenAICompatibility{{
					Name:    "Custom",
					BaseURL: "https://openai.example.com",
					APIKeyEntries: []config.OpenAICompatibilityAPIKey{
						{APIKey: "openai-key-a"},
						{APIKey: "openai-key-b", ProxyURL: "https://proxy.example.com"},
					},
				}}
				idGen := synthesizer.NewStableIDGenerator()
				idA, _ := idGen.Next("openai-compatibility:custom", "openai-key-a", "https://openai.example.com", "")
				idB, _ := idGen.Next("openai-compatibility:custom", "openai-key-b", "https://openai.example.com", "https://proxy.example.com")
				return []testProviderAuth{{id: idA, index: "auth-openai-a"}, {id: idB, index: "auth-openai-b"}}
			},
			target: "/v0/management/openai-compatibility?name=Custom",
			remove: (*Handler).DeleteOpenAICompat,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			manager := coreauth.NewManager(nil, nil, nil)
			authIndices := registerTestProviderAuths(t, manager, tt.configure(cfg))
			service := newBoundClientAccessService(t, authIndices)
			h := NewHandler(cfg, writeTestConfigFile(t), manager)
			h.SetClientAccessService(service)

			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodDelete, tt.target, nil)
			tt.remove(h, c)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
			}
			assertNoClientAccessCredentialBindings(t, service)
		})
	}
}
