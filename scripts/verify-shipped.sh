#!/bin/sh
# Verify the shipped executables as bytes, in seconds and without a build:
# the version, the checksums, the file
# format, the links, every counter the executable reports, both transports,
# a stateless --help and version, every command's help, the configuration
# examples, and the removed keys refused. scripts/verify-bundle.sh runs this
# first and then the tests and the suites; the release evidence runs this on
# the shipped bytes and lets scripts/verify-release.sh, which rebuilds to the
# same checksums, carry the tests and the suites once.
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"

[ "$(cat VERSION)" = "0.28.0" ]
sha256sum -c CHECKSUMS.sha256
file bin/karvi-linux-amd64 | grep -q 'ELF 64-bit.*x86-64'
file bin/karvi-askpass-linux-amd64 | grep -q 'ELF 64-bit.*x86-64'
file bin/karvi-prune-linux-amd64 | grep -q 'ELF 64-bit.*x86-64'
[ -x bin/karvi-linux-amd64 ] && [ -x bin/karvi-askpass-linux-amd64 ] && [ -x bin/karvi-prune-linux-amd64 ]
[ "$(readlink bin/karvi)" = karvi-linux-amd64 ]
[ "$(readlink bin/karvi-askpass)" = karvi-askpass-linux-amd64 ]
[ "$(readlink bin/karvi-prune)" = karvi-prune-linux-amd64 ]
./bin/karvi-linux-amd64 version --format json | grep -q '"version": "0.28.0"'
./bin/karvi-linux-amd64 version --format json | grep -q '"config_schema_version": 6'
./bin/karvi-linux-amd64 version --format json | grep -q '"command_record_schema_version": 3'
./bin/karvi-linux-amd64 version --format json | grep -q '"daemon_ipc_schema_version": 11'
./bin/karvi-linux-amd64 version --format json | grep -q '"job_schema_version": 3'
./bin/karvi-linux-amd64 version --format json | grep -q '"config_registry_schema_version": 28'
./bin/karvi-linux-amd64 version --format json | grep -q '"id": "system"'
./bin/karvi-linux-amd64 --version | grep -q '^  system: external (id=system, linkage=external-executable)$'
# The shipped executable is the native one. (This line was once a bare
# "! ... | grep -q" asserting the opposite, which set -e never enforced.)
./bin/karvi-linux-amd64 version --format json | grep -q '"id": "scrapligo-v1"' \
  || { echo 'bin/karvi-linux-amd64 lacks scrapligo-v1: build it with make build checksums' >&2; exit 1; }
./bin/karvi-linux-amd64 --version | grep -q '^  scrapligo: 1.4.2 (id=scrapligo-v1, linkage=compiled-in)$'

STATELESS=$(mktemp -d)
rm -rf "$STATELESS/home"
HOME="$STATELESS/home" XDG_CONFIG_HOME="$STATELESS/xdg" ./bin/karvi-linux-amd64 --help >/dev/null
HOME="$STATELESS/home" XDG_CONFIG_HOME="$STATELESS/xdg" ./bin/karvi-linux-amd64 version >/dev/null
[ ! -e "$STATELESS/home" ] && [ ! -e "$STATELESS/xdg" ]
rm -rf "$STATELESS"

for command in login command cmd run daemon config watch version; do
  ./bin/karvi-linux-amd64 "$command" --help >/dev/null
done

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT HUP INT TERM
# Config validation checks explicit executable mappings. Supply an isolated
# executable named ssh so the examples remain independent of the build host.
install -d -m 700 "$TMP/bin"
cat >"$TMP/bin/ssh" <<'SH'
#!/bin/sh
exit 0
SH
chmod 755 "$TMP/bin/ssh"
PATH="$TMP/bin:$PATH" ./bin/karvi-linux-amd64 config validate configs/development.toml >/dev/null
PATH="$TMP/bin:$PATH" ./bin/karvi-linux-amd64 config validate configs/example.toml >/dev/null

# Removed settings must fail as unknown keys. Breaking configuration changes
# are deliberate unless a future release explicitly documents an alias.
for removed in   'output.command-headers=true'   'ssh.binary="ssh"'   'login.transport="system"'   'command.transport="system"'   'run.transport="native"'   'ssh.control-master="auto"'; do
  set +e
  ./bin/karvi-linux-amd64 --set "$removed" config show >/dev/null 2>"$TMP/removed.err"
  removed_code=$?
  set -e
  [ "$removed_code" -eq 2 ]
  grep -q 'config_unknown_key' "$TMP/removed.err"
done

echo 'shipped executables: pass (bytes, counters, transports, helps, examples, removed keys)'
