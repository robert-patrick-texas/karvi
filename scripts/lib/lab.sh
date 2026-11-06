#!/bin/sh
# The labs' stops, in one place: the suites and the labs that start a daemon
# or a fake device stop them here, in their rows and in their cleanup. Source
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
# fake_stop VAR stops the process whose pid the variable VAR holds (TERM,
# then its wait, as a child of the script) and empties VAR, so a later stop
# never reaches a process that took the number since; nothing when VAR is
# empty.
#
# lab_stop_tenths, when set, bounds the socket's wait in tenths of a second
# (150); scripts/lib/lab-test.sh, the check, shortens it.
# POSIX sh: no local; the functions' variables carry the ds_ and fs_ prefixes.

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
