package apikeyalias

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/app"
	adminauthsvc "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/adminauth"
	apikeyaliassvc "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/apikeyalias"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/testutil"
)

func TestHandleAPIKeyAliasesUsesPaginationAndReturnsOnlySavedItems(t *testing.T) {
	cfg := testutil.NewConfig(t)
	st := testutil.NewStore(t, cfg)
	items := []store.APIKeyAlias{
		{APIKeyHash: strings.Repeat("a", 64), Alias: "Alpha"},
		{APIKeyHash: strings.Repeat("b", 64), Alias: "Beta"},
		{APIKeyHash: strings.Repeat("c", 64), Alias: "Gamma"},
	}
	if err := st.UpsertAPIKeyAliases(context.Background(), items); err != nil {
		t.Fatalf("seed aliases: %v", err)
	}
	handler := &Handler{App: &app.Context{
		Config:             cfg,
		AdminAuthService:   adminauthsvc.New(cfg, st),
		APIKeyAliasService: apikeyaliassvc.New(st),
	}}

	req := httptest.NewRequest(http.MethodGet, "/v0/management/api-key-aliases?search=a&page=2&page_size=1", nil)
	req.Header.Set("Authorization", "Bearer "+testutil.AdminKey)
	recorder := httptest.NewRecorder()
	handler.Handle(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var page apikeyaliassvc.ListResponse
	if err := json.NewDecoder(recorder.Body).Decode(&page); err != nil {
		t.Fatalf("decode page: %v", err)
	}
	if page.Total != 3 || page.Page != 2 || page.PageSize != 1 || len(page.Items) != 1 {
		t.Fatalf("page = %#v", page)
	}

	newHash := strings.Repeat("d", 64)
	req = httptest.NewRequest(http.MethodPut, "/v0/management/api-key-aliases", strings.NewReader(`{"items":[{"apiKeyHash":"`+newHash+`","alias":"Delta"}]}`))
	req.Header.Set("Authorization", "Bearer "+testutil.AdminKey)
	req.Header.Set("Content-Type", "application/json")
	recorder = httptest.NewRecorder()
	handler.Handle(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("PUT status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var saved struct {
		Items   []store.APIKeyAlias `json:"items"`
		Updated int                 `json:"updated"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&saved); err != nil {
		t.Fatalf("decode saved: %v", err)
	}
	if saved.Updated != 1 || len(saved.Items) != 1 || saved.Items[0].APIKeyHash != newHash {
		t.Fatalf("saved = %#v", saved)
	}
}
