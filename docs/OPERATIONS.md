<img width="1672" height="941" alt="A Viking keeping watch over the network racks of a datacenter" src="../images/viking-guardian-of-the-datacenter.png" />

# Operations guide

## Upgrade

An operator's own daemon, the one the operator's client launches
(`daemon.launch = "client"`, the default), needs nothing run after an upgrade.
A daemon serves a client only when its IPC schema and its build, the Go build ID
of its executable, equal the client's. The next submission (`run`, `crun`,
`--exercise`, `--detach`, a stream's job, or `daemon start`) drains a daemon of
another build. Holding no job, it exits at once, and the client starts one of
its own build in the same invocation:

```
daemon drained version=0.34.1 build=fNsX8VA8zKCBF5ctuqyS
daemon started socket=/home/netops/.local/share/karvi/socket/d.sock
```

Holding jobs, the drained daemon finishes them and exits after the last,
killing nothing. Until then every submission is refused, the daemon's build
over the client's:

```
daemon_draining: 1 job running; none submitted; retry when it exits, or --no-daemon
  daemon build fNsX8VA8zKCBF5ctuqyS 0.34.1
  client build onQ7svRY49FAEYS1IIHv 0.35.0
```

Meanwhile `karvi run --no-daemon` runs in the foreground, and `karvi job
follow` and `karvi job cancel` reach the draining daemon's jobs, as a client of
any build does at the same IPC schema. `karvi daemon status` stacks each of the
daemon's values over the client's, `daemon_version:` over `client_version:`,
then the commit, the IPC schema, and the build, above `compatible:`; `--format
json` takes the same keys.

To end the daemon rather than wait for the next submission:

```bash
karvi daemon stop --grace
```

`--grace` drains the daemon and waits for it to exit; `--after=DURATION` drains
and waits up to the limit, then forces; `--force` stops it at once, forced work
still receiving terminal records. A wait begins with a line naming the jobs
(`daemon draining: 1 job running; waiting for it to exit`), which `--quiet`
leaves out; interrupted, the wait ends and the daemon still finishes its jobs
and exits. With no option, `daemon stop` and `daemon restart` are refused while
the daemon runs a job, which it alone counts.

An older karvi beside a newer daemon is refused, `daemon_incompatible`, the
schemas stacked: the older client cannot speak the newer schema, so it neither
drains nor stops that daemon. A downgrade is the administrator's, who ends the
operators' running daemons or restarts the host; an idle daemon exits by itself
after `daemon.shutdown-idle-timer` (below).

A service account's daemon under systemd is never drained by a client; its
upgrade is the administrator's restart of its unit (see "A service account's
daemon under systemd", below).

Error messages begin with a registered code;
[`docs/ERROR-CODES.md`](ERROR-CODES.md) lists each code with its cause and exit
status.

## Rehearsal, detach, and follow

`karvi run --dry-run ...` plans on the client and probes the daemon without
launching one: it prints the inspection report (text, or `--format json`
or `jsonl`) and submits nothing. `karvi run --exercise ...` submits the
plan and package and has the daemon validate everything but the device: the
report lands in the job directory as `exercise.json` and on stdout, the
summary reads `exercise`, and the exit is 0 when every target is `ready`
or the first finding's exit otherwise. Neither mode sends ICMP, opens a
session, invokes askpass, or runs a command. The two flags exclude each
other, neither combines with `--detach`, and `--exercise` needs the daemon.

A live run renders records as the daemon makes them durable. `--detach`
prints the job ID and artifact directory and returns at acceptance (exit 0
means accepted; follow the job with `karvi job follow`, watch it with
`karvi watch`, or find it with `karvi job list` and read its `summary.json`
when it ends). `--follow=false` waits for the result without rendering.
Ctrl-C during a run stops the client's follow only: it names the job that
continues and its artifact directory and exits 113; the daemon finishes the
job and writes its records. A closed stdout does the same with exit 111.

To stop one accepted job without stopping the daemon:

```bash
karvi job cancel JOB-ID --reason 'wrong change window'
```

The job ID is the one `run --detach` printed, or `karvi job list` or `karvi
watch` shows. The daemon answers at once; the job's unfinished work is recorded
`cancel` and its summary ends `cancel` at exit 113 with the request's
time, reason, and requester in a `cancellation` block. `--follow` waits for the
job's end and exits with the job's exit. A job that already ended is reported
with its outcome, an unknown job ID is `job_unknown`, and the command never
launches a daemon. Other jobs are untouched; a foreground `run` whose job is
cancelled from elsewhere prints the cancelled line and exits 113.
A cancel that arrives too late changes nothing: a job whose work had
finished keeps its result and its exit, and an exercise (`run --exercise`)
never ends `cancel`, since its checks are local and take milliseconds.
`job cancel` may still answer `cancelling` for such a job; its summary then
carries the `cancellation` block under its own final status (`exercise`,
`complete`), the audit trail holds the `<kind>.cancel` event, and no
cancelled line is printed.
To see a detached or interrupted job again, from any later invocation:

```bash
karvi job follow JOB-ID [--format text|jsonl|json] [--echo] [--border|--noborder]
```

It renders the job from its beginning as the foreground run would have, live
until the job ends or at once for a job that has ended, ends the display as a
run's ends (the footer from the job's summary in text, the summary document as
the last line under `jsonl`, a collection's line after the footer in text; no
result line on standard error), and exits with the job's own exit (0 for
`complete`, 113 for `cancel`, 106 for `incomplete`). Under `jsonl` the output is
the job's `commands.jsonl` byte for byte, then its summary. The operator's
daemon serves the job while it holds it. Any other job is followed from its
directory, found by the job ID under `output.root`: a finished job the daemon no
longer holds (after a restart, or evicted from the bounded job table), a `run
--no-daemon` of another terminal, and in the shared tree another member's or the
service account's job. Each record of `commands.jsonl` is rendered as it is
written until the summary names a final status, and the display ends from it.
While the summary is not final, the job's scoreboard says whether it runs: one
that is gone, or stale by `watch.stale-after`, or that names the job's end that
long ago, is `job_orphaned`, the process that ran it gone. A site that turns
`watch.enabled` off has no scoreboard, so another member's live job is
`job_orphaned` to a follow; each operator still follows their own through their
daemon. A job run with `output.files.commands-jsonl` false is followed live by
the client that started it; a later `job follow` through the daemon starts at
the edge and says which records are not kept, and one from the directory says
the job's records are not kept and shows its end alone. Ctrl-C stops only the
follow. Neither `job` verb launches a daemon.

`karvi-run(1)` (`man karvi run`), REHEARSAL, restates the dry run, the
exercise, `--detach`, and Ctrl-C for the terminal; a change to one changes
both.

## The list of jobs

`karvi job list` lists the jobs of the output root from the summaries their
folders hold; it needs no daemon or credential and makes nothing. In shared
mode it reads the shared jobs tree, every operator's job and a service
account's; in individual mode the operator's own. With no option it lists the
newest 20 jobs by job ID, whatever their day, a running or unfinished job
among them:

```text
$ karvi job list
  JOB-ID            TIME      STATUS      OPERATOR  MODE    DONE   FAIL  TARGET
  261017-131827-00            unfinished  netops    run     u/1       u  fake-iosxe
  261017-131826-00  0m00s     exercise    netops    run     1/1       0  fake-iosxe
  261017-131825-00  0m01s     complete    netops    crun    1/1       0  fake-iosxe
  261017-131823-00  0m01s     error       netops    run     2/2       1  fake-iosxe dead-iosxe
  261017-131821-00  0m01s     complete    netops    cmd     1/1       0  fake-iosxe
$ karvi job list --target 'dead*'
  JOB-ID            TIME      OPERATOR  MODE  OUTCOME      DEVICE
  261017-131823-00  0m01s     netops    run   fail         dead-iosxe
```

The columns are the watch screen's without ACTV. TIME is the job's duration,
its start being its ID; OPERATOR is the owner of the job's summary, the UID
where the user database names none; MODE is the kind of job, `cmd`, `run`, or
`crun`, a job of `stream` being `run` and an exercise keeping its kind, its
STATUS `exercise`. At a terminal the line fits the width: TARGET takes what
the other columns leave, each device named whole while it fits and the rest
counted as `+N`, so at 80 columns the rows above end `+1` and `+2`; to a pipe
or a file nothing is cut. The rows stand in job-ID order, newest first. A job
ID names its start in the effective timezone, so a job run under another
`--timezone` stands by its ID.

An unfinished job, running or cut short, shows STATUS `unfinished`, TIME
blank, and `u`, unknown, in FAIL and in DONE over its devices (`u/1`), in the
warning colour. A folder that holds no summary reached no device: it shows `u`
in MODE and `u/u` in DONE, TARGET blank. A folder whose summary cannot be read
or decoded, and a day folder that cannot be read, is a `?` row, STATUS
`invalid`, its path in TARGET, or `…` at a terminal where the path does not
fit; `--format json` and `karvi job follow ID` name the path and the reason. A
list with no row prints `(no jobs)`, exit 0.

The filters, each judging a job before `--last` counts it:

- `--last N` lists N rows, `0` every job kept.
- `--failed` keeps the finished jobs whose exit is not 0, a partial failure
  and a cancellation among them.
- `--target NAME` keeps the jobs that reached a matching device and lists one
  row per such device, OUTCOME the device's own (`unfinished` while the job
  has not ended). NAME is taken as `run`'s `--target` takes it: a glob,
  repeatable, a value beginning with `!` removing the devices it matches. A
  line of `!` values alone is refused, `inventory_positive_selector_missing`
  (exit 5).
- `--operator NAME`, repeatable, keeps that operator's jobs, by name or UID.

`--operator` judges a `?` row by the owner of what could not be read;
`--failed` and `--target` keep it, since it may hide a match. `--format json`
prints one array of the rows, each the job's summary fields as the file holds
them, then `path`, the job's folder, and `operator`, `{"username", "uid"}`;
under `--target` the row adds `device` and its `outcome`, and a `?` row is
`{"path", "operator", "error"}`.

The job ID is the one `job follow` and `job cancel` take, and the folder of a
job that ended `error` holds the `failed-devices.txt` a rerun takes ("The job's
output files"). A job run under `--nof` or `output.persist-command = false`
makes no folder, and the list does not show it.

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

`karvi watch` is a screen to keep open through a maintenance window. It reads
the scoreboards (`scoreboards`: `/dev/shm/karvi/scoreboards` where the site made
the scratch root, and the operator's own `<basedir>/state/scoreboards`, every
one that exists, making nothing; "The shared trees") every `watch.refresh`
(`2s`) and shows one row per job: `JOB-ID`, `TIME` (the running duration, then
the end time), `STATUS`, `OPERATOR`, `MODE` (`login`, `cmd`, `run`, `crun`,
`exercise`), `DONE` as `done/total`, `FAIL`, `ACTV` (devices in flight), and
`TARGET` (a target file's name, then the devices in flight, then the rest), with
a line under the headings. Running jobs stand above a rule, sorted by start;
finished jobs below it, newest first; the footer counts what is not shown. The
line under the headings and the rule never leave the screen: the running rows
are pinned between them and only the finished rows scroll, so a sort or a long
list never takes a running job out of view. With no running job the two lines
stand together: an empty band says at once that nothing is in progress. A
running job whose snapshot has not moved for `watch.stale-after` (`10s`) is
flagged `!` and dimmed, its status word unchanged; a file the screen cannot read
is flagged `?` with `invalid` as its status and its path as its target. The
clock at the top right is the time of the last refresh. Nothing in the screen
names a command: the statements are read in the job's folder.

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
`scoreboards` under its own work directory, as it sets `basedir`, so the
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
! core-nyc-01 [2001:db8:10::1] ping(1) 2.1ms, ping(2) timeout, proceed
! edge-b [192.0.2.1] ping(1) timeout, ping(2) error, skip
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

## The daemon and the job's configuration

A job runs under the configuration of the invocation that submitted it,
through a daemon or not: the client sends its resolved values in the plan, and
the daemon builds the job's configuration from them and reads none of its own
for the job. A later client's `--set`, `--config`, `KARVI__*` variables, and
working directory reach its own job; a relative path in any of them is made
absolute against the client's working directory when it loads, so `karvi config
show` prints the path the job records. Two clients through one daemon, each
naming its own trust store:

```text
$ karvi --set ssh.known-hosts-file=kh1/known_hosts run --target 127.0.0.1 --platform linux --cmd 'echo 1'
daemon started socket=…/socket/d.sock
! ssh accepted new host-key 127.0.0.1 (ED25519)
1
$ karvi --set ssh.known-hosts-file=kh2/known_hosts run --target 127.0.0.1 --platform linux --cmd 'echo 2'
! ssh accepted new host-key 127.0.0.1 (ED25519)
2
```

The daemon's own configuration is the launching invocation's (its `--config`,
`--set`, and `KARVI__*` variables) and serves the process alone: the `daemon.*`
keys, the `basedir` of its socket, state, and log, and the sweeps at its start.
`karvi config show --explain` prints `reload: daemon-start` for the keys a
running daemon keeps from its start (`daemon.max-accepted-jobs`,
`daemon.shutdown-idle-timer`, `daemon.shutdown-grace-seconds`, and the daemon's
side of `daemon.max-ipc-frame-bytes` and `daemon.forced-grace-seconds`); a new
value of one reaches the daemon at its next start (`karvi daemon restart`). A
site's locks are enforced by each client's load; the daemon checks no lock of
its own. A job's audit events, the `cancel` among them, go where its
configuration names.

Every invocation reads its configuration once, its files, its environment,
and its options together, and every stage of it, a `crun`'s hook among them,
runs under that read; an edit made while a job runs reaches the next
invocation. A `stream` reads as it starts and runs every job under that
reading with the job's own option lines, so an edit reaches the next stream, or
the jobs after a `--restart` line, which reads the configuration again.
The read's warnings, an `@include?` file missing or a `KARVI__` variable
ignored under `config.reject-unknown-env = false`, are said once on standard
error, in the warning colour at a terminal, under `--quiet` and any format too;
a run through a daemon says them in its client. The daemon's own read writes
them into `daemon.log` at its start:

```text
level=WARN msg="configuration warning" warning="ignored unknown environment variable KARVI__NOPE"
```

## The daemon's idle exit

A daemon a client launched is one process per operator and per configuration
(`basedir`), and it outlives the terminal and the login that started it. On a
host used by a team, that is one daemon per operator, each holding its
start-time executable and `daemon.*` settings, for as long as no one stops it.
`daemon.shutdown-idle-timer` (`"10m"` by default) ends that: after that long
with no active job, no preparation in progress, and no request but `ping` and
`status`, the daemon stops itself exactly as `daemon stop` on an idle daemon
would, exit 0, with one line in `logs/daemon.log` (`stopping: idle`, the idle
time, the key and its value). The check runs once a minute, so the exit comes
within a minute of the timer. The next `run` finds no daemon and launches one
(some 80 ms; it prints `daemon started` unless `--quiet`) with the executable
and the `daemon.*` settings of that day. `0` never exits; otherwise the value is
1m to 720h. Measured: an idle daemon holds 19 MB and 9 threads fresh, 43 MB some
minutes after a 76 MB job, and no connection to any device.

`status` and `ping` keep no daemon alive, so a monitor may poll as often
as it likes. Every other request, and a job's end, restarts the clock.
The client probes the daemon just before its first request and repeats
that request once, with the plan it already made, if the daemon left in
between; an operator sees at most a `daemon started` line.

The service account's unit, `karvi-daemon@.service` (below), starts the daemon
with `--set daemon.shutdown-idle-timer=0`, because an idle exit is a clean exit
that `Restart=on-failure` would not restart. The timer is not what ends a daemon
at logout: a daemon a client launched runs in the login session's scope, and a
host whose logind has `KillUserProcesses=yes` ends every process of the session
at logout, a running job included, with or without the timer; a shutdown stops
the scope at once, the daemon's OpenSSH sessions with it.

`karvi-daemon(1)` (`man karvi daemon`), FILES, restates the daemon's
places, its filtered environment, and the idle exit for the terminal; a
change to one changes both.

## A service account's daemon under systemd

A site chooses how karvi runs: each operator's own daemon on the operator's own
trees; the same with the site's shared trees (`sudo karvi setup shared`); or a
service account's daemon under systemd. Neither the package nor an install from
the bundle sets any of them up. On a service platform the daemon is a service
account's, `karvi`, run by systemd and administered centrally, where on a jump
box each operator's client launches the operator's own. Only the daemon's own
account reaches it, its socket's directory 0700 and the daemon checking each
peer's uid, so the jobs it runs are the account's: its timers and its
automation, run as `karvi`.

The package installs the template unit
`/usr/lib/systemd/system/karvi-daemon@.service`, and the bundle's `sudo
./scripts/install.sh` the same under `/usr/local/lib/systemd/system`, with
`karvi-sysutil` in `/usr/local/sbin`; neither runs anything. The template names
`karvi` bare, which systemd finds in `/usr/local/bin` before `/usr/bin`. The
administrator sets the account up once, as root, under either install:

```bash
sudo karvi-sysutil daemon install
```

It runs `karvi setup shared` (the shared trees, in the operators' group,
`netops` unless `--group NAME` names another); makes the system user `karvi`,
its group, and its home `/opt/karvi/users/karvi` (0750), with no login shell;
adds it to the operators' group; writes its configuration,
`/opt/karvi/users/karvi/.config/karvi/config.toml` (0600), with `[daemon]
launch = "systemd"`; and enables and starts `karvi-daemon@karvi.service`,
checking `karvi daemon status` as the account. Each part is entered in the
record `/etc/karvi/service-karvi.installed` as it is made or found. Run again,
it changes nothing over a whole install and repairs what has drifted from what
it made; `karvi-sysutil(8)` lists every step and refusal. The account is a
system user, so logind's `RemoveIPC` leaves its files under `/dev/shm` alone,
and it needs no user manager and no `loginctl enable-linger`.

Under `daemon.launch = "systemd"` the account's client neither drains nor
starts a daemon:

- a daemon of its own build, not draining, takes the submission;
- a daemon of another build, or of a newer IPC schema, refuses it,
  `daemon_incompatible`, naming `systemctl restart karvi-daemon@karvi.service`;
- with no daemon, a submission and `daemon start` are refused,
  `daemon_unreachable`, naming `systemctl start` of the unit;
- a draining daemon, stopping under its unit, refuses it, `daemon_draining`;
- `daemon stop` and `daemon restart` are refused, `daemon_managed` (exit 112),
  naming `systemctl stop` or `restart` of the unit;
- `daemon status`'s `remediation:` names the unit's restart.

The unit starts the daemon after the network, so a shutdown stops it before the
network goes. A stop, a restart, or a shutdown signals the daemon alone
(`KillMode=mixed`): it admits no job, lets its jobs finish within
`daemon.shutdown-grace-seconds` (60 by default), cancels and records what
remains, and ends its own sessions; systemd kills what is left once it has
exited. `TimeoutStopSec=infinity` leaves that bound to the daemon's grace keys,
so a site that raises the grace changes no unit. `Restart=on-failure` restarts
a daemon that fails, and the idle timer is off (above). A site's own settings
go in a drop-in under `/etc/systemd/system/karvi-daemon@.service.d/`, which an
upgrade never touches.

**After an upgrade**, and after a rollback (`apt install --allow-downgrades`,
or an earlier bundle's `install.sh`, [`BUILD-HOWTO.md` section
10](../BUILD-HOWTO.md#10-install-and-roll-back)), the template has been
replaced and systemd reloaded, and the daemon still runs the program it
started with. The administrator's one step:

```bash
sudo systemctl restart karvi-daemon@karvi.service
```

The restart drains the old daemon under its grace and starts the installed
karvi. Until then the account's submissions are refused, `daemon_incompatible`,
the builds stacked.

**Removal.** `sudo karvi-sysutil daemon remove` disables and stops the
instance, the stop waiting for its jobs as at a shutdown, and removes the
configuration file and the group membership it made. The account and its home
stay, so that their files keep their owner, and a later `daemon install` takes
the account back. `--purge` also gives every file the account owns in the
shared trees and the shared scratch to root, their group and mode unchanged,
and removes the account, its home, its spool directories, and the record. The
shared trees and the template unit stay.

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

## Configuration macros

A `[macros]` table names values that any other value of the configuration takes
as `%NAME%`, the name a letter and then up to 63 letters, digits, `_`, and `-`.
The layers are merged first, so a macro one file defines reaches the values of
every other, and `--set 'macros.region="bos"'`, a TOML value, replaces one for
an invocation. A macro is filled when the configuration loads: `config show`,
the configuration digest, and the plan hold the filled values, and `config show
--explain` names the macros a value took:

```toml
[macros]
region = "nyc"
core = ["show version", "show inventory"]

[crun]
directory = "/srv/karvi/%region%/%operator%"

[platform.c9300]
driver = "cisco_iosxe"
crun-commands = ["%core%", "show running-config"]
```

```text
$ karvi config show --explain crun.directory | grep 'key:\|value:\|macros:'
key:        crun.directory
value:      "/srv/karvi/nyc/netops"
macros:      region, operator
$ karvi config show platform.c9300.crun-commands | grep 'value:'
value:      ["show version", "show inventory", "show running-config"]
```

- **A value that is the macro alone takes the macro's kind**, a list or a table
  as well as a string, and a list macro inside a list is spliced into it, as
  `%core%` above. Inside text a macro must be a string, a number, or a boolean.
- **`%operator%` is built in**: the operator's login name from the password
  database, filled in every value as a site's macro is. `[macros] operator` is
  refused, and `%Operator%` is an ordinary macro.
- **Seven keys keep `%operator%` as written**, and the backend fills it when it
  is asked, with the operator's name after the backend's `operator` transform
  ([the credential
  transforms](#the-operators-name-and-the-credential-transforms)): the env
  backend's three templates, Redis's `key-template`, Vault's `path-template`,
  and the `username-template` of a formula and of a policy. The key being filled
  decides, so a site's macro holding `%operator%` keeps it there too. Every
  `%operator%` in such a key is replaced as text, and a template without one is
  used as written: `karvi:shared` is one record for every operator. `%s` is
  text: a Redis key or a Vault path holding it finds no record, a formula sends
  it, and an env template holding it names no variable and is refused.
- **A store's answer is never expanded.** A username, password, or enable
  password from a credential CSV, a `.cloginrc`, or a Redis or Vault record is
  sent as written, `%operator%` and `%HOME%` included.

Refused when the configuration loads, `config validate` included (exit 2):

| Code | The value |
|---|---|
| `config_macro_undefined` | names a macro no layer defines |
| `config_macro_cycle` | names macros that name each other in a cycle |
| `config_macro_scalar_type` | has a list or table macro inside text |
| `config_macro_name_reserved` | is `[macros] operator` |

```text
config_macro_undefined: undefined macro %site% for crun.directory at FILE:2
```

`%operator%` with no name known for the operator is
`operator_identity_unavailable`.

## The platform's fallback and the operator's keys

When no backend of the policy answers for a device, its platform's
`fallback` says what follows, in order: `netvars` reads `NETUSER`, `NETPASS`,
and `NETENABLE` (the credential helper's values where it set them, else the
process environment's: [the credential helper](#the-credential-helper)); `keys`
is the operator's login name and own keys; `prompt` asks at the terminal ([the
credential prompts](#the-credential-prompts)).
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
never authenticated), and every `<kind>.command` audit event names it in
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
the client, before the job is planned, the credential helper's values where it
set them ([the credential helper](#the-credential-helper)). Three optional
templates name the variables, one per field:

```toml
[credential-backend.operator-env]
type = "env"
username-var-template = "KARVI_%operator%_USERNAME"
password-var-template = "KARVI_%operator%_PASSWORD"
enable-var-template = "KARVI_%operator%_ENABLE_PASSWORD"
```

- **`%operator%` is the operator's login name**, as the account has it, case
  kept, after the backend's `transform` ([the credential
  transforms](#the-operators-name-and-the-credential-transforms)): for the
  operator `netops` the templates above read `KARVI_netops_USERNAME`,
  `KARVI_netops_PASSWORD`, and `KARVI_netops_ENABLE_PASSWORD`. The templates
  keep it as written ([configuration macros](#configuration-macros)), so
  `config show` prints `KARVI_%operator%_PASSWORD`. A template without
  `%operator%` is the variable's name as written (`SRV_USER`,
  `enable-var-template = "NETENABLE"`); a template left out, or `""`, reads
  nothing for its field.
- **A template names a variable.** Each template, with `%operator%` stood in by
  a letter, must be a portable variable name, letters, digits, and underscores,
  not beginning with a digit; anything else is refused when the configuration
  loads, `config validate` included, with
  `config_credential_backend_variable_invalid` (exit 2), `%s` among it:

  ```text
  config_credential_backend_variable_invalid: "KARVI-%operator%-PASSWORD" not a variable name (letters, digits, underscores; no leading digit) for credential-backend.e.password-var-template at FILE:15
  ```

  An operator's name that makes no variable name, after the transform, stops
  the device at planning under the same code, naming the remedy:

  ```text
  config_credential_backend_variable_invalid: client planning: 1 of 1 targets failed credential resolution: name:d1 (credential backend e: password-var-template gives "KARVI_netops.adm_PASSWORD", not a variable name (letters, digits, underscores; no leading digit); remedy: replace-chars in the backend's operator transform policy=default backend=e)
  ```
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
- **A variable's value is the value.** `env:X` in a variable is the text
  `env:X`; a template names the variable to read. `env-indirection.username`,
  `.password`, and `.enable-password` are refused as removed,
  `config_key_removed` (exit 2).
- **Asked once.** The backend asks the same of every device, so it is asked
  once per invocation, or per job of a stream, and its answer held for every
  device the policies send to it.
- **Not keyed.** A device pinned by `credkeyref` skips the backend
  ([`docs/CREDENTIAL-CSV.md`, section
  5](CREDENTIAL-CSV.md#5-keys-and-pins-credkey-and-credkeyref)). The record's
  `matched_on` names the operator, not the device.

The fallback's `NETUSER`, `NETPASS`, and `NETENABLE` are read without a
backend, after the policy's backends, where the platform's fallback holds
`netvars` ([the platform's fallback](#the-platforms-fallback-and-the-operators-keys));
an `env` backend is how a site names variables of its own.

## The Vault backend

A backend of `type = "vault"` reads the operator's record from a Vault KV
version 2 engine, in the client before the job is planned, with a token from a
variable. It is asked only for a device whose policy's `backend-sequence` names
it:

```toml
[credential-backend.vault]
type = "vault"
address = "https://vault.example.net:8200"
# mount = "secret"
# path-template = "karvi/%operator%"
# token-env = "VAULT_TOKEN"

[credential-policy.default]
backend-sequence = ["vault"]
```

- **The keys.** `address` is required, an `http://` or `https://` URL with a
  host. `mount` (default `secret`) and `path-template` (default
  `karvi/%operator%`) make the read `GET <address>/v1/<mount>/data/<path>`,
  `%operator%` filled with the operator's name after the backend's transform;
  `namespace` is sent as `X-Vault-Namespace`; `username-field`,
  `password-field`, and `enable-field` (defaults `username`, `password`, and
  `enable_password`) name the record's fields; `connect-timeout` (default `5s`)
  bounds the read. `ca-file`, `client-cert-file`, `client-key-file`, and
  `tls-verify` are the TLS settings ([TLS to a store](#tls-to-a-store)).
- **The token** is the value of the variable `token-env` names (default
  `VAULT_TOKEN`), the credential helper's where it set it. It is read when a
  device's policy reaches the backend, and never by `config validate`; unset or
  empty, the device stops at planning before any connection, and the run with
  it:

  ```text
  credential_env_missing: client planning: 1 of 1 targets failed credential resolution: name:d1 (credential backend v: token-env: VAULT_TOKEN not set policy=default backend=v)
  ```

  A token the store refuses is `vault_http_error` (`Vault returned HTTP 403`)
  and a store that does not answer `vault_unavailable`, each exit 6.
- **The record** gives the username, the password, and the enable password. A
  field whose value, trimmed, begins with `env:` (the prefix's case ignored)
  names a variable, read as the token is: `enable_password = "env:NETENABLE"`
  takes `NETENABLE`'s value, and an unset or empty one stops the device, naming
  the record, the field, and the variable:

  ```text
  credential_env_missing: client planning: 1 of 1 targets failed credential resolution: name:d1 (credential backend v: v1/secret/data/karvi/netops enable_password: NETENABLE not set policy=default backend=v)
  ```

  A rest that is no variable name is the record's form wrong,
  `vault_response_malformed`, naming the field and never the value. Any other
  value is a literal, so a password that begins with `env:` lives in a variable
  and is named. The credential is judged as any backend's: a field the device
  needs and the record left blank is that field's missing code, a blank
  username `credential_incomplete`.
- **No record** at the path, or one holding none of the three fields, is not
  found: the device goes on to the policy's next backend, then the platform's
  fallback.
- **Asked once.** The read is the operator's, the same for every device, so the
  backend is asked once per invocation, or per job of a stream, and its answer
  held for every device the policies send to it: forty devices made one `GET`,
  and a store that does not answer costs one timeout, every device given the
  failure. A password rotated in the store reaches a stream's next job.
- **`matched_on.source`** is the path read, in each command record, the grant,
  and `--debug`:

  ```text
  DEBUG device credential target="d1" device_user="netops" backend="v" policy="default" matched_category="operator" matched_pattern="" matched_source="v1/secret/data/karvi/netops" credential_id=…
  ```

## The Redis backend

A backend of `type = "redis"` reads the operator's record, a hash, from a Redis
server (`AUTH` when a password is configured, `SELECT` for a database other
than 0, then `HGETALL`), in the client before the job is planned. It is asked
only for a device whose policy's `backend-sequence` names it:

```toml
[credential-backend.redis]
type = "redis"
address = "redis.example.net:6380"
auth-password-env = "REDIS_PASS"
# key-template = "karvi:credential:%operator%"
```

- **The keys.** `address` is required, `host:port`. `key-template` (default
  `karvi:credential:%operator%`) is the hash read, `%operator%` filled with the
  operator's name after the backend's transform; `database` (default 0);
  `connect-timeout` (default `2s`). `tls` (default `true`), `ca-file`,
  `client-cert-file`, `client-key-file`, and `tls-verify` are the TLS settings
  ([TLS to a store](#tls-to-a-store)).
- **The login.** A server that asks for a password takes `auth-password-env`,
  the variable holding it, and one with users `auth-username-env` beside it;
  `auth-username-env` alone is refused at load. Each is read as Vault's token
  is, the credential helper's value where it set it, and unset or empty stops
  the device before any connection:

  ```text
  credential_env_missing: client planning: 1 of 1 targets failed credential resolution: name:d1 (credential backend r: auth-password-env: REDIS_PASS not set policy=default backend=r)
  ```

  A password the server refuses is `redis_auth_failed`, exit 6. With neither
  key, no `AUTH` is sent.
- **The record**'s fields are `username`, `password`, and `enable_password`,
  each a literal or an `env:NAME` reference as Vault's are; a bad reference is
  `redis_malformed`. No hash at the key, or one holding none of the three
  fields, is not found.
- **Asked once**, as Vault is: three devices made one `AUTH` and one `HGETALL`.
- **`matched_on.source`** is the key read, `karvi:credential:netops` for the
  operator `netops` under the default template, or the template as written
  when it holds no `%operator%`.

## A store backend's keys at load

The env, Redis, and Vault backends' keys are checked when the configuration
loads, `config validate` included, each refusal exit 2 and naming the key and
its line:

- **Kinds.** Every key is a string but Redis's `tls` and both types'
  `tls-verify` (booleans), Redis's `database` (an integer of 0 or more), and
  `connect-timeout` (a duration above zero): `config_type_error`,
  `config_duration_error`, `config_duration_negative`, or
  `config_value_out_of_range`.
- **The address**, missing or malformed:
  `config_credential_backend_address_invalid`.
- **A variable's name.** `token-env`, `auth-username-env`, and
  `auth-password-env` must be portable variable names:
  `config_credential_backend_variable_invalid`, quoting the value.
- **A key the other keys leave unread**,
  `config_credential_backend_key_unsupported`:
  `auth-username-env` without `auth-password-env`; a client certificate without
  its key, or the reverse; Redis's `ca-file`, `client-cert-file`,
  `client-key-file`, and `tls-verify` under `tls = false`; Vault's with an
  `http://` address; `ca-file` under `tls-verify = false`.
- **An empty value.** `""` is none where a key's default is none (the env
  templates, `ca-file`, the client pair, Redis's two login variables, Vault's
  `namespace`), the same as the key left out, so a later layer or `--set`
  removes an earlier one's value with it. Where the default is a value (Vault's
  `mount`, `path-template`, and three field names, Redis's `key-template`,
  `transform` on the three types), `""` is refused, `config_type_error`, `must
  not be empty`: write the default to restore it.

```text
config_credential_backend_key_unsupported: ca-file read only under tls = true for credential-backend.r.ca-file at FILE:17
config_credential_backend_address_invalid: not an http:// or https:// URL with a host for credential-backend.v.address at FILE:15
config_value_out_of_range: must be above 0 for credential-backend.v.connect-timeout at FILE:16
```

Whether a named file exists is a question of the run: `config validate` opens no
credential file.

## TLS to a store

Redis speaks TLS unless `tls = false`, and Vault for an `https://` address. The
server's name is checked against the address, TLS 1.2 is the least, and what
the client trusts is the backend's:

- **`tls-verify = true`**, the default, verifies the server's certificate chain
  and its name: to a CA in `ca-file` when one is named, the file then the
  backend's whole trust, or else to the host's trust, the public roots and the
  CAs the site installed. A server the trust does not vouch for is refused
  before anything is sent:

  ```text
  vault_unavailable: client planning: 1 of 1 targets failed credential resolution: name:d1 (Get "https://127.0.0.1:8202/v1/secret/data/karvi/netops": tls: failed to verify certificate: x509: certificate signed by unknown authority policy=default backend=v)
  ```

- **`tls-verify = false`** checks neither the chain nor the name; the
  connection is still encrypted, but a host on the path can pose as the store
  and receive its token or password. `ca-file` beside it is refused at load.
- **A client certificate** is optional: given `client-cert-file` and
  `client-key-file`, a pair, karvi presents the certificate, with every further
  certificate in the file as its chain, to a store that asks for one. Neither
  file's permissions are checked, since a site's machine certificate may serve
  here; restrict them as the site installs them. A file karvi cannot read is
  `redis_tls_config_invalid` or `vault_tls`.
- **Plain text**, Redis's `tls = false` or a Vault `http://` address, is the
  site's choice, said nowhere; the token or password then crosses the network
  as written.

**A self-signed store** is trusted by naming its own certificate in `ca-file`,
`tls-verify` left `true`. Best, copy the certificate file from the store's
host. Else collect it from the store and compare its SHA-256 fingerprint with
the one read on the store's host, before naming it:

```sh
openssl s_client -connect vault.example.net:8200 </dev/null 2>/dev/null | openssl x509 -out ~/.karvi/vault-store.pem
openssl x509 -in ~/.karvi/vault-store.pem -noout -fingerprint -sha256
```

```toml
[credential-backend.vault]
type = "vault"
address = "https://vault.example.net:8200"
ca-file = "~/.karvi/vault-store.pem"
```

The same lines collect a TLS Redis's certificate. A store whose certificate is
replaced is refused until `ca-file` holds the new one; a store under a private
CA is trusted through that CA's certificate. The file is public, any mode.

## The credential helper

A credential helper is a site's executable, a script in any language, that
karvi runs before it resolves any credential and whose standard output sets
values for karvi's credential readers alone. A line `export NETPASS="…"` puts
`NETPASS` where every reader looks, as if the operator had exported it: the
`NET*` fallback, a credential CSV's `env:` cell, a Redis or Vault record's
`env:` field, an env backend's templates, Redis's login variables, and Vault's
token. A name it sets overrides the process environment's silently. Its values
configure nothing (the `KARVI__*` variables are read apart), enter no child's
environment, `ssh`'s included, and reach the daemon only inside the sealed
credentials.

```toml
[creds]
helper = "/usr/local/lib/karvi/credh"
# helper-enabled = true
# helper-unavailable = "skip"   # skip, warn, or fail
# helper-timeout = "60s"
```

`examples/karvi-credh` is a commented POSIX `sh` helper that prints `NETUSER`,
`NETPASS`, and `NETENABLE` from `pass show`, and
[`examples/README.md`](../examples/README.md) says how to install it.

- **Where it is.** `creds.helper` (default `karvi-credh`): a name without `/`
  is that file in the directory of the running karvi executable, where
  `karvi-askpass` is found, and never searched on `PATH`; any other value is a
  place, `~/` under the operator's home, an absolute path, or a path from the
  working directory (`./credh`), made absolute at load. `config show --explain
  creds.helper` prints the file that runs on its `resolved:` line. The package
  installs no `karvi-credh`, so the default is absent until a site places one.
- **When it runs.** Once per invocation that resolves credentials (`login`,
  `command`, `run` and its dry run, `crun`), at its first credential step,
  before any backend or fallback is asked, whether or not a device then reads
  a variable; `config validate`, `daemon`, `job`, `prune`, `version`, and the
  daemon never run it. A `stream` runs it at its first job and holds its values
  for every later job until `--restart`, after which the next job runs it again,
  so a rotated secret is fetched without leaving the stream. A failure refuses
  the invocation, or the stream's job, at planning, before any device is
  reached; nothing is held, and the stream's next job runs it again.
- **How it runs.** With no argument, standard input `/dev/null`, the operator's
  working directory, and karvi's environment as karvi received it, unfiltered,
  so an agent's socket, a keyring's session bus, and a vault client's settings
  reach it as they do in the operator's shell. karvi runs the file it checked
  through its descriptor, so a script's `$0` is `/proc/self/fd/3`: a helper
  names its own files by absolute paths. It runs in karvi's process group, so it
  can ask at the terminal and Ctrl-C reaches it with karvi (exit 113). Its
  standard error is discarded: a helper that must ask, a PIN, a passphrase, or a
  push approval, asks on `/dev/tty`, as `pinentry` and `ssh` do, and writes the
  reason for a failure there.
- **Its checks**, on the file karvi opened, as a credential file's: a symlink
  is refused unless `security.allow-credential-symlinks` is on; the file must be
  regular, owned by the operator, root, or a name in
  `security.approved-admin-users`, and writable by neither its group nor others
  (`0700`, `0750`, and `0755` pass; `0775` does not). A failed check is a
  credential file code ([credential files](#credential-files)), exit 6, whatever
  `creds.helper-unavailable` says:

  ```text
  credential_file_mode_unsafe: client planning: credential helper PATH writable by group or others; remediation: chmod go-w PATH
  ```

- **Unavailable**, no file at the path or one the operator cannot execute,
  follows `creds.helper-unavailable`: `skip`, the default, passes it by
  silently, `--debug` saying `credential helper skipped path="…": REASON`;
  `warn` passes it by after a warning, at every job of a stream, since nothing
  is held; `fail` refuses the invocation at planning,
  `credential_helper_unavailable` (exit 6). A site that locks
  `creds.helper-enabled = true` and `creds.helper-unavailable = "fail"` makes
  its helper mandatory.

  ```text
  warning: credential_helper_unavailable: credential helper PATH: no such file or directory; continuing without it
  ```

- **Its exit.** Exit 0 has its output read. Anything else is
  `credential_helper_failed` (exit 6), naming the path and the status or the
  signal, its output unread: karvi never goes on without the values of a helper
  that failed.

  ```text
  credential_helper_failed: client planning: credential helper PATH exited 1; run it by hand to see its message
  ```

- **Its time.** A helper that prints and exits costs its own time. Past
  `creds.helper-timeout` (default `60s`, from `1s` to `1h`) karvi sends SIGTERM
  to the helper and to every process descended from it, SIGKILL two seconds on
  to any still running, and refuses the invocation, `credential_helper_failed`,
  `did not exit within 60s; run it by hand to see why`; a cancelled invocation
  ends them alike. A process that left the tree, a daemon's double fork as
  `gpg-agent` makes, is not ended, and a helper that exits by itself leaves what
  it started. After the helper exits, karvi reads its output to the end, at most
  two seconds more while a process it left holds the output open: a helper that
  never exits is refused 60 seconds from its start, 62 if it ignores SIGTERM, 64
  at most.
- **`--no-credh`**, on `login`, `command`, `run`, `crun`, and `stream`, sets
  `creds.helper-enabled = false` for the invocation, a site's lock refusing it
  (`config_lock_violation`, exit 3). In a stream it is a run option: on the
  stream's command line it covers every job, as a draft line the jobs whose
  draft holds it.
- **`--debug`** names the path, the names it set, and the time, never a value:
  `credential helper ran path="…" names=NETUSER,NETPASS,NETENABLE elapsed=…`.
- **Under a timer** there is no terminal, so a collection's helper answers
  without asking, and its failure says only the status: run it by hand, as the
  account the timer runs as, to see its message.

**Its output** is read as text, line by line, and never given to a shell, and a
line karvi accepts means what dash and bash give it under `eval`, so a site
tests its helper with `eval "$(karvi-credh)"`:

- A blank line is skipped, and so is a comment, its first non-blank character
  `#`.
- Any other line is an optional `export` and one or more assignments separated
  by blanks, each a portable variable name, `=` with no blank on either side,
  and a value. Unquoted, a value is letters, digits, and `_ . , : / @ % + = -`
  alone. Double-quoted, blanks and `; | & #` are literal, `\"`, `\\`, `\$`, and
  `` \` `` are the four escapes, any other backslash is kept, and a bare `$` or
  `` ` `` is refused. Single-quoted, every character is as written but `'`.
  Empty (`NETPASS=`, `""`, `''`), the name is set and empty.
- A comment after a blank ends the line. A value holds no control character, a
  tab and a carriage return included.
- Anything else is refused, `credential_helper_output_invalid` (exit 6),
  naming the line and the reason and never quoting the line, which may hold a
  secret: a blank on either side of `=`, an unquoted special character, a bare
  `$` or `` ` `` in double quotes, a control character, a word that is no
  assignment (`export NETPASS=a b`), a name set twice (both lines named), and
  output past 64 KiB or a line past 4 KiB. An empty output sets nothing.

```text
credential_helper_output_invalid: client planning: credential helper PATH output line 1: a blank before =
```

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

The credential helper's file takes the first five codes under its own owner
and mode rule, its message naming `credential helper PATH` ([the credential
helper](#the-credential-helper)).

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
| Cells that name environment variables, `env:NAME`; `credential_env_missing` | [section 6](CREDENTIAL-CSV.md#6-secrets-in-the-environment-instead-of-the-file) |
| A formula whose password source is a credential CSV; the row's username is dropped | [section 7](CREDENTIAL-CSV.md#7-a-formula-over-a-credential-csv) |
| `inventory_secret_column`, `config_inventory_source_secret_mapping`: an inventory file holds no secrets, there is no override, and what the check does not find | [section 9](CREDENTIAL-CSV.md#9-an-inventory-file-holds-no-secrets) |
| `credential_csv_malformed`, `credential_csv_row_invalid`, `credential_csv_credkey_duplicate`, `credential_env_missing`: the messages, which never quote a cell but an `env:` cell's variable | [section 10](CREDENTIAL-CSV.md#10-what-a-bad-credential-csv-says) |

Two things to know at an upgrade. An inventory file with a column named
like a secret (`password`, `enable-password`, `api_token`) no longer loads
(exit 5): move the secrets into a credential CSV and delete the column. And
a pinned device never falls to a general row, to `NETUSER`/`NETPASS`, or to
a prompt: a mistyped `credkeyref` stops that device with
`credkeyref_unresolved`, naming the device and the key.

## The operator's name and the credential transforms

A backend reads the operator's login name where a credential is the
operator's: the `%operator%` of the `env` backend's templates, the Redis key,
the Vault path, and a formula's username ([configuration
macros](#configuration-macros)), and the username of a `.cloginrc` with no
`user` line or of a credential CSV row whose `username` cell is blank. A
`[credential-transform.NAME]` table rewrites that name, and the username a
backend answers:

```toml
[credential-transform.adm]
operator = [{op = "add-suffix", suffix = "-adm"}]

[credential-backend.site-creds]
type = "csv"
scope = "user"
path = "~/.karvi/credentials.csv"
transform = "adm"
```

- **`operator`** rewrites the operator's name before the backend is asked;
  **`username`** rewrites the username the backend answers, from a cell, a
  variable, or the operator's name, before the credential is judged. Each is
  a list of operations applied in order: `lowercase`, `uppercase`,
  `crop-to-dot` (up to the first dot), `strip-chars` (`chars`),
  `replace-chars` (`pairs`, each `{from, to}`), `strip-suffix` (`suffix`),
  `replace-suffix` (`suffix`, `replacement`), and `add-suffix` (`suffix`,
  added only to a name without a dot). A username the operations leave empty
  is `credential_incomplete`.
- **A backend's table** is its `transform`; without one, the table named like
  the backend, and otherwise `default`, which changes nothing unless defined. A
  `transform` naming no table is refused when the configuration loads, `config
  validate` included, with `credential_transform_unknown` (exit 2), and so is a
  list that does not build in any transform table, credential or name, whether
  or not anything names the table: `transform_operation_unknown` for an
  operation karvi does not know, naming the list and its line.
- **The platform's fallback** (the `NET*` variables, the prompt, and the
  operator's keys) takes `default`'s `username` operations.

Under the table above the operator `netops`, given a row whose `username` cell
is blank, logs in as `netops-adm`, and the dry run's credential line names
`user=netops-adm`:

```text
! 127.0.0.1 [127.0.0.1] platform=cisco_iosxe user=netops-adm backend=site-creds transport=native
```

With `username = [{op = "lowercase"}, {op = "add-suffix", suffix = "-adm"}]`
under `[credential-transform.default]`, the fallback given `NETUSER=NetOps`
logs in as `netops-adm`, `backend=builtin-env-fallback`.

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

## Long commands: a copy and a show tech

One command can take its own timeout and output limit while the rest of the
job keeps the site's, so a hung `show` beside a long `copy` still fails fast.
`--timeout DURATION` and `--maxbytes BYTES` go after the `--cmd` they bound,
as `--expect` does
([`docs/COMMAND-SESSION.md`](COMMAND-SESSION.md#a-commands-own-bounds)).

- **An image copy.**
  `karvi command rtr1 --cmd 'copy scp://host/image.bin bootflash:' --expect
  'Destination filename.*\?=' --timeout 45m` gives the copy 45 minutes. The
  `!` the device prints now and then does not extend it, one deadline running
  across every prompt and answer, and the keepalives still end a peer gone
  silent ([`docs/TIMEOUTS.md`](TIMEOUTS.md)). Over telnet the declaration
  replaces `telnet.read-timeout`'s cap for that command. An expiry's message
  names the bound: `command timed out after 45m0s (--timeout)`. With
  `execution.device-timeout` set below 45 minutes the job is refused before
  any device (`timeout_over_device_timeout`), naming the `--set
  execution.device-timeout=…` that raises the ceiling.
- **A large show.** `--maxbytes 1073741824 --timeout 45m` after `--cmd 'show
  tech-support'` lets that one response reach 1 GiB, the ceiling of
  `output.max-command-bytes`; a value above `output.max-job-bytes` is refused
  (`maxbytes_over_job_limit`). The free-space check counts the job's width
  times the largest command limit for the spool directory, every device in
  flight possibly sending the large one, so a wide run with a 1 GiB
  declaration asks for that many GiB free there, or runs narrower with
  `spool_width_narrowed` ([the spool directory](#the-spool-directory)). The
  response is recorded whole, in its `commands.jsonl` line and its text
  block, both counted against the job limit.
- **Output past 1 GiB.** No command limit holds it. Let the device write it
  to its own flash, on IOS XE `show tech-support | redirect
  bootflash:tech.txt` and on NX-OS `show tech-support details >
  bootflash:tech.txt`, with a `--timeout` for the time it takes, and copy the
  file off the device.
- **In a stream**, a `--timeout` or `--maxbytes` line after a `--cmd` stays
  with that command from one job to the next; after a bare line it goes with
  that job alone.

## The job's output files

A job folder (`<basedir>/jobs/YYMMDD/<job id>/`) holds `summary.json`
and seven other kinds of file, each with a true/false key under
`[output.files]`, all true by default. A file set false is not created.
Every file is written from the job's memory and none from another file,
so switching one off never breaks the writing of another; what you lose
is what reads it.

| Key | File | What it is | What is lost when false |
|---|---|---|---|
| `output-txt` | `output.NAME.txt`, one per device; NAME is the device name's first label, lowercased (`output.crop-to-dot`, true; `false` writes the whole name), or an address with its dots and colons as hyphens | the session as you would read it in a terminal; its header's time is `display.timestamp` in the effective `timezone` | the readable copy; it can be derived again from `commands.jsonl` (`tools/textfile`, given the same two settings) |
| `commands-jsonl` | `commands.jsonl` | every record, one JSON object per line: the authority | `karvi job follow` cannot replay the job; a device's text cannot be derived again; tools that read records |
| `errors-jsonl` | `errors.jsonl` | the records that did not succeed, each also in `commands.jsonl` | the short list of failures |
| `failed-devices-txt` | `failed-devices.txt` | the devices to run again | the rerun's `--tf` file |
| `commands-txt` | `commands.txt` | the commands as requested | the rerun's `--cf` file |
| `manifest-json` | `manifest.json` | the job as it was accepted | the record of what was asked, by whom, under which configuration |
| `metrics-json` | `metrics.json` | process and dispatch measurements | the measurements |

Relationships worth knowing:

- **The text file does not need `commands.jsonl`.** `commands-jsonl = false`
  with `output-txt = true` keeps text only. It is a one-way choice: without
  the records there is nothing to derive a device's text from again, and
  the notice of a text file that could not be written says so.
- **A rerun needs `failed-devices.txt` and `commands.txt` together**
  (`karvi run --tf …/failed-devices.txt --cf …/commands.txt`).
- **`summary.json` has no key: every folder holds it.** It is written
  when the job is accepted, naming the job's devices, and again whole at
  the job's end with the result, the counts, the causes, and each
  device's outcome; a job has ended when its summary names a final
  status. `karvi job list`, `karvi job follow`, and `karvi-prune` read
  it.
- **The summary names only the files that were written** (`paths`, and
  `output.commands_jsonl`). The devices' text files are not listed; the
  name is `output.` + the device's name (or its address) + `.txt`.
- **All seven false leaves the folder with its summary alone.** No folder
  at all is `output.persist-command = false`, the same as `cmd --nof`:
  the footer ends `artifacts=none`.
- **The invocation decides a job's files on every path.** The plan carries
  the seven switches, `output.persist-command`, and the resolved
  `output.root`, so a run through the daemon keeps the files the client's
  configuration says and writes them under the client's root; the daemon
  reads its own `output.files.*` for no job. A job that keeps no
  `commands.jsonl` is followed from the live edge (records before the
  follow are not kept, and the client says so once). A folder without
  `summary.json`, a job cut short before its summary's first write, is
  unfinished to `job list`, an orphan to `job follow` once its scoreboard
  is stale or gone, and an orphan to the pruner.
- **The invocation's timeouts and command byte limit travel too.**
  `execution.command-timeout`, `execution.device-timeout`,
  `execution.prompt-timeout`, `execution.enable-timeout`,
  `telnet.read-timeout`, `output.max-command-bytes`, and
  `execution.halt-device-on-command-error` reach the job from the client
  on every path, so `karvi --set execution.command-timeout=45m run …`
  bounds a run through the daemon as it bounds `run --no-daemon` and
  `command`. The daemon's own values for these keys bound no job; its
  connect and handshake timeouts, keepalives, and spool settings are
  still its own.
- **`output.max-job-bytes` counts the devices' output as written: the
  `commands.jsonl` lines and the text blocks together.** With both files
  on, a job's output is on disk twice, and the space preflight
  (`output.expected-bytes-per-device`) asks for both copies. At the
  limit the record comes first: a text block that would pass it is left
  out (`output_text_write_failed`, the job continues); a record that
  would pass it ends the job as an output failure, exit 111, with
  `output_job_limit_exceeded` and the device on standard error, the
  device listed in `failed-devices.txt`.
- `output.persist-command = false` (`--nof`) writes no folder and no
  file, on `cmd`, `run --no-daemon`, and a run through the daemon alike: the
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
`spooldir`, and `scoreboards` with the site's monitoring, and let
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
never touched. A control socket a killed master left is swept at the same
two moments, logged `control_socket_abandoned_removed` ([Linux
servers](#linux-servers)). What a karvi killed outright leaves in the
scratch, a system session's `ssh` configuration, an askpass socket, a
recorded login's timing log, is swept at the same two moments and at a
login's start, logged `scratch_abandoned_removed`; the session's own
processes end with it, so no device session is held. A followed record
whose line is larger than `daemon.max-ipc-frame-bytes` allows, or one
spooled in a job that keeps no `commands.jsonl`, reaches the follower with
its output left out and the notice `follow_output_omitted` saying where
the output is.

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
`/usr/share/karvi/tmpfiles.d/karvi.conf` is the rule for the group `netops`.

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
daemon's sockets and state, the logs, and the jobs and transcripts a site
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
list` shows every member's jobs and `karvi job follow ID` reads another
operator's job, live or finished, from any member's shell. A tree the
operator cannot create files in is refused, not passed by
(`output_directory_not_writable` and its siblings, the message naming the group
and the setting); `sharedroot = "none"` keeps every tree under `basedir`, as
every test suite sets it. `basedir` itself is never shared: its `socket` and
`state` are one operator's daemon. `job cancel` reaches only the caller's own
daemon, so another operator's job is cancelled by its owner.

The service account's daemon unit sets no sandbox, so its daemon writes in the
shared trees wherever the account may, as a client-launched daemon does for its
operator ("Retention" says why).

## Retention

`karvi-prune` removes finished work older than the retention age (thirty-one
days by default) from the trees karvi writes: finished jobs, ended transcripts,
and terminal scoreboard files by their end time; job folders whose summary
names no final status and scoreboard files a crashed daemon left at `running`,
by age alone; then the
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

A run removes only what the invoking user owns, under every private root of the
operator's that exists (the site's `users/<user>` under `/opt/karvi` and
`/var/lib/karvi`, and the home's `~/.local/share/karvi`: their jobs,
transcripts, and scoreboards), in the site's shared trees, and in the shared
scoreboards, and passes a colleague's job by; it makes nothing.
On a shared or site install, `sudo karvi-prune` prunes every operator's
work in the provisioned private roots and the shared trees at once, and
is sufficient for the default practice on a host that installs neither
the timer nor the cron or simply wants to clean up by hand. The packaged
schedules are the per-operator timer (`/usr/share/karvi/systemd/user/`), the
site's root timer (`/usr/share/karvi/systemd/system/`), and the cron script
(`/usr/share/karvi/cron/karvi-prune`), all over the one executable with the
same flags, so they make the same decisions.

**The units' writable places.** The site's unit runs in a sandbox:
`ProtectSystem=strict` mounts the whole file
system read-only for the process except `/dev`, `/proc`, and `/sys`,
`ProtectHome` hides the homes, and `ReadWritePaths` names the only places
it may write. It ships with

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
`read-only file system` and the run exits 1 daily. The run makes
nothing: a root that does not exist is not walked.

The user units (`karvi-prune.service`, `karvi-crun.service`) carry no
file-system sandbox: each runs with the operator's own view of the files, as the
client does, so a path means the same to the unit and to the operator, and the
unit writes wherever the operator may. A user manager sets up a mount namespace
only inside an unprivileged user namespace, and a host that restricts those
(Ubuntu's `kernel.apparmor_restrict_unprivileged_userns = 1`) denies it the
capability: systemd starts the unit without the sandbox and says nothing.
`PrivateTmp`, `ProtectSystem`, and `ProtectHome` in a user unit would promise
what such a host does not give, and where they did apply a `/tmp` path would
land in the unit's private `/tmp`, out of the operator's sight. The site's prune
unit runs under the system manager, where the sandbox applies. The service
account's daemon unit, a system unit too, sets none: its daemon writes where its
clients read, the shared trees, the shared scratch, and the spool.

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
nothing. It completes `karvi-audit`'s long flags alike, through
`karvi-audit __complete`; a name, a time, and a pattern are the operator's
to type. A host that holds the file from before a helper was added runs
`sudo karvi setup tab` once more, which reports `updated`.

A shorter name, such as `k` for the fewest keys to type, is a symbolic link
or an alias, never the executable renamed or copied: files depend on its name
and place, `karvi-askpass` found beside it and a scratch file or spool kept
while its maker runs a file named `karvi…`.

A site that gives karvi a shorter name, a symbolic link on the path, binds
that name to the same function in a file of its own, as root:

```bash
sudo ln -s "$(command -v karvi)" /usr/local/bin/k
echo 'complete -F _karvi k' | sudo tee /etc/bash_completion.d/k
```

From the next login Tab after `k` offers what it offers after `karvi`: the
function runs the command word it was given (`k __complete …`), and `k` is
the karvi executable, which answers under any name. The line stands in its
own file, never in `/etc/bash_completion.d/karvi`, which `setup tab` writes
again whenever it differs (`updated`); an operator's `~/.bashrc` holds it for
that operator alone. An alias (`alias k=karvi`) is not completed this way:
the function runs `k` as a command, which an alias does not reach, and Tab
offers nothing.

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
  the footer, with `! collection=DIR replace=N keep=M`
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
  contacted (`crun_directory_not_writable`): one the operator cannot create
  files in, or a sticky one of another owner, with that shape in the message;
  a file at or above the path, or a missing directory that cannot be made,
  with the cause alone, naming that file or the directory.
- **Under systemd.** The service account's daemon unit sets no sandbox
  ("Retention"), so a collection directory anywhere the account may write
  serves through its daemon as in the client process.
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
- **The hook.** `crun.after` names an executable the client runs once the
  collection has ended and its display is printed, on the in-process path and
  through the daemon alike, never for `--detach`: in the collection directory,
  the replaced files' names on stdin one per line, and `KARVI_JOB_ID`,
  `KARVI_JOB_DIR`, `KARVI_CRUN_DIRECTORY`, `KARVI_CRUN_REPLACE`,
  `KARVI_CRUN_KEEP`, and `KARVI_EXIT` in its environment; its output follows the
  display on stderr. A hook that fails, cannot start, or runs past
  `crun.after-timeout` (`5m`) is the warning `crun_after_failed`, the run's exit
  code unchanged, and the audit holds a `crun.after` event either way. The
  shipped examples in `/usr/share/karvi/crun/` commit the replaced files to a
  git repository over the directory and mail the commit's diff
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
- **The mask.** Under `crun.mask = true` (`false` by default) every collection
  file, a `crun`'s and a `--cd` run's, has the secrets a platform's `masks`
  match replaced by `<removed>`, in the lines kept and the `! COMMAND`
  markers; the record and the job's files keep them. The lists are the
  audit's ("The audit trail" below), built in for the five network platforms;
  `karvi --set crun.mask=false crun …` keeps one run's files whole unless the
  site locks the key. [`docs/COLLECTION.md` section
  6.1](COLLECTION.md#61-the-secrets-the-mask).
- **The schedule.** A recurring collection is the site's systemd timer or cron
  over `karvi crun --all --no-daemon`, not a configuration key or a
  daemon-resident schedule: `/usr/share/karvi/systemd/user/karvi-crun.service`
  and `karvi-crun.timer` (a oneshot, one instance at a time, a missed tick run
  at the next start), and `/usr/share/karvi/cron/karvi-crun`, which holds a lock
  for the run and skips a tick that finds it held (one line, exit 75). Two
  collections never overlap in one directory that way; karvi itself does not
  refuse one, so an operator's single-device `crun` during the nightly run is
  not turned away. Under a timer the daemon is not launched: a oneshot's end
  would terminate it. [`docs/COLLECTION.md` section
  7](COLLECTION.md#7-the-schedule).

`karvi-crun(1)` (`man karvi crun`), COLLECTION, restates the directory,
the file, the replacement, the lists, the hook, and the shared directory
for the terminal; a change to one changes both.

## Stream mode

`karvi stream`, `karvi s`, or `karvi -`, composes a run from standard input,
one line at a time. Typed at a terminal it is
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
  line is the job's alone; a job's commands go in the order typed.
  `--expect`, `--blind`, `--blind-return`, `--timeout`, and `--maxbytes`
  lines attach to the command before them and stay or clear with it; one
  after a `--cf` line is dropped. The
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
  the draft. `--restart` empties the draft and reads the configuration
  again as the stream's start does, under the stream's command-line
  options and `--set`, its warnings said again and, at a terminal, the
  opening line; the jobs after it run under the new reading, and a
  reading that fails ends the stream with its code and exit (2, or 3 for
  a lock violation), naming the line. The line count, the last exit, and
  the recalled lines go on through it. `--end`, `--quit`, `--exit`, the
  input's end (Ctrl-D), or Ctrl-C leave without executing, and a Ctrl-C
  outranks lines already read. Only `--go` and `--sendit` execute.
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

## The audit trail

Every job and every login writes its audit events to journald, always, and to
`audit.file`, one JSON line each, when the key names a file. An event is one
journald entry: its message, one line of `key=value` pairs; the fields that
match it; and the whole event as JSON in `KARVI_AUDIT_JSON`, the line
`audit.file` holds (`schema/audit.schema.json`, audit schema 2). A job's events
are named `<kind>.<event>`, the kind `cmd`, `run`, or `crun` as `job list`
shows it:

- `start` and `end`, once each per job; an exercise ends in `end` with the
  outcome `exercise`;
- `command`, once per device per command, a command never sent included, so
  a job's `command` events name its every device;
- `cancel`, when a cancel is asked;
- `crun.after`, a collection's hook.

A login writes `login.start` and `login.end`. A run through the daemon of two
devices, `dead-iosxe` unreachable, the second statement an SNMP community,
read back by its job ID:

```text
$ journalctl SYSLOG_IDENTIFIER=karvi KARVI_JOB_ID=261017-131831-00 -o cat
karvi.audit=run.start operator=netops job=261017-131831-00
karvi.audit=run.command operator=netops outcome=success job=261017-131831-00 device=fake-iosxe command="show clock"
karvi.audit=run.command operator=netops outcome=device_error job=261017-131831-00 device=fake-iosxe command="snmp-server community <removed> RO"
karvi.audit=run.command operator=netops outcome=connection_error job=261017-131831-00 device=dead-iosxe command="show clock"
karvi.audit=run.command operator=netops outcome=not_attempted_prior_command_failure job=261017-131831-00 device=dead-iosxe command="snmp-server community <removed> RO"
karvi.audit=run.end operator=netops outcome=error job=261017-131831-00
```

The line's keys, in this order:

- `karvi.audit`, the event's name, which tells an audit line from the
  daemon's log under the same identifier;
- `operator`, the account the job or the login ran as, a job through the
  daemon its client's; and `loginuser` when the kernel's login UID names
  another account, the person behind `sudo`;
- `outcome`, which a `start` and a `cancel` leave out: a `command` event's is
  its record's `status` (`job list --target`'s OUTCOME), an `end`'s the
  summary's `final_status` (`job list`'s STATUS), a login's `complete` or
  `error`, and `crun.after`'s `success` or `fail`;
- `job` or `login`, the ID;
- `device`, the device's canonical name, on a `command` event and a login's;
- `command`, the statement as sent, on a `command` event alone, quoted
  always, so that a statement holding spaces, `=`, or quotes stays one value.

A job's `start` carries its statements in order in the JSON (`action.commands`);
the audit records no digest. journald's fields match the line:
`KARVI_EVENT_NAME`; `KARVI_JOB_ID` on every event of a job, `KARVI_LOGIN_ID` on
a login's; `KARVI_DEVICE`, `KARVI_OPERATOR`, and `KARVI_LOGINUSER`. journalctl
matches a field whole, with its case, so a device is found by the name `job
list` shows, by every operator and service account, a run under `--nof`
included:

```text
$ journalctl SYSLOG_IDENTIFIER=karvi KARVI_DEVICE=dead-iosxe --since -1min -o cat
karvi.audit=run.command operator=netops outcome=connection_error job=261017-131823-00 device=dead-iosxe command="show version"
karvi.audit=run.command operator=netops outcome=connection_error job=261017-131831-00 device=dead-iosxe command="show clock"
karvi.audit=run.command operator=netops outcome=not_attempted_prior_command_failure job=261017-131831-00 device=dead-iosxe command="snmp-server community <removed> RO"
```

**Reading it: `karvi-audit`.** `karvi-audit` shows the trail as a table, one
event per row: it runs `journalctl` with the trail's identifier and message ID,
so the daemon's log under the same identifier stays out, and lays out each
event's JSON. Its flags are journalctl's, passed as given: `-S` and `-U` the
window, `-n` the newest N, `-g` a pattern over the line (a job's ID, a
statement's words), `-r` newest first, and `-f` to follow until Ctrl-C; and the
trail's own, `-o NAME` the operator or the login user behind `sudo`, `-d NAME`
the device, each repeatable. It reads no configuration. A run of two devices,
`dead-iosxe` unreachable, read back by its job ID:

```text
$ karvi-audit --nocolor -S '2026-10-17 17:10' -g 261017-171016-00
JOB-ID                OPERATOR      MODE  EVENT   OUTCOME                      DEVICE             COMMAND
261017-171016-00       netops       run   start
261017-171016-00       netops       run   command success                    fake-iosxe           show clock
261017-171016-00       netops       run   command connection_error           dead-iosxe           show clock
261017-171016-00       netops       run   end     error
```

The columns stand at fixed widths, so a followed trail stays in line: JOB-ID,
OPERATOR (`OPERATOR/LOGINUSER` under `sudo`), MODE, EVENT, OUTCOME, DEVICE, and
the statement. At a terminal the line is cut with `…` to its width; to a pipe
or a file nothing is cut. OUTCOME is green for work done, orange for work not
done or cut short, red for a failure; the colour goes to a terminal and through
a pipe (`karvi-audit | less -R`), never into a file, and `--nocolor` or
`NO_COLOR` turns it off. Nothing matched is `karvi-audit: no matches` on
standard error, exit 0; a value journalctl refuses is its own message, exit 1;
an event of another audit schema has no row, and one line at the end counts
them. `man karvi-audit` holds the columns, the colours, and the exits in full.

**The mask.** Every statement the audit records, in the line and the JSON,
journald and `audit.file` alike, is masked: the text of every capturing group
of every match of a platform's `masks` is replaced by `<removed>`. A job's
events are masked by the lists of all its devices' platforms together, so that
no event leaves in clear what another platform's list hides; the job's folder
keeps every statement as sent (the run above recorded `snmp-server community
Publ1c RO` in `commands.jsonl`). `cisco_iosxe`, `cisco_iosxr`, `cisco_nxos`,
`arista_eos`, and `juniper_junos` each have a built-in list, in force by
default; a site's `[platform.NAME] masks` replaces a list whole, `[]` turns it
off, and a pattern that does not compile or holds no group refuses the
configuration (`config_platform_mask_invalid`, exit 2). A secret with no word
before it, an answer to a `Password:` prompt sent as its own statement, passes
in clear; an `--expect` response is never in the audit. The same lists mask a
collection file under `crun.mask = true` ([`docs/COLLECTION.md` section
6.1](COLLECTION.md#61-the-secrets-the-mask)), and chapter 63 of
[`docs/EXAMPLES.md`](EXAMPLES.md#63-the-audit-trails-names-and-fields-2026-10-17)
holds each list and the checks it passed.

Who reads the trail is journald's: its files are `root:systemd-journal` 0640,
an operator reads their own events, and the members of `adm` and
`systemd-journal` everyone's. A host whose rsyslog copies the journal into
`/var/log/syslog`, or forwards it, sends the message alone, which still names
the operator, the job, the device, and the statement. The trail's retention is
journald's and the forwarder's; `karvi-prune` never touches it ("Retention").

## Structured evidence

Treat `commands.jsonl`, `errors.jsonl`, `summary.json`, `manifest.json`, audit
records, and scoreboards as authoritative. Human headers, borders, and footers
are projections only.
