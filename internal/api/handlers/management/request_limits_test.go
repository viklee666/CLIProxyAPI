package management

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestManagementRequestBodyLimitAndFrozenExclusions(t *testing.T) {
	h := &Handler{}
	router := gin.New()
	router.Use(h.RequestBodyLimitMiddleware())
	router.POST("/v0/management/debug", func(c *gin.Context) {
		data, _ := io.ReadAll(c.Request.Body)
		c.String(http.StatusOK, strconv.Itoa(len(data)))
	})
	router.POST("/v0/management/api-call", func(c *gin.Context) {
		data, _ := io.ReadAll(c.Request.Body)
		c.String(http.StatusOK, strconv.Itoa(len(data)))
	})

	oversized := bytes.Repeat([]byte("x"), maxDefaultManagementRequestBytes+1)
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v0/management/debug", bytes.NewReader(oversized))
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("default route status = %d, want %d; body=%s", recorder.Code, http.StatusRequestEntityTooLarge, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/v0/management/api-call", bytes.NewReader(oversized))
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK || recorder.Body.String() != strconv.Itoa(len(oversized)) {
		t.Fatalf("api-call was limited: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
