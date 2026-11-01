// Package audit emits canonical, secret-screened records to journald and an
// optional append-only user file. Sink health is explicit.
package audit

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/sys/unix"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
	"github.com/robert-patrick-texas/karvi/platform"
	"github.com/robert-patrick-texas/karvi/records"
)

// forbidden is the one sensitive-property pattern shared with plan-report
// findings.
var forbidden = records.SensitivePropertyPattern

type Sink struct {
	mu                            sync.Mutex
	journal                       *net.UnixConn
	file                          *os.File
	journalRequired, fileRequired bool
	warnings                      []string
	// journalWarned and fileWarned: the sink's warnings already name a
	// failed write of that sink, which is not required.
	journalWarned, fileWarned bool
	// masks hide the secrets of every statement an event carries
	// (SetMasks).
	masks []*regexp.Regexp
}
type Status struct {
	Journald bool     `json:"journald"`
	File     bool     `json:"file"`
	Warnings []string `json:"warnings"`
}

// journalSocket is journald's native socket; a test points it at a fake.
var journalSocket = "/run/systemd/journal/socket"

// New opens the sinks: journald, and audit.file when set, its path by
// osutil.ResolvePath (~ home, the operator's from the password database; a
// relative path from the working directory), its folder made 0700 when
// missing.
func New(cfg configload.Snapshot, home string) (*Sink, error) {
	s := &Sink{journalRequired: cfg.Bool("audit.journald-required"), fileRequired: cfg.Bool("audit.file-required")}
	addr := &net.UnixAddr{Name: journalSocket, Net: "unixgram"}
	conn, err := net.DialUnix("unixgram", nil, addr)
	if err != nil {
		if s.journalRequired {
			return nil, fmt.Errorf("audit_journald_unavailable: %w", err)
		}
		s.warnings = append(s.warnings, "journald audit sink unavailable: "+err.Error())
	} else {
		s.journal = conn
	}
	if path := cfg.String("audit.file"); path != "" {
		path, err := osutil.ResolvePath(path, home)
		if err != nil {
			return nil, err
		}
		if err := osutil.CheckSetupPlaces(filepath.Dir(path), "audit.file"); err != nil {
			return nil, err
		}
		if err := osutil.MakeDirectories(filepath.Dir(path), 0700); err != nil {
			return nil, errorcodes.Errorf("audit_directory_create_failed", "create audit directory: %w", err)
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			if s.fileRequired {
				return nil, fmt.Errorf("audit_file_unavailable: %w", err)
			}
			s.warnings = append(s.warnings, "audit file unavailable: "+err.Error())
		} else {
			s.file = f
		}
	} else if s.fileRequired {
		return nil, errorcodes.Errorf("config_audit_file_required_missing", "audit.file is required but empty")
	}
	return s, nil
}
func (s *Sink) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Status{Journald: s.journal != nil, File: s.file != nil, Warnings: append([]string(nil), s.warnings...)}
}

// SetMasks makes the sink mask every statement an event carries,
// action.command and action.commands, by res (platform.Mask), in journald
// and audit.file alike: a job's sink takes the plan's masks of every target
// platform together (docs/EXAMPLES.md, chapter 63, issue 8). Called before
// the first event.
func (s *Sink) SetMasks(res []*regexp.Regexp) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.masks = res
}

// WriteAudit writes the event to every open sink. A write that fails on a
// required sink, or a required sink that is not open, is audit_write_failed;
// one that fails on a sink that is not required fails nothing, and the
// sink's first such failure is one of its warnings (Status).
func (s *Sink) WriteAudit(r records.AuditRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// The sink writes every audit line, so it stamps the version the line
	// is written in.
	r.SchemaVersion = records.AuditSchemaVersion
	r.Action = maskAction(r.Action, s.masks)
	if err := screen(r); err != nil {
		return err
	}
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	var errs []string
	if s.journal != nil {
		if err := s.writeJournal(journalEntry(r, data)); err != nil {
			if s.journalRequired {
				errs = append(errs, "journald: "+err.Error())
			} else if !s.journalWarned {
				s.journalWarned = true
				s.warnings = append(s.warnings, "journald audit sink: "+err.Error())
			}
		}
	} else if s.journalRequired {
		errs = append(errs, "journald required but unavailable")
	}
	if s.file != nil {
		err := func() error {
			if _, err := s.file.Write(append(data, '\n')); err != nil {
				return err
			}
			return s.file.Sync()
		}()
		if err != nil {
			if s.fileRequired {
				errs = append(errs, "file: "+err.Error())
			} else if !s.fileWarned {
				s.fileWarned = true
				s.warnings = append(s.warnings, "audit file: "+err.Error())
			}
		}
	} else if s.fileRequired {
		errs = append(errs, "file required but unavailable")
	}
	if len(errs) > 0 {
		return errorcodes.Errorf("audit_write_failed", "audit sink failure: %s", strings.Join(errs, "; "))
	}
	return nil
}

// writeJournal sends an entry to journald as its native protocol does: one
// datagram, or, an entry the socket refuses by its size (EMSGSIZE, ENOBUFS),
// an empty datagram carrying a memfd that holds the entry, sealed against
// every change, as sd_journal_send passes one.
func (s *Sink) writeJournal(entry []byte) error {
	_, err := s.journal.Write(entry)
	if !errors.Is(err, unix.EMSGSIZE) && !errors.Is(err, unix.ENOBUFS) {
		return err
	}
	fd, err := unix.MemfdCreate("karvi-audit", unix.MFD_ALLOW_SEALING|unix.MFD_CLOEXEC)
	if err != nil {
		return os.NewSyscallError("memfd_create", err)
	}
	f := os.NewFile(uintptr(fd), "karvi-audit")
	defer f.Close()
	if _, err := f.Write(entry); err != nil {
		return err
	}
	if _, err := unix.FcntlInt(f.Fd(), unix.F_ADD_SEALS, unix.F_SEAL_SHRINK|unix.F_SEAL_GROW|unix.F_SEAL_WRITE|unix.F_SEAL_SEAL); err != nil {
		return os.NewSyscallError("fcntl F_ADD_SEALS", err)
	}
	// WriteMsgUnix refuses a connected datagram socket, so the descriptor
	// goes by the socket's own sendmsg.
	rc, err := s.journal.SyscallConn()
	if err != nil {
		return err
	}
	rights := unix.UnixRights(int(f.Fd()))
	var sendErr error
	if err := rc.Write(func(sock uintptr) bool {
		sendErr = unix.Sendmsg(int(sock), nil, rights, nil, 0)
		return sendErr != unix.EAGAIN
	}); err != nil {
		return err
	}
	return os.NewSyscallError("sendmsg", sendErr)
}
func (s *Sink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var err error
	if s.journal != nil {
		err = s.journal.Close()
	}
	if s.file != nil {
		if e := s.file.Close(); err == nil {
			err = e
		}
	}
	return err
}

// The journald entry's identifier, its message ID, which tells an audit
// event from the daemon's log under the same identifier, and the fields a
// reader matches or reads (karvi-audit, internal/auditview).
const (
	Identifier     = "karvi"
	MessageID      = "9cb838d10f074d8a928e9f7c2f98450a"
	FieldOperator  = "KARVI_OPERATOR"
	FieldLoginUser = "KARVI_LOGINUSER"
	FieldDevice    = "KARVI_DEVICE"
	FieldJSON      = "KARVI_AUDIT_JSON"
)

// journalEntry is the event's journald entry: the line as its MESSAGE, and
// the fields a match names whole (journalctl KARVI_DEVICE=NAME), each only
// where the event has a value.
func journalEntry(r records.AuditRecord, data []byte) []byte {
	var b strings.Builder
	field := func(k, v string) {
		if v != "" {
			b.WriteString(k + "=" + clean(v) + "\n")
		}
	}
	field("MESSAGE", Line(r))
	field("PRIORITY", strconv.Itoa(priority(r.Severity)))
	field("SYSLOG_IDENTIFIER", Identifier)
	field("MESSAGE_ID", MessageID)
	field("KARVI_EVENT_NAME", r.EventName)
	field(FieldOperator, r.Operator.Username)
	field(FieldLoginUser, r.LoginUser)
	field("KARVI_JOB_ID", r.JobID)
	field("KARVI_LOGIN_ID", r.LoginID)
	if r.Device != nil {
		field(FieldDevice, r.Device.CanonicalName)
	}
	field(FieldJSON, string(data))
	return []byte(b.String())
}

// Line is the event's message, the line syslog carries: key=value pairs in
// one order, `karvi.audit=<kind>.<event>` first, a pair left out where the
// event has no value, and a device command's statement last, quoted always
// (docs/EXAMPLES.md, chapter 63, issue 6).
func Line(r records.AuditRecord) string {
	var b strings.Builder
	pair := func(k, v string) {
		if v == "" {
			return
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(k + "=" + value(v))
	}
	pair("karvi.audit", r.EventName)
	pair("operator", r.Operator.Username)
	pair("loginuser", r.LoginUser)
	pair("outcome", r.Outcome)
	pair("job", r.JobID)
	pair("login", r.LoginID)
	if r.Device != nil {
		pair("device", r.Device.CanonicalName)
	}
	if c, ok := r.Action["command"].(string); ok {
		b.WriteString(" command=" + strconv.Quote(c))
	}
	return b.String()
}

// value is v bare, or quoted with Go's escapes where a space, a quote, an
// equals sign, a backslash, or a control character would end the pair early.
func value(v string) string {
	if strings.ContainsFunc(v, func(c rune) bool { return c <= ' ' || c == '"' || c == '=' || c == '\\' || c == 0x7f }) {
		return strconv.Quote(v)
	}
	return v
}

func priority(severity string) int {
	switch severity {
	case "critical":
		return 2
	case "error":
		return 3
	case "warning":
		return 4
	default:
		return 6
	}
}
func clean(s string) string { return strings.NewReplacer("\n", " ", "\r", " ", "\x00", "").Replace(s) }

// maskAction is an event's action with its statements masked: a copy, so
// that the caller's map and the plan's command list stay as sent.
func maskAction(action map[string]any, res []*regexp.Regexp) map[string]any {
	if len(res) == 0 || action == nil {
		return action
	}
	out := maps.Clone(action)
	if c, ok := out["command"].(string); ok {
		out["command"] = platform.Mask(res, c)
	}
	if list, ok := out["commands"].([]string); ok {
		masked := make([]string, len(list))
		for i, c := range list {
			masked[i] = platform.Mask(res, c)
		}
		out["commands"] = masked
	}
	return out
}

func screen(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var x any
	if err := json.Unmarshal(data, &x); err != nil {
		return err
	}
	return walk(x, "")
}
func walk(v any, path string) error {
	switch x := v.(type) {
	case map[string]any:
		for k, v := range x {
			p := k
			if path != "" {
				p = path + "." + k
			}
			if forbidden.MatchString(k) {
				return errorcodes.Errorf("audit_sensitive_property_rejected", "audit schema rejected sensitive property %s", p)
			}
			if err := walk(v, p); err != nil {
				return err
			}
		}
	case []any:
		for _, v := range x {
			if err := walk(v, path); err != nil {
				return err
			}
		}
	}
	return nil
}
