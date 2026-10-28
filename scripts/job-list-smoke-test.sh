#!/bin/sh
# The job list suite: karvi job list (docs/EXAMPLES.md chapter 61) on the
# jobs of a lab output root, made against the fake IOS XE device built from
# the tree over the native transport: a command, a run of two devices of
# which one is unreachable, a crun, an exercise through the daemon, a run
# killed while it ran, the empty folder of a run through the daemon refused
# at planning, and, unless run as root, a folder whose summary cannot
# be read and one that cannot be entered. The table to a pipe and at a
# terminal of 80 columns, each filter, the rows under --target, the
# refusals, and the JSON. Needs `go`; writes
# nothing outside its own directory.
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
KARVI=${KARVI:-$ROOT/bin/karvi-linux-amd64}
GO=${GO:-go}
. "$ROOT/scripts/lib/json.sh"
TMP=$(mktemp -d "${TMPDIR:-/tmp}/karvi-job-list-smoke-XXXXXX")
. "$ROOT/scripts/lib/host.sh"; host_trust_store "$TMP"   # the trust store under the work directory, never the operator's
FAKE_PID=
unset NETUSER NETPASS NETENABLE

fail() { echo "job list smoke: FAIL: $*" >&2; exit 1; }
stop_daemon() {
  [ -S "$TMP/base/socket/d.sock" ] || return 0
  HOME=$TMP/home "$KARVI" --quiet --config "$TMP/karvi.toml" daemon stop --force >/dev/null 2>&1 || true
  i=0
  while [ -S "$TMP/base/socket/d.sock" ] && [ $i -lt 100 ]; do sleep 0.1; i=$((i + 1)); done
}
cleanup() {
  stop_daemon
  if [ -n "$FAKE_PID" ]; then kill "$FAKE_PID" 2>/dev/null || true; wait "$FAKE_PID" 2>/dev/null || true; fi
  chmod -R u+rwX "$TMP" 2>/dev/null || true
  rm -rf "$TMP"
}
trap cleanup EXIT HUP INT TERM

install -d -m 700 "$TMP/bin" "$TMP/home" "$TMP/base" "$TMP/score" "$TMP/crun"
(cd "$ROOT" && GOTOOLCHAIN=local "$GO" build -mod=vendor -o "$TMP/bin/fake" ./cmd/karvi-fake-device)
"$TMP/bin/fake" -host-key-file "$TMP/hostkey" -slow 20s -port 0 >"$TMP/port" 2>"$TMP/fake.err" &
FAKE_PID=$!
i=0
while [ ! -s "$TMP/port" ] && [ $i -lt 100 ]; do sleep 0.05; i=$((i + 1)); done
[ -s "$TMP/port" ] || fail "the fake did not start: $(head -1 "$TMP/fake.err")"
PORT=$(cat "$TMP/port")

# dead-iosxe's address is a loopback the fake does not listen on: refused.
printf 'name,management_address,platform\nfake-iosxe,127.0.0.1,cisco_ios\ndead-iosxe,127.0.0.9,cisco_ios\n' >"$TMP/inv.csv"
cat >"$TMP/karvi.toml" <<EOF_CFG
basedir = "$TMP/base"
sharedroot = "none"
spooldir = "$TMP/spool"
scoreboards = "$TMP/score"
platform-resolution.default = ""
[ssh]
host-key-policy = "accept-new"
known-hosts-file = "$TMP/store/known_hosts"
connect-timeout = "2s"
[audit]
journald-required = false
file = "$TMP/base/audit.jsonl"
[sessions]
shared-capacity-root = "$TMP/base/cap"
[crun]
directory = "$TMP/crun"
[display]
color = "never"
[platform.cisco_ios]
ssh-port = $PORT
[[inventory-source]]
name = "lab"
type = "csv"
path = "$TMP/inv.csv"
required = true
mode = "header"
delimiter = ","
mandatory-fields = ["name", "platform"]
name-transform = "default"
[inventory-source.mappings]
name = ["name"]
management_address = ["management_address"]
platform = ["platform"]
EOF_CFG
chmod 600 "$TMP/karvi.toml"
install -d -m 700 "$TMP/store"

# karvi TAG ARGS...: the client with the fake's credentials; its output in
# out.TAG, its diagnostics in err.TAG, its exit in CODE.
karvi() {
  tag=$1; shift
  CODE=0
  HOME=$TMP/home NETUSER=netops NETPASS=pw NETENABLE=en "$KARVI" --config "$TMP/karvi.toml" "$@" >"$TMP/out.$tag" 2>"$TMP/err.$tag" || CODE=$?
}
# expect TAG CODE: the call's exit.
expect() { [ "$CODE" = "$2" ] || fail "$1: exit $CODE, want $2: $(head -3 "$TMP/err.$1")"; }
# newest: the newest job folder's name, the last a glob sorts.
newest() { for nw_dir in "$TMP/base/jobs"/*/*; do :; done; basename "$nw_dir"; }

karvi cmd command fake-iosxe 'show version';                                        expect cmd 0;   J1=$(newest)
karvi two run --no-daemon --target fake-iosxe --target dead-iosxe --cmd 'show version'; expect two 101; J2=$(newest)
karvi crun crun --no-daemon --target fake-iosxe --cmd 'show version';               expect crun 0;  J3=$(newest)
karvi ex run --exercise --target fake-iosxe --cmd 'show version';                    expect ex 0;    J4=$(newest)
# A run killed two seconds into show slow: its summary stays the one
# written at acceptance. The background is karvi itself, so $! is its pid.
HOME=$TMP/home NETUSER=netops NETPASS=pw NETENABLE=en "$KARVI" --config "$TMP/karvi.toml" run --no-daemon --target fake-iosxe --cmd 'show slow' >"$TMP/out.kill" 2>"$TMP/err.kill" &
KILLED=$!
sleep 2
kill -9 "$KILLED"; wait "$KILLED" 2>/dev/null || true
J5=$(newest)
for j in "$J1" "$J2" "$J3" "$J4" "$J5"; do [ -n "$j" ] || fail "a job made no folder"; done
day() { printf '%s' "$TMP/base/jobs/${1%%-*}/$1"; }
for pair in "$J1 cmd" "$J2 run" "$J3 crun" "$J4 run" "$J5 run"; do
  # shellcheck disable=SC2086
  set -- $pair
  [ "$(json_get "$(day "$1")/summary.json" kind)" = "$2" ] || fail "$1: summary kind $(json_get "$(day "$1")/summary.json" kind), want $2"
done
json_has "$(day "$J5")/summary.json" final_status && fail "the killed run's summary is final"
# A run through the daemon without NETPASS is refused at planning, exit 6,
# after the daemon made its folder: a folder without a summary.
CODE=0
HOME=$TMP/home NETUSER=netops "$KARVI" --config "$TMP/karvi.toml" run --target fake-iosxe --cmd 'show version' >"$TMP/out.nocred" 2>"$TMP/err.nocred" </dev/null || CODE=$?
expect nocred 6
J8=$(newest)
[ "$J8" != "$J5" ] && [ -z "$(ls -A "$(day "$J8")")" ] || fail "the refused run left no empty folder"
UNREADABLE=
if [ "$(id -u)" != 0 ]; then
  karvi r6 run --no-daemon --target fake-iosxe --cmd 'show version'; expect r6 0; J6=$(newest)
  karvi r7 run --no-daemon --target fake-iosxe --cmd 'show version'; expect r7 0; J7=$(newest)
  chmod 000 "$(day "$J6")/summary.json"
  chmod 000 "$(day "$J7")"
  UNREADABLE=yes
fi

# The table to a pipe, the TIME of a finished job masked: OPERATOR as wide
# as the operator's name, at least eight; DONE's O over the slash.
OP=$(id -un)
UID_=$(id -u)
W=${#OP}; [ "$W" -ge 8 ] || W=8
job_row() { printf '  %-16s  %-8s  %-10s  %-*s  %-4s  %3s/%-3s  %4s  %s\n' "$1" "$2" "$3" "$W" "$OP" "$4" "$5" "$6" "$7" "$8" | sed 's/ *$//'; }
bad_row() { printf '? %-16s  %-8s  %-10s  %-*s  %-4s     -     %4s  %s\n' "$1" - invalid "$W" "$OP" - - "$2"; }
mask() { awk '{ t = substr($0, 21, 8); if (t ~ /^[0-9]+m[0-9][0-9]s +$|^[0-9]+h[0-9][0-9]m +$/) $0 = substr($0, 1, 20) "DUR     " substr($0, 29); print }' "$1"; }
{
  printf '  %-16s  %-8s  %-10s  %-*s  %-4s  %-7s  %4s  %s\n' JOB-ID TIME STATUS "$W" OPERATOR MODE '  DONE' FAIL TARGET
  if [ -n "$UNREADABLE" ]; then
    bad_row "$J7" "$(day "$J7")/summary.json"
    bad_row "$J6" "$(day "$J6")/summary.json"
  fi
  job_row "$J8" '' unfinished u u u u ''
  job_row "$J5" '' unfinished run u 1 u fake-iosxe
  job_row "$J4" DUR exercise run 1 1 0 fake-iosxe
  job_row "$J3" DUR complete crun 1 1 0 fake-iosxe
  job_row "$J2" DUR error run 2 2 1 'fake-iosxe dead-iosxe'
  job_row "$J1" DUR complete cmd 1 1 0 fake-iosxe
} >"$TMP/want.all"
karvi all job list; expect all 0
mask "$TMP/out.all" >"$TMP/got.all"
cmp -s "$TMP/want.all" "$TMP/got.all" || { diff "$TMP/want.all" "$TMP/got.all" >&2 || true; fail "job list: the table differs"; }
[ ! -s "$TMP/err.all" ] || fail "job list wrote on stderr: $(head -1 "$TMP/err.all")"
grep -q "$(printf '\033')" "$TMP/out.all" && fail "colour into a pipe under display.color = never"

# --last counts; --failed keeps the errored run and the folders it cannot
# judge; --operator by name and by UID, and one with no jobs.
karvi last job list --last 2; expect last 0
[ "$(wc -l <"$TMP/out.last")" = 3 ] || fail "--last 2: $(wc -l <"$TMP/out.last") lines"
karvi failed job list --failed; expect failed 0
mask "$TMP/out.failed" | sed 1d | cut -c3-18 >"$TMP/got.failed"
{ if [ -n "$UNREADABLE" ]; then echo "$J7"; echo "$J6"; fi; echo "$J2"; } >"$TMP/want.failed"
cmp -s "$TMP/want.failed" "$TMP/got.failed" || fail "--failed: $(tr '\n' ' ' <"$TMP/got.failed")"
karvi byname job list --operator "$OP"; expect byname 0
cmp -s "$TMP/out.all" "$TMP/out.byname" || fail "--operator $OP is not every job"
karvi byuid job list --operator "$UID_" --last 1; expect byuid 0
[ "$(wc -l <"$TMP/out.byuid")" = 2 ] || fail "--operator $UID_ --last 1"
karvi nobody job list --operator nobody-at-all; expect nobody 0
[ "$(cat "$TMP/out.nobody")" = "(no jobs)" ] || fail "--operator nobody-at-all: $(head -1 "$TMP/out.nobody")"

# --target: a row per device left, its outcome; "!" removes; --last counts
# rows; none that selects is refused before any folder is read.
dev_row() { printf '  %-16s  %-8s  %-*s  %-4s  %-11s  %s\n' "$1" "$2" "$W" "$OP" "$3" "$4" "$5" | sed 's/ *$//'; }
dev_head() { printf '  %-16s  %-8s  %-*s  %-4s  %-11s  %s\n' JOB-ID TIME "$W" OPERATOR MODE OUTCOME DEVICE; }
karvi dead job list --target 'DEAD*'; expect dead 0
{ dev_head; dev_row "$J2" DUR run fail dead-iosxe; } >"$TMP/want.dead"
{ if [ -n "$UNREADABLE" ]; then sed -n '1p;4,$p' "$TMP/out.dead"; else cat "$TMP/out.dead"; fi; } >"$TMP/raw.dead"
mask "$TMP/raw.dead" >"$TMP/got.dead"
cmp -s "$TMP/want.dead" "$TMP/got.dead" || { diff "$TMP/want.dead" "$TMP/got.dead" >&2 || true; fail "--target DEAD*"; }
if [ -n "$UNREADABLE" ]; then
  sed -n 2p "$TMP/out.dead" | grep -q "^? $J7  -  .*  -     invalid      $(day "$J7")/summary.json\$" || fail "--target keeps the folder it cannot judge: $(sed -n 2p "$TMP/out.dead")"
fi
karvi notdead job list --target '*-iosxe' --target '!dead*' --last 3; expect notdead 0
mask "$TMP/out.notdead" | sed 1d | awk '{ print $1 == "?" ? "?" : $NF }' | tr '\n' ' ' >"$TMP/got.notdead"
if [ -n "$UNREADABLE" ]; then want='? ? fake-iosxe '; else want='fake-iosxe fake-iosxe fake-iosxe '; fi
[ "$(cat "$TMP/got.notdead")" = "$want" ] || fail "--target *-iosxe --target !dead* --last 3: $(cat "$TMP/got.notdead")"
grep -q "^  $J5  .*  unfinished   fake-iosxe\$" "$TMP/out.notdead" || fail "the killed run's device is not unfinished"
karvi only job list --target '!dead*'; expect only 5
grep -qx 'inventory_positive_selector_missing: job list --target requires a value that selects (a value beginning with "!" only removes devices)' "$TMP/err.only" || fail "--target !dead*: $(head -1 "$TMP/err.only")"
karvi neg job list --last -1; expect neg 4
grep -qx 'cli_option_value_invalid: --last takes 0 or more, not -1' "$TMP/err.neg" || fail "--last -1: $(head -1 "$TMP/err.neg")"

# At a terminal of 80 columns TARGET is the 7 cells the fixed columns
# leave: the run of two devices is +2, a ? row's path …; every line fits.
script -qefc "stty cols 80; HOME='$TMP/home' '$KARVI' --config '$TMP/karvi.toml' job list" /dev/null >"$TMP/pty.raw" 2>"$TMP/pty.err" || fail "job list under script: $(head -1 "$TMP/pty.err")"
tr -d '\r' <"$TMP/pty.raw" >"$TMP/pty.txt"
grep -q "^  $J2  .*  2/2       1  +2\$" "$TMP/pty.txt" || fail "80 columns: $(grep "$J2" "$TMP/pty.txt")"
awk 'length > 80 { bad = 1 } END { exit bad }' "$TMP/pty.txt" || fail "80 columns: a line is wider"
if [ -n "$UNREADABLE" ]; then
  grep -q "^? $J7  .*  invalid  .*  …\$" "$TMP/pty.txt" || fail "80 columns: the ? row's path is not …: $(grep "$J7" "$TMP/pty.txt")"
fi
# Into a pipe under display.color = always, coloured; into a file never.
HOME=$TMP/home "$KARVI" --config "$TMP/karvi.toml" --set display.color=always job list --last 1 | cat >"$TMP/out.pipe"
grep -q "$(printf '\033')\\[" "$TMP/out.pipe" || fail "display.color = always into a pipe: no colour"
karvi colour --set display.color=always job list --last 1; expect colour 0
grep -q "$(printf '\033')" "$TMP/out.colour" && fail "display.color = always into a file: coloured"

# The JSON: one array of the rows, each the summary's fields with the
# folder's path and the operator's name and UID; under --target the device
# and its outcome; a folder that cannot be read its path, operator, error.
karvi json job list --format json; expect json 0
n=0
[ -z "$UNREADABLE" ] || n=2
[ "$(json_get "$TMP/out.json" "$n.path")" = "$(day "$J8")" ] || fail "json: row $n is not $J8"
json_has "$TMP/out.json" "$n.job_id" && fail "json: the folder without a summary has summary fields"
[ "$(json_get "$TMP/out.json" "$n.operator.username")" = "$OP" ] || fail "json: $J8's operator"
[ "$(json_get "$TMP/out.json" "$((n + 1)).job_id")" = "$J5" ] || fail "json: row $((n + 1)) is not $J5"
json_has "$TMP/out.json" "$((n + 1)).final_status" && fail "json: the killed run has a final status"
[ "$(json_get "$TMP/out.json" "$((n + 4)).path")" = "$(day "$J2")" ] || fail "json: $J2's path"
[ "$(json_get "$TMP/out.json" "$((n + 4)).operator.username")" = "$OP" ] || fail "json: $J2's operator"
[ "$(json_get "$TMP/out.json" "$((n + 4)).operator.uid")" = "$UID_" ] || fail "json: $J2's UID"
[ "$(json_get "$TMP/out.json" "$((n + 4)).primary_exit_code")" = 101 ] || fail "json: $J2's exit"
[ "$(json_get "$TMP/out.json" '*.kind' | tr '\n' ' ')" = "run run crun run cmd " ] || fail "json: kinds $(json_get "$TMP/out.json" '*.kind' | tr '\n' ' ')"
if [ -n "$UNREADABLE" ]; then
  json_get "$TMP/out.json" 0.error | grep -q '^run_output_records_unreadable: ' || fail "json: the ? row's error"
  [ "$(json_get "$TMP/out.json" 0.path)" = "$(day "$J7")" ] || fail "json: the ? row's path"
  [ "$(json_get "$TMP/out.json" 0.operator.username)" = "$OP" ] || fail "json: the ? row's operator"
fi
karvi jdev job list --format json --target 'dead*' --last 3 --failed; expect jdev 0
[ "$(json_get "$TMP/out.jdev" '*.device' | tr '\n' ' ')" = "dead-iosxe " ] && [ "$(json_get "$TMP/out.jdev" '*.outcome' | tr '\n' ' ')" = "fail " ] || fail "json --target: $(head -c 200 "$TMP/out.jdev")"
karvi jnone job list --format json --operator nobody-at-all; expect jnone 0
[ "$(cat "$TMP/out.jnone")" = "[]" ] || fail "json, no rows: $(cat "$TMP/out.jnone")"

echo "job list smoke: ok"
