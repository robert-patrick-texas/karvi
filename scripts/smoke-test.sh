#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
. "$ROOT/scripts/lib/json.sh"
KARVI=${KARVI:-$ROOT/bin/karvi-linux-amd64}
. "$ROOT/scripts/lib/host.sh"; host_config_clean "$KARVI"   # refused on a host whose configuration reaches karvi
TMP=$(mktemp -d "${TMPDIR:-/tmp}/karvi-smoke-XXXXXX")
host_trust_store "$TMP"   # the trust store under the work directory, never the operator's
SECRET="karvi-smoke-secret-$$"
BASE=$TMP/state
SCORE=$BASE/scoreboards
CAP=$BASE/capacity
FAKE=$TMP/fake-ssh

cleanup() {
  daemon_stop_force "$BASE/socket/d.sock" "$KARVI" --config "$TMP/places.toml" \
    --set 'platform-resolution.default=""' --set "daemon.sockets=\"$BASE/socket\"" || :
  rm -rf "$TMP"
}
. "$ROOT/scripts/lib/exit.sh"; . "$ROOT/scripts/lib/lab.sh"; on_exit cleanup

install -d -m 700 "$BASE" "$SCORE" "$CAP"
lab_places "$TMP/places.toml" "$BASE"
printf '%s' "$SECRET" | sha256sum | awk '{print $1}' > "$FAKE.expected"
cat > "$FAKE" <<'SH_INNER'
#!/bin/sh
args=" $* "
case "$args" in
  *" ControlPath=none "*)
    # command and run open one fresh interactive shell per device. Validate
    # that askpass is available only for this initial authentication and keep
    # the shell alive for every command in the invocation.
    answer=$("$SSH_ASKPASS" 'Password:') || exit 91
    actual=$(printf '%s' "$answer" | sha256sum | awk '{print $1}')
    expected=$(cat "$0.expected")
    unset answer
    [ "$actual" = "$expected" ] || {
      echo 'askpass response digest mismatch' >&2
      exit 92
    }
    printf 'smoke-device#'
    while IFS= read -r line; do
      [ "$line" = exit ] && exit 0
      printf '%s\r\nkarvi smoke output\r\nsmoke-device#' "$line"
    done
    exit 0
    ;;
esac
# command and run both use the interactive shell above; any other
# invocation is unexpected.
printf 'unexpected noninteractive invocation\n' >&2
exit 96
SH_INNER
chmod 755 "$FAKE"

common_args() {
  printf '%s\n' \
    --config "$TMP/places.toml" \
    --quiet \
    --set 'platform-resolution.default=""' \
    --set "ssh.transports.system=\"$FAKE\"" \
    --set 'ssh.command.transport="system"'
}

# shellcheck disable=SC2046
NETUSER=smoke NETPASS=$SECRET "$KARVI" $(common_args) \
  command --management-address 127.0.0.1 smoke-device 'show version' \
  >"$TMP/command.out" 2>"$TMP/command.err"
grep -q '^karvi smoke output$' "$TMP/command.out"

# Exercise daemon auto-launch and record cardinality.
# shellcheck disable=SC2046
NETUSER=smoke NETPASS=$SECRET "$KARVI" $(common_args) \
  run --target 127.0.0.1 --target 127.0.0.2 \
  --transport system --dispatch parallel --workers 2 --format jsonl 'show clock' \
  >"$TMP/run.jsonl" 2>"$TMP/run.err"
[ "$(wc -l < "$TMP/run.jsonl" | tr -d ' ')" -eq 3 ] # two records and the summary line

[ "$(jsonl_records "$TMP/run.jsonl" status | paste -sd, -)" = success,success ]
find "$BASE/jobs" -name summary.json -type f | grep -q .

# The idle timer (daemon.shutdown-idle-timer):
# a daemon started with a one-minute timer leaves by itself within two
# minutes (the check runs once a minute), status probes every five seconds
# notwithstanding, writes one log line naming the key, and removes its
# socket; the next client finds none. The daemon the run above launched
# carries the default (10m) and is stopped first.
# shellcheck disable=SC2046
daemon_stop_force "$BASE/socket/d.sock" "$KARVI" $(common_args) || { echo "the run's daemon did not stop" >&2; exit 1; }
# shellcheck disable=SC2046
"$KARVI" $(common_args) --set daemon.shutdown-idle-timer=1m daemon start >/dev/null 2>"$TMP/idle.err"
i=0
# shellcheck disable=SC2046
while "$KARVI" $(common_args) --quiet daemon status >/dev/null 2>&1; do
  i=$((i + 1))
  [ $i -le 30 ] || { echo "the daemon did not leave within 150s of a 1m idle timer" >&2; exit 1; }
  sleep 5
done
[ $i -ge 10 ] || { echo "the daemon left after ${i}x5s, before its 1m idle timer" >&2; exit 1; }
grep -q 'msg="stopping: idle" .*key=daemon.shutdown-idle-timer value=1m0s' "$BASE/logs/daemon.log"
[ ! -e "$BASE/socket/d.sock" ]

if grep -R -F -l -- "$SECRET" "$BASE" "$SCORE" "$CAP" >/dev/null 2>&1; then
  echo "secret sentinel found in an artifact" >&2
  exit 1
fi

echo "smoke: pass"
