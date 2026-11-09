package cycle

import (
	"fmt"
	"strings"
)

// Context is what the model is told about the state it inherited — a merged PR is
// a STOP, an open PR is the prior attempt to resume, blindness is stated not
// omitted.
//
// proven-by: cycle_test.go
func Context(st State) string {
	var b strings.Builder
	b.WriteString("## The state this run inherited\n\n")

	switch {
	case st.Blind != "":
		fmt.Fprintf(&b, "This run could NOT determine whether it has already produced a pull "+
			"request: %s.\n\nTreat the repository as being in an unknown state. Do NOT assume "+
			"this is a first attempt: a previous attempt may have pushed a branch, opened a PR "+
			"or landed commits before it was killed. Find out from the repository itself before "+
			"changing anything.\n", st.Blind)

	case st.Merged:
		fmt.Fprintf(&b, "Pull request #%d for this run is already MERGED (resolved from %q).\n\n"+
			"THE WORK HAS LANDED. Do not redo it and do not open another PR. Confirm that what "+
			"was asked for is present on the default branch, report that, and stop.\n",
			st.Number, st.Source)

	case st.Found:
		fmt.Fprintf(&b, "Pull request #%d for this run already exists and is %s (resolved from %q).\n\n"+
			"IT IS THE RECORD OF A PREVIOUS ATTEMPT that did not finish — most likely it was "+
			"killed by a deadline or an eviction, which gives it no chance to say so. BEFORE changing "+
			"anything, read the PR and your own last comment on it, then `.shipproven/CONTINUE.md` "+
			"on the branch if present, and `git diff origin/HEAD...HEAD`. Then continue that work from "+
			"the first gap rather than re-surveying or starting again — you have a limited tool budget "+
			"this iteration. Push to the same branch and do not open a second PR; the checks run on the "+
			"existing one.\n", st.Number, strings.ToLower(prStateOrUnknown(st.PRState)), st.Source)

	case st.ResumedCommits > 0:
		fmt.Fprintf(&b, "No pull request exists for this run yet, but your own branch %q already "+
			"carries %d commit(s) a previous attempt pushed before it was killed — most likely by "+
			"an eviction, a deadline or the tool budget, which gave it no chance to open a PR.\n\n"+
			"THE WORK IS ON THE BRANCH, NOT LOST, AND YOU HAVE A LIMITED TOOL BUDGET THIS "+
			"ITERATION — SPEND IT CLOSING THE GAP, NOT RE-SURVEYING. Read `.shipproven/CONTINUE.md` "+
			"on the branch FIRST if it is present: the previous iteration's harness wrote where it "+
			"got to there. Then do ONE orientation pass, not a full re-exploration: "+
			"`git log --oneline origin/HEAD..HEAD` to see what landed "+
			"and one `go build ./... && go test ./...` (or the repo's `make`) to see what the "+
			"predecessor left failing. That is your worklist. Then go straight to the FIRST thing "+
			"that is not done — do not re-read files that already build, do not re-derive the plan, "+
			"do not restate the contract. The single most important thing an unfinished run can do "+
			"is REACH A PULL REQUEST: the moment the build and tests pass, open ONE PR with "+
			"mcp__pr_context__open_pr (same branch, never a second PR) so the checkpoint has "+
			"somewhere durable to live and CI can start — you can keep fixing on the PR. A run "+
			"that polishes for ten turns and is then killed before opening a PR leaves the next "+
			"iteration exactly where you started. Open early, iterate on the gates.\n",
			st.ResumedBranch, st.ResumedCommits)

	default:
		b.WriteString("No pull request exists for this run yet, and that was confirmed rather " +
			"than assumed. This is a first attempt.\n")
	}
	return b.String()
}
