#!/bin/sh
# The executable suites, in one place: both
# verifiers run the same list against bin/ as it stands, so the list cannot
# drift between them. Source it, then call run_suites from the tree's root.
# The JSON reader's own test goes first: the suites assert through it.
#
# The suites run in three lanes at once: the parity suite alone, the v0100
# suite alone, and the fifteen others in order. Each suite keeps its
# own work directory, socket, scoreboard directory, capacity root, and
# ephemeral ports, so the lanes share nothing but the CPU; the parity suite
# and the smoke suite mostly wait on timeouts. A lane's output goes to a file
# and is printed whole when every lane has ended, lane by lane, so a log reads
# as it did. KARVI_SUITE_LANES=1 runs the same list one suite after another,
# the output live, for a machine or a fault that wants it.
#
# The canary suite needs bin/secret-scan (make tools-build); the parity suite
# builds the fake device from the tree; the
# completion suite reads the bash function's constant from the source tree.
# POSIX sh: no local; the functions' variables carry the rs_ prefix.
RS_LANE1='native-smoke-test'
RS_LANE2='v0100-smoke-test'
RS_LANE3='smoke-test halt-smoke-test hostkey-smoke-test transcript-smoke-test display-smoke-test v060-smoke-test v061-smoke-test v070-smoke-test v080-smoke-test v090-smoke-test k03-smoke-test completion-smoke-test daemon-upgrade-smoke-test canary-smoke-test spool-smoke-test'

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

run_suites() {
  ./scripts/lib/json-test.sh >/dev/null || return 1
  if [ "${KARVI_SUITE_LANES:-3}" = 1 ]; then
    # shellcheck disable=SC2086
    rs_run $RS_LANE1 $RS_LANE2 $RS_LANE3
    return $?
  fi
  rs_out=$(mktemp -d)
  # shellcheck disable=SC2086
  rs_lane "$rs_out" 1 $RS_LANE1 &
  # shellcheck disable=SC2086
  rs_lane "$rs_out" 2 $RS_LANE2 &
  # shellcheck disable=SC2086
  rs_lane "$rs_out" 3 $RS_LANE3 &
  wait
  rs_failed=0
  for rs_n in 1 2 3; do
    cat "$rs_out/$rs_n.out"
    [ "$(cat "$rs_out/$rs_n.status" 2>/dev/null)" = 0 ] || { echo "suite lane $rs_n failed" >&2; rs_failed=1; }
  done
  rm -rf "$rs_out"
  return $rs_failed
}
