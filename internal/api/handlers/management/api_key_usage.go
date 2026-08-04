package management

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

const (
	defaultAPIKeyUsagePageSize = 100
	maxAPIKeyUsagePageSize     = 200
)

type apiKeyUsageEntry struct {
	Success        int64                          `json:"success"`
	Failed         int64                          `json:"failed"`
	RecentRequests []coreauth.RecentRequestBucket `json:"recent_requests"`
}

type apiKeyUsageItem struct {
	Provider     string `json:"provider"`
	CompositeKey string `json:"composite_key"`
	apiKeyUsageEntry
}

type apiKeyUsageQuery struct {
	page      int
	pageSize  int
	providers map[string]struct{}
	search    string
}

type apiKeyUsageAggregate struct {
	provider     string
	compositeKey string
	success      int64
	failed       int64
	auths        []*coreauth.Auth
}

func mergeRecentRequestBuckets(dst, src []coreauth.RecentRequestBucket) []coreauth.RecentRequestBucket {
	if len(dst) == 0 {
		return src
	}
	if len(src) == 0 {
		return dst
	}
	if len(dst) != len(src) {
		n := len(dst)
		if len(src) < n {
			n = len(src)
		}
		for i := 0; i < n; i++ {
			dst[i].Success += src[i].Success
			dst[i].Failed += src[i].Failed
		}
		return dst
	}
	for i := range dst {
		dst[i].Success += src[i].Success
		dst[i].Failed += src[i].Failed
	}
	return dst
}

func apiKeyUsageProviderKey(auth *coreauth.Auth) string {
	provider := strings.ToLower(strings.TrimSpace(auth.Provider))
	if auth.Attributes != nil {
		if compatName := strings.TrimSpace(auth.Attributes["compat_name"]); compatName != "" {
			provider = strings.ToLower(compatName)
		}
	}
	if provider == "" {
		return "unknown"
	}
	return provider
}

func parseAPIKeyUsageQuery(c *gin.Context) (apiKeyUsageQuery, error) {
	query := apiKeyUsageQuery{
		page:      1,
		pageSize:  defaultAPIKeyUsagePageSize,
		providers: make(map[string]struct{}),
	}
	if c == nil {
		return query, nil
	}
	if rawPage := strings.TrimSpace(c.Query("page")); rawPage != "" {
		page, errPage := strconv.Atoi(rawPage)
		if errPage != nil || page <= 0 || page > 1_000_000 {
			return query, fmt.Errorf("page must be an integer between 1 and 1000000")
		}
		query.page = page
	}
	if rawPageSize := strings.TrimSpace(c.Query("page_size")); rawPageSize != "" {
		pageSize, errPageSize := strconv.Atoi(rawPageSize)
		if errPageSize != nil || pageSize <= 0 || pageSize > maxAPIKeyUsagePageSize {
			return query, fmt.Errorf("page_size must be an integer between 1 and %d", maxAPIKeyUsagePageSize)
		}
		query.pageSize = pageSize
	}
	for _, rawProvider := range c.QueryArray("provider") {
		for _, provider := range strings.Split(rawProvider, ",") {
			provider = strings.ToLower(strings.TrimSpace(provider))
			if provider != "" {
				query.providers[provider] = struct{}{}
			}
		}
	}
	query.search = strings.ToLower(strings.TrimSpace(c.Query("search")))
	return query, nil
}

func apiKeyUsageMatchesQuery(provider, compositeKey string, query apiKeyUsageQuery) bool {
	if len(query.providers) > 0 {
		if _, ok := query.providers[provider]; !ok {
			return false
		}
	}
	if query.search == "" {
		return true
	}
	return strings.Contains(provider, query.search) ||
		strings.Contains(strings.ToLower(compositeKey), query.search)
}

func apiKeyUsagePageBounds(total int, query *apiKeyUsageQuery) (start, end, totalPages int) {
	if query == nil || query.pageSize <= 0 {
		return 0, 0, 0
	}
	if total > 0 {
		totalPages = (total + query.pageSize - 1) / query.pageSize
		if query.page > totalPages {
			query.page = totalPages
		}
	}
	start = (query.page - 1) * query.pageSize
	if start > total {
		start = total
	}
	end = start + query.pageSize
	if end > total {
		end = total
	}
	return start, end, totalPages
}

// GetAPIKeyUsage returns a bounded page of recent request buckets for in-memory
// api_key auths. Duplicate runtime auths are grouped by provider and
// "base_url|api_key" before pagination.
func (h *Handler) GetAPIKeyUsage(c *gin.Context) {
	if h == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "handler not initialized"})
		return
	}

	h.mu.Lock()
	manager := h.authManager
	h.mu.Unlock()
	if manager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "core auth manager unavailable"})
		return
	}
	query, errQuery := parseAPIKeyUsageQuery(c)
	if errQuery != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errQuery.Error()})
		return
	}

	aggregatesByKey := make(map[string]*apiKeyUsageAggregate)
	for _, auth := range manager.List() {
		if auth == nil {
			continue
		}
		// Tenant runtime credentials are intentionally visible only through the
		// dedicated masked tenant-provider views. The legacy usage payload uses
		// a composite value containing the raw upstream API key.
		if isTenantRuntimeAuth(auth) {
			continue
		}
		kind, apiKey := auth.AccountInfo()
		if !strings.EqualFold(strings.TrimSpace(kind), "api_key") {
			continue
		}
		apiKey = strings.TrimSpace(apiKey)
		if apiKey == "" {
			continue
		}
		baseURL := ""
		if auth.Attributes != nil {
			baseURL = strings.TrimSpace(auth.Attributes["base_url"])
			if baseURL == "" {
				baseURL = strings.TrimSpace(auth.Attributes["base-url"])
			}
		}
		compositeKey := baseURL + "|" + apiKey
		provider := apiKeyUsageProviderKey(auth)
		if !apiKeyUsageMatchesQuery(provider, compositeKey, query) {
			continue
		}

		groupKey := provider + "\x00" + compositeKey
		aggregate := aggregatesByKey[groupKey]
		if aggregate == nil {
			aggregate = &apiKeyUsageAggregate{
				provider:     provider,
				compositeKey: compositeKey,
			}
			aggregatesByKey[groupKey] = aggregate
		}
		aggregate.success += auth.Success
		aggregate.failed += auth.Failed
		aggregate.auths = append(aggregate.auths, auth)
	}

	aggregates := make([]*apiKeyUsageAggregate, 0, len(aggregatesByKey))
	for _, aggregate := range aggregatesByKey {
		aggregates = append(aggregates, aggregate)
	}
	sort.Slice(aggregates, func(i, j int) bool {
		if aggregates[i].provider != aggregates[j].provider {
			return aggregates[i].provider < aggregates[j].provider
		}
		return aggregates[i].compositeKey < aggregates[j].compositeKey
	})

	total := len(aggregates)
	start, end, totalPages := apiKeyUsagePageBounds(total, &query)
	items := make([]apiKeyUsageItem, 0, end-start)
	now := time.Now()
	for _, aggregate := range aggregates[start:end] {
		var recent []coreauth.RecentRequestBucket
		for _, auth := range aggregate.auths {
			recent = mergeRecentRequestBuckets(recent, auth.RecentRequestsSnapshot(now))
		}
		if recent == nil {
			recent = []coreauth.RecentRequestBucket{}
		}
		items = append(items, apiKeyUsageItem{
			Provider:     aggregate.provider,
			CompositeKey: aggregate.compositeKey,
			apiKeyUsageEntry: apiKeyUsageEntry{
				Success:        aggregate.success,
				Failed:         aggregate.failed,
				RecentRequests: recent,
			},
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"items":       items,
		"total":       total,
		"page":        query.page,
		"page_size":   query.pageSize,
		"total_pages": totalPages,
		"has_more":    query.page < totalPages,
	})
}
