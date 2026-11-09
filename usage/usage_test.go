package usage_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/mikelear/leartech-go-common/pkg/aigateway"

	"github.com/mikelear/leartech-agent-core/usage"
)

func withCache(prompt, completion, read, write5m int) aigateway.ChatUsage {
	return aigateway.ChatUsage{
		PromptTokens: prompt, CompletionTokens: completion,
		TotalTokens:         prompt + completion,
		PromptTokensDetails: &aigateway.PromptTokensDetails{CachedTokens: read},
		LeartechCache:       &aigateway.LeartechCache{Write5mTokens: write5m},
	}
}

func withoutCache(prompt, completion int) aigateway.ChatUsage {
	return aigateway.ChatUsage{
		PromptTokens: prompt, CompletionTokens: completion,
		TotalTokens: prompt + completion,
	}
}

func TestCorrelationOrigin_NamesWhichSourceAnswered(t *testing.T) {
	withRun := envOf(map[string]string{"LEARTECH_RUN_ID": "run-1"})
	cases := []struct {
		name    string
		session func() string
		env     func(string) string
		want    string
	}{
		{"registered", func() string { return "s" }, withRun, "session"},
		{"scripted", func() string { return "" }, withRun, "LEARTECH_RUN_ID"},
		{"neither", func() string { return "" }, envOf(nil), "none"},
	}
	for _, c := range cases {
		if got := usage.CorrelationOrigin(c.session, c.env); got != c.want {
			t.Errorf("%s: origin = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestCorrelationSource_FallsBackToTheRunIDWhenScripted(t *testing.T) {
	src := usage.CorrelationSource(
		func() string { return "" },
		envOf(map[string]string{"LEARTECH_RUN_ID": "run-123"}))
	if got := src(); got != "run-123" {
		t.Errorf("got %q, want run-123", got)
	}
}

func TestCorrelationSource_PrefersTheRegisteredSession(t *testing.T) {
	src := usage.CorrelationSource(
		func() string { return "sess-real" },
		envOf(map[string]string{"LEARTECH_RUN_ID": "run-123"}))
	if got := src(); got != "sess-real" {
		t.Errorf("got %q, want the session id to win", got)
	}
}

func TestCorrelationSource_TreatsBlankRunIDAsAbsent(t *testing.T) {
	src := usage.CorrelationSource(
		func() string { return "" },
		envOf(map[string]string{"LEARTECH_RUN_ID": "   "}))
	if got := src(); got != "" {
		t.Errorf("got %q, want empty for a blank run id", got)
	}
}

func TestCorrelationSource_WithNeitherCorrelatesNothing(t *testing.T) {
	src := usage.CorrelationSource(func() string { return "" }, envOf(nil))
	if got := src(); got != "" {
		t.Errorf("got %q, want empty with no session and no run id", got)
	}
}

func TestTurnCorrelator_AnUnknownKindCorrelatesNothing(t *testing.T) {
	c := usage.TurnCorrelator(
		func() string { return "something" },
		func() string { return "invented" })
	if v, kind := c(); v != "" || kind != "" {
		t.Errorf("got (%q, %q), want empty for an unrecognised kind", v, kind)
	}
}

func TestTurnCorrelator_IsSafeUnderConcurrentTurns(t *testing.T) {
	c := usage.TurnCorrelator(func() string { return "s" }, func() string { return "session" })
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if v, kind := c(); v != "s" || kind != aigateway.CorrelateSession {
				t.Errorf("got (%q, %q)", v, kind)
			}
		}()
	}
	wg.Wait()
}

func TestTurnCorrelator_ReportsAnInheritedRunAsARun(t *testing.T) {
	c := usage.TurnCorrelator(
		func() string { return "agentrun-x-3" },
		func() string { return "LEARTECH_RUN_ID" })
	v, kind := c()
	if v != "agentrun-x-3" || kind != aigateway.CorrelateRun {
		t.Errorf("got (%q, %q), want (agentrun-x-3, %q)", v, kind, aigateway.CorrelateRun)
	}
}

func TestTurnCorrelator_ReportsTheSessionAndItsKind(t *testing.T) {
	c := usage.TurnCorrelator(
		func() string { return "sess-7" },
		func() string { return "session" })
	for i := 0; i < 3; i++ {
		v, kind := c()
		if v != "sess-7" {
			t.Errorf("call %d: value = %q, want the bare session id with no counter", i+1, v)
		}
		if kind != aigateway.CorrelateSession {
			t.Errorf("call %d: kind = %q, want %q", i+1, kind, aigateway.CorrelateSession)
		}
	}
}

func TestTurnCorrelator_WithoutASessionCorrelatesNothing(t *testing.T) {
	c := usage.TurnCorrelator(func() string { return "" }, func() string { return "none" })
	for i := 0; i < 3; i++ {
		if v, kind := c(); v != "" || kind != "" {
			t.Fatalf("call %d = (%q, %q), want empty", i+1, v, kind)
		}
	}
}

func TestUsageMeter_AccumulatesAcrossTurns(t *testing.T) {
	m := &usage.Meter{}
	m.Observe(withCache(1000, 50, 0, 1000))
	m.Observe(withCache(2000, 60, 1000, 1000))
	m.Observe(withCache(3000, 70, 2000, 1000))

	got := m.Total()
	for _, want := range []string{"3 turns", "6000 prompt", "180 completion", "3000 cached"} {
		if !strings.Contains(got, want) {
			t.Errorf("total() = %q, missing %q", got, want)
		}
	}
	// 3000 of 6000 prompt tokens came from cache.
	if !strings.Contains(got, "50%") {
		t.Errorf("total() = %q, want the 50%% cache share", got)
	}
}

func TestUsageMeter_AnUnreportedCacheIsNotCountedAsZero(t *testing.T) {
	m := &usage.Meter{}
	m.Observe(withoutCache(500, 20))
	m.Observe(withoutCache(500, 20))

	got := m.Total()
	if strings.Contains(got, "cached") || strings.Contains(got, "%") {
		t.Errorf("total() = %q, want no cache claim for a supplier that reported none", got)
	}
	if !strings.Contains(got, "1000 prompt") {
		t.Errorf("total() = %q, want the token counts regardless", got)
	}
}

func TestUsageMeter_TotalIsSilentBeforeAnyTurn(t *testing.T) {
	m := &usage.Meter{}
	if got := m.Total(); got != "" {
		t.Errorf("total() = %q, want empty before any turn", got)
	}
}

func TestUsageMeter_TotalOmitsCacheWhenNoSupplierReportedAny(t *testing.T) {
	m := &usage.Meter{}
	m.Observe(withoutCache(100, 10))
	if strings.Contains(m.Total(), "cached") {
		t.Error("cache appeared with no supplier reporting it")
	}
	m.Observe(withCache(100, 10, 64, 0))
	if !strings.Contains(m.Total(), "64 cached") {
		t.Errorf("total() = %q, want the reported cache once one supplier reported", m.Total())
	}
}

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// CorrelatedGateway stamps the correlation id on the request — the lib-level
// proof (the shell keeps the reminting-rotation variant, which needs its own
// Streamer). One request, assert the header the gateway groups on.
func TestCorrelatedGateway_StampsTheCorrelationID(t *testing.T) {
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("X-Leartech-Session-Id")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(srv.Close)

	correlate := usage.TurnCorrelator(
		func() string { return "sess-1" },
		func() string { return "session" })
	c := usage.CorrelatedGateway(srv.URL, "key", correlate)
	ch, err := c.ChatStream(context.Background(), aigateway.ChatRequest{Model: "claude"})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	for range ch {
	}
	if seen != "sess-1" {
		t.Errorf("correlation id = %q, want sess-1", seen)
	}
}
