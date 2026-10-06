#!/bin/sh
# Exercise v0.8.0 display layout, pretty JSON, dynamic borders, and the
# multi-target text projection that previously hid outputless failures.
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
TMP=${TMPDIR:-/tmp}/karvi-v080-smoke-$$
. "$ROOT/scripts/lib/host.sh"; host_trust_store "$TMP"   # the trust store under the work directory, never the operator's
HOME_DIR=$TMP/home
BASE=$TMP/state
SCORE=$TMP/scoreboards
CAP=$TMP/capacity
FAKE=$TMP/fake-ssh
LOG=$FAKE.log
CFG=$TMP/config.toml
INV=$TMP/inventory.csv

cleanup() {
  "$KARVI" --config "$CFG" daemon stop >/dev/null 2>&1 || true
  rm -rf "$TMP"
}
trap cleanup EXIT HUP INT TERM
install -d -m 700 "$HOME_DIR" "$BASE" "$SCORE" "$CAP"

cat >"$INV" <<'EOF_INV'
name,management_address,platform
vzn-ohio,127.0.0.1,generic
vzn-ftc,127.0.0.2,generic
EOF_INV

cat >"$CFG" <<EOF_CFG
basedir = "$BASE"
sharedroot = "none"
spooldir = "$BASE/spool"
scoreboards = "$SCORE"
platform-resolution.default = ""

[config]
schema-version = 6

[ssh.transports]
system = "$FAKE"

[ssh.command]
transport = "system"

[audit]
journald-required = false
file = "$BASE/audit.jsonl"

[sessions]
shared-capacity-root = "$CAP"

[output]
min-free-bytes-after-job = 0

[display]
color = "never"

[[inventory-source]]
name = "smoke"
type = "csv"
path = "$INV"
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
EOF_CFG
chmod 600 "$CFG"

cat >"$FAKE" <<'EOF_FAKE'
#!/bin/sh
printf 'CALL:%s\n' "$*" >>"$0.log"
case " $* " in
  *" 127.0.0.2 "*)
    printf 'ssh: connect to host 127.0.0.2 port 22: Connection refused\n' >&2
    exit 255
    ;;
esac
case " $* " in
  *" BatchMode=no "*)
    printf 'router1#'
    while IFS= read -r line; do
      [ "$line" = exit ] && exit 0
      printf '%s\r\nOHIO-CLOCK\r\nrouter1#' "$line"
    done
    exit 0
    ;;
  *" -O check "*) exit 1 ;;
esac
case " $* " in
  *" 127.0.0.2 "*)
    printf 'ssh: connect to host 127.0.0.2 port 22: Connection refused\n' >&2
    exit 255
    ;;
esac
printf 'OHIO-CLOCK\n'
exit 0
EOF_FAKE
chmod 755 "$FAKE"

# Exact operator-style argument ordering. The second device intentionally
# fails. Both targets must be attempted, persisted, and visible in text.
set +e
HOME="$HOME_DIR" NETUSER=smoke NETPASS=not-a-real-secret "$KARVI" --config "$CFG" \
  run --target vzn-ohio --command 'show clock' --transport system \
  --format text --target vzn-ftc >"$TMP/run.out" 2>"$TMP/run.err"
rc=$?
set -e
[ "$rc" -eq 101 ] || { cat "$TMP/run.err" >&2; exit 1; }
grep -q '^! vzn-ohio \[127.0.0.1\]' "$TMP/run.out"
grep -q '^OHIO-CLOCK$' "$TMP/run.out"
grep -q '^! vzn-ftc \[127.0.0.2\]' "$TMP/run.out"
grep -q 'target=vzn-ftc status=connection_error error=' "$TMP/run.out"
[ "$(grep -c 'CALL:.*127.0.0.[12]' "$LOG")" -ge 2 ]
job=$(find "$BASE/jobs" -name commands.jsonl -type f | head -n 1)
[ -n "$job" ]
[ "$(wc -l <"$job" | tr -d ' ')" -eq 2 ]
grep -q '"input_target":"vzn-ohio"' "$job"
grep -q '"input_target":"vzn-ftc"' "$job"

# Pretty JSON is a valid indented array; compact JSONL remains line-oriented.
HOME="$HOME_DIR" NETUSER=smoke NETPASS=not-a-real-secret "$KARVI" --config "$CFG" \
  --set display.json.indent=4 run --no-daemon --target vzn-ohio \
  --transport system --format json --cmd 'show clock' >"$TMP/run.json" 2>"$TMP/json.err"
python3 -m json.tool "$TMP/run.json" >/dev/null
grep -q '^    {' "$TMP/run.json"

# --border replaces a configured border and follows the rendered header width.
HOME="$HOME_DIR" NETUSER=smoke NETPASS=not-a-real-secret "$KARVI" --config "$CFG" \
  --set 'display.command.header=target=<target>' \
  --set 'display.command.border=<repeat:=:4>' \
  --set display.command.last-border=true \
  command --border --host vzn-ohio --address 127.0.0.1 \
  --cmd 'show clock' >"$TMP/border.out" 2>"$TMP/border.err"
grep -q '^target=vzn-ohio$' "$TMP/border.out"
grep -q '^! -------------$' "$TMP/border.out"
absent '====' "$TMP/border.out"

# Exercise the real terminal-width path through a 36-column pseudo-terminal.
# ANSI bytes must not affect layout, the configured 80-character border is
# cropped to 36 columns, and the artifacts element moves to its own line.
if command -v script >/dev/null 2>&1; then
  cat >"$TMP/pty-display.toml" <<'EOF_PTY'
[display]
color = "always"

[display.command]
header = "<target> [<address>] platform=<platform> transport=<transport>"
footer = "exit=<exit-code> elapsed=<elapsed> artifacts=<artifacts>"
border = "<repeat:=:80>\n"
last-border = true
EOF_PTY
  pty_command="stty cols 36; HOME='$HOME_DIR' NETUSER=smoke NETPASS=not-a-real-secret '$KARVI' --config '$CFG' --config '$TMP/pty-display.toml' command --host vzn-ohio --address 127.0.0.1 --cmd 'show clock'"
  script -qefc "$pty_command" /dev/null >"$TMP/pty.raw" 2>"$TMP/pty-script.err"
  python3 - "$TMP/pty.raw" "$TMP/pty.txt" <<'PY_PTY'
from pathlib import Path
import re
import sys
raw = Path(sys.argv[1]).read_bytes().decode("utf-8", errors="replace")
plain = re.sub(r"\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07]*(?:\x07|\x1b\\))", "", raw)
Path(sys.argv[2]).write_text(plain.replace("\r", ""))
PY_PTY
  grep -q '^vzn-ohio \[127\.0\.0\.1\]$' "$TMP/pty.txt"
  grep -q '^platform=generic transport=system$' "$TMP/pty.txt"
  grep -q '^====================================$' "$TMP/pty.txt"
  grep -Eq '^exit=0 elapsed=[^ ]+$' "$TMP/pty.txt"
  grep -q '^artifacts=' "$TMP/pty.txt"
fi

# Context help advertises all three formats and the dynamic border.
"$KARVI" command --help >"$TMP/command-help.txt"
grep -q -- '--format text|jsonl|json' "$TMP/command-help.txt"
grep -q -- '--border' "$TMP/command-help.txt"
"$KARVI" run --help >"$TMP/run-help.txt"
grep -q -- '--format text|jsonl|json' "$TMP/run-help.txt"

echo 'v0.8.0 display, JSON, border, and multi-target smoke: pass'
