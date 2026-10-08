#!/bin/sh
# Exercise the unified system-OpenSSH host-key policy without contacting a
# network device. The fake SSH process emits OpenSSH-compatible diagnostics so
# karvi's observable records, exit codes, warnings, and continuation semantics
# are tested end to end.
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
. "$ROOT/scripts/lib/json.sh"
KARVI=${KARVI:-$ROOT/bin/karvi-linux-amd64}
TMP=$(mktemp -d "${TMPDIR:-/tmp}/karvi-hostkey-smoke-XXXXXX")
. "$ROOT/scripts/lib/host.sh"; host_trust_store "$TMP"   # the trust store under the work directory, never the operator's
HOME_DIR=$TMP/home
BASE=$TMP/state
SCORE=$TMP/scoreboards
CAPACITY=$TMP/capacity
FAKE_SSH=$TMP/fake-ssh
CAPTURE=$TMP/managed-ssh.conf

cleanup() { rm -rf "$TMP"; }
trap cleanup EXIT HUP INT TERM

install -d -m 700 "$HOME_DIR" "$BASE" "$SCORE" "$CAPACITY" "$TMP/bin"
cat > "$FAKE_SSH" <<EOF_INNER
#!/bin/sh
state="\$0.master"
args=" \$* "
case "\$args" in
  *" -O check "*) test -f "\$state"; exit \$? ;;
esac
while [ "\$#" -gt 0 ]; do
  if [ "\$1" = "-F" ] && [ "\$#" -ge 2 ]; then
    shift
    cp "\$1" "$CAPTURE"
  fi
  shift || true
done
case "\$args" in
  *" -M "*) : > "\$state"; exit 0 ;;
esac
case "\$args" in
  *"127.0.0.1"*)
    echo '@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@' >&2
    echo 'WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!' >&2
    exit 255
    ;;
  *"127.0.0.3"*)
    # A first contact: OpenSSH stores the key and says so at LogLevel INFO
    # and above (the command session runs at VERBOSE).
    echo "Warning: Permanently added '127.0.0.3' (ED25519) to the list of known hosts." >&2
    printf 'enrolled-device#'
    while IFS= read -r line; do
      [ "\$line" = exit ] && exit 0
      printf '%s\r\nhost-key smoke succeeded\r\nenrolled-device#' "\$line"
    done
    exit 0
    ;;
  *" ControlPath=none "*)
    printf 'mismatch-device#'
    while IFS= read -r line; do
      [ "\$line" = exit ] && exit 0
      printf '%s\r\nhost-key smoke succeeded\r\nmismatch-device#' "\$line"
    done
    exit 0
    ;;
esac
printf 'host-key smoke succeeded\n'
EOF_INNER
chmod 755 "$FAKE_SSH"

common_args() {
  printf '%s\n' \
    --quiet \
    --set "basedir=\"$BASE\"" --set 'sharedroot="none"' --set "spooldir=\"$BASE/spool\"" --set 'platform-resolution.default=""' \
    --set "ssh.transports.system=\"$FAKE_SSH\"" \
    --set 'ssh.command.transport="system"' \
    --set audit.journald-required=false \
    --set "audit.file=\"$BASE/audit.jsonl\"" \
    --set "scoreboards=\"$SCORE\"" \
    --set "sessions.shared-capacity-root=\"$CAPACITY\"" \
    --set output.min-free-bytes-after-job=0
}

# Auto/default: first device reports a changed key, but the second still runs.
# The store is "auto" here, without the work directory's variable: under the
# basedir the run sets, never the operator's own.
# shellcheck disable=SC2046
set +e
env -u KARVI__SSH__KNOWN_HOSTS_FILE HOME="$HOME_DIR" NETUSER=smoke NETPASS=not-a-real-secret "$KARVI" $(common_args) \
  run --no-daemon --target 127.0.0.1 --target 127.0.0.2 \
  --transport system --dispatch serial --format jsonl 'show clock' \
  >"$TMP/auto.jsonl" 2>"$TMP/auto.err"
auto_code=$?
set -e
[ "$auto_code" -eq 101 ] || {
  echo "auto run: expected exit 101, received $auto_code" >&2
  cat "$TMP/auto.err" >&2
  exit 1
}
[ "$(wc -l < "$TMP/auto.jsonl" | tr -d ' ')" -eq 3 ] # two records and the summary line
[ "$(jsonl_records "$TMP/auto.jsonl" error.code | grep -cx host_key_changed)" -eq 1 ]
[ "$(jsonl_records "$TMP/auto.jsonl" status | grep -cx succeeded)" -eq 1 ]
grep -q 'StrictHostKeyChecking accept-new' "$CAPTURE"
AUTO_KNOWN=$BASE/known_hosts
grep -q "UserKnownHostsFile \"$AUTO_KNOWN\"" "$CAPTURE"
[ -f "$AUTO_KNOWN" ]
[ "$(stat -c '%a' "$(dirname "$AUTO_KNOWN")")" = 700 ]
[ "$(stat -c '%a' "$AUTO_KNOWN")" = 600 ]

# A first contact over system: OpenSSH's "Permanently added" line becomes
# the client's "! ssh accepted new host key" line on standard error, under
# --quiet too, the notice host_key_enrolled on the device's first record
# alone, and details.host_key_enrolled on its command_completed audit event.
# shellcheck disable=SC2046
env -u KARVI__SSH__KNOWN_HOSTS_FILE HOME="$HOME_DIR" NETUSER=smoke NETPASS=not-a-real-secret "$KARVI" $(common_args) \
  run --no-daemon --target 127.0.0.3 \
  --transport system --format jsonl --cmd 'show clock' --cmd 'show version' \
  >"$TMP/enroll.jsonl" 2>"$TMP/enroll.err"
[ "$(cat "$TMP/enroll.err")" = '! ssh accepted new host-key 127.0.0.3 (ED25519)' ]
[ "$(jsonl_records "$TMP/enroll.jsonl" 'notices.*.code' 'notices.*.details.key_type' | paste -sd'|' -)" = "$(printf 'host_key_enrolled\tED25519|\t')" ]
grep '"command_completed"' "$BASE/audit.jsonl" | tail -2 >"$TMP/enroll.audit"
sed -n 1p "$TMP/enroll.audit" >"$TMP/enroll.audit1"; sed -n 2p "$TMP/enroll.audit" >"$TMP/enroll.audit2"
json_is "$TMP/enroll.audit1" details.host_key_enrolled ED25519
[ "$(json_get "$TMP/enroll.audit2" details)" = '{}' ]

# Opt-in mismatch halt: the first changed key stops scheduling the second
# target and returns the dedicated run outcome.
rm -f "$FAKE_SSH.master"
set +e
HOME="$HOME_DIR" NETUSER=smoke NETPASS=not-a-real-secret "$KARVI" $(common_args) \
  --set ssh.halt-run-on-host-key-mismatch=true \
  run --no-daemon --target 127.0.0.1 --target 127.0.0.2 \
  --transport system --dispatch serial --format jsonl 'show clock' \
  >"$TMP/halt.jsonl" 2>"$TMP/halt.err"
halt_code=$?
set -e
[ "$halt_code" -eq 114 ] || {
  echo "host-key halt: expected exit 114, received $halt_code" >&2
  cat "$TMP/halt.err" >&2
  exit 1
}
[ "$(jsonl_records "$TMP/halt.jsonl" status | grep -cx not_started_halt)" -eq 1 ]
jsonl_records "$TMP/halt.jsonl" error.code | grep -qx halt_host_key_mismatch

# Secure: a missing pre-enrolled file fails before the fake SSH binary runs and
# returns the stable direct-command host-key exit code.
SECURE_HOME=$TMP/secure-home
install -d -m 700 "$SECURE_HOME"
rm -f "$CAPTURE"
# shellcheck disable=SC2046
set +e
HOME="$SECURE_HOME" NETUSER=smoke NETPASS=not-a-real-secret "$KARVI" $(common_args) \
  command --ssh-host-key-policy secure --ssh-known-hosts-file "$SECURE_HOME/karvi/known_hosts" \
  --management-address 127.0.0.2 secure-device 'show clock' \
  >"$TMP/secure.out" 2>"$TMP/secure.err"
secure_code=$?
set -e
[ "$secure_code" -eq 109 ] || {
  echo "secure command: expected exit 109, received $secure_code" >&2
  cat "$TMP/secure.err" >&2
  exit 1
}
grep -q 'host_key_not_enrolled' "$TMP/secure.err"
[ ! -e "$CAPTURE" ]

# Insecure: retain the private trust file only as comparison evidence. A fake
# key scan presents a different key; karvi says the insecure policy once for
# the job, in two lines, and the mismatch on the device's first record, under
# --quiet, then allows the fake SSH command to succeed; the audit's details
# name both keys.
INSECURE_HOME=$TMP/insecure-home
INSECURE_DIR=$INSECURE_HOME/.local/share/karvi
INSECURE_KNOWN=$INSECURE_DIR/known_hosts
install -d -m 700 "$INSECURE_DIR"
ENROLLED='AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA='
PRESENTED='AgMEBQYHCAkKCwwNDg8QERITFBUWFxgZGhscHR4fICE='
printf 'mismatch-device,127.0.0.3 ssh-ed25519 %s\n' "$ENROLLED" > "$INSECURE_KNOWN"
chmod 600 "$INSECURE_KNOWN"
cat > "$TMP/bin/ssh-keyscan" <<EOF_SCAN
#!/bin/sh
printf '127.0.0.3 ssh-ed25519 %s\n' '$PRESENTED'
EOF_SCAN
chmod 755 "$TMP/bin/ssh-keyscan"
# shellcheck disable=SC2046
HOME="$INSECURE_HOME" PATH="$TMP/bin:$PATH" NETUSER=smoke NETPASS=not-a-real-secret "$KARVI" $(common_args) \
  command --ssh-host-key-policy insecure --ssh-known-hosts-file "$INSECURE_KNOWN" \
  --management-address 127.0.0.3 mismatch-device 'show clock' \
  >"$TMP/insecure.out" 2>"$TMP/insecure.err"
grep -q '^host-key smoke succeeded$' "$TMP/insecure.out"
[ "$(cat "$TMP/insecure.err")" = "$(printf '%s\n' \
  '! ssh host-key policy insecure: unknown and changed keys accepted;' \
  '!  connecting to devices with wrong keys and MITM attacks allowed' \
  '! ssh host-key mismatch mismatch-device proceeding at risk')" ]
grep '"command_completed"' "$BASE/audit.jsonl" | tail -1 >"$TMP/insecure.audit"
[ "$(json_get "$TMP/insecure.audit" 'details.host_key_mismatch_accepted.*.*' | wc -l)" -eq 2 ]
grep -q 'StrictHostKeyChecking no' "$CAPTURE"
[ "$(grep -c 'KnownHostsFile "/dev/null"' "$CAPTURE")" -eq 2 ]

echo 'host-key smoke: pass'
