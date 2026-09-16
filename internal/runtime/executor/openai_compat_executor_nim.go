package executor

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/runtime/executor/helps"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/util"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	log "github.com/sirupsen/logrus"
)

const openAINIMPingInterval = 15 * time.Second

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
				if !sendOpenAINIMChunk(ctx, out, cliproxyexecutor.StreamChunk{Payload: chunks[i]}) {
					return false
				}
			}
			return true
		}

		if !emitTranslated(helps.OpenAINIMEagerStartLine) {
			return
		}

		pingDone := make(chan struct{})
		go emitOpenAINIMPings(ctx, out, responseFormat, pingDone)
		defer close(pingDone)

		httpReq, errReq := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(translated))
		if errReq != nil {
			reporter.PublishFailure(ctx, errReq)
			_ = sendOpenAINIMChunk(ctx, out, cliproxyexecutor.StreamChunk{Err: errReq})
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
			_ = sendOpenAINIMChunk(ctx, out, cliproxyexecutor.StreamChunk{Err: errDo})
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
			_ = sendOpenAINIMChunk(ctx, out, cliproxyexecutor.StreamChunk{Err: errRead})
			return
		}
		helps.AppendAPIResponseChunk(ctx, e.cfg, body)
		if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
			helps.LogWithRequestID(ctx).Debugf("request error, error status: %d, error message: %s", httpResp.StatusCode, helps.SummarizeErrorBody(httpResp.Header.Get("Content-Type"), body))
			streamErr := newOpenAICompatStatusError(httpResp.StatusCode, httpResp.Header, body)
			reporter.PublishFailure(ctx, streamErr)
			_ = sendOpenAINIMChunk(ctx, out, cliproxyexecutor.StreamChunk{Err: streamErr})
			return
		}
		body = helps.RestoreOpenAINIMToolNames(body, toolNames)
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

func emitOpenAINIMPings(ctx context.Context, out chan<- cliproxyexecutor.StreamChunk, responseFormat sdktranslator.Format, done <-chan struct{}) {
	ticker := time.NewTicker(openAINIMPingInterval)
	defer ticker.Stop()
	payload := helps.OpenAINIMClaudePingSSE
	if responseFormat != sdktranslator.FormatClaude {
		payload = []byte(": keep-alive\n\n")
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			if !sendOpenAINIMChunk(ctx, out, cliproxyexecutor.StreamChunk{Payload: payload}) {
				return
			}
		}
	}
}

func sendOpenAINIMChunk(ctx context.Context, out chan<- cliproxyexecutor.StreamChunk, chunk cliproxyexecutor.StreamChunk) bool {
	select {
	case out <- chunk:
		return true
	case <-ctx.Done():
		return false
	}
}
