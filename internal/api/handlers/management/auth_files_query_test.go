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
	"strings"
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

func TestListAuthFilesQueryDoesNotExposeAgentIdentitySecrets(t *testing.T) {
	t.Parallel()

	manager := coreauth.NewManager(nil, nil, nil)
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.json")
	if err := writeTestJSON(path, `{"type":"codex","agent_private_key":"private-secret","task_id":"task-secret"}`); err != nil {
		t.Fatalf("write auth file: %v", err)
	}
	auth := &coreauth.Auth{
		ID:       "agent-auth",
		FileName: "agent.json",
		Provider: "codex",
		Metadata: map[string]any{
			"auth_kind":         "agent_identity",
			"agent_runtime_id":  "runtime-id",
			"agent_private_key": "private-secret",
			"task_id":           "task-secret",
			"email":             "agent@example.com",
		},
		Attributes: map[string]string{"path": path},
	}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	h := &Handler{authManager: manager}
	for _, view := range []string{"summary", "detail"} {
		t.Run(view, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/auth-files?view="+view, nil)

			h.ListAuthFiles(ctx)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
			}
			body := recorder.Body.String()
			for _, secret := range []string{"private-secret", "task-secret", "AgentAssertion"} {
				if strings.Contains(body, secret) {
					t.Fatalf("%s response leaked %q: %s", view, secret, body)
				}
			}
		})
	}
}

func TestListAuthFilesQueryOmitsTenantRuntimeCredentials(t *testing.T) {
	t.Parallel()

	manager := coreauth.NewManager(nil, nil, nil)
	auth := &coreauth.Auth{
		ID:       "tenant-private-auth",
		Provider: "claude",
		Attributes: map[string]string{
			"api_key":      "tenant-private-secret",
			"runtime_only": "true",
			"tenant_id":    "42",
		},
	}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("register tenant auth: %v", err)
	}

	h := &Handler{authManager: manager}
	for _, view := range []string{"summary", "detail"} {
		t.Run(view, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/auth-files?view="+view, nil)

			h.ListAuthFiles(ctx)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
			}
			if strings.Contains(recorder.Body.String(), "tenant-private-secret") {
				t.Fatalf("%s response leaked tenant API key: %s", view, recorder.Body.String())
			}
			var response struct {
				Total int `json:"total"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if response.Total != 0 {
				t.Fatalf("tenant runtime credential appeared in %s response: %s", view, recorder.Body.String())
			}
		})
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
		SnapshotETag string           `json:"snapshot_etag"`
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
	if response.SnapshotETag == "" {
		t.Fatal("snapshot etag is empty")
	}
}

func TestAuthFileSnapshotETagIsOrderIndependentAndIdentitySensitive(t *testing.T) {
	t.Parallel()

	first := authFileCandidate{
		name:      "one.json",
		provider:  "codex",
		planType:  "plus",
		authIndex: "auth-one",
		status:    "active",
	}
	second := authFileCandidate{
		name:      "two.json",
		provider:  "gemini",
		planType:  "pro",
		authIndex: "auth-two",
	}
	base := authFileSnapshotETag([]authFileCandidate{first, second})
	if base == "" {
		t.Fatal("snapshot etag is empty")
	}
	if reordered := authFileSnapshotETag([]authFileCandidate{second, first}); reordered != base {
		t.Fatalf("etag changed after reorder: %q != %q", reordered, base)
	}
	first.status = "disabled"
	if statusOnly := authFileSnapshotETag([]authFileCandidate{first, second}); statusOnly != base {
		t.Fatalf("status-only change altered identity etag: %q != %q", statusOnly, base)
	}
	first.planType = "team"
	if changed := authFileSnapshotETag([]authFileCandidate{first, second}); changed == base {
		t.Fatal("plan identity change did not alter etag")
	}
}

func TestListAuthFilesWithoutQueryUsesBoundedDefaultPage(t *testing.T) {
	manager := coreauth.NewManager(nil, nil, nil)
	for i := 0; i < defaultAuthFilesPageSize+25; i++ {
		name := fmt.Sprintf("auth-%03d.json", i)
		if _, errRegister := manager.Register(context.Background(), &coreauth.Auth{
			ID:       name,
			FileName: name,
			Provider: "codex",
			Attributes: map[string]string{
				"path": filepath.Join(t.TempDir(), name),
			},
			UpdatedAt: time.Now(),
		}); errRegister != nil {
			t.Fatalf("register %s: %v", name, errRegister)
		}
	}
	h := NewHandlerWithoutConfigFilePath(nil, manager)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/auth-files", nil)

	h.ListAuthFiles(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Files    []map[string]any `json:"files"`
		Total    int              `json:"total"`
		PageSize int              `json:"page_size"`
		HasMore  bool             `json:"has_more"`
	}
	if errDecode := json.Unmarshal(recorder.Body.Bytes(), &response); errDecode != nil {
		t.Fatalf("decode response: %v", errDecode)
	}
	if len(response.Files) != defaultAuthFilesPageSize || response.Total != defaultAuthFilesPageSize+25 || response.PageSize != defaultAuthFilesPageSize || !response.HasMore {
		t.Fatalf("unexpected bounded response: %#v", response)
	}
}

func TestCachedAuthFileCandidatesReuseSnapshotUntilInvalidated(t *testing.T) {
	manager := coreauth.NewManager(nil, nil, nil)
	if _, errRegister := manager.Register(context.Background(), &coreauth.Auth{
		ID:       "cached.json",
		FileName: "cached.json",
		Provider: "codex",
		Attributes: map[string]string{
			"path": filepath.Join(t.TempDir(), "cached.json"),
		},
	}); errRegister != nil {
		t.Fatalf("register auth: %v", errRegister)
	}
	h := NewHandlerWithoutConfigFilePath(nil, manager)
	now := time.Now()
	first, firstETag := h.cachedAuthFileCandidatesFromManager(now)
	second, secondETag := h.cachedAuthFileCandidatesFromManager(now.Add(time.Second))
	if len(first) != 1 || len(second) != 1 || first[0].auth != second[0].auth || firstETag != secondETag {
		t.Fatalf("candidate cache was not reused: first=%#v second=%#v", first, second)
	}
	h.invalidateAuthFileCandidateCatalog()
	third, _ := h.cachedAuthFileCandidatesFromManager(now.Add(time.Second))
	if len(third) != 1 || third[0].auth == first[0].auth {
		t.Fatalf("candidate cache was not rebuilt after invalidation")
	}
}
