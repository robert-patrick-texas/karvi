// Package audit emits canonical, secret-screened records to journald and an
// optional append-only user file. Sink health is explicit.
package audit

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
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
}
type Status struct {
	Journald bool     `json:"journald"`
	File     bool     `json:"file"`
	Warnings []string `json:"warnings"`
}

// New opens the sinks: journald, and audit.file when set, its path by
// osutil.ResolvePath (~ home, the operator's from the password database; a
// relative path from the working directory), its folder made 0700 when
// missing.
func New(cfg configload.Snapshot, home string) (*Sink, error) {
	s := &Sink{journalRequired: cfg.Bool("audit.journald-required"), fileRequired: cfg.Bool("audit.file-required")}
	addr := &net.UnixAddr{Name: "/run/systemd/journal/socket", Net: "unixgram"}
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
	return Status{Journald: s.journal != nil, File: s.file != nil, Warnings: append([]string(nil), s.warnings...)}
}
func (s *Sink) WriteAudit(r records.AuditRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// The sink writes every audit line, so it stamps the version the line
	// is written in.
	r.SchemaVersion = records.AuditSchemaVersion
	if err := screen(r); err != nil {
		return err
	}
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	var errs []string
	if s.journal != nil {
		payload := fmt.Sprintf("MESSAGE=karvi audit %s outcome=%s\nPRIORITY=%d\nSYSLOG_IDENTIFIER=karvi\nMESSAGE_ID=9cb838d10f074d8a928e9f7c2f98450a\nKARVI_EVENT_NAME=%s\nKARVI_ACTIVITY_ID=%s\nKARVI_JOB_ID=%s\nKARVI_AUDIT_JSON=%s\n", clean(r.EventName), clean(r.Outcome), priority(r.Severity), clean(r.EventName), clean(r.ActivityID), clean(r.JobID), clean(string(data)))
		if _, err := s.journal.Write([]byte(payload)); err != nil {
			errs = append(errs, "journald: "+err.Error())
		}
	} else if s.journalRequired {
		errs = append(errs, "journald required but unavailable")
	}
	if s.file != nil {
		if _, err := s.file.Write(append(data, '\n')); err != nil {
			errs = append(errs, "file: "+err.Error())
		} else if err := s.file.Sync(); err != nil {
			errs = append(errs, "file sync: "+err.Error())
		}
	} else if s.fileRequired {
		errs = append(errs, "file required but unavailable")
	}
	if len(errs) > 0 {
		return errorcodes.Errorf("audit_write_failed", "audit sink failure: %s", strings.Join(errs, "; "))
	}
	return nil
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
			if forbidden.MatchString(k) && !allowedKey(p) {
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
func allowedKey(path string) bool {
	return strings.HasSuffix(path, "command_sha256") || strings.HasSuffix(path, "command_sha256_array") || strings.HasSuffix(path, "command_count") || strings.HasSuffix(path, "transcript_recording")
}
