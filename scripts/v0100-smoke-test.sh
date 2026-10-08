#!/usr/bin/env bash
# The v0100 suite: the rehearsal flags, --detach, --follow=false, the
# interrupted client, the ICMP gate, the shutdown accounting, cancel_job,
# and job follow against a fake device.
# The fake device answers after the delay $TMP/slow holds, in seconds, so a
# job can be left running: 1 while a case only needs the job in flight, 3
# for the forced and expiring stops (s1 to s5), whose every record must
# still be in flight when the stop lands.
set -euo pipefail
cd "$(dirname "$0")/.."
KARVI=${KARVI:-./bin/karvi-linux-amd64}
. scripts/lib/json.sh
[ -x "$KARVI" ] || { echo "build the executable first: make build" >&2; exit 1; }
TMP=$(mktemp -d "${TMPDIR:-/tmp}/karvi-v0100-smoke-XXXXXX")
BASE=$TMP/base; HOME_DIR=$TMP/home; SCORE=$TMP/score; CAP=$TMP/cap
mkdir -p "$BASE" "$HOME_DIR" "$SCORE" "$CAP" "$TMP/streams"
FAKE=$TMP/fake-ssh
cat >"$FAKE" <<SH
#!/bin/sh
args=" \$* "
case "\$args" in *" -O check "*) exit 1 ;; esac
# One interactive shell per session; the slow marker holds each
# command three seconds so a job can be caught in flight.
printf 'dev#'
while IFS= read -r line; do
  [ "\$line" = exit ] && exit 0
  printf 'SENT:%s\\n' "\$line" >>"$FAKE.log"
  [ -e "$TMP/slow" ] && sleep "\$(cat "$TMP/slow")"
  printf '%s\\r\\nv0100 device output\\r\\ndev#' "\$line"
done
exit 0
SH
chmod 755 "$FAKE"
: >"$FAKE.log"
# The daemon locates the askpass helper beside the karvi executable (bin/).
export HOME=$HOME_DIR NETUSER=u NETPASS=p
common_args() {
  printf '%s\n' \
    --set "basedir=\"$BASE\"" --set 'sharedroot="none"' --set "spooldir=\"$BASE/spool\"" --set 'platform-resolution.default=""' \
    --set "ssh.transports.system=\"$FAKE\"" \
    --set 'ssh.command.transport="system"' \
    --set audit.journald-required=false \
    --set "audit.file=\"$BASE/audit.jsonl\"" \
    --set "scoreboards=\"$SCORE\"" \
    --set "sessions.shared-capacity-root=\"$CAP\"" \
    --set output.min-free-bytes-after-job=0 \
    --set display.color=never \
    --set 'ssh.host-key-policy="insecure"' \
    --set "ssh.known-hosts-file=\"$TMP/known_hosts\""
}
cleanup() {
  # shellcheck disable=SC2046
  "$KARVI" --quiet $(common_args) daemon stop --force >/dev/null 2>&1 || true
  rm -rf "$TMP"
}
trap cleanup EXIT
failures=0
fail() { echo "FAIL: $*" >&2; failures=$((failures + 1)); }
rows=0
# row NAME EXPECTED_EXIT -- args...   streams land in $TMP/streams/NAME.{out,err}
row() {
  local name=$1 want=$2; shift 3
  rows=$((rows + 1))
  set +e
  # shellcheck disable=SC2046
  "$KARVI" $(common_args) "$@" >"$TMP/streams/$name.out" 2>"$TMP/streams/$name.err" </dev/null
  local rc=$?
  set -e
  [ "$rc" -eq "$want" ] || fail "$name: exit $rc, want $want; stderr: $(head -1 "$TMP/streams/$name.err")"
  printf '%-4s exit %d\n' "$name" "$rc"
}
summaries() { find "$BASE/jobs" -name summary.json -type f 2>/dev/null | wc -l | tr -d ' '; }
wait_summaries() {  # wait_summaries N: until N summaries exist or 20 s pass
  local n=$1 i=0
  while [ "$(summaries)" -lt "$n" ] && [ $i -lt 200 ]; do sleep 0.1; i=$((i + 1)); done
  [ "$(summaries)" -ge "$n" ]
}
echo "v0100-smoke: rows"

# Gate 5: the dry-run in text and json against an absent daemon, then the
# daemon started explicitly.
row d1 0 -- run --dry-run --target 127.0.0.1 --transport system 'show clock'
grep -q '^outcome: planned$' "$TMP/streams/d1.out" || fail "d1: outcome"
grep -q '^daemon local: absent' "$TMP/streams/d1.out" || fail "d1: an absent daemon was not reported"
[ ! -S "$BASE/socket/daemon.sock" ] || fail "d1: a daemon was launched"
# shellcheck disable=SC2046
"$KARVI" $(common_args) daemon start >/dev/null 2>&1
row d2 0 -- run --dry-run --target 127.0.0.1 --transport system --format json 'show clock'
json_is "$TMP/streams/d2.out" kind inspection && json_is "$TMP/streams/d2.out" job_submitted false && json_is "$TMP/streams/d2.out" daemons.0.status running || fail "d2: report fields"
[ "$(summaries)" -eq 0 ] || fail "d2: a job appeared"
[ ! -s "$FAKE.log" ] || fail "d2: the fake device saw a session"

# Gate 6: the exercise in text and json.
row e1 0 -- run --exercise --target 127.0.0.1 --transport system 'show clock'
grep -q '^status: exercised$' "$TMP/streams/e1.out" && grep -q '^device_contacted: false$' "$TMP/streams/e1.out" && grep -q '^- name:127.0.0.1: ready$' "$TMP/streams/e1.out" || fail "e1: report text"
row e2 0 -- run --exercise --target 127.0.0.1 --transport system --format json 'show clock'
grep -q '"kind": "exercise"' "$TMP/streams/e2.out" && grep -q '"outcome": "exercised"' "$TMP/streams/e2.out" || fail "e2: report fields"
wait_summaries 2 || fail "e2: expected two exercise summaries"
[ "$(for e2_summary in $(find "$BASE/jobs" -name summary.json); do json_get "$e2_summary" final_status; done | grep -cx exercised)" -eq 2 ] || fail "e2: summaries are not exercised"
[ ! -s "$FAKE.log" ] || fail "e2: the fake device saw a session"

# The refused flag pairs and the detach combinations.
for pair in "--dry-run --exercise" "--dry-run --detach" "--exercise --detach" "--detach --follow" "--detach --no-daemon" "--exercise --no-daemon"; do
  rows=$((rows + 1))
  set +e
  # shellcheck disable=SC2046,SC2086
  "$KARVI" $(common_args) run $pair --target 127.0.0.1 --transport system 'show clock' >"$TMP/streams/c.out" 2>"$TMP/streams/c.err" </dev/null
  rc=$?
  set -e
  printf '%-4s exit %d  (%s)\n' c "$rc" "$pair"
  [ "$rc" -eq 4 ] && grep -q '^run_mode_conflict: ' "$TMP/streams/c.err" || fail "conflict $pair: exit $rc, $(head -1 "$TMP/streams/c.err")"
done

# --follow=false: no record on stdout, no result line; the exit is the job's.
row f1 0 -- run --follow=false --target 127.0.0.1 --transport system --format jsonl 'show clock'
[ ! -s "$TMP/streams/f1.out" ] || fail "f1: records were rendered"
! grep -q 'exit=' "$TMP/streams/f1.err" || fail "f1: a result line was printed"
grep -q '^SENT:show clock$' "$FAKE.log" || fail "f1: the device did not run the command"

# --detach: returns before the summary exists; the summary appears later.
printf 1 >"$TMP/slow"
before=$(summaries)
row t1 0 -- run --detach --target 127.0.0.1 --transport system 'show clock'
grep -q '^job_id: ' "$TMP/streams/t1.out" && grep -q '^artifact_dir: ' "$TMP/streams/t1.out" || fail "t1: receipt lines"
grep -q ' accepted artifacts=' "$TMP/streams/t1.err" || fail "t1: no accepted line"
[ "$(summaries)" -eq "$before" ] || fail "t1: the client waited for the job"
t1_dir=$(sed -n 's/^artifact_dir: //p' "$TMP/streams/t1.out")
wait_summaries $((before + 1)) || fail "t1: the detached job did not finish"
json_is "$t1_dir/summary.json" final_status completed || fail "t1: the detached job did not complete"
row t2 0 -- run --detach --target 127.0.0.1 --transport system --format json 'show clock'
grep -q '"job_id": ' "$TMP/streams/t2.out" && grep -q '"mode": "live"' "$TMP/streams/t2.out" || fail "t2: json receipt"
wait_summaries $((before + 2)) || fail "t2: the detached job did not finish"

# SIGINT during the follow: exit 113, the job continues to completion.
rows=$((rows + 1))
before=$(summaries)
set +e
# shellcheck disable=SC2046
"$KARVI" $(common_args) run --target 127.0.0.1 --transport system --format jsonl 'show clock' >"$TMP/streams/i1.out" 2>"$TMP/streams/i1.err" </dev/null &
cpid=$!
sleep 1
kill -INT "$cpid"
wait "$cpid"; irc=$?
set -e
printf '%-4s exit %d\n' i1 "$irc"
[ "$irc" -eq 113 ] || fail "i1: exit $irc, want 113"
grep -q '^interrupted: job .* continues in the daemon' "$TMP/streams/i1.err" || fail "i1: no interrupted line"
grep -q '^cancelled: ' "$TMP/streams/i1.err" || fail "i1: no cancelled code"
wait_summaries $((before + 1)) || fail "i1: the interrupted job did not finish"
i1_dir=$(sed -n 's/^interrupted: job [^ ]* continues in the daemon; artifacts \([^;]*\);.*/\1/p' "$TMP/streams/i1.err")
[ -n "$i1_dir" ] && json_is "$i1_dir/summary.json" final_status completed || fail "i1: the job did not complete after the interrupt"

# Broken stdout during the follow: exit 111, the job continues.
# Two targets in serial give a second record to write after head has
# closed the pipe on the first.
rows=$((rows + 1))
before=$(summaries)
set +e
# shellcheck disable=SC2046
"$KARVI" $(common_args) run --target 127.0.0.1 --target 127.0.0.2 --transport system --format jsonl 'show clock' 2>"$TMP/streams/p1.err" </dev/null | head -c 1 >/dev/null
prc=${PIPESTATUS[0]}
set -e
printf '%-4s exit %d\n' p1 "$prc"
[ "$prc" -eq 111 ] || fail "p1: exit $prc, want 111; $(head -2 "$TMP/streams/p1.err" | tr '\n' ' ')"
grep -q 'continues in the daemon' "$TMP/streams/p1.err" || fail "p1: the continuing job was not reported"
wait_summaries $((before + 1)) || fail "p1: the job did not finish"

# The shutdown accounting. The fake device is
# still slow, so each job is caught mid-command.
daemon_pid() { "$KARVI" $(common_args) daemon status --format json 2>/dev/null | json_get - pid 2>/dev/null || true; }
wait_stopped() {  # until the socket is gone or 15 s pass
  local i=0
  while [ -S "$BASE/socket/daemon.sock" ] && [ $i -lt 150 ]; do sleep 0.1; i=$((i + 1)); done
  [ ! -S "$BASE/socket/daemon.sock" ]
}
accounted() {  # accounted NAME DIR STATUS FINAL EXIT CAUSE REASON: the job directory's accounting
  local name=$1 dir=$2 status=$3 final=$4 exit=$5 cause=$6 reason=$7
  [ "$(wc -l <"$dir/commands.jsonl" | tr -d ' ')" -eq 4 ] || fail "$name: expected four records (two targets, two commands)"
  [ "$(jsonl_records "$dir/commands.jsonl" status | grep -cx "$status")" -eq 4 ] || fail "$name: not every record is $status"
  json_is "$dir/summary.json" final_status "$final" && json_is "$dir/summary.json" primary_exit_code "$exit" || fail "$name: summary is not $final at $exit"
  [ -z "$cause" ] || json_is "$dir/summary.json" terminal_causes "[\"$cause\"]" || fail "$name: summary cause is not $cause"
  grep -q "\"event_name\":\"run.completed\".*\"outcome\":\"$final\".*\"reason\":\"$reason\"" "$BASE/audit.jsonl" || fail "$name: no run.completed audit record with outcome $final and reason $reason"
}
# s1: a forced stop with a job in flight. Every unit is incomplete_shutdown,
# the summary incomplete at 106 under shutdown_forced, both devices in
# failed-devices.txt, and the daemon is gone. From here to s5 the device
# takes three seconds a command: the stop must land with every record in
# flight (s2 half a second in and a one-second grace; s5 a second after s4).
printf 3 >"$TMP/slow"
rows=$((rows + 1))
"$KARVI" $(common_args) run --detach --target 127.0.0.1 --target 127.0.0.2 --dispatch serial --transport system --cmd 'show a' --cmd 'show b' >"$TMP/streams/s1.out" 2>"$TMP/streams/s1.err" </dev/null
s1_dir=$(sed -n 's/^artifact_dir: //p' "$TMP/streams/s1.out")
sleep 0.5
set +e
"$KARVI" $(common_args) daemon stop --force >"$TMP/streams/s1-stop.out" 2>"$TMP/streams/s1-stop.err"
src=$?
set -e
printf '%-4s exit %d\n' s1 "$src"
[ "$src" -eq 0 ] || fail "s1: daemon stop --force exit $src: $(head -1 "$TMP/streams/s1-stop.err")"
wait_stopped || fail "s1: the daemon socket remains"
accounted s1 "$s1_dir" incomplete_shutdown incomplete 106 shutdown:shutdown_forced shutdown_forced
json_is "$s1_dir/summary.json" device_counts.incomplete 2 || fail "s1: device_counts do not count two incomplete devices"
[ "$(wc -l <"$s1_dir/failed-devices.txt" | tr -d ' ')" -eq 2 ] || fail "s1: failed-devices.txt does not list both devices"
# s2: SIGTERM with a one-second grace under held sessions: draining is
# reported, the grace expires, the same accounting under
# shutdown_grace_expired, and the daemon log names each step.
rows=$((rows + 1))
"$KARVI" $(common_args) --set daemon.shutdown-grace-seconds=1 --set daemon.forced-grace-seconds=1 daemon start >/dev/null 2>&1
s2_pid=$(daemon_pid)
[ -n "$s2_pid" ] || fail "s2: daemon pid unknown"
"$KARVI" $(common_args) run --detach --target 127.0.0.1 --target 127.0.0.2 --dispatch serial --transport system --cmd 'show a' --cmd 'show b' >"$TMP/streams/s2.out" 2>"$TMP/streams/s2.err" </dev/null
s2_dir=$(sed -n 's/^artifact_dir: //p' "$TMP/streams/s2.out")
sleep 0.5
kill -TERM "$s2_pid"
sleep 0.2
"$KARVI" $(common_args) daemon status >"$TMP/streams/s2-status.out" 2>&1 || true
grep -q '^status: draining$' "$TMP/streams/s2-status.out" || fail "s2: the daemon did not report draining after SIGTERM"
wait_stopped || fail "s2: the daemon did not stop after the grace"
printf '%-4s exit -  (SIGTERM, grace expired)\n' s2
accounted s2 "$s2_dir" incomplete_shutdown incomplete 106 shutdown:shutdown_grace_expired shutdown_grace_expired
grep -q 'code=daemon_draining' "$BASE/logs/daemon.log" && grep -q 'code=shutdown_grace_expired' "$BASE/logs/daemon.log" || fail "s2: the daemon log lacks the drain and expiry lines"
# s3: SIGTERM with the default grace: the job finishes and the daemon
# stops with it complete.
rows=$((rows + 1))
"$KARVI" $(common_args) daemon start >/dev/null 2>&1
s3_pid=$(daemon_pid)
"$KARVI" $(common_args) run --detach --target 127.0.0.1 --target 127.0.0.2 --dispatch serial --transport system --cmd 'show a' --cmd 'show b' >"$TMP/streams/s3.out" 2>"$TMP/streams/s3.err" </dev/null
s3_dir=$(sed -n 's/^artifact_dir: //p' "$TMP/streams/s3.out")
sleep 0.5
kill -TERM "$s3_pid"
wait_stopped || fail "s3: the daemon did not stop after the job finished"
printf '%-4s exit -  (SIGTERM, job finished within the grace)\n' s3
accounted s3 "$s3_dir" succeeded completed 0 "" ""
grep -q 'code=daemon_stopped' "$BASE/logs/daemon.log" || fail "s3: the daemon log lacks the stop line"
# The stop and restart cases end to end, still under the slow
# device.
serving() {  # daemon serve processes for this base directory
  local n=0 p
  for p in $(pgrep -f 'daemon serve' 2>/dev/null); do
    tr '\0' ' ' <"/proc/$p/cmdline" 2>/dev/null | grep -q -- "$BASE" && n=$((n + 1))
  done
  echo "$n"
}
held_job() {  # held_job NAME: detach a two-target two-command job; prints its directory
  "$KARVI" $(common_args) run --detach --target 127.0.0.1 --target 127.0.0.2 --dispatch serial --transport system --cmd 'show a' --cmd 'show b' >"$TMP/streams/$1.out" 2>"$TMP/streams/$1.err" </dev/null
  sed -n 's/^artifact_dir: //p' "$TMP/streams/$1.out"
}
# s4: no option with an active job refuses and leaves the daemon untouched;
# combined options and a non-positive --after are usage errors.
"$KARVI" $(common_args) daemon start >/dev/null 2>&1
s4_pid=$(daemon_pid)
s4_dir=$(held_job s4)
sleep 0.5
row s4 112 -- daemon stop
grep -q '^daemon_active_jobs: ' "$TMP/streams/s4.err" && grep -q -- '--grace' "$TMP/streams/s4.err" && grep -q -- '--after' "$TMP/streams/s4.err" && grep -q -- '--force' "$TMP/streams/s4.err" || fail "s4: the refusal does not name the three options"
[ "$(daemon_pid)" = "$s4_pid" ] || fail "s4: the refused stop replaced or stopped the daemon"
"$KARVI" $(common_args) daemon status 2>/dev/null | grep -q '^status: running$' || fail "s4: the daemon is not running after the refusal"
for combo in "stop --grace --force" "restart --after=1s --force" "stop --after=0s"; do
  rows=$((rows + 1))
  set +e
  # shellcheck disable=SC2046,SC2086
  "$KARVI" $(common_args) daemon $combo >"$TMP/streams/s4c.out" 2>"$TMP/streams/s4c.err" </dev/null
  rc=$?
  set -e
  printf '%-4s exit %d  (daemon %s)\n' s4c "$rc" "$combo"
  [ "$rc" -eq 4 ] && grep -Eq '^daemon_stop_(options_conflict|after_not_positive): ' "$TMP/streams/s4c.err" || fail "s4c: daemon $combo: exit $rc, $(head -1 "$TMP/streams/s4c.err")"
done
# s5: --after=1s drains, waits a second, forces; the held job is accounted.
row s5 0 -- daemon stop --after=1s
grep -q 'forcing stop' "$TMP/streams/s5.err" || fail "s5: no forcing line after the limit"
wait_stopped || fail "s5: the daemon socket remains"
accounted s5 "$s4_dir" incomplete_shutdown incomplete 106 shutdown:shutdown_forced shutdown_forced
printf 1 >"$TMP/slow"
# s6: --grace drains and waits for the job to finish.
"$KARVI" $(common_args) daemon start >/dev/null 2>&1
before_sent=$(grep -c '^SENT:' "$FAKE.log" || true)
s6_dir=$(held_job s6)
sleep 0.5
row s6 0 -- daemon stop --grace
wait_stopped || fail "s6: the daemon socket remains"
[ "$(grep -c '^SENT:' "$FAKE.log" || true)" -eq $((before_sent + 4)) ] || fail "s6: the device did not receive all four commands before the stop"
accounted s6 "$s6_dir" succeeded completed 0 "" ""
# s7: Ctrl-C during --grace ends only the client's wait; the daemon stays
# draining on the same pid, a run is refused without launching another
# daemon, and --force then accounts the job. The device takes three seconds
# a command again: the row's four steps before --force take about 1.3
# seconds, and at one second a command the first record was complete by
# then (the row passed only while the
# operator's own trust store, which the suite then read, cost the session
# a host-key scan first).
printf 3 >"$TMP/slow"
"$KARVI" $(common_args) daemon start >/dev/null 2>&1
s7_pid=$(daemon_pid)
s7_dir=$(held_job s7)
sleep 0.5
rows=$((rows + 1))
set +e
# shellcheck disable=SC2046
"$KARVI" $(common_args) daemon stop --grace >"$TMP/streams/s7.out" 2>"$TMP/streams/s7.err" </dev/null &
s7_client=$!
sleep 0.5
kill -INT "$s7_client"
wait "$s7_client"; s7_rc=$?
set -e
printf '%-4s exit %d  (Ctrl-C during --grace)\n' s7 "$s7_rc"
[ "$s7_rc" -eq 113 ] && grep -q '^daemon_stop_wait_interrupted: ' "$TMP/streams/s7.err" || fail "s7: exit $s7_rc, $(head -1 "$TMP/streams/s7.err")"
"$KARVI" $(common_args) daemon status 2>/dev/null | grep -q '^status: draining$' || fail "s7: the daemon is not draining after the interrupt"
[ "$(daemon_pid)" = "$s7_pid" ] || fail "s7: the interrupt stopped or replaced the daemon"
set +e
# shellcheck disable=SC2046
"$KARVI" $(common_args) run --target 127.0.0.1 --transport system 'show c' >"$TMP/streams/s7r.out" 2>"$TMP/streams/s7r.err" </dev/null
s7r_rc=$?
set -e
[ "$s7r_rc" -eq 112 ] && grep -q 'daemon_draining' "$TMP/streams/s7r.err" || fail "s7: run against the draining daemon: exit $s7r_rc, $(head -1 "$TMP/streams/s7r.err")"
[ "$(daemon_pid)" = "$s7_pid" ] || fail "s7: the refused run replaced the daemon"
[ "$(serving)" -eq 1 ] || fail "s7: $(serving) daemon serve processes for this base directory, want one"
row s7f 0 -- daemon stop --force
wait_stopped || fail "s7: the daemon socket remains after --force"
accounted s7 "$s7_dir" incomplete_shutdown incomplete 106 shutdown:shutdown_forced shutdown_forced
printf 1 >"$TMP/slow"
# s8: restart --force under a held job accounts the old daemon's job and
# serves from a new pid; s9: restart with no option and an active job
# refuses.
"$KARVI" $(common_args) daemon start >/dev/null 2>&1
s8_pid=$(daemon_pid)
s8_dir=$(held_job s8)
sleep 0.5
row s8 0 -- daemon restart --force
grep -q '^daemon restarted$' "$TMP/streams/s8.out" || fail "s8: no restarted line"
[ -n "$(daemon_pid)" ] && [ "$(daemon_pid)" != "$s8_pid" ] || fail "s8: the daemon was not replaced (pid $s8_pid then $(daemon_pid))"
"$KARVI" $(common_args) daemon status 2>/dev/null | grep -q '^status: running$' || fail "s8: the new daemon is not running"
i=0; while [ ! -f "$s8_dir/summary.json" ] && [ $i -lt 100 ]; do sleep 0.1; i=$((i + 1)); done
accounted s8 "$s8_dir" incomplete_shutdown incomplete 106 shutdown:shutdown_forced shutdown_forced
s9_pid=$(daemon_pid)
s9_dir=$(held_job s9)
sleep 0.5
row s9 112 -- daemon restart
grep -q '^daemon_active_jobs: ' "$TMP/streams/s9.err" || fail "s9: no daemon_active_jobs refusal"
[ "$(daemon_pid)" = "$s9_pid" ] || fail "s9: the refused restart replaced the daemon"
"$KARVI" $(common_args) daemon stop --force >/dev/null 2>&1 || true
wait_stopped || fail "s9: the daemon socket remains after the cleanup stop"

# cancel_job: c1 cancels a held detached job with a
# reason and asserts the cancelled accounting, the cancellation block, and
# the audit record; c2 an unknown job ID; c3 the finished job; c4 --follow
# on a held job exits with the job's exit and the daemon stays idle.
"$KARVI" $(common_args) daemon start >/dev/null 2>&1
c1_dir=$(held_job c1)
c1_job=$(sed -n 's/^job_id: //p' "$TMP/streams/c1.out")
sleep 0.5
row c1c 0 -- job cancel "$c1_job" --reason 'wrong change window'
grep -q "^cancel requested: job $c1_job; artifacts $c1_dir\$" "$TMP/streams/c1c.out" || fail "c1: no cancel requested line"
i=0; while [ ! -f "$c1_dir/summary.json" ] && [ $i -lt 100 ]; do sleep 0.1; i=$((i + 1)); done
accounted c1 "$c1_dir" cancelled cancelled 113 halt:cancelled cancelled
json_is "$c1_dir/summary.json" device_counts.cancelled 2 || fail "c1: device_counts do not count two cancelled devices"
json_has "$c1_dir/summary.json" cancellation.requested_at && json_is "$c1_dir/summary.json" cancellation.reason 'wrong change window' || fail "c1: no cancellation block with the reason"
grep -q '"event_name":"run.cancel_requested".*"reason":"wrong change window"' "$BASE/audit.jsonl" || fail "c1: no run.cancel_requested audit record"
[ "$(wc -l <"$c1_dir/failed-devices.txt" | tr -d ' ')" -eq 2 ] || fail "c1: failed-devices.txt does not list both devices"
row c2 112 -- job cancel 260915-170000-zz
grep -q '^job_unknown: ' "$TMP/streams/c2.err" || fail "c2: no job_unknown"
row c3 0 -- job cancel "$c1_job"
grep -q "^job $c1_job already ended: cancelled (exit 113); artifacts $c1_dir\$" "$TMP/streams/c3.out" || fail "c3: no already-ended line"
c4_dir=$(held_job c4)
c4_job=$(sed -n 's/^job_id: //p' "$TMP/streams/c4.out")
sleep 0.5
row c4f 113 -- job cancel "$c4_job" --follow
grep -q "^job $c4_job cancelled; artifacts $c4_dir (exit 113)\$" "$TMP/streams/c4f.out" || fail "c4: no cancelled terminal line"
accounted c4 "$c4_dir" cancelled cancelled 113 halt:cancelled cancelled
"$KARVI" $(common_args) daemon status --format json 2>/dev/null | grep -q '"active_jobs": *0' || fail "c4: the daemon still reports active jobs"
"$KARVI" $(common_args) daemon stop >/dev/null 2>&1 || fail "c4: the idle daemon refused a plain stop"
wait_stopped || fail "c4: the daemon socket remains after the stop"
rm -f "$TMP/slow"

# Job follow: w1 follows a held detached job live
# under jsonl and asserts stdout equals commands.jsonl byte for byte, the
# result line, and the job's exit; w2 the finished job again, at once; w3 an
# unknown ID; w4 SIGINT during a follow, the job completing; w5 the finished
# job read from its directory after a forced restart, the same bytes; w6 a
# cancelled job followed after the fact.
printf 1 >"$TMP/slow"
"$KARVI" $(common_args) daemon start >/dev/null 2>&1
w1_dir=$(held_job w1)
w1_job=$(sed -n 's/^job_id: //p' "$TMP/streams/w1.out")
sleep 0.3
row w1f 0 -- job follow "$w1_job" --format jsonl
[ "$(wc -l <"$w1_dir/commands.jsonl" | tr -d ' ')" -eq 4 ] || fail "w1: expected four records"
head -n 4 "$TMP/streams/w1f.out" | cmp -s - "$w1_dir/commands.jsonl" || fail "w1: jsonl stdout differs from commands.jsonl"
# The summary document is the stream's last line; nothing on stderr.
[ "$(wc -l <"$TMP/streams/w1f.out" | tr -d ' ')" -eq 5 ] || fail "w1: expected four records and the summary line"
sed -n 5p "$TMP/streams/w1f.out" >"$TMP/w1.summary"
json_is "$TMP/w1.summary" job_id "$w1_job" && json_is "$TMP/w1.summary" primary_exit_name ExitSuccess || fail "w1: the last line is not the job's summary"
! grep -q 'exit=' "$TMP/streams/w1f.err" || fail "w1: a result line was printed"
row w2 0 -- job follow "$w1_job" --format jsonl
head -n 4 "$TMP/streams/w2.out" | cmp -s - "$w1_dir/commands.jsonl" || fail "w2: the finished job's jsonl differs from commands.jsonl"
row w3 112 -- job follow 260915-170000-zz
grep -q '^job_unknown: ' "$TMP/streams/w3.err" || fail "w3: no job_unknown"
w4_dir=$(held_job w4)
w4_job=$(sed -n 's/^job_id: //p' "$TMP/streams/w4.out")
set +e
# shellcheck disable=SC2046
"$KARVI" $(common_args) job follow "$w4_job" >"$TMP/streams/w4f.out" 2>"$TMP/streams/w4f.err" </dev/null &
cpid=$!
sleep 1
kill -INT "$cpid"
wait "$cpid"; wrc=$?
set -e
rows=$((rows + 1))
printf '%-4s exit %d\n' w4f "$wrc"
[ "$wrc" -eq 113 ] || fail "w4: exit $wrc, want 113"
grep -q "^interrupted: job $w4_job continues in the daemon; artifacts $w4_dir; run \"karvi job follow $w4_job\" again" "$TMP/streams/w4f.err" || fail "w4: no interrupted line naming job follow"
i=0; while [ ! -f "$w4_dir/summary.json" ] && [ $i -lt 200 ]; do sleep 0.1; i=$((i + 1)); done
json_is "$w4_dir/summary.json" final_status completed || fail "w4: the job did not complete after the interrupt"
"$KARVI" $(common_args) daemon restart --force >/dev/null 2>&1 || fail "w5: restart --force failed"
row w5 0 -- job follow "$w1_job" --format jsonl
head -n 4 "$TMP/streams/w5.out" | cmp -s - "$w1_dir/commands.jsonl" || fail "w5: the directory-read jsonl differs from commands.jsonl"
sed -n 5p "$TMP/streams/w5.out" >"$TMP/w5.summary"
json_is "$TMP/w5.summary" job_id "$w1_job" || fail "w5: no summary line from the directory"
w6_dir=$(held_job w6)
w6_job=$(sed -n 's/^job_id: //p' "$TMP/streams/w6.out")
sleep 0.5
"$KARVI" $(common_args) job cancel "$w6_job" --reason 'wrong window' --follow >/dev/null 2>&1 || true
row w6 113 -- job follow "$w6_job" --format jsonl
[ "$(jsonl_records "$TMP/streams/w6.out" status | grep -cx cancelled)" -eq 4 ] || fail "w6: not every record is cancelled"
grep -q "^job $w6_job cancelled: wrong window; artifacts $w6_dir\$" "$TMP/streams/w6.err" || fail "w6: no cancelled line with the reason"
tail -1 "$TMP/streams/w6.out" >"$TMP/w6.summary"
json_is "$TMP/w6.summary" primary_exit_name ExitCancelled || fail "w6: the last line is not the cancelled job's summary"
"$KARVI" $(common_args) daemon stop >/dev/null 2>&1 || fail "w6: the idle daemon refused a plain stop"
wait_stopped || fail "w6: the daemon socket remains after the stop"
rm -f "$TMP/slow"

# The ICMP gate's text line, its switches, and the two rehearsals.
# The rows need an ICMP method on this host and are skipped with the reason
# when karvi version reports none.
icmp_method=$("$KARVI" version --format json 2>/dev/null | sed -n 's/.*"icmp_method": *"\([a-z]*\)".*/\1/p')
if [ "$icmp_method" = socket ] || [ "$icmp_method" = system ]; then
  before_log=$(wc -l <"$FAKE.log" | tr -d ' ')
  # g1: one line per gated device before its header; the unreachable one is skipped.
  row g1 101 -- --set 'network.ping-timeout="200ms"' run --ping --target 127.0.0.1 --target 192.0.2.1 --transport system 'show clock'
  grep -Eq '^! 127\.0\.0\.1 \[127\.0\.0\.1\] ping\(1\) [<0-9.]+ms, ping\(2\) [<0-9.]+ms, proceeding$' "$TMP/streams/g1.out" || fail "g1: no proceeding line for loopback"
  grep -q '^! 192.0.2.1 \[192.0.2.1\] ping(1) timeout, ping(2) timeout, skipped$' "$TMP/streams/g1.out" || fail "g1: no skipped line for TEST-NET"
  grep -q 'status=icmp_unreachable error=icmp_unreachable: ICMP gate enabled' "$TMP/streams/g1.out" || fail "g1: no icmp_unreachable failure line"
  [ "$(grep -c 'error=icmp_unreachable: icmp_unreachable' "$TMP/streams/g1.out")" -eq 0 ] || fail "g1: the code is printed twice"
  [ "$(wc -l <"$FAKE.log" | tr -d ' ')" -eq $((before_log + 1)) ] || fail "g1: expected exactly one new session at the fake device"
  # g2: display.ping.header="" prints no line; the records still carry the object.
  row g2 101 -- --set 'network.ping-timeout="200ms"' --set display.ping.header="" run --ping --target 127.0.0.1 --target 192.0.2.1 --transport system 'show clock'
  [ "$(grep -c 'ping(1)' "$TMP/streams/g2.out")" -eq 0 ] || fail "g2: an empty display.ping.header still printed the line"
  # g3: --quiet suppresses the line; --debug adds the gate's debug lines to stderr.
  row g3 101 -- --quiet --set 'network.ping-timeout="200ms"' run --ping --target 192.0.2.1 --transport system 'show clock'
  [ "$(grep -c 'ping(1)' "$TMP/streams/g3.out")" -eq 0 ] || fail "g3: --quiet still printed the line"
  row g4 101 -- --debug --set 'network.ping-timeout="200ms"' run --ping --no-daemon --target 192.0.2.1 --transport system 'show clock'
  grep -q 'device ping gate target=.*decision=skip' "$TMP/streams/g4.err" || fail "g4: no gate debug line"
  # g5: the rehearsals detect the capability and send no probe or session.
  before_log=$(wc -l <"$FAKE.log" | tr -d ' ')
  row g5 0 -- run --dry-run --ping --target 127.0.0.1 --transport system 'show clock'
  grep -q "intended: ping=enabled (2 probes, 500ms, capability=available via $icmp_method) transport=system port=22" "$TMP/streams/g5.out" || fail "g5: the dry-run does not report the capability"
  row g6 0 -- run --exercise --ping --target 127.0.0.1 --transport system 'show clock'
  grep -q "intended_ping: enabled (2 probes, 500ms, capability=available via $icmp_method)" "$TMP/streams/g6.out" && grep -q '^- name:127.0.0.1: ready$' "$TMP/streams/g6.out" || fail "g6: the exercise does not report the capability"
  [ "$(wc -l <"$FAKE.log" | tr -d ' ')" -eq "$before_log" ] || fail "g5/g6: a rehearsal contacted the fake device"
  # g7: the flags exclude each other before anything runs.
  row g7 4 -- run --ping --noping --target 127.0.0.1 --transport system 'show clock'
  grep -q '^ping_flag_conflict: ' "$TMP/streams/g7.err" || fail "g7: no ping_flag_conflict"
else
  echo "g1-g7 SKIP: no ICMP method on this host (icmp_method=${icmp_method:-unknown}); widen net.ipv4.ping_group_range or install ping"
fi

if [ "$failures" -ne 0 ]; then
  echo "v0100-smoke: $failures failure(s) over $rows rows" >&2
  exit 1
fi
echo "v0100 smoke (rehearsal, detach, follow, interrupt, the ICMP gate, shutdown accounting, cancel_job, job follow): pass ($rows rows)"
