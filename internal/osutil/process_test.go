package osutil

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestParseCPUList(t *testing.T) {
	for input, want := range map[string]int{"0": 1, "0-3": 4, "0-3,8,10-11": 7, "": 0} {
		if got := parseCPUList(input); got != want {
			t.Fatalf("parseCPUList(%q)=%d want %d", input, got, want)
		}
	}
}

func TestApplyGOMAXPROCSMatchesEffectiveCPU(t *testing.T) {
	p := ApplyGOMAXPROCS()
	if p.EffectiveCPUs < 1 || p.GOMAXPROCS != p.EffectiveCPUs {
		t.Fatalf("unexpected CPU profile: %+v", p)
	}
}

// TestProcessCommandName: the sweep's owner check reads argv[0]'s base name
// of a live process and nothing of a dead or absent one.
func TestProcessCommandName(t *testing.T) {
	if got := ProcessCommandName(os.Getpid()); got == "" || strings.Contains(got, "/") {
		t.Fatalf("own name %q", got)
	}
	if got := ProcessCommandName(0); got != "" {
		t.Fatalf("pid 0: %q", got)
	}
}

// TestProcessAliveOtherUser: init belongs to root, so an unprivileged
// signal check answers EPERM; the process is alive all the same, as another
// operator's lease holder is to the capacity ledger's reaper.
func TestProcessAliveOtherUser(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root signals every process")
	}
	start := ProcessStartIdentity(1)
	if start == "" {
		t.Skip("no /proc/1/stat")
	}
	if !ProcessAlive(1, start) {
		t.Fatal("pid 1 judged dead: EPERM from the signal check is a live process of another user")
	}
	if ProcessAlive(1, start+"0") {
		t.Fatal("pid 1 with another start identity judged alive")
	}
}

// TestStartTiedEndsWithParent: a parent killed outright takes the child it
// started with StartTied along, where an ordinary start would leave the child
// to init. The parent is this test binary again, run as the helper below; it
// prints its child's pid and waits to be killed.
func TestStartTiedEndsWithParent(t *testing.T) {
	parent := exec.Command(os.Args[0], "-test.run=^TestStartTiedHelper$")
	parent.Env = append(os.Environ(), "KARVI_START_TIED_HELPER=1")
	out, err := parent.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := parent.Start(); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		_ = parent.Process.Kill()
		t.Fatalf("the helper's child pid: %v", err)
	}
	child, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || !ProcessAlive(child, "") {
		_ = parent.Process.Kill()
		t.Fatalf("child %q not alive (%v)", line, err)
	}
	if err := parent.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = parent.Wait()
	deadline := time.Now().Add(5 * time.Second)
	for ProcessAlive(child, "") {
		if time.Now().After(deadline) {
			_ = syscall.Kill(child, syscall.SIGKILL)
			t.Fatalf("child %d outlived its killed parent", child)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestStartTiedHelper is the parent of TestStartTiedEndsWithParent, and
// nothing when run on its own.
func TestStartTiedHelper(t *testing.T) {
	if os.Getenv("KARVI_START_TIED_HELPER") != "1" {
		t.Skip("the helper of TestStartTiedEndsWithParent")
	}
	// Started from a goroutine that then ends, as a caller's would: the
	// child must live on with this process, not with that goroutine.
	result := make(chan error, 1)
	cmd := exec.Command("sleep", "60")
	go func() {
		_, err := StartTied(cmd, nil)
		result <- err
	}()
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	fmt.Println(cmd.Process.Pid)
	time.Sleep(time.Minute)
}

// TestStartTiedWaits: beforeWait runs before the wait, the channel carries
// the exit, and a command that cannot start is the start's error.
func TestStartTiedWaits(t *testing.T) {
	cmd := exec.Command("sh", "-c", "echo out; exit 3")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var read []byte
	done, err := StartTied(cmd, func() { read, _ = io.ReadAll(out) })
	if err != nil {
		t.Fatal(err)
	}
	err = <-done
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 3 || string(read) != "out\n" {
		t.Fatalf("err=%v read=%q", err, read)
	}
	if cmd.SysProcAttr == nil || cmd.SysProcAttr.Pdeathsig != syscall.SIGTERM {
		t.Fatalf("SysProcAttr %+v", cmd.SysProcAttr)
	}
	if _, err := StartTied(exec.Command(filepath.Join(t.TempDir(), "absent")), nil); err == nil {
		t.Fatal("an absent executable started")
	}
}
