package management

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
)

// GetStaticModelDefinitions returns static model metadata for a given channel.
// Channel is provided via path param (:channel) or query param (?channel=...).
func (h *Handler) GetStaticModelDefinitions(c *gin.Context) {
	channel := strings.TrimSpace(c.Param("channel"))
	if channel == "" {
		channel = strings.TrimSpace(c.Query("channel"))
	}
	if channel == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "channel is required"})
		return
	}

	models := registry.GetStaticModelDefinitionsByChannel(channel)
	if models == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unknown channel", "channel": channel})
		return
	}
	if search := strings.ToLower(strings.TrimSpace(c.Query("search"))); search != "" {
		filtered := make([]*registry.ModelInfo, 0, len(models))
		for _, model := range models {
			if model == nil {
				continue
			}
			haystack := strings.ToLower(strings.Join([]string{
				model.ID,
				model.DisplayName,
				model.Name,
				model.Type,
				model.OwnedBy,
			}, "\n"))
			if strings.Contains(haystack, search) {
				filtered = append(filtered, model)
			}
		}
		models = filtered
	}

	page, err := parseConfigCollectionPage(c, len(models))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_pagination", "message": err.Error()})
		return
	}
	paged := make([]*registry.ModelInfo, page.end-page.start)
	copy(paged, models[page.start:page.end])
	response := configCollectionPageMetadata(page)
	response["channel"] = strings.ToLower(strings.TrimSpace(channel))
	response["models"] = paged
	c.JSON(http.StatusOK, response)
}
