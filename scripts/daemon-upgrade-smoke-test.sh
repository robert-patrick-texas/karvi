#!/bin/sh
# Verify recovery from a still-running released daemon that uses IPC schema 2,
# and from one at this executable's schema but another version: a daemon is
# compatible only when its version and its IPC schema both equal the
# client's.
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"
KARVI=${KARVI:-./bin/karvi-linux-amd64}
TMP=$(mktemp -d)
. "$ROOT/scripts/lib/host.sh"; host_trust_store "$TMP"   # the trust store under the work directory, never the operator's
BASE="$TMP/state"
HOME_DIR="$TMP/home"
SOCKET="$BASE/socket/daemon.sock"
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

# start_fixture SCHEMA VERSION: a lifecycle-only daemon fixture at the socket.
start_fixture() {
  GOTOOLCHAIN=local go run -mod=vendor ./tools/daemon-fixture \
    --socket "$SOCKET" --schema "$1" --version "$2" \
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
grep -q '^version: 0.8.0$' "$TMP/status.out"
grep -q '^daemon_ipc_schema: 2$' "$TMP/status.out"
grep -q '^client_ipc_schema: 10$' "$TMP/status.out"
grep -q '^compatible: false$' "$TMP/status.out"
grep -q '^remediation: karvi daemon restart$' "$TMP/status.out"

set +e
HOME="$HOME_DIR" NETUSER=smoke NETPASS=not-a-secret \
"$KARVI" --quiet \
  --set "basedir=\"$BASE\"" --set 'sharedroot="none"' --set "spooldir=\"$BASE/spool\"" --set 'platform-resolution.default=""' \
  --set audit.journald-required=false \
  --set "audit.file=\"$BASE/audit.jsonl\"" \
  --set "watch.directory=\"$TMP/scoreboards\"" \
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

# The pair rule: a daemon at this executable's schema, 10, but
# another version is reached and reported, incompatible, its run refused
# before any job, and the restart replaces it.
"$KARVI" --set "basedir=\"$BASE\"" --set 'sharedroot="none"' --set "spooldir=\"$BASE/spool\"" --set 'platform-resolution.default=""' daemon stop >/dev/null
wait "$FIXTURE_PID"
FIXTURE_PID=
start_fixture 10 0.19.0
"$KARVI" --set "basedir=\"$BASE\"" --set 'sharedroot="none"' --set "spooldir=\"$BASE/spool\"" --set 'platform-resolution.default=""' daemon status >"$TMP/status10.out"
grep -q '^version: 0.19.0$' "$TMP/status10.out"
grep -q '^daemon_ipc_schema: 10$' "$TMP/status10.out"
grep -q '^client_ipc_schema: 10$' "$TMP/status10.out"
grep -q '^compatible: false$' "$TMP/status10.out"
grep -q '^remediation: karvi daemon restart$' "$TMP/status10.out"
set +e
HOME="$HOME_DIR" NETUSER=smoke NETPASS=not-a-secret \
"$KARVI" --quiet \
  --set "basedir=\"$BASE\"" --set 'sharedroot="none"' --set "spooldir=\"$BASE/spool\"" --set 'platform-resolution.default=""' \
  --set audit.journald-required=false \
  --set "audit.file=\"$BASE/audit.jsonl\"" \
  --set "watch.directory=\"$TMP/scoreboards\"" \
  --set "sessions.shared-capacity-root=\"$TMP/capacity\"" \
  --set output.min-free-bytes-after-job=0 \
  run --target 127.0.0.1 --transport system --command true \
  >"$TMP/run10.out" 2>"$TMP/run10.err"
code=$?
set -e
[ "$code" -eq 112 ]
grep -q '^daemon_incompatible: running daemon version 0.19.0 uses IPC schema 10; karvi 0.23.0 uses schema 10, and both must match' "$TMP/run10.err"
[ ! -e "$BASE/logs/daemon.log" ]

"$KARVI" --set "basedir=\"$BASE\"" --set 'sharedroot="none"' --set "spooldir=\"$BASE/spool\"" --set 'platform-resolution.default=""' daemon restart >"$TMP/restart.out" 2>"$TMP/restart.err"
grep -q '^daemon restarted$' "$TMP/restart.out"
wait "$FIXTURE_PID"
FIXTURE_PID=
"$KARVI" --set "basedir=\"$BASE\"" --set 'sharedroot="none"' --set "spooldir=\"$BASE/spool\"" --set 'platform-resolution.default=""' daemon status >"$TMP/current.out"
grep -q '^version: 0.23.0$' "$TMP/current.out"
grep -q '^daemon_ipc_schema: 10$' "$TMP/current.out"
grep -q '^compatible: true$' "$TMP/current.out"
"$KARVI" --set "basedir=\"$BASE\"" --set 'sharedroot="none"' --set "spooldir=\"$BASE/spool\"" --set 'platform-resolution.default=""' daemon stop >/dev/null

echo 'daemon upgrade smoke test: pass'
