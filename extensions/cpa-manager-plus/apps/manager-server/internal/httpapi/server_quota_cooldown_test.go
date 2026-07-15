package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/collector"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/testutil"
)

func TestServerCompatQuotaCooldownsList(t *testing.T) {
	cfg := testutil.NewConfig(t)
	db := testutil.NewStore(t, cfg)
	manager := collector.NewManager(cfg, db)
	handler := New(cfg, db, manager).Handler()

	now := int64(1_700_000_000_000)
	persisted, err := db.QuotaCooldowns.UpsertActive(context.Background(), model.QuotaCooldownUpsert{
		AuthFileName:    "codex-1.json",
		AuthIndex:       "0",
		Provider:        "codex",
		Owner:           model.QuotaCooldownOwnerUsage429,
		RecoverAtMS:     now + 3_600_000,
		DisabledAtMS:    now,
		AccountSnapshot: "should-not-leak",
		EventHash:       "should-not-leak",
	})
	if err != nil {
		t.Fatalf("seed cooldown: %v", err)
	}
	if persisted.RecoverAtMS != now+3_600_000 {
		t.Fatalf("seed recoverAtMs = %d", persisted.RecoverAtMS)
	}

	rr := testutil.Request(t, handler, http.MethodGet, "/usage-service/quota-cooldowns", "", testutil.AdminKey)
	testutil.RequireStatus(t, rr, http.StatusOK)

	var resp struct {
		Items []struct {
			AuthFileName string `json:"authFileName"`
			AuthIndex    string `json:"authIndex"`
			Provider     string `json:"provider"`
			Owner        string `json:"owner"`
			RecoverAtMs  int64  `json:"recoverAtMs"`
			DisabledAtMs int64  `json:"disabledAtMs"`
			CreatedAtMs  int64  `json:"createdAtMs"`
		} `json:"items"`
	}
	testutil.DecodeJSON(t, rr, &resp)
	if len(resp.Items) != 1 {
		t.Fatalf("items = %d, want 1, body = %s", len(resp.Items), rr.Body.String())
	}
	item := resp.Items[0]
	if item.AuthFileName != "codex-1.json" || item.Provider != "codex" || item.Owner != model.QuotaCooldownOwnerUsage429 {
		t.Fatalf("item = %#v", item)
	}
	if item.RecoverAtMs != now+3_600_000 || item.DisabledAtMs != now || item.CreatedAtMs <= 0 {
		t.Fatalf("timestamps = %#v", item)
	}
	// The read-only view must not leak internal/account-snapshot fields.
	if body := rr.Body.String(); containsInternalField(body) {
		t.Fatalf("response leaked internal fields, body = %s", body)
	}
}

func TestServerCompatQuotaCooldownsRequiresPanelAuth(t *testing.T) {
	cfg := testutil.NewConfig(t)
	db := testutil.NewStore(t, cfg)
	manager := collector.NewManager(cfg, db)
	handler := New(cfg, db, manager).Handler()

	noKey := testutil.Request(t, handler, http.MethodGet, "/usage-service/quota-cooldowns", "", "")
	testutil.RequireStatus(t, noKey, http.StatusUnauthorized)

	post := testutil.Request(t, handler, http.MethodPost, "/usage-service/quota-cooldowns", "", testutil.AdminKey)
	testutil.RequireStatus(t, post, http.StatusMethodNotAllowed)
}

func TestServerCompatQuotaCooldownsFiltersAndPaginates(t *testing.T) {
	cfg := testutil.NewConfig(t)
	db := testutil.NewStore(t, cfg)
	manager := collector.NewManager(cfg, db)
	handler := New(cfg, db, manager).Handler()

	for index, seed := range []model.QuotaCooldownUpsert{
		{AuthFileName: "codex-a.json", AuthIndex: "auth-a", AccountSnapshot: "alice@example.com", Provider: "codex", Owner: model.QuotaCooldownOwnerUsage429, RecoverAtMS: 100},
		{AuthFileName: "codex-b.json", AuthIndex: "auth-b", AccountSnapshot: "bob@example.com", Provider: "codex", Owner: model.QuotaCooldownOwnerCodexInspection, RecoverAtMS: 200},
		{AuthFileName: "xai-a.json", AuthIndex: "auth-x", AccountSnapshot: "xavier@example.com", Provider: "xai", Owner: model.QuotaCooldownOwnerXAIFreeUsage, RecoverAtMS: 300},
	} {
		seed.DisabledAtMS = int64(index + 1)
		if _, err := db.QuotaCooldowns.UpsertActive(context.Background(), seed); err != nil {
			t.Fatalf("seed %d: %v", index, err)
		}
	}

	rr := testutil.Request(t, handler, http.MethodGet, "/usage-service/quota-cooldowns?provider=codex&page=2&page_size=1", "", testutil.AdminKey)
	testutil.RequireStatus(t, rr, http.StatusOK)
	var page struct {
		Items []struct {
			AuthFileName string `json:"authFileName"`
		} `json:"items"`
		Total      int64 `json:"total"`
		Page       int   `json:"page"`
		PageSize   int   `json:"page_size"`
		TotalPages int   `json:"total_pages"`
		HasMore    bool  `json:"has_more"`
	}
	testutil.DecodeJSON(t, rr, &page)
	if len(page.Items) != 1 || page.Items[0].AuthFileName != "codex-b.json" || page.Total != 2 || page.Page != 2 || page.PageSize != 1 || page.TotalPages != 2 || page.HasMore {
		t.Fatalf("page = %#v", page)
	}

	authRR := testutil.Request(t, handler, http.MethodGet, "/usage-service/quota-cooldowns?auth=auth-x", "", testutil.AdminKey)
	testutil.RequireStatus(t, authRR, http.StatusOK)
	if !strings.Contains(authRR.Body.String(), `"authFileName":"xai-a.json"`) {
		t.Fatalf("auth filter body = %s", authRR.Body.String())
	}
	searchRR := testutil.Request(t, handler, http.MethodGet, "/usage-service/quota-cooldowns?search=alice", "", testutil.AdminKey)
	testutil.RequireStatus(t, searchRR, http.StatusOK)
	if !strings.Contains(searchRR.Body.String(), `"authFileName":"codex-a.json"`) || strings.Contains(searchRR.Body.String(), `"authFileName":"codex-b.json"`) {
		t.Fatalf("search filter body = %s", searchRR.Body.String())
	}
}

func containsInternalField(body string) bool {
	for _, needle := range []string{"accountSnapshot", "account_snapshot", "eventHash", "event_hash", "preDisabledState", "lastError"} {
		if strings.Contains(body, needle) {
			return true
		}
	}
	return false
}
