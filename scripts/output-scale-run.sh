#!/bin/sh
# The output-scale run:
# N devices answer `show big` at once through
# one daemon, and the daemon's resident memory is read from /proc while the
# job runs. It is a measurement, not a verifier suite: it asserts the
# accounting (N records, all succeeded, every output the expected size, N
# shells at the fake) and prints the memory it saw; what memory is
# acceptable is the gate's question, not this script's.
#
# All N names resolve to one fake IOS XE device (one host key, N identities
# enrolled under accept-new), so the run needs no more than the fake can
# serve. Needs the built executable, `go`, and /usr/bin/time (GNU time, its
# %M the client's peak). CHANNEL=exec runs the same
# over exec channels instead: the fake's Linux persona answering `big` (the
# same bytes), a platform `fexec` (driver linux, channel exec), and a key of
# the run's own as the operator's keys; it also needs `ssh-keygen`.
#
#   N=32 TRANSPORT=native scripts/output-scale-run.sh
#   N=32 TRANSPORT=system CHANNEL=exec scripts/output-scale-run.sh
#   N=32 FORMAT=text GATHER=1 scripts/output-scale-run.sh
#
# FORMAT=text measures the client's text display, the answer shown once per
# device; GATHER=1 its gathered display (display.run.gather), the client
# holding no output while the job runs and showing the N identical answers
# once, under one group header `! <targets> (N)`. The run asserts the copies
# the display shows.
#
# The spool directory is the base's under TMP (lab_places, scripts/lib/lab.sh);
# its size is sampled while the job runs
# and the peak is printed beside the memory,
# so the run measures where the responses go now that they leave memory.
#
# Settings: N (devices, default 8), TRANSPORT (system or native),
# BIG_LINES (lines of `show big`, 73 recorded bytes each; the default 72000
# is 5,256,000 bytes, just over 5 MiB), INFLIGHT (dispatch.server-max-inflight;
# the default 0 is the daemon's own min(256,max(32,8xCPU)), which caps the
# real concurrency below N on a small host, so the default here is N),
# FORMAT (the run's --format: jsonl, the default, json, or text), GATHER
# (1 for --set display.run.gather=true, text only; default 0).
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
. "$ROOT/scripts/lib/json.sh"
KARVI=${KARVI:-$ROOT/bin/karvi-linux-amd64}
. "$ROOT/scripts/lib/host.sh"; host_config_clean "$KARVI"   # refused on a host whose configuration reaches karvi
N=${N:-8}
TRANSPORT=${TRANSPORT:-native}
CHANNEL=${CHANNEL:-shell}
BIG_LINES=${BIG_LINES:-72000}
INFLIGHT=${INFLIGHT:-$N}
FORMAT=${FORMAT:-jsonl}
GATHER=${GATHER:-0}
TMP=${TMPDIR:-/tmp}/karvi-output-scale-$$
KH=$TMP/store/known_hosts
# shellcheck disable=SC2034 # set by fake_start, read by name: fake_stop FAKE_PID (scripts/lib/lab.sh)
FAKE_PID=
unset NETUSER NETPASS NETENABLE

fail() { echo "output scale: FAIL: $*" >&2; exit 1; }
cleanup() {
  daemon_stop_force "$TMP/base/socket/d.sock" env HOME="$TMP/home" "$KARVI" --config "$TMP/places.toml" --config "$TMP/karvi.toml" --quiet || :
  fake_stop FAKE_PID
  rm -rf "$TMP"
}
. "$ROOT/scripts/lib/exit.sh"; . "$ROOT/scripts/lib/lab.sh"; on_exit cleanup

case $FORMAT in
  jsonl | json | text) ;;
  *) fail "FORMAT is jsonl, json, or text, not $FORMAT" ;;
esac
case $GATHER in
  0) ;;
  1) [ "$FORMAT" = text ] || fail "GATHER=1 gathers the text display, not FORMAT=$FORMAT" ;;
  *) fail "GATHER is 0 or 1, not $GATHER" ;;
esac
[ -x /usr/bin/time ] || fail "no /usr/bin/time (GNU time) for the client's peak"

install -d -m 700 "$TMP" "$TMP/bin" "$TMP/store" "$TMP/home" "$TMP/base"
lab_places "$TMP/places.toml" "$TMP/base"
lab_build "$TMP/bin/fake" ./cmd/karvi-fake-device

# The operator's own trust store must not change (ssh.known-hosts-file is
# under TMP; HOME does not move the store).
OWN_STORE=$(host_own_store "$KARVI")
OWN_BEFORE=$(host_store_digest "$OWN_STORE")

# The platform, the command, and the fake's persona per channel; under exec
# the fake authorizes a key of the run's own, the operator's keys.
case $CHANNEL in
  shell) PLATFORM=cisco_ios; COMMAND='show big'; SESSIONS=$N; set -- ;;
  exec)
    PLATFORM=fexec; COMMAND=big; SESSIONS=0
    ssh-keygen -q -t ed25519 -N '' -C scale -f "$TMP/key" || fail "ssh-keygen"
    set -- -persona linux -user "$(id -un)" -authorized-keys "$TMP/key.pub" ;;
  *) fail "CHANNEL is shell or exec, not $CHANNEL" ;;
esac
fake_start "$TMP" "$TMP/fake.err" -big-lines "$BIG_LINES" "$@" || exit 1

: >"$KH"; chmod 600 "$KH"
{
  echo 'name,management_address,platform'
  i=1
  while [ "$i" -le "$N" ]; do printf 'big-%04d,127.0.0.1,%s\n' "$i" "$PLATFORM"; i=$((i + 1)); done
} >"$TMP/inv.csv"
cat >"$TMP/karvi.toml" <<EOF_CFG
platform-resolution.default = ""
[ssh]
host-key-policy = "accept-new"
known-hosts-file = "$KH"
connect-timeout = "10s"
identities = ["$TMP/key"]
[display]
color = "never"
[execution]
command-timeout = "120s"
[dispatch]
default = "parallel"
parallel-workers = $N
server-max-inflight = $INFLIGHT
[platform.cisco_ios]
ssh-port = $PORT
[platform.fexec]
driver = "linux"
channel = "exec"
ssh-port = $PORT
[[inventory-source]]
name = "scale"
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

karvi() { HOME=$TMP/home NETUSER=netops NETPASS=pw NETENABLE=en "$KARVI" --config "$TMP/places.toml" --config "$TMP/karvi.toml" "$@"; }
karvi --quiet daemon start >/dev/null || fail "the daemon did not start"
DPID=$(daemon_pid karvi)
[ -n "$DPID" ] || fail "no daemon pid in daemon status"
# VmRSS is the resident set now, VmHWM the kernel's own high-water mark for
# the process, so the daemon's peak needs no sampling loop; the client's is
# /usr/bin/time's %M, the kernel's maximum resident set of the process it
# forks, written at the client's exit.
status_kb() { awk -v k="$1:" '$1 == k {print $2}' "/proc/$2/status" 2>/dev/null || true; }
IDLE=$(status_kb VmRSS "$DPID")

START=$(date +%s.%N)
# Not through the karvi function: a function sent to the background runs in
# a subshell, and $! would be the subshell; here it is /usr/bin/time, which
# lives as long as the client it forks. The positional parameters, the
# fake's flags until now, become the gathered display's --set.
set --
[ "$GATHER" -eq 0 ] || set -- --set display.run.gather=true
HOME=$TMP/home NETUSER=netops NETPASS=pw NETENABLE=en /usr/bin/time -f %M -o "$TMP/client.kb" \
  "$KARVI" --config "$TMP/places.toml" --config "$TMP/karvi.toml" "$@" \
  run --all --transport "$TRANSPORT" --format "$FORMAT" "$COMMAND" >"$TMP/out" 2>"$TMP/err" &
CLIENT=$!
SPOOL_PEAK=0
SPOOL_FILES=0
while kill -0 "$CLIENT" 2>/dev/null; do
  # The spool directory's size and file count now (du in kB, apparent size).
  s=$(du -sk --apparent-size "$TMP/base/spool" 2>/dev/null | cut -f1); [ "${s:-0}" -le "$SPOOL_PEAK" ] || SPOOL_PEAK=$s
  f=$(ls "$TMP/base/spool" 2>/dev/null | wc -l); [ "$f" -le "$SPOOL_FILES" ] || SPOOL_FILES=$f
  sleep 0.05
done
CODE=0; wait "$CLIENT" || CODE=$?
END=$(date +%s.%N)
HWM=$(status_kb VmHWM "$DPID")
AFTER=$(status_kb VmRSS "$DPID")

[ "$CODE" -eq 0 ] || fail "run exited $CODE: $(grep -E '^[a-z_]+:' "$TMP/err" | head -1)"
CLIENT_PEAK=$(cat "$TMP/client.kb")
case $CLIENT_PEAK in '' | *[!0-9]*) fail "/usr/bin/time gave no client peak: $CLIENT_PEAK" ;; esac
JOB=$(newest_job "$TMP/base")
WANT_BYTES=$((BIG_LINES * 73))
records=$(wc -l <"$JOB/commands.jsonl")
ok=$(jsonl_records "$JOB/commands.jsonl" status output_bytes | grep -cx "$(printf 'success\t%s' "$WANT_BYTES")" || true)
[ "$records" -eq "$N" ] || fail "$records records, expected $N"
[ "$ok" -eq "$N" ] || fail "$ok records succeeded with $WANT_BYTES output bytes, expected $N"
# Under text the display shows every line of the answer once per device,
# but for an answer whose commands.jsonl line is more than a follow frame
# (daemon.max-ipc-frame-bytes), which the follow leaves out, a line saying so
# in its place; gathered, once, read back from commands.jsonl, none left
# out, under one group header (display.gather.header, its default
# `! <targets> (<count>)`) counting the N devices.
LEFT_OUT=0
if [ "$FORMAT" = text ]; then
  LEFT_OUT=$(grep -c '^karvi: output of [0-9]* bytes.* left out of the follow stream' "$TMP/out" || true)
  COPIES=$((N - LEFT_OUT))
  if [ "$GATHER" -eq 1 ]; then
    [ "$LEFT_OUT" -eq 0 ] || fail "the gathered display left $LEFT_OUT answers out"
    COPIES=1
    groups=$(grep -cx '! .* ([0-9][0-9]*)' "$TMP/out" || true)
    [ "$groups" -eq 1 ] || fail "the gathered display shows $groups group headers, expected 1"
    grep -qx "! .* ($N)" "$TMP/out" || fail "the group header does not end ($N): $(grep -x '! .* ([0-9][0-9]*)' "$TMP/out")"
  fi
  shown=$(grep -cx 'line [0-9]\{6\} x\{60\}' "$TMP/out" || true)
  [ "$shown" -eq $((COPIES * BIG_LINES)) ] || fail "the text display shows $shown lines of the answer, expected $COPIES copies of its $BIG_LINES"
fi
daemon_stop_force "$TMP/base/socket/d.sock" karvi --quiet || fail "the daemon socket remains"
fake_stop FAKE_PID
grep -q "^connections=$N sessions=$SESSIONS\$" "$TMP/fake.err" || fail "the fake saw $(grep '^connections=' "$TMP/fake.err"), expected $N connections and $SESSIONS shells"
[ "$(host_store_digest "$OWN_STORE")" = "$OWN_BEFORE" ] || fail "the operator's trust store $OWN_STORE changed"

# The responses held at once: N, or the server's cap when it is lower; an
# INFLIGHT of 0 is the daemon's default, computed here as the registry
# documents it.
CAP=$INFLIGHT
if [ "$CAP" -eq 0 ]; then
  CAP=$(( $(nproc) * 8 )); [ "$CAP" -ge 32 ] || CAP=32; [ "$CAP" -le 256 ] || CAP=256
fi
[ "$N" -ge "$CAP" ] || CAP=$N
awk -v n="$N" -v tr="$TRANSPORT" -v ch="$CHANNEL" -v format="$FORMAT" -v gather="$GATHER" -v inflight="$CAP" -v bytes="$WANT_BYTES" -v idle="$IDLE" -v hwm="$HWM" -v after="$AFTER" \
  -v client="$CLIENT_PEAK" -v leftout="$LEFT_OUT" -v wall="$(echo "$END - $START" | bc)" -v jsonl="$(wc -c <"$JOB/commands.jsonl")" \
  -v spool="$SPOOL_PEAK" -v files="$SPOOL_FILES" -v left="$(ls "$TMP/base/spool" 2>/dev/null | wc -l)" 'BEGIN {
  printf "output scale: n=%d transport=%s channel=%s format=%s gather=%d inflight=%d output_bytes=%d wall=%.1fs\n", n, tr, ch, format, gather, inflight, bytes, wall
  printf "output scale: daemon kB idle=%d peak=%d after=%d; per in-flight response %.1f MB, %.1fx its output\n", idle, hwm, after, (hwm - idle) / 1024 / inflight, (hwm - idle) * 1024 / inflight / bytes
  printf "output scale: client peak kB=%d (/usr/bin/time); commands.jsonl bytes=%d; %d records succeeded; %d answers left out of the display\n", client, jsonl, n, leftout
  printf "output scale: spool peak kB=%d in %d files under spooldir; %d left after\n", spool, files, left
}'
echo 'output scale: pass'
