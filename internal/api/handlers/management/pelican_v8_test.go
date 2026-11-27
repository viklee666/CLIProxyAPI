package management

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MoeForge/moego/json"
	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
)

type pelicanTestExecutor struct {
	apiCallAgentIdentityExecutor
	calls         []string
	sessions      []string
	request       coreexecutor.Request
	options       coreexecutor.Options
	err           error
	response      coreexecutor.Response
	retryOff      bool
	cancel        bool
	oauthRecovery bool
	refreshCalls  []string
}

func (e *pelicanTestExecutor) Identifier() string { return "pelican-test" }

func (e *pelicanTestExecutor) Execute(ctx context.Context, auth *coreauth.Auth, req coreexecutor.Request, opts coreexecutor.Options) (coreexecutor.Response, error) {
	e.calls = append(e.calls, auth.ID)
	e.sessions = append(e.sessions, opts.Headers.Get("X-Session-Id"))
	e.request, e.options = req, opts
	e.retryOff = coreexecutor.RequestRetriesDisabled(ctx)
	if e.cancel {
		<-ctx.Done()
		return coreexecutor.Response{}, ctx.Err()
	}
	if e.oauthRecovery && auth.ReadMetadataString("access_token") != "refreshed-token" {
		return coreexecutor.Response{}, &coreauth.Error{HTTPStatus: http.StatusUnauthorized, Message: "stale token"}
	}
	return e.response, e.err
}

func (e *pelicanTestExecutor) Refresh(_ context.Context, auth *coreauth.Auth) (*coreauth.Auth, error) {
	e.refreshCalls = append(e.refreshCalls, auth.ID)
	updated := auth.Clone()
	updated.Metadata["access_token"] = "refreshed-token"
	return updated, nil
}

func setupPelicanTest(t *testing.T) (*gin.Engine, *pelicanTestExecutor, []*coreauth.Auth) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	manager := coreauth.NewManager(nil, &coreauth.RoundRobinSelector{}, nil)
	manager.SetRetryConfig(8, 0, 0)
	executor := &pelicanTestExecutor{
		response: coreexecutor.Response{Payload: []byte(`{"model":"actual-model","choices":[{"finish_reason":"stop","message":{"content":"<svg><text>pelican</text></svg>"}}],"usage":{"total_tokens":28}}`)},
	}
	manager.RegisterExecutor(executor)
	auths := make([]*coreauth.Auth, 0, 2)
	for i, kind := range []string{coreauth.AuthKindAPIKey, coreauth.AuthKindOAuth} {
		auth := &coreauth.Auth{
			ID: t.Name() + "-" + kind, Provider: executor.Identifier(), Status: coreauth.StatusActive,
			Attributes: map[string]string{coreauth.AttributeAuthKind: kind, "priority": "10"},
			Metadata:   map[string]any{"disable_cooling": true, "request_retry": 8},
		}
		if i == 0 {
			auth.Attributes["priority"] = "1"
		} else {
			auth.FileName = "pelican-credential.json"
			auth.Metadata["access_token"] = "stale-token"
			auth.Metadata["refresh_token"] = "refresh-token"
		}
		registered, errRegister := manager.Register(coreauth.WithSkipPersist(t.Context()), auth)
		if errRegister != nil {
			t.Fatal(errRegister)
		}
		registry.GetGlobalRegistry().RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{{ID: "pelican-model"}})
		t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(auth.ID) })
		auths = append(auths, registered)
	}
	handler := &Handler{authManager: manager}
	router := gin.New()
	router.POST("/pelican", handler.PelicanTestV8)
	router.GET("/pelican", handler.GetPelicanTargetV8)
	return router, executor, auths
}

func callPelican(t *testing.T, router *gin.Engine, request pelicanRequest) *httptest.ResponseRecorder {
	t.Helper()
	body, errMarshal := json.Marshal(request)
	if errMarshal != nil {
		t.Fatal(errMarshal)
	}
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/pelican", strings.NewReader(string(body)))
	req.Header.Set("Authorization", "Bearer management-secret-must-not-reach-upstream")
	router.ServeHTTP(recorder, req)
	return recorder
}

func TestPelicanV8PinsProviderAndCredential(t *testing.T) {
	router, executor, auths := setupPelicanTest(t)
	for _, target := range auths {
		request := pelicanRequest{AuthIndex: target.Index, Model: "pelican-model", Prompt: "Draw a pelican riding a bicycle.\nReturn HTML.", Effort: "high"}
		if target.FileName != "" {
			request.Name = target.FileName
		}
		response := callPelican(t, router, request)
		if response.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
		body := ojson.Parse(response.Body.String())
		if body.Get("auth_id").String() != target.ID || body.Get("auth_index").String() != target.Index || body.Get("answer").String() != "<svg><text>pelican</text></svg>" || body.Get("model").String() != "actual-model" || body.Get("usage.total_tokens").Int() != 28 {
			t.Fatalf("wrong target/artifact: %s", response.Body.String())
		}
		if executor.calls[len(executor.calls)-1] != target.ID || !executor.retryOff {
			t.Fatalf("target or no-retry policy lost: calls=%v retryOff=%v", executor.calls, executor.retryOff)
		}
		if executor.options.Headers.Get("Authorization") != "" || executor.options.SourceFormat != sdktranslator.FormatOpenAI || executor.options.ResponseFormat != sdktranslator.FormatOpenAI {
			t.Fatalf("unexpected upstream options: %+v", executor.options)
		}
		payload := ojson.ParseBytes(executor.request.Payload)
		if payload.Get("messages.0.content").String() != request.Prompt || payload.Get("reasoning_effort").String() != "high" || payload.Get("store").Bool() {
			t.Fatalf("test prompt/effort not preserved: %s", executor.request.Payload)
		}
	}
	if len(executor.calls) != 2 || executor.sessions[0] == executor.sessions[1] || !strings.HasPrefix(executor.sessions[0], "pelican-") {
		t.Fatalf("test sessions not isolated: calls=%v sessions=%v", executor.calls, executor.sessions)
	}
}

func TestPelicanV8FailureNeverRetriesOrFallsBack(t *testing.T) {
	router, executor, auths := setupPelicanTest(t)
	executor.err = &coreauth.Error{HTTPStatus: http.StatusServiceUnavailable, Message: "upstream unavailable"}
	response := callPelican(t, router, pelicanRequest{AuthIndex: auths[0].Index, Model: "pelican-model", Prompt: "Draw a pelican.", Effort: "medium"})
	if response.Code != http.StatusServiceUnavailable || len(executor.calls) != 1 || executor.calls[0] != auths[0].ID {
		t.Fatalf("status=%d calls=%v body=%s", response.Code, executor.calls, response.Body.String())
	}
}

func TestPelicanV8RejectsInvalidAndMismatchedTargets(t *testing.T) {
	router, executor, auths := setupPelicanTest(t)
	for _, tc := range []struct {
		name    string
		request pelicanRequest
		status  int
	}{
		{"missing target", pelicanRequest{Model: "pelican-model", Prompt: "draw", Effort: "low"}, http.StatusBadRequest},
		{"missing model", pelicanRequest{AuthIndex: auths[0].Index, Prompt: "draw", Effort: "low"}, http.StatusBadRequest},
		{"missing prompt", pelicanRequest{AuthIndex: auths[0].Index, Model: "pelican-model", Effort: "low"}, http.StatusBadRequest},
		{"unsupported effort", pelicanRequest{AuthIndex: auths[0].Index, Model: "pelican-model", Prompt: "draw", Effort: "invalid"}, http.StatusBadRequest},
		{"unknown index", pelicanRequest{AuthIndex: "missing", Model: "pelican-model", Prompt: "draw", Effort: "low"}, http.StatusNotFound},
		{"mismatched name", pelicanRequest{AuthIndex: auths[0].Index, Name: auths[1].FileName, Model: "pelican-model", Prompt: "draw", Effort: "low"}, http.StatusNotFound},
		{"mismatched id and index", pelicanRequest{AuthID: auths[0].ID, AuthIndex: auths[1].Index, Model: "pelican-model", Prompt: "draw"}, http.StatusNotFound},
		{"mismatched id and name", pelicanRequest{AuthID: auths[0].ID, Name: auths[1].FileName, Model: "pelican-model", Prompt: "draw"}, http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := callPelican(t, router, tc.request)
			if response.Code != tc.status {
				t.Fatalf("status=%d want=%d body=%s", response.Code, tc.status, response.Body.String())
			}
		})
	}
	if len(executor.calls) != 0 {
		t.Fatalf("invalid target made upstream calls: %v", executor.calls)
	}
}

func TestPelicanV8CancelledRequestDoesNotGenerate(t *testing.T) {
	router, executor, auths := setupPelicanTest(t)
	executor.cancel = true
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	body, errMarshal := json.Marshal(pelicanRequest{AuthIndex: auths[0].Index, Model: "pelican-model", Prompt: "draw", Effort: "low"})
	if errMarshal != nil {
		t.Fatal(errMarshal)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/pelican", strings.NewReader(string(body))).WithContext(ctx))
	if recorder.Code < 400 {
		t.Fatalf("cancelled generation reported success: %s", recorder.Body.String())
	}
}

func TestPelicanAnswerRejectsIncompleteAndExtractsOnlyOutputText(t *testing.T) {
	for _, tc := range []struct {
		name     string
		response coreexecutor.Response
		answer   string
		wantErr  bool
	}{
		{"incomplete", coreexecutor.Response{Incomplete: true, Payload: []byte(`{"choices":[{"finish_reason":"stop","message":{"content":"partial answer"}}]}`)}, "", true},
		{"malformed", coreexecutor.Response{Payload: []byte(`{"choices":[`)}, "", true},
		{"truncated", coreexecutor.Response{Payload: []byte(`{"choices":[{"finish_reason":"length","message":{"content":"partial"}}]}`)}, "", true},
		{"content filtered", coreexecutor.Response{Payload: []byte(`{"choices":[{"finish_reason":"content_filter","message":{"content":"partial"}}]}`)}, "", true},
		{"tool only", coreexecutor.Response{Payload: []byte(`{"choices":[{"finish_reason":"tool_calls","message":{"content":"not a final answer"}}]}`)}, "", true},
		{"empty", coreexecutor.Response{Payload: []byte(`{"choices":[{"message":{"content":""}}]}`)}, "", true},
		{"reasoning only", coreexecutor.Response{Payload: []byte(`{"choices":[{"message":{"reasoning_content":"not final"}}]}`)}, "", true},
		{"text parts", coreexecutor.Response{Payload: []byte(`{"choices":[{"message":{"content":[{"type":"text","text":"<svg>"},{"type":"thinking","text":"private"},{"type":"text","text":"</svg>"}]}}]}`)}, "<svg></svg>", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			answer, _, _, errAnswer := pelicanAnswer(tc.response)
			if (errAnswer != nil) != tc.wantErr || answer != tc.answer {
				t.Fatalf("answer=%q err=%v", answer, errAnswer)
			}
		})
	}
}

func TestPelicanV8RejectsIncompleteArtifact(t *testing.T) {
	router, executor, auths := setupPelicanTest(t)
	// Translation can produce finish_reason=stop for an unknown native incomplete
	// reason, so the executor's pretranslation completeness marker is authoritative.
	executor.response.Incomplete = true
	response := callPelican(t, router, pelicanRequest{AuthID: auths[0].ID, Model: "pelican-model", Prompt: "draw"})
	if response.Code != http.StatusBadGateway || ojson.Get(response.Body.String(), "answer").Exists() || ojson.Get(response.Body.String(), "error").String() != "test generation was incomplete" {
		t.Fatalf("incomplete artifact was exposed: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestPelicanV8UpstreamUnauthorizedDoesNotInvalidateManagementSession(t *testing.T) {
	router, executor, auths := setupPelicanTest(t)
	executor.err = &coreauth.Error{HTTPStatus: http.StatusUnauthorized, Message: "upstream credential rejected"}
	response := callPelican(t, router, pelicanRequest{AuthIndex: auths[0].Index, Model: "pelican-model", Prompt: "Draw a pelican.", Effort: "medium"})
	if response.Code != http.StatusBadGateway || ojson.Get(response.Body.String(), "upstream_status").Int() != http.StatusUnauthorized {
		t.Fatalf("target error confused with management authentication: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestPelicanV8FailureDoesNotExposeCredentialDiagnostics(t *testing.T) {
	router, executor, auths := setupPelicanTest(t)
	executor.err = &coreauth.Error{HTTPStatus: http.StatusBadGateway, Message: `request failed https://oauth.example/callback?code=secret-code&state=secret-state access_token=secret-token Bearer secret-bearer`}
	response := callPelican(t, router, pelicanRequest{AuthID: auths[0].ID, Model: "pelican-model", Prompt: "draw"})
	if response.Code != http.StatusBadGateway || ojson.Get(response.Body.String(), "error").String() == "" {
		t.Fatalf("unexpected failure response: status=%d body=%s", response.Code, response.Body.String())
	}
	for _, secret := range []string{"oauth.example", "secret-code", "secret-state", "secret-token", "secret-bearer"} {
		if strings.Contains(response.Body.String(), secret) {
			t.Fatalf("credential diagnostic exposed %q: %s", secret, response.Body.String())
		}
	}
}

func TestPelicanV8DefaultsEffortAndBypassesHome(t *testing.T) {
	manager := coreauth.NewManager(nil, nil, nil)
	manager.SetConfig(&config.Config{Home: config.HomeConfig{Enabled: true}})
	executor := &pelicanTestExecutor{response: coreexecutor.Response{Payload: []byte(`{"choices":[{"message":{"content":"answer"}}]}`)}}
	manager.RegisterExecutor(executor)
	target, err := manager.Register(coreauth.WithSkipPersist(t.Context()), &coreauth.Auth{
		ID: t.Name(), Provider: executor.Identifier(), Status: coreauth.StatusActive,
		Attributes: map[string]string{coreauth.AttributeAuthKind: coreauth.AuthKindOAuth},
	})
	if err != nil {
		t.Fatal(err)
	}
	registry.GetGlobalRegistry().RegisterClient(target.ID, target.Provider, []*registry.ModelInfo{{ID: "pelican-model"}})
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(target.ID) })
	handler := &Handler{authManager: manager}
	router := gin.New()
	router.POST("/pelican", handler.PelicanTestV8)
	response := callPelican(t, router, pelicanRequest{AuthID: target.ID, Model: "pelican-model", Prompt: "draw"})
	if response.Code != http.StatusOK || len(executor.calls) != 1 || executor.calls[0] != target.ID || !executor.retryOff {
		t.Fatalf("local pinned generation failed: status=%d calls=%v body=%s", response.Code, executor.calls, response.Body.String())
	}
	if effort := ojson.ParseBytes(executor.request.Payload).Get("reasoning_effort").String(); effort != "medium" {
		t.Fatalf("default effort = %q, want medium", effort)
	}
}

func TestPelicanV8OAuthRecoveryStaysOnPinnedIdentity(t *testing.T) {
	router, executor, auths := setupPelicanTest(t)
	executor.oauthRecovery = true
	target := auths[1]
	response := callPelican(t, router, pelicanRequest{AuthID: target.ID, Model: "pelican-model", Prompt: "draw"})
	if response.Code != http.StatusOK || len(executor.calls) != 2 || executor.calls[0] != target.ID || executor.calls[1] != target.ID || len(executor.refreshCalls) != 1 || executor.refreshCalls[0] != target.ID {
		t.Fatalf("OAuth recovery escaped pinned target: status=%d calls=%v refreshes=%v body=%s", response.Code, executor.calls, executor.refreshCalls, response.Body.String())
	}
}
