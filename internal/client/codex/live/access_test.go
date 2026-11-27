package live

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	sdkaccess "github.com/router-for-me/CLIProxyAPI/v7/sdk/access"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executionregistry"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

type liveTestOwnership struct{}

func (liveTestOwnership) OwnerOf(index string) int64 {
	if index == "tenant-index" {
		return 42
	}
	return 0
}

func (liveTestOwnership) HasOwnedCredentials() bool { return true }

type liveAllowGroups struct{}

func (liveAllowGroups) ResolveCredentialAccess(string, []int64, bool, bool) (bool, int, bool) {
	return true, 0, false
}

type liveDenyGroups struct{}

func (liveDenyGroups) ResolveCredentialAccess(string, []int64, bool, bool) (bool, int, bool) {
	return false, 0, false
}

type liveMetadataSelector struct {
	metadata map[string]any
}

func (s *liveMetadataSelector) Pick(_ context.Context, _, _ string, opts coreexecutor.Options, candidates []*auth.Auth) (*auth.Auth, error) {
	s.metadata = opts.Metadata
	if len(candidates) == 0 {
		return nil, nil
	}
	return candidates[0], nil
}

func liveTestAccess() map[string]string {
	return map[string]string{
		coreexecutor.ClientKeyIDMetadataKey:          "7",
		coreexecutor.ClientTenantIDMetadataKey:       "42",
		coreexecutor.ClientGroupIDsMetadataKey:       "11,12",
		coreexecutor.ClientAllowAllGroupsMetadataKey: "false",
		coreexecutor.ClientAllowUngroupedMetadataKey: "false",
		coreexecutor.ClientReservationIDMetadataKey:  "reservation-current-request",
	}
}

func TestLiveSelectionPropagatesClientAccessAndQuotaMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	selector := &liveMetadataSelector{}
	manager := auth.NewManager(nil, selector, nil)
	manager.SetCredentialOwnershipResolver(liveTestOwnership{})
	manager.SetCredentialGroupResolver(liveAllowGroups{})
	executor := &captureExecutor{responseBody: io.NopCloser(strings.NewReader("v=0\r\n"))}
	manager.RegisterExecutor(executor)
	registerCredential(t, manager, &auth.Auth{ID: "tenant-oauth", Index: "tenant-index", Provider: "codex", Status: auth.StatusActive, Attributes: map[string]string{auth.AttributeTenantID: "42"}, Metadata: map[string]any{"access_token": "test-token"}})
	handler := NewHandler(manager, nil)
	t.Cleanup(handler.Close)
	router := gin.New()
	router.POST("/v1/live", func(c *gin.Context) {
		c.Set("accessMetadata", liveTestAccess())
		handler.Handle(c)
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/live", strings.NewReader(`{"model":"gpt-live-1-codex"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	for key, value := range liveTestAccess() {
		var expected any = value
		if key == coreexecutor.ClientAllowAllGroupsMetadataKey || key == coreexecutor.ClientAllowUngroupedMetadataKey {
			expected = false
		}
		if got := selector.metadata[key]; got != expected {
			t.Errorf("metadata[%s]=%#v, want %#v", key, got, expected)
		}
	}
}

func TestLiveEndpointsEnforceCredentialGroups(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []string{"bootstrap", "websocket", "sideband", "hangup"} {
		t.Run(endpoint, func(t *testing.T) {
			manager := auth.NewManager(nil, nil, nil)
			manager.SetCredentialGroupResolver(liveDenyGroups{})
			manager.SetCredentialOwnershipResolver(liveTestOwnership{})
			executor := &captureExecutor{responseBody: io.NopCloser(strings.NewReader("v=0\r\n"))}
			manager.RegisterExecutor(executor)
			registerCredential(t, manager, &auth.Auth{ID: "forbidden-oauth", Index: "tenant-index", Provider: "codex", Status: auth.StatusActive, Attributes: map[string]string{auth.AttributeTenantID: "42"}, Metadata: map[string]any{"access_token": "test-token"}})
			handler := NewHandler(manager, nil)
			t.Cleanup(handler.Close)
			var upstreamCalls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				upstreamCalls.Add(1)
				w.WriteHeader(http.StatusBadGateway)
			}))
			t.Cleanup(upstream.Close)
			handler.sidebandAPIBaseURL = "ws" + strings.TrimPrefix(upstream.URL, "http")
			handler.sessions.put("call-123", liveSession{
				authID: "forbidden-oauth", model: defaultLiveModel,
				ownerPrincipal: "issuer", ownerProvider: "client-access", clientKeyID: "7", clientTenantID: "42",
			})
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set("userApiKey", "issuer")
				c.Set("accessProvider", "client-access")
				c.Set("accessMetadata", liveTestAccess())
			})
			method, path := http.MethodPost, "/v1/live"
			switch endpoint {
			case "bootstrap":
				router.POST(path, handler.Handle)
			case "websocket":
				method, path = http.MethodGet, "/v1/realtime"
				router.GET(path, handler.HandleDirectWebsocket)
			case "sideband":
				method, path = http.MethodGet, "/v1/realtime/calls/call-123"
				router.GET("/v1/realtime/calls/:call_id", handler.HandleSideband)
			case "hangup":
				path = "/v1/realtime/calls/call-123/hangup"
				router.POST("/v1/realtime/calls/:call_id/hangup", handler.HandleHangup)
			}
			request := httptest.NewRequest(method, path, strings.NewReader(`{"model":"gpt-live-1-codex"}`))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Connection", "Upgrade")
			request.Header.Set("Upgrade", "websocket")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			if recorder.Code < 400 || executor.httpCalls.Load() != 0 || upstreamCalls.Load() != 0 {
				t.Fatalf("status=%d HTTP calls=%d websocket calls=%d body=%s", recorder.Code, executor.httpCalls.Load(), upstreamCalls.Load(), recorder.Body.String())
			}
		})
	}
}

func TestLiveTenantKeyDoesNotDispatchThroughGlobalHome(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager := auth.NewManager(nil, nil, nil)
	manager.SetConfig(&config.Config{Home: config.HomeConfig{Enabled: true}})
	dispatcher := &homeDispatcher{}
	manager.PublishHomeDispatch(dispatcher, executionregistry.New(), 1)
	manager.RegisterExecutor(&captureExecutor{responseBody: io.NopCloser(strings.NewReader("v=0\r\n"))})
	handler := NewHandler(manager, nil)
	t.Cleanup(handler.Close)
	router := gin.New()
	router.POST("/v1/live", func(c *gin.Context) {
		c.Set("accessMetadata", liveTestAccess())
		handler.Handle(c)
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/live", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code < 400 || dispatcher.model != "" {
		t.Fatalf("status=%d global Home model=%q", recorder.Code, dispatcher.model)
	}
}

func TestLiveCallOwnershipRejectsIdentityChanges(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, scenario := range []string{"anonymous-session", "different-key", "different-tenant", "different-secret"} {
		for _, hangup := range []bool{true, false} {
			t.Run(scenario+map[bool]string{true: "-hangup", false: "-sideband"}[hangup], func(t *testing.T) {
				handler := NewHandler(auth.NewManager(nil, nil, nil), nil)
				t.Cleanup(handler.Close)
				session := liveSession{authID: "credential", model: defaultLiveModel, ownerPrincipal: "issuer", ownerProvider: "client-access", clientKeyID: "7", clientTenantID: "42"}
				switch scenario {
				case "anonymous-session":
					session.ownerPrincipal, session.ownerProvider, session.clientKeyID, session.clientTenantID = "", "", "", ""
				case "different-key":
					session.clientKeyID = "8"
				case "different-tenant":
					session.clientTenantID = "43"
				case "different-secret":
					session.clientSecretPrincipal = "sess-owner"
				}
				handler.sessions.put("call-123", session)
				router := gin.New()
				router.Use(func(c *gin.Context) {
					c.Set("userApiKey", "issuer")
					c.Set("accessProvider", "client-access")
					c.Set("accessMetadata", liveTestAccess())
					if scenario == "different-secret" {
						c.Set(ClientSecretPrincipalContextKey, "sess-other")
					}
				})
				method, path := http.MethodGet, "/v1/realtime/calls/call-123"
				if hangup {
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
				if recorder.Code != http.StatusForbidden {
					t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
				}
				claimed, claim := handler.sessions.claim("call-123")
				if claim != sessionClaimAcquired {
					t.Fatalf("unauthorized request consumed or locked call: %v", claim)
				}
				handler.sessions.release(claimed)
			})
		}
	}
}

type liveIssuerProvider struct {
	authenticate func(*http.Request) (*sdkaccess.Result, *sdkaccess.AuthError)
}

func (liveIssuerProvider) Identifier() string { return "client-access" }
func (p liveIssuerProvider) Authenticate(_ context.Context, request *http.Request) (*sdkaccess.Result, *sdkaccess.AuthError) {
	return p.authenticate(request)
}

func TestCreateClientSecretRetainsIssuingClientIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &Handler{clientSecrets: newClientSecretStore()}
	t.Cleanup(handler.Close)
	router := gin.New()
	router.POST("/v1/realtime/client_secrets", func(c *gin.Context) {
		c.Set("userApiKey", "issuer-key")
		c.Set("accessProvider", "client-access")
		c.Set("accessMetadata", liveTestAccess())
		handler.CreateClientSecret(c)
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/realtime/client_secrets", strings.NewReader(`{}`)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response clientSecretCreateResponse
	if errDecode := json.Unmarshal(recorder.Body.Bytes(), &response); errDecode != nil {
		t.Fatal(errDecode)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/realtime/calls", nil)
	request.Header.Set("Authorization", "Bearer "+response.Value)
	authorization, matched, errAuthenticate := handler.AuthenticateClientSecret(request)
	if errAuthenticate != nil || !matched || authorization.IssuerPrincipal != "issuer-key" || authorization.IssuerProvider != "client-access" {
		t.Fatalf("matched=%v issuer=%q/%q error=%v", matched, authorization.IssuerProvider, authorization.IssuerPrincipal, errAuthenticate)
	}
	if !reflect.DeepEqual(authorization.AccessMetadata, cloneClientSecretAccessMetadata(liveTestAccess())) {
		t.Fatalf("secret identity=%v", authorization.AccessMetadata)
	}
}

func TestClientSecretAnonymousIssuerRequiresAnonymousServer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, configured := range []bool{false, true} {
		manager := sdkaccess.NewManager()
		if configured {
			manager.SetProviders([]sdkaccess.Provider{liveIssuerProvider{authenticate: func(*http.Request) (*sdkaccess.Result, *sdkaccess.AuthError) {
				t.Fatal("anonymous secret attempted provider authentication")
				return nil, nil
			}}})
		}
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/realtime/calls", nil)
		_, ok := AuthorizeClientSecretIssuer(c, ClientSecretAuthorization{Principal: "sess-anonymous"}, manager)
		if ok == configured {
			t.Fatalf("configured=%v authorized=%v status=%d", configured, ok, recorder.Code)
		}
	}
}

func TestClientSecretRevalidatesIssuerAndUsesFreshQuotaAndPermissions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newClientSecretStore()
	metadata := liveTestAccess()
	token, _, _, errCreate := store.create(json.RawMessage(`{"model":"gpt-live-1-codex"}`), time.Minute, "issuer-key", "client-access", metadata)
	if errCreate != nil {
		t.Fatal(errCreate)
	}
	metadata[coreexecutor.ClientTenantIDMetadataKey] = "99"
	authorization, errAuthenticate := store.authenticate(token)
	if errAuthenticate != nil {
		t.Fatal(errAuthenticate)
	}
	if authorization.AccessMetadata[coreexecutor.ClientTenantIDMetadataKey] != "42" || authorization.AccessMetadata[coreexecutor.ClientReservationIDMetadataKey] != "" {
		t.Fatalf("secret retained mutable identity or stale reservation: %#v", authorization.AccessMetadata)
	}
	authorization.AccessMetadata[coreexecutor.ClientTenantIDMetadataKey] = "100"
	authorization, _ = store.authenticate(token)
	if authorization.AccessMetadata[coreexecutor.ClientTenantIDMetadataKey] != "42" {
		t.Fatal("authentication result aliases stored identity")
	}
	for _, scenario := range []string{"allowed", "revoked", "quota-exhausted", "identity-replaced"} {
		t.Run(scenario, func(t *testing.T) {
			manager := sdkaccess.NewManager()
			released, reached := false, false
			current := liveTestAccess()
			current[coreexecutor.ClientGroupIDsMetadataKey] = "13"
			current[coreexecutor.ClientReservationIDMetadataKey] = "fresh-reservation"
			manager.SetProviders([]sdkaccess.Provider{liveIssuerProvider{authenticate: func(request *http.Request) (*sdkaccess.Result, *sdkaccess.AuthError) {
				if request.Header.Get("Authorization") != "Bearer issuer-key" || request.URL.RawQuery != "" || request.Header.Get("X-Api-Key") != "" {
					t.Errorf("issuer reauthentication accepted caller credentials: headers=%v query=%q", request.Header, request.URL.RawQuery)
				}
				switch scenario {
				case "revoked":
					return nil, sdkaccess.NewInvalidCredentialError()
				case "quota-exhausted":
					errQuota := sdkaccess.NewRateLimitedError("quota exhausted")
					errQuota.Headers = http.Header{"Retry-After": []string{"60"}}
					return nil, errQuota
				case "identity-replaced":
					current[coreexecutor.ClientTenantIDMetadataKey] = "99"
				}
				return &sdkaccess.Result{Principal: "issuer-key", Provider: "client-access", Metadata: current, Release: func() { released = true }}, nil
			}}})
			router := gin.New()
			router.POST("/v1/realtime/calls", func(c *gin.Context) {
				release, ok := AuthorizeClientSecretIssuer(c, authorization, manager)
				if release != nil {
					defer release()
				}
				if !ok {
					return
				}
				reached = true
				got, _ := c.Get("accessMetadata")
				if !reflect.DeepEqual(got, current) {
					t.Errorf("metadata=%#v want %#v", got, current)
				}
				c.Status(http.StatusNoContent)
			})
			request := httptest.NewRequest(http.MethodPost, "/v1/realtime/calls?api_key=attacker", nil)
			request.Header.Set("Authorization", "Bearer "+token)
			request.Header.Set("X-Api-Key", "attacker")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			if scenario == "allowed" {
				if !reached || !released || recorder.Code != http.StatusNoContent {
					t.Fatalf("reached=%v released=%v status=%d", reached, released, recorder.Code)
				}
			} else if reached || recorder.Code < 400 || (scenario == "identity-replaced" && !released) {
				t.Fatalf("reached=%v released=%v status=%d", reached, released, recorder.Code)
			}
			if scenario == "quota-exhausted" && (recorder.Code != http.StatusTooManyRequests || recorder.Header().Get("Retry-After") != "60") {
				t.Fatalf("quota status=%d Retry-After=%q", recorder.Code, recorder.Header().Get("Retry-After"))
			}
			if request.URL.RawQuery != "api_key=attacker" || request.Header.Get("Authorization") != "Bearer "+token {
				t.Fatal("issuer reauthentication mutated downstream request")
			}
		})
	}
}
