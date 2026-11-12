package remotemcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/mikelear/leartech-go-common/pkg/agentloop"

	"github.com/mikelear/leartech-agent-core/mockmcp"
	"github.com/mikelear/leartech-agent-core/remotemcp"
)

// ToolsOf + InvokerFor work over a real in-memory MCP session (mockmcp), the same
// conversion the trust policy judges — proving the remote lane end to end without
// a network server.
func TestToolsOfAndInvoke_OverARealSession(t *testing.T) {
	cs := mockmcp.Session(t, []mockmcp.Tool{
		{Name: "search", Description: "web search", Result: "forty-two"},
		{Name: "deploy", Description: "ship it", ReadOnlyHint: false},
	}, mockmcp.Options{})

	tools, err := remotemcp.ToolsOf(context.Background(), cs, "mock")
	if err != nil {
		t.Fatalf("ToolsOf: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("got %d tools, want 2", len(tools))
	}

	// Register under a governed registry and run one tool through the invoker.
	reg := agentloop.NewRegistry()
	reg.Govern(agentloop.NewPolicy(
		agentloop.ForEffect(agentloop.Writes, agentloop.Allow, ""),
		agentloop.ForEffect(agentloop.Executes, agentloop.Allow, ""),
	), agentloop.AllowAll{})
	if errs := reg.AddRemote(tools, agentloop.TrustNothing, remotemcp.InvokerFor(cs)); len(errs) != 0 {
		t.Fatalf("AddRemote: %v", errs)
	}
	out, ok := reg.Run("mcp__mock__search", json.RawMessage(`{"q":"x"}`))
	if !ok {
		t.Fatalf("search did not run: %s", out)
	}
	if out != "forty-two" {
		t.Fatalf("search result = %q, want forty-two", out)
	}
}

// A failing tool returns the server's own error text, not a wrapper.
func TestInvoker_ReturnsTheServersErrorText(t *testing.T) {
	cs := mockmcp.Session(t, []mockmcp.Tool{
		{Name: "boom", CallFails: true},
	}, mockmcp.Options{})
	inv := remotemcp.InvokerFor(cs)
	_, err := inv(context.Background(), "mock", "boom", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("a failing tool must return an error")
	}
}

// ToolsOf on a gone session errors, not panics — the "server went away" path.
func TestToolsOf_OnAClosedSessionErrors(t *testing.T) {
	cs := mockmcp.ClosedSession(t)
	if _, err := remotemcp.ToolsOf(context.Background(), cs, "gone"); err == nil {
		t.Fatal("expected ToolsOf to error on a closed session")
	}
}

// BearerTransport adds the Authorization header.
func TestBearerTransport_AddsTheHeader(t *testing.T) {
	var seen string
	rt := remotemcp.BearerTransport{Token: "tok-123", Base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		seen = r.Header.Get("Authorization")
		return &http.Response{StatusCode: 200, Body: http.NoBody, Header: make(http.Header)}, nil
	})}
	req, _ := http.NewRequest(http.MethodGet, "https://x/mcp", nil)
	if _, err := rt.RoundTrip(req); err != nil {
		t.Fatalf("round trip: %v", err)
	}
	if seen != "Bearer tok-123" {
		t.Fatalf("Authorization = %q, want Bearer tok-123", seen)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDynamicBearerTransport_ReadsTheCurrentToken(t *testing.T) {
	var seen []string
	cur := "tok-A"
	rt := remotemcp.DynamicBearerTransport{
		Token: func() string { return cur },
		Base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			seen = append(seen, r.Header.Get("Authorization"))
			return &http.Response{StatusCode: 200, Body: http.NoBody, Header: make(http.Header)}, nil
		}),
	}
	req, _ := http.NewRequest(http.MethodGet, "https://x/mcp", nil)
	_, _ = rt.RoundTrip(req)
	cur = "tok-B" // the credential rotated between calls
	_, _ = rt.RoundTrip(req)
	if len(seen) != 2 || seen[0] != "Bearer tok-A" || seen[1] != "Bearer tok-B" {
		t.Fatalf("expected the fresh token each call, got %v", seen)
	}
}
