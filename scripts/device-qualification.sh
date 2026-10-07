#!/bin/sh
# The device qualification run (docs/DEVICE-QUALIFICATION-RUNBOOK.md): the
# rows of docs/CISCO-IOSXE-QUALIFICATION.md marked device, each on both
# transports, against one laboratory device, with the evidence of "Required
# evidence" collected into one directory.
#
#   DEVICE=c9300-lab ADDRESS=192.0.2.10 scripts/device-qualification.sh
#   FAKE=1 scripts/device-qualification.sh      # the script's own check
#
# What it never does:
#   - write a credential: NETUSER, NETPASS, and NETENABLE come from the
#     operator's environment and reach karvi through it; the run ends with
#     bin/secret-scan looking for both secrets in everything collected;
#   - touch the operator's trust store or state: the configuration, the
#     trust store (ssh.known-hosts-file), the base directory, and HOME are
#     under the evidence directory's work/ (HOME does not move the store);
#   - change the device's host key: a mismatch is a wrong entry written
#     into the scratch trust store, so the device presents its own key and
#     karvi refuses before authentication;
#   - send anything but read-only show commands unless a row is opted in
#     (CONFIG_ROW, RELOAD below);
#   - assert a guess: a row whose behaviour no device has shown yet is
#     recorded as "observe" and cannot fail; it becomes an assertion only
#     from a laboratory run's evidence.
# These are the script's limits (the runbook's section 8); an edit that
# breaks one is a defect.
#
# Settings:
#   DEVICE, ADDRESS     the inventory name and the management address
#   PORT                the SSH port (22)
#   PLATFORM            the row's platform (cisco_iosxe)
#   EVIDENCE            the evidence directory (./karvi-qualification-DEVICE-UTC)
#   ROWS                the rows to run ("D1 D7"); all by default
#   TRANSPORTS          "system scrapligo-v1"
#   INVALID_COMMAND     a read-only command the device rejects (show bogus)
#   BIG_COMMAND         D9: a read-only command with a large output, such
#                       as "show tech-support"; unset skips the row
#   BIG_LIMIT           D9: the lowered output.max-command-bytes (65536)
#   CONCURRENCY         D8: sessions opened at once; keep it at or below
#                       the device's free vty lines; unset skips the row
#   DUALSTACK_NAME      D12: a DNS name of the device with A and AAAA
#                       records; unset skips the row
#   RESTRICTED_USER, RESTRICTED_PASS
#                       D5: an account refused `terminal length 0`; unset
#                       skips the row
#   CONFIG_ROW=1        D4: sends `configure terminal` and `end`, nothing
#                       between them
#   RELOAD="scrapligo-v1"
#                       D13: reloads the device once per transport listed,
#                       answering no to a save prompt; a laboratory unit only
#   RELOAD_WAIT         D13: seconds to await the device's return (900)
#   PARITYCHECK         a built tools/paritycheck; built with `go` when
#                       unset and `go` is present, else the streams are kept
#                       and the comparison is marked skip
#
# Dispositions in results.tsv: pass, fail, skip (with the reason), observe
# (evidence retained for the reviewers, nothing asserted). Exit 1 on any
# fail.
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
KARVI=${KARVI:-$ROOT/bin/karvi-linux-amd64}
SECRET_SCAN=${SECRET_SCAN:-$ROOT/bin/secret-scan}
. "$ROOT/scripts/lib/json.sh"
GO=${GO:-go}
PLATFORM=${PLATFORM:-cisco_iosxe}
PORT=${PORT:-22}
TRANSPORTS=${TRANSPORTS:-system scrapligo-v1}
INVALID_COMMAND=${INVALID_COMMAND:-show bogus}
BIG_LIMIT=${BIG_LIMIT:-65536}
RELOAD_WAIT=${RELOAD_WAIT:-900}
FAKE_PID=

if [ -n "${FAKE:-}" ]; then
  DEVICE=fake-iosxe; ADDRESS=127.0.0.1
  # Secrets long enough that the final scan finding one means something.
  NETUSER=netops; NETPASS=qualification-canary-login-7f3a; NETENABLE=qualification-canary-enable-9c1e
  BIG_COMMAND=${BIG_COMMAND:-show big}; CONCURRENCY=${CONCURRENCY:-4}
  export NETUSER NETPASS NETENABLE
fi
: "${DEVICE:?set DEVICE to the inventory name of the laboratory device}"
: "${ADDRESS:?set ADDRESS to its management address}"
: "${NETUSER:?export NETUSER, NETPASS, and NETENABLE for the laboratory account}"
: "${NETPASS:?export NETPASS}"
DEVICE=$(printf '%s' "$DEVICE" | tr '[:upper:]' '[:lower:]')
STAMP=$(date -u +%Y%m%dT%H%M%SZ)
EVIDENCE=${EVIDENCE:-$PWD/karvi-qualification-$DEVICE-$STAMP}
WORK=$EVIDENCE/work
KH=$WORK/store/known_hosts
# The daemon's socket lives in a short temporary directory of its own, not
# under the evidence: a Unix socket path is bound to about a hundred bytes,
# and an evidence directory under a deep path would leave every daemon row
# failing with daemon_serve_failed (bind: invalid argument). The directory
# is removed at exit; nothing in it is evidence.
SOCKDIR=$(mktemp -d "${TMPDIR:-/tmp}/karvi-qual-sock.XXXXXX")
SOCK=$SOCKDIR/daemon.sock
RESULTS=$EVIDENCE/results.tsv
FAILED=0

say() { echo "device qualification: $*"; }
result() {  # row, check, disposition, detail
  printf '%s\t%s\t%s\t%s\n' "$1" "$2" "$3" "$4" >>"$RESULTS"
  say "$1 $2: $3${4:+ ($4)}"
  [ "$3" != fail ] || FAILED=$((FAILED + 1))
}
stop_daemon() {
  [ -S "${SOCK:-}" ] || return 0
  HOME=$WORK/home "$KARVI" --quiet --config "$WORK/karvi.toml" daemon stop --force >/dev/null 2>&1 || true
}
cleanup() {
  stop_daemon
  [ -z "$FAKE_PID" ] || { kill "$FAKE_PID" 2>/dev/null || true; wait "$FAKE_PID" 2>/dev/null || true; }
  [ -z "${SOCKDIR:-}" ] || rm -rf "$SOCKDIR"
}
trap cleanup EXIT HUP INT TERM
wanted() { [ -z "${ROWS:-}" ] && return 0; for r in $ROWS; do [ "$1" != "$r" ] || return 0; done; return 1; }

[ ! -e "$EVIDENCE" ] || { echo "device qualification: $EVIDENCE exists; name another EVIDENCE" >&2; exit 2; }
install -d -m 700 "$EVIDENCE" "$WORK" "$WORK/bin" "$WORK/store" "$WORK/home"
printf 'row\tcheck\tdisposition\tdetail\n' >"$RESULTS"

# The operator's own trust store must not change.
. "$ROOT/scripts/lib/host.sh"
OWN_STORE=$(host_own_store "$KARVI")
OWN_BEFORE=$(host_store_digest "$OWN_STORE")

if [ -z "${PARITYCHECK:-}" ] && command -v "$GO" >/dev/null 2>&1; then
  (cd "$ROOT" && GOTOOLCHAIN=local "$GO" build -mod=vendor -o "$WORK/bin/paritycheck" ./tools/paritycheck) && PARITYCHECK=$WORK/bin/paritycheck
fi
if [ -n "${FAKE:-}" ]; then
  (cd "$ROOT" && GOTOOLCHAIN=local "$GO" build -mod=vendor -o "$WORK/bin/fake" ./cmd/karvi-fake-device)
  : >"$WORK/port"
  "$WORK/bin/fake" -host-key-file "$WORK/hostkey" -big-lines 2000 -password "$NETPASS" -enable "$NETENABLE" 2>"$EVIDENCE/fake.err" >"$WORK/port" &
  FAKE_PID=$!
  i=0
  while [ ! -s "$WORK/port" ] && [ $i -lt 100 ]; do sleep 0.05; i=$((i + 1)); done
  [ -s "$WORK/port" ] || { echo "device qualification: the fake did not start" >&2; exit 2; }
  PORT=$(cat "$WORK/port")
fi
if [ "$PORT" -eq 22 ]; then IDENTITY=$DEVICE; ADDRESS_IDENTITY=$ADDRESS
else IDENTITY="[$DEVICE]:$PORT"; ADDRESS_IDENTITY="[$ADDRESS]:$PORT"; fi

# The identification "Required evidence" names; the device's model and
# release are D1's `show version` output.
{
  echo "date_utc: $STAMP"
  echo "host: $(uname -srm)"
  echo "device: $DEVICE address: $ADDRESS port: $PORT platform: $PLATFORM${FAKE:+ (the fake IOS XE device, $(cd "$ROOT" && git rev-parse --short HEAD 2>/dev/null || echo 'no git'))}"
  echo "openssh: $(ssh -V 2>&1)"
  echo "executable_sha256: $(sha256sum "$KARVI" | cut -d' ' -f1)"
  "$KARVI" version
} >"$EVIDENCE/identification.txt"
"$KARVI" version --format json >"$EVIDENCE/version.json"
json_get "$EVIDENCE/version.json" 'ssh_transports.*.id' | grep -qx scrapligo-v1 || { echo "device qualification: $KARVI lacks scrapligo-v1" >&2; exit 2; }

write_config() {  # policy, further tables; NAMES (inventory names, default DEVICE), SSH, OUTPUT, DISPATCH
  {
    echo 'name,management_address,platform'
    for n in ${NAMES:-$DEVICE}; do printf '%s,%s,%s\n' "$n" "$ADDRESS" "$PLATFORM"; done
  } >"$WORK/inv.csv"
  cat >"$WORK/karvi.toml" <<EOF_CFG
basedir = "$WORK/base"
sharedroot = "none"
spooldir = "$WORK/spool"
scoreboards = "$WORK/base/score"
[daemon]
socket = "$SOCK"
[ssh]
host-key-policy = "$1"
known-hosts-file = "$KH"
${SSH:-}
[audit]
journald-required = false
file = "$WORK/base/audit.jsonl"
[sessions]
shared-capacity-root = "$WORK/base/cap"
[display]
color = "never"
[platform-resolution]
default = "$PLATFORM"
[platform.$PLATFORM]
ssh-port = $PORT
${OUTPUT:+[output]
$OUTPUT}
${DISPATCH:+[dispatch]
$DISPATCH}
[[inventory-source]]
name = "qualification"
type = "csv"
path = "$WORK/inv.csv"
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
${2:-}
EOF_CFG
  chmod 600 "$WORK/karvi.toml"
}

# qrun ROW TAG ACTIVITY TRANSPORT -- karvi args...; sets CODE, OUT, ERR. The
# base directory is new for each invocation and is moved into the evidence
# with the streams, so each job's records, summary, manifest, audit, and
# scoreboard stay beside the streams that produced them.
qrun() {
  q_row=$1; q_tag=$2; q_act=$3; q_tr=$4; shift 5
  case $q_act in run) q_sub='run --no-daemon' ;; daemon) q_sub=run ;; *) q_sub=command ;; esac
  q_dir=$EVIDENCE/$q_row; install -d -m 700 "$q_dir"
  rm -rf "$WORK/base"; install -d -m 700 "$WORK/base"
  OUT=$q_dir/$q_tag.out; ERR=$q_dir/$q_tag.err; CODE=0
  # shellcheck disable=SC2086
  HOME=$WORK/home "$KARVI" --config "$WORK/karvi.toml" $q_sub --format "${FORMAT:-jsonl}" --transport "$q_tr" "$@" >"$OUT" 2>"$ERR" </dev/null || CODE=$?
  echo "$CODE" >"$q_dir/$q_tag.exit"
  stop_daemon
  cp "$WORK/karvi.toml" "$q_dir/$q_tag.karvi.toml"
  [ ! -d "$WORK/base" ] || { rm -rf "$WORK/base/socket" "$WORK/base/cap"; mv "$WORK/base" "$q_dir/$q_tag.base"; }
}
statuses() { jsonl_records "$1" status | paste -sd, -; }  # the command records' statuses, in order
first_code() { jsonl_records "$1" error.code | grep -m1 .; }  # the first record's error code
expect_exit() {  # row, tag, wanted exit
  if [ "$CODE" -eq "$3" ]; then result "$1" "$2 exit" pass "$CODE"
  else result "$1" "$2 exit" fail "exit $CODE, expected $3: $(grep -E '^[a-z_]+:' "$ERR" | head -1)"; fi
}
parity() {  # row, expect, streams...
  p_row=$1; p_expect=$2; shift 2
  if [ -z "${PARITYCHECK:-}" ]; then result "$p_row" parity skip "no paritycheck on this host; compare the .out streams afterwards"; return 0; fi
  if p_out=$("$PARITYCHECK" -expect "$p_expect" "$@" 2>&1); then result "$p_row" parity pass "$p_out"
  else printf '%s\n' "$p_out" >"$EVIDENCE/$p_row/parity.findings"; result "$p_row" parity fail "$(printf '%s' "$p_out" | head -1); see $p_row/parity.findings"; fi
}
fresh_store() { : >"$KH"; chmod 600 "$KH"; }
# both ROW EXPECT COMMANDEXIT -- karvi args: the parity suite's shape on the
# device: `command` and `run` over each transport, one comparison.
both() {
  b_row=$1; b_expect=$2; b_exit=$3; shift 4
  b_streams=
  for b_tr in $TRANSPORTS; do
    for b_act in command run; do
      qrun "$b_row" "$b_act.$b_tr" "$b_act" "$b_tr" -- --target "$DEVICE" "$@"
      b_want=$b_exit; [ "$b_act" = command ] || [ "$b_exit" -eq 0 ] || b_want=101
      expect_exit "$b_row" "$b_act.$b_tr" "$b_want"
      b_streams="$b_streams $OUT"
    done
  done
  # shellcheck disable=SC2086
  parity "$b_row" "$b_expect" $b_streams
}

REJECTED=device_error:device_command_error
NOT_ATTEMPTED=not_attempted_prior_command_failure

# D1 (the session matrix's first rows): a success, a rejected
# command, and a success under continue; `show version` is also the record
# of the model and the release.
if wanted D1; then
  fresh_store; write_config accept-new
  both D1 "succeeded,$REJECTED,succeeded" 107 -- --continue-device-on-error --cmd 'show clock' --cmd "$INVALID_COMMAND" --cmd 'show version'
  grep -h -o 'Cisco IOS[^"\\]*Version [^",\\ ]*' "$EVIDENCE"/D1/*.out 2>/dev/null | sort -u | head -3 >"$EVIDENCE/device-release.txt" || true
  result D1 'device release' observe "$(head -1 "$EVIDENCE/device-release.txt")"
fi

# D2: the same list under halt.
if wanted D2; then
  fresh_store; write_config accept-new
  both D2 "succeeded,$REJECTED,$NOT_ATTEMPTED" 107 -- --cmd 'show clock' --cmd "$INVALID_COMMAND" --cmd 'show version'
fi

# D3: the banner, the first prompt, the one enable, and the paging
# commands as --debug and --echo show them, in text. Observed, and the
# final secret scan covers the debug output.
if wanted D3; then
  fresh_store; write_config accept-new
  for tr in $TRANSPORTS; do
    FORMAT=text qrun D3 "command.$tr" command "$tr" -- --target "$DEVICE" --debug --echo --cmd 'show clock' --cmd 'show privilege'
    expect_exit D3 "command.$tr" 0
    result D3 "command.$tr debug" observe "$(grep -c . "$ERR") stderr lines"
  done
fi

# D4 (opt-in): the (config)# prompt level. Nothing is sent between
# `configure terminal` and `end`.
if wanted D4; then
  if [ -n "${CONFIG_ROW:-}" ]; then
    fresh_store; write_config accept-new
    both D4 'succeeded,succeeded,succeeded' 0 -- --cmd 'configure terminal' --cmd 'end' --cmd 'show clock'
    for f in "$EVIDENCE"/D4/*.out; do
      head -1 "$f" | grep -q '"prompt":"[^"]*(config[^"]*)#"' && result D4 "$(basename "$f" .out) prompt level" pass "$(head -1 "$f" | grep -o '"prompt":"[^"]*"')" \
        || result D4 "$(basename "$f" .out) prompt level" fail "the first record's prompt is $(head -1 "$f" | grep -o '"prompt":"[^"]*"')"
    done
  else result D4 'configuration mode' skip 'CONFIG_ROW is not set'; fi
fi

# D5 (opt-in): a restricted account refused `terminal length 0` is
# paging_disable_failed; what the account may not run is observed.
if wanted D5; then
  if [ -n "${RESTRICTED_USER:-}" ] && [ -n "${RESTRICTED_PASS:-}" ]; then
    fresh_store; write_config accept-new
    for tr in $TRANSPORTS; do
      NETUSER=$RESTRICTED_USER NETPASS=$RESTRICTED_PASS qrun D5 "command.$tr" command "$tr" -- --target "$DEVICE" --cmd 'show clock' --cmd 'show running-config'
      result D5 "command.$tr" observe "exit $CODE, $(statuses "$OUT"), $(first_code "$OUT")"
    done
  else result D5 'restricted account' skip 'RESTRICTED_USER and RESTRICTED_PASS are not set'; fi
fi

# D7: host keys, every mismatch a wrong entry in the scratch store.
if wanted D7; then
  ssh-keygen -q -t ed25519 -N '' -f "$WORK/other" >/dev/null
  OTHER=$(cut -d' ' -f1,2 "$WORK/other.pub")
  for tr in $TRANSPORTS; do
    # a: accept-new on an empty store enrolls under the identity; a repeat matches.
    fresh_store; write_config accept-new
    qrun D7 "a1.$tr" command "$tr" -- --target "$DEVICE" --cmd 'show clock'; expect_exit D7 "a1.$tr enroll" 0
    cp "$KH" "$EVIDENCE/D7/a1.$tr.known_hosts"
    if [ "$(cut -d' ' -f1 "$KH" | sort -u)" = "$IDENTITY" ]; then result D7 "a1.$tr identity" pass "$IDENTITY $(cut -d' ' -f2 "$KH" | tr '\n' ' ')"
    else result D7 "a1.$tr identity" fail "the store holds $(cut -d' ' -f1,2 "$KH" | tr '\n' ';'), expected $IDENTITY"; fi
    before=$(cksum <"$KH")
    qrun D7 "a2.$tr" command "$tr" -- --target "$DEVICE" --cmd 'show clock'; expect_exit D7 "a2.$tr repeat" 0
    [ "$(cksum <"$KH")" = "$before" ] && result D7 "a2.$tr store unchanged" pass '' || result D7 "a2.$tr store unchanged" fail 'the repeat rewrote the store'
    # b: the other transport reads that entry under secure.
    for other in $TRANSPORTS; do
      [ "$other" != "$tr" ] || continue
      write_config secure
      qrun D7 "b.$tr-enrolled.$other" run "$other" -- --target "$DEVICE" --cmd 'show clock'; expect_exit D7 "b $tr enrolls, $other reads under secure" 0
    done
    # c: the same entries hashed by hand (ssh-keygen -H) under secure. The
    # policy is written here and not left to row b, which writes nothing
    # when TRANSPORTS names one transport.
    write_config secure
    ssh-keygen -H -f "$KH" >/dev/null 2>&1; rm -f "$KH.old"; chmod 600 "$KH"
    cp "$KH" "$EVIDENCE/D7/c.$tr.known_hosts"
    qrun D7 "c.$tr" command "$tr" -- --target "$DEVICE" --cmd 'show clock'; expect_exit D7 "c.$tr hashed entry under secure" 0
    # d: secure with an empty store; e: another key under the identity.
    fresh_store
    qrun D7 "d.$tr" command "$tr" -- --target "$DEVICE" --cmd 'show clock'; expect_exit D7 "d.$tr secure, not enrolled" 109
    [ "$(first_code "$OUT")" = host_key_not_enrolled ] && result D7 "d.$tr code" pass host_key_not_enrolled || result D7 "d.$tr code" fail "$(first_code "$OUT")"
    printf '%s %s\n' "$IDENTITY" "$OTHER" >"$KH"
    qrun D7 "e.$tr" command "$tr" -- --target "$DEVICE" --cmd 'show clock'; expect_exit D7 "e.$tr secure, changed key" 109
    [ "$(first_code "$OUT")" = host_key_changed ] && result D7 "e.$tr code" pass host_key_changed || result D7 "e.$tr code" fail "$(first_code "$OUT")"
    # f: insecure over the same wrong entry: the warning, then access.
    write_config insecure
    qrun D7 "f.$tr" command "$tr" -- --target "$DEVICE" --cmd 'show clock'; expect_exit D7 "f.$tr insecure, mismatch" 0
    grep -q "SSH host key mismatch for $DEVICE" "$ERR" && result D7 "f.$tr warning" pass '' || result D7 "f.$tr warning" fail 'no mismatch warning on stderr'
    # g: the literal address as the target is its own identity.
    fresh_store; write_config accept-new
    qrun D7 "g.$tr" command "$tr" -- --target "$ADDRESS" --cmd 'show clock'; expect_exit D7 "g.$tr literal address" 0
    [ "$(cut -d' ' -f1 "$KH" | sort -u)" = "$ADDRESS_IDENTITY" ] && result D7 "g.$tr identity" pass "$ADDRESS_IDENTITY" || result D7 "g.$tr identity" fail "the store holds $(cut -d' ' -f1 "$KH" | sort -u | tr '\n' ' ')"
    # h: two clients enroll at once: both succeed, one entry per key type.
    fresh_store
    rm -rf "$WORK/base"; install -d -m 700 "$WORK/base"
    for k in 1 2; do
      HOME=$WORK/home "$KARVI" --config "$WORK/karvi.toml" command --format jsonl --transport "$tr" --target "$DEVICE" --cmd 'show clock' \
        >"$EVIDENCE/D7/h$k.$tr.out" 2>"$EVIDENCE/D7/h$k.$tr.err" </dev/null &
      eval "H$k=\$!"
    done
    hc=0; wait "$H1" || hc=$?; wait "$H2" || hc=$?
    dup=$(cut -d' ' -f1,2 "$KH" | sort | uniq -d | wc -l)
    cp "$KH" "$EVIDENCE/D7/h.$tr.known_hosts"
    if [ "$hc" -eq 0 ] && [ "$dup" -eq 0 ] && [ -s "$KH" ]; then result D7 "h.$tr concurrent first enrollment" pass "$(wc -l <"$KH") entries, none twice"
    else result D7 "h.$tr concurrent first enrollment" fail "exit $hc, $dup duplicated entries"; fi
    rm -rf "$WORK/base/socket" "$WORK/base/cap"; mv "$WORK/base" "$EVIDENCE/D7/h.$tr.base"
    # i: a trust store of mode 0644. secure and
    # accept-new refuse it with the message an operator sees, insecure
    # continues, and none of the three changes the mode.
    for i_pol in secure accept-new; do
      write_config "$i_pol"; chmod 644 "$KH"
      qrun D7 "i.$i_pol.$tr" command "$tr" -- --target "$DEVICE" --cmd 'show clock'; expect_exit D7 "i.$tr $i_pol, store mode 0644" 109
      [ "$(first_code "$OUT")" = host_key_trust_store_permission ] && result D7 "i.$tr $i_pol code" pass "$(grep -ho -m1 'file mode is [0-7]*; run chmod 600 [^"]*' "$ERR" "$OUT" | head -1)" || result D7 "i.$tr $i_pol code" fail "$(first_code "$OUT")"
      [ "$(stat -c %a "$KH")" = 644 ] && result D7 "i.$tr $i_pol store mode unchanged" pass 644 || result D7 "i.$tr $i_pol store mode unchanged" fail "the store's mode afterwards $(stat -c %a "$KH")"
    done
    write_config insecure; chmod 644 "$KH"
    qrun D7 "i.insecure.$tr" command "$tr" -- --target "$DEVICE" --cmd 'show clock'; expect_exit D7 "i.$tr insecure, store mode 0644" 0
    [ "$(stat -c %a "$KH")" = 644 ] && result D7 "i.$tr insecure store mode unchanged" pass 644 || result D7 "i.$tr insecure store mode unchanged" fail "the store's mode afterwards $(stat -c %a "$KH")"
    chmod 600 "$KH"
  done
  # j: fleet isolation: two names for the device, the first with a wrong
  # entry; the second still runs, and under the run-wide halt it does not.
  NAMES="$DEVICE-mismatch $DEVICE"
  if [ "$PORT" -eq 22 ]; then bad="$DEVICE-mismatch"; else bad="[$DEVICE-mismatch]:$PORT"; fi
  for tr in $TRANSPORTS; do
    fresh_store; printf '%s %s\n' "$bad" "$OTHER" >"$KH"; write_config accept-new
    qrun D7 "j1.$tr" run "$tr" -- --target "$DEVICE-mismatch" --target "$DEVICE" --dispatch serial --cmd 'show clock'
    [ "$(statuses "$OUT")" = connection_error,succeeded ] && result D7 "j1.$tr one mismatch, the next target runs" pass "exit $CODE" || result D7 "j1.$tr one mismatch, the next target runs" fail "exit $CODE, $(statuses "$OUT")"
    fresh_store; printf '%s %s\n' "$bad" "$OTHER" >"$KH"
    SSH='halt-run-on-host-key-mismatch = true' write_config accept-new
    qrun D7 "j2.$tr" run "$tr" -- --target "$DEVICE-mismatch" --target "$DEVICE" --dispatch serial --cmd 'show clock'
    result D7 "j2.$tr the run-wide halt" observe "exit $CODE, $(statuses "$OUT")"
  done
  unset NAMES
fi

# D8 (opt-in): CONCURRENCY sessions at once, each its own inventory name
# for the one device; every record succeeded is the pass.
if wanted D8; then
  if [ -n "${CONCURRENCY:-}" ]; then
    NAMES=$(i=1; while [ "$i" -le "$CONCURRENCY" ]; do printf '%s-s%02d ' "$DEVICE" "$i"; i=$((i + 1)); done)
    for tr in $TRANSPORTS; do
      fresh_store
      DISPATCH="default = \"parallel\"
parallel-workers = $CONCURRENCY" write_config accept-new
      qrun D8 "daemon.$tr" daemon "$tr" -- --all --cmd 'show clock' --cmd 'show users'
      n=$(jsonl_records "$OUT" status | grep -cx succeeded || true)
      [ "$CODE" -eq 0 ] && [ "$n" -eq $((CONCURRENCY * 2)) ] && result D8 "daemon.$tr $CONCURRENCY sessions" pass "$n records succeeded" || result D8 "daemon.$tr $CONCURRENCY sessions" fail "exit $CODE, $n of $((CONCURRENCY * 2)) records succeeded"
    done
    unset NAMES
  else result D8 'concurrent sessions' skip 'CONCURRENCY is not set'; fi
fi

# D9 (opt-in): a large output under the default limit, then the same
# command over a lowered limit (output_limit_exceeded, exit 111).
if wanted D9; then
  if [ -n "${BIG_COMMAND:-}" ]; then
    for tr in $TRANSPORTS; do
      fresh_store; write_config accept-new '[execution]
command-timeout = "600s"'
      qrun D9 "default.$tr" command "$tr" -- --target "$DEVICE" --cmd "$BIG_COMMAND"; expect_exit D9 "default.$tr" 0
      result D9 "default.$tr output" observe "$(grep -o '"output_bytes":[0-9]*' "$OUT" | head -1)"
      OUTPUT="max-command-bytes = $BIG_LIMIT" write_config accept-new '[execution]
command-timeout = "600s"'
      qrun D9 "limit.$tr" command "$tr" -- --target "$DEVICE" --cmd "$BIG_COMMAND"; expect_exit D9 "limit.$tr" 111
    done
  else result D9 'large output' skip 'BIG_COMMAND is not set'; fi
fi

# D12 (opt-in): a dual-stack name, each family preferred in turn,
# the selection in the --debug output.
if wanted D12; then
  if [ -n "${DUALSTACK_NAME:-}" ]; then
    fresh_store; write_config accept-new
    for tr in $TRANSPORTS; do
      for fam in 4 6; do
        qrun D12 "ipv$fam.$tr" command "$tr" -- --target "$DUALSTACK_NAME" "--ipv$fam" --debug --cmd 'show clock'
        expect_exit D12 "ipv$fam.$tr" 0
        # The debug stream's "device resolved ... address=" line names the
        # address chosen: dotted for IPv4, with a colon for IPv6.
        chosen=$(sed -n 's/.*device resolved .* address=\([^ ]*\) .*/\1/p' "$ERR" | head -1)
        candidates=$(sed -n 's/.*device resolved .* candidates=\([0-9]*\) .*/\1/p' "$ERR" | head -1)
        case $fam:$chosen in 4:*.*.*.*|6:*:*) result D12 "ipv$fam.$tr selection" pass "$chosen of ${candidates:-?} candidates" ;;
        *) result D12 "ipv$fam.$tr selection" fail "--ipv$fam chose '$chosen' of ${candidates:-?} candidates; the row needs a name with A and AAAA records (the option is a preference)" ;; esac
      done
    done
  else result D12 'dual-stack selection' skip 'DUALSTACK_NAME is not set'; fi
fi

# D13 (opt-in; a laboratory unit): a blind reload with the save and confirm
# declarations, the save answered no; then the device's return is awaited.
if wanted D13; then
  if [ -n "${RELOAD:-}" ]; then
    for tr in $RELOAD; do
      fresh_store; write_config accept-new
      qrun D13 "reload.$tr" command "$tr" -- --target "$DEVICE" --cmd reload --blind --expect 'Save\? \[yes/no\]:=no' --expect 'confirm\]='
      expect_exit D13 "reload.$tr" 0
      grep -q prompt_not_observed_after_blind_send "$OUT" && result D13 "reload.$tr notice" pass '' || result D13 "reload.$tr notice" observe 'no prompt_not_observed_after_blind_send notice'
      [ -z "${FAKE:-}" ] || continue
      waited=0; CODE=1
      while [ "$CODE" -ne 0 ] && [ "$waited" -lt "$RELOAD_WAIT" ]; do
        sleep 30; waited=$((waited + 30))
        qrun D13 "return.$tr" command "$tr" -- --target "$DEVICE" --cmd 'show version'
      done
      [ "$CODE" -eq 0 ] && result D13 "return.$tr" pass "answered after ${waited}s" || result D13 "return.$tr" fail "no answer within ${RELOAD_WAIT}s"
    done
  else result D13 reload skip 'RELOAD is not set'; fi
fi

# D14: what the device holds afterwards: no session of this run may remain
# on a vty line. Observed.
if wanted D14; then
  fresh_store; write_config accept-new
  set -- $TRANSPORTS
  qrun D14 "users.$1" command "$1" -- --target "$DEVICE" --cmd 'show users'
  result D14 'sessions left on the device' observe "exit $CODE; read D14/users.$1.out"
fi

# Every record collected: no carriage return and no escape byte in any
# output (ANSI and line endings from a real terminal), and each run job's
# summary present beside its records.
cr=$(cat "$EVIDENCE"/D*/*.out 2>/dev/null | grep -c '\\r\|\\u001b' || true)
[ "$cr" -eq 0 ] && result ALL 'no carriage return or escape byte in any recorded output' pass '' || result ALL 'no carriage return or escape byte in any recorded output' fail "$cr records"
missing=0; jobs=0
for j in "$EVIDENCE"/D*/*.base/jobs/*/*; do
  [ -d "$j" ] || continue
  jobs=$((jobs + 1))
  [ -s "$j/summary.json" ] && [ -f "$j/commands.jsonl" ] || missing=$((missing + 1))
done
[ "$missing" -eq 0 ] && result ALL 'every job directory holds its records and summary' pass "$jobs job directories" || result ALL 'every job directory holds its records and summary' fail "$missing of $jobs"

# The secrets are in nothing collected. The values come from the
# environment, never from a command line.
if [ -x "$SECRET_SCAN" ]; then
  scan_env='-canary-env NETPASS'; [ -z "${NETENABLE:-}" ] || scan_env="$scan_env -canary-env NETENABLE"
  [ -z "${RESTRICTED_PASS:-}" ] || scan_env="$scan_env -canary-env RESTRICTED_PASS"
  # shellcheck disable=SC2086
  if scan=$("$SECRET_SCAN" $scan_env "$EVIDENCE" 2>&1); then result ALL 'no secret in the evidence' pass "$scan"
  else printf '%s\n' "$scan" >"$EVIDENCE/secret-scan.findings"; result ALL 'no secret in the evidence' fail 'see secret-scan.findings; do not share this directory'; fi
else result ALL 'no secret in the evidence' skip "no $SECRET_SCAN (make tools-build)"; fi
[ "$(host_store_digest "$OWN_STORE")" = "$OWN_BEFORE" ] && result ALL "the operator's trust store unchanged" pass '' || result ALL "the operator's trust store unchanged" fail "$OWN_STORE changed"

cleanup; FAKE_PID=
rm -rf "$WORK"
(cd "$EVIDENCE" && find . -type f ! -name SHA256SUMS -exec sha256sum {} + | sort -k2 >SHA256SUMS)
say "$(cut -f3 "$RESULTS" | sed 1d | sort | uniq -c | tr '\n' ' ')"
say "evidence: $EVIDENCE"
[ "$FAILED" -eq 0 ] || { say "$FAILED failed"; exit 1; }
say pass
