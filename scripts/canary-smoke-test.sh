#!/bin/bash
# Canary suite. Two
# seeded canaries, the password and the enable password, flow through login,
# command, and run against a fake device that proves the password reached it,
# two more through the concurrent-client row r3, and two more through the
# sequential-job row s1;
# then secret-scan searches every artifact, log, socket directory, scoreboard,
# capacity file, and captured stream in every encoding a leak would take.
# Row r2 scans the auto-launched daemon's process.
# Row s1 starts a second daemon from an environment holding no credential and
# runs two later jobs through it, each with its own canary.
set -eu
ROOT=$(cd "$(dirname "$0")/.." && pwd)
KARVI=${KARVI:-$ROOT/bin/karvi-linux-amd64}
. "$ROOT/scripts/lib/json.sh"
SCAN=${SECRET_SCAN:-$ROOT/bin/secret-scan}
[ -x "$SCAN" ] || { echo "canary-smoke: $SCAN is missing; run make tools-build" >&2; exit 2; }
TMP=$(mktemp -d)
. "$ROOT/scripts/lib/host.sh"; host_trust_store "$TMP"   # the trust store under the work directory, never the operator's
BASE=$TMP/state
SCORE=$TMP/scoreboards
CAP=$TMP/capacity
HOME_DIR=$TMP/home
FAKE=$TMP/fake-ssh
BASE_S1=$TMP/state-s1
cleanup() {
  "$KARVI" --set "basedir=\"$BASE\"" --set 'sharedroot="none"' --set "spooldir=\"$BASE/spool\"" --set 'platform-resolution.default=""' --set "daemon.socket=\"$BASE/socket/daemon.sock\"" daemon stop >/dev/null 2>&1 || true
  "$KARVI" --set "basedir=\"$BASE_S1\"" daemon stop --force >/dev/null 2>&1 || true
  rm -rf "$TMP"
}
trap cleanup EXIT HUP INT TERM
install -d -m 700 "$BASE" "$BASE_S1" "$SCORE" "$CAP" "$HOME_DIR" "$TMP/streams"

seed() { printf 'ndcanary-%s' "$(tr -dc '0-9abcdefghjkmnpqrstvwxyz' </dev/urandom | head -c 20)"; }
PASS=$(seed)
ENABLE=$(seed)
# The canaries reach karvi only as NETPASS and NETENABLE on the invocations
# below, never as exported variables of this shell.
DPASS=$(printf '%s' "$PASS" | sha256sum | awk '{print $1}')

cat >"$FAKE" <<'SH_INNER'
#!/bin/sh
# The fake device: one interactive shell per session (the device session
# of karvi). Every session authenticates through askpass and records
# its first command beside the SHA-256 of the answer, so the rows prove
# which password each session received; every command is logged and held
# a second so concurrent jobs overlap.
args=" $* "
case "$args" in *" -O check "*) exit 1 ;; esac
actual=""
if [ -n "$SSH_ASKPASS" ]; then
  answer=$("$SSH_ASKPASS" 'Password:') || exit 91
  actual=$(printf '%s' "$answer" | sha256sum | awk '{print $1}')
  unset answer
fi
first=1
printf 'canary-device#'
while IFS= read -r line; do
  [ "$line" = exit ] && exit 0
  if [ "$first" = 1 ] && [ -n "$actual" ]; then
    printf 'AUTH:%s DIGEST:%s\n' "$line" "$actual" >>"$0.log"
    first=0
  fi
  printf 'SENT:%s\n' "$line" >>"$0.log"
  sleep 1
  printf '%s\r\ncanary device output\r\ncanary-device#' "$line"
done
exit 0
SH_INNER
chmod 755 "$FAKE"

common_args() {
  printf '%s\n' \
    --set "basedir=\"$BASE\"" --set 'sharedroot="none"' --set "spooldir=\"$BASE/spool\"" --set 'platform-resolution.default=""' \
    --set "ssh.transports.system=\"$FAKE\"" \
    --set audit.journald-required=false \
    --set "audit.file=\"$BASE/audit.jsonl\"" \
    --set "scoreboards=\"$SCORE\"" \
    --set "sessions.shared-capacity-root=\"$CAP\"" \
    --set output.min-free-bytes-after-job=0 \
    --set display.color=never \
    --set 'ssh.host-key-policy="insecure"'
}

failures=0
fail() { echo "FAIL: $*" >&2; failures=$((failures + 1)); }
rows=0
# row NAME EXPECTED_EXIT -- args...   streams land in $TMP/streams/NAME.{out,err}
row() {
  local name=$1 want=$2; shift 3
  rows=$((rows + 1))
  set +e
  # shellcheck disable=SC2046
  HOME=$HOME_DIR NETUSER=u NETPASS=$PASS NETENABLE=$ENABLE "$KARVI" $(common_args) "$@" \
    >"$TMP/streams/$name.out" 2>"$TMP/streams/$name.err" </dev/null
  local rc=$?
  set -e
  [ "$rc" -eq "$want" ] || fail "$name: exit $rc, want $want; stderr: $(head -1 "$TMP/streams/$name.err")"
  printf '%-4s exit %d\n' "$name" "$rc"
}

echo "canary-smoke: rows"
# command: the password flows through askpass to the fake device.
row c1 0 -- command --management-address 127.0.0.1 canary-device 'show version'
grep -q 'canary device output' "$TMP/streams/c1.out" || fail "c1: device output missing"
grep -q '^SENT:show version$' "$FAKE.log" || fail "c1: the device did not receive the command"
grep -q "DIGEST:$DPASS\$" "$FAKE.log" || fail "c1: the device did not receive the password through askpass"
# command with debug, and with the secret form --debug-show-secrets allows.
row c2 0 -- --debug command --management-address 127.0.0.1 canary-device 'show version'
grep -q 'DEBUG' "$TMP/streams/c2.err" || fail "c2: no debug output"
row c3 0 -- --debug --debug-show-secrets command --management-address 127.0.0.1 canary-device 'show version'
# run through the auto-launched daemon, with debug.
row r1 0 -- --debug run --target 127.0.0.1 --target 127.0.0.2 --transport system --dispatch parallel --workers 2 --format jsonl 'show clock'
[ "$(wc -l <"$TMP/streams/r1.out" | tr -d ' ')" -eq 3 ] || fail "r1: expected two records and the summary line"
find "$BASE/jobs" -name summary.json -type f | grep -q . || fail "r1: no job summary"
find "$BASE/jobs" -name manifest.json -type f | grep -q . || fail "r1: no job manifest"
# r2: the daemon r1 auto-launched carries no canary in its command line or
# environment. The launcher passes the child
# allow-list only.
rows=$((rows + 1))
# shellcheck disable=SC2046
pid=$(HOME=$HOME_DIR "$KARVI" $(common_args) daemon status --format json 2>/dev/null | sed -n 's/.*"pid": *\([0-9]*\).*/\1/p' | head -1)
[ -n "$pid" ] || fail "r2: daemon pid unknown"
set +e
KARVI_CANARY_PASS=$PASS KARVI_CANARY_ENABLE=$ENABLE "$SCAN" -canary-env KARVI_CANARY_PASS -canary-env KARVI_CANARY_ENABLE -proc "${pid:-0}" \
  >"$TMP/streams/r2.out" 2>"$TMP/streams/r2.err"
prc=$?
set -e
printf '%-4s exit %d (daemon pid %s)\n' r2 "$prc" "${pid:-?}"
[ "$prc" -eq 0 ] || fail "r2: secret-scan exit $prc: $(cat "$TMP/streams/r2.out" "$TMP/streams/r2.err" | head -3 | tr '\n' ' ')"
grep -q '1 processes; 0 hits' "$TMP/streams/r2.out" || fail "r2: scanner did not report one process with no hits: $(cat "$TMP/streams/r2.out")"
# r3: two simultaneous clients of one operator, each with its own canary
# password, against the same device. Every
# session must authenticate with its own job's password.
rows=$((rows + 1))
PASS_A=$(seed)
PASS_B=$(seed)
set +e
# shellcheck disable=SC2046
HOME=$HOME_DIR NETUSER=u NETPASS=$PASS_A NETENABLE=$ENABLE "$KARVI" $(common_args) --debug run --target 127.0.0.1 --transport system --format jsonl 'show a' \
  >"$TMP/streams/r3a.out" 2>"$TMP/streams/r3a.err" </dev/null &
pa=$!
# shellcheck disable=SC2046
HOME=$HOME_DIR NETUSER=u NETPASS=$PASS_B NETENABLE=$ENABLE "$KARVI" $(common_args) --debug run --target 127.0.0.1 --transport system --format jsonl 'show b' \
  >"$TMP/streams/r3b.out" 2>"$TMP/streams/r3b.err" </dev/null &
pb=$!
wait "$pa"; ra=$?
wait "$pb"; rb=$?
set -e
printf '%-4s exit %d/%d\n' r3 "$ra" "$rb"
[ "$ra" -eq 0 ] && [ "$rb" -eq 0 ] || fail "r3: exits $ra/$rb; $(head -1 "$TMP/streams/r3a.err") $(head -1 "$TMP/streams/r3b.err")"
da=$(printf '%s' "$PASS_A" | sha256sum | awk '{print $1}')
db=$(printf '%s' "$PASS_B" | sha256sum | awk '{print $1}')
[ "$(grep -c "^AUTH:show a DIGEST:$da\$" "$FAKE.log")" -eq 1 ] || fail "r3: job a did not authenticate with its own password"
[ "$(grep -c "^AUTH:show b DIGEST:$db\$" "$FAKE.log")" -eq 1 ] || fail "r3: job b did not authenticate with its own password"
if grep -Eq "^AUTH:show a DIGEST:$db|^AUTH:show b DIGEST:$da" "$FAKE.log"; then fail "r3: a job authenticated with the other job's password"; fi
grep -q '^SENT:show a$' "$FAKE.log" && grep -q '^SENT:show b$' "$FAKE.log" || fail "r3: a job's command did not reach the device"
[ "$(find "$BASE/jobs" -name manifest.json -type f | wc -l | tr -d ' ')" -ge 3 ] || fail "r3: expected three job manifests"
# s1: a daemon started without device credentials executes later jobs with
# each submitting client's current credentials (the open defect of
# v0.9.1 fixed in Stages C and D). The daemon runs on its own state tree and
# starts from an environment holding only PATH and HOME, before the two
# canaries of this row exist; job a then job b run through it sequentially,
# the device records each job's own password, the daemon is the same
# process throughout, and its command line and environment hold neither.
rows=$((rows + 1))
# s1's daemon runs under env -i (its environment is HOME and PATH only, the
# row's own assertion), so the trust store's setting rides as an argument
# here, not through host_trust_store's variable.
s1_args() { common_args | sed "s|basedir=\"$BASE\"|basedir=\"$BASE_S1\"|; s|audit.file=\"$BASE/|audit.file=\"$BASE_S1/|"; printf '%s\n' --set "ssh.known-hosts-file=\"$BASE_S1/known_hosts\""; }
set +e
# shellcheck disable=SC2046
env -i PATH="$PATH" HOME="$HOME_DIR" "$KARVI" $(s1_args) daemon start >"$TMP/streams/s1-start.out" 2>"$TMP/streams/s1-start.err"
src=$?
set -e
[ "$src" -eq 0 ] || fail "s1: daemon start exit $src: $(head -1 "$TMP/streams/s1-start.err")"
# shellcheck disable=SC2046
s1_pid=$(HOME=$HOME_DIR "$KARVI" $(s1_args) daemon status --format json 2>/dev/null | sed -n 's/.*"pid": *\([0-9]*\).*/\1/p' | head -1)
[ -n "$s1_pid" ] || fail "s1: daemon pid unknown"
s1_env=$(tr '\0' '\n' <"/proc/${s1_pid:-0}/environ" 2>/dev/null | cut -d= -f1 | sort | tr '\n' ' ')
[ "$s1_env" = "HOME PATH " ] || fail "s1: the daemon's environment is not HOME and PATH only: $s1_env"
PASS_A2=$(seed)
PASS_B2=$(seed)
set +e
# shellcheck disable=SC2046
HOME=$HOME_DIR NETUSER=u NETPASS=$PASS_A2 NETENABLE=$ENABLE "$KARVI" $(s1_args) run --target 127.0.0.1 --transport system --format jsonl 'show s1a' \
  >"$TMP/streams/s1a.out" 2>"$TMP/streams/s1a.err" </dev/null
ra=$?
# shellcheck disable=SC2046
HOME=$HOME_DIR NETUSER=u NETPASS=$PASS_B2 NETENABLE=$ENABLE "$KARVI" $(s1_args) run --target 127.0.0.1 --transport system --format jsonl 'show s1b' \
  >"$TMP/streams/s1b.out" 2>"$TMP/streams/s1b.err" </dev/null
rb=$?
set -e
printf '%-4s exit %d/%d (daemon pid %s)\n' s1 "$ra" "$rb" "${s1_pid:-?}"
[ "$ra" -eq 0 ] && [ "$rb" -eq 0 ] || fail "s1: exits $ra/$rb; $(head -1 "$TMP/streams/s1a.err") $(head -1 "$TMP/streams/s1b.err")"
da2=$(printf '%s' "$PASS_A2" | sha256sum | awk '{print $1}')
db2=$(printf '%s' "$PASS_B2" | sha256sum | awk '{print $1}')
[ "$(grep -c "^AUTH:show s1a DIGEST:$da2\$" "$FAKE.log")" -eq 1 ] || fail "s1: job a did not authenticate with its own password"
[ "$(grep -c "^AUTH:show s1b DIGEST:$db2\$" "$FAKE.log")" -eq 1 ] || fail "s1: job b did not authenticate with its own password"
if grep -Eq "^AUTH:show s1b DIGEST:$da2|^AUTH:show s1a DIGEST:$db2" "$FAKE.log"; then fail "s1: a job authenticated with the other job's password"; fi
grep -q '"status":"succeeded"' "$TMP/streams/s1a.out" && grep -q '"status":"succeeded"' "$TMP/streams/s1b.out" || fail "s1: a job did not succeed"
# shellcheck disable=SC2046
s1_pid_after=$(HOME=$HOME_DIR "$KARVI" $(s1_args) daemon status --format json 2>/dev/null | sed -n 's/.*"pid": *\([0-9]*\).*/\1/p' | head -1)
[ "$s1_pid_after" = "$s1_pid" ] || fail "s1: the daemon was replaced (pid $s1_pid then ${s1_pid_after:-none})"
[ "$(find "$BASE_S1/jobs" -name manifest.json -type f | wc -l | tr -d ' ')" -eq 2 ] || fail "s1: expected two job manifests on the daemon's own state tree"
set +e
KARVI_CANARY_A2=$PASS_A2 KARVI_CANARY_B2=$PASS_B2 "$SCAN" -canary-env KARVI_CANARY_A2 -canary-env KARVI_CANARY_B2 -proc "${s1_pid:-0}" \
  >"$TMP/streams/s1-scan.out" 2>"$TMP/streams/s1-scan.err"
prc=$?
set -e
[ "$prc" -eq 0 ] || fail "s1: secret-scan exit $prc: $(cat "$TMP/streams/s1-scan.out" "$TMP/streams/s1-scan.err" | head -3 | tr '\n' ' ')"
grep -q '1 processes; 0 hits' "$TMP/streams/s1-scan.out" || fail "s1: scanner did not report one process with no hits: $(cat "$TMP/streams/s1-scan.out")"
# d1: a dry-run sends nothing: no prepare,
# frame, or commit reaches the daemon, no job directory appears, the fake
# device sees no session, and the report says so in every field.
before_log=$(wc -l <"$FAKE.log" | tr -d ' ')
before_manifests=$(find "$BASE/jobs" -name manifest.json -type f | wc -l | tr -d ' ')
before_ops=$(grep -Ec 'operation=(prepare_job|commit_job|provide_credentials)' "$BASE/logs/daemon.log" || true)
row d1 0 -- run --dry-run --target 127.0.0.1 --target 127.0.0.2 --transport system --format json 'show clock'
grep -q '"kind": "inspection"' "$TMP/streams/d1.out" || fail "d1: not an inspection report"
grep -q '"outcome": "planned"' "$TMP/streams/d1.out" || fail "d1: outcome is not planned"
grep -q '"job_submitted": false' "$TMP/streams/d1.out" || fail "d1: job_submitted is not false"
grep -q '"target_data_submitted": false' "$TMP/streams/d1.out" || fail "d1: target_data_submitted is not false"
grep -q '"device_contacted": false' "$TMP/streams/d1.out" || fail "d1: device_contacted is not false"
grep -q '"status": "running"' "$TMP/streams/d1.out" || fail "d1: the running daemon was not reported"
[ "$(wc -l <"$FAKE.log" | tr -d ' ')" -eq "$before_log" ] || fail "d1: the fake device saw a session"
[ "$(find "$BASE/jobs" -name manifest.json -type f | wc -l | tr -d ' ')" -eq "$before_manifests" ] || fail "d1: a job directory appeared"
after_ops=$(grep -Ec 'operation=(prepare_job|commit_job|provide_credentials)' "$BASE/logs/daemon.log" || true)
[ "${after_ops:-0}" -eq "${before_ops:-0}" ] || fail "d1: the daemon log gained a job operation ($before_ops -> $after_ops)"
# e1: an exercise submits the job and the package and contacts no device
# one new job directory with an
# exercise report and an empty commands.jsonl, no fake-device session, no
# askpass invocation, and the report says so in every field.
before_log=$(wc -l <"$FAKE.log" | tr -d ' ')
before_manifests=$(find "$BASE/jobs" -name manifest.json -type f | wc -l | tr -d ' ')
row e1 0 -- run --exercise --target 127.0.0.1 --target 127.0.0.2 --transport system --format json 'show clock'
grep -q '"kind": "exercise"' "$TMP/streams/e1.out" || fail "e1: not an exercise report"
grep -q '"outcome": "exercised"' "$TMP/streams/e1.out" || fail "e1: outcome is not exercised"
grep -q '"job_submitted": true' "$TMP/streams/e1.out" || fail "e1: job_submitted is not true"
grep -q '"device_contacted": false' "$TMP/streams/e1.out" || fail "e1: device_contacted is not false"
grep -q '"readiness": "ready"' "$TMP/streams/e1.out" || fail "e1: no target is ready"
# The report carries the outcome; a run prints no result line.
[ "$(wc -l <"$FAKE.log" | tr -d ' ')" -eq "$before_log" ] || fail "e1: the fake device saw a session"
[ "$(find "$BASE/jobs" -name manifest.json -type f | wc -l | tr -d ' ')" -eq $((before_manifests + 1)) ] || fail "e1: expected one new job directory"
e1_dir=$(find "$BASE/jobs" -name exercise.json -type f -newer "$TMP/streams/d1.out" | head -1)
[ -n "$e1_dir" ] || fail "e1: no exercise.json was written"
[ ! -s "$(dirname "$e1_dir")/commands.jsonl" ] || fail "e1: commands.jsonl is not empty"
json_is "$(dirname "$e1_dir")/summary.json" final_status exercised || fail "e1: the summary is not exercised"
grep -q '"event_name":"run.exercised"' "$BASE/audit.jsonl" || fail "e1: no run.exercised audit record"
# p1–p3: the two-probe ICMP gate with the
# real pinger against one loopback target and one TEST-NET-1 target
# (192.0.2.1, RFC 5737, never answered). The rows need an ICMP method on
# this host, the ping socket or the system ping; when karvi version reports
# neither they are skipped with the reason, so the bundle verifier passes
# on a host without capability and the gate is claimed from a host with one.
icmp_method=$("$KARVI" version --format json 2>/dev/null | sed -n 's/.*"icmp_method": *"\([a-z]*\)".*/\1/p')
if [ "$icmp_method" = socket ] || [ "$icmp_method" = system ]; then
  before_auth=$(grep -c '^AUTH:' "$FAKE.log" || true)
  before_manifests=$(find "$BASE/jobs" -name manifest.json -type f | wc -l | tr -d ' ')
  row p1 101 -- run --ping --target 127.0.0.1 --target 192.0.2.1 --transport system --format jsonl 'show clock'
  [ "$(wc -l <"$TMP/streams/p1.out" | tr -d ' ')" -eq 3 ] || fail "p1: expected two records and the summary line"
  grep '"selected_address":"192.0.2.1"' "$TMP/streams/p1.out" | grep -q '"status":"icmp_unreachable"' || fail "p1: the unreachable target was not skipped as icmp_unreachable"
  grep '"selected_address":"192.0.2.1"' "$TMP/streams/p1.out" | grep -q '"decision":"skip"' || fail "p1: the skipped record's ping decision is not skip"
  grep '"selected_address":"127.0.0.1"' "$TMP/streams/p1.out" | grep -q '"status":"succeeded"' || fail "p1: the reachable target did not succeed"
  grep '"selected_address":"127.0.0.1"' "$TMP/streams/p1.out" | grep -q '"decision":"proceed"' || fail "p1: the reachable record's ping decision is not proceed"
  grep -q "\"method\":\"$icmp_method\"" "$TMP/streams/p1.out" || fail "p1: the records do not name the method $icmp_method"
  [ "$(grep -c '^AUTH:' "$FAKE.log" || true)" -eq $((before_auth + 1)) ] || fail "p1: expected exactly one new session at the fake device"
  p1_summary=$(find "$BASE/jobs" -name summary.json -type f -newer "$TMP/streams/e1.out" | head -1)
  [ -n "$p1_summary" ] || fail "p1: no summary"
  json_is "$p1_summary" ping.devices.gated 2 && json_is "$p1_summary" ping.devices.skipped 1 && json_is "$p1_summary" ping.devices.proceeded 1 || fail "p1: the summary's ping block does not count one skip and one proceed"
  # The gate's own duration is the ping object's total_ns (the device's
  # timing.total_ns also covers queueing and the failure set).
  skip_ns=$(grep '"selected_address":"192.0.2.1"' "$TMP/streams/p1.out" | grep -o '"decision":"skip","total_ns":[0-9]*' | sed 's/.*://' | head -1)
  [ "${skip_ns:-0}" -gt 0 ] && [ "$skip_ns" -le 2000000000 ] || fail "p1: the skipped gate took ${skip_ns:-?} ns, want within two timeouts plus a second"
  [ "$(find "$BASE/jobs" -name manifest.json -type f | wc -l | tr -d ' ')" -eq $((before_manifests + 1)) ] || fail "p1: expected one new job directory"
  # p2: login with the gate against the unreachable target exits 110 before
  # any transport; the fake device sees no session.
  before_auth=$(grep -c '^AUTH:' "$FAKE.log" || true)
  row p2 110 -- login --ping --management-address 192.0.2.1 canary-device
  grep -q 'ping(1) timeout, ping(2) timeout, skipped' "$TMP/streams/p2.err" || fail "p2: no one-line gate result"
  grep -q '^icmp_unreachable: ' "$TMP/streams/p2.err" || fail "p2: no icmp_unreachable failure"
  [ "$(grep -c '^AUTH:' "$FAKE.log" || true)" -eq "$before_auth" ] || fail "p2: the fake device saw a session for a skipped target"
  # p3: both flags are refused before anything runs.
  row p3 4 -- command --ping --noping --management-address 127.0.0.1 canary-device 'show version'
  grep -q '^ping_flag_conflict: ' "$TMP/streams/p3.err" || fail "p3: no ping_flag_conflict"
else
  echo "p1-p3 SKIP: no ICMP method on this host (icmp_method=${icmp_method:-unknown}); widen net.ipv4.ping_group_range or install ping"
fi
# login, recorded, under a pseudo-terminal; "exit" typed at the prompt ends
# the session so the transcript pair is complete.
rows=$((rows + 1))
set +e
# shellcheck disable=SC2046
{ sleep 1; printf 'exit\n'; } | HOME=$HOME_DIR NETUSER=u NETPASS=$PASS NETENABLE=$ENABLE timeout 20s script -qefc \
  "$KARVI $(common_args | tr '\n' ' ') --debug login --record --management-address 127.0.0.1 canary-device" /dev/null \
  >"$TMP/streams/l1.out" 2>"$TMP/streams/l1.err"
lrc=$?
set -e
printf '%-4s exit %d\n' l1 "$lrc"
[ "$lrc" -eq 0 ] || fail "l1: exit $lrc, want 0; stderr: $(head -1 "$TMP/streams/l1.err")"
[ "$(find "$BASE/transcripts" -type f | wc -l | tr -d ' ')" -ge 2 ] || fail "l1: transcript pair missing"

# Everything the run touched, in every encoding.
echo "canary-smoke: scan"
set +e
KARVI_CANARY_PASS=$PASS KARVI_CANARY_ENABLE=$ENABLE KARVI_CANARY_A=$PASS_A KARVI_CANARY_B=$PASS_B KARVI_CANARY_A2=$PASS_A2 KARVI_CANARY_B2=$PASS_B2 "$SCAN" \
  -canary-env KARVI_CANARY_PASS -canary-env KARVI_CANARY_ENABLE -canary-env KARVI_CANARY_A -canary-env KARVI_CANARY_B -canary-env KARVI_CANARY_A2 -canary-env KARVI_CANARY_B2 \
  "$BASE" "$BASE_S1" "$SCORE" "$CAP" "$FAKE.log" "$TMP/streams" | tee "$TMP/scan.txt"
src=${PIPESTATUS[0]}
set -e
[ "$src" -eq 0 ] || fail "secret-scan exit $src"
grep -q '^scanned: ' "$TMP/scan.txt" || fail "scanner reported no summary"
files=$(sed -n 's/^scanned: \([0-9]*\) files.*/\1/p' "$TMP/scan.txt")
[ "${files:-0}" -ge 8 ] || fail "scanner saw only ${files:-0} files"
# The password did flow: the fake device accepted it and the audit names the user.
grep -q '"device_username":"u"' "$BASE/audit.jsonl" 2>/dev/null || grep -q '"username":"u"' "$BASE/audit.jsonl" || fail "audit lacks the device username"

if [ "$failures" -ne 0 ]; then
  echo "canary-smoke: $failures failure(s) over $rows rows" >&2
  exit 1
fi
echo "canary-smoke: pass ($rows rows, $files files scanned, 0 hits)"
