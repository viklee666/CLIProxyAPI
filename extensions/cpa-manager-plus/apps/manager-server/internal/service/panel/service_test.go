package panel

import (
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestServeEmbeddedPanelAndAssets(t *testing.T) {
	service := New("", fstest.MapFS{
		"web/management.html":      &fstest.MapFile{Data: []byte("<html>panel</html>")},
		"web/assets/app-abc123.js": &fstest.MapFile{Data: []byte("console.log('ok')")},
	})
	writeError := func(w http.ResponseWriter, status int, err error) {
		http.Error(w, err.Error(), status)
	}

	htmlRes := httptest.NewRecorder()
	service.ServeManagementHTML(htmlRes, httptest.NewRequest(http.MethodGet, "/management.html", nil), writeError)
	if htmlRes.Code != http.StatusOK || htmlRes.Body.String() != "<html>panel</html>" {
		t.Fatalf("management response = %d %q", htmlRes.Code, htmlRes.Body.String())
	}

	assetRes := httptest.NewRecorder()
	service.ServeManagementAsset(assetRes, httptest.NewRequest(http.MethodGet, "/manager-assets/assets/app-abc123.js", nil), writeError)
	if assetRes.Code != http.StatusOK || assetRes.Body.String() != "console.log('ok')" {
		t.Fatalf("asset response = %d %q", assetRes.Code, assetRes.Body.String())
	}
	if got := assetRes.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if got := assetRes.Header().Get("Content-Type"); got != "text/javascript; charset=utf-8" && got != "application/javascript" {
		t.Fatalf("Content-Type = %q", got)
	}
}

func TestServeManagementAssetRejectsNonAssetPaths(t *testing.T) {
	service := New("", fstest.MapFS{
		"web/management.html": &fstest.MapFile{Data: []byte("secret")},
	})
	res := httptest.NewRecorder()
	service.ServeManagementAsset(
		res,
		httptest.NewRequest(http.MethodGet, "/manager-assets/../management.html", nil),
		func(w http.ResponseWriter, status int, err error) { http.Error(w, err.Error(), status) },
	)
	if res.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusNotFound)
	}
}

func TestServeManagementHTMLReportsMissingEmbeddedFile(t *testing.T) {
	service := New("", fstest.MapFS{})
	var gotErr error
	service.ServeManagementHTML(
		httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/management.html", nil),
		func(_ http.ResponseWriter, _ int, err error) { gotErr = err },
	)
	if !errors.Is(gotErr, fs.ErrNotExist) {
		t.Fatalf("error = %v, want fs.ErrNotExist", gotErr)
	}
}
