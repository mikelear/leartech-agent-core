package filelock_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/mikelear/leartech-agent-core/filelock"
)

// helperEnv makes this test binary act as a second PROCESS that tries to
// take the lock and reports whether it got it.
const helperEnv = "FILELOCK_HELPER_TARGET"

// TestMain doubles as the helper. Re-execing THIS binary is the only
// portable way to get a second process: flock is per open-file-
// description, so a goroutine here would be granted the lock this process
// already holds, and shelling out to /usr/bin/flock passes silently on
// macOS, which does not have it.
func TestMain(m *testing.M) {
	if target := os.Getenv(helperEnv); target != "" {
		done := make(chan struct{})
		go func() {
			release, err := filelock.Acquire(target)
			if err == nil {
				release()
			}
			close(done)
		}()
		select {
		case <-done:
			os.Exit(0) // took it: nobody was holding it
		case <-time.After(2 * time.Second):
			os.Exit(3) // blocked: it is held
		}
	}
	os.Exit(m.Run())
}

// The lock must hold against a SECOND PROCESS, which is the only case that
// matters: a mutex already covers one. Two shells refreshing a rotating
// refresh token is the failure it exists to prevent.
func TestAcquire_IsHeldAgainstASecondProcess(t *testing.T) {
	target := filepath.Join(t.TempDir(), "mcp-abc.json")

	release, err := filelock.Acquire(target)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	cmd := exec.Command(os.Args[0], "-test.run", "TestAcquire_IsHeldAgainstASecondProcess")
	cmd.Env = append(os.Environ(), helperEnv+"="+target)
	err = cmd.Run()
	release()

	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 3 {
		t.Fatalf("a second process was not blocked (err=%v) — refreshes are not "+
			"serialised and two shells will keep spending each other's refresh token", err)
	}
}

func TestAcquire_ReleaseLetsTheNextHolderIn(t *testing.T) {
	target := filepath.Join(t.TempDir(), "mcp-abc.json")

	release, err := filelock.Acquire(target)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	release()

	done := make(chan error, 1)
	go func() {
		r2, err := filelock.Acquire(target)
		if err == nil {
			r2()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("second Acquire after release: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the lock was never released — every later refresh would block forever")
	}
}

// The lock file is a SIBLING, not the target itself: a safefile write
// replaces the target by rename, so a lock on that inode would be dropped
// by the very write it is meant to serialise.
func TestAcquire_DoesNotTouchTheTargetFile(t *testing.T) {
	target := filepath.Join(t.TempDir(), "mcp-abc.json")

	release, err := filelock.Acquire(target)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer release()

	if _, err := os.Stat(target + ".lock"); err != nil {
		t.Errorf("no sibling lock file: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("locking created or touched the target file itself (%v)", err)
	}
}
