#!/bin/sh
# Verify the v0.6.1 restoration of --address as the literal management-address
# alias. It must never select a target in login, command, or run.
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
KARVI=${KARVI:-$ROOT/bin/karvi-linux-amd64}
TMP=$(mktemp -d "${TMPDIR:-/tmp}/karvi-v061-smoke-XXXXXX")
. "$ROOT/scripts/lib/host.sh"; host_trust_store "$TMP"   # the trust store under the work directory, never the operator's
HOME_DIR=$TMP/home
BASE=$TMP/state
SCORE=$TMP/scoreboards
CAP=$TMP/capacity
FAKE=$TMP/fake-ssh
LOG=$TMP/fake-ssh.log
trap 'rm -rf "$TMP"' EXIT HUP INT TERM
install -d -m 700 "$HOME_DIR" "$BASE" "$SCORE" "$CAP"

cat > "$FAKE" <<'SH'
#!/bin/sh
log="$0.log"
args=" $* "
# The system transport's ssh -Q capability queries are not sessions.
[ "$1" = -Q ] && exit 0
printf 'START:%s\n' "$*" >> "$log"
case "$args" in
  *" BatchMode=no "*)
    printf 'router1#'
    while IFS= read -r line; do
      printf 'COMMAND:%s\n' "$line" >> "$log"
      [ "$line" = exit ] && exit 0
      printf '%s\r\nOUTPUT:%s\r\nrouter1#' "$line" "$line"
    done
    exit 0
    ;;
  *" -tt "*) printf 'LOGIN-OK\n'; exit 0 ;;
esac
last=""
for arg in "$@"; do last=$arg; done
printf 'OUTPUT:%s\n' "$last"
printf 'COMMAND:%s\n' "$last" >> "$log"
SH
chmod 755 "$FAKE"

common_args() {
  printf '%s\n' \
    --set "basedir=\"$BASE\"" --set 'sharedroot="none"' --set "spooldir=\"$BASE/spool\"" --set 'platform-resolution.default=""' \
    --set "ssh.transports.system=\"$FAKE\"" \
    --set 'ssh.command.transport="system"' \
    --set audit.journald-required=false \
    --set "audit.file=\"$BASE/audit.jsonl\"" \
    --set "scoreboards=\"$SCORE\"" \
    --set "sessions.shared-capacity-root=\"$CAP\"" \
    --set output.min-free-bytes-after-job=0
}

# Help must describe --address only as the management-address alias.
for mode in login command run; do
  "$KARVI" "$mode" --help >"$TMP/$mode.help"
  grep -q -- '--management-address IP, --address, --a' "$TMP/$mode.help"
  ! grep -q -- '--address .*Alias for --target' "$TMP/$mode.help"
done

# Command: --address may appear among explicit commands but does not supply the
# required target.
rm -f "$FAKE.master" "$LOG"
# shellcheck disable=SC2046
HOME="$HOME_DIR" NETUSER=smoke NETPASS=not-a-real-secret "$KARVI" $(common_args) \
  command --cmd 'show clock' --address 127.0.0.1 --host router1 --quiet \
  >"$TMP/command.out" 2>"$TMP/command.err"
grep -q '^OUTPUT:show clock$' "$TMP/command.out"
grep -q '127.0.0.1' "$LOG"

set +e
# shellcheck disable=SC2046
HOME="$HOME_DIR" NETUSER=smoke NETPASS=not-a-real-secret "$KARVI" $(common_args) \
  command --address 127.0.0.1 --cmd 'show clock' \
  >"$TMP/command-missing-target.out" 2>"$TMP/command-missing-target.err"
rc=$?
set -e
[ "$rc" -eq 4 ]
grep -q '^cli_device_missing: ' "$TMP/command-missing-target.err"

# Login: target and address are independent, with options accepted in either
# order by the process-level transcript-aware normalizer.
rm -f "$FAKE.master" "$LOG"
# shellcheck disable=SC2046
HOME="$HOME_DIR" NETUSER=smoke NETPASS=not-a-real-secret "$KARVI" $(common_args) \
  login --address 127.0.0.1 --quiet --host router1 \
  >"$TMP/login.out" 2>"$TMP/login.err"
grep -q '^LOGIN-OK$' "$TMP/login.out"
grep -q '127.0.0.1' "$LOG"

set +e
# shellcheck disable=SC2046
HOME="$HOME_DIR" NETUSER=smoke NETPASS=not-a-real-secret "$KARVI" $(common_args) \
  login --address 127.0.0.1 --quiet \
  >"$TMP/login-missing-target.out" 2>"$TMP/login-missing-target.err"
rc=$?
set -e
[ "$rc" -eq 4 ]
grep -q '^cli_device_missing: ' "$TMP/login-missing-target.err"

# Run: one selected target may receive a literal address override. The alias
# cannot create target scope by itself and is rejected for multi-device scope.
rm -f "$FAKE.master" "$LOG"
# shellcheck disable=SC2046
HOME="$HOME_DIR" NETUSER=smoke NETPASS=not-a-real-secret "$KARVI" $(common_args) \
  run --no-daemon --target router1 --address 127.0.0.1 --transport system \
  --format text --cmd 'show users' \
  >"$TMP/run.out" 2>"$TMP/run.err"
grep -q '^OUTPUT:show users$' "$TMP/run.out"
grep -q '127.0.0.1' "$LOG"

set +e
# shellcheck disable=SC2046
HOME="$HOME_DIR" NETUSER=smoke NETPASS=not-a-real-secret "$KARVI" $(common_args) \
  run --no-daemon --address 127.0.0.1 --transport system --cmd 'show users' \
  >"$TMP/run-missing-target.out" 2>"$TMP/run-missing-target.err"
rc=$?
set -e
[ "$rc" -eq 5 ]
grep -q '^inventory_positive_selector_missing: ' "$TMP/run-missing-target.err"

set +e
# shellcheck disable=SC2046
HOME="$HOME_DIR" NETUSER=smoke NETPASS=not-a-real-secret "$KARVI" $(common_args) \
  run --no-daemon --target router1 --target router2 --address 127.0.0.1 \
  --transport system --cmd 'show users' \
  >"$TMP/run-multiple.out" 2>"$TMP/run-multiple.err"
rc=$?
set -e
[ "$rc" -eq 4 ]
grep -q 'management_address_scope_error' "$TMP/run-multiple.err"

echo 'v0.6.1 address-alias smoke: pass'
