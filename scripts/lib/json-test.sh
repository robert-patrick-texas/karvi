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
# Python 3.14's json.tool colours its output under FORCE_COLOR, into a file
# too; PYTHON_COLORS=0 outranks it.
PYTHON_COLORS=0 python3 -m json.tool "$TMP/compact.json" >"$TMP/indented.json"
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
# jsonl_records: the command records of a stream, the summary skipped.
TAB=$(printf '\t')
{
  printf '%s\n' '{"record_id":"r1","status":"succeeded","error":null,"output":"a\tb\r\nc","notices":[{"code":"n1"},{"code":"n2"}],"selected_address":"127.0.0.1"}'
  printf '\n'
  printf '%s\n' '{"record_id":"r2","status":"connection_error","error":{"code":"ssh_process_failed"},"notices":[]}'
  printf '%s\n' '{"schema_version":2,"final_status":"errored","status":"not a record"}'
} >"$TMP/stream.jsonl"
check "records one path" 'succeeded connection_error' "$(jsonl_records "$TMP/stream.jsonl" status | tr '\n' ' ' | sed 's/ $//')"
check "records two paths" "127.0.0.1${TAB}succeeded" "$(jsonl_records "$TMP/stream.jsonl" selected_address status | head -1)"
check "records absent empty" "${TAB}connection_error" "$(jsonl_records "$TMP/stream.jsonl" selected_address status | sed -n 2p)"
check "records null" 'null' "$(jsonl_records "$TMP/stream.jsonl" error | head -1)"
check "records object" '{"code":"ssh_process_failed"}' "$(jsonl_records "$TMP/stream.jsonl" error | sed -n 2p)"
check "records the first error" ssh_process_failed "$(jsonl_records "$TMP/stream.jsonl" error.code | grep -m1 .)"
check "records each joined" 'n1,n2' "$(jsonl_records "$TMP/stream.jsonl" 'notices.*.code' | head -1)"
check "records a tab or newline quoted" '"a\tb\r\nc"' "$(jsonl_records "$TMP/stream.jsonl" output | head -1)"
check "records count" 2 "$(jsonl_records "$TMP/stream.jsonl" record_id | wc -l | tr -d ' ')"
check "records joined" 'succeeded,connection_error' "$(jsonl_records "$TMP/stream.jsonl" status | paste -sd, -)"
check "records on standard input" r2 "$(sed -n 3p "$TMP/stream.jsonl" | jsonl_records - record_id)"
printf '%s\n' '{"record_id":"r1"}' '{"record_id":' >"$TMP/broken.jsonl"
check "records a line not JSON exit" 2 "$(code jsonl_records "$TMP/broken.jsonl" record_id)"
check "records missing file exit" 2 "$(code jsonl_records "$TMP/none.jsonl" status)"
[ "$failures" -eq 0 ] || { echo "json lib: $failures failed" >&2; exit 1; }
echo "json lib: pass"
