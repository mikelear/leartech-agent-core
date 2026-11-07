package gatewaytools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mikelear/leartech-go-common/pkg/agentloop"
	"github.com/mikelear/leartech-go-common/pkg/aigateway"
)

// GatewayToolsOf fetches the gateway's hosted tools for THIS credential and
// turns the available ones into agentloop Tools, qualified gateway__<name>.
//
// QUALIFIED, per the 2026-09-19 decision recorded in the ai-gateway BACKLOG
// (§4): local__, mcp__server__ and gateway__ names collide nowhere, and the
// name shown at approval time says what the call does — here, "spends at the
// gateway" rather than "reads a local file".
//
// UNAVAILABLE TOOLS ARE REPORTED, not offered: a model planning around a
// capability its key lacks gets a 403 mid-task. The caller prints them so
// the missing capability is named at startup.
// proven-by: TestGatewayToolsOf_OffersAvailableAndReportsUnavailable
func GatewayToolsOf(ctx context.Context, c *aigateway.Client) (tools []agentloop.Tool, unavailable []string, err error) {
	listed, err := c.Tools(ctx)
	if err != nil {
		return nil, nil, err
	}
	for _, t := range listed {
		if !t.Available {
			unavailable = append(unavailable, fmt.Sprintf("%s (needs %s)", t.Function.Name, t.Scope))
			continue
		}
		schema, merr := json.Marshal(t.Function.Parameters)
		if merr != nil {
			unavailable = append(unavailable, fmt.Sprintf("%s (schema did not marshal)", t.Function.Name))
			continue
		}
		tools = append(tools, agentloop.Tool{
			Name:        "gateway__" + t.Function.Name,
			Description: "[gateway] " + t.Function.Description,
			Schema:      schema,
			Effect:      agentloop.Spends,
			Run:         gwInvokerFor(c, t.Function.Name),
		})
	}
	return tools, unavailable, nil
}

// gwInvokerFor runs one gateway tool by name. Search and fetch are the two the
// gateway hosts today; an unknown name is a clear error rather than a guess.
//
// proven-by: TestGwInvokerFor_SearchAndFetch
func gwInvokerFor(c *aigateway.Client, name string) func(json.RawMessage) (string, error) {
	return func(args json.RawMessage) (string, error) {
		var in map[string]any
		if len(args) > 0 {
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("arguments are not valid JSON: %w", err)
			}
		}
		ctx := context.Background()
		switch name {
		case "search":
			q, _ := in["query"].(string)
			if q == "" {
				return "", fmt.Errorf("search: query is required")
			}
			prov, _ := in["provider"].(string)
			max := 5
			if m, ok := in["max_results"].(float64); ok && int(m) > 0 && int(m) <= 20 {
				max = int(m)
			}
			res, err := c.Search(ctx, aigateway.SearchRequest{Query: q, Provider: prov, MaxResults: max})
			if err != nil {
				return "", err
			}
			return renderSearchResults(res), nil
		case "fetch":
			u, _ := in["url"].(string)
			if u == "" {
				return "", fmt.Errorf("fetch: url is required")
			}
			res, err := c.Fetch(ctx, aigateway.FetchRequest{URL: u})
			if err != nil {
				return "", err
			}
			return renderFetchResult(res), nil
		default:
			return "", fmt.Errorf("gateway tool %q is not implemented", name)
		}
	}
}

// renderSearchResults prints the normalized shape compactly: answer first
// when the provider synthesized one, then title+url+snippet lines. The
// provider is last, named, so the model (and the transcript) can see who
// answered — a lane change would otherwise be invisible.
//
// proven-by: TestRenderSearchResults_NamesTheProviderAndSnipsContent
func renderSearchResults(res aigateway.SearchResults) string {
	var b strings.Builder
	if res.Answer != "" {
		b.WriteString(res.Answer + "\n\n")
	}
	for _, it := range res.Items {
		fmt.Fprintf(&b, "%s\n%s\n%s\n\n", it.Title, it.URL, snip(it.Content, 240))
	}
	fmt.Fprintf(&b, "(via %s)\n", res.Provider)
	return b.String()
}

// renderFetchResult prints the fetched page: title and final URL as a header,
// then the sanitized content, with a note when the gateway truncated it so a
// short body is not mistaken for the whole page.
//
// proven-by: TestRenderFetchResult_HeadsWithURLAndFlagsTruncation
func renderFetchResult(res aigateway.FetchResult) string {
	var b strings.Builder
	if res.Title != "" {
		b.WriteString(res.Title + "\n")
	}
	url := res.FinalURL
	if url == "" {
		url = res.URL
	}
	b.WriteString(url + "\n\n")
	b.WriteString(res.Content)
	if res.Truncated {
		b.WriteString("\n\n(truncated by the gateway)")
	}
	return b.String()
}

func snip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// MountGatewayTools fetches and registers the gateway's hosted tools,
// reporting what it did — the mountMCP shape, minus the server list (there
// is exactly one gateway).
//
// proven-by: TestMountGatewayTools_RegistersAndAnnounces
func MountGatewayTools(ctx context.Context, reg *agentloop.Registry, c *aigateway.Client, ui agentloop.UI) {
	tools, unavailable, err := GatewayToolsOf(ctx, c)
	if err != nil {
		// An older gateway without /v1/tools, or one that is down, costs
		// its tools and nothing else — the shell still has local + MCP.
		ui.Note("gateway: hosted tools unavailable (%v)", err)
		return
	}
	for _, t := range tools {
		if err := reg.Add(t); err != nil {
			ui.Note("gateway: %v", err)
		}
	}
	for _, u := range unavailable {
		ui.Note("gateway: not offering %s", u)
	}
	if len(tools) > 0 {
		ui.Note("gateway: offering %d hosted tool(s)", len(tools))
	}
}
