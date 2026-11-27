package helps

import (
	"strings"

	"github.com/MoeForge/moego/ojson"
)

const maxResponseModelLength = 128

// ObserveResponseModel records only models reported by the upstream, before any
// downstream translation restores the client's requested model or alias.
func (r *UsageReporter) ObserveResponseModel(payload []byte) {
	if r == nil || r.IsResponseModelFinal() {
		return
	}
	model, terminal := extractResponseModelEvent(payload, r.provider)
	r.responseModelMu.Lock()
	defer r.responseModelMu.Unlock()
	if r.responseModelFinal {
		return
	}
	if model != "" && model != r.responseModel {
		// The JSON parser may return a view into a reusable transport buffer.
		r.responseModel = strings.Clone(model)
	}
	r.responseModelFinal = terminal
}

// SetResponseModel records models supplied by a structured upstream transport.
func (r *UsageReporter) SetResponseModel(model string) {
	if r == nil {
		return
	}
	model = strings.TrimSpace(model)
	if model == "" || len(model) > maxResponseModelLength {
		return
	}
	r.responseModelMu.Lock()
	defer r.responseModelMu.Unlock()
	if !r.responseModelFinal && model != r.responseModel {
		r.responseModel = strings.Clone(model)
	}
}

func (r *UsageReporter) ResponseModel() string {
	if r == nil {
		return ""
	}
	r.responseModelMu.RLock()
	defer r.responseModelMu.RUnlock()
	return r.responseModel
}

func (r *UsageReporter) IsResponseModelFinal() bool {
	if r == nil {
		return false
	}
	r.responseModelMu.RLock()
	defer r.responseModelMu.RUnlock()
	return r.responseModelFinal
}

func extractResponseModelEvent(payload []byte, provider string) (model string, terminal bool) {
	data := jsonPayload(payload)
	if len(data) == 0 {
		return "", false
	}
	eventType := ojson.GetBytesNoCopy(data, "type").String()
	provider = strings.ToLower(strings.TrimSpace(provider))
	// Codex output/tool events can contain model-like fields unrelated to the
	// response. Only its authoritative response lifecycle carries the model.
	if provider == "codex" {
		switch eventType {
		case "response.created", "response.in_progress":
		case "response.completed", "response.done", "response.incomplete":
			terminal = true
		case "":
			object := ojson.GetBytesNoCopy(data, "object").String()
			if object != "response" && object != "response.compaction" {
				return "", false
			}
		default:
			return "", false
		}
	}
	if !ojson.ValidBytes(data) {
		return "", false
	}
	get := func(path string) ojson.Result { return ojson.GetBytesNoCopy(data, path) }
	terminal = terminal || eventType == "response.completed" || eventType == "response.done" || eventType == "response.incomplete" || eventType == "message_stop"
	if eventType == "" {
		eventType = get("event_type").String()
	}
	switch eventType {
	case "interaction.completed", "interaction.done", "interaction.failed", "interaction.cancelled":
		terminal = true
	}
	for _, path := range []string{"status", "response.status", "interaction.status"} {
		switch get(path).String() {
		case "completed", "incomplete", "cancelled", "failed":
			terminal = true
		}
	}
	if get("object").String() == "chat.completion" || get("choices.0.finish_reason").String() != "" ||
		get("candidates.0.finishReason").String() != "" || get("response.candidates.0.finishReason").String() != "" {
		terminal = true
	}
	if provider == "codex" {
		if eventType == "" {
			return responseModelString(get("model")), terminal
		}
		return responseModelString(get("response.model")), terminal
	}
	if provider == "claude" {
		switch eventType {
		case "content_block_start":
			if get("content_block.type").String() == "fallback" {
				return responseModelString(get("content_block.to.model")), false
			}
			return "", false
		case "message_delta", "message":
			var fallback string
			get("usage.iterations").ForEach(func(_, iteration ojson.Result) bool {
				if iteration.Get("type").String() == "fallback_message" {
					if served := responseModelString(iteration.Get("model")); served != "" {
						fallback = served
					}
				}
				return true
			})
			if fallback != "" {
				return fallback, eventType == "message"
			}
		}
		terminal = terminal || eventType == "message"
	}
	for _, path := range []string{"response.modelVersion", "modelVersion", "response.model", "interaction.model", "message.model", "model"} {
		if served := responseModelString(get(path)); served != "" {
			return served, terminal
		}
	}
	return "", terminal
}

func responseModelString(result ojson.Result) string {
	if result.Type != ojson.String {
		return ""
	}
	model := strings.TrimSpace(result.String())
	if len(model) > maxResponseModelLength {
		return ""
	}
	return model
}
