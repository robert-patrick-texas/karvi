#!/bin/sh
# The check of scripts/lib/lab.sh: under sh and bash, daemon_stop_force
# against a model executable and a socket file (a daemon that stops, one
# whose stop fails, one that goes late, one that ignores the stop), a
# cleanup that reaches its removal past a daemon that stays, and fake_stop;
# and over the tree, no `daemon stop` whose status is ignored outside lab.sh
# and none in a cleanup.
set -eu
ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/../.." && pwd)
. "$ROOT/scripts/lib/exit.sh"
TMP=$(mktemp -d)
cleanup() { rm -rf "$TMP"; }
on_exit cleanup
failures=0
check() { # check NAME WANT GOT
  if [ "$2" = "$3" ]; then echo "lab lib: $1: pass"; else echo "lab lib: $1: FAIL: want [$2] got [$3]" >&2; failures=$((failures + 1)); fi
}

# The model executable: its HOME and arguments on a line of MODEL_LOG, then
# what MODEL_DO says to the socket MODEL_SOCK.
cat >"$TMP/model" <<'EOF'
#!/bin/sh
printf 'HOME=%s %s\n' "${HOME:-}" "$*" >>"$MODEL_LOG"
case $MODEL_DO in
  stops) rm -f "$MODEL_SOCK" ;;
  fails) rm -f "$MODEL_SOCK"; exit 1 ;;
  late) (sleep 0.3; rm -f "$MODEL_SOCK") >/dev/null 2>&1 & ;;
  ignores) ;;
  ignores-fails) exit 1 ;;
esac
EOF
chmod 755 "$TMP/model"

# The caller: sources the libraries under the shell that runs it, then one
# case; lab_stop_tenths 5 (half a second) for the socket that stays.
cat >"$TMP/caller.sh" <<'EOF'
set -eu
. "$1/exit.sh"; . "$1/lab.sh"; W=$2; shift 2
lab_stop_tenths=5
export MODEL_LOG=$W/log MODEL_SOCK=$W/d.sock
case $1 in
  call)
    rc=0
    daemon_stop_force "$MODEL_SOCK" env HOME="$W/home" MODEL_DO="$2" "$W/../model" --quiet --config "$W/k.toml" || rc=$?
    echo "status $rc" ;;
  function)
    k() { MODEL_DO=stops "$W/../model" --set 'basedir="x y"' "$@"; }
    rc=0; daemon_stop_force "$MODEL_SOCK" k || rc=$?
    echo "status $rc" ;;
  cleanup)
    # A suite whose daemon stays: its cleanup, under set -e, still reaches
    # its removal, and the suite's status is its own ($2).
    cleanup() { daemon_stop_force "$MODEL_SOCK" env MODEL_DO=ignores "$W/../model" || :; rm -rf "$W/work"; echo removed; }
    on_exit cleanup
    mkdir "$W/work"
    exit "$2" ;;
  fake)
    sleep 30 & FAKE_PID=$!
    p=$FAKE_PID
    fake_stop FAKE_PID
    kill -0 "$p" 2>/dev/null && echo "running" || echo "gone"
    echo "var [$FAKE_PID]"
    rc=0; fake_stop FAKE_PID || rc=$?; echo "empty $rc"
    sleep 0 & DONE_PID=$!; sleep 0.2
    rc=0; fake_stop DONE_PID || rc=$?; echo "ended $rc [$DONE_PID]"
    rc=0; fake_stop 'x;y' 2>/dev/null || rc=$?; echo "bad name $rc" ;;
esac
EOF

# fresh [nosocket]: a new work folder, LAB_W, holding the socket file (bound,
# then left, by python3) unless nosocket.
fresh() {
  LAB_W=$(mktemp -d "$TMP/w.XXXXXX")
  [ "${1:-}" = nosocket ] || python3 -I -c 'import socket, sys; socket.socket(socket.AF_UNIX).bind(sys.argv[1])' "$LAB_W/d.sock"
}
# lab SHELL CASE [ARG]: the caller under SHELL in LAB_W; its status and its
# output on one line, its stderr in LAB_W/err.
lab() {
  l_rc=0
  "$1" "$TMP/caller.sh" "$ROOT/scripts/lib" "$LAB_W" "$2" "${3:-}" >"$LAB_W/out" 2>"$LAB_W/err" || l_rc=$?
  echo "$l_rc $(tr '\n' ' ' <"$LAB_W/out" | sed 's/ $//')"
}
sock_left() { if [ -S "$LAB_W/d.sock" ]; then echo socket; else echo gone; fi; }
work_left() { if [ -e "$LAB_W/work" ]; then echo work; else echo "no work"; fi; }
logged() { sed "s|$LAB_W|W|g" "$LAB_W/log" 2>/dev/null | tr '\n' '|'; }

for sh in sh bash; do
  fresh nosocket
  check "$sh no socket: nothing run" "0 status 0|" "$(lab "$sh" call stops)|$(logged)"
  fresh
  check "$sh the daemon stops" "0 status 0 gone" "$(lab "$sh" call stops) $(sock_left)"
  check "$sh the command as given, then daemon stop --force" "HOME=W/home --quiet --config W/k.toml daemon stop --force|" "$(logged)"
  fresh
  check "$sh the stop fails, the daemon goes" "0 status 0 gone" "$(lab "$sh" call fails) $(sock_left)"
  fresh
  check "$sh the daemon goes 0.3 s after the stop" "0 status 0 gone" "$(lab "$sh" call late) $(sock_left)"
  fresh
  check "$sh the daemon ignores the stop" "0 status 1 socket" "$(lab "$sh" call ignores) $(sock_left)"
  check "$sh its line on stderr" "$TMP/caller.sh: the daemon at $LAB_W/d.sock outlived daemon stop --force" "$(cat "$LAB_W/err")"
  fresh
  check "$sh the stop fails and the daemon stays" "0 status 1 socket" "$(lab "$sh" call ignores-fails) $(sock_left)"
  fresh
  check "$sh a function of the script as the command" "0 status 0 gone" "$(lab "$sh" function) $(sock_left)"
  check "$sh the function's arguments kept" "HOME=$HOME --set basedir=\"x y\" daemon stop --force|" "$(logged)"
  fresh
  check "$sh a cleanup past a daemon that stays, the suite's 0: its removal reached" "0 removed no work socket" "$(lab "$sh" cleanup 0) $(work_left) $(sock_left)"
  fresh
  check "$sh a cleanup past a daemon that stays, the suite's 1: its removal reached" "1 removed no work socket" "$(lab "$sh" cleanup 1) $(work_left) $(sock_left)"
  fresh nosocket
  check "$sh fake_stop" "0 gone var [] empty 0 ended 0 [] bad name 2" "$(lab "$sh" fake)"
done

# Over the tree: a daemon stop whose status is ignored is daemon_stop_force's
# alone, and no cleanup stops the daemon but through it.
ignored=$(cd "$ROOT" && grep -nE '^[^#]*daemon stop[^|]*\|\| *(true|:)' scripts/*.sh scripts/lib/*.sh Makefile |
  grep -v '^scripts/lib/lab\.sh:' || true)
check "no daemon stop with its status ignored" "" "$ignored"
in_cleanup=$(cd "$ROOT" && for f in scripts/*.sh scripts/lib/*.sh; do
  awk -v f="$f" '
    /^cleanup\(\) *\{/ { body = 1 }
    body && /daemon stop/ { print f ":" FNR ": " $0 }
    body && (/^\}/ || /^cleanup\(\) *\{.*\} *$/) { body = 0 }
  ' "$f"
done)
check "no daemon stop in a cleanup" "" "$in_cleanup"

[ "$failures" -eq 0 ] || { echo "lab lib: $failures failed" >&2; exit 1; }
echo "lab lib: pass"
