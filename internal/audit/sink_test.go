package audit

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/testdir"
	"github.com/robert-patrick-texas/karvi/records"
)

// fakeJournal is a unixgram socket in a short directory, the journal socket
// a sink made by New dials for the rest of the test.
func fakeJournal(t *testing.T) *net.UnixConn {
	t.Helper()
	path := filepath.Join(testdir.Short(t), "socket")
	l, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: path, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	old := journalSocket
	journalSocket = path
	t.Cleanup(func() { journalSocket = old })
	return l
}

// journalSink is a sink made by New with journald required or not and no
// audit file, its journal the fake.
func journalSink(t *testing.T, required bool) *Sink {
	t.Helper()
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: []string{"audit.journald-required=" + strconv.FormatBool(required)}})
	if err != nil {
		t.Fatal(err)
	}
	sink, err := New(cfg, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sink.Close() })
	if sink.journal == nil {
		t.Fatalf("the sink did not dial the fake journal: %v", sink.Status())
	}
	return sink
}

// fullFile is a file every write to fails, as a full volume's does.
func fullFile(t *testing.T) *os.File {
	t.Helper()
	f, err := os.OpenFile("/dev/full", os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// received is one datagram the fake journal took: its bytes, or the bytes
// of the file it carried and that file's seals.
type received struct {
	data      []byte
	file      bool
	seals     int
	datagramN int
}

func receive(t *testing.T, l *net.UnixConn) received {
	t.Helper()
	if err := l.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64<<10)
	oob := make([]byte, unix.CmsgSpace(4*4))
	n, oobn, _, _, err := l.ReadMsgUnix(buf, oob)
	if err != nil {
		t.Fatal(err)
	}
	got := received{data: buf[:n], datagramN: n}
	if oobn == 0 {
		return got
	}
	msgs, err := unix.ParseSocketControlMessage(oob[:oobn])
	if err != nil || len(msgs) != 1 {
		t.Fatalf("control messages: %d %v", len(msgs), err)
	}
	fds, err := unix.ParseUnixRights(&msgs[0])
	if err != nil || len(fds) != 1 {
		t.Fatalf("descriptors: %v %v", fds, err)
	}
	f := os.NewFile(uintptr(fds[0]), "journal-entry")
	defer f.Close()
	got.file = true
	if got.seals, err = unix.FcntlInt(f.Fd(), unix.F_GET_SEALS, 0); err != nil {
		t.Fatalf("the descriptor's seals: %v", err)
	}
	info, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	got.data = make([]byte, info.Size())
	if _, err := f.ReadAt(got.data, 0); err != nil {
		t.Fatal(err)
	}
	return got
}

// journalField is the value of field k in a journal entry, "" where the
// entry has none.
func journalField(entry []byte, k string) string {
	for _, line := range strings.Split(string(entry), "\n") {
		if v, ok := strings.CutPrefix(line, k+"="); ok {
			return v
		}
	}
	return ""
}

// TestJournalTakesALargeEntryAsASealedMemfd: an entry the socket takes goes
// as one datagram; one larger than the socket's send buffer goes as
// journald's protocol has it, an empty datagram carrying a memfd sealed
// against every change, holding the entry whole. The sink's send buffer is
// set small, so that the bound does not hang on the host's
// net.core.wmem_default; the large entry, 6,000 statements, is past the
// default too.
func TestJournalTakesALargeEntryAsASealedMemfd(t *testing.T) {
	l := fakeJournal(t)
	sink := journalSink(t, true)
	if err := sink.journal.SetWriteBuffer(32 << 10); err != nil {
		t.Fatal(err)
	}
	op := records.Operator{Username: "jsmith", UID: 1000}
	if err := sink.WriteAudit(records.AuditRecord{EventID: "1", EventName: "run.command", Operator: op, JobID: "261101-090000-00", Device: &records.AuditDevice{CanonicalName: "router1"}, Action: map[string]any{"command": "show clock"}}); err != nil {
		t.Fatal(err)
	}
	small := receive(t, l)
	if small.file || !strings.HasPrefix(string(small.data), "MESSAGE=karvi.audit=run.command operator=jsmith job=261101-090000-00 device=router1 command=\"show clock\"\n") {
		t.Fatalf("the small entry: file %v, %q", small.file, small.data)
	}
	commands := make([]string, 6000)
	for i := range commands {
		commands[i] = fmt.Sprintf("ip prefix-list PL-CUST seq %d permit 192.0.2.0/24 le 32", (i+1)*5)
	}
	if err := sink.WriteAudit(records.AuditRecord{EventID: "2", EventName: "cmd.start", Operator: op, JobID: "261101-090100-00", Action: map[string]any{"mode": "cmd", "commands": commands}}); err != nil {
		t.Fatal(err)
	}
	large := receive(t, l)
	if !large.file || large.datagramN != 0 {
		t.Fatalf("the large entry came as a datagram of %d bytes, not a descriptor", large.datagramN)
	}
	if want := unix.F_SEAL_SHRINK | unix.F_SEAL_GROW | unix.F_SEAL_WRITE | unix.F_SEAL_SEAL; large.seals != want {
		t.Fatalf("seals %#x, want %#x", large.seals, want)
	}
	if len(large.data) < 300_000 {
		t.Fatalf("the entry is %d bytes", len(large.data))
	}
	var r records.AuditRecord
	if err := json.Unmarshal([]byte(journalField(large.data, FieldJSON)), &r); err != nil {
		t.Fatalf("the entry's JSON: %v", err)
	}
	if list, _ := r.Action["commands"].([]any); len(list) != len(commands) || list[len(list)-1] != commands[len(commands)-1] {
		t.Fatalf("the entry's statements: %d", len(list))
	}
	if m, id, mid := journalField(large.data, "MESSAGE"), journalField(large.data, "SYSLOG_IDENTIFIER"), journalField(large.data, "MESSAGE_ID"); m != "karvi.audit=cmd.start operator=jsmith job=261101-090100-00" || id != Identifier || mid != MessageID {
		t.Fatalf("the entry's fields: %q %q %q", m, id, mid)
	}
}

// TestUnrequiredSinkWriteFailureIsOneWarning: a write that fails on a sink
// that is not required, journald's or the audit file's, fails no event; the
// sink's status carries one warning per sink however many writes fail.
func TestUnrequiredSinkWriteFailureIsOneWarning(t *testing.T) {
	l := fakeJournal(t)
	sink := journalSink(t, false)
	sink.file = fullFile(t)
	l.Close()
	for i := range 2 {
		if err := sink.WriteAudit(records.AuditRecord{EventID: strconv.Itoa(i), EventName: "run.start"}); err != nil {
			t.Fatalf("write %d: %v", i+1, err)
		}
	}
	st := sink.Status()
	if !st.Journald || !st.File || len(st.Warnings) != 2 || !strings.HasPrefix(st.Warnings[0], "journald audit sink: ") || !strings.HasPrefix(st.Warnings[1], "audit file: ") || !strings.Contains(st.Warnings[1], "/dev/full") {
		t.Fatalf("status: %#v", st)
	}
}

// TestRequiredSinkWriteFailureFailsTheWrite: a write that fails on a
// required sink is audit_write_failed, naming the sink, and no warning.
func TestRequiredSinkWriteFailureFailsTheWrite(t *testing.T) {
	l := fakeJournal(t)
	journal := journalSink(t, true)
	l.Close()
	if err := journal.WriteAudit(records.AuditRecord{EventID: "1", EventName: "run.start"}); errorcodes.Of(err) != "audit_write_failed" || !strings.Contains(err.Error(), "journald: ") {
		t.Fatalf("journald required: %v", err)
	}
	file := &Sink{file: fullFile(t), fileRequired: true}
	defer file.Close()
	if err := file.WriteAudit(records.AuditRecord{EventID: "2", EventName: "run.start"}); errorcodes.Of(err) != "audit_write_failed" || !strings.Contains(err.Error(), "file: ") {
		t.Fatalf("audit file required: %v", err)
	}
	if w := append(journal.Status().Warnings, file.Status().Warnings...); len(w) != 0 {
		t.Fatalf("warnings beside the failure: %v", w)
	}
}
