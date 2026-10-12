package buildinfo

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestBuildIsTheGoBuildIDsLastPart reads the test binary's own build and
// compares it with the last part of `go tool buildid`'s answer for the same
// file, when the go command is at hand.
func TestBuildIsTheGoBuildIDsLastPart(t *testing.T) {
	got, err := Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{20}$`).MatchString(got) {
		t.Fatalf("Build = %q, want 20 characters of the build ID's alphabet", got)
	}
	if BuildOrNone() != got {
		t.Fatalf("BuildOrNone = %q, want %q", BuildOrNone(), got)
	}
	if Current().Build != got {
		t.Fatalf("Current().Build = %q, want %q", Current().Build, got)
	}
	goCmd, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go command to compare with")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(goCmd, "tool", "buildid", exe).Output()
	if err != nil {
		t.Skipf("go tool buildid: %v", err)
	}
	id := strings.TrimSpace(string(out))
	if want := id[strings.LastIndexByte(id, '/')+1:]; got != want {
		t.Fatalf("Build = %q, go tool buildid's last part %q (%s)", got, want, id)
	}
}

// TestReadBuildRefusesAnExecutableWithoutOne: a file that is no ELF, and an
// ELF executable that carries no Go build ID, name no build.
func TestReadBuildRefusesAnExecutableWithoutOne(t *testing.T) {
	text := filepath.Join(t.TempDir(), "not-elf")
	if err := os.WriteFile(text, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if b, err := readBuild(text); err == nil {
		t.Fatalf("readBuild(%s) = %q, want an error", text, b)
	}
	if b, err := readBuild(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatalf("readBuild(missing) = %q, want an error", b)
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	sh, _ = filepath.EvalSymlinks(sh)
	b, err := readBuild(sh)
	if err == nil {
		t.Fatalf("readBuild(%s) = %q, want an error", sh, b)
	}
	if !strings.Contains(err.Error(), "carries no Go build ID") {
		t.Fatalf("readBuild(%s): %v, want \"carries no Go build ID\"", sh, err)
	}
}
