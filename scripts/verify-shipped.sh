#!/bin/sh
# Verify the shipped executables as bytes, in seconds and without a build:
# the version, the checksums, the file format, the links, every schema counter
# the executable reports against the release's manifest, its build, both
# transports, a stateless --help and
# version, every command's help, the configuration examples, and the removed
# keys refused. scripts/verify-bundle.sh runs this first and then the tests
# and the suites; the release evidence runs this on the shipped bytes and lets
# scripts/verify-release.sh, which rebuilds to the same checksums, carry the
# tests and the suites once.
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"

sha256sum -c CHECKSUMS.sha256
file bin/karvi-linux-amd64 | grep -q 'ELF 64-bit.*x86-64'
file bin/karvi-askpass-linux-amd64 | grep -q 'ELF 64-bit.*x86-64'
file bin/karvi-prune-linux-amd64 | grep -q 'ELF 64-bit.*x86-64'
file bin/karvi-audit-linux-amd64 | grep -q 'ELF 64-bit.*x86-64'
[ -x bin/karvi-linux-amd64 ]
[ -x bin/karvi-askpass-linux-amd64 ]
[ -x bin/karvi-prune-linux-amd64 ]
[ -x bin/karvi-audit-linux-amd64 ]
[ "$(readlink bin/karvi)" = karvi-linux-amd64 ]
[ "$(readlink bin/karvi-askpass)" = karvi-askpass-linux-amd64 ]
[ "$(readlink bin/karvi-prune)" = karvi-prune-linux-amd64 ]
[ "$(readlink bin/karvi-audit)" = karvi-audit-linux-amd64 ]
# From here karvi runs: nothing it does before the stateless check below may
# write in its places under the home of the passwd entry, which it takes in
# place of $HOME, or in the host's shared places (scripts/lib/host.sh).
. "$ROOT/scripts/lib/host.sh"
home=$(host_passwd_home)
before=$(host_places "$home")
. "$ROOT/scripts/lib/json.sh"
./bin/karvi-linux-amd64 version --format json | json_get - 'ssh_transports.*.id' | grep -qx system
./bin/karvi-linux-amd64 --version | grep -q '^  system: external-executable$'
./bin/karvi-linux-amd64 --version | grep -q '^  native: compiled-in$'
# The executable names its build, the Go build ID's last part: without it
# its daemon would refuse to start (docs/EXAMPLES.md chapter 57, issue 8).
version=$(./bin/karvi-linux-amd64 version --format json)
# The release's number is written once, in VERSION; the executable reports
# it, and the manifest records it.
want=$(cat VERSION)
for pair in "bin/karvi-linux-amd64|$(printf '%s\n' "$version" | json_get - version)" \
    "release/release-manifest.json|$(json_get release/release-manifest.json version)"; do
  [ "${pair#*|}" = "$want" ] || {
    printf 'version differs\n  %-30s %s\n  %-30s %s\n' VERSION "$want" "${pair%%|*}" "${pair#*|}" >&2
    exit 1
  }
done
build=$(printf '%s\n' "$version" | json_get - build)
[ ${#build} -eq 20 ] || { echo "bin/karvi-linux-amd64 names no build (build: $build)" >&2; exit 1; }
# Each schema counter is written once, in the package that owns its schema,
# and the executable reports it; release/release-manifest.json is the
# release's record of them (configuration_ where the executable writes
# config_), so a manifest that names another value fails here.
for key in config_schema_version config_registry_schema_version \
    command_record_schema_version daemon_ipc_schema_version \
    scoreboard_schema_version audit_schema_version job_schema_version; do
  got=$(printf '%s\n' "$version" | json_get - "$key")
  want=$(json_get release/release-manifest.json "$(echo "$key" | sed 's/^config_/configuration_/')")
  [ "$got" = "$want" ] || {
    printf 'schema counter %s differs\n  bin/karvi-linux-amd64          %s\n  release/release-manifest.json  %s\n' "$key" "$got" "$want" >&2
    exit 1
  }
done

TMP=$(mktemp -d)
cleanup() { rm -rf "$TMP"; }
. "$ROOT/scripts/lib/exit.sh"; on_exit cleanup

# --help and version write nothing: not under a fresh HOME, and nothing
# since the first run above in karvi's places or the host's shared places.
HOME="$TMP/stateless/home" XDG_CONFIG_HOME="$TMP/stateless/xdg" ./bin/karvi-linux-amd64 --help >/dev/null
HOME="$TMP/stateless/home" XDG_CONFIG_HOME="$TMP/stateless/xdg" ./bin/karvi-linux-amd64 version >/dev/null
[ ! -e "$TMP/stateless" ]
host_places_unchanged "$home" "$before"

for command in login command cmd run daemon config watch version; do
  ./bin/karvi-linux-amd64 "$command" --help >/dev/null
done

# Config validation checks explicit executable mappings. Supply an isolated
# executable named ssh so the examples remain independent of the build host.
install -d -m 700 "$TMP/bin"
cat >"$TMP/bin/ssh" <<'SH'
#!/bin/sh
exit 0
SH
chmod 755 "$TMP/bin/ssh"
PATH="$TMP/bin:$PATH" ./bin/karvi-linux-amd64 config validate configs/development.toml >/dev/null
PATH="$TMP/bin:$PATH" ./bin/karvi-linux-amd64 config validate examples/config.toml >/dev/null

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

echo 'shipped executables: pass (bytes, counters, build, transports, helps, examples, removed keys)'
