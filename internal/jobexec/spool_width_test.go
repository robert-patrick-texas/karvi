package jobexec

import (
	"testing"

	"github.com/robert-patrick-texas/karvi/executionplan"
)

// TestJobWidthAndNarrowDispatch: the width the
// spool's free-space rule reasons about is the smaller of the job's own
// width, the server's cap, and the device count; a narrowing caps every
// width the job runs under and leaves a serial job alone.
func TestJobWidthAndNarrowDispatch(t *testing.T) {
	parallel := executionplan.DispatchSettings{Mode: executionplan.DispatchParallel, Width: 16, StartWidth: 16, MaxWidth: 64}
	wave := executionplan.DispatchSettings{Mode: executionplan.DispatchWave, Width: 4, StartWidth: 16, MaxWidth: 64}
	serial := executionplan.DispatchSettings{Mode: executionplan.DispatchSerial, Width: 1}
	for _, c := range []struct {
		d                     executionplan.DispatchSettings
		server, devices, want int
	}{
		{parallel, 32, 100, 16}, {parallel, 8, 100, 8}, {parallel, 32, 5, 5}, {parallel, 0, 100, 16},
		{wave, 32, 100, 32}, {wave, 256, 100, 64}, {serial, 32, 100, 1}, {parallel, 32, 0, 1},
	} {
		if got := jobWidth(c.d, c.server, c.devices); got != c.want {
			t.Errorf("%s server %d devices %d: %d, want %d", c.d.Mode, c.server, c.devices, got, c.want)
		}
	}
	n := narrowDispatch(wave, 10)
	if n.Width != 4 || n.StartWidth != 10 || n.MaxWidth != 10 {
		t.Errorf("wave narrowed to 10: %+v", n)
	}
	n = narrowDispatch(parallel, 0)
	if n.Width != 1 || n.StartWidth != 1 || n.MaxWidth != 1 {
		t.Errorf("parallel narrowed below one: %+v", n)
	}
	if n := narrowDispatch(serial, 3); n != serial {
		t.Errorf("serial narrowed: %+v", n)
	}
}
