package management

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/clientaccess"
)

func newClientAccessHandler(t *testing.T) (*Handler, *clientaccess.Service) {
	t.Helper()
	service, errNew := clientaccess.New(filepath.Join(t.TempDir(), "client-access.sqlite"))
	if errNew != nil {
		t.Fatalf("clientaccess.New() error = %v", errNew)
	}
	t.Cleanup(func() {
		if errClose := service.Close(); errClose != nil {
			t.Fatalf("client access close error = %v", errClose)
		}
	})
	return &Handler{clientAccess: service}, service
}

func clientAccessContext(method, target string, body any, params ...gin.Param) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	var encoded []byte
	if body != nil {
		encoded, _ = json.Marshal(body)
	}
	ctx.Request = httptest.NewRequest(method, target, bytes.NewReader(encoded))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Params = params
	return ctx, recorder
}

func TestClientAccessManagementCRUDAndBindings(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, _ := newClientAccessHandler(t)

	ctx, recorder := clientAccessContext(http.MethodPost, "/v0/management/client-access/groups", map[string]any{"name": "premium"})
	handler.CreateClientAccessGroup(ctx)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create group status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var group clientaccess.Group
	if errDecode := json.Unmarshal(recorder.Body.Bytes(), &group); errDecode != nil {
		t.Fatalf("decode group: %v", errDecode)
	}

	ctx, recorder = clientAccessContext(http.MethodPatch, "/v0/management/client-access/groups/1", map[string]any{"description": "primary pool"}, gin.Param{Key: "id", Value: "1"})
	handler.UpdateClientAccessGroup(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("update group status = %d, body=%s", recorder.Code, recorder.Body.String())
	}

	ctx, recorder = clientAccessContext(http.MethodPost, "/v0/management/client-access/keys", map[string]any{
		"name": "desktop", "secret": "sk-cpa-management-test", "allow_all_groups": false, "group_ids": []int64{group.ID},
	})
	handler.CreateClientAccessKey(ctx)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create key status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var key clientaccess.CreatedKey
	if errDecode := json.Unmarshal(recorder.Body.Bytes(), &key); errDecode != nil {
		t.Fatalf("decode key: %v", errDecode)
	}
	if key.Secret != "sk-cpa-management-test" || key.ID == 0 {
		t.Fatalf("created key = %+v", key)
	}

	ctx, recorder = clientAccessContext(http.MethodPut, "/v0/management/client-access/credential-bindings", map[string]any{
		"auth_indices": []string{"auth-a", "auth-b"},
		"groups":       []map[string]any{{"group_id": group.ID, "priority": 30}},
	})
	handler.ReplaceClientAccessCredentialBindings(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("replace bindings status = %d, body=%s", recorder.Code, recorder.Body.String())
	}

	ctx, recorder = clientAccessContext(http.MethodGet, "/v0/management/client-access/credential-bindings?page=1&page_size=10&auth_indices=auth-b", nil)
	handler.ListClientAccessCredentialBindings(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("list bindings status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var bindings clientaccess.Page[clientaccess.CredentialBinding]
	if errDecode := json.Unmarshal(recorder.Body.Bytes(), &bindings); errDecode != nil {
		t.Fatalf("decode bindings: %v", errDecode)
	}
	if bindings.Total != 1 || len(bindings.Items) != 1 || bindings.Items[0].AuthIndex != "auth-b" {
		t.Fatalf("bindings = %+v", bindings)
	}

	ctx, recorder = clientAccessContext(http.MethodGet, "/v0/management/client-access/keys?page=1&page_size=10&search=desk", nil)
	handler.ListClientAccessKeys(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("list keys status = %d, body=%s", recorder.Code, recorder.Body.String())
	}

	ctx, recorder = clientAccessContext(http.MethodDelete, "/v0/management/client-access/keys/1", nil, gin.Param{Key: "id", Value: "1"})
	handler.DeleteClientAccessKey(ctx)
	if ctx.Writer.Status() != http.StatusNoContent {
		t.Fatalf("delete key status = %d, body=%s", ctx.Writer.Status(), recorder.Body.String())
	}
	ctx, recorder = clientAccessContext(http.MethodDelete, "/v0/management/client-access/groups/1", nil, gin.Param{Key: "id", Value: "1"})
	handler.DeleteClientAccessGroup(ctx)
	if ctx.Writer.Status() != http.StatusNoContent {
		t.Fatalf("delete group status = %d, body=%s", ctx.Writer.Status(), recorder.Body.String())
	}
}

func TestClientAccessManagementDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, recorder := clientAccessContext(http.MethodGet, "/v0/management/client-access/groups", nil)
	(&Handler{}).ListClientAccessGroups(ctx)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
}
