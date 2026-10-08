#!/bin/sh
# Exercise v0.9.0 address-family overrides, between-record borders, run border
# parity, trailing command options, and error-path prompt/command echo.
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
TMP=$(mktemp -d "${TMPDIR:-/tmp}/karvi-v090-smoke-XXXXXX")
. "$ROOT/scripts/lib/host.sh"; host_trust_store "$TMP"   # the trust store under the work directory, never the operator's
HOME_DIR=$TMP/home
BASE=$TMP/state
SCORE=$TMP/scoreboards
CAP=$TMP/capacity
FAKE=$TMP/fake-ssh
LOG=$FAKE.log

cleanup() {
  "$KARVI" --set "basedir=\"$BASE\"" --set 'sharedroot="none"' --set "spooldir=\"$BASE/spool\"" --set 'platform-resolution.default=""' daemon stop >/dev/null 2>&1 || true
  rm -rf "$TMP"
}
trap cleanup EXIT HUP INT TERM
install -d -m 700 "$HOME_DIR" "$BASE" "$SCORE" "$CAP"

cat >"$FAKE" <<'EOF_FAKE'
#!/bin/sh
printf 'CALL:%s\n' "$*" >>"$0.log"
case " $* " in
  *" -O check "*) exit 1 ;;
  *" BatchMode=no "*)
    printf 'Model: Cisco 4451-X\r\nSN: TEST090\r\n\r\nvzn-ohio#'
    while IFS= read -r line; do
      printf 'INPUT:%s\n' "$line" >>"$0.log"
      case "$line" in
        exit) exit 0 ;;
        'show clock')
          printf 'show clock\r\n01:20:19.849 EDT Thu Sep 10 2026\r\nvzn-ohio#'
          ;;
        'show version')
          printf 'show version\r\nCisco IOS XE Software, Version 17.12\r\nvzn-ohio#'
          ;;
        *)
          printf '%s\r\n                    ^\r\n%% Invalid input detected at '\''^'\'' marker.\r\nvzn-ohio#' "$line"
          ;;
      esac
    done
    exit 0
    ;;
esac
# The run/system path invokes one noninteractive process per command. Keep the
# fixture deterministic while allowing OpenSSH-style option probes above.
printf 'RUN-OUTPUT\n'
exit 0
EOF_FAKE
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
    --set output.min-free-bytes-after-job=0 \
    --set display.color=never
}

# Two positional command words form one combined invalid command. Options
# after freeform text would be device text, so the options precede the
# device. The device answers with its invalid-input text; a direct target is
# platform generic, which checks for no command errors, so the answer is
# recorded as it comes and the command succeeds. The output must echo the
# observed prompt plus the command sent.
set +e
# shellcheck disable=SC2046
HOME="$HOME_DIR" NETUSER=svc.quinlan NETPASS=test "$KARVI" $(common_args) \
  cmd --address 10.0.2.9 --border --transport system --echo vzn-ohio "show clock" "show clock" \
  >"$TMP/error-echo.out" 2>"$TMP/error-echo.err"
rc=$?
set -e
[ "$rc" -eq 0 ] || { cat "$TMP/error-echo.err" >&2; exit 1; }
grep -q '^vzn-ohio#show clock show clock$' "$TMP/error-echo.out"
grep -q "% Invalid input detected at '\^' marker\." "$TMP/error-echo.out"
absent 'device_command_error' "$TMP/error-echo.err"
# One record and last-border=false means even --border has no trailing divider.
[ "$(grep -Ec '^! -+$' "$TMP/error-echo.out" || true)" -eq 0 ]

# Borders are separators between command records. The default suppresses the
# final border; last-border=true restores it.
# shellcheck disable=SC2046
HOME="$HOME_DIR" NETUSER=svc.quinlan NETPASS=test "$KARVI" $(common_args) \
  command --host vzn-ohio --address 10.0.2.9 --transport system --echo --border \
  --cmd 'show clock' --cmd 'show version' >"$TMP/command-border.out" 2>"$TMP/command-border.err"
[ "$(grep -Ec '^! -+$' "$TMP/command-border.out" || true)" -eq 1 ]
grep -q '^vzn-ohio#show clock$' "$TMP/command-border.out"
grep -q '^vzn-ohio#show version$' "$TMP/command-border.out"

# shellcheck disable=SC2046
HOME="$HOME_DIR" NETUSER=svc.quinlan NETPASS=test "$KARVI" $(common_args) \
  --set display.command.last-border=true \
  command --host vzn-ohio --address 10.0.2.9 --transport system --border \
  --cmd 'show clock' --cmd 'show version' >"$TMP/command-last-border.out" 2>"$TMP/command-last-border.err"
[ "$(grep -Ec '^! -+$' "$TMP/command-last-border.out" || true)" -eq 2 ]

# --noborder disables both configured and dynamic separators.
# shellcheck disable=SC2046
HOME="$HOME_DIR" NETUSER=svc.quinlan NETPASS=test "$KARVI" $(common_args) \
  --set 'display.command.border="VISIBLE-COMMAND-BORDER\n"' \
  --set display.command.last-border=true \
  command --host vzn-ohio --address 10.0.2.9 --transport system --noborder \
  --cmd 'show clock' --cmd 'show version' >"$TMP/command-noborder.out" 2>"$TMP/command-noborder.err"
absent 'VISIBLE-COMMAND-BORDER' "$TMP/command-noborder.out"

# Run mode has the same configured/dynamic border behavior.
# shellcheck disable=SC2046
HOME="$HOME_DIR" NETUSER=svc.quinlan NETPASS=test "$KARVI" $(common_args) \
  run --no-daemon --target vzn-ohio --address 10.0.2.9 --transport system \
  --format text --border --cmd 'show clock' --cmd 'show version' \
  >"$TMP/run-border.out" 2>"$TMP/run-border.err"
[ "$(grep -Ec '^! -+$' "$TMP/run-border.out" || true)" -eq 1 ]
[ "$(grep -Ec '^(01:20:19.849 EDT Thu Sep 10 2026|Cisco IOS XE Software, Version 17.12)$' "$TMP/run-border.out")" -eq 2 ]

# shellcheck disable=SC2046
HOME="$HOME_DIR" NETUSER=svc.quinlan NETPASS=test "$KARVI" $(common_args) \
  --set display.run.last-border=true \
  run --no-daemon --target vzn-ohio --address 10.0.2.9 --transport system \
  --format text --border --cmd 'show clock' --cmd 'show version' \
  >"$TMP/run-last-border.out" 2>"$TMP/run-last-border.err"
[ "$(grep -Ec '^! -+$' "$TMP/run-last-border.out" || true)" -eq 2 ]

# shellcheck disable=SC2046
HOME="$HOME_DIR" NETUSER=svc.quinlan NETPASS=test "$KARVI" $(common_args) \
  --set 'display.run.border="VISIBLE-RUN-BORDER\n"' \
  --set display.run.last-border=true \
  run --no-daemon --target vzn-ohio --address 10.0.2.9 --transport system \
  --format text --noborder --cmd 'show clock' --cmd 'show version' \
  >"$TMP/run-noborder.out" 2>"$TMP/run-noborder.err"
absent 'VISIBLE-RUN-BORDER' "$TMP/run-noborder.out"

# run --no-daemon is displayed as command is: rendered from memory as the
# records arrive and ended by the footer (display.run.footer, the command
# footer's default), nothing on standard error (28.4: the result line is
# gone); nothing is read back from commands.jsonl. The footer is the
# display's last line, once; a site's template is used; an empty one
# disables it.
[ "$(tail -n 1 "$TMP/run-border.out" | grep -Ec '^! exit=0 elapsed=[^ ]+ artifacts=.+$')" -eq 1 ]
[ "$(grep -c ' artifacts=' "$TMP/run-border.out")" -eq 1 ]
[ "$(grep -Ec 'exit=ExitSuccess\(0\) artifacts=' "$TMP/run-border.err")" -eq 0 ]
# shellcheck disable=SC2046
HOME="$HOME_DIR" NETUSER=svc.quinlan NETPASS=test "$KARVI" $(common_args) \
  --set 'display.run.footer="RUN-FOOTER <exit-status>"' \
  run --no-daemon --target vzn-ohio --address 10.0.2.9 --transport system \
  --format text --cmd 'show clock' >"$TMP/run-footer.out" 2>"$TMP/run-footer.err"
[ "$(tail -n 1 "$TMP/run-footer.out")" = 'RUN-FOOTER ExitSuccess(0)' ]
# shellcheck disable=SC2046
HOME="$HOME_DIR" NETUSER=svc.quinlan NETPASS=test "$KARVI" $(common_args) \
  --set 'display.run.footer=""' \
  run --no-daemon --target vzn-ohio --address 10.0.2.9 --transport system \
  --format text --cmd 'show clock' >"$TMP/run-nofooter.out" 2>"$TMP/run-nofooter.err"
absent 'artifacts=' "$TMP/run-nofooter.out"
[ "$(tail -n 1 "$TMP/run-nofooter.out")" = '01:20:19.849 EDT Thu Sep 10 2026' ]
# A reader that goes away is a write error the job outlives, never a SIGPIPE
# death with a device mid-list: both commands are recorded and the exit is
# the output failure's. The reader is `true`, gone before the first record
# exists; `head -n 1` would race a display small enough for the pipe's buffer.
# shellcheck disable=SC2046
{ HOME="$HOME_DIR" NETUSER=svc.quinlan NETPASS=test "$KARVI" $(common_args) \
  run --no-daemon --target vzn-ohio --address 10.0.2.9 --transport system \
  --format text --cmd 'show clock' --cmd 'show version' 2>"$TMP/run-pipe.err" || echo "$?" >"$TMP/run-pipe.code"; } | true
[ "$(cat "$TMP/run-pipe.code")" -eq 111 ]
grep -q '^terminal_write_failed: ' "$TMP/run-pipe.err"
pipe_job=$(ls -d "$BASE"/jobs/*/* | tail -1)
[ "$(wc -l <"$pipe_job/commands.jsonl")" -eq 2 ]

# Border selection and address-family selection are deliberately exclusive.
for invocation in \
  "command --host vzn-ohio --cmd show-clock --border --noborder" \
  "run --no-daemon --target vzn-ohio --transport system --cmd show-clock --border --noborder" \
  "login --host vzn-ohio --4 --6" \
  "command --host vzn-ohio --cmd show-clock --ipv4 --ipv6" \
  "run --no-daemon --target vzn-ohio --transport system --cmd show-clock --4 --6"
do
  set +e
  # Intentional word splitting: each test contains no embedded spaces in values.
  # shellcheck disable=SC2086,SC2046
  HOME="$HOME_DIR" NETUSER=svc.quinlan NETPASS=test "$KARVI" $(common_args) $invocation \
    >"$TMP/conflict.out" 2>"$TMP/conflict.err"
  conflict_rc=$?
  set -e
  [ "$conflict_rc" -eq 4 ] || { cat "$TMP/conflict.err" >&2; exit 1; }
  grep -q 'mutually exclusive' "$TMP/conflict.err"
done

# Generated/effective defaults are part of the executable contract.
# shellcheck disable=SC2046
HOME="$HOME_DIR" "$KARVI" $(common_args) config show >"$TMP/config.out"
grep -Fq 'name.dns-timeout = "2s"' "$TMP/config.out"
grep -Fq 'telnet.read-timeout = "60s"' "$TMP/config.out"
grep -Fq 'display.command.border = "\n"' "$TMP/config.out"
grep -Fq 'display.command.last-border = false' "$TMP/config.out"
grep -Fq 'display.run.border = "\n"' "$TMP/config.out"
grep -Fq 'display.run.dynamic-border-length = 72' "$TMP/config.out"
grep -Fq 'display.run.last-border = false' "$TMP/config.out"

for mode in login command run; do
  "$KARVI" "$mode" --help >"$TMP/$mode-help.txt"
  grep -q -- '--ipv4, --4' "$TMP/$mode-help.txt"
  grep -q -- '--ipv6, --6' "$TMP/$mode-help.txt"
done
grep -q -- '--noborder' "$TMP/command-help.txt"
grep -q -- '--noborder' "$TMP/run-help.txt"

echo 'v0.9.0 family-preference, border, and error-echo smoke: pass'
