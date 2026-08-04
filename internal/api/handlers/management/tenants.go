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
	"github.com/router-for-me/CLIProxyAPI/v7/internal/tenant"
)

func (h *Handler) tenantManagementServices() (*tenant.Service, *clientaccess.Service, func(context.Context) error) {
	if h == nil {
		return nil, nil, nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.tenantService, h.clientAccess, h.tenantAuthSync
}

func requireTenantManagement(c *gin.Context, h *Handler) (*tenant.Service, *clientaccess.Service, func(context.Context) error, bool) {
	tenants, clientAccess, syncAuths := h.tenantManagementServices()
	if tenants == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "tenant service is unavailable"})
		return nil, nil, nil, false
	}
	return tenants, clientAccess, syncAuths, true
}

func managementTenantID(c *gin.Context) (int64, bool) {
	id, errParse := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
	if errParse != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid tenant id"})
		return 0, false
	}
	return id, true
}

type tenantCreateRequest struct {
	DisplayName string `json:"display_name"`
	Password    string `json:"password,omitempty"`
}

type tenantUpdateRequest struct {
	DisplayName *string `json:"display_name,omitempty"`
	Enabled     *bool   `json:"enabled,omitempty"`
}

func (h *Handler) ListTenants(c *gin.Context) {
	tenants, _, _, ok := requireTenantManagement(c, h)
	if !ok {
		return
	}
	items, errList := tenants.List(c.Request.Context())
	if errList != nil {
		writeManagementTenantError(c, errList)
		return
	}
	c.JSON(http.StatusOK, items)
}

func (h *Handler) CreateTenant(c *gin.Context) {
	tenants, _, _, ok := requireTenantManagement(c, h)
	if !ok {
		return
	}
	var request tenantCreateRequest
	if errBind := c.ShouldBindJSON(&request); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	item, password, errCreate := tenants.Create(c.Request.Context(), tenant.CreateInput{DisplayName: request.DisplayName, Password: request.Password})
	if errCreate != nil {
		writeManagementTenantError(c, errCreate)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"tenant": item, "password": password})
}

func (h *Handler) UpdateTenant(c *gin.Context) {
	tenants, _, syncAuths, ok := requireTenantManagement(c, h)
	if !ok {
		return
	}
	id, valid := managementTenantID(c)
	if !valid {
		return
	}
	var request tenantUpdateRequest
	if errBind := c.ShouldBindJSON(&request); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	item, errUpdate := tenants.Update(c.Request.Context(), id, tenant.UpdateInput{DisplayName: request.DisplayName, Enabled: request.Enabled})
	if errUpdate != nil {
		writeManagementTenantError(c, errUpdate)
		return
	}
	if request.Enabled != nil {
		if errSync := runTenantAuthSync(c.Request.Context(), syncAuths); errSync != nil {
			writeManagementTenantError(c, errSync)
			return
		}
	}
	c.JSON(http.StatusOK, item)
}

func (h *Handler) ResetTenantPassword(c *gin.Context) {
	tenants, _, _, ok := requireTenantManagement(c, h)
	if !ok {
		return
	}
	id, valid := managementTenantID(c)
	if !valid {
		return
	}
	password, errReset := tenants.ResetPassword(c.Request.Context(), id)
	if errReset != nil {
		writeManagementTenantError(c, errReset)
		return
	}
	c.JSON(http.StatusOK, gin.H{"password": password})
}

func (h *Handler) DeleteTenant(c *gin.Context) {
	tenants, clientAccess, syncAuths, ok := requireTenantManagement(c, h)
	if !ok {
		return
	}
	id, valid := managementTenantID(c)
	if !valid {
		return
	}
	// Clean the independent client-access database first. Deleting the tenant
	// then removes credential ownership immediately, so either partial outcome
	// remains fail-closed for the provider routing path.
	if clientAccess != nil {
		if errDeleteResources := clientAccess.DeleteTenantResources(c.Request.Context(), id); errDeleteResources != nil {
			writeManagementTenantError(c, errDeleteResources)
			return
		}
	}
	if errDelete := tenants.Delete(c.Request.Context(), id); errDelete != nil {
		writeManagementTenantError(c, errDelete)
		return
	}
	if errSync := runTenantAuthSync(c.Request.Context(), syncAuths); errSync != nil {
		writeManagementTenantError(c, errSync)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) ListTenantProviders(c *gin.Context) {
	tenants, _, _, ok := requireTenantManagement(c, h)
	if !ok {
		return
	}
	id, valid := managementTenantID(c)
	if !valid {
		return
	}
	items, errList := tenants.ListProviderAdminViews(c.Request.Context(), &id)
	if errList != nil {
		writeManagementTenantError(c, errList)
		return
	}
	c.JSON(http.StatusOK, items)
}

func (h *Handler) ListAllTenantProviders(c *gin.Context) {
	tenants, _, _, ok := requireTenantManagement(c, h)
	if !ok {
		return
	}
	items, errList := tenants.ListProviderAdminViews(c.Request.Context(), nil)
	if errList != nil {
		writeManagementTenantError(c, errList)
		return
	}
	c.JSON(http.StatusOK, items)
}

func runTenantAuthSync(ctx context.Context, syncAuths func(context.Context) error) error {
	if syncAuths == nil {
		return errors.New("tenant provider runtime synchronization is unavailable")
	}
	return syncAuths(ctx)
}

func writeManagementTenantError(c *gin.Context, err error) {
	if err == nil {
		return
	}
	switch {
	case errors.Is(err, sql.ErrNoRows):
		c.JSON(http.StatusNotFound, gin.H{"error": "record not found"})
	case strings.Contains(strings.ToLower(err.Error()), "required"),
		strings.Contains(strings.ToLower(err.Error()), "must be"),
		strings.Contains(strings.ToLower(err.Error()), "invalid"):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case strings.Contains(strings.ToLower(err.Error()), "unique constraint"):
		c.JSON(http.StatusConflict, gin.H{"error": "record already exists"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "tenant management request failed"})
	}
}
