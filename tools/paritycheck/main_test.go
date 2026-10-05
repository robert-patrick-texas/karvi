package main

import (
	"reflect"
	"strings"
	"testing"
)

func stream(t *testing.T, text string) []map[string]any {
	t.Helper()
	recs, err := records(strings.NewReader(text))
	if err != nil {
		t.Fatal(err)
	}
	return recs
}

const systemCommand = `{"kind":"activity_started"}
not json
{"record_id":"a1","activity_id":"a","activity_type":"command","candidate_count":1,"transport":"system","command_index":0,"command":"show clock","status":"succeeded","prompt":"Router#","connection_reused":false,"notices":[],"credential":{"credential_id":"c1","backend":"env"},"timing":{"total_ns":5},"error":null}
{"record_id":"a2","activity_id":"a","activity_type":"command","candidate_count":1,"transport":"system","command_index":1,"command":"show bogus","status":"device_error","prompt":"Router#","connection_reused":true,"notices":[],"credential":{"credential_id":"c1","backend":"env"},"timing":{"total_ns":6},"error":{"code":"device_command_error","message":"rejected","retryable":false}}
`

const nativeRun = `{"record_id":"b1","job_id":"j","activity_id":"j","activity_type":"run","transport":"native","command_index":0,"command":"show clock","status":"succeeded","prompt":"Router#","connection_reused":false,"notices":[],"credential":{"credential_id":"c2","backend":"env"},"timing":{"total_ns":9},"error":null}
{"record_id":"b2","job_id":"j","activity_id":"j","activity_type":"run","transport":"native","command_index":1,"command":"show bogus","status":"device_error","prompt":"Router#","connection_reused":true,"notices":[],"credential":{"credential_id":"c2","backend":"env"},"timing":{"total_ns":7},"error":{"code":"device_command_error","message":"rejected","retryable":false}}
`

func TestCompareAcceptsTheDocumentedDifferences(t *testing.T) {
	findings := compare([]string{"command.system", "run.native"},
		[][]map[string]any{stream(t, systemCommand), stream(t, nativeRun)},
		[]string{"succeeded", "device_error:device_command_error"})
	if len(findings) != 0 {
		t.Fatalf("findings=%v", findings)
	}
}

func TestCompareReportsEachDifference(t *testing.T) {
	other := strings.Replace(nativeRun, `"prompt":"Router#","connection_reused":true`, `"prompt":"Router>","connection_reused":null`, 1)
	other = strings.Replace(other, `"message":"rejected"`, `"message":"refused"`, 1)
	other = strings.Replace(other, `"notices":[],"credential":{"credential_id":"c2","backend":"env"},"timing":{"total_ns":9}`, `"credential":{"credential_id":"c2","backend":"env"},"timing":{"total_ns":9}`, 1)
	findings := compare([]string{"command.system", "run.native"},
		[][]map[string]any{stream(t, systemCommand), stream(t, other)}, nil)
	want := []string{
		`record 0 notices: command.system=[], run.native=<absent>`,
		`record 1 connection_reused: command.system=true, run.native=null`,
		`record 1 error.message: command.system="rejected", run.native="refused"`,
		`record 1 prompt: command.system="Router#", run.native="Router>"`,
	}
	if !reflect.DeepEqual(findings, want) {
		t.Fatalf("findings=%q", findings)
	}
}

func TestCompareReportsCountsAndExpectations(t *testing.T) {
	one := stream(t, strings.SplitAfter(nativeRun, "\n")[0])
	findings := compare([]string{"command.system", "run.native"},
		[][]map[string]any{stream(t, systemCommand), one}, []string{"succeeded", "succeeded"})
	want := []string{
		"command.system: records are succeeded,device_error:device_command_error, expected succeeded,succeeded",
		"run.native: 1 records, command.system has 2",
	}
	if !reflect.DeepEqual(findings, want) {
		t.Fatalf("findings=%q", findings)
	}
	if got := compare([]string{"a", "b"}, [][]map[string]any{nil, nil}, nil); !reflect.DeepEqual(got, []string{"a: no requested-command records"}) {
		t.Fatalf("empty streams: %q", got)
	}
}

// A failure at open carries the transport's own text; its code and
// retryable compare. The output limit's observed count follows read timing.
func TestOpenFailureMessageAndObservedCount(t *testing.T) {
	a := `{"command_index":0,"status":"connection_error","error":{"code":"host_key_changed","message":"OpenSSH says","retryable":false}}` + "\n" +
		`{"command_index":1,"status":"output_limit_exceeded","error":{"code":"output_limit_exceeded","message":"exceeded 2000 bytes (4096 observed)"}}`
	b := `{"command_index":0,"status":"connection_error","error":{"code":"host_key_changed","message":"x/crypto says","retryable":false}}` + "\n" +
		`{"command_index":1,"status":"output_limit_exceeded","error":{"code":"output_limit_exceeded","message":"exceeded 2000 bytes (4104 observed)"}}`
	if findings := compare([]string{"a", "b"}, [][]map[string]any{stream(t, a), stream(t, b)}, nil); len(findings) != 0 {
		t.Fatalf("findings=%v", findings)
	}
	c := strings.Replace(b, `"host_key_changed"`, `"host_key_not_enrolled"`, 1)
	c = strings.Replace(c, "exceeded 2000", "exceeded 3000", 1)
	want := []string{
		`record 0 error.code: a="host_key_changed", b="host_key_not_enrolled"`,
		`record 1 error.message: a="exceeded 2000 bytes (N observed)", b="exceeded 3000 bytes (N observed)"`,
	}
	if findings := compare([]string{"a", "b"}, [][]map[string]any{stream(t, a), stream(t, c)}, nil); !reflect.DeepEqual(findings, want) {
		t.Fatalf("findings=%q", findings)
	}
}

func TestExcludedPaths(t *testing.T) {
	for path, want := range map[string]bool{
		"timing.total_ns": true, "timing.connect_ns": true, "transport": true, "record_id": true,
		"credential.credential_id": true, "credential.backend": false, "connection_reused": false,
		"dispatch.worker_id": false, "transport_detail": false, "prompt_source": false,
	} {
		if got := isExcluded(path); got != want {
			t.Errorf("isExcluded(%q)=%v", path, got)
		}
	}
}

// A pin's value per transport is read by the JSON decoder, so a ';' inside
// a value does not end it; absent is no value.
func TestParsePin(t *testing.T) {
	p, err := parsePin(`5.notices=system:[{"code":"x","message":"a; b"}];native:absent`)
	if err != nil {
		t.Fatal(err)
	}
	want := pin{Index: 5, Path: "notices", Values: map[string]map[string]string{
		"system": {"notices[0].code": `"x"`, "notices[0].message": `"a; b"`},
		"native": {},
	}}
	if !reflect.DeepEqual(p, want) {
		t.Fatalf("pin=%+v", p)
	}
	for _, bad := range []string{"exit_signal=system:1", "x.exit_signal=system:1", "3.=system:1", "3.exit_signal", "3.exit_signal=", "3.exit_signal=system", "3.exit_signal=system:nope", "3.exit_signal=system:1 native:2", "3.exit_signal=:1"} {
		if _, err := parsePin(bad); err == nil {
			t.Errorf("parsePin(%q) accepted", bad)
		}
	}
}

// A pinned path is checked against each stream's transport's value and is
// not compared across the streams; a wrong value, a transport with none,
// and a pin past the records are findings.
func TestComparePins(t *testing.T) {
	sys := `{"transport":"system","command_index":0,"status":"device_error","exit_signal":"unnamed","notices":[{"code":"remote_command_not_stopped"}],"error":{"code":"command_exit_signal","message":"ended by signal unnamed"}}`
	nat := `{"transport":"native","command_index":0,"status":"device_error","exit_signal":"TERM","notices":[],"error":{"code":"command_exit_signal","message":"ended by signal TERM"}}`
	names := []string{"command.system", "command.native", "run.system", "run.native"}
	streams := [][]map[string]any{stream(t, sys), stream(t, nat), stream(t, sys), stream(t, nat)}
	pins := func(texts ...string) []pin {
		var out []pin
		for _, text := range texts {
			p, err := parsePin(text)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, p)
		}
		return out
	}
	signal := pins(`0.exit_signal=system:"unnamed";native:"TERM"`, `0.error.message=system:"ended by signal unnamed";native:"ended by signal TERM"`, `0.notices=system:[{"code":"remote_command_not_stopped"}];native:[]`)
	if findings := compare(names, streams, []string{"device_error:command_exit_signal"}, signal...); len(findings) != 0 {
		t.Fatalf("findings=%q", findings)
	}
	if findings := compare(names, streams, nil, signal[:2]...); !reflect.DeepEqual(findings, []string{
		`record 0 notices: command.system=<absent>, command.native=[]`,
		`record 0 notices[0].code: command.system="remote_command_not_stopped", command.native=<absent>`,
		`record 0 notices: command.system=<absent>, run.native=[]`,
		`record 0 notices[0].code: command.system="remote_command_not_stopped", run.native=<absent>`,
	}) {
		t.Fatalf("unpinned notices: %q", findings)
	}
	wrong := pins(`0.exit_signal=system:"unnamed";native:"KILL"`, `0.error.message=system:"ended by signal unnamed"`, `1.notices=system:[];native:[]`)
	want := []string{
		`record 0 exit_signal: command.native=exit_signal="TERM", pinned native=exit_signal="KILL"`,
		`record 0 error.message: command.native's transport "native" has no pinned value`,
		`record 0 exit_signal: run.native=exit_signal="TERM", pinned native=exit_signal="KILL"`,
		`record 0 error.message: run.native's transport "native" has no pinned value`,
		`record 1 notices: pinned, command.system has 1 records`,
		`record 0 notices: command.system=<absent>, command.native=[]`,
		`record 0 notices[0].code: command.system="remote_command_not_stopped", command.native=<absent>`,
		`record 0 notices: command.system=<absent>, run.native=[]`,
		`record 0 notices[0].code: command.system="remote_command_not_stopped", run.native=<absent>`,
	}
	findings := compare(names, streams, nil, wrong...)
	if !reflect.DeepEqual(findings, want) {
		t.Fatalf("findings=%q", findings)
	}
}
