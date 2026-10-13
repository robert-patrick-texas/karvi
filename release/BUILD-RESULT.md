# karvi v0.30.0 build and qualification result

## Result

The build, with the native scrapligo-v1 transport compiled in beside the
external `system` transport, is **qualified as an engineering candidate**.
v0.30.0 (2026-10-13) is a minor release on 0.29.0. The configuration registry
moves from 28 to 29, the execution plan from 12 to 13, and the daemon IPC
schema from 11 to 12; every other counter is 0.29.0's. Among the changes: a
daemon serves only a client of its own build, and a client that launches its
own daemon drains one of another build and starts its own, a busy one
finishing its jobs first; `daemon status` stacks the daemon's values over the
client's; a build names its Go build ID and its commit, and no build time; the
service account's daemon runs under the package's system template, set up by
`karvi-sysutil`, and the bundle installs itself (`scripts/install.sh`); the
daemon binds its sockets in the directory `daemon.sockets`; an invocation
reads its configuration once and says its warnings; a credential helper
(`creds.helper`), `%operator%` in the credential templates, an explicit
`env:NAME` cell or field, and the store backends' keys checked at load; the
transports' own SSH algorithm lists; a stream's `--restart`; and refusals in
colour. The changes are in [`CHANGELOG.md`](../CHANGELOG.md). It is not a
production claim and not a real-device qualification: the native transport is
proven against the fake device built from the tree, its IOS XE and Linux
personas.

## Build identity

```text
version:                  0.30.0
commit:                   source-release-v0.30.0
build:                    P7zVqO_tVBEzQMcMrxna
Go toolchain:             go1.27.1
build tag:                none (every build carries the adapter)
dependencies:             vendored (go.mod, go.sum, vendor/); no network
scrapligo:                v1.4.2 (id=scrapligo-v1, linkage=compiled-in)
configuration schema:    6
configuration registry:  29
command record schema:   3
daemon IPC schema:       12
job manifest/summary:    3
execution plan:          13
scoreboard:              3
credential package:      2
plan report:             2
CGO:                      disabled
```

The authoritative `go.mod` names the Go 1.27 floor and pins
`github.com/scrapli/scrapligo v1.4.2`; `evidence/go-version-m.txt` shows the
module in the shipped executables, and `evidence/version.txt` lists both
transports.

## Qualification performed

The release source passed, with Go 1.27.1:

- 1,020 top-level named Go tests across 89 packages in vendor mode, no
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
  exec channel included, records compared path by path), the forty-four-row
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
- no lifecycle replay: the newest release whose daemon IPC schema (11) differs
  from this client's (12) is v0.29.0, whose daemon predates the drain that exits
  and listens on `daemon.sock`, which this client never reaches; the drain is
  the daemon-upgrade suite's, against a fixture daemon of an older schema and of
  another build. The compatibility example ran before the number was committed:
  beside the released v0.29.0 daemon, started from the released executable of
  the tree's `bin/` before the rebuild, the client's `daemon status` found no
  daemon of its own (`daemon_unreachable`, exit 110), its run's exercise started
  one on `d.sock` (exit 0, no device contacted), and each executable stopped its
  own daemon (`evidence/ipc-schema-compat.log`);
- the host's shared places (the shared scoreboard directory, the shared
  directories `/opt/karvi/shared` and `/var/lib/karvi/shared`) as they
  were after the Go tests and the suites, in both verifiers
  (`scripts/lib/host.sh`);
- the tree's Debian package built in the release verifier's run and checked
  by `scripts/check-deb.sh`: the three executables `bin/`'s bytes, every
  document and example configuration under its own name, uncompressed, each
  unit's documentation target, the reference configuration's link, and
  `lintian --fail-on error,warning` with the package's overrides, then tested
  as root in an Ubuntu 26.04 container under Docker by
  `scripts/sysutil-smoke-test.sh`: `karvi-sysutil`'s refusals, install,
  repair, and removal, the template unit and the maintainer scripts, the
  service account's daemon through a shutdown and an upgrade, and the bundle's
  `scripts/install.sh`;
- byte-for-byte reproduction of the source bundle across two packaging
  runs, and the bundle verified from its own archive with its own verifier;
  and
- the published package built from that verified extraction twice to the
  same bytes, its executables the bundle's `CHECKSUMS.sha256`, and checked by
  `scripts/check-deb.sh` against it.

The release verifier had first passed on a clean clone of `dev` at
`45a3a8e` before the number was assigned (the maintainer's release tools
run it there, since a clone has no shipped bytes to check; the logs are kept
with the maintainer's release evidence). The must-not-appear checks of the
suites ran enforced throughout.

Evidence for this release is in `evidence/`.

## Included executable checksums

| Executable | Bytes | SHA-256 |
|---|---:|---|
| `bin/karvi-linux-amd64` | 12,636,320 | `ec2f2466a97264b9cc7a78a13b3e94047734d9c0bf142986f0faf1d1f640b8df` |
| `bin/karvi-askpass-linux-amd64` | 3,911,840 | `dbafbb9af9bf2bde030f93c61a6a241f8b2fe1e80fec2f0c1133bebe7738aff2` |
| `bin/karvi-prune-linux-amd64` | 3,788,960 | `05adab25dfb1136bd1d3c109e42271f29bd2cf30eadaf6b92564d6262eddd271` |

## Operational upgrade sequence

A daemon serves a client only when its daemon IPC schema and its build, the Go
build ID of its executable, equal the client's. An operator's own daemon, the
one the client launches (`daemon.launch = "client"`, the default), needs
nothing run after an upgrade: the next submission drains a daemon of another
build, an idle one exiting and replaced in the same invocation, a busy one
finishing its jobs and exiting after the last while each submission is refused
`daemon_draining` (exit 112), the daemon's build over the client's. To end it
rather than wait:

```bash
karvi daemon status
karvi daemon stop --grace
```

`--after=DURATION` drains and waits up to the limit, then forces; `--force`
stops at once, and a forced stop accounts every unfinished unit as
`incomplete_shutdown`. An operator's idle daemon stops itself after ten
minutes (`daemon.shutdown-idle-timer`). A service account's daemon under the
template `karvi-daemon@.service` is never drained by a client; after an upgrade
the administrator restarts its unit:

```bash
sudo systemctl restart karvi-daemon@karvi.service
```

Every release is installed as new, from its package or its bundle (`sudo
./scripts/install.sh`); no source patch is shipped.

## Remaining boundary

What karvi does not do yet is [`ROADMAP.md`](../ROADMAP.md), none of which this
release claims. Cisco IOS XE laboratory qualification on real devices was not
performed and follows on this executable
([`docs/DEVICE-QUALIFICATION-RUNBOOK.md`](../docs/DEVICE-QUALIFICATION-RUNBOOK.md)).
