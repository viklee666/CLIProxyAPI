package management

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestPatchAuthFileStatusTargetsAuthIndex(t *testing.T) {
	manager := coreauth.NewManager(nil, nil, nil)
	first := &coreauth.Auth{
		ID:       "auth-first",
		Index:    "index-first",
		FileName: "shared.json",
		Provider: "codex",
	}
	second := &coreauth.Auth{
		ID:       "auth-second",
		Index:    "index-second",
		FileName: "shared.json",
		Provider: "codex",
	}
	for _, auth := range []*coreauth.Auth{first, second} {
		if _, err := manager.Register(context.Background(), auth); err != nil {
			t.Fatalf("register auth: %v", err)
		}
	}

	h := &Handler{authManager: manager}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPatch,
		"/v0/management/auth-files/status",
		strings.NewReader(`{"name":"shared.json","auth_index":"index-second","disabled":true}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")

	h.PatchAuthFileStatus(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	updatedFirst, _ := manager.GetByID(first.ID)
	updatedSecond, _ := manager.GetByID(second.ID)
	if updatedFirst.Disabled {
		t.Fatal("first auth was disabled despite auth_index mismatch")
	}
	if !updatedSecond.Disabled || updatedSecond.Status != coreauth.StatusDisabled {
		t.Fatalf("second auth state = disabled:%v status:%s", updatedSecond.Disabled, updatedSecond.Status)
	}
}
