#!/bin/sh
# Verify the client's sequence beside a daemon of another build
# (docs/EXAMPLES.md chapter 57, issues 1, 2, and 5): a daemon of an older IPC
# schema, and one at this executable's schema and version but another build,
# is reported incompatible; holding no job, a run drains it, waits for it to
# exit, and starts a daemon of this build in the same invocation; holding a
# job, it is drained and the run refused, daemon_draining, the daemon's build
# over the client's, and no daemon is started beside it; daemon stop and
# daemon restart reach it at its own schema. Each run is an exercise to
# 192.0.2.1, which reaches no device.
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
# k ARGS: the executable under the suite's places.
k() {
  "$KARVI" --set "basedir=\"$BASE\"" --set 'sharedroot="none"' --set "spooldir=\"$BASE/spool\"" \
    --set "tempdir=\"$TMP/tmp\"" --set 'platform-resolution.default=""' \
    --set audit.journald-required=false --set "audit.file=\"$BASE/audit.jsonl\"" \
    --set "scoreboards=\"$TMP/scoreboards\"" --set "sessions.shared-capacity-root=\"$TMP/capacity\"" \
    --set output.min-free-bytes-after-job=0 "$@"
}
cleanup() {
  k --quiet daemon stop --force >/dev/null 2>&1 || true
  if [ -n "$FIXTURE_PID" ]; then
    kill "$FIXTURE_PID" >/dev/null 2>&1 || true
    wait "$FIXTURE_PID" >/dev/null 2>&1 || true
  fi
  rm -rf "$TMP"
}
trap cleanup EXIT HUP INT TERM
install -d -m 700 "$BASE" "$HOME_DIR" "$TMP/tmp"
# The executable's own version, IPC schema, and build, so the rule holds for
# any build; OTHER is a build of another program.
VERSION=$("$KARVI" version | sed -n '1s/^karvi //p')
SCHEMA=$("$KARVI" version | sed -n 's/^daemon_ipc_schema: //p')
BUILD=$("$KARVI" version | sed -n 's/^build: //p')
OTHER=9Au3WHU839F5xE-67H93
[ -n "$VERSION" ] && [ -n "$SCHEMA" ] && [ -n "$BUILD" ] && [ "$BUILD" != none ]

# start_fixture SCHEMA VERSION [BUILD [JOBS]]: a lifecycle-only daemon
# fixture at the socket, naming BUILD, or no build, and holding JOBS.
start_fixture() {
  GOTOOLCHAIN=local go run -mod=vendor ./tools/daemon-fixture \
    --socket "$SOCKET" --schema "$1" --version "$2" --build "${3:-}" --jobs "${4:-0}" \
    >"$TMP/fixture.out" 2>"$TMP/fixture.err" &
  FIXTURE_PID=$!
  sf_i=0
  while [ ! -S "$SOCKET" ]; do
    sf_i=$((sf_i + 1))
    if [ "$sf_i" -gt 100 ]; then
      cat "$TMP/fixture.err" >&2 || true
      echo "schema-$1 fixture did not start" >&2
      exit 1
    fi
    sleep 0.02
  done
}
# fixture_gone: the fixture has exited.
fixture_gone() {
  wait "$FIXTURE_PID"
  FIXTURE_PID=
}
# exercise NAME: a run's exercise to 192.0.2.1, its exit in code, its output
# in NAME.out and NAME.err.
exercise() {
  set +e
  HOME="$HOME_DIR" NETUSER=smoke NETPASS=not-a-secret \
    k run --exercise --target 192.0.2.1 --transport system 'show clock' \
    >"$TMP/$1.out" 2>"$TMP/$1.err"
  code=$?
  set -e
}
# replaced NAME DAEMON_VERSION DAEMON_BUILD: the run NAME drained the
# fixture, which exited, and ran on a daemon of this build that it started,
# saying both.
replaced() {
  [ "$code" -eq 0 ] || { cat "$TMP/$1.err" >&2; exit 1; }
  [ "$(cat "$TMP/$1.err")" = "daemon drained version=$2 build=$3
daemon started socket=$SOCKET" ]
  fixture_gone
  grep -q "^daemon local: running pid=[0-9]* version=$VERSION build=$BUILD ipc_schema=$SCHEMA " "$TMP/$1.out"
  k daemon status >"$TMP/$1.status"
  grep -q "^daemon_build: $BUILD\$" "$TMP/$1.status"
  grep -q '^compatible: true$' "$TMP/$1.status"
  k daemon stop >/dev/null
}

# 1. An idle daemon of an older IPC schema: reported incompatible, then
# drained and replaced by a run.
start_fixture 2 0.8.0
k daemon status >"$TMP/status.out"
grep -q '^daemon_version: 0.8.0$' "$TMP/status.out"
grep -q '^daemon_ipc_schema: 2$' "$TMP/status.out"
grep -q "^client_ipc_schema: $SCHEMA\$" "$TMP/status.out"
grep -q '^compatible: false$' "$TMP/status.out"
grep -q '^remediation: karvi daemon restart$' "$TMP/status.out"
exercise run
replaced run 0.8.0 none

# 2. An idle daemon at this schema and version but of another build: its
# pairs stacked, then drained and replaced by a run.
start_fixture "$SCHEMA" "$VERSION" "$OTHER"
k daemon status >"$TMP/status10.out"
grep -q "^daemon_version: $VERSION\$" "$TMP/status10.out"
grep -q "^daemon_build: $OTHER\$" "$TMP/status10.out"
grep -q "^client_build: $BUILD\$" "$TMP/status10.out"
grep -q "^daemon_ipc_schema: $SCHEMA\$" "$TMP/status10.out"
grep -q "^client_ipc_schema: $SCHEMA\$" "$TMP/status10.out"
grep -q '^compatible: false$' "$TMP/status10.out"
grep -q '^remediation: karvi daemon restart$' "$TMP/status10.out"
exercise run10
replaced run10 "$VERSION" "$OTHER"

# 3. A daemon of an older schema holding a job: drained, the run refused
# with the builds stacked, no daemon started beside it; daemon stop --force
# reaches it at its schema.
rm -f "$BASE/logs/daemon.log"
start_fixture 2 0.8.0 '' 1
exercise busy
[ "$code" -eq 112 ]
[ "$(cat "$TMP/busy.err")" = "daemon_draining: 1 job running; none submitted; retry when it exits, or --no-daemon
  daemon build none 0.8.0
  client build $BUILD $VERSION" ]
[ ! -e "$BASE/logs/daemon.log" ]
k daemon status >"$TMP/busy.status"
grep -q '^status: draining$' "$TMP/busy.status"
grep -q '^active_jobs: 1$' "$TMP/busy.status"
k daemon stop --force >"$TMP/stop.out"
grep -q "^daemon stopped (version=0.8.0 ipc_schema=2; client_schema=$SCHEMA)\$" "$TMP/stop.out"
fixture_gone

# 4. daemon restart replaces an idle daemon of another build.
start_fixture "$SCHEMA" "$VERSION" "$OTHER"
k daemon restart >"$TMP/restart.out" 2>"$TMP/restart.err"
grep -q '^daemon restarted$' "$TMP/restart.out"
fixture_gone
k daemon status >"$TMP/current.out"
grep -q "^daemon_version: $VERSION\$" "$TMP/current.out"
grep -q "^daemon_build: $BUILD\$" "$TMP/current.out"
grep -q "^daemon_ipc_schema: $SCHEMA\$" "$TMP/current.out"
grep -q '^compatible: true$' "$TMP/current.out"
k daemon stop >/dev/null

echo 'daemon upgrade smoke test: pass'
