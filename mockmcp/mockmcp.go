// Package mockmcp is a controllable MCP server for tests — the shared mock both
// the shell and the middle layer use to exercise the MCP tool lane without a live
// server.
//
// WHY IT IS LIBRARY CODE, NOT A _test.go HELPER. The whole point of extracting
// the agent loop into this module is that two services drive it from one copy; a
// mock MCP that only one repo's tests can see would be re-written in the other,
// and the two would drift exactly where the behaviour is subtlest (auth, error
// branches, lying annotations). So the mock lives here, importable, and both
// repos' tests build on it.
//
// WHY A REAL SESSION, NOT A HAND-ROLLED IMITATION. It runs the SDK's own client
// and server over an in-memory transport — the real protocol — so what a test
// proves is the real conversion (SDK tool -> agentloop.RemoteTool -> policy), not
// a stand-in for it. The knobs below produce the cases a real, well-behaved
// server will NOT produce on cue: an error on tools/list, a tool whose call
// fails, a slow tool (for retry/wait), a tool that lies about being read-only.
package mockmcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mikelear/leartech-go-common/pkg/agentloop"
)

// Tool describes one tool the mock advertises, with how it should misbehave.
type Tool struct {
	Name        string
	Description string
	// ReadOnlyHint / DestructiveHint are the annotation fields the trust policy
	// judges — set ReadOnlyHint true on a destructive tool to test a lying hint.
	ReadOnlyHint    bool
	DestructiveHint bool
	// CallFails makes tools/call return an error, to exercise the tool-failed
	// branch a real server rarely produces on demand.
	CallFails bool
	// CallDelay holds the call open, for retry/wait-shaped tests.
	CallDelay time.Duration
	// Result is the text a successful call returns; empty means "ran <name>".
	Result string
}

// Options configures a Server.
type Options struct {
	// Instructions is the initialize-time instruction string a skill server
	// sends; empty for none.
	Instructions string
}

// Session starts an in-memory MCP server advertising tools and returns a
// connected client session. It registers cleanup on t. This is the primitive the
// shell's agentmcp tests and agent-core's own lift tests share.
func Session(t *testing.T, tools []Tool, opts Options) *mcp.ClientSession {
	t.Helper()

	var sopts *mcp.ServerOptions
	if opts.Instructions != "" {
		sopts = &mcp.ServerOptions{Instructions: opts.Instructions}
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: "mock", Version: "0.0.1"}, sopts)

	for _, tl := range tools {
		tool := tl // capture
		mt := &mcp.Tool{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: ObjectSchema(),
		}
		if tool.ReadOnlyHint || tool.DestructiveHint {
			d := tool.DestructiveHint
			mt.Annotations = &mcp.ToolAnnotations{ReadOnlyHint: tool.ReadOnlyHint, DestructiveHint: &d}
		}
		srv.AddTool(mt, func(ctx context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if tool.CallDelay > 0 {
				select {
				case <-time.After(tool.CallDelay):
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			if tool.CallFails {
				return nil, errors.New("tool " + tool.Name + " failed on purpose")
			}
			text := tool.Result
			if text == "" {
				text = "ran " + tool.Name
			}
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil
		})
	}

	clientT, serverT := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	if _, err := srv.Connect(ctx, serverT, nil); err != nil {
		t.Fatalf("mockmcp: server connect: %v", err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0.0.1"}, nil).
		Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("mockmcp: client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// RemoteToolsFrom lists a session's tools in the shape the trust policy judges —
// the conversion under test (SDK tool -> agentloop.RemoteTool).
func RemoteToolsFrom(t *testing.T, cs *mcp.ClientSession, server string) []agentloop.RemoteTool {
	t.Helper()
	out, err := listRemotes(cs, server)
	if err != nil {
		t.Fatalf("mockmcp: list tools: %v", err)
	}
	return out
}

// ListToolsErr is RemoteToolsFrom's error-returning sibling, for tests that
// assert the list-failed branch.
func ListToolsErr(cs *mcp.ClientSession, server string) ([]agentloop.RemoteTool, error) {
	return listRemotes(cs, server)
}

func listRemotes(cs *mcp.ClientSession, server string) ([]agentloop.RemoteTool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		return nil, err
	}
	out := make([]agentloop.RemoteTool, 0, len(res.Tools))
	for _, tool := range res.Tools {
		schema, merr := json.Marshal(tool.InputSchema)
		if merr != nil {
			schema = []byte(`"unmarshalable"`)
		}
		rt := agentloop.RemoteTool{
			Server: server, Name: tool.Name, Description: tool.Description, Schema: schema,
		}
		if a := tool.Annotations; a != nil {
			rt.ReadOnlyHint = a.ReadOnlyHint
			rt.DestructiveHint = a.DestructiveHint != nil && *a.DestructiveHint
		}
		out = append(out, rt)
	}
	return out, nil
}

// ObjectSchema is a minimal valid object input schema, the common case.
func ObjectSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`)
}

// ClosedSession returns a session whose server has already gone away, so
// ListTools / a call on it errors. It is how a test exercises the "server
// reachable at connect, then unavailable" branch without a flaky real server.
func ClosedSession(t *testing.T) *mcp.ClientSession {
	t.Helper()
	srv := mcp.NewServer(&mcp.Implementation{Name: "gone", Version: "0.0.1"}, nil)
	clientT, serverT := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	ss, err := srv.Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatalf("mockmcp: server connect: %v", err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0.0.1"}, nil).
		Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("mockmcp: client connect: %v", err)
	}
	_ = ss.Close() // server gone — subsequent list/call on cs will error
	return cs
}
