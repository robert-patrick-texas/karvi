package errorcodes

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestRegistryIsValid(t *testing.T) {
	for _, err := range Validate() {
		t.Error(err)
	}
}

func TestGeneratedDocumentIsCurrent(t *testing.T) {
	path := filepath.Join(moduleRoot(t), "docs", "ERROR-CODES.md")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v (run make generate)", path, err)
	}
	if string(got) != RenderMarkdown() {
		t.Fatalf("%s is stale; run make generate", path)
	}
}

type testCodedError struct{ code string }

func (e testCodedError) Error() string     { return "message without prefix" }
func (e testCodedError) ErrorCode() string { return e.code }

func TestOfFindsCodesThroughWrapping(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{errors.New("dns_timeout: lookup timed out"), "dns_timeout"},
		{errors.New("ipc_invalid_json"), "ipc_invalid_json"},
		{fmt.Errorf("open: %w", testCodedError{"host_key_changed"}), "host_key_changed"},
		{fmt.Errorf("open: %w", errors.New("connection_refused: refused")), "connection_refused"},
		{errors.New("unregistered_code: text"), ""},
		{errors.New("config_semantic_error: retired codes are not returned"), ""},
		{errors.New("plain failure"), ""},
		{nil, ""},
	}
	for _, tc := range cases {
		if got := Of(tc.err); got != tc.want {
			t.Errorf("Of(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

func TestWithExitFixesExitAndKeepsCode(t *testing.T) {
	err := WithExit(errors.New("native_transport_unavailable: not compiled in"), 2)
	if got := ExitAt(err, "config_load_failed"); got != 2 {
		t.Fatalf("exit = %d, want 2", got)
	}
	if got := Message(err); got != "native_transport_unavailable: not compiled in" {
		t.Fatalf("message = %q", got)
	}
	if WithExit(nil, 2) != nil {
		t.Fatal("WithExit(nil) must be nil")
	}
}

func TestExitForUsesRegistryThenFallback(t *testing.T) {
	if got := ExitFor(errors.New("management_address_scope_error: two devices"), 5); got != 4 {
		t.Fatalf("exit = %d, want 4", got)
	}
	if got := ExitFor(errors.New("askpass_prompt_unrecognized"), 8); got != 8 {
		t.Fatalf("code without an exit should use fallback; got %d", got)
	}
	if got := ExitFor(errors.New("plain"), 8); got != 8 {
		t.Fatalf("uncoded error should use fallback; got %d", got)
	}
}
