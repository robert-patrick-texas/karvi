package app

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"

	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/records"
)

// readExerciseReport reads the daemon's exercise.json with the job-file
// discipline: no link following, a regular
// file owned by this UID at mode 0640 or stricter; then validates it as an
// exercise report for this job and plan.
func readExerciseReport(path, jobID string, planDigest executionplan.Digest) (records.PlanReport, error) {
	unreadable := func(format string, args ...any) error {
		return errorcodes.Errorf("exercise_report_unreadable", "%s: %s", path, fmt.Sprintf(format, args...))
	}
	if path == "" {
		return records.PlanReport{}, errorcodes.Errorf("exercise_report_unreadable", "the receipt names no report")
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return records.PlanReport{}, unreadable("open: %v", err)
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return records.PlanReport{}, unreadable("stat: %v", err)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || !fi.Mode().IsRegular() {
		return records.PlanReport{}, unreadable("not a regular file")
	}
	if int(st.Uid) != os.Geteuid() {
		return records.PlanReport{}, unreadable("owned by uid %d, not %d", st.Uid, os.Geteuid())
	}
	if fi.Mode().Perm()&0o137 != 0 {
		return records.PlanReport{}, unreadable("mode %04o; the report is 0640 or stricter", fi.Mode().Perm())
	}
	raw, err := io.ReadAll(f)
	if err != nil {
		return records.PlanReport{}, unreadable("read: %v", err)
	}
	var report records.PlanReport
	if err := json.Unmarshal(raw, &report); err != nil {
		return records.PlanReport{}, errorcodes.Errorf("exercise_report_invalid", "%s: %v", path, err)
	}
	if err := report.Validate(); err != nil {
		return records.PlanReport{}, errorcodes.Errorf("exercise_report_invalid", "%s: %v", path, err)
	}
	if report.Kind != records.ReportExercise || report.JobID != jobID || report.PlanDigest != planDigest {
		return records.PlanReport{}, errorcodes.Errorf("exercise_report_invalid", "%s: names kind %s job %s plan %s, not this exercise", path, report.Kind, report.JobID, report.PlanDigest)
	}
	return report, nil
}

// renderExercise writes the report in the run's --format.
func renderExercise(out io.Writer, format string, r records.PlanReport, socket string) error {
	switch format {
	case "json":
		b, err := json.MarshalIndent(r, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(out, "%s\n", b)
		return err
	case "jsonl":
		b, err := json.Marshal(r)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(out, "%s\n", b)
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "exercise %s\n", r.JobID)
	fmt.Fprintf(&b, "status: %s\n", r.Outcome)
	fmt.Fprintf(&b, "job_submitted: %t\ndevice_contacted: %t\n", r.JobSubmitted, r.DeviceContacted)
	for _, d := range r.Daemons {
		fmt.Fprintf(&b, "daemon %s: %s pid=%d version=%s ipc_schema=%d policy_digest=%s", d.ExecutionEndpoint, d.Status, d.PID, d.Version, d.IPCSchemaVersion, shortDigest(d.PolicyDigest))
		if socket != "" {
			fmt.Fprintf(&b, " socket=%s", socket)
		}
		b.WriteString("\n")
	}
	for _, p := range r.Preparations {
		state := "refused"
		if p.Accepted {
			state = "accepted"
		}
		fmt.Fprintf(&b, "preparation %s: %s, %d daemon DNS answer(s)\n", p.PreparationID, state, len(p.Evidence.Addresses))
	}
	for _, f := range r.Findings {
		fmt.Fprintf(&b, "finding: %s %s %s\n", f.Severity, f.Code, f.Message)
	}
	for i, t := range r.Targets {
		pt := r.Plan.Targets[i]
		fmt.Fprintf(&b, "- %s: %s\n", t.TargetID, t.Readiness)
		if t.Address.Authority == executionplan.AddressByDaemon {
			fmt.Fprintf(&b, "  address: daemon %s (resolver_context=%s)\n", t.Address.Selected, pt.AddressPlan.ResolverContext)
		} else {
			fmt.Fprintf(&b, "  address: client %s\n", t.Address.Selected)
		}
		if t.CredentialBinding.Status == records.BindingBound {
			fmt.Fprintf(&b, "  credential: bound %s (policy=%s backend=%s user=%s; value not displayed)\n", t.CredentialBinding.CredentialID, t.CredentialBinding.Policy, t.CredentialBinding.Backend, t.CredentialBinding.DeviceUsername)
		} else {
			fmt.Fprintf(&b, "  credential: %s\n", t.CredentialBinding.Status)
		}
		transport := fmt.Sprintf("  transport: %s %s", t.IntendedTransport.Transport, t.IntendedTransport.Available)
		for _, f := range t.Findings {
			switch f.Code {
			case "transport_assessment":
				if f.Details["binary"] != "" {
					transport += fmt.Sprintf(" (%s %s)", f.Details["implementation"], f.Details["binary"])
				} else if f.Details["implementation"] != "" {
					transport += fmt.Sprintf(" (%s)", f.Details["implementation"])
				}
			case "host_key_enrollment":
				transport += fmt.Sprintf(" host_key=%s enrolled=%s", t.IntendedTransport.HostKeyPolicy, yesNo(f.Details["enrolled"]))
			}
		}
		if t.IntendedTransport.HostKeyPolicy != "" && !strings.Contains(transport, "host_key=") {
			transport += " host_key=" + t.IntendedTransport.HostKeyPolicy
		}
		b.WriteString(transport + "\n")
		fmt.Fprintf(&b, "  intended_ping: %s\n", describeIntendedPing(t.IntendedPing))
		for _, f := range t.Findings {
			if f.Severity == executionplan.SeverityInfo {
				continue
			}
			fmt.Fprintf(&b, "  finding: %s %s %s\n", f.Severity, f.Code, f.Message)
		}
	}
	fmt.Fprintf(&b, "next: %s (no device was contacted; this is not a command result)\n", r.NextOperation)
	_, err := io.WriteString(out, b.String())
	return err
}

func yesNo(v string) string {
	if v == "true" {
		return "yes"
	}
	return "no"
}
