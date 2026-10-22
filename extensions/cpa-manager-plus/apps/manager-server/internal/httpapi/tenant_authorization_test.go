package httpapi

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	monitoringsvc "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/monitoring"
	usagesvc "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/usage"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/testutil"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/usage"
)

const tenantObservabilityFromMS int64 = 1_778_000_000_000

func TestTenantObservabilityAuthorization(t *testing.T) {
	handler, hashes := newTenantObservabilityHandler(t)
	for _, subject := range []struct {
		name  string
		count int64
	}{
		{name: "tenant-a", count: 2},
		{name: "tenant-b", count: 1},
		{name: "no-keys", count: 0},
	} {
		t.Run(subject.name, func(t *testing.T) {
			for _, selectors := range []bool{false, true} {
				t.Run(fmt.Sprintf("selectors=%t", selectors), func(t *testing.T) {
					body := tenantAnalyticsBody(t, selectors)
					rr := testutil.Request(t, handler, http.MethodPost, "/v0/tenant/monitoring/analytics", body, subject.name)
					testutil.RequireStatus(t, rr, http.StatusOK)
					assertTenantResponse(t, rr.Body.String(), subject.name, hashes)
					var result monitoringsvc.Response
					testutil.DecodeJSON(t, rr, &result)
					if result.Summary == nil || result.Summary.TotalCalls != subject.count || result.Summary.TotalTokens != subject.count*3 {
						t.Fatalf("summary = %#v, want %d calls", result.Summary, subject.count)
					}
					if result.Events == nil || int64(len(result.Events.Items)) != subject.count {
						t.Fatalf("events = %#v, want %d", result.Events, subject.count)
					}
					if result.DrilldownPreview == nil || int64(len(result.DrilldownPreview.Items)) != subject.count {
						t.Fatalf("drilldown = %#v, want %d", result.DrilldownPreview, subject.count)
					}
					if result.FilterOptions == nil {
						t.Fatal("filter options missing")
					}
					if selectors {
						want := []string(nil)
						if subject.count > 0 {
							want = []string{hashes[subject.name]}
						}
						if !slices.Equal(result.FilterOptions.APIKeyHashes, want) {
							t.Fatalf("selector keys = %v, want %v", result.FilterOptions.APIKeyHashes, want)
						}
					} else {
						var calls int64
						for _, row := range result.FilterOptions.APIKeyStats {
							calls += row.Calls
						}
						if calls != subject.count {
							t.Fatalf("filter option calls = %d, want %d", calls, subject.count)
						}
					}
				})
			}

			rr := testutil.Request(t, handler, http.MethodGet, fmt.Sprintf("/v0/tenant/dashboard/summary?today_start_ms=%d&now_ms=%d", tenantObservabilityFromMS, tenantObservabilityFromMS+3_600_000), "", subject.name)
			testutil.RequireStatus(t, rr, http.StatusOK)
			assertTenantResponse(t, rr.Body.String(), subject.name, hashes)
			var dashboard monitoringsvc.Response
			testutil.DecodeJSON(t, rr, &dashboard)
			if dashboard.Summary == nil || dashboard.Summary.TotalCalls != subject.count {
				t.Fatalf("dashboard summary = %#v", dashboard.Summary)
			}

			// A cursor obtained from the unrestricted administrator view must not
			// replace ownership predicates in the tenant count or page queries.
			adminPage := testutil.Request(t, handler, http.MethodGet, "/v0/management/usage?page_size=1", "", testutil.AdminKey)
			testutil.RequireStatus(t, adminPage, http.StatusOK)
			var adminUsage usagesvc.CompatibleUsageResponse
			testutil.DecodeJSON(t, adminPage, &adminUsage)
			if adminUsage.NextCursor == "" {
				t.Fatal("expected an administrator pagination cursor")
			}
			for _, suffix := range []string{"", "&cursor=" + adminUsage.NextCursor, "&page=2"} {
				path := "/v0/tenant/usage?page_size=1&api_key_hashes=" + hashes["admin"] + suffix
				rr := testutil.Request(t, handler, http.MethodGet, path, "", subject.name)
				testutil.RequireStatus(t, rr, http.StatusOK)
				assertTenantResponse(t, rr.Body.String(), subject.name, hashes)
				var result usagesvc.CompatibleUsageResponse
				testutil.DecodeJSON(t, rr, &result)
				if result.Total != subject.count || result.TotalRequests > 1 {
					t.Fatalf("usage total=%d page count=%d, want total %d", result.Total, result.TotalRequests, subject.count)
				}
			}
		})
	}

	for _, token := range []string{"", "invalid", testutil.AdminKey} {
		rr := testutil.Request(t, handler, http.MethodPost, "/v0/tenant/monitoring/analytics", tenantAnalyticsBody(t, false), token)
		testutil.RequireStatus(t, rr, http.StatusUnauthorized)
	}
}

func TestTenantAnalyticsUserFiltersCannotReplaceAuthorization(t *testing.T) {
	handler, hashes := newTenantObservabilityHandler(t)
	for _, selectors := range []bool{false, true} {
		for _, attack := range []string{"api_key_hashes", "search_api_key_hash", "search_query"} {
			t.Run(fmt.Sprintf("%s/selectors=%t", attack, selectors), func(t *testing.T) {
				var body map[string]any
				if err := json.Unmarshal([]byte(tenantAnalyticsBody(t, selectors)), &body); err != nil {
					t.Fatal(err)
				}
				switch attack {
				case "api_key_hashes":
					body["filters"].(map[string]any)[attack] = []string{hashes["tenant-b"], hashes["admin"]}
				case "search_api_key_hash":
					body[attack] = hashes["tenant-b"]
				case "search_query":
					body[attack] = "model-tenant-b"
				}
				encoded, err := json.Marshal(body)
				if err != nil {
					t.Fatal(err)
				}
				rr := testutil.Request(t, handler, http.MethodPost, "/v0/tenant/monitoring/analytics", string(encoded), "tenant-a")
				testutil.RequireStatus(t, rr, http.StatusOK)
				assertTenantResponse(t, rr.Body.String(), "tenant-a", hashes)
				var result monitoringsvc.Response
				testutil.DecodeJSON(t, rr, &result)
				if result.Summary == nil || result.Summary.TotalCalls != 0 || result.Events == nil || len(result.Events.Items) != 0 {
					t.Fatalf("foreign filter must intersect ownership: summary=%#v events=%#v", result.Summary, result.Events)
				}
			})
		}
	}
}

func TestAdminAnalyticsFilterOptionsRemainUnrestricted(t *testing.T) {
	handler, hashes := newTenantObservabilityHandler(t)
	for _, selectors := range []bool{false, true} {
		var body map[string]any
		if err := json.Unmarshal([]byte(tenantAnalyticsBody(t, selectors)), &body); err != nil {
			t.Fatal(err)
		}
		body["filters"].(map[string]any)["api_key_hashes"] = []string{hashes["tenant-a"]}
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rr := testutil.Request(t, handler, http.MethodPost, "/v0/management/monitoring/analytics", string(encoded), testutil.AdminKey)
		testutil.RequireStatus(t, rr, http.StatusOK)
		var result monitoringsvc.Response
		testutil.DecodeJSON(t, rr, &result)
		if result.Summary == nil || result.Summary.TotalCalls != 2 || result.FilterOptions == nil {
			t.Fatalf("admin response = %#v", result)
		}
		if selectors {
			if !slices.Contains(result.FilterOptions.APIKeyHashes, hashes["tenant-b"]) || !slices.Contains(result.FilterOptions.APIKeyHashes, hashes["admin"]) {
				t.Fatalf("admin selectors must ignore interactive key filter: %#v", result.FilterOptions)
			}
		} else if len(result.FilterOptions.ModelStats) != 4 {
			t.Fatalf("admin options must include all models: %#v", result.FilterOptions.ModelStats)
		}
	}
}

func tenantAnalyticsBody(t *testing.T, selectors bool) string {
	t.Helper()
	body := map[string]any{
		"from_ms": tenantObservabilityFromMS, "to_ms": tenantObservabilityFromMS + 3_600_000,
		"now_ms": tenantObservabilityFromMS + 3_600_000,
		// Neither JSON spelling can populate the server-only authorization field.
		"Authorization": nil, "authorization": map[string]any{"hashes": []string{"forged"}},
		"filters": map[string]any{"include_failed": true, "Authorization": nil},
		"include": map[string]any{
			"summary": true, "summary_percentiles": true, "summary_comparison": true,
			"timeline": true, "hourly_distribution": true, "heatmap": true, "anomaly_points": true,
			"model_share": true, "model_stats": true, "channel_share": true, "failure_sources": true,
			"account_stats": true, "credential_stats": true, "credential_timeline": true, "api_key_stats": true,
			"filter_options": true, "filter_selectors": selectors, "task_buckets": true, "recent_failures": 10,
			"events_page": map[string]any{"limit": 50}, "granularity": "hour",
			"drilldown_preview": map[string]any{"from_ms": tenantObservabilityFromMS - 3_600_000, "to_ms": tenantObservabilityFromMS + 7_200_000},
		},
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func assertTenantResponse(t *testing.T, body, subject string, hashes map[string]string) {
	t.Helper()
	for _, owner := range []string{"tenant-a", "tenant-b", "admin", "unknown"} {
		if owner == subject {
			continue
		}
		for _, marker := range []string{"model-" + owner, "account-" + owner, "auth-" + owner, "file-" + owner, hashes[owner]} {
			if marker != "" && strings.Contains(body, marker) {
				t.Fatalf("%s response contains foreign marker %q: %s", subject, marker, body)
			}
		}
	}
}

func newTenantObservabilityHandler(t *testing.T) (http.Handler, map[string]string) {
	t.Helper()
	cfg := testutil.NewConfig(t)
	cfg.TenantDBPath = filepath.Join(t.TempDir(), "tenant.sqlite")
	cfg.ClientAccessDBPath = filepath.Join(t.TempDir(), "client-access.sqlite")
	cfg.DashboardHourlyRollupEnabled = true
	tenantDB := openTenantFixtureDB(t, cfg.TenantDBPath)
	keyDB := openTenantFixtureDB(t, cfg.ClientAccessDBPath)
	execTenantFixtureSQL(t, tenantDB, `CREATE TABLE tenants (id INTEGER PRIMARY KEY, enabled INTEGER);
		CREATE TABLE tenant_sessions (tenant_id INTEGER, token_hash TEXT, expires_at_ms INTEGER)`)
	execTenantFixtureSQL(t, keyDB, `CREATE TABLE client_access_keys (id INTEGER PRIMARY KEY, tenant_id INTEGER, key_hash TEXT)`)
	hashes := make(map[string]string)
	for i, name := range []string{"tenant-a", "tenant-b", "no-keys", "admin"} {
		sum := sha256.Sum256([]byte("client-key-" + name))
		hashes[name] = hex.EncodeToString(sum[:])
		if name == "admin" {
			continue
		}
		id := i + 1
		tokenHash := sha256.Sum256([]byte(name))
		execTenantFixtureSQL(t, tenantDB, `INSERT INTO tenants VALUES (?, 1)`, id)
		execTenantFixtureSQL(t, tenantDB, `INSERT INTO tenant_sessions VALUES (?, ?, ?)`, id, hex.EncodeToString(tokenHash[:]), time.Now().Add(time.Hour).UnixMilli())
		if name != "no-keys" {
			execTenantFixtureSQL(t, keyDB, `INSERT INTO client_access_keys VALUES (?, ?, ?)`, id, id, hashes[name])
		}
	}
	handler, db := newCompatHandler(t, cfg, nil)
	events := make([]usage.Event, 0, 5)
	for i, owner := range []string{"tenant-a", "tenant-a", "tenant-b", "admin", "unknown"} {
		event := compatEvent(fmt.Sprintf("tenant-event-%d", i), int64(i+1)*1000)
		event.APIKeyHash = hashes[owner]
		event.Model = "model-" + owner
		event.AccountSnapshot = "account-" + owner
		event.AuthIndex = "auth-" + owner
		event.AuthFileSnapshot = "file-" + owner
		event.Source = "source-" + owner
		event.SourceHash = "source-hash-" + owner
		event.AuthProviderSnapshot = "provider-" + owner
		event.Failed = i == 1 || i == 2
		events = append(events, event)
	}
	if _, err := db.InsertEvents(context.Background(), events); err != nil {
		t.Fatalf("seed usage: %v", err)
	}
	// Populate the global hourly rollup to exercise the authorization-aware
	// eligibility guard rather than relying on an empty rollup fallback.
	for {
		result, err := db.CatchUpDashboardHourlyRollups(context.Background(), 100, tenantObservabilityFromMS+7_200_000)
		if err != nil {
			t.Fatalf("seed hourly rollup: %v", err)
		}
		if !result.Pending {
			break
		}
	}
	return handler, hashes
}

func openTenantFixtureDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if errClose := db.Close(); errClose != nil {
			t.Errorf("close fixture database: %v", errClose)
		}
	})
	return db
}

func execTenantFixtureSQL(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("seed fixture: %v", err)
	}
}
