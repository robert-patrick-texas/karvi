# karvi v0.28.0 build and qualification result

## Result

The build, with the native scrapligo-v1 transport compiled in beside the
external `system` transport, is **qualified as an engineering candidate**.
v0.28.0 (2026-10-08) is a minor release on 0.27.0. The configuration registry
moves from 26 to 27 (`logging.level`, `logging.file`, and
`logging.file-required` removed, refused with `config_key_removed`); the daemon
IPC schema and every other counter are 0.27.0's. Among the changes: `command`
over the native transport by default; the Debian package carrying the documents,
the example configurations, and the material a site installs, published with the
release, built from its bundle and checked by `scripts/check-deb.sh` and
`lintian`; every session process ending with karvi; the scratch swept of what a
killed karvi left, and bounded for the askpass socket (`tempdir_too_long`); a
first contact and `insecure` said alike on both transports and through the
daemon; a recorded login's own lines at the first column; `command_completed`
naming its process; and `docs/SSH-TROUBLE.md` for a session that never reaches
the prompt. The changes are in [`CHANGELOG.md`](../CHANGELOG.md). It is not a
production claim and not a real-device qualification: the native transport is
proven against the fake device built from the tree, its IOS XE and Linux
personas.

## Build identity

```text
version:                  0.28.0
commit:                   source-release-v0.28.0
build time:               2026-10-08T00:00:00Z
Go toolchain:             go1.27.1
build tag:                none (every build carries the adapter)
dependencies:             vendored (go.mod, go.sum, vendor/); no network
scrapligo:                v1.4.2 (id=scrapligo-v1, linkage=compiled-in)
configuration schema:    6
configuration registry:  27
command record schema:   3
daemon IPC schema:       10
job manifest/summary:    2
execution plan:          11
scoreboard:              3
credential package:      2
plan report:             1
CGO:                      disabled
```

The authoritative `go.mod` names the Go 1.27 floor and pins
`github.com/scrapli/scrapligo v1.4.2`; `evidence/go-version-m.txt` shows the
module in the shipped executables, and `evidence/version.txt` lists both
transports.

## Qualification performed

The release source passed, with Go 1.27.1:

- 902 top-level named Go tests across 84 packages in vendor mode, no
  build tag, 0 failures (`evidence/core-qualification.log`);
- `go vet` across all packages, and the Go race detector across all
  packages in the release verifier's run
  (`evidence/release-qualification.log`; the battery runs once per bytes);
- every Unix-socket test with 25-byte and 145-byte `TMPDIR` values
  (`evidence/socket-tmpdir-qualification.log`);
- `gofmt` listing nothing outside `vendor/`;
- deterministic regeneration of `schema/config-schema.json`,
  `configs/reference.toml`, [`docs/ERROR-CODES.md`](../docs/ERROR-CODES.md),
  and the manual pages under `packaging/man`;
- example configuration validation and removed-key rejection;
- all seventeen executable suites against the native executable, in three lanes
  (`scripts/lib/suites.sh`): the parity suite (`command` and `run` over `system`
  and `scrapligo-v1` against the fake device's IOS XE and Linux personas, the
  exec channel included, records compared path by path), the forty-five-row
  v0100 suite (rehearsals, detach, follow, the interrupted client, the ICMP
  gate, shutdown accounting, stop and restart, cancel, `job follow`), and the
  fifteen others in order, among them the spool suite (a 65.7 MB response
  through the spool and across a mid-line threshold), the k03 parser suite, the
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
- no lifecycle replay: the daemon IPC schema has been 10 since v0.20.0 and no
  release has moved it, so no earlier executable's daemon differs in schema from
  this client's, and the release tooling skipped the replay and said so; the
  compatibility example ran before the number was committed: the released
  v0.27.0 daemon, started from the released executable of the tree's `bin/`
  before the rebuild, reported `compatible: false` to this client on the version
  alone, refused its run with `daemon_incompatible` (exit 112) before any job,
  and was stopped by the client (`evidence/ipc-schema-compat.log`);
- the host's shared places (the shared scoreboard directory, the shared
  directories `/opt/karvi/shared` and `/var/lib/karvi/shared`) as they
  were after the Go tests and the suites, in both verifiers
  (`scripts/lib/host.sh`);
- the tree's Debian package built in the release verifier's run and checked
  by `scripts/check-deb.sh`: the three executables `bin/`'s bytes, every
  document and example configuration under its own name, uncompressed, each
  unit's documentation target, the reference configuration's link, and
  `lintian --fail-on error,warning` with the package's overrides;
- byte-for-byte reproduction of the source bundle across two packaging
  runs, and the bundle verified from its own archive with its own verifier;
  and
- the published package built from that verified extraction twice to the
  same bytes, its executables the bundle's `CHECKSUMS.sha256`, and checked by
  `scripts/check-deb.sh` against it.

The release verifier had first passed on a clean clone of `dev` at
`71b00a2` before the number was assigned (the maintainer's release tools
run it there, since a clone has no shipped bytes to check; the logs are kept
with the maintainer's release evidence). The must-not-appear checks of the
suites ran enforced throughout.

Evidence for this release is in `evidence/`.

## Included executable checksums

| Executable | Bytes | SHA-256 |
|---|---:|---|
| `bin/karvi-linux-amd64` | 12,263,584 | `17d213ccdec442e4b782f9681b672905bf31a1d4a78120fe20b32adbb93ac9d3` |
| `bin/karvi-askpass-linux-amd64` | 3,903,648 | `5365ef1d907eaf1a4ece7b4b9fb9d117bcc26b5584dd4cbbf0099afa20098356` |
| `bin/karvi-prune-linux-amd64` | 3,776,672 | `aa92806c85df02adc54cab11486307e012fb8aa98931e6051b97dd3fb7322e2b` |

## Operational upgrade sequence

A daemon is compatible only when its version and its daemon IPC schema both
equal the client's, so a running v0.27.0 daemon is `compatible: false` to
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

What karvi does not do yet is [`ROADMAP.md`](../ROADMAP.md), none of which this
release claims. Cisco IOS XE laboratory qualification on real devices was not
performed and follows on this executable
([`docs/DEVICE-QUALIFICATION-RUNBOOK.md`](../docs/DEVICE-QUALIFICATION-RUNBOOK.md));
build numbers in the version are the roadmap's first item.
