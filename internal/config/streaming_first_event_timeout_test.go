package config

import "testing"

func TestParseConfigBytesStreamingFirstEventTimeout(t *testing.T) {
	cfg, err := ParseConfigBytes([]byte(`
streaming:
  first-event-timeout-seconds: 20
  first-event-timeout-retries: 2
`))
	if err != nil {
		t.Fatalf("ParseConfigBytes(): %v", err)
	}
	if cfg.Streaming.FirstEventTimeoutSeconds != 20 {
		t.Fatalf("first-event-timeout-seconds = %d, want 20", cfg.Streaming.FirstEventTimeoutSeconds)
	}
	if cfg.Streaming.FirstEventTimeoutRetries != 2 {
		t.Fatalf("first-event-timeout-retries = %d, want 2", cfg.Streaming.FirstEventTimeoutRetries)
	}
}

func TestParseConfigBytesStreamingFirstEventTimeoutDefaultsDisabled(t *testing.T) {
	cfg, err := ParseConfigBytes([]byte("host: 127.0.0.1\n"))
	if err != nil {
		t.Fatalf("ParseConfigBytes(): %v", err)
	}
	if cfg.Streaming.FirstEventTimeoutSeconds != 0 || cfg.Streaming.FirstEventTimeoutRetries != 0 {
		t.Fatalf("default first-event timeout config = %+v, want disabled", cfg.Streaming)
	}
}
