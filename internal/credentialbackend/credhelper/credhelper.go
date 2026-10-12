// Package credhelper runs the credential helper (docs/EXAMPLES.md, chapter
// 56, issues 8 to 12 and 14): a site's executable, named by creds.helper,
// that karvi runs once per invocation that resolves credentials, before any
// backend or fallback is asked, and whose output's NAME=value lines set
// values every credential reader finds through the resolver's one lookup, in
// front of the process environment. Its values reach the readers alone:
// they configure nothing, enter no child's environment, and reach the daemon
// only inside the sealed credentials. The daemon resolves no credential and
// never runs it; its boundary keeps this package out of its reach.
package credhelper

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/robert-patrick-texas/karvi/configschema"
	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/credentialbackend"
	"github.com/robert-patrick-texas/karvi/internal/credentialbackend/credfile"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// Settings are what a run of the helper reads from the invocation's
// configuration and the operator.
type Settings struct {
	// Path is creds.helper as loaded: a bare name, the file beside the
	// running executable, or an absolute path.
	Path string
	// Unavailable is creds.helper-unavailable: skip, warn, or fail.
	Unavailable string
	// Timeout is creds.helper-timeout; TimeoutText is the value as written,
	// which a failure's message names.
	Timeout     time.Duration
	TimeoutText string
	// AllowSymlink is security.allow-credential-symlinks.
	AllowSymlink bool
	// Admins is security.approved-admin-users.
	Admins []string
	// UID is the operator's, an owner the file may have.
	UID int
}

// FromConfig is the run's settings from the invocation's configuration.
func FromConfig(cfg configload.Snapshot, operator credentials.Operator) Settings {
	return Settings{
		Path: cfg.String("creds.helper"), Unavailable: cfg.String("creds.helper-unavailable"),
		Timeout: cfg.Duration("creds.helper-timeout"), TimeoutText: cfg.String("creds.helper-timeout"),
		AllowSymlink: cfg.Bool("security.allow-credential-symlinks"), Admins: cfg.Strings("security.approved-admin-users"),
		UID: operator.UID,
	}
}

// killDelay is how long a helper sent SIGTERM has before SIGKILL; drainDelay
// is how long its output is read after it exits, for a child it left that
// still holds its standard output. Tests shorten them.
var (
	killDelay  = 2 * time.Second
	drainDelay = 2 * time.Second
)

// Locate is the file a creds.helper value names: a bare name (no /, not
// beginning with ~) is that file in the running executable's directory, the
// executable a link names, as karvi-askpass is found, and never a name
// searched on PATH; any other value was made absolute when the
// configuration loaded and is returned as it is.
func Locate(value string) (string, error) {
	if !configschema.BareName("creds.helper", value) {
		return value, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return value, err
	}
	return filepath.Join(filepath.Dir(exe), value), nil
}

// Run runs the helper once and reads its output (issues 9 to 12). It
// returns the values, or nil and nil for a helper passed by: one that
// cannot be run (no file at its path, or one the operator cannot execute)
// under skip, silently, or under warn after a warning on warn; under fail
// that is credential_helper_unavailable. A file present but unsafe is a
// credential file code, a helper that exits non-zero or outlives the
// timeout credential_helper_failed, and an output the grammar refuses
// credential_helper_output_invalid, whatever creds.helper-unavailable says;
// a context ended during the run (Ctrl-C) is cancelled. debug receives the
// path and the names set, never a value.
func Run(ctx context.Context, s Settings, warn, debug func(string)) (*Values, error) {
	path, err := Locate(s.Path)
	if err != nil {
		return s.passBy(path, "the executable's directory: "+reason(err), warn, debug)
	}
	f, err := s.open(path)
	if err != nil {
		return s.passBy(path, reason(err), warn, debug)
	}
	defer f.Close()
	if err := s.check(path, f); err != nil {
		return nil, err
	}
	started := time.Now()
	out, n, err := s.run(ctx, f, path)
	var unrunnable *startError
	if errors.As(err, &unrunnable) {
		return s.passBy(path, "cannot run: "+reason(unrunnable.err), warn, debug)
	}
	if err != nil {
		return nil, err
	}
	defer clear(out)
	if n > maxOutput {
		return nil, errorcodes.Errorf("credential_helper_output_invalid", "credential helper %s output past 64 KiB", path)
	}
	set, err := parse(out[:n])
	if err != nil {
		return nil, errorcodes.Errorf("credential_helper_output_invalid", "credential helper %s output %v", path, err)
	}
	v := &Values{path: path, m: make(map[string][]byte, len(set))}
	for _, a := range set {
		v.names = append(v.names, a.name)
		v.m[a.name] = a.value
	}
	debug(fmt.Sprintf("credential helper ran path=%q names=%s elapsed=%s", path, v.nameList(), time.Since(started).Round(time.Millisecond)))
	return v, nil
}

// passBy is a helper that cannot be run, by creds.helper-unavailable.
func (s Settings) passBy(path, why string, warn, debug func(string)) (*Values, error) {
	err := errorcodes.Errorf("credential_helper_unavailable", "credential helper %s: %s", path, why)
	switch s.Unavailable {
	case "fail":
		return nil, err
	case "warn":
		warn(errorcodes.Message(err) + "; continuing without it")
	}
	debug(fmt.Sprintf("credential helper skipped path=%q: %s", path, why))
	return nil, nil
}

// reason is an error's cause as the system names it, without the path the
// message names already.
func reason(err error) string {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno.Error()
	}
	return err.Error()
}

// open opens the helper's path for the descriptor its checks read and its
// run executes, never following a symlink in the final component unless
// symlinks are allowed. O_PATH opens a file the operator may execute and
// not read; a symlink so opened is the link itself, refused by check.
func (s Settings) open(path string) (*os.File, error) {
	flags := unix.O_PATH
	if !s.AllowSymlink {
		flags |= syscall.O_NOFOLLOW
	}
	return os.OpenFile(path, flags, 0)
}

// check is issue 11's check set over the opened file: not a symlink unless
// allowed, a regular file, owned by the operator, root, or a name in
// security.approved-admin-users, and writable by neither its group nor
// others. Each failure is a credential file code naming the helper.
func (s Settings) check(path string, f *os.File) error {
	fi, err := f.Stat()
	if err != nil {
		return errorcodes.Errorf("credential_file_owner_uninspectable", "cannot inspect credential helper %s: %s", path, reason(err))
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return errorcodes.Errorf("credential_file_symlink_rejected", "credential helper %s is a symlink; set security.allow-credential-symlinks to allow it", path)
	}
	if !fi.Mode().IsRegular() {
		return errorcodes.Errorf("credential_file_not_regular", "credential helper %s not a regular file", path)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return errorcodes.Errorf("credential_file_owner_uninspectable", "cannot inspect the owner of credential helper %s", path)
	}
	if int(st.Uid) != s.UID && !(credfile.Rules{ApprovedAdminUsers: s.Admins}).ApprovedUID(int(st.Uid)) {
		return errorcodes.Errorf("credential_file_owner_mismatch", "credential helper %s owned by uid %d, not the operator, root, or a name in security.approved-admin-users", path, st.Uid)
	}
	if fi.Mode().Perm()&0o022 != 0 {
		return errorcodes.Errorf("credential_file_mode_unsafe", "credential helper %s writable by group or others; remediation: chmod go-w %s", path, path)
	}
	return nil
}

// startError is a helper the system would not start: the exec of the
// checked file failed (not executable by the operator, a format the system
// cannot run, an interpreter missing).
type startError struct{ err error }

func (e *startError) Error() string { return e.err.Error() }

// run executes the checked descriptor and reads its standard output (issue
// 12): through /proc/self/fd, so the file checked is the file run, a
// script's interpreter reading the script through the same descriptor; with
// karvi's own environment, unfiltered; no argument; standard input
// /dev/null; standard error discarded; karvi's working directory and
// process group, so the helper can ask on /dev/tty and Ctrl-C reaches it.
// Past the timeout, or the context ended, it and its descendants are sent
// SIGTERM and, killDelay on, SIGKILL (stop). It
// returns the output's buffer, cleared by the caller, and the bytes
// written, which exceed maxOutput when the output did; the bytes past it
// are read and discarded so the helper never blocks on a full pipe.
func (s Settings) run(ctx context.Context, f *os.File, path string) ([]byte, int, error) {
	null, err := os.Open(os.DevNull)
	if err != nil {
		return nil, 0, errorcodes.Errorf("credential_helper_failed", "credential helper %s: %s", path, reason(err))
	}
	defer null.Close()
	r, w, err := os.Pipe()
	if err != nil {
		return nil, 0, errorcodes.Errorf("credential_helper_failed", "credential helper %s: %s", path, reason(err))
	}
	defer r.Close()
	cmd := &exec.Cmd{Path: "/proc/self/fd/3", Args: []string{path}, Stdin: null, Stdout: w, ExtraFiles: []*os.File{f}}
	err = cmd.Start()
	w.Close()
	if err != nil {
		return nil, 0, &startError{err: err}
	}
	buf := make([]byte, maxOutput+1)
	n := 0
	read := make(chan struct{})
	go func() {
		defer close(read)
		var discard [512]byte
		defer clear(discard[:])
		for {
			var err error
			if n < len(buf) {
				var m int
				m, err = r.Read(buf[n:])
				n += m
			} else {
				_, err = r.Read(discard[:])
			}
			if err != nil {
				return
			}
		}
	}()
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	timer := time.NewTimer(s.Timeout)
	defer timer.Stop()
	var waitErr error
	timedOut := false
	select {
	case waitErr = <-exited:
	case <-timer.C:
		timedOut = true
		waitErr = stop(cmd, exited)
	case <-ctx.Done():
		waitErr = stop(cmd, exited)
	}
	_ = r.SetReadDeadline(time.Now().Add(drainDelay))
	<-read
	fail := func(err error) ([]byte, int, error) {
		clear(buf)
		return nil, 0, err
	}
	switch {
	case ctx.Err() != nil:
		return fail(errorcodes.Errorf("cancelled", "credential helper %s interrupted", path))
	case timedOut:
		return fail(errorcodes.Errorf("credential_helper_failed", "credential helper %s did not exit within %s; run it by hand to see why", path, s.TimeoutText))
	case waitErr != nil:
		return fail(errorcodes.Errorf("credential_helper_failed", "credential helper %s %s; run it by hand to see its message", path, exitText(waitErr)))
	}
	return buf, n, nil
}

// stop ends a helper still running, past its timeout or with the
// invocation cancelled, with its descendants (issue 18): the tree listed
// while the helper runs, SIGTERM to the helper and every descendant, and
// killDelay on SIGKILL to each still running, the tree listed again first
// while the helper runs. It returns as soon as every one has ended, with
// the wait's result.
func stop(cmd *exec.Cmd, exited <-chan error) error {
	pid := cmd.Process.Pid
	tree := descendants(pid)
	defer func() { release(tree) }()
	_ = cmd.Process.Signal(syscall.SIGTERM)
	signalAll(tree, syscall.SIGTERM)
	deadline := time.NewTimer(killDelay)
	defer deadline.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	var waitErr error
	ended := false
	for !ended || running(tree) {
		select {
		case waitErr = <-exited:
			ended = true
		case <-tick.C:
		case <-deadline.C:
			if !ended {
				tree = merge(tree, descendants(pid))
				_ = cmd.Process.Kill()
			}
			signalAll(tree, syscall.SIGKILL)
			if !ended {
				waitErr = <-exited
			}
			return waitErr
		}
	}
	return waitErr
}

// exitText says how a helper ended other than with status 0: its status,
// or the signal that ended it.
func exitText(err error) string {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		if ws, ok := exit.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return fmt.Sprintf("ended by signal %d (%s)", int(ws.Signal()), ws.Signal())
		}
		return fmt.Sprintf("exited %d", exit.ExitCode())
	}
	return reason(err)
}

// Values are the names a helper's output set and their values, held as
// secrets: each value its own copy, cleared by Destroy. Lookups are read
// alone while the values are held.
type Values struct {
	path  string
	names []string
	m     map[string][]byte
}

// LookupEnv is the value the helper set for name.
func (v *Values) LookupEnv(name string) (string, bool) {
	b, ok := v.m[name]
	if !ok {
		return "", false
	}
	return string(b), true
}

// Names are the names set, in the output's order.
func (v *Values) Names() []string { return append([]string(nil), v.names...) }

func (v *Values) nameList() string {
	if len(v.names) == 0 {
		return "none"
	}
	return strings.Join(v.names, ",")
}

// Destroy clears every value.
func (v *Values) Destroy() {
	for name, b := range v.m {
		clear(b)
		delete(v.m, name)
	}
	v.names = nil
}

// Holder holds a helper's values for an invocation, or for a stream's jobs
// until --restart (issue 8): Values runs the helper on its first call and
// gives every later call the values it read. A helper passed by, or one
// that failed, holds nothing, so the next call runs it again.
type Holder struct {
	mu     sync.Mutex
	values *Values
}

// Values are the held values, or the helper's, run now.
func (h *Holder) Values(ctx context.Context, s Settings, warn, debug func(string)) (*Values, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.values != nil {
		debug(fmt.Sprintf("credential helper held path=%q names=%s", h.values.path, h.values.nameList()))
		return h.values, nil
	}
	v, err := Run(ctx, s, warn, debug)
	if err != nil {
		return nil, err
	}
	h.values = v
	return v, nil
}

// Destroy clears the held values; the next Values runs the helper again.
func (h *Holder) Destroy() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.values != nil {
		h.values.Destroy()
		h.values = nil
	}
}

// Input is inner with the helper's values in front of its lookup: a name
// the helper set is read from v, silently overriding the process
// environment (issue 9); every other name, and every prompt, is inner's.
// A nil v is inner.
func Input(inner credentialbackend.InputProvider, v *Values) credentialbackend.InputProvider {
	if v == nil {
		return inner
	}
	return input{InputProvider: inner, values: v}
}

type input struct {
	credentialbackend.InputProvider
	values *Values
}

func (i input) LookupEnv(name string) (string, bool) {
	if value, ok := i.values.LookupEnv(name); ok {
		return value, true
	}
	return i.InputProvider.LookupEnv(name)
}
