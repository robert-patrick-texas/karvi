#!/bin/sh
# The spool suite: the output
# spool and the memory budget against the fake IOS XE device built from the
# tree, over the native transport, in process and through one daemon. Each
# row asserts through scripts/lib/json.sh. A response of 65.7 MB is the
# fake's `show big` at 900,000 lines, the size that streams for over a second
# so a cut lands inside it; the small answers are the fake's usual ones.
# Needs the native executable, karvi-prune beside it, and `go`.
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
KARVI=${KARVI:-$ROOT/bin/karvi-linux-amd64}
PRUNE=${KARVI_PRUNE:-$(dirname "$KARVI")/karvi-prune}
GO=${GO:-go}
. "$ROOT/scripts/lib/json.sh"
TMP=${TMPDIR:-/tmp}/karvi-spool-smoke-$$
KH=$TMP/store/known_hosts
SPOOL=$TMP/spool
SCORE=$TMP/base/score
FAKE_PID=
PORT=
unset NETUSER NETPASS NETENABLE

fail() { echo "spool smoke: FAIL: $*" >&2; exit 1; }
stop_fake() {
  [ -n "$FAKE_PID" ] || return 0
  kill "$FAKE_PID" 2>/dev/null || true
  wait "$FAKE_PID" 2>/dev/null || true
  FAKE_PID=
}
stop_daemon() {
  [ -S "$TMP/base/socket/daemon.sock" ] || return 0
  HOME=$TMP/home "$KARVI" --quiet --config "$TMP/karvi.toml" daemon stop --force >/dev/null 2>&1 || true
  i=0
  while [ -S "$TMP/base/socket/daemon.sock" ] && [ $i -lt 100 ]; do sleep 0.1; i=$((i + 1)); done
  [ ! -S "$TMP/base/socket/daemon.sock" ] || fail "the daemon socket remains"
}
cleanup() { stop_daemon 2>/dev/null || true; stop_fake; rm -rf "$TMP"; }
trap cleanup EXIT HUP INT TERM

"$KARVI" version --format json | grep -q '"id": "scrapligo-v1"' || fail "$KARVI lacks scrapligo-v1; build with make native-build"
[ -x "$PRUNE" ] || fail "karvi-prune not found at $PRUNE"
install -d -m 700 "$TMP" "$TMP/bin" "$TMP/store" "$TMP/home" "$TMP/base" "$SPOOL"
(cd "$ROOT" && GOTOOLCHAIN=local "$GO" build -mod=vendor -o "$TMP/bin/fake" ./cmd/karvi-fake-device)

# The operator's own trust store must not change (ssh.known-hosts-file is
# under TMP; HOME does not move the store).
. "$ROOT/scripts/lib/host.sh"
OWN_STORE=$(host_own_store "$KARVI")
OWN_BEFORE=$(host_store_digest "$OWN_STORE")

# start_fake: the fake on the port it took first (a restart is the same
# device), `show big` at 65.7 MB, `show slow` at three seconds; its pid is
# the fake's own.
start_fake() {
  : >"$TMP/port"
  "$TMP/bin/fake" -host-key-file "$TMP/hostkey" -big-lines 900000 -slow 3s -port "${PORT:-0}" 2>"$TMP/fake.err" >"$TMP/port" &
  FAKE_PID=$!
  i=0
  while [ ! -s "$TMP/port" ] && [ $i -lt 100 ]; do sleep 0.05; i=$((i + 1)); done
  [ -s "$TMP/port" ] || fail "the fake did not start: $(head -1 "$TMP/fake.err")"
  PORT=$(cat "$TMP/port")
}
start_fake
: >"$KH"; chmod 600 "$KH"

# The inventory: the fake under one name, and enough names on its address
# for the width the narrowing row needs.
FITS=$(( $(df --output=avail -B1 "$SPOOL" | tail -1) / 1073741824 ))
DEVICES=$((FITS + 8))
[ "$DEVICES" -le 200 ] || DEVICES=0
{
  echo 'name,management_address,platform'
  echo 'fake-iosxe,127.0.0.1,cisco_iosxe'
  i=1
  while [ "$i" -le "$DEVICES" ]; do printf 'wide-%03d,127.0.0.1,cisco_iosxe\n' "$i"; i=$((i + 1)); done
} >"$TMP/inv.csv"
cat >"$TMP/karvi.toml" <<EOF_CFG
basedir = "$TMP/base"
sharedroot = "none"
spooldir = "$SPOOL"
platform-resolution.default = ""
[ssh]
host-key-policy = "accept-new"
known-hosts-file = "$KH"
connect-timeout = "5s"
[audit]
journald-required = false
file = "$TMP/base/audit.jsonl"
[watch]
directory = "$SCORE"
refresh = "250ms"
[sessions]
shared-capacity-root = "$TMP/base/cap"
[display]
color = "never"
[execution]
command-timeout = "20s"
[output]
# Every answer spools (the suites' way): the daemon's jobs read
# the daemon's configuration, not a client's --set, so the threshold is
# here; a row that wants memory sets 1 GiB.
spool-threshold-bytes = 0
[dispatch]
default = "parallel"
parallel-workers = 200
server-max-inflight = 200
[platform.cisco_iosxe]
ssh-port = $PORT
[[inventory-source]]
name = "spool"
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
[name-transform.default]
operations = [{op = "lowercase"}]
EOF_CFG
chmod 600 "$TMP/karvi.toml"

# karvi TAG ARGS...: the client with the fake's credentials; its output in
# out.TAG, its diagnostics in err.TAG, its exit in CODE.
karvi() {
  tag=$1; shift
  CODE=0
  HOME=$TMP/home NETUSER=netops NETPASS=pw NETENABLE=en "$KARVI" --config "$TMP/karvi.toml" "$@" >"$TMP/out.$tag" 2>"$TMP/err.$tag" || CODE=$?
}
# record TAG: the first record of out.TAG into rec.json (a jsonl line).
record() { head -1 "$TMP/out.$1" >"$TMP/rec.json"; [ -s "$TMP/rec.json" ] || fail "$1: no record"; }
# output_digest: the SHA-256 of rec.json's output field, as the record's
# output_sha256 states it (an output that is UTF-8; none here is base64).
output_digest() { json_get "$TMP/rec.json" output | head -c -1 | sha256sum | cut -d' ' -f1; }
# spool_empty ROW: nothing left under the spool directory.
spool_empty() { [ "$(ls -A "$SPOOL" | wc -l)" -eq 0 ] || fail "$1: the spool directory holds $(ls -A "$SPOOL")"; }
# check_record ROW STATUS: rec.json's status, its output's digest against
# output_sha256, and its byte count against the field.
check_record() {
  json_is "$TMP/rec.json" status "$2" || fail "$1: status $(json_get "$TMP/rec.json" status), expected $2"
  [ "$(output_digest)" = "$(json_get "$TMP/rec.json" output_sha256)" ] || fail "$1: the output's digest is not output_sha256"
  [ "$(json_get "$TMP/rec.json" output | head -c -1 | wc -c)" -eq "$(json_get "$TMP/rec.json" output_bytes)" ] || fail "$1: output_bytes does not count the output"
}
BIG_BYTES=65700000

# S1: threshold 0 spools a small answer from its first byte; the record is
# the memory form's, the spool opened and removed.
karvi s1 --set output.spool-threshold-bytes=0 --debug cmd fake-iosxe --format jsonl --transport scrapligo-v1 --cmd 'show clock'
[ "$CODE" -eq 0 ] || fail "S1: exit $CODE: $(grep -E '^[a-z_]+:' "$TMP/err.s1" | head -1)"
record s1; check_record S1 succeeded
json_is "$TMP/rec.json" output_bytes 34 || fail "S1: output_bytes $(json_get "$TMP/rec.json" output_bytes)"
grep -q 'device command spooled target="fake-iosxe" index=1 .*bytes=34' "$TMP/err.s1" || fail "S1: the debug stream lacks the spool's opening"
grep -q 'device command spool removed target="fake-iosxe" index=1' "$TMP/err.s1" || fail "S1: the debug stream lacks the spool's removal"
spool_empty S1
echo "spool smoke: S1 threshold 0 spools a 34-byte answer, the spool removed"

# S2: the reference equality: the 65.7 MB response through the spool at the
# default threshold and whole in memory at a threshold above its size give
# the same record bytes and digest, and the same text block; a threshold
# that crosses mid-line (5000 bytes, the lines are 73)
# gives the same again. Mid-character is the reader's reference test: the
# fake's output is ASCII.
prev_digest=""; prev_text=""
for th in 1048576 1073741824 5000; do
  karvi s2 --set output.spool-threshold-bytes=$th cmd fake-iosxe --format jsonl --transport scrapligo-v1 --cmd 'show big'
  [ "$CODE" -eq 0 ] || fail "S2 ($th): exit $CODE: $(grep -E '^[a-z_]+:' "$TMP/err.s2" | head -1)"
  record s2; check_record "S2 ($th)" succeeded
  json_is "$TMP/rec.json" output_bytes $BIG_BYTES || fail "S2 ($th): output_bytes $(json_get "$TMP/rec.json" output_bytes)"
  digest=$(json_get "$TMP/rec.json" output_sha256)
  [ -z "$prev_digest" ] || [ "$digest" = "$prev_digest" ] || fail "S2 ($th): digest $digest differs from $prev_digest"
  prev_digest=$digest
  job=$(ls -d "$TMP"/base/jobs/*/* | tail -1)
  # The text file: the header's time differs per run; the set-up lines
  # and the block after it hold the output whole and are the same size
  # on every path.
  text=$(tail -n +2 "$job/output.fake-iosxe.txt" | wc -c)
  [ "$text" -gt $BIG_BYTES ] || fail "S2 ($th): the text block is $text bytes"
  [ -z "$prev_text" ] || [ "$text" -eq "$prev_text" ] || fail "S2 ($th): the text block is $text bytes, before $prev_text"
  prev_text=$text
  tail -c 74 "$job/output.fake-iosxe.txt" | grep -q '^line 900000 x' || fail "S2 ($th): the text block does not end with the output's last line"
  spool_empty "S2 ($th)"
done
echo "spool smoke: S2 the 65.7 MB record equal through the spool, in memory, and across a mid-line threshold ($prev_digest)"

# S3: a failure pattern in a spooled answer is found as the bytes settle:
# `show bogus` at threshold 0 is device_command_error.
karvi s3 --set output.spool-threshold-bytes=0 cmd fake-iosxe --format jsonl --transport scrapligo-v1 --cmd 'show bogus'
record s3; check_record S3 device_error
json_is "$TMP/rec.json" error.code device_command_error || fail "S3: $(json_get "$TMP/rec.json" error.code)"
spool_empty S3
echo "spool smoke: S3 the failure pattern found through the spool"

# S4: the cut endings record the settled bytes:
# the limit holds exactly the first 20,000 settled bytes and names the
# settled count observed; the command timeout at one second holds what
# settled by the cut; a cancel (SIGINT to the client mid-stream) and a lost
# session (the fake killed mid-stream, then restarted on its port) hold what
# settled too. Every spool is gone after.
karvi s4l --set output.max-command-bytes=20000 cmd fake-iosxe --format jsonl --transport scrapligo-v1 --cmd 'show big'
[ "$CODE" -eq 111 ] || fail "S4 limit: exit $CODE"
record s4l; check_record "S4 limit" output_limit_exceeded
json_is "$TMP/rec.json" output_bytes 20000 || fail "S4 limit: output_bytes $(json_get "$TMP/rec.json" output_bytes)"
observed=$(json_get "$TMP/rec.json" error.message | sed -n 's/.*, \([0-9]*\) observed.*/\1/p')
[ -n "$observed" ] && [ "$observed" -gt 20000 ] && [ "$observed" -lt 30000 ] || fail "S4 limit: observed $observed"
karvi s4t --set 'execution.command-timeout="1s"' cmd fake-iosxe --format jsonl --transport scrapligo-v1 --cmd 'show big'
[ "$CODE" -eq 107 ] || fail "S4 timeout: exit $CODE"
record s4t; check_record "S4 timeout" timeout
json_is "$TMP/rec.json" error.code command_timeout || fail "S4 timeout: $(json_get "$TMP/rec.json" error.code)"
[ "$(json_get "$TMP/rec.json" output_bytes)" -gt 1048576 ] || fail "S4 timeout: only $(json_get "$TMP/rec.json" output_bytes) bytes settled by the cut"
spool_empty "S4 timeout"
# The cancel: the client interrupted 0.7 s into the command, past the fake's
# build of its answer and inside the stream.
HOME=$TMP/home NETUSER=netops NETPASS=pw NETENABLE=en "$KARVI" --config "$TMP/karvi.toml" cmd fake-iosxe --format jsonl --transport scrapligo-v1 --cmd 'show big' >"$TMP/out.s4c" 2>"$TMP/err.s4c" &
CLIENT=$!
sleep 0.7; kill -INT "$CLIENT"
CODE=0; wait "$CLIENT" || CODE=$?
[ "$CODE" -ne 0 ] || fail "S4 cancel: the client exited 0"
record s4c; check_record "S4 cancel" cancelled
spool_empty "S4 cancel"
# The lost session: the fake killed 0.7 s in, then the same device again.
HOME=$TMP/home NETUSER=netops NETPASS=pw NETENABLE=en "$KARVI" --config "$TMP/karvi.toml" cmd fake-iosxe --format jsonl --transport scrapligo-v1 --cmd 'show big' >"$TMP/out.s4x" 2>"$TMP/err.s4x" &
CLIENT=$!
sleep 0.7; kill -9 "$FAKE_PID" 2>/dev/null || true; wait "$FAKE_PID" 2>/dev/null || true; FAKE_PID=
CODE=0; wait "$CLIENT" || CODE=$?
[ "$CODE" -ne 0 ] || fail "S4 lost: the client exited 0"
record s4x; check_record "S4 lost" connection_error
spool_empty "S4 lost"
start_fake
echo "spool smoke: S4 the limit (20000 held, $observed observed), the timeout, the cancel, and the lost session record their settled bytes"

# S5: the sweep at admission: a spool named for a dead pid
# goes, one named for the running daemon's pid stays, a file of another
# shape is not touched; karvi-prune leaves the spool directory alone.
karvi s5d --quiet daemon start
[ "$CODE" -eq 0 ] || fail "S5: the daemon did not start: $(cat "$TMP/err.s5d")"
DPID=$(json_get "$TMP/base/state/daemon.json" pid)
sh -c 'exit 0' & DEAD=$!; wait "$DEAD" 2>/dev/null || true
: >"$SPOOL/260927-100000-00.fake-iosxe.1.$DEAD.spool"
: >"$SPOOL/260927-100000-00.fake-iosxe.2.$DPID.spool"
: >"$SPOOL/notes.txt"
rc=0; "$PRUNE" --basedir "$TMP/base" --sharedroot none --scoreboards "$SCORE" --days 0 >"$TMP/out.prune" 2>"$TMP/err.prune" || rc=$?
[ "$(ls -A "$SPOOL" | wc -l)" -eq 3 ] || fail "S5: karvi-prune touched the spool directory: $(ls -A "$SPOOL")"
karvi s5 --debug cmd fake-iosxe --format jsonl --transport scrapligo-v1 --cmd 'show clock'
[ "$CODE" -eq 0 ] || fail "S5: exit $CODE"
grep -q "spool_abandoned_removed: removed the abandoned spool $SPOOL/260927-100000-00.fake-iosxe.1.$DEAD.spool" "$TMP/err.s5" || fail "S5: the sweep did not report the dead pid's spool"
[ ! -e "$SPOOL/260927-100000-00.fake-iosxe.1.$DEAD.spool" ] || fail "S5: the dead pid's spool remains"
[ -e "$SPOOL/260927-100000-00.fake-iosxe.2.$DPID.spool" ] || fail "S5: the daemon's spool was swept"
[ -e "$SPOOL/notes.txt" ] || fail "S5: a file of another shape was touched"
rm -f "$SPOOL/260927-100000-00.fake-iosxe.2.$DPID.spool" "$SPOOL/notes.txt"
echo "spool smoke: S5 the sweep removed the dead pid's spool and kept the daemon's; karvi-prune left the directory alone"

# S6: the follower: a spooled record that fits the frame
# reaches the client from the file, its jsonl equal to commands.jsonl; one
# over the frame bound arrives with its output omitted and the notice; a
# job without commands.jsonl sends a spooled record with "not kept". The
# daemon spools every answer (the configuration's threshold 0).
karvi s6a run --target fake-iosxe --format jsonl 'show running-config'
[ "$CODE" -eq 0 ] || fail "S6 file: exit $CODE: $(grep -E '^[a-z_]+:' "$TMP/err.s6a" | head -1)"
job=$(ls -d "$TMP"/base/jobs/*/* | tail -1)
# The records byte for byte, then the summary line.
head -n "$(wc -l <"$job/commands.jsonl")" "$TMP/out.s6a" | cmp -s - "$job/commands.jsonl" || fail "S6 file: the follower's jsonl differs from commands.jsonl"
record s6a; check_record "S6 file" succeeded
karvi s6b run --target fake-iosxe --format jsonl 'show big'
[ "$CODE" -eq 0 ] || fail "S6 bound: exit $CODE"
record s6b
json_is "$TMP/rec.json" output_bytes $BIG_BYTES || fail "S6 bound: output_bytes $(json_get "$TMP/rec.json" output_bytes)"
json_is "$TMP/rec.json" output "" || fail "S6 bound: the output was sent"
json_is "$TMP/rec.json" notices.0.code follow_output_omitted || fail "S6 bound: $(json_get "$TMP/rec.json" notices.0.code)"
json_get "$TMP/rec.json" notices.0.message | grep -q 'the output is in commands.jsonl' || fail "S6 bound: $(json_get "$TMP/rec.json" notices.0.message)"
karvi s6c --set output.files.commands-jsonl=false run --target fake-iosxe --format jsonl 'show clock'
[ "$CODE" -eq 0 ] || fail "S6 not kept: exit $CODE: $(grep -E '^[a-z_]+:' "$TMP/err.s6c" | head -1)"
record s6c
json_is "$TMP/rec.json" output "" || fail "S6 not kept: the output was sent"
json_get "$TMP/rec.json" notices.0.message | grep -q 'so the output is not kept' || fail "S6 not kept: $(json_get "$TMP/rec.json" notices.0.message)"
spool_empty S6
echo "spool smoke: S6 the follower fed from the file, omitted over the bound, and told when not kept"

# S7: `cmd --nof` displays a spooled record and creates no job folder:
# the text format streams from the spool before its
# removal.
before=$(ls -d "$TMP"/base/jobs/*/* 2>/dev/null | wc -l)
karvi s7 --set output.spool-threshold-bytes=0 cmd fake-iosxe --format text --nof --transport scrapligo-v1 --cmd 'show clock'
[ "$CODE" -eq 0 ] || fail "S7: exit $CODE: $(grep -E '^[a-z_]+:' "$TMP/err.s7" | head -1)"
grep -q '^\*10:00:00.000 UTC Tue Sep 15 2026$' "$TMP/out.s7" || fail "S7: the display lacks the answer: $(cat "$TMP/out.s7")"
[ "$(ls -d "$TMP"/base/jobs/*/* 2>/dev/null | wc -l)" -eq "$before" ] || fail "S7: --nof created a job folder"
spool_empty S7
echo "spool smoke: S7 --nof displayed the spooled answer without a job folder"

# S8: the count on the scoreboard during a command: the
# target row's bytes and the metrics' in_flight_bytes above zero at some
# beat of the 65.7 MB response, the snapshot at schema 3, and both back to
# zero at the end. The file is copied before it is read: the heartbeat
# rewrites it every 250 ms.
rm -f "$SCORE"/*.json
HOME=$TMP/home NETUSER=netops NETPASS=pw NETENABLE=en "$KARVI" --config "$TMP/karvi.toml" cmd fake-iosxe --format jsonl --transport scrapligo-v1 --cmd 'show big' >"$TMP/out.s8" 2>"$TMP/err.s8" &
CLIENT=$!
seen=0; inflight=0
while kill -0 "$CLIENT" 2>/dev/null; do
  for f in "$SCORE"/*.json; do
    [ -s "$f" ] || continue
    cp "$f" "$TMP/snap.json" 2>/dev/null || continue
    b=$(json_get "$TMP/snap.json" targets.0.bytes 2>/dev/null || echo 0)
    m=$(json_get "$TMP/snap.json" metrics.in_flight_bytes 2>/dev/null || echo 0)
    [ "$b" -le "$seen" ] || seen=$b
    [ "$m" -le "$inflight" ] || inflight=$m
  done
  sleep 0.1
done
CODE=0; wait "$CLIENT" || CODE=$?
[ "$CODE" -eq 0 ] || fail "S8: exit $CODE"
[ "$seen" -gt 0 ] && [ "$inflight" -gt 0 ] || fail "S8: no beat carried the running count (bytes $seen, in flight $inflight)"
snap=$(ls "$SCORE"/*.json | head -1)
json_is "$snap" schema_version 3 || fail "S8: schema $(json_get "$snap" schema_version)"
json_is "$snap" targets.0.bytes 0 || fail "S8: the ended target still counts $(json_get "$snap" targets.0.bytes)"
json_is "$snap" metrics.in_flight_bytes 0 || fail "S8: in_flight_bytes $(json_get "$snap" metrics.in_flight_bytes) at the end"
json_is "$snap" status completed || fail "S8: status $(json_get "$snap" status)"
echo "spool smoke: S8 the scoreboard carried $seen bytes in flight during the response and 0 at the end"

# S9: the free-space check's three words as this host allows (the one
# check of every volume): `auto` (the default)
# passed every row above; `never` reads nothing; `auto` narrows a job of
# more devices than the disk holds at the 1 GiB limit, above the floor and
# the job's own files, to the width that fits, with the warning, when the
# host's free space lets the suite ask for that many devices.
karvi s9n --set 'freecheck="never"' --debug cmd fake-iosxe --format jsonl --transport scrapligo-v1 --cmd 'show clock'
[ "$CODE" -eq 0 ] || fail "S9 never: exit $CODE"
grep -q 'freecheck=never volumes=0' "$TMP/err.s9n" || fail "S9 never: the debug stream lacks the word and the empty read"
if [ "$DEVICES" -gt 0 ]; then
  karvi s9a --set output.max-command-bytes=1073741824 run --no-daemon --target 'wide-*' --format jsonl 'show clock'
  [ "$CODE" -eq 0 ] || fail "S9 auto: exit $CODE: $(grep -E '^[a-z_]+:' "$TMP/err.s9a" | head -1)"
  grep -q "warning: spool_width_narrowed: .*$SPOOL has [0-9]* bytes free, 1073741824 per command in flight (output.max-command-bytes) above the [0-9]* bytes the job's other files and the floor take: the job runs [0-9]* at a time instead of $DEVICES" "$TMP/err.s9a" || fail "S9 auto: no narrowing warning: $(grep warning "$TMP/err.s9a" | head -1)"
  [ "$(wc -l <"$TMP/out.s9a")" -eq $((DEVICES + 1)) ] || fail "S9 auto: $(wc -l <"$TMP/out.s9a") lines for $DEVICES devices and the summary"
  echo "spool smoke: S9 never read nothing; auto narrowed $DEVICES devices to what $FITS GiB holds"
else
  echo "spool smoke: S9 never read nothing; auto not narrowed here (the disk holds more than 192 limits)"
fi

# S10: the floor on every volume. `always` with the floor a
# byte above the free space refuses before any device, naming the job's
# path and the spool on their volume; a `--nof` command still asks the
# spool's volume; a crun whose collection directory sits on another volume
# with less room is refused naming that directory alone, and runs under
# `never`. The other-volume row needs a tmpfs at /dev/shm on a device
# other than the suite's, with less free space than the suite's volume.
# The floors sit a GiB above the free space read here: the suites run in
# lanes beside one another and free space moves by more than a byte.
AVAIL=$(df --output=avail -B1 "$TMP" | tail -1)
FLOOR=$((AVAIL + 1073741824))
if [ "$FLOOR" -lt 1099511627776 ]; then
  karvi s10a --set 'freecheck="always"' --set output.min-free-bytes-after-job=$FLOOR cmd fake-iosxe --format jsonl --transport scrapligo-v1 --cmd 'show clock'
  [ "$CODE" -eq 111 ] || fail "S10 always: exit $CODE, expected 111"
  grep -q "^output_preflight_space: $TMP/base/jobs/[0-9]*/[0-9-]*, $SPOOL: need $FLOOR bytes free, only [0-9]* free" "$TMP/err.s10a" || fail "S10 always: the refusal does not name both paths and the figures: $(head -1 "$TMP/err.s10a")"
  # The refused job's reserved folder is released: the path the refusal
  # names is gone.
  s10_dir=$(sed -n 's/^output_preflight_space: \([^,]*\),.*/\1/p' "$TMP/err.s10a")
  [ -n "$s10_dir" ] && [ ! -d "$s10_dir" ] || fail "S10 always: the refused job's folder $s10_dir was left behind"
  karvi s10n --set 'freecheck="always"' --set output.min-free-bytes-after-job=$FLOOR cmd --nof fake-iosxe --format jsonl --transport scrapligo-v1 --cmd 'show clock'
  [ "$CODE" -eq 111 ] || fail "S10 nof: exit $CODE, expected 111"
  grep -q "^output_preflight_space: $SPOOL: need" "$TMP/err.s10n" || fail "S10 nof: the spool's volume alone is named: $(head -1 "$TMP/err.s10n")"
  echo "spool smoke: S10 always refused at the floor, the job's folder and the spool named; --nof asked the spool's volume"
else
  echo "spool smoke: S10 always not exercised (more than 1 TiB free)"
fi
SHM_AVAIL=$(df --output=avail -B1 /dev/shm 2>/dev/null | tail -1)
SHM_FLOOR=$((${SHM_AVAIL:-0} + 1073741824))
if [ -d /dev/shm ] && [ "$(stat -c %d /dev/shm)" != "$(stat -c %d "$TMP")" ] && [ -n "$SHM_AVAIL" ] && [ "$SHM_FLOOR" -lt "$AVAIL" ] && [ "$SHM_FLOOR" -lt 1099511627776 ]; then
  CRUN_SHM=$(mktemp -d /dev/shm/karvi-spool-smoke-XXXXXX)
  karvi s10c --set output.min-free-bytes-after-job=$SHM_FLOOR --set "crun.directory=\"$CRUN_SHM\"" crun --no-daemon --target fake-iosxe --format jsonl
  [ "$CODE" -eq 111 ] || { rm -rf "${CRUN_SHM:?}"; fail "S10 crun: exit $CODE, expected 111: $(head -1 "$TMP/err.s10c")"; }
  # The ask is the floor plus the collection's one copy of the device
  # estimate, so the row checks the path alone and the shortfall's shape.
  grep -q "^output_preflight_space: $CRUN_SHM: need [0-9]* bytes free, only [0-9]* free" "$TMP/err.s10c" || { rm -rf "${CRUN_SHM:?}"; fail "S10 crun: the other volume is named alone: $(head -1 "$TMP/err.s10c")"; }
  [ "$(ls -A "$CRUN_SHM" | wc -l)" -eq 0 ] || { rm -rf "${CRUN_SHM:?}"; fail "S10 crun: the refused crun wrote into $CRUN_SHM"; }
  karvi s10cn --set 'freecheck="never"' --set output.min-free-bytes-after-job=$SHM_FLOOR --set "crun.directory=\"$CRUN_SHM\"" crun --no-daemon --target fake-iosxe --format jsonl
  [ "$CODE" -eq 0 ] && [ -s "$CRUN_SHM/fake-iosxe" ] || { rm -rf "${CRUN_SHM:?}"; fail "S10 crun never: exit $CODE, file $(ls -A "$CRUN_SHM")"; }
  rm -rf "${CRUN_SHM:?}"
  echo "spool smoke: S10 the collection directory on /dev/shm refused alone at its floor and written under never"
else
  echo "spool smoke: S10 the other-volume row not exercised here (/dev/shm not a smaller separate volume)"
fi

stop_daemon
stop_fake
grep -q "^connections=" "$TMP/fake.err" || fail "the fake left no summary"
[ "$(host_store_digest "$OWN_STORE")" = "$OWN_BEFORE" ] || fail "the operator's trust store $OWN_STORE changed"
echo "spool smoke: pass"
