#!/bin/bash
# K03 parser acceptance (the command rows, aliases, spellings, renames,
# and parser codes) against the prebuilt karvi binary and a fake ssh that logs
# exactly what reaches the device. Runs without a network.
set -eu
ROOT=$(cd "$(dirname "$0")/.." && pwd)
KARVI=${KARVI:-$ROOT/bin/karvi-linux-amd64}
. "$ROOT/scripts/lib/json.sh"
TMP=$(mktemp -d)
. "$ROOT/scripts/lib/host.sh"; host_trust_store "$TMP"   # the trust store under the work directory, never the operator's
trap 'rm -rf "$TMP"' EXIT
install -d -m 700 "$TMP/home" "$TMP/state" "$TMP/sb" "$TMP/cap"
FAKE=$TMP/fake-ssh
cat >"$FAKE" <<'EOF'
#!/bin/sh
case " $* " in
  " -Q "*) exit 0 ;; # the system transport's ssh -Q capability queries
  *" -O check "*) exit 1 ;;
  *" BatchMode=no "*)
    printf '127.0.0.1#'
    while IFS= read -r line; do
      [ "$line" = exit ] && exit 0
      printf 'SENT:%s\n' "$line" >>"$0.log"
      printf '%s\r\nok\r\n127.0.0.1#' "$line"
    done
    exit 0 ;;
esac
for last; do :; done
printf 'SENT:%s\n' "$last" >>"$0.log"
printf 'ok\n'
EOF
chmod 755 "$FAKE"
G=(--set "basedir=\"$TMP/state\"" --set 'sharedroot="none"' --set "spooldir=\"$TMP/state/spool\"" --set 'platform-resolution.default=""' --set "ssh.transports.system=\"$FAKE\""
   --set audit.journald-required=false --set "audit.file=\"$TMP/state/audit.jsonl\""
   --set "watch.directory=\"$TMP/sb\"" --set "sessions.shared-capacity-root=\"$TMP/cap\""
   --set output.min-free-bytes-after-job=0 --set display.color=never
   --set 'ssh.host-key-policy="insecure"' --set 'ssh.run.transport="system"')

failures=0
fail() { echo "FAIL: $*" >&2; failures=$((failures + 1)); }

# run NAME EXPECTED_EXIT -- args...   (records exit, stdout, stderr, device log)
run() {
  local name=$1 want=$2; shift 3
  : >"$FAKE.log"
  set +e
  HOME=$TMP/home NETUSER=u NETPASS=p "$KARVI" "${G[@]}" "$@" >"$TMP/out" 2>"$TMP/err" </dev/null
  rc=$?
  set -e
  [ "$rc" -eq "$want" ] || fail "$name: exit $rc, want $want; stderr: $(head -1 "$TMP/err")"
}
sent() { # sent NAME LINE...  device received exactly these lines in order
  local name=$1; shift
  printf '%s\n' "$@" | sed 's/^/SENT:/' >"$TMP/want"
  [ -f "$FAKE.log" ] || : >"$FAKE.log"
  cmp -s "$TMP/want" "$FAKE.log" || fail "$name: device received $(tr '\n' '|' <"$FAKE.log"), want $(tr '\n' '|' <"$TMP/want")"
}
nothing_sent() { [ ! -s "$FAKE.log" ] || fail "$1: device received $(tr '\n' '|' <"$FAKE.log"), want nothing"; }
code() { # code NAME CODE  stderr starts with the code, stdout is empty
  grep -q "^$2: " "$TMP/err" || fail "$1: stderr $(head -1 "$TMP/err"), want code $2"
  [ ! -s "$TMP/out" ] || fail "$1: stdout must be empty"
}
mentions() { grep -q -- "$2" "$TMP/err" || fail "$1: stderr lacks $2: $(head -1 "$TMP/err")"; }
echoed() { grep -q "^127.0.0.1#$2\$" "$TMP/out" || fail "$1: no echo of $2"; }
not_echoed() { ! grep -q "^127.0.0.1#" "$TMP/out" || fail "$1: unexpected echo"; }
helped() { grep -q '^Usage' "$TMP/out" || fail "$1: no help on stdout"; [ ! -s "$TMP/err" ] || fail "$1: stderr not empty"; }

echo "== the parser acceptance rows"
run row1 0 -- com --ech 127.0.0.1 show ip route;                          sent row1 "show ip route"; echoed row1 "show ip route"
run row2 0 -- command 127.0.0.1 --echo show ip route;                     sent row2 "show ip route"; echoed row2 "show ip route"
run row3 0 -- command 127.0.0.1 show ip route --echo;                     sent row3 "show ip route --echo"; not_echoed row3
run row4 0 -- command 127.0.0.1 traceroute -4 192.0.2.1;                  sent row4 "traceroute -4 192.0.2.1"
run row5 0 -- command 127.0.0.1 show run '|' grep -e bgp;                 sent row5 "show run | grep -e bgp"
run row6 0 -- command 127.0.0.1 show interfaces '|' grep -h up;           sent row6 "show interfaces | grep -h up"; ! grep -q '^Usage' "$TMP/out" || fail "row6: help printed"
run row7 0 -- command --echo 127.0.0.1 "show ip route" "show clock";      sent row7 "show ip route show clock"
run row8 0 -- command --echo 127.0.0.1 --cmd "show ip route" --cmd "show clock"; sent row8 "show ip route" "show clock"
run row9 0 -- command --targ 127.0.0.1 --command "show ip route" --ech;   sent row9 "show ip route"; echoed row9 "show ip route"
run row10 4 -- command --tar 127.0.0.1 --co "show ip route";              nothing_sent row10; code row10 cli_option_ambiguous; mentions row10 -- "--cmd (as --command)"; mentions row10 -- "--continue-device-on-error"
run row11 4 -- co 127.0.0.1 show clock;                                   nothing_sent row11; code row11 cli_command_ambiguous; mentions row11 command; mentions row11 config
run row12 0 -- command 127.0.0.1 show clock --cmd "show version";         sent row12 "show clock --cmd show version"
run row13 0 -- command --echo 127.0.0.1 -- -example device text;          sent row13 "-example device text"
run row14 0 -- run --no-daemon --host 127.0.0.1 show clock --echo;        sent row14 "show clock --echo"
run row15 0 -- run --no-daemon --targ 127.0.0.1 show clock --echo;        sent row15 "show clock --echo"

echo "== aliases and spellings"
run a1 0 -- command -t 127.0.0.1 -c "show clock";                         sent a1 "show clock"
run a2 0 -- command --t 127.0.0.1 --c "show clock";                       sent a2 "show clock"
run a3 0 -- command -a 127.0.0.1 -t 127.0.0.1 -c "show clock";            sent a3 "show clock"
run a4 0 -- command -echo -target 127.0.0.1 show clock;                   sent a4 "show clock"; echoed a4 "show clock"
run a5 0 -- command -ech=true -tar 127.0.0.1 show clock;                  sent a5 "show clock"; echoed a5 "show clock"
run a6 0 -- command --echo=false 127.0.0.1 show clock;                    sent a6 "show clock"; not_echoed a6
run a8 0 -- command 127.0.0.1 -h;                                          nothing_sent a8; helped a8
run a9 0 -- cmd --h;                                                       helped a9
run a10 0 -- -h;                                                           helped a10
run a11 0 -- login 127.0.0.1 --help;                                       helped a11
run a12 0 -- run --no-daemon -t 127.0.0.1 -t 127.0.0.1 --c "show clock";   sent a12 "show clock"

echo "== renamed and removed spellings"
run r1 4 -- run --targets-file x show clock;                               code r1 cli_option_unknown
run r2 4 -- run --targets x show clock;                                    code r2 cli_option_unknown
run r3 4 -- command --commands-file x 127.0.0.1;                           code r3 cli_option_unknown
run r4 4 -- command --host-key-policy insecure 127.0.0.1 show clock;       code r4 cli_option_unknown
run r5 4 -- command --known-hosts-file auto 127.0.0.1 show clock;          code r5 cli_option_unknown
run r6 4 -- login 127.0.0.1 --record-file x;                               code r6 cli_option_unknown
run r7 0 -- command --ssh-host-key-policy insecure --ssh-known-hosts-file auto 127.0.0.1 show clock; sent r7 "show clock"
printf 'show clock\n\n# comment\nshow version\r\n' >"$TMP/cmds"
run r8 0 -- command 127.0.0.1 --cf "$TMP/cmds";                            sent r8 "show clock" "" "# comment" "show version"
run r9 4 -- command 127.0.0.1 --cf "$TMP/cmds" --cf "$TMP/cmds";           code r9 commands_file_repeated
: >"$TMP/empty"
run r10 4 -- command 127.0.0.1 --cf "$TMP/empty";                          code r10 commands_file_empty
run r11 4 -- command 127.0.0.1 --cf "$TMP";                                code r11 commands_file_is_directory
printf '127.0.0.1\n' >"$TMP/targets"
run r12 0 -- run --no-daemon --tf "$TMP/targets" show clock;               sent r12 "show clock"

echo "== target sets in every mode"
# Inventory: two devices that both resolve to the fake device. Core-A keeps its
# inventory spelling; its identity is lowercase.
printf 'name,management_address,platform,site\nCore-A,127.0.0.1,generic,hq\nedge-b,127.0.0.1,generic,branch\n' >"$TMP/inv.csv"
cat >"$TMP/inv.toml" <<EOF_INV
[[inventory-source]]
name = "smoke"
type = "csv"
path = "$TMP/inv.csv"
required = true
mode = "header"
delimiter = ","
mandatory-fields = ["name", "platform"]
name-transform = "none"

[inventory-source.mappings]
name = ["name"]
management_address = ["management_address"]
platform = ["platform"]
site = ["site"]

[name-transform.none]
operations = []
EOF_INV
chmod 600 "$TMP/inv.toml"
order() { # order NAME DEVICE...  stdout shows these devices in this order
  local name=$1; shift
  printf '%s\n' "$@" >"$TMP/want"
  sed -n 's/^! \([^ ]*\) \[.*/\1/p' "$TMP/out" >"$TMP/got"
  cmp -s "$TMP/want" "$TMP/got" || fail "$name: devices $(tr '\n' '|' <"$TMP/got"), want $(tr '\n' '|' <"$TMP/want")"
}
printf 'LOCALHOST\n# comment\n\n127.0.0.1\n' >"$TMP/tset"
run t1 0 -- run --no-daemon --target 127.0.0.1 --tf "$TMP/tset" --target localhost show clock
order t1 127.0.0.1 localhost                                              # order kept; LOCALHOST folded and deduplicated
run t2 0 -- --config "$TMP/inv.toml" run --no-daemon --target localhost --site hq --target 127.0.0.1 show clock
order t2 localhost Core-A 127.0.0.1                                       # a selector contributes at its position
run t3 0 -- --config "$TMP/inv.toml" run --no-daemon --target CORE-A --site hq --target 'edge-*' show clock
order t3 Core-A edge-b                                                    # lowercase identity; glob at its position
run t4 0 -- run --no-daemon --target 127.0.0.1 --target localhost --exclude LOCALHOST show clock
order t4 127.0.0.1                                                        # --exclude removes a direct target
run t5 0 -- command --target localhost --target 127.0.0.1 show clock;    sent t5 "show clock"; order t5 localhost
run t6 0 -- command --tf "$TMP/tset" show clock;                         sent t6 "show clock"; order t6 localhost
run t7 0 -- --config "$TMP/inv.toml" command --site branch --exclude 'core-*' show clock;  order t7 edge-b
run t8 0 -- login 127.0.0.1 localhost;                                   order t8 127.0.0.1
run t9 4 -- command 127.0.0.1 --target localhost --address 127.0.0.1 show clock; code t9 management_address_scope_error; nothing_sent t9
run t10 5 -- run --no-daemon --target 127.0.0.1 --exclude 127.0.0.1 show clock; code t10 target_set_empty; nothing_sent t10
run t11 5 -- --config "$TMP/inv.toml" command --site nowhere show clock;  code t11 inventory_empty_selection; nothing_sent t11
run t12 0 -- command --target 127.0.0.1 --cmd "show clock" --format jsonl
grep -q '"candidate_count":1' "$TMP/out" || fail "t12: command record lacks candidate_count: $(head -c 300 "$TMP/out")"

echo "== target folders"
# hosts/: files in byte order; README, .hidden, and backups skipped; a
# subfolder is skipped by --tf and read by --tfr at its name's position.
mkdir -p "$TMP/hosts/sub/deep/deeper" "$TMP/hosts/.git"
printf 'localhost\n' >"$TMP/hosts/20-b.txt"; printf '127.0.0.1\n' >"$TMP/hosts/10-a.txt"
printf 'nothost\n' >"$TMP/hosts/README"; printf 'nothost\n' >"$TMP/hosts/x.bak"; printf 'nothost\n' >"$TMP/hosts/.hidden"; printf 'nothost\n' >"$TMP/hosts/.git/HEAD"
printf 'LOCALHOST\n' >"$TMP/hosts/sub/c.txt"; printf '127.0.0.1\n' >"$TMP/hosts/sub/deep/d.txt"; printf 'localhost\n' >"$TMP/hosts/sub/deep/deeper/e.txt"
run f1 0 -- run --no-daemon --tf "$TMP/hosts" show clock;                 order f1 127.0.0.1 localhost
run f2 0 -- run --no-daemon --tfr "$TMP/hosts" --target 127.0.0.2 show clock; order f2 127.0.0.1 localhost 127.0.0.2   # tree deduplicates to two, then the later target
run f3 0 -- command --tf "$TMP/hosts" show clock;                         sent f3 "show clock"; order f3 127.0.0.1
run f4 0 -- --set targets.recursion-max-depth=3 run --no-daemon --tfr "$TMP/hosts/sub" show clock; order f4 localhost 127.0.0.1
run f5 5 -- --set targets.recursion-max-depth=2 run --no-daemon --tfr "$TMP/hosts" show clock; code f5 target_source_depth_exceeded; nothing_sent f5
run f6 2 -- --set targets.recursion-max-depth=17 run --no-daemon --tfr "$TMP/hosts" show clock; code f6 config_value_out_of_range
: >"$TMP/empty.txt"
run f7 5 -- run --no-daemon --tf "$TMP/empty.txt" --target 127.0.0.1 show clock; code f7 target_source_empty; nothing_sent f7
run f8 0 -- --set 'targets.empty-source="warn"' run --no-daemon --tf "$TMP/empty.txt" --target 127.0.0.1 show clock
order f8 127.0.0.1; mentions f8 "yields no targets"
run f9 5 -- --set 'targets.empty-source="warn"' run --no-daemon --tf "$TMP/empty.txt" show clock; code f9 target_set_empty
run f10 5 -- run --no-daemon --tf "$TMP/nowhere" show clock;              code f10 target_source_missing; nothing_sent f10
run f11 5 -- login --tfr "$TMP/nowhere";                                   code f11 target_source_missing
mkfifo "$TMP/hosts/queue"
run f12 5 -- run --no-daemon --tf "$TMP/hosts" show clock;                code f12 target_source_special_file
rm -f "$TMP/hosts/queue"
run f13 4 -- run --no-daemon --tfr - show clock;                          code f13 target_source_stdin_invalid
printf '127.0.0.1\n' | HOME=$TMP/home NETUSER=u NETPASS=p "$KARVI" "${G[@]}" run --no-daemon --tf - show clock >"$TMP/out" 2>"$TMP/err" || fail "f14: --tf - exit $?"
order f14 127.0.0.1

echo "== output folder layout and permissions"
# A fresh state root: created subtrees, the day-folder layout, and the modes
# of folders and files karvi creates, independent of the umask.
R=$TMP/root-a; install -d -m 700 "$R"
GA=("${G[@]}"); GA[1]="basedir=\"$R\""
umask 077
HOME=$TMP/home NETUSER=u NETPASS=p "$KARVI" "${GA[@]}" run --no-daemon --target 127.0.0.1 show clock >"$TMP/out" 2>"$TMP/err" </dev/null || fail "p1: exit $?"
umask 022
job=$(sed -n 's/^! exit=.* artifacts=//p' "$TMP/out" | tail -1)
case "$job" in "$R"/jobs/[0-9][0-9][0-9][0-9][0-9][0-9]/[0-9][0-9][0-9][0-9][0-9][0-9]-[0-9][0-9][0-9][0-9][0-9][0-9]-[0-9a-z][0-9a-z]) ;; *) fail "p1: layout $job" ;; esac
[ "$(stat -c %a "$R/logs")" = 750 ] || fail "p1: logs $(stat -c %a "$R/logs")"
[ "$(stat -c %a "$R/socket")" = 700 ] && [ "$(stat -c %a "$R/state")" = 700 ] || fail "p1: socket/state not 0700"
[ "$(stat -c %a "$R/jobs")" = 750 ] && [ "$(stat -c %a "$(dirname "$job")")" = 750 ] && [ "$(stat -c %a "$job")" = 750 ] || fail "p1: folder modes $(stat -c %a "$R/jobs" "$(dirname "$job")" "$job" | tr '\n' ' ')"
for f in manifest.json summary.json commands.jsonl commands.txt failures.jsonl; do [ "$(stat -c %a "$job/$f")" = 640 ] || fail "p1: $f mode $(stat -c %a "$job/$f")"; done
# output.directory-mode 0700 applies to folders karvi creates; existing ones stay.
run p2 0 -- --set 'output.directory-mode="0700"' run --no-daemon --target 127.0.0.1 show clock
job=$(sed -n 's/^! exit=.* artifacts=//p' "$TMP/out" | tail -1)
[ "$(stat -c %a "$job")" = 700 ] || fail "p2: job folder $(stat -c %a "$job")"
[ "$(stat -c %a "$TMP/state/jobs")" = 750 ] || fail "p2: existing jobs subtree changed to $(stat -c %a "$TMP/state/jobs")"   # created 0750 by an earlier row; never changed
run p3 2 -- --set 'output.directory-mode="0755"' run --no-daemon --target 127.0.0.1 show clock; code p3 config_enum_value_invalid
# An existing group-readable root and a group-readable jobs subtree are accepted and never changed.
R=$TMP/root-b; install -d -m 750 "$R"; install -d -m 2770 "$R/jobs"
GB=("${G[@]}"); GB[1]="basedir=\"$R\""
HOME=$TMP/home NETUSER=u NETPASS=p "$KARVI" "${GB[@]}" run --no-daemon --target 127.0.0.1 show clock >"$TMP/out" 2>"$TMP/err" </dev/null || fail "p4: exit $? $(head -1 "$TMP/err")"
[ "$(stat -c %a "$R")" = 750 ] && [ "$(stat -c %a "$R/jobs")" = 2770 ] || fail "p4: existing root or jobs changed: $(stat -c %a "$R" "$R/jobs" | tr '\n' ' ')"
# An existing state subtree that is group-readable is refused.
R=$TMP/root-c; install -d -m 700 "$R"; install -d -m 750 "$R/state"
GC=("${G[@]}"); GC[1]="basedir=\"$R\""
rc=0; HOME=$TMP/home NETUSER=u NETPASS=p "$KARVI" "${GC[@]}" run --no-daemon --target 127.0.0.1 show clock >"$TMP/out" 2>"$TMP/err" </dev/null || rc=$?
[ "$rc" -eq 9 ] && grep -q '^private_directory_mode_exposed: ' "$TMP/err" || fail "p5: exit $rc $(head -1 "$TMP/err")"
# An existing day folder the operator cannot write to is refused with its own code.
if [ "$(id -u)" != 0 ]; then
  R=$TMP/root-d; install -d -m 700 "$R"; day=$(date +%y%m%d); install -d -m 500 "$R/jobs/$day"
  GD=("${G[@]}"); GD[1]="basedir=\"$R\""
  rc=0; HOME=$TMP/home NETUSER=u NETPASS=p "$KARVI" "${GD[@]}" run --no-daemon --target 127.0.0.1 show clock >"$TMP/out" 2>"$TMP/err" </dev/null || rc=$?
  [ "$rc" -eq 9 ] && grep -q '^output_directory_not_writable: ' "$TMP/err" || fail "p6: exit $rc $(head -1 "$TMP/err")"
  chmod 700 "$R/jobs/$day"
fi
# p7: the retention helper over root-a's tree (docs/PRUNE.md): a dry run as the operator walks the root's jobs and
# transcripts trees (and a scoreboard directory of its own, absent here, so
# the suite's scoreboards are not counted) and names p1's young job as kept,
# the summary line last on standard output and nothing on standard error,
# exit 0; a retention age below one is a usage error, exit 2. The helper is
# the one beside KARVI (a lab's bin, or the release's symlink).
PRUNE=${KARVI_PRUNE:-$(dirname "$KARVI")/karvi-prune}
R=$TMP/root-a
rc=0; "$PRUNE" --basedir "$R" --sharedroot none --scoreboards "$TMP/scoreboards" --dry-run --verbose >"$TMP/out" 2>"$TMP/err" || rc=$?
[ "$rc" -eq 0 ] || fail "p7: exit $rc $(head -1 "$TMP/err")"
[ ! -s "$TMP/err" ] || fail "p7: standard error holds $(head -1 "$TMP/err")"
grep -q "^walk kind=activity path=$R/jobs$" "$TMP/out" || fail "p7: the jobs tree is not walked"
grep -q "^kept kind=activity path=$R/jobs/[0-9]*/[0-9a-z-]* reason=young$" "$TMP/out" || fail "p7: p1's job is not kept as young: $(grep '^kept' "$TMP/out" | head -1)"
[ "$(tail -1 "$TMP/out")" = "examined=1 removed=0 skipped=0 not_owned=0 failed=0 bytes=0 dry_run=true" ] || fail "p7: the summary line is $(tail -1 "$TMP/out")"
rc=0; "$PRUNE" --basedir "$R" --sharedroot none --days 0 --dry-run >"$TMP/out" 2>"$TMP/err" || rc=$?
[ "$rc" -eq 2 ] && grep -q -- '--days takes one or more' "$TMP/err" || fail "p7: --days 0 exit $rc $(head -1 "$TMP/err")"
# p8: the same run as JSON documents: every line one
# document, the kept line and the walk line with their fields, the summary
# the last document with the counts of p7's text line; a format word that is
# neither text nor jsonl is a usage error, exit 2.
rc=0; "$PRUNE" --basedir "$R" --sharedroot none --scoreboards "$TMP/scoreboards" --dry-run --verbose --format jsonl >"$TMP/out" 2>"$TMP/err" || rc=$?
[ "$rc" -eq 0 ] && [ ! -s "$TMP/err" ] || fail "p8: exit $rc $(head -1 "$TMP/err")"
while IFS= read -r p8_line; do printf '%s\n' "$p8_line" >"$TMP/p8.json"; json_get "$TMP/p8.json" event >/dev/null || fail "p8: not a document: $p8_line"; done <"$TMP/out"
grep '"event":"kept"' "$TMP/out" | head -1 >"$TMP/p8.json"
json_is "$TMP/p8.json" kind activity && json_is "$TMP/p8.json" reason young || fail "p8: the kept document: $(cat "$TMP/p8.json")"
grep '"event":"walk"' "$TMP/out" | head -1 >"$TMP/p8.json"
json_is "$TMP/p8.json" path "$R/jobs" || fail "p8: the walk document: $(cat "$TMP/p8.json")"
tail -1 "$TMP/out" >"$TMP/p8.json"
json_is "$TMP/p8.json" event summary && json_is "$TMP/p8.json" examined 1 && json_is "$TMP/p8.json" removed 0 && json_is "$TMP/p8.json" dry_run true || fail "p8: the summary document: $(cat "$TMP/p8.json")"
rc=0; "$PRUNE" --basedir "$R" --sharedroot none --dry-run --format yaml >"$TMP/out" 2>"$TMP/err" || rc=$?
[ "$rc" -eq 2 ] && grep -q -- '--format takes text or jsonl' "$TMP/err" || fail "p8: --format yaml exit $rc $(head -1 "$TMP/err")"

echo "== dispatch order"
# Byte order: "127.0.0.1" < "localhost"; shuffle with key "k" is a fixed
# order; the manifest, summary, and command record carry the order.
run o1 0 -- run --no-daemon --target localhost --target 127.0.0.1 --order sorted show clock
order o1 127.0.0.1 localhost
run o2 0 -- --set 'dispatch.order="name"' run --no-daemon --target localhost --target 127.0.0.1 show clock
order o2 127.0.0.1 localhost
job=$(sed -n 's/^! exit=.* artifacts=//p' "$TMP/out" | tail -1)
json_is "$job/manifest.json" plan.dispatch.dispatch_order sorted || fail "o2: manifest lacks dispatch_order sorted (alias name)"
json_is "$job/summary.json" dispatch_order sorted || fail "o2: summary lacks dispatch_order"
json_has "$job/manifest.json" plan.dispatch.shuffle_key && fail "o2: shuffle_key present for sorted"
run o3 0 -- --set 'dispatch.order="shuffle"' --set 'dispatch.shuffle-key="k"' run --no-daemon --target 127.0.0.1 --target localhost show clock
job=$(sed -n 's/^! exit=.* artifacts=//p' "$TMP/out" | tail -1)
json_is "$job/manifest.json" plan.dispatch.dispatch_order shuffle && json_is "$job/manifest.json" plan.dispatch.shuffle_key k || fail "o3: manifest lacks shuffle order and key"
json_is "$job/summary.json" shuffle_key k || fail "o3: summary lacks shuffle_key"
grep -q '"dispatch_order":"shuffle"' "$job/commands.jsonl" && grep -q '"shuffle_key":"k"' "$job/commands.jsonl" || fail "o3: command record lacks order and key"
sed -n 's/^\([^ ]*\) \[.*/\1/p' "$TMP/out" >"$TMP/o3"
run o3b 0 -- --set 'dispatch.order="shuffle"' --set 'dispatch.shuffle-key="k"' run --no-daemon --target localhost --target 127.0.0.1 show clock
sed -n 's/^\([^ ]*\) \[.*/\1/p' "$TMP/out" | cmp -s - "$TMP/o3" || fail "o3b: the same key must give the same order whatever the input order"
run o4 0 -- run --no-daemon --target 127.0.0.1 --target localhost --order random show clock
job=$(sed -n 's/^! exit=.* artifacts=//p' "$TMP/out" | tail -1)
json_is "$job/manifest.json" plan.dispatch.dispatch_order random && json_get "$job/manifest.json" plan.dispatch.shuffle_key | grep -Eqx '[0-9]{10}' || fail "o4: random must record epoch seconds as the key"
run o5 0 -- command --target localhost --target 127.0.0.1 --order sorted --cmd "show clock" --format jsonl
grep -q '"dispatch_order":"sorted"' "$TMP/out" && grep -q '"candidate_count":2' "$TMP/out" || fail "o5: command record lacks order or candidate count"
grep -q '"canonical_name":"127.0.0.1"' "$TMP/out" || fail "o5: sorted must select 127.0.0.1 first"
run o6 2 -- --set 'dispatch.order="inventory"' run --no-daemon --target 127.0.0.1 show clock;     code o6 config_dispatch_order_inventory_removed; mentions o6 default
run o7 2 -- --set 'dispatch.order="random-seeded"' run --no-daemon --target 127.0.0.1 show clock; code o7 config_dispatch_order_random_seeded_removed; mentions o7 shuffle
run o8 2 -- --set 'dispatch.random-seed="k"' run --no-daemon --target 127.0.0.1 show clock;       code o8 config_unknown_key
run o9 4 -- login 127.0.0.1 --order inventory;                                                    code o9 cli_option_value_invalid

echo "== command text in debug output"
# Each command sent appears exactly once, as the operator provided it, in the
# device command start event; the transport events keep hash and length only.
run d1 0 -- command --debug 127.0.0.1 show  ip route
[ "$(grep -c 'DEBUG device command start .* command="show ip route"' "$TMP/err")" = 1 ] || fail "d1: command text not shown exactly once: $(grep -c 'command=' "$TMP/err")"
[ "$(grep -c 'show ip route' "$TMP/err")" = 1 ] || fail "d1: command text appears outside the start event"
printf 'show clock\n  show version  \n' >"$TMP/dcmds"
run d2 0 -- command --debug 127.0.0.1 --cf "$TMP/dcmds"
grep -q 'index=1 total=2 .* command="show clock"' "$TMP/err" && grep -q 'index=2 total=2 .* command="  show version  "' "$TMP/err" || fail "d2: --cf lines as written: $(grep 'command start' "$TMP/err" | tr '\n' '|')"
long=$(head -c 6000 /dev/zero | tr '\0' 'x')
run d3 0 -- command --debug 127.0.0.1 --cmd "show $long"
grep -q 'command="show xxxx' "$TMP/err" && grep -q '\.\.\.$' "$TMP/err" || fail "d3: long command not bounded with a marker"
[ "$(awk '{ if (length($0) > m) m = length($0) } END { print m }' "$TMP/err")" -le 4096 ] || fail "d3: debug line exceeds the bound"
run d4 0 -- login --debug 127.0.0.1
! grep -q 'command=' "$TMP/err" || fail "d4: login debug shows command text"

echo "== parser codes"
run c1 4 -- bogus;                                  code c1 cli_command_unknown
run c2 4 -- daemon;                                 code c2 cli_subcommand_missing
run c3 4 -- daemon bounce;                          code c3 cli_subcommand_unknown
run c4 4 -- daemon s;                               code c4 cli_subcommand_ambiguous; mentions c4 start; mentions c4 serve; mentions c4 status; mentions c4 stop
run c5 4 -- command --bogus 127.0.0.1 show clock;   code c5 cli_option_unknown
run c6 4 -- command 127.0.0.1 --port;               code c6 cli_option_value_missing
run c7 4 -- command 127.0.0.1 --port abc show clock; code c7 cli_option_value_invalid
run c8 4 -- command 127.0.0.1 --format xml show clock; code c8 cli_option_value_invalid; mentions c8 "text, jsonl, or json"
run c9 4 -- command --cmd "show clock";             code c9 cli_device_missing
run c10 4 -- command "" show clock;                 code c10 cli_device_empty
run c11 4 -- command 127.0.0.1;                     code c11 cli_command_text_missing
run c12 4 -- command 127.0.0.1 --cmd x show clock;  code c12 cli_command_text_mixed
run c13 4 -- version extra;                         code c13 cli_positional_unexpected
run c14 4 -- command 127.0.0.1 --tfr - show clock;  code c14 target_source_stdin_invalid
run c15 4 -- command 127.0.0.1 --port 70000 show clock; code c15 port_out_of_range
run c16 4 -- --debug-show-secrets version;          code c16 debug_show_secrets_requires_debug
run c17 4 -- command -4 -6 127.0.0.1 show clock;    code c17 address_family_conflict
run c18 4 -- command --border --noborder 127.0.0.1 show clock; code c18 border_options_conflict
run c19 4 -- login --all=false;                     code c19 cli_device_missing
run c20 4 -- config generate --minimal --full;      code c20 config_generate_mode_conflict
run c21 0 -- config validate --format json;          grep -q '"valid": *true' "$TMP/out" || fail "c21: $(cat "$TMP/out")"
run c22 0 -- config sh --format json --explain basedir; grep -q basedir "$TMP/out" || fail "c22"
run c23 0 -- version --format json;                  grep -q '"version"' "$TMP/out" || fail "c23"

if [ "$failures" -ne 0 ]; then
  echo "k03-smoke-test: $failures failure(s)" >&2
  exit 1
fi
echo "k03-smoke-test: ok"
