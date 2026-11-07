// Package filelock takes an exclusive, cross-PROCESS lock on a sibling
// .lock file.
//
// WHY THIS EXISTS AT ALL. Hydra rotates the refresh token on every grant
// and refuses the one just spent. A mutex serialises one process; two
// shells are two processes sharing one credential file, and they refresh
// at nearly the SAME MOMENT because both were started from the same file
// and so hold tokens with the same expiry. Whichever loses gets
// invalid_grant and is locked out of a credential sitting valid on disk.
//
// internal/session.Lock solved this for the login session. The MCP token
// path has the SAME shape — one per-resource token file, several shells —
// and so takes the same lock. Lifting it here is what lets both use one
// implementation rather than two copies drifting apart.
package filelock

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Acquire takes an exclusive cross-process lock keyed on target, and
// returns a release func. It blocks until the lock is free.
//
// THE LOCK IS ON A SIBLING .lock FILE, not on target itself. A safefile
// write replaces target by rename, so a lock held on that inode would be
// dropped by the very write it is meant to serialise.
//
// proven-by: TestAcquire_IsHeldAgainstASecondProcess
// proven-by: TestAcquire_ReleaseLetsTheNextHolderIn
// proven-by: TestAcquire_DoesNotTouchTheTargetFile
func Acquire(target string) (release func(), err error) {
	path := target + ".lock"
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("filelock: prepare the lock directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) // #nosec G304 -- the path is the caller's target, which is this package's entire purpose
	if err != nil {
		return nil, fmt.Errorf("filelock: open the lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("filelock: take the lock: %w", err)
	}
	return func() {
		// Unlock BEFORE close. Closing releases it anyway, but doing it
		// explicitly means a future change that keeps the handle open
		// does not silently hold the lock for the life of the process.
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
