#!/bin/sh
# The check of scripts/lib/exit.sh: a model script under sh and bash, ended
# by each signal and by itself, runs its cleanup once and ends with the status
# on_exit names; and every script sets its cleanup through on_exit.
set -eu
ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/../.." && pwd)
. "$ROOT/scripts/lib/exit.sh"
TMP=$(mktemp -d)
cleanup() { rm -rf "$TMP"; }
on_exit cleanup
failures=0
check() { # check NAME WANT GOT
  if [ "$2" = "$3" ]; then echo "exit lib: $1: pass"; else echo "exit lib: $1: FAIL: want [$2] got [$3]" >&2; failures=$((failures + 1)); fi
}

# The model: on_exit, a mark that it is waiting, then a wait on a command
# (sleep) until the stop file appears, or an end of its own.
cat >"$TMP/model.sh" <<'EOF'
. "$1"; OUT=$2; READY=$3; STOP=$4
cleanup() { echo cleanup >>"$OUT"; false; }
on_exit cleanup
: >"$READY"
case $5 in
  wait) while [ ! -e "$STOP" ]; do sleep 0.05; done; echo "carried on" >>"$OUT" ;;
  exit3) exit 3 ;;
  errexit) set -e; false; echo "carried on" >>"$OUT" ;;
  end) ;;
esac
EOF
# model SHELL END [SIGNAL TARGET]: the model's status and what it wrote, on
# one line. The model starts as a terminal's job (terminal_job), not as a
# plain asynchronous command, whose INT starts ignored. TARGET "group"
# signals the model and its command, as a Ctrl-C or `kill -- -PGID` does;
# "script" the model alone, as timeout(1) does.
model() {
  d=$(mktemp -d "$TMP/model.XXXXXX")
  terminal_job "$1" "$TMP/model.sh" "$ROOT/scripts/lib/exit.sh" "$d/out" "$d/ready" "$d/stop" "$2" 2>"$d/err"
  pid=$!
  i=0
  while [ ! -e "$d/ready" ] && [ "$i" -lt 400 ]; do sleep 0.05; i=$((i + 1)); done
  if [ $# -eq 4 ]; then
    if [ "$4" = group ]; then kill -s "$3" -- "-$pid"; else kill -s "$3" "$pid"; fi
  fi
  : >"$d/stop"
  rc=0; wait "$pid" || rc=$?
  echo "$rc $({ tr '\n' ' ' <"$d/out"; } 2>/dev/null | sed 's/ $//')"
}

for sh in sh bash; do
  check "$sh HUP" "129 cleanup" "$(model "$sh" wait HUP group)"
  check "$sh INT (a Ctrl-C)" "130 cleanup" "$(model "$sh" wait INT group)"
  check "$sh TERM" "143 cleanup" "$(model "$sh" wait TERM group)"
  check "$sh TERM to the script alone" "143 cleanup" "$(model "$sh" wait TERM script)"
  check "$sh its end" "0 carried on cleanup" "$(model "$sh" wait)"
  check "$sh exit 3" "3 cleanup" "$(model "$sh" exit3)"
  check "$sh set -e" "1 cleanup" "$(model "$sh" errexit)"
done

# Every script sets its cleanup through on_exit: no trap on EXIT elsewhere.
others=$(cd "$ROOT" && grep -rnE "^[^#]*trap +('[^']*'|\"[^\"]*\"|[A-Za-z_][A-Za-z0-9_]*) +[A-Z0-9 ]*EXIT" scripts Makefile |
  grep -v '^scripts/lib/exit\.sh:' || true)
check "every script through on_exit" "" "$others"

[ "$failures" -eq 0 ] || { echo "exit lib: $failures failed" >&2; exit 1; }
echo "exit lib: pass"
