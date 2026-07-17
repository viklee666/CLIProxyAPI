package management

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/clientaccess"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestBulkReplaceClientAccessCredentialBindingsQuerySelection(t *testing.T) {
	handler, service := newClientAccessHandler(t)
	manager := coreauth.NewManager(nil, nil, nil)
	dir := t.TempDir()
	register := func(id, provider, planType string) string {
		t.Helper()
		path := filepath.Join(dir, id+".json")
		if errWrite := writeTestJSON(path, `{"type":"`+provider+`"}`); errWrite != nil {
			t.Fatalf("write auth file: %v", errWrite)
		}
		auth := &coreauth.Auth{
			ID:       id,
			FileName: id + ".json",
			Provider: provider,
			Attributes: map[string]string{
				"path":      path,
				"plan_type": planType,
			},
		}
		index := auth.EnsureIndex()
		if _, errRegister := manager.Register(context.Background(), auth); errRegister != nil {
			t.Fatalf("register auth: %v", errRegister)
		}
		return index
	}
	codexTeam := register("codex-team", "codex", "team")
	register("codex-plus", "codex", "plus")
	claudeTeam := register("claude-team", "claude", "team")
	register("gemini-team", "gemini", "team")
	handler.authManager = manager

	group, errGroup := service.CreateGroup(context.Background(), clientaccess.GroupCreate{Name: "selected"})
	if errGroup != nil {
		t.Fatalf("create group: %v", errGroup)
	}
	body := map[string]any{
		"selection": map[string]any{
			"mode":                  "query",
			"all":                   false,
			"providers":             []string{"CODEX", "claude"},
			"plan_types":            []string{"team"},
			"excluded_auth_indices": []string{codexTeam},
		},
		"groups": []map[string]any{{"group_id": group.ID, "priority": 25}},
	}
	ctx, recorder := clientAccessContext(http.MethodPost, "/v0/management/client-access/credential-bindings/bulk", body)
	handler.BulkReplaceClientAccessCredentialBindings(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("bulk replace status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var response clientAccessCredentialBindingsBulkResponse
	if errDecode := json.Unmarshal(recorder.Body.Bytes(), &response); errDecode != nil {
		t.Fatalf("decode response: %v", errDecode)
	}
	if response.Matched != 1 || response.Updated != 1 || response.Unchanged != 0 || response.Excluded != 1 {
		t.Fatalf("bulk response = %+v", response)
	}
	page, errList := service.ListCredentialBindings(context.Background(), clientaccess.ListOptions{Page: 1, PageSize: 20})
	if errList != nil {
		t.Fatalf("list bindings: %v", errList)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].AuthIndex != claudeTeam || page.Items[0].Priority != 25 {
		t.Fatalf("bindings = %+v", page)
	}

	ctx, recorder = clientAccessContext(http.MethodPost, "/v0/management/client-access/credential-bindings/bulk", body)
	handler.BulkReplaceClientAccessCredentialBindings(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("second bulk replace status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if errDecode := json.Unmarshal(recorder.Body.Bytes(), &response); errDecode != nil {
		t.Fatalf("decode second response: %v", errDecode)
	}
	if response.Matched != 1 || response.Updated != 0 || response.Unchanged != 1 || response.Excluded != 1 {
		t.Fatalf("second bulk response = %+v", response)
	}
}

func TestBulkReplaceClientAccessCredentialBindingsAllIgnoresFiltersAndSupportsDryRun(t *testing.T) {
	handler, service := newClientAccessHandler(t)
	manager := coreauth.NewManager(nil, nil, nil)
	dir := t.TempDir()
	indices := make([]string, 0, 3)
	for _, item := range []struct {
		id       string
		provider string
		plan     string
	}{{"one", "codex", "team"}, {"two", "claude", "plus"}, {"three", "gemini", "free"}} {
		path := filepath.Join(dir, item.id+".json")
		if errWrite := writeTestJSON(path, `{}`); errWrite != nil {
			t.Fatalf("write auth file: %v", errWrite)
		}
		auth := &coreauth.Auth{ID: item.id, FileName: item.id + ".json", Provider: item.provider, Attributes: map[string]string{"path": path, "plan_type": item.plan}}
		indices = append(indices, auth.EnsureIndex())
		if _, errRegister := manager.Register(context.Background(), auth); errRegister != nil {
			t.Fatalf("register auth: %v", errRegister)
		}
	}
	handler.authManager = manager
	group, errGroup := service.CreateGroup(context.Background(), clientaccess.GroupCreate{Name: "all"})
	if errGroup != nil {
		t.Fatalf("create group: %v", errGroup)
	}
	body := map[string]any{
		"selection": map[string]any{
			"mode":                  "query",
			"all":                   true,
			"providers":             []string{"missing"},
			"plan_types":            []string{"missing"},
			"excluded_auth_indices": []string{indices[0]},
		},
		"groups":  []map[string]any{{"group_id": group.ID, "priority": 1}},
		"dry_run": true,
	}
	ctx, recorder := clientAccessContext(http.MethodPost, "/v0/management/client-access/credential-bindings/bulk", body)
	handler.BulkReplaceClientAccessCredentialBindings(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("dry run status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var response clientAccessCredentialBindingsBulkResponse
	if errDecode := json.Unmarshal(recorder.Body.Bytes(), &response); errDecode != nil {
		t.Fatalf("decode response: %v", errDecode)
	}
	if response.Matched != 2 || response.Excluded != 1 || !response.DryRun {
		t.Fatalf("dry run response = %+v", response)
	}
	page, errList := service.ListCredentialBindings(context.Background(), clientaccess.ListOptions{Page: 1, PageSize: 20})
	if errList != nil || page.Total != 0 {
		t.Fatalf("dry run changed bindings: page=%+v err=%v", page, errList)
	}
}

func TestReplaceClientAccessGroupCredentialBindingsQuerySelectionPreservesOtherGroups(t *testing.T) {
	handler, service := newClientAccessHandler(t)
	manager := coreauth.NewManager(nil, nil, nil)
	dir := t.TempDir()
	register := func(id, provider, planType string) string {
		t.Helper()
		path := filepath.Join(dir, id+".json")
		if errWrite := writeTestJSON(path, `{}`); errWrite != nil {
			t.Fatalf("write auth file: %v", errWrite)
		}
		auth := &coreauth.Auth{
			ID:       id,
			FileName: id + ".json",
			Provider: provider,
			Attributes: map[string]string{
				"path":      path,
				"plan_type": planType,
			},
		}
		index := auth.EnsureIndex()
		if _, errRegister := manager.Register(context.Background(), auth); errRegister != nil {
			t.Fatalf("register auth: %v", errRegister)
		}
		return index
	}
	teamOne := register("team-one", "codex", "team")
	teamTwo := register("team-two", "codex", "team")
	register("plus-one", "codex", "plus")
	handler.authManager = manager

	first, errFirst := service.CreateGroup(context.Background(), clientaccess.GroupCreate{Name: "first"})
	if errFirst != nil {
		t.Fatalf("create first group: %v", errFirst)
	}
	second, errSecond := service.CreateGroup(context.Background(), clientaccess.GroupCreate{Name: "second"})
	if errSecond != nil {
		t.Fatalf("create second group: %v", errSecond)
	}
	if errReplace := service.ReplaceCredentialBindings(context.Background(), clientaccess.CredentialBindingBatch{
		AuthIndices: []string{teamOne},
		Groups:      []clientaccess.CredentialGroupInput{{GroupID: second.ID, Priority: 3}},
	}); errReplace != nil {
		t.Fatalf("seed second group: %v", errReplace)
	}

	body := map[string]any{
		"selection": map[string]any{
			"mode":                  "query",
			"all":                   false,
			"plan_types":            []string{"team"},
			"included_auth_indices": []string{teamOne, teamTwo},
			"excluded_auth_indices": []string{teamTwo},
		},
		"priority": 12,
	}
	ctx, recorder := clientAccessContext(http.MethodPut, "/v0/management/client-access/groups/1/credential-bindings", body)
	ctx.Params = append(ctx.Params, gin.Param{Key: "id", Value: strconv.FormatInt(first.ID, 10)})
	handler.ReplaceClientAccessGroupCredentialBindings(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("group query replace status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var response clientAccessCredentialBindingsBulkResponse
	if errDecode := json.Unmarshal(recorder.Body.Bytes(), &response); errDecode != nil {
		t.Fatalf("decode response: %v", errDecode)
	}
	if response.Matched != 1 || response.Updated != 1 || response.Excluded != 1 {
		t.Fatalf("group response = %+v", response)
	}
	page, errList := service.ListCredentialBindings(context.Background(), clientaccess.ListOptions{Page: 1, PageSize: 20})
	if errList != nil {
		t.Fatalf("list bindings: %v", errList)
	}
	if page.Total != 2 {
		t.Fatalf("binding total = %d, items=%+v", page.Total, page.Items)
	}
	foundFirst := false
	foundSecond := false
	for _, binding := range page.Items {
		if binding.AuthIndex != teamOne {
			t.Fatalf("unexpected auth index: %+v", binding)
		}
		if binding.GroupID == first.ID && binding.Priority == 12 {
			foundFirst = true
		}
		if binding.GroupID == second.ID && binding.Priority == 3 {
			foundSecond = true
		}
	}
	if !foundFirst || !foundSecond {
		t.Fatalf("bindings were not preserved: %+v", page.Items)
	}
}
