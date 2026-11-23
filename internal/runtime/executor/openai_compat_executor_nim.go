package executor

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
	log "github.com/sirupsen/logrus"
)

func (e *OpenAICompatExecutor) executeOpenAINIMStream(
	ctx context.Context,
	auth *cliproxyauth.Auth,
	req cliproxyexecutor.Request,
	opts cliproxyexecutor.Options,
	baseURL, apiKey string,
	from, to, responseFormat sdktranslator.Format,
	translated []byte,
	toolNames map[string]string,
	reporter *helps.UsageReporter,
) (*cliproxyexecutor.StreamResult, error) {
	url := strings.TrimSuffix(baseURL, "/") + "/chat/completions"
	out := make(chan cliproxyexecutor.StreamChunk)
	go func() {
		defer close(out)
		var param any
		originalForTranslate := opts.OriginalRequest
		if len(originalForTranslate) == 0 {
			originalForTranslate = req.Payload
		}
		claudeInputTokens := helps.NewClaudeInputTokenState(from, to, responseFormat, originalForTranslate)
		emitTranslated := func(streamLine []byte) bool {
			chunks := helps.TranslateStreamWithClaudeInputTokens(ctx, to, responseFormat, req.Model, originalForTranslate, translated, streamLine, &param, claudeInputTokens)
			for i := range chunks {
				if !helps.SendOpenAINIMChunk(ctx, out, cliproxyexecutor.StreamChunk{Payload: chunks[i]}) {
					return false
				}
			}
			return true
		}

		if !emitTranslated(helps.OpenAINIMEagerStartLine) {
			return
		}

		stopPings := helps.StartOpenAINIMPings(ctx, out, responseFormat)
		defer stopPings()

		httpReq, errReq := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(translated))
		if errReq != nil {
			reporter.PublishFailure(ctx, errReq)
			_ = helps.SendOpenAINIMChunk(ctx, out, cliproxyexecutor.StreamChunk{Err: errReq})
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "application/json")
		if apiKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+apiKey)
		}
		httpReq.Header.Set("User-Agent", "cli-proxy-openai-compat")
		var attrs map[string]string
		if auth != nil {
			attrs = auth.Attributes
		}
		util.ApplyCustomHeadersFromAttrs(httpReq, attrs, opts.Headers)

		var authID, authLabel, authType, authValue string
		if auth != nil {
			authID = auth.ID
			authLabel = auth.Label
			authType, authValue = auth.AccountInfo()
		}
		helps.RecordAPIRequest(ctx, e.cfg, helps.UpstreamRequestLog{
			URL:       url,
			Method:    http.MethodPost,
			Headers:   httpReq.Header.Clone(),
			Body:      translated,
			Provider:  e.Identifier(),
			AuthID:    authID,
			AuthLabel: authLabel,
			AuthType:  authType,
			AuthValue: authValue,
		})

		httpClient := helps.NewProxyAwareHTTPClient(ctx, e.cfg, auth, 0)
		httpClient = reporter.TrackHTTPClient(httpClient)
		httpResp, errDo := httpClient.Do(httpReq)
		if errDo != nil {
			helps.RecordAPIResponseError(ctx, e.cfg, errDo)
			reporter.PublishFailure(ctx, errDo)
			_ = helps.SendOpenAINIMChunk(ctx, out, cliproxyexecutor.StreamChunk{Err: errDo})
			return
		}
		defer func() {
			if errClose := httpResp.Body.Close(); errClose != nil {
				log.Errorf("openai compat executor: close NIM response body error: %v", errClose)
			}
		}()
		helps.RecordAPIResponseMetadata(ctx, e.cfg, httpResp.StatusCode, httpResp.Header.Clone())
		body, errRead := io.ReadAll(httpResp.Body)
		if errRead != nil {
			helps.RecordAPIResponseError(ctx, e.cfg, errRead)
			reporter.PublishFailure(ctx, errRead)
			_ = helps.SendOpenAINIMChunk(ctx, out, cliproxyexecutor.StreamChunk{Err: errRead})
			return
		}
		helps.AppendAPIResponseChunk(ctx, e.cfg, body)
		if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
			helps.LogWithRequestID(ctx).Debugf("request error, error status: %d, error message: %s", httpResp.StatusCode, helps.SummarizeErrorBody(httpResp.Header.Get("Content-Type"), body))
			streamErr := newOpenAICompatStatusError(httpResp.StatusCode, httpResp.Header, body)
			reporter.PublishFailure(ctx, streamErr)
			_ = helps.SendOpenAINIMChunk(ctx, out, cliproxyexecutor.StreamChunk{Err: streamErr})
			return
		}
		body = helps.RestoreOpenAINIMToolNames(body, toolNames)
		reporter.ObserveResponseModel(body)
		reporter.Publish(ctx, helps.ParseOpenAIUsage(body))
		reporter.EnsurePublished(ctx)

		for _, line := range helps.OpenAIChatCompletionToStreamLines(body) {
			if !emitTranslated(line) {
				return
			}
		}
	}()
	return &cliproxyexecutor.StreamResult{Chunks: out}, nil
}
