package openai

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/api/handlers"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
)

func TestResponsesWebsocketCompactionPinRespectsPriority(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, providerRoute := range []bool{false, true} {
		t.Run(fmt.Sprintf("provider-route=%t", providerRoute), func(t *testing.T) {
			ctx := coreauth.WithSkipPersist(context.Background())
			model := t.Name()
			executor := &websocketProviderCaptureExecutor{
				provider: "compaction-priority-provider",
				websocketCaptureExecutor: websocketCaptureExecutor{responses: [][]byte{
					[]byte(`{"type":"response.completed","response":{"id":"resp-1","output":[{"type":"compaction","id":"cmp-1","encrypted_content":"opaque"}]}}`),
					[]byte(`{"type":"response.completed","response":{"id":"resp-2","output":[]}}`),
				}},
			}
			manager := coreauth.NewManager(nil, nil, nil)
			manager.RegisterExecutor(executor)
			low := &coreauth.Auth{ID: t.Name() + "-low", Provider: executor.provider, Status: coreauth.StatusActive, Attributes: map[string]string{"priority": "-1"}}
			high := &coreauth.Auth{ID: t.Name() + "-high", Provider: executor.provider, Status: coreauth.StatusActive, Attributes: map[string]string{"priority": "-2"}}
			for _, auth := range []*coreauth.Auth{low, high} {
				registry.GetGlobalRegistry().RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{{ID: model}})
				t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(auth.ID) })
				if _, errRegister := manager.Register(ctx, auth); errRegister != nil {
					t.Fatal(errRegister)
				}
			}
			base := handlers.NewBaseAPIHandlers(&config.SDKConfig{}, manager)
			requestModel := model
			if providerRoute {
				base.SetModelRouterHost(&websocketCustomProviderRouteHost{targetProvider: executor.provider, targetModel: model})
				requestModel = "compaction-priority-source-model"
			}
			handler := NewOpenAIResponsesAPIHandler(base)
			router := gin.New()
			router.GET("/v1/responses", handler.ResponsesWebsocket)
			server := httptest.NewServer(router)
			t.Cleanup(server.Close)
			conn, _, errDial := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/v1/responses", nil)
			if errDial != nil {
				t.Fatal(errDial)
			}
			t.Cleanup(func() {
				if errClose := conn.Close(); errClose != nil {
					t.Errorf("close websocket: %v", errClose)
				}
			})
			requests := []string{
				fmt.Sprintf(`{"type":"response.create","model":%q,"route_to_custom":%t,"input":[{"role":"user","content":"compact"}]}`, requestModel, providerRoute),
				fmt.Sprintf(`{"type":"response.create","model":%q,"route_to_custom":%t,"input":[{"type":"compaction","id":"cmp-1","encrypted_content":"opaque"},{"role":"user","content":"continue"}]}`, requestModel, providerRoute),
			}
			for index, request := range requests {
				if index == 1 {
					updated := high.Clone()
					updated.Attributes["priority"] = "1"
					if _, errUpdate := manager.Update(ctx, updated); errUpdate != nil {
						t.Fatal(errUpdate)
					}
				}
				if errWrite := conn.WriteMessage(websocket.TextMessage, []byte(request)); errWrite != nil {
					t.Fatal(errWrite)
				}
				_, payload, errRead := conn.ReadMessage()
				if errRead != nil {
					t.Fatal(errRead)
				}
				if ojson.GetBytesNoCopy(payload, "type").String() != wsEventTypeCompleted {
					t.Fatalf("response = %s, want response.completed", payload)
				}
			}
			if got := executor.authIDs; len(got) != 2 || got[0] != low.ID || got[1] != high.ID {
				t.Fatalf("compaction auth IDs = %v, want [%s %s]", got, low.ID, high.ID)
			}
		})
	}
}

func TestResponsesWebsocketRechecksPinnedAuthPriority(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, crossProvider := range []bool{false, true} {
		for _, followup := range []string{"full", "previous-response", "append"} {
			for strategy, newSelector := range map[string]func() coreauth.Selector{
				"round-robin":          func() coreauth.Selector { return &coreauth.RoundRobinSelector{} },
				"weighted-round-robin": func() coreauth.Selector { return &coreauth.WeightedRoundRobinSelector{} },
				"fill-first":           func() coreauth.Selector { return &coreauth.FillFirstSelector{} },
			} {
				t.Run(fmt.Sprintf("%s/cross-provider=%t/%s", strategy, crossProvider, followup), func(t *testing.T) {
					ctx := coreauth.WithSkipPersist(context.Background())
					model := t.Name()
					selector := coreauth.NewSessionAffinitySelector(newSelector())
					t.Cleanup(selector.Stop)
					manager := coreauth.NewManager(nil, selector, nil)
					codexExecutor := &websocketDirectCaptureExecutor{provider: "codex"}
					manager.RegisterExecutor(codexExecutor)
					upstreamExecutor := codexExecutor
					if crossProvider {
						upstreamExecutor = &websocketDirectCaptureExecutor{provider: "priority-responses-provider"}
						manager.RegisterExecutor(upstreamExecutor)
					}
					oauth := &coreauth.Auth{
						ID: t.Name() + "-oauth", Provider: "codex", Status: coreauth.StatusActive,
						Attributes: map[string]string{"priority": "-1", "websockets": "true"},
						Metadata:   map[string]any{"access_token": "test-token"},
					}
					upstream := &coreauth.Auth{
						ID: t.Name() + "-upstream", Provider: upstreamExecutor.Identifier(), Status: coreauth.StatusActive,
						Attributes: map[string]string{"priority": "-2", "api_key": "test-key", "websockets": "false"},
					}
					for _, auth := range []*coreauth.Auth{oauth, upstream} {
						registry.GetGlobalRegistry().RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{{ID: model}})
						t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(auth.ID) })
						if _, errRegister := manager.Register(ctx, auth); errRegister != nil {
							t.Fatal(errRegister)
						}
					}
					handler := NewOpenAIResponsesAPIHandler(handlers.NewBaseAPIHandlers(&config.SDKConfig{}, manager))
					router := gin.New()
					router.GET("/v1/responses", handler.ResponsesWebsocket)
					server := httptest.NewServer(router)
					t.Cleanup(server.Close)
					conn, _, errDial := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/v1/responses", nil)
					if errDial != nil {
						t.Fatal(errDial)
					}
					t.Cleanup(func() {
						if errClose := conn.Close(); errClose != nil {
							t.Errorf("close websocket: %v", errClose)
						}
					})
					request := []byte(fmt.Sprintf(`{"type":"response.create","model":%q,"input":[{"role":"user","content":"hello"}]}`, model))
					execute := func() {
						t.Helper()
						if errWrite := conn.WriteMessage(websocket.TextMessage, request); errWrite != nil {
							t.Fatal(errWrite)
						}
						_, payload, errRead := conn.ReadMessage()
						if errRead != nil {
							t.Fatal(errRead)
						}
						if ojson.GetBytesNoCopy(payload, "type").String() != wsEventTypeCompleted {
							t.Fatalf("response = %s, want response.completed", payload)
						}
					}
					execute()
					if got := codexExecutor.AuthIDs(); len(got) != 1 || got[0] != oauth.ID {
						t.Fatalf("initial auth IDs = %v, want [%s]", got, oauth.ID)
					}
					updated := upstream.Clone()
					updated.Attributes["priority"] = "1"
					if _, errUpdate := manager.Update(ctx, updated); errUpdate != nil {
						t.Fatal(errUpdate)
					}
					if followup != "full" {
						request = []byte(`{"type":"response.append","input":[]}`)
						if followup == "previous-response" {
							request = []byte(fmt.Sprintf(`{"type":"response.create","model":%q,"previous_response_id":"resp-1","input":[]}`, model))
						}
						if errWrite := conn.WriteMessage(websocket.TextMessage, request); errWrite != nil {
							t.Fatal(errWrite)
						}
						_, payload, errRead := conn.ReadMessage()
						var closeErr *websocket.CloseError
						if !errors.As(errRead, &closeErr) || closeErr.Code != websocket.CloseServiceRestart || closeErr.Text != wsHTTPReplayRequiredCloseReason {
							t.Fatalf("incremental response = %s, error = %v, want full replay close", payload, errRead)
						}
						if got := codexExecutor.AuthIDs(); len(got) != 1 {
							t.Fatalf("incremental request executed with stale upstream state: %v", got)
						}
						if crossProvider && len(upstreamExecutor.AuthIDs()) != 0 {
							t.Fatal("incremental request was sent to a new provider without replay")
						}
						return
					}
					execute()
					execute()
					got := upstreamExecutor.AuthIDs()
					if len(got) < 2 || got[len(got)-2] != upstream.ID || got[len(got)-1] != upstream.ID {
						t.Fatalf("auth IDs after raising upstream priority = %v, want last two auths %s", got, upstream.ID)
					}
				})
			}
		}
	}
}
