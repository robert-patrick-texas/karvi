#!/bin/sh
# The labs' shared functions, in one place: the suites and the labs that
# start a daemon or a fake device stop them here, in their rows and in their
# cleanup, and a suite under set -e asserts what must not appear here. Source
# it:
#
#   . "$ROOT/scripts/lib/lab.sh"
#
# daemon_stop_force SOCKET COMMAND... stops the daemon at SOCKET with
# `COMMAND daemon stop --force`, COMMAND being the executable and its
# arguments as the script runs them (`"$KARVI" --config "$TMP/karvi.toml"`,
# `env HOME="$TMP/home" "$KARVI" ...`, or a function of the script), then
# waits up to 15 s for SOCKET to go. Its status is 0 once SOCKET is gone, or
# when there was none (nothing run); 1 when it stays, after a line on stderr
# naming it. It never exits, so a cleanup goes on to its removals:
#
#   cleanup() { daemon_stop_force "$SOCK" "$KARVI" ... || :; rm -rf "$TMP"; }
#
# and a row that needs the daemon gone says `daemon_stop_force ... || fail ...`.
# --force because a plain stop is refused while a job runs (if_idle), and a
# cleanup also runs after a Ctrl-C in the middle of a row: the daemon would
# outlive its removed folder, drain its job, and write the folder again.
#
# daemon_pid COMMAND... prints the pid of the daemon COMMAND reaches, read
# from `COMMAND daemon status --format json` by json_get (so a script
# sourcing lab.sh for it sources scripts/lib/json.sh too), or nothing when
# none answers. Its status is 0 either way; the caller tests the answer:
#
#   pid=$(daemon_pid env HOME="$HOME_DIR" "$KARVI" ...); [ -n "$pid" ] || fail ...
#
# fake_stop VAR stops the process whose pid the variable VAR holds (TERM,
# then its wait, as a child of the script) and empties VAR, so a later stop
# never reaches a process that took the number since; nothing when VAR is
# empty.
#
# absent PATTERN FILE ends the script, status 1, with a line on stderr, when
# FILE matches PATTERN (grep's basic expression) or cannot be read; it
# returns when FILE is read and does not match. A bare `! grep` line asserts
# nothing, since set -e ignores a pipeline that begins with `!` (POSIX), so
# a suite's must-not-appear check goes through here or says `! ... || fail`.
#
# lab_stop_tenths, when set, bounds the socket's wait in tenths of a second
# (150); scripts/lib/lab-test.sh, the check, shortens it.
# POSIX sh: no local; the functions' variables carry the ds_, fs_, and ab_
# prefixes.

daemon_stop_force() {
  ds_sock=$1; shift
  [ -S "$ds_sock" ] || return 0
  "$@" daemon stop --force >/dev/null 2>&1 || :
  ds_i=0
  while [ -S "$ds_sock" ] && [ "$ds_i" -lt "${lab_stop_tenths:-150}" ]; do sleep 0.1; ds_i=$((ds_i + 1)); done
  [ -S "$ds_sock" ] || return 0
  echo "$0: the daemon at $ds_sock outlived daemon stop --force" >&2
  return 1
}

daemon_pid() {
  "$@" daemon status --format json 2>/dev/null | json_get - pid 2>/dev/null || :
}

fake_stop() {
  case $1 in
    '' | [0-9]* | *[!A-Za-z0-9_]*) echo "fake_stop: not a variable name: $1" >&2; return 2 ;;
  esac
  eval "fs_pid=\${$1:-}"
  [ -n "$fs_pid" ] || return 0
  kill "$fs_pid" 2>/dev/null || :
  wait "$fs_pid" 2>/dev/null || :
  eval "$1="
}

absent() {
  ab_rc=0
  grep -q -- "$1" "$2" 2>/dev/null || ab_rc=$?
  case $ab_rc in
    1) return 0 ;;
    0) echo "$0: $2 must not match: $1" >&2 ;;
    *) echo "$0: cannot read $2, which must not match: $1" >&2 ;;
  esac
  exit 1
}
