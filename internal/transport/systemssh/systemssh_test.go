package systemssh

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/configload"
)

func TestManagedOptionsPrecedeUserInclude(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".ssh", "config"), []byte("Host bastion\n  ProxyJump jump.example\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := configload.Load(configload.Options{HomeDir: home, SkipAuto: true, Environment: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	text, err := (Factory{Config: cfg, Home: home, ControlRoot: filepath.Join(home, "ctl")}).renderConfig()
	if err != nil {
		t.Fatal(err)
	}
	managed := strings.Index(text, "Host *")
	include := strings.Index(text, "Include ")
	if managed < 0 || include < 0 || managed >= include {
		t.Fatalf("managed block must precede user Include:\n%s", text)
	}
	if !strings.Contains(text, "StrictHostKeyChecking accept-new") {
		t.Fatalf("missing managed host-key policy:\n%s", text)
	}
}
