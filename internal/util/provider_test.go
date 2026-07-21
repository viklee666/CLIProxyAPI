package util

import (
	"strings"
	"testing"
)

func TestMaskSensitiveHeaderValueMasksAgentAssertion(t *testing.T) {
	const assertion = "AgentAssertion eyJhZ2VudF9ydW50aW1lX2lkIjoicnVudGltZS1zZWNyZXQifQ"
	masked := MaskSensitiveHeaderValue("Authorization", assertion)
	if !strings.HasPrefix(masked, "AgentAssertion ") {
		t.Fatalf("masked authorization = %q, want AgentAssertion scheme", masked)
	}
	if strings.Contains(masked, "eyJhZ2VudF9ydW50aW1lX2lk") {
		t.Fatalf("masked authorization leaked assertion: %q", masked)
	}
}
