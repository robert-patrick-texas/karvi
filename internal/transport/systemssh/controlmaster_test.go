package systemssh

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/testsocket"
	"github.com/robert-patrick-texas/karvi/platform"
)

// One interactive shell is the device's whole session, so OpenSSH
// connection reuse is pinned off on the command line whatever the platform
// table says: ControlMaster is not to be used.
func TestControlMasterAlwaysDisabled(t *testing.T) {
	for _, tc := range []struct {
		name    string
		enabled bool
	}{
		{name: "platform default", enabled: false},
		{name: "platform table asks for reuse", enabled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			bin := filepath.Join(home, "fake-ssh")
			script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$0.log\"\ncase \" $* \" in *\" -O check \"*|*\" -M \"*) printf 'CONTROLMASTER-PROBE\\n' >> \"$0.log\"; exit 97 ;; esac\nprintf 'server01$ '\nwhile IFS= read -r line; do\n  [ \"$line\" = exit ] && exit 0\n  printf '%s\\r\\nok\\r\\nserver01$ ' \"$line\"\ndone\n"
			if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			cfg, err := configload.Load(configload.Options{HomeDir: home, SkipAuto: true, Environment: []string{}, Sets: []string{"ssh.include-user-config=false", "audit.journald-required=false", "execution.prompt-timeout=2s"}})
			if err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(home, "control")
			factory := Factory{Binary: bin, Config: cfg, Home: home, BaseDir: filepath.Join(home, ".local", "share", "karvi"), ScratchDir: testsocket.Dir(t), ControlRoot: root, AskpassPath: "/bin/true"}
			def, _ := platform.Builtin("linux")
			def.ControlMaster = tc.enabled
			opened, err := factory.Open(context.Background(), platform.OpenRequest{
				Address: "192.0.2.10", Port: 22, Username: "operator",
				Definition: def,
				Metadata:   map[string]string{"canonical_name": "server01"},
				Password:   func(fn func([]byte) error) error { return fn([]byte("test-only")) },
			})
			if err != nil {
				t.Fatal(err)
			}
			driver := opened.(*Driver)
			defer driver.Close()

			rendered, err := os.ReadFile(driver.configPath)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"ControlMaster no", "ControlPath none", "ControlPersist no"} {
				if !strings.Contains(string(rendered), want) {
					t.Fatalf("generated config lacks %q:\n%s", want, rendered)
				}
			}
			if err := driver.Prepare(context.Background()); err != nil {
				t.Fatal(err)
			}
			if result := driver.Execute(context.Background(), platform.Command{Text: "uptime"}); result.Err != nil || string(result.Output) != "ok\n" || result.Prompt != "server01$" {
				t.Fatalf("uptime: %+v output=%q", result, result.Output)
			}
			logged, err := os.ReadFile(bin + ".log")
			if err != nil {
				t.Fatal(err)
			}
			args := string(logged)
			for _, want := range []string{"ControlMaster=no", "ControlPath=none", "ControlPersist=no"} {
				if !strings.Contains(args, want) {
					t.Fatalf("arguments lack %q:\n%s", want, args)
				}
			}
			if strings.Contains(args, "ControlMaster=auto") || strings.Contains(args, "CONTROLMASTER-PROBE") {
				t.Fatalf("connection reuse was attempted:\n%s", args)
			}
		})
	}
}
