#!/bin/sh
# Exercise v0.6.0 transport selection, aliases, flexible direct-mode parsing,
# version inventory, and prompt/command echo behavior without a real device.
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
KARVI=${KARVI:-$ROOT/bin/karvi-linux-amd64}
. "$ROOT/scripts/lib/json.sh"
TMP=${TMPDIR:-/tmp}/karvi-v060-smoke-$$
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
    --set audit.journald-required=false \
    --set "audit.file=\"$BASE/audit.jsonl\"" \
    --set "scoreboards=\"$SCORE\"" \
    --set "sessions.shared-capacity-root=\"$CAP\"" \
    --set output.min-free-bytes-after-job=0
}

# Version output describes only transports present in this exact executable.
"$KARVI" --version >"$TMP/version.txt"
grep -q '^ssh_transports:$' "$TMP/version.txt"
grep -q '^  system: external (id=system, linkage=external-executable)$' "$TMP/version.txt"
"$KARVI" version --format json >"$TMP/version.json"
json_get "$TMP/version.json" 'ssh_transports.*.id' | grep -qx system

# Every executable carries the scrapligo-v1 adapter (the build tag and the
# dependency-free preview are gone), and reports the exact compiled-in
# implementation.
"$KARVI" config validate >"$TMP/internal-validate.out"
"$KARVI" run --help >"$TMP/run-help.txt"
json_get "$TMP/version.json" 'ssh_transports.*.id' | grep -qx scrapligo-v1
grep -q 'status: available (scrapligo 1.4.2 compiled in)' "$TMP/run-help.txt"
grep -q 'scrapligo: 1.4.2' "$TMP/version.txt"
line=$(grep -n 'silently using OpenSSH\.$' "$TMP/run-help.txt" | cut -d: -f1)
[ -n "$line" ]
next=$((line + 1))
[ -z "$(sed -n "${next}p" "$TMP/run-help.txt")" ]

# An operator-authored unavailable implementation is a configuration error even
# if the corresponding slot is not selected by an activity.
set +e
"$KARVI" --set 'ssh.transports.alternate1="scrapligo-v2"' config validate \
  >"$TMP/unavailable.out" 2>"$TMP/unavailable.err"
rc=$?
set -e
[ "$rc" -eq 2 ]
grep -q 'ssh.transports.alternate1' "$TMP/unavailable.err"
grep -q 'scrapligo-v2' "$TMP/unavailable.err"

# `cmd`, --cmd, --host, and an after-command --target remain equivalent to the
# long command syntax. --echo renders the prompt plus sent command. Command mode uses one interactive SSH process.
rm -f "$FAKE.master" "$LOG"
# shellcheck disable=SC2046
HOME="$HOME_DIR" NETUSER=smoke NETPASS=not-a-real-secret "$KARVI" $(common_args) \
  cmd --management-address 127.0.0.1 --echo --cmd 'show clock' --host router1 \
  >"$TMP/cmd.out" 2>"$TMP/cmd.err"
grep -q '^router1#show clock$' "$TMP/cmd.out"
grep -q '^OUTPUT:show clock$' "$TMP/cmd.out"
[ "$(grep -c '^START:' "$LOG")" -eq 1 ]
[ "$(grep -c '^COMMAND:show clock$' "$LOG")" -eq 1 ]

# The target may occur between separately declared commands. Both commands use
# one authenticated interactive OpenSSH shell; the final `exit` is session
# cleanup rather than a requested device command.
rm -f "$FAKE.master" "$LOG"
# shellcheck disable=SC2046
HOME="$HOME_DIR" NETUSER=smoke NETPASS=not-a-real-secret "$KARVI" $(common_args) \
  command --cmd 'show clock' --address 127.0.0.1 --target router1 --command 'show version' \
  --quiet \
  >"$TMP/flexible.out" 2>"$TMP/flexible.err"
[ "$(grep -c '^OUTPUT:' "$TMP/flexible.out")" -eq 2 ]
[ "$(grep -c '^START:' "$LOG")" -eq 1 ]
[ "$(grep -c '^COMMAND:show ' "$LOG")" -eq 2 ]

# display.command.echo supplies the same behavior without the CLI switch.
rm -f "$FAKE.master" "$LOG"
# shellcheck disable=SC2046
HOME="$HOME_DIR" NETUSER=smoke NETPASS=not-a-real-secret "$KARVI" $(common_args) \
  --set display.command.echo=true \
  command --management-address 127.0.0.1 --cmd 'show inventory' --target router1 --quiet \
  >"$TMP/config-echo.out" 2>"$TMP/config-echo.err"
grep -q '^router1#show inventory$' "$TMP/config-echo.out"

# Login accepts positional, --target, and --host target spellings around options.
rm -f "$FAKE.master" "$LOG"
# shellcheck disable=SC2046
HOME="$HOME_DIR" NETUSER=smoke NETPASS=not-a-real-secret "$KARVI" $(common_args) \
  login --quiet --host router1 --management-address 127.0.0.1 \
  >"$TMP/login.out" 2>"$TMP/login.err"
grep -q '^LOGIN-OK$' "$TMP/login.out"

# Run target and command aliases, text echo, and the --tf target file.
rm -f "$FAKE.master" "$LOG"
printf '127.0.0.1\n' >"$TMP/targets.txt"
# shellcheck disable=SC2046
HOME="$HOME_DIR" NETUSER=smoke NETPASS=not-a-real-secret "$KARVI" $(common_args) \
  run --no-daemon --tf "$TMP/targets.txt" --transport system \
  --format text --echo --cmd 'show users' \
  >"$TMP/run.out" 2>"$TMP/run.err"
grep -q '^router1#show users$' "$TMP/run.out"   # the observed prompt, as command mode
grep -q '^OUTPUT:show users$' "$TMP/run.out"

echo 'v0.6.0 transport and CLI smoke: pass'
