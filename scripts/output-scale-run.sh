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
# serve. Needs the built executable and `go`. CHANNEL=exec runs the same
# over exec channels instead: the fake's Linux persona answering `big` (the
# same bytes), a platform `fexec` (driver linux, channel exec), and a key of
# the run's own as the operator's keys; it also needs `ssh-keygen`.
#
#   N=32 TRANSPORT=scrapligo-v1 scripts/output-scale-run.sh
#   N=32 TRANSPORT=system CHANNEL=exec scripts/output-scale-run.sh
#
# The spool directory is under TMP; its size is sampled while the job runs
# and the peak is printed beside the memory,
# so the run measures where the responses go now that they leave memory.
#
# Settings: N (devices, default 8), TRANSPORT (system or scrapligo-v1),
# BIG_LINES (lines of `show big`, 73 recorded bytes each; the default 72000
# is 5,256,000 bytes, just over 5 MiB), INFLIGHT (dispatch.server-max-inflight;
# the default 0 is the daemon's own min(256,max(32,8xCPU)), which caps the
# real concurrency below N on a small host, so the default here is N).
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
. "$ROOT/scripts/lib/json.sh"
KARVI=${KARVI:-$ROOT/bin/karvi-linux-amd64}
GO=${GO:-go}
N=${N:-8}
TRANSPORT=${TRANSPORT:-scrapligo-v1}
CHANNEL=${CHANNEL:-shell}
BIG_LINES=${BIG_LINES:-72000}
INFLIGHT=${INFLIGHT:-$N}
TMP=${TMPDIR:-/tmp}/karvi-output-scale-$$
KH=$TMP/store/known_hosts
FAKE_PID=
unset NETUSER NETPASS NETENABLE

fail() { echo "output scale: FAIL: $*" >&2; exit 1; }
cleanup() {
  if [ -S "$TMP/base/socket/d.sock" ]; then
    HOME=$TMP/home "$KARVI" --quiet --config "$TMP/karvi.toml" daemon stop --force >/dev/null 2>&1 || true
  fi
  [ -z "$FAKE_PID" ] || { kill "$FAKE_PID" 2>/dev/null || true; wait "$FAKE_PID" 2>/dev/null || true; }
  rm -rf "$TMP"
}
trap cleanup EXIT HUP INT TERM

install -d -m 700 "$TMP" "$TMP/bin" "$TMP/store" "$TMP/home" "$TMP/base"
(cd "$ROOT" && GOTOOLCHAIN=local "$GO" build -mod=vendor -o "$TMP/bin/fake" ./cmd/karvi-fake-device)

# The operator's own trust store must not change (ssh.known-hosts-file is
# under TMP; HOME does not move the store).
. "$ROOT/scripts/lib/host.sh"
OWN_STORE=$(host_own_store "$KARVI")
OWN_BEFORE=$(host_store_digest "$OWN_STORE")

# The platform, the command, and the fake's persona per channel; under exec
# the fake authorizes a key of the run's own, the operator's keys.
case $CHANNEL in
  shell) PLATFORM=cisco_iosxe; COMMAND='show big'; SESSIONS=$N; set -- ;;
  exec)
    PLATFORM=fexec; COMMAND=big; SESSIONS=0
    ssh-keygen -q -t ed25519 -N '' -C scale -f "$TMP/key" || fail "ssh-keygen"
    set -- -persona linux -user "$(id -un)" -authorized-keys "$TMP/key.pub" ;;
  *) fail "CHANNEL is shell or exec, not $CHANNEL" ;;
esac
: >"$TMP/port"
"$TMP/bin/fake" -host-key-file "$TMP/hostkey" -big-lines "$BIG_LINES" "$@" 2>"$TMP/fake.err" >"$TMP/port" &
FAKE_PID=$!
i=0
while [ ! -s "$TMP/port" ] && [ $i -lt 100 ]; do sleep 0.05; i=$((i + 1)); done
[ -s "$TMP/port" ] || fail "the fake did not start: $(head -1 "$TMP/fake.err")"
PORT=$(cat "$TMP/port")

: >"$KH"; chmod 600 "$KH"
{
  echo 'name,management_address,platform'
  i=1
  while [ "$i" -le "$N" ]; do printf 'big-%04d,127.0.0.1,%s\n' "$i" "$PLATFORM"; i=$((i + 1)); done
} >"$TMP/inv.csv"
cat >"$TMP/karvi.toml" <<EOF_CFG
basedir = "$TMP/base"
sharedroot = "none"
spooldir = "$TMP/spool"
scoreboards = "$TMP/base/score"
platform-resolution.default = ""
[ssh]
host-key-policy = "accept-new"
known-hosts-file = "$KH"
connect-timeout = "10s"
identities = ["$TMP/key"]
[audit]
journald-required = false
file = "$TMP/base/audit.jsonl"
[sessions]
shared-capacity-root = "$TMP/base/cap"
[display]
color = "never"
[execution]
command-timeout = "120s"
[dispatch]
default = "parallel"
parallel-workers = $N
server-max-inflight = $INFLIGHT
[platform.cisco_iosxe]
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

karvi() { HOME=$TMP/home NETUSER=netops NETPASS=pw NETENABLE=en "$KARVI" --config "$TMP/karvi.toml" "$@"; }
karvi --quiet daemon start >/dev/null || fail "the daemon did not start"
DPID=$(karvi daemon status --format json | sed -n 's/.*"pid": *\([0-9]*\).*/\1/p' | head -1)
[ -n "$DPID" ] || fail "no daemon pid in daemon status"
# VmRSS is the resident set now, VmHWM the kernel's own high-water mark for
# the process, so the peak needs no sampling loop; the client is sampled
# because it is gone before its mark can be read.
status_kb() { awk -v k="$1:" '$1 == k {print $2}' "/proc/$2/status" 2>/dev/null || true; }
IDLE=$(status_kb VmRSS "$DPID")

START=$(date +%s.%N)
# Not through the karvi function: a function sent to the background runs in
# a subshell, and $! would be the subshell, not the client.
HOME=$TMP/home NETUSER=netops NETPASS=pw NETENABLE=en "$KARVI" --config "$TMP/karvi.toml" \
  run --all --transport "$TRANSPORT" --format jsonl "$COMMAND" >"$TMP/out" 2>"$TMP/err" &
CLIENT=$!
CLIENT_PEAK=0
SPOOL_PEAK=0
SPOOL_FILES=0
while kill -0 "$CLIENT" 2>/dev/null; do
  c=$(status_kb VmRSS "$CLIENT"); [ "${c:-0}" -le "$CLIENT_PEAK" ] || CLIENT_PEAK=$c
  # The spool directory's size and file count now (du in kB, apparent size).
  s=$(du -sk --apparent-size "$TMP/spool" 2>/dev/null | cut -f1); [ "${s:-0}" -le "$SPOOL_PEAK" ] || SPOOL_PEAK=$s
  f=$(ls "$TMP/spool" 2>/dev/null | wc -l); [ "$f" -le "$SPOOL_FILES" ] || SPOOL_FILES=$f
  sleep 0.05
done
CODE=0; wait "$CLIENT" || CODE=$?
END=$(date +%s.%N)
HWM=$(status_kb VmHWM "$DPID")
AFTER=$(status_kb VmRSS "$DPID")

[ "$CODE" -eq 0 ] || fail "run exited $CODE: $(grep -E '^[a-z_]+:' "$TMP/err" | head -1)"
JOB=$(ls -d "$TMP"/base/jobs/*/* | tail -1)
WANT_BYTES=$((BIG_LINES * 73))
records=$(wc -l <"$JOB/commands.jsonl")
ok=$(jsonl_records "$JOB/commands.jsonl" status output_bytes | grep -cx "$(printf 'succeeded\t%s' "$WANT_BYTES")" || true)
[ "$records" -eq "$N" ] || fail "$records records, expected $N"
[ "$ok" -eq "$N" ] || fail "$ok records succeeded with $WANT_BYTES output bytes, expected $N"
karvi --quiet daemon stop --force >/dev/null 2>&1 || true
kill "$FAKE_PID" 2>/dev/null || true; wait "$FAKE_PID" 2>/dev/null || true; FAKE_PID=
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
awk -v n="$N" -v tr="$TRANSPORT" -v ch="$CHANNEL" -v inflight="$CAP" -v bytes="$WANT_BYTES" -v idle="$IDLE" -v hwm="$HWM" -v after="$AFTER" \
  -v client="$CLIENT_PEAK" -v wall="$(echo "$END - $START" | bc)" -v jsonl="$(wc -c <"$JOB/commands.jsonl")" \
  -v spool="$SPOOL_PEAK" -v files="$SPOOL_FILES" -v left="$(ls "$TMP/spool" 2>/dev/null | wc -l)" 'BEGIN {
  printf "output scale: n=%d transport=%s channel=%s inflight=%d output_bytes=%d wall=%.1fs\n", n, tr, ch, inflight, bytes, wall
  printf "output scale: daemon kB idle=%d peak=%d after=%d; per in-flight response %.1f MB, %.1fx its output\n", idle, hwm, after, (hwm - idle) / 1024 / inflight, (hwm - idle) * 1024 / inflight / bytes
  printf "output scale: client peak kB=%d; commands.jsonl bytes=%d; %d records succeeded\n", client, jsonl, n
  printf "output scale: spool peak kB=%d in %d files under spooldir; %d left after\n", spool, files, left
}'
echo 'output scale: pass'
