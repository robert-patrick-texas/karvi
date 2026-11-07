#!/bin/sh
# The host's shared places, for the verifiers' look that the Go tests and
# the suites left them as they were: the shared scoreboard directory and
# the shared directory under each of the two roots (the three trees inside
# it); and the scratch's last two automatic candidates, /tmp/karvi-<uid> and
# /var/tmp/karvi-<uid>, by their existence alone, since the operator's own
# daemon keeps its spool in the first: a test whose basedir is too long for
# the askpass socket's bound made it (docs/EXAMPLES.md chapter 44).
# Source this file; POSIX sh, no local.
#
#   before=$(host_shared_entries)
#   ... the tests and the suites ...
#   host_shared_unchanged "$before"
#
# And the operator's own trust store: under ssh.known-hosts-file "auto" the
# store is <basedir>/known_hosts, so a script that sets neither the store nor
# basedir enrols into the operator's real store (basedir's automatic root is
# under the home of the passwd entry, not $HOME); every script calls
# host_trust_store with its work directory as soon as it has one, and the
# suites that compare the operator's store before and after name it with
# host_own_store.
#
# And the host's configuration: a suite asserts karvi's defaults and passes
# its own settings by --config and --set, which a site's file changes or its
# [config-lock] refuses; every suite, lab, and verifier calls
# host_config_clean first, and run_suites before its lanes.

# host_shared_entries prints one line per entry under the host's shared
# places (a place that does not exist contributes nothing), sorted, so two
# calls compare as text. The loop ends with status 0 whatever exists: an
# `[ -d ] && find` list left the loop, and the pipeline under pipefail,
# at 1 when the last place was absent (the v0.16.0 baseline's second run).
host_shared_entries() {
  hse_uid=$(id -u)
  { for hse_dir in /dev/shm/karvi/scoreboards \
        /opt/karvi/shared /var/lib/karvi/shared; do
      if [ -d "$hse_dir" ]; then find "$hse_dir" -mindepth 1 2>/dev/null; fi
    done
    for hse_dir in "/tmp/karvi-$hse_uid" "/var/tmp/karvi-$hse_uid"; do
      if [ -e "$hse_dir" ]; then printf '%s\n' "$hse_dir"; fi
    done; } | sort
}

# host_shared_unchanged BEFORE fails, naming the entries that appeared or
# went, when the host's shared places differ from BEFORE (the output of an
# earlier host_shared_entries).
host_shared_unchanged() {
  hsu_after=$(host_shared_entries)
  [ "$hsu_after" = "$1" ] && return 0
  echo "the host's shared places changed during the run (a test or a suite wrote there):" >&2
  host_entries_diff "$1" "$hsu_after"
  return 1
}

# host_entries_diff BEFORE AFTER prints on stderr the lines that went (<)
# and came (>) between two listings. An empty listing is no line, not an
# empty one: a host with no shared entry (a container) compared a blank line.
host_entries_diff() {
  { [ -z "$1" ] || printf '%s\n' "$1"; } > "${TMPDIR:-/tmp}/karvi-host-before.$$"
  { [ -z "$2" ] || printf '%s\n' "$2"; } | diff "${TMPDIR:-/tmp}/karvi-host-before.$$" - >&2 || true
  rm -f "${TMPDIR:-/tmp}/karvi-host-before.$$"
}

# host_passwd_home prints the home of the operator's passwd entry, the one
# karvi takes for its places (`user.Current()`; $HOME only when the entry
# cannot be read), so a script that moves HOME does not move what karvi
# writes. Status 1, with a line on stderr, when there is no entry.
host_passwd_home() {
  hph_home=$(getent passwd "$(id -u)" | cut -d: -f6)
  [ -n "$hph_home" ] || { echo "no passwd entry for uid $(id -u)" >&2; return 1; }
  printf '%s\n' "$hph_home"
}

# host_places HOME prints host_shared_entries' lines and karvi's two places
# under HOME, each by its existence (a link counts): the private root's
# automatic place HOME/.local/share/karvi and the operator's configuration
# folder HOME/.config/karvi. By existence alone, as the scratch candidates,
# since the operator's own daemon writes inside the first; on a host where
# neither exists (the build host), a stateless command or a suite that
# writes either is seen. HOME is the passwd entry's (host_passwd_home); a
# test passes a folder of its own.
#
#   home=$(host_passwd_home)
#   before=$(host_places "$home")
#   ... HOME="$TMP/home" "$KARVI" --help ...
#   host_places_unchanged "$home" "$before"
host_places() {
  { host_shared_entries
    for hp_dir in "$1/.local/share/karvi" "$1/.config/karvi"; do
      if [ -e "$hp_dir" ] || [ -L "$hp_dir" ]; then printf '%s\n' "$hp_dir"; fi
    done; } | sort
}

# host_places_unchanged HOME BEFORE fails, naming what appeared or went,
# when host_places HOME differs from BEFORE (an earlier host_places HOME).
# A HOME that already had its places compares equal: the run added nothing.
host_places_unchanged() {
  hpu_after=$(host_places "$1")
  [ "$hpu_after" = "$2" ] && return 0
  echo "karvi's places under $1 or the host's shared places changed during the run:" >&2
  host_entries_diff "$2" "$hpu_after"
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

# host_own_store KARVI prints the operator's own trust store as KARVI
# resolves it for the operator's own configuration (`config show --explain`,
# its resolved line), the work directory's variables left out; an executable
# without the line gives the automatic store of a host without users roots.
host_own_store() {
  hos_path=$(env -u KARVI__SSH__KNOWN_HOSTS_FILE -u KARVI__BASEDIR "$1" config show --explain ssh.known-hosts-file 2>/dev/null | sed -n 's/^resolved: *//p')
  [ -n "$hos_path" ] || hos_path=${HOME:-/nonexistent}/.local/share/karvi/known_hosts
  printf '%s\n' "$hos_path"
}

# host_store_digest FILE prints the store's checksum, or "absent", so two
# calls compare as text.
host_store_digest() {
  if [ -f "$1" ]; then cksum <"$1"; else echo absent; fi
}

# host_config_clean KARVI refuses, status 1, a host whose configuration
# reaches karvi, naming each part on its own line: the files karvi reads on
# its own, the global and user layers with their includes, as KARVI's
# `config show --format json` names them in `sources` (the paths are
# karvi's alone, docs/FILES.md; --show-sources adds only a line per file on
# stderr); and the KARVI__ variables the script inherited, which outrank a
# suite's files and which karvi refuses when it knows no such key. KARVI
# reads its files with those variables left out, so a variable it would
# refuse does not hide a file. A script calls it before it makes anything
# or exports a variable of its own (host_trust_store); a host so configured
# verifies the bundle on a build host or in a container with an init
# (docs/BUILD-QUALIFICATION.md). Source scripts/lib/json.sh first.
#
#   . "$ROOT/scripts/lib/json.sh"; . "$ROOT/scripts/lib/host.sh"
#   host_config_clean "$KARVI"
host_config_clean() {
  command -v json_get >/dev/null 2>&1 || { echo "host_config_clean: source scripts/lib/json.sh first" >&2; return 2; }
  hcc_vars=$(awk 'BEGIN { for (n in ENVIRON) if (n ~ /^KARVI__[A-Za-z0-9_]*$/) print n }' | sort)
  hcc_unset=
  for hcc_v in $hcc_vars; do hcc_unset="$hcc_unset -u $hcc_v"; done
  # shellcheck disable=SC2086 # the -u NAME words; a name holds no space
  hcc_json=$(env $hcc_unset "$1" config show --format json) || {
    echo "host_config_clean: $1 config show failed: the host's configuration does not load" >&2; return 1; }
  hcc_sources=$(printf '%s\n' "$hcc_json" | json_get - sources 2>/dev/null) || {
    echo "host_config_clean: $1 config show names no sources" >&2; return 1; }
  hcc_files=
  [ "$hcc_sources" = null ] || hcc_files=$(printf '%s\n' "$hcc_json" | json_get - 'sources.*' 2>/dev/null) || :
  [ -n "$hcc_files$hcc_vars" ] || return 0
  echo "karvi reads this host's configuration; run where it reads none (docs/BUILD-QUALIFICATION.md):" >&2
  [ -z "$hcc_files" ] || printf '%s\n' "$hcc_files" | sed 's/^/  file: /' >&2
  [ -z "$hcc_vars" ] || printf '%s\n' "$hcc_vars" | sed 's/^/  variable: /' >&2
  return 1
}
