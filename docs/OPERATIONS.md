# Operations guide

## Upgrade

After replacing the executable, inspect the private per-user daemon:

```bash
karvi daemon status
```

A daemon is compatible when its version and its IPC schema both equal the
client's, so every upgrade restarts the daemon. When either differs, karvi
reports `compatible: false` and the exact remediation. Confirm
`active_jobs: 0`, then perform an explicit restart:

```bash
karvi daemon restart
```

With no option, stop and restart refuse while the daemon reports active jobs.
`--grace` drains and waits for them, `--after=DURATION` drains and waits up to
the limit before forcing, and `--force` stops immediately for an operator who
intentionally accepts interruption; forced work still receives terminal
records. Interrupting a `--grace` or `--after` wait leaves the daemon draining.

Error messages begin with a registered code;
[`docs/ERROR-CODES.md`](ERROR-CODES.md) lists each code with its cause and exit
status.

A daemon left by a v0.10.0 preview before schema 6 (pre4 and earlier) answers
the same way: `compatible: false`, stop it with its own executable or `karvi
daemon restart`. The new client uses lifecycle-only compatibility to stop the
older same-UID daemon and then starts the current executable. Job requests are
never downgraded, and a normal `karvi run` never kills or silently replaces an
incompatible daemon. Operators who need a one-shot foreground run before
restarting may explicitly use `karvi run --no-daemon ...`.

For a planned upgrade, stopping before binary replacement remains valid:

```bash
karvi daemon stop
```

## Rehearsal, detach, and follow

`karvi run --dry-run ...` plans on the client and probes the daemon without
launching one: it prints the inspection report (text, or `--format json`
or `jsonl`) and submits nothing. `karvi run --exercise ...` submits the
plan and package and has the daemon validate everything but the device: the
report lands in the job directory as `exercise.json` and on stdout, the
summary reads `exercised`, and the exit is 0 when every target is `ready`
or the first finding's exit otherwise. Neither mode sends ICMP, opens a
session, invokes askpass, or runs a command. The two flags exclude each
other, neither combines with `--detach`, and `--exercise` needs the daemon.

A live run renders records as the daemon makes them durable. `--detach`
prints the job ID and artifact directory and returns at acceptance (exit 0
means accepted; follow the job with `karvi job follow`, watch it with
`karvi watch`, or read its `summary.json` when it ends). `--follow=false`
waits for the result without rendering.
Ctrl-C during a run stops the client's follow only: it names the job that
continues and its artifact directory and exits 113; the daemon finishes the
job and writes its records. A closed stdout does the same with exit 111.

To stop one accepted job without stopping the daemon:

```bash
karvi job cancel JOB-ID --reason 'wrong change window'
```

The job ID is the one `run --detach` printed or `karvi watch` shows. The
daemon answers at once; the job's unfinished work is recorded `cancelled`
and its summary ends `cancelled` at exit 113 with the request's time,
reason, and requester in a `cancellation` block. `--follow` waits for the
job's end and exits with the job's exit. A job that already ended is
reported with its outcome, an unknown job ID is `job_unknown`, and the
command never launches a daemon. Other jobs are untouched; a foreground
`run` whose job is cancelled from elsewhere prints the cancelled line and
exits 113.
A cancel that arrives too late changes nothing: a job whose work had
finished keeps its result and its exit, and an exercise (`run --exercise`)
never ends `cancelled`, since its checks are local and take milliseconds.
`job cancel` may still answer `cancelling` for such a job; its summary then
carries the `cancellation` block under its own final status (`exercised`,
`succeeded`), the audit trail holds the `run.cancel_requested` record, and
no cancelled line is printed.
To see a detached or interrupted job again, from any later invocation:

```bash
karvi job follow JOB-ID [--format text|jsonl|json] [--echo] [--border|--noborder]
```

It renders the job from its beginning as the foreground run would have, live
until the job ends or at once for a job that has ended, ends the display as a
run's ends (the footer from the job's summary in text, the summary document as
the last line under `jsonl`, a collection's line after the footer in text; no
result line on standard error), and exits with the job's own exit (0 completed,
113 cancelled, 106 incomplete). Under `jsonl` the output is the job's
`commands.jsonl` byte for byte, then its summary. The daemon serves the job
while it holds it; a finished job the daemon no longer holds (after a restart,
or evicted from the bounded job table) is read from its directory, found by the
job ID under `output.root`. A directory without a summary whose job no daemon
holds is `job_orphaned`: the daemon that ran it is gone, or the job was run
with `output.files.summary-json` false and has no terminal fact on disk. A job
run with `output.files.commands-jsonl` false is followed live by the client
that started it; a later `job follow` starts at the edge and says which records
are not kept. Ctrl-C stops only the follow. Neither `job` verb launches a
daemon.

`karvi-run(1)` (`man karvi run`), REHEARSAL, restates the dry run, the
exercise, `--detach`, and Ctrl-C for the terminal; a change to one changes
both.

## Dispatch: serial, parallel, and wave

A `run` speaks to its devices one at a time unless told otherwise
(`dispatch.default = "serial"`); `command` is always one device at a
time. Two modes widen it:

- `--dispatch parallel` (`--dp`) starts a fixed pool of workers,
  `--workers N` or `dispatch.parallel-workers` (the host's logical CPU
  count at `0`), over one queue in the dispatch order: a worker that
  finishes a device takes the next at once, and the pool never changes
  size during the job.
- `--dispatch wave` (`--dw`) runs the devices in waves of `width × 4`
  (`dispatch.wave-depth-multiplier`), each with `width` workers, and
  sizes the next wave by the host's CPU: up by half while the host is
  under 65 % busy, down by a tenth over 85 %, never under the start width
  (`--start-width`) and never over the ceiling (`--max-width`); between
  waves the error gate (`--wave-gate-error-count`,
  `--wave-gate-error-percent`) can stop the job and `--wave-delay` can
  pause it.

Each of these options sets its key for the one run (`--dispatch` and its
shortcuts `dispatch.default`, `--workers` `dispatch.parallel-workers`, and
so on), above the files and the environment and below `--set`: a key the
site has locked refuses the option (`config_lock_violation`, exit 3), and
a value outside the key's range is the key's own error, exit 2
(`--wave-delay 2h` is `config_value_out_of_range`, `0s..1h`).

Whichever the mode, the host's cap `dispatch.server-max-inflight` bounds the
sessions in flight across every job and every operator on the host: a worker
past the cap waits for a lease before it connects, so a wide job beside another
shares the cap rather than exceeding it. The records name each device's mode,
wave, width, and worker (`dispatch` in `commands.jsonl`), `metrics.json` the
wave decisions with the CPU signal behind each, and the watch screen the job's
devices in flight. [`docs/SCALE.md` "The width"](SCALE.md#the-width) has the
defaults by host, the ramp's rules, and 100 devices worked through both modes,
executed.

`karvi-run(1)`, DISPATCH, restates the modes, the widths at 0 with their
values on 4, 8, and 32 logical CPUs, the cap, the halts, and the gates for
the terminal; a change to one changes both.

## The watch screen

`karvi watch` is a screen to keep open through a maintenance window. It
reads the shared scoreboard directory
(`watch.directory`, `/dev/shm/karvi/scoreboards`; on a host without the
scratch root, the operator's own under `basedir`, "The shared trees")
every `watch.refresh`
(`2s`) and shows one row per job: `JOB-ID`, `TIME` (the running duration,
then the end time), `STATUS`, `OPERATOR`, `MODE` (`login`, `cmd`, `run`,
`crun`, `exercise`), `DONE` as `done/total`, `FAIL`, `ACTV` (devices in
flight), and `TARGET` (a target file's name, then the devices in flight,
then the rest), with a line under the headings. Running jobs stand above a
rule, sorted by start; finished jobs below it, newest first; the footer
counts what is not shown. The line under the headings and the rule never
leave the screen: the running rows are pinned between them and only the
finished rows scroll, so a sort or a long list never takes a running job
out of view. With no running job the two lines
stand together: an empty band says at once that nothing is in progress. A
running job whose snapshot has not moved for `watch.stale-after` (`10s`)
is flagged `!` and dimmed, its status word unchanged; a file the screen
cannot read is flagged `?` with `invalid` as its status and its path as
its target. The clock at the top right is the time of the last refresh.
Nothing in the screen names a command: the statements are read in the
job's folder.

The keys: `q` or Ctrl-C leave, and the shell is as it was; `↑`/`↓` or
`k`/`j` select a row (marked `>`), PgUp/PgDn a screen of rows, Home/End
the first and last; Enter opens the detail pane on the selected job (the
kind of work, operator, host, pid, daemon or in process, dispatch, the
times, the metrics, and every device with its state, running first, then
failed; `←`/`→` scroll the device columns) and Escape closes it; `/`
opens a prompt in the footer's place and filters the rows by one
case-insensitive substring over the operator, the job ID, the target
names, `MODE`, and `STATUS`, applied as it is typed (Enter keeps it,
shown as `filter: TEXT` with the counts as shown of all; Escape clears
it); `s` cycles the sort key in the columns' order, left to right (time,
status, operator, mode, `FAIL` most first, `TARGET`'s first name) within
each section, so the running rows stay above the rule, and `S` reverses
it (`sort: fail ↓`; `S` again restores it); `t` toggles dark and light for
this screen; `?` lists the keys. The selection is a job, so a refresh, a
filter, or a resort keeps it while its row is shown. The footer's key
hints shrink on a narrow terminal; `?` has the whole list. A resize
redraws the screen; a terminal narrower than 40 columns or shorter than 8
lines shows one line saying so until it grows. The colours are the twelve
`display.colors.*` roles under `display.theme`, dark by default.

For a script, `--format table` prints the same columns once and `--format json`
every snapshot; the TUI refuses a stdout that is not a terminal
(`watch_tui_requires_terminal`). `--filter TEXT` and `--sort KEY` start the
screen with them and apply the same rule to `--format table`; `--format json`
refuses both (`watch_json_filter_unsupported`), since it prints whole snapshots
for the script to filter itself. Neither has a configuration key: the filter and
the sort are the session's. A test or a script that starts an activity sets
`watch.directory` under its own work directory, as it sets `basedir`, so the
shared directory holds only real jobs; the files a host already holds are yours
to remove (`karvi-prune`).

`karvi-watch(1)` (`man karvi watch`), FILES, restates the scoreboard
directory and its fallback for the terminal; a change to one changes both.

## The ICMP gate

`karvi run --ping ...`, `karvi command --ping ...`, and `karvi login --ping
...` send exactly two ICMP echo probes to each target's selected address
before opening its transport; `--noping` sends none. The flags set
`network.ping-targets` through the lock-aware layer for one invocation (a
locked key refuses them; `--set` outranks them), and the key itself turns
the gate on for every invocation. Both flags together exit 4. One reply
lets the device proceed, with an `icmp_packet_loss` notice on its first
record; two misses skip the device as `icmp_unreachable` with no transport
opened and no credential exposed: `command` and `login` exit 110, a `run`
counts a failed device under its ordinary halt and gate policy. Each probe
waits `network.ping-timeout` (500 ms by default, at most 10 s), so a
skipped device costs about two timeouts. The gate never probes an
alternate address and never changes address selection.

The gate sends its probes directly over a Linux ping socket when the kernel
grants one to the operator's group. Set the sysctl to cover the groups your
operators run under, for example:

```
net.ipv4.ping_group_range = 1000 9999
```

Without it karvi falls back to the system `ping` executable on `PATH`,
which works but opens a sub-process for every gated device. `karvi version`
reports the method the host gives this process as `icmp_method`.
`network.ping-socket=false` or `network.ping-system=false` disables a
method; with the gate enabled at least one must remain, or the
configuration is refused (`config_ping_methods_disabled`). With neither
method available an enabled job is refused before it starts,
`icmp_capability_unavailable` (exit 8), and `run --dry-run --ping` or
`run --exercise --ping` shows the same as `capability=unavailable` (a
warning in the dry-run, `not_ready` in the exercise) without sending a
probe.

Text output prints one line per gated device before its header:

```
! core-nyc-01 [2001:db8:10::1] ping(1) 2.1ms, ping(2) timeout, proceeding
! edge-b [192.0.2.1] ping(1) timeout, ping(2) error, skipped
```

The line is the `display.ping.header` template, rendered and coloured as the
headers are (`<rtt1>`, `<rtt2>`, `<result>`;
[`docs/DISPLAY-CONFIGURATION.md`](DISPLAY-CONFIGURATION.md)); an empty template
turns the line off and `--quiet` suppresses it; `--debug` adds the ICMP error
details and the packet-loss notice. Every record of a gated device carries a
`ping` object (address, method, the two outcomes with round-trip times, the
decision) and `ping_ns`; the job's `summary.json` carries a `ping` block
counting gated, proceeded, degraded, skipped, and capability-failed devices and
the probes sent, replies, timeouts, and errors. With the gate disabled every
such field is null and no ICMP socket or process is created.

## Daemon environment

`karvi run` starts the per-user daemon with a filtered environment: `HOME`,
`USER`, `LOGNAME`, `PATH`, the names in `security.child-environment-allowlist`,
every `KARVI__*` configuration variable, `KARVI_ASKPASS_PATH`, `TZ`, and
`TMPDIR`. Nothing else from the shell reaches the daemon, so exported device
credentials such as `NETPASS` never sit in the daemon process. The daemon
does not read them in any case: the submitting client resolves credentials
and delivers them per job over the credential socket.

A daemon started another way, `karvi daemon start --foreground` or a service
unit, keeps whatever environment its starter gave it. When that environment
holds `NETUSER`, `NETPASS`, or `NETENABLE`, the daemon writes one
`daemon_environment_secret_variable` line per name to its log, naming the
variable and never its value; start it from a shell or unit without them.
A variable the daemon needs from the shell, an agent socket for example,
goes in `security.child-environment-allowlist`.

## The daemon's idle exit

A daemon a client launched is one process per operator and per
configuration (`basedir`), and it outlives the terminal and the login
that started it. On a host used by a team, that is one daemon per
operator, each holding its start-time configuration and executable, for
as long as no one stops it. `daemon.shutdown-idle-timer` (`"1h"` by
default) ends that: after that long with no active job, no preparation in
progress, and no request but `ping` and `status`, the daemon stops itself
exactly as `daemon stop` on an idle daemon would, exit 0, with one line in
`logs/daemon.log` (`stopping: idle`, the idle time, the key and its
value). The check runs once a minute, so the exit comes within a minute
of the timer. The next `run` finds no daemon and launches one (some 80
ms; it prints `daemon started` unless `--quiet`) with the configuration
and executable of that day. `0` never exits; otherwise the value is 1m to
720h. Measured: an idle daemon holds 19 MB and 9 threads fresh, 43 MB some
minutes after a 76 MB job, and no connection to any device.

`status` and `ping` keep no daemon alive, so a monitor may poll as often
as it likes. Every other request, and a job's end, restarts the clock.
The client probes the daemon just before its first request and repeats
that request once, with the plan it already made, if the daemon left in
between; an operator sees at most a `daemon started` line.

Under systemd the packaged unit (`packaging/systemd/user/karvi-daemon.service`)
starts the daemon with `--set daemon.shutdown-idle-timer=0`, because an
idle exit is a clean exit that `Restart=on-failure` would not restart,
and the next client would then launch a daemon outside the unit and its
limits. A site running its own unit does the same. The timer is not what
ends a daemon at logout: a host whose logind has
`KillUserProcesses=yes` ends every process of the session, a running job
included, with or without the timer; `loginctl enable-linger` or the
unit keeps a daemon through logout there.

`karvi-daemon(1)` (`man karvi daemon`), FILES, restates the daemon's
places, its filtered environment, and the idle exit for the terminal; a
change to one changes both.

## Linux servers

A server is the built-in `linux`, an alias of it, or `linux_shell`. `linux`
runs each command on an exec channel of its own over either transport: one
connection per server for its command list, each command's exit status its
verdict, stdout and stderr recorded apart
([`docs/COMMAND-SESSION.md`, "The exec
channel"](COMMAND-SESSION.md#the-exec-channel)). `linux_shell` is the same
platform over the interactive shell, for a server that refuses exec requests
(`ssh_session_channel_refused` under `linux`): its records carry no exit
status, so a failed command is recorded as a success. An inventory row names
either; a `[platform.NAME]` table with `driver = "linux"` gives a class of
servers its own caps, port, fallback, or collection list
([`docs/COLLECTION.md`, section
2.1](COLLECTION.md#21-servers-linux-and-linux_shell)). A server's credential
is the operator's own keys unless the site says otherwise ([the platform's
fallback](#the-platforms-fallback-and-the-operators-keys)).

**A failed command.** A non-zero exit is `command_exit_nonzero` (exit 107), a
signal `command_exit_signal`, and a channel closed without a status
`command_exit_missing`; each is a device error, so the device's later
commands are not attempted unless `--continue-device-on-error`, and a command
that may fail on purpose says so (`grep … || true`). A command that needs root
is written `sudo -n …`: where `sudo` wants a password it is refused at once,
`command_exit_nonzero` with `sudo`'s message in `stderr`, and never waits.

**A command given up.** At its command timeout, a cancel, or the output limit,
`scrapligo-v1` asks the server to end the command before closing its channel.
The system transport's OpenSSH client cannot ask, so the record carries the
notice `remote_command_not_stopped` and the command may still be running on
the server, its parent pid 1; a later command finds it (`ps -eo
pid,etime,args`) if it matters. In both cases the connection serves the next
command.

**The control sockets.** Over `system` each `linux` server's connection is an
OpenSSH ControlMaster, the client's child for `command` and the daemon's for
`run`, with a socket in `ssh.control-path-root`:
`/dev/shm/karvi/<user>/sockets` where the site's scratch root exists, else
`<basedir>/socket/ssh`, one 16-hex-character name per device session, removed
when the session ends. The root may be at most 73 bytes, so that a socket's
path fits OpenSSH's limit; a longer one stops each exec target at planning with
`control_path_root_too_long` (exit 2), naming the root and its length. `~` in
the root is the home the password database names. A master killed outright
leaves its socket; the next daemon start and the next job's admission remove
it, only a name karvi makes and only when nothing answers, logged in
`daemon.log` and under `--debug`:

```text
time=… level=INFO msg="removed the abandoned control socket" code=control_socket_abandoned_removed path=/home/netops/.local/share/karvi/socket/ssh/fedcba9876543210
DEBUG control_socket_abandoned_removed: removed the abandoned control socket /home/netops/.local/share/karvi/socket/ssh/0123456789abcdef
```

A server is qualified by the rows of
[`docs/DEVICE-QUALIFICATION-RUNBOOK.md`, section
9](DEVICE-QUALIFICATION-RUNBOOK.md#9-a-production-server).

## The platform's fallback and the operator's keys

When no backend of the policy answers for a device, its platform's
`fallback` says what follows, in order: `netvars` reads `NETUSER`, `NETPASS`,
and `NETENABLE`; `keys` is the operator's login name and own keys; `prompt`
asks at the terminal ([the credential prompts](#the-credential-prompts)).
The network built-ins and `generic` are `["netvars", "prompt"]`, `linux` and
`linux_shell` `["keys"]`, an alias its driver's; a table replaces the list
whole, and an empty list is no fallback. The variables an operator exports
for routers reach no server unless the site says so:

```toml
[platform.linux]
fallback = ["netvars", "keys"]
```

The operator's keys are `ssh.identities`, in its order: by default
`~/.ssh/id_ed25519`, `~/.ssh/id_ecdsa`, `~/.ssh/id_rsa`, `~` the home the
password database names, each an absolute path or one under the home; a site
may lock the list. At planning a file that does not exist is passed over, and
one that exists is used only when it is the operator's own regular file with
mode `0600` or `0400` (a symbolic link under
`security.allow-credential-symlinks`), opens without a passphrase, and is not
hardware-backed. Any other is skipped with the notice `operator_key_skipped`,
naming the file and the reason, as a warning line, a dry-run finding, and on
each device's first record. The dry run shows each key used with its
fingerprint:

```text
- name:srv1: planned
  credential: bound 20261005T055915.472140-0400-2zmnvah6w9h7jap1d0fr (policy=default backend=builtin-operator-keys user=netops; value not displayed)
  key: /home/netops/.ssh/id_ed25519 SHA256:lCkD25f/uZQGbWYmns4BurmVr65NAa+wSHkq5Y/lnVk
```

With no key left the device fails with `credential_operator_keys_missing`
(exit 6), listing the files examined. Telnet takes no key. A backend's
credential holds no key, so it needs a password (`credential_password_missing`
otherwise).

At the connection the process that connects (the daemon for `run`, the client
for `command` and `login`) reads the key files as they are then, compared with
nothing, and offers them in order, each spending one of the server's
authentication attempts (`MaxAuthTries`, 6 by default in OpenSSH). The system
transport writes `IdentitiesOnly yes`, `IdentityAgent none`, and an
`IdentityFile` per key, and the native transport offers the keys as signers;
neither offers a password method for a credential with keys and no password,
so a server that refuses every key is `authentication_failed` (exit 108) with
no prompt. Under `ssh.include-user-config` (the default) the operator's
`~/.ssh/config` is still included after karvi's settings, and OpenSSH adds its
`IdentityFile` lines after karvi's keys even under `IdentitiesOnly`: the system
transport may offer such a key once karvi's are refused.

Both transports try the methods in one order: `publickey`, then
`keyboard-interactive`, then `password`, the last two answered with the
password, since some servers allow keyboard-interactive and refuse password.
Each record's `credential.auth` names the method that authenticated the session
(`publickey`, `keyboard-interactive`, or `password`; absent where the session
never authenticated), and every `command_completed` audit event names it in
`device_identity` beside `device_username` and `backend`:

```text
1 {"auth": "publickey", "backend": "builtin-operator-keys", "device_username": "netops", "selected_address": "127.0.0.1"}
2 {"auth": "publickey", "backend": "builtin-operator-keys", "device_username": "netops", "selected_address": "127.0.0.1"}
```

Which key authenticated is not recorded. `login` names no method.

## The credential prompts

When the platform's fallback reaches `prompt` (after `netvars` on the
network built-ins), karvi asks at the controlling
terminal for what is missing and needed (`creds.interactive-prompt`,
`creds.prompt-for-username`, `creds.prompt-for-password`): `Username for
router1: `, then `Password for router1: `, and the enable secret only where
the platform requires one. A run of several targets asks once and names
the count (`Password for 3 targets: `); the daemon never asks. The answer
is edited as a stream line is: Backspace (DEL or Ctrl-H) and the Delete
key, the arrows, Ctrl-A and Ctrl-E, Ctrl-K, Ctrl-U, and Ctrl-W; a password
is not echoed. A username and password typed or pasted together are both
read.

No job runs without the credential a prompt asks for, so the prompt is
the run's last chance and its refusals end karvi at once, returning to the
shell, before any later prompt and with no job made:

| At the prompt | Code | Exit |
|---|---|---|
| Ctrl-C (in a stream too, which ends) | `credential_prompt_interrupted` | 113 |
| Enter on an empty answer | `credential_username_missing`, `credential_password_missing`, or `credential_enable_missing` | 6 |
| Ctrl-D on an empty line, or no controlling terminal | `credential_prompt_unavailable` | 6 |

```text
Username for router1:
credential_prompt_interrupted: 1 of 1 targets failed credential resolution: name:router1 (interrupted at the username prompt policy=default)
```

A script that runs karvi without a terminal sets the three variables, or
names a backend, since nothing can be asked.

## The environment backend

A backend of `type = "env"` reads one credential from environment variables in
the client, before the job is planned. Three optional templates name the
variables, one per field:

```toml
[credential-backend.operator-env]
type = "env"
username-var-template = "KARVI_%s_USERNAME"
password-var-template = "KARVI_%s_PASSWORD"
enable-var-template = "KARVI_%s_ENABLE_PASSWORD"
```

- **`%s` is the operator's login name**, as the account has it, case kept:
  for the operator `netops` the templates above read `KARVI_netops_USERNAME`,
  `KARVI_netops_PASSWORD`, and `KARVI_netops_ENABLE_PASSWORD`. A template
  without `%s` is the variable's name as written (`SRV_USER`); a template holds
  at most one `%s`; a template left out reads nothing for its field.
- **One credential per operator.** Nothing in a template names the device, so
  every device the policy sends to the backend takes the same credential. A
  credential per device comes from a backend whose rows select devices: the
  credential CSV ([`docs/CREDENTIAL-CSV.md`](CREDENTIAL-CSV.md)) or a
  `.cloginrc`. A policy map rule sends a class of devices to a policy of its
  own (`platform = "linux"`, or a glob such as `linux*`).
- **It answers when any of its variables is set.** With none set it answers
  nothing and the policy's next backend is asked. With one set it answers, and
  the credential is judged as any backend's: a field the device needs and the
  backend left empty is that field's missing code (`credential_password_missing`
  for a password), and no later backend, variable, or prompt is asked.
- **Indirection.** `env-indirection.username`, `.password`, and
  `.enable-password`, each `false` by default, make the variable's value the
  name of another variable whose value is used; when that variable is not set,
  the value is taken as written.
- **Not keyed.** A device pinned by `credkeyref` skips the backend
  ([`docs/CREDENTIAL-CSV.md`, section
  5](CREDENTIAL-CSV.md#5-keys-and-pins-credkey-and-credkeyref)). The record's
  `matched_on` names the operator, not the device.

The fallback's `NETUSER`, `NETPASS`, and `NETENABLE` are read without a
backend, after the policy's backends, where the platform's fallback holds
`netvars` ([the platform's fallback](#the-platforms-fallback-and-the-operators-keys));
an `env` backend is how a site names variables of its own.

## Credential files

A `cloginrc` backend declares `scope = "user"` (the default) or
`scope = "shared"` and, for a user file, `required`. A user file is the
operator's own, mode `0600`; when optional, an absent or unreadable file is
a silent not-found and the next backend in the policy is consulted. A shared
file is always required, needs an absolute path, mode `0640`, an owner of
root or a name in `security.approved-admin-users`, and the
`security.shared-group` group; an absent or unreadable one refuses the run
with `credential_file_unavailable` before a job exists. Any file that is
present but fails its symlink, type, owner, or mode check fails whatever
the scope. A relative `include` resolves beside the file that includes it
and inherits its scope; a shared file cannot include from a home directory.

The checks are the same for every credential file backend and carry one
code each, the message naming the backend and the path and never a value:

| Code | The file |
|---|---|
| `credential_file_symlink_rejected` | is a symlink and `security.allow-credential-symlinks` is off |
| `credential_file_not_regular` | is a directory, a FIFO, a device, or a socket |
| `credential_file_owner_uninspectable` | has an owner the system cannot report |
| `credential_file_owner_mismatch` | is a user file owned by someone else |
| `credential_file_mode_unsafe` | is a user file whose mode is not `0600`; the message gives the `chmod` line |
| `credential_file_shared_mode_invalid` | is a shared file whose mode is not `0640` |
| `credential_file_shared_owner_unapproved` | is a shared file owned by neither root nor an approved administrator |
| `credential_file_shared_group_unknown` | is a shared file and `security.shared-group` names no group |
| `credential_file_shared_group_mismatch` | is a shared file in another group |

karvi opens the file first and checks the file it opened, so the file that
passed the checks is the file that is read. A file that is present but cannot be
opened (mode `0000`, say) is reported by its check and is not treated as absent.
These codes replaced the `cloginrc_*` check codes of v0.10.0 and v0.11.0;
[`docs/ERROR-CODES.md`](ERROR-CODES.md) lists the retired names.

### The credential CSV

A `csv` backend is the second file backend: device-keyed rows of username,
password, and enable password under the file rules above.
[`docs/CREDENTIAL-CSV.md`](CREDENTIAL-CSV.md) is its guide and this section only
says where to look in it:

| Task or symptom | [`docs/CREDENTIAL-CSV.md`](CREDENTIAL-CSV.md) |
|---|---|
| Declaring the backend; `scope` and `path` have no default; the `config validate` codes | [section 2](CREDENTIAL-CSV.md#2-declaring-the-backend) |
| The nine fields, headers and mappings, numeric mode, quoting, verbatim secret cells | [section 3](CREDENTIAL-CSV.md#3-the-file) |
| Which row a device takes (top to bottom, first match, no longest prefix); a blank password; `matched_on` for a `csv_row` | [section 4](CREDENTIAL-CSV.md#4-how-a-row-is-chosen) |
| Pinning a device to a row with the inventory's `credkeyref`; `credkeyref_unresolved`, `inventory_credkeyref_invalid` | [section 5](CREDENTIAL-CSV.md#5-keys-and-pins-credkey-and-credkeyref) |
| Cells that name environment variables, and the unset-variable hazard | [section 6](CREDENTIAL-CSV.md#6-secrets-in-the-environment-instead-of-the-file) |
| A formula whose password source is a credential CSV; the row's username is dropped | [section 7](CREDENTIAL-CSV.md#7-a-formula-over-a-credential-csv) |
| `inventory_secret_column`, `config_inventory_source_secret_mapping`: an inventory file holds no secrets, there is no override, and what the check does not find | [section 9](CREDENTIAL-CSV.md#9-an-inventory-file-holds-no-secrets) |
| `credential_csv_malformed`, `credential_csv_row_invalid`, `credential_csv_credkey_duplicate`: the messages, which never quote a cell | [section 10](CREDENTIAL-CSV.md#10-what-a-bad-credential-csv-says) |

Two things to know at an upgrade. An inventory file with a column named
like a secret (`password`, `enable-password`, `api_token`) no longer loads
(exit 5): move the secrets into a credential CSV and delete the column. And
a pinned device never falls to a general row, to `NETUSER`/`NETPASS`, or to
a prompt: a mistyped `credkeyref` stops that device with
`credkeyref_unresolved`, naming the device and the key.

## Address-family troubleshooting

The configured default is IPv6-first. Use `--ipv4`/`--4` or `--ipv6`/`--6` for
one invocation and `--debug` to inspect the selected address. The flags are
mutually exclusive. A literal `--address` bypasses DNS.

```bash
karvi command --debug --4 --host router1 --cmd 'show clock' \
  >command.out 2>command.debug
```

The DNS timeout default is 2 seconds. Resolver failures retain distinct stable
codes such as `dns_nxdomain`, `dns_timeout`, and `dns_servfail`.

## Border operation

Default text output places one blank line between records and none at the end.
Use `--border` for a dynamic dash separator, `--noborder` for no separator, or
configure static values separately for command and run. Set `last-border=true`
only when a terminal trailing separator is operationally useful.

## Device-error echo

When investigating CLI syntax or privilege errors, enable `--echo`. The prompt
and command are displayed for success and device-reported errors. Debug output
holds no secret and no device output, and shows each command once, as the plan
holds it; capture stdout and stderr separately when opening a support issue.

## Start statements for generic devices

A device that runs as `generic` (a row set to `generic`, any device under
`--pg`, and a row with no platform or a target outside the inventory
when the site has set `platform-resolution.default` to `generic` or
cleared it; as shipped the key is `cisco_iosxe`) is sent nothing at the
start of its session: `generic` has no paging
statements of its own. Two mechanisms send statements before the requested
commands, in `command` and `run`. They differ in which devices they reach
and in what they record.

| | `[platform.generic] paging-commands` | a `[session-init.NAME]` profile by `[[session-init-map]]` |
|---|---|---|
| Reaches | every device that runs as `generic`, a platform-less row and a target outside the inventory included | the devices its map rule matches; a `platform = "generic"` rule matches only a row whose platform is set to `generic` |
| Recorded as | set-up lines of `output.TARGET.txt`; no command record | a command record per statement, `command_kind` `session_init` |
| A statement the device rejects | under `generic` nothing fails: the device's answer is in the text file and the session goes on (`generic` has no failure patterns) | `on-error`: `fail-device` (the default) or `continue` |
| Chosen by | the platform the device runs as | name, site, group, or platform, first match |

```toml
# In the operator's own configuration: per user.
[platform.generic]
paging-commands = ["terminal length 0", "terminal width 512"]
```

Use the table when every generic session is to start the same way; use a
profile when the statements are to have records, an `on-error` rule, or
a selection by name or site. The catch with a profile: a device whose
platform is not set is matched by no `platform =` rule, so a target
outside the inventory is reached only by a
`name = "*"` rule, or by giving it a platform with `--pg` or
`--platform`. The `paging-commands` of any `[platform.NAME]` table are
that platform's start statements in the same way; the key's name comes
from their usual content. `login` hands the terminal to OpenSSH and
types nothing into it: neither mechanism acts there.

## The job's output files

A job folder (`<basedir>/jobs/YYMMDD/<job id>/`) holds eight kinds of
file, each with a true/false key under `[output.files]`, all true by
default. A file set false is not created. Every file is written from the
job's memory and none from another file, so switching one off never
breaks the writing of another; what you lose is what reads it.

| Key | File | What it is | What is lost when false |
|---|---|---|---|
| `output-txt` | `output.NAME.txt`, one per device; NAME is the device name's first label, lowercased (`output.crop-to-dot`, true; `false` writes the whole name), or an address with its dots and colons as hyphens | the session as you would read it in a terminal; its header's time is `display.timestamp` in the effective `timezone` | the readable copy; it can be derived again from `commands.jsonl` (`tools/textfile`, given the same two settings) |
| `commands-jsonl` | `commands.jsonl` | every record, one JSON object per line: the authority | `karvi job follow` cannot replay the job; a device's text cannot be derived again; tools that read records |
| `errors-jsonl` | `errors.jsonl` | the records that did not succeed, each also in `commands.jsonl` | the short list of failures |
| `failed-devices-txt` | `failed-devices.txt` | the devices to run again | the rerun's `--tf` file |
| `commands-txt` | `commands.txt` | the commands as requested | the rerun's `--cf` file |
| `manifest-json` | `manifest.json` | the job as it was accepted | the record of what was asked, by whom, under which configuration |
| `metrics-json` | `metrics.json` | process and dispatch measurements | the measurements |
| `summary-json` | `summary.json` | the job's result, counts, causes, and the paths of the files written | `karvi job follow` reports the job as orphaned once no daemon holds it; `karvi-prune` knows a finished job by its summary and treats a folder without one as an orphan, removed by age alone once its day, everything in it, and its scoreboard file are older than the retention age, never under free-space pressure |

Relationships worth knowing:

- **The text file does not need `commands.jsonl`.** `commands-jsonl = false`
  with `output-txt = true` keeps text only. It is a one-way choice: without
  the records there is nothing to derive a device's text from again, and
  the notice of a text file that could not be written says so.
- **A rerun needs `failed-devices.txt` and `commands.txt` together**
  (`karvi run --tf …/failed-devices.txt --cf …/commands.txt`).
- **The summary names only the files that were written** (`paths`, and
  `output.commands_jsonl`). The devices' text files are not listed; the
  name is `output.` + the device's name (or its address) + `.txt`.
- **All eight false is no job folder at all,** the same as `cmd --nof`:
  the footer ends `artifacts=none`.
- **The invocation decides a job's files on every path.** The plan carries
  the eight switches, `output.persist-command`, and the resolved
  `output.root`, so a run through the daemon keeps the files the client's
  configuration says and writes them under the client's root; the daemon
  reads its own `output.files.*` for no job. A job that keeps no
  `commands.jsonl` is followed from the live edge (records before the
  follow are not kept, and the client says so once); a folder without
  `summary.json` is an orphan to `job follow` and to the pruner.
- **`output.max-job-bytes` counts the devices' output as written: the
  `commands.jsonl` lines and the text blocks together.** With both files
  on, a job's output is on disk twice, and the space preflight
  (`output.expected-bytes-per-device`) asks for both copies. At the
  limit the record comes first: a text block that would pass it is left
  out (`output_text_write_failed`, the job continues); a record that
  would pass it ends the job as an output failure, exit 111, with
  `output_job_limit_exceeded` and the device on standard error, the
  device listed in `failed-devices.txt`.
- `output.persist-command = false` (`--nof`) is all eight false, on
  `cmd`, `run --no-daemon`, and a run through the daemon alike: the
  invocation's keys travel in the plan and the daemon's store honours
  them. `run --nof` with `--detach` or
  `--exercise` is refused; `run --nof --follow=false` is a run for its
  exit code.
- **`--of[=PATH]` is the opposite of `--nof`, and names the root**:
  `cmd --of` is `output.persist-command =
  true`; `--of=PATH` also sets `output.root` for that invocation (`~`
  expanded, a relative path from the working directory), so the job's
  folder is `PATH/YYMMDD/<id>`. PATH attaches with `=` only, since
  the word after a bare `--of` is the device: a word there of a path's
  form (beginning with `/`, `~`, `./`, or `../`, or `.` or `..`) is
  refused as `cli_option_value_detached`, naming `--of=PATH`, and any
  other word keeps its meaning, so a relative path is written
  `--of=out`. `run --of=PATH` does the
  same for a run on every path: the root travels in the plan, so a job
  through the daemon is written where the invocation said. Both are
  flag-origin values: a site that
  locks either key refuses the option.
- `exercise.json`, the audit log, the scoreboard, the capacity ledger,
  and login transcripts are not the job's output files and have their
  own settings.

`karvi-run(1)`, OUTPUT, restates the display, the folder's files, and the
rerun for the terminal; a change to one changes both.

## The spool directory

A command's response is held in memory up to `output.spool-threshold-bytes` (1
MiB by default) and continues into a file, the spool, above it: one file per
command in flight under `spooldir`, removed as soon as the command's record is
written and handed on ([`docs/DESIGN.md`](DESIGN.md), the output spool). The
record is the same whether the response spooled or not.

**Where.** `spooldir` is `"auto"` by default, which tries `/tmp/karvi-<uid>`
and then `/var/tmp/karvi-<uid>`, each made 0700 for the effective user;
an absolute path replaces the list. It is not `tempdir`: the scratch
chain prefers `/dev/shm`, a tmpfs, by design, and a spool must cost disk.
A site that wants both on one volume points both keys at paths under it.
A directory that cannot be written fails the job with
`spool_directory_unavailable` before any device is contacted. The daemon
reads its own configuration for its jobs: a `--set` of `spooldir` or of
the threshold on a `run` through the daemon does not reach the job, an
in-process `cmd` or `run --no-daemon` it does.

**Free space.** `freecheck` decides what free space every volume an activity
writes must have at admission, before any device is contacted: the job's folder,
a `crun`'s collection directory, `spooldir`, and a recorded login's transcripts
root, read once per volume however many of them share it. `auto` (the default)
asks each volume for the sum of what its places will hold when the job has
finished (the folder `output.expected-bytes-per-device` per device per file it
writes, times `output.reserve-multiplier`; the collection directory one copy;
the spool the job's width times `output.max-command-bytes`) plus the floor
`output.min-free-bytes-after-job` (2 GiB) once; when the spool's share is short
but at least one command fits, the job runs narrower with the warning
`spool_width_narrowed` on standard error and on the receipt and the follow start
under the daemon; otherwise the job is refused with `output_preflight_space`,
which names the volume's paths and both figures. `always` asks each volume for
the floor alone. `never` skips the free-space check (the directories are still
resolved and probed writable). The check costs one `stat` per place and one
`statfs` per volume, a few microseconds; it is a guard against writing into a
full disk, not an alert: watch the volumes behind `basedir`, `sharedroot`,
`spooldir`, and `watch.directory` with the site's monitoring, and let
`karvi-prune --minfree` keep them clear ([`docs/SCALE.md`](SCALE.md)). The
daemon's memory for output is the width in flight times the threshold, plus a
few KiB per command; there is no memory key.

**What you see.** The watch screen's detail pane shows the bytes settled
so far beside a running target and the in-flight total on the job's
metrics line, refreshed every `watch.refresh`; the scoreboard is rewritten
at that interval while any activity runs, so a login or a quiet command is
not shown stale. A spool left by a process that died mid-command is
removed at the daemon's next start and at the next job's admission, and
logged `spool_abandoned_removed`; a file in `spooldir` of another shape is
never touched. A control socket a killed master left is swept at the same two
moments, logged `control_socket_abandoned_removed` ([Linux
servers](#linux-servers)). A followed record whose line is larger than
`daemon.max-ipc-frame-bytes` allows, or one spooled in a job that keeps no
`commands.jsonl`, reaches the follower with its output left out and the
notice `follow_output_omitted` saying where the output is.

## The shared trees

A team keeps one job tree, one collection directory, and one transcript
tree. The site makes them once, as root ([`docs/FILES.md`](FILES.md) lists
every directory and file with its mode, owner, and group):

```bash
sudo karvi setup shared            # the group of the operator who ran sudo
sudo karvi setup shared --group netops --mode 2775
```

That creates `/opt/karvi` (0755), under it `shared`, and under that
`jobs`, `crun`, and `transcripts`, the four in the group with group write
and search and the setgid bit (2770, or 2775 with `--mode`), so the trees
carry the shared directory's own group and mode; beside `shared` the
operators' `users` directory in the same group at 1770 (group write and
search, the sticky bit); and the scratch root with its rule for every
boot (below). Each is reported as `created`, `exists`, or
`repaired`: a real directory with another group or mode is set right and
the line says what it had, so the command is also the way to fix a broken
layout; only a path that is not a real directory (a file, a link) is left
as it is and reported (`setup_directory_mismatch`). Without root the
command is refused before anything is looked at (`setup_requires_root`).

**The scratch root.** On the tmpfs `/dev/shm`, setup makes
`/dev/shm/karvi` and its `scoreboards` at 3770 (setgid and sticky: every
member makes its own folder or scoreboard file there and none removes
another's) and its `capacity` and `capacity/devices` at 2770 (setgid
alone: every member rewrites the ledger files another wrote), all in the
group and root's, and writes `/etc/tmpfiles.d/karvi.conf` naming the same
four with the same group and modes:

```text
created  /dev/shm/karvi  group netops  mode 3770
created  /dev/shm/karvi/scoreboards  group netops  mode 3770
created  /dev/shm/karvi/capacity  group netops  mode 2770
created  /dev/shm/karvi/capacity/devices  group netops  mode 2770
created  /etc/tmpfiles.d/karvi.conf  mode 0644
```

`/dev/shm` is emptied at every boot, and systemd-tmpfiles makes the four
again from the rule; `systemd-tmpfiles --create /etc/tmpfiles.d/karvi.conf`
does it at once. The rule is reported as `created`, `exists`, or `updated`
(another group or mode: run setup again after changing either); a file
there without karvi's first line is the site's and is reported and left
(`setup_tmpfiles_mismatch`), and a host without `/etc/tmpfiles.d` is told
(`setup_tmpfiles_dir_missing`), the directories made either way.
`packaging/tmpfiles.d/karvi.conf` is the rule for the group `netops`.

In the scratch root each operator's runs make the operator's own folder,
`/dev/shm/karvi/<username>` (0700), for the askpass socket, the system
transport's ssh configuration (`tempdir`), and the control sockets
(`ssh.control-path-root`); every operator's activities write their
scoreboards to `scoreboards`, so `karvi watch` shows the team's work; and
the session leases of `dispatch.server-max-inflight` are held in
`capacity`, so the cap holds across every operator on the host.

No operator's run creates the scratch root: the first operator to create
it would close it to every other. On a host without it, the scratch and
the control sockets are under `basedir` (`tmp`, `socket/ssh`), and the
scoreboards and the leases in `basedir/state`, one operator's: `karvi
watch` shows the operator's own jobs and the cap holds per operator, with
no warning, since nothing the site made is wrong. A `scoreboards` or
`capacity` folder missing from a scratch root that exists is made by the
first run that needs it, in the root's group. One that exists but the
operator cannot use is passed by with one warning naming it and the
private place taken: `shared scoreboard directory unavailable`, or
`capacity_root_unusable` in `shared capacity root unavailable`, whose
message names the shape it needs. A scratch root an earlier release left
behind (0700, the first operator's) is set right by `sudo karvi setup
shared`, which reports it `repaired`.

**The operators' roots.** Once `/opt/karvi/users` exists, an operator's
first activity that needs the private root creates
`/opt/karvi/users/<username>` (0750) and every later one uses it: the
daemon's socket and state, the logs, and the jobs and transcripts a site
without shared trees keeps there. The name is the operator's username, so
an administrator reads the tree without a lookup. A `users` directory the
site did not provision is never created by karvi, and the operator's root
stays under the home (`~/.local/share/karvi`); one that exists but is not
right (a file or a link, writable by everyone, or one the operator cannot
create in, for example an operator outside the group) refuses the activity
with `private_directory_not_real` or `private_directory_not_writable`
naming what to set right, rather than falling through to the home, so no
operator's state is split across two roots. A root the site provisioned
by hand for an operator is taken as it is, whatever its mode.

Once a tree exists, every operator's `output.root`, `crun.directory`, and
`transcript.root` at `auto` default to it in place of the tree under `basedir`:
`sharedroot` (`auto`) consults `/opt/karvi/shared`, then
`/var/lib/karvi/shared`, tree by tree, so a site may share the collections and
not the jobs. Jobs land in `/opt/karvi/shared/jobs/YYMMDD/ID`, the day folder
with the tree's own mode so any member can add a job folder to it, the job
folder at `output.directory-mode` and its files 0640 in the group, so `karvi job
follow ID` reads another operator's job from any member's shell. A tree the
operator cannot create files in is refused, not passed by
(`output_directory_not_writable` and its siblings, the message naming the group
and the setting); `sharedroot = "none"` keeps every tree under `basedir`, as
every test suite sets it. `basedir` itself is never shared: its `socket` and
`state` are one operator's daemon. `job cancel` reaches only the caller's own
daemon, so another operator's job is cancelled by its owner.

Under the packaged systemd user unit the daemon may write only under the
places its `ReadWritePaths` names: the basedir candidates, the shared
trees under both system roots, the configuration root, and
`/dev/shm/karvi`. A collection directory, a basedir, or a spool directory
elsewhere needs the drop-in below ("The collection run", "Under systemd")
naming it. A run in the client process (`--no-daemon`, `command`, `login`)
needs nothing of the unit.

## Retention

`karvi-prune` removes finished work older than the retention age (thirty-one
days by default) from the trees karvi writes: finished jobs, ended transcripts,
and terminal scoreboard files by their end time; job folders without a summary
and scoreboard files a crashed daemon left at `running`, by age alone; then the
day folders left empty. It never touches a running job, the collection
directory, the audit log, or journald, and long-term audit retention is the
central forwarder's, not the helper's. [`docs/PRUNE.md`](PRUNE.md) holds the
detail: what goes and why, every place looked at in sequence, the report's
lines, the exits, and the schedules; on an installed host, `man karvi-prune`
holds the rules, the report, and the exits.

Any operator cleans their own folders:

```bash
karvi-prune --dry-run            # what would go; nothing removed
karvi-prune                      # remove it
karvi-prune --dry-run --verbose  # why each item stays, and the places looked at
```

A run removes only what the invoking user owns, under the operator's
basedir and in the site's shared trees, and passes a colleague's job by.
On a shared or site install, `sudo karvi-prune` prunes every operator's
work in the provisioned private roots and the shared trees at once, and
is sufficient for the default practice on a host that installs neither
the timer nor the cron or simply wants to clean up by hand. The packaged
schedules are the per-operator timer (`packaging/systemd/user/`), the
site's root timer (`packaging/systemd/system/`), and the cron script
(`packaging/cron/karvi-prune`), all over the one executable with the
same flags, so they make the same decisions.

**The units' writable places.** Each systemd unit runs in a sandbox:
`ProtectSystem=strict` mounts the whole file
system read-only for the process except `/dev`, `/proc`, and `/sys`,
`ProtectHome` hides the homes, and `ReadWritePaths` names the only places
it may write. The site's unit ships with

```ini
ReadWritePaths=-/opt/karvi -/var/lib/karvi -/dev/shm/karvi/scoreboards
```

- `/opt/karvi` and `/var/lib/karvi`: the two system roots, whose `users`
  directories hold the operators' roots the root run walks and whose
  `shared` directories hold the shared trees.
- `/dev/shm/karvi/scoreboards`: the scoreboard directory the run prunes
  (already writable under `strict`, since `/dev` is left writable; listed
  so the line says what the unit touches).
- The dash before each path: an absent one is ignored, so the unit starts
  on a host with one system root, or before the first daemon of the boot
  has created the scoreboard directory. Without it the unit fails to
  start with status 226 (`NAMESPACE`) and prunes nothing.

The line names the same places as the unit's `ExecStart`. A site that
gives the command another `--basedir`, `--sharedroot`, or `--scoreboards`
adds that path to the line, with the dash, or every removal there fails as
`read-only file system` and the run exits 1 daily. The per-operator unit
lists the operator's basedir candidates (`%h/.local/share/karvi` and the
two system roots) and the scoreboards the same way.
The sandbox is fixed when the unit starts, so a root the process would
create must exist before the first start; an operator's first activity by
hand creates it.

## Tab completion

Bash completes karvi's words when the site has placed the function once,
as root:

```bash
sudo karvi setup tab
```

That writes `/etc/bash_completion.d/karvi`, which the bash-completion
package reads at every login, and reports it as `created`, `exists`, or
`updated` (an older karvi script, replaced). A file there that karvi did
not write is reported and left as it is (`setup_completion_mismatch`); a
host without the bash-completion package is told
(`setup_completion_dir_missing`). From the next login Tab completes the
command and subcommand words, the options valid where the cursor stands,
an option's values, platform names, `--set` keys, device names from the
inventory, job IDs from the scoreboard, and file names for a path option;
once device text has begun in `command`, `run`, or `crun`, nothing is
offered. The words come from the executable at each Tab (the hidden word
`karvi __complete`, which creates no state and resolves no credential),
so the file needs no change when karvi changes. Abbreviation stands as it
is: completion is the discoverable path and the prefix the fast one.

The same file completes `karvi-prune`: its
flags, a flag's words (`--format` text or jsonl, `--sharedroot` auto or
none, `--basedir` auto), and file names for the three path flags once no
word matches; the helper answers from its own flag set through its own
hidden word, `karvi-prune __complete`, which resolves no root and writes
nothing. A host that holds the file from before the helper was added runs
`sudo karvi setup tab` once more, which reports `updated`.

`karvi-setup(1)` (`man karvi setup`), FILES, restates what `setup shared`
and `setup tab` make, with the modes, for the terminal; a change to one
changes both.

## The collection run

`karvi crun` collects one or more commands from a scope into one file per
device, named by the device, in one flat directory: a replacement for
`rancid-run` and the Oxidized collector; [`docs/COLLECTION.md`](COLLECTION.md)
is the guide to what to collect (the command lists of both tools per platform,
and sub-platform tables per device model). The daily invocation is one word and
a scope:

```bash
karvi crun --all                       # each device its platform's crun-commands list
karvi crun --select-platform cisco_iosxe --cf ios-collect.txt
karvi crun --target core-nyc-01.example.net --cd=/opt/karvi/shared/crun
```

- **The directory** is `crun.directory`, `auto` meaning `<basedir>/crun`;
  `--cd=PATH` names it for one run (`=` only, since the next word may be
  a device). It travels in the plan, so a run through the daemon writes
  where the invocation said. A missing directory is created at
  `crun.directory-mode` (0770); an existing one is left exactly as it is.
- **The file** is `NAME`, the device name's first label lowercased
  (`output.crop-to-dot`; `false` writes the whole name), or an address
  with its dots and colons as hyphens; two devices that would share a
  name are refused at planning. Its content is each command's output
  under a `! COMMAND` marker line, a blank line before every marker but
  the first, and nothing else: no header, no prompt, no timestamp.
- **Replacement only on success.** A device's file is written as a
  hidden temporary and renamed into place when every one of its commands
  came back, a rejected statement included (its error text is the
  block); a device not reached, timed out, halted, or cancelled keeps its
  previous file and leaves nothing behind. The text display ends, after
  the footer, with `! collection=DIR replaced=N kept=M`
  (`display.collection.footer`), the summary's `collection` block
  names each device's file and outcome, and `failed-devices.txt` is the
  rerun as for any run. A `crun` runs a device's whole list past a
  rejected statement.
- **The commands** are `--cmd`, `--cf`, or the device text as on `run`;
  with none, each device is sent its platform's `crun-commands` list
  (`[platform.NAME] crun-commands = [...]`; the built-in platforms with a
  configuration ship the configuration and the version; a platform
  without a list refuses the run at planning, `--dry-run` shows each
  device's list). `commands.PLATFORM.txt` in the job folder holds each
  list sent.
- **The job folder** is written as for any run, the collection's audit,
  without `output.NAME.txt` (the collection file is that text rendered
  once more; `tools/textfile` derives the session from `commands.jsonl`).
  `--nof` collects with no job folder.
- **A shared directory.** Any member of the group that owns a directory
  of mode `2770` or `2775` (group write and search, setgid, no sticky
  bit) collects into it and replaces a file any other member wrote; each
  file is owned by whoever collected it last, in the group by the setgid
  bit, at `crun.file-mode` (0660). `sudo karvi setup shared` makes it once
  as `/opt/karvi/shared/crun` with the job and transcript trees ("The shared
  trees" above), and `crun.directory` at `auto` finds it; a directory
  elsewhere is made by hand:

  ```bash
  install -d -m 2770 -g netops /srv/configs
  ```

  A directory that cannot be prepared is refused before any device is
  contacted (`crun_directory_not_writable`), with that shape in the
  message.
- **Under systemd.** The packaged user unit sandboxes the daemon to the
  places its `ReadWritePaths` names: the basedir candidates and the shared
  trees under both system roots, which cover `<basedir>/crun` and
  `/opt/karvi/shared/crun` as shipped ("Retention" says what the sandbox
  is). A collection directory elsewhere, a `basedir` elsewhere (the
  daemon's socket, state, and job tree), or a `spooldir` elsewhere needs
  a drop-in that adds the path, with the dash that makes an absent path
  ignored instead of failing the start; `ReadWritePaths` merges across
  files:

  ```ini
  # ~/.config/systemd/user/karvi-daemon.service.d/crun.conf
  [Service]
  ReadWritePaths=-/srv/configs
  ```

  then `systemctl --user daemon-reload` and a restart.
  `packaging/systemd/user/karvi-daemon.service.d/crun.conf.example` is
  that file. `crun --no-daemon` runs in the client process and needs
  nothing of the unit.
- **`--fs=SUFFIX`** appends a literal suffix to each file's name in the
  collection directory (`--fs=.cfg` writes `NAME.cfg`), never to the
  directory's; on `crun` the directory stays `crun.directory`.
- **`run` and `command` with `--cd=PATH`** write the same file, beside their
  usual job folder (`output.NAME.txt` included), for an operator's capture:
  unfiltered (`crun-filters` are `crun`'s), replaced only when the device
  succeeds, `--continue-device-on-error` their own (without it a rejected
  statement keeps the previous file), the display ending with the same
  collection line, no hook, the watch screen showing `run` or `cmd`;
  `--fs=SUFFIX` without `--cd` is `--cd=.` as well; [`docs/COLLECTION.md`
  section 1.1](COLLECTION.md#11-a-runs-collection---cd-on-run-and-command).
  Through the daemon the directory meets the unit's sandbox as a `crun`'s does
  (above): a home directory is refused, and a path under `/tmp` would land in
  the daemon's private `/tmp`, so `--no-daemon` or the drop-in serves a capture
  there; `command` runs in the client process and meets neither.
- **The hook.** `crun.after` names an executable the client runs once the
  collection has ended and its display is printed, on the in-process path and
  through the daemon alike, never for `--detach`: in the collection directory,
  the replaced files' names on stdin one per line, and `KARVI_JOB_ID`,
  `KARVI_JOB_DIR`, `KARVI_CRUN_DIRECTORY`, `KARVI_CRUN_REPLACED`,
  `KARVI_CRUN_KEPT`, and `KARVI_EXIT` in its environment; its output follows the
  display on stderr. A hook that fails, cannot start, or runs past
  `crun.after-timeout` (`5m`) is the warning `crun_after_failed`, the run's exit
  code unchanged, and the audit holds a `crun.after` event either way. The
  shipped examples under `packaging/crun/` commit the replaced files to a git
  repository over the directory and mail the commit's diff
  ([`docs/COLLECTION.md` section
  5](COLLECTION.md#5-the-commit-and-the-diff-mail-the-hook)).
- **The drop list.** A platform's `crun-filters` (`[platform.NAME]
  crun-filters = ['^Building configuration\.\.\.$', ...]`, regular expressions
  in literal strings) drop the output lines that match from the collection file
  alone: the record and the job's files keep every line, a `! COMMAND` marker is
  never matched, and an emptied block keeps its marker. The built-in lists drop
  the byte count, the NTP clock period, the uptime, the free memory, and the
  time of the show, never the stamp that says when the configuration last
  changed; an array replaces the list whole, `[]` turns it off, and an alias
  inherits its driver's built-in list. The lists travel in the plan, so the
  daemon path drops the same lines and the manifest shows them; a pattern that
  does not compile refuses the configuration
  (`config_platform_crun_filter_invalid`). [`docs/COLLECTION.md` section
  6](COLLECTION.md#6-the-volatile-lines-the-drop-list) has the lists per
  platform.
- **The schedule.** A recurring collection is the site's systemd timer or cron
  over `karvi crun --all --no-daemon`, not a configuration key or a
  daemon-resident schedule: `packaging/systemd/user/karvi-crun.service` and
  `karvi-crun.timer` (a oneshot, one instance at a time, a missed tick run at
  the next start), and `packaging/cron/karvi-crun`, which holds a lock for the
  run and skips a tick that finds it held (one line, exit 75). Two collections
  never overlap in one directory that way; karvi itself does not refuse one, so
  an operator's single-device `crun` during the nightly run is not turned away.
  Under a timer the daemon is not launched: a oneshot's end would terminate it.
  [`docs/COLLECTION.md` section 7](COLLECTION.md#7-the-schedule).

`karvi-crun(1)` (`man karvi crun`), COLLECTION, restates the directory,
the file, the replacement, the lists, the hook, and the shared directory
for the terminal; a change to one changes both.

## Stream mode

`karvi stream`, or `karvi -`, composes a run from standard input, one
line at a time. Typed at a terminal it is
a session: set the targets once, then send batches of commands, each
`--go` a job with its own folder, display, and footer. Piped from a script
or a heredoc it drives several jobs through one karvi. The rules:

- A line is skipped when blank or when its first character is `!` or
  `#`. A line beginning with a dash and one more character is one run
  option, one dash or two alike, as on a `run` command line: the word,
  then its value as the rest of the line after a space or `=`, so
  `--target router1`, `-target=router1`, `--tl router1 router2`,
  `--dispatch parallel`, `--dp`, `--format jsonl`, `--no-daemon`. A
  value wholly wrapped in one pair of quotes, double or single, loses
  them as a shell would remove them: `--cmd "show clock"` sends `show
  clock`, and `--target 'router1'` names `router1`. Every other quote is
  sent as written: `--cmd echo "a  b"` keeps its quotes, and `--cmd
  '"x"'` sends `"x"`. Any other line is one command, sent as written,
  quotes and all, a `-` alone among them; `\r` at its end is read as
  `--cmd` reads it, and a line of `\r` alone sends a blank line. A
  command that begins with a dash goes as `--cmd`'s value: `--cmd -v` or
  `--cmd=-v`.
- The draft has two parts. The targets, the options, and the commands
  given in option form (`--cmd`, `-c`, `--command`, `--cf`, any
  spelling) stay from one job to the next; a command given as a bare
  line is the job's alone. `--expect`, `--blind`, and `--blind-return`
  lines attach to the command before them and stay or clear with it. The
  option words mean what they mean on a `run` command line,
  abbreviations and the `=` spelling included. An option whose value
  attaches with `=` alone takes it that way here too: a line `--of
  /tmp/x` is dropped with its number as `cli_option_value_detached`, and
  `--of=/tmp/x` is the line. A line that would leave text no option
  takes is dropped as `cli_positional_unexpected`: `--no-daemon yes`,
  `-- foo`, or `- foo`, which `run` would read as freeform command text
  taking every command after it.
- The directives are whole lines, one dash or two alike. `--go` or
  `--sendit` executes the draft; the bare-line commands clear and
  everything else stays, so a command given as `--cmd show clock` is
  sent by every later job; with no command left to send it prints a
  notice and runs nothing. `--clear` empties the bare-line commands and
  keeps the rest. `--purge-commands` (any prefix from `--purge-c`)
  empties every command; `--purge-targets` (from `--purge-t`) removes
  every target input (`--target`, `--tl`, `--tf`, `--tfr`, `--site`,
  `--device-group`, `--all`, `--select-platform`) and keeps the other
  options; `--purge` alone names both and is refused. `--reset` empties
  the draft. `--end`, `--quit`, `--exit`, the input's end (Ctrl-D), or
  Ctrl-C leave without executing, and a Ctrl-C outranks lines already
  read. Only `--go` and `--sendit` execute.
- Typed at a terminal, a line is edited before Enter sends it: Backspace
  (DEL or Ctrl-H) and the Delete key to erase, Ctrl-A and Ctrl-E to the
  line's ends, Ctrl-K, Ctrl-U, and Ctrl-W to cut, the left and right
  arrows to move, and the up and down arrows through the lines typed so
  far. The editing echoes on the controlling terminal, so
  `karvi stream > out` still shows what is typed. Piped input has no
  editing to do. A line is read when the stream is ready for it and never
  while a job runs, so the terminal is its own through the job: a Ctrl-C
  during a job is the interrupt it is for `run` (the follow stops, a
  daemon's job continues) and ends the stream, a credential prompt the
  job asks reads the keys typed at it ("The credential prompts"), and a
  line typed during the job waits for the stream.
- A line the parser refuses (`--typo`, a declaration before any command)
  is reported on standard error with its line number and dropped; the
  draft stands. `--cf`, `--tf`, and `--tfr` may not name `-` in any
  spelling: standard input is the stream.
- The exit is the last executed job's, 0 when none ran. A read failure,
  or a line over the reader's 1 MiB limit, ends the stream with
  `stream_input_read_failed` (exit 1) naming the line. Each job is a
  run: its records, its display, its footer or jsonl summary line, its
  exit. Global options go before the word: `karvi --quiet --config FILE
  stream`.

## Structured evidence

Treat `commands.jsonl`, `errors.jsonl`, `summary.json`, `manifest.json`, audit
records, and scoreboards as authoritative. Human headers, borders, and footers
are projections only.
