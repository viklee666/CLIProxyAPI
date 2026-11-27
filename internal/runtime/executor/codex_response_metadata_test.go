package executor

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
)

type codexResponseMetadataCapture struct {
	alias   string
	records chan coreusage.Record
}

func (c *codexResponseMetadataCapture) HandleUsage(_ context.Context, record coreusage.Record) {
	if record.Alias == c.alias {
		select {
		case c.records <- record:
		default:
		}
	}
}

type codexResponseMetadataNoopPlugin struct{}

func (codexResponseMetadataNoopPlugin) HandleUsage(context.Context, coreusage.Record) {}

func TestCodexNativeResponseMetadataSurvivesChatTranslation(t *testing.T) {
	for _, transport := range []string{"http", "websocket"} {
		for _, reason := range []string{"missing", "unknown"} {
			t.Run(transport+"/"+reason, func(t *testing.T) {
				details := ""
				if reason == "unknown" {
					details = `,"incomplete_details":{"reason":"provider_specific"}`
				}
				terminal := `{"type":"response.incomplete","response":{"id":"resp_1","model":"served-final","status":"incomplete"` + details + `,"output":[{"id":"msg_1","type":"message","role":"assistant","status":"incomplete","content":[{"type":"output_text","text":"partial"}]}],"usage":{"input_tokens":5,"output_tokens":2,"total_tokens":7}}}`
				frames := []string{
					`{"type":"response.created","response":{"id":"resp_1","model":"served-initial","status":"in_progress","output":[]}}`,
					codexOutputDeltaEvent,
					terminal,
				}
				var server *httptest.Server
				if transport == "http" {
					server = codexSSEServer(frames...)
				} else {
					server = codexWebsocketServerHoldingConnection(t, frames...)
				}
				defer server.Close()
				capture := &codexResponseMetadataCapture{alias: t.Name(), records: make(chan coreusage.Record, 4)}
				coreusage.RegisterNamedPlugin(t.Name(), capture)
				t.Cleanup(func() {
					coreusage.RegisterNamedPlugin(t.Name(), codexResponseMetadataNoopPlugin{})
				})
				ctx, cancel := codexCredentialBootstrapContext(t, transport)
				defer cancel()
				ctx = coreusage.WithRequestedModelAlias(ctx, capture.alias)
				req, opts := codexWebsocketRequest()
				opts.ResponseFormat = sdktranslator.FromString("openai")
				result, err := codexCredentialBootstrapExecutor(t, transport, nil).Execute(ctx, codexCredentialBootstrapAuth(server.URL), req, opts)
				if err != nil {
					t.Fatal(err)
				}
				if !result.Incomplete {
					t.Fatalf("native incomplete marker was lost: %s", result.Payload)
				}
				if got := ojson.GetBytesNoCopy(result.Payload, "choices.0.message.content").String(); got != "partial" {
					t.Fatalf("translated partial text = %q; payload=%s", got, result.Payload)
				}
				select {
				case record := <-capture.records:
					if record.Model != req.Model || record.ResponseModel != "served-final" {
						t.Fatalf("usage model pair = (%q, %q), requested %q", record.Model, record.ResponseModel, req.Model)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("timed out waiting for the native usage record")
				}
			})
		}
	}
}
