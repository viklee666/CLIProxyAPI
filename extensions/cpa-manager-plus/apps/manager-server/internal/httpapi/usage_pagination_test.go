package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/testutil"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/usage"
)

func TestCompatibleUsagePaginationSupportsCursorAndLegacyLimit(t *testing.T) {
	handler, db := newCompatHandler(t, testutil.NewConfig(t), nil)
	events := []usage.Event{
		compatEvent("page-one", 10),
		compatEvent("page-two", 20),
		compatEvent("page-three", 30),
	}
	if _, err := db.InsertEvents(context.Background(), events); err != nil {
		t.Fatalf("insert events: %v", err)
	}

	type pageResponse struct {
		usage.Payload
		Page       int    `json:"page"`
		PageSize   int    `json:"page_size"`
		NextCursor string `json:"next_cursor"`
		Total      int64  `json:"total"`
		TotalPages int    `json:"total_pages"`
		HasMore    bool   `json:"has_more"`
	}

	firstRR := testutil.Request(t, handler, http.MethodGet, "/v0/management/usage?limit=2", "", testutil.AdminKey)
	testutil.RequireStatus(t, firstRR, http.StatusOK)
	var first pageResponse
	if err := json.NewDecoder(firstRR.Body).Decode(&first); err != nil {
		t.Fatalf("decode first page: %v", err)
	}
	if first.Page != 1 || first.PageSize != 2 || first.Total != 3 || first.TotalPages != 2 || !first.HasMore || first.NextCursor == "" {
		t.Fatalf("first page metadata = %#v", first)
	}
	if first.TotalRequests != 2 {
		t.Fatalf("first page total_requests = %d, want 2", first.TotalRequests)
	}

	secondRR := testutil.Request(t, handler, http.MethodGet, "/v0/management/usage?page_size=2&cursor="+url.QueryEscape(first.NextCursor), "", testutil.AdminKey)
	testutil.RequireStatus(t, secondRR, http.StatusOK)
	var second pageResponse
	if err := json.NewDecoder(secondRR.Body).Decode(&second); err != nil {
		t.Fatalf("decode second page: %v", err)
	}
	if second.Total != 3 || second.TotalRequests != 1 || second.HasMore || second.NextCursor != "" {
		t.Fatalf("second page = %#v", second)
	}

	invalidRR := testutil.Request(t, handler, http.MethodGet, "/v0/management/usage?cursor=invalid", "", testutil.AdminKey)
	testutil.RequireStatus(t, invalidRR, http.StatusBadRequest)
}

func TestCompatibleUsagePaginationClampsPageSize(t *testing.T) {
	handler, _ := newCompatHandler(t, testutil.NewConfig(t), nil)
	rr := testutil.Request(t, handler, http.MethodGet, "/v0/management/usage?page_size=999999", "", testutil.AdminKey)
	testutil.RequireStatus(t, rr, http.StatusOK)
	var response struct {
		PageSize int `json:"page_size"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.PageSize != 500 {
		t.Fatalf("page_size = %d, want 500", response.PageSize)
	}
}
