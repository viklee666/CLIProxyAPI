package api

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/api/handlers/management"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func newPelicanManagementTestServer(t *testing.T, manager *coreauth.Manager) (*Server, string) {
	t.Helper()
	t.Setenv("MANAGEMENT_PASSWORD", "")
	gin.SetMode(gin.TestMode)
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("port: 8317\nrequest-retry: 3\nremote-management: {secret-key: test-password}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.AuthDir = t.TempDir()
	cfg.Plugins.Dir = filepath.Dir(path)
	h := management.NewHandler(cfg, path, manager)
	h.SetLocalPassword("test-password")
	s := &Server{cfg: cfg, engine: gin.New(), mgmt: h}
	s.managementRoutesEnabled.Store(true)
	s.registerManagementRoutes()
	return s, path
}

func TestManagementV8RoutesShareAccessControl(t *testing.T) {
	for _, tc := range []struct {
		name          string
		enabled, home bool
		key, remote   string
		want          int
	}{
		{"authorized", true, false, "test-password", "127.0.0.1:1234", 0},
		{"missing key", true, false, "", "127.0.0.1:1234", http.StatusUnauthorized},
		{"invalid key", true, false, "wrong-password", "127.0.0.1:1234", http.StatusUnauthorized},
		{"disabled", false, false, "test-password", "127.0.0.1:1234", http.StatusNotFound},
		{"home", true, true, "test-password", "127.0.0.1:1234", http.StatusNotFound},
		{"remote disabled", true, false, "test-password", "192.0.2.10:1234", http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newPelicanManagementTestServer(t, nil)
			s.managementRoutesEnabled.Store(tc.enabled)
			s.cfg.Home.Enabled = tc.home
			for _, route := range []struct {
				method, path string
				authorized   int
			}{
				{http.MethodGet, "/v0/management/config", http.StatusOK},
				{http.MethodGet, "/v0/management/plugins", http.StatusOK},
				{http.MethodGet, "/v8/management/requests/pelican", http.StatusServiceUnavailable},
				{http.MethodPost, "/v8/management/requests/pelican", http.StatusServiceUnavailable},
			} {
				req := httptest.NewRequest(route.method, route.path, nil)
				req.RemoteAddr = tc.remote
				if tc.key != "" {
					req.Header.Set("Authorization", "Bearer "+tc.key)
				}
				recorder := httptest.NewRecorder()
				s.engine.ServeHTTP(recorder, req)
				want := tc.want
				if want == 0 {
					want = route.authorized
				}
				if recorder.Code != want {
					t.Fatalf("%s %s: status=%d want=%d body=%s", route.method, route.path, recorder.Code, want, recorder.Body.String())
				}
			}
		})
	}
}

func TestManagementV8RetainsOnlyCustomPelicanContract(t *testing.T) {
	s, path := newPelicanManagementTestServer(t, nil)
	s.registerManagementRoutes()
	routes := make(map[string]bool)
	v8Routes := 0
	for _, route := range s.engine.Routes() {
		routes[route.Method+" "+route.Path] = true
		if strings.HasPrefix(route.Path, "/v8/management") {
			v8Routes++
		}
	}
	for _, route := range []string{
		"GET /v8/management/requests/pelican", "POST /v8/management/requests/pelican",
		"GET /v0/management/config", "GET /v0/management/config.yaml", "PUT /v0/management/config.yaml",
		"PUT /v0/management/request-retry", "GET /v0/management/auth-files", "POST /v0/management/auth-files",
		"DELETE /v0/management/auth-files", "PATCH /v0/management/auth-files/status", "PATCH /v0/management/auth-files/fields",
		"GET /v0/management/codex-auth-url", "POST /v0/management/oauth-callback", "GET /v0/management/get-auth-status",
	} {
		if !routes[route] {
			t.Errorf("missing route %s", route)
		}
	}
	if v8Routes != 2 {
		t.Fatalf("v8 route count=%d, want only Pelican discovery and generation", v8Routes)
	}
	baseline, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, url := range []string{"/v8/management/config", "/v8/management/credentials", "/v8/management/oauth/auth-url", "/v8/management/oauth/siwc/status"} {
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("Authorization", "Bearer test-password")
		response := httptest.NewRecorder()
		s.engine.ServeHTTP(response, req)
		if response.Code != http.StatusNotFound {
			t.Fatalf("removed upstream route %s returned %d", url, response.Code)
		}
	}
	saved, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(saved, baseline) {
		t.Fatalf("compatibility requests changed the legacy configuration: %v", err)
	}
}

func TestManagementV8PelicanAcceptsLegacyAuthPasteWorkflow(t *testing.T) {
	manager := coreauth.NewManager(nil, nil, nil)
	s, path := newPelicanManagementTestServer(t, manager)
	baseline, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const name = "cockpit-codex.json"
	const content = `{"type":"codex","access_token":"pasted-token","refresh_token":"pasted-refresh","email":"operator@example.invalid"}`
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v0/management/auth-files", &body)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Authorization", "Bearer test-password")
	req.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	s.engine.ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("legacy paste upload status=%d body=%s", response.Code, response.Body.String())
	}
	target, ok := manager.GetByID(name)
	if !ok || target == nil {
		t.Fatal("pasted credential was not registered")
	}
	req = httptest.NewRequest(http.MethodGet, "/v8/management/requests/pelican?name="+name, nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Authorization", "Bearer test-password")
	response = httptest.NewRecorder()
	s.engine.ServeHTTP(response, req)
	if response.Code != http.StatusOK || ojson.Get(response.Body.String(), "auth_id").String() != target.ID {
		t.Fatalf("pasted target discovery status=%d body=%s", response.Code, response.Body.String())
	}
	savedAuth, err := os.ReadFile(filepath.Join(s.cfg.AuthDir, name))
	if err != nil || ojson.GetBytes(savedAuth, "type").String() != "codex" || ojson.GetBytes(savedAuth, "provider").Exists() {
		t.Fatalf("paste unexpectedly migrated the auth-file schema: %v", err)
	}
	savedConfig, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(savedConfig, baseline) {
		t.Fatalf("paste or Pelican discovery changed the configuration: %v", err)
	}
}
