package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/transport/native"
)

func now() time.Time { return time.Now() }

// Help texts list every option of the parser table for their command; the
// drift test in help_test.go keeps them and the table in step.

// platformOptionHelp and selectPlatformHelp are the platform lines shared by
// login, command, and run: --platform means one thing in the three modes, and
// so does --select-platform, so each text exists once.
const platformOptionHelp = `  --platform NAME                The platform the devices of this invocation run
                                 as, whatever their inventory rows say: a
                                 built-in or a configured [platform.NAME]
                                 table; a name, never a pattern. login and
                                 command apply it to their one device, run to
                                 every device of the set (a direct target in no
                                 inventory runs as generic without it).
                                 Without it an inventory row's platform
                                 applies, then the source default, then
                                 platform-resolution.default (else generic); an
                                 unknown row platform is refused unless
                                 platform-resolution.on-unknown = "warn"
  --pi, --pn, --pr, --pj, --pa, --pg
                                 Short for --platform cisco_iosxe, cisco_nxos,
                                 cisco_iosxr, juniper_junos, arista_eos, generic
`

// commandBoundsHelp is the per-command bounds, shared by command and run:
// --timeout and --maxbytes are declarations in both.
const commandBoundsHelp = `A command's own bounds (each attaches to the --cmd it follows, or to the one
freeform command; at most one of each per command):
  --timeout DURATION             The command's timeout in place of
                                 execution.command-timeout, and over telnet of
                                 telnet.read-timeout (1s..12h); not on a blind
                                 command, nor above a set
                                 execution.device-timeout
  --maxbytes BYTES               The command's output limit in place of
                                 output.max-command-bytes, in whole bytes
                                 (1024..1073741824); not above
                                 output.max-job-bytes
`

// hostKeyPolicyHelp is --ssh-host-key-policy's entry, shared by login,
// command, and run: ssh.host-key-policy is one policy for the three.
const hostKeyPolicyHelp = `  --ssh-host-key-policy accept-new|secure|insecure
                                 The host-key policy (ssh.host-key-policy):
                                 accept-new accepts and persists a new key and
                                 rejects a changed one (the default); secure
                                 requires a matching pre-enrolled key before
                                 access; insecure accepts unknown or changed
                                 keys with prominent warnings
`

const streamHelp = `Usage:
  karvi stream
  karvi -

Reads a run line by line from standard input and executes it as run would.
A line is skipped when blank or when its first character is ! or #. A line
beginning with a dash is one run option, one dash or two alike: the word,
then its value as the rest of the line after a space or =, so --target
router1, -target=router1, --tl router1 router2, --dispatch parallel, --dp.
A value wholly wrapped in one pair of quotes loses them, as a shell would
remove them: --cmd "show clock". A line leaving text no option takes is
dropped. Any other line is one command, sent as written, quotes and all; \r
at its end is read as --cmd reads it, and a line of \r alone sends a blank
line. The targets and options stay from one job to the next, and so do the
commands given in option form (--cmd, -c, --command, --cf); a command given
as a bare line is the job's alone. --expect, --blind, --blind-return,
--timeout, and --maxbytes lines attach to the command before them. --cf,
--tf, and --tfr may not name -: standard input is the stream.

Directives, each a whole line, one dash or two:
  --go, --sendit                 Execute the draft as run; the bare-line
                                 commands clear and everything else stays;
                                 with no command to send, a notice
  --clear                        Empty the bare-line commands; the targets,
                                 options, and option-form commands stay
  --purge-commands               Empty every command (from --purge-c)
  --purge-targets                Remove every target input (from --purge-t):
                                 --target, --tl, --tf, --tfr, --site,
                                 --device-group, --all, --select-platform
  --reset                        Empty the draft
  --end, --quit, --exit          Leave without executing; so do EOF (Ctrl-D)
                                 and Ctrl-C

Typed at a terminal, a line is edited with Ctrl-A, Ctrl-E, Ctrl-K, Ctrl-U,
Ctrl-W, and the arrows, and the up arrow recalls earlier lines. A line the
parser refuses is reported with its number and dropped. The exit is the
last executed job's, 0 when none ran; a read failure or a line over 1 MiB
ends the stream with stream_input_read_failed. Global options go before
the word: karvi --quiet --config FILE stream.

Options:
  --help, -h                     Show this help
`

const selectPlatformHelp = `  A --select-platform value is a selector over the known platforms (the
  built-ins and the configured [platform.NAME] tables) and matches the
  platform set on a device's row or source default, before any --platform
  acts; a value reaching no known platform is refused. A device whose platform
  is not set, or falls back under platform-resolution.on-unknown = "warn", is
  matched by no --select-platform value (it stays reachable by name, site,
  group, and --all).
`

const topHelp = `karvi - run the fleet, gather the output

Usage:
  karvi [global-options] <command> [options] [payload]

Commands:
  login                          Open one interactive SSH session
  command, cmd                   Run commands on one device
  run                            Run commands against an explicit scope
  crun                           Collect commands from a scope into one file per device
  stream, -                      Read a run line by line from standard input
  daemon start|stop|restart|status|serve
                                 Manage the private per-user daemon
  job follow|cancel              Follow or cancel one accepted job by its ID
  config generate|validate|show|colors
                                 Configuration tools
  setup shared|tab               Site preparations as root: the shared trees, tab completion
  watch                          Render safe activity scoreboards
  version                        Print build, schema, and SSH transport versions

Global options (before the command word):
  --config PATH, --cfg PATH      Add a config root (repeatable)
  --set KEY=VALUE                Highest-precedence override (repeatable, lock-aware)
  --quiet                        Suppress routine narration
  --debug                        Safe provenance diagnostics
  --debug-show-secrets           Console-only bounded hints; requires --debug
  --timezone ZONE                Override timezone
  --ansi auto|strip|preserve     Device stdout ANSI behavior
  --ipv4, --4                    Prefer IPv4 when both families are available
  --ipv6, --6                    Prefer IPv6 when both families are available
  --version                      Print version without creating state
  --help, --h                    Print this help without creating state

Every command word, subcommand word, and option may be abbreviated to any
prefix that names exactly one word valid at that position; a full name or
alias always matches exactly, so --t is --target and --h is --help. Options
take one or two leading dashes (-echo and --echo are the same), and a value
may be attached with =. In command and run, options are recognized only until
device command text begins; after that every argument, including -h and --,
is sent to the device. Use -- to begin device text with a dash.

An option that stands for a configuration key, such as --order for
dispatch.order or --workers for dispatch.parallel-workers, sets that key for
the invocation, above the files and the environment and below --set: a key
the site has locked refuses the option, and the key's range applies.
`

const versionHelp = `Usage:
  karvi version [--format text|json] [--help]

Reports the exact SSH transports available to this executable. Optional native
adapters appear only when they were included in the build.
`

const commandHelp = `Usage:
  karvi command [options] DEVICE <device command words...>
  karvi command [options] --target DEVICE --cmd TEXT [--cmd TEXT...]
  karvi cmd [options] DEVICE --cf PATH|-

Target inputs (command-line order is kept; command connects to the first
device of the assembled set):
  DEVICE                         Positional device: the first positional word,
                                 unless a target input precedes it
  --target DEVICE, --host, --t   Repeatable inventory or direct target, or glob
  --tl LIST                      Targets in one argument, separated by commas
                                 or whitespace (quote a whitespace list); each
                                 is a --target at this position
  --tf FILE|PATH|-               Target file, or the files directly in a
                                 folder; - reads stdin
  --tfr PATH                     Target file, or a folder and its subfolders
                                 (targets.recursion-max-depth, default 3)
  --site GLOB, --device-group GLOB, --select-platform GLOB, --all
                                 Inventory selectors, matches at this position
  --exclude GLOB                 Remove name matches after duplicate removal
  A GLOB matches the whole value without regard to case: * ? [a-c] [!a-c],
  and \ escapes the next character; ^ and $ are refused. A selector value
  beginning with ! removes the devices it matches and selects none; in a
  target file, lines beginning with # or ! are comments.
` + selectPlatformHelp + `  --order default|sorted|shuffle|random
                                 Dispatch order for this invocation (sets the
                                 lock-aware dispatch.order; the shuffle key
                                 comes from dispatch.shuffle-key)
  Folder reads skip names beginning with . readme or disabled, and editor
  backups (~ .bak .swp .orig .rej #name#); an empty source follows
  targets.empty-source (error or warn).

Device commands (one form per invocation):
  Positional words after the device are joined with one space into one command.
  --cmd TEXT, --command, --c     Separate command; repeatable
  --cf PATH|-                    One command per line, sent as written; at most
                                 once; - reads standard input
  A command ending in \r sequences is sent without them, followed by that
  many carriage returns before the prompt is read (a [confirm] answer), and is
  blind; nothing else in the text is interpreted.

Interactive prompts (each attaches to the --cmd it follows, or to the one
freeform command; a command takes blind returns or --expect, not both):
  --expect PATTERN=RESPONSE      When a line matching PATTERN (RE2) appears,
                                 type RESPONSE and a return; consumed once, in
                                 declared order; empty RESPONSE is a bare
                                 return; at most 20 per command. A response is
                                 device text, recorded in clear: not for a
                                 password
  --blind                        The prompt may not return: await the blind
                                 wait in place of the command timeout, and a
                                 prompt never seen is still a success with the
                                 notice prompt_not_observed_after_blind_send
  --blind-return N               Send N carriage returns with the command
                                 (0..20), before any read, and treat it as
                                 --blind
  --blind-wait DURATION          Prompt-return wait for a blind command, in
                                 place of the command timeout (0 does not wait;
                                 sets the lock-aware execution.blind-wait)
  --literal                      Send every command as written; no \r
                                 interpretation; a RESPONSE is always as written

` + commandBoundsHelp + `
Options:
` + platformOptionHelp + `  --management-address IP, --address, --a
                                 Literal management IP; does not select the target
  --port N                       Device service port
  --transport SELECTOR           default, system, native, preferred, telnet,
                                 or a configured [ssh.transports] slot
` + hostKeyPolicyHelp + `  --ssh-known-hosts-file PATH|auto
                                 Unified karvi trust store
  --format text|jsonl|json       Text (default), compact JSON Lines, or an
                                 indented JSON array for human inspection
  --echo                         Show prompt plus sent command in text output,
                                 including device-reported command errors
  --border                       Replace the configured border with dashes
  --noborder                     Disable configured and dynamic borders
  --continue-device-on-error     Do not stop later commands after a device error
  --nof                          No output files: run and display, create no job
                                 folder (output.persist-command=false)
  --of[=PATH]                    Output files on (output.persist-command=true),
                                 to the configured folder or to PATH
                                 (output.root); PATH only with =, as the next
                                 word is the device (a path there is refused).
                                 The opposite of --nof
  --cd=PATH                      Also write the device's output to PATH/NAME,
                                 one file named by the device, a "! COMMAND"
                                 marker before each command's output and
                                 nothing else, replaced only when the device
                                 succeeds (crun.directory; ~ expanded, relative
                                 to the working directory; auto the collection
                                 tree)
  --fs=SUFFIX                    Append SUFFIX to the collection file's name
                                 (--fs=.cfg writes NAME.cfg); without --cd it
                                 is --cd=. as well
  --address-authority client|daemon
                                 Who selects the address: the client at planning
                                 (default) or the daemon at prepare
  --quiet                        Suppress configured header, footer, and border
  --debug                        Safe transport, resolution, and timing diagnostics
  --ping                         Send two ICMP probes to each target before its
                                 transport; skip a target that answers neither
  --noping                       Send no ICMP probes (the default unless
                                 network.ping-targets is true)
  --ipv4, --4                    Prefer IPv4 when both families are available
  --ipv6, --6                    Prefer IPv6 when both families are available
  --help, --h                    Show this help

Options are recognized before the device and between the device and the first
device command word. Once device text begins, karvi recognizes no options,
abbreviations, --, or help flags: "command r1 show clock --echo" sends
"show clock --echo". All commands use one authenticated SSH session and
execute sequentially, on its shell or, for an exec platform (linux), on an exec
channel each; no connection outlives the device's session.
`

// crunHelpText is run's help under the crun word: the usage
// lines say what a collection is, --cd names its directory, and --fs
// never implies one.
func crunHelpText() string {
	text := strings.ReplaceAll(runHelpText(), "karvi run ", "karvi crun ")
	usage := `Usage:
  karvi crun [target inputs] [options]
  karvi crun [target inputs] [options] --cmd TEXT [--cmd TEXT...]
  karvi crun [target inputs] [options] --cf PATH|-
  karvi crun [target inputs] [options] <device command words...>

The collection run: one file per device, named by the device, in one flat
directory (crun.directory: the shared /opt/karvi/shared/crun when the site
made it with setup shared, else <basedir>/crun, unless configured;
--cd=PATH for one run), replaced only when the device's collection
succeeded, holding each command's output under a "! COMMAND" marker line
and nothing else.
With no command on the line each device is sent its platform's
crun-commands list (a platform without one refuses the run at planning);
a rejected statement does not stop the device's later commands. The job
folder is written as for any run, without output.NAME.txt.
A platform's crun-filters (regular expressions) drop the output lines that
match from the collection file alone: the built-in lists drop the byte
count, the clock period, the uptime, and the time of the show.
crun.after names an executable the client runs once the collection has
ended (not with --detach): in the collection directory, the replaced
files' names on stdin, the job in KARVI_* variables (docs/COLLECTION.md).
A recurring collection is the site's systemd timer or cron over this word
(/usr/share/karvi/systemd/user/karvi-crun.timer,
/usr/share/karvi/cron/karvi-crun).
`
	i := strings.Index(text, "Target inputs")
	text = usage + "\n" + text[i:]
	j := strings.Index(text, "  --cd=PATH ")
	k := strings.Index(text, "  --follow ")
	return text[:j] + `  --cd=PATH                      The collection directory for this run
                                 (crun.directory); PATH only with =
  --fs=SUFFIX                    Append SUFFIX to each file's name in the
                                 collection directory (--fs=.cfg writes
                                 NAME.cfg); the directory keeps its name
` + text[k:]
}

func runHelpText() string {
	status := "unavailable in this executable"
	if native.Available("scrapligo-v1") {
		status = "available (scrapligo 1.4.2 compiled in)"
	}
	return fmt.Sprintf(`Usage:
  karvi run [target inputs] [options] <device command words...>
  karvi run [target inputs] [options] --cmd TEXT [--cmd TEXT...]
  karvi run [target inputs] [options] --cf PATH|-

Target inputs (at least one; command-line order is kept, the first occurrence
of a name keeps its position, names are lowercase):
  --target NAME, --host, --t     Repeatable inventory or direct target, or glob
  --tl LIST                      Targets in one argument, separated by commas
                                 or whitespace (quote a whitespace list); each
                                 is a --target at this position
  --tf FILE|PATH|-               Target file, or the files directly in a
                                 folder; - reads stdin
  --tfr PATH                     Target file, or a folder and its subfolders
                                 (targets.recursion-max-depth, default 3)
  --site GLOB, --device-group GLOB, --select-platform GLOB, --all
                                 Inventory selectors, matches at this position
  --exclude GLOB                 Remove name matches after duplicate removal
  A GLOB matches the whole value without regard to case: * ? [a-c] [!a-c],
  and \ escapes the next character; ^ and $ are refused. A selector value
  beginning with ! removes the devices it matches and selects none; in a
  target file, lines beginning with # or ! are comments.
`+selectPlatformHelp+`  An unknown row platform in the set refuses the run under the default, fail.
  --order default|sorted|shuffle|random
                                 Dispatch order for this invocation (sets the
                                 lock-aware dispatch.order; the shuffle key
                                 comes from dispatch.shuffle-key)
  Folder reads skip names beginning with . readme or disabled, and editor
  backups (~ .bak .swp .orig .rej #name#); an empty source follows
  targets.empty-source (error or warn).

Device commands (one form per invocation):
  Positional words are joined with one space into one command.
  --cmd TEXT, --command, --c     Separate command; repeatable
  --cf PATH|-                    One command per line, sent as written; at most once
  A command ending in \r sequences is sent without them, followed by that
  many carriage returns before the prompt is read (a [confirm] answer), and is
  blind; nothing else in the text is interpreted.

Interactive prompts (each attaches to the --cmd it follows, or to the one
freeform command; a command takes blind returns or --expect, not both):
  --expect PATTERN=RESPONSE      When a line matching PATTERN (RE2) appears,
                                 type RESPONSE and a return; consumed once, in
                                 declared order; empty RESPONSE is a bare
                                 return; at most 20 per command. A response is
                                 device text, recorded in clear: not for a
                                 password
  --blind                        The prompt may not return: await the blind
                                 wait in place of the command timeout, and a
                                 prompt never seen is still a success with the
                                 notice prompt_not_observed_after_blind_send
  --blind-return N               Send N carriage returns with the command
                                 (0..20), before any read, and treat it as
                                 --blind
  --blind-wait DURATION          Prompt-return wait for a blind command, in
                                 place of the command timeout (0 does not wait;
                                 sets the lock-aware execution.blind-wait)
  --literal                      Send every command as written; no \r
                                 interpretation; a RESPONSE is always as written

`+commandBoundsHelp+`
Platform:
`+platformOptionHelp+`
Dispatch:
  --dispatch serial|parallel|wave
                                 How the devices run (dispatch.default, serial
                                 by default): one at a time; a fixed pool of
                                 workers over one queue; or waves whose width
                                 follows the host's CPU
  --dp, --dw, --ds               Short for --dispatch parallel, wave, serial
  --workers N                    The parallel pool's size
                                 (dispatch.parallel-workers; 0 the host's
                                 logical CPUs)
  --start-width N                A wave job's first width, and its floor
                                 (dispatch.wave-start-width; 0 from the host's
                                 CPUs)
  --max-width N                  A wave job's widest wave
                                 (dispatch.wave-max-width; 0 from the host's
                                 CPUs)
  --halt-on-error-count N        Start no further device once N have failed;
                                 those in flight finish
                                 (dispatch.halt-on-error-count; 0 off)
  --halt-on-error-percent N      Start no further device once the failed are
                                 N percent or more of the devices ended so
                                 far, checked as each ends, so a first
                                 device's failure halts at any N
                                 (dispatch.halt-on-error-percent; 0 off)
  --wave-gate-error-count N      Between waves, stop a wave job whose last
                                 wave had N failed devices
                                 (dispatch.wave-gate-error-count; 0 off)
  --wave-gate-error-percent N    The same at N percent of that wave's devices
                                 (dispatch.wave-gate-error-percent; 0 off)
  --wave-delay DURATION          A pause between waves
                                 (dispatch.wave-gate-timed-delay; 0s none)
  --continue-device-on-error     Do not stop later commands after a device error
  --no-daemon                    Execute in the client process
  --nof                          No output files: run and display, create no job
                                 folder (output.persist-command=false); not with
                                 --detach or --exercise
  --of[=PATH]                    Output files on (output.persist-command=true),
                                 to the configured folder or to PATH
                                 (output.root), on every path; PATH only with =
                                 (a path as the next word is refused)
  --cd=PATH                      Also write each device's output to PATH/NAME,
                                 one file per device named by the device, a
                                 "! COMMAND" marker before each command's
                                 output and nothing else, replaced only when
                                 the device succeeds (crun.directory; ~
                                 expanded, relative to the working directory;
                                 auto the collection tree)
  --fs=SUFFIX                    Append SUFFIX to each collection file's name
                                 (--fs=.cfg writes NAME.cfg); without --cd it
                                 is --cd=. as well
  --follow                       Render durable records after completion (default)

Options:
  --transport SELECTOR           default, system, native, preferred, telnet,
                                 or a configured [ssh.transports] slot
  --management-address IP, --address, --a
                                 Override the address for exactly one selected device
  --address-authority TARGET=client|daemon
                                 Who selects TARGET's address: the client at
                                 planning (default) or the daemon at prepare;
                                 repeatable, TARGET names a device of the set
`+hostKeyPolicyHelp+`  --ssh-known-hosts-file PATH|auto
                                 Unified karvi trust store
  --format text|jsonl|json       Text (default), compact JSON Lines, or an
                                 indented JSON array for human inspection
  --echo                         Show prompt plus sent command in text output
  --border                       Replace the configured run border with dashes
  --noborder                     Disable configured and dynamic run borders
  --ipv4, --4                    Prefer IPv4 when both families are available
  --ipv6, --6                    Prefer IPv6 when both families are available
  --detach                       Submit and print the job ID and artifact directory;
                                 do not follow; exit 0 means accepted
  --follow, --follow=false       Render records as they become durable (default);
                                 false waits for the result without rendering
  --dry-run                      Plan on the client and probe the daemon; submit
                                 no job, send no target data, contact no device;
                                 print the inspection report in the --format
  --exercise                     Submit the plan and package for daemon-side
                                 validation of everything but the device; no
                                 ICMP, session, askpass, or command; print the
                                 exercise report in the --format
  --ping                         Send two ICMP probes to each target before its
                                 transport; skip a target that answers neither
  --noping                       Send no ICMP probes (the default unless
                                 network.ping-targets is true)
  --debug                        Safe transport, resolution, and timing diagnostics
  --help, --h                    Show this help

Options are recognized until the first device command word; after it every
argument is device text. Run uses the per-user daemon unless --no-daemon is
supplied.

The preferred native slot maps to scrapligo-v1 by default; status: %s.
Builds without that adapter reject native transport rather than silently using OpenSSH.

Set ssh.halt-run-on-host-key-mismatch=true to stop new scheduling after the
first changed host key while allowing already in-flight devices to finish.
`, status)
}

const loginHelp = `Usage:
  karvi login [options] DEVICE
  karvi login [options] --target DEVICE
  karvi login --host DEVICE [options]

Open one interactive session with the first device of the assembled target
set. Options may appear before or after the device; -- makes a later argument
beginning with a dash a device.

Target inputs (command-line order is kept):
  DEVICE                         Positional device, repeatable
  --target DEVICE, --host, --t   Repeatable inventory or direct target, or glob
  --tf FILE|PATH|-               Target file, or the files directly in a
                                 folder; - reads stdin
  --tfr PATH                     Target file, or a folder and its subfolders
                                 (targets.recursion-max-depth, default 3)
  --site GLOB, --device-group GLOB, --select-platform GLOB, --all
                                 Inventory selectors, matches at this position
  --exclude GLOB                 Remove name matches after duplicate removal
  A GLOB matches the whole value without regard to case: * ? [a-c] [!a-c],
  and \ escapes the next character; ^ and $ are refused. A selector value
  beginning with ! removes the devices it matches and selects none; in a
  target file, lines beginning with # or ! are comments.
` + selectPlatformHelp + `  --order default|sorted|shuffle|random
                                 Dispatch order for this invocation (sets the
                                 lock-aware dispatch.order; the shuffle key
                                 comes from dispatch.shuffle-key)
  Folder reads skip names beginning with . readme or disabled, and editor
  backups (~ .bak .swp .orig .rej #name#); an empty source follows
  targets.empty-source (error or warn).

Options:
` + platformOptionHelp + `  --management-address IP, --address, --a
                                 Literal management IP; does not select the target
  --port N                       Destination port
  --transport SELECTOR           Default resolves to system; the interactive
                                 implementation requires a system-compatible slot
` + hostKeyPolicyHelp + `  --ssh-known-hosts-file PATH|auto
                                 Unified karvi trust store
  --record[=PATH], --rec[=PATH]  Record the session; PATH only with = (a path
                                 as the next word is refused)
  --address-authority client|daemon
                                 Who selects the address: the client (default)
                                 or the daemon
  --ipv4, --4                    Prefer IPv4 when both families are available
  --ipv6, --6                    Prefer IPv6 when both families are available
  --ping                         Send two ICMP probes to each target before its
                                 transport; skip a target that answers neither
  --noping                       Send no ICMP probes (the default unless
                                 network.ping-targets is true)
  --quiet                        Suppress configured login header and footer
  --debug                        Safe transport, resolution, and timing diagnostics
  --help, --h                    Show this help
  Specified, not yet available:  --ssh-option KEY=VALUE
`

const configHelp = `Usage:
  karvi config generate [--minimal|--full] [--force] [file]
  karvi config validate [--format text|json] [file|dir]
  karvi config show [--format toml|json] [--explain] [--show-sources] [key...]
  karvi config colors

Options may appear before or after the positional argument; -- makes a later
argument beginning with a dash positional. --help shows this text. config
colors prints every display.colors role in its colour, the configured theme
first and then the other: the key, the colour, what set it, and the escape.
Specified, not yet available: config validate --internal.

config show prints the effective configuration. With --explain, or with keys
named, it prints each key in turn, every key when none is named: its value,
source, default, environment variable, reload class, and lock; a key not in
the configuration is error: not found. A key whose value is a place karvi
writes or reads has, after its default, a resolved: line, the path the next
activity in process would use, found by the activity's own rule without
creating anything, or error: CODE: message where the activity would refuse;
a candidate present on the host and passed by follows as a passed: line with
its reason. The lines come from the configuration this invocation loads: a
daemon already running keeps its tempdir, spooldir, ssh.control-path-root,
scoreboards, and sessions.shared-capacity-root until it is restarted.
`
const daemonHelp = `Usage:
  karvi daemon start [--foreground]
  karvi daemon stop [--grace | --after=DURATION | --force]
  karvi daemon restart [--grace | --after=DURATION | --force]
  karvi daemon status [--format text|json]
  karvi daemon serve

Without an option, stop and restart refuse while the daemon reports active
jobs. --grace drains the daemon, so it rejects new jobs, and waits for active
jobs to finish. --after=DURATION drains and waits until jobs finish or DURATION
elapses, whichever comes first, then forces the stop. --force stops at once;
unfinished work still receives terminal records. Interrupting a wait leaves the
daemon draining. --help shows this text. Specified, not yet available:
daemon start --start-timeout DURATION.

A daemon is compatible when its version and its IPC schema both equal the
client's; either difference is incompatible. An upgraded client never submits
a job to, kills, or silently replaces an incompatible running daemon. Status
and explicit stop/restart use the stable same-UID lifecycle envelope for
released IPC schemas, allowing an operator to recover after an upgrade without
reinstalling the older karvi executable.
`
const jobHelp = `Usage:
  karvi job follow JOB-ID [--format text|jsonl|json] [--echo] [--border | --noborder]
  karvi job cancel JOB-ID [--reason TEXT] [--follow] [--format text|json]

job operates on one accepted job, named by the job ID of its receipt (the
ID a detached run prints and karvi watch shows). No job verb launches a daemon.

follow renders the job's records from its beginning as the foreground run
would have, live until the job ends or at once for a job that has ended,
ends as the run's display ends (the footer, and a collection's line), and
exits with the job's own exit.
--format, --echo, --border, and --noborder mean what they mean for run.
Ctrl-C stops only the follow; the job continues.

cancel stops the job and leaves every other job running; the daemon answers
at once and the job's unfinished work is recorded cancelled (exit 113).
--reason TEXT is recorded with the request. --follow waits for the job's end
and exits with the job's own exit. A job that already ended is reported with
its outcome; an unknown job ID is job_unknown. --help shows this text.
`
const setupHelp = `Usage:
  sudo karvi setup shared [--group NAME] [--mode 2770|2775]
  sudo karvi setup tab

setup shared prepares the site once, as root. Under /opt/karvi (0755) it
makes shared, and under that jobs, crun, and transcripts, the four in the
operators' group with group write and search and the setgid bit (2770, or
2775 with --mode), so every member writes into them and what is made below
stays in the group; beside shared, users (1770), where each operator's
private root is made. On /dev/shm it makes the scratch root /dev/shm/karvi
and its scoreboards (3770: the sticky bit too, so no member removes
another's folder or file) and capacity with its devices (2770, the
host-wide session ledger every member writes), and it writes
/etc/tmpfiles.d/karvi.conf, so systemd-tmpfiles makes the scratch root
again at every boot. --group NAME is the group; without it the primary
group of the operator who ran sudo is taken. Each directory is reported as
created, exists, or repaired (another group or mode set right, what it had
named); a path that is not a real directory is reported and left
(setup_directory_mismatch). The rule is reported as created, exists, or
updated; a file there that karvi did not write is reported and left
(setup_tmpfiles_mismatch). Run it again after changing the group or the
mode.

An operator's run never creates the scratch root: on a host without it,
the scratch, the control sockets, the scoreboards, and the capacity leases
stay under basedir, one operator's.

Once the trees exist, every operator's output.root, crun.directory, and
transcript.root default to them (sharedroot auto consults
/opt/karvi/shared, then /var/lib/karvi/shared) in place of the trees under
basedir; a tree the operator cannot write to is refused with the group
named. sharedroot none keeps the trees under basedir.

setup tab places karvi's bash completion under /etc/bash_completion.d, which
the bash-completion package reads at every login, so Tab completes karvi's
command words, options, values, device names from the inventory, and job
IDs from the scoreboard; the words come from the executable at each Tab, so
the file needs no change when karvi changes. The file is reported as
created, exists, or updated; a file there that karvi did not write is
reported and left as it is (setup_completion_mismatch), and a host without
the bash-completion package is told (setup_completion_dir_missing). --help
shows this text.
`

const watchHelp = `Usage:
  karvi watch [--format tui|table|json] [--theme auto|dark|light|nocolor]
              [--color auto|always|never] [--refresh DURATION]
              [--stale-after DURATION] [--filter TEXT] [--sort KEY]
              [--once] [--help]

The screen shows one row per job from the scoreboards (scoreboards: the
shared folder and the operator's own), refreshed every watch.refresh (2s):
JOB-ID, TIME (the running duration, then the end time), STATUS, OPERATOR,
MODE, DONE as done/total, FAIL, ACTV (devices in flight), and TARGET,
with a line under the headings. Running jobs stand above a rule, finished
jobs below it newest first; the running rows and the rule stay on the
screen while the finished rows scroll; ! flags a stale running job, ? an
unreadable file. Keys: q or Ctrl-C leave; arrows or j/k select a row,
PgUp/PgDn a screen, Home/End the ends; Enter opens the detail pane on the
selected job and Escape closes it; left/right scroll the pane's
devices; / filters the rows by words, each a case-insensitive substring
that must occur in operator, job
ID, target, mode, or status (Enter keeps it, Escape clears it); s cycles the sort key in the
columns' order (time, status, operator, mode, fail, target) within each
section and S reverses it; t toggles dark and light for this screen; ?
lists the keys. --filter TEXT and --sort KEY start the screen with them and apply to
--format table, which prints the columns once; --format json prints every
snapshot and refuses both; the TUI needs a terminal on stdout. Human
timestamps, themes, and role colors are configured under [display].
Specified, not yet available: --all-retained.
`
