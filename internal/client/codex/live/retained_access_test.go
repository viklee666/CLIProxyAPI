package live

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executionregistry"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

type liveReassignedOwnership struct{}

func (liveReassignedOwnership) OwnerOf(string) int64      { return 42 }
func (liveReassignedOwnership) HasOwnedCredentials() bool { return true }

func TestRetainedLiveHomeSelectionRechecksCurrentAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []string{"sideband", "hangup"} {
		for _, change := range []string{"group-resolver", "ownership-resolver", "caller-flags", "disabled"} {
			t.Run(endpoint+"/"+change, func(t *testing.T) {
				manager := auth.NewManager(nil, nil, nil)
				manager.SetConfig(&config.Config{Home: config.HomeConfig{Enabled: true}})
				manager.PublishHomeDispatch(&homeDispatcher{}, executionregistry.New(), 1)
				manager.SetCredentialGroupResolver(liveAllowGroups{})
				executor := &captureExecutor{responseBody: io.NopCloser(strings.NewReader(`{"status":"ok"}`))}
				manager.RegisterExecutor(executor)

				currentAccess := liveTestAccess()
				delete(currentAccess, coreexecutor.ClientTenantIDMetadataKey)
				initialAccess := liveTestAccess()
				delete(initialAccess, coreexecutor.ClientTenantIDMetadataKey)
				if change == "caller-flags" {
					initialAccess[coreexecutor.ClientAllowAllGroupsMetadataKey] = "true"
					manager.SetCredentialGroupResolver(liveDenyGroups{})
				}
				initialContext, _ := gin.CreateTestContext(httptest.NewRecorder())
				initialContext.Set("accessMetadata", initialAccess)
				selection, errSelect := manager.SelectHomeAuthByKind(context.Background(), "codex", defaultLiveModel, auth.AuthKindOAuth, coreexecutor.Options{
					Metadata: liveAccessMetadata(initialContext),
				})
				if errSelect != nil {
					t.Fatalf("initial allowed Home selection: %v", errSelect)
				}
				selection.Retain()
				t.Cleanup(func() { selection.End("test_complete") })
				if !manager.CredentialAllowedForMetadata(selection.CloneAuth(), liveAccessMetadata(initialContext)) {
					t.Fatal("initial retained credential was not allowed")
				}

				switch change {
				case "group-resolver":
					manager.SetCredentialGroupResolver(liveDenyGroups{})
				case "ownership-resolver":
					manager.SetCredentialOwnershipResolver(liveReassignedOwnership{})
				case "disabled":
					credential := selection.CloneAuth()
					credential.Disabled = true
					selection.ReplaceAuth(credential)
				}

				var upstreamCalls atomic.Int32
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					upstreamCalls.Add(1)
					w.WriteHeader(http.StatusBadGateway)
				}))
				t.Cleanup(upstream.Close)
				handler := NewHandler(manager, nil)
				t.Cleanup(handler.Close)
				handler.sidebandAPIBaseURL = "ws" + strings.TrimPrefix(upstream.URL, "http")
				handler.sessions.put("call-retained", liveSession{
					authID: selection.CloneAuth().ID, model: defaultLiveModel, homeSelection: selection,
					ownerPrincipal: "issuer-key", ownerProvider: "client-access", clientKeyID: "7",
				})
				router := gin.New()
				router.Use(func(c *gin.Context) {
					c.Set("userApiKey", "issuer-key")
					c.Set("accessProvider", "client-access")
					c.Set("accessMetadata", currentAccess)
				})
				method, path := http.MethodGet, "/v1/realtime/calls/call-retained"
				if endpoint == "hangup" {
					method, path = http.MethodPost, path+"/hangup"
					router.POST("/v1/realtime/calls/:call_id/hangup", handler.HandleHangup)
				} else {
					router.GET("/v1/realtime/calls/:call_id", handler.HandleSideband)
				}
				request := httptest.NewRequest(method, path, nil)
				request.Header.Set("Connection", "Upgrade")
				request.Header.Set("Upgrade", "websocket")
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, request)
				if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), "realtime_credential_access_denied") {
					t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
				}
				if executor.httpCalls.Load() != 0 || upstreamCalls.Load() != 0 {
					t.Fatalf("revoked retained credential reached upstream: HTTP calls=%d websocket calls=%d", executor.httpCalls.Load(), upstreamCalls.Load())
				}
				if !selection.Active() {
					t.Fatal("denied request ended the retained selection")
				}
				claimed, claim := handler.sessions.claim("call-retained")
				if claim != sessionClaimAcquired {
					t.Fatalf("denied request consumed or locked the call: %v", claim)
				}
				handler.sessions.release(claimed)
			})
		}
	}
}
