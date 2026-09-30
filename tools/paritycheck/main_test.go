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
