package management

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func registerTenantRuntimeAuth(t *testing.T, manager *coreauth.Manager, filePath string) *coreauth.Auth {
	t.Helper()
	if errWrite := os.WriteFile(filePath, []byte(`{"type":"claude"}`), 0o600); errWrite != nil {
		t.Fatalf("write tenant auth file: %v", errWrite)
	}
	auth := &coreauth.Auth{
		ID:       "tenant-private-auth",
		FileName: filepath.Base(filePath),
		Provider: "claude",
		Status:   coreauth.StatusActive,
		Attributes: map[string]string{
			"api_key":      "tenant-private-secret",
			"runtime_only": "true",
			"tenant_id":    "42",
			"path":         filePath,
		},
		Metadata: map[string]any{"type": "claude"},
	}
	if _, errRegister := manager.Register(context.Background(), auth); errRegister != nil {
		t.Fatalf("register tenant auth: %v", errRegister)
	}
	return auth
}

func TestSingleItemAuthFileMutationsHideTenantRuntimeAuth(t *testing.T) {
	authDir := t.TempDir()
	filePath := filepath.Join(authDir, "tenant.json")
	manager := coreauth.NewManager(nil, nil, nil)
	auth := registerTenantRuntimeAuth(t, manager, filePath)
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: authDir}, manager)
	h.tokenStore = &memoryAuthStore{}

	t.Run("status", func(t *testing.T) {
		rec := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(rec)
		ctx.Request = httptest.NewRequest(http.MethodPatch, "/v0/management/auth-files/status", strings.NewReader(`{"name":"tenant-private-auth","disabled":true}`))
		ctx.Request.Header.Set("Content-Type", "application/json")
		h.PatchAuthFileStatus(ctx)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
		}
		updated, ok := manager.GetByID(auth.ID)
		if !ok || updated == nil || updated.Disabled {
			t.Fatalf("tenant auth was mutated: ok=%v disabled=%v", ok, updated != nil && updated.Disabled)
		}
	})

	t.Run("fields", func(t *testing.T) {
		rec := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(rec)
		ctx.Request = httptest.NewRequest(http.MethodPatch, "/v0/management/auth-files/fields", strings.NewReader(`{"name":"tenant-private-auth","prefix":"leaked"}`))
		ctx.Request.Header.Set("Content-Type", "application/json")
		h.PatchAuthFileFields(ctx)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
		}
		updated, ok := manager.GetByID(auth.ID)
		if !ok || updated == nil || updated.Prefix == "leaked" {
			t.Fatalf("tenant auth fields were mutated: ok=%v prefix=%q", ok, updated.Prefix)
		}
	})

	t.Run("models", func(t *testing.T) {
		for _, name := range []string{auth.ID, auth.FileName} {
			rec := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(rec)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/auth-files/models?name="+url.QueryEscape(name), nil)
			h.GetAuthFileModels(ctx)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("name=%s status = %d, want %d; body=%s", name, rec.Code, http.StatusNotFound, rec.Body.String())
			}
		}
	})

	t.Run("delete", func(t *testing.T) {
		for _, name := range []string{auth.ID, auth.FileName} {
			rec := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(rec)
			ctx.Request = httptest.NewRequest(http.MethodDelete, "/v0/management/auth-files?name="+url.QueryEscape(name), nil)
			h.DeleteAuthFile(ctx)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("name=%s status = %d, want %d; body=%s", name, rec.Code, http.StatusNotFound, rec.Body.String())
			}
		}
		if _, errStat := os.Stat(filePath); errStat != nil {
			t.Fatalf("tenant auth file was removed: %v", errStat)
		}
		if _, ok := manager.GetByID(auth.ID); !ok {
			t.Fatal("tenant runtime auth was removed from the manager")
		}
	})
}
