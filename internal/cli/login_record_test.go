package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/testdir"
)

// TestRecordedLoginRefusesALinkSwappedIntoTheDayFolder: once the session's
// files are claimed, another member of the transcript tree's group renames
// them aside and plants links to the operator's own files in the day
// folder. The fake script(1) does it before it writes, as the recorder's
// start can lose that race. The device stream reaches the claimed
// transcript, nothing is read or written through the links, and the end is
// refused with transcript_create_failed.
func TestRecordedLoginRefusesALinkSwappedIntoTheDayFolder(t *testing.T) {
	dir := testdir.Short(t) // the scratch holds the askpass socket
	home, base, bin, private := filepath.Join(dir, "home"), filepath.Join(dir, "state"), filepath.Join(dir, "bin"), filepath.Join(dir, "private")
	root, scratch, trust := filepath.Join(dir, "transcripts"), filepath.Join(dir, "scratch"), filepath.Join(dir, "trust")
	for _, d := range []string{home, base, bin, private, scratch, trust} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	// The shared tree's shape: group-writable and setgid, with no sticky
	// bit, so a member can rename another's entries.
	if err := os.Mkdir(root, 0o770); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, os.ModeSetgid|0o770); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	key, profile := filepath.Join(private, "id_ed25519"), filepath.Join(private, ".profile")
	const keyText, profileText = "-----BEGIN OPENSSH PRIVATE KEY-----\r\nb3BlbnNzaC1rZXktdjE=\r\n", "umask 077\n"
	for f, b := range map[string]string{key: keyText, profile: profileText} {
		if err := os.WriteFile(f, []byte(b), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	script := "#!/bin/sh\nfor a; do file=$a; done\n" +
		"t=$KARVI_LOGIN_TRANSCRIPT_PATH m=${KARVI_LOGIN_TRANSCRIPT_PATH%.log}.meta.jsonl\n" +
		"mv \"$t\" \"$t.moved\" && ln -s '" + key + "' \"$t\" || exit 1\n" +
		"mv \"$m\" \"$m.moved\" && ln -s '" + profile + "' \"$m\" || exit 1\n" +
		"printf 'router1#show clock\\r\\n' >>\"$file\"\n"
	if err := os.WriteFile(filepath.Join(bin, "script"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(bin, "fake-ssh")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	var args []string
	for _, s := range []string{
		fmt.Sprintf("basedir=%q", base), fmt.Sprintf("transcript.root=%q", root), fmt.Sprintf("tempdir=%q", scratch),
		fmt.Sprintf("ssh.transports.system=%q", fake), fmt.Sprintf("ssh.known-hosts-file=%q", filepath.Join(trust, "known_hosts")),
		"audit.journald-required=false", fmt.Sprintf("audit.file=%q", filepath.Join(base, "audit.jsonl")),
		`freecheck="never"`, "display.color=never",
	} {
		args = append(args, "--set", s)
	}
	args = append(args, "login", "--record", "--transport", "system", "--address", "127.0.0.1", "router1")
	var stdout, stderr bytes.Buffer
	code := Main(args, strings.NewReader(""), &stdout, &stderr)
	if code == 0 || !strings.Contains(stderr.String(), "transcript_create_failed: ") {
		t.Errorf("exit=%d stderr=%q", code, stderr.String())
	}
	links, _ := filepath.Glob(filepath.Join(root, "*", "router1-*.log"))
	metas, _ := filepath.Glob(filepath.Join(root, "*", "router1-*.meta.jsonl"))
	if len(links) != 1 || len(metas) != 1 {
		t.Fatalf("day folder: %v %v", links, metas)
	}
	for _, c := range []struct{ file, want, link string }{{key, keyText, links[0]}, {profile, profileText, metas[0]}} {
		if b, err := os.ReadFile(c.file); err != nil || string(b) != c.want {
			t.Errorf("%s was written through the link: %q %v", c.file, b, err)
		}
		if fi, err := os.Lstat(c.link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
			t.Errorf("%s is no longer the planted link: %v %v", c.link, fi, err)
		}
	}
	if b, err := os.ReadFile(links[0] + ".moved"); err != nil || string(b) != "router1#show clock\r\n" {
		t.Errorf("the claimed transcript: %q %v", b, err)
	}
}
