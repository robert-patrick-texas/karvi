package executionplan

import (
	"crypto/sha256"
	"fmt"
	"net/netip"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/robert-patrick-texas/karvi/inventory"
)

// SchemaVersion is the execution-plan counter; PublicJobHeader
// shares it because the two contracts live in one schema file. Version 2
// adds the session-init table and each target's profile; version 3 the
// blind sends, blind_returns and blind_wait_ns, and, without another
// bump (3 was unreleased at the time), the interactive-prompt declarations blind and
// expectations and each target's planning notices; version 4 the
// invocation's output files, output.persist, output.files, and
// output.root, and, at the same
// bump, sources.inventory_digest and selector_provenance.digest removed
// (recorded and never read); version 5 the job ID
// in the job form `YYMMDD-HHMMSS-xx`, which
// the header's validator requires and a schema 4 daemon refuses.
// Version 6 output.crop_to_dot, 7 the platform command lists and the
// collection sub-block, 8 sources.inputs and commands_file for
// the scoreboard, 10 the collection's word, since run and command
// collect too, and its suffix (--fs), and, without another bump (10 was
// unreleased), output.files' failures_jsonl renamed errors_jsonl; 11 each
// target's channel.
const SchemaVersion = 11

// Mode is the requested execution mode of a job.
type Mode string

const (
	ModeLive     Mode = "live"
	ModeExercise Mode = "exercise"
)

// ExecutionDomainLocal is the only execution domain in v1; every target's
// execution_endpoint must belong to it.
const ExecutionDomainLocal = "local"

// Dispatch modes and orders.
const (
	DispatchSerial   = "serial"
	DispatchParallel = "parallel"
	DispatchWave     = "wave"

	OrderDefault = "default"
	OrderSorted  = "sorted"
	OrderShuffle = "shuffle"
	OrderRandom  = "random"
)

// Output formats a run or command may request.
const (
	FormatText  = "text"
	FormatJSONL = "jsonl"
	FormatJSON  = "json"
)

// PingProbes is the only probe count v1 sends.
const PingProbes = 2

// Credential-package protections named by a header reference.
const (
	ProtectionLocalPeer = "local-peer"
	ProtectionSealed    = "sealed"
)

// idPattern is the timestamp-prefixed identifier: timestamp, offset, and twenty
// lowercase Crockford base32 characters.
var idPattern = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}\.[0-9]{6}[+-][0-9]{4}-[0-9a-hjkmnp-tv-z]{20}$`)

// ValidID reports whether s has the timestamp-prefixed identifier format.
func ValidID(s string) bool { return idPattern.MatchString(s) }

// jobIDPattern is the job ID: the stamp
// `YYMMDD-HHMMSS` in the effective timezone and a two-character sequence
// over the digits and lowercase letters. A run's activity ID is its job ID
// and a cmd activity's ID names its directory; both take this form.
var jobIDPattern = regexp.MustCompile(`^[0-9]{6}-[0-9]{6}-[0-9a-z]{2}$`)

// ValidJobID reports whether s is a job ID.
func ValidJobID(s string) bool { return jobIDPattern.MatchString(s) }

// Operator is the submitting operator as the plan records it. It is a
// separate type from records.Operator because records imports this package.
type Operator struct {
	Username   string   `json:"username"`
	UID        int      `json:"uid"`
	PrimaryGID int      `json:"primary_gid,omitempty"`
	Groups     []string `json:"groups,omitempty"`
}

// ExecutionPlan is the client-authored, immutable plan.
type ExecutionPlan struct {
	SchemaVersion int               `json:"schema_version"`
	PlanID        string            `json:"plan_id"`
	PlanDigest    Digest            `json:"plan_digest,omitzero"`
	Operator      Operator          `json:"operator"`
	Targets       []ExecutionTarget `json:"targets"`
	Commands      []string          `json:"commands"`
	// CommandsFile is the base name of the --cf file the commands came
	// from, for the scoreboard (schema 8);
	// empty when every command was given on the line.
	CommandsFile string `json:"commands_file,omitempty"`
	// PlatformCommands is a command list per platform (schema 7): a crun
	// that names no command sends
	// each device its platform's list. A target points at its list by its
	// device's platform, and every target's platform has a list when
	// Commands is empty. The blind and expect declarations belong to
	// Commands alone. CommandPlanDigest covers Commands and every list.
	PlatformCommands map[string][]string `json:"platform_commands,omitempty"`
	// PlatformFilters is a drop list per platform for the collection file
	// (schema 9): the patterns the writer
	// applies to each output line of a record of that platform; a platform
	// absent from the map has none. Present only with a collection; the
	// plan digest covers it.
	PlatformFilters   map[string][]string           `json:"platform_filters,omitempty"`
	CommandPlanDigest Digest                        `json:"command_plan_digest"`
	BlindReturns      []int                         `json:"blind_returns"`
	BlindWaitNS       int64                         `json:"blind_wait_ns"`
	Blind             []bool                        `json:"blind"`
	Expectations      [][]Expectation               `json:"expectations"`
	SessionInit       map[string]SessionInitProfile `json:"session_init"`
	Dispatch          DispatchSettings              `json:"dispatch"`
	Output            OutputSettings                `json:"output"`
	Ping              PingSettings                  `json:"ping"`
	Sources           SourceDigests                 `json:"sources"`
	Planning          PlanningTimestamps            `json:"planning"`
	Preparation       []PreparationEvidence         `json:"preparation"`
}

// Blind sends: BlindReturns is empty or
// one count per command, each 0..BlindReturnsMax, the carriage returns
// written after that command without a prompt match; BlindWaitNS is the
// client's effective execution.blind-wait, the wait for the prompt after a
// blind send in place of the command timeout, 0..BlindWaitMax where 0 does
// not wait. The client interprets the trailing \r escapes once and commands
// holds the text as sent.
//
// Interactive prompts: Blind is empty or one flag
// per command, the tolerance --blind declares (the prompt may not return;
// the command is awaited for the blind wait and its absence is a success
// with a notice); a blind_returns count above zero implies it. Expectations
// is empty or one list per command, each list present (empty allowed) and
// at most ExpectationsMax declarations, every pattern nonempty and a valid
// RE2 expression; a command with a count above zero and a declaration is
// invalid, since typed-ahead returns and answered prompts on one command
// reproduce the miscount hazard the declarations exist to remove. All four
// fields are under plan_digest; the commands digest rule is unchanged. The
// bounds are constants, not configuration keys: the closed schema states
// each as a number, and a client and a daemon configured apart would
// disagree after preparation. Both are twenty, the extended ping's nineteen
// prompts the measure (D3 set ten returns; D4 raised it to match).
const (
	BlindReturnsMax = 20
	BlindWaitMax    = 10 * time.Minute
	ExpectationsMax = 20
)

// Expectation is one expect-and-send declaration on a command: Pattern
// is a nonempty RE2 expression the client
// compiled at parse time, matched by the session against the last line of
// the device's output since its previous answer; Response is the device text
// sent with a carriage return when a line matches, empty for a bare return,
// recorded in clear as a command is (a secret is never a response).
// ExecutionPlan.Expectations holds one list per command, ExpectationsMax
// bounds a list, and validateBlindSends compiles every pattern, so a plan
// the daemon accepts never reaches the executor with a pattern it cannot
// compile.
type Expectation struct {
	Pattern  string `json:"pattern"`
	Response string `json:"response"`
}

// SessionInitNone is the profile name of a target no session-init profile
// was selected for; a configured profile may not take it.
const SessionInitNone = "none"

// Session-init on-error values.
const (
	SessionInitFailDevice = "fail-device"
	SessionInitContinue   = "continue"
)

// Session-init command-timeout bounds; 0 means execution.command-timeout.
const (
	SessionInitMinTimeout = time.Second
	SessionInitMaxTimeout = 12 * time.Hour
)

// SessionInitProfile is one selected profile as the plan carries it, so the
// daemon executes what the client's configuration held at planning.
type SessionInitProfile struct {
	Commands         []string `json:"commands"`
	OnError          string   `json:"on_error"`
	CommandTimeoutNS int64    `json:"command_timeout_ns"`
}

// DispatchSettings are the effective dispatch, halt, and gate values.
type DispatchSettings struct {
	Mode                  string  `json:"mode"`
	Width                 int     `json:"width"`
	StartWidth            int     `json:"start_width"`
	MaxWidth              int     `json:"max_width"`
	DispatchOrder         string  `json:"dispatch_order"`
	ShuffleKey            *string `json:"shuffle_key,omitempty"`
	HaltErrorCount        int     `json:"halt_error_count"`
	HaltErrorPercent      int     `json:"halt_error_percent"`
	WaveGateErrorCount    int     `json:"wave_gate_error_count"`
	WaveGateErrorPercent  int     `json:"wave_gate_error_percent"`
	WaveGateTimedDelayNS  int64   `json:"wave_gate_timed_delay_ns"`
	ContinueDeviceOnError bool    `json:"continue_device_on_error"`
}

// OutputSettings are the effective output and follow values. The
// invocation decides the job's files on every path: Persist is
// output.persist-command (false, `--nof`, is no
// folder and no file), Files the eight output.files switches, and Root the
// invocation's output.root resolved to an absolute path, so the daemon's
// store writes what the client's configuration says and the client's `job
// follow` looks where the daemon wrote.
type OutputSettings struct {
	Format          string      `json:"format"`
	Echo            bool        `json:"echo"`
	DynamicBorder   bool        `json:"dynamic_border"`
	NoBorder        bool        `json:"no_border"`
	Follow          bool        `json:"follow"`
	MaxCommandBytes int64       `json:"max_command_bytes"`
	MaxJobBytes     int64       `json:"max_job_bytes"`
	Persist         bool        `json:"persist"`
	Files           OutputFiles `json:"files"`
	Root            string      `json:"root"`
	// CropToDot is output.crop-to-dot as the invocation had it (schema 6):
	// a device name in a file name is its
	// first label, so a job's files are named as its invocation said on
	// every path.
	CropToDot bool `json:"crop_to_dot"`
	// Collection is present for a crun, and for a run or command given
	// --cd (schema 7, its word schema 10): the resolved collection
	// directory, the file mode, and the word that asked, decided by the
	// invocation and carried to whoever runs the job.
	Collection *CollectionSettings `json:"collection,omitempty"`
}

// CollectionSettings is the output block's collection sub-block: Directory
// is absolute (crun.directory resolved by the client), FileMode is
// crun.file-mode as configured ("0640", "0644", or "0660"), Word the
// operator's word (crun, run, or command): the daemon receives every job
// as a run, and a crun's hook, filters, and scoreboard mode are its own.
type CollectionSettings struct {
	Directory string `json:"directory"`
	FileMode  string `json:"file_mode"`
	Word      string `json:"word"`
	// Suffix is --fs, appended as written to each device's file name;
	// empty for none.
	Suffix string `json:"suffix,omitempty"`
}

// SuffixProblem says why a collection file suffix (--fs) cannot be used,
// empty when it can: it is not empty, holds no /, and no NUL or other
// control character (a newline would break the hook's one name per line).
func SuffixProblem(suffix string) string {
	switch {
	case suffix == "":
		return "is empty"
	case strings.Contains(suffix, "/"):
		return "holds /"
	}
	for _, r := range suffix {
		if r < 0x20 || r == 0x7f {
			return "holds a control character"
		}
	}
	return ""
}

// CollectionFileModes are the values crun.file-mode takes, the plan's
// validator and the registry's enum in one place.
var CollectionFileModes = []string{"0640", "0644", "0660"}

// CollectionWords are the words that ask for a collection.
var CollectionWords = []string{"crun", "run", "command"}

// Crun reports whether the plan is a crun's: a collection its word asked
// for as crun.
func (o OutputSettings) Crun() bool { return o.Collection != nil && o.Collection.Word == "crun" }

// CommandsFor is the command list a device of platform runs: its platform's
// list when the plan holds one, else the plan's Commands.
func (p *ExecutionPlan) CommandsFor(platform string) []string {
	if list, ok := p.PlatformCommands[platform]; ok {
		return list
	}
	return p.Commands
}

// CommandCount is the figure the metrics and the reports call "commands":
// the length of Commands, or of the longest platform list when the plan
// carries lists alone.
func (p *ExecutionPlan) CommandCount() int {
	n := len(p.Commands)
	for _, list := range p.PlatformCommands {
		if len(list) > n {
			n = len(list)
		}
	}
	return n
}

// OutputFiles is which of the job folder's files are written: the eight
// output.files keys, true for written.
type OutputFiles struct {
	CommandsJSONL    bool `json:"commands_jsonl"`
	CommandsTxt      bool `json:"commands_txt"`
	ErrorsJSONL      bool `json:"errors_jsonl"`
	FailedDevicesTxt bool `json:"failed_devices_txt"`
	ManifestJSON     bool `json:"manifest_json"`
	MetricsJSON      bool `json:"metrics_json"`
	SummaryJSON      bool `json:"summary_json"`
	OutputTxt        bool `json:"output_txt"`
}

// AllOutputFiles is every file written, the eight keys' default.
var AllOutputFiles = OutputFiles{true, true, true, true, true, true, true, true}

// PingSettings are the effective ICMP gate values the executor consumes.
type PingSettings struct {
	Enabled   bool  `json:"enabled"`
	Probes    int   `json:"probes"`
	TimeoutNS int64 `json:"timeout_ns"`
}

// SourceDigests identify the configuration and the inventory sources the
// client used: the configuration's digest and each source's own.
type SourceDigests struct {
	ConfigDigest string               `json:"config_digest"`
	Selectors    inventory.Provenance `json:"selector_provenance"`
	// Inputs are the invocation's target inputs as given, in order (schema
	// 8): what the scoreboard shows as the
	// job's scope on every path, the daemon's included. Never null.
	Inputs []ScopeInput `json:"inputs"`
}

// ScopeInput is one target input of an invocation: the kind (target, tf,
// site, group, platform, all) and its value, a file input's value the
// file's base name so that no path leaves the operator's shell.
type ScopeInput struct {
	Kind  string `json:"kind"`
	Value string `json:"value,omitempty"`
}

// PlanningTimestamps record the client's planning steps.
type PlanningTimestamps struct {
	DraftedAt   time.Time `json:"drafted_at"`
	PreparedAt  time.Time `json:"prepared_at,omitzero"`
	FinalizedAt time.Time `json:"finalized_at,omitzero"`
}

// PreparationEvidence is what one execution endpoint returned from
// prepare_job, embedded so the committed plan is self-contained.
type PreparationEvidence struct {
	ExecutionEndpoint string            `json:"execution_endpoint"`
	PreparationID     string            `json:"preparation_id"`
	PreparationDigest Digest            `json:"preparation_digest"`
	PreparedAt        time.Time         `json:"prepared_at"`
	Addresses         []AddressEvidence `json:"addresses"`
}

// AddressEvidence holds exactly the address-plan fields prepare_job may fill
// for a daemon-authority target; IncorporatePreparation copies
// them field for field and nothing else. SelectedSource says which branch
// of the selection rule the daemon took, inventory for a client hint or
// dns-daemon for its own lookup.
type AddressEvidence struct {
	TargetID         string       `json:"target_id"`
	DaemonCandidates []netip.Addr `json:"daemon_candidates"`
	Selected         netip.Addr   `json:"selected,omitzero"`
	Alternates       []netip.Addr `json:"alternates"`
	SelectedSource   string       `json:"selected_source,omitempty"`
	ResolverContext  string       `json:"resolver_context"`
	ResolutionDigest Digest       `json:"resolution_digest"`
}

// PublicJobHeader is the non-secret request header, sent at
// prepare_job with the draft digest and at commit_job with the final digest
// and the credential-package reference.
type PublicJobHeader struct {
	SchemaVersion     int               `json:"schema_version"`
	IdempotencyKey    string            `json:"idempotency_key"`
	JobID             string            `json:"job_id"`
	Operator          Operator          `json:"operator"`
	Client            ClientIdentity    `json:"client"`
	Mode              Mode              `json:"mode"`
	ExecutionDomain   string            `json:"execution_domain"`
	Priority          int               `json:"priority"`
	CommandPlanDigest Digest            `json:"command_plan_digest"`
	PlanDigest        Digest            `json:"plan_digest"`
	CredentialPackage *PackageReference `json:"credential_package,omitempty"`
}

// ClientIdentity names the submitting executable; it is hashed into the
// manifest, unlike the IPC envelope's client_pid.
type ClientIdentity struct {
	AppName  string `json:"app_name"`
	Version  string `json:"version"`
	Commit   string `json:"commit"`
	PID      int    `json:"pid"`
	Hostname string `json:"hostname"`
}

// PackageReference is the header's non-secret pointer to the credential
// package provided over the credential channel.
type PackageReference struct {
	Protection string `json:"protection"`
	Digest     Digest `json:"digest"`
}

// SumCommands is the command-plan digest of a plan without platform lists:
// SHA-256 over each command followed by a NUL byte, the rule the manifest
// field of the same name has always used.
func SumCommands(commands []string) Digest {
	return SumCommandPlan(commands, nil)
}

// SumCommandPlan is the command-plan digest over Commands and the platform
// lists: the commands as SumCommands has
// always summed them, then each platform in sorted order as a 0x01 byte,
// its name, a NUL, and its commands each followed by a NUL. A plan without
// lists sums as it did.
func SumCommandPlan(commands []string, lists map[string][]string) Digest {
	h := sha256.New()
	for _, c := range commands {
		h.Write([]byte(c))
		h.Write([]byte{0})
	}
	names := make([]string, 0, len(lists))
	for name := range lists {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		h.Write([]byte{1})
		h.Write([]byte(name))
		h.Write([]byte{0})
		for _, c := range lists[name] {
			h.Write([]byte(c))
			h.Write([]byte{0})
		}
	}
	var d Digest
	copy(d[:], h.Sum(nil))
	return d
}

// SumPlan returns the digest of the plan with plan_digest cleared. Over a
// draft it is the plan-payload digest the header carries at prepare_job;
// over the finalized plan it is the final digest at commit_job.
func SumPlan(p ExecutionPlan) (Digest, error) {
	p.PlanDigest = Digest{}
	return SumJSON(p)
}

// Verify recomputes the plan digest and reports plan_digest_mismatch when it
// differs from the recorded one.
func (p *ExecutionPlan) Verify() error {
	if p.PlanDigest.IsZero() {
		return planInvalid("plan_digest", "is not set")
	}
	sum, err := SumPlan(*p)
	if err != nil {
		return planInvalid("plan_digest", "cannot encode the plan: %v", err)
	}
	if sum != p.PlanDigest {
		return fmt.Errorf("plan_digest_mismatch: recorded %s, computed %s", p.PlanDigest, sum)
	}
	return nil
}

func planInvalid(field, format string, args ...any) error {
	return fmt.Errorf("execution_plan_invalid: %s: %s", field, fmt.Sprintf(format, args...))
}

func headerInvalid(field, format string, args ...any) error {
	return fmt.Errorf("job_header_invalid: %s: %s", field, fmt.Sprintf(format, args...))
}

// Validate checks the plan at the given stage.
func (p *ExecutionPlan) Validate(stage Stage) error {
	if p.SchemaVersion != SchemaVersion {
		return planInvalid("schema_version", "%d is not the supported version %d", p.SchemaVersion, SchemaVersion)
	}
	if !ValidID(p.PlanID) {
		return planInvalid("plan_id", "%q is not a valid identifier", p.PlanID)
	}
	if f, m := p.Operator.problem(); f != "" {
		return planInvalid("operator."+f, "%s", m)
	}
	if len(p.Targets) == 0 {
		return fmt.Errorf("plan_not_enumerated: the plan lists no targets")
	}
	seen := map[string]bool{}
	for i := range p.Targets {
		t := &p.Targets[i]
		if seen[t.TargetID] {
			return planInvalid("targets", "target_id %q appears twice", t.TargetID)
		}
		seen[t.TargetID] = true
		if err := t.Validate(stage); err != nil {
			return fmt.Errorf("execution_plan_invalid: targets[%d]: %w", i, err)
		}
	}
	if len(p.Commands) == 0 && len(p.PlatformCommands) == 0 {
		return planInvalid("commands", "at least one command is required")
	}
	for i, c := range p.Commands {
		if strings.ContainsRune(c, 0) {
			return planInvalid("commands", "command %d contains a NUL byte", i+1)
		}
	}
	for name, list := range p.PlatformCommands {
		if len(list) == 0 {
			return planInvalid("platform_commands", "platform %s has an empty list", name)
		}
		for i, c := range list {
			if strings.ContainsRune(c, 0) {
				return planInvalid("platform_commands", "platform %s command %d contains a NUL byte", name, i+1)
			}
		}
	}
	if len(p.Commands) == 0 {
		for _, t := range p.Targets {
			if _, ok := p.PlatformCommands[t.Device.Platform]; !ok {
				return planInvalid("platform_commands", "target %s is platform %s, which has no list", t.TargetID, t.Device.Platform)
			}
		}
	}
	for name, list := range p.PlatformFilters {
		if !p.Output.Crun() {
			return planInvalid("platform_filters", "present without a crun's collection")
		}
		// Compiled here with the standard library alone (this package imports
		// only inventory): the same regexp the platform's compiler uses.
		for i, pattern := range list {
			if _, err := regexp.Compile(pattern); err != nil {
				return planInvalid("platform_filters", "platform %s pattern %d does not compile: %v", name, i+1, err)
			}
		}
	}
	if want := SumCommandPlan(p.Commands, p.PlatformCommands); p.CommandPlanDigest != want {
		return planInvalid("command_plan_digest", "%s does not match the commands (%s)", p.CommandPlanDigest, want)
	}
	if c := p.Output.Collection; c != nil {
		if !filepath.IsAbs(c.Directory) {
			return planInvalid("output.collection.directory", "%q is not absolute", c.Directory)
		}
		if !slices.Contains(CollectionFileModes, c.FileMode) {
			return planInvalid("output.collection.file_mode", "%q is not one of %s", c.FileMode, strings.Join(CollectionFileModes, ", "))
		}
		if !slices.Contains(CollectionWords, c.Word) {
			return planInvalid("output.collection.word", "%q is not one of %s", c.Word, strings.Join(CollectionWords, ", "))
		}
		if problem := SuffixProblem(c.Suffix); c.Suffix != "" && problem != "" {
			return planInvalid("output.collection.suffix", "%q %s", c.Suffix, problem)
		}
	}
	if err := p.validateBlindSends(); err != nil {
		return err
	}
	if err := p.validateExecDeclarations(); err != nil {
		return err
	}
	if err := p.validateSessionInit(); err != nil {
		return err
	}
	if err := p.Dispatch.validate(); err != nil {
		return err
	}
	if err := p.Output.validate(); err != nil {
		return err
	}
	if err := p.Ping.validate(); err != nil {
		return err
	}
	if p.Sources.ConfigDigest == "" {
		return planInvalid("sources.config_digest", "is required")
	}
	if p.Sources.Selectors.Sources == nil {
		return planInvalid("sources.selector_provenance.sources", "must be present (empty allowed)")
	}
	if p.Planning.DraftedAt.IsZero() {
		return planInvalid("planning.drafted_at", "is required")
	}
	if p.Preparation == nil {
		return planInvalid("preparation", "must be present (empty allowed)")
	}
	endpoints := map[string]bool{}
	covered := map[string]bool{}
	for i, ev := range p.Preparation {
		field := fmt.Sprintf("preparation[%d]", i)
		if ev.ExecutionEndpoint != EndpointLocal {
			return planInvalid(field+".execution_endpoint", "%q is not supported", ev.ExecutionEndpoint)
		}
		if endpoints[ev.ExecutionEndpoint] {
			return planInvalid(field+".execution_endpoint", "%q appears twice", ev.ExecutionEndpoint)
		}
		endpoints[ev.ExecutionEndpoint] = true
		if !ValidID(ev.PreparationID) {
			return planInvalid(field+".preparation_id", "%q is not a valid identifier", ev.PreparationID)
		}
		if ev.PreparationDigest.IsZero() {
			return planInvalid(field+".preparation_digest", "is required")
		}
		if ev.PreparedAt.IsZero() {
			return planInvalid(field+".prepared_at", "is required")
		}
		if ev.Addresses == nil {
			return planInvalid(field+".addresses", "must be present (empty allowed)")
		}
		for j, a := range ev.Addresses {
			t := p.target(a.TargetID)
			if t == nil {
				return planInvalid(fmt.Sprintf("%s.addresses[%d].target_id", field, j), "%q is not a plan target", a.TargetID)
			}
			if t.AddressPlan.Authority != AddressByDaemon {
				return planInvalid(fmt.Sprintf("%s.addresses[%d].target_id", field, j), "%q is not a daemon-authority target", a.TargetID)
			}
			if t.ExecutionEndpoint != ev.ExecutionEndpoint {
				return planInvalid(fmt.Sprintf("%s.addresses[%d].target_id", field, j), "%q belongs to endpoint %q", a.TargetID, t.ExecutionEndpoint)
			}
			if covered[a.TargetID] {
				return planInvalid(fmt.Sprintf("%s.addresses[%d].target_id", field, j), "%q has evidence twice", a.TargetID)
			}
			covered[a.TargetID] = true
		}
	}
	if stage == Committed {
		if p.Planning.FinalizedAt.IsZero() {
			return planInvalid("planning.finalized_at", "is required before commit")
		}
		for i := range p.Targets {
			t := &p.Targets[i]
			if t.AddressPlan.Authority == AddressByDaemon && !covered[t.TargetID] {
				return planInvalid("preparation", "daemon-authority target %q has no address evidence", t.TargetID)
			}
		}
		if err := p.Verify(); err != nil {
			return err
		}
	}
	return nil
}

// validateBlindSends checks the interactive-prompt fields: the counts
// present, empty or one per command and
// each 0..BlindReturnsMax; the wait 0..BlindWaitMax; the flags present,
// empty or one per command; the declarations present, empty or one list per
// command, each list present and at most ExpectationsMax long, each pattern
// nonempty and compiling; a count above zero only with the flag, since the
// count implies the tolerance and the client records the implication; and
// no command with both a count above zero and a declaration. The daemon
// validates the plan it is given, so these rules hold whatever client
// drafted it.
// validateExecDeclarations refuses a declaration that answers a terminal
// on a command for an exec target: a blind send (the flag, which a count
// of returns implies) or an expectation. An exec channel has no terminal
// and no prompt. The client refuses it at planning and the daemon's check
// refuses a plan that carries it, naming the target and the command's
// index; --literal leaves nothing here.
func (p *ExecutionPlan) validateExecDeclarations() error {
	for _, t := range p.Targets {
		if t.Channel != ChannelExec {
			continue
		}
		for i := range p.Commands {
			kind := ""
			switch {
			case i < len(p.Blind) && p.Blind[i]:
				kind = "a blind send (--blind, --blind-return, or a trailing \\r)"
			case i < len(p.Expectations) && len(p.Expectations[i]) > 0:
				kind = "an --expect"
			}
			if kind != "" {
				return fmt.Errorf("channel_exec_declaration_refused: %s: command %d carries %s, which answers a terminal, and the target's platform runs it on an exec channel, which has none; remove the declaration or use a platform on the shell channel", t.Device.CanonicalName, i+1, kind)
			}
		}
	}
	return nil
}

func (p *ExecutionPlan) validateBlindSends() error {
	if p.BlindReturns == nil {
		return planInvalid("blind_returns", "must be present (empty allowed)")
	}
	if len(p.BlindReturns) != 0 && len(p.BlindReturns) != len(p.Commands) {
		return planInvalid("blind_returns", "%d entries for %d commands; empty or one per command", len(p.BlindReturns), len(p.Commands))
	}
	for i, n := range p.BlindReturns {
		if n < 0 || n > BlindReturnsMax {
			return planInvalid("blind_returns", "command %d: %d must be 0..%d", i+1, n, BlindReturnsMax)
		}
	}
	if d := time.Duration(p.BlindWaitNS); d < 0 || d > BlindWaitMax {
		return planInvalid("blind_wait_ns", "%d must be 0..10m", p.BlindWaitNS)
	}
	if p.Blind == nil {
		return planInvalid("blind", "must be present (empty allowed)")
	}
	if len(p.Blind) != 0 && len(p.Blind) != len(p.Commands) {
		return planInvalid("blind", "%d entries for %d commands; empty or one per command", len(p.Blind), len(p.Commands))
	}
	for i, n := range p.BlindReturns {
		if n > 0 && (len(p.Blind) == 0 || !p.Blind[i]) {
			return planInvalid("blind", "command %d: %d blind returns require the flag", i+1, n)
		}
	}
	if p.Expectations == nil {
		return planInvalid("expectations", "must be present (empty allowed)")
	}
	if len(p.Expectations) != 0 && len(p.Expectations) != len(p.Commands) {
		return planInvalid("expectations", "%d entries for %d commands; empty or one per command", len(p.Expectations), len(p.Commands))
	}
	for i, list := range p.Expectations {
		if list == nil {
			return planInvalid("expectations", "command %d: must be present (empty allowed)", i+1)
		}
		if len(list) > ExpectationsMax {
			return planInvalid("expectations", "command %d: %d declarations; at most %d", i+1, len(list), ExpectationsMax)
		}
		if len(list) > 0 && i < len(p.BlindReturns) && p.BlindReturns[i] > 0 {
			return planInvalid("expectations", "command %d: carries %d blind returns and a declaration; a command takes one or the other", i+1, p.BlindReturns[i])
		}
		for j, d := range list {
			if d.Pattern == "" {
				return planInvalid("expectations", "command %d declaration %d: the pattern is required", i+1, j+1)
			}
			if _, err := regexp.Compile(d.Pattern); err != nil {
				return planInvalid("expectations", "command %d declaration %d: pattern %q: %v", i+1, j+1, d.Pattern, err)
			}
		}
	}
	return nil
}

// validateSessionInit checks the session-init table and each target's
// selection: every entry well formed and selected by a target, every
// target's profile none or an entry, as the daemon applies it. A draft's
// targets carry no profile yet.
func (p *ExecutionPlan) validateSessionInit() error {
	if p.SessionInit == nil {
		return planInvalid("session_init", "must be present (empty allowed)")
	}
	selected := map[string]bool{}
	for i, t := range p.Targets {
		if t.SessionInitProfile == "" || t.SessionInitProfile == SessionInitNone {
			continue
		}
		if _, ok := p.SessionInit[t.SessionInitProfile]; !ok {
			return planInvalid(fmt.Sprintf("targets[%d].session_init_profile", i), "%q is not %s or a profile in session_init", t.SessionInitProfile, SessionInitNone)
		}
		selected[t.SessionInitProfile] = true
	}
	names := make([]string, 0, len(p.SessionInit))
	for name := range p.SessionInit {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		prof := p.SessionInit[name]
		field := fmt.Sprintf("session_init[%q]", name)
		switch name {
		case "":
			return planInvalid(field, "the name is required")
		case SessionInitNone:
			return planInvalid(field, "the name %s is reserved", SessionInitNone)
		}
		if prof.Commands == nil {
			return planInvalid(field+".commands", "must be present (empty allowed)")
		}
		for i, c := range prof.Commands {
			if strings.ContainsRune(c, 0) {
				return planInvalid(field+".commands", "command %d contains a NUL byte", i+1)
			}
		}
		switch prof.OnError {
		case SessionInitFailDevice, SessionInitContinue:
		default:
			return planInvalid(field+".on_error", "%q is not %s or %s", prof.OnError, SessionInitFailDevice, SessionInitContinue)
		}
		if d := time.Duration(prof.CommandTimeoutNS); d != 0 && (d < SessionInitMinTimeout || d > SessionInitMaxTimeout) {
			return planInvalid(field+".command_timeout_ns", "%d must be 0 or 1s..12h", prof.CommandTimeoutNS)
		}
		if !selected[name] {
			return planInvalid(field, "no target selects it")
		}
	}
	return nil
}

func (p *ExecutionPlan) target(id string) *ExecutionTarget {
	for i := range p.Targets {
		if p.Targets[i].TargetID == id {
			return &p.Targets[i]
		}
	}
	return nil
}

// IncorporatePreparation copies each address evidence entry into its target,
// exactly the fields the address contract delegates to the daemon, verifies the
// resolution digest the daemon computed over them, and appends the evidence.
// It returns a new plan; the input is not modified.
func IncorporatePreparation(plan ExecutionPlan, evidence PreparationEvidence) (ExecutionPlan, error) {
	out := plan
	out.Targets = append([]ExecutionTarget(nil), plan.Targets...)
	out.Preparation = append([]PreparationEvidence(nil), plan.Preparation...)
	for _, existing := range out.Preparation {
		if existing.ExecutionEndpoint == evidence.ExecutionEndpoint {
			return plan, planInvalid("preparation", "endpoint %q already has evidence", evidence.ExecutionEndpoint)
		}
	}
	for _, a := range evidence.Addresses {
		t := out.target(a.TargetID)
		if t == nil {
			return plan, planInvalid("preparation.addresses", "%q is not a plan target", a.TargetID)
		}
		if t.AddressPlan.Authority != AddressByDaemon {
			return plan, planInvalid("preparation.addresses", "%q is not a daemon-authority target", a.TargetID)
		}
		if t.ExecutionEndpoint != evidence.ExecutionEndpoint {
			return plan, planInvalid("preparation.addresses", "%q belongs to endpoint %q", a.TargetID, t.ExecutionEndpoint)
		}
		t.AddressPlan.DaemonCandidates = append([]netip.Addr{}, a.DaemonCandidates...)
		t.AddressPlan.Selected = a.Selected
		t.AddressPlan.Alternates = append([]netip.Addr{}, a.Alternates...)
		t.AddressPlan.SelectedSource = a.SelectedSource
		t.AddressPlan.ResolverContext = a.ResolverContext
		t.AddressPlan.ResolutionDigest = a.ResolutionDigest
		sum, err := SumResolution(*t)
		if err != nil {
			return plan, err
		}
		if sum != a.ResolutionDigest {
			return plan, invalid("address_plan.resolution_digest", "evidence for %q records %s, fields hash to %s", a.TargetID, a.ResolutionDigest, sum)
		}
	}
	out.Preparation = append(out.Preparation, evidence)
	if evidence.PreparedAt.After(out.Planning.PreparedAt) {
		out.Planning.PreparedAt = evidence.PreparedAt
	}
	return out, nil
}

// Finalize records the finalization time and the final digest.
func Finalize(plan ExecutionPlan, now time.Time) (ExecutionPlan, error) {
	plan.Planning.FinalizedAt = now
	sum, err := SumPlan(plan)
	if err != nil {
		return plan, err
	}
	plan.PlanDigest = sum
	return plan, nil
}

// problem names the first invalid operator field, or "" when valid; the
// caller attaches its own code.
func (o Operator) problem() (field, msg string) {
	if strings.TrimSpace(o.Username) == "" {
		return "username", "is required"
	}
	if o.UID < 0 {
		return "uid", "must not be negative"
	}
	return "", ""
}

func (d DispatchSettings) validate() error {
	switch d.Mode {
	case DispatchSerial, DispatchParallel, DispatchWave:
	default:
		return planInvalid("dispatch.mode", "%q is not serial, parallel, or wave", d.Mode)
	}
	switch d.DispatchOrder {
	case OrderDefault, OrderSorted:
		if d.ShuffleKey != nil {
			return planInvalid("dispatch.shuffle_key", "must be absent for order %q", d.DispatchOrder)
		}
	case OrderShuffle, OrderRandom:
		if d.ShuffleKey == nil {
			return planInvalid("dispatch.shuffle_key", "is required for order %q", d.DispatchOrder)
		}
	default:
		return planInvalid("dispatch.dispatch_order", "%q is not default, sorted, shuffle, or random", d.DispatchOrder)
	}
	for field, v := range map[string]int{"dispatch.width": d.Width, "dispatch.start_width": d.StartWidth, "dispatch.max_width": d.MaxWidth, "dispatch.halt_error_count": d.HaltErrorCount, "dispatch.wave_gate_error_count": d.WaveGateErrorCount} {
		if v < 0 {
			return planInvalid(field, "must not be negative")
		}
	}
	for field, v := range map[string]int{"dispatch.halt_error_percent": d.HaltErrorPercent, "dispatch.wave_gate_error_percent": d.WaveGateErrorPercent} {
		if v < 0 || v > 100 {
			return planInvalid(field, "must be 0..100")
		}
	}
	if d.WaveGateTimedDelayNS < 0 {
		return planInvalid("dispatch.wave_gate_timed_delay_ns", "must not be negative")
	}
	return nil
}

func (o OutputSettings) validate() error {
	switch o.Format {
	case FormatText, FormatJSONL, FormatJSON:
	default:
		return planInvalid("output.format", "%q is not text, jsonl, or json", o.Format)
	}
	if o.MaxCommandBytes <= 0 || o.MaxJobBytes <= 0 {
		return planInvalid("output", "max_command_bytes and max_job_bytes must be positive")
	}
	if !filepath.IsAbs(o.Root) {
		return planInvalid("output.root", "%q is not an absolute path", o.Root)
	}
	return nil
}

func (p PingSettings) validate() error {
	if p.Probes != PingProbes {
		return planInvalid("ping.probes", "%d is not %d", p.Probes, PingProbes)
	}
	if p.TimeoutNS <= 0 {
		return planInvalid("ping.timeout_ns", "must be positive")
	}
	return nil
}

// Validate checks the header at the given stage: Draft for prepare_job,
// Committed for commit_job.
func (h *PublicJobHeader) Validate(stage Stage) error {
	if h.SchemaVersion != SchemaVersion {
		return headerInvalid("schema_version", "%d is not the supported version %d", h.SchemaVersion, SchemaVersion)
	}
	if err := identifier("idempotency_key", h.IdempotencyKey); err != nil {
		return headerInvalid("idempotency_key", "%v", err)
	}
	if !ValidJobID(h.JobID) {
		return headerInvalid("job_id", "%q is not a job ID (YYMMDD-HHMMSS-xx)", h.JobID)
	}
	if f, m := h.Operator.problem(); f != "" {
		return headerInvalid("operator."+f, "%s", m)
	}
	if strings.TrimSpace(h.Client.AppName) == "" || strings.TrimSpace(h.Client.Version) == "" {
		return headerInvalid("client", "app_name and version are required")
	}
	switch h.Mode {
	case ModeLive, ModeExercise:
	default:
		return headerInvalid("mode", "%q is not live or exercise", string(h.Mode))
	}
	if h.ExecutionDomain != ExecutionDomainLocal {
		return headerInvalid("execution_domain", "%q is not supported; v1 accepts %q", h.ExecutionDomain, ExecutionDomainLocal)
	}
	if h.Priority != 0 {
		return headerInvalid("priority", "%d is reserved; v1 accepts 0", h.Priority)
	}
	if h.CommandPlanDigest.IsZero() {
		return headerInvalid("command_plan_digest", "is required")
	}
	if h.PlanDigest.IsZero() {
		return headerInvalid("plan_digest", "is required")
	}
	switch stage {
	case Draft:
		if h.CredentialPackage != nil {
			return headerInvalid("credential_package", "must be absent at prepare_job")
		}
	case Committed:
		if h.CredentialPackage == nil {
			return headerInvalid("credential_package", "is required at commit_job")
		}
		switch h.CredentialPackage.Protection {
		case ProtectionLocalPeer, ProtectionSealed:
		default:
			return headerInvalid("credential_package.protection", "%q is not local-peer or sealed", h.CredentialPackage.Protection)
		}
		if h.CredentialPackage.Digest.IsZero() {
			return headerInvalid("credential_package.digest", "is required")
		}
	}
	return nil
}

// Matches checks that the header describes plan at the given stage: the
// command-plan digest, the plan digest (draft or final), and the execution
// domain of every target.
func (h *PublicJobHeader) Matches(plan *ExecutionPlan, stage Stage) error {
	if h.CommandPlanDigest != plan.CommandPlanDigest {
		return headerInvalid("command_plan_digest", "%s differs from the plan's %s", h.CommandPlanDigest, plan.CommandPlanDigest)
	}
	sum, err := SumPlan(*plan)
	if err != nil {
		return headerInvalid("plan_digest", "cannot encode the plan: %v", err)
	}
	if h.PlanDigest != sum {
		return fmt.Errorf("plan_digest_mismatch: header %s, plan %s", h.PlanDigest, sum)
	}
	if stage == Committed && plan.PlanDigest != sum {
		return fmt.Errorf("plan_digest_mismatch: header %s, recorded %s", h.PlanDigest, plan.PlanDigest)
	}
	for _, t := range plan.Targets {
		if t.ExecutionEndpoint != h.ExecutionDomain {
			return headerInvalid("execution_domain", "target %q runs at %q, outside domain %q", t.TargetID, t.ExecutionEndpoint, h.ExecutionDomain)
		}
	}
	return nil
}
