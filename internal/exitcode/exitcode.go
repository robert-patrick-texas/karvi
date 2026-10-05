package exitcode

// Exit codes are a stable external API. Existing values must never be reused or
// renumbered; new execution outcomes append at 115 or greater.
const (
	ExitSuccess                   = 0
	ExitGenericError              = 1
	ExitConfigValidationError     = 2
	ExitConfigLockViolation       = 3
	ExitUsageError                = 4
	ExitInventoryError            = 5
	ExitCredentialResolutionError = 6
	ExitNameResolutionError       = 7
	ExitDependencyError           = 8
	ExitPermissionError           = 9

	ExitPartialFailure        = 101
	ExitHaltErrorCount        = 102
	ExitHaltErrorPercent      = 103
	ExitWaveGateErrorCount    = 104
	ExitWaveGateErrorPercent  = 105
	ExitShutdownIncomplete    = 106
	ExitDeviceFailure         = 107
	ExitAuthenticationFailure = 108
	ExitHostKeyFailure        = 109
	ExitConnectionFailure     = 110
	ExitOutputFailure         = 111
	ExitJobRejected           = 112
	ExitCancelled             = 113
	ExitHaltHostKeyMismatch   = 114
)

// Status is one exit status: its number, the name the records and the audit
// carry, and what it means to an operator holding it.
type Status struct {
	Code    int
	Name    string
	Meaning string
}

// Statuses is every exit status, in order, each defined once: the manual
// page's EXIT STATUS and docs/ERROR-CODES.md are generated from it, and a
// test holds every constant above to exactly one entry.
var Statuses = []Status{
	{ExitSuccess, "ExitSuccess", "Every device succeeded, or the command did what it was asked."},
	{ExitGenericError, "ExitGenericError", "An internal failure with no more specific status."},
	{ExitConfigValidationError, "ExitConfigValidationError", "The configuration did not load: a file, a key, a value, or its range."},
	{ExitConfigLockViolation, "ExitConfigLockViolation", "A site lock refused a write to a key: --set, an option, the environment, or a lower file."},
	{ExitUsageError, "ExitUsageError", "The command line was refused: an option, a value, a command, or a file it names."},
	{ExitInventoryError, "ExitInventoryError", "The target set could not be built: an inventory source, a target file, a row, or a platform is unreadable or invalid, or nothing was selected."},
	{ExitCredentialResolutionError, "ExitCredentialResolutionError", "No credential could be resolved for a device, or a credential source is invalid or unsafe."},
	{ExitNameResolutionError, "ExitNameResolutionError", "A device name did not resolve to an address of a usable family."},
	{ExitDependencyError, "ExitDependencyError", "A program, transport, or facility karvi needs is missing or would not start."},
	{ExitPermissionError, "ExitPermissionError", "A file or directory karvi must use is missing, unsafe, or not writable, or an action needs root or is not allowed."},
	{ExitPartialFailure, "ExitPartialFailure", "A run in which one or more devices failed or did not start, no halt or gate applying."},
	{ExitHaltErrorCount, "ExitHaltErrorCount", "A run stopped starting devices at its failure count (dispatch.halt-on-error-count)."},
	{ExitHaltErrorPercent, "ExitHaltErrorPercent", "A run stopped starting devices at its failure percent (dispatch.halt-on-error-percent)."},
	{ExitWaveGateErrorCount, "ExitWaveGateErrorCount", "A wave job stopped between waves at the gate's count (dispatch.wave-gate-error-count)."},
	{ExitWaveGateErrorPercent, "ExitWaveGateErrorPercent", "A wave job stopped between waves at the gate's percent (dispatch.wave-gate-error-percent)."},
	{ExitShutdownIncomplete, "ExitShutdownIncomplete", "The daemon stopped before the job ended, or a stop did not finish in time."},
	{ExitDeviceFailure, "ExitDeviceFailure", "The device refused a command, a command exited with a failure status or a signal, a command or the device timed out, or the session's set-up failed."},
	{ExitAuthenticationFailure, "ExitAuthenticationFailure", "The device refused the login."},
	{ExitHostKeyFailure, "ExitHostKeyFailure", "The device's host key was refused, or the trust store could not be used."},
	{ExitConnectionFailure, "ExitConnectionFailure", "A connection to a device or to the daemon failed or was lost."},
	{ExitOutputFailure, "ExitOutputFailure", "Output could not be written: a record, a file, the audit, or standard output."},
	{ExitJobRejected, "ExitJobRejected", "The daemon refused the request, or the exchange with it failed."},
	{ExitCancelled, "ExitCancelled", "Cancelled: job cancel, Ctrl-C, or an interrupted wait."},
	{ExitHaltHostKeyMismatch, "ExitHaltHostKeyMismatch", "A run stopped starting devices after a changed host key (ssh.halt-run-on-host-key-mismatch)."},
}

// Lookup is the status of a code, and whether it is defined.
func Lookup(code int) (Status, bool) {
	for _, s := range Statuses {
		if s.Code == code {
			return s, true
		}
	}
	return Status{}, false
}

func ExitName(code int) string {
	if s, ok := Lookup(code); ok {
		return s.Name
	}
	return "ExitUnknown"
}
