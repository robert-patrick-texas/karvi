#!/bin/sh
# The audit suite: karvi-audit (docs/EXAMPLES.md chapter 64) on the events of
# jobs made against the fake IOS XE device built from the tree over the
# native transport, read back from the host's journal: a command, and a run
# of two statements at two devices of which one is unreachable, the devices
# named for this run alone. The table through a pipe, each row the event
# audit.file holds, laid out as chapter 64 has it; by device, by job ID, by
# operator, newest first, a count; the colours through a pipe, none into a
# file, none under --nocolor or NO_COLOR; a terminal of 80 columns; a follow
# that shows a new event and ends 0 on Ctrl-C; nothing matched; journalctl's
# refusal; the reader's own. On a host without journald the rows that read
# the journal are skipped with the reason, as the canary suite's ICMP rows
# are. Needs the native executable, karvi-audit beside it, and `go`; writes
# nothing outside its own directory.
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
KARVI=${KARVI:-$ROOT/bin/karvi-linux-amd64}
AUDIT=${KARVI_AUDIT:-$(dirname "$KARVI")/karvi-audit}
GO=${GO:-go}
. "$ROOT/scripts/lib/json.sh"
TMP=$(mktemp -d "${TMPDIR:-/tmp}/karvi-audit-smoke-XXXXXX")
. "$ROOT/scripts/lib/host.sh"; host_trust_store "$TMP"   # the trust store under the work directory, never the operator's
FAKE_PID=
FOLLOW_PID=
unset NETUSER NETPASS NETENABLE NO_COLOR

fail() { echo "audit smoke: FAIL: $*" >&2; exit 1; }
cleanup() {
  if [ -n "$FOLLOW_PID" ]; then env kill -TERM -- "-$FOLLOW_PID" 2>/dev/null || true; wait "$FOLLOW_PID" 2>/dev/null || true; fi
  if [ -n "$FAKE_PID" ]; then kill "$FAKE_PID" 2>/dev/null || true; wait "$FAKE_PID" 2>/dev/null || true; fi
  rm -rf "$TMP"
}
trap cleanup EXIT HUP INT TERM

[ -x "$AUDIT" ] || fail "karvi-audit not found at $AUDIT"

# audit TAG ARGS...: karvi-audit; its output in out.TAG, its diagnostics in
# err.TAG, its exit in CODE.
audit() {
  tag=$1; shift
  CODE=0
  "$AUDIT" "$@" >"$TMP/out.$tag" 2>"$TMP/err.$tag" || CODE=$?
}
# expect TAG CODE: the call's exit.
expect() { [ "$CODE" = "$2" ] || fail "$1: exit $CODE, want $2: $(head -3 "$TMP/err.$1")"; }

# The reader's own refusals and journalctl missing need no journal.
audit usage -n x; expect usage 2
[ ! -s "$TMP/out.usage" ] && [ "$(head -1 "$TMP/err.usage")" = 'karvi-audit: invalid value "x" for flag -n: parse error' ] ||
  fail "-n x: $(head -1 "$TMP/err.usage")"
grep -q '^Usage:$' "$TMP/err.usage" || fail "-n x: no help after the message"
audit extra extra; expect extra 2
[ "$(head -1 "$TMP/err.extra")" = 'karvi-audit: no positional argument is taken' ] || fail "extra: $(head -1 "$TMP/err.extra")"
CODE=0
PATH=/nonexistent "$AUDIT" >"$TMP/out.nojc" 2>"$TMP/err.nojc" || CODE=$?
expect nojc 1
[ ! -s "$TMP/out.nojc" ] && [ "$(cat "$TMP/err.nojc")" = 'karvi-audit: journalctl not found' ] || fail "no journalctl: $(cat "$TMP/err.nojc")"
audit help --help; expect help 0
[ "$(head -1 "$TMP/out.help")" = "karvi-audit - show karvi's audit trail from journald, one event per row" ] || fail "--help: $(head -1 "$TMP/out.help")"

if ! [ -S /run/systemd/journal/socket ] || ! command -v journalctl >/dev/null 2>&1; then
  echo "audit smoke: j1-j9 SKIP: no journald on this host (/run/systemd/journal/socket, journalctl)"
  echo "audit smoke: ok"
  exit 0
fi

"$KARVI" version --format json >"$TMP/version.json"
json_get "$TMP/version.json" 'ssh_transports.*.id' | grep -qx scrapligo-v1 || fail "$KARVI lacks scrapligo-v1; build with make native-build"
install -d -m 700 "$TMP/bin" "$TMP/home" "$TMP/base" "$TMP/score" "$TMP/store"
(cd "$ROOT" && GOTOOLCHAIN=local "$GO" build -mod=vendor -o "$TMP/bin/fake" ./cmd/karvi-fake-device)
"$TMP/bin/fake" -host-key-file "$TMP/hostkey" -port 0 >"$TMP/port" 2>"$TMP/fake.err" &
FAKE_PID=$!
i=0
while [ ! -s "$TMP/port" ] && [ $i -lt 100 ]; do sleep 0.05; i=$((i + 1)); done
[ -s "$TMP/port" ] || fail "the fake did not start: $(head -1 "$TMP/fake.err")"
PORT=$(cat "$TMP/port")

# The devices are named for this run alone, so that the host's journal,
# which holds every other run's events, matches this run's by device; the
# dead one's address is a loopback the fake does not listen on: refused.
T=$(od -An -N4 -tx4 /dev/urandom | tr -d ' \n')
UP=up-$T
DEAD=dead-$T
printf 'name,management_address,platform\n%s,127.0.0.1,cisco_iosxe\n%s,127.0.0.9,cisco_iosxe\n' "$UP" "$DEAD" >"$TMP/inv.csv"
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
journald-required = true
file = "$TMP/base/audit.jsonl"
[sessions]
shared-capacity-root = "$TMP/base/cap"
[display]
color = "never"
[platform.cisco_iosxe]
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

# karvi TAG ARGS...: the client with the fake's credentials, in process.
karvi() {
  tag=$1; shift
  CODE=0
  HOME=$TMP/home NETUSER=netops NETPASS=pw NETENABLE=en "$KARVI" --config "$TMP/karvi.toml" "$@" >"$TMP/out.$tag" 2>"$TMP/err.$tag" || CODE=$?
}
newest() { for nw_dir in "$TMP/base/jobs"/*/*; do :; done; basename "$nw_dir"; }

# Every read names the run's start: the host's trail is every run's, and
# read whole it takes seconds.
S=$(date '+%Y-%m-%d %H:%M:%S')
karvi cmd command "$UP" 'show version'; expect cmd 0; J1=$(newest)
karvi two run --no-daemon --target "$UP" --target "$DEAD" --cmd 'show clock' --cmd 'show version'; expect two 101; J2=$(newest)
[ -n "$J1" ] && [ "$J1" != "$J2" ] || fail "the jobs made no folders"

# want FILE AWK-CONDITION: the table chapter 64 lays out to a pipe, the
# heading and a row for each event of audit.file the condition keeps ($1 the
# job ID, $6 the device, $7 the statement), in its order: the cells at their
# widths with one space between, OPERATOR and DEVICE centred (an odd cell to
# the right), a cell longer than its column cut with …, the line's end
# trimmed. A cut is marked \001 until the line is whole, so that length
# counts cells; every name here is ASCII. The condition is awk's own text,
# set into the program.
want() {
  w_cond=$2
  audit_events "$TMP/base/audit.jsonl" job_id operator.username loginuser event_name outcome device.canonical_name action.command |
    awk -F "$TAB" '
      function cut(s, w) { return length(s) > w ? substr(s, 1, w - 1) "\001" : s }
      function pad(n,   s) { s = ""; while (n-- > 0) s = s " "; return s }
      function cell(s, w, centre,   l) { s = cut(s, w); l = centre ? int((w - length(s)) / 2) : 0; return pad(l) s pad(w - length(s) - l) }
      function op(o, u,   t) { if (u == "") return o; t = "/" u; return length(t) >= 17 ? o t : cut(o, 18 - length(t)) t }
      function line(id, o, mode, event, outcome, dev, cmd,   l) {
        l = cell(id, 16) " " cell(o, 18, 1) " " cell(mode, 5) " " cell(event, 7) " " cell(outcome, 16) " " cell(dev, 20, 1) " " cmd
        sub(/ +$/, "", l); gsub(/\001/, "…", l); print l
      }
      BEGIN { line("JOB-ID", "OPERATOR", "MODE", "EVENT", "OUTCOME", "DEVICE", "COMMAND") }
      '"$w_cond"' { split($4, n, "."); line($1, op($2, $3), n[1], n[2], $5, $6, $7) }' >"$1"
}
# same TAG WANT-FILE: out.TAG is the table in the file, and nothing on stderr.
same() {
  cmp -s "$2" "$TMP/out.$1" || { diff "$2" "$TMP/out.$1" >&2 || true; fail "$1: the table differs"; }
  [ ! -s "$TMP/err.$1" ] || fail "$1 wrote on stderr: $(head -1 "$TMP/err.$1")"
}

# j1: by device, the dead one's name in capitals, lowercased: each command
# event of both jobs, as audit.file has them. journald takes the events a
# moment after the job: the read is repeated for up to ten seconds.
want "$TMP/want.dev" "\$6 == \"$UP\" || \$6 == \"$DEAD\""
i=0
while :; do
  audit dev --nocolor -S "$S" -d "$UP" -d "$(echo "$DEAD" | tr "[:lower:]" "[:upper:]")"; expect dev 0
  cmp -s "$TMP/want.dev" "$TMP/out.dev" && break
  [ $i -lt 50 ] || { diff "$TMP/want.dev" "$TMP/out.dev" >&2 || true; fail "j1: by device, the table differs (is the operator's own journal readable?)"; }
  sleep 0.2; i=$((i + 1))
done
same dev "$TMP/want.dev"
[ "$(wc -l <"$TMP/want.dev")" = 6 ] || fail "j1: audit.file holds $(($(wc -l <"$TMP/want.dev") - 1)) command events, want 5"

# j2: by job ID, the run's start, its four commands, and its end in order.
# A job ID names a job in its own output root alone, and another suite's job
# may start in the same second beside this one: the run's rows are looked for
# in order among what the pattern matched.
want "$TMP/want.job" "\$1 == \"$J2\""
[ "$(wc -l <"$TMP/want.job")" = 7 ] || fail "j2: audit.file holds $(($(wc -l <"$TMP/want.job") - 1)) events of $J2, want 6"
audit job --nocolor -S "$S" -g "$J2"; expect job 0
awk 'NR == FNR { want[++n] = $0; next } $0 == want[i + 1] { i++ } END { exit i != n }' "$TMP/want.job" "$TMP/out.job" ||
  { cat "$TMP/out.job" >&2; fail "j2: by job ID, the run's rows are not all there in order"; }

# j3: by operator, with the device: the up device's rows; by the login user
# behind sudo where there is one; nothing matched, said on stderr, exit 0.
OP=$(id -un)
want "$TMP/want.up" "\$6 == \"$UP\""
audit op --nocolor -S "$S" -o "$OP" -d "$UP"; expect op 0
same op "$TMP/want.up"
LU=$(audit_events "$TMP/base/audit.jsonl" loginuser | head -1)
if [ -n "$LU" ]; then
  audit lu --nocolor -S "$S" -o "$LU" -d "$UP"; expect lu 0
  same lu "$TMP/want.up"
fi
audit none --nocolor -S "$S" -o "nobody-$T"; expect none 0
[ ! -s "$TMP/out.none" ] && [ "$(cat "$TMP/err.none")" = 'karvi-audit: no matches' ] || fail "j3: nothing matched: $(cat "$TMP/out.none" "$TMP/err.none")"

# j4: newest first, two of them.
audit rev --nocolor -S "$S" -d "$UP" -d "$DEAD" -r -n 2; expect rev 0
{ head -1 "$TMP/want.dev"; tail -2 "$TMP/want.dev" | tac; } >"$TMP/want.rev"
same rev "$TMP/want.rev"

# j5: the colours: through a pipe by default, never into a file, none under
# --nocolor or NO_COLOR; the same text once the codes are taken out.
ESC=$(printf '\033')
"$AUDIT" -S "$S" -d "$UP" -d "$DEAD" | cat >"$TMP/out.pipe"
grep -q "$ESC\\[" "$TMP/out.pipe" || fail "j5: no colour through a pipe"
sed "s/$ESC\\[[0-9;]*m//g" "$TMP/out.pipe" | cmp -s - "$TMP/want.dev" || fail "j5: the coloured table's text differs"
audit file -S "$S" -d "$UP" -d "$DEAD"; expect file 0
same file "$TMP/want.dev"
"$AUDIT" --nocolor -S "$S" -d "$UP" | cat >"$TMP/out.nocolor"
cmp -s "$TMP/out.nocolor" "$TMP/want.up" || fail "j5: colour through a pipe under --nocolor"
NO_COLOR=1 "$AUDIT" -S "$S" -d "$UP" | cat >"$TMP/out.nocolour"
cmp -s "$TMP/out.nocolour" "$TMP/want.up" || fail "j5: colour through a pipe under NO_COLOR"

# j6: at a terminal of 80 columns every line leaves its last cell free, and
# a row is cut with … (DEVICE at its column's left, then …).
script -qefc "stty cols 80; '$AUDIT' --nocolor -S '$S' -d '$UP'" /dev/null >"$TMP/pty.raw" 2>"$TMP/pty.err" || fail "j6: karvi-audit under script: $(head -1 "$TMP/pty.err")"
tr -d '\r' <"$TMP/pty.raw" >"$TMP/pty.txt"
[ "$(wc -l <"$TMP/pty.txt")" = "$(wc -l <"$TMP/want.up")" ] || fail "j6: 80 columns: $(wc -l <"$TMP/pty.txt") lines"
awk '{ gsub(/…/, "."); if (length > 79) bad = 1 } END { exit bad }' "$TMP/pty.txt" || fail "j6: 80 columns: a line takes the last cell"
sed 1d "$TMP/pty.txt" | while IFS= read -r l; do
  case $l in "$J1 "*" $UP"*"…" | "$J2 "*" $UP"*"…") ;; *) fail "j6: 80 columns: the row is not cut after DEVICE: $l" ;; esac
done

# j7: a follow shows the newest none, then a new event as it comes, and ends
# 0 on Ctrl-C, sent to its process group as a terminal sends it.
setsid "$AUDIT" --nocolor -f -n 0 -d "$UP" >"$TMP/out.follow" 2>"$TMP/err.follow" &
FOLLOW_PID=$!
i=0
while [ ! -s "$TMP/out.follow" ] && [ $i -lt 50 ]; do sleep 0.1; i=$((i + 1)); done
[ "$(cat "$TMP/out.follow")" = "$(head -1 "$TMP/want.up")" ] || fail "j7: the follow's heading: $(cat "$TMP/out.follow" "$TMP/err.follow")"
karvi new command "$UP" 'show clock'; expect new 0
want "$TMP/want.new" "\$6 == \"$UP\" && \$7 == \"show clock\" && \$1 != \"$J2\""
i=0
while [ "$(wc -l <"$TMP/out.follow")" -lt 2 ] && [ $i -lt 50 ]; do sleep 0.2; i=$((i + 1)); done
env kill -INT -- "-$FOLLOW_PID"
CODE=0; wait "$FOLLOW_PID" || CODE=$?
FOLLOW_PID=
expect follow 0
same follow "$TMP/want.new"

# j8: journalctl's refusal of a value is its own, as it wrote it: exit 1,
# nothing on stdout, no heading.
audit badtime --nocolor -S yesterdayx; expect badtime 1
[ ! -s "$TMP/out.badtime" ] && [ -s "$TMP/err.badtime" ] || fail "j8: -S yesterdayx: $(cat "$TMP/out.badtime")"
grep -q '^karvi-audit:' "$TMP/err.badtime" && fail "j8: -S yesterdayx: the refusal is the reader's: $(cat "$TMP/err.badtime")"

# j9: the pattern and a device together: the run's commands at the dead
# device.
audit both --nocolor -S "$S" -g "$J2" -d "$DEAD"; expect both 0
want "$TMP/want.both" "\$1 == \"$J2\" && \$6 == \"$DEAD\""
same both "$TMP/want.both"

echo "audit smoke: ok"
