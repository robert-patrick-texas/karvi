package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/app"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/display"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/exitcode"
	"github.com/robert-patrick-texas/karvi/internal/jobexec"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
	"github.com/robert-patrick-texas/karvi/internal/output"
	"github.com/robert-patrick-texas/karvi/internal/planner"
	"github.com/robert-patrick-texas/karvi/internal/scoreboard"
	"github.com/robert-patrick-texas/karvi/internal/transcript"
	"github.com/robert-patrick-texas/karvi/internal/transportselect"
)

const (
	loginTranscriptChildEnv = "KARVI_LOGIN_TRANSCRIPT_CHILD"
	// loginDiagnosticsFDEnv names the descriptor the child writes diagnostics
	// to: the wrapper's real stderr, so karvi's own lines never enter the
	// transcript.
	loginDiagnosticsFDEnv = "KARVI_LOGIN_DIAGNOSTICS_FD"
)

// recorderDiagnostics returns the diagnostics descriptor the recording wrapper
// passed to this child, or nil.
func recorderDiagnostics() *os.File {
	v := os.Getenv(loginDiagnosticsFDEnv)
	if v == "" {
		return nil
	}
	fd, err := strconv.Atoi(v)
	if err != nil || fd < 3 {
		return nil
	}
	return os.NewFile(uintptr(fd), "karvi-diagnostics")
}

// recordedLogin records a login session through util-linux script(1), which
// captures only the stream the device returns. The
// wrapper resolves the destination and layout:
// it assembles the target set to learn the device, claims the transcript and
// metadata pair under the day folder, writes the metadata start record,
// launches the child with the original arguments, and at the end strips the
// script(1) marker lines, writes the metadata end record, and names the
// transcript again as the header did (display.record.header and
// display.record.footer, one line each by default). The child parses
// the same invocation, uses the session ID given here, and does not record.
func recordedLogin(inv *Invocation, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	streams := app.IO{Stdin: stdin, Stdout: stdout, Stderr: stderr}
	inputs, code, ok := targetInputs(inv, streams)
	if !ok {
		return code
	}
	common := inv.common()
	cfg, err := app.LoadConfig(common)
	if err != nil {
		return reportError(stderr, "config_load_failed", err)
	}
	// --platform is checked here as the child checks it: after the
	// configuration and before the target set and the
	// transcript claim, so a refused value creates no transcript file and
	// launches no child. The name is the metadata's platform.
	platformName, err := app.CheckPlatformOption(cfg, inv.String(optPlatform))
	if err != nil {
		return reportError(stderr, "platform_option_unknown", err)
	}
	format, metaFormat := cfg.String("transcript.format"), cfg.String("transcript.metadata-format")
	if format != transcript.FormatText {
		return reportError(stderr, "transcript_format_unavailable", errorcodes.Errorf("transcript_format_unavailable", "transcript.format %q requires the transcript event recorder, which this executable does not implement; use text", format))
	}
	operator, err := osutil.CurrentOperator()
	if err != nil {
		return reportError(stderr, "operator_identity_unavailable", err)
	}
	base, err := osutil.ResolveBaseDir(cfg.String("basedir"), operator.Home, operator.Username)
	if err != nil {
		return reportError(stderr, "base_directory_unavailable", err)
	}
	directoryMode := osutil.DirectoryMode(cfg.String("output.directory-mode"))
	if err := osutil.EnsureStateTree(base, operator.UID, directoryMode); err != nil {
		return reportError(stderr, "state_tree_create_failed", err)
	}
	location, err := display.Location(cfg.String("timezone"))
	if err != nil {
		return reportError(stderr, "config_display_timestamp_invalid", err)
	}
	set, err := app.AssembleTargets(context.Background(), common, inputs, inv.Strings(optExclude), stderr)
	if err != nil {
		return reportError(stderr, "inventory_load_failed", err)
	}
	if err := app.CheckManagementAddress(set, inv.String(optAddress)); err != nil {
		return reportError(stderr, "management_address_invalid", err)
	}
	device := set.Devices[0]
	// The platform the session uses, for the metadata: an unknown row
	// platform is refused here, before the transcript claim and the child;
	// the child prints any warning line.
	if platformName == "" {
		resolution, err := planner.ResolvePlatform(cfg, device)
		if err != nil {
			return reportError(stderr, "platform_unknown", err)
		}
		platformName = resolution.Platform
	}
	selection, err := transportselect.Resolve(cfg, "login", inv.String(optTransport))
	if err != nil {
		return reportError(stderr, "transport_unavailable", err)
	}
	// The two lines naming the transcript are display templates, checked
	// here with the formatter so a bad one creates no transcript and
	// launches no child.
	formatter, err := display.NewFormatter(cfg.String("display.timestamp"), cfg.String("timezone"))
	if err != nil {
		return reportError(stderr, "config_display_timestamp_invalid", err)
	}
	for _, key := range []string{"display.record.header", "display.record.footer"} {
		if err := display.ValidateLineTemplate(cfg.String(key)); err != nil {
			return reportError(stderr, "config_display_template_invalid", err)
		}
	}
	started := time.Now()
	dest, err := transcript.Resolve(*inv.Record, cfg.String("transcript.root"), cfg.String("sharedroot"), base, operator.Home, cfg.Locked("transcript.root"), started, location)
	if err != nil {
		return reportError(stderr, "transcript_path_invalid", err)
	}
	// The transcripts root's free space: a session's length is nobody's
	// estimate, so the root is
	// floored alone under `freecheck`, before the recorder and the child.
	if _, err := (output.Preflight{Check: cfg.String("freecheck"), Floor: cfg.Int64("output.min-free-bytes-after-job"), Places: []output.Place{{Path: dest.Root}}}).Run(); err != nil {
		return reportError(stderr, "output_preflight_space", err)
	}
	scriptPath, err := exec.LookPath("script")
	if err != nil {
		return reportError(stderr, "dependency_script_unavailable", fmt.Errorf("karvi login recording requires util-linux script(1): %w", err))
	}
	executable, err := os.Executable()
	if err != nil {
		return reportError(stderr, "karvi_executable_unlocatable", err)
	}
	// The session ID is the login's activity ID in the job form,
	// YYMMDD-HHMMSS-xx, reserved here by the scoreboard file the child will
	// write: the wrapper needs it for the
	// metadata's start record before the child runs, and the child takes it
	// from the environment. The reservation is released if the child never
	// writes its first snapshot.
	sb, err := scoreboard.NewWriter(cfg.String("watch.directory"), filepath.Join(base, "state", "scoreboards"), cfg.Bool("watch.enabled"), func(s string) { fmt.Fprintf(stderr, "warning: %s\n", s) })
	if err != nil {
		return reportError(stderr, "scoreboard_directory_unavailable", err)
	}
	sessionID, err := sb.Reserve(started, location)
	if err != nil {
		return reportError(stderr, "activity_id_generation_failed", err)
	}
	defer sb.Release()
	pair, err := transcript.Claim(dest, device.CanonicalName, started, location, transcript.TranscriptExtension(format), transcript.MetadataExtension(metaFormat), directoryMode)
	if err != nil {
		return reportError(stderr, "transcript_create_failed", err)
	}
	rows, columns := 0, 0
	if f, ok := stdout.(*os.File); ok {
		rows, columns = osutil.TerminalSize(f)
	}
	meta := transcript.Metadata{
		SessionID: sessionID, OperatorUsername: operator.Username, OperatorUID: operator.UID,
		InputTarget: device.SuppliedName(), DeviceName: strings.ToLower(device.CanonicalName), CanonicalName: device.CanonicalName, Platform: platformName,
		Transport: selection.Implementation, DispatchOrder: set.Order, ShuffleKey: set.ShuffleKey, CandidateCount: set.CandidateCount(),
		TranscriptFile: filepath.Base(pair.Transcript), TranscriptFormat: format, Rows: rows, Columns: columns, StartedAt: started,
	}
	if err := transcript.WriteStart(pair.Metadata, metaFormat, meta); err != nil {
		return reportError(stderr, "transcript_create_failed", err)
	}
	commandParts := []string{shellQuote(executable)}
	for _, arg := range args {
		commandParts = append(commandParts, shellQuote(arg))
	}
	// -a appends to the pre-created 0640 file, so script(1) neither creates
	// nor truncates it.
	cmd := exec.Command(scriptPath, "-q", "-e", "-f", "-a", "-c", strings.Join(commandParts, " "), pair.Transcript)
	cmd.Env = append(os.Environ(), loginTranscriptChildEnv+"=1", "KARVI_LOGIN_TRANSCRIPT_PATH="+pair.Transcript, app.RecorderSessionIDEnv+"="+sessionID)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	if f, ok := stderr.(*os.File); ok {
		cmd.ExtraFiles = []*os.File{f} // descriptor 3 in the child
		cmd.Env = append(cmd.Env, loginDiagnosticsFDEnv+"=3")
	}
	values := display.Values{Timestamp: started, Target: device.CanonicalName, Platform: platformName, Transport: selection.Implementation, Transcript: pair.Transcript}
	if !inv.Global.quiet {
		writeRecordLine(cfg, formatter, "display.record.header", values, stderr)
	}
	exit := 0
	recordingFailed := false
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exit = exitErr.ExitCode()
		} else {
			recordingFailed = true
			exit = reportError(stderr, "transcript_recorder_failed", err)
		}
	}
	if err := transcript.StripScriptMarkers(pair.Transcript); err != nil {
		recordingFailed = true
		msg := errorcodes.Message(errorcodes.Ensure(err, "transcript_create_failed"))
		fmt.Fprintf(stderr, "warning: %s keeps the script(1) marker lines: %s\n", pair.Transcript, msg)
	}
	meta.EndedAt = time.Now()
	meta.ExitClassification = exitcode.ExitName(exit)
	meta.RecordingFailed = recordingFailed
	if sum, err := transcript.FileSHA256(pair.Transcript); err == nil {
		meta.TranscriptSHA256 = sum
	} else {
		meta.RecordingFailed = true
	}
	if err := transcript.WriteEnd(pair.Metadata, metaFormat, meta); err != nil {
		code := reportError(stderr, "transcript_create_failed", err)
		if exit == 0 {
			exit = code
		}
	}
	// The header's line once more at the end, after the device's last
	// output, so the operator leaving the session reads where it was kept;
	// a failed session has its transcript too.
	if !inv.Global.quiet {
		values.Timestamp, values.Elapsed = meta.EndedAt, meta.EndedAt.Sub(started)
		values.ExitCode, values.ExitStatus = exit, fmt.Sprintf("%s(%d)", exitcode.ExitName(exit), exit)
		writeRecordLine(cfg, formatter, "display.record.footer", values, stderr)
	}
	return exit
}

// writeRecordLine renders a display.record template as the login's header
// and footer are rendered, with the display's formatter, colors, and
// width, for the terminal on w; an empty template writes nothing. The
// formatter and both templates were checked before the transcript was
// claimed, so nothing here can refuse, and the session's outcome stands.
func writeRecordLine(cfg configload.Snapshot, formatter display.Formatter, key string, values display.Values, w io.Writer) {
	lines, err := formatter.RenderStyledLines(cfg.String(key), values, jobexec.DisplayLineStyle(cfg, jobexec.DisplayTerminal(w)), jobexec.DisplayTerminalWidth(w))
	if err != nil {
		return
	}
	for _, line := range lines {
		if line != "" {
			fmt.Fprintln(w, line)
		}
	}
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
