package configload

// Removed configuration keys.
//
// A key that karvi once registered and has since removed is refused at load
// with a code, from every layer that can set it (a file, the environment,
// --set), so a site's configuration does not carry a setting that nothing
// reads. The table below is the one place such keys are named in the code;
// each row carries the key's path, the environment variable the registry
// gave it, the release that removed it, and the hint the refusal prints.
// Restoration breadcrumbs, with the registry rows as they were, are kept
// in the operator's private archive.
//
// Codes: the two compatibility-class keys keep the code they were released
// with (config_ssh_legacy_hosts_removed); every
// later removal is refused as config_key_removed, and the message names the
// key and the release, so one code serves every removal from here on.

// removedKey is one row of the removed-key table.
type removedKey struct {
	path        string // the registry path, as a file or --set names it
	environment string // the KARVI__ variable the registry mapped to the path
	removedIn   string // the release that first refuses the key
	code        string // the code the refusal carries
	hint        string // what replaces the key, or why nothing does
}

// refuse builds the load error for a removed key found at src. key is the
// name as the layer gave it (the path, or the path recovered from an
// environment variable), so the message and Error.Key agree.
func (r removedKey) refuse(key string, src SourceRef) error {
	return newError(r.code, "removed in "+r.removedIn+"; "+r.hint, key, src, nil)
}

const (
	legacyHostsHint = "name the devices in an [[ssh-algorithms-map]] rule whose [ssh-algorithms-profile.NAME] appends the algorithms they need"
	unreadHint      = "the key was read by nothing and has no replacement; remove it from the configuration"
)

// removedKeys lists every key karvi has removed, in the order they left.
var removedKeys = []removedKey{
	// v0.12.0: the compatibility classes gave
	// way to the algorithm profiles and map.
	{path: "ssh.legacy-hosts", environment: "KARVI__SSH__LEGACY_HOSTS", removedIn: "v0.12.0", code: "config_ssh_legacy_hosts_removed", hint: legacyHostsHint},
	{path: "ssh.ancient-hosts", environment: "KARVI__SSH__ANCIENT_HOSTS", removedIn: "v0.12.0", code: "config_ssh_legacy_hosts_removed", hint: legacyHostsHint},
	// The configuration review of v0.14.0: twenty keys
	// read by no code, and the
	// per-platform session-cap pattern, which the platform definition and the
	// inventory row's cap had replaced. The release is v0.14.0 (the string
	// was "the release after v0.13.0" until the number was assigned).
	{path: "sessions.idle-timeout", environment: "KARVI__SESSIONS__IDLE_TIMEOUT", removedIn: reviewRelease, code: "config_key_removed", hint: unreadHint},
	{path: "ssh.control-persist", environment: "KARVI__SSH__CONTROL_PERSIST", removedIn: reviewRelease, code: "config_key_removed", hint: unreadHint},
	{path: "sessions.max-idle-total", environment: "KARVI__SESSIONS__MAX_IDLE_TOTAL", removedIn: reviewRelease, code: "config_key_removed", hint: unreadHint},
	{path: "sessions.max-idle-per-device", environment: "KARVI__SESSIONS__MAX_IDLE_PER_DEVICE", removedIn: reviewRelease, code: "config_key_removed", hint: unreadHint},
	{path: "sessions.capacity-wait-timeout", environment: "KARVI__SESSIONS__CAPACITY_WAIT_TIMEOUT", removedIn: reviewRelease, code: "config_key_removed", hint: unreadHint},
	{path: "sessions.adopt-on-restart", environment: "KARVI__SESSIONS__ADOPT_ON_RESTART", removedIn: reviewRelease, code: "config_key_removed", hint: unreadHint},
	{path: "sessions.lease-heartbeat", environment: "KARVI__SESSIONS__LEASE_HEARTBEAT", removedIn: reviewRelease, code: "config_key_removed", hint: unreadHint},
	{path: "daemon.socket-exit-timeout", environment: "KARVI__DAEMON__SOCKET_EXIT_TIMEOUT", removedIn: reviewRelease, code: "config_key_removed", hint: unreadHint},
	{path: "daemon.status-detail-limit", environment: "KARVI__DAEMON__STATUS_DETAIL_LIMIT", removedIn: reviewRelease, code: "config_key_removed", hint: unreadHint},
	{path: "daemon.reconcile-timeout", environment: "KARVI__DAEMON__RECONCILE_TIMEOUT", removedIn: reviewRelease, code: "config_key_removed", hint: unreadHint},
	{path: "ssh.allowed-cli-options", environment: "KARVI__SSH__ALLOWED_CLI_OPTIONS", removedIn: reviewRelease, code: "config_key_removed", hint: unreadHint},
	{path: "execution.max-commands-per-device", environment: "KARVI__EXECUTION__MAX_COMMANDS_PER_DEVICE", removedIn: reviewRelease, code: "config_key_removed", hint: unreadHint},
	{path: "dispatch.shared-admission", environment: "KARVI__DISPATCH__SHARED_ADMISSION", removedIn: reviewRelease, code: "config_key_removed", hint: unreadHint},
	{path: "dispatch.wave-cpu-sample-interval", environment: "KARVI__DISPATCH__WAVE_CPU_SAMPLE_INTERVAL", removedIn: reviewRelease, code: "config_key_removed", hint: unreadHint},
	{path: "audit.buffer-records", environment: "KARVI__AUDIT__BUFFER_RECORDS", removedIn: reviewRelease, code: "config_key_removed", hint: unreadHint},
	{path: "logging.journald", environment: "KARVI__LOGGING__JOURNALD", removedIn: reviewRelease, code: "config_key_removed", hint: unreadHint},
	{path: "watch.change-highlight-refreshes", environment: "KARVI__WATCH__CHANGE_HIGHLIGHT_REFRESHES", removedIn: reviewRelease, code: "config_key_removed", hint: unreadHint},
	{path: "metrics.enabled", environment: "KARVI__METRICS__ENABLED", removedIn: reviewRelease, code: "config_key_removed", hint: unreadHint},
	{path: "retention.days", environment: "KARVI__RETENTION__DAYS", removedIn: reviewRelease, code: "config_key_removed", hint: "karvi-prune takes the retention age from its --days flag"},
	{path: "retention.low-space-free-percent", environment: "KARVI__RETENTION__LOW_SPACE_FREE_PERCENT", removedIn: reviewRelease, code: "config_key_removed", hint: unreadHint},
	// Section (b) of the same review: read by one rule that compared it to
	// output.max-command-bytes; nothing spooled then.
	{path: "output.memory-spool-threshold-bytes", environment: "KARVI__OUTPUT__MEMORY_SPOOL_THRESHOLD_BYTES", removedIn: reviewRelease, code: "config_key_removed", hint: "nothing spools; output.max-command-bytes bounds one command's output in memory"},
	// v0.21.0: the spool's own free-space
	// switch became `freecheck`, the one switch of every volume an activity
	// writes; `always` is the floor output.min-free-bytes-after-job, 2 GiB.
	{path: "spoolfreecheck", environment: "KARVI__SPOOLFREECHECK", removedIn: "v0.21.0", code: "config_key_removed", hint: "freecheck (auto, always, never) governs every volume an activity writes; always asks the floor output.min-free-bytes-after-job"},
	// v0.23.0:
	// the ping line became a template, display.ping.header, whose empty
	// value is the switch the boolean was.
	{path: "display.ping", environment: "KARVI__DISPLAY__PING", removedIn: "v0.23.0", code: "config_key_removed", hint: "display.ping.header is the ping line's template; set it empty to print no line"},
	// v0.26.0: the job folder's file of the records that did not succeed
	// became errors.jsonl, and its key with it.
	{path: "output.files.failures-jsonl", environment: "KARVI__OUTPUT__FILES__FAILURES_JSONL", removedIn: "v0.26.0", code: "config_key_removed", hint: "the file is errors.jsonl now; its switch is output.files.errors-jsonl"},
	// v0.27.0: whether keys are offered is the credential's, the operator's
	// own keys through ssh.identities and the platform's fallback.
	{path: "ssh.pubkey-authentication", environment: "KARVI__SSH__PUBKEY_AUTHENTICATION", removedIn: "v0.27.0", code: "config_key_removed", hint: "a credential with keys offers them; the operator's own keys are ssh.identities, reached where a platform's fallback lists keys"},
	{path: "security.fips-legacy-system-ssh-exception", environment: "KARVI__SECURITY__FIPS_LEGACY_SYSTEM_SSH_EXCEPTION", removedIn: reviewRelease, code: "config_key_removed", hint: "the compatibility classes it excepted left with ssh.legacy-hosts"},
}

// reviewRelease names the release that carries the configuration review:
// v0.14.0, set when that number was assigned.
const reviewRelease = "v0.14.0"

// removedKeyByPath reports the removed key a file or --set names. The
// per-platform session-cap pattern is matched by its prefix, since the
// registry knew it as a dynamic table and not as fixed paths.
func removedKeyByPath(key string) (removedKey, bool) {
	for _, r := range removedKeys {
		if r.path == key {
			return r, true
		}
	}
	if len(key) > len(platformCapsPrefix) && key[:len(platformCapsPrefix)] == platformCapsPrefix {
		return removedKey{path: key, removedIn: reviewRelease, code: "config_key_removed", hint: "a device's session cap is its platform definition's, or the inventory row's"}, true
	}
	return removedKey{}, false
}

// platformCapsPrefix is the removed sessions.platform-caps.<platform> table.
const platformCapsPrefix = "sessions.platform-caps."

// removedKeyByEnvironment reports the removed key an environment variable
// names, so the environment layer refuses it before the registry index, which
// no longer knows the name, would treat it as unknown.
func removedKeyByEnvironment(name string) (removedKey, bool) {
	for _, r := range removedKeys {
		if r.environment == name {
			return r, true
		}
	}
	return removedKey{}, false
}
