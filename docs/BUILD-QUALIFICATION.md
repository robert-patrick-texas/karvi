# Build qualification

## Build gates

The release ships the scrapligo-v1 executable. Both
verifiers run offline from the committed `vendor/` tree:

```bash
make build COMMIT=source-release-v$(cat VERSION) BUILD_TIME=2026-09-30T00:00:00Z
make checksums tools-build
./scripts/verify-bundle.sh
COMMIT=source-release-v$(cat VERSION) BUILD_TIME=2026-09-30T00:00:00Z ./scripts/verify-release.sh
```

`verify-bundle.sh` verifies the executables as they are in `bin/`, without
rebuilding them: identity and checksums, static Linux binaries, both
transport IDs in `version` (it refuses an executable without
`scrapligo-v1`), stateless help/version behavior, script syntax, `gofmt`
over the Go source outside `vendor/`, the unit tests and vet in vendor mode,
deterministic generated files, the example configurations, removed-key rejection, and every
historical smoke suite through `v090-smoke-test.sh`, plus the k03 parser
suite, the daemon-upgrade smoke suite, the v0100 suite
`scripts/v0100-smoke-test.sh` (`make v0100-smoke`), the canary suite
`scripts/canary-smoke-test.sh`, and the parity suite
`scripts/native-smoke-test.sh`.

`verify-release.sh` builds from source: the import boundary and host-key
assertions on the adapter, `gofmt`, `go mod verify`, the tests, vet, and
the race detector in vendor mode, the build, its module metadata and version counters, and the same
smoke suites. It rebuilds `bin/` and rewrites `CHECKSUMS.sha256`; with the
release's `COMMIT` and `BUILD_TIME` the bytes do not change.

A must-not-appear check in a script MUST be able to stop it: a bare
`! command` line is exempt from `set -e`. The
smoke suites use `absent PATTERN FILE` or `! … || fail "…"`.

## Release gates

- The expectations below are the suite-level form of the release gates.
- Every unit test that creates a Unix socket MUST pass regardless of `TMPDIR`
  length; release evidence uses 25-byte and 145-byte values.
- `scripts/package-source-bundle.sh` MUST name the archive root from `VERSION`
  regardless of the working-directory name and MUST exclude `.git`.
- The release tree MUST be clean at the tag, no untracked or modified entry
  and no empty directory (`release-tools/artifacts.sh` refuses otherwise): the
  bundle is packaged from the tree as it stands. This gate replaced the
  cumulative patch's equivalence check when the patch stream was retired.
- `scripts/daemon-lifecycle-replay.sh` passes against the newest release
  whose daemon IPC schema differs from the new client's (it refuses a
  same-schema executable; for v0.12.0, v0.12.1, and v0.13.0 that release was
  v0.10.0, for v0.14.0, which moved the schema to 8, and v0.14.1 it was
  v0.13.0, for v0.15.0, which moved the schema to 9, it was v0.14.1, and
  for v0.16.0 to v0.19.0, which keep 9, it stayed v0.14.1, and for v0.20.0,
  which moved the schema to 10, and v0.21.0 and v0.21.1, which keep it,
  it is v0.19.0; for v0.22.0, the karvi line's first release, and
  v0.23.0 to v0.25.0, which keep 10, there is none, and the replay resumes when a
  karvi release moves the schema from 10).
- The Go tests and the suites MUST leave the host's shared places as they
  were: both verifiers list the shared scoreboard directory and the shared
  trees under `/opt/karvi` and `/var/lib/karvi` before the tests and fail
  after the suites when the list changed (`scripts/lib/host.sh`).

## v0.10.0 planning-boundary expectations

- `karvi version` reports daemon IPC schema 5, registry schema 9, and
  configuration schema 6; `daemon status` reports `daemon_ipc_schema: 5`
  and `client_ipc_schema: 5`; the upgrade suite's schema-2 fixture stays
  visible as incompatible.
- Every job directory holds a manifest of schema 2 that `schema-check`
  validates against its own plan, header, and credential-package
  projection; the k03 assertions on `dispatch_order` and `shuffle_key`
  read them inside the plan's dispatch block.
- A daemon-backed run passes through `prepare_job`, the credential frame
  on `socket/credentials.sock`, and `commit_job`; the canary suite's
  daemon-backed row finds the canary on no JSON socket, log, or artifact.
- A resolution or credential failure on any target refuses the run before
  a job directory exists; `--address-authority daemon` without
  `name.allow-daemon-resolution` is `daemon_resolution_not_allowed`.
- The two boundary tests pass: `internal/daemon` and `internal/jobexec`
  reach no inventory loader, tabular reader, target source, credential
  backend, planner, or application package.

## v0.10.0 ICMP-gate expectations

- The canary suite runs twelve rows (thirteen with shutdown accounting): p1 runs `--ping` over `127.0.0.1` and
  `192.0.2.1` with the real pinger and asserts one new session at the fake
  device, the TEST-NET record `icmp_unreachable` with decision `skip`, the
  loopback record `succeeded` with decision `proceed`, the summary block,
  and the skipped gate within two timeouts plus one second; p2 runs
  `login --ping` against `192.0.2.1` and asserts exit 110 with no session;
  p3 asserts `ping_flag_conflict`. The final scan covers 106
  files.
- The v0100 suite runs twenty-two rows (twenty-five with shutdown accounting): g1–g7 assert the one-line text
  result for a proceeding and a skipped device, an empty `display.ping.header`,
  `--quiet`, the `--debug` gate line, the dry-run's and the exercise's
  capability lines with the fake device untouched, and the conflict.
- Both suites read `icmp_method` from `karvi version --format json` first
  and skip their gate rows with the reason when the host grants this
  process no ICMP method (no ping socket for its group and no `ping` on
  `PATH`), so `verify-bundle.sh` passes on such a host; the gate is claimed
  from a host with a method. On this host the method is the ping socket
  (`net.ipv4.ping_group_range = 1000 9999`).
- `karvi version --format json` reports `"config_registry_schema_version": 10`
  and an `icmp_method`; both verifiers check the registry counter.

## v0.10.0 shutdown-accounting expectations

- The canary suite runs thirteen rows: s1 starts a daemon on its own state
  tree from an environment holding only `HOME` and `PATH`, before the row's
  two canaries exist, runs two sequential jobs through it with different
  passwords, and asserts the device received each job's own password, the
  daemon's pid unchanged, and no canary in the daemon process. The
  final scan covers 133 files and six canaries.
- The v0100 suite runs twenty-five rows (thirty-five with s4–s9): s1 forces a stop with a job in
  flight, s2 sends SIGTERM under a one-second grace, and s3 sends SIGTERM
  under the default grace; s1 and s2 assert four `incomplete_shutdown`
  records, the summary `incomplete` at exit 106 under `shutdown_forced` or
  `shutdown_grace_expired`, and the audit reason; s3 asserts the job
  completed before the stop.
- The v0100 suite runs forty-five rows: s4–s9 run the stop
  and restart cases against the real daemon with a held device,
  c1–c4 cancel a held job, an unknown job, and a finished job and follow a
  cancelled job to its exit, and w1–w6 follow a
  held job live under `jsonl` to byte equality with `commands.jsonl`, the
  finished job again, an unknown ID, an interrupted follow, the finished
  job from its directory after a forced restart, and a cancelled job.
- `scripts/daemon-lifecycle-replay.sh OLD_KARVI` replays `status`, `run`,
  `stop`, and `restart` against a daemon launched by an older executable;
  release engineering runs it with the prior bundle's executable and keeps
  the output with the release's evidence; the prior
  is the newest karvi release whose daemon IPC schema differs from the new
  client's, and until one exists the replay is skipped and the evidence
  says so.
- A forced stop takes up to `daemon.forced-grace-seconds` (floor two
  seconds) longer than before while the jobs write their terminal records;
  the stop client's socket wait already allows for it.

## v0.10.0 credential-delivery expectations

- The canary suite runs nine rows (d1 the dry-run and e1 the exercise,
  each asserting the fake device's log unchanged): row r2 scans the auto-launched daemon's
  command line and environment and reports one process and no hits (gate
  7); row r3 runs two parallel `run` clients with different canaries and
  the fake device records that each session authenticated with its own
  job's password; the final scan covers 79 files and four canaries.
- A package fault is refused at the credential frame with its rule
  (`credential_package_invalid: rule=...`) before commit; only a wrong plan
  digest or a binding that differs from the final plan waits for commit.
- A `daemon start --foreground` from a shell that exports `NETPASS` writes a
  `daemon_environment_secret_variable` notice, name only, to its log.
- `config validate` accepts `credential-backend.<name>.scope` and
  `.required`; an absent optional user `.cloginrc` no longer fails a run.

## Daemon upgrade gates

- A schema-2 fixture MUST be visible through `daemon status` as incompatible.
- A daemon-backed run MUST fail with exit 112 and actionable restart guidance,
  without creating a competing daemon log or submitting a job.
- `daemon stop` and `daemon restart` MUST use the older lifecycle envelope and
  leave a daemon of the current schema ready.
- The prior release's executable SHOULD be replayed with
  `scripts/daemon-lifecycle-replay.sh` during release engineering when its
  bundle is available and its daemon IPC schema differs.

## Official native build and session gates

On an approved Go 1.26+ builder:

```bash
make deps            # optional on a connected host: download and verify
./scripts/verify-release.sh
```

- Vendor metadata, executable module metadata, JSON version output, and
  text version output MUST identify ScrapliGo v1.4.2 (v1.4.1 in earlier
  releases). Only
  `internal/adapters/scrapligov1` may import it; the verifier fails on any
  other import.
- The scrapligo-v1 path MUST be karvi's connection under karvi's session:
  the verifier asserts `WithCustomTransport`, `HostKeyCallback`, and
  `hostkey.Verify` in the adapter and `devsession.Open` in the provider,
  and fails if either references `ssh-keyscan`, `os/exec`, or the removed
  pre-scan (`PrepareNative`, `InspectRemote`).
- The tagged tests, `go vet`, and the race run MUST pass under
  `-mod=vendor`.
- `karvi version --format json` MUST report configuration schema 6, command
  record schema 2, daemon IPC schema 10, job schema 2, registry schema 22,
  and both transport IDs; the text form's transport line is checked
  verbatim.
- **The parity suite** (`scripts/native-smoke-test.sh`) MUST pass:
  twenty-six cases, each as `command` and
  `run --no-daemon` over `system` and `scrapligo-v1` (S1 through the daemon
  as well), against the fake IOS XE server built from the tree into the
  suite's work directory with its own trust store, base directory, and
  `HOME`. `tools/paritycheck` compares every requested-command record path
  by path, excluding only `record_id`, `job_id`, `activity_id`,
  `activity_type`, `candidate_count`, `credential.credential_id`,
  `timing.*`, `transport`, and `error.message` for a failure at open, with
  the observed count in the output-limit message normalised; a new record
  field is compared until excluded on purpose. The suite also asserts the
  records' count, each case's statuses and codes, the lines the fake
  received equal across the four combinations, one connection and one
  session per combination, the activity's exit status, that the operator's
  trust store is unchanged, and that no fake and no daemon is left running.
  `make native-smoke` runs it alone after `make build`; `ONLY=S21` selects
  one case.

A passing verifier qualifies the executable, not native production use:
`docs/CISCO-IOSXE-QUALIFICATION.md` holds the laboratory matrix and
`ROADMAP.md` the open production gates.
