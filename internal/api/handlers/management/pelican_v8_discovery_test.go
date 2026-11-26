package management

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

func getPelicanTarget(t *testing.T, router *gin.Engine, query url.Values) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/pelican?"+query.Encode(), nil))
	return response
}

func setupPelicanDiscovery(t *testing.T, cfg *config.Config) (*gin.Engine, *coreauth.Manager, []*coreauth.Auth) {
	t.Helper()
	manager := coreauth.NewManager(nil, nil, nil)
	auths := synthesizeConfigIdentityAuths(cfg)
	for _, auth := range auths {
		if _, err := manager.Register(coreauth.WithSkipPersist(t.Context()), auth); err != nil {
			t.Fatal(err)
		}
		registry.GetGlobalRegistry().RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{{ID: auth.ID + "-model"}})
		t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(auth.ID) })
	}
	handler := &Handler{cfg: cfg, authManager: manager}
	router := gin.New()
	router.GET("/pelican", handler.GetPelicanTargetV8)
	router.POST("/pelican", handler.PelicanTestV8)
	return router, manager, auths
}

func TestPelicanV8DiscoversExactNativeProviderIdentity(t *testing.T) {
	for _, kind := range []string{"gemini", "interactions", "codex", "claude", "vertex", "xai", "meta"} {
		t.Run(kind, func(t *testing.T) {
			var cfg config.Config
			gemini := []config.GeminiKey{
				{APIKey: "shared-key", BaseURL: "https://same.example", ProxyURL: "http://proxy-a"},
				{APIKey: "shared-key", BaseURL: "https://same.example", ProxyURL: "http://proxy-b"},
				{APIKey: "shared-key", BaseURL: "https://same.example", ProxyURL: "http://proxy-b"},
			}
			codex := []config.CodexKey{
				{APIKey: "shared-key", BaseURL: "https://same.example", ProxyURL: "http://proxy-a"},
				{APIKey: "shared-key", BaseURL: "https://same.example", ProxyURL: "http://proxy-b"},
				{APIKey: "shared-key", BaseURL: "https://same.example", ProxyURL: "http://proxy-b"},
			}
			switch kind {
			case "gemini":
				cfg.GeminiKey = gemini
			case "interactions":
				cfg.InteractionsKey = gemini
			case "codex":
				cfg.CodexKey = codex
			case "xai":
				cfg.XAIKey = codex
			case "meta":
				cfg.MetaKey = codex
			case "claude":
				for _, entry := range gemini {
					cfg.ClaudeKey = append(cfg.ClaudeKey, config.ClaudeKey{APIKey: entry.APIKey, BaseURL: entry.BaseURL, ProxyURL: entry.ProxyURL})
				}
			case "vertex":
				for _, entry := range gemini {
					cfg.VertexCompatAPIKey = append(cfg.VertexCompatAPIKey, config.VertexCompatKey{APIKey: entry.APIKey, BaseURL: entry.BaseURL, ProxyURL: entry.ProxyURL})
				}
			}
			router, _, auths := setupPelicanDiscovery(t, &cfg)
			if len(auths) != 3 || auths[0].ID == auths[1].ID || auths[1].ID == auths[2].ID {
				t.Fatalf("expected three distinct runtime identities: %+v", auths)
			}
			for i, auth := range auths {
				response := getPelicanTarget(t, router, url.Values{"provider_kind": {kind}, "config_index": {strconv.Itoa(i)}})
				body := ojson.Parse(response.Body.String())
				if response.Code != http.StatusOK || body.Get("auth_id").String() != auth.ID || body.Get("models.#").Int() != 1 || body.Get("models.0").String() != auth.ID+"-model" {
					t.Fatalf("wrong exact identity/models: index=%d status=%d body=%s", i, response.Code, response.Body.String())
				}
			}
		})
	}
}

func TestPelicanV8DiscoversExactOpenAIKeyAndKeylessIdentity(t *testing.T) {
	cfg := &config.Config{OpenAICompatibility: []config.OpenAICompatibility{
		{Name: "first", BaseURL: "https://same.example", APIKeyEntries: []config.OpenAICompatibilityAPIKey{{APIKey: "shared-key", ProxyURL: "http://proxy-a"}, {APIKey: "shared-key", ProxyURL: "http://proxy-b"}}},
		{Name: "second", BaseURL: "https://same.example", APIKeyEntries: []config.OpenAICompatibilityAPIKey{{APIKey: "shared-key", ProxyURL: "http://proxy-b"}}},
		{Name: "second", BaseURL: "https://same.example", APIKeyEntries: []config.OpenAICompatibilityAPIKey{{APIKey: "shared-key", ProxyURL: "http://proxy-b"}}},
		{Name: "keyless", BaseURL: "https://same.example"},
	}}
	router, _, auths := setupPelicanDiscovery(t, cfg)
	if len(auths) != 5 {
		t.Fatalf("auth count = %d, want 5", len(auths))
	}
	for i, selector := range []url.Values{
		{"provider_kind": {"openai"}, "config_index": {"0"}, "key_index": {"0"}},
		{"provider_kind": {"openai"}, "config_index": {"0"}, "key_index": {"1"}},
		{"provider_kind": {"openai"}, "config_index": {"1"}, "key_index": {"0"}},
		{"provider_kind": {"openai"}, "config_index": {"2"}, "key_index": {"0"}},
		{"provider_kind": {"openai"}, "config_index": {"3"}},
	} {
		response := getPelicanTarget(t, router, selector)
		body := ojson.Parse(response.Body.String())
		if response.Code != http.StatusOK || body.Get("auth_id").String() != auths[i].ID || body.Get("models.0").String() != auths[i].ID+"-model" || body.Get("models.#").Int() != 1 {
			t.Fatalf("wrong openai target: status=%d body=%s", response.Code, response.Body.String())
		}
	}
	for _, selector := range []url.Values{
		{"provider_kind": {"openai"}, "config_index": {"0"}},
		{"provider_kind": {"openai"}, "config_index": {"0"}, "key_index": {"2"}},
		{"provider_kind": {"openai"}, "config_index": {"3"}, "key_index": {"0"}},
		{"provider_kind": {"openai"}, "config_index": {"0"}, "auth_id": {auths[0].ID}},
	} {
		if response := getPelicanTarget(t, router, selector); response.Code != http.StatusBadRequest {
			t.Fatalf("invalid selector accepted: %v status=%d body=%s", selector, response.Code, response.Body.String())
		}
	}
}

func TestPelicanV8RejectsAmbiguousLegacyIdentity(t *testing.T) {
	manager := coreauth.NewManager(nil, nil, nil)
	executor := &pelicanTestExecutor{response: coreexecutor.Response{Payload: []byte(`{"choices":[{"message":{"content":"answer"}}]}`)}}
	manager.RegisterExecutor(executor)
	auths := make([]*coreauth.Auth, 0, 2)
	for _, id := range []string{t.Name() + "-a", t.Name() + "-b"} {
		auth, err := manager.Register(coreauth.WithSkipPersist(t.Context()), &coreauth.Auth{
			ID: id, FileName: "shared-name.json", Provider: executor.Identifier(), Status: coreauth.StatusActive,
			Attributes: map[string]string{coreauth.AttributeAuthKind: coreauth.AuthKindOAuth},
		})
		if err != nil {
			t.Fatal(err)
		}
		auths = append(auths, auth)
		registry.GetGlobalRegistry().RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{{ID: "pelican-model"}, {ID: id + "-only"}})
		t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(id) })
	}
	if auths[0].Index != auths[1].Index {
		t.Fatal("fixture must collide on auth_index")
	}
	handler := &Handler{authManager: manager}
	router := gin.New()
	router.GET("/pelican", handler.GetPelicanTargetV8)
	router.POST("/pelican", handler.PelicanTestV8)
	for _, selector := range []url.Values{
		{"auth_index": {auths[0].Index}},
		{"name": {"shared-name.json"}},
		{"name": {"shared-name.json"}, "auth_index": {auths[0].Index}},
	} {
		if response := getPelicanTarget(t, router, selector); response.Code != http.StatusConflict {
			t.Fatalf("ambiguous discovery accepted: status=%d body=%s", response.Code, response.Body.String())
		}
		response := callPelican(t, router, pelicanRequest{Name: selector.Get("name"), AuthIndex: selector.Get("auth_index"), Model: "pelican-model", Prompt: "draw"})
		if response.Code != http.StatusConflict || len(executor.calls) != 0 {
			t.Fatalf("ambiguous generation accepted: status=%d calls=%v", response.Code, executor.calls)
		}
	}
	for _, auth := range auths {
		response := getPelicanTarget(t, router, url.Values{"auth_id": {auth.ID}, "name": {auth.FileName}, "auth_index": {auth.Index}})
		body := ojson.Parse(response.Body.String())
		if response.Code != http.StatusOK || body.Get("auth_id").String() != auth.ID || body.Get("models.1").String() != auth.ID+"-only" {
			t.Fatalf("exact discovery failed: status=%d body=%s", response.Code, response.Body.String())
		}
		response = callPelican(t, router, pelicanRequest{AuthID: auth.ID, Name: auth.FileName, AuthIndex: auth.Index, Model: "pelican-model", Prompt: "draw"})
		if response.Code != http.StatusOK || executor.calls[len(executor.calls)-1] != auth.ID {
			t.Fatalf("exact generation failed: status=%d calls=%v body=%s", response.Code, executor.calls, response.Body.String())
		}
	}
}

func TestPelicanV8RejectsUnloadedConfigIdentity(t *testing.T) {
	configured := &config.Config{GeminiKey: []config.GeminiKey{{APIKey: "shared-key", BaseURL: "https://same.example", ProxyURL: "http://configured-proxy"}}}
	router, manager, _ := setupPelicanDiscovery(t, &config.Config{})
	stale := synthesizeConfigIdentityAuths(&config.Config{GeminiKey: []config.GeminiKey{{APIKey: "shared-key", BaseURL: "https://same.example", ProxyURL: "http://old-proxy"}}})[0]
	if _, err := manager.Register(coreauth.WithSkipPersist(t.Context()), stale); err != nil {
		t.Fatal(err)
	}
	handler := &Handler{cfg: configured, authManager: manager}
	router.GET("/configured", handler.GetPelicanTargetV8)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/configured?provider_kind=gemini&config_index=0", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("stale index-colliding auth was used: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestPelicanV8DiscoveryAndExecutionRejectUnavailableAndTenantTargets(t *testing.T) {
	for _, scenario := range []string{"disabled", "sign-in", "tenant"} {
		t.Run(scenario, func(t *testing.T) {
			manager := coreauth.NewManager(nil, nil, nil)
			executor := &pelicanTestExecutor{}
			manager.RegisterExecutor(executor)
			auth := &coreauth.Auth{ID: t.Name(), Provider: executor.Identifier(), Status: coreauth.StatusActive, Attributes: map[string]string{}}
			status := http.StatusConflict
			switch scenario {
			case "disabled":
				auth.Disabled, auth.Status = true, coreauth.StatusDisabled
			case "sign-in":
				auth.RequiresSignIn = true
			case "tenant":
				auth.Attributes[coreauth.AttributeTenantID] = "7"
				auth.Attributes["runtime_only"] = "true"
				status = http.StatusNotFound
			}
			if _, err := manager.Register(coreauth.WithSkipPersist(t.Context()), auth); err != nil {
				t.Fatal(err)
			}
			handler := &Handler{authManager: manager}
			router := gin.New()
			router.GET("/pelican", handler.GetPelicanTargetV8)
			router.POST("/pelican", handler.PelicanTestV8)
			response := getPelicanTarget(t, router, url.Values{"auth_id": {auth.ID}})
			if response.Code != status {
				t.Fatalf("discovery status=%d want=%d body=%s", response.Code, status, response.Body.String())
			}
			response = callPelican(t, router, pelicanRequest{AuthID: auth.ID, Model: "pelican-model", Prompt: "draw"})
			if response.Code != status || len(executor.calls) != 0 {
				t.Fatalf("execution status=%d want=%d calls=%v body=%s", response.Code, status, executor.calls, response.Body.String())
			}
		})
	}
}
