package handlers

import (
	"fmt"
	"net/http"
	"time"

	coreexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/context"
)

type firstEventStreamOpen func(context.Context) (*coreexecutor.StreamResult, error)

type firstEventStreamOutcome struct {
	result  *coreexecutor.StreamResult
	pending []coreexecutor.StreamChunk
	closed  bool
	err     error
}

type firstEventTimeoutError struct {
	timeout time.Duration
}

func (e *firstEventTimeoutError) Error() string {
	return fmt.Sprintf("upstream timed out waiting for the first streaming event after %s", e.timeout)
}

func (e *firstEventTimeoutError) StatusCode() int {
	return http.StatusGatewayTimeout
}

// executeStreamThroughFirstEvent applies the optional first-event timeout to one or more
// identical upstream attempts. A successful attempt keeps its context alive for the rest
// of the stream and returns a cancel function that the caller must invoke when forwarding ends.
func executeStreamThroughFirstEvent(
	ctx context.Context,
	timeout time.Duration,
	maxRetries int,
	route string,
	model string,
	open firstEventStreamOpen,
) (*coreexecutor.StreamResult, []coreexecutor.StreamChunk, bool, context.CancelFunc, error) {
	if timeout <= 0 {
		result, err := open(ctx)
		return result, nil, false, nil, err
	}
	if maxRetries < 0 {
		maxRetries = 0
	}
	parent := ctx
	if parent == nil {
		parent = context.Background()
	}

	for retry := 0; ; retry++ {
		attemptCtx, cancel := context.WithCancel(parent)
		outcomeCh := make(chan firstEventStreamOutcome, 1)
		go func() {
			outcomeCh <- openStreamThroughFirstEvent(attemptCtx, open)
		}()

		timer := time.NewTimer(timeout)
		select {
		case <-parent.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			cancel()
			return nil, nil, false, nil, parent.Err()
		case outcome := <-outcomeCh:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			if outcome.err != nil {
				cancel()
				return nil, nil, false, nil, outcome.err
			}
			return outcome.result, outcome.pending, outcome.closed, cancel, nil
		case <-timer.C:
			cancel()
			if err := parent.Err(); err != nil {
				return nil, nil, false, nil, err
			}
			if retry < maxRetries {
				log.WithFields(log.Fields{
					"route":       route,
					"model":       model,
					"timeout":     timeout.String(),
					"retry":       retry + 1,
					"max_retries": maxRetries,
				}).Warn("upstream first streaming event timed out; retrying identical request")
				continue
			}
			return nil, nil, false, nil, &firstEventTimeoutError{timeout: timeout}
		}
	}
}

func openStreamThroughFirstEvent(ctx context.Context, open firstEventStreamOpen) firstEventStreamOutcome {
	result, err := open(ctx)
	if err != nil {
		return firstEventStreamOutcome{err: err}
	}
	if result == nil {
		return firstEventStreamOutcome{err: fmt.Errorf("upstream returned a nil stream")}
	}
	if result.Chunks == nil {
		return firstEventStreamOutcome{result: result, closed: true}
	}

	pending := make([]coreexecutor.StreamChunk, 0, 1)
	for {
		select {
		case <-ctx.Done():
			return firstEventStreamOutcome{err: ctx.Err()}
		case chunk, ok := <-result.Chunks:
			if !ok {
				return firstEventStreamOutcome{result: result, pending: pending, closed: true}
			}
			pending = append(pending, chunk)
			if chunk.Err != nil || len(chunk.Payload) > 0 {
				return firstEventStreamOutcome{result: result, pending: pending}
			}
		}
	}
}
