package helps

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	// OpenAINIMMaxFunctionNameLen is the hosted NVIDIA NIM function-name limit.
	OpenAINIMMaxFunctionNameLen  = 96
	openAINIMFunctionNameHashLen = 8
)

// OpenAINIMClaudePingSSE is an Anthropic Messages ping event used to keep
// Claude Code alive while hosted NIM is silent.
var OpenAINIMClaudePingSSE = []byte("event: ping\ndata: {\"type\":\"ping\"}\n\n")

// OpenAINIMEagerStartLine is a synthetic OpenAI SSE chunk that makes the
// Claude translator emit message_start before the upstream first byte.
var OpenAINIMEagerStartLine = []byte(`data: {"id":"chatcmpl-nim-pending","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant"}}]}`)

// OpenAINIMRewrite is the NIM-adjusted chat/completions payload plus the
// truncated-to-original function name map for this request.
type OpenAINIMRewrite struct {
	Payload   []byte
	ToolNames map[string]string
}

// OpenAICompatibilityNIMCompat reports whether this openai-compatibility
// provider enables hosted NVIDIA NIM workarounds.
func OpenAICompatibilityNIMCompat(compat *config.OpenAICompatibility) bool {
	return compat != nil && compat.NIMCompat
}

// ApplyOpenAINIMCompat rewrites an OpenAI chat/completions payload for hosted NIM:
// thinking via chat_template_kwargs/nvext, 96-character function names, and no
// stream_options (NIM is not a strict OpenAI clone).
func ApplyOpenAINIMCompat(payload, originalRequest []byte) OpenAINIMRewrite {
	if len(payload) == 0 {
		return OpenAINIMRewrite{Payload: payload}
	}
	out := applyOpenAINIMThinking(payload, originalRequest)
	out = deleteJSONPath(out, "stream_options")
	out, names := rewriteOpenAINIMFunctionNames(out)
	return OpenAINIMRewrite{Payload: out, ToolNames: names}
}

// RestoreOpenAINIMToolNames maps truncated upstream function names back to the
// original client names on a chat/completions JSON body or SSE data payload.
func RestoreOpenAINIMToolNames(payload []byte, shortToOriginal map[string]string) []byte {
	if len(payload) == 0 || len(shortToOriginal) == 0 {
		return payload
	}
	out := payload
	choices := gjson.GetBytes(out, "choices")
	if !choices.Exists() || !choices.IsArray() {
		return out
	}
	choiceIndex := 0
	choices.ForEach(func(_, choice gjson.Result) bool {
		out = restoreOpenAINIMToolCallArray(out, fmt.Sprintf("choices.%d.message.tool_calls", choiceIndex), shortToOriginal)
		out = restoreOpenAINIMToolCallArray(out, fmt.Sprintf("choices.%d.delta.tool_calls", choiceIndex), shortToOriginal)
		choiceIndex++
		return true
	})
	return out
}

// OpenAIChatCompletionToStreamLines turns a complete chat/completions JSON
// body into OpenAI SSE data lines, including [DONE].
func OpenAIChatCompletionToStreamLines(body []byte) [][]byte {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return [][]byte{[]byte("data: [DONE]")}
	}
	root := gjson.ParseBytes(body)
	base := []byte(`{"id":"","object":"chat.completion.chunk","created":0,"model":"","choices":[{"index":0,"delta":{},"finish_reason":null}]}`)
	base = SetStringIfDifferent(base, "id", root.Get("id").String())
	base = SetStringIfDifferent(base, "model", root.Get("model").String())
	if created := root.Get("created"); created.Exists() && created.Type == gjson.Number {
		base = setJSONInt(base, "created", created.Int())
	}

	var lines [][]byte
	appendChunk := func(delta []byte, finishReason string) {
		chunk := append([]byte(nil), base...)
		if len(delta) > 0 {
			updated, errSet := sjson.SetRawBytes(chunk, "choices.0.delta", delta)
			if errSet == nil {
				chunk = updated
			}
		}
		if finishReason != "" {
			chunk = SetStringIfDifferent(chunk, "choices.0.finish_reason", finishReason)
		}
		lines = append(lines, append([]byte("data: "), chunk...))
	}

	appendChunk([]byte(`{"role":"assistant"}`), "")

	message := root.Get("choices.0.message")
	if reasoning := strings.TrimSpace(message.Get("reasoning_content").String()); reasoning != "" {
		delta, errSet := sjson.SetBytes([]byte("{}"), "reasoning_content", reasoning)
		if errSet == nil {
			appendChunk(delta, "")
		}
	}
	if content := openAINIMMessageText(message.Get("content")); content != "" {
		delta, errSet := sjson.SetBytes([]byte("{}"), "content", content)
		if errSet == nil {
			appendChunk(delta, "")
		}
	}
	if toolCalls := message.Get("tool_calls"); toolCalls.Exists() && toolCalls.IsArray() && len(toolCalls.Array()) > 0 {
		items := make([][]byte, 0, len(toolCalls.Array()))
		index := 0
		toolCalls.ForEach(func(_, toolCall gjson.Result) bool {
			raw := []byte(toolCall.Raw)
			if !toolCall.Get("index").Exists() {
				raw = setJSONInt(raw, "index", int64(index))
			}
			items = append(items, raw)
			index++
			return true
		})
		delta, errSet := sjson.SetRawBytes([]byte("{}"), "tool_calls", JoinRawJSONArray(items))
		if errSet == nil {
			appendChunk(delta, "")
		}
	}

	appendChunk([]byte("{}"), root.Get("choices.0.finish_reason").String())

	if usage := root.Get("usage"); usage.Exists() && usage.Raw != "null" {
		usageChunk := append([]byte(nil), base...)
		usageChunk, _ = sjson.SetRawBytes(usageChunk, "choices", []byte("[]"))
		usageChunk, _ = sjson.SetRawBytes(usageChunk, "usage", []byte(usage.Raw))
		lines = append(lines, append([]byte("data: "), usageChunk...))
	}
	lines = append(lines, []byte("data: [DONE]"))
	return lines
}

// ShortenOpenAINIMFunctionName truncates a function name to NIM's 96-character
// limit while keeping a stable hash suffix so collisions stay distinct.
func ShortenOpenAINIMFunctionName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) <= OpenAINIMMaxFunctionNameLen {
		return name
	}
	sum := sha256.Sum256([]byte(name))
	suffix := "_" + hex.EncodeToString(sum[:openAINIMFunctionNameHashLen/2])
	keep := OpenAINIMMaxFunctionNameLen - utf8.RuneCountInString(suffix)
	if keep < 1 {
		return suffix
	}
	runes := []rune(name)
	if keep > len(runes) {
		keep = len(runes)
	}
	return string(runes[:keep]) + suffix
}

func applyOpenAINIMThinking(payload, originalRequest []byte) []byte {
	thinkingNode := gjson.GetBytes(originalRequest, "thinking")
	effort := strings.ToLower(strings.TrimSpace(gjson.GetBytes(payload, "reasoning_effort").String()))
	enabled := false
	explicit := thinkingNode.Exists() || effort != ""
	budget := int64(0)

	if thinkingNode.Exists() && thinkingNode.IsObject() {
		switch strings.ToLower(strings.TrimSpace(thinkingNode.Get("type").String())) {
		case "disabled", "none":
			enabled = false
		default:
			enabled = true
		}
		if tokens := thinkingNode.Get("budget_tokens"); tokens.Exists() && tokens.Type == gjson.Number {
			budget = tokens.Int()
		}
	} else if effort != "" {
		switch effort {
		case "none", "minimal", "disable", "disabled":
			enabled = false
		default:
			enabled = true
		}
	}

	out := payload
	if enabled {
		out = SetBoolIfDifferent(out, "chat_template_kwargs.enable_thinking", true)
		if budget > 0 {
			out = setJSONInt(out, "nvext.max_thinking_tokens", budget)
		}
	} else if explicit {
		out = SetBoolIfDifferent(out, "chat_template_kwargs.enable_thinking", false)
	}
	return deleteJSONPath(out, "reasoning_effort")
}

func rewriteOpenAINIMFunctionNames(payload []byte) ([]byte, map[string]string) {
	shortToOriginal := make(map[string]string)
	originalToShort := make(map[string]string)
	shorten := func(name string) string {
		name = strings.TrimSpace(name)
		if name == "" {
			return name
		}
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

	out := payload
	tools := gjson.GetBytes(payload, "tools")
	if tools.Exists() && tools.IsArray() {
		index := 0
		tools.ForEach(func(_, tool gjson.Result) bool {
			name := tool.Get("function.name").String()
			if name == "" {
				name = tool.Get("name").String()
			}
			if short := shorten(name); short != "" && short != name {
				out = SetStringIfDifferent(out, fmt.Sprintf("tools.%d.function.name", index), short)
			}
			index++
			return true
		})
	}

	messages := gjson.GetBytes(out, "messages")
	if messages.Exists() && messages.IsArray() {
		messageIndex := 0
		messages.ForEach(func(_, message gjson.Result) bool {
			out = rewriteOpenAINIMToolCallArray(out, fmt.Sprintf("messages.%d.tool_calls", messageIndex), shorten)
			messageIndex++
			return true
		})
	}

	if choice := gjson.GetBytes(out, "tool_choice"); choice.Exists() && choice.IsObject() {
		name := choice.Get("function.name").String()
		if short := shorten(name); short != "" && short != name {
			out = SetStringIfDifferent(out, "tool_choice.function.name", short)
		}
	}

	if len(shortToOriginal) == 0 {
		return out, nil
	}
	return out, shortToOriginal
}

func rewriteOpenAINIMToolCallArray(payload []byte, path string, shorten func(string) string) []byte {
	toolCalls := gjson.GetBytes(payload, path)
	if !toolCalls.Exists() || !toolCalls.IsArray() {
		return payload
	}
	out := payload
	index := 0
	toolCalls.ForEach(func(_, toolCall gjson.Result) bool {
		name := toolCall.Get("function.name").String()
		if short := shorten(name); short != "" && short != name {
			out = SetStringIfDifferent(out, fmt.Sprintf("%s.%d.function.name", path, index), short)
		}
		index++
		return true
	})
	return out
}

func restoreOpenAINIMToolCallArray(payload []byte, path string, shortToOriginal map[string]string) []byte {
	toolCalls := gjson.GetBytes(payload, path)
	if !toolCalls.Exists() || !toolCalls.IsArray() {
		return payload
	}
	out := payload
	index := 0
	toolCalls.ForEach(func(_, toolCall gjson.Result) bool {
		name := strings.TrimSpace(toolCall.Get("function.name").String())
		if original, ok := shortToOriginal[name]; ok && original != "" && original != name {
			out = SetStringIfDifferent(out, fmt.Sprintf("%s.%d.function.name", path, index), original)
		}
		index++
		return true
	})
	return out
}

func openAINIMMessageText(content gjson.Result) string {
	if !content.Exists() || content.Type == gjson.Null {
		return ""
	}
	if content.Type == gjson.String {
		return content.String()
	}
	if !content.IsArray() {
		return ""
	}
	var parts []string
	content.ForEach(func(_, part gjson.Result) bool {
		switch {
		case part.Type == gjson.String:
			if text := part.String(); text != "" {
				parts = append(parts, text)
			}
		case part.IsObject() && (part.Get("type").String() == "text" || part.Get("type").String() == "output_text"):
			if text := part.Get("text").String(); text != "" {
				parts = append(parts, text)
			}
		}
		return true
	})
	return strings.Join(parts, "")
}

func deleteJSONPath(payload []byte, path string) []byte {
	if !gjson.GetBytes(payload, path).Exists() {
		return payload
	}
	updated, errDelete := sjson.DeleteBytes(payload, path)
	if errDelete != nil {
		return payload
	}
	return updated
}

func setJSONInt(payload []byte, path string, value int64) []byte {
	current := gjson.GetBytes(payload, path)
	if current.Type == gjson.Number && current.Int() == value {
		return payload
	}
	updated, errSet := sjson.SetBytes(payload, path, value)
	if errSet != nil {
		return payload
	}
	return updated
}
