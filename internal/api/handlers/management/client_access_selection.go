package management

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/clientaccess"
)

var errClientAccessAuthManagerUnavailable = errors.New("auth manager is unavailable")

type clientAccessCredentialSelection struct {
	Mode                string   `json:"mode"`
	All                 bool     `json:"all"`
	Providers           []string `json:"providers"`
	PlanTypes           []string `json:"plan_types"`
	IncludedAuthIndices []string `json:"included_auth_indices"`
	ExcludedAuthIndices []string `json:"excluded_auth_indices"`
}

type clientAccessCredentialBindingsBulkRequest struct {
	Selection clientAccessCredentialSelection     `json:"selection"`
	Groups    []clientaccess.CredentialGroupInput `json:"groups"`
	DryRun    bool                                `json:"dry_run,omitempty"`
}

type clientAccessCredentialBindingsBulkResponse struct {
	Matched   int  `json:"matched"`
	Updated   int  `json:"updated"`
	Unchanged int  `json:"unchanged"`
	Excluded  int  `json:"excluded"`
	DryRun    bool `json:"dry_run,omitempty"`
}

func (h *Handler) BulkReplaceClientAccessCredentialBindings(c *gin.Context) {
	service := requireClientAccess(c, h)
	if service == nil {
		return
	}
	var input clientAccessCredentialBindingsBulkRequest
	if errBind := c.ShouldBindJSON(&input); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	authIndices, excluded, errResolve := h.resolveClientAccessCredentialSelection(input.Selection)
	if errResolve != nil {
		if errors.Is(errResolve, errClientAccessAuthManagerUnavailable) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": errResolve.Error()})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": errResolve.Error()})
		return
	}
	if input.DryRun {
		c.JSON(http.StatusOK, clientAccessCredentialBindingsBulkResponse{
			Matched:  len(authIndices),
			Excluded: excluded,
			DryRun:   true,
		})
		return
	}
	stats, errReplace := service.ReplaceCredentialBindingsWithStats(c.Request.Context(), clientaccess.CredentialBindingBatch{
		AuthIndices: authIndices,
		Groups:      input.Groups,
	})
	if errReplace != nil {
		writeClientAccessError(c, errReplace)
		return
	}
	c.JSON(http.StatusOK, clientAccessCredentialBindingsBulkResponse{
		Matched:   stats.Matched,
		Updated:   stats.Updated,
		Unchanged: stats.Unchanged,
		Excluded:  excluded,
	})
}

func (h *Handler) resolveClientAccessCredentialSelection(selection clientAccessCredentialSelection) ([]string, int, error) {
	mode := strings.ToLower(strings.TrimSpace(selection.Mode))
	if mode == "" {
		mode = "query"
	}
	if mode != "query" {
		return nil, 0, errors.New("selection mode must be query")
	}
	providers := normalizedStringSet(selection.Providers)
	planTypes := normalizedStringSet(selection.PlanTypes)
	included := normalizedExactStringSet(selection.IncludedAuthIndices)
	if !selection.All && len(providers) == 0 && len(planTypes) == 0 && len(included) == 0 {
		return nil, 0, errors.New("query selection requires all, providers, plan_types, or included_auth_indices")
	}
	if h == nil || h.authManager == nil {
		return nil, 0, errClientAccessAuthManagerUnavailable
	}
	excluded := normalizedExactStringSet(selection.ExcludedAuthIndices)
	candidates := h.clientAccessCredentialSelectionCandidates()
	result := make([]string, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	excludedCount := 0
	for _, candidate := range candidates {
		authIndex := strings.TrimSpace(candidate.authIndex)
		if authIndex == "" {
			continue
		}
		if !selection.All {
			matchesFilters := len(providers) > 0 || len(planTypes) > 0
			if len(providers) > 0 {
				if _, ok := providers[strings.ToLower(strings.TrimSpace(candidate.provider))]; !ok {
					matchesFilters = false
				}
			}
			if len(planTypes) > 0 {
				if _, ok := planTypes[strings.ToLower(strings.TrimSpace(candidate.planType))]; !ok {
					matchesFilters = false
				}
			}
			_, explicitlyIncluded := included[authIndex]
			if !matchesFilters && !explicitlyIncluded {
				continue
			}
		}
		if _, ok := seen[authIndex]; ok {
			continue
		}
		seen[authIndex] = struct{}{}
		if _, ok := excluded[authIndex]; ok {
			excludedCount++
			continue
		}
		result = append(result, authIndex)
	}
	return result, excludedCount, nil
}

func (h *Handler) clientAccessCredentialSelectionCandidates() []authFileCandidate {
	if h == nil || h.authManager == nil {
		return nil
	}
	auths := h.authManager.List()
	result := make([]authFileCandidate, 0, len(auths))
	for _, auth := range auths {
		if auth == nil {
			continue
		}
		auth.EnsureIndex()
		authIndex := strings.TrimSpace(auth.Index)
		if authIndex == "" {
			continue
		}
		result = append(result, authFileCandidate{
			provider:  strings.ToLower(strings.TrimSpace(auth.Provider)),
			planType:  authPlanType(auth),
			authIndex: authIndex,
		})
	}
	return result
}

func normalizedStringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" || value == "all" {
			continue
		}
		result[value] = struct{}{}
	}
	return result
}

func normalizedExactStringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		result[value] = struct{}{}
	}
	return result
}
