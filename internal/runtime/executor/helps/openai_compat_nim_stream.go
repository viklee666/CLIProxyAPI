package helps

import (
	"context"
	"time"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

// StartOpenAINIMPings returns a stop function that joins the ping sender before
// the executor closes its output channel.
func StartOpenAINIMPings(ctx context.Context, out chan<- cliproxyexecutor.StreamChunk, responseFormat sdktranslator.Format) func() {
	stop := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		payload := OpenAINIMClaudePingSSE
		if responseFormat != sdktranslator.FormatClaude {
			payload = []byte(": keep-alive\n\n")
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-stop:
				return
			case <-ticker.C:
				select {
				case out <- cliproxyexecutor.StreamChunk{Payload: payload}:
				case <-ctx.Done():
					return
				case <-stop:
					return
				}
			}
		}
	}()
	return func() {
		close(stop)
		<-stopped
	}
}

func SendOpenAINIMChunk(ctx context.Context, out chan<- cliproxyexecutor.StreamChunk, chunk cliproxyexecutor.StreamChunk) bool {
	select {
	case out <- chunk:
		return true
	case <-ctx.Done():
		return false
	}
}
