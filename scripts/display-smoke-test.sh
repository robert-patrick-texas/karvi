#!/bin/sh
# Verify the display templates, human timestamps, border expansion, and local
# --quiet suppression without changing machine records; then, as counted rows
# over a fake fleet, a run's gathered display (--gather), line mode
# (--prefix), the two together, a --cf folder, job follow --gather from the
# job's folder, and --gather's refusal beside --nof.
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
KARVI=${KARVI:-$ROOT/bin/karvi-linux-amd64}
. "$ROOT/scripts/lib/json.sh"; . "$ROOT/scripts/lib/host.sh"
host_config_clean "$KARVI"   # refused on a host whose configuration reaches karvi
TMP=$(mktemp -d "${TMPDIR:-/tmp}/karvi-display-smoke-XXXXXX")
host_trust_store "$TMP"   # the trust store under the work directory, never the operator's
HOME_DIR=$TMP/home
BASE=$TMP/state
SCORE=$BASE/scoreboards
CAP=$BASE/capacity
FAKE=$TMP/fake-ssh
cleanup() { rm -rf "$TMP"; }
. "$ROOT/scripts/lib/exit.sh"; . "$ROOT/scripts/lib/lab.sh"; on_exit cleanup
install -d -m 700 "$HOME_DIR" "$BASE" "$SCORE" "$CAP"
lab_places "$TMP/places.toml" "$BASE"
cat > "$FAKE" <<'SH'
#!/bin/sh
log="$0.log"
args=" $* "
# The system transport's ssh -Q capability queries are not sessions.
[ "$1" = -Q ] && exit 0
printf 'START:%s\n' "$*" >> "$log"
case "$args" in
  *" ControlPath=none "*)
    printf 'display-device#'
    while IFS= read -r line; do
      printf 'COMMAND:%s\n' "$line" >> "$log"
      [ "$line" = exit ] && exit 0
      printf '%s\r\nOUTPUT:%s\r\ndisplay-device#' "$line" "$line"
    done
    exit 0
    ;;
esac
printf 'unexpected noninteractive invocation\n' >&2
exit 96
SH
chmod 755 "$FAKE"

common_args() {
  printf '%s\n' \
    --config "$TMP/places.toml" \
    --set 'platform-resolution.default=""' \
    --set "ssh.transports.system=\"$FAKE\"" \
    --set 'ssh.command.transport="system"'
}

# Defaults: one requested header, one final footer, no border.
rm -f "$FAKE.master" "$FAKE.log"
# shellcheck disable=SC2046
HOME="$HOME_DIR" NETUSER=smoke NETPASS=test "$KARVI" $(common_args) \
  command --management-address 127.0.0.1 --command 'show clock' display-device \
  >"$TMP/default.out" 2>"$TMP/default.err"
grep -Eq '^! display-device \[127\.0\.0\.1\] platform=generic user=smoke backend=[^ ]+ transport=system$' "$TMP/default.out"
[ "$(grep -c '^! display-device \[127\.0\.0\.1\] ' "$TMP/default.out")" -eq 1 ]
grep -Eq '^! exit=0 elapsed=[^ ]+ artifacts=.+$' "$TMP/default.out"
[ "$(grep -c '^! exit=0 elapsed=.* artifacts=' "$TMP/default.out")" -eq 1 ]
[ "$(grep -c '^OUTPUT:show clock$' "$TMP/default.out")" -eq 1 ]
[ "$(grep -c '^START:' "$FAKE.log")" -eq 1 ]
[ "$(grep -c '^COMMAND:show clock$' "$FAKE.log")" -eq 1 ]

# --nof (output.persist-command=false): the same display, the footer's
# artifacts is "none", and no job folder or output file is created; the
# audit log still has the command.
nof_folders() { find "$BASE/jobs" -mindepth 1 2>/dev/null | wc -l; }
nof_before=$(nof_folders)
rm -f "$FAKE.master" "$FAKE.log"
# shellcheck disable=SC2046
HOME="$HOME_DIR" NETUSER=smoke NETPASS=test "$KARVI" $(common_args) \
  command --management-address 127.0.0.1 --command 'show clock' --nof display-device \
  >"$TMP/nof.out" 2>"$TMP/nof.err"
grep -Eq '^! exit=0 elapsed=[^ ]+ artifacts=none$' "$TMP/nof.out"
[ "$(grep -c '^OUTPUT:show clock$' "$TMP/nof.out")" -eq 1 ]
[ "$(nof_folders)" -eq "$nof_before" ]
[ "$(audit_events "$BASE/audit.jsonl" event_name | grep -c '\.command$')" -eq 2 ]

# --of[=PATH]: the PATH attaches with = only,
# so the word after a bare --of is the device and the files go to the
# configured folder; with =PATH the job folder is created under PATH; --of
# with --nof is refused before any device is planned.
rm -f "$FAKE.master" "$FAKE.log"
# shellcheck disable=SC2046
HOME="$HOME_DIR" NETUSER=smoke NETPASS=test "$KARVI" $(common_args) \
  command --management-address 127.0.0.1 --command 'show clock' --of display-device \
  >"$TMP/of.out" 2>"$TMP/of.err"
grep -Eq "^! exit=0 elapsed=[^ ]+ artifacts=$BASE/jobs/" "$TMP/of.out"
[ "$(nof_folders)" -gt "$nof_before" ]
rm -f "$FAKE.master" "$FAKE.log"
# shellcheck disable=SC2046
HOME="$HOME_DIR" NETUSER=smoke NETPASS=test "$KARVI" $(common_args) \
  command --management-address 127.0.0.1 --command 'show clock' "--of=$TMP/of-root" display-device \
  >"$TMP/ofpath.out" 2>"$TMP/ofpath.err"
grep -Eq "^! exit=0 elapsed=[^ ]+ artifacts=$TMP/of-root/" "$TMP/ofpath.out"
[ "$(grep -c '^OUTPUT:show clock$' "$TMP/ofpath.out")" -eq 1 ]
[ -s "$(ls -d "$TMP"/of-root/*/* | tail -1)/commands.jsonl" ]
of_code=0
# shellcheck disable=SC2046
HOME="$HOME_DIR" NETUSER=smoke NETPASS=test "$KARVI" $(common_args) \
  command --management-address 127.0.0.1 --command 'show clock' --of --nof display-device \
  >"$TMP/ofnof.out" 2>"$TMP/ofnof.err" || of_code=$?
[ "$of_code" -eq 4 ]
grep -q '^output_options_conflict: --of and --nof are mutually exclusive' "$TMP/ofnof.err"

rm -f "$FAKE.master" "$FAKE.log"
# shellcheck disable=SC2046
HOME="$HOME_DIR" NETUSER=smoke NETPASS=test "$KARVI" $(common_args) \
  --set 'display.timestamp="dd/mm/yyyy hh:mm:ss.sss"' \
  --set 'display.command.header="H <timestamp> <target> <address> <platform> <user> <auth-backend> <transport>"' \
  --set 'display.command.footer="F <reference-id> <exit-status> <artifacts> <timestamp>"' \
  --set 'display.command.border="<repeat:-:40>\n"' \
  --set display.command.last-border=true \
  command --management-address 127.0.0.1 --command 'show clock' --command 'show version' display-device \
  >"$TMP/decorated.out" 2>"$TMP/decorated.err"
grep -Eq '^H [0-9]{2}/[0-9]{2}/[0-9]{4} [0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{3} display-device 127\.0\.0\.1 generic smoke ' "$TMP/decorated.out"
[ "$(grep -c '^----------------------------------------$' "$TMP/decorated.out")" -eq 2 ]
grep -q '^F .* ExitSuccess(0) ' "$TMP/decorated.out"
[ "$(grep -c '^OUTPUT:' "$TMP/decorated.out")" -eq 2 ]
[ "$(grep -c '^START:' "$FAKE.log")" -eq 1 ]
[ "$(grep -c '^COMMAND:show ' "$FAKE.log")" -eq 2 ]

rm -f "$FAKE.master"
# shellcheck disable=SC2046
HOME="$HOME_DIR" NETUSER=smoke NETPASS=test "$KARVI" $(common_args) \
  --set 'display.command.header="SHOULD-NOT-APPEAR"' \
  --set 'display.command.footer="SHOULD-NOT-APPEAR"' \
  --set 'display.command.border="SHOULD-NOT-APPEAR\n"' \
  command --quiet --management-address 127.0.0.1 --command 'show clock' display-device \
  >"$TMP/quiet.out" 2>"$TMP/quiet.err"
grep -q '^OUTPUT:show clock$' "$TMP/quiet.out"
absent 'SHOULD-NOT-APPEAR' "$TMP/quiet.out"

# Structured command records remain undecorated even when display templates
# contain conspicuous values.
rm -f "$FAKE.master" "$FAKE.log"
# shellcheck disable=SC2046
HOME="$HOME_DIR" NETUSER=smoke NETPASS=test "$KARVI" $(common_args) \
  --set 'display.command.header="SHOULD-NOT-APPEAR"' \
  --set 'display.command.footer="SHOULD-NOT-APPEAR"' \
  --set 'display.command.border="SHOULD-NOT-APPEAR\n"' \
  command --format jsonl --management-address 127.0.0.1 --command 'show clock' display-device \
  >"$TMP/jsonl.out" 2>"$TMP/jsonl.err"
[ "$(wc -l < "$TMP/jsonl.out" | tr -d ' ')" -eq 1 ]
grep -q '^{' "$TMP/jsonl.out"
[ "$(jsonl_records "$TMP/jsonl.out" command)" = "show clock" ]
absent 'SHOULD-NOT-APPEAR' "$TMP/jsonl.out"

# The rows: each runs one karvi and checks it, counted, the suite going on
# past a failed row (scripts/lib/rows.sh). Every run is --no-daemon and job
# follow reads the job's folder, so the suite starts no daemon (the daemon's
# paths are the Go tests'). The fleet is a second fake, which answers by the
# address it is given, the system transport's last argument, and logs what
# each address was sent: router01-03 (127.0.0.1-3) answer alike, edge1
# (127.0.0.4) another clock, a second group, and branch1 (127.0.0.5) exits as
# ssh does for an unreachable device, the failure group. An inventory gives
# each name its address, so the groups' headers fold names.
# shellcheck disable=SC2034 # read by scripts/lib/rows.sh
row_dir=$TMP/streams; . "$ROOT/scripts/lib/rows.sh"
install -d -m 700 "$row_dir"
FLEET=$TMP/fleet-ssh
cat >"$FLEET" <<'SH'
#!/bin/sh
# The system transport's ssh -Q capability queries are not sessions.
[ "$1" = -Q ] && exit 0
for addr; do :; done
printf 'open %s\n' "$addr" >>"$0.log"
case $addr in
  127.0.0.[123]) clock='*10:00:00.000 UTC Tue Nov 10 2026' ;;
  127.0.0.4) clock='*10:00:07.000 UTC Tue Nov 10 2026' ;;
  *) printf 'ssh: connect to host %s port 22: Connection refused\n' "$addr" >&2; exit 255 ;;
esac
printf 'fleet#'
while IFS= read -r line; do
  printf 'sent %s %s\n' "$addr" "$line" >>"$0.log"
  case $line in
    exit) exit 0 ;;
    'show clock') out=$clock ;;
    'show version') out='Cisco IOS XE Software, Version 17.09.04a' ;;
    *) out='% Invalid input detected' ;;
  esac
  printf '%s\r\n%s\r\nfleet#' "$line" "$out"
done
SH
chmod 755 "$FLEET"
printf 'name,management_address,platform\nbranch1,127.0.0.5,generic\nedge1,127.0.0.4,generic\nrouter01,127.0.0.1,generic\nrouter02,127.0.0.2,generic\nrouter03,127.0.0.3,generic\n' >"$TMP/fleet.csv"
printf '[[inventory-source]]\nname = "fleet"\ntype = "csv"\npath = "%s"\nrequired = true\n' "$TMP/fleet.csv" >"$TMP/fleet.toml"
chmod 600 "$TMP/fleet.toml"

# fleet ARGS: karvi over the fleet, its log emptied first. fleet_run ARGS:
# a run of the whole fleet, two commands each, the devices named in an
# order that is not the gathered display's, nor its reverse, nor by name.
fleet() {
  : >"$FLEET.log"
  HOME="$HOME_DIR" NETUSER=smoke NETPASS=test "$KARVI" --config "$TMP/places.toml" --config "$TMP/fleet.toml" \
    --set 'platform-resolution.default=""' --set "ssh.transports.system=\"$FLEET\"" --set 'ssh.run.transport="system"' "$@"
}
fleet_run() { fleet run --no-daemon "$@" --tl edge1,branch1,router01,router02,router03 --cmd 'show clock' --cmd 'show version'; }
# shown NAME WANT: the row's display, every line before the footer, is
# WANT's text. shown_by NAME WANT DEVICE...: each DEVICE's lines (those
# beginning "DEVICE: ") are WANT's, in WANT's order, and there are no other
# lines; the devices may come in any order, as line mode writes each record
# as it comes. footer NAME EXIT [DIR]: the last line is a footer of exit
# EXIT, its artifacts DIR or a job folder under BASE. quiet NAME: nothing on
# standard error (no gather progress line to a file).
# shellcheck disable=SC2154 # row_out and row_err: the last row's streams, set by scripts/lib/rows.sh
shown() {
  sed '$d' "$row_out" >"$TMP/got"
  cmp -s "$2" "$TMP/got" && return 0
  fail "$1: the display before the footer is not $2's (< want, > shown):"
  diff "$2" "$TMP/got" >&2 || :
}
shown_by() {
  sb_name=$1; sb_want=$2; shift 2
  sed '$d' "$row_out" >"$TMP/got"
  sb_got=$(wc -l <"$TMP/got" | tr -d ' '); sb_lines=$(wc -l <"$sb_want" | tr -d ' ')
  [ "$sb_got" -eq "$sb_lines" ] || fail "$sb_name: $sb_got lines before the footer, want $sb_lines"
  for sb_dev; do
    grep "^$sb_dev: " "$sb_want" >"$TMP/want.dev" || :
    grep "^$sb_dev: " "$TMP/got" >"$TMP/got.dev" || :
    cmp -s "$TMP/want.dev" "$TMP/got.dev" || fail "$sb_name: $sb_dev's lines are $(tr '\n' '|' <"$TMP/got.dev"), want $(tr '\n' '|' <"$TMP/want.dev")"
  done
}
footer() {
  ft_last=$(sed -n '$p' "$row_out")
  ft_dir=$BASE'/jobs/[0-9]{6}/[0-9]{6}-[0-9]{6}-[0-9a-z]{2}'
  [ -z "${3:-}" ] || ft_dir=$3
  printf '%s\n' "$ft_last" | grep -Eqx "! exit=$2 elapsed=[^ ]+ artifacts=$ft_dir" || fail "$1: the last line is not the footer of exit $2: $ft_last"
}
# shellcheck disable=SC2154 # row_err, set by scripts/lib/rows.sh
quiet() { [ ! -s "$row_err" ] || fail "$1: standard error holds $(head -1 "$row_err")"; }

# gath: the gathered display. A block per command under its marker; in it
# the groups, each its header of folded names and count and its answer
# once: the succeeded first, the largest first (router[01-03] before edge1,
# though edge1 comes first by name and in the run's list), then the failure
# group, branch1, last though first by name, its failure line with the
# message, as a group of one, and in the second block its not-attempted
# line; in the second block edge1's answer is router01-03's, one group.
# The run's exit with a failed device is 101 (ExitPartialFailure).
printf '%s\n' \
  '! show clock' \
  '! router[01-03] (3)' \
  '*10:00:00.000 UTC Tue Nov 10 2026' \
  '' \
  '! edge1 (1)' \
  '*10:00:07.000 UTC Tue Nov 10 2026' \
  '' \
  '! branch1 (1)' \
  'karvi: status=connection_error error=connection_refused: ssh: connect to host 127.0.0.5 port 22: Connection refused' \
  '' \
  '! show version' \
  '! edge1,router[01-03] (4)' \
  'Cisco IOS XE Software, Version 17.09.04a' \
  '' \
  '! branch1 (1)' \
  'karvi: status=not_attempted_prior_command_failure' \
  >"$TMP/want.gath"
row gath 101 -- fleet_run --gather
shown gath "$TMP/want.gath"; footer gath 101; quiet gath

# line: line mode, every line its device's name first, no device header or
# border, the failure and not-attempted lines without target=; its job, run
# without --gather, is the one jf follows.
printf '%s\n' \
  'branch1: karvi: status=connection_error error=connection_refused: ssh: connect to host 127.0.0.5 port 22: Connection refused' \
  'branch1: karvi: status=not_attempted_prior_command_failure command=2/2' \
  'edge1: *10:00:07.000 UTC Tue Nov 10 2026' \
  'edge1: Cisco IOS XE Software, Version 17.09.04a' \
  'router01: *10:00:00.000 UTC Tue Nov 10 2026' \
  'router01: Cisco IOS XE Software, Version 17.09.04a' \
  'router02: *10:00:00.000 UTC Tue Nov 10 2026' \
  'router02: Cisco IOS XE Software, Version 17.09.04a' \
  'router03: *10:00:00.000 UTC Tue Nov 10 2026' \
  'router03: Cisco IOS XE Software, Version 17.09.04a' \
  >"$TMP/want.line"
row line 101 -- fleet_run --prefix
shown_by line "$TMP/want.line" branch1 edge1 router01 router02 router03; footer line 101; quiet line
line_job=$(sed -n 's/^! exit=.* artifacts=//p' "$row_out")

# both: gathered in line mode, each line of a group under its folded names,
# the block's marker unprefixed, no group header and no border.
printf '%s\n' \
  '! show clock' \
  'router[01-03]: *10:00:00.000 UTC Tue Nov 10 2026' \
  'edge1: *10:00:07.000 UTC Tue Nov 10 2026' \
  'branch1: karvi: status=connection_error error=connection_refused: ssh: connect to host 127.0.0.5 port 22: Connection refused' \
  '! show version' \
  'edge1,router[01-03]: Cisco IOS XE Software, Version 17.09.04a' \
  'branch1: karvi: status=not_attempted_prior_command_failure' \
  >"$TMP/want.both"
row both 101 -- fleet_run --gather --prefix
shown both "$TMP/want.both"; footer both 101; quiet both

# cf: --cf DIR, a command file per device, DIR/NAME (NAME the device's
# collection file name, here its name); the lists differ in their commands
# and order, so the blocks are by command, show version's first (edge1's
# first command, edge1 first in the run), and each device was sent its own
# list; the job folder keeps each list as commands/NAME, as written.
install -d -m 700 "$TMP/lists"
printf 'show version\nshow clock\n' >"$TMP/lists/edge1"
printf 'show clock\nshow version\n' >"$TMP/lists/router01"
printf 'show clock\nshow version\n' >"$TMP/lists/router02"
printf 'show clock\n' >"$TMP/lists/router03"
printf '%s\n' \
  '! show version' \
  '! edge1,router[01-02] (3)' \
  'Cisco IOS XE Software, Version 17.09.04a' \
  '' \
  '! show clock' \
  '! router[01-03] (3)' \
  '*10:00:00.000 UTC Tue Nov 10 2026' \
  '' \
  '! edge1 (1)' \
  '*10:00:07.000 UTC Tue Nov 10 2026' \
  >"$TMP/want.cf"
row cf 0 -- fleet run --no-daemon --gather --tl edge1,router01,router02,router03 --cf "$TMP/lists"
shown cf "$TMP/want.cf"; footer cf 0; quiet cf
for cf_pair in 127.0.0.4:edge1 127.0.0.1:router01 127.0.0.2:router02 127.0.0.3:router03; do
  { cat "$TMP/lists/${cf_pair#*:}"; echo exit; } >"$TMP/want.sent"
  sed -n "s/^sent ${cf_pair%:*} //p" "$FLEET.log" >"$TMP/got.sent"
  cmp -s "$TMP/want.sent" "$TMP/got.sent" || fail "cf: ${cf_pair#*:} was sent $(tr '\n' '|' <"$TMP/got.sent"), want $(tr '\n' '|' <"$TMP/want.sent")"
done
cf_job=$(sed -n 's/^! exit=.* artifacts=//p' "$row_out")
if [ -d "$cf_job/commands" ]; then
  cf_names=
  for cf_file in "$cf_job"/commands/*; do
    cf_names="$cf_names ${cf_file##*/}"
    cmp -s "$cf_file" "$TMP/lists/${cf_file##*/}" || fail "cf: the job's commands/${cf_file##*/} is not the list $TMP/lists/${cf_file##*/}"
  done
  [ "$cf_names" = ' edge1 router01 router02 router03' ] || fail "cf: the job's commands/ holds$cf_names, want edge1 router01 router02 router03"
else
  fail "cf: no commands/ in the job's folder ${cf_job:-(no footer)}"
fi

# jf: job follow --gather of line's finished job, run without the option:
# with no daemon it reads the job's folder (the job ID found under the
# output root) and shows the job gathered as gath showed its own, in the
# follow's display (no line mode, which only the job's run had), the job's
# folder in the footer and its exit; and it launches no daemon, no socket
# under BASE.
if [ -d "$line_job" ]; then
  row jf 101 -- fleet job follow --gather "${line_job##*/}"
  shown jf "$TMP/want.gath"; footer jf 101 "$line_job"; quiet jf
else
  row_open jf
  fail "jf: line left no job to follow (no footer naming its folder)"
fi
jf_sockets=$(find "$BASE" -type s)
[ -z "$jf_sockets" ] || fail "jf: a socket under $BASE: $jf_sockets"

# nof: --gather beside --nof, which leaves no commands.jsonl to read back,
# is refused before any device is planned: the one line, exit 4, nothing
# sent to the fleet.
row nof 4 -- fleet_run --gather --nof
[ "$(cat "$row_err")" = 'gather_option_conflict: --gather and --nof are mutually exclusive' ] || fail "nof: standard error is $(head -1 "$row_err")"
[ ! -s "$row_out" ] || fail "nof: standard output must be empty"
[ ! -s "$FLEET.log" ] || fail "nof: the fleet was sent $(tr '\n' '|' <"$FLEET.log")"

finish 'display smoke'
