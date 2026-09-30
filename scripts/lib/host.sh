#!/bin/sh
# The host's shared places, for the verifiers' look that the Go tests and
# the suites left them as they were: the shared scoreboard directory and
# the shared directory under each of the two roots (the three trees inside
# it).
# Source this file; POSIX sh, no local.
#
#   before=$(host_shared_entries)
#   ... the tests and the suites ...
#   host_shared_unchanged "$before"
#
# And the operator's own trust store: a script that
# sets no ssh.known-hosts-file enrols into the operator's real store (the
# policy resolves the home from the passwd entry, not $HOME), so every
# script calls host_trust_store with its work directory as soon as it has
# one.

# host_shared_entries prints one line per entry under the host's shared
# places (a place that does not exist contributes nothing), sorted, so two
# calls compare as text. The loop ends with status 0 whatever exists: an
# `[ -d ] && find` list left the loop, and the pipeline under pipefail,
# at 1 when the last place was absent (the v0.16.0 baseline's second run).
host_shared_entries() {
  for hse_dir in /dev/shm/karvi/scoreboards \
      /opt/karvi/shared /var/lib/karvi/shared; do
    if [ -d "$hse_dir" ]; then find "$hse_dir" -mindepth 1 2>/dev/null; fi
  done | sort
}

# host_shared_unchanged BEFORE fails, naming the entries that appeared or
# went, when the host's shared places differ from BEFORE (the output of an
# earlier host_shared_entries).
host_shared_unchanged() {
  hsu_after=$(host_shared_entries)
  [ "$hsu_after" = "$1" ] && return 0
  echo "the host's shared places changed during the run (a test or a suite wrote there):" >&2
  printf '%s\n' "$1" > "${TMPDIR:-/tmp}/karvi-host-before.$$"
  printf '%s\n' "$hsu_after" | diff "${TMPDIR:-/tmp}/karvi-host-before.$$" - >&2 || true
  rm -f "${TMPDIR:-/tmp}/karvi-host-before.$$"
  return 1
}

# host_trust_store DIR points the trust store under DIR through the
# environment layer, which the daemon a client launches inherits with every
# KARVI__ variable. A flag or a --set outranks the environment; a setting in
# a configuration file does not, so a script that places its store per row
# in the file it writes (the native and spool suites) does not call this.
# The store is created by the accept-new policy at its first enrollment,
# 0600 in DIR, which the policy accepts as the operator's own directory.
host_trust_store() {
  KARVI__SSH__KNOWN_HOSTS_FILE=$1/known_hosts
  export KARVI__SSH__KNOWN_HOSTS_FILE
}
