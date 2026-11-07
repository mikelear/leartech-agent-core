package gatewaytools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mikelear/leartech-go-common/pkg/agentloop"

	"github.com/mikelear/leartech-go-common/pkg/aigateway"
)

func gatewayStub(t *testing.T) (*aigateway.Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/tools" && r.Method == "GET":
			_, _ = w.Write([]byte(`{"object":"list","data":[
				{"type":"function","available":true,
				 "function":{"name":"search","description":"Search the web","parameters":{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}}},
				{"type":"function","available":true,
				 "function":{"name":"fetch","description":"Fetch a URL","parameters":{"type":"object","properties":{"url":{"type":"string"}},"required":["url"]}}}]}
			 `))
		case r.URL.Path == "/v1/fetch" && r.Method == "POST":
			_, _ = w.Write([]byte(`{"url":"https://x.example","final_url":"https://x.example","title":"X","content":"page text","content_type":"text/html"}`))
		case r.URL.Path == "/v1/search" && r.Method == "POST":
			_, _ = w.Write([]byte(`{"answer":"the sky is blue","results":[
				{"title":"A","url":"https://a.example","content":"aaa","score":0.9},
				{"title":"B","url":"https://b.example","content":"bbb"}],"provider":"searxng"}`))
		default:
			http.Error(w, "no", 404)
		}
	}))
	t.Cleanup(srv.Close)
	return aigateway.New(srv.URL, "tok", srv.Client()), srv
}

// AVAILABLE tools are offered (qualified gateway__<name>); UNAVAILABLE ones
// are reported with the scope that would fix them. A model offered a tool
// its key cannot call plans around a capability it does not have and 403s
// mid-task.
func TestGatewayToolsOf_OffersAvailableAndReportsUnavailable(t *testing.T) {
	c, _ := gatewayStub(t)
	tools, unavailable, err := GatewayToolsOf(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]agentloop.Tool{}
	for _, tl := range tools {
		names[tl.Name] = tl
	}
	if _, ok := names["gateway__search"]; !ok {
		t.Fatalf("tools=%v, want gateway__search", tools)
	}
	if _, ok := names["gateway__fetch"]; !ok {
		t.Fatalf("tools=%v, want gateway__fetch", tools)
	}
	if names["gateway__fetch"].Effect != agentloop.Spends {
		t.Errorf("fetch effect=%v, want Spends — fetch debits the key's budget", names["gateway__fetch"].Effect)
	}
	if !strings.HasPrefix(names["gateway__search"].Description, "[gateway]") {
		t.Errorf("description=%q, want the [gateway] prefix so the model and the approval prompt name the source", names["gateway__search"].Description)
	}
	if len(unavailable) != 0 {
		t.Errorf("unavailable=%v, want none — both tools are available in the stub", unavailable)
	}
}

// The invocation goes through the client, forwards the named provider, and
// the rendered result names WHO ANSWERED — the silent-fallback detector.
func TestGwInvokerFor_SearchAndFetch(t *testing.T) {
	c, _ := gatewayStub(t)
	run := gwInvokerFor(c, "search")
	out, err := run(json.RawMessage(`{"query":"why is the sky blue","provider":"searxng"}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"the sky is blue", "https://a.example", "(via searxng)"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if _, err := run(json.RawMessage(`{}`)); err == nil {
		t.Error("a search with no query returned no error")
	}
	fout, ferr := gwInvokerFor(c, "fetch")(json.RawMessage(`{"url":"https://x.example"}`))
	if ferr != nil {
		t.Fatal(ferr)
	}
	if !strings.Contains(fout, "page text") || !strings.Contains(fout, "https://x.example") {
		t.Errorf("fetch output missing page text or url:\n%s", fout)
	}
	if _, err := gwInvokerFor(c, "fetch")(json.RawMessage(`{}`)); err == nil {
		t.Error("a fetch with no url returned no error")
	}
	if _, err := gwInvokerFor(c, "nope")(json.RawMessage(`{}`)); err == nil {
		t.Error("an unimplemented gateway tool returned no error rather than saying so")
	}
}

// The registry mounts what the credential may call, and says what it did.
func TestMountGatewayTools_RegistersAndAnnounces(t *testing.T) {
	c, _ := gatewayStub(t)
	reg := agentloop.NewRegistry()
	ui := &recordingUI{}
	MountGatewayTools(context.Background(), reg, c, ui)
	names := reg.Names()
	found := false
	for _, n := range names {
		if n == "gateway__search" {
			found = true
		}
	}
	if !found {
		t.Errorf("gateway__search not registered: %v", names)
	}
	hasFetch := false
	for _, n := range names {
		if n == "gateway__fetch" {
			hasFetch = true
		}
	}
	if !hasFetch {
		t.Errorf("gateway__fetch not registered: %v", names)
	}
	joined := strings.Join(ui.notes, " | ")
	if !strings.Contains(joined, "offering 2 hosted tool") {
		t.Errorf("the mount did not announce both tools: %s", joined)
	}
}

// The approval reason for a spend names the budget, not a local file.

// The session-key mint carries the web scopes beside chat — the shell's
// hosted tools run under that key, so it is the credential whose scopes
// decide availability.

// The rendered result names the provider and snips long content.
func TestRenderSearchResults_NamesTheProviderAndSnipsContent(t *testing.T) {
	res := aigateway.SearchResults{
		Provider: "tavily",
		Items: []aigateway.SearchItem{
			{Title: "T", URL: "https://t.example", Content: strings.Repeat("x", 300)},
		},
	}
	out := renderSearchResults(res)
	if !strings.Contains(out, "(via tavily)") {
		t.Errorf("provider not named: %q", out)
	}
	if !strings.Contains(out, "…") {
		t.Errorf("long content was not snipped: %d chars", len(out))
	}
}

// The rendered fetch heads with the URL and flags gateway truncation.
func TestRenderFetchResult_HeadsWithURLAndFlagsTruncation(t *testing.T) {
	res := aigateway.FetchResult{
		Title:     "Doc",
		URL:       "https://d.example",
		FinalURL:  "https://d.example/final",
		Content:   "body",
		Truncated: true,
	}
	out := renderFetchResult(res)
	if !strings.Contains(out, "https://d.example/final") {
		t.Errorf("final url not shown: %q", out)
	}
	if !strings.Contains(out, "body") || !strings.Contains(out, "truncated") {
		t.Errorf("content or truncation flag missing: %q", out)
	}
}
