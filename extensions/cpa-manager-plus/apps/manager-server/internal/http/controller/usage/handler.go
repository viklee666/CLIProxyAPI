package usage

import (
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/app"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/http/middleware"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/http/response"
	usagesvc "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/service/usage"
)

const maxUsageImportBytes int64 = 64 * 1024 * 1024

type Handler struct {
	App *app.Context
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	if !middleware.AuthorizePanel(w, r, h.App.AdminAuthService) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		if strings.HasSuffix(r.URL.Path, "/export") {
			h.Export(w, r)
			return
		}
		query := r.URL.Query()
		page, err := positiveQueryInt(query.Get("page"), 1)
		if err != nil {
			response.Error(w, http.StatusBadRequest, errors.New("page must be a positive integer"))
			return
		}
		if page > 1_000_000 {
			response.Error(w, http.StatusBadRequest, errors.New("page must be less than or equal to 1000000"))
			return
		}
		pageSizeRaw := query.Get("page_size")
		if strings.TrimSpace(pageSizeRaw) == "" {
			pageSizeRaw = query.Get("limit")
		}
		pageSize, err := positiveQueryInt(pageSizeRaw, 200)
		if err != nil {
			response.Error(w, http.StatusBadRequest, errors.New("page_size must be a positive integer"))
			return
		}
		if pageSize > 500 {
			pageSize = 500
		}
		result, err := h.App.UsageService.CompatibleUsage(r.Context(), usagesvc.CompatibleUsageRequest{
			Page:     page,
			PageSize: pageSize,
			Cursor:   query.Get("cursor"),
		})
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, usagesvc.ErrInvalidUsageCursor) {
				status = http.StatusBadRequest
			}
			response.Error(w, status, err)
			return
		}
		response.JSON(w, http.StatusOK, result)
	case http.MethodPost:
		if strings.HasSuffix(r.URL.Path, "/import") {
			h.Import(w, r)
			return
		}
		response.MethodNotAllowed(w)
	default:
		response.MethodNotAllowed(w)
	}
}

func positiveQueryInt(raw string, fallback int) (int, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(trimmed)
	if err != nil || value <= 0 {
		return 0, errors.New("query value must be a positive integer")
	}
	return value, nil
}

func (h *Handler) Export(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Content-Disposition", `attachment; filename="usage-events.jsonl"`)
	writer := &countingWriter{writer: w}
	if err := h.App.UsageService.WriteExport(r.Context(), writer, h.App.Config.QueryLimit); err != nil {
		if writer.written == 0 {
			w.Header().Del("Content-Disposition")
			response.Error(w, http.StatusInternalServerError, err)
		} else {
			log.Printf("usage export stream failed after %d bytes: %v", writer.written, err)
		}
	}
}

type countingWriter struct {
	writer  io.Writer
	written int64
}

func (w *countingWriter) Write(data []byte) (int, error) {
	written, err := w.writer.Write(data)
	w.written += int64(written)
	return written, err
}

func (h *Handler) Import(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength > maxUsageImportBytes {
		response.Error(w, http.StatusRequestEntityTooLarge, errors.New("http: request body too large"))
		return
	}
	body := http.MaxBytesReader(w, r.Body, maxUsageImportBytes)
	result, parsed, err := h.App.UsageService.Import(r.Context(), body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			response.Error(w, http.StatusRequestEntityTooLarge, err)
			return
		}
		var persistenceErr *usagesvc.ImportPersistenceError
		if errors.As(err, &persistenceErr) || result.Added+result.Skipped > 0 {
			response.Error(w, http.StatusInternalServerError, err)
			return
		}
		if parsed == nil {
			response.Error(w, http.StatusBadRequest, err)
			return
		}
		response.JSON(w, http.StatusBadRequest, map[string]any{
			"error":       err.Error(),
			"format":      parsed.Format,
			"failed":      parsed.Failed,
			"unsupported": parsed.Unsupported,
			"warnings":    parsed.Warnings,
		})
		return
	}
	response.JSON(w, http.StatusOK, result)
}
