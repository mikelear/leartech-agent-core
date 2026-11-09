package cycle_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikelear/leartech-agent-core/cycle"
)

// realGit is the os/exec runner the shell injects — used here so BranchCommitsAhead
// is proven against actual git, not only a stub. (The shell's gitIn is the same
// shape.)
func realGit(root string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// gitRepoWithRemote builds a work tree with an origin and, optionally, a branch
// carrying one commit beyond main. Lifted from the shell's preservework_test.go
// so the resumed-work count travels with BranchCommitsAhead.
func gitRepoWithRemote(t *testing.T, branch string) string {
	t.Helper()
	base := t.TempDir()
	origin := filepath.Join(base, "origin.git")
	work := filepath.Join(base, "work")
	must := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
		}
	}
	must(base, "init", "--bare", "-q", origin)
	must(base, "init", "-q", "-b", "main", work)
	must(work, "config", "user.email", "t@example.com")
	must(work, "config", "user.name", "t")
	must(work, "commit", "-q", "--allow-empty", "-m", "base")
	must(work, "remote", "add", "origin", origin)
	must(work, "push", "-q", "origin", "main")
	if branch != "" {
		must(work, "checkout", "-q", "-b", branch)
		must(work, "commit", "-q", "--allow-empty", "-m", "the work that must survive")
		must(work, "push", "-q", "-u", "origin", branch)
		must(work, "remote", "set-head", "origin", "main")
	}
	return work
}

func TestBranchCommitsAhead_CountsWipOnTheRunsBranch(t *testing.T) {
	work := gitRepoWithRemote(t, "agent/resumed")
	if n := cycle.BranchCommitsAhead(realGit, work, "agent/resumed"); n < 1 {
		t.Fatalf("expected >=1 inherited commit, got %d", n)
	}
}

func TestBranchCommitsAhead_ZeroOnAFreshBranch(t *testing.T) {
	work := gitRepoWithRemote(t, "")
	cmd := exec.Command("git", "remote", "set-head", "origin", "main")
	cmd.Dir = work
	_ = cmd.Run()
	cmd = exec.Command("git", "checkout", "-q", "-b", "agent/fresh")
	cmd.Dir = work
	_ = cmd.Run()
	if n := cycle.BranchCommitsAhead(realGit, work, "agent/fresh"); n != 0 {
		t.Fatalf("a branch with no new commits counted %d", n)
	}
}

func TestBranchCommitsAhead_ZeroWhenGitCannotAnswer(t *testing.T) {
	if n := cycle.BranchCommitsAhead(realGit, t.TempDir(), "whatever"); n != 0 {
		t.Fatalf("a non-repo answered %d, want 0 (fail safe)", n)
	}
}
