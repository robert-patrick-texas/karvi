package targetsource

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// tree builds the fixture used by the folder tests:
//
//	root/
//	  10-b.txt         b1, b2
//	  05-a.txt         a1
//	  README.md        skipped
//	  Disabled-x.txt   skipped
//	  .hidden          skipped
//	  old.txt.BAK, x.swp, y.orig, z.rej, w.txt~, #auto#   skipped
//	  link-a.txt -> 05-a.txt         read (link to a regular file)
//	  sub/             c1 (level 1); sub/deep/ d1 (level 2); sub/deep/deeper/ e1 (level 3)
//	  link-sub -> sub  folder link (skipped by --tf, followed once by --tfr)
//	  .git/            skipped even in recursive reads
//	  disabled-old/    skipped folder
func tree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("10-b.txt", "b1\n# comment\n\n  b2  \n")
	write("05-a.txt", "a1\n")
	write("README.md", "readme\n")
	write("Disabled-x.txt", "disabled\n")
	write(".hidden", "hidden\n")
	for _, n := range []string{"old.txt.BAK", "x.swp", "y.orig", "z.rej", "w.txt~", "#auto#"} {
		write(n, "skip\n")
	}
	if err := os.Symlink("05-a.txt", filepath.Join(root, "link-a.txt")); err != nil {
		t.Fatal(err)
	}
	write("sub/c.txt", "c1\n")
	write("sub/deep/d.txt", "d1\n")
	write("sub/deep/deeper/e.txt", "e1\n")
	if err := os.Symlink("sub", filepath.Join(root, "link-sub")); err != nil {
		t.Fatal(err)
	}
	write(".git/HEAD", "ref\n")
	write("disabled-old/h.txt", "h1\n")
	return root
}

func TestSkipNames(t *testing.T) {
	for name, want := range map[string]bool{
		"hosts.txt": false, "core": false, "readme-hosts": true, "README": true, "Readme.txt": true,
		"disabled": true, "DISABLED-lab": true, ".hidden": true, ".": true, "a~": true, "a.BAK": true,
		"a.bak": true, "a.swp": true, "a.Orig": true, "a.rej": true, "#a#": true, "#": false, "#a": false,
		"a#": false, "bakery": false, "x.bakup": false,
	} {
		if got := Skip(name); got != want {
			t.Errorf("Skip(%q)=%v, want %v", name, got, want)
		}
	}
}

func TestLines(t *testing.T) {
	got, err := Lines(strings.NewReader("  R1 \n\n# c\n   # also a comment\n!r2\n  !r9 is a comment too\nr2\r\n"), "in")
	if err != nil || !reflect.DeepEqual(got, []string{"R1", "r2"}) {
		t.Fatalf("%q %v", got, err)
	}
	got, err = Lines(strings.NewReader(""), "in")
	if err != nil || len(got) != 0 || got == nil {
		t.Fatalf("empty input must yield an empty, non-nil list: %#v %v", got, err)
	}
}

func TestReadFileAndFolder(t *testing.T) {
	root := tree(t)
	got, err := Read(filepath.Join(root, "10-b.txt"), Options{})
	if err != nil || !reflect.DeepEqual(got, []string{"b1", "b2"}) {
		t.Fatalf("file: %q %v", got, err)
	}
	// --tf DIR: byte order of names; links to files read; folders and folder
	// links skipped; hidden, readme, disabled, and backup names skipped.
	got, err = Read(root, Options{})
	if err != nil || !reflect.DeepEqual(got, []string{"a1", "b1", "b2", "a1"}) {
		t.Fatalf("folder: %q %v", got, err)
	}
	// --tfr: depth-first, a subfolder at its name's position; the folder link
	// resolves to sub, already read, so it is read once.
	got, err = Read(root, Options{Recursive: true, MaxDepth: 3})
	if err != nil || !reflect.DeepEqual(got, []string{"a1", "b1", "b2", "a1", "c1", "d1", "e1"}) {
		t.Fatalf("recursive: %q %v", got, err)
	}
	// --tfr on a file reads it as --tf does.
	got, err = Read(filepath.Join(root, "link-a.txt"), Options{Recursive: true, MaxDepth: 3})
	if err != nil || !reflect.DeepEqual(got, []string{"a1"}) {
		t.Fatalf("recursive file: %q %v", got, err)
	}
}

func TestReadDepthLimitIsAnError(t *testing.T) {
	root := tree(t)
	// sub is level 1, deep level 2, deeper level 3.
	if _, err := Read(root, Options{Recursive: true, MaxDepth: 2}); errorcodes.Of(err) != "target_source_depth_exceeded" || !strings.Contains(err.Error(), "deeper") {
		t.Fatalf("depth 2: %v", err)
	}
	if got, err := Read(filepath.Join(root, "sub", "deep"), Options{Recursive: true, MaxDepth: 1}); err != nil || !reflect.DeepEqual(got, []string{"d1", "e1"}) {
		t.Fatalf("PATH is level 0: %q %v", got, err)
	}
}

func TestReadLinkCycleReadOnce(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a", "h.txt"), []byte("h1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("..", filepath.Join(root, "a", "up")); err != nil {
		t.Fatal(err)
	}
	got, err := Read(root, Options{Recursive: true, MaxDepth: 16})
	if err != nil || !reflect.DeepEqual(got, []string{"h1"}) {
		t.Fatalf("cycle: %q %v", got, err)
	}
}

func TestReadEmptySourcesYieldNothing(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "empty.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README"), []byte("r1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(root, "empty.txt"), root} {
		got, err := Read(p, Options{Recursive: true, MaxDepth: 3})
		if err != nil || len(got) != 0 || got == nil {
			t.Fatalf("%s: %#v %v", p, got, err)
		}
	}
}

func TestReadErrorCodes(t *testing.T) {
	root := tree(t)
	if _, err := Read(filepath.Join(root, "missing"), Options{}); errorcodes.Of(err) != "target_source_missing" {
		t.Fatalf("missing: %v", err)
	}
	fifo := filepath.Join(root, "queue")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	if _, err := Read(fifo, Options{}); errorcodes.Of(err) != "target_source_special_file" || !strings.Contains(err.Error(), "named pipe") {
		t.Fatalf("fifo as PATH: %v", err)
	}
	if _, err := Read(root, Options{}); errorcodes.Of(err) != "target_source_special_file" {
		t.Fatalf("fifo inside a folder is a hard error: %v", err)
	}
	if err := os.Remove(fifo); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() == 0 {
		return // root reads everything
	}
	secret := filepath.Join(root, "secret.txt")
	if err := os.WriteFile(secret, []byte("s1\n"), 0o000); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(secret, Options{}); errorcodes.Of(err) != "target_source_permission_denied" {
		t.Fatalf("permission: %v", err)
	}
	if _, err := Read(root, Options{}); errorcodes.Of(err) != "target_source_permission_denied" {
		t.Fatalf("permission inside a folder is always an error: %v", err)
	}
}
