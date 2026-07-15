package modelprice

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/app"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	adminauthsvc "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/adminauth"
	modelpricesvc "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/modelprice"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/testutil"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/usage"
)

func TestHandleUsageSummaryUsesQueryLimitAndPanelAuthorization(t *testing.T) {
	cfg := testutil.NewConfig(t)
	cfg.QueryLimit = 1
	st := testutil.NewStore(t, cfg)
	if _, err := st.UsageEvents.InsertBatch(context.Background(), []usage.Event{
		{EventHash: "older", TimestampMS: 100, Timestamp: "2026-01-01T00:00:00Z", Model: "gpt-old", CreatedAtMS: 100},
		{EventHash: "newer", TimestampMS: 200, Timestamp: "2026-01-01T00:00:01Z", Model: "gpt-new", CreatedAtMS: 200},
	}); err != nil {
		t.Fatalf("insert events: %v", err)
	}

	handler := &Handler{App: &app.Context{
		Config:            cfg,
		AdminAuthService:  adminauthsvc.New(cfg, st),
		ModelPriceService: modelpricesvc.New(st, nil),
	}}

	unauthorized := httptest.NewRecorder()
	handler.Handle(unauthorized, httptest.NewRequest(http.MethodGet, "/v0/management/model-prices/usage-summary", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d body = %s", unauthorized.Code, unauthorized.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/v0/management/model-prices/usage-summary", nil)
	req.Header.Set("Authorization", "Bearer "+testutil.AdminKey)
	recorder := httptest.NewRecorder()
	handler.Handle(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}

	var summary model.ModelUsageSummary
	if err := json.NewDecoder(recorder.Body).Decode(&summary); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if summary.SampledEvents != 1 || summary.TotalEvents != 2 || !summary.Truncated {
		t.Fatalf("summary metadata = %#v", summary)
	}
	if len(summary.Models) != 1 || summary.Models[0].Model != "gpt-new" || summary.Models[0].Calls != 1 || summary.Models[0].RequestedCalls != 1 {
		t.Fatalf("models = %#v", summary.Models)
	}
}

func TestHandleModelPricesUsesSQLPaginationAndSearch(t *testing.T) {
	cfg := testutil.NewConfig(t)
	st := testutil.NewStore(t, cfg)
	if err := st.SaveModelPrices(context.Background(), map[string]model.ModelPrice{
		"claude-a": {Prompt: 1},
		"gpt-a":    {Prompt: 2, Source: "openai"},
		"gpt-b":    {Prompt: 3, Source: "openai"},
	}); err != nil {
		t.Fatalf("save prices: %v", err)
	}
	handler := &Handler{App: &app.Context{
		Config:            cfg,
		AdminAuthService:  adminauthsvc.New(cfg, st),
		ModelPriceService: modelpricesvc.New(st, nil),
	}}

	req := httptest.NewRequest(http.MethodGet, "/v0/management/model-prices?search=gpt&page=2&page_size=1", nil)
	req.Header.Set("Authorization", "Bearer "+testutil.AdminKey)
	recorder := httptest.NewRecorder()
	handler.Handle(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var page modelpricesvc.ListResponse
	if err := json.NewDecoder(recorder.Body).Decode(&page); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if page.Total != 2 || page.Page != 2 || page.PageSize != 1 || page.HasMore || len(page.Prices) != 1 {
		t.Fatalf("page = %#v", page)
	}
	if _, ok := page.Prices["gpt-b"]; !ok {
		t.Fatalf("prices = %#v, want gpt-b", page.Prices)
	}
}
