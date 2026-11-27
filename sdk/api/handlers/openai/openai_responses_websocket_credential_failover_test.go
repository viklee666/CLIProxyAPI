package openai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	runtimeexecutor "github.com/router-for-me/CLIProxyAPI/v7/internal/runtime/executor"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestResponsesWebsocketSameSessionLaterCreate429FailoverToAlternativeCredential(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const quota429Frame = `{"type":"error","status":429,"error":{"type":"usage_limit_reached","message":"The usage limit has been reached","plan_type":"team","resets_in_seconds":3600}}`

	for _, buffering := range []bool{false, true} {
		t.Run(fmt.Sprintf("buffering=%t", buffering), func(t *testing.T) {
			var authACreates, authBCreates atomic.Int32
			var authA429Next atomic.Bool

			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
				if authHeader != "Bearer key-a" && authHeader != "Bearer key-b" {
					t.Errorf("unexpected Authorization header: %q", authHeader)
					http.Error(w, "unauthorized", http.StatusUnauthorized)
					return
				}
				c, errUpgrade := (&websocket.Upgrader{}).Upgrade(w, r, nil)
				if errUpgrade != nil {
					t.Error(errUpgrade)
					return
				}
				defer func() { _ = c.Close() }()
				_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))

				for {
					_, payload, errRead := c.ReadMessage()
					if errRead != nil {
						return
					}
					if ojson.GetBytesNoCopy(payload, "type").String() != "response.create" {
						continue
					}
					if authHeader == "Bearer key-a" {
						turn := authACreates.Add(1)
						if authA429Next.Load() {
							if buffering {
								_ = c.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.created","response":{"id":"resp-a-buffered","status":"in_progress","output":[]}}`))
								_ = c.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.in_progress","response":{"id":"resp-a-buffered","status":"in_progress","output":[]}}`))
							}
							_ = c.WriteMessage(websocket.TextMessage, []byte(quota429Frame))
							continue
						}
						respID := fmt.Sprintf("resp-a-%d", turn)
						_ = c.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(`{"type":"response.created","response":{"id":%q,"status":"in_progress","output":[]}}`, respID)))
						_ = c.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(`{"type":"response.completed","response":{"id":%q,"status":"completed","output":[{"id":"msg-a-%d","type":"message","role":"assistant","content":[{"type":"output_text","text":"from-a"}]}]}}`, respID, turn)))
						continue
					}
					turn := authBCreates.Add(1)
					respID := fmt.Sprintf("resp-b-%d", turn)
					_ = c.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(`{"type":"response.created","response":{"id":%q,"status":"in_progress","output":[]}}`, respID)))
					_ = c.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(`{"type":"response.completed","response":{"id":%q,"status":"completed","output":[{"id":"msg-b-%d","type":"message","role":"assistant","content":[{"type":"output_text","text":"from-b"}]}]}}`, respID, turn)))
				}
			}))
			t.Cleanup(upstream.Close)

			cfg := &config.Config{
				DisableCooling:      false,
				RequestRetry:        3,
				MaxRetryCredentials: 0,
				Codex: config.CodexConfig{
					StreamBootstrapBuffering: buffering,
				},
			}
			manager := coreauth.NewManager(nil, &coreauth.FillFirstSelector{}, nil)
			manager.SetConfig(cfg)
			manager.SetRetryConfig(cfg.RequestRetry, 0, cfg.MaxRetryCredentials)
			manager.RegisterExecutor(runtimeexecutor.NewCodexAutoExecutor(cfg))

			modelName := "codex-same-session-create-429-" + strings.ReplaceAll(t.Name(), "/", "-")
			authAID := "auth-a-" + modelName
			authBID := "auth-b-" + modelName
			for _, spec := range []struct {
				id       string
				key      string
				priority string
			}{
				{id: authAID, key: "key-a", priority: "4"},
				{id: authBID, key: "key-b", priority: "3"},
			} {
				if _, errRegister := manager.Register(context.Background(), &coreauth.Auth{
					ID:       spec.id,
					Provider: "codex",
					Status:   coreauth.StatusActive,
					Attributes: map[string]string{
						// Cross-tier exhaustion failover applies to stored credentials.
						coreauth.AttributeSourceBackend: coreauth.AuthSourceFile,
						"api_key":                       spec.key,
						"base_url":                      upstream.URL,
						"websockets":                    "true",
						"priority":                      spec.priority,
					},
				}); errRegister != nil {
					t.Fatal(errRegister)
				}
				registry.GetGlobalRegistry().RegisterClient(spec.id, "codex", []*registry.ModelInfo{{ID: modelName}})
				id := spec.id
				t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(id) })
			}

			base := handlers.NewBaseAPIHandlers(&cfg.SDKConfig, manager)
			h := NewOpenAIResponsesAPIHandler(base)
			router := gin.New()
			router.GET("/v1/responses", h.ResponsesWebsocket)
			downstream := httptest.NewServer(router)
			t.Cleanup(downstream.Close)
			wsURL := "ws" + strings.TrimPrefix(downstream.URL, "http") + "/v1/responses"

			conn, resp, errDial := websocket.DefaultDialer.Dial(wsURL, nil)
			if resp != nil && resp.Body != nil {
				defer func() { _ = resp.Body.Close() }()
			}
			if errDial != nil {
				t.Fatalf("dial downstream websocket: %v", errDial)
			}
			defer func() { _ = conn.Close() }()

			// Turn 1: full response.create succeeds on auth-a and pins auth-a for the connection.
			req1 := fmt.Sprintf(`{"type":"response.create","model":%q,"input":[{"type":"message","id":"msg-1","role":"user","content":"turn-1"}]}`, modelName)
			if errWrite := conn.WriteMessage(websocket.TextMessage, []byte(req1)); errWrite != nil {
				t.Fatalf("write turn 1: %v", errWrite)
			}
			for {
				_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
				_, frame, errRead := conn.ReadMessage()
				if errRead != nil {
					t.Fatalf("turn 1 read failed: %v", errRead)
				}
				if ojson.GetBytesNoCopy(frame, "type").String() == "response.completed" {
					if got := ojson.GetBytesNoCopy(frame, "response.id").String(); got != "resp-a-1" {
						t.Fatalf("turn 1 response.id = %q, want resp-a-1", got)
					}
					break
				}
			}

			// Turn 2: on the same downstream websocket, auth-a is now quota-exhausted and returns
			// a pre-content 429 usage_limit_reached. Another full response.create must fail over
			// to auth-b without closing the downstream websocket or leaking frames from auth-a.
			authA429Next.Store(true)
			req2 := fmt.Sprintf(`{"type":"response.create","model":%q,"input":[{"type":"message","id":"msg-2","role":"user","content":"turn-2"}]}`, modelName)
			if errWrite := conn.WriteMessage(websocket.TextMessage, []byte(req2)); errWrite != nil {
				t.Fatalf("write turn 2: %v", errWrite)
			}
			var turn2Completed []byte
			for {
				_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
				_, frame, errRead := conn.ReadMessage()
				if errRead != nil {
					t.Fatalf("turn 2 failed instead of failing over to auth-b: %v (authACreates=%d authBCreates=%d)", errRead, authACreates.Load(), authBCreates.Load())
				}
				if gotID := ojson.GetBytesNoCopy(frame, "response.id").String(); gotID == "resp-a-buffered" {
					t.Fatalf("buffered frame from failed credential A leaked to downstream: %s", frame)
				}
				if ojson.GetBytesNoCopy(frame, "type").String() == "error" {
					t.Fatalf("received error frame instead of failing over to auth-b: %s", frame)
				}
				if ojson.GetBytesNoCopy(frame, "type").String() == "response.completed" {
					turn2Completed = frame
					break
				}
			}
			if got := ojson.GetBytesNoCopy(turn2Completed, "response.id").String(); got != "resp-b-1" {
				t.Fatalf("turn 2 response.id = %q, want resp-b-1", got)
			}
			if authACreates.Load() != 2 || authBCreates.Load() != 1 {
				t.Fatalf("after turn 2: authACreates=%d authBCreates=%d, want 2 and 1", authACreates.Load(), authBCreates.Load())
			}

			authA, okA := manager.GetByID(authAID)
			if !okA || authA == nil || !authA.Quota.Exceeded {
				t.Fatalf("expected auth-a quota exceeded cooldown to be recorded, got %+v (ok=%t)", authA, okA)
			}

			// Turn 3: continuation on the same downstream websocket must stay pinned to auth-b.
			req3 := fmt.Sprintf(`{"type":"response.create","model":%q,"previous_response_id":"resp-b-1","input":[{"type":"message","id":"msg-3","role":"user","content":"turn-3"}]}`, modelName)
			if errWrite := conn.WriteMessage(websocket.TextMessage, []byte(req3)); errWrite != nil {
				t.Fatalf("write turn 3: %v", errWrite)
			}
			for {
				_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
				_, frame, errRead := conn.ReadMessage()
				if errRead != nil {
					t.Fatalf("turn 3 read failed: %v", errRead)
				}
				if ojson.GetBytesNoCopy(frame, "type").String() == "response.completed" {
					if got := ojson.GetBytesNoCopy(frame, "response.id").String(); got != "resp-b-2" {
						t.Fatalf("turn 3 response.id = %q, want resp-b-2", got)
					}
					break
				}
			}
			if authACreates.Load() != 2 || authBCreates.Load() != 2 {
				t.Fatalf("after turn 3: authACreates=%d authBCreates=%d, want 2 and 2", authACreates.Load(), authBCreates.Load())
			}
		})
	}
}
