package test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	runtimeexecutor "github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/watcher/synthesizer"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/api/handlers"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/api/handlers/openai"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

const singleAttemptModel = "single-attempt-model"

func newSingleAttemptProviderHandler(t *testing.T, provider, upstreamURL string) *openai.OpenAIAPIHandler {
	t.Helper()
	var raw strings.Builder
	raw.WriteString("request-retry: 8\nmax-retry-credentials: 0\ndisable-cooling: true\nstreaming:\n  bootstrap-retries: 4\n  first-event-timeout-retries: 4\n")
	if strings.HasPrefix(provider, "openai-compatible") {
		fmt.Fprintf(&raw, "openai-compatibility:\n  - name: single-attempt\n    base-url: %q\n    request-retry: 12\n    disable-cooling: true\n", upstreamURL)
		if provider != "openai-compatible-keyless" {
			raw.WriteString("    api-key-entries:\n      - api-key: key-a\n      - api-key: key-b\n      - api-key: key-c\n")
		}
		raw.WriteString("    request-scoped-errors:\n      - status: 503\n        action: continue\n    models:\n      - name: upstream-a\n        alias: " + singleAttemptModel + "\n      - name: upstream-b\n        alias: " + singleAttemptModel + "\n")
	} else {
		section := provider + "-api-key"
		if provider == "gemini-interactions" {
			section = "interactions-api-key"
		}
		fmt.Fprintf(&raw, "%s:\n", section)
		for _, key := range []string{"key-a", "key-b", "key-c"} {
			fmt.Fprintf(&raw, "  - api-key: %s\n    base-url: %q\n    request-retry: 12\n    disable-cooling: true\n    models:\n      - name: upstream-a\n        alias: %s\n", key, upstreamURL, singleAttemptModel)
		}
	}
	cfg, errParse := config.ParseConfigBytes([]byte(raw.String()))
	if errParse != nil {
		t.Fatalf("parse provider config: %v", errParse)
	}
	manager := cliproxyauth.NewManager(nil, &cliproxyauth.RoundRobinSelector{}, nil)
	manager.SetConfig(cfg)
	manager.SetRetryConfig(cfg.RequestRetry, 0, cfg.MaxRetryCredentials)
	var executor cliproxyauth.ProviderExecutor
	switch provider {
	case "claude":
		executor = runtimeexecutor.NewClaudeExecutor(cfg)
	case "codex":
		executor = runtimeexecutor.NewCodexExecutor(cfg)
	case "xai":
		executor = runtimeexecutor.NewXAIExecutor(cfg)
	case "meta":
		executor = runtimeexecutor.NewMetaExecutor(cfg)
	case "gemini":
		executor = runtimeexecutor.NewGeminiExecutor(cfg)
	case "gemini-interactions":
		executor = runtimeexecutor.NewGeminiInteractionsExecutor(cfg)
	case "vertex":
		executor = runtimeexecutor.NewGeminiVertexExecutor(cfg)
	default:
		executor = runtimeexecutor.NewOpenAICompatExecutor("openai-compatible-single-attempt", cfg)
	}
	manager.RegisterExecutor(executor)
	auths, errSynthesize := synthesizer.NewConfigSynthesizer().Synthesize(&synthesizer.SynthesisContext{
		Config: cfg, Now: time.Now(), IDGenerator: synthesizer.NewStableIDGenerator(),
	})
	if errSynthesize != nil || len(auths) == 0 {
		t.Fatalf("synthesize provider credentials: count=%d err=%v", len(auths), errSynthesize)
	}
	for _, auth := range auths {
		if _, errRegister := manager.Register(context.Background(), auth); errRegister != nil {
			t.Fatalf("register provider: %v", errRegister)
		}
		registry.GetGlobalRegistry().RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{{ID: singleAttemptModel}})
		t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(auth.ID) })
	}
	return openai.NewOpenAIAPIHandler(handlers.NewBaseAPIHandlers(&cfg.SDKConfig, manager))
}

func singleAttemptChatRequest(t *testing.T, handler *openai.OpenAIAPIHandler, stream bool) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}],"stream":%t}`, singleAttemptModel, stream)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	handler.ChatCompletions(c)
	return recorder
}

func TestConfigProvidersForwardEachDownstreamRequestOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, provider := range []string{"openai-compatible", "openai-compatible-keyless", "claude", "codex", "xai", "meta", "gemini", "gemini-interactions", "vertex"} {
		for _, status := range []int{400, 401, 402, 403, 404, 408, 409, 422, 429, 500, 502, 503, 504, 520, 522, 524, 529, 599} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%d/stream=%t", provider, status, stream), func(t *testing.T) {
					var received atomic.Int32
					upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						received.Add(1)
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(status)
						_, _ = w.Write([]byte(`{"error":{"message":"upstream rejected this request","type":"upstream_error"}}`))
					}))
					defer upstream.Close()
					handler := newSingleAttemptProviderHandler(t, provider, upstream.URL)
					for request := 1; request <= 10; request++ {
						response := singleAttemptChatRequest(t, handler, stream)
						if response.Code != status {
							t.Fatalf("request %d: downstream status=%d, want %d; body=%s", request, response.Code, status, response.Body.String())
						}
						if got := received.Load(); got != int32(request) {
							t.Fatalf("%d downstream requests produced %d upstream requests", request, got)
						}
					}
				})
			}
		}
	}
}

func TestConfigProviderConcurrentRequestsAreNotReplayed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var received atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"message":"upstream unavailable"}}`))
	}))
	defer upstream.Close()
	handler := newSingleAttemptProviderHandler(t, "openai-compatible", upstream.URL)
	statuses := make(chan int, 10)
	for request := 0; request < 10; request++ {
		go func(stream bool) {
			statuses <- singleAttemptChatRequest(t, handler, stream).Code
		}(request%2 == 0)
	}
	for request := 0; request < 10; request++ {
		if status := <-statuses; status != http.StatusServiceUnavailable {
			t.Errorf("downstream status=%d, want 503", status)
		}
	}
	if got := received.Load(); got != 10 {
		t.Fatalf("10 concurrent downstream requests produced %d upstream requests", got)
	}
}

func TestConfigProviderStreamBootstrapIsNotReplayed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, body := range []string{
		"",
		"data: {\"status\":503,\"error\":{\"message\":\"upstream unavailable\"}}\n\n",
		"data: {\"id\":\"test\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\ndata: {\"status\":503,\"error\":{\"message\":\"upstream unavailable\"}}\n\n",
	} {
		t.Run(fmt.Sprintf("body-length=%d", len(body)), func(t *testing.T) {
			var received atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				received.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte(body))
			}))
			defer upstream.Close()
			handler := newSingleAttemptProviderHandler(t, "openai-compatible", upstream.URL)
			response := singleAttemptChatRequest(t, handler, true)
			if got := received.Load(); got != 1 {
				t.Fatalf("upstream requests=%d, want 1; status=%d body=%s", got, response.Code, response.Body.String())
			}
			if response.Code == http.StatusOK && !strings.Contains(response.Body.String(), "error") {
				t.Fatalf("missing downstream stream failure: %s", response.Body.String())
			}
		})
	}
}

func TestConfigProviderFirstEventTimeoutIsNotReplayed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var received atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer upstream.Close()
	handler := newSingleAttemptProviderHandler(t, "openai-compatible", upstream.URL)
	handler.Cfg.Streaming.FirstEventTimeoutSeconds = 1
	response := singleAttemptChatRequest(t, handler, true)
	if response.Code != http.StatusGatewayTimeout {
		t.Fatalf("downstream status=%d, want 504; body=%s", response.Code, response.Body.String())
	}
	if got := received.Load(); got != 1 {
		t.Fatalf("upstream requests=%d, want 1 despite configured first-event retries", got)
	}
}

func TestConfigProviderWebsocketRejectionDoesNotFallBackToHTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%t", stream), func(t *testing.T) {
			var received atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				received.Add(1)
				w.WriteHeader(http.StatusUpgradeRequired)
				_, _ = w.Write([]byte(`{"error":{"message":"websocket unavailable"}}`))
			}))
			defer upstream.Close()
			handler := newSingleAttemptProviderHandler(t, "codex", upstream.URL)
			handler.AuthManager.RegisterExecutor(runtimeexecutor.NewCodexWebsocketsExecutor(&config.Config{}))
			response := singleAttemptChatRequest(t, handler, stream)
			if response.Code != http.StatusUpgradeRequired || received.Load() != 1 {
				t.Fatalf("status=%d upstream requests=%d, want 426 and 1; body=%s", response.Code, received.Load(), response.Body.String())
			}
		})
	}
}

func TestConfigProviderTransportFailureIsNotReplayed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var received atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		received.Add(1)
		conn, _, errHijack := w.(http.Hijacker).Hijack()
		if errHijack != nil {
			t.Errorf("hijack upstream connection: %v", errHijack)
			return
		}
		if errClose := conn.Close(); errClose != nil {
			t.Errorf("close upstream connection: %v", errClose)
		}
	}))
	defer upstream.Close()
	handler := newSingleAttemptProviderHandler(t, "openai-compatible", upstream.URL)
	for request := 1; request <= 10; request++ {
		response := singleAttemptChatRequest(t, handler, request%2 == 0)
		if response.Code < http.StatusInternalServerError || received.Load() != int32(request) {
			t.Fatalf("request %d: status=%d upstream requests=%d; body=%s", request, response.Code, received.Load(), response.Body.String())
		}
	}
}

func TestConfigProviderRedirectDoesNotSendAnotherRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, provider := range []string{"openai-compatible", "claude"} {
		t.Run(provider, func(t *testing.T) {
			var received atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				received.Add(1)
				w.Header().Set("Location", "/redirected")
				w.WriteHeader(http.StatusTemporaryRedirect)
			}))
			defer upstream.Close()
			handler := newSingleAttemptProviderHandler(t, provider, upstream.URL)
			response := singleAttemptChatRequest(t, handler, false)
			if response.Code != http.StatusTemporaryRedirect || received.Load() != 1 {
				t.Fatalf("status=%d upstream requests=%d, want 307 and 1", response.Code, received.Load())
			}
		})
	}
}
