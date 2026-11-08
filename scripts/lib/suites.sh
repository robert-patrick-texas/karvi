#!/bin/sh
# The executable suites, in one place: both
# verifiers run the same list against bin/ as it stands, so the list cannot
# drift between them. Source it after scripts/lib/exit.sh (the caller's
# cleanup set through on_exit), then call run_suites from the tree's root;
# run_suites refuses a caller that has not sourced exit.sh, before anything
# runs, where it would fail at the lanes' end.
# The libraries' own tests go first: the suites assert through the JSON
# reader, clean up through on_exit, stop their daemons and fakes and assert
# what must not appear through lab.sh, and compare the home's places through
# host.sh, and run_suites's lanes are the subject of suites-test.sh, which
# runs them over model suites. Then a host whose configuration reaches karvi
# ($KARVI, or bin/'s) is refused once, before the lanes start
# (host_config_clean in host.sh), rather than by each suite in its lane.
#
# The suites run in three lanes at once: the parity suite alone, the v0100
# suite alone, and the seventeen others in order. Each suite keeps its
# own work directory, socket, scoreboard directory, capacity root, and
# ephemeral ports, so the lanes share nothing but the CPU; the parity suite
# and the smoke suite mostly wait on timeouts. A lane's output goes to a file
# and is printed whole when every lane has ended, lane by lane, so a log reads
# as it did. KARVI_SUITE_LANES=1 runs the same list one suite after another,
# the output live, for a machine or a fault that wants it.
#
# Each lane runs in a session, and so a process group, of its own (setsid),
# its pid the group's id: an asynchronous command of a script starts with INT
# ignored, which no trap undoes, so a Ctrl-C would end the caller and leave
# the lanes running. While they run, HUP, INT, and TERM stop them (rs_stop):
# TERM to each lane's group, so each suite runs its cleanup; up to 30 s for
# the groups to empty, a second signal meanwhile ignored; the lanes' output
# printed, the lane folder removed, and the caller ended with 129, 130, or
# 143, its own cleanup run. Once the lanes have ended, the caller's signal
# traps are on_exit's again (signal_exits).
#
# The canary suite needs bin/secret-scan (make tools-build); the parity suite
# builds the fake device from the tree; the
# completion suite reads the bash function's constant from the source tree;
# the audit suite reads the host's journal, its rows skipped on a host
# without journald.
# POSIX sh: no local; the functions' variables carry the rs_ prefix.
RS_LANE1='native-smoke-test'
RS_LANE2='v0100-smoke-test'
RS_LANE3='smoke-test halt-smoke-test hostkey-smoke-test transcript-smoke-test display-smoke-test v060-smoke-test v061-smoke-test v070-smoke-test v080-smoke-test v090-smoke-test k03-smoke-test completion-smoke-test daemon-upgrade-smoke-test canary-smoke-test spool-smoke-test job-list-smoke-test audit-smoke-test'

# rs_run SUITE...: the suites in order, stopping at the first failure.
rs_run() {
  for rs_suite in "$@"; do
    "./scripts/$rs_suite.sh" || return 1
  done
}

# rs_lane DIR N SUITE...: rs_run into DIR/N.out, its status into DIR/N.status.
rs_lane() {
  rs_dir=$1; rs_n=$2; shift 2
  rs_run "$@" >"$rs_dir/$rs_n.out" 2>&1
  echo $? >"$rs_dir/$rs_n.status"
}

# rs_start N SUITE...: lane N in a session of its own, its pid (its group's
# id) added to rs_groups. The lane's shell sources this file from the tree's
# root, as run_suites runs.
rs_start() {
  setsid sh -c '. ./scripts/lib/suites.sh && rs_lane "$@"' sh "$rs_out" "$@" &
  rs_groups="$rs_groups $!"
}

# rs_stop STATUS: the lanes stopped at a signal, the caller ended with
# STATUS. A lane not yet in its own group takes the TERM by its pid.
rs_stop() {
  trap '' HUP INT TERM
  for rs_pg in $rs_groups; do
    kill -TERM "-$rs_pg" 2>/dev/null || kill -TERM "$rs_pg" 2>/dev/null || :
  done
  rs_i=0
  for rs_pg in $rs_groups; do
    while kill -0 "-$rs_pg" 2>/dev/null && [ "$rs_i" -lt 300 ]; do sleep 0.1; rs_i=$((rs_i + 1)); done
    kill -0 "-$rs_pg" 2>/dev/null && echo "suite lane group $rs_pg still running after 30 s" >&2
  done
  signal_exits
  if [ -n "$rs_out" ]; then
    for rs_n in 1 2 3; do cat "$rs_out/$rs_n.out" 2>/dev/null || :; done
    rm -rf "$rs_out"
  fi
  exit "$1"
}

run_suites() {
  command -v signal_exits >/dev/null 2>&1 \
    || { printf '%s\n' 'run_suites: source scripts/lib/exit.sh before scripts/lib/suites.sh' >&2; return 1; }
  ./scripts/lib/json-test.sh >/dev/null || return 1
  ./scripts/lib/exit-test.sh >/dev/null || return 1
  ./scripts/lib/lab-test.sh >/dev/null || return 1
  ./scripts/lib/rows-test.sh >/dev/null || return 1
  ./scripts/lib/host-test.sh >/dev/null || return 1
  ./scripts/lib/suites-test.sh >/dev/null || return 1
  . ./scripts/lib/json.sh; . ./scripts/lib/host.sh
  host_config_clean "${KARVI:-./bin/karvi-linux-amd64}" || return 1
  if [ "${KARVI_SUITE_LANES:-3}" = 1 ]; then
    # shellcheck disable=SC2086
    rs_run $RS_LANE1 $RS_LANE2 $RS_LANE3
    return $?
  fi
  rs_out=
  rs_groups=
  trap 'rs_stop 129' HUP
  trap 'rs_stop 130' INT
  trap 'rs_stop 143' TERM
  rs_out=$(mktemp -d)
  # shellcheck disable=SC2086
  rs_start 1 $RS_LANE1
  # shellcheck disable=SC2086
  rs_start 2 $RS_LANE2
  # shellcheck disable=SC2086
  rs_start 3 $RS_LANE3
  wait
  signal_exits
  rs_failed=0
  for rs_n in 1 2 3; do
    cat "$rs_out/$rs_n.out"
    [ "$(cat "$rs_out/$rs_n.status" 2>/dev/null)" = 0 ] || { echo "suite lane $rs_n failed" >&2; rs_failed=1; }
  done
  rm -rf "$rs_out"
  return $rs_failed
}
