package usage

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

type usagePluginFunc func(context.Context, Record)

func (f usagePluginFunc) HandleUsage(ctx context.Context, record Record) { f(ctx, record) }

func TestManagerStopAndWaitDrainsQueue(t *testing.T) {
	manager := NewManager(4)
	var delivered atomic.Int64
	manager.Register(usagePluginFunc(func(context.Context, Record) {
		delivered.Add(1)
	}))
	manager.Publish(context.Background(), Record{AuthID: "auth-a"})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	manager.StopAndWait(ctx)
	if delivered.Load() != 1 {
		t.Fatalf("delivered = %d, want 1", delivered.Load())
	}
}
