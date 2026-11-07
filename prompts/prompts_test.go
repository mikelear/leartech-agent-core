package prompts_test

import (
	"strings"
	"testing"

	"github.com/mikelear/leartech-agent-core/prompts"
)

func TestAgentPrompt_DoesNotAskForWhatTheLoopAlreadyDoes(t *testing.T) {
	p := prompts.Dev

	if strings.Contains(p, "post_pr_handoff") {
		t.Error("the prompt asks the model to post a handoff, which the loop does deterministically")
	}
	if strings.Contains(p, "several of these in a row") {
		t.Error("the prompt describes repeated window_elapsed, which retryingWaits absorbs")
	}
}

func TestAgentPrompt_EmptyChecksAreNotGreen(t *testing.T) {
	p := prompts.Dev
	if !strings.Contains(p, "Empty is not green") {
		t.Error("the prompt must say an empty check set is not a pass")
	}
	if !strings.Contains(p, "not REGISTERED yet") {
		t.Error("no_checks must be explained as checks-not-registered-yet, not repo-has-none")
	}
	norm := strings.Join(strings.Fields(p), " ")
	if !strings.Contains(norm, "Never exit success on an empty check set") {
		t.Error("the prompt must forbid exiting success on an empty check set")
	}
}

func TestAgentPrompt_ExplainsTheWaitVerdicts(t *testing.T) {
	p := prompts.Dev

	for _, want := range []string{"all_passed", "first_failure", "no_checks", "window_elapsed"} {
		if !strings.Contains(p, want) {
			t.Errorf("the prompt does not explain the %q verdict", want)
		}
	}
	if !strings.Contains(p, "not confirmed") {
		t.Error("a bounded all_passed is not distinguished from a full one")
	}
}

func TestAgentPrompt_TellsItHowToTellItsFailuresFromInherited(t *testing.T) {
	p := prompts.Dev

	if !strings.Contains(p, "diff of your own PR") {
		t.Error("it is not told the test for whose failure it is")
	}
	if !strings.Contains(p, "it is yours") || !strings.Contains(p, "inherited") {
		t.Error("it is not told both outcomes of that test")
	}
}

func TestAgentPrompt_TellsItToBranchBeforeWriting(t *testing.T) {
	p := prompts.Dev

	if !strings.Contains(p, "LEARTECH_AGENT_BRANCH") {
		t.Error("the prompt never names the branch variable the run owns")
	}
	// Ordering matters: branching after the work is done is what leaves
	// commits on the default branch that preserveWork then refuses to push.
	branch := strings.Index(p, "BRANCH FIRST")
	change := strings.Index(p, "Make the change")
	if branch < 0 {
		t.Fatal("the prompt does not tell it to branch first")
	}
	if change > 0 && branch > change {
		t.Error("branching is described after making the change, which is the order that loses work")
	}
	if !strings.Contains(p, "cannot be pushed") {
		t.Error("it does not say WHY branching first matters, so it reads as ceremony")
	}
}

func TestAgentPrompt_TellsItToIterateOnFailingChecks(t *testing.T) {
	p := prompts.Dev

	for _, want := range []string{
		"wait_for_first_failure_or_all_pass", // the fail-fast primitive
		"step_logs",                          // how to find out why
		"After EVERY push",                   // the cadence
	} {
		if !strings.Contains(p, want) {
			t.Errorf("the loop does not mention %q", want)
		}
	}
	if !strings.Contains(p, "Repeat until") {
		t.Error("nothing tells it to iterate; reading one failure and stopping is what happened")
	}
}

func TestAgentPrompt_TellsItToStopWhenGreen(t *testing.T) {
	p := prompts.Dev

	if !strings.Contains(p, "STOP when the checks are green") {
		t.Error("it is not told to stop at green")
	}
	if !strings.Contains(p, "Tide merges, not you") {
		t.Error("it is not told who merges, so it may wait for a merge that is not its job")
	}
}

func TestAgentPrompts_TeachNoGH(t *testing.T) {
	for name, p := range map[string]string{
		"default":      prompts.Dev,
		"ba":           prompts.BAResearch,
		"integration":  prompts.IntegrationSeed,
		"cartographer": prompts.Cartographer,
		"featurePlan":  prompts.FeaturePlan,
	} {
		for _, banned := range []string{"gh issue view", "gh api repos", "gh pr ", "gh auth"} {
			if strings.Contains(p, banned) {
				t.Errorf("the %s prompt still teaches %q; agents use the forge MCP so the same prompt works on GitHub or Forgejo", name, banned)
			}
		}
	}
}

func TestAgentPrompts_TheRolesAreDistinct(t *testing.T) {
	prompts := map[string]string{
		"dev":          prompts.Dev,
		"ba":           prompts.BAResearch,
		"integration":  prompts.IntegrationSeed,
		"cartographer": prompts.Cartographer,
		"feature":      prompts.FeaturePlan,
	}
	for a := range prompts {
		for b := range prompts {
			if a < b && prompts[a] == prompts[b] {
				t.Errorf("the %q and %q prompts are identical — each role must have its own", a, b)
			}
		}
	}
}

func TestBAResearchPrompt_AsksForCitedEvidence(t *testing.T) {
	p := prompts.BAResearch
	if !strings.Contains(p, "research/<slug>.md") {
		t.Error("the BA prompt must name the EXACT finding path research/<slug>.md — the workflow reads research/, so a loose 'research/' is not enough")
	}
	// The path is a contract with project resume/submit; naming the wrong dir
	// (findings/, docs/) must be called out as invisible, not merely preferred.
	if !strings.Contains(p, "project submit") || !strings.Contains(p, "invisible") {
		t.Error("the BA prompt must say WHY the path is fixed: the workflow reads research/, so a finding elsewhere is invisible to project submit")
	}
	if !strings.Contains(p, "Confidence") {
		t.Error("the BA prompt must require a confidence line — a finding a human merges states how sure it is")
	}
	if !strings.Contains(p, "invent") {
		t.Error("the BA prompt must forbid invented sources — a cited finding is the whole point")
	}
}

func TestBAResearchPrompt_DoesNotChaseGreenChecks(t *testing.T) {
	p := prompts.BAResearch
	for _, devism := range []string{"every check is green", "checks are green", "wait_for_first_failure"} {
		if strings.Contains(p, devism) {
			t.Errorf("the BA research prompt carries the dev-loop instruction %q; a research finding has no CI to turn green", devism)
		}
	}
	if !strings.Contains(p, "STOP once the PR is open") {
		t.Error("the BA prompt must tell the agent to stop at the open PR — a human reviews and merges the finding")
	}
}

func TestBAResearchPrompt_NamesTheIssueAndTheFindingPR(t *testing.T) {
	p := prompts.BAResearch
	if !strings.Contains(p, "mcp__forge__get_issue") {
		t.Error("the BA prompt must tell the agent to read the ba-question Issue with the forge tool (it only gets repo+number in the brief)")
	}
	if !strings.Contains(p, "mcp__pr_context__open_pr") {
		t.Error("the BA prompt must name the open_pr tool — the agent opens the finding PR, it has no raw push path for the PR")
	}
	if !strings.Contains(p, "Closes #") {
		t.Error("the BA prompt must put Closes #<number> in the PR so merging the finding closes the Issue")
	}
}

func TestCartographerPrompt_DoesNotEditWatchesOrLenses(t *testing.T) {
	p := prompts.Cartographer
	if !strings.Contains(p, "arch-proposal") {
		t.Error("the Cartographer prompt must route proposals through arch-proposal Issues, not direct edits")
	}
	if !strings.Contains(p, "Neither file is yours to edit") {
		t.Error("the Cartographer prompt must say watches.yaml/lenses.yaml are not the agent's to edit")
	}
}

func TestCartographerPrompt_ReadsWatchesAndLensesFromTheArchRepo(t *testing.T) {
	p := prompts.Cartographer
	for _, want := range []string{"watches.yaml", "lenses.yaml", "dossiers/<repo>.md", "estate.md"} {
		if !strings.Contains(p, want) {
			t.Errorf("the Cartographer prompt must name %q — the dossier contract is fixed by the design", want)
		}
	}
}

func TestCartographerPrompt_StopsAtThePR(t *testing.T) {
	p := prompts.Cartographer
	if !strings.Contains(p, "STOP once the PR is open") {
		t.Error("the Cartographer prompt must stop at the open PR — a human merges the pass")
	}
	for _, devism := range []string{"wait_for_first_failure", "checks are green"} {
		if strings.Contains(p, devism) {
			t.Errorf("the Cartographer prompt carries the dev-loop instruction %q; a dossier pass has no CI to turn green", devism)
		}
	}
}

func TestCartographerPrompt_WritesDossiersAndEstateAsOnePR(t *testing.T) {
	p := prompts.Cartographer
	if !strings.Contains(p, "ONE pull request") {
		t.Error("the Cartographer prompt must open ONE PR per pass — the estate map must not land separately from the dossiers")
	}
	if !strings.Contains(p, "mcp__pr_context__open_pr") {
		t.Error("the Cartographer prompt must name the open_pr tool — the pass PR is tracked, not a raw gh PR")
	}
}

func TestFeaturePrompt_AuthorsAPlanNotProductCode(t *testing.T) {
	p := prompts.FeaturePlan
	if !strings.Contains(p, "not product code") {
		t.Error("the feature prompt must say its deliverable is a Plan, not product code")
	}
	for _, devism := range []string{"wait_for_first_failure", "every check is green"} {
		if strings.Contains(p, devism) {
			t.Errorf("the feature prompt carries the dev-loop instruction %q; authoring a Plan has no CI to turn green", devism)
		}
	}
}

func TestFeaturePrompt_ReadsProjectEvidenceAndTheDossier(t *testing.T) {
	p := prompts.FeaturePlan
	for _, want := range []string{"research/", "architecture/services.md", "design/", "dossiers/"} {
		if !strings.Contains(p, want) {
			t.Errorf("the feature prompt must read %q — a Plan built without it re-derives it in code", want)
		}
	}
	if !strings.Contains(p, "paused: true") {
		t.Error("the feature prompt must pin spec.paused true — Gate 3 is a human unpause")
	}
	if !strings.Contains(p, "auto-injected") || !strings.Contains(p, "R23") {
		t.Error("the feature prompt must not hand-author verify-release-flow (R23 auto-injects it)")
	}
}

func TestFeaturePrompt_StopsAtTheCatalogPR(t *testing.T) {
	p := prompts.FeaturePlan
	if !strings.Contains(p, "leartech-plan-catalog") {
		t.Error("the feature prompt must name the catalog as the PR target")
	}
	if !strings.Contains(p, "STOP once the catalog PR is open") {
		t.Error("the feature prompt must stop at the open PR — lint and the human merge are the gate")
	}
	if !strings.Contains(p, "ba-question") {
		t.Error("the feature prompt must know the honest fallback: open a ba-question instead of a Plan on a gap")
	}
}

func TestIntegrationSeedPrompt_DoesNotHandAuthorTheIntegration(t *testing.T) {
	p := prompts.IntegrationSeed
	if !strings.Contains(p, "do NOT hand-author") && !strings.Contains(p, "not wiring the provider into one app") {
		t.Error("the integration prompt must warn against hand-authoring the integration — it drives the Project verbs instead")
	}
	// A gap in the contract opens a ba-question and STOPS, not a guess.
	if !strings.Contains(p, "project ask") || !strings.Contains(p, "STOP") {
		t.Error("the integration prompt must open a ba-question for a contract gap and stop, not guess")
	}
}

func TestIntegrationSeedPrompt_StopsForHumanReviewAtTheCatalogPR(t *testing.T) {
	p := prompts.IntegrationSeed
	if !strings.Contains(p, "leartech-plan-catalog") {
		t.Error("the integration prompt must name the catalog as the hand-off target")
	}
	if !strings.Contains(p, "PAUSED") || !strings.Contains(p, "Gate 3") {
		t.Error("the integration prompt must say Plans land PAUSED and Gate 3 is a human unpause")
	}
	if !strings.Contains(p, "you do not merge the catalog PR") && !strings.Contains(p, "a human does") {
		t.Error("the integration prompt must stop at the catalog PR for a human to merge")
	}
}

func TestIntegrationSeedPrompt_UsesTheProjectVerbs(t *testing.T) {
	p := prompts.IntegrationSeed
	for _, verb := range []string{
		"ship-proven project ask",
		"ship-proven project architecture",
		"ship-proven project plans",
		"ship-proven project graduate",
	} {
		if !strings.Contains(p, verb) {
			t.Errorf("the integration prompt must drive the work with %q, not hand-authored code", verb)
		}
	}
	// It reads the seed Issue, which carries the resolved contract.
	if !strings.Contains(p, "mcp__forge__get_issue") {
		t.Error("the integration prompt must read the seed Issue — it carries the provider contract")
	}
}

func TestForRole_SelectsTheBuiltInPromptAndRefusesTheUnknown(t *testing.T) {
	cases := map[string]string{
		"":             prompts.Dev,
		"dev":          prompts.Dev,
		"ba":           prompts.BAResearch,
		"integration":  prompts.IntegrationSeed,
		"cartographer": prompts.Cartographer,
		"feature":      prompts.FeaturePlan,
	}
	for role, want := range cases {
		got, ok := prompts.ForRole(role)
		if !ok || got != want {
			t.Errorf("ForRole(%q): ok=%v, prompt mismatch", role, ok)
		}
	}
	if _, ok := prompts.ForRole("nonsense"); ok {
		t.Error("an unknown role must return ok=false so the caller can refuse it")
	}
}
