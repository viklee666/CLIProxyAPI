package management

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	defaultConfigCollectionPageSize = 100
	maxConfigCollectionPageSize     = 200
	maxConfigCollectionBodyBytes    = 4 * 1024 * 1024
	maxConfigCollectionItems        = 10_000
)

type configCollectionPage struct {
	page       int
	pageSize   int
	total      int
	totalPages int
	start      int
	end        int
}

func parseConfigCollectionPage(c *gin.Context, total int) (configCollectionPage, error) {
	query := configCollectionPage{
		page:     1,
		pageSize: defaultConfigCollectionPageSize,
		total:    total,
	}

	if rawPage := strings.TrimSpace(c.Query("page")); rawPage != "" {
		page, err := strconv.Atoi(rawPage)
		if err != nil || page < 1 {
			return query, fmt.Errorf("page must be a positive integer")
		}
		query.page = page
	}
	if rawPageSize := strings.TrimSpace(c.Query("page_size")); rawPageSize != "" {
		pageSize, err := strconv.Atoi(rawPageSize)
		if err != nil || pageSize < 1 || pageSize > maxConfigCollectionPageSize {
			return query, fmt.Errorf("page_size must be an integer between 1 and %d", maxConfigCollectionPageSize)
		}
		query.pageSize = pageSize
	}

	if total > 0 {
		query.totalPages = (total + query.pageSize - 1) / query.pageSize
	}
	if query.page > query.totalPages && query.totalPages > 0 {
		query.start = total
		query.end = total
		return query, nil
	}
	if query.totalPages == 0 {
		return query, nil
	}
	query.start = (query.page - 1) * query.pageSize
	if query.start > total {
		query.start = total
	}
	query.end = query.start + query.pageSize
	if query.end > total {
		query.end = total
	}
	return query, nil
}

func configCollectionPageMetadata(page configCollectionPage) gin.H {
	return gin.H{
		"page":        page.page,
		"page_size":   page.pageSize,
		"total":       page.total,
		"total_pages": page.totalPages,
		"has_more":    page.end < page.total,
	}
}

func writeConfigCollectionPage[T any](c *gin.Context, responseKey string, items []T) {
	page, err := parseConfigCollectionPage(c, len(items))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_pagination", "message": err.Error()})
		return
	}

	paged := make([]T, page.end-page.start)
	copy(paged, items[page.start:page.end])
	response := configCollectionPageMetadata(page)
	response[responseKey] = paged
	c.JSON(http.StatusOK, response)
}

func readConfigCollectionBody(c *gin.Context) ([]byte, bool) {
	body, err := readManagementRequestBody(c, maxConfigCollectionBodyBytes)
	if err == nil {
		return body, true
	}
	if isManagementRequestTooLarge(err) {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{
			"error":   "request_too_large",
			"message": fmt.Sprintf("config collection request must not exceed %d bytes", maxConfigCollectionBodyBytes),
		})
		return nil, false
	}
	c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read body"})
	return nil, false
}

func bindConfigCollectionJSON(c *gin.Context, target any) bool {
	limitManagementRequestBody(c, maxConfigCollectionBodyBytes)
	if err := c.ShouldBindJSON(target); err != nil {
		if isManagementRequestTooLarge(err) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{
				"error":   "request_too_large",
				"message": fmt.Sprintf("config collection request must not exceed %d bytes", maxConfigCollectionBodyBytes),
			})
			return false
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return false
	}
	return true
}

func validateConfigCollectionSize(c *gin.Context, count int) bool {
	if count <= maxConfigCollectionItems {
		return true
	}
	c.JSON(http.StatusRequestEntityTooLarge, gin.H{
		"error":   "too_many_items",
		"message": fmt.Sprintf("config collection must not contain more than %d items", maxConfigCollectionItems),
	})
	return false
}
