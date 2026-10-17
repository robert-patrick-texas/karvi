package auditview

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/robert-patrick-texas/karvi/internal/audit"
	"github.com/robert-patrick-texas/karvi/records"
)

// Args are journalctl's arguments for the flags (docs/EXAMPLES.md, chapter
// 64, issue 5): JSON entries holding the event's JSON alone, no pager, the
// options journalctl takes as given, and the matches. Every term names the
// trail's identifier and message ID, which tell an audit event from the
// daemon's log under the same identifier; the devices, lowercased, are one
// field given once per name, which journalctl ORs; and with operators the
// matches are two terms joined by +, the names as KARVI_OPERATOR in one and as
// KARVI_LOGINUSER in the other, so that a name matches either.
func Args(f *Flags) []string {
	args := []string{"--no-pager", "--output=json", "--output-fields=" + audit.FieldJSON}
	if *f.Follow {
		args = append(args, "--follow")
	}
	if *f.Reverse {
		args = append(args, "--reverse")
	}
	if *f.Grep != "" {
		args = append(args, "--grep="+*f.Grep)
	}
	if f.Lines.set {
		args = append(args, "--lines="+strconv.Itoa(f.Lines.n))
	}
	if *f.Since != "" {
		args = append(args, "--since="+*f.Since)
	}
	if *f.Until != "" {
		args = append(args, "--until="+*f.Until)
	}
	term := []string{"SYSLOG_IDENTIFIER=" + audit.Identifier, "MESSAGE_ID=" + audit.MessageID}
	for _, d := range *f.Devices {
		term = append(term, audit.FieldDevice+"="+strings.ToLower(d))
	}
	if len(*f.Operators) == 0 {
		return append(args, term...)
	}
	for i, field := range []string{audit.FieldOperator, audit.FieldLoginUser} {
		if i > 0 {
			args = append(args, "+")
		}
		args = append(args, term...)
		for _, o := range *f.Operators {
			args = append(args, field+"="+o)
		}
	}
	return args
}

// Row is one event's cells: the job's or the login's ID, the operator and
// the login user, the kind and the event of its name, the outcome, the
// device, and the statement.
type Row struct {
	ID, Operator, LoginUser, Mode, Event, Outcome, Device, Command string
}

// Decode is the row of one line journalctl wrote under --output=json, and
// false for an entry the reader cannot read: no KARVI_AUDIT_JSON, a field
// that is not one string (journalctl writes a repeated or a binary field as an
// array), JSON that does not decode, or an audit schema other than this
// tree's.
func Decode(line []byte) (Row, bool) {
	var entry map[string]json.RawMessage
	if json.Unmarshal(line, &entry) != nil {
		return Row{}, false
	}
	var text string
	if json.Unmarshal(entry[audit.FieldJSON], &text) != nil {
		return Row{}, false
	}
	var version struct {
		SchemaVersion int `json:"schema_version"`
	}
	if json.Unmarshal([]byte(text), &version) != nil || version.SchemaVersion != records.AuditSchemaVersion {
		return Row{}, false
	}
	var r records.AuditRecord
	if json.Unmarshal([]byte(text), &r) != nil {
		return Row{}, false
	}
	mode, event, _ := strings.Cut(r.EventName, ".")
	row := Row{ID: r.JobID, Operator: r.Operator.Username, LoginUser: r.LoginUser, Mode: mode, Event: event, Outcome: r.Outcome}
	if row.ID == "" {
		row.ID = r.LoginID
	}
	if r.Device != nil {
		row.Device = r.Device.CanonicalName
	}
	row.Command, _ = r.Action["command"].(string)
	return row, true
}
