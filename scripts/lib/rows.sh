#!/bin/sh
# The counted-failure row runner, in one place: a suite whose rows each run
# one command and check its exit, then go on to the next row whatever
# failed, counting the failures and ending on the count. A suite of rows
# sets row_dir, the folder for the rows' streams, and sources it; a suite of
# checks alone (fail and finish) needs no row_dir:
#
#   row_dir=$TMP/streams; . "$ROOT/scripts/lib/rows.sh"
#
# row NAME WANT -- COMMAND... runs COMMAND (the executable and its
# arguments, or a function of the suite) with stdin from /dev/null, its
# stdout into row_dir/NAME.out and its stderr into row_dir/NAME.err, and
# counts the row; an exit other than WANT is a failure naming the exit and
# stderr's first line. It prints "NAME exit N" and sets row_out and row_err
# to the row's two streams, which the row's checks read. Its status is 0
# whatever COMMAND's exit, so the suite goes on; 2, nothing run or counted,
# without the `--` or with row_dir unset.
#
# row_open NAME counts a row the suite runs itself (a command in the
# background, a signal) and sets row_out and row_err for NAME, as row does.
#
# fail MESSAGE... prints "FAIL: MESSAGE" on stderr and counts it; the suite
# goes on.
#
# finish LABEL [DETAIL] ends the suite: "LABEL: N failure(s) over R rows" on
# stderr and exit 1 when a row or a check failed; "LABEL: pass (R rows,
# DETAIL)" and status 0 otherwise. A suite of checks alone (no row) uses
# fail and finish too: its lines name no rows ("LABEL: N failure(s)",
# "LABEL: pass (DETAIL)", "LABEL: pass").
#
# A suite sourcing this file defines no fail of its own (the suites whose
# fail ends the script at the first failure source lab.sh alone).
# POSIX sh: no local; the functions' variables carry the rw_ prefix, the
# state row_count, row_failures, row_out, and row_err.

row_count=0
row_failures=0
row_out=
row_err=

fail() {
  printf '%s\n' "FAIL: $*" >&2
  row_failures=$((row_failures + 1))
}

row_open() {
  if [ -z "${row_dir:-}" ]; then
    echo "row: row_dir names no folder for the rows' streams" >&2
    return 2
  fi
  row_count=$((row_count + 1))
  row_out=$row_dir/$1.out
  row_err=$row_dir/$1.err
}

row() {
  if [ "$#" -lt 4 ] || [ "$3" != -- ]; then
    echo "row: usage: row NAME WANT -- COMMAND..." >&2
    return 2
  fi
  rw_name=$1; rw_want=$2; shift 3
  row_open "$rw_name" || return 2
  rw_rc=0
  "$@" >"$row_out" 2>"$row_err" </dev/null || rw_rc=$?
  [ "$rw_rc" -eq "$rw_want" ] || fail "$rw_name: exit $rw_rc, want $rw_want; stderr: $(head -1 "$row_err")"
  printf '%-4s exit %d\n' "$rw_name" "$rw_rc"
}

finish() {
  rw_rows=
  [ "$row_count" -eq 0 ] || rw_rows="$row_count rows"
  if [ "$row_failures" -ne 0 ]; then
    printf '%s\n' "$1: $row_failures failure(s)${rw_rows:+ over $rw_rows}" >&2
    exit 1
  fi
  rw_detail=$rw_rows${2:+${rw_rows:+, }$2}
  printf '%s\n' "$1: pass${rw_detail:+ ($rw_detail)}"
}
