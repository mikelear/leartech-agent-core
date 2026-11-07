// Package mockgateway is a controllable ai-gateway for tests — the shared stub
// both the shell and the middle layer use to exercise the gateway lane (models,
// tools, chat streaming, key minting, search/fetch) without a live gateway.
//
// Like mockmcp, it is library code rather than a per-repo _test.go helper so the
// two services that drive the same loop test it against the SAME gateway
// behaviour. It speaks the real HTTP shapes aigateway.Client expects, so a test
// builds a real client against httptest and drives genuine code paths.
package mockgateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Model is one model the gateway advertises.
type Model struct {
	ID       string
	Provider string
}

// GatewayTool is one hosted tool the gateway advertises on /v1/tools.
type GatewayTool struct {
	Name      string
	Available bool
}

// Options configures the stub.
type Options struct {
	Models []Model
	Tools  []GatewayTool
	// KeySecret is the secret returned by POST /admin/v1/keys. Empty means the
	// stub returns a key WITHOUT a secret, to exercise the "secretless key is an
	// error" branch.
	KeySecret string
	// SearchAnswer is returned by /v1/search.
	SearchAnswer string
	// ChatReply is the plain text a chat turn streams when it is NOT a tool
	// round-trip. Empty defaults to "hello".
	ChatReply string
	// ToolCallOn, when the request body contains this substring (and is not a
	// tool-result replay), makes the chat stream emit a tool_call for ToolCallName
	// instead of plain text — so a test can drive a round-trip deterministically.
	ToolCallOn   string
	ToolCallName string
	// ChatAfterTool is the text streamed on the replay (after a tool result).
	ChatAfterTool string
}

// New starts the stub and returns it. Register cleanup via t.
func New(t *testing.T, opts Options) *httptest.Server {
	t.Helper()
	if opts.ChatReply == "" {
		opts.ChatReply = "hello"
	}
	if opts.ChatAfterTool == "" {
		opts.ChatAfterTool = "done"
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/models":
			data := make([]map[string]any, 0, len(opts.Models))
			for _, m := range opts.Models {
				data = append(data, map[string]any{"id": m.ID, "provider": m.Provider})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": data})

		case r.URL.Path == "/v1/tools":
			data := make([]map[string]any, 0, len(opts.Tools))
			for _, tl := range opts.Tools {
				data = append(data, map[string]any{
					"type":      "function",
					"function":  map[string]any{"name": tl.Name, "description": tl.Name, "parameters": map[string]any{}},
					"available": tl.Available,
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": data})

		case r.URL.Path == "/admin/v1/keys" && r.Method == http.MethodPost:
			resp := map[string]any{"key_id": "key-mock"}
			if opts.KeySecret != "" {
				resp["secret"] = opts.KeySecret
			}
			_ = json.NewEncoder(w).Encode(resp)

		case r.URL.Path == "/v1/search":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"answer": opts.SearchAnswer, "results": []any{}, "provider": "mock",
			})

		case r.URL.Path == "/v1/fetch":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"url": "https://x", "final_url": "https://x", "title": "T", "content": "body",
			})

		case r.URL.Path == "/v1/chat/completions":
			w.Header().Set("Content-Type", "text/event-stream")
			fl, _ := w.(http.Flusher)
			body, _ := io.ReadAll(r.Body)
			b := string(body)
			replay := strings.Contains(b, "tool_calls") || strings.Contains(b, `"role":"tool"`)
			switch {
			case replay:
				writeChunk(w, `{"choices":[{"delta":{"content":"`+opts.ChatAfterTool+`"}}]}`)
				writeChunk(w, `{"choices":[{"delta":{},"finish_reason":"stop"}]}`)
			case opts.ToolCallOn != "" && strings.Contains(b, opts.ToolCallOn):
				writeChunk(w, `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"`+
					opts.ToolCallName+`","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
			default:
				writeChunk(w, `{"choices":[{"delta":{"content":"`+opts.ChatReply+`"}}]}`)
				writeChunk(w, `{"choices":[{"delta":{},"finish_reason":"stop"}]}`)
			}
			io.WriteString(w, "data: [DONE]\n\n")
			if fl != nil {
				fl.Flush()
			}

		default:
			http.Error(w, "mockgateway: no route "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func writeChunk(w io.Writer, json string) {
	_, _ = io.WriteString(w, "data: "+json+"\n\n")
}
