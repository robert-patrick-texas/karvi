package executionplan

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// SensitivePropertyPattern names properties that may never appear in a
// safe record. The audit screen and Finding.Validate share it; records
// re-exports it.
var SensitivePropertyPattern = regexp.MustCompile(`(?i)(pass(word)?|secret|token|private[_-]?key|credential_value|output|transcript)`)

// Finding severities and stages.
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
	SeverityInfo    = "info"
)

var findingStages = map[string]bool{
	"client_config": true, "client_inventory": true, "client_dns": true, "client_credential": true,
	"daemon_probe": true, "daemon_policy": true, "daemon_dns": true, "credential_package": true,
	"transport": true, "host_key": true, "output": true, "capacity": true,
	"ping":     true, // the ICMP gate's capability
	"platform": true, // a platform resolution notice on the target
}

// Daemon readiness states.
const (
	DaemonRunning      = "running"
	DaemonDraining     = "draining"
	DaemonAbsent       = "absent"
	DaemonIncompatible = "incompatible"
)

// Finding is one validation result; always safe.
type Finding struct {
	Code     string            `json:"code"`
	Severity string            `json:"severity"`
	Stage    string            `json:"stage"`
	TargetID string            `json:"target_id,omitempty"`
	Message  string            `json:"message"`
	Details  map[string]string `json:"details,omitempty"`
}

func reportInvalid(field, format string, args ...any) error {
	return fmt.Errorf("plan_report_invalid: %s: %s", field, fmt.Sprintf(format, args...))
}

// Validate checks the finding; field prefixes the error's field name.
func (f Finding) Validate(field string) error {
	if err := identifier(field+".code", f.Code); err != nil {
		return reportInvalid(field+".code", "%v", err)
	}
	switch f.Severity {
	case SeverityError, SeverityWarning, SeverityInfo:
	default:
		return reportInvalid(field+".severity", "%q is not error, warning, or info", f.Severity)
	}
	if !findingStages[f.Stage] {
		return reportInvalid(field+".stage", "%q is not a known stage", f.Stage)
	}
	if strings.TrimSpace(f.Message) == "" {
		return reportInvalid(field+".message", "is required")
	}
	for k := range f.Details {
		if SensitivePropertyPattern.MatchString(k) {
			return reportInvalid(field+".details", "key %q names sensitive content", k)
		}
	}
	return nil
}

// DaemonReadiness is what the client learned from one execution endpoint's
// control plane.
type DaemonReadiness struct {
	ExecutionEndpoint string    `json:"execution_endpoint"`
	Audience          string    `json:"audience,omitempty"`
	Reachable         bool      `json:"reachable"`
	AutoLaunched      bool      `json:"auto_launched"`
	Status            string    `json:"status"`
	PID               int       `json:"pid,omitempty"`
	Version           string    `json:"version,omitempty"`
	IPCSchemaVersion  int       `json:"ipc_schema_version,omitempty"`
	PolicyDigest      string    `json:"execution_policy_digest,omitempty"`
	Capabilities      []string  `json:"capabilities"`
	Findings          []Finding `json:"findings"`
}

// Validate checks the readiness record.
func (d DaemonReadiness) Validate(field string) error {
	if d.ExecutionEndpoint != EndpointLocal {
		return reportInvalid(field+".execution_endpoint", "%q is not supported", d.ExecutionEndpoint)
	}
	switch d.Status {
	case DaemonRunning, DaemonDraining, DaemonAbsent, DaemonIncompatible:
	default:
		return reportInvalid(field+".status", "%q is not a readiness status", d.Status)
	}
	if d.Reachable && d.Status == DaemonAbsent || !d.Reachable && d.Status == DaemonRunning {
		return reportInvalid(field+".reachable", "does not agree with status %q", d.Status)
	}
	if d.Capabilities == nil {
		return reportInvalid(field+".capabilities", "must be present (empty allowed)")
	}
	if d.Findings == nil {
		return reportInvalid(field+".findings", "must be present (empty allowed)")
	}
	for i, f := range d.Findings {
		if err := f.Validate(fmt.Sprintf("%s.findings[%d]", field, i)); err != nil {
			return err
		}
	}
	return nil
}

// PreparationReport is the prepare_job result without the channel token;
// an exercise report embeds it.
type PreparationReport struct {
	PreparationID     string              `json:"preparation_id"`
	ExecutionEndpoint string              `json:"execution_endpoint"`
	Audience          string              `json:"audience"`
	PreparedAt        time.Time           `json:"prepared_at"`
	ExpiresAt         time.Time           `json:"expires_at"`
	DraftDigest       Digest              `json:"draft_digest"`
	PreparationDigest Digest              `json:"preparation_digest,omitzero"`
	Accepted          bool                `json:"accepted"`
	Evidence          PreparationEvidence `json:"evidence"`
	Daemon            DaemonReadiness     `json:"daemon"`
	Findings          []Finding           `json:"findings"`
}

// SumPreparation is the preparation digest: the report with its own digest
// cleared in both places it appears (the report and its evidence copy).
func SumPreparation(r PreparationReport) (Digest, error) {
	r.PreparationDigest = Digest{}
	r.Evidence.PreparationDigest = Digest{}
	return SumJSON(r)
}

// Validate checks the report against its own evidence and, when accepted,
// its digest.
func (r PreparationReport) Validate(field string) error {
	if !ValidID(r.PreparationID) {
		return reportInvalid(field+".preparation_id", "%q is not a valid identifier", r.PreparationID)
	}
	if r.ExecutionEndpoint != EndpointLocal {
		return reportInvalid(field+".execution_endpoint", "%q is not supported", r.ExecutionEndpoint)
	}
	if err := identifier(field+".audience", r.Audience); err != nil {
		return reportInvalid(field+".audience", "%v", err)
	}
	if r.PreparedAt.IsZero() || !r.PreparedAt.Before(r.ExpiresAt) {
		return reportInvalid(field+".expires_at", "must follow prepared_at")
	}
	if r.DraftDigest.IsZero() {
		return reportInvalid(field+".draft_digest", "is required")
	}
	if r.Evidence.ExecutionEndpoint != r.ExecutionEndpoint || r.Evidence.PreparationID != r.PreparationID {
		return reportInvalid(field+".evidence", "names a different endpoint or preparation")
	}
	if r.Evidence.PreparationDigest != r.PreparationDigest {
		return reportInvalid(field+".evidence.preparation_digest", "differs from the report's digest")
	}
	if r.Daemon.ExecutionEndpoint != r.ExecutionEndpoint {
		return reportInvalid(field+".daemon.execution_endpoint", "differs from the report's endpoint")
	}
	if err := r.Daemon.Validate(field + ".daemon"); err != nil {
		return err
	}
	if r.Findings == nil {
		return reportInvalid(field+".findings", "must be present (empty allowed)")
	}
	for i, f := range r.Findings {
		if err := f.Validate(fmt.Sprintf("%s.findings[%d]", field, i)); err != nil {
			return err
		}
	}
	if r.Accepted {
		if r.PreparationDigest.IsZero() {
			return reportInvalid(field+".preparation_digest", "is required when accepted")
		}
		sum, err := SumPreparation(r)
		if err != nil {
			return reportInvalid(field+".preparation_digest", "cannot encode: %v", err)
		}
		if sum != r.PreparationDigest {
			return reportInvalid(field+".preparation_digest", "recorded %s, computed %s", r.PreparationDigest, sum)
		}
	}
	return nil
}

// Seal fills the preparation digest of an accepted report and mirrors it
// into the evidence.
func Seal(r PreparationReport) (PreparationReport, error) {
	r.PreparationDigest = Digest{}
	r.Evidence.PreparationDigest = Digest{}
	sum, err := SumPreparation(r)
	if err != nil {
		return r, err
	}
	r.PreparationDigest = sum
	r.Evidence.PreparationDigest = sum
	return r, nil
}
