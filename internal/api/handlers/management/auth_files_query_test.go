package management

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/clientaccess"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func writeTestJSON(path string, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}

func TestListAuthFilesQueryPaginatesAndOmitsHeavyFields(t *testing.T) {
	t.Parallel()

	manager := coreauth.NewManager(nil, nil, nil)
	dir := t.TempDir()
	register := func(id, name, provider string, priority int) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := writeTestJSON(path, `{"type":"`+provider+`"}`); err != nil {
			t.Fatalf("write auth file: %v", err)
		}
		auth := &coreauth.Auth{
			ID:       id,
			FileName: name,
			Provider: provider,
			Metadata: map[string]any{"email": name + "@example.com"},
			Attributes: map[string]string{
				"path":     path,
				"priority": strconv.Itoa(priority),
			},
		}
		if _, err := manager.Register(context.Background(), auth); err != nil {
			t.Fatalf("register auth: %v", err)
		}
	}

	register("codex-b", "b.json", "codex", 10)
	register("claude-a", "a.json", "claude", 20)
	register("codex-c", "c.json", "codex", 30)

	h := &Handler{authManager: manager}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/auth-files?view=summary&page=1&page_size=2&sort=name", nil)

	h.ListAuthFiles(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}

	var response struct {
		Files      []map[string]any `json:"files"`
		Total      int              `json:"total"`
		Page       int              `json:"page"`
		PageSize   int              `json:"page_size"`
		TotalPages int              `json:"total_pages"`
		HasMore    bool             `json:"has_more"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Total != 3 || response.Page != 1 || response.PageSize != 2 || response.TotalPages != 2 || !response.HasMore {
		t.Fatalf("unexpected pagination: %#v", response)
	}
	if len(response.Files) != 2 || response.Files[0]["name"] != "a.json" || response.Files[1]["name"] != "b.json" {
		t.Fatalf("unexpected files: %#v", response.Files)
	}
	for _, file := range response.Files {
		if _, ok := file["path"]; ok {
			t.Fatalf("summary leaked path: %#v", file)
		}
		if _, ok := file["recent_requests"]; ok {
			t.Fatalf("summary included recent requests: %#v", file)
		}
		if _, ok := file["id_token"]; ok {
			t.Fatalf("summary included id token claims: %#v", file)
		}
	}
}

func TestListAuthFilesQueryFiltersByAuthIndex(t *testing.T) {
	t.Parallel()

	manager := coreauth.NewManager(nil, nil, nil)
	dir := t.TempDir()
	path := filepath.Join(dir, "target.json")
	if err := writeTestJSON(path, `{"type":"codex"}`); err != nil {
		t.Fatalf("write auth file: %v", err)
	}
	auth := &coreauth.Auth{
		ID:       "target",
		FileName: "target.json",
		Provider: "codex",
		Attributes: map[string]string{
			"path": path,
		},
	}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}
	index := auth.EnsureIndex()

	h := &Handler{authManager: manager}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/auth-files?view=snapshot&auth_index="+index, nil)

	h.ListAuthFiles(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Files []map[string]any `json:"files"`
		Total int              `json:"total"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Total != 1 || len(response.Files) != 1 || response.Files[0]["name"] != "target.json" {
		t.Fatalf("unexpected response: %#v", response)
	}
}

func TestListAuthFilesQueryFiltersByPlanType(t *testing.T) {
	t.Parallel()

	manager := coreauth.NewManager(nil, nil, nil)
	dir := t.TempDir()
	register := func(id, name, planType string) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := writeTestJSON(path, `{"type":"codex"}`); err != nil {
			t.Fatalf("write auth file: %v", err)
		}
		auth := &coreauth.Auth{
			ID:       id,
			FileName: name,
			Provider: "codex",
			Attributes: map[string]string{
				"path":      path,
				"plan_type": planType,
			},
		}
		if _, err := manager.Register(context.Background(), auth); err != nil {
			t.Fatalf("register auth: %v", err)
		}
	}

	register("plus-auth", "plus.json", "plus")
	register("team-auth", "team.json", "team")

	h := &Handler{authManager: manager}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/auth-files?view=summary&plan_type=team", nil)

	h.ListAuthFiles(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Files  []map[string]any `json:"files"`
		Total  int              `json:"total"`
		Facets struct {
			PlanTypes map[string]int `json:"plan_types"`
		} `json:"facets"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Total != 1 || len(response.Files) != 1 || response.Files[0]["name"] != "team.json" {
		t.Fatalf("unexpected response: %#v", response)
	}
	if response.Files[0]["plan_type"] != "team" {
		t.Fatalf("plan_type not exposed in summary: %#v", response.Files[0])
	}
	if response.Facets.PlanTypes["team"] != 1 || response.Facets.PlanTypes["plus"] != 0 {
		t.Fatalf("plan facets = %#v", response.Facets.PlanTypes)
	}
}

func TestListAuthFilesQueryFiltersByClientAccessGroup(t *testing.T) {
	t.Parallel()

	manager := coreauth.NewManager(nil, nil, nil)
	dir := t.TempDir()
	register := func(id, name string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := writeTestJSON(path, `{"type":"codex"}`); err != nil {
			t.Fatalf("write auth file: %v", err)
		}
		auth := &coreauth.Auth{
			ID:       id,
			FileName: name,
			Provider: "codex",
			Attributes: map[string]string{
				"path": path,
			},
		}
		if _, err := manager.Register(context.Background(), auth); err != nil {
			t.Fatalf("register auth: %v", err)
		}
		return auth.EnsureIndex()
	}

	groupIndex := register("group-auth", "group.json")
	register("other-auth", "other.json")

	clientAccessService, errNew := clientaccess.New(filepath.Join(t.TempDir(), "client-access.sqlite"))
	if errNew != nil {
		t.Fatalf("clientaccess.New() error = %v", errNew)
	}
	t.Cleanup(func() {
		if errClose := clientAccessService.Close(); errClose != nil {
			t.Fatalf("client access close error = %v", errClose)
		}
	})
	group, errGroup := clientAccessService.CreateGroup(context.Background(), clientaccess.GroupCreate{Name: "codex K12"})
	if errGroup != nil {
		t.Fatalf("create group: %v", errGroup)
	}
	if errBind := clientAccessService.ReplaceCredentialBindings(context.Background(), clientaccess.CredentialBindingBatch{
		AuthIndices: []string{groupIndex},
		Groups:      []clientaccess.CredentialGroupInput{{GroupID: group.ID, Priority: 10}},
	}); errBind != nil {
		t.Fatalf("replace bindings: %v", errBind)
	}

	h := &Handler{authManager: manager, clientAccess: clientAccessService}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/auth-files?view=summary&group_id="+strconv.FormatInt(group.ID, 10), nil)

	h.ListAuthFiles(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Files []map[string]any `json:"files"`
		Total int              `json:"total"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Total != 1 || len(response.Files) != 1 || response.Files[0]["name"] != "group.json" {
		t.Fatalf("unexpected response: %#v", response)
	}
}

func TestListAuthFilesQueryLargeSetReturnsBoundedPage(t *testing.T) {
	t.Parallel()

	manager := coreauth.NewManager(nil, nil, nil)
	path := filepath.Join(t.TempDir(), "shared.json")
	if err := writeTestJSON(path, `{"type":"codex"}`); err != nil {
		t.Fatalf("write auth file: %v", err)
	}
	for i := 0; i < 2000; i++ {
		name := fmt.Sprintf("account-%04d.json", i)
		auth := &coreauth.Auth{
			ID:       fmt.Sprintf("auth-%04d", i),
			FileName: name,
			Provider: "codex",
			Metadata: map[string]any{"email": fmt.Sprintf("user-%04d@example.com", i)},
			Attributes: map[string]string{
				"path":                          path,
				coreauth.AttributeAuthIndexSeed: name,
			},
		}
		if _, err := manager.Register(context.Background(), auth); err != nil {
			t.Fatalf("register auth %d: %v", i, err)
		}
	}

	h := &Handler{authManager: manager}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/auth-files?view=summary&page=2&page_size=25&sort=name", nil)

	h.ListAuthFiles(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Files []map[string]any `json:"files"`
		Total int              `json:"total"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Total != 2000 || len(response.Files) != 25 {
		t.Fatalf("unexpected response sizes: total=%d files=%d", response.Total, len(response.Files))
	}
	if recorder.Body.Len() > 100_000 {
		t.Fatalf("paged summary payload too large: %d bytes", recorder.Body.Len())
	}
}

func TestListAuthFilesQueryReturnsOnlyStatusesUpdatedAfterCursor(t *testing.T) {
	t.Parallel()

	manager := coreauth.NewManager(nil, nil, nil)
	dir := t.TempDir()
	path := filepath.Join(dir, "shared.json")
	if err := writeTestJSON(path, `{"type":"codex"}`); err != nil {
		t.Fatalf("write auth file: %v", err)
	}
	cursor := time.Now().Add(-time.Minute).Truncate(time.Millisecond)
	oldAuth := &coreauth.Auth{
		ID:         "old-auth",
		FileName:   "old.json",
		Provider:   "codex",
		UpdatedAt:  cursor.Add(-time.Second),
		Attributes: map[string]string{"path": path},
	}
	newAuth := &coreauth.Auth{
		ID:         "new-auth",
		FileName:   "new.json",
		Provider:   "codex",
		UpdatedAt:  cursor.Add(time.Second),
		Attributes: map[string]string{"path": path},
	}
	for _, auth := range []*coreauth.Auth{oldAuth, newAuth} {
		if _, err := manager.Register(context.Background(), auth); err != nil {
			t.Fatalf("register auth: %v", err)
		}
	}

	h := &Handler{authManager: manager}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodGet,
		"/v0/management/auth-files?view=snapshot&updated_after_ms="+strconv.FormatInt(cursor.UnixMilli(), 10),
		nil,
	)

	h.ListAuthFiles(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Files        []map[string]any `json:"files"`
		Total        int              `json:"total"`
		ServerTimeMS int64            `json:"server_time_ms"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Total != 1 || len(response.Files) != 1 || response.Files[0]["name"] != "new.json" {
		t.Fatalf("unexpected response: %#v", response)
	}
	if response.ServerTimeMS <= cursor.UnixMilli() {
		t.Fatalf("server time = %d, cursor = %d", response.ServerTimeMS, cursor.UnixMilli())
	}
}
