// Package records contains the nonsecret JSON contracts shared by the CLI,
// daemon, artifact writers, and external Go integrations.
package records

import (
	"time"

	"github.com/robert-patrick-texas/karvi/credentialpackage"
	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

const (
	CommandSchemaVersion    = 2
	ScoreboardSchemaVersion = 3 // 2: mode, targets with states, inputs, commands, collection, metrics, daemon; 3: the target row's bytes and the metrics' in_flight_bytes
	AuditSchemaVersion      = 1
	JobSchemaVersion        = 2
	MetricsSchemaVersion    = 1
)

type StructuredError struct {
	Code        string         `json:"code"`
	Category    string         `json:"category"`
	Message     string         `json:"message"`
	Operation   string         `json:"operation"`
	Retryable   bool           `json:"retryable"`
	External    bool           `json:"external"`
	CauseCode   any            `json:"cause_code,omitempty"`
	Details     map[string]any `json:"details,omitempty"`
	Remediation string         `json:"remediation,omitempty"`
}

type Operator struct {
	Username   string   `json:"username"`
	UID        int      `json:"uid"`
	PrimaryGID int      `json:"primary_gid,omitempty"`
	Groups     []string `json:"groups,omitempty"`
}

type DeviceProjection struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	CanonicalName  string   `json:"canonical_name"`
	Site           string   `json:"site,omitempty"`
	Groups         []string `json:"groups"`
	RiskTier       string   `json:"risk_tier,omitempty"`
	DeploymentRing string   `json:"deployment_ring,omitempty"`
	TopologyDomain string   `json:"topology_domain,omitempty"`
}

type CredentialProjection struct {
	Policy         string         `json:"policy"`
	DeviceUsername string         `json:"device_username"`
	Backend        string         `json:"backend"`
	MatchedOn      map[string]any `json:"matched_on"`
	// CredentialID and Protection name the grant and package that carried
	// the credential.
	CredentialID string `json:"credential_id,omitempty"`
	Protection   string `json:"protection,omitempty"`
}

// Notice is a non-fatal fact about a record, carried on it (the ICMP
// gate's packet loss, a session-init command that failed under continue,
// the follow stream leaving an output out). Code is a notice row of the
// error-code registry.
type Notice struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// OutputOmitted reports the record's follow_output_omitted notice, if the
// follow stream sent the record with its output left out because its
// frame would pass daemon.max-ipc-frame-bytes (the daemon puts the notice
// on the copy it sends). The text
// renderer prints the message where the output would stand; the record's
// output_bytes and output_sha256 describe the output that was left out.
func (r *CommandRecord) OutputOmitted() *Notice {
	for i := range r.Notices {
		if r.Notices[i].Code == "follow_output_omitted" {
			return &r.Notices[i]
		}
	}
	return nil
}

// PingReport is the ICMP gate's result for one device. Outcomes has one
// entry per probe.
type PingReport struct {
	Address           string        `json:"address"`
	Family            string        `json:"family"`
	Method            string        `json:"method"`
	ExecutionEndpoint string        `json:"execution_endpoint"`
	Probes            int           `json:"probes"`
	TimeoutNS         int64         `json:"timeout_ns"`
	Outcomes          []PingOutcome `json:"outcomes"`
	Replies           int           `json:"replies"`
	Losses            int           `json:"losses"`
	Decision          string        `json:"decision"`
	TotalNS           int64         `json:"total_ns"`
}

// PingOutcome is one probe: reply with its round-trip time, timeout, or
// error with the answering address and detail.
type PingOutcome struct {
	Sequence int       `json:"sequence"`
	SentAt   time.Time `json:"sent_at"`
	Status   string    `json:"status"`
	RTTNS    *int64    `json:"rtt_ns"`
	From     string    `json:"from,omitempty"`
	Detail   string    `json:"detail,omitempty"`
}

// Ping statuses, decisions, and the two record statuses the gate emits.
const (
	PingProbes = 2

	PingReply   = "reply"
	PingTimeout = "timeout"
	PingError   = "error"

	PingDecisionProceed               = "proceed"
	PingDecisionProceedDegraded       = "proceed_degraded"
	PingDecisionSkip                  = "skip"
	PingDecisionCapabilityUnavailable = "capability_unavailable"

	StatusICMPUnreachable           = "icmp_unreachable"
	StatusICMPCapabilityUnavailable = "icmp_capability_unavailable"
)

var (
	pingOutcomeStatuses = map[string]bool{PingReply: true, PingTimeout: true, PingError: true}
	pingDecisions       = map[string]bool{PingDecisionProceed: true, PingDecisionProceedDegraded: true, PingDecisionSkip: true, PingDecisionCapabilityUnavailable: true}
)

// Validate checks the object's internal consistency: two probes, one
// outcome per probe in sequence order with a known status, a reply count
// that matches the outcomes, losses the remainder, and a known decision.
func (p *PingReport) Validate() error {
	if p.Probes != PingProbes || len(p.Outcomes) != PingProbes {
		return errorcodes.Errorf("record_ping_invalid", "ping carries %d probes and %d outcomes, want %d", p.Probes, len(p.Outcomes), PingProbes)
	}
	replies := 0
	for i, o := range p.Outcomes {
		if o.Sequence != i+1 || !pingOutcomeStatuses[o.Status] {
			return errorcodes.Errorf("record_ping_invalid", "ping outcome %d has sequence %d and status %q", i+1, o.Sequence, o.Status)
		}
		if (o.Status == PingReply) != (o.RTTNS != nil) {
			return errorcodes.Errorf("record_ping_invalid", "ping outcome %d: a reply carries rtt_ns and nothing else does", i+1)
		}
		if o.Status == PingReply {
			replies++
		}
	}
	if p.Replies != replies || p.Losses != PingProbes-replies {
		return errorcodes.Errorf("record_ping_invalid", "ping counts replies=%d losses=%d, outcomes say %d and %d", p.Replies, p.Losses, replies, PingProbes-replies)
	}
	if !pingDecisions[p.Decision] || p.TimeoutNS <= 0 || p.Address == "" || p.Method == "" {
		return errorcodes.Errorf("record_ping_invalid", "ping decision %q, timeout %d, address %q, method %q", p.Decision, p.TimeoutNS, p.Address, p.Method)
	}
	return nil
}

type DispatchContext struct {
	Mode              string `json:"mode"`
	ServerID          string `json:"server_id"`
	WaveNumber        int    `json:"wave_number"`
	WaveWidth         int    `json:"wave_width"`
	WaveDepth         int    `json:"wave_depth"`
	WorkerID          string `json:"worker_id"`
	ScopePosition     int    `json:"scope_position"`
	DesiredWidth      int    `json:"desired_width"`
	EffectiveInflight int    `json:"effective_inflight"`
	HaltContext       any    `json:"halt_context,omitempty"`
}

type Timing struct {
	QueuedAt             time.Time  `json:"queued_at"`
	DeviceStartedAt      *time.Time `json:"device_started_at"`
	CommandStartedAt     *time.Time `json:"command_started_at"`
	EndedAt              time.Time  `json:"ended_at"`
	TotalNS              int64      `json:"total_ns"`
	SchedulerWaitNS      *int64     `json:"scheduler_wait_ns"`
	ServerCapacityWaitNS *int64     `json:"server_capacity_wait_ns"`
	DeviceCapacityWaitNS *int64     `json:"device_capacity_wait_ns"`
	DNSNS                *int64     `json:"dns_ns"`
	CredentialNS         *int64     `json:"credential_ns"`
	PingNS               *int64     `json:"ping_ns"` // the ICMP gate's duration; null when not reached
	ConnectNS            *int64     `json:"connect_ns"`
	AuthenticateNS       *int64     `json:"authenticate_ns"`
	PrivilegeNS          *int64     `json:"privilege_ns"`
	SessionInitNS        *int64     `json:"session_init_ns"`
	DeviceResponseNS     *int64     `json:"device_response_ns"`
	OutputPersistNS      *int64     `json:"output_persist_ns"`
}

type CommandRecord struct {
	SchemaVersion     int              `json:"schema_version"`
	RecordID          string           `json:"record_id"`
	JobID             string           `json:"job_id,omitempty"`
	ActivityID        string           `json:"activity_id"`
	ActivityType      string           `json:"activity_type"`
	Sequence          int64            `json:"sequence"`
	Operator          Operator         `json:"operator"`
	Device            DeviceProjection `json:"device"`
	InputTarget       string           `json:"input_target"`
	TransformedName   string           `json:"transformed_name"`
	DNSQueryName      string           `json:"dns_query_name,omitempty"`
	DNSSuffixAction   string           `json:"dns_suffix_action"`
	AddressCandidates []string         `json:"address_candidates"`
	SelectedAddress   string           `json:"selected_address,omitempty"`
	AddressFamily     string           `json:"address_family,omitempty"`
	AddressSource     string           `json:"address_source,omitempty"` // inventory | dns-client | dns-daemon
	// The address-plan fields.
	AddressAuthority        string                `json:"address_authority,omitempty"`
	ClientAddressCandidates []string              `json:"client_address_candidates,omitempty"`
	DaemonAddressCandidates []string              `json:"daemon_address_candidates,omitempty"`
	AlternateAddresses      []string              `json:"alternate_addresses,omitempty"`
	AddressResolutionActor  string                `json:"address_resolution_actor,omitempty"`
	Platform                string                `json:"platform"`
	Transport               string                `json:"transport"`
	Port                    uint16                `json:"port"`
	ConnectionReused        *bool                 `json:"connection_reused"`
	Credential              *CredentialProjection `json:"credential,omitempty"`
	SessionInitProfile      string                `json:"session_init_profile,omitempty"`
	// Ping is the ICMP gate's result for the device: null when the
	// gate is disabled or not reached, the same object on every record of a
	// gated device.
	Ping           *PingReport     `json:"ping"`
	Dispatch       DispatchContext `json:"dispatch"`
	DispatchOrder  string          `json:"dispatch_order"`            // default, sorted, shuffle, random
	ShuffleKey     *string         `json:"shuffle_key,omitempty"`     // shuffle and random only; may be empty
	CandidateCount int             `json:"candidate_count,omitempty"` // command: devices in the ordered target set
	CommandIndex   int             `json:"command_index"`
	CommandCount   int             `json:"command_count"`
	CommandKind    string          `json:"command_kind"`
	Command        string          `json:"command"`
	CommandSHA256  string          `json:"command_sha256"`
	Status         string          `json:"status"`
	Output         string          `json:"output"`
	OutputEncoding string          `json:"output_encoding"`
	OutputBytes    int64           `json:"output_bytes"`
	OutputSHA256   string          `json:"output_sha256"`
	// PromptBefore is the prompt the device showed when the statement was
	// sent; Prompt is the one that came back after it. Both are the device's
	// own bytes. output.TARGET.txt prints PromptBefore and the statement on
	// one line. Empty when nothing was sent.
	PromptBefore   string           `json:"promptbefore"`
	Prompt         string           `json:"prompt"`
	PromptSource   string           `json:"prompt_source"`
	PromptObserved *bool            `json:"prompt_observed"`
	Notices        []Notice         `json:"notices"`
	Timing         Timing           `json:"timing"`
	Error          *StructuredError `json:"error"`
	RetryEligible  bool             `json:"retry_eligible"`
}

var terminalStatuses = map[string]bool{"succeeded": true, "device_error": true, "connection_error": true, "authentication_error": true, "privilege_error": true, "timeout": true, "output_limit_exceeded": true, "output_error": true, "not_attempted_prior_command_failure": true, "not_attempted_session_init_failure": true, "not_started_halt": true, "not_started_wave_gate": true, "cancelled": true, "incomplete_shutdown": true, "incomplete_daemon_recovery": true, StatusICMPUnreachable: true, StatusICMPCapabilityUnavailable: true}

func (r *CommandRecord) Validate() error {
	if r.SchemaVersion == 0 {
		r.SchemaVersion = CommandSchemaVersion
	}
	if r.SchemaVersion != CommandSchemaVersion {
		return errorcodes.Errorf("record_schema_unsupported", "unsupported command record schema %d", r.SchemaVersion)
	}
	if r.RecordID == "" || r.ActivityID == "" {
		return errorcodes.Errorf("record_identity_missing", "record_id and activity_id are required")
	}
	if r.ActivityType != "run" && r.ActivityType != "command" {
		return errorcodes.Errorf("record_activity_type_invalid", "invalid activity_type %q", r.ActivityType)
	}
	if r.ActivityType == "run" && r.JobID == "" {
		return errorcodes.Errorf("record_job_id_missing", "run record requires job_id")
	}
	if r.Sequence < 1 {
		return errorcodes.Errorf("record_sequence_invalid", "sequence must be at least 1")
	}
	if r.CommandKind != "requested" && r.CommandKind != "session_init" {
		return errorcodes.Errorf("record_command_kind_invalid", "invalid command_kind %q", r.CommandKind)
	}
	if r.CommandKind == "session_init" && (r.SessionInitProfile == "" || r.SessionInitProfile == "none") {
		return errorcodes.Errorf("record_session_init_profile_missing", "a session_init record requires its session_init_profile")
	}
	if !terminalStatuses[r.Status] {
		return errorcodes.Errorf("record_status_invalid", "invalid terminal status %q", r.Status)
	}
	if r.OutputEncoding != "utf-8" && r.OutputEncoding != "base64" {
		return errorcodes.Errorf("record_output_encoding_invalid", "invalid output_encoding %q", r.OutputEncoding)
	}
	if r.PromptSource != "" && r.PromptSource != "observed" && r.PromptSource != "inferred" {
		return errorcodes.Errorf("record_prompt_source_invalid", "invalid prompt_source %q", r.PromptSource)
	}
	if r.Ping != nil {
		if err := r.Ping.Validate(); err != nil {
			return err
		}
	}
	if r.Notices == nil {
		r.Notices = []Notice{}
	}
	if r.AddressCandidates == nil {
		r.AddressCandidates = []string{}
	}
	if r.Device.Groups == nil {
		r.Device.Groups = []string{}
	}
	return nil
}

type Counts struct {
	Total      int `json:"total"`
	Completed  int `json:"completed"`
	Succeeded  int `json:"succeeded"`
	Failed     int `json:"failed"`
	NotStarted int `json:"not_started"`
	InFlight   int `json:"in_flight"`
	Incomplete int `json:"incomplete"`
	Cancelled  int `json:"cancelled"` // devices a cancel interrupted or prevented
}
type Producer struct {
	Hostname             string `json:"hostname"`
	BootID               string `json:"boot_id"`
	PID                  int    `json:"pid"`
	ProcessStartIdentity string `json:"process_start_identity"`
	AppVersion           string `json:"app_version"`
}

// The scoreboard's target states: a device
// is queued until the dispatcher starts it, running until its result, then
// succeeded or failed; a device the job never reached is cancelled or
// incomplete when the job ended that way, and stays queued when a halt or
// a wave gate left it.
const (
	TargetQueued     = "queued"
	TargetRunning    = "running"
	TargetSucceeded  = "succeeded"
	TargetFailed     = "failed"
	TargetCancelled  = "cancelled"
	TargetIncomplete = "incomplete"
)

// ScoreboardTarget is one device of the activity and its state.
// ScoreboardTarget is one device of the activity: its name, its state,
// and, while it runs a command, the settled bytes of that command's
// response so far, memory and spool (0 when none).
type ScoreboardTarget struct {
	Name  string `json:"name"`
	State string `json:"state"`
	Bytes int64  `json:"bytes"`
}

// ScoreboardCommands says what the job sends each device: the count of
// statements (the largest platform list for a crun) and, under --cf, the
// file's base name; never a statement's text.
type ScoreboardCommands struct {
	Count int    `json:"count"`
	File  string `json:"file,omitempty"`
}

// ScoreboardCollection is a crun's collection so far: the directory's
// base name and the files replaced and kept.
type ScoreboardCollection struct {
	Directory string `json:"directory"`
	Replaced  int    `json:"replaced"`
	Kept      int    `json:"kept"`
}

// ScoreboardMetrics: the output bytes the job holds and the durations of
// the devices that finished, in milliseconds.
// ScoreboardMetrics: OutputBytes is what the store has written of the
// devices' output; InFlightBytes the sum of the targets' bytes, the
// responses still arriving.
type ScoreboardMetrics struct {
	OutputBytes   int64 `json:"output_bytes"`
	InFlightBytes int64 `json:"in_flight_bytes"`
	Finished      int   `json:"finished"`
	MinMS         int64 `json:"min_ms"`
	AvgMS         int64 `json:"avg_ms"`
	MaxMS         int64 `json:"max_ms"`
}

// ScoreboardSnapshot is what an activity publishes about itself for
// karvi watch (schema 2): Mode is what the operator
// ran (login, cmd, run, crun, exercise); Targets are its devices with their
// states; Inputs are the invocation's target inputs as given, a file by its
// base name; Daemon says the daemon runs it.
type ScoreboardSnapshot struct {
	SchemaVersion int                        `json:"schema_version"`
	ActivityID    string                     `json:"activity_id"`
	JobID         string                     `json:"job_id,omitempty"`
	Operator      Operator                   `json:"operator"`
	ActivityType  string                     `json:"activity_type"`
	Mode          string                     `json:"mode"`
	Status        string                     `json:"status"`
	DispatchMode  *string                    `json:"dispatch_mode"`
	WaveNumber    int                        `json:"wave_number"`
	Width         int                        `json:"width"`
	Depth         int                        `json:"depth"`
	Counts        Counts                     `json:"counts"`
	Target        map[string]any             `json:"target,omitempty"`
	Targets       []ScoreboardTarget         `json:"targets"`
	Inputs        []executionplan.ScopeInput `json:"inputs"`
	Commands      *ScoreboardCommands        `json:"commands,omitempty"`
	Collection    *ScoreboardCollection      `json:"collection,omitempty"`
	Metrics       *ScoreboardMetrics         `json:"metrics,omitempty"`
	Daemon        bool                       `json:"daemon"`
	Recording     bool                       `json:"recording"`
	StartedAt     time.Time                  `json:"started_at"`
	LastUpdatedAt time.Time                  `json:"last_updated_at"`
	EndedAt       *time.Time                 `json:"ended_at,omitempty"`
	ElapsedNS     int64                      `json:"elapsed_ns"`
	Halt          any                        `json:"halt"`
	ErrorSummary  any                        `json:"error_summary"`
	Producer      Producer                   `json:"producer"`
}

func (s *ScoreboardSnapshot) Validate() error {
	if s.SchemaVersion == 0 {
		s.SchemaVersion = ScoreboardSchemaVersion
	}
	if s.Targets == nil {
		s.Targets = []ScoreboardTarget{}
	}
	if s.Inputs == nil {
		s.Inputs = []executionplan.ScopeInput{}
	}
	if s.Counts.Completed+s.Counts.InFlight+s.Counts.NotStarted+s.Counts.Incomplete+s.Counts.Cancelled != s.Counts.Total {
		return errorcodes.Errorf("scoreboard_counts_inconsistent", "scoreboard count partition does not equal total")
	}
	return nil
}

type AuditRecord struct {
	SchemaVersion  int            `json:"schema_version"`
	EventID        string         `json:"event_id"`
	EventName      string         `json:"event_name"`
	Timestamp      time.Time      `json:"timestamp"`
	Outcome        string         `json:"outcome"`
	Severity       string         `json:"severity"`
	Operator       Operator       `json:"operator"`
	Process        map[string]any `json:"process"`
	ActivityID     string         `json:"activity_id,omitempty"`
	JobID          string         `json:"job_id,omitempty"`
	Device         map[string]any `json:"device,omitempty"`
	DeviceIdentity map[string]any `json:"device_identity,omitempty"`
	Action         map[string]any `json:"action"`
	Policy         map[string]any `json:"policy"`
	Result         map[string]any `json:"result"`
	Source         map[string]any `json:"source"`
	Details        map[string]any `json:"details"`
}

// TargetInput is one target input at its command-line position. login,
// command, and run build the same list; the manifest's selection records
// it.
//
// Kind is one of:
//   - "target": Value is a device name or glob (--target, a positional device)
//   - "names": Names are target names read from a file in file order (--tf);
//     Source names the file
//   - "site", "device-group", "platform": Value is the selector glob
//   - "all": every inventory device
type TargetInput struct {
	Kind   string   `json:"kind"`
	Value  string   `json:"value,omitempty"`
	Names  []string `json:"names,omitempty"`
	Source string   `json:"source,omitempty"`
}

// Selection is what the operator asked for: the target inputs, the excludes,
// the literal address, and the address-authority overrides as given.
type Selection struct {
	Inputs             []TargetInput `json:"inputs"`
	Excludes           []string      `json:"excludes"`
	ManagementAddress  string        `json:"management_address,omitempty"`
	AddressAuthorities []string      `json:"address_authorities"`
}

// ExecutionPolicy is the daemon's own SSH and Telnet policy at acceptance,
// which a client-authored plan never sets; its
// digest is what the daemon reports at prepare.
type ExecutionPolicy struct {
	HostKeyPolicy         string `json:"ssh_host_key_policy"`
	KnownHostsFile        string `json:"ssh_known_hosts_file"`
	HaltOnHostKeyMismatch bool   `json:"ssh_halt_run_on_host_key_mismatch"`
	AllowTelnet           bool   `json:"allow_telnet"`
	Digest                string `json:"digest,omitempty"`
}

// Sum is the policy digest: SHA-256 over the JSON encoding with digest
// cleared, the rule every digest in the tree follows.
func (p ExecutionPolicy) Sum() (string, error) {
	p.Digest = ""
	d, err := executionplan.SumJSON(p)
	if err != nil {
		return "", err
	}
	return d.String(), nil
}

// InitialState is one (device, command) pair before execution starts.
type InitialState struct {
	DeviceID     string `json:"device_id"`
	CommandIndex int    `json:"command_index"`
	State        string `json:"state"`
}

// Manifest is the job manifest, version 2: typed
// around the commit header, the final plan, the credential package's safe
// projection, and the daemon's execution policy. Everything the plan and
// header already say is not repeated.
type Manifest struct {
	SchemaVersion     int                                     `json:"schema_version"`
	JobID             string                                  `json:"job_id"`
	ActivityID        string                                  `json:"activity_id"`
	AcceptedAt        time.Time                               `json:"accepted_at"`
	Operator          Operator                                `json:"operator"`
	App               map[string]any                          `json:"app"`
	Mode              string                                  `json:"mode"`
	Header            executionplan.PublicJobHeader           `json:"header"`
	Plan              executionplan.ExecutionPlan             `json:"plan"`
	CredentialPackage credentialpackage.SafePackageProjection `json:"credential_package"`
	Policy            ExecutionPolicy                         `json:"policy"`
	Selection         Selection                               `json:"selection"`
	InitialStates     []InitialState                          `json:"initial_states"`
}

// Validate checks the manifest against its own plan, header, and package
// projection: the plan verifies, the header
// validates at committed stage and matches the plan, the projection's
// digest is its recorded one and the header's reference, every initial
// state names a plan target with a command index in range, and the
// identities agree.
func (m *Manifest) Validate() error {
	if m.SchemaVersion != JobSchemaVersion {
		return errorcodes.Errorf("manifest_schema_unsupported", "manifest schema %d is not %d", m.SchemaVersion, JobSchemaVersion)
	}
	if m.JobID == "" || m.ActivityID == "" {
		return errorcodes.Errorf("manifest_invalid", "job_id and activity_id are required")
	}
	if m.JobID != m.Header.JobID {
		return errorcodes.Errorf("manifest_invalid", "job_id %q differs from the header's %q", m.JobID, m.Header.JobID)
	}
	if err := m.Plan.Verify(); err != nil {
		return err
	}
	if err := m.Plan.Validate(executionplan.Committed); err != nil {
		return err
	}
	if err := m.Header.Validate(executionplan.Committed); err != nil {
		return err
	}
	if err := m.Header.Matches(&m.Plan, executionplan.Committed); err != nil {
		return err
	}
	recorded := m.CredentialPackage.PackageDigest
	m.CredentialPackage.PackageDigest = executionplan.Digest{}
	sum, err := m.CredentialPackage.Sum()
	m.CredentialPackage.PackageDigest = recorded
	if err != nil {
		return err
	}
	if sum != recorded || m.Header.CredentialPackage == nil || m.Header.CredentialPackage.Digest != recorded {
		return errorcodes.Errorf("manifest_invalid", "credential package digest %s is not the projection's %s and the header's reference", recorded, sum)
	}
	if m.CredentialPackage.JobID != m.JobID || m.CredentialPackage.PlanDigest != m.Plan.PlanDigest {
		return errorcodes.Errorf("manifest_invalid", "credential package names another job or plan")
	}
	if m.Policy.Digest != "" {
		if sum, err := m.Policy.Sum(); err != nil || sum != m.Policy.Digest {
			return errorcodes.Errorf("manifest_invalid", "policy digest %s does not match the policy", m.Policy.Digest)
		}
	}
	targets := map[string]bool{}
	for _, t := range m.Plan.Targets {
		targets[t.TargetID] = true
	}
	for i, s := range m.InitialStates {
		if !targets[s.DeviceID] {
			return errorcodes.Errorf("manifest_invalid", "initial_states[%d] names %q, not a plan target", i, s.DeviceID)
		}
		if s.CommandIndex < 1 || s.CommandIndex > len(m.Plan.Commands) {
			return errorcodes.Errorf("manifest_invalid", "initial_states[%d] command index %d is out of range", i, s.CommandIndex)
		}
	}
	if m.Selection.Inputs == nil || m.Selection.Excludes == nil || m.Selection.AddressAuthorities == nil {
		return errorcodes.Errorf("manifest_invalid", "selection lists must be present (empty allowed)")
	}
	return nil
}

// IsPlanTarget reports whether id names a target of the manifest's plan.
func (m *Manifest) IsPlanTarget(id string) bool {
	for _, t := range m.Plan.Targets {
		if t.TargetID == id {
			return true
		}
	}
	return false
}

// FinalStatuses are the six statuses a job ends in, the last value of a
// scoreboard file's status and a summary's final_status: completed, halted,
// errored, cancelled, incomplete (the exit code decides which), and
// exercised (an exercise, which contacts no device). The one
// list for every reader that tells a finished activity from a running one:
// the scoreboard reader's staleness, the watch screen's rule, and
// karvi-prune's eligibility.
var FinalStatuses = []string{"completed", "halted", "errored", "cancelled", "incomplete", "exercised"}

// TerminalStatus says whether status is one of FinalStatuses.
func TerminalStatus(status string) bool {
	for _, s := range FinalStatuses {
		if s == status {
			return true
		}
	}
	return false
}

type Summary struct {
	SchemaVersion          int               `json:"schema_version"`
	JobID                  string            `json:"job_id"`
	ActivityID             string            `json:"activity_id"`
	StartedAt              time.Time         `json:"started_at"`
	EndedAt                time.Time         `json:"ended_at"`
	DurationNS             int64             `json:"duration_ns"`
	FinalStatus            string            `json:"final_status"`
	ExitCode               int               `json:"primary_exit_code"`
	ExitName               string            `json:"primary_exit_name"`
	TerminalCauses         []string          `json:"terminal_causes"`
	DispatchOrder          string            `json:"dispatch_order"`        // default, sorted, shuffle, random
	ShuffleKey             *string           `json:"shuffle_key,omitempty"` // shuffle and random only; may be empty
	PlanID                 string            `json:"plan_id"`               // additive fields under counter 2
	PlanDigest             string            `json:"plan_digest"`
	Mode                   string            `json:"mode"`
	AddressAuthorityCounts map[string]int    `json:"address_authority_counts"`
	Ping                   *PingSummary      `json:"ping"`         // null when the gate is disabled
	Cancellation           *Cancellation     `json:"cancellation"` // null unless cancelled through cancel_job
	DeviceCounts           map[string]int    `json:"device_counts"`
	RequestedCommandCounts map[string]int    `json:"requested_command_counts"`
	SessionInitCounts      map[string]int    `json:"session_init_counts"`
	Halt                   any               `json:"halt"`
	Output                 map[string]any    `json:"output"`
	AuditSinkStatus        map[string]any    `json:"audit_sink_status"`
	Bottleneck             map[string]any    `json:"bottleneck"`
	Recovery               map[string]any    `json:"recovery"`
	Paths                  map[string]string `json:"paths"`
	// Collection is present for a crun:
	// the directory and, per device, its file and whether the file was
	// replaced or kept. Additive under job schema 2 (optional).
	Collection *CollectionSummary `json:"collection,omitempty"`
}

// PingSummary is the summary's ping block: the gate's settings and method,
// per-device and per-probe counters; Capability is set by an exercise only.
// Cancellation is the summary's record of a cancel_job request: the first
// request's time, the operator's reason as given,
// the requester from the connection's peer credentials, and the IPC
// request ID. An operator's interrupt of an in-process run leaves the
// block null; its presence is what tells the two apart.
type Cancellation struct {
	RequestedAt time.Time       `json:"requested_at"`
	Reason      string          `json:"reason"`
	Requester   CancelRequester `json:"requester"`
	RequestID   string          `json:"request_id"`
}

// CancelledBy is the request that cancelled the job: the block when the
// final status is cancelled, nil otherwise. A summary can carry the block
// under another final status, an exercise (which never ends cancelled) or a
// live job whose work had finished, when cancel_job was accepted too late
// to change anything; a reader that prints the cancelled line asks this
// and not the block.
func (s Summary) CancelledBy() *Cancellation {
	if s.FinalStatus != "cancelled" {
		return nil
	}
	return s.Cancellation
}

type CancelRequester struct {
	PID int `json:"pid"`
	UID int `json:"uid"`
}

func (c *Cancellation) details() map[string]any {
	return map[string]any{"requested_at": c.RequestedAt, "reason": c.Reason, "requester_pid": c.Requester.PID, "requester_uid": c.Requester.UID, "request_id": c.RequestID}
}

// AuditDetails is the block as the audit record carries it.
func (c *Cancellation) AuditDetails() map[string]any {
	if c == nil {
		return nil
	}
	return c.details()
}

type PingSummary struct {
	Enabled    bool           `json:"enabled"`
	Method     string         `json:"method,omitempty"`
	Capability string         `json:"capability,omitempty"`
	Probes     int            `json:"probes"`
	TimeoutNS  int64          `json:"timeout_ns"`
	Devices    map[string]int `json:"devices"`
	ProbesSent int            `json:"probes_sent"`
	Replies    int            `json:"replies"`
	Timeouts   int            `json:"timeouts"`
	Errors     int            `json:"errors"`
	TotalNS    int64          `json:"total_ns"`
}

// PingSummary device counter keys.
const (
	PingDevicesGated            = "gated"
	PingDevicesProceeded        = "proceeded"
	PingDevicesDegraded         = "degraded"
	PingDevicesSkipped          = "skipped"
	PingDevicesCapabilityFailed = "capability_failed"
)

type Metrics struct {
	SchemaVersion    int              `json:"schema_version"`
	JobID            string           `json:"job_id"`
	MeasurementStart time.Time        `json:"measurement_start"`
	MeasurementEnd   time.Time        `json:"measurement_end"`
	HostProfile      map[string]any   `json:"host_profile"`
	JobProfile       map[string]any   `json:"job_profile"`
	CPUSamples       []map[string]any `json:"cpu_samples"`
	ProcessSamples   []map[string]any `json:"process_samples"`
	WaveDecisions    []map[string]any `json:"wave_decisions"`
	Admission        map[string]any   `json:"admission"`
	StageHistograms  map[string]any   `json:"stage_histograms"`
	Errors           map[string]any   `json:"errors"`
	Storage          map[string]any   `json:"storage"`
	Bottleneck       map[string]any   `json:"bottleneck"`
	Limitations      []string         `json:"limitations"`
}

type CommandSink interface {
	Append(record CommandRecord) error
	Sync() error
}
type AuditSink interface {
	WriteAudit(record AuditRecord) error
}

// CollectionSummary is a crun's outcome per device: Replaced counts the
// devices whose file was renamed into
// place, Kept those whose previous file was left untouched.
type CollectionSummary struct {
	Directory string                      `json:"directory"`
	Replaced  int                         `json:"replaced"`
	Kept      int                         `json:"kept"`
	Devices   map[string]CollectionDevice `json:"devices"`
}

// CollectionDevice is one device's line in the collection summary: its
// file name in the directory and "replaced" or "kept".
type CollectionDevice struct {
	File    string `json:"file"`
	Outcome string `json:"outcome"`
}

// The two collection outcomes.
const (
	CollectionReplaced = "replaced"
	CollectionKept     = "kept"
)
