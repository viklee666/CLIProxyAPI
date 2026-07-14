package management

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	"github.com/tidwall/gjson"
)

const (
	defaultAuthFilesPageSize = 50
	maxAuthFilesPageSize     = 500
)

type authFileQuery struct {
	view      string
	page      int
	pageSize  int
	paged     bool
	updatedAt time.Time
	search    string
	providers map[string]struct{}
	names     map[string]struct{}
	indexes   map[string]struct{}
	disabled  *bool
	problem   *bool
	runtime   *bool
	sort      string
	order     string
}

type authFileCandidate struct {
	auth        *coreauth.Auth
	entry       gin.H
	name        string
	provider    string
	authIndex   string
	account     string
	note        string
	status      string
	statusMsg   string
	priority    int
	disabled    bool
	unavailable bool
	runtimeOnly bool
	updatedAt   time.Time
}

func hasAuthFileQuery(c *gin.Context) bool {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return false
	}
	query := c.Request.URL.Query()
	for _, key := range []string{
		"view", "page", "page_size", "limit", "search", "provider", "type",
		"name", "auth_index", "disabled", "problem", "healthy", "runtime_only",
		"sort", "order", "updated_after_ms",
	} {
		if _, ok := query[key]; ok {
			return true
		}
	}
	return false
}

func parseAuthFileQuery(c *gin.Context) (authFileQuery, error) {
	q := authFileQuery{
		view:      strings.ToLower(strings.TrimSpace(c.Query("view"))),
		page:      1,
		pageSize:  defaultAuthFilesPageSize,
		search:    strings.ToLower(strings.TrimSpace(c.Query("search"))),
		providers: parseAuthFileQuerySet(c, "provider", "type"),
		names:     parseAuthFileQuerySet(c, "name"),
		indexes:   parseAuthFileQuerySet(c, "auth_index"),
		sort:      strings.ToLower(strings.TrimSpace(c.Query("sort"))),
		order:     strings.ToLower(strings.TrimSpace(c.Query("order"))),
	}
	if q.view == "" {
		q.view = "detail"
	}
	switch q.view {
	case "detail", "summary", "snapshot", "count":
	default:
		return q, fmt.Errorf("view must be one of detail, summary, snapshot, or count")
	}

	rawPage, hasPage := c.GetQuery("page")
	rawPageSize, hasPageSize := c.GetQuery("page_size")
	if !hasPageSize {
		rawPageSize, hasPageSize = c.GetQuery("limit")
	}
	q.paged = hasPage || hasPageSize
	if hasPage {
		parsed, err := strconv.Atoi(strings.TrimSpace(rawPage))
		if err != nil || parsed <= 0 {
			return q, fmt.Errorf("page must be a positive integer")
		}
		q.page = parsed
	}
	if hasPageSize {
		parsed, err := strconv.Atoi(strings.TrimSpace(rawPageSize))
		if err != nil || parsed <= 0 {
			return q, fmt.Errorf("page_size must be a positive integer")
		}
		if parsed > maxAuthFilesPageSize {
			parsed = maxAuthFilesPageSize
		}
		q.pageSize = parsed
	}
	if rawUpdatedAfter, ok := c.GetQuery("updated_after_ms"); ok {
		milliseconds, err := strconv.ParseInt(strings.TrimSpace(rawUpdatedAfter), 10, 64)
		if err != nil || milliseconds < 0 {
			return q, fmt.Errorf("updated_after_ms must be a non-negative unix millisecond timestamp")
		}
		if milliseconds > 0 {
			q.updatedAt = time.UnixMilli(milliseconds)
		}
	}

	var err error
	if q.disabled, err = parseOptionalQueryBool(c, "disabled"); err != nil {
		return q, err
	}
	if q.problem, err = parseOptionalQueryBool(c, "problem"); err != nil {
		return q, err
	}
	if healthy, errHealthy := parseOptionalQueryBool(c, "healthy"); errHealthy != nil {
		return q, errHealthy
	} else if healthy != nil {
		problem := !*healthy
		q.problem = &problem
	}
	if q.runtime, err = parseOptionalQueryBool(c, "runtime_only"); err != nil {
		return q, err
	}

	if q.sort == "" || q.sort == "default" {
		q.sort = "provider"
	}
	switch q.sort {
	case "provider", "name", "note", "priority", "status", "updated_at":
	default:
		return q, fmt.Errorf("unsupported sort field")
	}
	if q.order == "" {
		if q.sort == "priority" {
			q.order = "desc"
		} else {
			q.order = "asc"
		}
	}
	if q.order != "asc" && q.order != "desc" {
		return q, fmt.Errorf("order must be asc or desc")
	}
	return q, nil
}

func parseAuthFileQuerySet(c *gin.Context, keys ...string) map[string]struct{} {
	result := make(map[string]struct{})
	for _, key := range keys {
		for _, raw := range c.QueryArray(key) {
			for _, value := range strings.Split(raw, ",") {
				value = strings.ToLower(strings.TrimSpace(value))
				if value != "" && value != "all" {
					result[value] = struct{}{}
				}
			}
		}
	}
	return result
}

func parseOptionalQueryBool(c *gin.Context, key string) (*bool, error) {
	raw, ok := c.GetQuery(key)
	if !ok || strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	parsed, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("%s must be a boolean", key)
	}
	return &parsed, nil
}

func (h *Handler) listAuthFilesQuery(c *gin.Context) {
	q, err := parseAuthFileQuery(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	responseTime := time.Now()

	var candidates []authFileCandidate
	if h.authManager != nil {
		candidates = h.authFileCandidatesFromManager()
	} else {
		candidates, err = h.authFileCandidatesFromDisk()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	filtered := make([]authFileCandidate, 0, len(candidates))
	providerCounts := make(map[string]int)
	for i := range candidates {
		candidate := candidates[i]
		if !candidate.matches(q, false) {
			continue
		}
		providerCounts[candidate.provider]++
		if !candidate.matches(q, true) {
			continue
		}
		filtered = append(filtered, candidate)
	}
	sortAuthFileCandidates(filtered, q)

	total := len(filtered)
	start, end := 0, total
	if q.paged {
		start = (q.page - 1) * q.pageSize
		if start > total {
			start = total
		}
		end = start + q.pageSize
		if end > total {
			end = total
		}
	}

	files := make([]gin.H, 0, end-start)
	if q.view != "count" {
		for i := start; i < end; i++ {
			entry := h.authFileQueryEntry(filtered[i], q.view)
			if entry != nil {
				files = append(files, entry)
			}
		}
	}

	response := gin.H{
		"files":          files,
		"total":          total,
		"facets":         gin.H{"providers": providerCounts},
		"server_time_ms": responseTime.UnixMilli(),
	}
	if q.paged {
		totalPages := 0
		if total > 0 {
			totalPages = (total + q.pageSize - 1) / q.pageSize
		}
		response["page"] = q.page
		response["page_size"] = q.pageSize
		response["total_pages"] = totalPages
		response["has_more"] = end < total
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, response)
}

func (h *Handler) authFileCandidatesFromManager() []authFileCandidate {
	auths := h.authManager.List()
	result := make([]authFileCandidate, 0, len(auths))
	for _, auth := range auths {
		if auth == nil {
			continue
		}
		auth.EnsureIndex()
		runtimeOnly := isRuntimeOnlyAuth(auth)
		if runtimeOnly && (auth.Disabled || auth.Status == coreauth.StatusDisabled) {
			continue
		}
		path := strings.TrimSpace(authAttribute(auth, "path"))
		if path == "" && !runtimeOnly {
			continue
		}
		name := strings.TrimSpace(auth.FileName)
		if name == "" {
			name = auth.ID
		}
		provider := strings.ToLower(strings.TrimSpace(auth.Provider))
		account := ""
		if kind, value := auth.AccountInfo(); strings.EqualFold(kind, "oauth") {
			account = strings.TrimSpace(value)
		}
		if account == "" {
			account = authEmail(auth)
		}
		priority := 0
		if raw := strings.TrimSpace(authAttribute(auth, "priority")); raw != "" {
			priority, _ = strconv.Atoi(raw)
		}
		note := strings.TrimSpace(authAttribute(auth, "note"))
		if note == "" && auth.Metadata != nil {
			note, _ = auth.Metadata["note"].(string)
			note = strings.TrimSpace(note)
		}
		result = append(result, authFileCandidate{
			auth:        auth,
			name:        name,
			provider:    provider,
			authIndex:   strings.TrimSpace(auth.Index),
			account:     account,
			note:        note,
			status:      strings.ToLower(strings.TrimSpace(string(auth.Status))),
			statusMsg:   strings.TrimSpace(auth.StatusMessage),
			priority:    priority,
			disabled:    auth.Disabled || auth.Status == coreauth.StatusDisabled,
			unavailable: auth.Unavailable,
			runtimeOnly: runtimeOnly,
			updatedAt:   auth.UpdatedAt,
		})
	}
	return result
}

func (h *Handler) authFileCandidatesFromDisk() ([]authFileCandidate, error) {
	entries, err := os.ReadDir(h.cfg.AuthDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read auth dir: %w", err)
	}
	result := make([]authFileCandidate, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			continue
		}
		info, errInfo := entry.Info()
		if errInfo != nil {
			continue
		}
		path := filepath.Join(h.cfg.AuthDir, entry.Name())
		data, _ := os.ReadFile(path)
		provider := strings.ToLower(strings.TrimSpace(gjson.GetBytes(data, "type").String()))
		email := strings.TrimSpace(gjson.GetBytes(data, "email").String())
		note := strings.TrimSpace(gjson.GetBytes(data, "note").String())
		priority := int(gjson.GetBytes(data, "priority").Int())
		disabled := gjson.GetBytes(data, "disabled").Bool()
		fileEntry := gin.H{
			"name":     entry.Name(),
			"type":     provider,
			"provider": provider,
			"email":    email,
			"size":     info.Size(),
			"modtime":  info.ModTime(),
			"source":   "file",
			"disabled": disabled,
		}
		if note != "" {
			fileEntry["note"] = note
		}
		if priority != 0 {
			fileEntry["priority"] = priority
		}
		result = append(result, authFileCandidate{
			entry:     fileEntry,
			name:      entry.Name(),
			provider:  provider,
			account:   email,
			note:      note,
			priority:  priority,
			disabled:  disabled,
			status:    map[bool]string{true: "disabled", false: "active"}[disabled],
			statusMsg: "",
			updatedAt: info.ModTime(),
		})
	}
	return result, nil
}

func (c authFileCandidate) matches(q authFileQuery, includeProvider bool) bool {
	if !q.updatedAt.IsZero() && (c.updatedAt.IsZero() || !c.updatedAt.After(q.updatedAt)) {
		return false
	}
	if includeProvider && len(q.providers) > 0 {
		if _, ok := q.providers[c.provider]; !ok {
			return false
		}
	}
	if len(q.names) > 0 {
		if _, ok := q.names[strings.ToLower(c.name)]; !ok {
			return false
		}
	}
	if len(q.indexes) > 0 {
		if _, ok := q.indexes[strings.ToLower(c.authIndex)]; !ok {
			return false
		}
	}
	if q.disabled != nil && c.disabled != *q.disabled {
		return false
	}
	if q.runtime != nil && c.runtimeOnly != *q.runtime {
		return false
	}
	if q.problem != nil && c.problem() != *q.problem {
		return false
	}
	if q.search != "" {
		haystack := strings.ToLower(strings.Join([]string{
			c.name, c.provider, c.account, c.note, c.status, c.statusMsg,
		}, "\n"))
		if !strings.Contains(haystack, q.search) {
			return false
		}
	}
	return true
}

func (c authFileCandidate) problem() bool {
	if c.disabled || c.unavailable {
		return true
	}
	message := strings.ToLower(strings.TrimSpace(c.statusMsg))
	if message == "" {
		return false
	}
	switch message {
	case "ok", "healthy", "ready", "success", "available":
		return false
	default:
		return true
	}
}

func sortAuthFileCandidates(candidates []authFileCandidate, q authFileQuery) {
	sort.SliceStable(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		cmp := 0
		switch q.sort {
		case "name":
			cmp = strings.Compare(strings.ToLower(left.name), strings.ToLower(right.name))
		case "note":
			cmp = strings.Compare(strings.ToLower(left.note), strings.ToLower(right.note))
		case "priority":
			cmp = left.priority - right.priority
		case "status":
			cmp = strings.Compare(left.status, right.status)
		case "updated_at":
			var leftTime, rightTime time.Time
			if left.auth != nil {
				leftTime = left.auth.UpdatedAt
			}
			if right.auth != nil {
				rightTime = right.auth.UpdatedAt
			}
			cmp = leftTime.Compare(rightTime)
		default:
			cmp = strings.Compare(left.provider, right.provider)
			if cmp == 0 {
				cmp = strings.Compare(strings.ToLower(left.name), strings.ToLower(right.name))
			}
		}
		if cmp == 0 && q.sort != "name" {
			cmp = strings.Compare(strings.ToLower(left.name), strings.ToLower(right.name))
		}
		if q.order == "desc" {
			return cmp > 0
		}
		return cmp < 0
	})
}

func (h *Handler) authFileQueryEntry(candidate authFileCandidate, view string) gin.H {
	if candidate.entry != nil {
		return candidate.entry
	}
	if candidate.auth == nil {
		return nil
	}
	if view == "detail" {
		return h.buildAuthFileEntry(candidate.auth)
	}
	auth := candidate.auth
	entry := gin.H{
		"id":             auth.ID,
		"auth_index":     auth.Index,
		"name":           candidate.name,
		"type":           strings.TrimSpace(auth.Provider),
		"provider":       strings.TrimSpace(auth.Provider),
		"label":          auth.Label,
		"status":         auth.Status,
		"status_message": auth.StatusMessage,
		"disabled":       candidate.disabled,
		"unavailable":    auth.Unavailable,
		"runtime_only":   candidate.runtimeOnly,
		"source":         map[bool]string{true: "memory", false: "file"}[candidate.runtimeOnly],
	}
	if candidate.account != "" {
		entry["account"] = candidate.account
		entry["email"] = candidate.account
	}
	if projectID := authProjectID(auth); projectID != "" {
		entry["project_id"] = projectID
	}
	if candidate.priority != 0 {
		entry["priority"] = candidate.priority
	}
	if candidate.note != "" {
		entry["note"] = candidate.note
	}
	if websockets, ok := authWebsocketsValue(auth); ok {
		entry["websockets"] = websockets
	}
	if !auth.UpdatedAt.IsZero() {
		entry["modtime"] = auth.UpdatedAt
		entry["updated_at"] = auth.UpdatedAt
	}
	if !auth.LastRefreshedAt.IsZero() {
		entry["last_refresh"] = auth.LastRefreshedAt
	}
	if !auth.NextRetryAfter.IsZero() {
		entry["next_retry_after"] = auth.NextRetryAfter
	}
	if view == "summary" {
		entry["success"] = auth.Success
		entry["failed"] = auth.Failed
		if claims := extractCodexIDTokenClaims(auth); claims != nil {
			entry["id_token"] = claims
		}
	}
	return entry
}
