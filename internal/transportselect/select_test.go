package transportselect

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/configload"
)

func defaultConfig(t *testing.T) configload.Snapshot {
	t.Helper()
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}})
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	return cfg
}

// lazySystemConfig is the built-in configuration with the system slot
// pointed at a known executable. Unit-test sandboxes do not necessarily
// provide OpenSSH; the built-in provenance is retained.
func lazySystemConfig(t *testing.T) configload.Snapshot {
	t.Helper()
	cfg := defaultConfig(t)
	system := cfg.Values["ssh.transports.system"]
	system.Data = "/bin/true"
	cfg.Values["ssh.transports.system"] = system
	return cfg
}

func TestModeDefaultsAndLazyUnavailableNative(t *testing.T) {
	cfg := lazySystemConfig(t)
	if err := ValidateConfigured(cfg); err != nil {
		t.Fatalf("built-in unavailable native preference must be lazy: %v", err)
	}
	login, err := Resolve(cfg, "login", "default")
	if err != nil {
		t.Fatalf("resolve login default: %v", err)
	}
	if login.Kind != KindSystem || login.Implementation != "system" || login.Binary == "" {
		t.Fatalf("unexpected login selection: %#v", login)
	}
	command, err := Resolve(cfg, "command", "")
	if err != nil || command.Kind != KindSystem {
		t.Fatalf("unexpected command selection %#v err=%v", command, err)
	}
}

func TestExplicitUnavailableSystemMappingFailsConfigurationCheck(t *testing.T) {
	cfg, err := configload.Load(configload.Options{
		InternalOnly: true,
		Environment:  []string{},
		Sets:         []string{`ssh.transports.system="/definitely/not/a/karvi-ssh"`},
	})
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	err = ValidateConfigured(cfg)
	if err == nil || !strings.Contains(err.Error(), "ssh.transports.system") || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("expected named unavailable transport error, got %v", err)
	}
}

func TestExplicitUnavailableAlternateBuiltInFailsConfigurationCheck(t *testing.T) {
	cfg, err := configload.Load(configload.Options{
		InternalOnly: true,
		Environment:  []string{},
		Sets:         []string{`ssh.transports.alternate1="scrapligo-v2"`},
	})
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	err = ValidateConfigured(cfg)
	if err == nil || !strings.Contains(err.Error(), "alternate1") || !strings.Contains(err.Error(), "scrapligo-v2") {
		t.Fatalf("expected alternate mapping error, got %v", err)
	}
}

func TestRelativeExecutableResolvesFromKarviExecutableDirectory(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	executable, _ = filepath.EvalSymlinks(executable)
	name := "karvi-test-ssh-relative"
	path := filepath.Join(filepath.Dir(executable), name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Skipf("cannot create helper beside test executable: %v", err)
	}
	defer os.Remove(path)
	got, err := resolveExecutable("./" + name)
	if err != nil {
		t.Fatalf("resolve relative executable: %v", err)
	}
	if got != path {
		t.Fatalf("resolved %q, want %q", got, path)
	}
}

func TestBareSystemExecutableUsesPATH(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "karvi-test-ssh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	got, err := resolveExecutable("karvi-test-ssh")
	if err != nil {
		t.Fatal(err)
	}
	if got != path {
		t.Fatalf("resolved %q, want %q", got, path)
	}
}

func TestExplicitExecAlternateResolvesSystemProcess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "handler")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	cfg, err := configload.Load(configload.Options{
		InternalOnly: true,
		Environment:  []string{},
		Sets: []string{
			`ssh.transports.alternate1="exec:` + path + `"`,
			`ssh.command.transport="alternate1"`,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateConfigured(cfg); err != nil {
		t.Fatal(err)
	}
	got, err := Resolve(cfg, "command", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Selector != "alternate1" || got.Kind != KindSystem || got.Binary != path {
		t.Fatalf("unexpected selection: %#v", got)
	}
}
