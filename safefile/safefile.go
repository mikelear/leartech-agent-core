// Package safefile writes files that hold credentials.
package safefile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// FileMode is the mode every credential file is created with.
	FileMode = 0o600
	// DirMode is the mode every credential directory is created with.
	DirMode = 0o700
)

// ErrPermissionsTooOpen reports a credential file that others can read.
var ErrPermissionsTooOpen = errors.New("safefile: the file is readable by others")

// Write creates path with mode 0600 and the given contents.
//
// Written to a sibling temp file and RENAMED into place. A truncating write
// over an existing file keeps that file's old mode, and a chmod afterwards
// leaves a window where a credential sits at a known path readable by every
// account on the machine. A rename has no such window, and it also makes the
// mode testable: the final mode IS the creation mode.
//
// proven-by: TestWrite_TightensAPathThatWasAlreadyTooOpen
func Write(path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), DirMode); err != nil {
		return fmt.Errorf("safefile: create the directory: %w", err)
	}

	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, FileMode) // #nosec G304 -- a caller-supplied path is this package's entire purpose; the mode, not the path, is what it guarantees.
	if err != nil {
		return fmt.Errorf("safefile: create: %w", err)
	}
	if _, err := f.Write(body); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("safefile: write: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("safefile: close: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("safefile: install: %w", err)
	}
	return nil
}

// Read returns the contents of path, refusing a file others can read.
//
// Refusing rather than tightening: a credential that has been world-readable
// should be treated as disclosed, and silently fixing the mode hides that it
// ever happened.
//
// proven-by: TestRead_DoesNotSilentlyFixThePermissions
func Read(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return nil, fmt.Errorf("%w: %s is %04o; chmod 600 it, and treat what it holds as disclosed",
			ErrPermissionsTooOpen, path, perm)
	}
	return os.ReadFile(path) // #nosec G304 -- as above; the path is the caller's and the permission check above is the guarantee.
}
