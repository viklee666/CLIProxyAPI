package auth

import "testing"

func TestPriorityHealthCompletedGenerationPayloads(t *testing.T) {
	for _, tc := range []struct {
		name, payload string
		completed     bool
	}{
		{"responses", `{"type":"response.completed","response":{"status":"completed"}}`, true},
		{"chat", `{"choices":[{"finish_reason":"stop"}]}`, true},
		{"anthropic", `{"type":"message","stop_reason":"end_turn"}`, true},
		{"gemini", `{"candidates":[{"finishReason":"STOP"}]}`, true},
		{"partial responses", `{"type":"response.incomplete"}`, false},
		{"partial chat", `{"choices":[{"finish_reason":"length"}]}`, false},
		{"partial anthropic", `{"type":"message","stop_reason":"max_tokens"}`, false},
		{"partial gemini", `{"candidates":[{"finishReason":"MAX_TOKENS"}]}`, false},
		{"failed response", `{"status":"failed","output":[{}]}`, false},
		{"queued response", `{"status":"queued","output":[{}]}`, false},
		{"error", `{"status":"completed","error":{"message":"failure"}}`, false},
		{"incomplete details", `{"type":"message","stop_reason":"end_turn","incomplete_details":{"reason":"limit"}}`, false},
		{"preflight", `{}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isCompletedExecutionPayload([]byte(tc.payload)); got != tc.completed {
				t.Fatalf("completed=%t, want %t", got, tc.completed)
			}
		})
	}
}

func TestPriorityHealthStreamCompletionPreservesIncompleteFlag(t *testing.T) {
	payload := []byte("event: response.incomplete\ndata: {\"type\":\"response.incomplete\"}\n\nevent: response.completed\ndata: {\"type\":\"response.completed\"}\n\ndata: [DONE]\n")
	completed, incomplete := streamPayloadCompletionFlags(payload)
	if !completed || !incomplete {
		t.Fatalf("flags=(%t,%t), want both terminal signals retained", completed, incomplete)
	}
	if completed, incomplete := streamPayloadCompletionFlags([]byte("data: [DONE]\n")); completed || incomplete {
		t.Fatal("transport sentinel alone counted as completed generation")
	}
}
