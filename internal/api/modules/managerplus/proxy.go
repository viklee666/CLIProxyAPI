// Package managerplus provides the optional in-container bridge to CPA Manager Plus.
package managerplus

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

var managerPathPrefixes = []string{
	"/manager-assets",
	"/usage-service",
	"/v0/management/account-action-candidates",
	"/v0/management/api-key-aliases",
	"/v0/management/codex-inspection",
	"/v0/management/dashboard",
	"/v0/management/model-prices",
	"/v0/management/monitoring",
	"/v0/management/usage",
	"/v0/tenant/dashboard",
	"/v0/tenant/monitoring",
	"/v0/tenant/usage",
}

var managerExactPaths = map[string]struct{}{
	"/health":          {},
	"/management.html": {},
	"/user":            {},
	"/setup":           {},
	"/status":          {},
}

// NewMiddleware returns a Gin middleware that forwards Manager Plus-owned paths
// to the embedded companion process while leaving all CLIProxyAPI routes intact.
func NewMiddleware(rawTarget string) (gin.HandlerFunc, error) {
	target, err := url.Parse(strings.TrimSpace(rawTarget))
	if err != nil {
		return nil, fmt.Errorf("parse CPA Manager Plus URL: %w", err)
	}
	if target.Scheme != "http" && target.Scheme != "https" {
		return nil, fmt.Errorf("CPA Manager Plus URL must use http or https")
	}
	if target.Host == "" {
		return nil, fmt.Errorf("CPA Manager Plus URL must include a host")
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, proxyErr error) {
		log.WithError(proxyErr).WithField("path", r.URL.Path).Error("CPA Manager Plus proxy failed")
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "manager_plus_unavailable"})
	}

	return func(c *gin.Context) {
		if c == nil || c.Request == nil || c.Request.URL == nil || !ShouldProxyPath(c.Request.URL.Path) {
			c.Next()
			return
		}
		proxy.ServeHTTP(c.Writer, c.Request)
		c.Abort()
	}, nil
}

// ShouldProxyPath reports whether a request belongs to the embedded Manager Plus component.
func ShouldProxyPath(path string) bool {
	cleaned := strings.TrimRight(path, "/")
	if cleaned == "" {
		cleaned = "/"
	}
	if _, ok := managerExactPaths[cleaned]; ok {
		return true
	}
	for _, prefix := range managerPathPrefixes {
		if cleaned == prefix || strings.HasPrefix(cleaned, prefix+"/") {
			return true
		}
	}
	return false
}
