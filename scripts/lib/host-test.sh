#!/bin/sh
# The check of scripts/lib/host.sh's places under the home: under sh and
# bash, host_places and host_places_unchanged over a scratch folder standing
# for the passwd home, against a model executable that writes there whatever
# HOME says, as karvi does (never the operator's real home); host_passwd_home
# against getent; and over the tree, the scripts whose karvi runs must leave
# the home alone compare its places.
set -eu
ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/../.." && pwd)
. "$ROOT/scripts/lib/exit.sh"
TMP=$(mktemp -d)
cleanup() { rm -rf "$TMP"; }
on_exit cleanup
failures=0
check() { # check NAME WANT GOT
  if [ "$2" = "$3" ]; then echo "host lib: $1: pass"; else echo "host lib: $1: FAIL: want [$2] got [$3]" >&2; failures=$((failures + 1)); fi
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
# status, its stderr in P.err.
cat >"$TMP/caller.sh" <<'EOF'
set -eu
. "$1/host.sh"; P=$2
case $3 in
  shared)
    if [ "$(host_places "$P")" = "$(host_shared_entries)" ]; then echo same; else echo differs; fi ;;
  passwd)
    host_passwd_home ;;
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

# Over the tree: the stateless check and the completion suite's hidden words
# compare the passwd home's places, not only a fresh $HOME.
for f in scripts/verify-shipped.sh scripts/completion-smoke-test.sh; do
  # The pattern names the scripts' $home literally.
  # shellcheck disable=SC2016
  check "$f compares the passwd home's places" "yes" "$(if grep -q '^[^#]*host_places_unchanged "\$home"' "$ROOT/$f"; then echo yes; else echo no; fi)"
done

[ "$failures" -eq 0 ] || { echo "host lib: $failures failed" >&2; exit 1; }
echo "host lib: pass"
