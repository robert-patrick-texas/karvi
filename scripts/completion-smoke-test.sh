#!/bin/bash
# Tab completion (the command's and the helpers')
# against the prebuilt karvi binary and the helpers beside it: the bash
# function karvi installs, sourced from the executable's own constant and
# driven under bash's completion machinery on a lab inventory and scoreboard
# for karvi and on the flag sets for karvi-prune and karvi-audit, and the
# three hidden words behind it. Runs without a network and writes nothing outside its own
# directory.
set -eu
ROOT=$(cd "$(dirname "$0")/.." && pwd)
KARVI=${KARVI:-$ROOT/bin/karvi-linux-amd64}
PRUNE=${KARVI_PRUNE:-$(dirname "$KARVI")/karvi-prune}
AUDIT=${KARVI_AUDIT:-$(dirname "$KARVI")/karvi-audit}
TMP=$(mktemp -d "${TMPDIR:-/tmp}/karvi-completion-smoke-XXXXXX")
. "$ROOT/scripts/lib/host.sh"; host_trust_store "$TMP"   # the trust store under the work directory, never the operator's
trap 'rm -rf "$TMP"' EXIT
install -d -m 700 "$TMP/home" "$TMP/state" "$TMP/sb" "$TMP/bin"
ln -s "$KARVI" "$TMP/bin/karvi"
ln -s "$PRUNE" "$TMP/bin/karvi-prune"
ln -s "$AUDIT" "$TMP/bin/karvi-audit"
export PATH=$TMP/bin:$PATH HOME=$TMP/home

printf 'name,management_address,platform\ncore-nyc-01,192.0.2.1,cisco_iosxe\nedge-sfo-02,192.0.2.2,generic\n' >"$TMP/inv.csv"
for id in 260925-101010-00 260925-090909-00; do
  printf '{"schema_version":4,"activity_id":"%s","job_id":"%s","operator":{"username":"u"},"mode":"run","status":"complete","started_at":"2026-09-25T10:00:00Z","last_updated_at":"2026-09-25T10:00:01Z"}\n' "$id" "$id" >"$TMP/sb/$id.json"
done
CFG=$TMP/karvi.toml
cat >"$CFG" <<EOF_CFG
basedir = "$TMP/state"
sharedroot = "none"
spooldir = "$TMP/spool"
scoreboards = "$TMP/sb"
[[inventory-source]]
name = "lab"
type = "csv"
path = "$TMP/inv.csv"
required = true
mode = "header"
delimiter = ","
mandatory-fields = ["name", "platform"]
[inventory-source.mappings]
name = ["name"]
management_address = ["management_address"]
platform = ["platform"]
EOF_CFG
chmod 600 "$CFG"

failures=0
fail() { echo "FAIL: $*" >&2; failures=$((failures + 1)); }

# The hidden word, as the function calls it: hidden EXE NAME CURSOR WORD...,
# CURSOR the index of the word under the cursor (the last word, or one past
# it for a Tab after a space); the candidates in $TMP/out. word is karvi's,
# pword karvi-prune's, aword karvi-audit's.
hidden() {
  local exe=$1 name=$2; shift 2
  "$exe" __complete "$@" >"$TMP/out" 2>"$TMP/err" </dev/null || fail "$name: exit $?"
  [ ! -s "$TMP/err" ] || fail "$name: stderr $(head -1 "$TMP/err")"
}
word() { hidden "$KARVI" "$@"; }
pword() { hidden "$PRUNE" "$@"; }
aword() { hidden "$AUDIT" "$@"; }
lines() { # lines NAME LINE...  the output is exactly these lines
  local name=$1; shift
  printf '%s\n' "$@" >"$TMP/want"
  cmp -s "$TMP/want" "$TMP/out" || fail "$name: got $(tr '\n' '|' <"$TMP/out"), want $(tr '\n' '|' <"$TMP/want")"
}
has() { grep -qx -- "$2" "$TMP/out" || fail "$1: lacks $2 in $(tr '\n' '|' <"$TMP/out")"; }
empty() { [ ! -s "$TMP/out" ] || fail "$1: got $(tr '\n' '|' <"$TMP/out"), want nothing"; }

word w1 1 karvi ru;                               lines w1 run
word w2 2 karvi run --tar;                        lines w2 --target
word w3 5 karvi --config "$CFG" run --target;     lines w3 core-nyc-01 edge-sfo-02
word w4 5 karvi --config "$CFG" job follow;       has w4 260925-101010-00; has w4 --format
word w5 7 karvi --config "$CFG" run --target core-nyc-01 show;  empty w5
word w6 2 karvi --set output.r;                   lines w6 output.reserve-multiplier= output.root=
word w7 2 karvi --config;                         lines w7 :files
word w8 3 karvi run --order;                      lines w8 default random shuffle sorted
word w9 2 karvi setup t;                          lines w9 tab
word w10 0 karvi;                                 empty w10
word w11 1 karvi --set=display.col;               has w11 --set=display.color=; has w11 --set=display.colors.success=
[ ! -e "$TMP/state" ] || [ -z "$(ls -A "$TMP/state")" ] || fail "the hidden word created state: $(ls -A "$TMP/state")"

# The helper's hidden word: its flags from its own flag set, a flag's
# words, the path directive, an inline value, nothing after -- or a
# positional; and no private root resolved (nothing under the lab's home).
FLAGS=(--basedir --days --dry-run --format --minfree --scoreboards --sharedroot --verbose)
pword p1 1 karvi-prune;                            lines p1 "${FLAGS[@]}"
pword p2 1 karvi-prune --d;                        lines p2 --days --dry-run
pword p3 2 karvi-prune --format;                   lines p3 jsonl text
pword p4 2 karvi-prune --sharedroot;               lines p4 auto none :files
pword p5 2 karvi-prune --scoreboards;              lines p5 :files
pword p6 1 karvi-prune --format=js;                lines p6 --format=jsonl
pword p7 3 karvi-prune --days 7;                   lines p7 "${FLAGS[@]}"
pword p8 2 karvi-prune --days;                     empty p8
pword p9 2 karvi-prune --;                         empty p9
pword p10 2 karvi-prune extra;                     empty p10
[ -z "$(ls -A "$TMP/home")" ] || fail "the helper's hidden word wrote under the home: $(ls -A "$TMP/home")"

# karvi-audit's hidden word: its long flags alone, a one-letter spelling
# completing to nothing; nothing for a value Tab cannot know (a name, a
# time), after a positional, or after --.
AFLAGS=(--device --follow --grep --lines --nocolor --operator --reverse --since --until)
aword a1 1 karvi-audit;                            lines a1 "${AFLAGS[@]}"
aword a2 1 karvi-audit --f;                        lines a2 --follow
aword a3 1 karvi-audit -f;                         empty a3
aword a4 2 karvi-audit -o;                         empty a4
aword a5 2 karvi-audit --since;                    empty a5
aword a6 3 karvi-audit -n 5;                       lines a6 "${AFLAGS[@]}"
aword a7 2 karvi-audit extra;                      empty a7
aword a8 2 karvi-audit --;                         empty a8

# setup tab as the operator: refused before anything is looked at.
set +e
"$KARVI" setup tab >"$TMP/out" 2>"$TMP/err" </dev/null; rc=$?
set -e
[ "$rc" -eq 9 ] || fail "s1: setup tab as the operator exited $rc, want 9"
grep -q '^setup_requires_root: ' "$TMP/err" || fail "s1: $(head -1 "$TMP/err")"
[ ! -s "$TMP/out" ] || fail "s1: stdout must be empty"

# The bash function itself, driven under bash's completion machinery with
# the bash-completion library, which supplies _init_completion; without the
# library the function is not driven. The function is the Go constant of
# internal/cli/setup_tab.go, which is what setup tab writes (TestSetupTab
# holds the written file to it byte for byte); the suite has no root, so it
# takes the constant from the source tree beside the executable.
LIB=/usr/share/bash-completion/bash_completion
if [ -r "$LIB" ]; then
  SCRIPT=$TMP/karvi.bash
  awk '/^const completionScript = `/{f=1; next} f && /^`$/{exit} f' "$ROOT/internal/cli/setup_tab.go" >"$SCRIPT"
  grep -q '^complete -F _karvi karvi$' "$SCRIPT" || fail "b0: the script was not extracted"
  grep -q '^complete -F _karvi karvi-prune$' "$SCRIPT" || fail "b0: karvi-prune is not registered"
  grep -q '^complete -F _karvi karvi-audit$' "$SCRIPT" || fail "b0: karvi-audit is not registered"
  # tab NAME CWORD WORD... : COMPREPLY after the function, one per line; a
  # Tab after a space is an empty last word, as bash passes it. The function
  # is called as bash calls it, with the command word, the current word, and
  # the previous word (17.1: the command word names the executable asked).
  tab() {
    local name=$1 cword=$2; shift 2
    bash -c '
      set -u
      . "$1"; . "$2"; shift 2
      COMP_CWORD=$1; shift
      COMP_WORDS=("$@")
      COMP_LINE="${COMP_WORDS[*]}"
      COMP_POINT=${#COMP_LINE}
      _karvi "${COMP_WORDS[0]}" "${COMP_WORDS[COMP_CWORD]}" "${COMP_WORDS[COMP_CWORD - 1]}"
      printf "%s\n" "${COMPREPLY[@]}"
    ' bash "$LIB" "$SCRIPT" "$cword" "$@" >"$TMP/out" 2>"$TMP/err" || fail "$name: bash exit $?"
    [ ! -s "$TMP/err" ] || fail "$name: stderr $(head -1 "$TMP/err")"
  }
  tab b1 1 karvi ru;                                 lines b1 run
  tab b2 2 karvi run --tar;                          lines b2 --target
  tab b3 5 karvi --config "$CFG" run --target "";    lines b3 core-nyc-01 edge-sfo-02
  tab b4 5 karvi --config "$CFG" run --target e;     lines b4 edge-sfo-02
  tab b5 5 karvi --config "$CFG" job follow "";      has b5 260925-090909-00; has b5 --echo
  tab b6 7 karvi --config "$CFG" run --target core-nyc-01 show "";  lines b6 ""
  tab b7 2 karvi --set output.r;                     lines b7 output.reserve-multiplier= output.root=
  tab b8 1 karvi --set=display.col;                  has b8 --set=display.color=
  tab b9 2 karvi setup "";                           lines b9 --help shared tab
  # The helpers through the same function. No row asks for file names:
  # compopt runs only inside an interactive completion.
  tab pb1 1 karvi-prune --d;                         lines pb1 --days --dry-run
  tab pb2 2 karvi-prune --format "";                 lines pb2 jsonl text
  tab pb3 2 karvi-prune --sharedroot "";             lines pb3 auto none
  tab pb4 1 karvi-prune --format=js;                 lines pb4 --format=jsonl
  tab pb5 3 karvi-prune --days 7 "";                 lines pb5 "${FLAGS[@]}"
  tab pb6 2 karvi-prune extra "";                    lines pb6 ""
  tab ab1 1 karvi-audit --f;                         lines ab1 --follow
  tab ab2 3 karvi-audit -o netops "";                lines ab2 "${AFLAGS[@]}"
  tab ab3 2 karvi-audit -d "";                       lines ab3 ""
  echo "completion-smoke: the function driven under bash $BASH_VERSION with $LIB"
else
  echo "completion-smoke: $LIB absent; the function was not driven (the hidden word was)"
fi

[ "$failures" -eq 0 ] || { echo "completion-smoke: $failures failure(s)" >&2; exit 1; }
echo "completion-smoke: ok"
