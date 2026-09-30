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

Error messages begin with a registered code; `docs/ERROR-CODES.md` lists each
code with its cause and exit status.

A daemon left by a v0.10.0 preview before schema 6 (pre4 and earlier)
answers the same way: `compatible: false`, stop it with its own executable
or `karvi daemon restart`. The new client uses lifecycle-only compatibility
to stop the older same-UID daemon and then starts the current executable. Job requests are never downgraded, and a normal
`karvi run` never kills or silently replaces an incompatible daemon. Operators
who need a one-shot foreground run before restarting may explicitly use
`karvi run --no-daemon ...`.

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

It renders the job from its beginning as the foreground run would have,
live until the job ends or at once for a job that has ended, ends the
display as a run's ends (the footer from the job's summary in text, the
summary document as the last line under `jsonl`; no result line on
standard error, a `crun` excepted), and exits with the job's own exit (0
completed, 113 cancelled, 106 incomplete). Under `jsonl` the output is the
job's `commands.jsonl` byte for byte, then its summary. The daemon serves the job while it holds it;
a finished job the daemon no longer holds (after a restart, or evicted from
the bounded job table) is read from its directory, found by the job ID
under `output.root`. A directory without a summary whose job no daemon
holds is `job_orphaned`: the daemon that ran it is gone, or the job was
run with `output.files.summary-json` false and has no terminal fact on
disk. A job run with `output.files.commands-jsonl` false is followed
live by the client that started it; a later `job follow` starts at the
edge and says which records are not kept. Ctrl-C stops only
the follow. Neither `job` verb launches a daemon.

## The watch screen

`karvi watch` is a screen to keep open through a maintenance window. It
reads the shared scoreboard directory
(`watch.directory`, `/dev/shm/karvi/scoreboards`) every `watch.refresh`
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

For a script, `--format table` prints the same columns once and
`--format json` every snapshot; the TUI refuses a stdout that is not a
terminal (`watch_tui_requires_terminal`). `--filter TEXT` and `--sort
KEY` start the screen with them and apply the same rule to `--format
table`; `--format json` refuses both (`watch_json_filter_unsupported`),
since it prints whole snapshots for the script to filter itself. Neither
has a configuration key: the filter and the sort are the session's. A test or a script that starts
an activity sets `watch.directory` under its own work directory, as it
sets `basedir`, so the shared directory holds only real jobs; the files a
host already holds are yours to remove (`karvi-prune`).

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

The line is the `display.ping.header` template, rendered and coloured as
the headers are (`<rtt1>`, `<rtt2>`, `<result>`; `docs/DISPLAY-CONFIGURATION.md`);
an empty template turns the line off and `--quiet` suppresses it;
`--debug` adds the ICMP error details and the packet-loss notice. Every
record of a gated device carries a `ping` object (address, method, the two
outcomes with round-trip times, the decision) and `ping_ns`; the job's
`summary.json` carries a `ping` block counting gated, proceeded, degraded,
skipped, and capability-failed devices and the probes sent, replies,
timeouts, and errors. With the gate disabled every such field is null and
no ICMP socket or process is created.

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
passed the checks is the file that is read. A file that is present but
cannot be opened (mode `0000`, say) is reported by its check and is not
treated as absent. These codes replaced the `cloginrc_*` check codes of
v0.10.0 and v0.11.0; `docs/ERROR-CODES.md` lists the retired names.

### The credential CSV

A `csv` backend is the second file backend: device-keyed rows of username,
password, and enable password under the file rules above.
`docs/CREDENTIAL-CSV.md` is its guide and this section only says where to
look in it:

| Task or symptom | `docs/CREDENTIAL-CSV.md` |
|---|---|
| Declaring the backend; `scope` and `path` have no default; the `config validate` codes | section 2 |
| The nine fields, headers and mappings, numeric mode, quoting, verbatim secret cells | section 3 |
| Which row a device takes (top to bottom, first match, no longest prefix); a blank password; `matched_on` for a `csv_row` | section 4 |
| Pinning a device to a row with the inventory's `credkeyref`; `credkeyref_unresolved`, `inventory_credkeyref_invalid` | section 5 |
| Cells that name environment variables, and the unset-variable hazard | section 6 |
| A formula whose password source is a credential CSV; the row's username is dropped | section 7 |
| `inventory_secret_column`, `config_inventory_source_secret_mapping`: an inventory file holds no secrets, there is no override, and what the check does not find | section 9 |
| `credential_csv_malformed`, `credential_csv_row_invalid`, `credential_csv_credkey_duplicate`: the messages, which never quote a cell | section 10 |

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
remains secret-safe and excludes raw command text; capture stdout and stderr
separately when opening a support issue.

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
| `failures-jsonl` | `failures.jsonl` | the records that did not succeed, each also in `commands.jsonl` | the short list of failures |
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
  the word after a bare `--of` is the device. `run --of=PATH` does the
  same for a run on every path: the root travels in the plan, so a job
  through the daemon is written where the invocation said. Both are
  flag-origin values: a site that
  locks either key refuses the option.
- `exercise.json`, the audit log, the scoreboard, the capacity ledger,
  and login transcripts are not the job's output files and have their
  own settings.

## The spool directory

A command's response is held in memory up to `output.spool-threshold-bytes`
(1 MiB by default) and continues into a file, the spool, above it: one
file per command in flight under `spooldir`, removed as soon as the
command's record is written and handed on (`docs/DESIGN.md`, the output
spool). The record is the same whether the response spooled or not.

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

**Free space.** `freecheck` decides what free space every volume an
activity writes must have at admission, before any device is contacted:
the job's folder, a `crun`'s collection directory, `spooldir`, and a
recorded login's transcripts root, read once per volume however many of
them share it. `auto` (the default) asks each volume for the sum of what
its places will hold when the job has finished (the folder
`output.expected-bytes-per-device` per device per file it writes, times
`output.reserve-multiplier`; the collection directory one copy; the spool
the job's width times `output.max-command-bytes`) plus the floor
`output.min-free-bytes-after-job` (2 GiB) once; when the spool's share is
short but at least one command fits, the job runs narrower with the
warning `spool_width_narrowed` on standard error and on the receipt and
the follow start under the daemon; otherwise the job is refused with
`output_preflight_space`, which names the volume's paths and both
figures. `always` asks each volume for the floor alone. `never` skips the
free-space check (the directories are still resolved and probed
writable). The check costs one `stat` per place and one `statfs` per
volume, a few microseconds; it is a guard against writing into a full
disk, not an alert: watch the volumes behind `basedir`, `sharedroot`,
`spooldir`, and `watch.directory` with the site's monitoring, and let
`karvi-prune --minfree` keep them clear (`docs/SCALE.md`). The daemon's
memory for output is the width in flight times the threshold, plus a few
KiB per command; there is no memory key.

**What you see.** The watch screen's detail pane shows the bytes settled
so far beside a running target and the in-flight total on the job's
metrics line, refreshed every `watch.refresh`; the scoreboard is rewritten
at that interval while any activity runs, so a login or a quiet command is
not shown stale. A spool left by a process that died mid-command is
removed at the daemon's next start and at the next job's admission, and
logged `spool_abandoned_removed`; a file in `spooldir` of another shape is
never touched. A followed record whose line is larger than
`daemon.max-ipc-frame-bytes` allows, or one spooled in a job that keeps no
`commands.jsonl`, reaches the follower with its output left out and the
notice `follow_output_omitted` saying where the output is.

## The shared trees

A team keeps one job tree, one collection directory, and one transcript
tree. The site makes them once, as root:

```bash
sudo karvi setup shared            # the group of the operator who ran sudo
sudo karvi setup shared --group netops --mode 2775
```

That creates `/opt/karvi` (0755), under it `shared`, and under that
`jobs`, `crun`, and `transcripts`, the four in the group with group write
and search and the setgid bit (2770, or 2775 with `--mode`), so the trees
carry the shared directory's own group and mode; and beside `shared` the
operators' `users` directory in the same group at 1770 (group write and
search, the sticky bit). Each is reported as `created`, `exists`, or
`repaired`: a real directory with another group or mode is set right and
the line says what it had, so the command is also the way to fix a broken
layout; only a path that is not a real directory (a file, a link) is left
as it is and reported (`setup_directory_mismatch`). Without root the
command is refused before anything is looked at (`setup_requires_root`).

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
`transcript.root` at `auto` default to it in place of the tree under
`basedir`: `sharedroot` (`auto`) consults `/opt/karvi/shared`, then
`/var/lib/karvi/shared`, tree by tree, so a site may share the collections
and not the jobs. Jobs land in `/opt/karvi/shared/jobs/YYMMDD/ID`, the day folder
with the tree's own mode so any member can add a job folder to it, the
job folder at `output.directory-mode` and its files 0640 in the group, so
`karvi job follow ID` reads another operator's job from any member's
shell. A tree the operator cannot create files in is refused, not passed
by (`output_directory_not_writable` and its siblings, the message naming
the group and the setting); `sharedroot = "none"` keeps every tree under
`basedir`, as every test suite sets it. `basedir` itself is never shared:
its `socket` and `state` are one operator's daemon. `job cancel` reaches
only the caller's own daemon, so another operator's job is cancelled by
its owner.

Under the packaged systemd user unit the daemon may write only under the
places its `ReadWritePaths` names: the basedir candidates, the shared
trees under both system roots, the configuration root, and
`/dev/shm/karvi`. A collection directory, a basedir, or a spool directory
elsewhere needs the drop-in below ("The collection run", "Under systemd")
naming it. A run in the client process (`--no-daemon`, `command`, `login`)
needs nothing of the unit.

## Retention

`karvi-prune` removes finished work older than the retention age
(thirty-one days by default) from the trees karvi writes: finished jobs,
ended transcripts, and terminal scoreboard files by their end time; job
folders without a summary and scoreboard files a crashed daemon left at
`running`, by age alone; then the day folders left empty. It never touches
a running job, the collection directory, the audit log, or journald, and
long-term audit retention is the central forwarder's, not the helper's.
`docs/PRUNE.md` holds the detail: what goes and why, every place looked
at in sequence, the report's lines, the exits, and the schedules.

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

## The collection run

`karvi crun` collects one or more commands from a scope into one file per
device, named by the device, in one flat directory: a replacement for
`rancid-run` and the Oxidized collector; `docs/COLLECTION.md` is the guide
to what to collect (the
command lists of both tools per platform, and sub-platform tables per
device model). The daily invocation is one word and a scope:

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
  previous file and leaves nothing behind. The result line ends with
  `collection=DIR replaced=N kept=M`, the summary's `collection` block
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
- **The hook.** `crun.after` names an executable the client runs once the
  collection has ended and the result line is printed, on the in-process
  path and through the daemon alike, never for `--detach`: in the
  collection directory, the replaced files' names on stdin one per line,
  and `KARVI_JOB_ID`, `KARVI_JOB_DIR`, `KARVI_CRUN_DIRECTORY`,
  `KARVI_CRUN_REPLACED`, `KARVI_CRUN_KEPT`, and `KARVI_EXIT` in its
  environment; its output follows the result line on stderr. A hook that
  fails, cannot start, or runs past `crun.after-timeout` (`5m`) is the
  warning `crun_after_failed`, the run's exit code unchanged, and the
  audit holds a `crun.after` event either way. The shipped examples under
  `packaging/crun/` commit the replaced files to a git repository over the
  directory and mail the commit's diff (`docs/COLLECTION.md` section 5).
- **The drop list.** A platform's `crun-filters` (`[platform.NAME]
  crun-filters = ['^Building configuration\.\.\.$', ...]`, regular
  expressions in literal strings) drop the output lines that match from the
  collection file alone: the record and the job's files keep every line, a
  `! COMMAND` marker is never matched, and an emptied block keeps its
  marker. The built-in lists drop the byte count, the NTP clock period, the
  uptime, the free memory, and the time of the show, never the stamp that
  says when the configuration last changed; an array replaces the list
  whole, `[]` turns it off, and an alias inherits its driver's built-in
  list. The lists travel in the plan, so the daemon path drops the same
  lines and the manifest shows them; a pattern that does not compile
  refuses the configuration (`config_platform_crun_filter_invalid`).
  `docs/COLLECTION.md` section 6 has the lists per platform.
- **The schedule.** A recurring collection is the site's systemd timer or
  cron over `karvi crun --all --no-daemon`, not a configuration key or a
  daemon-resident schedule: `packaging/systemd/user/karvi-crun.service` and
  `karvi-crun.timer` (a oneshot, one instance at a time, a missed tick run
  at the next start), and `packaging/cron/karvi-crun`, which holds a lock
  for the run and skips a tick that finds it held (one line, exit 75). Two
  collections never overlap in one directory that way; karvi itself does
  not refuse one, so an operator's single-device `crun` during the nightly
  run is not turned away. Under a timer the daemon is not launched: a
  oneshot's end would terminate it. `docs/COLLECTION.md` section 7.

## Stream mode

`karvi stream`, or `karvi -`, composes a run from standard input, one
line at a time. Typed at a terminal it is
a session: set the targets once, then send batches of commands, each
`--go` a job with its own folder, display, and footer. Piped from a script
or a heredoc it drives several jobs through one karvi. The rules:

- A line is skipped when blank or when its first character is `!` or
  `#`. A line beginning with `--` is one run option: the word, then its
  value as the rest of the line after a space or `=`, so `--target
  router1`, `--target=router1`, `--tl router1 router2`, `--dispatch
  parallel`, `--dp`, `--format jsonl`, `--no-daemon`. Any other line is
  one command, sent as written; `\r` at its end is read as `--cmd` reads
  it, and a line of `\r` alone sends a blank line. The draft has two
  parts: the targets and options, which stay from one job to the next,
  and the commands, which are the job's. A `--cmd`, `--command`, or
  `--cf` line is a command like a bare line, and `--expect`, `--blind`,
  and `--blind-return` lines attach to the command before them; the
  option words mean what they mean on a `run` command line, abbreviations
  and the `=` spelling included.
- `--go` or `--sendit` executes the draft; the targets and options stay,
  the commands clear; with no command to send it prints a notice and runs
  nothing. `--clear` empties the commands and keeps the targets and
  options. `--reset` empties the draft. `--end`, `--quit`, the input's
  end (Ctrl-D), or Ctrl-C leave without executing, and a Ctrl-C outranks
  lines already read. Only `--go` and `--sendit` execute.
- Typed at a terminal, a line is edited before Enter sends it: Ctrl-A and
  Ctrl-E to the line's ends, Ctrl-K, Ctrl-U, and Ctrl-W to cut, the left
  and right arrows to move, and the up and down arrows through the lines
  typed so far. The editing echoes on the controlling terminal, so
  `karvi stream > out` still shows what is typed. Piped input has no
  editing to do.
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

Treat `commands.jsonl`, `failures.jsonl`, `summary.json`, `manifest.json`, audit
records, and scoreboards as authoritative. Human headers, borders, and footers
are projections only.
