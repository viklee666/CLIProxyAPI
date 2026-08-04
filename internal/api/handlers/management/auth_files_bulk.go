package management

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

const (
	maxAuthFileBulkTargets        = 1000
	maxAuthFileBulkJSONBytes      = 2 * 1024 * 1024
	maxAuthFileBytes              = 16 * 1024 * 1024
	maxAuthFileUploadRequestBytes = 128 * 1024 * 1024
	maxAuthFileBatchArchiveBytes  = 128 * 1024 * 1024
)

type authFileBulkTarget struct {
	Name        string   `json:"name"`
	AuthIndex   string   `json:"auth_index,omitempty"`
	AuthIndexes []string `json:"auth_indices,omitempty"`
}

func normalizeAuthFileBulkTargets(items []authFileBulkTarget) []authFileBulkTarget {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]authFileBulkTarget, 0, len(items))
	for _, item := range items {
		item.Name = strings.TrimSpace(item.Name)
		item.AuthIndex = strings.TrimSpace(item.AuthIndex)
		item.AuthIndexes = uniqueAuthFileNames(item.AuthIndexes)
		if item.Name == "" {
			continue
		}
		key := item.Name + "\x00" + item.AuthIndex + "\x00" + strings.Join(item.AuthIndexes, "\x00")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func (h *Handler) patchAuthFileStatusTarget(ctx context.Context, name, authIndex string, disabled bool) (gin.H, int, error) {
	name = strings.TrimSpace(name)
	authIndex = strings.TrimSpace(authIndex)
	if name == "" {
		return nil, http.StatusBadRequest, fmt.Errorf("name is required")
	}

	// Find auth by name or ID. When auth_index is supplied, require an exact
	// match so a multi-account source file does not toggle the wrong credential.
	var targetAuth *coreauth.Auth
	if auth, ok := h.authManager.GetByID(name); ok && (authIndex == "" || auth.Index == authIndex) {
		targetAuth = auth
	} else {
		for _, auth := range h.authManager.List() {
			if auth != nil && auth.FileName == name && (authIndex == "" || auth.Index == authIndex) {
				targetAuth = auth
				break
			}
		}
	}
	if targetAuth == nil {
		return nil, http.StatusNotFound, fmt.Errorf("auth file not found")
	}
	if isTenantRuntimeAuth(targetAuth) {
		return nil, http.StatusNotFound, fmt.Errorf("auth file not found")
	}

	if coreauth.IsPluginVirtualAuth(targetAuth) {
		if !isPluginVirtualSourceDelete(name, targetAuth) {
			return nil, http.StatusConflict, errPluginVirtualAuth
		}
		if errPatch := h.patchPluginVirtualSourceStatus(ctx, targetAuth, disabled); errPatch != nil {
			status := http.StatusInternalServerError
			if os.IsNotExist(errPatch) {
				status = http.StatusNotFound
			}
			return nil, status, errPatch
		}
		h.invalidateAuthFileCandidateCatalog()
		return gin.H{"status": "ok", "name": name, "auth_index": targetAuth.Index, "disabled": disabled}, http.StatusOK, nil
	}

	if coreauth.IsConfigAPIKeyAuth(targetAuth) {
		h.mu.Lock()
		handled, errToggle := toggleConfigAPIKeyExcludedAll(h.cfg, targetAuth, disabled)
		if errToggle != nil {
			h.mu.Unlock()
			return nil, http.StatusInternalServerError, fmt.Errorf("failed to update config api key: %w", errToggle)
		}
		if !handled {
			h.mu.Unlock()
			return nil, http.StatusNotFound, fmt.Errorf("config api key entry not found")
		}
		if errSave := config.SaveConfigPreserveComments(h.configFilePath, h.cfg); errSave != nil {
			h.mu.Unlock()
			return nil, http.StatusInternalServerError, fmt.Errorf("failed to save config: %w", errSave)
		}
		snapshot := h.reloadSnapshotConfigLocked()
		h.mu.Unlock()
		h.reloadConfigAfterManagementSave(ctx, snapshot)
		h.invalidateAuthFileCandidateCatalog()
		if h.tokenStore != nil {
			_ = h.tokenStore.Delete(ctx, targetAuth.ID)
		}
		return gin.H{
			"status":           "ok",
			"name":             name,
			"auth_index":       targetAuth.Index,
			"disabled":         disabled,
			"via":              "config:excluded-models",
			"excluded_pattern": configAPIKeyDisablePattern,
		}, http.StatusOK, nil
	}

	applyAuthDisabledState(targetAuth, disabled)
	if _, errUpdate := h.authManager.Update(ctx, targetAuth); errUpdate != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to update auth: %w", errUpdate)
	}
	h.invalidateAuthFileCandidateCatalog()
	return gin.H{
		"status":     "ok",
		"name":       name,
		"auth_index": targetAuth.Index,
		"disabled":   disabled,
	}, http.StatusOK, nil
}

// PatchAuthFileStatusBatch applies one disabled state to many credentials in a
// single management request. Each item is isolated so callers receive precise
// partial-failure information instead of issuing N PATCH requests.
func (h *Handler) PatchAuthFileStatusBatch(c *gin.Context) {
	if h.authManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "core auth manager unavailable"})
		return
	}
	limitManagementRequestBody(c, maxAuthFileBulkJSONBytes)
	var req struct {
		Items    []authFileBulkTarget `json:"items"`
		Disabled *bool                `json:"disabled"`
	}
	if errBind := c.ShouldBindJSON(&req); errBind != nil {
		if isManagementRequestTooLarge(errBind) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request body too large"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	items := normalizeAuthFileBulkTargets(req.Items)
	if req.Disabled == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "disabled is required"})
		return
	}
	if len(items) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "items are required"})
		return
	}
	if len(items) > maxAuthFileBulkTargets {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": fmt.Sprintf("too many items; maximum is %d", maxAuthFileBulkTargets)})
		return
	}

	results := make([]gin.H, 0, len(items))
	failed := make([]gin.H, 0)
	ctx := c.Request.Context()
	for _, item := range items {
		result, status, errPatch := h.patchAuthFileStatusTarget(ctx, item.Name, item.AuthIndex, *req.Disabled)
		if errPatch != nil {
			failed = append(failed, gin.H{
				"name":       item.Name,
				"auth_index": item.AuthIndex,
				"status":     status,
				"error":      errPatch.Error(),
			})
			continue
		}
		results = append(results, result)
	}

	status := http.StatusOK
	state := "ok"
	if len(failed) > 0 {
		status = http.StatusMultiStatus
		state = "partial"
	}
	c.JSON(status, gin.H{
		"status":  state,
		"updated": len(results),
		"items":   results,
		"failed":  failed,
	})
}

func (h *Handler) patchAuthFileFieldsTarget(ctx context.Context, name, authIndex string, fields map[string]json.RawMessage) (gin.H, int, error) {
	name = strings.TrimSpace(name)
	authIndex = strings.TrimSpace(authIndex)
	if name == "" {
		return nil, http.StatusBadRequest, fmt.Errorf("name is required")
	}
	if len(fields) == 0 {
		return nil, http.StatusBadRequest, fmt.Errorf("no fields to update")
	}

	var targetAuth *coreauth.Auth
	if auth, ok := h.authManager.GetByID(name); ok && (authIndex == "" || auth.Index == authIndex) {
		targetAuth = auth
	} else {
		for _, auth := range h.authManager.List() {
			if auth != nil && auth.FileName == name && (authIndex == "" || auth.Index == authIndex) {
				targetAuth = auth
				break
			}
		}
	}
	if targetAuth == nil {
		return nil, http.StatusNotFound, fmt.Errorf("auth file not found")
	}
	if isTenantRuntimeAuth(targetAuth) {
		return nil, http.StatusNotFound, fmt.Errorf("auth file not found")
	}
	if coreauth.IsPluginVirtualAuth(targetAuth) {
		return nil, http.StatusConflict, errPluginVirtualAuth
	}

	touchedRoots := make(map[string]struct{}, len(fields))
	for key, rawValue := range fields {
		fieldPath := strings.TrimSpace(key)
		if fieldPath == "" {
			return nil, http.StatusBadRequest, fmt.Errorf("field name is required")
		}
		value, errDecode := decodeAuthFileFieldValue(rawValue)
		if errDecode != nil {
			return nil, http.StatusBadRequest, fmt.Errorf("invalid field %s", fieldPath)
		}
		if targetAuth.Metadata == nil {
			targetAuth.Metadata = make(map[string]any)
		}
		if fieldPath == "headers" {
			applyAuthFileHeadersPatch(targetAuth, value)
		} else if errSet := setAuthFileMetadataValue(targetAuth.Metadata, fieldPath, value); errSet != nil {
			return nil, http.StatusBadRequest, errSet
		}
		if root := rootAuthFileField(fieldPath); root != "" {
			touchedRoots[root] = struct{}{}
		}
	}
	syncAuthFileMetadataFields(targetAuth, touchedRoots)
	targetAuth.UpdatedAt = time.Now()
	if _, errUpdate := h.authManager.Update(ctx, targetAuth); errUpdate != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to update auth: %w", errUpdate)
	}
	h.invalidateAuthFileCandidateCatalog()
	return gin.H{
		"status":     "ok",
		"name":       name,
		"auth_index": targetAuth.Index,
	}, http.StatusOK, nil
}

// PatchAuthFileFieldsBatch updates many file/auth-index targets with one field
// patch. The server performs target resolution, so the browser does not need
// to download and re-upload every credential JSON before editing metadata.
func (h *Handler) PatchAuthFileFieldsBatch(c *gin.Context) {
	if h.authManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "core auth manager unavailable"})
		return
	}
	limitManagementRequestBody(c, maxAuthFileBulkJSONBytes)
	var req struct {
		Items  []authFileBulkTarget       `json:"items"`
		Fields map[string]json.RawMessage `json:"fields"`
	}
	decoder := json.NewDecoder(c.Request.Body)
	decoder.UseNumber()
	if errDecode := decoder.Decode(&req); errDecode != nil {
		if isManagementRequestTooLarge(errDecode) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request body too large"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	items := normalizeAuthFileBulkTargets(req.Items)
	if len(items) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "items are required"})
		return
	}
	if len(req.Fields) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "fields are required"})
		return
	}

	targetCount := 0
	for _, item := range items {
		count := len(item.AuthIndexes)
		if count == 0 {
			count = 1
		}
		targetCount += count
	}
	if targetCount > maxAuthFileBulkTargets {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": fmt.Sprintf("too many targets; maximum is %d", maxAuthFileBulkTargets)})
		return
	}

	results := make([]gin.H, 0, targetCount)
	failed := make([]gin.H, 0)
	ctx := c.Request.Context()
	for _, item := range items {
		authIndexes := item.AuthIndexes
		if len(authIndexes) == 0 {
			authIndexes = []string{item.AuthIndex}
		}
		for _, authIndex := range authIndexes {
			result, status, errPatch := h.patchAuthFileFieldsTarget(ctx, item.Name, authIndex, req.Fields)
			if errPatch != nil {
				failed = append(failed, gin.H{
					"name":       item.Name,
					"auth_index": authIndex,
					"status":     status,
					"error":      errPatch.Error(),
				})
				continue
			}
			results = append(results, result)
		}
	}

	status := http.StatusOK
	state := "ok"
	if len(failed) > 0 {
		status = http.StatusMultiStatus
		state = "partial"
	}
	c.JSON(status, gin.H{
		"status":  state,
		"updated": len(results),
		"items":   results,
		"failed":  failed,
	})
}

type authFileDownloadCandidate struct {
	name    string
	path    string
	modTime time.Time
}

// DownloadAuthFilesBatch streams selected credentials as one ZIP archive,
// replacing the browser's previous sequence of N download requests.
func (h *Handler) DownloadAuthFilesBatch(c *gin.Context) {
	limitManagementRequestBody(c, maxAuthFileBulkJSONBytes)
	var req struct {
		Names []string `json:"names"`
	}
	if errBind := c.ShouldBindJSON(&req); errBind != nil {
		if isManagementRequestTooLarge(errBind) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request body too large"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	names := uniqueAuthFileNames(req.Names)
	if len(names) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "names are required"})
		return
	}
	if len(names) > maxAuthFileBulkTargets {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": fmt.Sprintf("too many files; maximum is %d", maxAuthFileBulkTargets)})
		return
	}

	files := make([]authFileDownloadCandidate, 0, len(names))
	failed := make([]gin.H, 0)
	var totalBytes int64
	for _, name := range names {
		if isUnsafeAuthFileName(name) || !strings.HasSuffix(strings.ToLower(name), ".json") {
			failed = append(failed, gin.H{"name": name, "error": "invalid name"})
			continue
		}
		path := filepath.Join(h.cfg.AuthDir, name)
		info, errStat := os.Stat(path)
		if errStat != nil || info.IsDir() {
			message := "file not found"
			if errStat != nil && !os.IsNotExist(errStat) {
				message = errStat.Error()
			}
			failed = append(failed, gin.H{"name": name, "error": message})
			continue
		}
		if info.Size() > maxAuthFileBytes {
			failed = append(failed, gin.H{"name": name, "error": fmt.Sprintf("file exceeds %d bytes", maxAuthFileBytes)})
			continue
		}
		if totalBytes+info.Size() > maxAuthFileBatchArchiveBytes {
			failed = append(failed, gin.H{"name": name, "error": fmt.Sprintf("archive exceeds %d bytes", maxAuthFileBatchArchiveBytes)})
			continue
		}
		totalBytes += info.Size()
		files = append(files, authFileDownloadCandidate{name: name, path: path, modTime: info.ModTime()})
	}
	if len(files) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "no requested auth files were found", "failed": failed})
		return
	}

	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"auth-files-%s.zip\"", time.Now().UTC().Format("20060102-150405")))
	c.Header("X-Auth-Files-Included", fmt.Sprintf("%d", len(files)))
	c.Header("X-Auth-Files-Failed", fmt.Sprintf("%d", len(failed)))
	c.Status(http.StatusOK)

	zipWriter := zip.NewWriter(c.Writer)
	for _, file := range files {
		header := &zip.FileHeader{Name: file.name, Method: zip.Deflate}
		header.SetModTime(file.modTime)
		entryWriter, errCreate := zipWriter.CreateHeader(header)
		if errCreate != nil {
			continue
		}
		source, errOpen := os.Open(file.path)
		if errOpen != nil {
			continue
		}
		_, _ = io.Copy(entryWriter, source)
		_ = source.Close()
	}
	if len(failed) > 0 {
		if errorWriter, errCreate := zipWriter.Create("_errors.json"); errCreate == nil {
			_ = json.NewEncoder(errorWriter).Encode(gin.H{"failed": failed})
		}
	}
	_ = zipWriter.Close()
}
