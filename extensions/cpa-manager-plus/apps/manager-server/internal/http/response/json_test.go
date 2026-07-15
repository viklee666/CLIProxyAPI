package response

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSONRejectsOversizedAndMultipleValues(t *testing.T) {
	t.Run("content length", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(`{"value":"too-large"}`))
		recorder := httptest.NewRecorder()
		var payload map[string]any
		if DecodeJSON(recorder, req, &payload, JSONDecodeOptions{MaxBytes: 4}) {
			t.Fatal("oversized body was accepted")
		}
		if recorder.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
		}
	})

	t.Run("multiple values", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(`{} {}`))
		recorder := httptest.NewRecorder()
		var payload map[string]any
		if DecodeJSON(recorder, req, &payload, JSONDecodeOptions{}) {
			t.Fatal("multiple JSON values were accepted")
		}
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
		}
	})

	t.Run("streamed body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(`{"value":"too-large"}`))
		req.ContentLength = -1
		recorder := httptest.NewRecorder()
		var payload map[string]any
		if DecodeJSON(recorder, req, &payload, JSONDecodeOptions{MaxBytes: 4}) {
			t.Fatal("oversized streamed body was accepted")
		}
		if recorder.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
		}
	})
}

func TestDecodeJSONAllowsOptionalEmptyBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	recorder := httptest.NewRecorder()
	var payload map[string]any
	if !DecodeJSON(recorder, req, &payload, JSONDecodeOptions{AllowEmpty: true}) {
		t.Fatalf("empty body rejected: status = %d body = %s", recorder.Code, recorder.Body.String())
	}
}
