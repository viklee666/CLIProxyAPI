package config

import (
	"fmt"
	"testing"
)

func TestRequestLimitsYAMLValidation(t *testing.T) {
	for _, family := range []string{"gemini-api-key", "interactions-api-key", "claude-api-key", "codex-api-key", "xai-api-key", "meta-api-key", "vertex-api-key", "openai-compatibility"} {
		for _, field := range []string{"concurrency-limit", "rpm-limit"} {
			for _, invalid := range []string{"-1", "1.5", `"2"`, "true", "9223372036854775808"} {
				t.Run(family+"/"+field+"/"+invalid, func(t *testing.T) {
					data := []byte(fmt.Sprintf("%s:\n  - api-key: test\n    base-url: https://example.com\n    %s: %s\n", family, field, invalid))
					if _, errParse := ParseConfigBytes(data); errParse == nil {
						t.Fatal("accepted invalid request limit")
					}
				})
			}
		}
	}
}
