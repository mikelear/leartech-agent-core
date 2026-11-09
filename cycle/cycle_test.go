package cycle_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mikelear/leartech-go-common/pkg/agentloop"

	"github.com/mikelear/leartech-agent-core/cycle"
)

// recLog records the last resolved state — the shell's agentLog implements the
// same Cycle(State) method; here a test double is enough.
type recLog struct{ last cycle.State }

func (r *recLog) Cycle(s cycle.State) { r.last = s }

// quietUI satisfies agentloop.UI with no-ops; Resolve only calls ToolStarted /
// ToolFinished, and the test asserts state, not UI.
type quietUI struct{ agentloop.Quiet }

// env builds the environment the controller projects.
func env(over map[string]string) func(string) string {
	base := map[string]string{
		"LEARTECH_PR_REPO":         "mikelear/leartech-plan-api",
		"LEARTECH_RUN_ID":          "run-1",
		"AGENT_RUN_NAMESPACE":      "jx-staging",
		"LEARTECH_INITIATIVE_YAML": `{"goal":"do the thing","branch":"feat/x"}`,
	}
	for k, v := range over {
		base[k] = v
	}
	return func(k string) string { return base[k] }
}

// noGit is a git runner that always fails — branchCommitsAhead must treat that as
// zero, never a crash.
func noGit(string, ...string) (string, error) { return "", context.DeadlineExceeded }

// resolvePRReg mounts a resolve_pr stand-in in a GOVERNED registry — the same
// shape production uses: a remote tool under TrustNothing (so the server's
// readOnlyHint can't lower its effect), with a policy that allows the executes
// effect and an approver that says yes. That the governed call still goes through
// is part of what's proven.
func resolvePRReg(t *testing.T, reply string, fail bool) *agentloop.Registry {
	t.Helper()
	reg := agentloop.NewRegistry()
	reg.Govern(agentloop.NewPolicy(
		agentloop.ForEffect(agentloop.Executes, agentloop.Allow, ""),
		agentloop.ForEffect(agentloop.Writes, agentloop.Allow, ""),
	), agentloop.AllowAll{})
	errs := reg.AddRemote([]agentloop.RemoteTool{{
		Server: "pr_context", Name: "resolve_pr", Description: "read side of PR identity",
		Schema: json.RawMessage(`{"type":"object"}`), ReadOnlyHint: true,
	}}, agentloop.TrustNothing,
		func(_ context.Context, _, _ string, _ json.RawMessage) (string, error) {
			if fail {
				return reply, context.Canceled
			}
			return reply, nil
		})
	for _, e := range errs {
		t.Fatalf("mount: %v", e)
	}
	if got := reg.Names(); len(got) != 1 || got[0] != cycle.ToolResolvePR {
		t.Fatalf("mounted as %v, want [%s]", got, cycle.ToolResolvePR)
	}
	return reg
}

func TestResolve_ReportsAMergedPR(t *testing.T) {
	reg := resolvePRReg(t, `{"found":true,"number":41,"state":"CLOSED","merged":true,"source":"status"}`, false)
	log := &recLog{}
	st := cycle.Resolve(reg, quietUI{}, env(nil), log, noGit)
	if !st.Asked || !st.Found || !st.Merged || st.Number != 41 {
		t.Fatalf("state = %+v, want asked/found/merged PR 41", st)
	}
	if st.Blind != "" {
		t.Errorf("Blind = %q, want empty", st.Blind)
	}
	if log.last.Number != 41 {
		t.Errorf("logger saw %+v", log.last)
	}
}

func TestResolve_ReportsAnOpenPR(t *testing.T) {
	reg := resolvePRReg(t, `{"found":true,"number":7,"state":"OPEN","merged":false,"source":"github"}`, false)
	st := cycle.Resolve(reg, quietUI{}, env(nil), &recLog{}, noGit)
	if !st.Found || st.Merged || st.Number != 7 || st.PRState != "OPEN" {
		t.Fatalf("state = %+v, want open PR 7", st)
	}
}

func TestResolve_AMissingToolIsBlindNotAbsent(t *testing.T) {
	st := cycle.Resolve(agentloop.NewRegistry(), quietUI{}, env(nil), &recLog{}, noGit)
	if st.Asked {
		t.Error("Asked should be false when the tool is not mounted")
	}
	if !strings.Contains(st.Blind, "not mounted") {
		t.Errorf("Blind = %q, want a not-mounted reason", st.Blind)
	}
}

func TestResolve_AToolErrorIsBlindNotAbsent(t *testing.T) {
	reg := resolvePRReg(t, "resolve_pr exploded", true)
	st := cycle.Resolve(reg, quietUI{}, env(nil), &recLog{}, noGit)
	if st.Found || st.Merged {
		t.Errorf("a failed tool must not report a PR: %+v", st)
	}
	if !strings.Contains(st.Blind, "resolve_pr did not answer") {
		t.Errorf("Blind = %q", st.Blind)
	}
}

func TestResolve_NoRepoIsBlind(t *testing.T) {
	st := cycle.Resolve(agentloop.NewRegistry(), quietUI{}, env(map[string]string{"LEARTECH_PR_REPO": ""}), &recLog{}, noGit)
	if !strings.Contains(st.Blind, "LEARTECH_PR_REPO") {
		t.Errorf("Blind = %q", st.Blind)
	}
}

func TestResolve_NoPRButCommitsOnTheBranchResumes(t *testing.T) {
	reg := resolvePRReg(t, `{"found":false,"merged":false}`, false)
	// git reports 3 commits ahead on the run's branch.
	git := func(_ string, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "rev-list" {
			return "3\n", nil
		}
		return "", nil
	}
	st := cycle.Resolve(reg, quietUI{}, env(nil), &recLog{}, git)
	if st.ResumedCommits != 3 || st.ResumedBranch != "feat/x" {
		t.Fatalf("resume = %d on %q, want 3 on feat/x", st.ResumedCommits, st.ResumedBranch)
	}
}

func TestContext_RendersEachInheritedState(t *testing.T) {
	merged := cycle.Context(cycle.State{Asked: true, Found: true, Number: 41, Merged: true, Source: "status"})
	if !strings.Contains(merged, "MERGED") || !strings.Contains(merged, "#41") {
		t.Errorf("merged brief = %q", merged)
	}
	open := cycle.Context(cycle.State{Asked: true, Found: true, Number: 7, PRState: "OPEN", Source: "github"})
	if !strings.Contains(open, "#7") || !strings.Contains(open, "PREVIOUS ATTEMPT") {
		t.Errorf("open brief = %q", open)
	}
	blind := cycle.Context(cycle.State{Blind: "pr_context is not mounted"})
	if !strings.Contains(blind, "could NOT determine") || !strings.Contains(blind, "not mounted") {
		t.Errorf("blind brief = %q", blind)
	}
	first := cycle.Context(cycle.State{Asked: true, Found: false})
	if !strings.Contains(first, "first attempt") {
		t.Errorf("first brief = %q", first)
	}
	resume := cycle.Context(cycle.State{ResumedBranch: "feat/x", ResumedCommits: 3})
	if !strings.Contains(resume, "feat/x") || !strings.Contains(resume, "3 commit") {
		t.Errorf("resume brief = %q", resume)
	}
}

func TestInitiativeField(t *testing.T) {
	if got := cycle.InitiativeField(`{"branch":"feat/x"}`, "branch"); got != "feat/x" {
		t.Errorf("got %q", got)
	}
	if got := cycle.InitiativeField(`not json`, "branch"); got != "" {
		t.Errorf("unparseable should be empty, got %q", got)
	}
	if got := cycle.InitiativeField(`{"branch":123}`, "branch"); got != "" {
		t.Errorf("a non-string should be empty, got %q", got)
	}
}
