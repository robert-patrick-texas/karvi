<img width="1733" height="907" alt="KARVI-viking-fleet-command" src="images/karvi-viking-fleet-command.png" />

# karvi

`karvi` is a Go network-operations tool for interactive login, one-device
commands, and governed concurrent command execution. The code and its tests are
the reference for what karvi does; [`docs/DESIGN.md`](docs/DESIGN.md) states the
settled decisions, the reasons behind them, and the alternatives not taken.

## Development status

This tree carries the table-driven parser with unique-prefix abbreviation,
target files and dispatch order, login recording and output folders,
registered error codes, the client-owned planning boundary with the
credential plane, `run --dry-run`, `run --exercise`, `--detach` and
`follow_job`, the two-probe ICMP gate, shutdown accounting, `cancel_job`
with `karvi job cancel`, one device session driving both SSH transports
with scrapligo-v1 as karvi's own connection, and platform names and their
resolution.
Items still open are listed in [`ROADMAP.md`](ROADMAP.md). The sections below
describe the behavior of the code as it exists; `VERSION` names the release.

## Release status

[`CHANGELOG.md`](CHANGELOG.md) lists every release's changes, the breaking ones
first in each release's block. A running daemon of another release accepts no
job from the new client until `karvi daemon restart`: a daemon is compatible
only when its version and its daemon IPC schema both equal the client's
([`docs/OPERATIONS.md`, "Upgrade"](docs/OPERATIONS.md#upgrade)); since v0.14.0
the records of a run travel over the socket, the invocation decides the job's
files on every path, and the configuration keys nothing read are refused. The
breaking changes of v0.10.0 from v0.9.2 remain in force:

1. `ssh.host-key-policy` accepts `accept-new` (the default), `secure`, and
   `insecure`; `auto` and `default` are rejected.
2. OpenSSH connection reuse is disabled for every mode (the platform
   table's `control-master` key is accepted and inert).
3. `karvi daemon stop` and `karvi daemon restart` refuse while jobs are active
   unless `--grace`, `--after=DURATION`, or `--force` is given.
4. Every error carries one registered code, listed in
   [`docs/ERROR-CODES.md`](docs/ERROR-CODES.md).
5. Configuration schema 6, registry schema 10, and job manifest schema 2 (this
   tree: registry schema 24, daemon IPC schema 10, execution plan 10, scoreboard
   3).

A newer client still inspects, stops, and restarts a daemon launched by an
older executable. Job submission remains exact-schema only; `karvi run` never
silently downgrades a request or kills an incompatible daemon.

The official Linux/amd64 executable is the **scrapligo-v1 build** (`make
build`; `scripts/verify-release.sh`), whose version output lists the
external `system` transport and `scrapligo: 1.4.2` compiled in. The source
pins ScrapliGo **v1.4.2** behind the karvi-owned adapter boundary, with the
module files and `vendor/` committed so a build host needs no network. It
is the only build: the `scrapligo_v1` build tag and the dependency-free
preview module are gone, and a plain `go build` carries the adapter. A
native slot that names an implementation the executable does not carry
fails closed (`native_transport_unavailable`): missing native support never
falls back silently to OpenSSH.

## Address-family selection

The configuration default remains IPv6-first:

```toml
[name]
address-family-preference = "ipv6"
dns-timeout = "2s"
```

Each network-facing mode accepts a one-invocation override:

```bash
karvi login --ipv4 router1
karvi command --4 --host router1 --cmd 'show clock'
karvi run --ipv6 --target router1 --transport system --cmd 'show version'
```

`--ipv4` and `--4` are equivalent; `--ipv6` and `--6` are equivalent. Supplying
both preferences is `ExitUsageError(4)`. A literal
`--address`/`--management-address` continues to bypass DNS and therefore makes
family preference irrelevant for that target.

For DNS targets, karvi queries the preferred usable family first, falls back to
the other usable family if necessary, and selects the numerically lowest
address within the chosen family. A startup capability probe determines whether
IPv4 and IPv6 sockets are locally available.

## Command grammar and one-session execution

`command` has alias `cmd`; `--cmd` aliases `--command`. Targets are supplied
positionally or with `--target`/`--host`. `--address` aliases
`--management-address` and supplies a literal connection address; it never
selects a target.

```bash
karvi command router1 show clock
karvi cmd router1 --cmd 'show clock'
karvi command --host router1 --cmd 'show clock'
karvi command --cmd 'show clock' --target router1 --command 'show version'
karvi command --address 192.0.2.10 --target router1 --cmd 'show clock'
```

Recognized karvi options may follow positional command words. This makes the
following valid and sends one device command, `show clock show clock`:

```bash
karvi cmd router1 'show clock' 'show clock' --echo --border --transport system
```

Use `--` before literal device command text containing a token that matches a
karvi option:

```bash
karvi command router1 -- show running-config | include --echo
```

One device session drives `command` and `run` on both SSH transports: on
`system` one fresh `ssh -tt` process with ControlMaster off, on `scrapligo-v1`
one connection with no subprocess. Every requested command runs sequentially
through that one authenticated interactive shell, after one privilege escalation
and the platform's paging commands
([`docs/COMMAND-SESSION.md`](docs/COMMAND-SESSION.md)). This avoids Cisco IOS XE
devices that accept login but reject secondary SSH session channels.

## Prompt and command echo

Enable echo in configuration or at the command line:

```toml
[display.command]
echo = false

[display.run]
echo = false
```

```bash
karvi command --echo --host router1 --cmd 'show clock'
karvi run --echo --target router1 --transport system --cmd 'show clock'
```

Human text then includes the effective prompt and sent command:

```text
router1#show clock
01:20:19.849 EDT Thu Sep 10 2026
```

The prompt/command line is also displayed when the device reports a command
error. An observed driver prompt is preferred; if prompt metadata is
unavailable but the result proves the command reached the device, karvi uses a
deterministic inferred prompt. Structured command records distinguish
`observed` from `inferred` prompt provenance.

## Display, borders, and formats

```toml
[display]
theme = "dark"                 # auto, dark, light, nocolor
color = "auto"                 # auto, always, never
timestamp = "hh:mm:ss yyyy-mm-dd"

[display.colors]
target = "default"
address = "default"
label = "default"
value = "default"
timestamp = "default"
border = "default"
dynamic-border = "default"
success = "default"
warning = "default"
error = "default"
muted = "default"
accent = "default"

[display.login]
header = "! <target> [<address>] platform=<platform> user=<user> backend=<auth-backend> transport=<transport>"
footer = ""

[display.command]
header = "! <target> [<address>] platform=<platform> user=<user> backend=<auth-backend> transport=<transport>"
footer = "! exit=<exit-code> elapsed=<elapsed> artifacts=<artifacts>"
border = "\n"
dynamic-border-length = 72
last-border = false
echo = false

[display.run]
header = "! <target> [<address>] platform=<platform> user=<user> backend=<auth-backend> transport=<transport>"
border = "\n"
dynamic-border-length = 72
last-border = false
echo = false

[display.json]
indent = 2
```

With dark-theme defaults, target is bold yellow, resolved address is bold
magenta (in both themes), labels/brackets are blue, values are white, success is
green, warnings are bold orange (in both themes), and borders are gray. Each
role is independently configurable; `orange` is the 256-colour index 208.

A border separates adjacent command records. By default one blank line appears
between records and no border follows the final record. Set the relevant
`last-border = true` to emit the final separator as well. Set `border = ""` to
disable the configured separator.

```bash
# Dynamic dashes based on the visible header width, or the configured fallback.
karvi command --border --host router1 --cmd 'show clock' --cmd 'show version'

# Suppress configured and dynamic borders.
karvi command --noborder --host router1 --cmd 'show clock'
karvi run --noborder --target router1 --transport system --cmd 'show clock'
```

`--border` and `--noborder` are mutually exclusive. Only generated border lines
may be cropped to terminal width. Device output and echoed prompt/command text
are never cropped or rewritten.

Command and run formats are:

- `text` — default human presentation;
- `jsonl` — compact one-record-per-line machine stream; and
- `json` — one pretty-printed JSON array controlled by `display.json.indent`.

## Semantic terminal layout

Generated headers and footers measure visible columns without counting ANSI
escape sequences. When a generated line exceeds terminal width, karvi first
moves a trailing `artifacts=<artifacts>` element to a new line; otherwise it
splits only between complete template elements. A single element value is never
cut to fit. `--quiet` suppresses generated headers, footers, and borders but not
device output, mandatory warnings, errors, or explicitly requested debug
messages.

## Safe debug diagnostics

`--debug` can appear globally or in login, command/cmd, and run:

```bash
karvi command --debug --host router1 --cmd 'show clock' \
  >command.out 2>command.debug
```

Diagnostics include configuration provenance, resolution, credential backend,
transport selection, session milestones, command index/hash, timing, output
size, prompt provenance, and stable errors. They do not include passwords,
enable passwords, raw command text, or device output.

## SSH transport configuration

Configuration schema **6** and registry schema **13** use per-mode selectors and
named implementation slots ([`docs/SSH-TRANSPORTS.md`](docs/SSH-TRANSPORTS.md)):

```toml
[config]
schema-version = 6

[ssh.login]
transport = "default"       # default -> system

[ssh.command]
transport = "default"       # default -> system

[ssh.run]
transport = "default"       # default -> native

[ssh.transports]
system = "ssh"
native = "scrapligo-v1"
# alternate1 = "scrapligo-v2"
# alternate2 = "exec:./future-device-handler"
```

A selector may be `default`, `system`, `native`, `preferred`, `telnet`, a named
slot, or a compiled implementation ID. A bare system executable is found
through `PATH`; a slash-containing relative path is resolved from the karvi
executable directory. Explicit unavailable mappings fail configuration
validation. The default native mapping is lazy, allowing a system-only preview
to operate until native is actually selected.

`karvi --version` reports the exact transport composition of the executable.

## Login, transcripts, and host keys

```bash
karvi login router1
karvi login --host router1 --record
karvi login router1 --record=./router1.log
karvi login --host router1 --address 192.0.2.10
```

Recording uses a controlling PTY, propagates terminal resize, restores terminal
state, and writes a `0640` transcript and metadata pair below
`transcript.root/YYMMDD/` (see
[`docs/LOGIN-TRANSCRIPTS.md`](docs/LOGIN-TRANSCRIPTS.md)).

One host-key policy governs login, command, and run:

```toml
[ssh]
host-key-policy = "accept-new"
known-hosts-file = "auto"
halt-run-on-host-key-mismatch = false
```

- `accept-new` (default): enroll a first-seen key and reject a changed key;
- `secure`: require pre-enrollment; and
- `insecure`: accept unknown or changed keys with prominent warnings.

The automatic trust store is `~/.local/share/karvi/known_hosts`, in the
operator's own root. Both transports check the key in the SSH handshake and
enrol it under the device's canonical name, or `[name]:PORT` on a port other
than 22 ([`docs/SSH-HOST-KEY-POLICY.md`](docs/SSH-HOST-KEY-POLICY.md)).

## Fleet run

```bash
karvi run --target router1 --target router2 \
  --transport system --dispatch parallel --workers 2 \
  --cmd 'show clock'

karvi run --tf routers.txt --transport system \
  --format text --echo --border --cmd 'show version'
```

Every selected target receives a terminal command record. Text mode identifies
each target and renders explicit diagnostics for outputless failures; JSONL is
the canonical machine stream.

`run` defaults to the native transport, which the shipped executable has
compiled in; configure `[ssh.run] transport="system"` or pass `--transport
system` to use OpenSSH instead.

Two rehearsals precede a live run. `--dry-run` plans on the client, probes
the daemon without launching one, and prints the inspection report;
nothing is submitted and no device is touched. `--exercise` submits the
plan and package and has the daemon validate everything but the device,
writing `exercise.json` beside an empty `commands.jsonl`; the summary reads
`exercised`. Both take `--format json`. A live run renders records as they
become durable; `--detach` returns at acceptance with the job ID and
artifact directory, `--follow=false` waits without rendering, and Ctrl-C
leaves the job running in the daemon. `karvi job follow JOB-ID` renders a
detached or interrupted job again from its beginning, live or finished, and
`karvi job cancel JOB-ID` stops one job.

`--ping` sends two ICMP probes to each target before its transport and skips a
target that answers neither (`icmp_unreachable`); `--noping` sends none. The
gate is off unless `network.ping-targets` is true. Widen
`net.ipv4.ping_group_range` to your operators' groups so the probes go over a
ping socket; otherwise the system `ping` binary is used. Text output shows one
line per gated device (the `display.ping.header` template), and every record
carries the probe outcomes.

## Stream mode

`karvi stream`, or `karvi -`, reads a run line by line from standard
input, typed at a terminal or piped from a script: a line beginning with
`--` is one run option (`--target router1`, `--tl "r1 r2"`, `--dp`), any
other line is one command sent as written (a `--cmd`, `--command`, or
`--cf` line counts as a command), blank and `!` or `#` lines are skipped.
`--go` (or `--sendit`) executes the draft as `run` would and keeps the
targets and options for the next batch of commands; `--clear` drops the
commands and keeps the rest; `--reset` empties the draft; `--end` (or
`--quit`), EOF, or Ctrl-C leave without executing. Typed at a terminal,
a line is edited with the usual keys and the up arrow recalls earlier
lines. A line the parser refuses is reported by its number and dropped.
The exit is the last job's.

```sh
karvi - <<'EOF'
--tl core-nyc-01,core-nyc-02
--dp
show clock
show version\r
--go
--end
EOF
```

## Collection run

```bash
karvi crun --all                                   # each device its platform's list
karvi crun --target core-nyc-01.example.net --cd=/opt/karvi/shared/crun
```

`crun` collects one or more commands from a scope into one file per device,
named by the device, in one flat directory (`crun.directory`, `<basedir>/crun`
unless configured; `--cd=PATH` for one run): each command's output under a `!
COMMAND` marker line and nothing else, the file replaced only when every command
of the device came back, so a device not reached keeps its previous file. With
no command on the line each device is sent its platform's `crun-commands` list.
It takes every `run` option; the job folder is written as for any run, without
`output.NAME.txt`; the display ends with a line counting the files replaced and
kept. `run` and `command` given `--cd=PATH` write the same file, unfiltered,
beside their usual job folder; `--fs=SUFFIX` appends a suffix to each file's
name (`--fs=.cfg`), and on `run` and `command` alone writes into the working
directory. A platform's `crun-filters` drop the output lines that change at
every collection without the device having changed (the byte count, the clock
period, the uptime, the time of the show) from the collection file alone, the
last-change stamp kept; a site's array replaces the built-in list. `crun.after`
names a hook the client runs when the collection has ended, in the directory
with the replaced files on stdin; the shipped examples commit them to git and
mail the diff ([`docs/COLLECTION.md`](docs/COLLECTION.md)). A recurring
collection is the site's systemd timer or cron over the word
(`packaging/systemd/user/karvi-crun.timer`, `packaging/cron/karvi-crun`;
[`docs/COLLECTION.md` section 7](docs/COLLECTION.md#7-the-schedule)). A shared
directory of mode `2770` or `2775` lets every member of its group collect; `sudo
karvi setup shared` makes it once as `/opt/karvi/shared/crun` with the shared
job and transcript trees, which every operator's run then uses by default
([`docs/OPERATIONS.md` "The shared
trees"](docs/OPERATIONS.md#the-shared-trees)). [`docs/OPERATIONS.md` "The
collection run"](docs/OPERATIONS.md#the-collection-run) has the rest.

## Context help

```bash
karvi login --help
karvi command --help
karvi cmd --help
karvi run --help
karvi config --help
karvi daemon --help
karvi setup --help
karvi watch --help
karvi version --help
```

Tab completes the words, options, values, device names, and job IDs, and
`karvi-prune`'s flags and their words, once the site has run `sudo karvi setup
tab` ([`docs/OPERATIONS.md` "Tab
completion"](docs/OPERATIONS.md#tab-completion)).

## Implemented foundation and remaining boundary

The candidate includes layered TOML configuration with include graphs, locks,
macros, validation and provenance; CSV inventory and deterministic DNS;
credential policies and authenticated askpass; one device session over system
OpenSSH and the compiled scrapligo-v1 connection, gated Telnet; serial,
parallel, and adaptive-wave dispatch; durable JSONL and summaries; audit,
scoreboards, metrics, per-user daemon IPC, capacity admission, retention
(`karvi-prune`, [`docs/PRUNE.md`](docs/PRUNE.md)), PTY login recording, shared
display formatting, and the watch screen (`karvi watch`: the running jobs above
a rule, the finished ones below, a detail pane per job, the `/` filter and the
`s` sort, [`docs/OPERATIONS.md` "The watch
screen"](docs/OPERATIONS.md#the-watch-screen)), the site's shared trees, the
operators' roots under `/opt/karvi/users/<username>`, and the scratch root
`/dev/shm/karvi` with its rule for every boot (`sudo karvi setup shared`;
[`docs/OPERATIONS.md` "The shared trees"](docs/OPERATIONS.md#the-shared-trees)).

Scaling up (a wide parallel host, a large network, the free-space check per
volume, the daemon's memory budget) is [`docs/SCALE.md`](docs/SCALE.md); the
settings it names are in the configuration reference.

Cisco IOS XE laboratory qualification, durable daemon recovery, signed
packages/SBOM/FIPS evidence, and the 2,500-device performance/accounting
gate remain incomplete. See [`ROADMAP.md`](ROADMAP.md).

## Build paths

Verify the shipped executables as bytes in seconds, or the executables and
their source without rebuilding them (offline, from the vendor tree):

```bash
./scripts/verify-shipped.sh
./scripts/verify-bundle.sh
```

Rebuild with the release identity, which reproduces `CHECKSUMS.sha256` byte
for byte:

```bash
make build COMMIT=source-release-v$(cat VERSION) BUILD_TIME=2026-09-30T00:00:00Z
sha256sum -c CHECKSUMS.sha256
```

The full build-and-verify run, with the race detector and the parity suite
(Go 1.26 or later; no network is needed, the dependencies are in `vendor/`):

```bash
make deps            # optional on a connected host: download and verify
./scripts/verify-release.sh
```

Only `internal/adapters/scrapligov1` imports ScrapliGo.

## Documents

Operator guides:

- The documentation as HTML: `make html` writes every document below, an
  index, and the images into `../html`, beside the tree; open
  `html/index.html`. Every link in it is relative, so the directory can be
  copied anywhere and read without the network.
- `packaging/man/`: the manual pages the package installs: `man karvi`
  (the configuration, the values, the environment, the files, the exit
  statuses), `man karvi WORD` for each command word (`man karvi run`), `man
  karvi-prune`, and `man karvi-askpass`; each word's SYNOPSIS and DESCRIPTION
  are its `--help`, generated (`make generate`).
- [`docs/QUICKSTART.md`](docs/QUICKSTART.md): inspect the executable, first
  configuration, first commands.
- [`docs/OPERATIONS.md`](docs/OPERATIONS.md): upgrade, the daemon, retention,
  credential files.
- [`docs/FILES.md`](docs/FILES.md): every directory and file karvi reads or
  writes, in shared and in individual mode, with its mode, owner, and group,
  and how karvi chooses each place.
- [`docs/PRUNE.md`](docs/PRUNE.md): what `karvi-prune` removes, where it looks,
  the timers, the hand run.
- [`docs/COLLECTION.md`](docs/COLLECTION.md): the collection run `crun`: what a
  collection is, the command lists RANCID and Oxidized send per platform as
  examples, and sub-platform tables per device model.
- [`docs/CREDENTIAL-CSV.md`](docs/CREDENTIAL-CSV.md): the credential CSV
  backend: declaring it, the file's fields, how a row is chosen, keys and pins
  (`credkey`, `credkeyref`), variables instead of secrets, a formula over it,
  the file rules, why an inventory file holds no secrets, the errors.
- [`docs/TIMEOUTS.md`](docs/TIMEOUTS.md): every bound on a device session, its
  key, and what its expiry records.
- [`docs/COMMAND-SESSION.md`](docs/COMMAND-SESSION.md): the command grammar and
  the device session's lifecycle.
- [`docs/COMMAND-TROUBLESHOOTING.md`](docs/COMMAND-TROUBLESHOOTING.md): what to
  capture and how to read it.
- [`docs/DISPLAY-CONFIGURATION.md`](docs/DISPLAY-CONFIGURATION.md): human-facing
  output settings.
- [`docs/LOGIN-TRANSCRIPTS.md`](docs/LOGIN-TRANSCRIPTS.md): `login --record`.
- [`docs/SSH-HOST-KEY-POLICY.md`](docs/SSH-HOST-KEY-POLICY.md): the unified
  host-key policy, the identity a key is enrolled under, controlled enrollment.
- [`docs/SSH-TRANSPORTS.md`](docs/SSH-TRANSPORTS.md): transport selection,
  slots, build composition.
- [`docs/ERROR-CODES.md`](docs/ERROR-CODES.md): every registered code
  (generated).

Design:

- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md): the packages and their
  boundaries.
- [`docs/DESIGN.md`](docs/DESIGN.md): why karvi does what it does, the settled
  decisions with their reasons and the alternatives not taken.
- [`docs/EXAMPLES.md`](docs/EXAMPLES.md): the worked design sessions, one
  chapter each, from the public-repository preparation on.
- [`docs/TRANSPORT-DRIVER-ARCHITECTURE.md`](docs/TRANSPORT-DRIVER-ARCHITECTURE.md):
  the driver contract, the session layer, the transports, the extension rule.
- `examples/`: one commented example of each file karvi reads (a configuration,
  an inventory, a credential CSV, a `.cloginrc`, a target file, a commands file)
  and how to try them with a dry run.

Qualification and release:

- [`docs/BUILD-QUALIFICATION.md`](docs/BUILD-QUALIFICATION.md): the build,
  release, and native session gates.
- [`docs/CISCO-IOSXE-QUALIFICATION.md`](docs/CISCO-IOSXE-QUALIFICATION.md): what
  the fixture shows and the laboratory matrix.
- [`ROADMAP.md`](ROADMAP.md), [`CHANGELOG.md`](CHANGELOG.md): what is not built
  yet, what each release changed.
- [`BUILD-HOWTO.md`](BUILD-HOWTO.md), [`BUILDING.md`](BUILDING.md): building
  from source.

## Repository map

Reusable public packages are `inventory`, `credentials`, `transform`,
`tabular`, `dispatch`, `platform`, `records`, and `configschema`
(an earlier public `transport` package was removed). Concrete runtime,
operating-system, secret, and third-party adapters remain under
`internal/`.

## License

MIT. See [`LICENSE.md`](LICENSE.md).
