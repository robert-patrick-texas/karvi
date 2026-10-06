#!/bin/bash
# Replay the current client's lifecycle operations against a daemon launched
# by an older karvi executable. Release-engineering
# evidence: run with the
# executable from the newest release whose daemon IPC schema differs from
# the new client's, and save the output beside the release evidence as
# <old>-to-<new>-lifecycle.log. The bundle verifier does not depend on it.
# A same-schema daemon is simply compatible and proves nothing here, so the
# script refuses one before it starts anything (v0.12.0 kept v0.11.0's
# schema 7 and replays against v0.10.0, schema 6).
#
#   scripts/daemon-lifecycle-replay.sh /path/to/prior-release/bin/karvi-linux-amd64
#
# Every step must print what the assertions below expect, or the script fails.
set -euo pipefail
cd "$(dirname "$0")/.."
OLD=${1:?usage: daemon-lifecycle-replay.sh OLD_KARVI_EXECUTABLE}
NEW=${KARVI:-./bin/karvi-linux-amd64}
[ -x "$OLD" ] || { echo "old executable $OLD is not executable" >&2; exit 2; }
[ -x "$NEW" ] || { echo "build the executable first: make build" >&2; exit 2; }
TMP=$(mktemp -d); BASE=$TMP/state; H=$TMP/home
. ./scripts/lib/host.sh; host_trust_store "$TMP"   # the trust store under the work directory, never the operator's
install -d -m 700 "$BASE" "$H"
A=(--set "basedir=\"$BASE\"" --set "scoreboards=\"$BASE/scoreboards\"" --set audit.journald-required=false)
# The new executable keeps its trees under the base (sharedroot none) and
# its spool directory; the prior release
# does not know the keys.
N=("${A[@]}" --set 'sharedroot="none"' --set 'platform-resolution.default=""' --set "spooldir=\"$BASE/spool\"")
cleanup() {
  HOME=$H "$NEW" "${N[@]}" daemon stop --force >/dev/null 2>&1 || true
  HOME=$H "$OLD" "${A[@]}" daemon stop >/dev/null 2>&1 || true
  rm -rf "$TMP"
}
trap cleanup EXIT
failures=0
fail() { echo "FAIL: $*" >&2; failures=$((failures + 1)); }
pid() { HOME=$H "$NEW" "${N[@]}" daemon status 2>/dev/null | sed -n 's/^pid: //p'; }
# gone PID waits for the old daemon's process to exit: an older executable
# removes its socket as it exits, and a new daemon bound in the meantime
# would lose its socket file.
gone() {
  local i=0
  while kill -0 "$1" 2>/dev/null && [ $i -lt 100 ]; do sleep 0.05; i=$((i + 1)); done
  kill -0 "$1" 2>/dev/null && fail "$2: the old daemon process $1 is still running" || true
}
old_start() {
  HOME=$H "$OLD" "${A[@]}" daemon start >/dev/null 2>&1 || fail "$1: the old daemon did not start"
  [ -S "$BASE/socket/daemon.sock" ] || fail "$1: no socket after the old daemon started"
}
stopped() { [ ! -S "$BASE/socket/daemon.sock" ] || fail "$1: the socket remains"; }

echo "old executable: $("$OLD" version | head -1); $("$OLD" version | grep '^daemon_ipc_schema')"
echo "new executable: $("$NEW" version | head -1); $("$NEW" version | grep '^daemon_ipc_schema')"
old_schema=$("$OLD" version | sed -n 's/^daemon_ipc_schema: //p')
new_schema=$("$NEW" version | sed -n 's/^daemon_ipc_schema: //p')
[ "$old_schema" != "$new_schema" ] || { echo "the old executable has the new client's IPC schema ($new_schema); replay against an older release" >&2; exit 2; }

echo "[1] the old daemon, then the new client's status"
old_start 1
p1=$(pid)
HOME=$H "$NEW" "${N[@]}" daemon status >"$TMP/status" 2>&1 || fail "1: daemon status exit $?"
sed 's/^/    /' "$TMP/status" | grep -E '^    (status|version|daemon_ipc_schema|client_ipc_schema|compatible|active_jobs|remediation):'
grep -q "^daemon_ipc_schema: $old_schema\$" "$TMP/status" && grep -q "^client_ipc_schema: $new_schema\$" "$TMP/status" && grep -q '^compatible: false$' "$TMP/status" && grep -q '^active_jobs: 0$' "$TMP/status" && grep -q '^remediation: karvi daemon restart$' "$TMP/status" || fail "1: status fields"

echo "[2] the new client's run is refused and launches no replacement"
set +e
HOME=$H NETUSER=u NETPASS=p "$NEW" "${N[@]}" run --target 127.0.0.1 --transport system 'show clock' >"$TMP/run.out" 2>"$TMP/run.err"
rc=$?
set -e
echo "    exit $rc: $(head -1 "$TMP/run.err" | cut -c1-100)"
[ "$rc" -eq 112 ] && grep -q '^daemon_incompatible: ' "$TMP/run.err" || fail "2: run exit $rc"
[ "$(pid)" = "$p1" ] || fail "2: the refused run replaced the old daemon"

echo "[3] daemon stop with no option (the old daemon reports no active job)"
HOME=$H "$NEW" "${N[@]}" daemon stop 2>&1 | sed 's/^/    /' | tee "$TMP/stop"
grep -q "ipc_schema=$old_schema; client_schema=$new_schema" "$TMP/stop" || fail "3: the stop line does not name both schemas"
stopped 3
gone "$p1" 3

echo "[4] daemon restart replaces the old daemon with a compatible one"
old_start 4
p4=$(pid)
if ! HOME=$H "$NEW" "${N[@]}" daemon restart >"$TMP/restart" 2>&1; then
  sed 's/^/    /' "$TMP/restart"
  echo "    daemon log:"; sed 's/^/      /' "$BASE/logs/daemon.log" 2>/dev/null | tail -20
  fail "4: restart failed"
fi
sed 's/^/    /' "$TMP/restart"
grep -q "stopped incompatible daemon version=.* ipc_schema=$old_schema" "$TMP/restart" && grep -q 'daemon restarted' "$TMP/restart" || fail "4: restart lines"
HOME=$H "$NEW" "${N[@]}" daemon status >"$TMP/status4" 2>&1
sed 's/^/    /' "$TMP/status4" | grep -E '^    (status|pid|version|daemon_ipc_schema|compatible):'
grep -q "^daemon_ipc_schema: $new_schema\$" "$TMP/status4" && grep -q '^compatible: true$' "$TMP/status4" || fail "4: the new daemon is not compatible"
[ "$(pid)" != "$p4" ] || fail "4: the pid did not change"
gone "$p4" 4
p4n=$(pid)
HOME=$H "$NEW" "${N[@]}" daemon stop --force >/dev/null 2>&1 || fail "4: cleanup stop"
stopped 4
gone "$p4n" 4

for opt in --force --grace --after=2s; do
  echo "[5] daemon stop $opt against the old daemon"
  old_start "5 $opt"
  p5=$(pid)
  HOME=$H "$NEW" "${N[@]}" daemon stop "$opt" 2>&1 | sed 's/^/    /' | tee "$TMP/stop5"
  grep -q "ipc_schema=$old_schema" "$TMP/stop5" || fail "5 $opt: no stop line"
  stopped "5 $opt"
  gone "$p5" "5 $opt"
done

if [ "$failures" -ne 0 ]; then
  echo "daemon lifecycle replay: $failures failure(s)" >&2
  exit 1
fi
echo "daemon lifecycle replay: pass (old schema $old_schema, new schema $new_schema)"
