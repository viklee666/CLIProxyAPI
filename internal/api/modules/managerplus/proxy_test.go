package managerplus

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestShouldProxyPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{path: "/management.html", want: true},
		{path: "/manager-assets/assets/app.js", want: true},
		{path: "/usage-service/info", want: true},
		{path: "/v0/management/dashboard/summary", want: true},
		{path: "/v0/management/usage/events", want: true},
		{path: "/v0/management/usage-statistics-enabled", want: false},
		{path: "/v0/management/auth-files", want: false},
		{path: "/v1/chat/completions", want: false},
		{path: "/healthz", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := ShouldProxyPath(tt.path); got != tt.want {
				t.Fatalf("ShouldProxyPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestMiddlewareProxiesOnlyManagerPaths(t *testing.T) {
	gin.SetMode(gin.TestMode)
	observed := make(chan *http.Request, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed <- r.Clone(r.Context())
		w.Header().Set("X-Manager-Plus", "true")
		_, _ = io.WriteString(w, "proxied")
	}))
	t.Cleanup(upstream.Close)

	middleware, err := NewMiddleware(upstream.URL)
	if err != nil {
		t.Fatalf("NewMiddleware() error = %v", err)
	}
	engine := gin.New()
	engine.Use(middleware)
	engine.Any("/*path", func(c *gin.Context) {
		c.String(http.StatusOK, "cli")
	})
	bridge := httptest.NewServer(engine)
	t.Cleanup(bridge.Close)

	proxiedReq, err := http.NewRequest(http.MethodPost, bridge.URL+"/v0/management/monitoring/events?limit=10", strings.NewReader("payload"))
	if err != nil {
		t.Fatalf("new proxied request: %v", err)
	}
	proxiedReq.Header.Set("Authorization", "Bearer key")
	proxiedRes, err := http.DefaultClient.Do(proxiedReq)
	if err != nil {
		t.Fatalf("proxied request: %v", err)
	}
	defer proxiedRes.Body.Close()
	proxiedBody, err := io.ReadAll(proxiedRes.Body)
	if err != nil {
		t.Fatalf("read proxied response: %v", err)
	}
	if proxiedRes.StatusCode != http.StatusOK || string(proxiedBody) != "proxied" {
		t.Fatalf("proxied response = %d %q", proxiedRes.StatusCode, proxiedBody)
	}
	got := <-observed
	if got.URL.Path != "/v0/management/monitoring/events" || got.URL.RawQuery != "limit=10" {
		t.Fatalf("upstream URL = %s", got.URL.String())
	}
	if got.Header.Get("Authorization") != "Bearer key" {
		t.Fatalf("Authorization = %q", got.Header.Get("Authorization"))
	}

	directRes, err := http.Get(bridge.URL + "/v0/management/auth-files")
	if err != nil {
		t.Fatalf("direct request: %v", err)
	}
	defer directRes.Body.Close()
	directBody, err := io.ReadAll(directRes.Body)
	if err != nil {
		t.Fatalf("read direct response: %v", err)
	}
	if directRes.StatusCode != http.StatusOK || string(directBody) != "cli" {
		t.Fatalf("direct response = %d %q", directRes.StatusCode, directBody)
	}
}
