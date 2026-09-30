package ipc

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/records"
)

// PrepareCommitSchema is the IPC schema that carries prepare_job and
// commit_job; SchemaVersion is it.
const PrepareCommitSchema = SchemaVersion

// Operations of schema 5, unchanged in 6. provide_credentials is not an envelope operation:
// its request is the credential frame and its result the receipt.
const (
	OpPrepareJob         = "prepare_job"
	OpCommitJob          = "commit_job"
	OpProvideCredentials = "provide_credentials"
)

// PreparationLifetime bounds prepare_job to commit_job; it equals the
// credential-package acceptance window.
const PreparationLifetime = 10 * time.Minute

// PrepareRequest is the prepare_job payload.
type PrepareRequest struct {
	Header executionplan.PublicJobHeader `json:"header"`
	Draft  executionplan.ExecutionPlan   `json:"draft"`
}

// Validate checks the header at prepare stage, the draft, and that the
// header describes the draft.
func (r *PrepareRequest) Validate() error {
	if err := r.Header.Validate(executionplan.Draft); err != nil {
		return err
	}
	if err := r.Draft.Validate(executionplan.Draft); err != nil {
		return err
	}
	return r.Header.Matches(&r.Draft, executionplan.Draft)
}

// PrepareResult is the prepare_job result.
type PrepareResult struct {
	Preparation       executionplan.PreparationReport `json:"preparation"`
	CredentialChannel CredentialChannel               `json:"credential_channel"`
}

// LogValue logs the preparation identity and the redacted channel.
func (r PrepareResult) LogValue() slog.Value {
	return slog.GroupValue(slog.String("preparation_id", r.Preparation.PreparationID), slog.Bool("accepted", r.Preparation.Accepted), slog.Any("credential_channel", r.CredentialChannel))
}

// Validate checks the result; the channel is required only when the
// preparation was accepted.
func (r *PrepareResult) Validate() error {
	if err := r.Preparation.Validate("preparation"); err != nil {
		return err
	}
	if !r.Preparation.Accepted {
		return nil
	}
	if r.CredentialChannel.Socket == "" || !strings.HasPrefix(r.CredentialChannel.Socket, "/") {
		return errorcodes.Errorf("ipc_result_malformed", "credential_channel.socket must be an absolute path")
	}
	if r.CredentialChannel.Token.IsZero() {
		return errorcodes.Errorf("ipc_result_malformed", "credential_channel.token is required")
	}
	if !r.CredentialChannel.ExpiresAt.Equal(r.Preparation.ExpiresAt) {
		return errorcodes.Errorf("ipc_result_malformed", "credential_channel.expires_at must equal the preparation expiry")
	}
	if r.CredentialChannel.MaxFrame <= 0 {
		return errorcodes.Errorf("ipc_result_malformed", "credential_channel.max_frame_bytes must be positive")
	}
	return nil
}

// CredentialChannel tells the client where and how to send the package.
// Every container of a ChannelToken implements LogValue: slog's JSON
// handler serializes a struct attribute with encoding/json, which would
// print the token's hex form past the token's own redaction.
type CredentialChannel struct {
	Socket    string       `json:"socket"`
	Token     ChannelToken `json:"token"`
	ExpiresAt time.Time    `json:"expires_at"`
	MaxFrame  int64        `json:"max_frame_bytes"`
}

// LogValue redacts the token and keeps the rest.
func (c CredentialChannel) LogValue() slog.Value {
	return slog.GroupValue(slog.String("socket", c.Socket), slog.String("token", "<redacted>"), slog.Time("expires_at", c.ExpiresAt), slog.Int64("max_frame_bytes", c.MaxFrame))
}

// ChannelToken is the one-use capability that binds a credential frame to
// its preparation. It must cross the JSON envelope, so it encodes as hex
// text; it redacts under fmt, slog, and templates.
type ChannelToken [32]byte

// NewChannelToken returns 32 random bytes.
func NewChannelToken() (ChannelToken, error) {
	var t ChannelToken
	if _, err := rand.Read(t[:]); err != nil {
		return ChannelToken{}, err
	}
	return t, nil
}

func (t ChannelToken) IsZero() bool { return t == ChannelToken{} }

// Equal compares in constant time.
func (t ChannelToken) Equal(o ChannelToken) bool { return subtle.ConstantTimeCompare(t[:], o[:]) == 1 }

func (t ChannelToken) MarshalText() ([]byte, error) { return []byte(hex.EncodeToString(t[:])), nil }

func (t *ChannelToken) UnmarshalText(text []byte) error {
	if len(text) != 64 {
		return errorcodes.Errorf("ipc_result_malformed", "channel token must be 64 hex characters")
	}
	_, err := hex.Decode(t[:], text)
	return err
}

func (t ChannelToken) String() string             { return "<redacted>" }
func (t ChannelToken) GoString() string           { return "<redacted>" }
func (t ChannelToken) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, "<redacted>") }
func (t ChannelToken) LogValue() slog.Value       { return slog.StringValue("<redacted>") }

// CommitRequest is the commit_job payload.
type CommitRequest struct {
	Header       executionplan.PublicJobHeader `json:"header"`
	Plan         executionplan.ExecutionPlan   `json:"plan"`
	Preparations []PreparationReference        `json:"preparations"`
	Mode         executionplan.Mode            `json:"mode"`
}

// PreparationReference names one preparation the plan incorporated.
type PreparationReference struct {
	ExecutionEndpoint string               `json:"execution_endpoint"`
	PreparationID     string               `json:"preparation_id"`
	PreparationDigest executionplan.Digest `json:"preparation_digest"`
}

// Validate checks the header at commit stage, the final plan, that the
// header describes it, the mode, and that every preparation the plan
// embeds is referenced with the same digest.
func (r *CommitRequest) Validate() error {
	if err := r.Header.Validate(executionplan.Committed); err != nil {
		return err
	}
	if err := r.Plan.Validate(executionplan.Committed); err != nil {
		return err
	}
	if err := r.Header.Matches(&r.Plan, executionplan.Committed); err != nil {
		return err
	}
	if r.Mode != r.Header.Mode {
		return errorcodes.Errorf("job_header_invalid", "mode: request %q differs from header %q", string(r.Mode), string(r.Header.Mode))
	}
	if r.Preparations == nil {
		return errorcodes.Errorf("execution_plan_invalid", "preparations: must be present (empty allowed)")
	}
	refs := map[string]PreparationReference{}
	for _, p := range r.Preparations {
		refs[p.ExecutionEndpoint] = p
	}
	if len(refs) != len(r.Preparations) {
		return errorcodes.Errorf("execution_plan_invalid", "preparations: an endpoint is referenced twice")
	}
	for _, ev := range r.Plan.Preparation {
		ref, ok := refs[ev.ExecutionEndpoint]
		if !ok || ref.PreparationID != ev.PreparationID || ref.PreparationDigest != ev.PreparationDigest {
			return errorcodes.Errorf("plan_digest_mismatch", "preparation %s at %q is not referenced with its digest", ev.PreparationID, ev.ExecutionEndpoint)
		}
	}
	if len(refs) != len(r.Plan.Preparation) {
		return errorcodes.Errorf("execution_plan_invalid", "preparations: references name a preparation the plan does not embed")
	}
	return nil
}

// CommitResult is the commit_job result: the receipt at acceptance. The
// activity outcome arrives through follow_job's terminal event.
type CommitResult struct {
	Receipt JobReceipt `json:"receipt"`
}

// ActivityOutcome mirrors the app's activity result field for field so the
// protocol package does not import the application; it is the payload of
// the follow_job terminal.
type ActivityOutcome struct {
	ExitCode    int             `json:"exit_code"`
	ExitName    string          `json:"exit_name"`
	ActivityID  string          `json:"activity_id"`
	JobID       string          `json:"job_id,omitempty"`
	ArtifactDir string          `json:"artifact_dir,omitempty"`
	Summary     records.Summary `json:"summary"`
	Error       string          `json:"error,omitempty"`
}

// JobReceipt is the accepted job's identity.
type JobReceipt struct {
	JobID       string               `json:"job_id"`
	AcceptedAt  time.Time            `json:"accepted_at"`
	ArtifactDir string               `json:"artifact_dir"`
	PlanDigest  executionplan.Digest `json:"plan_digest"`
	Mode        executionplan.Mode   `json:"mode"`
	ReportPath  string               `json:"report_path,omitempty"`
	// Warnings are the admission warnings the daemon raised for the job
	// before any device was contacted, each "code: message" (schema 10:
	// spool_width_narrowed), for the client's
	// standard error; absent when none.
	Warnings []string `json:"warnings,omitempty"`
}

// Validate checks the receipt against the request it answers.
func (r JobReceipt) Validate(req *CommitRequest) error {
	if r.JobID != req.Header.JobID {
		return errorcodes.Errorf("ipc_result_malformed", "receipt job_id %q differs from the request's %q", r.JobID, req.Header.JobID)
	}
	if r.AcceptedAt.IsZero() {
		return errorcodes.Errorf("ipc_result_malformed", "receipt needs accepted_at")
	}
	// A job that keeps no folder (output.persist-command false, `run --nof`)
	// has no artifact_dir; one that does names it
	// absolutely.
	if r.ArtifactDir != "" && r.ArtifactDir[0] != '/' {
		return errorcodes.Errorf("ipc_result_malformed", "receipt artifact_dir %q is not absolute", r.ArtifactDir)
	}
	if r.PlanDigest != req.Plan.PlanDigest || r.Mode != req.Mode {
		return errorcodes.Errorf("ipc_result_malformed", "receipt plan digest or mode differs from the request")
	}
	if r.Mode == executionplan.ModeExercise && r.ReportPath == "" {
		return errorcodes.Errorf("ipc_result_malformed", "an exercise receipt names its report")
	}
	return nil
}
