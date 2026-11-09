// Package usage accumulates a session's token spend across turns and correlates
// each gateway call to the run or session it belongs to — the cost-accounting the
// shell gained after a session once exhausted a budget with nothing on screen.
//
// WHY IT IS LIBRARY CODE. The shell and the middle layer both run metered turns
// and both must attribute spend; the accounting belongs beside the loop, once.
// Lifted from the shell's usagemeter.go, split from the CLI: the pure parts —
// Meter (observe/total), the turn correlator, the correlation source/origin,
// reportable, and the one-line renderer — live here. The term.Writer renderers
// (reportUsage/reportTotal) stay in the shell, because WHERE a line is printed is
// the shell's concern; WHAT the line says is shared.
package usage

import (
	"fmt"
	"strings"
	"sync"

	"github.com/mikelear/leartech-go-common/pkg/aigateway"
)

// Meter accumulates a session's usage across turns. Safe under concurrent turns.
type Meter struct {
	mu sync.Mutex

	turns      int
	prompt     int
	completion int
	cacheRead  int
	cacheWrite int

	// reported counts turns where the supplier said anything about caching at
	// all. ABSENT IS NOT ZERO: a supplier with no cache reports nothing, and
	// calling that a 0% hit rate invents a denominator.
	reported int
}

// Observe records one turn.
//
// proven-by: usage_test.go
func (m *Meter) Observe(u aigateway.ChatUsage) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.turns++
	m.prompt += u.PromptTokens
	m.completion += u.CompletionTokens
	if u.CacheReported() {
		m.reported++
		m.cacheRead += u.CachedTokens()
		m.cacheWrite += u.CacheWrite5mTokens() + u.CacheWrite1hTokens()
	}
}

// Total renders the session summary, or "" when no turn reported usage.
//
// proven-by: usage_test.go
func (m *Meter) Total() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.turns == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d turns, %d prompt + %d completion tokens", m.turns, m.prompt, m.completion)
	if m.reported > 0 {
		fmt.Fprintf(&b, "; %d cached, %d written", m.cacheRead, m.cacheWrite)
		if m.prompt > 0 {
			fmt.Fprintf(&b, " (%d%% of prompt served from cache)", m.cacheRead*100/m.prompt)
		}
	}
	return b.String()
}

// TurnCorrelator reports the id, and the KIND of id, for each gateway call. A
// bare id (not <session>-<turn>): the gateway records run_id/session_id and
// stamps its own request_id per call. Empty when there is neither, so an
// uncorrelated run never shares a real-looking key with every other.
//
// proven-by: usage_test.go
func TurnCorrelator(id func() string, kind func() string) func() (string, string) {
	var mu sync.Mutex
	return func() (string, string) {
		mu.Lock()
		defer mu.Unlock()
		v := id()
		if v == "" {
			return "", ""
		}
		switch kind() {
		case "session":
			return v, aigateway.CorrelateSession
		case "LEARTECH_RUN_ID":
			return v, aigateway.CorrelateRun
		}
		return "", ""
	}
}

// CorrelatedGateway builds a gateway client that correlates every call. One
// constructor both the start-up and key-rotation sites call, so a rotated client
// cannot silently drop out of the session's rows.
//
// proven-by: usage_test.go
func CorrelatedGateway(base, key string, correlate func() (value, kind string)) *aigateway.Client {
	return aigateway.New(base, key, nil).Correlate(correlate)
}

// CorrelatedGatewayInGroup is CorrelatedGateway plus the correlation GROUP id
// that ties this run/session to the larger effort it belongs to -- a Project's
// research and the Plan it produces, a shell session and the sub-tasks it
// spawns. Empty group leaves the group header absent, so this is a safe
// superset of CorrelatedGateway.
//
// proven-by: usage_test.go
func CorrelatedGatewayInGroup(base, key, group string, correlate func() (value, kind string)) *aigateway.Client {
	c := aigateway.New(base, key, nil).Correlate(correlate)
	if group != "" {
		c = c.CorrelateGroup(group)
	}
	return c
}

// GroupSource resolves the correlation GROUP id from the environment:
// LEARTECH_CORRELATION_ID when set, else empty. Unlike the run/session source
// it is additive -- a caller may carry a group id with either a run or a
// session, or on its own.
//
// proven-by: usage_test.go
func GroupSource(env func(string) string) string {
	return strings.TrimSpace(env("LEARTECH_CORRELATION_ID"))
}

// CorrelationSource resolves the id a run's turns are grouped under: the
// registered session if any, else LEARTECH_RUN_ID (a scripted run registers no
// session but still carries the run id), else empty.
//
// proven-by: usage_test.go
func CorrelationSource(session func() string, env func(string) string) func() string {
	return func() string {
		if id := session(); id != "" {
			return id
		}
		return strings.TrimSpace(env("LEARTECH_RUN_ID"))
	}
}

// CorrelationOrigin names which source answered, for the startup line — the same
// value means different things from a session vs an inherited run id.
//
// proven-by: usage_test.go
func CorrelationOrigin(session func() string, env func(string) string) string {
	if session() != "" {
		return "session"
	}
	if strings.TrimSpace(env("LEARTECH_RUN_ID")) != "" {
		return "LEARTECH_RUN_ID"
	}
	return "none"
}

// Reportable is whether a turn's usage says anything at all — a supplier can
// report zero tokens and no cache detail (the echo adapter does), which is not a
// figure worth a line.
//
// proven-by: usage_test.go
func Reportable(u aigateway.ChatUsage) bool {
	return u.PromptTokens > 0 || u.CompletionTokens > 0 || u.CacheReported()
}

// Line renders one turn's cost: a plain prompt+completion count, or the
// fresh/cached/written breakdown when the supplier reports cache detail.
//
// proven-by: usage_test.go
func Line(u aigateway.ChatUsage) string {
	if !u.CacheReported() {
		return fmt.Sprintf("%d prompt + %d completion tokens", u.PromptTokens, u.CompletionTokens)
	}
	parts := []string{fmt.Sprintf("%d fresh", u.FreshTokens())}
	if n := u.CachedTokens(); n > 0 {
		parts = append(parts, fmt.Sprintf("%d cached", n))
	}
	if n := u.CacheWrite5mTokens(); n > 0 {
		parts = append(parts, fmt.Sprintf("%d written 5m", n))
	}
	if n := u.CacheWrite1hTokens(); n > 0 {
		parts = append(parts, fmt.Sprintf("%d written 1h", n))
	}
	return fmt.Sprintf("%d prompt (%s) + %d completion tokens",
		u.PromptTokens, strings.Join(parts, ", "), u.CompletionTokens)
}

// Snapshot is the meter's accumulated figures, for a caller that logs them (the
// shell's run-total event). Returned by value under the lock, so a reader never
// races the accumulator.
type Snapshot struct {
	Turns      int
	Prompt     int
	Completion int
	CacheRead  int
	CacheWrite int
	// Reported is the count of turns whose supplier reported cache detail —
	// absent-is-not-zero, so a caller can tell "no cache" from "cache, none hit".
	Reported int
}

// Snapshot returns the current accumulated figures.
//
// proven-by: usage_test.go
func (m *Meter) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return Snapshot{
		Turns: m.turns, Prompt: m.prompt, Completion: m.completion,
		CacheRead: m.cacheRead, CacheWrite: m.cacheWrite, Reported: m.reported,
	}
}
