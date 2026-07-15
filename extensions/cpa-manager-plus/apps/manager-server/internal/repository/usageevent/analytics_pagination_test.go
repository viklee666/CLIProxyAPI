package usageevent

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/usage"
)

func TestAggregatePagesBoundFinalEntitiesAndReportTotal(t *testing.T) {
	repo := newAnalyticsPreaggregationRepo(t)
	ctx := context.Background()
	base := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	events := make([]usage.Event, 0, 6)
	for index := 0; index < 4; index++ {
		timestamp := base.Add(time.Duration(index) * time.Minute)
		events = append(events, usage.Event{
			EventHash:        fmt.Sprintf("aggregate-page-%d", index),
			TimestampMS:      timestamp.UnixMilli(),
			Timestamp:        timestamp.Format(time.RFC3339Nano),
			Model:            fmt.Sprintf("model-%d", index),
			APIKeyHash:       fmt.Sprintf("api-%d", index),
			AccountSnapshot:  fmt.Sprintf("account-%d", index),
			AuthFileSnapshot: fmt.Sprintf("credential-%d.json", index),
			AuthIndex:        fmt.Sprintf("auth-%d", index),
			Source:           fmt.Sprintf("source-%d", index),
			SourceHash:       fmt.Sprintf("source-hash-%d", index),
			InputTokens:      int64(10 + index),
			OutputTokens:     2,
			TotalTokens:      int64(12 + index),
			CreatedAtMS:      timestamp.UnixMilli(),
		})
	}
	// The newest account/credential/API key has two model rows. A page size of
	// one must still return the complete entity rather than one raw group row.
	latest := base.Add(5 * time.Minute)
	events = append(events, usage.Event{
		EventHash:        "aggregate-page-extra-model",
		TimestampMS:      latest.UnixMilli(),
		Timestamp:        latest.Format(time.RFC3339Nano),
		Model:            "model-extra",
		APIKeyHash:       "api-3",
		AccountSnapshot:  "account-3",
		AuthFileSnapshot: "credential-3.json",
		AuthIndex:        "auth-3",
		Source:           "source-3",
		SourceHash:       "source-hash-3",
		InputTokens:      20,
		OutputTokens:     3,
		TotalTokens:      23,
		CreatedAtMS:      latest.UnixMilli(),
	})
	if _, err := repo.InsertBatch(ctx, events); err != nil {
		t.Fatalf("insert events: %v", err)
	}
	filter := AnalyticsFilter{
		FromMS:        base.UnixMilli(),
		ToMS:          base.Add(time.Hour).UnixMilli(),
		IncludeFailed: true,
	}

	models, err := repo.ModelStatsPageWithFilter(ctx, filter, 2, 2)
	if err != nil {
		t.Fatalf("model page: %v", err)
	}
	if models.Total != 5 || models.Count != 2 || len(models.Items) != 2 {
		t.Fatalf("model page = %#v", models)
	}

	accounts, err := repo.AccountModelStatsPageWithFilter(ctx, filter, 1, 0)
	if err != nil {
		t.Fatalf("account page: %v", err)
	}
	if accounts.Total != 4 || accounts.Count != 1 || len(accounts.Items) != 2 {
		t.Fatalf("account page = %#v", accounts)
	}

	credentials, err := repo.CredentialModelStatsPageWithFilter(ctx, filter, 1, 0)
	if err != nil {
		t.Fatalf("credential page: %v", err)
	}
	if credentials.Total != 4 || credentials.Count != 1 || len(credentials.Items) != 2 {
		t.Fatalf("credential page = %#v", credentials)
	}

	apiKeys, err := repo.APIKeyModelStatsPageWithFilter(ctx, filter, 1, 0)
	if err != nil {
		t.Fatalf("api key page: %v", err)
	}
	if apiKeys.Total != 4 || apiKeys.Count != 1 || len(apiKeys.Items) != 2 {
		t.Fatalf("api key page = %#v", apiKeys)
	}
}

func TestAPIKeyAggregatePageSearchRunsBeforePagination(t *testing.T) {
	repo := newAnalyticsPreaggregationRepo(t)
	ctx := context.Background()
	base := time.Date(2026, time.July, 2, 0, 0, 0, 0, time.UTC)
	events := []usage.Event{
		{EventHash: "search-a", TimestampMS: base.UnixMilli(), Timestamp: base.Format(time.RFC3339Nano), Model: "gpt", APIKeyHash: "team-alpha", TotalTokens: 1, CreatedAtMS: base.UnixMilli()},
		{EventHash: "search-b", TimestampMS: base.Add(time.Minute).UnixMilli(), Timestamp: base.Add(time.Minute).Format(time.RFC3339Nano), Model: "gpt", APIKeyHash: "team-beta", TotalTokens: 1, CreatedAtMS: base.Add(time.Minute).UnixMilli()},
	}
	if _, err := repo.InsertBatch(ctx, events); err != nil {
		t.Fatalf("insert events: %v", err)
	}
	page, err := repo.APIKeyModelStatsPageWithFilter(ctx, AnalyticsFilter{
		FromMS:                base.Add(-time.Minute).UnixMilli(),
		ToMS:                  base.Add(time.Hour).UnixMilli(),
		IncludeFailed:         true,
		APIKeyAggregateSearch: "alpha",
	}, 10, 0)
	if err != nil {
		t.Fatalf("api key search page: %v", err)
	}
	if page.Total != 1 || page.Count != 1 || len(page.Items) != 1 || page.Items[0].APIKeyHash != "team-alpha" {
		t.Fatalf("api key search page = %#v", page)
	}
}

func TestAPIKeyAggregatePageKeepsFallbackIdentitiesDistinct(t *testing.T) {
	repo := newAnalyticsPreaggregationRepo(t)
	ctx := context.Background()
	base := time.Date(2026, time.July, 3, 0, 0, 0, 0, time.UTC)
	events := []usage.Event{
		{EventHash: "fallback-a", TimestampMS: base.UnixMilli(), Timestamp: base.Format(time.RFC3339Nano), Provider: "codex", Model: "gpt", Source: "source-a", TotalTokens: 1, CreatedAtMS: base.UnixMilli()},
		{EventHash: "fallback-b", TimestampMS: base.Add(time.Minute).UnixMilli(), Timestamp: base.Add(time.Minute).Format(time.RFC3339Nano), Provider: "codex", Model: "gpt", Source: "source-b", TotalTokens: 1, CreatedAtMS: base.Add(time.Minute).UnixMilli()},
	}
	if _, err := repo.InsertBatch(ctx, events); err != nil {
		t.Fatalf("insert events: %v", err)
	}
	page, err := repo.APIKeyModelStatsPageWithFilter(ctx, AnalyticsFilter{
		FromMS:        base.Add(-time.Minute).UnixMilli(),
		ToMS:          base.Add(time.Hour).UnixMilli(),
		IncludeFailed: true,
	}, 10, 0)
	if err != nil {
		t.Fatalf("fallback api key page: %v", err)
	}
	if page.Total != 2 || page.Count != 2 || len(page.Items) != 2 {
		t.Fatalf("fallback api key page = %#v", page)
	}
}
