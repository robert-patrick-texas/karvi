package jobexec

import (
	"io"

	"github.com/robert-patrick-texas/karvi/internal/executor"
	"github.com/robert-patrick-texas/karvi/platform"
	"github.com/robert-patrick-texas/karvi/records"
)

// liveDevice is one device's live view (--live): the session's bytes
// written to the renderer's output as the transport reads them, through
// the secret filter and, when output.ansi asks for it, the escape-sequence
// stripper. The renderer's lock orders them with karvi's own lines.
type liveDevice struct {
	r      *recordRenderer
	filter platform.SecretFilter
	strip  *ansiStripper
	ended  bool
}

var _ executor.LiveView = (*liveDevice)(nil)

// LiveDevice opens a device's live view before its transport opens: the
// device's ICMP line and header, as its first record would write them, and
// then the session's bytes as they come.
func (r *recordRenderer) LiveDevice(rec records.CommandRecord) executor.LiveView {
	r.mu.Lock()
	defer r.mu.Unlock()
	d := &liveDevice{r: r}
	var sink io.Writer = rendererWriter{r}
	if r.stripANSI {
		d.strip = &ansiStripper{w: sink}
		sink = d.strip
	}
	d.filter.W = sink
	values := displayValuesFromRecord(rec)
	values.ReferenceID = r.activityID
	values.Artifacts = r.artifact
	target := values.Target
	if target == "" {
		target = rec.Device.CanonicalName
	}
	if ping, ok := r.pingLinesLocked(rec, values, target); ok {
		if header, ok := r.headerLinesLocked(values); ok {
			r.writeKarviLocked(append(ping, header...))
		}
	}
	r.current = d
	return d
}

func (d *liveDevice) Write(p []byte) {
	d.r.mu.Lock()
	defer d.r.mu.Unlock()
	if d.ended || d.r.err != nil {
		return
	}
	_, _ = d.filter.Write(p) // a failed write is the renderer's error
}

func (d *liveDevice) Secret(value []byte) {
	d.r.mu.Lock()
	defer d.r.mu.Unlock()
	d.filter.Add(value)
}

// End writes what the filters held back and forgets the secrets.
func (d *liveDevice) End() {
	d.r.mu.Lock()
	defer d.r.mu.Unlock()
	d.flushLocked()
	d.filter.Wipe()
	d.ended = true
}

// flushLocked writes what the filters hold back: a tail that could begin a
// secret, an escape sequence not yet whole.
func (d *liveDevice) flushLocked() {
	if d.ended {
		return
	}
	_ = d.filter.Flush()
	if d.strip != nil {
		_ = d.strip.flush()
	}
}

// writeKarviLocked writes karvi's own lines: in the live view on a line of
// their own after the session's bytes.
func (r *recordRenderer) writeKarviLocked(data []byte) {
	if len(data) == 0 {
		return
	}
	r.liveBreakLocked()
	r.write(data)
}

// liveBreakLocked ends the live view's last line, what the device's filters
// hold written first; it does nothing outside the live view.
func (r *recordRenderer) liveBreakLocked() {
	if !r.live {
		return
	}
	if r.current != nil {
		r.current.flushLocked()
	}
	if r.outputByteSize > 0 && r.lastByte != '\n' {
		r.write([]byte{'\n'})
	}
}
