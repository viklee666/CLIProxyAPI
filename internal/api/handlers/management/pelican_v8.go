package management

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/MoeForge/moego/json"
	"github.com/MoeForge/moego/ojson"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/clienterror"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

type pelicanRequest struct {
	AuthIndex string `json:"auth_index"`
	Name      string `json:"name"`
	Model     string `json:"model"`
	Prompt    string `json:"prompt"`
	Effort    string `json:"effort"`
}

// PelicanTestV8 generates an operator-requested comparison artifact on exactly one
// managed credential. It never accepts upstream credentials or endpoints from the UI.
func (h *Handler) PelicanTestV8(c *gin.Context) {
	if h == nil || h.authManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "auth manager is not available"})
		return
	}
	body, errRead := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 128*1024))
	if errRead != nil {
		status := http.StatusBadRequest
		var tooLarge *http.MaxBytesError
		if errors.As(errRead, &tooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		c.JSON(status, gin.H{"error": "invalid or oversized request body"})
		return
	}
	var input pelicanRequest
	if errDecode := json.Unmarshal(body, &input); errDecode != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	input.AuthIndex = strings.TrimSpace(input.AuthIndex)
	input.Name = strings.TrimSpace(input.Name)
	input.Model = strings.TrimSpace(input.Model)
	if (input.AuthIndex == "" && input.Name == "") || input.Model == "" || len(input.Model) > 512 || strings.TrimSpace(input.Prompt) == "" || len(input.Prompt) > 100000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "target, model and prompt are required; model and prompt must fit their size limits"})
		return
	}
	switch input.Effort {
	case "low", "medium", "high", "xhigh":
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "effort must be low, medium, high or xhigh"})
		return
	}
	target, found := h.lookupAuthFile(input.Name, input.AuthIndex)
	if !found || target == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "test target not found"})
		return
	}
	if target.Disabled || target.Status == coreauth.StatusDisabled || target.RequiresSignIn {
		c.JSON(http.StatusConflict, gin.H{"error": "test target is disabled or requires sign-in"})
		return
	}

	// Reuse the normal translation, payload rules, proxy and refresh pipeline. Only
	// the fixed target is eligible; neither retries nor session reuse can hide failures.
	doc := ojson.NewDocString(`{"model":"","messages":[{"role":"user","content":""}],"stream":false,"store":false}`)
	_ = doc.Set("model", input.Model)
	_ = doc.Set("messages.0.content", input.Prompt)
	_ = doc.Set("reasoning_effort", input.Effort)
	payload := doc.Bytes()
	sessionID := "pelican-" + uuid.NewString()
	ctx := coreexecutor.WithRequestRetryState(c.Request.Context())
	coreexecutor.DisableRequestRetries(ctx)
	opts := coreexecutor.Options{
		SourceFormat:    sdktranslator.FormatOpenAI,
		ResponseFormat:  sdktranslator.FormatOpenAI,
		OriginalRequest: payload,
		Headers:         http.Header{"X-Session-Id": {sessionID}},
		Metadata: map[string]any{
			coreexecutor.PinnedAuthMetadataKey:           target.ID,
			coreexecutor.ClientAllowAllGroupsMetadataKey: true,
			coreexecutor.CanonicalSessionIDMetadataKey:   sessionID,
			coreexecutor.RequestedModelMetadataKey:       input.Model,
			coreexecutor.ReasoningEffortMetadataKey:      input.Effort,
		},
	}
	started := time.Now()
	response, errExecute := h.authManager.Execute(ctx, []string{target.Provider}, coreexecutor.Request{
		Model: input.Model, Payload: payload, Format: sdktranslator.FormatOpenAI,
	}, opts)
	if errExecute != nil {
		upstreamStatus := clienterror.HTTPStatusFromErrorOr(errExecute, http.StatusBadGateway)
		status := upstreamStatus
		// A target's rejected credential must not sign the operator out of the UI.
		if status < 400 || status > 599 || status == http.StatusUnauthorized || status == http.StatusForbidden {
			status = http.StatusBadGateway
		}
		c.JSON(status, gin.H{"error": coreauth.SanitizeRenewalMessage(errExecute.Error()), "upstream_status": upstreamStatus})
		return
	}
	answer, responseModel, usage, errAnswer := pelicanAnswer(response)
	if errAnswer != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": errAnswer.Error()})
		return
	}
	if responseModel == "" {
		responseModel = input.Model
	}
	c.JSON(http.StatusOK, gin.H{
		"auth_index":  target.Index,
		"name":        firstNonEmpty(target.FileName, target.ID),
		"model":       responseModel,
		"answer":      answer,
		"duration_ms": time.Since(started).Milliseconds(),
		"usage":       usage,
	})
}

func pelicanAnswer(response coreexecutor.Response) (string, string, json.RawMessage, error) {
	if response.Incomplete {
		return "", "", nil, errors.New("test generation was incomplete")
	}
	if len(response.Payload) > 8*1024*1024 || !json.Valid(response.Payload) {
		return "", "", nil, errors.New("invalid or oversized test response")
	}
	root := ojson.ParseBytes(response.Payload)
	choice := root.Get("choices.0")
	switch choice.Get("finish_reason").String() {
	case "length", "content_filter", "tool_calls", "function_call":
		return "", "", nil, errors.New("test generation did not produce a complete answer")
	}
	content := choice.Get("message.content")
	var answer string
	if content.Type == ojson.String {
		answer = content.String()
	} else if content.IsArray() {
		var text strings.Builder
		content.ForEach(func(_, part ojson.Result) bool {
			if part.Get("type").String() == "text" {
				text.WriteString(part.Get("text").String())
			}
			return true
		})
		answer = text.String()
	}
	if strings.TrimSpace(answer) == "" {
		return "", "", nil, errors.New("test generation returned no text answer")
	}
	var usage json.RawMessage
	if value := root.Get("usage"); value.IsObject() {
		usage = json.RawMessage(value.Raw)
	}
	return answer, root.Get("model").String(), usage, nil
}
