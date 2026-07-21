package management

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/clientaccess"
)

func (h *Handler) clientAccessService() *clientaccess.Service {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.clientAccess
}

func (h *Handler) deleteClientAccessCredentialBindings(ctx context.Context, authIndices []string) error {
	return deleteClientAccessCredentialBindings(ctx, h.clientAccessService(), authIndices)
}

func deleteClientAccessCredentialBindings(ctx context.Context, service *clientaccess.Service, authIndices []string) error {
	if service == nil || len(authIndices) == 0 {
		return nil
	}
	return service.DeleteCredentialBindings(ctx, authIndices)
}

func requireClientAccess(c *gin.Context, h *Handler) *clientaccess.Service {
	service := h.clientAccessService()
	if service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "client access is disabled"})
		return nil
	}
	return service
}

func clientAccessListOptions(c *gin.Context) clientaccess.ListOptions {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	options := clientaccess.ListOptions{
		Page:     page,
		PageSize: pageSize,
		Search:   strings.TrimSpace(c.Query("search")),
	}
	if raw := strings.TrimSpace(c.Query("enabled")); raw != "" {
		if value, errParse := strconv.ParseBool(raw); errParse == nil {
			options.Enabled = &value
		}
	}
	seenAuthIndices := make(map[string]struct{})
	for _, raw := range append(c.QueryArray("auth_index"), strings.Split(c.Query("auth_indices"), ",")...) {
		authIndex := strings.TrimSpace(raw)
		if authIndex == "" {
			continue
		}
		if _, ok := seenAuthIndices[authIndex]; ok {
			continue
		}
		seenAuthIndices[authIndex] = struct{}{}
		options.AuthIndices = append(options.AuthIndices, authIndex)
	}
	seenGroupIDs := make(map[int64]struct{})
	for _, raw := range append(c.QueryArray("group_id"), strings.Split(c.Query("group_ids"), ",")...) {
		groupID, errParse := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if errParse != nil || groupID <= 0 {
			continue
		}
		if _, ok := seenGroupIDs[groupID]; ok {
			continue
		}
		seenGroupIDs[groupID] = struct{}{}
		options.GroupIDs = append(options.GroupIDs, groupID)
	}
	return options
}

func clientAccessID(c *gin.Context) (int64, bool) {
	id, errParse := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
	if errParse != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return 0, false
	}
	return id, true
}

func writeClientAccessError(c *gin.Context, err error) {
	if err == nil {
		return
	}
	switch {
	case errors.Is(err, sql.ErrNoRows):
		c.JSON(http.StatusNotFound, gin.H{"error": "record not found"})
	case strings.Contains(strings.ToLower(err.Error()), "unique constraint"):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case strings.Contains(strings.ToLower(err.Error()), "required"),
		strings.Contains(strings.ToLower(err.Error()), "cannot"),
		strings.Contains(strings.ToLower(err.Error()), "must contain"),
		strings.Contains(strings.ToLower(err.Error()), "do not exist"):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}

func (h *Handler) ListClientAccessGroups(c *gin.Context) {
	service := requireClientAccess(c, h)
	if service == nil {
		return
	}
	page, errList := service.ListGroups(c.Request.Context(), clientAccessListOptions(c))
	if errList != nil {
		writeClientAccessError(c, errList)
		return
	}
	c.JSON(http.StatusOK, page)
}

func (h *Handler) CreateClientAccessGroup(c *gin.Context) {
	service := requireClientAccess(c, h)
	if service == nil {
		return
	}
	var input clientaccess.GroupCreate
	if errBind := c.ShouldBindJSON(&input); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	group, errCreate := service.CreateGroup(c.Request.Context(), input)
	if errCreate != nil {
		writeClientAccessError(c, errCreate)
		return
	}
	c.JSON(http.StatusCreated, group)
}

func (h *Handler) GetClientAccessGroup(c *gin.Context) {
	service := requireClientAccess(c, h)
	if service == nil {
		return
	}
	id, ok := clientAccessID(c)
	if !ok {
		return
	}
	group, errGet := service.GetGroup(c.Request.Context(), id)
	if errGet != nil {
		writeClientAccessError(c, errGet)
		return
	}
	c.JSON(http.StatusOK, group)
}

func (h *Handler) UpdateClientAccessGroup(c *gin.Context) {
	service := requireClientAccess(c, h)
	if service == nil {
		return
	}
	id, ok := clientAccessID(c)
	if !ok {
		return
	}
	var input clientaccess.GroupUpdate
	if errBind := c.ShouldBindJSON(&input); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	group, errUpdate := service.UpdateGroup(c.Request.Context(), id, input)
	if errUpdate != nil {
		writeClientAccessError(c, errUpdate)
		return
	}
	c.JSON(http.StatusOK, group)
}

func (h *Handler) DeleteClientAccessGroup(c *gin.Context) {
	service := requireClientAccess(c, h)
	if service == nil {
		return
	}
	id, ok := clientAccessID(c)
	if !ok {
		return
	}
	if errDelete := service.DeleteGroup(c.Request.Context(), id); errDelete != nil {
		writeClientAccessError(c, errDelete)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) ListClientAccessKeys(c *gin.Context) {
	service := requireClientAccess(c, h)
	if service == nil {
		return
	}
	page, errList := service.ListKeys(c.Request.Context(), clientAccessListOptions(c))
	if errList != nil {
		writeClientAccessError(c, errList)
		return
	}
	c.JSON(http.StatusOK, page)
}

func (h *Handler) CreateClientAccessKey(c *gin.Context) {
	service := requireClientAccess(c, h)
	if service == nil {
		return
	}
	var input clientaccess.KeyCreate
	if errBind := c.ShouldBindJSON(&input); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	key, errCreate := service.CreateKey(c.Request.Context(), input)
	if errCreate != nil {
		writeClientAccessError(c, errCreate)
		return
	}
	c.JSON(http.StatusCreated, key)
}

func (h *Handler) GetClientAccessKey(c *gin.Context) {
	service := requireClientAccess(c, h)
	if service == nil {
		return
	}
	id, ok := clientAccessID(c)
	if !ok {
		return
	}
	key, errGet := service.GetKey(c.Request.Context(), id)
	if errGet != nil {
		writeClientAccessError(c, errGet)
		return
	}
	c.JSON(http.StatusOK, key)
}

func (h *Handler) UpdateClientAccessKey(c *gin.Context) {
	service := requireClientAccess(c, h)
	if service == nil {
		return
	}
	id, ok := clientAccessID(c)
	if !ok {
		return
	}
	var input clientaccess.KeyUpdate
	if errBind := c.ShouldBindJSON(&input); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	key, errUpdate := service.UpdateKey(c.Request.Context(), id, input)
	if errUpdate != nil {
		writeClientAccessError(c, errUpdate)
		return
	}
	c.JSON(http.StatusOK, key)
}

func (h *Handler) DeleteClientAccessKey(c *gin.Context) {
	service := requireClientAccess(c, h)
	if service == nil {
		return
	}
	id, ok := clientAccessID(c)
	if !ok {
		return
	}
	if errDelete := service.DeleteKey(c.Request.Context(), id); errDelete != nil {
		writeClientAccessError(c, errDelete)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) ListClientAccessCredentialBindings(c *gin.Context) {
	service := requireClientAccess(c, h)
	if service == nil {
		return
	}
	page, errList := service.ListCredentialBindings(c.Request.Context(), clientAccessListOptions(c))
	if errList != nil {
		writeClientAccessError(c, errList)
		return
	}
	c.JSON(http.StatusOK, page)
}

func (h *Handler) ReplaceClientAccessCredentialBindings(c *gin.Context) {
	service := requireClientAccess(c, h)
	if service == nil {
		return
	}
	var input clientaccess.CredentialBindingBatch
	if errBind := c.ShouldBindJSON(&input); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	if errReplace := service.ReplaceCredentialBindings(c.Request.Context(), input); errReplace != nil {
		writeClientAccessError(c, errReplace)
		return
	}
	c.JSON(http.StatusOK, gin.H{"updated": len(input.AuthIndices)})
}

func (h *Handler) ReplaceClientAccessGroupCredentialBindings(c *gin.Context) {
	service := requireClientAccess(c, h)
	if service == nil {
		return
	}
	groupID, ok := clientAccessID(c)
	if !ok {
		return
	}
	var input struct {
		AuthIndices []string                         `json:"auth_indices"`
		Priority    int                              `json:"priority"`
		Selection   *clientAccessCredentialSelection `json:"selection,omitempty"`
		DryRun      bool                             `json:"dry_run,omitempty"`
	}
	if errBind := c.ShouldBindJSON(&input); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	authIndices := input.AuthIndices
	excluded := 0
	if input.Selection != nil {
		var errResolve error
		authIndices, excluded, errResolve = h.resolveClientAccessCredentialSelection(*input.Selection)
		if errResolve != nil {
			if errors.Is(errResolve, errClientAccessAuthManagerUnavailable) {
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": errResolve.Error()})
				return
			}
			c.JSON(http.StatusBadRequest, gin.H{"error": errResolve.Error()})
			return
		}
	}
	if input.DryRun {
		c.JSON(http.StatusOK, clientAccessCredentialBindingsBulkResponse{
			Matched:  len(authIndices),
			Excluded: excluded,
			DryRun:   true,
		})
		return
	}
	stats, errReplace := service.ReplaceGroupCredentialBindings(c.Request.Context(), groupID, clientaccess.GroupCredentialBindingBatch{
		AuthIndices: authIndices,
		Priority:    input.Priority,
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
