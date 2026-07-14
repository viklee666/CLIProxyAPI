package panel

import (
	"bytes"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Service struct {
	PanelPath string
	Embedded  fs.FS
}

func New(panelPath string, embedded fs.FS) *Service {
	return &Service{PanelPath: panelPath, Embedded: embedded}
}

func (s *Service) ServeManagementHTML(w http.ResponseWriter, r *http.Request, writeError func(http.ResponseWriter, int, error)) {
	if s.PanelPath != "" {
		if file, err := os.Open(s.PanelPath); err == nil {
			defer file.Close()
			info, statErr := file.Stat()
			if statErr != nil {
				writeError(w, http.StatusInternalServerError, statErr)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			http.ServeContent(w, r, "management.html", info.ModTime(), file)
			return
		}
	}
	data, err := fs.ReadFile(s.Embedded, "web/management.html")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	contentType := mime.TypeByExtension(".html")
	if !strings.Contains(contentType, "charset=") {
		contentType += "; charset=utf-8"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}

func (s *Service) ServeManagementAsset(w http.ResponseWriter, r *http.Request, writeError func(http.ResponseWriter, int, error)) {
	assetPath := strings.TrimPrefix(r.URL.Path, "/manager-assets/")
	assetPath = strings.TrimPrefix(path.Clean("/"+assetPath), "/")
	if !strings.HasPrefix(assetPath, "assets/") {
		http.NotFound(w, r)
		return
	}

	if s.PanelPath != "" {
		filePath := filepath.Join(filepath.Dir(s.PanelPath), filepath.FromSlash(assetPath))
		if file, err := os.Open(filePath); err == nil {
			defer file.Close()
			info, statErr := file.Stat()
			if statErr != nil {
				writeError(w, http.StatusInternalServerError, statErr)
				return
			}
			serveAssetContent(w, r, filepath.Base(filePath), info.ModTime(), file)
			return
		}
	}

	data, err := fs.ReadFile(s.Embedded, "web/"+assetPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	serveAssetContent(w, r, path.Base(assetPath), time.Time{}, bytes.NewReader(data))
}

func serveAssetContent(w http.ResponseWriter, r *http.Request, name string, modTime time.Time, content io.ReadSeeker) {
	contentType := mime.TypeByExtension(filepath.Ext(name))
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeContent(w, r, name, modTime, content)
}
