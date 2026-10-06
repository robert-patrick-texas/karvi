// Package credfile owns the file rules every credential file backend
// shares: the declared scope,
// the ~/ expansion under user scope, the check set per scope, the
// availability classes, and the cache that holds a backend's loaded records
// or its failure for the resolver's lifetime. A backend keeps its own
// parser and calls Rules.Open for every file it reads, a root file and an
// included file alike.
//
// The checks run on the opened descriptor, not on the path:
// the file inspected is the file read, so a path swapped between the check
// and the read cannot pass one file's checks for another's content.
package credfile

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
)

// Scope is a file backend's declared scope. The scope, never the path,
// selects the check set.
type Scope string

const (
	ScopeUser   Scope = "user"
	ScopeShared Scope = "shared"
)

// Rules is what a file backend declares about its files. One Rules value
// serves the root file and every file it includes: an included file
// inherits the scope.
type Rules struct {
	BackendName        string   // named in every message
	Scope              Scope    // user when empty
	Required           bool     // a shared file is always required
	Home               string   // the operator's home, for ~/ under user scope
	UID                int      // the operator's uid, the owner a user file must have
	SharedGroup        string   // security.shared-group; unchecked when empty
	ApprovedAdminUsers []string // security.approved-admin-users; root is always approved
	AllowSymlink       bool     // security.allow-credential-symlinks
	ReadOnlyAllowed    bool     // a user file may also be 0400 (an operator's key)
}

// EffectiveScope is the declared scope, user when none is declared.
func (r Rules) EffectiveScope() Scope {
	if r.Scope == ScopeShared {
		return ScopeShared
	}
	return ScopeUser
}

// ExpandPath resolves a user-scope path by osutil.ResolvePath: ~ and ~/ the
// operator's home, ~user refused, a relative path from the working
// directory. A shared file has no home to resolve against (configuration
// validation refuses a relative shared path), so its path is returned as
// written.
func (r Rules) ExpandPath(path string) (string, error) {
	if r.EffectiveScope() != ScopeUser {
		return path, nil
	}
	return osutil.ResolvePath(path, r.Home)
}

// UnavailableError marks a root file that is absent, untraversable, or
// unreadable; Classify turns it into a silent NotFound or
// credential_file_unavailable by scope and required.
type UnavailableError struct{ Err error }

func (e *UnavailableError) Error() string { return e.Err.Error() }
func (e *UnavailableError) Unwrap() error { return e.Err }

// IsUnavailable reports whether err says the path is absent, untraversable,
// or unreadable, as against present and failing a check.
func IsUnavailable(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) || errors.Is(err, syscall.ENOTDIR)
}

// Open opens path for reading and checks the opened descriptor against the
// scope's rules. It returns the file, positioned at its start, and the
// canonical path the backend names in evidence and messages. The caller
// closes the file.
//
// The open never follows a symlink in the final component unless
// AllowSymlink is set, and never blocks on a FIFO or a device: O_NONBLOCK
// has no effect on a regular file, and anything else is refused by the
// regular-file check before a byte is read.
//
// An error for which IsUnavailable is true means the file could not be
// reached at all; every other error carries one of the credential_file_*
// codes and fails whatever the scope or required.
func (r Rules) Open(path string) (*os.File, string, error) {
	flags := os.O_RDONLY | syscall.O_NONBLOCK
	if !r.AllowSymlink {
		flags |= syscall.O_NOFOLLOW
	}
	f, err := os.OpenFile(path, flags, 0)
	if err != nil {
		return nil, "", r.openFailure(path, err)
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, "", err
	}
	if err := r.check(path, fi); err != nil {
		f.Close()
		return nil, "", err
	}
	canonical, err := r.canonical(path)
	if err != nil {
		f.Close()
		return nil, "", err
	}
	return f, canonical, nil
}

// openFailure explains a failed open. The platform's errno for O_NOFOLLOW
// over a symlink differs between systems, so the path is inspected instead
// of the errno. A present file the operator cannot read (mode 0000, say)
// still fails its check by name rather than passing as unavailable: a file
// that will never be read has no check-then-read gap, so inspecting the
// path is sound here, and the unsafe file is reported even for an optional
// backend.
func (r Rules) openFailure(path string, openErr error) error {
	li, err := os.Lstat(path)
	if err != nil {
		return openErr
	}
	if li.Mode()&os.ModeSymlink != 0 {
		if !r.AllowSymlink {
			return errorcodes.Errorf("credential_file_symlink_rejected", "credential backend %s: credential file %s is a symlink; set security.allow-credential-symlinks to allow it", r.BackendName, path)
		}
		if li, err = os.Stat(path); err != nil {
			return openErr
		}
	}
	if errors.Is(openErr, fs.ErrPermission) {
		if err := r.check(path, li); err != nil {
			return err
		}
	}
	return openErr
}

// canonical is the absolute path, with a symlink in the final component
// resolved when symlinks are allowed. It names the file; it is never
// reopened, so it takes no part in the checks.
func (r Rules) canonical(path string) (string, error) {
	if r.AllowSymlink {
		if li, err := os.Lstat(path); err == nil && li.Mode()&os.ModeSymlink != 0 {
			if resolved, err := filepath.EvalSymlinks(path); err == nil {
				path = resolved
			}
		}
	}
	return filepath.Abs(path)
}

// check is the check set over one file's metadata: a user file is a regular
// file owned by the operator with mode 0600 (or 0400 where ReadOnlyAllowed); a shared file is a regular file
// with mode 0640, owned by root or an approved administrator, in the shared
// group when one is configured. A message names the backend and the path and
// never a value.
func (r Rules) check(path string, fi fs.FileInfo) error {
	kind := string(r.EffectiveScope())
	if !fi.Mode().IsRegular() {
		return errorcodes.Errorf("credential_file_not_regular", "credential backend %s: %s credential file %s is not a regular file", r.BackendName, kind, path)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return errorcodes.Errorf("credential_file_owner_uninspectable", "credential backend %s: cannot inspect the ownership of %s credential file %s", r.BackendName, kind, path)
	}
	if r.EffectiveScope() == ScopeUser {
		if int(st.Uid) != r.UID {
			return errorcodes.Errorf("credential_file_owner_mismatch", "credential backend %s: user credential file %s must be owned by uid %d", r.BackendName, path, r.UID)
		}
		if perm := fi.Mode().Perm(); perm != 0o600 && !(r.ReadOnlyAllowed && perm == 0o400) {
			return errorcodes.Errorf("credential_file_mode_unsafe", "credential backend %s: user credential file %s mode is %04o; remediation: chmod 600 %s", r.BackendName, path, fi.Mode().Perm(), path)
		}
		return nil
	}
	if fi.Mode().Perm() != 0o640 {
		return errorcodes.Errorf("credential_file_shared_mode_invalid", "credential backend %s: shared credential file %s mode is %04o and must be 0640", r.BackendName, path, fi.Mode().Perm())
	}
	if !r.approvedUID(int(st.Uid)) {
		return errorcodes.Errorf("credential_file_shared_owner_unapproved", "credential backend %s: shared credential file %s owner uid %d is not root or in security.approved-admin-users", r.BackendName, path, st.Uid)
	}
	if r.SharedGroup != "" {
		g, err := user.LookupGroup(r.SharedGroup)
		if err != nil {
			return errorcodes.Errorf("credential_file_shared_group_unknown", "credential backend %s: shared group %s: %w", r.BackendName, r.SharedGroup, err)
		}
		gid, _ := strconv.Atoi(g.Gid)
		if int(st.Gid) != gid {
			return errorcodes.Errorf("credential_file_shared_group_mismatch", "credential backend %s: shared credential file %s group %d is not %s", r.BackendName, path, st.Gid, r.SharedGroup)
		}
	}
	return nil
}

func (r Rules) approvedUID(uid int) bool {
	if uid == 0 {
		return true
	}
	for _, name := range r.ApprovedAdminUsers {
		u, err := user.Lookup(name)
		if err == nil {
			if n, _ := strconv.Atoi(u.Uid); n == uid {
				return true
			}
		}
	}
	return false
}

// Classify classifies a failed load of the root file at
// path. An unavailable optional user file is a silent NotFound; unavailable
// otherwise is credential_file_unavailable naming the backend, the scope,
// and the path; every other failure keeps its own code, and an error with
// no code takes malformedCode, the backend's own parse-failure code.
func (r Rules) Classify(path string, err error, malformedCode string) credentials.BackendResult {
	var u *UnavailableError
	if errors.As(err, &u) {
		if r.EffectiveScope() == ScopeUser && !r.Required {
			return credentials.BackendResult{Outcome: credentials.NotFound}
		}
		return credentials.BackendResult{Outcome: credentials.Unavailable, ErrorCode: "credential_file_unavailable", Message: fmt.Sprintf("credential backend %s: %s-scope file %s is unavailable: %v", r.BackendName, r.EffectiveScope(), path, u.Err)}
	}
	code := errorcodes.Of(err)
	if code == "" {
		code = malformedCode
	}
	return credentials.BackendResult{Outcome: credentials.Malformed, ErrorCode: code, Message: err.Error()}
}

// Cache holds one backend's loaded records, or its failure, for the
// resolver's lifetime: the file is read on the
// first resolution that reaches the backend and never again, so every
// device of a run sees the same file. The zero value is ready to use.
type Cache[T any] struct {
	mu      sync.Mutex
	loaded  bool
	value   T
	failure *credentials.BackendResult
}

// Load returns the cached value or failure, calling read on first use and
// classify on its error. A cancelled context is reported and not cached,
// so a later resolution under a live context reads the file.
func (c *Cache[T]) Load(ctx context.Context, read func() (T, error), classify func(error) credentials.BackendResult) (T, *credentials.BackendResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.loaded {
		return c.value, c.failure
	}
	value, err := read()
	if err != nil {
		var zero T
		if ctx.Err() != nil {
			return zero, &credentials.BackendResult{Outcome: credentials.Unavailable, ErrorCode: "credential_backend_error", Message: err.Error(), Retryable: true}
		}
		result := classify(err)
		c.loaded, c.failure = true, &result
		return zero, c.failure
	}
	c.loaded, c.value = true, value
	return value, nil
}
