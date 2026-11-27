package management

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestImportVertexCredentialRejectsOversizedFile(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "service-account.json")
	if err != nil {
		t.Fatalf("create multipart file: %v", err)
	}
	if _, err = part.Write(bytes.Repeat([]byte("x"), maxVertexImportFileBytes+1)); err != nil {
		t.Fatalf("write multipart file: %v", err)
	}
	if err = writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, nil)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v0/management/vertex/import", &body)
	ctx.Request.Header.Set("Content-Type", writer.FormDataContentType())

	h.ImportVertexCredential(ctx)

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusRequestEntityTooLarge, recorder.Body.String())
	}
}
