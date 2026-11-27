package usage

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

type usagePluginFunc func(context.Context, Record)

func (f usagePluginFunc) HandleUsage(ctx context.Context, record Record) {
	f(ctx, record)
}

func TestStreamFromContextDefaultsMissingToFalse(t *testing.T) {
	if StreamFromContext(context.Background()) {
		t.Fatalf("StreamFromContext(background) = true, want false")
	}
}

func TestStreamFromContextHonorsExplicitTrue(t *testing.T) {
	ctx := WithStream(context.Background(), true)
	if !StreamFromContext(ctx) {
		t.Fatalf("StreamFromContext(true) = false, want true")
	}
}

func TestRecordStreamField(t *testing.T) {
	record := Record{
		Provider: "openai",
		Model:    "gpt-5.4",
		Stream:   true,
	}
	if !record.Stream {
		t.Fatalf("Record.Stream = false, want true")
	}
}

func TestRecordBaseURLField(t *testing.T) {
	record := Record{
		Provider: "openai",
		Model:    "gpt-5.4",
		BaseURL:  "https://custom-gateway.example.com/v1",
	}
	if record.BaseURL != "https://custom-gateway.example.com/v1" {
		t.Fatalf("Record.BaseURL = %q, want %q", record.BaseURL, "https://custom-gateway.example.com/v1")
	}
}

func TestGenerateEnabledDefaultsNilToTrue(t *testing.T) {
	if !GenerateEnabled(nil) {
		t.Fatalf("GenerateEnabled(nil) = false, want true")
	}
}

func TestGenerateEnabledHonorsExplicitFalse(t *testing.T) {
	if GenerateEnabled(GenerateFlag(false)) {
		t.Fatalf("GenerateEnabled(false) = true, want false")
	}
}

func TestGenerateEnabledHonorsExplicitTrue(t *testing.T) {
	if !GenerateEnabled(GenerateFlag(true)) {
		t.Fatalf("GenerateEnabled(true) = false, want true")
	}
}

func TestGenerateFromContextDefaultsMissingToTrue(t *testing.T) {
	if !GenerateFromContext(context.Background()) {
		t.Fatalf("GenerateFromContext(background) = false, want true")
	}
}

func TestGenerateFromContextHonorsExplicitFalse(t *testing.T) {
	ctx := WithGenerate(context.Background(), false)
	if GenerateFromContext(ctx) {
		t.Fatalf("GenerateFromContext(false) = true, want false")
	}
}

func TestRecordOmittedGenerateIsEnabled(t *testing.T) {
	// Existing callers construct Record without setting Generate.
	// Omission must remain distinguishable from explicit false and default to true.
	record := Record{
		Provider: "openai",
		Model:    "gpt-5.4",
	}
	if record.Generate != nil {
		t.Fatalf("Record.Generate = %v, want nil for omitted field", record.Generate)
	}
	if !GenerateEnabled(record.Generate) {
		t.Fatalf("GenerateEnabled(omitted) = false, want true")
	}
}

func TestUnregisterNamedStopsDispatchAndCompactsIndexes(t *testing.T) {
	manager := NewManager(0)
	defer manager.Stop()

	var dropped atomic.Bool
	keepDone := make(chan struct{}, 1)
	dropPlugin := usagePluginFunc(func(context.Context, Record) {
		dropped.Store(true)
	})
	keepPlugin := usagePluginFunc(func(context.Context, Record) {
		select {
		case keepDone <- struct{}{}:
		default:
		}
	})

	manager.RegisterNamed("drop", dropPlugin)
	manager.RegisterNamed("keep", keepPlugin)
	if got := manager.UnregisterNamed("drop"); got == nil {
		t.Fatal("UnregisterNamed(drop) = nil, want dropped plugin")
	}
	if got := manager.UnregisterNamed("missing"); got != nil {
		t.Fatal("UnregisterNamed(missing) returned a plugin")
	}

	manager.Publish(context.Background(), Record{Provider: "provider"})
	select {
	case <-keepDone:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for remaining named plugin")
	}
	if dropped.Load() {
		t.Fatal("unregistered plugin received usage")
	}

	replaced := make(chan struct{}, 1)
	manager.RegisterNamed("keep", usagePluginFunc(func(context.Context, Record) {
		select {
		case replaced <- struct{}{}:
		default:
		}
	}))
	manager.Publish(context.Background(), Record{Provider: "provider"})
	select {
	case <-replaced:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for replaced named plugin")
	}
	select {
	case <-keepDone:
		t.Fatal("original keep plugin still dispatched after RegisterNamed replacement")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestUnregisterNamedPluginRemovesDefaultPlugin(t *testing.T) {
	const name = "test-unregister-named-plugin"
	var dropped atomic.Bool
	keepDone := make(chan struct{}, 1)
	t.Cleanup(func() {
		UnregisterNamedPlugin(name)
		UnregisterNamedPlugin(name + "-keep")
	})

	RegisterNamedPlugin(name, usagePluginFunc(func(context.Context, Record) {
		dropped.Store(true)
	}))
	RegisterNamedPlugin(name+"-keep", usagePluginFunc(func(context.Context, Record) {
		select {
		case keepDone <- struct{}{}:
		default:
		}
	}))
	if got := UnregisterNamedPlugin(name); got == nil {
		t.Fatal("UnregisterNamedPlugin() = nil, want registered plugin")
	}

	PublishRecord(context.Background(), Record{Provider: "provider"})
	select {
	case <-keepDone:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for remaining default named plugin")
	}
	if dropped.Load() {
		t.Fatal("unregistered default plugin received usage")
	}
}
