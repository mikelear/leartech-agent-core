// Package cycle resolves what a respawned agent run already produced — a prior
// PR, or commits pushed on its branch — BEFORE the model is asked anything, and
// turns that into the brief the model reads.
//
// WHY IT IS GO RATHER THAN PROSE IN THE GOAL. A respawned iteration has to begin
// by reading its own PR: the PR is where the previous attempt recorded progress,
// and a pod killed by a deadline or eviction records nothing on its way out.
// Leaving that to goal text made it a suggestion, and dev-agent runs reported
// success on PRs that were never merged.
//
// WHY IT IS LIBRARY CODE. The shell and the middle layer drive the same loop; the
// cycle resolution belongs beside the loop, not copied per client. Lifted from
// the shell's agentcycle.go, decoupled from the CLI: it takes the governed
// Registry + a UI (both from agentloop), an env lookup, a Logger interface, and a
// Git runner — the shell supplies its agentLog and os/exec git; a test supplies
// fakes. No term, no clusters, no os/exec of its own.
package cycle

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/mikelear/leartech-go-common/pkg/agentloop"
)

// ToolResolvePR is the read side of PR identity, qualified the way agentloop
// mounts it (mcp__<server>__<tool>).
const ToolResolvePR = "mcp__pr_context__resolve_pr"

// State is what a run knows about itself before the model is asked anything.
type State struct {
	// Asked records the tool was reachable and called — what separates "there
	// is no PR" from "we never found out".
	Asked   bool
	Found   bool
	Number  int
	PRState string
	Merged  bool
	Source  string
	// Blind is why the cycle could not resolve, when it could not; carried into
	// the brief rather than dropped.
	Blind string
	// ResumedCommits is how many commits the run's OWN branch carries beyond the
	// default — work a predecessor pushed before it stopped at a branch (no PR).
	ResumedCommits int
	ResumedBranch  string
}

// Logger records the resolved cycle. The shell's agentLog implements it; it is
// an interface so cycle has no dependency on the shell's logging.
type Logger interface {
	// Cycle records the resolved state as one structured event. A blind cycle
	// should be recorded at WARN so a degraded start is greppable.
	Cycle(State)
}

// Git runs a git command in a working directory and returns stdout. The shell
// passes an os/exec runner; a test passes a fake. cycle never shells out itself.
type Git func(root string, args ...string) (string, error)

// Resolve asks the MCP who this run already is, before any model call. It calls
// THROUGH the governed registry and reports THROUGH the UI, so a deterministic
// call is indistinguishable in logs from one the model chose to make.
//
// proven-by: cycle_test.go
func Resolve(reg *agentloop.Registry, ui agentloop.UI, env func(string) string, log Logger, git Git) State {
	st := State{}

	repo := strings.TrimSpace(env("LEARTECH_PR_REPO"))
	if repo == "" {
		st.Blind = "LEARTECH_PR_REPO is empty, so there is no repository to resolve a PR in"
		log.Cycle(st)
		return st
	}

	mounted := false
	for _, n := range reg.Names() {
		if n == ToolResolvePR {
			mounted = true
			break
		}
	}
	if !mounted {
		st.Blind = fmt.Sprintf("%s is not mounted, so this run cannot find out what it produced before", ToolResolvePR)
		log.Cycle(st)
		return st
	}

	args, err := json.Marshal(map[string]string{
		"run_id":    env("LEARTECH_RUN_ID"),
		"namespace": env("AGENT_RUN_NAMESPACE"),
		"repo":      repo,
		"head":      InitiativeField(env("LEARTECH_INITIATIVE_YAML"), "branch"),
	})
	if err != nil {
		st.Blind = "the resolve_pr arguments could not be encoded: " + err.Error()
		log.Cycle(st)
		return st
	}

	ui.ToolStarted(ToolResolvePR, args)
	out, ok := reg.Run(ToolResolvePR, args)
	ui.ToolFinished(ToolResolvePR, ok, len(out), out)
	if !ok {
		st.Blind = "resolve_pr did not answer: " + strings.TrimSpace(firstLine(out))
		log.Cycle(st)
		return st
	}
	st.Asked = true

	var got struct {
		Found  bool   `json:"found"`
		Number int    `json:"number"`
		State  string `json:"state"`
		Merged bool   `json:"merged"`
		Source string `json:"source"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		st.Asked = false
		st.Blind = "resolve_pr returned something this cannot read: " + err.Error()
		log.Cycle(st)
		return st
	}
	st.Found, st.Number, st.PRState, st.Merged, st.Source = got.Found, got.Number, got.State, got.Merged, got.Source

	if !st.Found && !st.Merged {
		branch := strings.TrimSpace(InitiativeField(env("LEARTECH_INITIATIVE_YAML"), "branch"))
		if branch == "" {
			branch = strings.TrimSpace(env("LEARTECH_AGENT_BRANCH"))
		}
		if branch != "" {
			st.ResumedBranch = branch
			st.ResumedCommits = branchCommitsAhead(git, env("LEARTECH_AGENT_ROOT"), branch)
		}
	}

	log.Cycle(st)
	return st
}

// branchCommitsAhead counts commits on branch not on the default branch, via the
// injected git runner. Zero on any uncertainty: this only ADDS context, so a
// wrong non-zero is worse than a missed signal.
func branchCommitsAhead(git Git, root, branch string) int {
	root = strings.TrimSpace(root)
	if root == "" {
		root = "/workspace"
	}
	out, err := git(root, "rev-list", "--count", "origin/HEAD.."+branch)
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0
	}
	return n
}

// InitiativeField reads one top-level string from the controller-inlined
// initiative (JSON, which is valid YAML), without imposing a shape on the rest.
func InitiativeField(raw, key string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return ""
	}
	s, _ := m[key].(string)
	return strings.TrimSpace(s)
}

func prStateOrUnknown(s string) string {
	if strings.TrimSpace(s) == "" {
		return "in an unknown state"
	}
	return s
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
