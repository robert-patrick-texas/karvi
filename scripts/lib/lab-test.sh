#!/bin/sh
# The check of scripts/lib/lab.sh: under sh and bash, daemon_stop_force
# against a model executable and a socket file (a daemon that stops, one
# whose stop fails, one that goes late, one that ignores the stop), a
# cleanup that reaches its removal past a daemon that stays, daemon_pid (an
# indented and a compact status, a pid only inside another object, no
# daemon), fake_stop, and absent (a file without the pattern, with it, a
# pattern that begins with --, a file that cannot be read), lab_build
# against a model toolchain (its form, its folder, GO, a failed build),
# fake_start against a model fake (its port, its pid, its flags, a fake
# that prints no port), newest_job (the order, a file among the folders, a
# path with a space, none), and lab_places (the file read as TOML, its mode,
# the paths it refuses); and over the tree, no `daemon stop` whose status is
# ignored outside lab.sh and none in a cleanup, no daemon pid read but
# through daemon_pid, no copy of absent, no must-not-appear line that cannot
# stop its script, and no build, fake start, newest-job lookup, or place
# key but through lab.sh.
set -eu
ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/../.." && pwd)
. "$ROOT/scripts/lib/exit.sh"
TMP=$(mktemp -d)
cleanup() { rm -rf "$TMP"; }
on_exit cleanup
failures=0
check() { # check NAME WANT GOT
  if [ "$2" = "$3" ]; then echo "lab lib: $1: pass"; else printf '%s\n' "lab lib: $1: FAIL: want [$2] got [$3]" >&2; failures=$((failures + 1)); fi
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
  status-indented) printf '{\n  "status": "running",\n  "pid": 4242,\n  "uid": 1000\n}\n' ;;
  status-compact) printf '{"status":"running","pid":4242}\n' ;;
  status-nested) printf '{"status":"running","daemon":{"pid":4242}}\n' ;;
  status-fails) echo 'daemon_unreachable: no daemon' >&2; exit 110 ;;
esac
EOF
chmod 755 "$TMP/model"

# The model toolchain, as `go` on PATH and as another name for GO: its
# name, its folder, the two variables, and its arguments on a line of
# MODEL_LOG, then OUT written, or a failure under MODEL_GO=fails.
mkdir "$TMP/gobin"
cat >"$TMP/gobin/go" <<'EOF_GO'
#!/bin/sh
printf '%s cwd=%s CGO_ENABLED=%s GOTOOLCHAIN=%s %s\n' "${0##*/}" "$(pwd)" "${CGO_ENABLED:-}" "${GOTOOLCHAIN:-}" "$*" >>"$MODEL_LOG"
[ "${MODEL_GO:-}" != fails ] || { echo "model build failed" >&2; exit 1; }
while [ "$#" -gt 0 ]; do [ "$1" != -o ] || : >"$2"; shift; done
EOF_GO
chmod 755 "$TMP/gobin/go"
cp "$TMP/gobin/go" "$TMP/gobin/othergo"

# The model fake: its pid and arguments into MODEL_FAKE_LOG, a line on
# stderr, its port on stdout unless MODEL_FAKE=silent, then it waits to be
# stopped (exec, so the pid stays the fake's).
cat >"$TMP/fake" <<'EOF_FAKE'
#!/bin/sh
printf 'pid %s args %s\n' "$$" "$*" >>"$MODEL_FAKE_LOG"
echo "fake: listening" >&2
[ "${MODEL_FAKE:-}" = silent ] || echo 40123
exec sleep 30
EOF_FAKE
chmod 755 "$TMP/fake"

# The caller: sources the libraries under the shell that runs it, then one
# case; lab_stop_tenths 5 (half a second) for the socket that stays.
cat >"$TMP/caller.sh" <<'EOF'
set -eu
. "$1/exit.sh"; . "$1/lab.sh"; . "$1/json.sh"; W=$2; shift 2
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
  pid)
    rc=0; p=$(daemon_pid env HOME="$W/home" MODEL_DO="$2" "$W/../model" --quiet) || rc=$?
    echo "status $rc pid [$p]" ;;
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
  absent)
    printf 'one\n      --address string        Alias for --target\n' >"$W/f"
    case $2 in
      none) absent SECRET "$W/f" ;;
      match) absent 'Alias for' "$W/f" ;;
      dash) absent '--address .*Alias for --target' "$W/f" ;;
      escape) absent 'Alias\b\1' "$W/missing" ;;
      missing) absent SECRET "$W/missing" ;;
    esac
    echo "went on" ;;
  build)
    ROOT=$W/root; mkdir -p "$ROOT" "$W/built"
    PATH=$W/../gobin:$PATH
    case $2 in
      go) unset GO; lab_build "$W/built/fake" ./cmd/karvi-fake-device; echo "status $? built $(ls "$W/built")" ;;
      named) GO=$W/../gobin/othergo; lab_build "$W/built/tool" ./tools/textfile; echo "status $?" ;;
      fails) MODEL_GO=fails; export MODEL_GO; lab_build "$W/built/fake" ./cmd/karvi-fake-device; echo "went on" ;;
      caught) MODEL_GO=fails; export MODEL_GO; rc=0; lab_build "$W/built/fake" ./cmd/karvi-fake-device || rc=$?; echo "caught $rc" ;;
    esac ;;
  fakestart)
    mkdir -p "$W/bin"; cp "$W/../fake" "$W/bin/fake"
    export MODEL_FAKE_LOG=$W/fake.log
    lab_start_tenths=3
    FAKE_PID=; PORT=
    if [ "$2" = silent ]; then MODEL_FAKE=silent; export MODEL_FAKE; fi
    rc=0; fake_start "$W" "$W/fake.err" -port 0 -slow 3s || rc=$?
    logged_pid=$(sed -n 's/^pid \([0-9]*\) .*/\1/p' "$W/fake.log")
    if [ -n "$FAKE_PID" ] && [ "$FAKE_PID" = "$logged_pid" ] && kill -0 "$FAKE_PID" 2>/dev/null; then own=own; else own="not own [$FAKE_PID] [$logged_pid]"; fi
    echo "status $rc port [$PORT] pid $own $(sed -n 's/^pid [0-9]* args //p' "$W/fake.log") err $(cat "$W/fake.err")"
    fake_stop FAKE_PID
    if kill -0 "$logged_pid" 2>/dev/null; then echo "fake running"; else echo "fake stopped"; fi ;;
  newest)
    case $2 in
      order)
        B="$W/the base"
        mkdir -p "$B/jobs/261106/261106-235959-00" "$B/jobs/261107/261107-000001-00" "$B/jobs/261107/261107-000001-01"
        : >"$B/jobs/261107/zz-a-file"
        j=$(newest_job "$B"); echo "status $? ${j#"$W"/}" ;;
      none)
        mkdir -p "$W/empty/jobs"
        rc=0; j=$(newest_job "$W/empty") || rc=$?; echo "status $rc [$j]" ;;
      stops)
        j=$(newest_job "$W/missing"); echo "went on $j" ;;
    esac ;;
  places)
    case $2 in
      ok) lab_places "$W/places.toml" "$W/the base"; echo "status $? mode $(stat -c %a "$W/places.toml")" ;;
      relative) rc=0; lab_places "$W/places.toml" base || rc=$?; echo "status $rc files $(ls "$W" | grep -c places || :)" ;;
      quote) rc=0; lab_places "$W/places.toml" "$W/a\"b" || rc=$?; echo "status $rc files $(ls "$W" | grep -c places || :)" ;;
      backslash) rc=0; lab_places "$W/places.toml" "$W/a\\b" || rc=$?; echo "status $rc files $(ls "$W" | grep -c places || :)" ;;
      replace) umask 022; : >"$W/places.toml"; chmod 644 "$W/places.toml"; lab_places "$W/places.toml" "$W/b"; echo "status $? mode $(stat -c %a "$W/places.toml") $(ls "$W" | tr '\n' ' ')" ;;
    esac ;;
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
# places_read FILE: FILE parsed as TOML, as sorted JSON; places_want BASE:
# what lab_places writes for BASE, the same way.
places_read() { python3 -I -c 'import json, sys, tomllib; print(json.dumps(tomllib.load(open(sys.argv[1], "rb")), sort_keys=True))' "$1" 2>&1; }
places_want() {
  python3 -I -c 'import json, sys; b = sys.argv[1]; print(json.dumps({"basedir": b, "sharedroot": "none", "spooldir": b + "/spool", "scoreboards": b + "/scoreboards", "audit": {"journald-required": False, "file": b + "/audit.jsonl"}, "sessions": {"shared-capacity-root": b + "/capacity"}, "output": {"min-free-bytes-after-job": 0}}, sort_keys=True))' "$1"
}

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
  fresh nosocket
  check "$sh absent, no match: returns" "0 went on|" "$(lab "$sh" absent none)|$(cat "$LAB_W/err")"
  fresh nosocket
  check "$sh absent, a match: the script ends" "1 |$TMP/caller.sh: W/f must not match: Alias for" "$(lab "$sh" absent match)|$(sed "s|$LAB_W|W|" "$LAB_W/err")"
  fresh nosocket
  check "$sh absent, a pattern that begins with --" "1 |$TMP/caller.sh: W/f must not match: --address .*Alias for --target" "$(lab "$sh" absent dash)|$(sed "s|$LAB_W|W|" "$LAB_W/err")"
  fresh nosocket
  check "$sh absent, a pattern with backslashes named as written" "1 |$TMP/caller.sh: cannot read W/missing, which must not match: Alias\\b\\1" "$(lab "$sh" absent escape)|$(sed "s|$LAB_W|W|" "$LAB_W/err")"
  fresh nosocket
  check "$sh absent, a file that cannot be read: the script ends" "1 |$TMP/caller.sh: cannot read W/missing, which must not match: SECRET" "$(lab "$sh" absent missing)|$(sed "s|$LAB_W|W|" "$LAB_W/err")"
  fresh nosocket
  check "$sh daemon_pid, an indented status" "0 status 0 pid [4242]|HOME=W/home --quiet daemon status --format json|" "$(lab "$sh" pid status-indented)|$(logged)"
  fresh nosocket
  check "$sh daemon_pid, a compact status" "0 status 0 pid [4242]" "$(lab "$sh" pid status-compact)"
  fresh nosocket
  check "$sh daemon_pid, a pid only inside another object: none" "0 status 0 pid []" "$(lab "$sh" pid status-nested)"
  fresh nosocket
  check "$sh daemon_pid, no daemon answers: none, status 0" "0 status 0 pid []|" "$(lab "$sh" pid status-fails)|$(cat "$LAB_W/err")"
  fresh nosocket
  check "$sh lab_build: the release's form, from the tree, go on PATH" "0 status 0 built fake|go cwd=W/root CGO_ENABLED=0 GOTOOLCHAIN=local build -mod=vendor -trimpath -o W/built/fake ./cmd/karvi-fake-device|" "$(lab "$sh" build go)|$(logged)"
  fresh nosocket
  check "$sh lab_build: GO names the toolchain" "0 status 0|othergo cwd=W/root CGO_ENABLED=0 GOTOOLCHAIN=local build -mod=vendor -trimpath -o W/built/tool ./tools/textfile|" "$(lab "$sh" build named)|$(logged)"
  fresh nosocket
  check "$sh lab_build: a failed build stops the script under set -e" "1 |model build failed" "$(lab "$sh" build fails)|$(cat "$LAB_W/err")"
  fresh nosocket
  check "$sh lab_build: its status to the caller" "0 caught 1" "$(lab "$sh" build caught)"
  fresh nosocket
  check "$sh fake_start: the port, the fake's own pid, its flags" "0 status 0 port [40123] pid own -host-key-file W/hostkey -port 0 -slow 3s err fake: listening fake stopped" "$(lab "$sh" fakestart ok | sed "s|$LAB_W|W|g")"
  check "$sh fake_start: nothing on stderr" "" "$(cat "$LAB_W/err")"
  fresh nosocket
  check "$sh fake_start: no port, status 1" "0 status 1 port [] pid own -host-key-file W/hostkey -port 0 -slow 3s err fake: listening fake stopped" "$(lab "$sh" fakestart silent | sed "s|$LAB_W|W|g")"
  check "$sh fake_start: its line names the fake's stderr" "$TMP/caller.sh: the fake did not start: fake: listening" "$(cat "$LAB_W/err")"
  fresh nosocket
  check "$sh newest_job: the last day's last job, a file skipped, a space kept" "0 status 0 the base/jobs/261107/261107-000001-01" "$(lab "$sh" newest order)"
  fresh nosocket
  check "$sh newest_job: none, status 1, its line" "0 status 1 []|$TMP/caller.sh: no job folder under W/empty/jobs" "$(lab "$sh" newest none)|$(sed "s|$LAB_W|W|" "$LAB_W/err")"
  fresh nosocket
  check "$sh newest_job: none stops the script under set -e" "1 " "$(lab "$sh" newest stops)"
  fresh nosocket
  check "$sh lab_places: written, 0600" "0 status 0 mode 600" "$(lab "$sh" places ok)"
  check "$sh lab_places: the places, read as TOML" "$(places_want "$LAB_W/the base")" "$(places_read "$LAB_W/places.toml")"
  fresh nosocket
  check "$sh lab_places: a relative path refused, nothing written" "0 status 2 files 0|lab_places: not an absolute path: base" "$(lab "$sh" places relative)|$(cat "$LAB_W/err")"
  fresh nosocket
  check "$sh lab_places: a quote refused, nothing written" "0 status 2 files 0|lab_places: a path TOML would need escaped: W/a\"b" "$(lab "$sh" places quote)|$(sed "s|$LAB_W|W|" "$LAB_W/err")"
  fresh nosocket
  check "$sh lab_places: a backslash refused, nothing written" "0 status 2 files 0|lab_places: a path TOML would need escaped: W/a\\b" "$(lab "$sh" places backslash)|$(sed "s|$LAB_W|W|" "$LAB_W/err")"
  fresh nosocket
  check "$sh lab_places: an existing file replaced at 0600, no temporary left" "0 status 0 mode 600 err out places.toml " "$(lab "$sh" places replace)"
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

# Over the tree: a daemon's pid is read through daemon_pid alone.
pid_reads=$(cd "$ROOT" && grep -nE '^[^#]*daemon status[^|]*\|.*\bpid\b' scripts/*.sh scripts/lib/*.sh |
  grep -vE '^scripts/lib/lab(-test)?\.sh:' || true)
check "no daemon pid read but through daemon_pid" "" "$pid_reads"

# Over the tree: absent is lab.sh's alone, and every must-not-appear line
# can stop its script: a command that begins with `!` (a negated pipeline,
# which set -e ignores) is a condition of if/while/until or ends in `||`.
SHELLS="scripts/*.sh scripts/lib/*.sh packaging/sbin/karvi-sysutil packaging/cron/karvi-crun packaging/cron/karvi-prune packaging/debian/karvi.postinst packaging/debian/karvi.postrm packaging/crun/*.example examples/karvi-credh"
# shellcheck disable=SC2086
copies=$(cd "$ROOT" && grep -n '^absent() *{' $SHELLS | grep -v '^scripts/lib/lab\.sh:' || true)
check "absent defined in lab.sh alone" "" "$copies"
# shellcheck disable=SC2086
bare=$(cd "$ROOT" && grep -nE '(^|;|&&|\bthen|\bdo|\belse)[[:space:]]*![[:space:]]' $SHELLS |
  grep -vE '^[^:]+:[0-9]+:[[:space:]]*#' | grep -v '||' || true)
check "no bare negated pipeline" "" "$bare"

# Over the tree: the suites and labs build through lab_build, start the fake
# through fake_start, find their newest job through newest_job, and name a
# place key through lab_places alone, but on a line whose comment says "the
# row's own" (a row whose subject is another root or another floor).
# shellcheck disable=SC2016 # the patterns name shell text
builds=$(cd "$ROOT" && grep -nE '^[^#]*(\bgo|\$GO"|\$\{GO:-go\}") build\b' scripts/*.sh || true)
check "no build but through lab_build" "" "$builds"
starts=$(cd "$ROOT" && grep -nE '^[^#]*bin/fake"[[:space:]]+-' scripts/*.sh || true)
check "no fake started but through fake_start" "" "$starts"
# shellcheck disable=SC2016
lookups=$(cd "$ROOT" && grep -nE '^[^#]*jobs"?/\*/\*.*(tail|do :; done)' scripts/*.sh || true)
check "no newest job found but through newest_job" "" "$lookups"
# shellcheck disable=SC2016
keys=$(cd "$ROOT" && grep -nE '(^|[^A-Za-z0-9_-])(basedir|sharedroot|spooldir|scoreboards|shared-capacity-root|min-free-bytes-after-job|audit\.file) *=|^[[:space:]]*file *= *"[^"]*audit\.jsonl"|KARVI__(BASEDIR|SHAREDROOT|SPOOLDIR|SCOREBOARDS|SESSIONS__SHARED_CAPACITY_ROOT|AUDIT__FILE|OUTPUT__MIN_FREE_BYTES_AFTER_JOB)=' scripts/*.sh |
  grep -vE '^[^:]+:[0-9]+:[[:space:]]*#' | grep -v "# the row's own" || true)
check "no place key but through lab_places" "" "$keys"

[ "$failures" -eq 0 ] || { echo "lab lib: $failures failed" >&2; exit 1; }
echo "lab lib: pass"
