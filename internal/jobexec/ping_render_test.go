package jobexec

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/records"
)

func pingRecord(decision string, outcomes []records.PingOutcome, notices []records.Notice, index int) records.CommandRecord {
	replies := 0
	for _, o := range outcomes {
		if o.Status == records.PingReply {
			replies++
		}
	}
	return records.CommandRecord{
		Device: records.DeviceProjection{CanonicalName: "core-nyc-01"}, InputTarget: "core-nyc-01", SelectedAddress: "2001:db8:10::1",
		Platform: "generic", Transport: "system", Status: "succeeded", OutputEncoding: "utf-8", Output: "ok\n", CommandIndex: index, CommandCount: 2,
		Ping:    &records.PingReport{Address: "2001:db8:10::1", Family: "ipv6", Method: "socket", ExecutionEndpoint: "local", Probes: 2, TimeoutNS: 500000000, Outcomes: outcomes, Replies: replies, Losses: 2 - replies, Decision: decision},
		Notices: notices, Timing: records.Timing{EndedAt: time.Now()},
	}
}

func rttNS(d time.Duration) *int64 { v := d.Nanoseconds(); return &v }

var (
	replyOut   = records.PingOutcome{Sequence: 1, Status: records.PingReply, RTTNS: rttNS(2100 * time.Microsecond), From: "2001:db8:10::1"}
	fastOut    = records.PingOutcome{Sequence: 2, Status: records.PingReply, RTTNS: rttNS(40 * time.Microsecond), From: "2001:db8:10::1"}
	timeoutOut = records.PingOutcome{Sequence: 2, Status: records.PingTimeout}
	errorOut   = records.PingOutcome{Sequence: 2, Status: records.PingError, From: "2001:668::7c1", Detail: "Destination unreachable: No route"}
	lossNotice = records.Notice{Code: "icmp_packet_loss", Message: "ICMP packet loss 50%; proceeding because one validated reply was received"}
)

func renderWith(t *testing.T, sets []string, quiet, debug bool, rec records.CommandRecord) string {
	t.Helper()
	return renderAll(t, sets, quiet, debug, rec)
}

// renderAll renders records in order through one run renderer.
func renderAll(t *testing.T, sets []string, quiet, debug bool, recs ...records.CommandRecord) string {
	t.Helper()
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: sets})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	r, err := newRecordRenderer(&out, "text", cfg, quiet, debug, "run-1", "/tmp/artifacts", "run", false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, rec := range recs {
		r.OnRecord(rec)
	}
	return out.String()
}

// TestPingLineShapes covers the operator's rendering of the ping line.
func TestPingLineShapes(t *testing.T) {
	for _, tc := range []struct {
		name string
		rec  records.CommandRecord
		want string
	}{
		{"two replies", pingRecord(records.PingDecisionProceed, []records.PingOutcome{replyOut, fastOut}, nil, 1), "! core-nyc-01 [2001:db8:10::1] ping(1) 2.1ms, ping(2) <0.1ms, proceeding"},
		{"one reply", pingRecord(records.PingDecisionProceedDegraded, []records.PingOutcome{replyOut, timeoutOut}, []records.Notice{lossNotice}, 1), "! core-nyc-01 [2001:db8:10::1] ping(1) 2.1ms, ping(2) timeout, proceeding"},
		{"skipped with an error", pingRecord(records.PingDecisionSkip, []records.PingOutcome{{Sequence: 1, Status: records.PingTimeout}, errorOut}, nil, 1), "! core-nyc-01 [2001:db8:10::1] ping(1) timeout, ping(2) error, skipped"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := renderWith(t, nil, false, false, tc.rec)
			lines := strings.Split(got, "\n")
			if lines[0] != tc.want {
				t.Errorf("first line %q, want %q\nfull:\n%s", lines[0], tc.want, got)
			}
			if strings.Contains(got, "karvi: ") {
				t.Errorf("details or notice printed without --debug:\n%s", got)
			}
			if !strings.Contains(got, "core-nyc-01 [2001:db8:10::1] platform=") {
				t.Errorf("the header no longer follows the ping line:\n%s", got)
			}
		})
	}
}

func TestPingLineDebugAddsDetailsAndNotice(t *testing.T) {
	got := renderWith(t, nil, false, true, pingRecord(records.PingDecisionProceedDegraded, []records.PingOutcome{replyOut, errorOut}, []records.Notice{lossNotice}, 1))
	lines := strings.Split(got, "\n")
	want := []string{
		"! core-nyc-01 [2001:db8:10::1] ping(1) 2.1ms, ping(2) error, proceeding",
		"karvi: ping(2) error: Destination unreachable: No route, from 2001:668::7c1",
		"karvi: ICMP packet loss 50%; proceeding because one validated reply was received",
	}
	for i, w := range want {
		if lines[i] != w {
			t.Errorf("line %d %q, want %q", i+1, lines[i], w)
		}
	}
}

func TestPingLineIsSwitchedOffByEmptyTemplateQuietAndLaterCommands(t *testing.T) {
	rec := pingRecord(records.PingDecisionProceed, []records.PingOutcome{replyOut, fastOut}, nil, 1)
	if got := renderWith(t, []string{`display.ping.header=""`}, false, true, rec); strings.Contains(got, "ping(1)") {
		t.Errorf("an empty display.ping.header still printed:\n%s", got)
	}
	if got := renderWith(t, nil, true, true, rec); strings.Contains(got, "ping(1)") {
		t.Errorf("--quiet still printed:\n%s", got)
	}
	// Once per device: the renderer tracks the devices it has seen.
	second := pingRecord(records.PingDecisionProceed, []records.PingOutcome{replyOut, fastOut}, nil, 2)
	if got := renderAll(t, nil, false, false, rec, second); strings.Count(got, "ping(1)") != 1 {
		t.Errorf("the line is not printed exactly once for the device:\n%s", got)
	}
	plain := pingRecord(records.PingDecisionProceed, nil, nil, 1)
	plain.Ping = nil
	if got := renderWith(t, nil, false, true, plain); strings.Contains(got, "ping(") {
		t.Errorf("a null gate printed a line:\n%s", got)
	}
}

func TestPingLineJSONFormatsAreUntouched(t *testing.T) {
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"jsonl", "json"} {
		var out bytes.Buffer
		r, err := newRecordRenderer(&out, format, cfg, false, true, "run-1", "/tmp/artifacts", "run", false, false, false)
		if err != nil {
			t.Fatal(err)
		}
		r.OnRecord(pingRecord(records.PingDecisionSkip, []records.PingOutcome{{Sequence: 1, Status: records.PingTimeout}, timeoutOut}, nil, 1))
		if strings.Contains(out.String(), "ping(1)") || !strings.Contains(out.String(), `"decision"`) {
			t.Errorf("%s output:\n%s", format, out.String())
		}
	}
}
