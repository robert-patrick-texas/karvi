# karvi v0.39.0 build and qualification result

## Result

The build, with the native transport compiled in beside the external `system`
transport, is **qualified as an engineering candidate**. v0.39.0 (2026-11-02) is
a minor release on 0.38.0 for the fixes of the code review of 2026-10-31: the
scratch and spool chains take only the operator's own directory, a revoked host
key is refused, askpass answers the login's username and password alone, a link
planted in a shared tree is refused, and the configuration, the inventory,
`.cloginrc`, and `display.timestamp` are read as their formats say; fourteen of
the changes are Breaking. The configuration registry moves to 33, every other
counter is 0.38.0's; goldmark, for the documents alone, moves to v1.8.7, Go and
the other modules are 0.38.0's; and the first submission after the upgrade
drains a 0.38.0 daemon. The changes are in [`CHANGELOG.md`](../CHANGELOG.md).
It is not a production claim and not a real-device qualification: the native
transport is proven against the fake device built from the tree, its IOS XE and
Linux personas.

## Build identity

```text
version:                  0.39.0
commit:                   source-release-v0.39.0
build:                    OVm9OErdgujJlrhYFLuV
Go toolchain:             go1.27.3
dependencies:             vendored (go.mod, go.sum, vendor/); no network
native:                   compiled-in
configuration schema:    6
configuration registry:  33
command record schema:   4
daemon IPC schema:       12
job manifest/summary:    5
execution plan:          15
scoreboard:              5
credential package:      2
plan report:             4
audit:                   2
CGO:                      disabled
```

The authoritative `go.mod` names the Go 1.27 floor and requires goldmark,
`x/crypto`, `x/sys`, and `x/term`; `evidence/go-version-m.txt` shows the
modules the shipped executables link, and `evidence/version.txt` lists both
transports.

## Qualification performed

The release source passed, with Go 1.27.3:

- 1,280 top-level named Go tests across 92 packages in vendor mode, 0
  failures (`evidence/core-qualification.log`);
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
- all nineteen executable suites against the native executable, in three lanes
  (`scripts/lib/suites.sh`): the parity suite (`command` and `run` over `system`
  and `native` against the fake device's IOS XE and Linux personas, the
  exec channel included, records compared path by path), the forty-four-row
  v0100 suite (rehearsals, detach, follow, the interrupted client, the ICMP
  gate, shutdown accounting, stop and restart, cancel, `job follow`), and the
  seventeen others in order, among them the spool suite (a 65.7 MB response
  through the spool and across a mid-line threshold), the k03 parser suite, the
  daemon-upgrade suite, the completion suite, the canary suite with
  `secret-scan`, the job list suite (`job list` on a lab output root made
  against the fake device), and the audit suite (`karvi-audit` reading the
  jobs it makes against the fake device back from the host's journal, each row
  checked against the event `audit.file` holds);
- `scripts/verify-shipped.sh` on the executables as shipped: the version,
  the checksums, the ELF format, the links, every counter the executable
  reports, both transports, the helps, the configuration examples, the
  removed keys refused (`evidence/bundle-qualification.log`);
- `scripts/verify-release.sh` end to end, rebuilding with the release
  identity to unchanged checksums, so that its battery ran against the
  shipped bytes (`evidence/release-qualification.log`): the native
  transport checking the host key in its own handshake, under karvi's device
  session, and running no subprocess, and `go mod verify`;
- no lifecycle replay: the newest release whose daemon IPC schema (11) differs
  from this client's (12) is v0.29.0, whose daemon predates the drain that exits
  and listens on `daemon.sock`, which this client never reaches; the drain is
  the daemon-upgrade suite's, against a fixture daemon of an older schema and of
  another build. The compatibility example ran before the number was committed,
  beside the released v0.38.0 daemon, started from the released executable of
  the tree's `bin/` before the rebuild, at this client's IPC schema and of
  another build: the client's `daemon status` stacked the two builds
  (`compatible: false`); its run's exercise drained the idle daemon and started
  one of its own (exit 0, no device contacted); with the older daemon holding a
  job on the fake device, the client's run was refused `daemon_draining` (exit
  112), the daemon's build over the client's, and the daemon exited after the
  job (`evidence/ipc-schema-compat.log`);
- the host's shared places (the shared scoreboard directory, the shared
  directories `/opt/karvi/shared` and `/var/lib/karvi/shared`) as they
  were after the Go tests and the suites, in both verifiers
  (`scripts/lib/host.sh`);
- the tree's Debian package built in the release verifier's run and checked
  by `scripts/check-deb.sh`: the four executables `bin/`'s bytes, every
  document and example configuration under its own name, uncompressed, each
  unit's documentation target, the reference configuration's link, and
  `lintian --fail-on error,warning` with the package's overrides, then tested
  as root in an Ubuntu 26.04 container under Docker by
  `scripts/sysutil-smoke-test.sh`: `karvi-sysutil`'s refusals, install,
  repair, and removal, the template unit and the maintainer scripts, the
  service account's daemon through a shutdown and an upgrade, members following
  the account's live job and each other's jobs from the shared jobs tree, and
  the bundle's `scripts/install.sh`;
- byte-for-byte reproduction of the source bundle across two packaging
  runs, and the bundle verified from its own archive with its own verifier;
  and
- the published package built from that verified extraction twice to the
  same bytes, its executables the bundle's `CHECKSUMS.sha256`, and checked by
  `scripts/check-deb.sh` against it.

The release verifier had first passed on a clean clone of `dev` at
`1a3372d` before the number was assigned (the maintainer's release tools
run it there, since a clone has no shipped bytes to check; the logs are kept
with the maintainer's release evidence). The must-not-appear checks of the
suites ran enforced throughout.

Evidence for this release is in `evidence/`.

## Included executable checksums

| Executable | Bytes | SHA-256 |
|---|---:|---|
| `bin/karvi-linux-amd64` | 12,914,848 | `70b320fde743cbdf63fadec549baa4b6c17ab7b20a12656cb48d4da7e24d250c` |
| `bin/karvi-askpass-linux-amd64` | 3,915,936 | `724d3de509c9fbe95095da5a97fe17b62dbf183f70ea8fec1b0236fa962d7abb` |
| `bin/karvi-prune-linux-amd64` | 3,809,440 | `a3f0bf76dcb633087b17c58780026675cea16a7bf1752062c7ff0886cc9b990e` |
| `bin/karvi-audit-linux-amd64` | 3,965,088 | `fac60afab7413fd9a0a1510dd04ab31898238d4e64b35eb6144f13b8ccc26ac2` |

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
