#!/bin/sh
# The check of scripts/lib/host.sh's places under the home: under sh and
# bash, host_places and host_places_unchanged over a scratch folder standing
# for the passwd home, against a model executable that writes there whatever
# HOME says, as karvi does (never the operator's real home); host_passwd_home
# against getent; host_config_clean against a fake karvi; and over the
# tree, the scripts whose karvi runs must leave the home alone compare its
# places, and every suite, lab, and verifier calls host_config_clean first.
set -eu
ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/../.." && pwd)
. "$ROOT/scripts/lib/exit.sh"
TMP=$(mktemp -d)
cleanup() { rm -rf "$TMP"; }
on_exit cleanup
failures=0
check() { # check NAME WANT GOT
  if [ "$2" = "$3" ]; then echo "host lib: $1: pass"; else printf '%s\n' "host lib: $1: FAIL: want [$2] got [$3]" >&2; failures=$((failures + 1)); fi
}

# The model executable: what MODEL_DO names, under MODEL_HOME, the passwd
# home it stands for, whatever HOME says.
cat >"$TMP/model" <<'EOF'
#!/bin/sh
case $MODEL_DO in
  nothing) ;;
  private) mkdir -p "$MODEL_HOME/.local/share/karvi/state" ;;
  config) mkdir -p "$MODEL_HOME/.config/karvi"; : >"$MODEL_HOME/.config/karvi/config.toml" ;;
  inside) : >"$MODEL_HOME/.local/share/karvi/jobs" ;;
  link) mkdir -p "$MODEL_HOME/.config"; ln -s /nonexistent "$MODEL_HOME/.config/karvi" ;;
esac
EOF
chmod 755 "$TMP/model"

# The caller: sources host.sh under the shell that runs it; the places of
# the home P before, the model run under another HOME, then the comparison's
# status, its stderr in P.err. Its diff case: host_entries_diff over two
# listings, its stderr's lines naming an entry, |-separated.
cat >"$TMP/caller.sh" <<'EOF'
set -eu
. "$1/host.sh"; P=$2
case $3 in
  shared)
    if [ "$(host_places "$P")" = "$(host_shared_entries)" ]; then echo same; else echo differs; fi ;;
  passwd)
    host_passwd_home ;;
  diff)
    host_entries_diff "$4" "$5" 2>&1 | { grep '^[<>]' || :; } | tr '\n' '|' ;;
  *)
    export MODEL_HOME=$P MODEL_DO=$3
    before=$(host_places "$P")
    HOME="$P.moved" "$4"
    rc=0; host_places_unchanged "$P" "$before" 2>"$P.err" || rc=$?
    echo "status $rc" ;;
esac
EOF

# home [PRE]: a new folder standing for the passwd home, HOST_H, holding the
# private root already when PRE is private.
home() {
  HOST_H=$(mktemp -d "$TMP/h.XXXXXX")
  [ "${1:-}" != private ] || mkdir -p "$HOST_H/.local/share/karvi"
}
# run SHELL CASE: the caller under SHELL over HOST_H; its output on one line.
run() { "$1" "$TMP/caller.sh" "$ROOT/scripts/lib" "$HOST_H" "$2" "$TMP/model" | tr '\n' ' ' | sed 's/ $//'; }
# named: the comparison's lines naming an entry, HOST_H as H, |-separated.
named() { grep '^[<>] ' "$HOST_H.err" | sed "s|$HOST_H|H|" | tr '\n' '|'; }

for sh in sh bash; do
  check "$sh host_passwd_home is the passwd entry's" "$(getent passwd "$(id -u)" | cut -d: -f6)" "$("$sh" "$TMP/caller.sh" "$ROOT/scripts/lib" "$TMP" passwd)"
  # An empty listing, a host with no shared entry (a container), is no line.
  check "$sh the diff from an empty listing names the new entry alone" "> /tmp/karvi-1001|" "$("$sh" "$TMP/caller.sh" "$ROOT/scripts/lib" "$TMP" diff "" /tmp/karvi-1001)"
  check "$sh the diff to an empty listing names the entry gone alone" "< /tmp/karvi-1001|" "$("$sh" "$TMP/caller.sh" "$ROOT/scripts/lib" "$TMP" diff /tmp/karvi-1001 "")"
  check "$sh the diff of two empty listings names nothing" "" "$("$sh" "$TMP/caller.sh" "$ROOT/scripts/lib" "$TMP" diff "" "")"
  home
  check "$sh an empty home adds nothing to the shared places" "same" "$(run "$sh" shared)"
  home
  check "$sh the model writes nothing" "status 0" "$(run "$sh" nothing)"
  home
  check "$sh the model makes the private root, HOME moved: seen" "status 1|> H/.local/share/karvi|" "$(run "$sh" private)|$(named)"
  check "$sh the line before the diff" "karvi's places under $HOST_H or the host's shared places changed during the run:" "$(head -1 "$HOST_H.err")"
  home
  check "$sh the model writes the operator's configuration: seen" "status 1|> H/.config/karvi|" "$(run "$sh" config)|$(named)"
  home
  check "$sh a link at the configuration folder: seen" "status 1|> H/.config/karvi|" "$(run "$sh" link)|$(named)"
  home private
  check "$sh a private root there before, written inside: unchanged (by existence)" "status 0" "$(run "$sh" inside)"
done

# host_config_clean under sh and bash, against a fake karvi whose `config
# show --format json` names the files FAKE_SOURCES says
# (none: null; empty: []; one; two, the second holding a space; nokey: no
# sources; broken: karvi's refusal, exit 2), and which writes its arguments
# and the KARVI__ variables it was given into FAKE_LOG. Each row runs under
# env -i, its inherited variables the row's own, never the operator's.
cat >"$TMP/fake-karvi" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >"$FAKE_LOG"
env | sed -n 's/^\(KARVI__[A-Za-z0-9_]*\)=.*/\1/p' >>"$FAKE_LOG"
case $FAKE_SOURCES in
  none) printf '{\n  "config_schema_version": 6,\n  "sources": null,\n  "values": {}\n}\n' ;;
  empty) printf '{"sources":[],"values":{}}\n' ;;
  one) printf '{\n  "sources": [\n    "/etc/karvi/config.toml"\n  ],\n  "values": {}\n}\n' ;;
  two) printf '{"sources":["/etc/karvi/config.toml","/opt/karvi/includes/lab site.toml"],"values":{}}\n' ;;
  nokey) printf '{"values":{}}\n' ;;
  broken) echo 'config_parse_error: /etc/karvi/config.toml:3: a value expected' >&2; exit 2 ;;
esac
EOF
chmod 755 "$TMP/fake-karvi"
# The caller: the libraries sourced as a script does (json.sh left out when
# MODE is nojson; a KARVI__ variable set but not exported when MODE is
# unexported); the status, its stderr in C.err.
cat >"$TMP/clean.sh" <<'EOF'
set -eu
[ "$4" = nojson ] || . "$1/json.sh"
. "$1/host.sh"
[ "$4" != unexported ] || KARVI__DISPLAY__COLOR=never
rc=0; host_config_clean "$2" 2>"$3.err" || rc=$?
echo "status $rc"
EOF
# clean SHELL SOURCES MODE [NAME=VALUE...]: the caller under SHELL over the
# fake, in an environment of PATH, the fake's two variables, and the
# NAME=VALUEs; its status and stderr's lines on one line, |-separated.
clean() {
  cl_sh=$1 cl_sources=$2 cl_mode=$3; shift 3
  cl_status=$(env -i PATH="$PATH" FAKE_SOURCES="$cl_sources" FAKE_LOG="$TMP/fake.log" "$@" \
    "$cl_sh" "$TMP/clean.sh" "$ROOT/scripts/lib" "$TMP/fake-karvi" "$TMP/c" "$cl_mode")
  printf '%s|%s' "$cl_status" "$(tr '\n' '|' <"$TMP/c.err")"
}
HEAD="karvi reads this host's configuration; run where it reads none (docs/BUILD-QUALIFICATION.md):"
for sh in sh bash; do
  check "$sh config: no file, no variable: clean" "status 0|" "$(clean "$sh" none -)"
  check "$sh config: the fake asked for the sources as JSON" "config show --format json" "$(cat "$TMP/fake.log")"
  check "$sh config: an empty list: clean" "status 0|" "$(clean "$sh" empty -)"
  check "$sh config: one file: refused, named" "status 1|$HEAD|  file: /etc/karvi/config.toml|" "$(clean "$sh" one -)"
  check "$sh config: two files, one holding a space: each on its own line" "status 1|$HEAD|  file: /etc/karvi/config.toml|  file: /opt/karvi/includes/lab site.toml|" "$(clean "$sh" two -)"
  check "$sh config: an inherited KARVI__ variable: refused, named" "status 1|$HEAD|  variable: KARVI__DISPLAY__COLOR|" "$(clean "$sh" none - KARVI__DISPLAY__COLOR=never)"
  check "$sh config: a file and two variables: the file, then the variables sorted" "status 1|$HEAD|  file: /etc/karvi/config.toml|  variable: KARVI__BASEDIR|  variable: KARVI__SSH__KNOWN_HOSTS_FILE|" "$(clean "$sh" one - KARVI__SSH__KNOWN_HOSTS_FILE=/x/known_hosts KARVI__BASEDIR=/x)"
  check "$sh config: karvi read its files without the variables" "config show --format json" "$(cat "$TMP/fake.log")"
  check "$sh config: a variable set, not exported: clean" "status 0|" "$(clean "$sh" none unexported)"
  check "$sh config: karvi refuses the configuration: its line, then the refusal" "status 1|config_parse_error: /etc/karvi/config.toml:3: a value expected|host_config_clean: $TMP/fake-karvi config show failed: the host's configuration does not load|" "$(clean "$sh" broken -)"
  check "$sh config: no sources key: refused" "status 1|host_config_clean: $TMP/fake-karvi config show names no sources|" "$(clean "$sh" nokey -)"
  check "$sh config: json.sh not sourced: status 2" "status 2|host_config_clean: source scripts/lib/json.sh first|" "$(clean "$sh" none nojson)"
done

# Over the tree: every script sourcing host.sh, and every suite run_suites
# names, calls host_config_clean before its first line that makes a folder
# or sets a KARVI__ variable of its own (mktemp, install -d, mkdir,
# host_trust_store, KARVI__NAME=), comment lines aside; run_suites calls it
# before its lanes.
# shellcheck disable=SC2086 # the lanes' words
lanes=$(. "$ROOT/scripts/lib/suites.sh" && echo $RS_LANE1 $RS_LANE2 $RS_LANE3)
config_late=$(cd "$ROOT" && { grep -l '^[^#]*\. .*scripts/lib/host\.sh' scripts/*.sh; for s in $lanes; do echo "scripts/$s.sh"; done; } | sort -u | while read -r f; do
  awk -v f="$f" '/^[[:space:]]*#/ { next }
    !c && /host_config_clean/ { c = NR }
    !m && /mktemp|install -d|mkdir|host_trust_store|KARVI__[A-Z0-9_]*=/ { m = NR }
    END { if (!c) print f ": no host_config_clean"; else if (m && m < c) print f ":" m ": makes or exports before host_config_clean (:" c ")" }' "$f"
done)
check "every script sourcing host.sh, and every suite, calls host_config_clean first" "" "$config_late"
check "run_suites calls host_config_clean before its lanes" "" "$(awk '/^run_suites\(\)/ { b = 1 } b && !c && /host_config_clean/ { c = NR } b && !l && /rs_run|rs_start/ { l = NR } END { if (!c || c > l) print "run_suites: no host_config_clean before its lanes" }' "$ROOT/scripts/lib/suites.sh")"

# Over the tree: the stateless check, the completion suite's hidden words,
# and the bundle and release verifiers around the Go tests and the suites
# compare the passwd home's places, not only a fresh $HOME or the shared
# entries.
for f in scripts/verify-shipped.sh scripts/completion-smoke-test.sh scripts/verify-bundle.sh scripts/verify-release.sh; do
  # The pattern names the scripts' $home literally.
  # shellcheck disable=SC2016
  check "$f compares the passwd home's places" "yes" "$(if grep -q '^[^#]*host_places_unchanged "\$home"' "$ROOT/$f"; then echo yes; else echo no; fi)"
done

[ "$failures" -eq 0 ] || { echo "host lib: $failures failed" >&2; exit 1; }
echo "host lib: pass"
