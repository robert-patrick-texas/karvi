# BUILD-HOWTO

This procedure covers Ubuntu 24.04 LTS and Ubuntu 26.04 LTS, initial source
installation, official and isolated builds, verification, and rollback. Run
builds as an ordinary release-engineering account; use `sudo` only for
operating-system packages, `/usr/local`, and final installation.

## 1. Install operating-system prerequisites

The same packages are used on Ubuntu 24.04 and 26.04:

```bash
sudo apt-get update
sudo apt-get install -y \
  ca-certificates curl git make gcc libc6-dev python3 \
  openssh-client util-linux patch xz-utils tar gzip file \
  dpkg-dev debhelper lintian
```

`openssh-client` supplies system SSH, `ssh-keyscan`, and `ssh-keygen`.
`util-linux` supplies `script(1)`, which karvi uses for PTY-backed login
transcripts. `dpkg-dev`, `debhelper`, and `lintian` build and check the Debian
package ([§4.3](#43-build-the-debian-package)); the release verifier builds
one and fails without them.

## 2. Install the newest stable Go release

The following sequence asks the official Go download metadata for the newest
stable release matching the server architecture, verifies the published
SHA-256, and installs it under `/usr/local/go`.

```bash
set -euo pipefail

case "$(dpkg --print-architecture)" in
  amd64) GO_ARCH=amd64 ;;
  arm64) GO_ARCH=arm64 ;;
  *) echo "unsupported build architecture" >&2; exit 1 ;;
esac

GO_VERSION="$({
  curl -fsSL https://go.dev/dl/?mode=json
} | python3 -c '
import json, sys
for release in json.load(sys.stdin):
    if release.get("stable"):
        print(release["version"])
        break
')"

GO_FILE="${GO_VERSION}.linux-${GO_ARCH}.tar.gz"
export GO_FILE
GO_SHA256="$({
  curl -fsSL https://go.dev/dl/?mode=json
} | python3 -c '
import json, os, sys
name = os.environ["GO_FILE"]
for release in json.load(sys.stdin):
    for f in release.get("files", []):
        if f.get("filename") == name:
            print(f["sha256"])
            raise SystemExit
raise SystemExit("official archive not found")
')"

curl -fL "https://go.dev/dl/${GO_FILE}" -o "/tmp/${GO_FILE}"
printf '%s  %s\n' "$GO_SHA256" "/tmp/${GO_FILE}" | sha256sum -c -
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf "/tmp/${GO_FILE}"
sudo tee /etc/profile.d/go.sh >/dev/null <<'PROFILE'
export PATH=/usr/local/go/bin:$PATH
PROFILE
. /etc/profile.d/go.sh
go version
```

For a controlled/reproducible build, install the exact approved Go version
instead of automatically selecting the newest one. The authoritative minimum
is the `go` directive in karvi's `go.mod`.

## 3. Verify and extract the full source bundle

```bash
sha256sum -c karvi-v0.28.0-source-linux-amd64.tar.gz.sha256
tar -tzf karvi-v0.28.0-source-linux-amd64.tar.gz >/dev/null
tar -xzf karvi-v0.28.0-source-linux-amd64.tar.gz
cd karvi-v0.28.0
```

Review the release boundary before building:

```bash
cat VERSION
cat go.mod
```

## 4. Choose the build path

### 4.1 Official native-enabled build

The source carries every module scrapligo-v1 needs: `go.mod` names
scrapligo v1.4.2, golang.org/x/crypto v0.26.0, and golang.org/x/term v0.23.0
with their three indirect requirements, `go.sum` holds their
sums, and `vendor/` holds their sources. goldmark v1.8.6 is vendored beside
them for `tools/md-to-html` (`make html`); no executable contains it. A build
host needs no network. On a host with an approved module proxy, `make deps`
re-downloads the modules and checks them against `go.sum` first:

```bash
export GOPROXY='https://proxy.golang.org,direct'   # or the approved internal proxy
export GOSUMDB='sum.golang.org'                    # or the approved internal sumdb

make deps            # optional on a connected host: download and verify
./scripts/verify-release.sh
```

`verify-release.sh` is the official build's gate. It asserts in source that
the scrapligo-v1 adapter carries karvi's host-key callback on scrapligo's
custom transport and that the provider opens karvi's device session, and
fails if that path references `ssh-keyscan`, `os/exec`, or the removed
pre-scan; runs `go mod verify`, the tests, `go vet`, and the race run
under `-mod=vendor`; builds the executable and
checks its module metadata, its version counters, and both transport IDs;
runs every historical smoke suite; then runs the parity suite
`scripts/native-smoke-test.sh`, which builds the fake device from the tree
and runs every case as `command` and `run` over `system` and
`scrapligo-v1` with its own trust store, base directory, and `HOME`,
comparing the records path by path (about a minute; no fake or daemon is
left running and the operator's trust store is unchanged); then the canary
suite. `make native-smoke` runs the parity suite alone after `make build`;
`ONLY=S21 scripts/native-smoke-test.sh` runs one case.
[`docs/BUILD-QUALIFICATION.md`](docs/BUILD-QUALIFICATION.md) states the gates.

The release must contain exactly:

```text
github.com/scrapli/scrapligo v1.4.2
```

Confirm the dependency in source, vendor metadata, and the executable:

```bash
grep -F 'github.com/scrapli/scrapligo v1.4.2' go.mod
grep -F '# github.com/scrapli/scrapligo v1.4.2' vendor/modules.txt
go version -m bin/karvi-linux-amd64 | \
  grep -E 'github.com/scrapli/scrapligo[[:space:]]+v1\.4\.2'
./bin/karvi-linux-amd64 --version | grep -F 'scrapligo: 1.4.2'
```

Do not use `go get ...@latest` in a release tree.

### 4.2 Verify the shipped executables without rebuilding

The bundle's `bin/` holds the scrapligo-v1 executables of
[§4.1](#41-official-native-enabled-build), built with the release identity, and
`CHECKSUMS.sha256` names them:

```bash
./scripts/verify-bundle.sh
```

It checks the checksums and the executable's version counters and both
transport IDs, refuses Go source that `gofmt` would change, tests and vets
offline from `vendor/`, compares the generated files,
validates the example configurations, and runs every smoke suite, the
canary suite, and the parity suite against `bin/` as shipped. To reproduce
the shipped bytes, rebuild with the release identity:

```bash
make build COMMIT=source-release-v$(cat VERSION) BUILD_TIME=2026-09-30T00:00:00Z
sha256sum -c CHECKSUMS.sha256
```

### 4.3 Build the Debian package

The package carries `bin/`'s executables as they are, the manual pages, the
documents under `/usr/share/doc/karvi`, and the units, cron scripts, hook
examples, tmpfiles rule, reference configuration, and schemas under
`/usr/share/karvi`, none of them active ([`docs/FILES.md`
§4.7](docs/FILES.md#47-what-the-package-installs)). It needs the `dpkg-dev`
and `debhelper` packages, and no Go:

```bash
make deb                          # dist/karvi_<version>_amd64.deb
dpkg-deb -c dist/karvi_*_amd64.deb
```

In a release bundle the package is the release's, `VERSION`, and `bin/` must
hold the executables `CHECKSUMS.sha256` lists; on the development line
(`CHANGELOG.md` headed `## Unreleased`) it is `VERSION+dev`, and `bin/` must
hold a build of the tree (`make build`), not the release's executables, which
karvi's identity tells apart (`karvi version`'s commit
`source-release-vVERSION`). The tree is built in a copy, so neither it nor its
parent gains a file but the package; `DIST=DIR make deb` writes it to `DIR`.
`scripts/check-deb.sh PACKAGE TREE` checks a package against the tree it was
built from: the executables, the documents and example configurations under
their own names, the units' documentation, and `lintian` with the package's
overrides. The release verifier builds and checks the tree's package.

## 5. Direct Go build commands

The Makefile is preferred because it keeps the build metadata consistent.
The equivalent core commands are (no build tag is needed; every build
carries the scrapligo-v1 adapter):

```bash
export CGO_ENABLED=0

go test -mod=vendor ./...
go vet  -mod=vendor ./...

go build -buildvcs=false -mod=vendor -trimpath \
  -ldflags='-s -w' -o bin/karvi-linux-amd64 ./cmd/karvi
go build -buildvcs=false -mod=vendor -trimpath -ldflags='-s -w' \
  -o bin/karvi-askpass-linux-amd64 ./cmd/karvi-askpass
go build -buildvcs=false -mod=vendor -trimpath -ldflags='-s -w' \
  -o bin/karvi-prune-linux-amd64 ./cmd/karvi-prune
```

## 6. Verify generated files and behavior

```bash
make generated-clean
./bin/karvi-linux-amd64 version --format json
./bin/karvi-linux-amd64 login --help
./bin/karvi-linux-amd64 command --help
./bin/karvi-linux-amd64 run --help
./bin/karvi-linux-amd64 config validate configs/development.toml
./scripts/smoke-test.sh
./scripts/halt-smoke-test.sh
./scripts/hostkey-smoke-test.sh
./scripts/transcript-smoke-test.sh
./scripts/display-smoke-test.sh
./scripts/v060-smoke-test.sh
./scripts/v061-smoke-test.sh
./scripts/v070-smoke-test.sh
./scripts/v080-smoke-test.sh
./scripts/v090-smoke-test.sh
```

The supplied smoke suite proves the following observable behavior:

- `accept-new` accepts new keys and rejects a mismatch;
- one mismatch in a multi-target run does not suppress later targets by
  default;
- `secure` rejects a missing enrollment;
- `insecure` warns and allows a mismatch;
- the optional host-key mismatch halt produces exit 114 and explicit unstarted
  records;
- display templates, border expansion, and local `--quiet` behave as
  configured;
- multiple command-mode commands use one fresh authenticated interactive
  OpenSSH shell, with ControlMaster and ControlPath explicitly disabled;
- semantic colors distinguish target, resolved address, labels/brackets,
  values, status, and borders; and
- `--debug` emits safe session/timing/hash diagnostics without passwords or raw
  command text;
- terminal-width layout ignores ANSI, splits complete display elements,
  relocates artifacts, and crops only borders;
- `--format json` emits valid pretty JSON using the configured indent; and
- two-target daemon execution persists and visibly reports both success and an
  outputless later-target failure.

## 7. SSH host-key configuration

The unified default is:

```toml
[ssh]
host-key-policy = "accept-new"
known-hosts-file = "auto"
```

The removed values `auto` and `default` are rejected. Under `known-hosts-file =
"auto"` the trust store is `known_hosts` in the operator's private root:
`/opt/karvi/users/<user>/known_hosts` where the site made the operators' roots,
`~/.local/share/karvi/known_hosts` otherwise; `karvi config show --explain
ssh.known-hosts-file` names it on its `resolved:` line.

For authenticated pre-enrollment, select `secure` and populate the store before
using `login`, `command`, or `run`, by
[`docs/SSH-HOST-KEY-POLICY.md`, "Controlled enrollment"](docs/SSH-HOST-KEY-POLICY.md#controlled-enrollment).

`ssh-keyscan` discovers a presented key; it does not authenticate that key.

## 8. Confirm the configuration and daemon/display boundary

This release requires configuration schema 6, daemon IPC schema 10, and
configuration-registry schema 27 (`karvi version`). After replacing the binary,
inspect and, when needed, explicitly restart a still-running older per-user
daemon:

```bash
karvi daemon status
# Confirm active_jobs is 0 before restart.
karvi daemon restart
```

With no option, `daemon stop` and `daemon restart` refuse while the daemon
reports active jobs. `--grace` drains and waits, `--after=DURATION` drains and
waits up to the limit before forcing, and `--force` stops immediately; forced
work still receives terminal records.
A normal run never kills an incompatible daemon or downgrades a job request.
The restart command is the explicit operator authorization to stop the older
same-UID daemon and launch the current executable.

Validate configuration before a maintenance window:

```bash
./bin/karvi-linux-amd64 config validate
./bin/karvi-linux-amd64 config show | grep -E \
  'name.dns-timeout|telnet.read-timeout|display.(command|run).(border|last-border)'
./bin/karvi-linux-amd64 --version
```

Expected defaults include:

```toml
[name]
address-family-preference = "ipv6"
dns-timeout = "2s"

[telnet]
read-timeout = "60s"

[display.command]
border = "\n"
dynamic-border-length = 72
last-border = false

[display.run]
border = "\n"
dynamic-border-length = 72
last-border = false
```

`--ipv4`/`--4` and `--ipv6`/`--6` provide lock-aware, one-invocation overrides
for login, command, and run. Supplying both must fail with exit 4 before any
connection. A literal `--address`/`--management-address` continues to bypass
DNS.

Borders are separators between records. The default blank line appears only
between adjacent command records. `--border` selects a dynamic dash line;
`--noborder` suppresses configured and dynamic separators; combining both is a
usage error. Set the mode's `last-border = true` only when a trailing separator
is desired.

Validate the direct command error-echo path:

```bash
./bin/karvi command --debug --echo --host DEVICE \
  --cmd 'show clock' --cmd 'intentionally invalid command' \
  >command.out 2>command.debug
```

The prompt plus exact sent command must appear before both successful and
device-error output. Debug output must not contain credentials, raw command
text, or device output.

Confirm human and machine projections:

```bash
./bin/karvi command --border --host DEVICE --cmd 'show clock' --cmd 'show version'
./bin/karvi run --noborder --target DEVICE --transport system --format text --cmd 'show clock'
./bin/karvi run --target DEVICE --transport system --format jsonl --cmd 'show clock'
./bin/karvi run --target DEVICE --transport system --format json --cmd 'show clock' | python3 -m json.tool >/dev/null
```

Review [`docs/DISPLAY-CONFIGURATION.md`](docs/DISPLAY-CONFIGURATION.md) and
[`docs/OPERATIONS.md`](docs/OPERATIONS.md).

## 9. Refresh dependencies

A dependency change changes `go.mod`, `go.sum`, and `vendor/` together, so the
source carries the complete reviewed vendor tree and an offline host builds
without a network. Review the change before building:

```bash
git diff --stat -- go.mod go.sum vendor
go list -mod=vendor -m all
go mod verify        # on a connected host: the sums against the proxy
```

To regenerate the three from `go.mod` on a connected host, for example at a
dependency bump:

```bash
rm -rf vendor
go mod tidy
go mod verify
go mod vendor
```

## 10. Install and roll back

The package ([§4.3](#43-build-the-debian-package)), published with each
release beside the bundle, installs into `/usr/bin`; `apt` installs a package
file with its dependency, `openssh-client`, where `dpkg -i` refuses on a host
without it; installing an earlier one rolls back:

```bash
sha256sum -c karvi_<version>_amd64.deb.sha256
sudo apt install ./karvi_<version>_amd64.deb
karvi version --format json
sudo apt install --allow-downgrades ./karvi_<earlier>_amd64.deb   # roll back
```

A minimized image, one whose dpkg excludes `/usr/share/doc/*` and
`/usr/share/man/*` (Docker's Ubuntu, the minimal cloud images), installs the
executables and `/usr/share/karvi` alone, no document or manual page; the
documents are in the bundle and the repository.

Without the package, use versioned destinations and an atomic symlink. A host
installed so finds the units, cron scripts, hook examples, and documents the
package puts under `/usr/share/karvi` and `/usr/share/doc/karvi` in the
bundle's `packaging/`, `configs/`, `schema/`, and `docs/`. Use one way or the
other on a host: `/usr/local/bin` comes before `/usr/bin` on the usual `PATH`.

```bash
sudo install -d -m 0755 /usr/local/lib/karvi
sudo install -m 0755 bin/karvi-linux-amd64 \
  /usr/local/lib/karvi/karvi-v0.28.0
sudo install -m 0755 bin/karvi-askpass-linux-amd64 \
  /usr/local/lib/karvi/karvi-askpass-v0.28.0
sudo install -m 0755 bin/karvi-prune-linux-amd64 \
  /usr/local/lib/karvi/karvi-prune-v0.28.0
sudo ln -sfn /usr/local/lib/karvi/karvi-v0.28.0 /usr/local/bin/karvi
sudo ln -sfn /usr/local/lib/karvi/karvi-askpass-v0.28.0 \
  /usr/local/bin/karvi-askpass
sudo ln -sfn /usr/local/lib/karvi/karvi-prune-v0.28.0 \
  /usr/local/bin/karvi-prune
karvi version --format json
```

Rollback by repointing the symlinks to the prior verified version. Keep the
source archive, its SHA-256 file, the test log, the dependency list, and the
`go version -m` output together as the local release record.

## 11. Evidence required from future AI or human development teams

Every release should provide:

- the exact version and its tag;
- SHA-256 for every distributed artifact;
- updated `go.mod` and `go.sum` plus an offline dependency path when needed;
- successful tests, vet, generated-file comparison, and smoke tests;
- executable `go version -m` output;
- schema/config breaking-change notes;
- implementation and qualification boundaries;
- installation and rollback instructions; and
- a statement of all remaining production gates.
