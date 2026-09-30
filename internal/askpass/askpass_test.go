package askpass

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/secrets"
	"github.com/robert-patrick-texas/karvi/internal/testsocket"
)

func TestBrokerReturnsOneRecognizedSecretAndThenCloses(t *testing.T) {
	material := secrets.NewMaterial("operator", "login-secret", "enable-secret")
	defer material.Destroy()
	b, err := Start(testsocket.Dir(t), material)
	if err != nil {
		t.Fatal(err)
	}
	env := b.Environment()
	var socket, token string
	for _, value := range env {
		if strings.HasPrefix(value, SocketEnv+"=") {
			socket = strings.TrimPrefix(value, SocketEnv+"=")
		}
		if strings.HasPrefix(value, TokenEnv+"=") {
			token = strings.TrimPrefix(value, TokenEnv+"=")
		}
	}
	conn, err := net.DialTimeout("unix", socket, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(conn).Encode(request{Token: token, Prompt: "Password:"}); err != nil {
		t.Fatal(err)
	}
	var got response
	if err := json.NewDecoder(conn).Decode(&got); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	if got.Error != "" || string(got.Secret) != "login-secret" {
		t.Fatalf("unexpected response: %+v", got)
	}
	b.Wait(time.Second)
	if second, err := net.DialTimeout("unix", socket, 50*time.Millisecond); err == nil {
		second.Close()
		t.Fatal("single-use socket accepted a second connection")
	}
}

func TestBrokerRejectsUnknownPrompt(t *testing.T) {
	material := secrets.NewMaterial("operator", "login-secret", "")
	defer material.Destroy()
	b, err := Start(testsocket.Dir(t), material)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	var socket, token string
	for _, value := range b.Environment() {
		key, val, _ := strings.Cut(value, "=")
		switch key {
		case SocketEnv:
			socket = val
		case TokenEnv:
			token = val
		}
	}
	conn, err := net.DialTimeout("unix", socket, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(request{Token: token, Prompt: "Security question:"}); err != nil {
		t.Fatal(err)
	}
	var got response
	if err := json.NewDecoder(conn).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Error != "askpass_prompt_unrecognized" || len(got.Secret) != 0 {
		t.Fatalf("unexpected response: %+v", got)
	}
}

// The broker's directory is as a rule the scratch directory and may be the
// operator's own: an existing one keeps its mode, a missing one is made 0700
// (an existing directory keeps its mode).
func TestStartLeavesAnExistingDirectoryAsItIs(t *testing.T) {
	existing := filepath.Join(testsocket.Dir(t), "mine")
	if err := os.Mkdir(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(testsocket.Dir(t), "made")
	for dir, want := range map[string]os.FileMode{existing: 0o755, missing: 0o700} {
		material := secrets.NewMaterial("operator", "login-secret", "enable-secret")
		b, err := Start(dir, material)
		if err != nil {
			t.Fatal(err)
		}
		b.Close()
		material.Destroy()
		if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != want {
			t.Fatalf("%s: mode %v err %v; want %04o", dir, info.Mode().Perm(), err, want)
		}
	}
}
