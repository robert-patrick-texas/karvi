#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
KARVI=${KARVI:-$ROOT/bin/karvi-linux-amd64}
. "$ROOT/scripts/lib/json.sh"
TMP=${TMPDIR:-/tmp}/karvi-halt-smoke-$$
. "$ROOT/scripts/lib/host.sh"; host_trust_store "$TMP"   # the trust store under the work directory, never the operator's
SECRET="karvi-halt-secret-$$"
BASE=$TMP/state
SCORE=$TMP/scoreboards
CAP=$TMP/capacity
FAKE=$TMP/fake-ssh
THIRD_MARKER=$TMP/third-target-opened

cleanup() { rm -rf "$TMP"; }
trap cleanup EXIT HUP INT TERM

install -d -m 700 "$BASE" "$SCORE" "$CAP"
cat > "$FAKE" <<SH_INNER
#!/bin/sh
case " \$* " in
  *" -O check "*) exit 1 ;;
  *"127.0.0.2"*) echo 'Permission denied (password).' >&2; exit 255 ;;
  *"127.0.0.3"*) : > "$THIRD_MARKER"; echo 'unexpected third execution' >&2; exit 99 ;;
esac
printf 'router#'
while IFS= read -r line; do
  [ "\$line" = exit ] && exit 0
  printf '%s\r\nfirst target succeeded\r\nrouter#' "\$line"
done
exit 0
SH_INNER
chmod 755 "$FAKE"

set +e
NETUSER=smoke NETPASS=$SECRET "$KARVI" --quiet \
  --set "basedir=\"$BASE\"" --set 'sharedroot="none"' --set "spooldir=\"$BASE/spool\"" --set 'platform-resolution.default=""' \
  --set "ssh.transports.system=\"$FAKE\"" \
  --set audit.journald-required=false \
  --set "audit.file=\"$BASE/audit.jsonl\"" \
  --set "scoreboards=\"$SCORE\"" \
  --set "sessions.shared-capacity-root=\"$CAP\"" \
  --set output.min-free-bytes-after-job=0 \
  run --no-daemon \
  --target 127.0.0.1 --target 127.0.0.2 --target 127.0.0.3 \
  --transport system --dispatch serial --halt-on-error-count 1 --format jsonl \
  'show clock' >"$TMP/run.jsonl" 2>"$TMP/run.err"
code=$?
set -e

[ "$code" -eq 102 ] || {
  echo "expected exit 102, received $code" >&2
  cat "$TMP/run.err" >&2
  exit 1
}
[ "$(wc -l < "$TMP/run.jsonl" | tr -d ' ')" -eq 4 ] # three records and the summary line
[ "$(jsonl_records "$TMP/run.jsonl" status | paste -sd, -)" = succeeded,authentication_error,not_started_halt ]
[ ! -e "$THIRD_MARKER" ] || {
  echo 'halted target was unexpectedly opened' >&2
  exit 1
}

summary=$(find "$BASE/jobs" -name summary.json -type f | head -n 1)
[ -n "$summary" ]
json_is "$summary" primary_exit_code 102
json_is "$summary" final_status halted

if grep -R -F -l -- "$SECRET" "$BASE" "$SCORE" "$CAP" >/dev/null 2>&1; then
  echo "secret sentinel found in a halt artifact" >&2
  exit 1
fi

echo "halt smoke: pass"
