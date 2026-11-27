package management

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestUploadAuthFile_BatchMultipart(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")

	authDir := t.TempDir()
	manager := coreauth.NewManager(nil, nil, nil)
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: authDir}, manager)

	files := []struct {
		name    string
		content string
	}{
		{name: "alpha.json", content: `{"type":"codex","email":"alpha@example.com"}`},
		{name: "beta.json", content: `{"type":"claude","email":"beta@example.com"}`},
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, file := range files {
		part, err := writer.CreateFormFile("file", file.name)
		if err != nil {
			t.Fatalf("failed to create multipart file: %v", err)
		}
		if _, err = part.Write([]byte(file.content)); err != nil {
			t.Fatalf("failed to write multipart content: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close multipart writer: %v", err)
	}

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/v0/management/auth-files", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	ctx.Request = req

	h.UploadAuthFile(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected upload status %d, got %d with body %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if got, ok := payload["uploaded"].(float64); !ok || int(got) != len(files) {
		t.Fatalf("expected uploaded=%d, got %#v", len(files), payload["uploaded"])
	}

	for _, file := range files {
		fullPath := filepath.Join(authDir, file.name)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			t.Fatalf("expected uploaded file %s to exist: %v", file.name, err)
		}
		if string(data) != file.content {
			t.Fatalf("expected file %s content %q, got %q", file.name, file.content, string(data))
		}
	}

	auths := manager.List()
	if len(auths) != len(files) {
		t.Fatalf("expected %d auth entries, got %d", len(files), len(auths))
	}
}

func TestUploadAuthFile_BatchMultipart_InvalidJSONDoesNotOverwriteExistingFile(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")

	authDir := t.TempDir()
	manager := coreauth.NewManager(nil, nil, nil)
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: authDir}, manager)

	existingName := "alpha.json"
	existingContent := `{"type":"codex","email":"alpha@example.com"}`
	if err := os.WriteFile(filepath.Join(authDir, existingName), []byte(existingContent), 0o600); err != nil {
		t.Fatalf("failed to seed existing auth file: %v", err)
	}

	files := []struct {
		name    string
		content string
	}{
		{name: existingName, content: `{"type":"codex"`},
		{name: "beta.json", content: `{"type":"claude","email":"beta@example.com"}`},
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, file := range files {
		part, err := writer.CreateFormFile("file", file.name)
		if err != nil {
			t.Fatalf("failed to create multipart file: %v", err)
		}
		if _, err = part.Write([]byte(file.content)); err != nil {
			t.Fatalf("failed to write multipart content: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close multipart writer: %v", err)
	}

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/v0/management/auth-files", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	ctx.Request = req

	h.UploadAuthFile(ctx)

	if rec.Code != http.StatusMultiStatus {
		t.Fatalf("expected upload status %d, got %d with body %s", http.StatusMultiStatus, rec.Code, rec.Body.String())
	}

	data, err := os.ReadFile(filepath.Join(authDir, existingName))
	if err != nil {
		t.Fatalf("expected existing auth file to remain readable: %v", err)
	}
	if string(data) != existingContent {
		t.Fatalf("expected existing auth file to remain %q, got %q", existingContent, string(data))
	}

	betaData, err := os.ReadFile(filepath.Join(authDir, "beta.json"))
	if err != nil {
		t.Fatalf("expected valid auth file to be created: %v", err)
	}
	if string(betaData) != files[1].content {
		t.Fatalf("expected beta auth file content %q, got %q", files[1].content, string(betaData))
	}
}

func TestDeleteAuthFile_BatchQuery(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")

	authDir := t.TempDir()
	files := []string{"alpha.json", "beta.json"}
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(authDir, name), []byte(`{"type":"codex"}`), 0o600); err != nil {
			t.Fatalf("failed to write auth file %s: %v", name, err)
		}
	}

	manager := coreauth.NewManager(nil, nil, nil)
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: authDir}, manager)
	h.tokenStore = &memoryAuthStore{}

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(
		http.MethodDelete,
		"/v0/management/auth-files?name="+url.QueryEscape(files[0])+"&name="+url.QueryEscape(files[1]),
		nil,
	)
	ctx.Request = req

	h.DeleteAuthFile(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected delete status %d, got %d with body %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if got, ok := payload["deleted"].(float64); !ok || int(got) != len(files) {
		t.Fatalf("expected deleted=%d, got %#v", len(files), payload["deleted"])
	}

	for _, name := range files {
		if _, err := os.Stat(filepath.Join(authDir, name)); !os.IsNotExist(err) {
			t.Fatalf("expected auth file %s to be removed, stat err: %v", name, err)
		}
	}
}

func TestPatchAuthFileStatusBatchReturnsPartialResults(t *testing.T) {
	manager := coreauth.NewManager(&memoryAuthStore{}, nil, nil)
	for _, auth := range []*coreauth.Auth{
		{ID: "alpha.json", FileName: "alpha.json", Index: "auth-a", Provider: "codex", Metadata: map[string]any{"type": "codex"}},
		{ID: "beta.json", FileName: "beta.json", Index: "auth-b", Provider: "claude", Metadata: map[string]any{"type": "claude"}},
	} {
		if _, errRegister := manager.Register(context.Background(), auth); errRegister != nil {
			t.Fatalf("register auth: %v", errRegister)
		}
	}
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, manager)

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPatch, "/v0/management/auth-files/status/batch", bytes.NewBufferString(`{
		"items":[{"name":"alpha.json"},{"name":"beta.json"},{"name":"missing.json"}],
		"disabled":true
	}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	h.PatchAuthFileStatusBatch(ctx)

	if rec.Code != http.StatusMultiStatus {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusMultiStatus, rec.Body.String())
	}
	for _, id := range []string{"alpha.json", "beta.json"} {
		auth, ok := manager.GetByID(id)
		if !ok || auth == nil || !auth.Disabled {
			t.Fatalf("auth %s was not disabled: %#v", id, auth)
		}
	}
	var payload struct {
		Updated int              `json:"updated"`
		Failed  []map[string]any `json:"failed"`
	}
	if errDecode := json.Unmarshal(rec.Body.Bytes(), &payload); errDecode != nil {
		t.Fatalf("decode response: %v", errDecode)
	}
	if payload.Updated != 2 || len(payload.Failed) != 1 {
		t.Fatalf("unexpected response: %#v", payload)
	}
}

func TestPatchAuthFileFieldsBatchUpdatesManyTargets(t *testing.T) {
	manager := coreauth.NewManager(&memoryAuthStore{}, nil, nil)
	for _, auth := range []*coreauth.Auth{
		{ID: "alpha.json", FileName: "alpha.json", Index: "auth-a", Provider: "codex", Metadata: map[string]any{"type": "codex"}},
		{ID: "beta.json", FileName: "beta.json", Index: "auth-b", Provider: "claude", Metadata: map[string]any{"type": "claude"}},
	} {
		if _, errRegister := manager.Register(context.Background(), auth); errRegister != nil {
			t.Fatalf("register auth: %v", errRegister)
		}
	}
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, manager)

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPatch, "/v0/management/auth-files/fields/batch", bytes.NewBufferString(`{
		"items":[{"name":"alpha.json"},{"name":"beta.json","auth_indices":["auth-b"]}],
		"fields":{"prefix":"team-a","priority":7,"websockets":false}
	}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	h.PatchAuthFileFieldsBatch(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	for _, id := range []string{"alpha.json", "beta.json"} {
		auth, ok := manager.GetByID(id)
		if !ok || auth == nil {
			t.Fatalf("auth %s missing", id)
		}
		if auth.Prefix != "team-a" || auth.Attributes["priority"] != "7" || auth.Attributes["websockets"] != "false" {
			t.Fatalf("auth %s fields not patched: %#v", id, auth)
		}
	}
}

func TestDownloadAuthFilesBatchStreamsZipAndFailureManifest(t *testing.T) {
	authDir := t.TempDir()
	for name, content := range map[string]string{
		"alpha.json": `{"type":"codex","email":"alpha@example.com"}`,
		"beta.json":  `{"type":"claude","email":"beta@example.com"}`,
	} {
		if errWrite := os.WriteFile(filepath.Join(authDir, name), []byte(content), 0o600); errWrite != nil {
			t.Fatalf("write %s: %v", name, errWrite)
		}
	}
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: authDir}, nil)

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v0/management/auth-files/download", bytes.NewBufferString(`{
		"names":["alpha.json","beta.json","missing.json"]
	}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	h.DownloadAuthFilesBatch(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if rec.Header().Get("X-Auth-Files-Included") != "2" || rec.Header().Get("X-Auth-Files-Failed") != "1" {
		t.Fatalf("unexpected count headers: %#v", rec.Header())
	}
	reader, errZip := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if errZip != nil {
		t.Fatalf("open zip: %v", errZip)
	}
	wantEntries := map[string]bool{"alpha.json": false, "beta.json": false, "_errors.json": false}
	for _, file := range reader.File {
		if _, ok := wantEntries[file.Name]; ok {
			wantEntries[file.Name] = true
		}
	}
	for name, found := range wantEntries {
		if !found {
			t.Fatalf("zip entry %s missing; entries=%#v", name, reader.File)
		}
	}
}
