#!/bin/sh
# The check of scripts/lib/json.sh: every form of path, every kind of value,
# the same answers from a compact and an indented file, and the exit codes.
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
. "$ROOT/scripts/lib/json.sh"
TMP=$(mktemp -d); trap 'rm -rf "$TMP"' EXIT HUP INT TERM
failures=0
check() { # check NAME WANT GOT
  if [ "$2" = "$3" ]; then echo "json lib: $1: pass"; else echo "json lib: $1: FAIL: want [$2] got [$3]" >&2; failures=$((failures + 1)); fi
}
code() { set +e; "$@" >/dev/null 2>&1; echo $?; set -e; }

printf '%s' '{"final_status":"completed","primary_exit_code":0,"terminal_causes":["halt"],"cancellation":null,"ok":true,"device_counts":{"incomplete":2,"total":3},"ssh_transports":[{"id":"scrapligo-v1"},{"id":"system"}],"text":"a \"quoted\" é: {x}"}' >"$TMP/compact.json"
python3 -m json.tool "$TMP/compact.json" >"$TMP/indented.json"
for f in compact indented; do
  F=$TMP/$f.json
  check "$f string" completed "$(json_get "$F" final_status)"
  check "$f number" 0 "$(json_get "$F" primary_exit_code)"
  check "$f nested" 2 "$(json_get "$F" device_counts.incomplete)"
  check "$f index" halt "$(json_get "$F" terminal_causes.0)"
  check "$f array" '["halt"]' "$(json_get "$F" terminal_causes)"
  check "$f object" '{"incomplete":2,"total":3}' "$(json_get "$F" device_counts)"
  check "$f null" null "$(json_get "$F" cancellation)"
  check "$f true" true "$(json_get "$F" ok)"
  check "$f each" 'scrapligo-v1 system' "$(json_get "$F" 'ssh_transports.*.id' | tr '\n' ' ' | sed 's/ $//')"
  check "$f string with quotes" 'a "quoted" é: {x}' "$(json_get "$F" text)"
  check "$f absent exit" 1 "$(code json_get "$F" device_counts.none)"
  check "$f index past the end exit" 1 "$(code json_get "$F" terminal_causes.1)"
  check "$f has null" 0 "$(code json_has "$F" cancellation)"
  check "$f has not" 1 "$(code json_has "$F" shuffle_key)"
  check "$f is" 0 "$(code json_is "$F" device_counts.total 3)"
  check "$f is not" 1 "$(code json_is "$F" final_status halted)"
  check "$f a value is not a key" 1 "$(code json_has "$F" completed)"
done
check "a line on standard input" 7 "$(printf '{"a":{"b":7}}\n' | json_get - a.b)"
printf '{"final_status":' >"$TMP/broken.json"
check "not JSON exit" 2 "$(code json_get "$TMP/broken.json" final_status)"
check "missing file exit" 2 "$(code json_get "$TMP/none.json" final_status)"
[ "$failures" -eq 0 ] || { echo "json lib: $failures failed" >&2; exit 1; }
echo "json lib: pass"
