#!/bin/sh
# The check of scripts/lib/rows.sh: under sh and bash, row against a model
# command (the exit it wants, another one, a function of the suite, stdin
# from /dev/null, the streams and row_out/row_err, a missing --, row_dir
# unset), row_open, fail, and finish (failures over rows, a pass with and
# without a detail, a suite of checks alone); and over the tree, no counted
# fail, row, row_open, or finish but rows.sh's.
set -eu
ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/../.." && pwd)
. "$ROOT/scripts/lib/exit.sh"
TMP=$(mktemp -d)
cleanup() { rm -rf "$TMP"; }
on_exit cleanup
failures=0
check() { # check NAME WANT GOT
  if [ "$2" = "$3" ]; then echo "rows lib: $1: pass"; else printf '%s\n' "rows lib: $1: FAIL: want [$2] got [$3]" >&2; failures=$((failures + 1)); fi
}

# The model command: its arguments on stdout, what it read on stdin after
# them, a line on stderr, then the exit its first argument names.
cat >"$TMP/model" <<'EOF'
#!/bin/sh
rc=$1; shift
printf 'args %s\n' "$*"
printf 'stdin [%s]\n' "$(cat)"
echo "model stderr $rc" >&2
echo "second line" >&2
exit "$rc"
EOF
chmod 755 "$TMP/model"

# The caller: sources rows.sh under the shell that runs it, then one case;
# set -eu as the suites. Its output is the rows' lines and what the case
# echoes; its stderr the failures and finish's line.
cat >"$TMP/caller.sh" <<'EOF'
set -eu
LIB=$1; W=$2; M=$3; shift 3
row_dir=$W/streams; mkdir -p "$row_dir"
. "$LIB/rows.sh"
case $1 in
  pass)
    row a1 0 -- "$M" 0 one 'two words'
    echo "out $(tr '\n' '|' <"$row_out") err $(tr '\n' '|' <"$row_err")"
    echo "files $(cd "$row_dir" && ls | tr '\n' ' ')"
    row a2 3 -- "$M" 3
    finish model ;;
  wrong)
    row b1 0 -- "$M" 4
    echo "went on $row_count $row_failures"
    finish model ;;
  function)
    k() { "$M" "$@" --tail; }
    row c1 5 -- k 5 x
    echo "out $(head -1 "$row_out")"
    finish model "the detail" ;;
  usage)
    rc=0; row d1 0 "$M" 0 || rc=$?
    echo "status $rc count $row_count files $(ls "$row_dir" | wc -l)"
    rc=0; row d2 0 -- || rc=$?
    echo "status $rc count $row_count" ;;
  nodir)
    row_dir=
    rc=0; row e1 0 -- "$M" 0 || rc=$?
    echo "status $rc count $row_count" ;;
  open)
    row_open f1
    echo "count $row_count out ${row_out#"$W"/} err ${row_err#"$W"/}"
    fail "f1: by hand"
    echo "went on $row_failures"
    finish model ;;
  escape)
    fail 'a\b and \1 as written'
    finish 'model\t' ;;
  checks)
    [ "${2:-}" != fail ] || fail "one check"
    finish 'checks alone' "${3:-}" ;;
esac
EOF

# rows SHELL CASE [ARG...]: the caller under SHELL in a new folder; its
# status and its output on one line, its stderr in TMP/last.err (rows runs
# in a command substitution, so the folder's name does not outlive it).
rows() {
  r_w=$(mktemp -d "$TMP/w.XXXXXX")
  r_sh=$1; shift
  r_rc=0
  "$r_sh" "$TMP/caller.sh" "$ROOT/scripts/lib" "$r_w" "$TMP/model" "$@" >"$r_w/out" 2>"$TMP/last.err" </dev/null || r_rc=$?
  echo "$r_rc $(tr '\n' '|' <"$r_w/out")"
}
err() { tr '\n' '|' <"$TMP/last.err"; }

for sh in sh bash; do
  check "$sh two rows at their exits" "0 a1   exit 0|out args one two words|stdin []| err model stderr 0|second line||files a1.err a1.out |a2   exit 3|model: pass (2 rows)|" "$(rows "$sh" pass)"
  check "$sh nothing on stderr" "" "$(err)"
  check "$sh another exit: counted, the suite goes on, finish ends it" "1 b1   exit 4|went on 1 1|" "$(rows "$sh" wrong)"
  check "$sh its failure and finish's line" "FAIL: b1: exit 4, want 0; stderr: model stderr 4|model: 1 failure(s) over 1 rows|" "$(err)"
  check "$sh a function of the suite as the command, a detail" "0 c1   exit 5|out args x --tail|model: pass (1 rows, the detail)|" "$(rows "$sh" function)"
  check "$sh without --: status 2, nothing run or counted" "0 status 2 count 0 files 0|status 2 count 0|" "$(rows "$sh" usage)"
  check "$sh its usage line" "row: usage: row NAME WANT -- COMMAND...|row: usage: row NAME WANT -- COMMAND...|" "$(err)"
  check "$sh row_dir unset: status 2, nothing counted" "0 status 2 count 0|" "$(rows "$sh" nodir)"
  check "$sh row_open, fail by hand" "1 count 1 out streams/f1.out err streams/f1.err|went on 1|" "$(rows "$sh" open)"
  check "$sh its lines" "FAIL: f1: by hand|model: 1 failure(s) over 1 rows|" "$(err)"
  check "$sh fail and finish print their text as written (no echo escapes)" "1 |FAIL: a\\b and \\1 as written|model\\t: 1 failure(s)|" "$(rows "$sh" escape)|$(err)"
  check "$sh checks alone, passing" "0 checks alone: pass|" "$(rows "$sh" checks)"
  check "$sh checks alone, a detail" "0 checks alone: pass (seven rows read)|" "$(rows "$sh" checks pass 'seven rows read')"
  check "$sh checks alone, failing" "1 |FAIL: one check|checks alone: 1 failure(s)|" "$(rows "$sh" checks fail)|$(err)"
done

# Over the tree: the counted-failure functions are rows.sh's alone; a
# suite's own fail ends it (an exit), it never counts.
SCRIPTS="scripts/*.sh"
# shellcheck disable=SC2086
copies=$(cd "$ROOT" && grep -nE '^[[:space:]]*(row|row_open|finish)\(\) *\{' $SCRIPTS || true)
check "row, row_open, and finish defined in rows.sh alone" "" "$copies"
# shellcheck disable=SC2086
counted=$(cd "$ROOT" && grep -nE '^[[:space:]]*fail\(\) *\{.*failures=' $SCRIPTS || true)
check "no counted fail but rows.sh's" "" "$counted"

[ "$failures" -eq 0 ] || { echo "rows lib: $failures failed" >&2; exit 1; }
echo "rows lib: pass"
