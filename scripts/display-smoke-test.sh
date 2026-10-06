#!/bin/sh
# Verify schema-4 display templates, human timestamps, border expansion, and
# local --quiet suppression without changing machine records.
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
TMP=${TMPDIR:-/tmp}/karvi-display-smoke-$$
. "$ROOT/scripts/lib/host.sh"; host_trust_store "$TMP"   # the trust store under the work directory, never the operator's
HOME_DIR=$TMP/home
BASE=$TMP/state
SCORE=$TMP/scoreboards
CAP=$TMP/capacity
FAKE=$TMP/fake-ssh
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
  *" ControlPath=none "*)
    printf 'display-device#'
    while IFS= read -r line; do
      printf 'COMMAND:%s\n' "$line" >> "$log"
      [ "$line" = exit ] && exit 0
      printf '%s\r\nOUTPUT:%s\r\ndisplay-device#' "$line" "$line"
    done
    exit 0
    ;;
esac
printf 'unexpected noninteractive invocation\n' >&2
exit 96
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

# Defaults: one requested header, one final footer, no border.
rm -f "$FAKE.master" "$FAKE.log"
# shellcheck disable=SC2046
HOME="$HOME_DIR" NETUSER=smoke NETPASS=test "$KARVI" $(common_args) \
  command --management-address 127.0.0.1 --command 'show clock' display-device \
  >"$TMP/default.out" 2>"$TMP/default.err"
grep -Eq '^! display-device \[127\.0\.0\.1\] platform=generic user=smoke backend=[^ ]+ transport=system$' "$TMP/default.out"
[ "$(grep -c '^! display-device \[127\.0\.0\.1\] ' "$TMP/default.out")" -eq 1 ]
grep -Eq '^! exit=0 elapsed=[^ ]+ artifacts=.+$' "$TMP/default.out"
[ "$(grep -c '^! exit=0 elapsed=.* artifacts=' "$TMP/default.out")" -eq 1 ]
absent '^command ' "$TMP/default.out"
[ "$(grep -c '^OUTPUT:show clock$' "$TMP/default.out")" -eq 1 ]
[ "$(grep -c '^START:' "$FAKE.log")" -eq 1 ]
[ "$(grep -c '^COMMAND:show clock$' "$FAKE.log")" -eq 1 ]

# --nof (output.persist-command=false): the same display, the footer's
# artifacts is "none", and no job folder or output file is created; the
# audit log still has the command. --no-persist, which it replaced, is no
# option.
nof_folders() { find "$BASE/jobs" -mindepth 1 2>/dev/null | wc -l; }
nof_before=$(nof_folders)
rm -f "$FAKE.master" "$FAKE.log"
# shellcheck disable=SC2046
HOME="$HOME_DIR" NETUSER=smoke NETPASS=test "$KARVI" $(common_args) \
  command --management-address 127.0.0.1 --command 'show clock' --nof display-device \
  >"$TMP/nof.out" 2>"$TMP/nof.err"
grep -Eq '^! exit=0 elapsed=[^ ]+ artifacts=none$' "$TMP/nof.out"
[ "$(grep -c '^OUTPUT:show clock$' "$TMP/nof.out")" -eq 1 ]
[ "$(nof_folders)" -eq "$nof_before" ]
[ "$(grep -c '"event_name":"command_completed"' "$BASE/audit.jsonl")" -eq 2 ]
nof_code=0
# shellcheck disable=SC2046
HOME="$HOME_DIR" NETUSER=smoke NETPASS=test "$KARVI" $(common_args) \
  command --management-address 127.0.0.1 --command 'show clock' --no-persist display-device \
  >"$TMP/nopersist.out" 2>"$TMP/nopersist.err" || nof_code=$?
[ "$nof_code" -eq 4 ]
grep -q '^cli_option_unknown: unknown option --no-persist' "$TMP/nopersist.err"

# --of[=PATH]: the PATH attaches with = only,
# so the word after a bare --of is the device and the files go to the
# configured folder; with =PATH the job folder is created under PATH; --of
# with --nof is refused before any device is planned.
rm -f "$FAKE.master" "$FAKE.log"
# shellcheck disable=SC2046
HOME="$HOME_DIR" NETUSER=smoke NETPASS=test "$KARVI" $(common_args) \
  command --management-address 127.0.0.1 --command 'show clock' --of display-device \
  >"$TMP/of.out" 2>"$TMP/of.err"
grep -Eq "^! exit=0 elapsed=[^ ]+ artifacts=$BASE/jobs/" "$TMP/of.out"
[ "$(nof_folders)" -gt "$nof_before" ]
rm -f "$FAKE.master" "$FAKE.log"
# shellcheck disable=SC2046
HOME="$HOME_DIR" NETUSER=smoke NETPASS=test "$KARVI" $(common_args) \
  command --management-address 127.0.0.1 --command 'show clock' "--of=$TMP/of-root" display-device \
  >"$TMP/ofpath.out" 2>"$TMP/ofpath.err"
grep -Eq "^! exit=0 elapsed=[^ ]+ artifacts=$TMP/of-root/" "$TMP/ofpath.out"
[ "$(grep -c '^OUTPUT:show clock$' "$TMP/ofpath.out")" -eq 1 ]
[ -s "$(ls -d "$TMP"/of-root/*/* | tail -1)/commands.jsonl" ]
of_code=0
# shellcheck disable=SC2046
HOME="$HOME_DIR" NETUSER=smoke NETPASS=test "$KARVI" $(common_args) \
  command --management-address 127.0.0.1 --command 'show clock' --of --nof display-device \
  >"$TMP/ofnof.out" 2>"$TMP/ofnof.err" || of_code=$?
[ "$of_code" -eq 4 ]
grep -q '^output_options_conflict: --of and --nof are mutually exclusive' "$TMP/ofnof.err"

rm -f "$FAKE.master" "$FAKE.log"
# shellcheck disable=SC2046
HOME="$HOME_DIR" NETUSER=smoke NETPASS=test "$KARVI" $(common_args) \
  --set 'display.timestamp="dd/mm/yyyy hh:mm:ss.sss"' \
  --set 'display.command.header="H <timestamp> <target> <address> <platform> <user> <auth-backend> <transport>"' \
  --set 'display.command.footer="F <reference-id> <exit-status> <artifacts> <timestamp>"' \
  --set 'display.command.border="<repeat:-:40>\n"' \
  --set display.command.last-border=true \
  command --management-address 127.0.0.1 --command 'show clock' --command 'show version' display-device \
  >"$TMP/decorated.out" 2>"$TMP/decorated.err"
grep -Eq '^H [0-9]{2}/[0-9]{2}/[0-9]{4} [0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{3} display-device 127\.0\.0\.1 generic smoke ' "$TMP/decorated.out"
[ "$(grep -c '^----------------------------------------$' "$TMP/decorated.out")" -eq 2 ]
grep -q '^F .* ExitSuccess(0) ' "$TMP/decorated.out"
[ "$(grep -c '^OUTPUT:' "$TMP/decorated.out")" -eq 2 ]
[ "$(grep -c '^START:' "$FAKE.log")" -eq 1 ]
[ "$(grep -c '^COMMAND:show ' "$FAKE.log")" -eq 2 ]

rm -f "$FAKE.master"
# shellcheck disable=SC2046
HOME="$HOME_DIR" NETUSER=smoke NETPASS=test "$KARVI" $(common_args) \
  --set 'display.command.header="SHOULD-NOT-APPEAR"' \
  --set 'display.command.footer="SHOULD-NOT-APPEAR"' \
  --set 'display.command.border="SHOULD-NOT-APPEAR\n"' \
  command --quiet --management-address 127.0.0.1 --command 'show clock' display-device \
  >"$TMP/quiet.out" 2>"$TMP/quiet.err"
grep -q '^OUTPUT:show clock$' "$TMP/quiet.out"
absent 'SHOULD-NOT-APPEAR' "$TMP/quiet.out"

# Structured command records remain undecorated even when display templates
# contain conspicuous values.
rm -f "$FAKE.master" "$FAKE.log"
# shellcheck disable=SC2046
HOME="$HOME_DIR" NETUSER=smoke NETPASS=test "$KARVI" $(common_args) \
  --set 'display.command.header="SHOULD-NOT-APPEAR"' \
  --set 'display.command.footer="SHOULD-NOT-APPEAR"' \
  --set 'display.command.border="SHOULD-NOT-APPEAR\n"' \
  command --format jsonl --management-address 127.0.0.1 --command 'show clock' display-device \
  >"$TMP/jsonl.out" 2>"$TMP/jsonl.err"
[ "$(wc -l < "$TMP/jsonl.out" | tr -d ' ')" -eq 1 ]
grep -q '^{' "$TMP/jsonl.out"
grep -q '"command":"show clock"' "$TMP/jsonl.out"
absent 'SHOULD-NOT-APPEAR' "$TMP/jsonl.out"

echo 'display smoke: pass'
