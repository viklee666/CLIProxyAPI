package httpapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/testutil"
)

func TestPelicanProxyRequiresAdminAndPreservesPinnedTarget(t *testing.T) {
	const path = "/v8/management/requests/pelican"
	const payload = `{"auth_index":"target-only","model":"pelican-model","prompt":"Draw a pelican.","effort":"high"}`
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body, errRead := io.ReadAll(r.Body)
		if errRead != nil || r.Method != http.MethodPost || r.URL.Path != path || string(body) != payload || r.Header.Get("Authorization") != "Bearer management-key" {
			t.Errorf("unexpected upstream request: method=%s path=%s body=%q err=%v", r.Method, r.URL.Path, body, errRead)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"auth_index":"target-only","answer":"<svg></svg>","model":"pelican-model","duration_ms":10}`))
	}))
	t.Cleanup(upstream.Close)
	handler := newTestHandler(t, upstream.URL, true)
	for _, key := range []string{"", "wrong-admin-key", testutil.AdminKey} {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(payload))
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		handler.ServeHTTP(recorder, req)
		if key != testutil.AdminKey {
			if recorder.Code != http.StatusUnauthorized || calls.Load() != 0 {
				t.Fatalf("unauthorized test reached upstream: status=%d calls=%d", recorder.Code, calls.Load())
			}
		} else if recorder.Code != http.StatusOK || calls.Load() != 1 || !strings.Contains(recorder.Body.String(), `"auth_index":"target-only"`) {
			t.Fatalf("pinned test was not proxied: status=%d calls=%d body=%s", recorder.Code, calls.Load(), recorder.Body.String())
		}
	}
}

func TestPelicanTargetDiscoveryProxyRequiresAdminAndPreservesSelector(t *testing.T) {
	const path = "/v8/management/requests/pelican"
	const query = "provider_kind=openai&config_index=3&key_index=1"
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != path || r.URL.RawQuery != query || r.Header.Get("Authorization") != "Bearer management-key" {
			t.Errorf("unexpected target discovery: method=%s url=%s", r.Method, r.URL)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"auth_id":"exact-runtime-id","auth_index":"shared-index","name":"exact-runtime-id","models":["only-this-target"]}`))
	}))
	t.Cleanup(upstream.Close)
	handler := newTestHandler(t, upstream.URL, true)
	for _, key := range []string{"", "tenant-a", "wrong-admin-key", testutil.AdminKey} {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path+"?"+query, nil)
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		handler.ServeHTTP(recorder, req)
		if key != testutil.AdminKey {
			if recorder.Code != http.StatusUnauthorized || calls.Load() != 0 {
				t.Fatalf("unauthorized discovery reached upstream: status=%d calls=%d", recorder.Code, calls.Load())
			}
		} else if recorder.Code != http.StatusOK || calls.Load() != 1 || !strings.Contains(recorder.Body.String(), `"auth_id":"exact-runtime-id"`) {
			t.Fatalf("target discovery was not proxied: status=%d calls=%d body=%s", recorder.Code, calls.Load(), recorder.Body.String())
		}
	}
}
