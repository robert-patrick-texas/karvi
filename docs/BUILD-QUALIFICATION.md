# Build qualification

## Build gates

Both verifiers run offline from the committed `vendor/` tree:

```bash
make build COMMIT=source-release-v$(cat VERSION)
make checksums tools-build
./scripts/verify-bundle.sh
COMMIT=source-release-v$(cat VERSION) ./scripts/verify-release.sh
```

`verify-bundle.sh` verifies the executables as they are in `bin/`, without
rebuilding them: identity and checksums, static Linux binaries, the build
`karvi version` names (20 characters, never `none`), both transports in
`version`, stateless help/version behavior, script syntax, `gofmt` over the Go
source outside `vendor/`, the unit tests and vet in vendor mode, deterministic
generated files, the example configurations, an unknown key refused, and every
historical smoke suite through `v090-smoke-test.sh`, plus the k03 parser suite,
the daemon-upgrade smoke suite, the v0100 suite `scripts/v0100-smoke-test.sh`,
the canary suite `scripts/canary-smoke-test.sh`, and the parity suite
`scripts/native-smoke-test.sh`.

`verify-release.sh` builds from source: the native transport's source
assertions (host key, device session, no subprocess), `gofmt`, `go mod
verify`, the tests, vet, and the race detector in vendor mode, the build, its
version and build, and the same smoke suites; then the tree's package, which
`scripts/check-deb.sh` checks against the tree and
`scripts/sysutil-smoke-test.sh` tests as root in an Ubuntu 26.04 container
under Docker (`karvi-sysutil`, the template unit, the maintainer scripts, the
service account's daemon, and the bundle's `scripts/install.sh` on the host
without the package), never on the build host. It rebuilds `bin/` and
rewrites `CHECKSUMS.sha256`; with the release's `COMMIT` the bytes do not
change. A build is stamped with its version and its commit, and no build time.

Both verifiers, every suite and lab, and `run_suites` before its lanes
refuse a host whose configuration reaches karvi, before they make
anything: a global file (`/etc/karvi/config.toml` or
`/opt/karvi/config.toml`), the operator's (`~/.config/karvi/config.toml`
under the home of the passwd entry), or an inherited `KARVI__` variable, each
named on its own line (`host_config_clean` in `scripts/lib/host.sh`, which
takes the files from the `sources` of `karvi config show --format json`). A
suite asserts karvi's defaults and passes its own settings by `--config` and
`--set`; a site's file changes the defaults and its `[config-lock]` refuses
the settings, and a variable outranks both. On a configured host, verify the
bundle on a build host or in a container with an init (`docker run
--init`), then install it: under a PID 1 that reaps nothing, as `sleep` or a
shell, the processes a suite ends stay as zombies, and
`scripts/lib/suites-test.sh`'s signal rows see their groups still there.

A must-not-appear check in a script MUST be able to stop it: a bare
`! command` line is exempt from `set -e`. The
smoke suites use `absent PATTERN FILE` (`scripts/lib/lab.sh`) or
`! … || fail "…"`.

## Release gates

- Every unit test that creates a Unix socket, or whose scratch must fit the
  askpass socket's bound, MUST pass regardless of `TMPDIR` length; release
  evidence uses 25-byte and 145-byte values. Such a test takes its directory
  from `internal/testdir`, under `/tmp`, not from `t.TempDir()`.
- `scripts/package-source-bundle.sh` MUST name the archive root from `VERSION`
  regardless of the working-directory name and MUST exclude `.git`.
- The release tree MUST be clean at the tag, no untracked or modified entry
  and no empty directory (`release-tools/artifacts.sh` refuses otherwise): the
  bundle is packaged from the tree as it stands.
- The published Debian package MUST be built from the bundle's extraction
  after its `verify-bundle.sh` passes, twice to the same bytes, and pass
  `scripts/check-deb.sh` against that extraction; `release-tools/artifacts.sh`
  stops otherwise and writes neither the package nor the aggregate checksums.
- `scripts/daemon-lifecycle-replay.sh OLD_KARVI` MUST pass against the
  executable of the newest release whose daemon IPC schema differs from the
  new client's and whose daemon the new client reaches; it refuses an
  executable at the new client's schema. Against a daemon that executable
  launched, it replays `daemon status` with its stacked pairs, a run's
  exercise to 192.0.2.1 that drains the idle older daemon and starts its own,
  `stop`, `restart`, and `stop` under `--force`, `--grace`, and `--after`, the
  last two draining first. Release engineering keeps its output with the
  release's evidence; while no such release exists the replay is skipped and
  the evidence says why.
- The Go tests and the suites MUST leave the host's shared places and
  karvi's places under the operator's home as they were: both verifiers list
  the shared scoreboard directory and the shared trees under `/opt/karvi` and
  `/var/lib/karvi`, and whether `/tmp/karvi-<uid>`, `/var/tmp/karvi-<uid>`,
  and the passwd home's `.local/share/karvi` and `.config/karvi` exist,
  before the tests and fail after the suites when the list changed
  (`scripts/lib/host.sh`).

## Daemon upgrade gates

`scripts/daemon-upgrade-smoke-test.sh` runs each step beside a lifecycle-only
fixture daemon, built from `tools/daemon-fixture`, which answers a drain as a
daemon of this tree does: holding no job it exits, and holding its emulated
jobs it stays, answering `draining`. Each run is an exercise to 192.0.2.1,
which reaches no device.

- An idle daemon of an older IPC schema MUST be reported incompatible by
  `daemon status`, and a run MUST drain it, wait for it to exit, and start a
  daemon of this build in the same invocation, saying both.
- An idle daemon at this schema and version but of another build MUST be
  reported with its pairs stacked, and drained and replaced by a run alike.
- A daemon of an older schema holding a job MUST be drained, the run refused,
  `daemon_draining` (exit 112), the builds stacked, with no daemon started
  beside it; `daemon stop --force` MUST reach it at its schema.
- `daemon restart` MUST replace an idle daemon of another build.

## Official build and session gates

On an approved Go 1.27+ builder:

```bash
make deps            # optional on a connected host: download and verify
./scripts/verify-release.sh
```

- The native transport MUST check the host key in its own handshake, under
  karvi's device session, and run no subprocess: the verifier asserts
  `HostKeyCallback` and `hostkey.Verify` in `internal/transport/native/dial.go`
  and `devsession.Open` in its `native.go`, and fails if the package
  references `ssh-keyscan`, `os/exec`, or the system transport's ssh-keyscan
  comparison (`CompareInsecure`, and `InspectRemote` behind it).
- The tests, `go vet`, and the race run MUST pass under `-mod=vendor`.
- The boundary tests of `internal/daemon` and `internal/jobexec` MUST pass:
  neither reaches an inventory loader, tabular reader, target source,
  credential backend, planner, or application package at any depth.
- `karvi version --format json` MUST report both transport IDs, the text
  form's transport line checked verbatim, and the shipped executable the
  schema counters `release/release-manifest.json` lists (configuration,
  registry, command record, daemon IPC, scoreboard, audit, and job).
- **The parity suite** (`scripts/native-smoke-test.sh`) MUST pass: every
  case the script lists, each as `command` and `run --no-daemon` over
  `system` and `native` (S1 through the daemon as well), against the
  fake device built from the tree into the suite's work directory with its
  own trust store, base directory, and `HOME`: its IOS XE persona, and its
  Linux persona for the exec channel and `linux_shell` (S35).
  `tools/paritycheck` compares every requested-command record path by path,
  excluding only `record_id`, `job_id`, `activity_id`, `activity_type`,
  `candidate_count`, `credential.credential_id`, `timing.*`, `transport`,
  and `error.message` for a failure at open, with the observed count in the
  output-limit message normalised; a new record field is compared until
  excluded on purpose, and a difference by design between the transports is
  pinned per transport (`-pin`: the exec channel's `exit_signal` and the
  notice `remote_command_not_stopped`), checked in every stream. The suite
  also asserts the records' count, each case's statuses and codes, the lines
  the fake received equal across the four combinations, one connection per
  combination with one shell or a channel per command, the activity's exit
  status, that the operator's trust store is unchanged, and that no fake and
  no daemon is left running. It runs alone by its shebang, against `bin/` or
  `KARVI`; `ONLY=S21` selects one case.
- **The audit suite** (`scripts/audit-smoke-test.sh`) MUST pass: jobs against
  the fake device, at two devices named for the run alone, read back from the
  host's journal by `karvi-audit` beside the executable, each row equal to the
  event `audit.file` holds as chapter 64 of
  [`docs/EXAMPLES.md`](EXAMPLES.md#64-karvi-audit-the-audit-trail-as-a-table-2026-10-17)
  lays it out; by device, job ID, and operator, newest first, a count, the
  colours, a terminal of 80 columns, a follow ended by Ctrl-C, nothing
  matched, and journalctl's refusal and the reader's own. On a host without
  journald (no `/run/systemd/journal/socket` or no `journalctl`) it runs the
  reader's own refusals alone and skips the journal's rows, j1–j9, with the
  reason, so `verify-bundle.sh` passes on such a host; the reader is claimed
  from a host with journald.
- **The canary suite** (`scripts/canary-smoke-test.sh`) MUST pass: the password
  and enable password it seeds flow through `login`, `command`, and `run`, the
  fake device proving the password arrived, and `secret-scan` finds no seeded
  canary, raw, hex, base64, or as Go prints a byte slice, in any artifact, log,
  socket directory, scoreboard, capacity file, or captured stream, nor in the
  command line or environment of a daemon a run launched. Two `run` clients at
  once, and two jobs in turn through a daemon started from `HOME` and `PATH`
  alone, each authenticate with their own job's password, that daemon one
  process throughout, neither password in its command line or environment. A
  dry-run and an exercise leave the fake device untouched. The ICMP gate skips
  an unreachable target as `icmp_unreachable` within two timeouts plus a second
  and proceeds on a reachable one; `login --ping` to an unreachable address
  exits 110 with no session; `--ping` with `--noping` is `ping_flag_conflict`.
- **The v0100 suite** (`scripts/v0100-smoke-test.sh`) MUST pass against the
  fake device: the rehearsal flags, `--detach`, `--follow=false`, the
  interrupted client, the ICMP gate's lines, the shutdown accounting,
  `cancel_job`, and `job follow`. A stop by `--force`, or by SIGTERM past a
  one-second grace, with a job in flight leaves every record
  `incomplete_shutdown` and the summary `incomplete` at exit 106 under
  `shutdown_forced` or `shutdown_grace_expired`, with the audit's reason;
  under SIGTERM within the default grace the job completes. A follow under
  `jsonl` writes the records of `commands.jsonl` byte for byte: live, once
  finished, and from the job's directory after a forced restart.
- Both suites read `icmp_method` from `karvi version --format json` before
  their gate rows: `socket` or `system` runs them, `unavailable` skips them
  with the reason, so `verify-bundle.sh` passes on a host without an ICMP
  method, and any other answer, the key absent included, fails; the gate is
  claimed from a host with a method.

A passing verifier qualifies the executable, not native production use:
[`docs/CISCO-IOSXE-QUALIFICATION.md`](CISCO-IOSXE-QUALIFICATION.md) holds the
laboratory matrix and [`ROADMAP.md`](../ROADMAP.md) the open production gates.
