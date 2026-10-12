package app

import (
	"errors"
	"fmt"
	"io/fs"
	"syscall"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/daemon"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// TestPrepareAgain: prepare_job is made once more when the daemon left
// between the probe and the request, or when a daemon of another build
// prepared (EXAMPLES chapter 57, issue 3), and for nothing else.
func TestPrepareAgain(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"socket gone", fmt.Errorf("dial: %w", fs.ErrNotExist), true},
		{"refused", fmt.Errorf("dial: %w", syscall.ECONNREFUSED), true},
		{"draining", errorcodes.Errorf("daemon_draining", "daemon is draining and accepts no new jobs"), true},
		{"another build", fmt.Errorf("prepare: %w", &daemon.OtherBuildError{Daemon: "9Au3WHU839F5xE-67H93", Client: "XfTtoeK1qq9HkGakcVu7"}), true},
		{"a digest mismatch", errorcodes.Errorf("plan_digest_mismatch", "the daemon prepared another draft"), false},
		{"anything else", errors.New("boom"), false},
	} {
		if got := prepareAgain(tc.err); got != tc.want {
			t.Errorf("%s: prepareAgain = %t, want %t", tc.name, got, tc.want)
		}
	}
}
