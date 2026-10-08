#!/bin/sh
# Exercise the v0.7.0 Cisco-style interactive command-session correction,
# semantic display colors, and bounded secret-safe debug diagnostics.
set -eu

# absent PATTERN FILE: the file must not match. A bare "! grep" line is exempt
# from set -e (POSIX: errexit ignores a negated pipeline) and asserts nothing,
# so every must-not-appear check goes through here.
absent() {
  if grep -q -- "$1" "$2"; then
    echo "$0: $2 must not match: $1" >&2
    exit 1
  fi
}

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
KARVI=${KARVI:-$ROOT/bin/karvi-linux-amd64}
TMP=$(mktemp -d "${TMPDIR:-/tmp}/karvi-v070-smoke-XXXXXX")
. "$ROOT/scripts/lib/host.sh"; host_trust_store "$TMP"   # the trust store under the work directory, never the operator's
HOME_DIR=$TMP/home
BASE=$TMP/state
SCORE=$TMP/scoreboards
CAP=$TMP/capacity
FAKE=$TMP/fake-ssh
LOG=$FAKE.log
trap 'rm -rf "$TMP"' EXIT HUP INT TERM
install -d -m 700 "$HOME_DIR" "$BASE" "$SCORE" "$CAP"

cat >"$FAKE" <<'SH'
#!/bin/sh
log="$0.log"
# The system transport's ssh -Q capability queries are not sessions.
[ "$1" = -Q ] && exit 0
printf 'START:%s\n' "$*" >>"$log"
case " $* " in
  *" -O check "*|*" -M "*)
    printf 'FORBIDDEN-CONTROLMASTER\n' >>"$log"
    printf 'Master refused session request: Permission denied\n' >&2
    exit 97
    ;;
esac
case " $* " in
  *" ControlPath=none "*) : ;;
  *) printf 'MISSING-CONTROLPATH-NONE\n' >>"$log"; exit 98 ;;
esac
printf 'Model: Cisco 4451-X\r\nSN: FJC1950D005\r\n\r\nvzn-ohio#'
while IFS= read -r line; do
  printf 'INPUT:%s\n' "$line" >>"$log"
  case "$line" in
    exit) exit 0 ;;
    'show clock')
      printf 'show clock\r\n01:20:19.849 EDT Thu Sep 10 2026\r\nvzn-ohio#'
      ;;
    'show version')
      printf 'show version\r\nCisco IOS XE Software, Version 17.12\r\nvzn-ohio#'
      ;;
    *)
      printf '%s\r\n%% Invalid input detected\r\nvzn-ohio#' "$line"
      ;;
  esac
done
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

# The reported Cisco failure is reproduced by a fake that refuses any
# ControlMaster flow. v0.7.0 must instead open one fresh interactive shell and
# execute every command through that same authenticated process.
rm -f "$LOG"
# shellcheck disable=SC2046
HOME="$HOME_DIR" NETUSER=svc.quinlan NETPASS=DO-NOT-LOG-THIS "$KARVI" $(common_args) \
  --set display.color=never \
  command --host vzn-ohio --address 10.0.2.9 --debug \
  --cmd 'show clock' --cmd 'show version' \
  >"$TMP/command.out" 2>"$TMP/command.err"

[ "$(grep -c '^START:' "$LOG")" -eq 1 ]
[ "$(grep -c '^INPUT:show ' "$LOG")" -eq 2 ]
absent 'FORBIDDEN-CONTROLMASTER\|MISSING-CONTROLPATH-NONE' "$LOG"
grep -q '^01:20:19.849 EDT Thu Sep 10 2026$' "$TMP/command.out"
grep -q '^Cisco IOS XE Software, Version 17.12$' "$TMP/command.out"
absent 'authentication_failed\|ExitAuthenticationFailure' "$TMP/command.err"
grep -q ' DEBUG activity=command ' "$TMP/command.err"
grep -q 'device resolved target="vzn-ohio" address=10.0.2.9' "$TMP/command.err"
grep -q 'device credential target="vzn-ohio" device_user="svc.quinlan"' "$TMP/command.err"
grep -q 'system SSH command session starting .*control_path=disabled' "$TMP/command.err"
grep -q 'system SSH command session ready .*prompt="vzn-ohio#"' "$TMP/command.err"
[ "$(grep -c 'device command start target="vzn-ohio"' "$TMP/command.err")" -eq 2 ]
grep -q 'output_bytes=' "$TMP/command.err"
absent 'DO-NOT-LOG-THIS' "$TMP/command.err"
# Command text appears in debug output on the "device command start" event
# only, once per command. The v0.7.0 form of this check, no
# command text at all, was a bare "! grep" and had asserted nothing since.
[ "$(grep -c 'show clock\|show version' "$TMP/command.err")" -eq 2 ]
[ "$(grep 'show clock\|show version' "$TMP/command.err" | grep -c 'device command start .* command=')" -eq 2 ]

# Color=always allows exact semantic-role verification even though this smoke
# test captures stdout rather than allocating a terminal.
rm -f "$LOG"
# shellcheck disable=SC2046
HOME="$HOME_DIR" NETUSER=svc.quinlan NETPASS=DO-NOT-LOG-THIS "$KARVI" $(common_args) \
  --set display.color=always \
  --set 'display.command.border="<repeat:-:8>\n"' \
  --set display.command.last-border=true \
  command --host vzn-ohio --address 10.0.2.9 --cmd 'show clock' \
  >"$TMP/color.out" 2>"$TMP/color.err"
ESC=$(printf '\033')
grep -Fq "${ESC}[1;33mvzn-ohio${ESC}[0m" "$TMP/color.out"
# The dark palette: the address bold magenta, the
# labels and brackets blue, the border gray.
grep -Fq "${ESC}[1;35m10.0.2.9${ESC}[0m" "$TMP/color.out"
grep -Fq "${ESC}[34m] platform=${ESC}[0m" "$TMP/color.out"
grep -Fq "${ESC}[90m--------" "$TMP/color.out"
grep -Fq "${ESC}[34m! exit=${ESC}[0m${ESC}[37m0${ESC}[0m" "$TMP/color.out"

# Quiet removes generated headers, footers, and borders but leaves device
# output and explicit debug diagnostics available.
rm -f "$LOG"
# shellcheck disable=SC2046
HOME="$HOME_DIR" NETUSER=svc.quinlan NETPASS=DO-NOT-LOG-THIS "$KARVI" $(common_args) \
  --set display.color=always \
  --set 'display.command.border="VISIBLE-BORDER\n"' \
  command --quiet --debug --host vzn-ohio --address 10.0.2.9 --cmd 'show clock' \
  >"$TMP/quiet.out" 2>"$TMP/quiet.err"
grep -q '^01:20:19.849 EDT Thu Sep 10 2026$' "$TMP/quiet.out"
absent 'vzn-ohio \[10.0.2.9\]\|VISIBLE-BORDER\|artifacts=' "$TMP/quiet.out"
grep -q ' DEBUG activity=command ' "$TMP/quiet.err"

# Context help exposes debug in the modes where the local spelling is accepted.
"$KARVI" command --help 2>&1 | grep -q -- '--debug'
"$KARVI" login --help 2>&1 | grep -q -- '--debug'
"$KARVI" run --help 2>&1 | grep -q -- '--debug'

echo 'v0.7.0 command-session, display-color, and debug smoke: pass'
