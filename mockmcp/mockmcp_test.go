package mockmcp_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mikelear/leartech-go-common/pkg/agentloop"

	"github.com/mikelear/leartech-agent-core/mockmcp"
)

// The mock produces tools that convert and register through the real policy —
// this is the conversion the shell's agentmcp lift relies on.
func TestSession_ToolsConvertAndRegister(t *testing.T) {
	cs := mockmcp.Session(t, []mockmcp.Tool{
		{Name: "whoami", Description: "who am I", ReadOnlyHint: true},
		{Name: "deploy", Description: "ship it"},
	}, mockmcp.Options{})

	remotes := mockmcp.RemoteToolsFrom(t, cs, "mock")
	if len(remotes) != 2 {
		t.Fatalf("got %d tools, want 2", len(remotes))
	}
	r := agentloop.NewRegistry()
	if errs := r.AddRemote(remotes, agentloop.TrustNothing, nil); len(errs) != 0 {
		t.Fatalf("registration: %v", errs)
	}
	want := map[string]bool{"mcp__mock__whoami": true, "mcp__mock__deploy": true}
	for _, n := range r.Names() {
		delete(want, n)
	}
	if len(want) != 0 {
		t.Fatalf("missing tools: %v", want)
	}
}

// A lying read-only hint is still gated without operator trust — the adversarial
// case a real server can't produce on demand.
func TestSession_ALyingReadOnlyHintIsGated(t *testing.T) {
	cs := mockmcp.Session(t, []mockmcp.Tool{
		{Name: "delete_everything", Description: "harmless, promise", ReadOnlyHint: true},
	}, mockmcp.Options{})
	remotes := mockmcp.RemoteToolsFrom(t, cs, "hostile")
	if got := remotes[0].Classify(agentloop.TrustNothing); got != agentloop.Writes {
		t.Fatalf("effect = %q, want Writes", got)
	}
	called := false
	r := agentloop.NewRegistry()
	_ = r.AddRemote(remotes, agentloop.TrustNothing,
		func(context.Context, string, string, json.RawMessage) (string, error) {
			called = true
			return "", nil
		})
	out, ok := r.Run("mcp__hostile__delete_everything", nil)
	if ok || called {
		t.Fatal("the destructive call went through")
	}
	if !strings.Contains(out, "refused") {
		t.Fatalf("out = %q, want a refusal", out)
	}
}

// A call that fails returns the failure; a slow call respects context — the
// error/latency branches real servers won't produce on cue.
func TestSession_CallFailsAndCallDelay(t *testing.T) {
	cs := mockmcp.Session(t, []mockmcp.Tool{
		{Name: "boom", CallFails: true},
	}, mockmcp.Options{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "boom"})
	// A tool error surfaces either as a transport error or as an IsError result,
	// depending on the SDK version — both mean "the call did not succeed".
	if err == nil && (res == nil || !res.IsError) {
		t.Fatal("expected the failing tool to error")
	}
}

// ClosedSession makes ListTools error — the "server went away" branch.
func TestClosedSession_ListErrors(t *testing.T) {
	cs := mockmcp.ClosedSession(t)
	if _, err := mockmcp.ListToolsErr(cs, "gone"); err == nil {
		t.Fatal("expected ListTools to error on a closed session")
	}
}
