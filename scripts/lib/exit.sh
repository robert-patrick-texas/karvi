#!/bin/sh
# The scripts' cleanup at their exit, in one place. Source it:
#
#   . "$ROOT/scripts/lib/exit.sh"; on_exit cleanup
#
# on_exit CLEANUP sets CLEANUP (a command, as trap takes it) to run once when
# the script ends: at its end, at an exit or a failure under set -e, and when
# HUP, INT (a Ctrl-C), or TERM ends it, the signal then ending the script with
# 129, 130, or 143. The script's status is its own, whatever CLEANUP's last
# command returns.
#
# Why not `trap CLEANUP EXIT HUP INT TERM`: a trap on a signal replaces the
# signal's end, so the script runs CLEANUP at the signal and goes on, under
# dash and bash alike, and runs it again at its exit. Why not EXIT alone:
# dash runs nothing when a signal ends the script (bash does). Here each
# signal's trap is an exit, which runs CLEANUP from EXIT, once.
#
# A signal sent to the script while it waits on a command takes effect when
# the command ends; a Ctrl-C reaches the command as well, ending both.
#
# signal_exits sets the three signals' traps alone, as on_exit sets them: a
# function that sets traps of its own for a while (run_suites) calls it to
# put them back.
#
# terminal_job COMMAND... starts COMMAND in the background as a terminal
# starts a job: in a session, and so a process group, of its own, with INT at
# its default; $! is its pid and its group's id. An asynchronous command of a
# script starts with INT ignored, which no trap can undo, so the tests of a
# Ctrl-C start their models this way.
# POSIX sh; scripts/lib/exit-test.sh is its check.

on_exit() {
  # shellcheck disable=SC2064 # CLEANUP is the trap's text, as given.
  trap "$1" EXIT
  signal_exits
}

signal_exits() {
  trap 'exit 129' HUP
  trap 'exit 130' INT
  trap 'exit 143' TERM
}

terminal_job() {
  setsid env --default-signal=INT "$@" &
}
