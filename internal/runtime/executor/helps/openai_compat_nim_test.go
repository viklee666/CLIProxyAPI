package helps

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/tidwall/gjson"
)

func TestApplyOpenAINIMCompatThinkingAndStreamOptions(t *testing.T) {
	payload := []byte(`{"model":"z-ai/glm5","reasoning_effort":"high","stream":true,"stream_options":{"include_usage":true},"messages":[{"role":"user","content":"hi"}]}`)
	original := []byte(`{"model":"z-ai/glm5","thinking":{"type":"enabled","budget_tokens":2048},"messages":[{"role":"user","content":"hi"}]}`)

	got := ApplyOpenAINIMCompat(payload, original)
	if gjson.GetBytes(got.Payload, "reasoning_effort").Exists() {
		t.Fatalf("reasoning_effort still present: %s", got.Payload)
	}
	if gjson.GetBytes(got.Payload, "stream_options").Exists() {
		t.Fatalf("stream_options still present: %s", got.Payload)
	}
	if !gjson.GetBytes(got.Payload, "chat_template_kwargs.enable_thinking").Bool() {
		t.Fatalf("enable_thinking = false, want true; payload=%s", got.Payload)
	}
	if gotBudget := gjson.GetBytes(got.Payload, "nvext.max_thinking_tokens").Int(); gotBudget != 2048 {
		t.Fatalf("max_thinking_tokens = %d, want 2048", gotBudget)
	}
}

func TestApplyOpenAINIMCompatDisablesThinking(t *testing.T) {
	payload := []byte(`{"model":"m","reasoning_effort":"none","messages":[]}`)
	original := []byte(`{"model":"m","thinking":{"type":"disabled"},"messages":[]}`)
	got := ApplyOpenAINIMCompat(payload, original)
	if gjson.GetBytes(got.Payload, "chat_template_kwargs.enable_thinking").Bool() {
		t.Fatalf("enable_thinking = true, want false; payload=%s", got.Payload)
	}
	if gjson.GetBytes(got.Payload, "nvext").Exists() {
		t.Fatalf("nvext present for disabled thinking: %s", got.Payload)
	}
}

func TestApplyOpenAINIMCompatShortensFunctionNames(t *testing.T) {
	longName := "mcp__very_long_server_name__" + strings.Repeat("tool", 30)
	if utf8.RuneCountInString(longName) <= OpenAINIMMaxFunctionNameLen {
		t.Fatalf("fixture name too short: %d", utf8.RuneCountInString(longName))
	}
	payload := []byte(`{"tools":[{"type":"function","function":{"name":"` + longName + `","parameters":{"type":"object"}}}],"messages":[{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"` + longName + `","arguments":"{}"}}]}],"tool_choice":{"type":"function","function":{"name":"` + longName + `"}}}`)

	got := ApplyOpenAINIMCompat(payload, nil)
	short := gjson.GetBytes(got.Payload, "tools.0.function.name").String()
	if utf8.RuneCountInString(short) > OpenAINIMMaxFunctionNameLen {
		t.Fatalf("shortened name still too long: %q (%d)", short, utf8.RuneCountInString(short))
	}
	if short == longName {
		t.Fatal("function name was not shortened")
	}
	if got.ToolNames[short] != longName {
		t.Fatalf("tool name map[%q] = %q, want %q", short, got.ToolNames[short], longName)
	}
	if gotName := gjson.GetBytes(got.Payload, "messages.0.tool_calls.0.function.name").String(); gotName != short {
		t.Fatalf("history tool name = %q, want %q", gotName, short)
	}
	if gotName := gjson.GetBytes(got.Payload, "tool_choice.function.name").String(); gotName != short {
		t.Fatalf("tool_choice name = %q, want %q", gotName, short)
	}

	upstream := []byte(`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call_2","type":"function","function":{"name":"` + short + `","arguments":"{}"}}]}}]}`)
	restored := RestoreOpenAINIMToolNames(upstream, got.ToolNames)
	if gotName := gjson.GetBytes(restored, "choices.0.message.tool_calls.0.function.name").String(); gotName != longName {
		t.Fatalf("restored name = %q, want %q", gotName, longName)
	}
}

func TestOpenAIChatCompletionToStreamLines(t *testing.T) {
	body := []byte(`{"id":"chatcmpl-1","model":"z-ai/glm5","created":1,"choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":"ok","tool_calls":[{"id":"call_1","type":"function","function":{"name":"Read","arguments":"{\"path\":\"a.go\"}"}}]}}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`)
	lines := OpenAIChatCompletionToStreamLines(body)
	if len(lines) < 5 {
		t.Fatalf("got %d lines, want at least 5: %#v", len(lines), lines)
	}
	joined := string(joinTestBytes(lines))
	if !strings.Contains(joined, `"role":"assistant"`) {
		t.Fatalf("missing role chunk: %s", joined)
	}
	if !strings.Contains(joined, `"content":"ok"`) {
		t.Fatalf("missing content chunk: %s", joined)
	}
	if !strings.Contains(joined, `"name":"Read"`) {
		t.Fatalf("missing tool call chunk: %s", joined)
	}
	if !strings.Contains(joined, `"finish_reason":"tool_calls"`) {
		t.Fatalf("missing finish_reason: %s", joined)
	}
	if string(lines[len(lines)-1]) != "data: [DONE]" {
		t.Fatalf("last line = %q, want data: [DONE]", lines[len(lines)-1])
	}
}

func TestShortenOpenAINIMFunctionNameLeavesShortNames(t *testing.T) {
	if got := ShortenOpenAINIMFunctionName("Read"); got != "Read" {
		t.Fatalf("ShortenOpenAINIMFunctionName(Read) = %q", got)
	}
}

func joinTestBytes(parts [][]byte) []byte {
	n := 0
	for _, part := range parts {
		n += len(part)
	}
	out := make([]byte, 0, n)
	for _, part := range parts {
		out = append(out, part...)
	}
	return out
}
