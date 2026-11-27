package auth

import (
	"bytes"
	"strings"

	"github.com/tidwall/gjson"
)

// Completed generation output, rather than a successful transport or a partial
// response, is the recovery signal for the priority health tracker.
func streamPayloadCompletionFlags(payload []byte) (completed, incomplete bool) {
	for len(payload) > 0 {
		line, rest, _ := bytes.Cut(payload, []byte{'\n'})
		payload = rest
		line = bytes.TrimSpace(line)
		if bytes.HasPrefix(line, []byte("data:")) {
			line = bytes.TrimSpace(line[5:])
		}
		if len(line) < 2 || line[0] != '{' || line[len(line)-1] != '}' {
			continue
		}
		comp, incomp := jsonCompletionFlags(gjson.ParseBytes(line))
		completed = completed || comp
		incomplete = incomplete || incomp
	}
	return completed, incomplete
}

func anthropicStopReasonFlags(reason string) (completed, incomplete bool) {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "end_turn", "stop_sequence", "tool_use":
		return true, false
	case "max_tokens", "stopsequencemaxtokens", "refusal", "model_context_window_exceeded", "pause_turn":
		return false, true
	default:
		return false, false
	}
}

func geminiCompletionFlags(candidates gjson.Result) (completed, incomplete bool) {
	candidates.ForEach(func(_, candidate gjson.Result) bool {
		switch strings.ToUpper(strings.TrimSpace(candidate.Get("finishReason").String())) {
		case "STOP":
			completed = true
		case "MAX_TOKENS", "SAFETY", "RECITATION", "OTHER", "MALFORMED_FUNCTION_CALL",
			"UNEXPECTED_TOOL_CALL", "MALFORMED_RESPONSE", "PROHIBITED_CONTENT", "BLOCKLIST",
			"SPII", "IMAGE_SAFETY", "LANGUAGE", "NO_IMAGE":
			incomplete = true
		}
		return true
	})
	return completed, incomplete
}

func jsonCompletionFlags(obj gjson.Result) (completed, incomplete bool) {
	if !obj.IsObject() {
		return false, false
	}
	if value := obj.Get("error"); value.Exists() && value.Type != gjson.Null {
		return false, true
	}
	if value := obj.Get("incomplete_details"); value.Exists() && value.Type != gjson.Null {
		incomplete = true
	}
	status := strings.ToLower(strings.TrimSpace(obj.Get("status").String()))
	response := obj.Get("response")
	if response.IsObject() {
		for _, key := range [...]string{"error", "incomplete_details"} {
			if value := response.Get(key); value.Exists() && value.Type != gjson.Null {
				incomplete = true
			}
		}
		if responseStatus := strings.TrimSpace(response.Get("status").String()); responseStatus != "" {
			status = strings.ToLower(responseStatus)
		}
	}
	switch status {
	case "incomplete", "failed", "cancelled", "canceled":
		return false, true
	}
	switch strings.TrimSpace(obj.Get("type").String()) {
	case "response.incomplete", "response.failed", "response.cancelled", "response.canceled", "error":
		return false, true
	case "response.completed", "response.done", "message_stop":
		if status == "" || status == "completed" {
			return true, incomplete
		}
		return false, true
	case "message_delta":
		comp, incomp := anthropicStopReasonFlags(obj.Get("delta.stop_reason").String())
		completed, incomplete = completed || comp, incomplete || incomp
	case "message":
		comp, incomp := anthropicStopReasonFlags(obj.Get("stop_reason").String())
		completed, incomplete = completed || comp, incomplete || incomp
	}
	obj.Get("choices").ForEach(func(_, choice gjson.Result) bool {
		switch strings.ToLower(strings.TrimSpace(choice.Get("finish_reason").String())) {
		case "stop", "tool_calls", "function_call", "end_turn":
			completed = true
		case "length", "content_filter":
			incomplete = true
		}
		return true
	})
	for _, candidates := range [...]gjson.Result{obj.Get("candidates"), response.Get("candidates")} {
		comp, incomp := geminiCompletionFlags(candidates)
		completed = completed || comp
		incomplete = incomplete || incomp
	}
	return completed, incomplete
}

func isCompletedExecutionPayload(payload []byte) bool {
	obj := gjson.ParseBytes(bytes.TrimSpace(payload))
	if !obj.IsObject() {
		return false
	}
	completed, incomplete := jsonCompletionFlags(obj)
	if incomplete {
		return false
	}
	if completed {
		return true
	}
	status := strings.ToLower(strings.TrimSpace(obj.Get("status").String()))
	if responseStatus := strings.TrimSpace(obj.Get("response.status").String()); responseStatus != "" {
		status = strings.ToLower(responseStatus)
	}
	return status == "completed" || status == "" && obj.Get("output.0").Exists()
}
