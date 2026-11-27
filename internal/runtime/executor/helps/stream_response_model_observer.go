package helps

import (
	"bytes"

	"github.com/MoeForge/moego/ojson"
)

const maxStreamResponseModelBytes = 64 * 1024

// StreamResponseModelObserver handles SSE split across arbitrary transport chunks.
// It retains at most one bounded line and one bounded multiline data event.
type StreamResponseModelObserver struct {
	reporter      *UsageReporter
	line          []byte
	event         []byte
	droppingLine  bool
	droppingEvent bool
}

func NewStreamResponseModelObserver(reporter *UsageReporter) *StreamResponseModelObserver {
	return &StreamResponseModelObserver{reporter: reporter}
}

func (o *StreamResponseModelObserver) Feed(chunk []byte) {
	if o == nil || o.reporter == nil || o.reporter.IsResponseModelFinal() {
		return
	}
	for len(chunk) > 0 {
		end := bytes.IndexByte(chunk, '\n')
		if end < 0 {
			if !o.droppingLine {
				if len(o.line)+len(chunk) > maxStreamResponseModelBytes {
					o.line = o.line[:0]
					o.event = o.event[:0]
					o.droppingLine, o.droppingEvent = true, true
				} else {
					o.line = append(o.line, chunk...)
					// Some relays deliver a complete JSON frame without an SSE newline.
					if !o.droppingEvent && len(o.event) == 0 {
						o.reporter.ObserveResponseModel(o.line)
					}
				}
			}
			return
		}
		part := chunk[:end]
		chunk = chunk[end+1:]
		if o.droppingLine || len(o.line)+len(part) > maxStreamResponseModelBytes {
			o.line = o.line[:0]
			o.event = o.event[:0]
			o.droppingLine = false
			o.droppingEvent = true
			continue
		}
		if len(o.line) > 0 {
			o.line = append(o.line, part...)
			o.observeLine(o.line)
			o.line = o.line[:0]
		} else {
			o.observeLine(part)
		}
		if o.reporter.IsResponseModelFinal() {
			o.line, o.event = nil, nil
			return
		}
	}
}

func (o *StreamResponseModelObserver) observeLine(line []byte) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		o.flushEvent()
		return
	}
	if o.droppingEvent {
		return
	}
	if !bytes.HasPrefix(line, []byte("data:")) {
		o.reporter.ObserveResponseModel(line)
		return
	}
	data := bytes.TrimSpace(line[len("data:"):])
	if len(o.event) == 0 && ojson.ValidBytes(data) {
		o.reporter.ObserveResponseModel(data)
		return
	}
	// Charging a newline also bounds events composed entirely of empty data lines.
	if len(o.event)+len(data)+1 > maxStreamResponseModelBytes {
		o.event = o.event[:0]
		o.droppingEvent = true
		return
	}
	o.event = append(o.event, data...)
	o.event = append(o.event, '\n')
	o.reporter.ObserveResponseModel(o.event)
}

func (o *StreamResponseModelObserver) flushEvent() {
	if !o.droppingEvent && len(o.event) > 0 {
		o.reporter.ObserveResponseModel(o.event)
	}
	o.event = o.event[:0]
	o.droppingEvent = false
}

func (o *StreamResponseModelObserver) Finish() {
	if o == nil || o.reporter == nil {
		return
	}
	if !o.droppingLine && len(o.line) > 0 {
		o.observeLine(o.line)
	}
	o.flushEvent()
	o.line = nil
}
