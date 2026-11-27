package helps

import "github.com/MoeForge/moego/ojson"

// IsResponsesIncomplete must be called on the native response, before a chat
// translator can collapse an unknown incomplete reason into finish_reason=stop.
func IsResponsesIncomplete(payload []byte) bool {
	for _, path := range []string{"type", "status", "response.status"} {
		value := ojson.GetBytesNoCopy(payload, path).String()
		if value == "response.incomplete" || value == "incomplete" {
			return true
		}
	}
	return false
}
