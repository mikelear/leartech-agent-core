// Package remotemcp connects to a REMOTE http_sse MCP server, lists its tools in
// the shape the trust policy judges, and invokes them — the remote half of the
// MCP tool lane, shared by the shell and the web middle layer.
//
// WHY REMOTE-ONLY, AND WHY HERE. The shell has a LOCAL lane too (stdio servers it
// spawns, an OAuth token cache in $HOME) that the web deliberately does not: a
// pod has no home and spawns nothing. What BOTH drive is the remote lane — a
// server reachable over HTTP, its tools converted to agentloop.RemoteTool and
// governed by the same policy. So only that lifts here (the rule we set: lift a
// file when a SECOND driver consumes it). leartech-console-api is that second
// consumer; the shell keeps its local-lane broker and re-points its remote parts
// at this in a follow-up.
//
// The SDK's StreamableClientTransport is the transport; a server that wants a
// token gets one via the injected http.Client (the caller wires oauth2.Transport
// or a static bearer). tools/list is anonymous-friendly; a privileged CALL
// carries whatever the http.Client presents.
package remotemcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mikelear/leartech-go-common/pkg/agentloop"
)

// DefaultTimeout bounds a single connect / list / call.
const DefaultTimeout = 60 * time.Second

// Connect opens a session to a remote MCP server at endpoint, using httpClient
// for every request (nil → a plain client with DefaultTimeout). The caller wires
// auth into httpClient — an oauth2.Transport for a machine credential, or a
// bearer-adding RoundTripper for a user token. A server needing a token that is
// absent answers 401, which surfaces as a connect error the caller can name.
//
// proven-by: remotemcp_test.go
func Connect(ctx context.Context, name, endpoint string, httpClient *http.Client) (*mcp.ClientSession, error) {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: DefaultTimeout}
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "leartech-agent-core", Version: "0"}, nil)
	sess, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:   strings.TrimRight(endpoint, "/"),
		HTTPClient: httpClient,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("connecting to MCP server %q at %s: %w", name, endpoint, err)
	}
	return sess, nil
}

// ToolsOf lists a session's tools as agentloop.RemoteTool — the conversion the
// trust policy judges. The SDK hands the schema back decoded; it is re-marshalled
// so a non-object schema survives the round trip and is refused later by the
// registry, which is where it should be caught.
//
// proven-by: remotemcp_test.go
func ToolsOf(ctx context.Context, sess *mcp.ClientSession, server string) ([]agentloop.RemoteTool, error) {
	res, err := sess.ListTools(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("listing tools on %q: %w", server, err)
	}
	out := make([]agentloop.RemoteTool, 0, len(res.Tools))
	for _, t := range res.Tools {
		schema, merr := json.Marshal(t.InputSchema)
		if merr != nil {
			schema = []byte(`"unmarshalable"`)
		}
		rt := agentloop.RemoteTool{
			Server: server, Name: t.Name, Description: t.Description, Schema: schema,
		}
		if a := t.Annotations; a != nil {
			rt.ReadOnlyHint = a.ReadOnlyHint
			rt.DestructiveHint = a.DestructiveHint != nil && *a.DestructiveHint
		}
		out = append(out, rt)
	}
	return out, nil
}

// InvokerFor returns an agentloop.Invoke that calls a tool on one session. The
// server's own error text is returned on failure, not a wrapper — a tool failing
// is information the model can act on, and "the tool failed" is not.
//
// proven-by: remotemcp_test.go
func InvokerFor(sess *mcp.ClientSession) agentloop.Invoke {
	return func(ctx context.Context, _, tool string, args json.RawMessage) (string, error) {
		var in map[string]any
		if len(args) > 0 {
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("arguments are not valid JSON: %w", err)
			}
		}
		res, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: in})
		if err != nil {
			return "", err
		}
		body := contentText(res.Content)
		if res.IsError {
			if body != "" {
				return "", fmt.Errorf("%s", body)
			}
			return "", fmt.Errorf("tool %q reported an error", tool)
		}
		return body, nil
	}
}

// contentText flattens MCP content blocks to text, naming a non-text block rather
// than dropping it (a model told nothing about an image assumes there was none).
func contentText(blocks []mcp.Content) string {
	var b strings.Builder
	for _, c := range blocks {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		} else {
			fmt.Fprintf(&b, "(%T content, not rendered here)", c)
		}
	}
	return b.String()
}

// BearerTransport is an http.RoundTripper that adds a static bearer to every
// request — the simplest auth for a server that accepts the user's token. (For a
// machine credential, wire oauth2.Transport into the http.Client instead.)
type BearerTransport struct {
	Token string
	Base  http.RoundTripper
}

// RoundTrip adds the Authorization header and delegates.
func (t BearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	if t.Token != "" {
		r = r.Clone(r.Context())
		r.Header.Set("Authorization", "Bearer "+t.Token)
	}
	return base.RoundTrip(r)
}
