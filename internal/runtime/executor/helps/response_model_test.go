package helps

import (
	"context"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

func TestUsageReporterObservesUpstreamResponseModel(t *testing.T) {
	for _, tc := range []struct {
		name, provider, payload, want string
	}{
		{"codex_created", "codex", `data: {"type":"response.created","response":{"model":"served-codex"}}`, "served-codex"},
		{"codex_completed", "codex", `{"type":"response.completed","response":{"model":"served-codex"}}`, "served-codex"},
		{"codex_compact", "codex", `{"object":"response.compaction","model":"served-compact"}`, "served-compact"},
		{"codex_tool_is_not_model", "codex", `{"type":"response.output_item.added","response":{"model":"tool-model"},"item":{"type":"function_call"}}`, ""},
		{"claude_start", "claude", `{"type":"message_start","message":{"model":"served-claude"}}`, "served-claude"},
		{"claude_fallback", "claude", `{"type":"content_block_start","content_block":{"type":"fallback","to":{"model":"served-fallback"}}}`, "served-fallback"},
		{"claude_iterations", "claude", `{"type":"message","model":"initial","usage":{"iterations":[{"type":"fallback_message","model":"served-fallback"}]}}`, "served-fallback"},
		{"gemini", "gemini", `{"modelVersion":"served-gemini","candidates":[]}`, "served-gemini"},
		{"antigravity", "antigravity", `{"response":{"modelVersion":"served-gemini","candidates":[]}}`, "served-gemini"},
		{"interactions", "gemini-interactions", `{"event_type":"interaction.completed","interaction":{"model":"served-interactions","status":"completed"}}`, "served-interactions"},
		{"compatible", "custom-provider", `{"object":"chat.completion","model":"served-chat","choices":[]}`, "served-chat"},
		{"responses", "xai", `{"type":"response.completed","response":{"model":"served-responses"}}`, "served-responses"},
		{"no_fallback_to_request", "codex", `{"type":"response.created","response":{"id":"resp_1"}}`, ""},
		{"invalid_json", "openai", `{"model":"untrusted"`, ""},
		{"nonstring", "openai", `{"model":23}`, ""},
		{"oversized", "openai", `{"model":"` + strings.Repeat("x", maxResponseModelLength+1) + `"}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reporter := NewUsageReporter(context.Background(), tc.provider, "requested-model", nil)
			reporter.ObserveResponseModel([]byte(tc.payload))
			if got := reporter.ResponseModel(); got != tc.want {
				t.Fatalf("ResponseModel = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestUsageReporterResponseModelSurvivesTokenTimingAndFinalizes(t *testing.T) {
	reporter := NewUsageReporter(context.Background(), "codex", "requested-model", nil)
	ObserveResponsesTokenEvent(reporter, []byte(`{"type":"response.created","response":{"model":"initial-model"}}`))
	ObserveResponsesTokenEvent(reporter, []byte(`{"type":"response.output_text.delta","delta":"hello"}`))
	ObserveResponsesTokenEvent(reporter, []byte(`{"type":"response.completed","response":{"model":"final-model"}}`))
	reporter.SetResponseModel("too-late")
	reporter.ObserveResponseModel([]byte(`{"type":"response.created","response":{"model":"also-too-late"}}`))
	if got := reporter.ResponseModel(); got != "final-model" || !reporter.IsResponseModelFinal() {
		t.Fatalf("final response model = %q, final=%t", got, reporter.IsResponseModelFinal())
	}
}

func TestUsageReporterResponseModelOwnsReusableBuffer(t *testing.T) {
	reporter := NewUsageReporter(context.Background(), "openai", "requested-model", nil)
	buffer := []byte(`{"model":"served-model"}`)
	reporter.ObserveResponseModel(buffer)
	for i := range buffer {
		buffer[i] = 'x'
	}
	if got := reporter.ResponseModel(); got != "served-model" {
		t.Fatalf("reused buffer changed the response model to %q", got)
	}
}

func TestUsageReporterResponseModelOnlyLabelsPrimaryUsage(t *testing.T) {
	reporter := NewUsageReporter(context.Background(), "codex", "requested-model", nil)
	reporter.SetResponseModel("served-model")
	record := reporter.buildRecord(usage.Detail{TotalTokens: 5}, false)
	if record.Model != "requested-model" || record.ResponseModel != "served-model" {
		t.Fatalf("usage model pair = (%q, %q)", record.Model, record.ResponseModel)
	}
	additional, ok := reporter.buildAdditionalModelRecord("gpt-image-1", usage.Detail{TotalTokens: 3})
	if !ok || additional.ResponseModel != "" {
		t.Fatalf("additional-model record inherited the response model: %+v", additional)
	}
}

func TestStreamResponseModelObserverChunkBoundaries(t *testing.T) {
	reporter := NewUsageReporter(context.Background(), "codex", "requested-model", nil)
	observer := NewStreamResponseModelObserver(reporter)
	payload := []byte("event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"model\":\"served-model\"}}\n\n")
	for len(payload) > 0 {
		n := min(3, len(payload))
		observer.Feed(payload[:n])
		payload = payload[n:]
	}
	observer.Finish()
	if got := reporter.ResponseModel(); got != "served-model" {
		t.Fatalf("split transport model = %q", got)
	}
}

func TestStreamResponseModelObserverMultilineAndOverflow(t *testing.T) {
	for _, tc := range []struct{ name, prefix string }{
		{"multiline", "data: {\"object\":\"chat.completion\",\n"},
		{"overflow", "data: " + strings.Repeat("x", maxStreamResponseModelBytes+1) + "\n\ndata: {\"object\":\"chat.completion\",\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reporter := NewUsageReporter(context.Background(), "openai", "requested-model", nil)
			observer := NewStreamResponseModelObserver(reporter)
			observer.Feed([]byte(tc.prefix + "data: \"model\":\"served-model\"}\n\n"))
			observer.Finish()
			if got := reporter.ResponseModel(); got != "served-model" {
				t.Fatalf("multiline response model = %q", got)
			}
		})
	}
}

func TestIsResponsesIncompletePreservesUnknownReasons(t *testing.T) {
	for _, tc := range []struct {
		payload string
		want    bool
	}{
		{`{"type":"response.incomplete","response":{"incomplete_details":{"reason":"provider_specific"}}}`, true},
		{`{"type":"response.incomplete","response":{}}`, true},
		{`{"object":"response","status":"incomplete"}`, true},
		{`{"response":{"status":"incomplete"}}`, true},
		{`{"type":"response.completed","response":{"status":"completed"}}`, false},
		{`{"choices":[{"finish_reason":"stop"}]}`, false},
	} {
		if got := IsResponsesIncomplete([]byte(tc.payload)); got != tc.want {
			t.Fatalf("IsResponsesIncomplete(%s) = %t, want %t", tc.payload, got, tc.want)
		}
	}
}
