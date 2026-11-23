package helps

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

const (
	// OpenAINIMMaxFunctionNameLen is the hosted NVIDIA NIM function-name limit.
	OpenAINIMMaxFunctionNameLen  = 96
	openAINIMFunctionNameHashLen = 8
)

// OpenAINIMClaudePingSSE keeps Claude Code alive while hosted NIM is silent.
var OpenAINIMClaudePingSSE = []byte("event: ping\ndata: {\"type\":\"ping\"}\n\n")

// OpenAINIMEagerStartLine makes the Claude translator emit message_start before the upstream first byte.
var OpenAINIMEagerStartLine = []byte(`data: {"id":"chatcmpl-nim-pending","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant"}}]}`)

type OpenAINIMRewrite struct {
	Payload   []byte
	ToolNames map[string]string
}

func OpenAICompatibilityNIMCompat(compat *config.OpenAICompatibility) bool {
	return compat != nil && compat.NIMCompat
}

// ApplyOpenAINIMCompat adapts a standalone request. Executors use the Doc variant
// so user payload rules can run last on the same document.
func ApplyOpenAINIMCompat(payload, originalRequest []byte) OpenAINIMRewrite {
	if len(payload) == 0 {
		return OpenAINIMRewrite{Payload: payload}
	}
	doc := ojson.NewDoc(payload)
	names := ApplyOpenAINIMCompatDoc(doc, originalRequest)
	return OpenAINIMRewrite{Payload: doc.Bytes(), ToolNames: names}
}

// ApplyOpenAINIMCompatDoc normalizes thinking, tool names and stream options before payload rules.
func ApplyOpenAINIMCompatDoc(doc *ojson.Doc, originalRequest []byte) map[string]string {
	if doc == nil {
		return nil
	}
	applyOpenAINIMThinkingDoc(doc, originalRequest)
	_ = doc.Delete("stream_options")
	return rewriteOpenAINIMFunctionNamesDoc(doc)
}

// RestoreOpenAINIMToolNames restores client names in a completion or SSE data payload.
func RestoreOpenAINIMToolNames(payload []byte, shortToOriginal map[string]string) []byte {
	if len(payload) == 0 || len(shortToOriginal) == 0 {
		return payload
	}
	doc := ojson.NewDoc(payload)
	choices := doc.Get("choices")
	if !choices.IsArray() {
		return payload
	}
	for i := range choices.Array() {
		for _, field := range []string{"message", "delta"} {
			rewriteOpenAINIMToolCallArrayDoc(doc, fmt.Sprintf("choices.%d.%s.tool_calls", i, field), func(name string) string {
				if original := shortToOriginal[strings.TrimSpace(name)]; original != "" {
					return original
				}
				return name
			})
		}
	}
	return doc.Bytes()
}

// OpenAIChatCompletionToStreamLines converts a complete response into OpenAI SSE data lines.
func OpenAIChatCompletionToStreamLines(body []byte) [][]byte {
	if len(body) == 0 || !ojson.ValidBytes(body) {
		return [][]byte{[]byte("data: [DONE]")}
	}
	root := ojson.ParseBytes(body)
	newChunk := func() *ojson.Doc {
		doc := ojson.NewDocString(`{"id":"","object":"chat.completion.chunk","created":0,"model":"","choices":[{"index":0,"delta":{},"finish_reason":null}]}`)
		_ = doc.Set("id", root.Get("id").String())
		_ = doc.Set("model", root.Get("model").String())
		if created := root.Get("created"); created.Type == ojson.Number {
			_ = doc.Set("created", created.Int())
		}
		return doc
	}
	var lines [][]byte
	appendChunk := func(doc *ojson.Doc) {
		lines = append(lines, append([]byte("data: "), doc.Bytes()...))
	}
	role := newChunk()
	_ = role.Set("choices.0.delta.role", "assistant")
	appendChunk(role)
	message := root.Get("choices.0.message")
	if reasoning := strings.TrimSpace(message.Get("reasoning_content").String()); reasoning != "" {
		chunk := newChunk()
		_ = chunk.Set("choices.0.delta.reasoning_content", reasoning)
		appendChunk(chunk)
	}
	if content := openAINIMMessageText(message.Get("content")); content != "" {
		chunk := newChunk()
		_ = chunk.Set("choices.0.delta.content", content)
		appendChunk(chunk)
	}
	if toolCalls := message.Get("tool_calls"); toolCalls.IsArray() && len(toolCalls.Array()) > 0 {
		chunk := newChunk()
		_ = chunk.SetRaw("choices.0.delta.tool_calls", toolCalls.Raw)
		for i, toolCall := range toolCalls.Array() {
			if !toolCall.Get("index").Exists() {
				_ = chunk.Set(fmt.Sprintf("choices.0.delta.tool_calls.%d.index", i), i)
			}
		}
		appendChunk(chunk)
	}
	finish := newChunk()
	if reason := root.Get("choices.0.finish_reason").String(); reason != "" {
		_ = finish.Set("choices.0.finish_reason", reason)
	}
	appendChunk(finish)
	if usage := root.Get("usage"); usage.Exists() && usage.Raw != "null" {
		chunk := newChunk()
		_ = chunk.SetRaw("choices", `[]`)
		_ = chunk.SetRaw("usage", usage.Raw)
		appendChunk(chunk)
	}
	lines = append(lines, []byte("data: [DONE]"))
	return lines
}

// ShortenOpenAINIMFunctionName retains a stable hash suffix within NIM's name limit.
func ShortenOpenAINIMFunctionName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) <= OpenAINIMMaxFunctionNameLen {
		return name
	}
	sum := sha256.Sum256([]byte(name))
	suffix := "_" + hex.EncodeToString(sum[:openAINIMFunctionNameHashLen/2])
	keep := OpenAINIMMaxFunctionNameLen - utf8.RuneCountInString(suffix)
	return string([]rune(name)[:keep]) + suffix
}

func applyOpenAINIMThinkingDoc(doc *ojson.Doc, originalRequest []byte) {
	thinking := ojson.GetBytesNoCopy(originalRequest, "thinking")
	effort := strings.ToLower(strings.TrimSpace(doc.Get("reasoning_effort").String()))
	enabled := false
	explicit := thinking.Exists() || effort != ""
	budget := int64(0)
	if thinking.IsObject() {
		switch strings.ToLower(strings.TrimSpace(thinking.Get("type").String())) {
		case "disabled", "none":
		default:
			enabled = true
		}
		if tokens := thinking.Get("budget_tokens"); tokens.Type == ojson.Number {
			budget = tokens.Int()
		}
	} else if effort != "" {
		switch effort {
		case "none", "minimal", "disable", "disabled":
		default:
			enabled = true
		}
	}
	if enabled || explicit {
		_ = doc.Set("chat_template_kwargs.enable_thinking", enabled)
	}
	if enabled && budget > 0 {
		_ = doc.Set("nvext.max_thinking_tokens", budget)
	}
	_ = doc.Delete("reasoning_effort")
}

func rewriteOpenAINIMFunctionNamesDoc(doc *ojson.Doc) map[string]string {
	shortToOriginal := make(map[string]string)
	originalToShort := make(map[string]string)
	shorten := func(name string) string {
		name = strings.TrimSpace(name)
		if mapped, ok := originalToShort[name]; ok {
			return mapped
		}
		short := ShortenOpenAINIMFunctionName(name)
		originalToShort[name] = short
		if short != name {
			shortToOriginal[short] = name
		}
		return short
	}
	for i, tool := range doc.Get("tools").Array() {
		name := tool.Get("function.name").String()
		if name == "" {
			name = tool.Get("name").String()
		}
		if short := shorten(name); short != "" && short != name {
			_ = doc.Set(fmt.Sprintf("tools.%d.function.name", i), short)
		}
	}
	for i := range doc.Get("messages").Array() {
		rewriteOpenAINIMToolCallArrayDoc(doc, fmt.Sprintf("messages.%d.tool_calls", i), shorten)
	}
	if choice := doc.Get("tool_choice"); choice.IsObject() {
		name := choice.Get("function.name").String()
		if short := shorten(name); short != "" && short != name {
			_ = doc.Set("tool_choice.function.name", short)
		}
	}
	if len(shortToOriginal) == 0 {
		return nil
	}
	return shortToOriginal
}

func rewriteOpenAINIMToolCallArrayDoc(doc *ojson.Doc, path string, rewrite func(string) string) {
	for i, toolCall := range doc.Get(path).Array() {
		name := toolCall.Get("function.name").String()
		if mapped := rewrite(name); mapped != "" && mapped != name {
			_ = doc.Set(fmt.Sprintf("%s.%d.function.name", path, i), mapped)
		}
	}
}

func openAINIMMessageText(content ojson.Result) string {
	if content.Type == ojson.String {
		return content.String()
	}
	if !content.IsArray() {
		return ""
	}
	var parts []string
	for _, part := range content.Array() {
		switch {
		case part.Type == ojson.String:
			parts = append(parts, part.String())
		case part.IsObject() && (part.Get("type").String() == "text" || part.Get("type").String() == "output_text"):
			parts = append(parts, part.Get("text").String())
		}
	}
	return strings.Join(parts, "")
}
