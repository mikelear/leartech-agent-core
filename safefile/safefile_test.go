package safefile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWrite_CreatesTheFile0600InA0700Directory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "cred")

	if err := Write(path, []byte("secret")); err != nil {
		t.Fatalf("write: %v", err)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Errorf("file mode = %04o, want 0600", got)
	}
	di, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if got := di.Mode().Perm(); got != 0o700 {
		t.Errorf("directory mode = %04o, want 0700", got)
	}
}

// A truncating write over an existing file keeps that file's old mode. This
// is the case the rename exists for: the credential must end up 0600 even
// when the path was already 0644.
func TestWrite_TightensAPathThatWasAlreadyTooOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cred")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Anchor: prove the precondition, or this test passes against a file
	// that was never loose in the first place.
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o644 {
		t.Fatalf("precondition failed: the file is %04o, not 0644", fi.Mode().Perm())
	}

	if err := Write(path, []byte("new")); err != nil {
		t.Fatalf("write: %v", err)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Errorf("file mode = %04o after rewriting a 0644 file, want 0600", got)
	}
	body, _ := os.ReadFile(path)
	if string(body) != "new" {
		t.Errorf("contents = %q, want the new value", body)
	}
}

// No temp file is left behind. A leftover .tmp holding a credential at a
// predictable path is the same disclosure by another name.
func TestWrite_LeavesNoTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cred")
	if err := Write(path, []byte("secret")); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("a temporary file survived: %s", e.Name())
		}
	}
	if len(entries) != 1 {
		t.Errorf("directory holds %d entries, want just the credential", len(entries))
	}
}

// The matched pair on the mode: 0600 is read, 0644 is refused. Asserting
// only the refusal would pass for a Read that refused everything.
func TestRead_Accepts0600_RefusesWhatOthersCanRead(t *testing.T) {
	for _, tc := range []struct {
		mode   os.FileMode
		accept bool
	}{
		{0o600, true},
		{0o400, true},
		{0o640, false},
		{0o604, false},
		{0o644, false},
		{0o666, false},
	} {
		t.Run(tc.mode.String(), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "cred")
			if err := os.WriteFile(path, []byte("secret"), tc.mode); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, tc.mode); err != nil {
				t.Fatal(err)
			}

			body, err := Read(path)
			if tc.accept {
				if err != nil {
					t.Fatalf("%04o was refused: %v", tc.mode, err)
				}
				if string(body) != "secret" {
					t.Errorf("contents = %q", body)
				}
				return
			}
			if err == nil {
				t.Fatalf("%04o was accepted", tc.mode)
			}
			if !errors.Is(err, ErrPermissionsTooOpen) {
				t.Errorf("err = %v, want ErrPermissionsTooOpen", err)
			}
		})
	}
}

// Read refuses rather than silently tightening. A credential that has been
// world-readable should be treated as disclosed, and quietly fixing the mode
// hides that it ever was.
func TestRead_DoesNotSilentlyFixThePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cred")
	if err := os.WriteFile(path, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(path); err == nil {
		t.Fatal("a world-readable credential was read")
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o644 {
		t.Errorf("Read changed the mode to %04o — the disclosure is now invisible", fi.Mode().Perm())
	}
}

// The refusal has to say what to do. "permission denied" sends the reader
// to sudo; this needs to send them to chmod AND to rotating the credential.
func TestRead_TheRefusalSaysWhatToDoAboutIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cred")
	if err := os.WriteFile(path, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Read(path)
	if err == nil {
		t.Fatal("expected a refusal")
	}
	for _, want := range []string{"0644", "chmod", "disclosed"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// Errors never contain the file's contents. This package exists precisely
// because those contents are credentials.
func TestErrors_NeverContainTheContents(t *testing.T) {
	const secret = "sk-lt-LIVE-CREDENTIAL"
	path := filepath.Join(t.TempDir(), "cred")
	if err := os.WriteFile(path, []byte(secret), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(path); err == nil || strings.Contains(err.Error(), secret) {
		t.Errorf("err = %v", err)
	}
}

// A failed write leaves the existing file untouched.
//
// The failure is forced by pre-creating the temp path as a DIRECTORY, so
// the create fails with EISDIR. An earlier version made the parent
// read-only instead, which works as an ordinary user and NOT as root —
// and CI runs as root, so it passed here and failed there reporting "a
// write into a read-only directory reported success".
//
// A negative test that depends on the permissions of whoever runs it is
// not testing the code. This one holds for any user.
//
// The rename is what makes this hold: either the whole new content is in
// place at 0600, or the old file is as it was.
func TestWrite_AFailureLeavesTheExistingFileIntact(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cred")
	if err := Write(path, []byte("original")); err != nil {
		t.Fatal(err)
	}

	// Occupy the temp path with a directory: os.OpenFile cannot truncate it.
	if err := os.Mkdir(path+".tmp", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, []byte("replacement")); err == nil {
		t.Fatal("a write whose temp path was unusable reported success")
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the original file is gone: %v", err)
	}
	if string(body) != "original" {
		t.Errorf("contents = %q, want the original — a failed write destroyed the credential", body)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %04o after a failed write, want the original 0600", fi.Mode().Perm())
	}
}

// A path whose parent cannot be created is an error, not a silent no-op. A
// Write that returned nil without writing would have the caller believe a
// credential was stored, and it is shown only once.
func TestWrite_AnUncreatableParentIsAnError(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := Write(filepath.Join(blocker, "nested", "cred"), []byte("secret"))
	if err == nil {
		t.Fatal("writing beneath a regular file reported success")
	}
	if !strings.Contains(err.Error(), "safefile") {
		t.Errorf("error %q does not identify where it came from", err)
	}
}

// Overwriting replaces the contents rather than appending to them. An
// append would leave the previous token in the file, which is both a stale
// credential on disk and a session file that no longer parses.
func TestWrite_ReplacesRatherThanAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cred")
	if err := Write(path, []byte("first-value-longer")); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, []byte("second")); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "second" {
		t.Errorf("contents = %q, want only the new value", body)
	}
}
