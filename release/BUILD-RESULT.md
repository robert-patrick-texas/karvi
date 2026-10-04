# karvi v0.26.0 build and qualification result

## Result

The build, with the native scrapligo-v1 transport compiled in beside the
external `system` transport, is **qualified as an engineering candidate**.
v0.26.0 (2026-10-04) is a minor release on 0.25.0. The configuration registry
moves from 22 to 24 (`display.record.header`, `display.record.footer`, and
`display.collection.footer` added; `output.files.failures-jsonl` replaced by
`output.files.errors-jsonl`, the job's `errors.jsonl`) and the execution plan
from 9 to 10 (the collection block's word and suffix); the daemon IPC schema
and every other counter are 0.25.0's. Among the changes: the scratch root and
the capacity ledger shared across operators, made by `setup shared`; the
credential prompts edited as stream mode is, Ctrl-C there ending karvi; `--cd`
and `--fs` on `run` and `command`; the collection line a display template;
`run`'s Dispatch options under their keys' locks and ranges; manual pages for
every executable and command word; a reference configuration that loads;
`NO_COLOR`; and the documentation as HTML (`make html`, `tools/md-to-html`,
goldmark vendored for the tool alone). The changes are in
[`CHANGELOG.md`](../CHANGELOG.md). It is not a production claim and not a
real-device qualification: the native transport is proven against the fake IOS
XE device built from the tree.

## Build identity

```text
version:                  0.26.0
commit:                   source-release-v0.26.0
build time:               2026-10-04T00:00:00Z
Go toolchain:             go1.27.1
build tag:                none (every build carries the adapter)
dependencies:             vendored (go.mod, go.sum, vendor/); no network
scrapligo:                v1.4.2 (id=scrapligo-v1, linkage=compiled-in)
configuration schema:    6
configuration registry:  24
command record schema:   2
daemon IPC schema:       10
job manifest/summary:    2
execution plan:          10
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

- 783 top-level named Go tests across 79 packages in vendor mode, no
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
- no lifecycle replay: the daemon IPC schema has been 10 since v0.20.0 and no
  release has moved it, so no earlier executable's daemon differs in schema from
  this client's, and the release tooling skipped the replay and said so; the
  compatibility example ran before the number was committed: the released
  v0.25.0 daemon, started from the released executable of the tree's `bin/`
  before the rebuild, reported `compatible: false` to this client on the version
  alone, refused its run with `daemon_incompatible` (exit 112) before any job,
  and was stopped by the client (`evidence/ipc-schema-compat.log`);
- the host's shared places (the shared scoreboard directory, the shared
  directories `/opt/karvi/shared` and `/var/lib/karvi/shared`) as they
  were after the Go tests and the suites, in both verifiers
  (`scripts/lib/host.sh`); and
- byte-for-byte reproduction of the source bundle across two packaging
  runs, and the bundle verified from its own archive with its own verifier.

The release verifier had first passed on a clean clone of `dev` at
`3ab5a74` before the number was assigned (the maintainer's release tools
run it there, since a clone has no shipped bytes to check; the logs are kept
with the maintainer's release evidence). The must-not-appear checks of the
suites ran enforced throughout.

Evidence for this release is in `evidence/`.

## Included executable checksums

| Executable | Bytes | SHA-256 |
|---|---:|---|
| `bin/karvi-linux-amd64` | 11,931,808 | `7ef14c28fb4cc61d7b399b1692e463fe364408672705a1d93f2da5fe846b97ed` |
| `bin/karvi-askpass-linux-amd64` | 3,895,456 | `0c6e968d29dcdce910f8450060938a1a031cd325324fd5008f118118a7bcfccb` |
| `bin/karvi-prune-linux-amd64` | 3,760,288 | `a5c31e6e767d55ba610772e77553999ff27a958d24d5b142dd98081ebf38cae1` |

## Operational upgrade sequence

A daemon is compatible only when its version and its daemon IPC schema both
equal the client's, so a running v0.25.0 daemon is `compatible: false` to
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
the package's contents is the roadmap's first item.
