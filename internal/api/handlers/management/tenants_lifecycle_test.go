package management

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/clientaccess"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/tenant"
)

func TestDeleteTenantRevokesAccessBeforeResourceCleanup(t *testing.T) {
	handler, clientAccess := newClientAccessHandler(t)
	tenants, errNew := tenant.New(filepath.Join(t.TempDir(), "tenant.sqlite"))
	if errNew != nil {
		t.Fatal(errNew)
	}
	t.Cleanup(func() { _ = tenants.Close() })
	handler.SetTenantService(tenants, nil)
	clientAccess.SetTenantStatusResolver(tenants)
	ctx := context.Background()
	owner, _, errCreate := tenants.Create(ctx, tenant.CreateInput{DisplayName: "owner"})
	if errCreate != nil {
		t.Fatal(errCreate)
	}
	key, errKey := clientAccess.CreateTenantKey(ctx, owner.ID, clientaccess.KeyCreate{Name: "key"})
	if errKey != nil {
		t.Fatal(errKey)
	}
	token, _, errSession := tenants.IssueSession(ctx, owner.ID, 0)
	if errSession != nil {
		t.Fatal(errSession)
	}
	// Force the independent resource-store cleanup to fail after revocation.
	if errClose := clientAccess.Close(); errClose != nil {
		t.Fatal(errClose)
	}
	c, response := clientAccessContext(http.MethodDelete, "/v0/management/tenants/"+strconv.FormatInt(owner.ID, 10), nil, gin.Param{Key: "id", Value: strconv.FormatInt(owner.ID, 10)})
	handler.DeleteTenant(c)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("delete status = %d, want cleanup failure", response.Code)
	}
	remaining, errGet := tenants.Get(ctx, owner.ID)
	if errGet != nil || remaining.Enabled {
		t.Fatalf("partial deletion left tenant enabled: %v", errGet)
	}
	if _, errAuthenticate := tenants.AuthenticateSession(ctx, token); errAuthenticate == nil {
		t.Fatal("partial deletion retained the tenant session")
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	request.Header.Set("Authorization", "Bearer "+key.Secret)
	if _, authErr := clientAccess.Authenticate(ctx, request); authErr == nil {
		t.Fatal("partial deletion retained client-key access")
	}
}
