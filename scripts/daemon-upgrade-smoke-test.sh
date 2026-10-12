#!/bin/sh
# Verify recovery from a still-running released daemon that uses IPC schema 2,
# and from one at this executable's schema and version but another build: a
# daemon is compatible only when its IPC schema and its build both equal the
# client's (docs/EXAMPLES.md chapter 57, issues 1 and 2).
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"
KARVI=${KARVI:-./bin/karvi-linux-amd64}
TMP=$(mktemp -d "${TMPDIR:-/tmp}/karvi-daemon-upgrade-smoke-XXXXXX")
. "$ROOT/scripts/lib/host.sh"; host_trust_store "$TMP"   # the trust store under the work directory, never the operator's
BASE="$TMP/state"
HOME_DIR="$TMP/home"
SOCKET="$BASE/socket/d.sock"
FIXTURE_PID=
cleanup() {
  "$KARVI" --quiet --set "basedir=\"$BASE\"" --set 'sharedroot="none"' --set "spooldir=\"$BASE/spool\"" --set 'platform-resolution.default=""' daemon stop >/dev/null 2>&1 || true
  if [ -n "$FIXTURE_PID" ]; then
    kill "$FIXTURE_PID" >/dev/null 2>&1 || true
    wait "$FIXTURE_PID" >/dev/null 2>&1 || true
  fi
  rm -rf "$TMP"
}
trap cleanup EXIT HUP INT TERM
install -d -m 700 "$BASE" "$HOME_DIR"
# The executable's own version, IPC schema, and build, so the rule holds for
# any build; OTHER is a build of another program.
VERSION=$("$KARVI" version | sed -n '1s/^karvi //p')
SCHEMA=$("$KARVI" version | sed -n 's/^daemon_ipc_schema: //p')
BUILD=$("$KARVI" version | sed -n 's/^build: //p')
OTHER=9Au3WHU839F5xE-67H93
[ -n "$VERSION" ] && [ -n "$SCHEMA" ] && [ -n "$BUILD" ] && [ "$BUILD" != none ]

# start_fixture SCHEMA VERSION [BUILD]: a lifecycle-only daemon fixture at the
# socket, naming BUILD, or no build.
start_fixture() {
  GOTOOLCHAIN=local go run -mod=vendor ./tools/daemon-fixture \
    --socket "$SOCKET" --schema "$1" --version "$2" --build "${3:-}" \
    >"$TMP/fixture.out" 2>"$TMP/fixture.err" &
  FIXTURE_PID=$!
  i=0
  while [ ! -S "$SOCKET" ]; do
    i=$((i + 1))
    if [ "$i" -gt 100 ]; then
      cat "$TMP/fixture.err" >&2 || true
      echo "schema-$1 fixture did not start" >&2
      exit 1
    fi
    sleep 0.02
  done
}

start_fixture 2 0.8.0
"$KARVI" --set "basedir=\"$BASE\"" --set 'sharedroot="none"' --set "spooldir=\"$BASE/spool\"" --set 'platform-resolution.default=""' daemon status >"$TMP/status.out"
grep -q '^daemon_version: 0.8.0$' "$TMP/status.out"
grep -q '^daemon_ipc_schema: 2$' "$TMP/status.out"
grep -q "^client_ipc_schema: $SCHEMA\$" "$TMP/status.out"
grep -q '^compatible: false$' "$TMP/status.out"
grep -q '^remediation: karvi daemon restart$' "$TMP/status.out"

set +e
HOME="$HOME_DIR" NETUSER=smoke NETPASS=not-a-secret \
"$KARVI" --quiet \
  --set "basedir=\"$BASE\"" --set 'sharedroot="none"' --set "spooldir=\"$BASE/spool\"" --set 'platform-resolution.default=""' \
  --set audit.journald-required=false \
  --set "audit.file=\"$BASE/audit.jsonl\"" \
  --set "scoreboards=\"$TMP/scoreboards\"" \
  --set "sessions.shared-capacity-root=\"$TMP/capacity\"" \
  --set output.min-free-bytes-after-job=0 \
  run --target 127.0.0.1 --transport system --command true \
  >"$TMP/run.out" 2>"$TMP/run.err"
code=$?
set -e
[ "$code" -eq 112 ]
grep -q 'daemon_incompatible' "$TMP/run.err"
grep -q 'version 0.8.0' "$TMP/run.err"
grep -q 'run "karvi daemon restart"' "$TMP/run.err"
[ ! -e "$BASE/logs/daemon.log" ]

# The build rule: a daemon at this executable's schema and version but of
# another build is reached and reported, incompatible, its run refused before
# any job, and the restart replaces it.
"$KARVI" --set "basedir=\"$BASE\"" --set 'sharedroot="none"' --set "spooldir=\"$BASE/spool\"" --set 'platform-resolution.default=""' daemon stop >/dev/null
wait "$FIXTURE_PID"
FIXTURE_PID=
start_fixture "$SCHEMA" "$VERSION" "$OTHER"
"$KARVI" --set "basedir=\"$BASE\"" --set 'sharedroot="none"' --set "spooldir=\"$BASE/spool\"" --set 'platform-resolution.default=""' daemon status >"$TMP/status10.out"
grep -q "^daemon_version: $VERSION\$" "$TMP/status10.out"
grep -q "^daemon_build: $OTHER\$" "$TMP/status10.out"
grep -q "^client_build: $BUILD\$" "$TMP/status10.out"
grep -q "^daemon_ipc_schema: $SCHEMA\$" "$TMP/status10.out"
grep -q "^client_ipc_schema: $SCHEMA\$" "$TMP/status10.out"
grep -q '^compatible: false$' "$TMP/status10.out"
grep -q '^remediation: karvi daemon restart$' "$TMP/status10.out"
set +e
HOME="$HOME_DIR" NETUSER=smoke NETPASS=not-a-secret \
"$KARVI" --quiet \
  --set "basedir=\"$BASE\"" --set 'sharedroot="none"' --set "spooldir=\"$BASE/spool\"" --set 'platform-resolution.default=""' \
  --set audit.journald-required=false \
  --set "audit.file=\"$BASE/audit.jsonl\"" \
  --set "scoreboards=\"$TMP/scoreboards\"" \
  --set "sessions.shared-capacity-root=\"$TMP/capacity\"" \
  --set output.min-free-bytes-after-job=0 \
  run --target 127.0.0.1 --transport system --command true \
  >"$TMP/run10.out" 2>"$TMP/run10.err"
code=$?
set -e
[ "$code" -eq 112 ]
grep -q "^daemon_incompatible: running daemon version $VERSION build $OTHER uses IPC schema $SCHEMA; karvi $VERSION build $BUILD uses schema $SCHEMA, and the schema and the build must both match" "$TMP/run10.err"
[ ! -e "$BASE/logs/daemon.log" ]

"$KARVI" --set "basedir=\"$BASE\"" --set 'sharedroot="none"' --set "spooldir=\"$BASE/spool\"" --set 'platform-resolution.default=""' daemon restart >"$TMP/restart.out" 2>"$TMP/restart.err"
grep -q '^daemon restarted$' "$TMP/restart.out"
wait "$FIXTURE_PID"
FIXTURE_PID=
"$KARVI" --set "basedir=\"$BASE\"" --set 'sharedroot="none"' --set "spooldir=\"$BASE/spool\"" --set 'platform-resolution.default=""' daemon status >"$TMP/current.out"
grep -q "^daemon_version: $VERSION\$" "$TMP/current.out"
grep -q "^daemon_build: $BUILD\$" "$TMP/current.out"
grep -q "^daemon_ipc_schema: $SCHEMA\$" "$TMP/current.out"
grep -q '^compatible: true$' "$TMP/current.out"
"$KARVI" --set "basedir=\"$BASE\"" --set 'sharedroot="none"' --set "spooldir=\"$BASE/spool\"" --set 'platform-resolution.default=""' daemon stop >/dev/null

echo 'daemon upgrade smoke test: pass'
