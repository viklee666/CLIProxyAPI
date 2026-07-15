package management

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

var errManagementBodyTooLarge = errors.New("management body too large")

const maxDefaultManagementRequestBytes = 4 * 1024 * 1024

// RequestBodyLimitMiddleware applies a default transport boundary to management
// handlers whose semantic payloads are small. Endpoints with a larger dedicated
// limit keep ownership of their own MaxBytesReader. /api-call is deliberately
// excluded because its request/response behavior is part of the frozen LLM probe
// boundary for this refactor.
func (h *Handler) RequestBodyLimitMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c == nil || c.Request == nil || c.Request.Body == nil || managementRequestUsesDedicatedLimit(c.Request.Method, c.Request.URL.Path) {
			c.Next()
			return
		}
		if c.Request.ContentLength > maxDefaultManagementRequestBytes {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{
				"error":   "request_too_large",
				"message": fmt.Sprintf("management request must not exceed %d bytes", maxDefaultManagementRequestBytes),
			})
			return
		}
		limitManagementRequestBody(c, maxDefaultManagementRequestBytes)
		c.Next()
	}
}

func managementRequestUsesDedicatedLimit(method, path string) bool {
	path = strings.TrimRight(strings.TrimSpace(path), "/")
	switch {
	case method == http.MethodPost && path == "/v0/management/api-call":
		return true
	case method == http.MethodPost && path == "/v0/management/auth-files":
		return true
	case method == http.MethodPut && path == "/v0/management/config.yaml":
		return true
	default:
		return false
	}
}

func readManagementRequestBody(c *gin.Context, maxBytes int64) ([]byte, error) {
	if c == nil || c.Request == nil || c.Request.Body == nil {
		return nil, nil
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
	body, errRead := io.ReadAll(c.Request.Body)
	if errRead != nil {
		if isManagementRequestTooLarge(errRead) {
			return nil, errManagementBodyTooLarge
		}
		return nil, errRead
	}
	return body, nil
}

func limitManagementRequestBody(c *gin.Context, maxBytes int64) {
	if c == nil || c.Request == nil || c.Request.Body == nil {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
}

func isManagementRequestTooLarge(err error) bool {
	if errors.Is(err, errManagementBodyTooLarge) {
		return true
	}
	var maxBytesErr *http.MaxBytesError
	return errors.As(err, &maxBytesErr)
}

func readManagementResponseBody(body io.Reader, maxBytes int64) ([]byte, error) {
	if body == nil {
		return nil, nil
	}
	data, errRead := io.ReadAll(io.LimitReader(body, maxBytes+1))
	if errRead != nil {
		return nil, errRead
	}
	if int64(len(data)) > maxBytes {
		return nil, errManagementBodyTooLarge
	}
	return data, nil
}
