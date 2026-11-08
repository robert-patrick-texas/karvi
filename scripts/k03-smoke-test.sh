#!/bin/bash
# K03 parser acceptance (the command rows, aliases, spellings, renames,
# and parser codes) against the prebuilt karvi binary and a fake ssh that logs
# exactly what reaches the device. Runs without a network.
set -eu
ROOT=$(cd "$(dirname "$0")/.." && pwd)
KARVI=${KARVI:-$ROOT/bin/karvi-linux-amd64}
. "$ROOT/scripts/lib/json.sh"
. "$ROOT/scripts/lib/host.sh"; host_config_clean "$KARVI"   # refused on a host whose configuration reaches karvi
TMP=$(mktemp -d "${TMPDIR:-/tmp}/karvi-k03-smoke-XXXXXX")
host_trust_store "$TMP"   # the trust store under the work directory, never the operator's
cleanup() { rm -rf "$TMP"; }
. "$ROOT/scripts/lib/exit.sh"; . "$ROOT/scripts/lib/lab.sh"; on_exit cleanup
install -d -m 700 "$TMP/home" "$TMP/state" "$TMP/state/scoreboards" "$TMP/state/capacity" "$TMP/streams"
lab_places "$TMP/places.toml" "$TMP/state"
# shellcheck disable=SC2034 # read by scripts/lib/rows.sh
row_dir=$TMP/streams; . "$ROOT/scripts/lib/rows.sh"
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
G=(--config "$TMP/places.toml" --set 'platform-resolution.default=""' --set "ssh.transports.system=\"$FAKE\"" --set 'ssh.command.transport="system"'
   --set display.color=never
   --set 'ssh.host-key-policy="insecure"' --set 'ssh.run.transport="system"')

# kv ARGS: karvi with the rows' home and device credentials; k ARGS: kv under
# the suite's list (G), the device log emptied first, the command of each
# row: `row NAME WANT -- k ARGS` records the exit, and the row's checks read
# its streams ("$row_out", "$row_err") and the device log.
kv() { HOME=$TMP/home NETUSER=u NETPASS=p "$KARVI" "$@"; }
k() { : >"$FAKE.log"; kv "${G[@]}" "$@"; }
sent() { # sent NAME LINE...  device received exactly these lines in order
  local name=$1; shift
  printf '%s\n' "$@" | sed 's/^/SENT:/' >"$TMP/want"
  [ -f "$FAKE.log" ] || : >"$FAKE.log"
  cmp -s "$TMP/want" "$FAKE.log" || fail "$name: device received $(tr '\n' '|' <"$FAKE.log"), want $(tr '\n' '|' <"$TMP/want")"
}
nothing_sent() { [ ! -s "$FAKE.log" ] || fail "$1: device received $(tr '\n' '|' <"$FAKE.log"), want nothing"; }
# shellcheck disable=SC2154 # row_out and row_err: the last row's streams, set by scripts/lib/rows.sh
code() { # code NAME CODE  stderr starts with the code, stdout is empty
  grep -q "^$2: " "$row_err" || fail "$1: stderr $(head -1 "$row_err"), want code $2"
  [ ! -s "$row_out" ] || fail "$1: stdout must be empty"
}
mentions() { grep -q -- "$2" "$row_err" || fail "$1: stderr lacks $2: $(head -1 "$row_err")"; }
echoed() { grep -q "^127.0.0.1#$2\$" "$row_out" || fail "$1: no echo of $2"; }
not_echoed() { ! grep -q "^127.0.0.1#" "$row_out" || fail "$1: unexpected echo"; }
helped() { grep -q '^Usage' "$row_out" || fail "$1: no help on stdout"; [ ! -s "$row_err" ] || fail "$1: stderr not empty"; }

echo "== the parser acceptance rows"
row row1 0 -- k com --ech 127.0.0.1 show ip route;                          sent row1 "show ip route"; echoed row1 "show ip route"
row row2 0 -- k command 127.0.0.1 --echo show ip route;                     sent row2 "show ip route"; echoed row2 "show ip route"
row row3 0 -- k command 127.0.0.1 show ip route --echo;                     sent row3 "show ip route --echo"; not_echoed row3
row row4 0 -- k command 127.0.0.1 traceroute -4 192.0.2.1;                  sent row4 "traceroute -4 192.0.2.1"
row row5 0 -- k command 127.0.0.1 show run '|' grep -e bgp;                 sent row5 "show run | grep -e bgp"
row row6 0 -- k command 127.0.0.1 show interfaces '|' grep -h up;           sent row6 "show interfaces | grep -h up"; ! grep -q '^Usage' "$row_out" || fail "row6: help printed"
row row7 0 -- k command --echo 127.0.0.1 "show ip route" "show clock";      sent row7 "show ip route show clock"
row row8 0 -- k command --echo 127.0.0.1 --cmd "show ip route" --cmd "show clock"; sent row8 "show ip route" "show clock"
row row9 0 -- k command --targ 127.0.0.1 --command "show ip route" --ech;   sent row9 "show ip route"; echoed row9 "show ip route"
row row10 4 -- k command --tar 127.0.0.1 --co "show ip route";              nothing_sent row10; code row10 cli_option_ambiguous; mentions row10 -- "--cmd (as --command)"; mentions row10 -- "--continue-device-on-error"
row row11 4 -- k co 127.0.0.1 show clock;                                   nothing_sent row11; code row11 cli_command_ambiguous; mentions row11 command; mentions row11 config
row row12 0 -- k command 127.0.0.1 show clock --cmd "show version";         sent row12 "show clock --cmd show version"
row row13 0 -- k command --echo 127.0.0.1 -- -example device text;          sent row13 "-example device text"
row row14 0 -- k run --no-daemon --host 127.0.0.1 show clock --echo;        sent row14 "show clock --echo"
row row15 0 -- k run --no-daemon --targ 127.0.0.1 show clock --echo;        sent row15 "show clock --echo"

echo "== aliases and spellings"
row a1 0 -- k command -t 127.0.0.1 -c "show clock";                         sent a1 "show clock"
row a2 0 -- k command --t 127.0.0.1 --c "show clock";                       sent a2 "show clock"
row a3 0 -- k command -a 127.0.0.1 -t 127.0.0.1 -c "show clock";            sent a3 "show clock"
row a4 0 -- k command -echo -target 127.0.0.1 show clock;                   sent a4 "show clock"; echoed a4 "show clock"
row a5 0 -- k command -ech=true -tar 127.0.0.1 show clock;                  sent a5 "show clock"; echoed a5 "show clock"
row a6 0 -- k command --echo=false 127.0.0.1 show clock;                    sent a6 "show clock"; not_echoed a6
row a8 0 -- k command 127.0.0.1 -h;                                          nothing_sent a8; helped a8
row a9 0 -- k cmd --h;                                                       helped a9
row a10 0 -- k -h;                                                           helped a10
row a11 0 -- k login 127.0.0.1 --help;                                       helped a11
row a12 0 -- k run --no-daemon -t 127.0.0.1 -t 127.0.0.1 --c "show clock";   sent a12 "show clock"

echo "== renamed and removed spellings"
row r1 4 -- k run --targets-file x show clock;                               code r1 cli_option_unknown
row r2 4 -- k run --targets x show clock;                                    code r2 cli_option_unknown
row r3 4 -- k command --commands-file x 127.0.0.1;                           code r3 cli_option_unknown
row r4 4 -- k command --host-key-policy insecure 127.0.0.1 show clock;       code r4 cli_option_unknown
row r5 4 -- k command --known-hosts-file auto 127.0.0.1 show clock;          code r5 cli_option_unknown
row r6 4 -- k login 127.0.0.1 --record-file x;                               code r6 cli_option_unknown
row r7 0 -- k command --ssh-host-key-policy insecure --ssh-known-hosts-file auto 127.0.0.1 show clock; sent r7 "show clock"
printf 'show clock\n\n# comment\nshow version\r\n' >"$TMP/cmds"
row r8 0 -- k command 127.0.0.1 --cf "$TMP/cmds";                            sent r8 "show clock" "" "# comment" "show version"
row r9 0 -- k command 127.0.0.1 --cf "$TMP/cmds" --cmd "show users" --cf "$TMP/cmds"; sent r9 "show clock" "" "# comment" "show version" "show users" "show clock" "" "# comment" "show version"
: >"$TMP/empty"
row r10 4 -- k command 127.0.0.1 --cf "$TMP/empty";                          code r10 commands_file_empty
row r11 5 -- k command 127.0.0.1 --cf "$TMP";                                code r11 commands_file_missing
install -d -m 700 "$TMP/lists"; printf 'show users\n' >"$TMP/lists/127-0-0-1"
row r11b 0 -- k command 127.0.0.1 --cmd "show clock" --cf "$TMP/lists";      sent r11b "show clock" "show users"
printf '127.0.0.1\n' >"$TMP/targets"
row r12 0 -- k run --no-daemon --tf "$TMP/targets" show clock;               sent r12 "show clock"

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
  sed -n 's/^! \([^ ]*\) \[.*/\1/p' "$row_out" >"$TMP/got"
  cmp -s "$TMP/want" "$TMP/got" || fail "$name: devices $(tr '\n' '|' <"$TMP/got"), want $(tr '\n' '|' <"$TMP/want")"
}
printf 'LOCALHOST\n# comment\n\n127.0.0.1\n' >"$TMP/tset"
row t1 0 -- k run --no-daemon --target 127.0.0.1 --tf "$TMP/tset" --target localhost show clock
order t1 127.0.0.1 localhost                                              # order kept; LOCALHOST folded and deduplicated
row t2 0 -- k --config "$TMP/inv.toml" run --no-daemon --target localhost --site hq --target 127.0.0.1 show clock
order t2 localhost Core-A 127.0.0.1                                       # a selector contributes at its position
row t3 0 -- k --config "$TMP/inv.toml" run --no-daemon --target CORE-A --site hq --target 'edge-*' show clock
order t3 Core-A edge-b                                                    # lowercase identity; glob at its position
row t4 0 -- k run --no-daemon --target 127.0.0.1 --target localhost --exclude LOCALHOST show clock
order t4 127.0.0.1                                                        # --exclude removes a direct target
row t5 0 -- k command --target localhost --target 127.0.0.1 show clock;    sent t5 "show clock"; order t5 localhost
row t6 0 -- k command --tf "$TMP/tset" show clock;                         sent t6 "show clock"; order t6 localhost
row t7 0 -- k --config "$TMP/inv.toml" command --site branch --exclude 'core-*' show clock;  order t7 edge-b
row t8 0 -- k login 127.0.0.1 localhost;                                   order t8 127.0.0.1
row t9 4 -- k command 127.0.0.1 --target localhost --address 127.0.0.1 show clock; code t9 management_address_scope_error; nothing_sent t9
row t10 5 -- k run --no-daemon --target 127.0.0.1 --exclude 127.0.0.1 show clock; code t10 target_set_empty; nothing_sent t10
row t11 5 -- k --config "$TMP/inv.toml" command --site nowhere show clock;  code t11 inventory_empty_selection; nothing_sent t11
row t12 0 -- k command --target 127.0.0.1 --cmd "show clock" --format jsonl
[ "$(jsonl_records "$row_out" candidate_count)" = 1 ] || fail "t12: command record lacks candidate_count: $(head -c 300 "$row_out")"

echo "== target folders"
# hosts/: files in byte order; README, .hidden, and backups skipped; a
# subfolder is skipped by --tf and read by --tfr at its name's position.
mkdir -p "$TMP/hosts/sub/deep/deeper" "$TMP/hosts/.git"
printf 'localhost\n' >"$TMP/hosts/20-b.txt"; printf '127.0.0.1\n' >"$TMP/hosts/10-a.txt"
printf 'nothost\n' >"$TMP/hosts/README"; printf 'nothost\n' >"$TMP/hosts/x.bak"; printf 'nothost\n' >"$TMP/hosts/.hidden"; printf 'nothost\n' >"$TMP/hosts/.git/HEAD"
printf 'LOCALHOST\n' >"$TMP/hosts/sub/c.txt"; printf '127.0.0.1\n' >"$TMP/hosts/sub/deep/d.txt"; printf 'localhost\n' >"$TMP/hosts/sub/deep/deeper/e.txt"
row f1 0 -- k run --no-daemon --tf "$TMP/hosts" show clock;                 order f1 127.0.0.1 localhost
row f2 0 -- k run --no-daemon --tfr "$TMP/hosts" --target 127.0.0.2 show clock; order f2 127.0.0.1 localhost 127.0.0.2   # tree deduplicates to two, then the later target
row f3 0 -- k command --tf "$TMP/hosts" show clock;                         sent f3 "show clock"; order f3 127.0.0.1
row f4 0 -- k --set targets.recursion-max-depth=3 run --no-daemon --tfr "$TMP/hosts/sub" show clock; order f4 localhost 127.0.0.1
row f5 5 -- k --set targets.recursion-max-depth=2 run --no-daemon --tfr "$TMP/hosts" show clock; code f5 target_source_depth_exceeded; nothing_sent f5
row f6 2 -- k --set targets.recursion-max-depth=17 run --no-daemon --tfr "$TMP/hosts" show clock; code f6 config_value_out_of_range
: >"$TMP/empty.txt"
row f7 5 -- k run --no-daemon --tf "$TMP/empty.txt" --target 127.0.0.1 show clock; code f7 target_source_empty; nothing_sent f7
row f8 0 -- k --set 'targets.empty-source="warn"' run --no-daemon --tf "$TMP/empty.txt" --target 127.0.0.1 show clock
order f8 127.0.0.1; mentions f8 "yields no targets"
row f9 5 -- k --set 'targets.empty-source="warn"' run --no-daemon --tf "$TMP/empty.txt" show clock; code f9 target_set_empty
row f10 5 -- k run --no-daemon --tf "$TMP/nowhere" show clock;              code f10 target_source_missing; nothing_sent f10
row f11 5 -- k login --tfr "$TMP/nowhere";                                   code f11 target_source_missing
mkfifo "$TMP/hosts/queue"
row f12 5 -- k run --no-daemon --tf "$TMP/hosts" show clock;                code f12 target_source_special_file
rm -f "$TMP/hosts/queue"
row f13 4 -- k run --no-daemon --tfr - show clock;                          code f13 target_source_stdin_invalid
piped() { # piped NAME WANT INPUT -- ARGS...  a row with INPUT on standard input
  local name=$1 want=$2; k_input=$3; shift 4
  row "$name" "$want" -- k_piped "$@"
}
k_piped() { printf '%b' "$k_input" | k "$@"; }
piped f14 0 '127.0.0.1\n' -- run --no-daemon --tf - show clock;          order f14 127.0.0.1
# A device of - reads standard input as --tf - does, at its position.
piped f15 0 '127.0.0.1\n' -- run --no-daemon -t - show clock;            order f15 127.0.0.1
piped f16 0 'localhost\n' -- run --no-daemon -tl=127.0.0.1,- show clock; order f16 127.0.0.1 localhost
piped f17 0 '# c\nlocalhost\n127.0.0.1\n' -- command - show clock;     order f17 localhost
piped f18 4 '127.0.0.1\n' -- run --no-daemon -t=- --tf - show clock;     code f18 stdin_source_repeated; nothing_sent f18
row f19 4 -- k job list --target -;                                          code f19 target_source_stdin_invalid

echo "== numeric ranges and the inventory option"
# rng.csv: router01, 02, 03, 05, and 10 at the fake device, at sites pod1,
# pod2, pod10, pod12, and pod12; more.csv: box01 and box02, a file for --inv
# outside every configured source; creds.csv: a credential row holding a range
# ahead of a glob row. A created name the inventory lacks is a direct target,
# which only a dry run plans here (DNS knows no such name).
printf 'name,management_address,platform,site\nrouter01,127.0.0.1,generic,pod1\nrouter02,127.0.0.1,generic,pod2\nrouter03,127.0.0.1,generic,pod10\nrouter05,127.0.0.1,generic,pod12\nrouter10,127.0.0.1,generic,pod12\n' >"$TMP/rng.csv"
printf '[[inventory-source]]\nname = "rng"\ntype = "csv"\npath = "%s"\nrequired = true\n' "$TMP/rng.csv" >"$TMP/rng.toml"
printf 'name,management_address,platform\nbox01,127.0.0.1,generic\nbox02,127.0.0.1,generic\n' >"$TMP/more.csv"
printf 'device_name,username,password\nrouter[10-12],svc.edge,lab-placeholder\nrouter*,svc.other,lab-placeholder\n' >"$TMP/creds.csv"
printf '[credential-backend.rng-csv]\ntype = "csv"\nscope = "user"\npath = "%s"\nrequired = true\n[credential-policy.default]\nbackend-sequence = ["rng-csv"]\n[[credential-policy-map]]\npolicy = "default"\nname = "*"\n' "$TMP/creds.csv" >"$TMP/creds.toml"
chmod 600 "$TMP/rng.toml" "$TMP/creds.csv" "$TMP/creds.toml"
planned() { # planned NAME DEVICE...  the dry run plans these devices in this order
  local name=$1; shift
  printf '%s\n' "$@" >"$TMP/want"
  sed -n 's/^- name:\(.*\): planned$/\1/p' "$row_out" >"$TMP/got"
  cmp -s "$TMP/want" "$TMP/got" || fail "$name: planned $(tr '\n' '|' <"$TMP/got"), want $(tr '\n' '|' <"$TMP/want")"
}
bound() { # bound NAME DEVICE=USER...  the dry run binds each device's credential to USER, in this order
  local name=$1; shift
  printf '%s\n' "$@" >"$TMP/want"
  sed -n -e 's/^- name:\(.*\): planned$/\1/p' -e 's/^  credential: bound .* user=\([^;]*\);.*/=\1/p' "$row_out" | paste -d '' - - >"$TMP/got"
  cmp -s "$TMP/want" "$TMP/got" || fail "$name: bound $(tr '\n' '|' <"$TMP/got"), want $(tr '\n' '|' <"$TMP/want")"
}
row n1 0 -- k --config "$TMP/rng.toml" run --no-daemon --tl 'router0[1-3]' show clock;  order n1 router01 router02 router03
row n2 0 -- k run --no-daemon --dry-run --tl 'switch0[1-2],router[1,10]' show clock;    planned n2 switch01 switch02 router1 router10   # created in written order, direct
row n3 0 -- k --config "$TMP/rng.toml" run --no-daemon --site 'pod[10-12]' show clock;  order n3 router03 router05 router10           # a range, not a class: pod1 and pod2 not matched
row n4 4 -- k run --no-daemon --target 'box[1-2001]' show clock;                        code n4 target_created_names_exceeded; mentions n4 "with --i it selects from the inventory"; nothing_sent n4
row n5 0 -- k --config "$TMP/rng.toml" run --no-daemon --i --tl 'router0[1-5]' show clock; order n5 router01 router02 router03 router05  # under --i a range selects
row n6 5 -- k --config "$TMP/rng.toml" run --no-daemon --i --tl router01,router04 show clock; code n6 target_not_in_inventory; mentions n6 router04; nothing_sent n6
row n7 0 -- k --config "$TMP/rng.toml" run --no-daemon -i="$TMP/more.csv" --tl 'box0[1-3]' show clock; order n7 box01 box02
row n8 5 -- k --config "$TMP/rng.toml" run --no-daemon --inv="$TMP/more.csv" --tl router01 show clock; code n8 target_not_in_inventory; nothing_sent n8   # the configured source unread
row n9 5 -- k run --no-daemon --inventory="$TMP/none.csv" --tl box01 show clock;       code n9 inventory_missing; nothing_sent n9
row n10 4 -- k run --no-daemon --inv= --tl box01 show clock;                            code n10 cli_option_value_missing
row n11 4 -- k run --no-daemon --inv "$TMP/more.csv" --tl box01 show clock;             code n11 cli_option_value_detached
row n12 0 -- k command --inv="$TMP/more.csv" box02 show clock;                           sent n12 "show clock"; order n12 box02
row n13 0 -- k login --inv="$TMP/more.csv" box01;                                        order n13 box01
row n14 0 -- k --config "$TMP/rng.toml" --config "$TMP/creds.toml" run --no-daemon --dry-run --tl router10,router02 show clock; bound n14 router10=svc.edge router02=svc.other

echo "== sleep between commands"
# --sleep waits before the next command (EXAMPLES chapter 95), on the command
# line and as a --cf line alone; --literal sends the line as text; a sleep
# with no command after it, a declaration after it, a line beginning --sleep
# that is not one, and sleeps above the device timeout are refused, nothing
# sent.
printf 'show clock\n--sleep 1s\nshow users\n' >"$TMP/sleep.txt"
printf 'show clock\n--sleep soon\nshow users\n' >"$TMP/sleep-bad.txt"
t0=$(date +%s%N)
row s1 0 -- k command 127.0.0.1 --cmd "show version" --sleep 1s --cf "$TMP/sleep.txt"; sent s1 "show version" "show clock" "show users"
t1=$(date +%s%N); [ $(((t1 - t0) / 1000000)) -ge 2000 ] || fail "s1: took $(((t1 - t0) / 1000000))ms, the sleeps are 2s"
row s2 0 -- k command 127.0.0.1 --literal --cf "$TMP/sleep.txt";              sent s2 "show clock" "--sleep 1s" "show users"
row s3 4 -- k command 127.0.0.1 --cmd "show clock" --sleep 1s;                code s3 sleep_without_command; nothing_sent s3
row s4 4 -- k command 127.0.0.1 --cmd "show clock" --sleep 1s --timeout 5s --cmd "show users"; code s4 declaration_after_sleep; nothing_sent s4
row s5 4 -- k command 127.0.0.1 --cf "$TMP/sleep-bad.txt";                    code s5 commands_file_sleep_invalid; nothing_sent s5
row s6 4 -- k --set 'execution.device-timeout="5s"' command 127.0.0.1 --cmd "show clock" --sleep 3s --sleep 3s --cmd "show users"; code s6 sleep_over_device_timeout; nothing_sent s6

echo "== root commands"
# --sudo TEXT is a command source run as root (EXAMPLES chapter 96), on a
# platform on the exec channel alone: on this device's shell channel it is
# refused at planning, from the command line and as a --cf line alike;
# --literal sends the line as text; a lone --sudo's unquoted words are its
# command, refused here as u1's; unquoted words beside another command, a
# blank --sudo, and a --cf line --sudo with no command are refused, nothing
# sent; a dry run on linux lists the root commands and the device's password
# source, the lone --sudo's words one root command, and after --sudo -- every
# word the root command; a command's own option right after its first word
# is refused, naming both fixes.
printf 'show clock\n--sudo id -u\n' >"$TMP/sudo.txt"
printf 'show clock\n--sudo\n' >"$TMP/sudo-bare.txt"
row u1 4 -- k command 127.0.0.1 --cmd "show clock" --sudo "id -u";           code u1 channel_shell_sudo_refused; nothing_sent u1
row u2 4 -- k command 127.0.0.1 --cf "$TMP/sudo.txt";                        code u2 channel_shell_sudo_refused; nothing_sent u2
row u3 0 -- k command 127.0.0.1 --literal --cf "$TMP/sudo.txt";              sent u3 "show clock" "--sudo id -u"
row u4 4 -- k command 127.0.0.1 --sudo systemctl restart nginx;              code u4 channel_shell_sudo_refused; mentions u4 'command 1 is a --sudo command'; nothing_sent u4
row u8 4 -- k command 127.0.0.1 --cmd whoami --sudo systemctl restart nginx; code u8 cli_command_text_mixed; mentions u8 'freeform text "restart nginx" joins only'; nothing_sent u8
row u5 4 -- k command 127.0.0.1 --sudo "";                                   code u5 cli_option_value_invalid; nothing_sent u5
row u6 4 -- k command 127.0.0.1 --cf "$TMP/sudo-bare.txt";                   code u6 commands_file_sudo_invalid; nothing_sent u6
row u7 0 -- k --set 'platform.linux.fallback=["netvars"]' run --dry-run --no-daemon --platform linux --target 127.0.0.1 --cmd uptime --sudo "systemctl restart nginx"; nothing_sent u7
grep -qx 'sudo: commands 2' "$row_out" || fail "u7: no top sudo line"
grep -qx '  sudo: none, sudo -n' "$row_out" || fail "u7: no device sudo line"
row u9 0 -- k --set 'platform.linux.fallback=["netvars"]' run --dry-run --no-daemon --platform linux --target 127.0.0.1 --sudo systemctl restart nginx; nothing_sent u9
grep -qx 'commands: 1' "$row_out" || fail "u9: not one command"
grep -qx 'sudo: commands 1' "$row_out" || fail "u9: not one root command"
row u10 0 -- k --set 'platform.linux.fallback=["netvars"]' run --dry-run --no-daemon --platform linux --target 127.0.0.1 --cmd uptime --sudo -- journalctl -u nginx; nothing_sent u10
grep -qx 'commands: 2' "$row_out" || fail "u10: not two commands"
grep -qx 'sudo: commands 2' "$row_out" || fail "u10: the dashed command is not root"
row u11 4 -- k command 127.0.0.1 --sudo id -u;                               code u11 cli_option_unknown; mentions u11 'write --sudo -- TEXT, or quote the command'; nothing_sent u11

echo "== output folder layout and permissions"
# A fresh state root: created subtrees, the day-folder layout, and the modes
# of folders and files karvi creates, independent of the umask.
R=$TMP/root-a; install -d -m 700 "$R"
GA=("${G[@]}" --set "basedir=\"$R\"")  # the row's own basedir
umask 077
row p1 0 -- kv "${GA[@]}" run --no-daemon --target 127.0.0.1 show clock
umask 022
job=$(sed -n 's/^! exit=.* artifacts=//p' "$row_out" | tail -1)
case "$job" in "$R"/jobs/[0-9][0-9][0-9][0-9][0-9][0-9]/[0-9][0-9][0-9][0-9][0-9][0-9]-[0-9][0-9][0-9][0-9][0-9][0-9]-[0-9a-z][0-9a-z]) ;; *) fail "p1: layout $job" ;; esac
[ "$(stat -c %a "$R/logs")" = 750 ] || fail "p1: logs $(stat -c %a "$R/logs")"
[ "$(stat -c %a "$R/socket")" = 700 ] && [ "$(stat -c %a "$R/state")" = 700 ] || fail "p1: socket/state not 0700"
[ "$(stat -c %a "$R/jobs")" = 750 ] && [ "$(stat -c %a "$(dirname "$job")")" = 750 ] && [ "$(stat -c %a "$job")" = 750 ] || fail "p1: folder modes $(stat -c %a "$R/jobs" "$(dirname "$job")" "$job" | tr '\n' ' ')"
for f in manifest.json summary.json commands.jsonl commands.txt errors.jsonl; do [ "$(stat -c %a "$job/$f")" = 640 ] || fail "p1: $f mode $(stat -c %a "$job/$f")"; done
# output.directory-mode 0700 applies to folders karvi creates; existing ones stay.
row p2 0 -- k --set 'output.directory-mode="0700"' run --no-daemon --target 127.0.0.1 show clock
job=$(sed -n 's/^! exit=.* artifacts=//p' "$row_out" | tail -1)
[ "$(stat -c %a "$job")" = 700 ] || fail "p2: job folder $(stat -c %a "$job")"
[ "$(stat -c %a "$TMP/state/jobs")" = 750 ] || fail "p2: existing jobs subtree changed to $(stat -c %a "$TMP/state/jobs")"   # created 0750 by an earlier row; never changed
row p3 2 -- k --set 'output.directory-mode="0755"' run --no-daemon --target 127.0.0.1 show clock; code p3 config_enum_value_invalid
# An existing group-readable root and a group-readable jobs subtree are accepted and never changed.
R=$TMP/root-b; install -d -m 750 "$R"; install -d -m 2770 "$R/jobs"
GB=("${G[@]}" --set "basedir=\"$R\"")  # the row's own basedir
row p4 0 -- kv "${GB[@]}" run --no-daemon --target 127.0.0.1 show clock
[ "$(stat -c %a "$R")" = 750 ] && [ "$(stat -c %a "$R/jobs")" = 2770 ] || fail "p4: existing root or jobs changed: $(stat -c %a "$R" "$R/jobs" | tr '\n' ' ')"
# An existing state subtree that is group-readable is refused.
R=$TMP/root-c; install -d -m 700 "$R"; install -d -m 750 "$R/state"
GC=("${G[@]}" --set "basedir=\"$R\"")  # the row's own basedir
row p5 9 -- kv "${GC[@]}" run --no-daemon --target 127.0.0.1 show clock
grep -q '^private_directory_mode_exposed: ' "$row_err" || fail "p5: $(head -1 "$row_err")"
# An existing day folder the operator cannot write to is refused with its own code.
if [ "$(id -u)" != 0 ]; then
  R=$TMP/root-d; install -d -m 700 "$R"; day=$(date +%y%m%d); install -d -m 500 "$R/jobs/$day"
  GD=("${G[@]}" --set "basedir=\"$R\"")  # the row's own basedir
  row p6 9 -- kv "${GD[@]}" run --no-daemon --target 127.0.0.1 show clock
  grep -q '^output_directory_not_writable: ' "$row_err" || fail "p6: $(head -1 "$row_err")"
  chmod 700 "$R/jobs/$day"
fi
# p7: the retention helper over root-a's tree (docs/PRUNE.md): a dry run as the operator walks the root's jobs and
# transcripts trees (and a scoreboard directory of its own, absent here, so
# the suite's scoreboards are not counted) and names p1's young job as kept,
# the summary line last on standard output and nothing on standard error,
# exit 0; a retention age below one is a usage error, exit 2. The helper is
# the one beside KARVI (a lab's bin, or the release's symlink). Its runs set
# no free-space floor (--minfree 0), so the host's disk never decides what
# a dry run names.
PRUNE=${KARVI_PRUNE:-$(dirname "$KARVI")/karvi-prune}
R=$TMP/root-a
row p7 0 -- "$PRUNE" --basedir "$R" --sharedroot none --scoreboards "$TMP/scoreboards" --minfree 0 --dry-run --verbose
[ ! -s "$row_err" ] || fail "p7: standard error holds $(head -1 "$row_err")"
grep -q "^walk kind=activity path=$R/jobs$" "$row_out" || fail "p7: the jobs tree is not walked"
grep -q "^kept kind=activity path=$R/jobs/[0-9]*/[0-9a-z-]* reason=young$" "$row_out" || fail "p7: p1's job is not kept as young: $(grep '^kept' "$row_out" | head -1)"
[ "$(tail -1 "$row_out")" = "examined=1 removed=0 skipped=0 not_owned=0 failed=0 bytes=0 dry_run=true" ] || fail "p7: the summary line is $(tail -1 "$row_out")"
row p7b 2 -- "$PRUNE" --basedir "$R" --sharedroot none --days 0 --dry-run
grep -q -- '--days takes one to 999' "$row_err" || fail "p7b: --days 0: $(head -1 "$row_err")"
# p8: the same run as JSON documents: every line one
# document, the kept line and the walk line with their fields, the summary
# the last document with the counts of p7's text line; a format word that is
# neither text nor jsonl is a usage error, exit 2.
row p8 0 -- "$PRUNE" --basedir "$R" --sharedroot none --scoreboards "$TMP/scoreboards" --minfree 0 --dry-run --verbose --format jsonl
[ ! -s "$row_err" ] || fail "p8: standard error holds $(head -1 "$row_err")"
while IFS= read -r p8_line; do printf '%s\n' "$p8_line" >"$TMP/p8.json"; json_get "$TMP/p8.json" event >/dev/null || fail "p8: not a document: $p8_line"; done <"$row_out"
p8_kept=$(jsonl_lines event "$row_out" event kind reason | awk -F"$TAB" '$1 == "kept" { print $2 " " $3; exit }')
[ "$p8_kept" = "activity young" ] || fail "p8: the kept document's kind and reason: ${p8_kept:-no kept document}"
p8_walk=$(jsonl_lines event "$row_out" event path | awk -F"$TAB" '$1 == "walk" { print $2; exit }')
[ "$p8_walk" = "$R/jobs" ] || fail "p8: the walk document's path: ${p8_walk:-no walk document}"
tail -1 "$row_out" >"$TMP/p8.json"
json_is "$TMP/p8.json" event summary && json_is "$TMP/p8.json" examined 1 && json_is "$TMP/p8.json" removed 0 && json_is "$TMP/p8.json" dry_run true || fail "p8: the summary document: $(cat "$TMP/p8.json")"
row p8b 2 -- "$PRUNE" --basedir "$R" --sharedroot none --dry-run --format yaml
grep -q -- '--format takes text or jsonl' "$row_err" || fail "p8b: --format yaml: $(head -1 "$row_err")"

echo "== dispatch order"
# Byte order: "127.0.0.1" < "localhost"; shuffle with key "k" is a fixed
# order; the manifest, summary, and command record carry the order.
row o1 0 -- k run --no-daemon --target localhost --target 127.0.0.1 --order sorted show clock
order o1 127.0.0.1 localhost
row o2 0 -- k --set 'dispatch.order="name"' run --no-daemon --target localhost --target 127.0.0.1 show clock
order o2 127.0.0.1 localhost
job=$(sed -n 's/^! exit=.* artifacts=//p' "$row_out" | tail -1)
json_is "$job/manifest.json" plan.dispatch.dispatch_order sorted || fail "o2: manifest lacks dispatch_order sorted (alias name)"
json_is "$job/summary.json" dispatch_order sorted || fail "o2: summary lacks dispatch_order"
json_has "$job/manifest.json" plan.dispatch.shuffle_key && fail "o2: shuffle_key present for sorted"
row o3 0 -- k --set 'dispatch.order="shuffle"' --set 'dispatch.shuffle-key="k"' run --no-daemon --target 127.0.0.1 --target localhost show clock
job=$(sed -n 's/^! exit=.* artifacts=//p' "$row_out" | tail -1)
json_is "$job/manifest.json" plan.dispatch.dispatch_order shuffle && json_is "$job/manifest.json" plan.dispatch.shuffle_key k || fail "o3: manifest lacks shuffle order and key"
json_is "$job/summary.json" shuffle_key k || fail "o3: summary lacks shuffle_key"
[ "$(jsonl_records "$job/commands.jsonl" dispatch_order shuffle_key | sort -u)" = "shuffle${TAB}k" ] || fail "o3: command record lacks order and key"
sed -n 's/^\([^ ]*\) \[.*/\1/p' "$row_out" >"$TMP/o3"
row o3b 0 -- k --set 'dispatch.order="shuffle"' --set 'dispatch.shuffle-key="k"' run --no-daemon --target localhost --target 127.0.0.1 show clock
sed -n 's/^\([^ ]*\) \[.*/\1/p' "$row_out" | cmp -s - "$TMP/o3" || fail "o3b: the same key must give the same order whatever the input order"
row o4 0 -- k run --no-daemon --target 127.0.0.1 --target localhost --order random show clock
job=$(sed -n 's/^! exit=.* artifacts=//p' "$row_out" | tail -1)
json_is "$job/manifest.json" plan.dispatch.dispatch_order random && json_get "$job/manifest.json" plan.dispatch.shuffle_key | grep -Eqx '[0-9]{10}' || fail "o4: random must record epoch seconds as the key"
row o5 0 -- k command --target localhost --target 127.0.0.1 --order sorted --cmd "show clock" --format jsonl
[ "$(jsonl_records "$row_out" dispatch_order candidate_count)" = "sorted${TAB}2" ] || fail "o5: command record lacks order or candidate count"
[ "$(jsonl_records "$row_out" device.canonical_name)" = 127.0.0.1 ] || fail "o5: sorted must select 127.0.0.1 first"
row o6 2 -- k --set 'dispatch.order="inventory"' run --no-daemon --target 127.0.0.1 show clock;     code o6 config_dispatch_order_inventory_removed; mentions o6 default
row o7 2 -- k --set 'dispatch.order="random-seeded"' run --no-daemon --target 127.0.0.1 show clock; code o7 config_dispatch_order_random_seeded_removed; mentions o7 shuffle
row o8 2 -- k --set 'dispatch.random-seed="k"' run --no-daemon --target 127.0.0.1 show clock;       code o8 config_unknown_key
row o9 4 -- k login 127.0.0.1 --order inventory;                                                    code o9 cli_option_value_invalid

echo "== command text in debug output"
# Each command sent appears exactly once, as the operator provided it, in the
# device command start event; the transport events keep hash and length only.
row d1 0 -- k command --debug 127.0.0.1 show  ip route
[ "$(grep -c 'DEBUG device command start .* command="show ip route"' "$row_err")" = 1 ] || fail "d1: command text not shown exactly once: $(grep -c 'command=' "$row_err")"
[ "$(grep -c 'show ip route' "$row_err")" = 1 ] || fail "d1: command text appears outside the start event"
printf 'show clock\n  show version  \n' >"$TMP/dcmds"
row d2 0 -- k command --debug 127.0.0.1 --cf "$TMP/dcmds"
grep -q 'index=1 total=2 .* command="show clock"' "$row_err" && grep -q 'index=2 total=2 .* command="  show version  "' "$row_err" || fail "d2: --cf lines as written: $(grep 'command start' "$row_err" | tr '\n' '|')"
long=$(head -c 6000 /dev/zero | tr '\0' 'x')
row d3 0 -- k command --debug 127.0.0.1 --cmd "show $long"
grep -q 'command="show xxxx' "$row_err" && grep -q '\.\.\.$' "$row_err" || fail "d3: long command not bounded with a marker"
[ "$(awk '{ if (length($0) > m) m = length($0) } END { print m }' "$row_err")" -le 4096 ] || fail "d3: debug line exceeds the bound"
row d4 0 -- k login --debug 127.0.0.1
! grep -q 'command=' "$row_err" || fail "d4: login debug shows command text"

echo "== parser codes"
row c1 4 -- k "bogus*";                                code c1 cli_command_unknown
row c2 4 -- k daemon;                                 code c2 cli_subcommand_missing
row c3 4 -- k daemon bounce;                          code c3 cli_subcommand_unknown
row c4 4 -- k daemon s;                               code c4 cli_subcommand_ambiguous; mentions c4 start; mentions c4 serve; mentions c4 status; mentions c4 stop
row c5 4 -- k command --bogus 127.0.0.1 show clock;   code c5 cli_option_unknown
row c6 4 -- k command 127.0.0.1 --port;               code c6 cli_option_value_missing
row c7 4 -- k command 127.0.0.1 --port abc show clock; code c7 cli_option_value_invalid
row c8 4 -- k command 127.0.0.1 --format xml show clock; code c8 cli_option_value_invalid; mentions c8 "text, jsonl, or json"
row c9 4 -- k command --cmd "show clock";             code c9 cli_device_missing
row c10 4 -- k command "" show clock;                 code c10 cli_device_empty
row c11 4 -- k command 127.0.0.1;                     code c11 cli_command_text_missing
row c12 4 -- k command 127.0.0.1 --cmd x --cmd y show clock; code c12 cli_command_text_mixed
row c12b 0 -- k command 127.0.0.1 --cmd show clock;   sent c12b "show clock"
row c13 4 -- k version extra;                         code c13 cli_positional_unexpected
row c14 4 -- k command 127.0.0.1 --tfr - show clock;  code c14 target_source_stdin_invalid
row c15 4 -- k command 127.0.0.1 --port 70000 show clock; code c15 port_out_of_range
row c16 4 -- k --debug-show-secrets version;          code c16 debug_show_secrets_requires_debug
row c17 4 -- k command -4 -6 127.0.0.1 show clock;    code c17 address_family_conflict
row c18 4 -- k command --border --noborder 127.0.0.1 show clock; code c18 border_options_conflict
row c19 4 -- k login --all=false;                     code c19 cli_device_missing
row c20 4 -- k config generate --minimal --full;      code c20 config_generate_mode_conflict
row c21 0 -- k config validate --format json;          json_is "$row_out" valid true || fail "c21: $(cat "$row_out")"
row c22 4 -- k config sh --format json --explain basedir; code c22 config_show_format_conflict
row c23 0 -- k version --format json;                  json_has "$row_out" version || fail "c23"

finish k03-smoke-test
