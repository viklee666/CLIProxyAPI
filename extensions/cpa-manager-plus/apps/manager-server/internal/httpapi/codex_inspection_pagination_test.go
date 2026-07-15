package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/testutil"
)

func TestCodexInspectionRunAndDetailPagination(t *testing.T) {
	handler, db := newCompatHandler(t, testutil.NewConfig(t), nil)
	ctx := context.Background()
	var newest model.CodexInspectionRun
	for index := 1; index <= 3; index++ {
		run, err := db.CreateCodexInspectionRun(ctx, model.CodexInspectionRun{
			TriggerType: model.CodexInspectionTriggerManual,
			Status:      model.CodexInspectionStatusCompleted,
			StartedAtMS: int64(index * 100),
		})
		if err != nil {
			t.Fatalf("create run %d: %v", index, err)
		}
		newest = run
	}
	for index := 1; index <= 3; index++ {
		if _, err := db.InsertCodexInspectionResult(ctx, model.CodexInspectionResult{
			RunID:          newest.ID,
			AccountKey:     fmt.Sprintf("account-%d", index),
			FileName:       fmt.Sprintf("auth-%d.json", index),
			DisplayAccount: fmt.Sprintf("user-%d@example.com", index),
			Provider:       "codex",
			Action:         "keep",
			CreatedAtMS:    int64(index),
		}); err != nil {
			t.Fatalf("insert result %d: %v", index, err)
		}
		if _, err := db.InsertCodexInspectionLog(ctx, model.CodexInspectionLog{
			RunID:       newest.ID,
			Level:       "info",
			Message:     fmt.Sprintf("log-%d", index),
			CreatedAtMS: int64(index),
		}); err != nil {
			t.Fatalf("insert log %d: %v", index, err)
		}
	}

	runsRR := testutil.Request(t, handler, http.MethodGet, "/v0/management/codex-inspection/runs?page=2&page_size=2", "", testutil.AdminKey)
	testutil.RequireStatus(t, runsRR, http.StatusOK)
	var runs struct {
		Items      []model.CodexInspectionRun `json:"items"`
		Page       int                        `json:"page"`
		PageSize   int                        `json:"page_size"`
		Total      int64                      `json:"total"`
		TotalPages int                        `json:"total_pages"`
		HasMore    bool                       `json:"has_more"`
	}
	testutil.DecodeJSON(t, runsRR, &runs)
	if len(runs.Items) != 1 || runs.Page != 2 || runs.PageSize != 2 || runs.Total != 3 || runs.TotalPages != 2 || runs.HasMore {
		t.Fatalf("runs page = %#v", runs)
	}

	detailPath := "/v0/management/codex-inspection/runs/" + strconv.FormatInt(newest.ID, 10) + "?results_page=2&results_page_size=2&include_logs=false"
	detailRR := testutil.Request(t, handler, http.MethodGet, detailPath, "", testutil.AdminKey)
	testutil.RequireStatus(t, detailRR, http.StatusOK)
	var detail struct {
		Results           []model.CodexInspectionResult `json:"results"`
		Logs              []model.CodexInspectionLog    `json:"logs"`
		ResultsPagination struct {
			Page       int   `json:"page"`
			PageSize   int   `json:"page_size"`
			Total      int64 `json:"total"`
			TotalPages int   `json:"total_pages"`
			HasMore    bool  `json:"has_more"`
		} `json:"results_pagination"`
		LogsPagination *struct{} `json:"logs_pagination"`
	}
	testutil.DecodeJSON(t, detailRR, &detail)
	if len(detail.Results) != 1 || len(detail.Logs) != 0 || detail.LogsPagination != nil || detail.ResultsPagination.Page != 2 || detail.ResultsPagination.PageSize != 2 || detail.ResultsPagination.Total != 3 || detail.ResultsPagination.TotalPages != 2 || detail.ResultsPagination.HasMore {
		t.Fatalf("detail page = %#v", detail)
	}
}

func TestCodexInspectionActionsRejectTooManyResultIDs(t *testing.T) {
	handler, _ := newCompatHandler(t, testutil.NewConfig(t), nil)
	ids := make([]string, 201)
	for index := range ids {
		ids[index] = strconv.Itoa(index + 1)
	}
	body := `{"resultIds":[` + strings.Join(ids, ",") + `]}`
	rr := testutil.Request(t, handler, http.MethodPost, "/v0/management/codex-inspection/runs/1/actions", body, testutil.AdminKey)
	testutil.RequireStatus(t, rr, http.StatusBadRequest)
	var payload map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&payload); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if !strings.Contains(fmt.Sprint(payload["error"]), "less than or equal to 200") {
		t.Fatalf("error payload = %#v", payload)
	}
}
