#!/bin/sh
# The check of scripts/lib/suites.sh's lanes: a model verifier under sh and
# bash runs run_suites over four model suites in its three lanes (a; b then
# c; d), in a scratch root whose library tests pass at once. At their end:
# each suite's output, lane by lane, status 0, and the verifier's signal
# traps as on_exit set them; a failing suite: its lane named, the lane's next
# suite not run, the run failed. A signal while the lanes run (HUP or INT to
# the verifier's group, as a terminal's hangup or Ctrl-C sends it; TERM to
# the verifier alone, as timeout(1) does): each running suite's cleanup run,
# no lane's process left, the output printed, the lane folder removed, and
# the verifier ended with 129, 130, or 143, its cleanup run once. A second
# Ctrl-C while the suites' cleanups run: ignored, each cleanup completed. A
# host whose configuration reaches karvi (a model karvi naming a file):
# refused, the file named, no suite run. Every run's environment is the
# row's own (env -i), never the operator's KARVI__ variables.
set -eu
ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/../.." && pwd)
. "$ROOT/scripts/lib/exit.sh"
TMP=$(mktemp -d)
# A model suite ends once its row's folder is gone, so the removal ends any
# the test leaves running.
cleanup() { rm -rf "$TMP"; }
on_exit cleanup
failures=0
check() { # check NAME WANT GOT
  if [ "$2" = "$3" ]; then echo "suites lib: $1: pass"; else echo "suites lib: $1: FAIL: want [$2] got [$3]" >&2; failures=$((failures + 1)); fi
}

# The scratch root: the library under test, exit.sh, json.sh and host.sh, a
# passing stub for each library test run_suites runs first, a model karvi
# whose configuration names CONFIGURED's file or none, the model suites, and
# the verifier.
mkdir -p "$TMP/root/scripts/lib" "$TMP/root/bin"
ln -s "$ROOT/scripts/lib/suites.sh" "$ROOT/scripts/lib/exit.sh" "$ROOT/scripts/lib/json.sh" "$ROOT/scripts/lib/host.sh" "$TMP/root/scripts/lib/"
cat >"$TMP/root/bin/karvi-linux-amd64" <<'EOF'
#!/bin/sh
if [ -n "${CONFIGURED:-}" ]; then printf '{"sources":["%s"]}\n' "$CONFIGURED"; else printf '{"sources":null}\n'; fi
EOF
chmod +x "$TMP/root/bin/karvi-linux-amd64"
for t in "$ROOT"/scripts/lib/*-test.sh; do
  printf '#!/bin/sh\n' >"$TMP/root/scripts/lib/${t##*/}"
  chmod +x "$TMP/root/scripts/lib/${t##*/}"
done
# The model suite: its output, its group's id and a mark that it runs, then
# a wait until the row's stop file appears (at most 20 s), or a failure when
# the row names it. Under SLOW its cleanup marks its start and takes 0.5 s.
cat >"$TMP/model-suite.sh" <<'EOF'
#!/bin/sh
. ./scripts/lib/exit.sh
NAME=$(basename "$0" .sh)
cleanup() {
  if [ -n "$SLOW" ]; then echo "$NAME cleanup start" >>"$STATE/marks"; sleep 0.5; fi
  echo "$NAME cleanup" >>"$STATE/marks"
}
on_exit cleanup
echo "$NAME output"
ps -o pgid= -p $$ | tr -d ' ' >"$STATE/$NAME.group"
: >"$STATE/$NAME.ready"
[ "$FAIL" != "$NAME" ] || exit 1
i=0
while [ -d "$STATE" ] && [ ! -e "$STATE/stop" ] && [ "$i" -lt 400 ]; do sleep 0.05; i=$((i + 1)); done
echo "$NAME end" >>"$STATE/marks"
EOF
chmod +x "$TMP/model-suite.sh"
for m in a b c d; do ln -s "$TMP/model-suite.sh" "$TMP/root/scripts/model-$m.sh"; done
# The verifier, as verify-release.sh and verify-bundle.sh call run_suites;
# its traps written before and after.
cat >"$TMP/root/verifier.sh" <<'EOF'
set -eu
. ./scripts/lib/exit.sh
cleanup() { echo "verifier cleanup" >>"$STATE/marks"; }
on_exit cleanup
trap >"$STATE/traps.before"
. ./scripts/lib/suites.sh
RS_LANE1=model-a
RS_LANE2='model-b model-c'
RS_LANE3=model-d
run_suites
trap >"$STATE/traps.after"
echo "verifier passed" >>"$STATE/marks"
EOF
cd "$TMP/root"

# run SHELL FAIL [SIGNAL TARGET [again]]: one run of the verifier, its
# suites' files in the row's folder D. The verifier starts as a terminal's
# job; TARGET "group" signals its group, "verifier" the verifier alone, once
# every suite that runs at the start (a, b, d; a, d when b fails) has marked
# it. "again": the suites' cleanups slow, and the same signal sent a second
# time once all three have started. Without a signal the stop file ends the
# suites at once. After the verifier ends:
# the groups of the suites still running in D/left, then the stop file, and
# the test waits (at most 10 s) for those to end.
run() {
  D=$(mktemp -d "$TMP/row.XXXXXX")
  mkdir "$D/tmp"
  : >"$D/marks"
  slow=
  [ "${5:-}" != again ] || slow=1
  terminal_job env -i PATH="$PATH" TMPDIR="$D/tmp" STATE="$D" FAIL="$2" SLOW="$slow" "$1" ./verifier.sh >"$D/out" 2>"$D/err"
  pid=$!
  first='a b d'
  [ "$2" != model-b ] || first='a d'
  i=0
  for m in $first; do
    while [ ! -e "$D/model-$m.ready" ] && [ "$i" -lt 400 ]; do sleep 0.05; i=$((i + 1)); done
  done
  if [ $# -ge 4 ]; then
    signal "$3" "$4"
    if [ -n "$slow" ]; then
      i=0
      while [ "$(grep -c ' cleanup start$' "$D/marks")" -lt 3 ] && [ "$i" -lt 400 ]; do sleep 0.02; i=$((i + 1)); done
      signal "$3" "$4"
    fi
  else
    : >"$D/stop"
  fi
  status=0
  wait "$pid" || status=$?
  : >"$D/left"
  for g in "$D"/*.group; do
    [ -e "$g" ] || continue
    if kill -0 "-$(cat "$g")" 2>/dev/null; then basename "$g" .group >>"$D/left"; fi
  done
  : >"$D/stop"
  i=0
  while [ -s "$D/left" ] && [ "$i" -lt 200 ]; do
    still=
    while read -r m; do kill -0 "-$(cat "$D/$m.group")" 2>/dev/null && still=1; done <"$D/left"
    [ -n "$still" ] || break
    sleep 0.05; i=$((i + 1))
  done
}
signal() { # signal SIGNAL TARGET: to the verifier's group or the verifier
  if [ "$2" = group ]; then kill -s "$1" -- "-$pid"; else kill -s "$1" "$pid"; fi
}
lines() { # lines FILE: its lines on one, sorted when SORT is set
  if [ -n "${SORT:-}" ]; then sort "$1"; else cat "$1"; fi | tr '\n' ' ' | sed 's/ $//'
}
# The suites' own lines of the verifier's output: a lane's file also holds
# dash's "Terminated" when the TERM ends a suite's command, as it may or may
# not.
suite_lines() {
  grep -x 'model-[a-d] output' "$1" | tr '\n' ' ' | sed 's/ $//'
}

stopped='model-a cleanup model-b cleanup model-d cleanup verifier cleanup'
for sh in sh bash; do
  run "$sh" none
  check "$sh their end: status" 0 "$status"
  check "$sh their end: output lane by lane" 'model-a output model-b output model-c output model-d output' "$(lines "$D/out")"
  check "$sh their end: marks" 'model-a cleanup model-a end model-b cleanup model-b end model-c cleanup model-c end model-d cleanup model-d end verifier cleanup verifier passed' "$(SORT=1 lines "$D/marks")"
  check "$sh their end: the traps after as before" "$(cat "$D/traps.before")" "$(cat "$D/traps.after" 2>/dev/null)"
  check "$sh their end: no lane folder" '' "$(ls -A "$D/tmp")"

  run "$sh" model-b
  check "$sh a failing suite: status" 1 "$status"
  check "$sh a failing suite: its lane named" 'suite lane 2 failed' "$(lines "$D/err")"
  check "$sh a failing suite: output" 'model-a output model-b output model-d output' "$(lines "$D/out")"
  check "$sh a failing suite: marks, model-c not run" 'model-a cleanup model-a end model-b cleanup model-d cleanup model-d end verifier cleanup' "$(SORT=1 lines "$D/marks")"
  check "$sh a failing suite: no lane folder" '' "$(ls -A "$D/tmp")"

  for s in 'HUP group 129' 'INT group 130' 'TERM verifier 143'; do
    # shellcheck disable=SC2086 # the row's three words
    set -- $s
    name="$sh $1 to the $2"
    run "$sh" none "$1" "$2"
    check "$name: status" "$3" "$status"
    check "$name: each running suite's cleanup, the verifier's once" "$stopped" "$(SORT=1 lines "$D/marks")"
    check "$name: no lane left" '' "$(lines "$D/left")"
    check "$name: output printed" 'model-a output model-b output model-d output' "$(suite_lines "$D/out")"
    check "$name: no lane folder" '' "$(ls -A "$D/tmp")"
  done

  D=$(mktemp -d "$TMP/row.XXXXXX")
  mkdir "$D/tmp"
  : >"$D/marks"
  status=0
  env -i PATH="$PATH" TMPDIR="$D/tmp" STATE="$D" FAIL=none SLOW= CONFIGURED=/etc/karvi/config.toml "$sh" ./verifier.sh >"$D/out" 2>"$D/err" || status=$?
  check "$sh a configured host: status" 1 "$status"
  check "$sh a configured host: refused, the file named" "karvi reads this host's configuration; run where it reads none (docs/BUILD-QUALIFICATION.md):   file: /etc/karvi/config.toml" "$(lines "$D/err")"
  check "$sh a configured host: no suite run, the verifier's cleanup" 'verifier cleanup' "$(lines "$D/marks")"
  check "$sh a configured host: no lane folder" '' "$(ls -A "$D/tmp")"

  run "$sh" none INT group again
  check "$sh a second Ctrl-C: status" 130 "$status"
  check "$sh a second Ctrl-C: each cleanup completed" 'model-a cleanup model-a cleanup start model-b cleanup model-b cleanup start model-d cleanup model-d cleanup start verifier cleanup' "$(SORT=1 lines "$D/marks")"
  check "$sh a second Ctrl-C: no lane left" '' "$(lines "$D/left")"
  check "$sh a second Ctrl-C: no lane folder" '' "$(ls -A "$D/tmp")"
done

[ "$failures" -eq 0 ] || { echo "suites lib: $failures failed" >&2; exit 1; }
echo "suites lib: pass"
