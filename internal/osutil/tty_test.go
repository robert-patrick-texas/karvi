package osutil

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/testpty"
)

// TestRawTerminalLinesEndsEachLineAtTheFirstColumn writes karvi's lines in
// the pieces a writer may be handed: a lone line feed gains its carriage
// return, one already after a carriage return does not, a carriage return
// ending one write counts for a line feed opening the next, and every
// other byte is written as it came.
func TestRawTerminalLinesEndsEachLineAtTheFirstColumn(t *testing.T) {
	var got bytes.Buffer
	w := &rawLines{w: &got}
	for _, piece := range []string{"DEBUG one\n", "DEBUG two\r\n", "\x1b[34m! header\x1b[0m\r", "\n", "\n", "tab\there\n"} {
		n, err := w.Write([]byte(piece))
		if err != nil || n != len(piece) {
			t.Fatalf("Write(%q) = %d, %v; want %d, nil", piece, n, err, len(piece))
		}
	}
	want := "DEBUG one\r\nDEBUG two\r\n\x1b[34m! header\x1b[0m\r\n\r\ntab\there\r\n"
	if got.String() != want {
		t.Fatalf("written %q, want %q", got.String(), want)
	}
}

// TestRawTerminalLinesTranslatesOnlyOnATerminal: a redirected stream is
// returned as it is; a terminal is written through the translation.
func TestRawTerminalLinesTranslatesOnlyOnATerminal(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if w := RawTerminalLines(file); w != file {
		t.Fatalf("a regular file became %T", w)
	}
	_, slave := testpty.Open(t)
	if _, ok := RawTerminalLines(slave).(*rawLines); !ok {
		t.Fatalf("a terminal was returned as %T", RawTerminalLines(slave))
	}
}

// TestIsRegularFile: a regular file is one, as a stream redirected into a
// file is; a pipe is not.
func TestIsRegularFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if !IsRegularFile(f) {
		t.Errorf("a regular file is not one")
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if IsRegularFile(w) {
		t.Errorf("a pipe taken for a regular file")
	}
}

// TestForegroundGroup: a terminal says whether this process's group is
// its foreground group only to a process it is the controlling terminal
// of. A pseudo-terminal opened here (O_NOCTTY), or a regular file, cannot
// say. In a child made a session leader with the terminal as its
// controlling one, the child's group is the foreground group; a
// grandchild put in a group of its own, as a shell puts a job sent to the
// background, is not.
func TestForegroundGroup(t *testing.T) {
	_, slave := testpty.Open(t)
	if _, known := ForegroundGroup(slave); known {
		t.Errorf("a terminal opened O_NOCTTY says")
	}
	file, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, known := ForegroundGroup(file); known {
		t.Errorf("a regular file says")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestForegroundGroupChild$")
	cmd.Env = append(os.Environ(), "KARVI_TEST_FOREGROUND=leader")
	cmd.Stdin = slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the child: %v\n%s", err, out)
	}
	for _, want := range []string{"leader foreground=true known=true\n", "background foreground=false known=true\n"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("the child said\n%s\nwant %q", out, want)
		}
	}
}

// TestForegroundGroupChild is TestForegroundGroup's child: it says what
// its standard input, its controlling terminal, tells it, as the session
// leader and from a grandchild in a group of its own.
func TestForegroundGroupChild(t *testing.T) {
	role := os.Getenv("KARVI_TEST_FOREGROUND")
	if role == "" {
		t.Skip("run by TestForegroundGroup")
	}
	foreground, known := ForegroundGroup(os.Stdin)
	fmt.Printf("%s foreground=%v known=%v\n", role, foreground, known)
	if role != "leader" {
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestForegroundGroupChild$")
	cmd.Env = append(os.Environ(), "KARVI_TEST_FOREGROUND=background")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Run(); err != nil {
		t.Fatalf("the grandchild: %v", err)
	}
}
