package daemon

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/askpass"
	"github.com/robert-patrick-texas/karvi/internal/credentialbackend"
	"github.com/robert-patrick-texas/karvi/internal/osutil/osutiltest"
)

// TestMain lets the test binary play two roles for the Go fake device:
// linked as fake-ssh it is the device, which
// authenticates through askpass and records each command beside the
// SHA-256 of the answer; linked as karvi-askpass it is the real helper.
func TestMain(m *testing.M) {
	switch filepath.Base(os.Args[0]) {
	case "fake-ssh":
		os.Exit(fakeSSHMain())
	case "karvi-askpass":
		os.Exit(askpass.HelperMain(os.Args[1:]))
	}
	// Off the host (osutiltest.Isolate): the daemon's tests draft plans
	// through the planner, so without this the jobs of a run land in the
	// host's shared job tree once `sudo karvi setup shared` has made it,
	// and their scoreboards in the
	// host's shared directory.
	done := osutiltest.Isolate()
	code := m.Run()
	done()
	os.Exit(code)
}

func linkTestBinary(t *testing.T, path string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(exe, path); err != nil {
		t.Fatal(err)
	}
}

// fakeSSHMain is the device: one interactive shell per session, which
// authenticates through askpass, prints
// the prompt, and answers every line until "exit". KARVI_TEST_FAKE_DIR
// (allow-listed into the child environment by the test) names a directory
// holding the log and the barrier: a "show a" command waits for the
// "show b" command to arrive, and vice versa, so two jobs are proven to
// run at the same time.
func fakeSSHMain() int {
	args := os.Args[1:]
	for _, a := range args {
		if a == "-O" {
			return 1
		}
	}
	if len(args) == 0 {
		return 2
	}
	out, err := exec.Command(os.Getenv("SSH_ASKPASS"), "Password:").Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake-ssh: askpass:", err)
		return 91
	}
	sum := sha256.Sum256(bytes.TrimRight(out, "\n"))
	dir := os.Getenv("KARVI_TEST_FAKE_DIR")
	hold, _ := time.ParseDuration(os.Getenv("KARVI_TEST_FAKE_HOLD"))
	extra := os.Getenv("KARVI_TEST_FAKE_OUTPUT")
	fmt.Print("dev#")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		command := scanner.Text()
		if command == "exit" {
			fmt.Print("exit\r\n")
			return 0
		}
		if dir != "" {
			fields := strings.Fields(command)
			marker := fields[len(fields)-1]
			other := "b"
			if marker == "b" {
				other = "a"
			}
			_ = os.WriteFile(filepath.Join(dir, "ready-"+marker), nil, 0o600)
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				if _, err := os.Stat(filepath.Join(dir, "ready-"+other)); err == nil {
					_ = os.WriteFile(filepath.Join(dir, "overlap-"+marker), nil, 0o600)
					break
				}
				time.Sleep(20 * time.Millisecond)
			}
			f, err := os.OpenFile(filepath.Join(dir, "log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
			if err == nil {
				fmt.Fprintf(f, "%s %x\n", command, sum)
				f.Close()
			}
		}
		// KARVI_TEST_FAKE_HOLD keeps the command in flight after it is
		// logged, so a test can stop the daemon while a command is at the
		// device.
		if hold > 0 {
			time.Sleep(hold)
		}
		fmt.Printf("%s\r\nok\r\n", command)
		// A large or marked output, when the test asks for one (a record
		// larger than the frame cap, a canary that must stay in the file).
		if extra != "" {
			fmt.Printf("%s\r\n", extra)
		}
		fmt.Print("dev#")
	}
	return 0
}

// fixedInput answers the built-in fallback for one job.
type fixedInput struct{ user, pass string }

func (i fixedInput) LookupEnv(_ context.Context, name string) (string, bool, error) {
	switch name {
	case "NETUSER":
		return i.user, true, nil
	case "NETPASS":
		return i.pass, true, nil
	}
	return "", false, nil
}
func (fixedInput) Prompt(context.Context, credentialbackend.PromptRequest) (string, error) {
	return "", os.ErrNotExist
}
