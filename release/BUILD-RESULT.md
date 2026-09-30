# karvi v0.24.0 build and qualification result

## Result

The build, with the native scrapligo-v1 transport compiled in beside the
external `system` transport, is **qualified as an engineering candidate**.
v0.24.0 (2026-09-30) is the first release from the public repository, on
0.23.0. No behaviour of the executables changed and every counter is
0.23.0's; the tree was prepared for public release: the cumulative patch
stream retired, the specification and its records archived with
`docs/DESIGN.md` stating the settled decisions, the module path
`github.com/robert-patrick-texas/karvi`, and the history begun anew at this
tree's root commit. The changes are in `CHANGELOG.md`. It is not a
production claim and not a real-device qualification: the native transport
is proven against the fake IOS XE device built from the tree.

## Build identity

```text
version:                  0.24.0
commit:                   source-release-v0.24.0
build time:               2026-09-30T00:00:00Z
Go toolchain:             go1.27.1
build tag:                none (every build carries the adapter)
dependencies:             vendored (go.mod, go.sum, vendor/); no network
scrapligo:                v1.4.2 (id=scrapligo-v1, linkage=compiled-in)
configuration schema:    6
configuration registry:  22
command record schema:   2
daemon IPC schema:       10
job manifest/summary:    2
execution plan:          9
scoreboard:              3
credential package:      1
plan report:             1
CGO:                      disabled
```

The authoritative `go.mod` keeps the Go 1.26 floor and pins
`github.com/scrapli/scrapligo v1.4.2`; `evidence/go-version-m.txt` shows the
module in the shipped executables, and `evidence/version.txt` lists both
transports.

## Qualification performed

The release source passed, with Go 1.27.1:

- 727 top-level named Go tests across 75 packages in vendor mode, no
  build tag, 0 failures (`evidence/core-qualification.log`);
- `go vet` across all packages, and the Go race detector across all
  packages in the release verifier's run
  (`evidence/release-qualification.log`; the battery runs once per bytes);
- every Unix-socket test with 25-byte and 145-byte `TMPDIR` values
  (`evidence/socket-tmpdir-qualification.log`);
- `gofmt` listing nothing outside `vendor/`;
- deterministic regeneration of `schema/config-schema.json`,
  `configs/reference.toml`, and `docs/ERROR-CODES.md`;
- example configuration validation and removed-key rejection;
- all seventeen executable suites against the native executable, in three
  lanes (`scripts/lib/suites.sh`): the parity suite (`command` and `run`
  over `system` and `scrapligo-v1` against the fake IOS XE device, records
  compared path by path), the forty-five-row v0100 suite (rehearsals,
  detach, follow, the interrupted client, the ICMP gate, shutdown
  accounting, stop and restart, cancel, `job follow`), and the fifteen
  others in order, among them the spool suite (a 65.7 MB response through
  the spool and across a mid-line threshold), the k03 parser suite, the
  daemon-upgrade suite, the completion suite, and the canary suite with
  `secret-scan`;
- `scripts/verify-shipped.sh` on the executables as shipped: the version,
  the checksums, the ELF format, the links, every counter the executable
  reports, both transports, the helps, the configuration examples, the
  removed keys refused (`evidence/bundle-qualification.log`);
- `scripts/verify-release.sh` end to end, rebuilding with the release
  identity to unchanged checksums, so that its battery ran against the
  shipped bytes (`evidence/release-qualification.log`): the adapter's
  import boundary and host-key assertions, `go mod verify`, the module
  metadata of the executable;
- no lifecycle replay: the daemon IPC schema has been 10 since v0.20.0 and
  no release has moved it, so no earlier executable's daemon differs in
  schema from this client's, and the release tooling skipped the replay
  and said so; the compatibility example ran before the number was
  committed: the released v0.23.0 daemon, started from the released
  executable, reported `compatible: false` to this client on the version
  alone, refused its run with `daemon_incompatible` (exit 112) before any
  job, and was stopped by the client (`evidence/ipc-schema-compat.log`);
- the host's shared places (the shared scoreboard directory, the shared
  directories `/opt/karvi/shared` and `/var/lib/karvi/shared`) as they
  were after the Go tests and the suites, in both verifiers
  (`scripts/lib/host.sh`); and
- byte-for-byte reproduction of the source bundle across two packaging
  runs, and the bundle verified from its own archive with its own verifier.

The release verifier had first passed on a clean clone of `dev` at
`7018f3f` before the number was assigned (the maintainer's release tools
run it there, since a clone has no shipped bytes to check; the logs are kept
with the maintainer's release evidence). The must-not-appear checks of the
suites ran enforced throughout.

Evidence for this release is in `evidence/`.

## Included executable checksums

| Executable | Bytes | SHA-256 |
|---|---:|---|
| `bin/karvi-linux-amd64` | 11,833,504 | `67cef10a4a333d58016c45388c4e8e00cf6e51c1326e2949925c8ff4a2cfdae0` |
| `bin/karvi-askpass-linux-amd64` | 3,887,264 | `3c1a7e8036d1e19628fe45a29a2c56918c89cd9f3145bd3570e140f5ac7427b9` |
| `bin/karvi-prune-linux-amd64` | 3,719,328 | `085057327f3f7276e7492dcc5872a6967d0745c7e58b890072630ef9160f399b` |

## Operational upgrade sequence

A daemon is compatible only when its version and its daemon IPC schema both
equal the client's, so a running v0.23.0 daemon is `compatible: false` to
this client on the version alone (both at schema 10), and the client
refuses a job to it with `daemon_incompatible` (exit 112) before any job is
submitted, naming both pairs and the remediation. Every release restarts
the daemon:

```bash
karvi daemon status
karvi daemon restart
karvi daemon status
```

With active jobs reported, `restart --grace` waits for them and `--force`
stops at once; a forced stop accounts every unfinished unit as
`incomplete_shutdown`. A daemon started by the client stops itself after an
hour idle (`daemon.shutdown-idle-timer`); the packaged unit sets the timer
to `0`. Every release is installed as new from its bundle; no source patch
is shipped.

## Remaining boundary

What karvi does not do yet is `ROADMAP.md`, none of which this release
claims. Cisco IOS XE laboratory qualification on real devices was not
performed and follows on this executable
(`docs/DEVICE-QUALIFICATION-RUNBOOK.md`); the man page is the roadmap's
first item.
