#!/bin/sh
# The labs' shared functions, in one place: the suites and the labs build
# their helpers, write their places, and start a fake device here, stop
# their daemon and their fake here, in their rows and in their cleanup, find
# their newest job here, and a suite under set -e asserts what must not
# appear here. Source it:
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
# lab_build OUT PACKAGE builds the tree's PACKAGE (./cmd/... or ./tools/...)
# into OUT as the Makefile builds the release (static, -trimpath, the
# vendored modules, the local toolchain), from ROOT, the tree, with GO, the
# toolchain (go when unset). Its status is the build's, so `lab_build ... ||
# fail ...` and a bare call under set -e both stop on a failed build.
#
# fake_start DIR ERR FLAG... starts the fake device DIR/bin/fake (built by
# lab_build) in the background with its host key at DIR/hostkey and FLAGs,
# its stderr into ERR, and waits up to 5 s for the port it prints into
# DIR/port. It sets FAKE_PID (the fake's own pid, for fake_stop FAKE_PID)
# and PORT. Its status is 0 once the port is read; 1 when none came, after a
# line on stderr naming ERR's first line:
#
#   fake_start "$TMP" "$TMP/fake.err" -port 0 || exit 1
#
# newest_job BASE prints the path of BASE's newest job folder, the last a
# glob of BASE/jobs/*/* sorts (the day folder, then the job's own name, both
# by time). Its status is 0 with a path; 1 when BASE holds none, after a
# line on stderr, so `JOB=$(newest_job "$BASE")` under set -e stops there.
#
# lab_places FILE BASE writes the suite's places into FILE (0600): basedir
# BASE, sharedroot none, the spool, the scoreboards, the capacity root, and
# the audit file under BASE (journald not required), and no floor. Every
# suite passes FILE first, `--config FILE`, and its own file and --set rows
# after: each --config is its own layer, a later one outranks it key by key,
# so a suite's own [output] table never meets a second header, and the
# daemon a client launches inherits the list. A row that needs another root
# writes a second file, passed after the first, or says --set on a line
# whose comment begins "# the row's own" (the row's own basedir, the row's
# own floor), which the tree check in scripts/lib/lab-test.sh reads. Status 2,
# nothing written, for a BASE that TOML would need escaped (a quote, a
# backslash) or that is not absolute.
#
# lab_stop_tenths and lab_start_tenths, when set, bound the socket's wait
# (150) and the fake's port wait (50) in tenths of a second;
# scripts/lib/lab-test.sh, the check, shortens them.
# POSIX sh: no local; the functions' variables carry the ds_, fs_, ab_, fk_,
# nj_, and lp_ prefixes.

daemon_stop_force() {
  ds_sock=$1; shift
  [ -S "$ds_sock" ] || return 0
  "$@" daemon stop --force >/dev/null 2>&1 || :
  ds_i=0
  while [ -S "$ds_sock" ] && [ "$ds_i" -lt "${lab_stop_tenths:-150}" ]; do sleep 0.1; ds_i=$((ds_i + 1)); done
  [ -S "$ds_sock" ] || return 0
  printf '%s\n' "$0: the daemon at $ds_sock outlived daemon stop --force" >&2
  return 1
}

daemon_pid() {
  "$@" daemon status --format json 2>/dev/null | json_get - pid 2>/dev/null || :
}

fake_stop() {
  case $1 in
    '' | [0-9]* | *[!A-Za-z0-9_]*) printf '%s\n' "fake_stop: not a variable name: $1" >&2; return 2 ;;
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
    0) printf '%s\n' "$0: $2 must not match: $1" >&2 ;;
    *) printf '%s\n' "$0: cannot read $2, which must not match: $1" >&2 ;;
  esac
  exit 1
}

lab_build() {
  (cd "$ROOT" && CGO_ENABLED=0 GOTOOLCHAIN=local "${GO:-go}" build -mod=vendor -trimpath -o "$1" "$2")
}

fake_start() {
  fk_dir=$1; fk_err=$2; shift 2
  : >"$fk_dir/port"
  "$fk_dir/bin/fake" -host-key-file "$fk_dir/hostkey" "$@" 2>"$fk_err" >"$fk_dir/port" &
  # shellcheck disable=SC2034 # the caller's: fake_stop FAKE_PID
  FAKE_PID=$!
  fk_i=0
  while [ ! -s "$fk_dir/port" ] && [ "$fk_i" -lt "${lab_start_tenths:-50}" ]; do sleep 0.1; fk_i=$((fk_i + 1)); done
  if [ ! -s "$fk_dir/port" ]; then
    printf '%s\n' "$0: the fake did not start: $(head -1 "$fk_err" 2>/dev/null)" >&2
    return 1
  fi
  # shellcheck disable=SC2034 # the caller's: the fake's port
  PORT=$(cat "$fk_dir/port")
}

newest_job() {
  nj_last=
  for nj_dir in "$1"/jobs/*/*; do
    [ -d "$nj_dir" ] && nj_last=$nj_dir
  done
  if [ -z "$nj_last" ]; then
    printf '%s\n' "$0: no job folder under $1/jobs" >&2
    return 1
  fi
  printf '%s\n' "$nj_last"
}

lab_places() {
  case $2 in
    /*) ;;
    *) printf '%s\n' "lab_places: not an absolute path: $2" >&2; return 2 ;;
  esac
  case $2 in
    *'"'* | *\\*) printf '%s\n' "lab_places: a path TOML would need escaped: $2" >&2; return 2 ;;
  esac
  lp_tmp=$1.lp$$
  (umask 077 && cat >"$lp_tmp" <<EOF_PLACES
basedir = "$2"
sharedroot = "none"
spooldir = "$2/spool"
scoreboards = "$2/scoreboards"
[audit]
journald-required = false
file = "$2/audit.jsonl"
[sessions]
shared-capacity-root = "$2/capacity"
[output]
min-free-bytes-after-job = 0
EOF_PLACES
  ) && mv -f "$lp_tmp" "$1" && return 0
  rm -f "$lp_tmp"
  return 1
}
