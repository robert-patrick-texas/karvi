package credhelper

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/credentialbackend"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// settings are a run's settings for path under fail, the operator this
// test's user.
func settings(path string) Settings {
	return Settings{Path: path, Unavailable: "fail", Timeout: 10 * time.Second, TimeoutText: "10s", UID: os.Getuid()}
}

// helper writes a #!/bin/sh helper of body at dir/name with mode.
func helper(t *testing.T, dir, name, body string, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
	return p
}

// recorder collects what Run gives warn and debug.
type recorder struct{ warnings, debug []string }

func (r *recorder) warn(s string)   { r.warnings = append(r.warnings, s) }
func (r *recorder) debugf(s string) { r.debug = append(r.debug, s) }

func run(t *testing.T, s Settings) (*Values, *recorder, error) {
	t.Helper()
	r := &recorder{}
	v, err := Run(context.Background(), s, r.warn, r.debugf)
	return v, r, err
}

// TestLocate is issue 9: a bare name is the file beside the running
// executable, never a name searched on PATH; any other value as loaded.
func TestLocate(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for value, want := range map[string]string{
		"karvi-credh":                filepath.Join(filepath.Dir(exe), "karvi-credh"),
		"credh":                      filepath.Join(filepath.Dir(exe), "credh"),
		"/usr/local/lib/karvi/credh": "/usr/local/lib/karvi/credh",
	} {
		if got, err := Locate(value); err != nil || got != want {
			t.Errorf("%s: %q %v, want %q", value, got, err, want)
		}
	}
}

// TestRunValues: the output's values held, in order, the debug stream
// naming the path and the names and never a value; the process
// environment unchanged.
func TestRunValues(t *testing.T) {
	t.Setenv("NETPASS", "from-the-environment")
	p := helper(t, t.TempDir(), "credh", "echo 'export NETUSER=netops NETPASS=\"S3 cr3t\"'\necho '# done'\n", 0o755)
	v, r, err := run(t, settings(p))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(v.Names(), ","); got != "NETUSER,NETPASS" {
		t.Errorf("names %s", got)
	}
	if got, ok := v.LookupEnv("NETPASS"); !ok || got != "S3 cr3t" {
		t.Errorf("NETPASS %q %v", got, ok)
	}
	if _, ok := v.LookupEnv("NETENABLE"); ok {
		t.Error("NETENABLE set")
	}
	if os.Getenv("NETPASS") != "from-the-environment" || os.Getenv("NETUSER") != "" {
		t.Error("the helper's values reached the process environment")
	}
	if len(r.debug) != 1 || !strings.Contains(r.debug[0], fmt.Sprintf("credential helper ran path=%q names=NETUSER,NETPASS", p)) || strings.Contains(r.debug[0], "S3") {
		t.Errorf("debug %q", r.debug)
	}
	held := v.m["NETPASS"]
	v.Destroy()
	if strings.Trim(string(held), "\x00") != "" {
		t.Errorf("Destroy left %q", held)
	}
	if _, ok := v.LookupEnv("NETPASS"); ok {
		t.Error("NETPASS after Destroy")
	}
}

// TestRunChecks is issue 11: the opened file's checks, each a credential
// file code naming the helper, under any creds.helper-unavailable.
func TestRunChecks(t *testing.T) {
	dir := t.TempDir()
	good := helper(t, dir, "good", "echo A=1\n", 0o755)
	link := filepath.Join(dir, "link")
	if err := os.Symlink(good, link); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	me, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	other := os.Getuid() + 1
	for _, tc := range []struct {
		name   string
		s      Settings
		code   string
		phrase string
	}{
		{"symlink", Settings{Path: link}, "credential_file_symlink_rejected", "credential helper " + link + " is a symlink; set security.allow-credential-symlinks"},
		{"symlink allowed", Settings{Path: link, AllowSymlink: true}, "", ""},
		{"directory", Settings{Path: dir}, "credential_file_not_regular", "credential helper " + dir + " not a regular file"},
		{"fifo", Settings{Path: fifo}, "credential_file_not_regular", "not a regular file"},
		{"owner", Settings{Path: good, UID: other}, "credential_file_owner_mismatch", fmt.Sprintf("credential helper %s owned by uid %d, not the operator, root, or a name in security.approved-admin-users", good, os.Getuid())},
		{"owner approved", Settings{Path: good, UID: other, Admins: []string{me.Username}}, "", ""},
	} {
		s := tc.s
		if s.UID == 0 {
			s.UID = os.Getuid()
		}
		s.Unavailable, s.Timeout, s.TimeoutText = "skip", 10*time.Second, "10s"
		v, _, err := run(t, s)
		if tc.code == "" {
			if err != nil || v == nil {
				t.Errorf("%s: %v %v", tc.name, v, err)
			}
			continue
		}
		if errorcodes.Of(err) != tc.code || !strings.Contains(err.Error(), tc.phrase) {
			t.Errorf("%s: %v, want %s %q", tc.name, err, tc.code, tc.phrase)
		}
	}
	for _, mode := range []os.FileMode{0o775, 0o757, 0o722, 0o777} {
		p := helper(t, dir, fmt.Sprintf("m%o", mode), "echo A=1\n", mode)
		_, _, err := run(t, Settings{Path: p, Unavailable: "skip", UID: os.Getuid(), Timeout: time.Second})
		want := fmt.Sprintf("credential helper %s writable by group or others; remediation: chmod go-w %s", p, p)
		if errorcodes.Of(err) != "credential_file_mode_unsafe" || !strings.HasSuffix(err.Error(), want) {
			t.Errorf("%o: %v", mode, err)
		}
	}
	for _, mode := range []os.FileMode{0o700, 0o750, 0o755, 0o500} {
		p := helper(t, dir, fmt.Sprintf("m%o", mode), "echo A=1\n", mode)
		if v, _, err := run(t, settings(p)); err != nil || v == nil {
			t.Errorf("%o: %v %v", mode, v, err)
		}
	}
}

// TestRunUnavailable is issue 9: a helper that cannot be run passed by
// silently under skip, after a warning under warn, refused under fail.
func TestRunUnavailable(t *testing.T) {
	dir := t.TempDir()
	notfile := helper(t, dir, "plain", "echo A=1\n", 0o755)
	for _, tc := range []struct {
		path, why string
	}{
		{filepath.Join(dir, "absent"), "no such file or directory"},
		{filepath.Join(notfile, "x"), "not a directory"},
		{helper(t, dir, "noexec", "echo A=1\n", 0o644), "cannot run: permission denied"},
	} {
		s := settings(tc.path)
		want := fmt.Sprintf("credential helper %s: %s", tc.path, tc.why)
		_, _, err := run(t, s)
		if errorcodes.Of(err) != "credential_helper_unavailable" || !strings.HasSuffix(err.Error(), want) {
			t.Errorf("fail: %v, want %q", err, want)
		}
		s.Unavailable = "warn"
		v, r, err := run(t, s)
		if v != nil || err != nil || len(r.warnings) != 1 || r.warnings[0] != "credential_helper_unavailable: "+want+"; continuing without it" {
			t.Errorf("warn: %v %v %q", v, err, r.warnings)
		}
		s.Unavailable = "skip"
		v, r, err = run(t, s)
		if v != nil || err != nil || len(r.warnings) != 0 || len(r.debug) != 1 || !strings.Contains(r.debug[0], "credential helper skipped") {
			t.Errorf("skip: %v %v %q %q", v, err, r.warnings, r.debug)
		}
	}
	noShebang := filepath.Join(dir, "noshebang")
	if err := os.WriteFile(noShebang, []byte("echo A=1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	noInterp := filepath.Join(dir, "nointerpreter")
	if err := os.WriteFile(noInterp, []byte("#!/nonexistent/sh\necho A=1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	for path, why := range map[string]string{noShebang: "cannot run: exec format error", noInterp: "cannot run: no such file or directory"} {
		_, _, err := run(t, settings(path))
		if errorcodes.Of(err) != "credential_helper_unavailable" || !strings.HasSuffix(err.Error(), why) {
			t.Errorf("%s: %v", path, err)
		}
	}
}

// TestRunFailed is issue 12: a non-zero exit, a signal, or the timeout is
// credential_helper_failed whatever creds.helper-unavailable says, the
// output never read.
func TestRunFailed(t *testing.T) {
	saved := killDelay
	killDelay = 300 * time.Millisecond
	t.Cleanup(func() { killDelay = saved })
	dir := t.TempDir()
	for _, tc := range []struct {
		body, want string
		timeout    time.Duration
	}{
		{"echo NETPASS=s3cretX\nexit 3\n", "exited 3; run it by hand to see its message", 0},
		{"echo NETPASS=s3cretX\nkill -9 $$\n", "ended by signal 9 (killed); run it by hand to see its message", 0},
		{"exec sleep 30\n", "did not exit within 400ms; run it by hand to see why", 400 * time.Millisecond},
		{"trap '' TERM\necho NETPASS=s3cretX\nwhile :; do sleep 0.05; done\n", "did not exit within 400ms; run it by hand to see why", 400 * time.Millisecond},
	} {
		p := helper(t, dir, "h", tc.body, 0o755)
		s := settings(p)
		s.Unavailable = "skip"
		if tc.timeout > 0 {
			s.Timeout, s.TimeoutText = tc.timeout, tc.timeout.String()
		}
		started := time.Now()
		v, _, err := run(t, s)
		if v != nil || errorcodes.Of(err) != "credential_helper_failed" || err.Error() != "credential_helper_failed: credential helper "+p+" "+tc.want {
			t.Errorf("%q: %v %v", tc.body, v, err)
		}
		if strings.Contains(fmt.Sprint(err), "s3cretX") {
			t.Errorf("%q: the output in %v", tc.body, err)
		}
		if elapsed := time.Since(started); elapsed > 5*time.Second {
			t.Errorf("%q: %s", tc.body, elapsed)
		}
	}
}

// TestRunOutputInvalid is issue 10's refusal in a run: the path, the line,
// and the reason, nothing of the line; an output past 64 KiB read to its end
// and refused.
func TestRunOutputInvalid(t *testing.T) {
	dir := t.TempDir()
	p := helper(t, dir, "h", "echo A=1\necho 'NETPASS = s3cretX'\n", 0o755)
	_, _, err := run(t, settings(p))
	if errorcodes.Of(err) != "credential_helper_output_invalid" || err.Error() != "credential_helper_output_invalid: credential helper "+p+" output line 2: a blank before =" {
		t.Errorf("%v", err)
	}
	p = helper(t, dir, "big", "i=0\nwhile [ $i -lt 9000 ]; do echo '# filler'; i=$((i+1)); done\n", 0o755)
	_, _, err = run(t, settings(p))
	if errorcodes.Of(err) != "credential_helper_output_invalid" || err.Error() != "credential_helper_output_invalid: credential helper "+p+" output past 64 KiB" {
		t.Errorf("%v", err)
	}
	p = helper(t, dir, "empty", "exit 0\n", 0o755)
	if v, _, err := run(t, settings(p)); err != nil || v == nil || len(v.Names()) != 0 {
		t.Errorf("empty: %v %v", v, err)
	}
}

// TestRunEnvironment is issue 12: karvi's environment unfiltered, no
// argument, standard input and standard error /dev/null, karvi's working
// directory and process group, and the checked descriptor run.
func TestRunEnvironment(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("KARVI_CREDH_TEST", "seen")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/run/bus")
	body := `echo "+ trace" >&2
printf 'ARGS=%s IN=%s ERR=%s ZERO=%s\n' "$#" "$(readlink /proc/self/fd/0)" "$(readlink /proc/self/fd/2)" "$0"
printf 'DIR=%s PG=%s\n' "$(pwd)" "$(ps -o pgid= -p $$ | tr -d ' ')"
printf 'SEEN=%s BUS=%s\n' "$KARVI_CREDH_TEST" "$DBUS_SESSION_BUS_ADDRESS"
`
	p := helper(t, dir, "h", body, 0o755)
	v, _, err := run(t, settings(p))
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{
		"ARGS": "0", "IN": "/dev/null", "ERR": "/dev/null", "ZERO": "/proc/self/fd/3",
		"DIR": dir, "PG": strconv.Itoa(syscall.Getpgrp()),
		"SEEN": "seen", "BUS": "unix:path=/run/bus",
	} {
		if got, _ := v.LookupEnv(name); got != want {
			t.Errorf("%s=%q, want %q", name, got, want)
		}
	}
}

// TestRunExecuteOnly: a binary the operator may execute and not read runs,
// opened for its descriptor alone.
func TestRunExecuteOnly(t *testing.T) {
	src, err := exec.LookPath("true")
	if err != nil {
		t.Skip("no true")
	}
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "credh")
	if err := os.WriteFile(p, b, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o111); err != nil {
		t.Fatal(err)
	}
	if v, _, err := run(t, settings(p)); err != nil || v == nil {
		t.Errorf("%v %v", v, err)
	}
}

// TestRunCancelled: a context ended during the run (Ctrl-C) is cancelled,
// the helper ended.
func TestRunCancelled(t *testing.T) {
	p := helper(t, t.TempDir(), "h", "exec sleep 30\n", 0o755)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := Run(ctx, settings(p), func(string) {}, func(string) {})
	if errorcodes.Of(err) != "cancelled" || err.Error() != "cancelled: credential helper "+p+" interrupted" {
		t.Errorf("%v", err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Errorf("%s", elapsed)
	}
}

// TestHolder is issue 8: the helper run once and its values held; a
// failure holds nothing, the next call running it again; Destroy clears
// the values, the next call running it again.
func TestHolder(t *testing.T) {
	dir := t.TempDir()
	count := filepath.Join(dir, "count")
	p := helper(t, dir, "h", fmt.Sprintf("echo x >>%s\nn=$(wc -l <%s)\n[ \"$n\" -ge 2 ] || exit 1\necho A=$n\n", count, count), 0o755)
	runs := func() int {
		b, _ := os.ReadFile(count)
		return strings.Count(string(b), "x")
	}
	var h Holder
	nop := func(string) {}
	if _, err := h.Values(context.Background(), settings(p), nop, nop); errorcodes.Of(err) != "credential_helper_failed" {
		t.Fatalf("first: %v", err)
	}
	for i := 0; i < 3; i++ {
		v, err := h.Values(context.Background(), settings(p), nop, nop)
		if a, _ := v.LookupEnv("A"); err != nil || a != "2" {
			t.Errorf("held %d: %q %v", i, a, err)
		}
	}
	if runs() != 2 {
		t.Errorf("runs %d, want 2", runs())
	}
	h.Destroy()
	v, err := h.Values(context.Background(), settings(p), nop, nop)
	if a, _ := v.LookupEnv("A"); err != nil || a != "3" || runs() != 3 {
		t.Errorf("after Destroy: %q %v, runs %d", a, err, runs())
	}
	absent := settings(filepath.Join(dir, "absent"))
	absent.Unavailable = "skip"
	var none Holder
	if v, err := none.Values(context.Background(), absent, nop, nop); v != nil || err != nil {
		t.Errorf("passed by: %v %v", v, err)
	}
}

type fakeInput map[string]string

func (f fakeInput) LookupEnv(name string) (string, bool) { v, ok := f[name]; return v, ok }
func (fakeInput) Prompt(context.Context, credentialbackend.PromptRequest) (string, error) {
	return "typed", nil
}

// TestInput is issue 9: a name the helper set overrides the environment's;
// every other name and every prompt is the inner provider's.
func TestInput(t *testing.T) {
	v := &Values{m: map[string][]byte{"NETPASS": []byte("helper"), "EMPTY": {}}}
	in := Input(fakeInput{"NETPASS": "exported", "NETUSER": "netops", "EMPTY": "exported"}, v)
	for name, want := range map[string]string{"NETPASS": "helper", "NETUSER": "netops", "EMPTY": ""} {
		if got, ok := in.LookupEnv(name); !ok || got != want {
			t.Errorf("%s: %q %v, want %q", name, got, ok, want)
		}
	}
	if _, ok := in.LookupEnv("NETENABLE"); ok {
		t.Error("NETENABLE")
	}
	if got, _ := in.Prompt(context.Background(), credentialbackend.PromptRequest{}); got != "typed" {
		t.Errorf("prompt %q", got)
	}
	inner := fakeInput{}
	if Input(inner, nil) == nil {
		t.Error("nil values")
	}
}

// pidIn is the pid a helper wrote to path.
func pidIn(t *testing.T, path string) int {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	return pid
}

// ends reports whether pid ends within two seconds.
func ends(pid int) bool {
	for i := 0; i < 100; i++ {
		if st, ok := readStat(pid); !ok || st.state == 'Z' || st.state == 'X' {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// TestRunEndsDescendants is issue 18: past the timeout, or with the
// context ended, the helper and every process it started that is still its
// descendant end with it: a fetch hung in a child (SIGTERM), a child
// ignoring SIGTERM (SIGKILL, killDelay on), a child in the background.
func TestRunEndsDescendants(t *testing.T) {
	saved := killDelay
	killDelay = 300 * time.Millisecond
	t.Cleanup(func() { killDelay = saved })
	dir := t.TempDir()
	pf := filepath.Join(dir, "pid")
	child := fmt.Sprintf(`sh -c 'echo $$ >%s; exec sleep 600'`, pf)
	for _, tc := range []struct {
		name, body, code string
		cancel           bool
	}{
		{"a hung fetch", "v=$(" + child + ")\necho A=$v\n", "credential_helper_failed", false},
		{"a child ignoring SIGTERM", "trap '' TERM\n" + child + "\n", "credential_helper_failed", false},
		{"a child in the background", child + " >/dev/null &\nexec sleep 600\n", "credential_helper_failed", false},
		{"a hung fetch, cancelled", "v=$(" + child + ")\necho A=$v\n", "cancelled", true},
	} {
		os.Remove(pf)
		s := settings(helper(t, dir, "h", tc.body, 0o755))
		s.Timeout = 400 * time.Millisecond
		ctx, cancel := context.WithCancel(context.Background())
		if tc.cancel {
			s.Timeout = time.Minute
			time.AfterFunc(400*time.Millisecond, cancel)
		}
		started := time.Now()
		_, err := Run(ctx, s, func(string) {}, func(string) {})
		cancel()
		if errorcodes.Of(err) != tc.code {
			t.Errorf("%s: %v", tc.name, err)
		}
		if elapsed := time.Since(started); elapsed > 2*time.Second {
			t.Errorf("%s: %s", tc.name, elapsed)
		}
		if pid := pidIn(t, pf); !ends(pid) {
			syscall.Kill(pid, syscall.SIGKILL)
			t.Errorf("%s: descendant %d still running", tc.name, pid)
		}
	}
}

// TestRunLeavesWhatLeft: a process that left the helper's tree before the
// timeout, its parent ended before it (a daemon's double fork), is left
// running.
func TestRunLeavesWhatLeft(t *testing.T) {
	dir := t.TempDir()
	pf := filepath.Join(dir, "pid")
	s := settings(helper(t, dir, "h", fmt.Sprintf("(sh -c 'echo $$ >%s; exec sleep 600' >/dev/null 2>&1 &)\nexec sleep 600\n", pf), 0o755))
	s.Timeout = 400 * time.Millisecond
	if _, err := Run(context.Background(), s, func(string) {}, func(string) {}); errorcodes.Of(err) != "credential_helper_failed" {
		t.Fatalf("%v", err)
	}
	pid := pidIn(t, pf)
	defer syscall.Kill(pid, syscall.SIGKILL)
	if ends(pid) {
		t.Errorf("the process that left, %d, ended", pid)
	}
}

// TestDescendants: the tree below a process, each member held by a pidfd;
// the process itself and others outside it are not members.
func TestDescendants(t *testing.T) {
	cmd := exec.Command("sh", "-c", "sleep 30 & sh -c 'sleep 30' & wait")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	var tree []member
	for i := 0; i < 100 && len(tree) < 3; i++ {
		release(tree)
		time.Sleep(20 * time.Millisecond)
		tree = descendants(cmd.Process.Pid)
	}
	defer release(tree)
	if len(tree) != 3 {
		t.Fatalf("%d members, want 3 (two sleeps and a shell)", len(tree))
	}
	for _, m := range tree {
		if m.pid == cmd.Process.Pid || m.pid == os.Getpid() || m.fd < 0 {
			t.Errorf("member %+v", m)
		}
	}
	if !running(tree) {
		t.Error("not running")
	}
	signalAll(tree, syscall.SIGKILL)
	for _, m := range tree {
		if !ends(m.pid) {
			t.Errorf("%d still running", m.pid)
		}
	}
}
