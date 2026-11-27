package management

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	runtimeexecutor "github.com/router-for-me/CLIProxyAPI/v7/internal/runtime/executor"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestPelicanV8UsesRealExecutorAndFinalPayloadRules(t *testing.T) {
	const model = "pelican-http-model"
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body, errRead := io.ReadAll(r.Body)
		if errRead != nil {
			t.Error(errRead)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		root := ojson.ParseBytes(body)
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer provider-only-key" || root.Get("messages.0.content").String() != "configured prompt" || root.Get("reasoning_effort").Exists() || root.Get("store").Exists() || root.Get("model").String() != model {
			t.Errorf("wrong credential or final payload rules were overwritten: path=%s payload=%s", r.URL.Path, body)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"pelican-http","model":"pelican-http-model","choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"<html><svg></svg></html>"}}]}`))
	}))
	t.Cleanup(upstream.Close)
	cfg := &config.Config{Payload: config.PayloadConfig{
		Override: []config.PayloadRule{{Models: []config.PayloadModelRule{{Name: model}}, Params: map[string]any{"messages.0.content": "configured prompt"}}},
		Filter:   []config.PayloadFilterRule{{Models: []config.PayloadModelRule{{Name: model}}, Params: []string{"reasoning_effort", "store"}}},
	}}
	manager := coreauth.NewManager(nil, &coreauth.RoundRobinSelector{}, nil)
	manager.SetConfig(cfg)
	manager.RegisterExecutor(runtimeexecutor.NewOpenAICompatExecutor("pelican-http", cfg))
	target, errRegister := manager.Register(coreauth.WithSkipPersist(t.Context()), &coreauth.Auth{
		ID: t.Name(), Provider: "pelican-http", Status: coreauth.StatusActive,
		Attributes: map[string]string{coreauth.AttributeAuthKind: coreauth.AuthKindAPIKey, "base_url": upstream.URL + "/v1", "api_key": "provider-only-key"},
	})
	if errRegister != nil {
		t.Fatal(errRegister)
	}
	registry.GetGlobalRegistry().RegisterClient(target.ID, target.Provider, []*registry.ModelInfo{{ID: model}})
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(target.ID) })
	h := &Handler{authManager: manager}
	router := gin.New()
	router.POST("/pelican", h.PelicanTestV8)
	response := callPelican(t, router, pelicanRequest{AuthIndex: target.Index, Model: model, Prompt: "original prompt", Effort: "high"})
	if response.Code != http.StatusOK || calls.Load() != 1 || ojson.Get(response.Body.String(), "answer").String() != "<html><svg></svg></html>" {
		t.Fatalf("generation failed: status=%d calls=%d body=%s", response.Code, calls.Load(), response.Body.String())
	}
}
