package main

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/canary"
)

func TestScanFindsPlantedCanariesAndReportsClean(t *testing.T) {
	v := canary.New()
	dir := t.TempDir()
	plant := map[string][]byte{
		"raw.log":    []byte("password=" + v.Raw + "\n"),
		"hex.bin":    []byte(hex.EncodeToString([]byte("xx" + v.Raw))),
		"b64.txt":    []byte(base64.StdEncoding.EncodeToString([]byte("shift" + v.Raw + "tail"))),
		"bytes.txt":  []byte("[110 100 " + strings.Trim(strings.TrimPrefix(bytesForm(v.Raw), "110 100 "), " ") + "]"),
		"masked.txt": []byte(strings.Repeat("*", len(v.Raw)-4) + v.Raw[len(v.Raw)-4:]),
		"clean.json": []byte(`{"device_username":"u","status":"succeeded"}`),
	}
	for name, data := range plant {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("/etc/hostname", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	t.Setenv("CANARY_TEST", v.Raw)
	rc := run([]string{"-canary-env", "CANARY_TEST", dir}, nil, &out, &errOut)
	if rc != 1 {
		t.Fatalf("rc=%d stderr=%s stdout=%s", rc, errOut.String(), out.String())
	}
	for _, want := range []string{"raw.log:9:raw", "hex.bin:", "b64.txt:", "bytes.txt:"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %s:\n%s", want, out.String())
		}
	}
	for _, absent := range []string{"masked.txt", "clean.json", "link"} {
		if strings.Contains(out.String(), absent) {
			t.Errorf("output names %s:\n%s", absent, out.String())
		}
	}
	// A clean tree reports what it scanned.
	clean := t.TempDir()
	_ = os.WriteFile(filepath.Join(clean, "a.txt"), []byte("nothing here"), 0o600)
	out.Reset()
	rc = run([]string{"-canary", v.Raw, clean}, nil, &out, &errOut)
	if rc != 0 || !strings.HasPrefix(out.String(), "scanned: 1 files, 12 bytes") {
		t.Fatalf("clean rc=%d out=%s", rc, out.String())
	}
	// Standard input, the environment, and this process.
	out.Reset()
	rc = run([]string{"-canary", v.Raw, "-"}, strings.NewReader("x"+v.Raw), &out, &errOut)
	if rc != 1 || !strings.Contains(out.String(), "stdin:1:raw") {
		t.Fatalf("stdin rc=%d out=%s", rc, out.String())
	}
	out.Reset()
	rc = run([]string{"-canary", v.Raw, "-env"}, nil, &out, &errOut)
	if rc != 1 || !strings.Contains(out.String(), "env:CANARY_TEST:0:raw") {
		t.Fatalf("env rc=%d out=%s", rc, out.String())
	}
	// /proc/<pid>/environ shows the environment a process started with, so
	// scan a child started with the canary, which is the real use.
	child := exec.Command("sleep", "30")
	child.Env = append(os.Environ(), "CHILD_CANARY="+v.Raw)
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Process.Kill(); _ = child.Wait() }()
	out.Reset()
	rc = run([]string{"-canary", v.Raw, "-proc", itoa(child.Process.Pid)}, nil, &out, &errOut)
	if rc != 1 || !strings.Contains(out.String(), "/environ:CHILD_CANARY:0:raw") {
		t.Fatalf("proc rc=%d out=%s stderr=%s", rc, out.String(), errOut.String())
	}
	// Usage errors.
	for _, args := range [][]string{{}, {"-canary", v.Raw}, {"-canary-env", "UNSET_CANARY_VAR", dir}, {"-canary", v.Raw, filepath.Join(dir, "missing")}} {
		if rc := run(args, nil, &out, &errOut); rc != 2 {
			t.Errorf("%v: rc=%d", args, rc)
		}
	}
}

func bytesForm(s string) string {
	parts := make([]string, len(s))
	for i := range s {
		parts[i] = itoa(int(s[i]))
	}
	return strings.Join(parts, " ")
}

func itoa(i int) string { return strconv.Itoa(i) }
