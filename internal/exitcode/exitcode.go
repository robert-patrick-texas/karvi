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

var exitNames = map[int]string{
	ExitSuccess: "ExitSuccess", ExitGenericError: "ExitGenericError",
	ExitConfigValidationError: "ExitConfigValidationError", ExitConfigLockViolation: "ExitConfigLockViolation",
	ExitUsageError: "ExitUsageError", ExitInventoryError: "ExitInventoryError",
	ExitCredentialResolutionError: "ExitCredentialResolutionError", ExitNameResolutionError: "ExitNameResolutionError",
	ExitDependencyError: "ExitDependencyError", ExitPermissionError: "ExitPermissionError",
	ExitPartialFailure: "ExitPartialFailure", ExitHaltErrorCount: "ExitHaltErrorCount",
	ExitHaltErrorPercent: "ExitHaltErrorPercent", ExitWaveGateErrorCount: "ExitWaveGateErrorCount",
	ExitWaveGateErrorPercent: "ExitWaveGateErrorPercent", ExitShutdownIncomplete: "ExitShutdownIncomplete",
	ExitDeviceFailure: "ExitDeviceFailure", ExitAuthenticationFailure: "ExitAuthenticationFailure",
	ExitHostKeyFailure: "ExitHostKeyFailure", ExitConnectionFailure: "ExitConnectionFailure",
	ExitOutputFailure: "ExitOutputFailure", ExitJobRejected: "ExitJobRejected", ExitCancelled: "ExitCancelled",
	ExitHaltHostKeyMismatch: "ExitHaltHostKeyMismatch",
}

func ExitName(code int) string {
	if name, ok := exitNames[code]; ok {
		return name
	}
	return "ExitUnknown"
}
