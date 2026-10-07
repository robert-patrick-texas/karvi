#!/bin/sh
# The parity suite: the
# same command list run by `command` and by `run --no-daemon`, over `system`
# and over `scrapligo-v1`, against the fake IOS XE device built from the
# tree, gives the same requested-command records (tools/paritycheck holds
# the excluded paths), the same lines at the device, and one connection and
# one shell per combination. Needs the native executable and `go`.
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
KARVI=${KARVI:-$ROOT/bin/karvi-linux-amd64}
GO=${GO:-go}
. "$ROOT/scripts/lib/json.sh"
TMP=${TMPDIR:-/tmp}/karvi-native-smoke-$$
KH=$TMP/store/known_hosts
FAKE_PID=
unset NETUSER NETPASS NETENABLE

fail() { echo "native smoke: FAIL: $*" >&2; exit 1; }
stop_fake() {
  [ -n "$FAKE_PID" ] || return 0
  kill "$FAKE_PID" 2>/dev/null || true
  wait "$FAKE_PID" 2>/dev/null || true
  FAKE_PID=
}
stop_daemon() {
  [ -S "$TMP/base/socket/daemon.sock" ] || return 0
  HOME=$TMP/home "$KARVI" --quiet --config "$TMP/karvi.toml" daemon stop --force >/dev/null 2>&1 || true
  i=0
  while [ -S "$TMP/base/socket/daemon.sock" ] && [ $i -lt 100 ]; do sleep 0.1; i=$((i + 1)); done
  [ ! -S "$TMP/base/socket/daemon.sock" ] || fail "the daemon socket remains"
}
cleanup() { stop_fake; stop_daemon 2>/dev/null || true; rm -rf "$TMP"; }
trap cleanup EXIT HUP INT TERM

"$KARVI" version --format json | grep -q '"id": "scrapligo-v1"' || fail "$KARVI lacks scrapligo-v1; build with make native-build"
install -d -m 700 "$TMP" "$TMP/bin" "$TMP/store"
(cd "$ROOT" && GOTOOLCHAIN=local "$GO" build -mod=vendor -o "$TMP/bin/fake" ./cmd/karvi-fake-device)
# tools/textfile derives a device's output.TARGET.txt from commands.jsonl (S27).
(cd "$ROOT" && GOTOOLCHAIN=local "$GO" build -mod=vendor -o "$TMP/bin/textfile" ./tools/textfile)
(cd "$ROOT" && GOTOOLCHAIN=local "$GO" build -mod=vendor -o "$TMP/bin/paritycheck" ./tools/paritycheck)

# The operator's own trust store must not change (ssh.known-hosts-file is
# under TMP in every configuration here; HOME does not move the store).
. "$ROOT/scripts/lib/host.sh"
OWN_STORE=$(host_own_store "$KARVI")
OWN_BEFORE=$(host_store_digest "$OWN_STORE")

start_fake() {  # fake flags...; sets PORT
  : >"$TMP/port"
  "$TMP/bin/fake" -host-key-file "$TMP/hostkey" "$@" 2>"$TMP/fake.err" >"$TMP/port" &
  FAKE_PID=$!
  i=0
  while [ ! -s "$TMP/port" ] && [ $i -lt 100 ]; do sleep 0.05; i=$((i + 1)); done
  [ -s "$TMP/port" ] || fail "the fake did not start: $(head -1 "$TMP/fake.err")"
  PORT=$(cat "$TMP/port")
}

write_config() {  # policy, tables (PORT substituted)
  cat >"$TMP/karvi.toml" <<EOF_CFG
basedir = "$TMP/base"
sharedroot = "none"
spooldir = "$TMP/spool"
scoreboards = "$TMP/base/score"
[ssh]
host-key-policy = "$1"
known-hosts-file = "$KH"
connect-timeout = "2s"
${SSH:-}
[audit]
journald-required = false
file = "$TMP/base/audit.jsonl"
[sessions]
shared-capacity-root = "$TMP/base/cap"
[display]
color = "never"
[execution]
command-timeout = "${COMMAND_TIMEOUT:-1s}"
${EXECUTION:-}
[[inventory-source]]
name = "parity"
type = "csv"
path = "$TMP/inv.csv"
required = true
mode = "header"
delimiter = ","
mandatory-fields = ["name", "platform"]
name-transform = "default"
[inventory-source.mappings]
name = ["name"]
management_address = ["management_address"]
platform = ["platform"]
[name-transform.default]
operations = [{op = "lowercase"}]
$(printf '%s\n' "$2" | sed "s/PORT/$PORT/")
EOF_CFG
  chmod 600 "$TMP/karvi.toml"
}

run_karvi() {  # tag, activity (command|run|daemon), transport, enable, karvi args...; sets CODE
  tag=$1; act=$2; tr=$3; enable=$4; shift 4
  case $act in run) sub='run --no-daemon' ;; daemon) sub=run ;; *) sub=command ;; esac
  rm -rf "$TMP/base" "$TMP/home"; install -d -m 700 "$TMP/base" "$TMP/home"
  CODE=0
  # shellcheck disable=SC2086
  env ${enable:+NETENABLE=$enable} HOME="$TMP/home" NETUSER=netops NETPASS="${PASS:-pw}" \
    "$KARVI" --config "$TMP/karvi.toml" --set 'platform-resolution.default=""' $sub --format jsonl --target fake-iosxe --transport "$tr" "$@" \
    >"$TMP/out.$tag" 2>"$TMP/err.$tag" || CODE=$?
  [ "$act" != daemon ] || stop_daemon
}

# parity_case LABEL POLICY PLATFORM TABLES ENABLE FAKEFLAGS EXPECT COMMANDEXIT LINES -- karvi args...
# One fake per combination, so the lines it received and its connection
# count belong to that combination; all of a case with one host-key file and
# on the port the first took, so the record's port and the host-key identity
# are one device's. Settings:
# The timeouts the cases wait out are the smallest that still bound the
# fake's delays: a command 1s, a blind wait 1s, the
# device timeout 2s over a 1s "show slow", the connect allowance 2s, one
# missed keepalive; each is waited once per stream, four streams a case.
# COMBOS (activity:transport ...), PASS, OTHERKEY (another key enrolled under
# the identity), KEYSCAN (the system transport's insecure comparison connects
# too), WARNING (text each combination's stderr must hold), COMMAND_TIMEOUT
# and EXECUTION (the [execution] table's command-timeout and further lines),
# SSH (further [ssh] lines), KEYLOGIN (the fingerprint of the one key the
# fake must have logged in by), AUTH (the method each stream's first
# record names, credential.auth),
# SHELL_OPENED (a shell that opens and receives no line), CHANNELS (the exec
# channels the one connection carried, and no shell), PIN (paritycheck's
# -pin, one per line: a difference between the transports by design).
# ONLY=S7 runs the cases whose label begins so.
parity_case() {
  case $1 in "${ONLY:-}"*) ;; *) return 0 ;; esac
  label=$1; policy=$2; row=$3; tables=$4; enable=$5; fflags=$6; expect=$7; cexit=$8; lines=$9
  shift 9; [ "$1" = -- ] && shift
  printf 'name,management_address,platform\nfake-iosxe,127.0.0.1,%s\n' "$row" >"$TMP/inv.csv"
  streams=; PORT=0
  for combo in ${COMBOS:-command:system command:native run:system run:native}; do
    act=${combo%%:*}; tr=${combo##*:}; tag=$act.$tr
    # shellcheck disable=SC2086
    start_fake -port "$PORT" $fflags
    : >"$KH"; chmod 600 "$KH"
    [ -z "${OTHERKEY:-}" ] || printf '[fake-iosxe]:%s %s\n' "$PORT" "$OTHERKEY" >"$KH"
    write_config "$policy" "$tables"
    run_karvi "$tag" "$act" "$tr" "$enable" "$@"
    stop_fake
    want=$cexit
    [ "$act" = command ] || [ "$cexit" -eq 0 ] || want=101
    [ "$CODE" -eq "$want" ] || fail "$label $tag: exit $CODE, expected $want: $(grep -E '^[a-z_]+:' "$TMP/err.$tag" | head -1)"
    sed -n 's/^line: //p' "$TMP/fake.err" | tr '\n' '|' >"$TMP/lines.$tag"
    [ "$(cat "$TMP/lines.$tag")" = "$lines" ] || fail "$label $tag: the device received $(cat "$TMP/lines.$tag"), expected $lines"
    counts=$(grep '^connections=' "$TMP/fake.err") || fail "$label $tag: the fake reported no counts"
    sessions=${counts##*sessions=}
    if [ -n "$lines" ] || [ -n "${SHELL_OPENED:-}" ]; then wantsessions=1; else wantsessions=0; fi
    if [ -n "${CHANNELS:-}" ]; then
      wantsessions=0
      grep -qx "channels=$CHANNELS" "$TMP/fake.err" || fail "$label $tag: $(grep '^channels=' "$TMP/fake.err"), expected channels=$CHANNELS"
    fi
    [ "$sessions" -eq "$wantsessions" ] || fail "$label $tag: $counts, expected sessions=$wantsessions"
    if [ -z "${KEYSCAN:-}" ] || [ "$tr" = native ]; then
      [ "${counts%% *}" = connections=1 ] || fail "$label $tag: $counts, expected one connection"
    fi
    [ -z "${WARNING:-}" ] || grep -q "$WARNING" "$TMP/err.$tag" || fail "$label $tag: no warning holding '$WARNING'"
    if [ -n "${AUTH:-}" ]; then
      grep '"credential"' "$TMP/out.$tag" | head -1 >"$TMP/first.$tag"
      json_is "$TMP/first.$tag" credential.auth "$AUTH" || fail "$label $tag: the record's credential.auth is $(json_get "$TMP/first.$tag" credential.auth 2>&1), expected $AUTH"
    fi
    [ -z "${KEYLOGIN:-}" ] || [ "$(sed -n 's/^key: //p' "$TMP/fake.err")" = "$KEYLOGIN" ] || fail "$label $tag: the fake's key logins: $(sed -n 's/^key: //p' "$TMP/fake.err" | tr '\n' ' '), expected $KEYLOGIN"
    streams="$streams $TMP/out.$tag"
  done
  set --
  while IFS= read -r pin; do
    [ -z "$pin" ] || set -- "$@" -pin "$pin"
  done <<EOF_PIN
${PIN:-}
EOF_PIN
  # shellcheck disable=SC2086
  result=$("$TMP/bin/paritycheck" -expect "$expect" "$@" $streams) || fail "$label: $(printf '%s' "$result" | sed "s|$TMP/out\.||g" | head -12)"
  echo "native smoke: $label: $result"
}

BUILTIN='[platform.cisco_iosxe]
ssh-port = PORT'
ALIAS='[platform.c9300]
driver = "cisco_iosxe"
ssh-port = PORT'
PROFILE_FAIL='[session-init.init]
commands = ["show bogus", "show clock"]
on-error = "fail-device"
[[session-init-map]]
profile = "init"
name = "*"'
PROFILE_CONTINUE='[session-init.init]
commands = ["show bogus", "show clock"]
on-error = "continue"
[[session-init-map]]
profile = "init"
name = "*"'
ALGORITHM_PROFILE='[ssh-algorithms-profile.legacy-ios]
ciphers-append = ["aes128-ctr"]
[[ssh-algorithms-map]]
profile = "legacy-ios"
name = "fake-iosxe"'
LEGACY='-rsa-sha1-only -kex diffie-hellman-group14-sha1 -ciphers aes128-ctr -macs hmac-sha1'
OPEN='"enable"|"<secret>"|"terminal length 0"|"terminal width 512"|'
NOT_ATTEMPTED=not_attempted_prior_command_failure
REJECTED=device_error:device_command_error

COMBOS='command:system command:native run:system run:native daemon:system daemon:native'
AUTH=keyboard-interactive
parity_case 'S1 success, a rejected command, success under continue (the daemon path too)' accept-new cisco_iosxe "$BUILTIN" en '' \
  "succeeded,$REJECTED,succeeded" 107 "$OPEN"'"show clock"|"show bogus"|"show version"|"exit"|' \
  -- --continue-device-on-error --cmd 'show clock' --cmd 'show bogus' --cmd 'show version'
unset COMBOS AUTH
parity_case 'S2 a rejected command under halt' accept-new cisco_iosxe "$BUILTIN" en '' \
  "succeeded,$REJECTED,$NOT_ATTEMPTED" 107 "$OPEN"'"show clock"|"show bogus"|"exit"|' \
  -- --cmd 'show clock' --cmd 'show bogus' --cmd 'show version'
parity_case 'S3 a timeout under continue' accept-new cisco_iosxe "$BUILTIN" en '-slow 10s' \
  "succeeded,timeout:command_timeout,$NOT_ATTEMPTED" 107 "$OPEN"'"show clock"|"show slow"|' \
  -- --continue-device-on-error --cmd 'show clock' --cmd 'show slow' --cmd 'show version'
parity_case 'S4 a session-init profile, fail-device' accept-new cisco_iosxe "$BUILTIN
$PROFILE_FAIL" en '' \
  "$REJECTED,$NOT_ATTEMPTED,not_attempted_session_init_failure,not_attempted_session_init_failure" 107 "$OPEN"'"show bogus"|"exit"|' \
  -- --cmd 'show version' --cmd 'show clock'
parity_case 'S5 a session-init profile, continue' accept-new cisco_iosxe "$BUILTIN
$PROFILE_CONTINUE" en '' \
  "$REJECTED,succeeded,succeeded,succeeded" 0 "$OPEN"'"show bogus"|"show clock"|"show version"|"show clock"|"exit"|' \
  -- --cmd 'show version' --cmd 'show clock'
parity_case 'S6 an alias of cisco_iosxe' accept-new c9300 "$ALIAS" en '' \
  "succeeded,$REJECTED" 107 "$OPEN"'"show clock"|"show bogus"|"exit"|' \
  -- --continue-device-on-error --cmd 'show clock' --cmd 'show bogus'
parity_case 'S7 privilege 15 at login, no enable secret' accept-new cisco_iosxe "$BUILTIN" '' '-start-privileged' \
  'succeeded,succeeded' 0 '"terminal length 0"|"terminal width 512"|"show clock"|"show version"|"exit"|' \
  -- --cmd 'show clock' --cmd 'show version'
parity_case 'S8 the device asks for an enable secret, none resolved' accept-new cisco_iosxe "$BUILTIN" '' '' \
  "privilege_error:privilege_failed,$NOT_ATTEMPTED" 107 '"enable"|' \
  -- --cmd 'show clock' --cmd 'show version'
parity_case 'S9 a wrong enable secret' accept-new cisco_iosxe "$BUILTIN" wrong '' \
  'privilege_error:privilege_failed' 107 '"enable"|"<secret>"|' \
  -- --cmd 'show clock'
parity_case 'S10 generic records a rejected command as it comes' accept-new generic '[platform.generic]
ssh-port = PORT' '' '' \
  'succeeded,succeeded' 0 '"show clock"|"show bogus"|"exit"|' \
  -- --cmd 'show clock' --cmd 'show bogus'
parity_case 'S11 the output limit' accept-new cisco_iosxe "$BUILTIN
[output]
max-command-bytes = 2000" en '' \
  "output_limit_exceeded:output_limit_exceeded,$NOT_ATTEMPTED" 111 "$OPEN"'"show big"|' \
  -- --continue-device-on-error --cmd 'show big' --cmd 'show clock'
parity_case 'S12a the legacy device under the defaults' accept-new cisco_iosxe "$BUILTIN" en "$LEGACY" \
  'connection_error:ssh_algorithm_negotiation_failed' 110 '' \
  -- --cmd 'show clock'
parity_case 'S12b the legacy device through an algorithm profile' accept-new cisco_iosxe "$BUILTIN
$ALGORITHM_PROFILE" en "$LEGACY" \
  'succeeded,succeeded' 0 "$OPEN"'"show clock"|"show version"|"exit"|' \
  -- --cmd 'show clock' --cmd 'show version'
PASS=bad
parity_case 'S13 a rejected password' accept-new cisco_iosxe "$BUILTIN" en '' \
  'authentication_error:authentication_failed' 108 '' \
  -- --cmd 'show clock'
unset PASS

# S13b-S13c: the operator's keys. The platform's fallback is the keys
# alone, ssh.identities names a key the fake holds after one it does not,
# and the fake's user is the operator's login name, the credential's; the
# shell starts privileged, a key credential holding no enable secret.
ssh-keygen -q -t ed25519 -N '' -f "$TMP/opkey" -C parity >/dev/null
ssh-keygen -q -t ed25519 -N '' -f "$TMP/stranger" -C parity >/dev/null
OPKEY=$(ssh-keygen -lf "$TMP/opkey.pub" | cut -d' ' -f2)
KEYS="[platform.cisco_iosxe]
ssh-port = PORT
fallback = [\"keys\"]"
OPERATOR=$(id -un)
SSH="identities = [\"$TMP/stranger\", \"$TMP/opkey\"]"
KEYLOGIN=$OPKEY AUTH=publickey
parity_case 'S13b a login by the operator'"'"'s key, no password offered' accept-new cisco_iosxe "$KEYS" '' "-start-privileged -user $OPERATOR -authorized-keys $TMP/opkey.pub" \
  'succeeded' 0 '"terminal length 0"|"terminal width 512"|"show clock"|"exit"|' \
  -- --cmd 'show clock'
SSH="identities = [\"$TMP/stranger\"]"
KEYLOGIN= AUTH=
parity_case 'S13c a key the device refuses, no password offered' accept-new cisco_iosxe "$KEYS" '' "-start-privileged -user $OPERATOR -authorized-keys $TMP/opkey.pub" \
  'authentication_error:authentication_failed' 108 '' \
  -- --cmd 'show clock'
unset SSH KEYLOGIN AUTH
# S13d: keyboard-interactive is tried before the password method, both
# answered with the password; a server that refuses keyboard-interactive
# takes the password method.
AUTH=password
parity_case 'S13d a server without keyboard-interactive takes the password method' accept-new cisco_iosxe "$BUILTIN" en '-no-keyboard-interactive' \
  'succeeded' 0 "$OPEN"'"show clock"|"exit"|' \
  -- --cmd 'show clock'
unset AUTH

# S35: the exec channel over the Linux persona, logged in by the operator's
# key (S13b's), one connection carrying a channel per command. Two
# differences are by design and pinned: OpenSSH's client names no signal
# (exit_signal unnamed on system, the name on scrapligo-v1), and a command
# given up on system carries remote_command_not_stopped, since its client
# cannot ask the device to end it.
LINUX='[platform.linux]
ssh-port = PORT'
LINUXFLAGS="-persona linux -user $OPERATOR -authorized-keys $TMP/opkey.pub"
# A first record's notice when the stream's session stored the fake's key.
ENROLLED='{"code":"host_key_enrolled","message":"ssh accepted new host key for fake-iosxe (ED25519)","details":{"key_type":"ED25519"}}'
NOT_STOPPED="[{\"code\":\"remote_command_not_stopped\",\"message\":\"the command's channel was closed and the command may still be running on the device: OpenSSH's client cannot ask the device to end it\"}]"
SSH="identities = [\"$TMP/opkey\"]"
KEYLOGIN=$OPKEY AUTH=publickey
CHANNELS=8
PIN='3.exit_signal=system:"unnamed";native:"TERM"
3.error.message=system:"ended by signal unnamed";native:"ended by signal TERM"
5.notices=system:'"$NOT_STOPPED"';native:[]'
parity_case 'S35a exec: each way a command ends, under continue' accept-new linux "$LINUX" '' "$LINUXFLAGS" \
  'succeeded,succeeded,device_error:command_exit_nonzero,device_error:command_exit_signal,device_error:command_exit_missing,timeout:command_timeout,device_error:command_exit_nonzero,succeeded' 107 \
  '"exec: uname -snrm"|"exec: both"|"exec: fail 3"|"exec: signal TERM"|"exec: nostatus"|"exec: slow"|"exec: ls /nonexistent"|"exec: sudo -n id -u"|' \
  -- --continue-device-on-error --cmd 'uname -snrm' --cmd both --cmd 'fail 3' --cmd 'signal TERM' --cmd nostatus \
  --cmd slow --cmd 'ls /nonexistent' --cmd 'sudo -n id -u'
CHANNELS=3
# Record 0 is the device's first: each stream starts from an emptied store,
# so it also carries host_key_enrolled (ENROLLED), after system's notice of
# the command given up.
PIN='0.notices=system:'"${NOT_STOPPED%]},$ENROLLED]"';native:['"$ENROLLED"']
1.notices=system:'"$NOT_STOPPED"';native:[]'
parity_case 'S35b exec: the output limit across both streams, the connection serving the next' accept-new linux "$LINUX
[output]
max-command-bytes = 2000" '' "$LINUXFLAGS" \
  'output_limit_exceeded:output_limit_exceeded,output_limit_exceeded:output_limit_exceeded,succeeded' 111 \
  '"exec: big"|"exec: bigerr"|"exec: uname -snrm"|' \
  -- --continue-device-on-error --cmd big --cmd bigerr --cmd 'uname -snrm'
PIN=
parity_case 'S35c exec: each stream spooled past the threshold' accept-new linux "$LINUX
[output]
spool-threshold-bytes = 4096" '' "$LINUXFLAGS -big-lines 3000" \
  'succeeded,succeeded,succeeded' 0 '"exec: big"|"exec: bigerr"|"exec: both"|' \
  -- --cmd big --cmd bigerr --cmd both
CHANNELS=1
parity_case 'S35d exec: sudo refused, under halt' accept-new linux "$LINUX" '' "$LINUXFLAGS -sudo-asks" \
  "device_error:command_exit_nonzero,$NOT_ATTEMPTED" 107 '"exec: sudo -n id -u"|' \
  -- --cmd 'sudo -n id -u' --cmd 'uname -snrm'
CHANNELS=
parity_case 'S35e linux_shell over the Linux persona'"'"'s shell, with bash'"'"'s decorations' accept-new linux_shell '[platform.linux_shell]
ssh-port = PORT' '' "$LINUXFLAGS -decorations" \
  'succeeded,succeeded,succeeded,succeeded' 0 '"uname -snrm"|"both"|"fail 3"|"ls /nonexistent"|"exit"|' \
  -- --continue-device-on-error --cmd 'uname -snrm' --cmd both --cmd 'fail 3' --cmd 'ls /nonexistent'
unset SSH KEYLOGIN AUTH
# The IOS XE persona refuses every exec request: an alias asking for exec
# is refused at its first command, which never ran, and the session ends.
CHANNELS=1 AUTH=keyboard-interactive
parity_case 'S35f exec: a refused exec request' accept-new iosexec '[platform.iosexec]
driver = "cisco_iosxe"
channel = "exec"
ssh-port = PORT' en '' \
  "connection_error:ssh_session_channel_refused,$NOT_ATTEMPTED" 110 '"exec: show clock"|' \
  -- --continue-device-on-error --cmd 'show clock' --cmd 'show version'
unset CHANNELS AUTH PIN

# S14: host keys. Another device's key for the changed and mismatch cases.
ssh-keygen -q -t ed25519 -N '' -f "$TMP/other" >/dev/null
OTHER=$(cut -d' ' -f1,2 "$TMP/other.pub")
parity_case 'S14a secure, an empty store' secure cisco_iosxe "$BUILTIN" en '' \
  'connection_error:host_key_not_enrolled' 109 '' \
  -- --cmd 'show clock'
OTHERKEY=$OTHER
parity_case 'S14b secure, another key enrolled under the identity' secure cisco_iosxe "$BUILTIN" en '' \
  'connection_error:host_key_changed' 109 '' \
  -- --cmd 'show clock'
KEYSCAN=1
WARNING='SSH host key mismatch for fake-iosxe'
parity_case 'S14c insecure, another key enrolled (the mismatch warning)' insecure cisco_iosxe "$BUILTIN" en '' \
  'succeeded' 0 "$OPEN"'"show clock"|"exit"|' \
  -- --cmd 'show clock'
unset OTHERKEY KEYSCAN WARNING

# S14d: one transport enrolls under accept-new and the other reads the entry
# under secure, in both directions, against one running fake: the identity
# holds the port, so the pair shares a port. The enrolling stream's first
# record carries host_key_enrolled and the reading stream's none, pinned by
# transport.
cross_pair() {  # label, enrolling activity:transport, reading activity:transport, pin
  case $1 in "${ONLY:-}"*) ;; *) return 0 ;; esac
  label=$1
  printf 'name,management_address,platform\nfake-iosxe,127.0.0.1,cisco_iosxe\n' >"$TMP/inv.csv"
  start_fake
  : >"$KH"; chmod 600 "$KH"
  streams=
  for step in "accept-new:$2" "secure:$3"; do
    policy=${step%%:*}; combo=${step#*:}; act=${combo%%:*}; tr=${combo##*:}; tag=$policy.$act.$tr
    write_config "$policy" "$BUILTIN"
    run_karvi "$tag" "$act" "$tr" en --cmd 'show clock'
    [ "$CODE" -eq 0 ] || fail "$label $tag: exit $CODE: $(grep -E '^[a-z_]+:' "$TMP/err.$tag" | head -1)"
    [ "$(awk '{print $1" "$2}' "$KH")" = "[fake-iosxe]:$PORT ssh-ed25519" ] || fail "$label $tag: the trust store holds: $(cat "$KH")"
    streams="$streams $TMP/out.$tag"
  done
  stop_fake
  grep -q '^connections=2 sessions=2$' "$TMP/fake.err" || fail "$label: $(grep '^connections=' "$TMP/fake.err"), expected two connections and two shells"
  # shellcheck disable=SC2086
  result=$("$TMP/bin/paritycheck" -expect succeeded -pin "$4" $streams) || fail "$label: $(printf '%s' "$result" | sed "s|$TMP/out\.||g" | head -12)"
  echo "native smoke: $label: $result"
}
cross_pair 'S14d system enrolls, scrapligo-v1 reads under secure' command:system run:native "0.notices=system:[$ENROLLED];native:[]"
cross_pair 'S14e scrapligo-v1 enrolls, system reads under secure' run:native command:system "0.notices=system:[];native:[$ENROLLED]"

# S15-S17: the timeouts. The system transport awaits
# the first prompt for ssh.connect-timeout plus execution.prompt-timeout and
# scrapligo-v1 for the prompt timeout after its handshake, so the first
# prompt stays away past both.
EXECUTION='prompt-timeout = "1s"'
SHELL_OPENED=1
parity_case 'S15 no first prompt within the login allowance' accept-new cisco_iosxe "$BUILTIN" en '-login-delay 20s' \
  'connection_error:command_session_prompt_timeout' 110 '' \
  -- --cmd 'show clock'
unset SHELL_OPENED
EXECUTION='enable-timeout = "1s"'
parity_case 'S16 no privileged prompt within the enable timeout' accept-new cisco_iosxe "$BUILTIN" en '-secret-delay 20s' \
  "privilege_error:privilege_failed,$NOT_ATTEMPTED" 107 '"enable"|"<secret>"|' \
  -- --cmd 'show clock' --cmd 'show version'
COMMAND_TIMEOUT=5s
EXECUTION='device-timeout = "2s"'
parity_case 'S17 the device timeout cuts the second command under continue' accept-new cisco_iosxe "$BUILTIN" en '-slow 1s' \
  "succeeded,timeout:device_timeout,$NOT_ATTEMPTED" 107 "$OPEN"'"show slow"|"show slow"|' \
  -- --continue-device-on-error --cmd 'show slow' --cmd 'show slow' --cmd 'show clock'
unset COMMAND_TIMEOUT EXECUTION

# S18: keepalives. On `show mute` the fake
# answers nothing more, keepalive requests included, with the connection up;
# each transport's own keepalive keys end the session long before the command
# timeout.
COMMAND_TIMEOUT=30s
SSH='server-alive-interval = "1s"
server-alive-count-max = 2'
parity_case 'S18 a device gone silent is ended by the keepalives' accept-new cisco_iosxe "$BUILTIN
[native-ssh]
keepalive-interval = \"1s\"
keepalive-count-max = 1" en '' \
  "succeeded,connection_error:session_keepalive_timeout,$NOT_ATTEMPTED" 110 "$OPEN"'"show clock"|"show mute"|' \
  -- --continue-device-on-error --cmd 'show clock' --cmd 'show mute' --cmd 'show version'
unset COMMAND_TIMEOUT SSH

# S19-S20: blind sends (the count implying the tolerance). The fake answers
# `clear counters` and `reload` with a [confirm] taking one unechoed key;
# after `reload` nothing more is written, the connection up. A trailing \r
# sends the return in the command's write; the prompt comes back after
# `clear counters` (the exchange in the output, <confirm> among the device's
# lines), and never after `reload`, so the blind wait passes and the record
# is a success with the notice, the session ended without exit.
parity_case 'S19 a [confirm] answered by the escape, the prompt returns' accept-new cisco_iosxe "$BUILTIN" en '' \
  'succeeded,succeeded' 0 "$OPEN"'"clear counters"|"<confirm>"|"show clock"|"exit"|' \
  -- --cmd 'clear counters\r' --cmd 'show clock'
EXECUTION='blind-wait = "1s"'
parity_case 'S20 a [confirm] answered by the escape, the prompt never returns' accept-new cisco_iosxe "$BUILTIN" en '' \
  'succeeded' 0 "$OPEN"'"reload"|"<confirm>"|' \
  -- --cmd 'reload\r'
unset EXECUTION

# S21-S24: expect-and-send and the declared tolerance. The fake's
# `copy running-config startup-config` asks
# `Destination filename [startup-config]? ` and takes one echoed line,
# recorded <value:TEXT> (<value:> for the default); under -unsaved its
# `reload` first asks `System configuration has been modified. Save?
# [yes/no]: ` and then the D3 [confirm]. S21 answers the value prompt with a
# bare return and the prompt returns. S22 declares a pattern the device never
# writes: the command timeout ends the session, the message naming the last
# line seen and none of one answered, the same on both transports. S23 and
# S24 are the D4 note's confirmed reload, `--blind` with the save and confirm
# declarations: under -unsaved both prompts are answered (<value:y>, then
# <confirm>), without it the save declaration stays unconsumed and the
# [confirm] alone is answered; either way the device goes quiet, the blind
# wait passes, and the record is a success with the notice.
parity_case 'S21 a value prompt answered by --expect, the prompt returns' accept-new cisco_iosxe "$BUILTIN" en '' \
  'succeeded,succeeded' 0 "$OPEN"'"copy running-config startup-config"|"<value:>"|"show clock"|"exit"|' \
  -- --cmd 'copy running-config startup-config' --expect 'filename \[startup-config\]\?=' --cmd 'show clock'
parity_case 'S22 a mistyped pattern waits out the command timeout' accept-new cisco_iosxe "$BUILTIN" en '' \
  'timeout:command_timeout' 107 "$OPEN"'"copy running-config startup-config"|' \
  -- --cmd 'copy running-config startup-config' --expect 'Destinaton=x'
EXECUTION='blind-wait = "1s"'
parity_case 'S23 a blind reload with the save and confirm declarations, unsaved' accept-new cisco_iosxe "$BUILTIN" en '-unsaved' \
  'succeeded' 0 "$OPEN"'"reload"|"<value:y>"|"<confirm>"|' \
  -- --cmd reload --blind --expect 'Save\? \[yes/no\]:=y' --expect 'confirm\]='
parity_case 'S24 a blind reload with the save and confirm declarations, saved' accept-new cisco_iosxe "$BUILTIN" en '' \
  'succeeded' 0 "$OPEN"'"reload"|"<confirm>"|' \
  -- --cmd reload --blind --expect 'Save\? \[yes/no\]:=y' --expect 'confirm\]='
unset EXECUTION

# S25: an alias table's paging-commands.
# The row names the alias c9300; its table is IOS XE with one paging command
# in place of the built-in's two, so after enable the fake sees exactly one
# paging line, the same on both transports; the record's platform is the
# alias name. The section's usage refusals (--platform values, the run
# selector) never connect and are covered by the app and CLI tests.
ALIAS_PAGING='[platform.c9300]
driver = "cisco_iosxe"
ssh-port = PORT
paging-commands = ["terminal length 0"]'
parity_case 'S25 an alias with its own paging-commands sends one paging line' accept-new c9300 "$ALIAS_PAGING" en '' \
  'succeeded' 0 '"enable"|"<secret>"|"terminal length 0"|"show clock"|"exit"|' \
  -- --cmd 'show clock'

# S30: start statements for generic devices.
# The row has no platform, as a target outside the inventory has none, so
# under this suite's cleared platform-resolution.default (the shipped
# value is cisco_iosxe) it runs as generic with the warning
# platform_not_set. The generic table's
# paging-commands are the platform's start statements: the fake sees them
# before the requested command and sees no enable, the same on both
# transports. A [[session-init-map]] rule of platform = "generic" would not
# reach this row (a platform that is not set matches no platform rule); the
# table does, because it acts on the platform the device runs as. The table
# also carries ssh-port, so the row reaches the fake and nothing else.
GENERIC_START='[platform.generic]
ssh-port = PORT
paging-commands = ["terminal length 0", "terminal width 512"]'
WARNING='warning: platform_not_set: ' \
parity_case 'S30 a platform-less row receives the generic table start statements' accept-new '' "$GENERIC_START" '' '' \
  'succeeded' 0 '"terminal length 0"|"terminal width 512"|"show clock"|"exit"|' \
  -- --cmd 'show clock'

# S26: an unknown row platform under on-unknown = "warn". The row names
# cisco_iosx, a typo; the fallback is
# cisco_iosxe, so the fake sees enable and both paging lines on both
# transports, the client prints the warning line on every path, and the
# record's platform is the fallback with the notice platform_unknown_fallback
# on the first record. The default, fail, never connects and is covered by
# the planner and CLI tests.
WARN_FALLBACK="$BUILTIN"'
[platform-resolution]
on-unknown = "warn"
unknown-fallback = "cisco_iosxe"'
WARNING='warning: platform_unknown_fallback: inventory source parity line 2: platform "cisco_iosx" is not a known platform; 1 device proceeds as cisco_iosxe (platform-resolution.unknown-fallback): fake-iosxe' \
parity_case 'S26 an unknown row platform falls back under warn with the notice' accept-new cisco_iosx "$WARN_FALLBACK" en '' \
  'succeeded' 0 "$OPEN"'"show clock"|"exit"|' \
  -- --cmd 'show clock'
if [ -z "${ONLY:-}" ] || [ "${ONLY}" = S26 ]; then
  last=$(ls -d "$TMP"/base/jobs/*/* 2>/dev/null | tail -1)
  jsonl_records "$last/commands.jsonl" platform 2>/dev/null | grep -qx cisco_iosxe || fail "S26: the record's platform is not the fallback cisco_iosxe"
  jsonl_records "$last/commands.jsonl" 'notices.*.code' 2>/dev/null | head -1 | tr , '\n' | grep -qx platform_unknown_fallback || fail "S26: the first record carries no platform_unknown_fallback notice"
fi

# S27: promptbefore is the prompt a statement was sent at, prompt the one
# that came back: they differ across a mode
# change, and `end` answers with nothing but the next prompt. The parity
# check compares both fields in all four streams; the rows below read the
# last run's records.
unset WARNING
parity_case 'S27 promptbefore across a mode change, and a statement with no answer' accept-new cisco_iosxe "$BUILTIN" en '' \
  'succeeded,succeeded,succeeded,succeeded' 0 "$OPEN"'"configure terminal"|"interface Loopback0"|"end"|"show clock"|"exit"|' \
  -- --cmd 'configure terminal' --cmd 'interface Loopback0' --cmd 'end' --cmd 'show clock'
if [ -z "${ONLY:-}" ] || [ "${ONLY}" = S27 ]; then
  last=$(ls -d "$TMP"/base/jobs/*/* 2>/dev/null | tail -1)
  # s27_row LINE PROMPTBEFORE PROMPT OUTPUT_BYTES
  s27_row() {
    # The line goes from sed to the reader: `echo` would expand the
    # record's own \n escapes.
    for s27_pair in "promptbefore=$2" "prompt=$3" "output_bytes=$4"; do
      sed -n "$1p" "$last/commands.jsonl" | json_is - "${s27_pair%%=*}" "${s27_pair#*=}" ||
        fail "S27: record $1 ${s27_pair%%=*} is $(sed -n "$1p" "$last/commands.jsonl" | json_get - "${s27_pair%%=*}"), expected ${s27_pair#*=}"
    done
  }
  s27_row 1 'Router#' 'Router(config)#' 62
  # An accepted configuration line answers with nothing but the next prompt.
  s27_row 2 'Router(config)#' 'Router(config-if)#' 0
  s27_row 3 'Router(config-if)#' 'Router#' 0
  s27_row 4 'Router#' 'Router#' 34
  # The device's text file: after the header, karvi's own set-up as the
  # session sent it (no record behind it; the secret is not a line), then
  # the prompt each statement was sent at and the statement on one line,
  # the accepted line and `end` with no answer under them; and the file
  # less its set-up lines equal to its derivation from the records.
  s27_text=$last/output.fake-iosxe.txt
  [ "$(sed -n 1p "$s27_text" | cut -c1-17)" = '! ### fake-iosxe ' ] || fail "S27: the text file's header is $(sed -n 1p "$s27_text")"
  # The header's time is display.timestamp in the effective zone: here the
  # default pattern, hh:mm:ss yyyy-mm-dd, not RFC 3339.
  sed -n 1p "$s27_text" | grep -Eq '^! ### fake-iosxe( \([^)]*\))? [0-9]{2}:[0-9]{2}:[0-9]{2} [0-9]{4}-[0-9]{2}-[0-9]{2} ###$' || fail "S27: the header's time is not in display.timestamp's shape: $(sed -n 1p "$s27_text")"
  [ "$(sed -n '2,5p' "$s27_text" | tr '\n' '|')" = 'Router>enable|Password:|Router#terminal length 0|Router#terminal width 512|' ] ||
    fail "S27: the text file's set-up lines are $(sed -n '2,5p' "$s27_text" | tr '\n' '|')"
  [ "$(sed -n '6p;8,10p' "$s27_text" | tr '\n' '|')" = 'Router#configure terminal|Router(config)#interface Loopback0|Router(config-if)#end|Router#show clock|' ] ||
    fail "S27: the text file's statement lines are $(sed -n '6p;8,10p' "$s27_text" | tr '\n' '|')"
  "$TMP/bin/textfile" "$last/commands.jsonl" fake-iosxe >"$TMP/s27.derived"
  sed '2,5d' "$s27_text" | cmp -s - "$TMP/s27.derived" || fail "S27: $s27_text less its set-up lines differs from its derivation from commands.jsonl"
fi

# S28: a device that echoes the enable secret and then returns no prompt.
# Nothing read after the secret is sent is kept: the secret
# is in no output file, no stream, and no message, on either transport.
# Through v0.12.1 the timeout's message quoted it as the "last output".
S28_SECRET=Zq7-echoed-SECRET
EXECUTION='enable-timeout = "1s"'
parity_case 'S28 an echoed enable secret reaches nothing' accept-new cisco_iosxe "$BUILTIN" "$S28_SECRET" "-enable $S28_SECRET -echo-secret -secret-delay 20s" \
  "privilege_error:privilege_failed" 107 '"enable"|"<secret>"|' \
  -- --cmd 'show clock'
unset EXECUTION
if [ -z "${ONLY:-}" ] || [ "${ONLY}" = S28 ]; then
  s28_hits=$(grep -rl -- "$S28_SECRET" "$TMP/base" "$TMP"/out.* "$TMP"/err.* 2>/dev/null || true)
  [ -z "$s28_hits" ] || fail "S28: the echoed enable secret is in $s28_hits"
  s28_text=$(ls -d "$TMP"/base/jobs/*/* | tail -1)/output.fake-iosxe.txt
  [ "$(sed -n '2,3p' "$s28_text" | tr '\n' '|')" = 'Router>enable|Password:|' ] || fail "S28: the text file's set-up lines are $(sed -n '2,3p' "$s28_text" | tr '\n' '|')"
fi

# S29: output.files. Each key switches its own file; all eight false is no
# job folder on `command`, `run --no-daemon`, and a run through the daemon
# alike (the invocation decides the files on every path); the summary names
# only the files that are written; output.max-job-bytes counts the text, and
# a record past it is an output failure with the code on standard error.
S29_ALL='[output.files]
output-txt = false
commands-jsonl = false
commands-txt = false
errors-jsonl = false
failed-devices-txt = false
manifest-json = false
metrics-json = false
summary-json = false'
# job_run TAG ACTIVITY TABLES FAKEFLAGS karvi args...: one fake, one run
# through run_karvi under the tag; sets CODE, and JOB_DIR to the run's job
# folder (empty if there is none). S29 and S31 use it.
job_run() {
  job_tag=$1; job_act=$2; job_tables=$3; job_fflags=$4; shift 4
  printf 'name,management_address,platform\nfake-iosxe,127.0.0.1,cisco_iosxe\n' >"$TMP/inv.csv"
  # shellcheck disable=SC2086
  start_fake -port 0 $job_fflags
  : >"$KH"; chmod 600 "$KH"
  write_config accept-new "$BUILTIN
$job_tables"
  run_karvi "$job_tag" "$job_act" native en "$@"
  stop_fake
  JOB_DIR=$(ls -d "$TMP"/base/jobs/*/* 2>/dev/null | tail -1)
}
job_files() { ls "$JOB_DIR" | tr '\n' ' '; }
if [ -z "${ONLY:-}" ] || [ "${ONLY}" = S29 ]; then
  for s29_activity in command run; do
    job_run s29 "$s29_activity" "$S29_ALL" '' --cmd 'show clock'
    [ "$CODE" -eq 0 ] || fail "S29 all false, $s29_activity: exit $CODE"
    [ -z "$JOB_DIR" ] || fail "S29 all false, $s29_activity: a job folder was made: $JOB_DIR"
    # A run's jsonl ends with the summary line; a command's does not.
    s29_lines=1; [ "$s29_activity" = command ] || s29_lines=2
    [ "$(wc -l <"$TMP/out.s29")" -eq "$s29_lines" ] || fail "S29 all false, $s29_activity: $(wc -l <"$TMP/out.s29") lines displayed, expected $s29_lines"
  done
  tail -1 "$TMP/out.s29" >"$TMP/s29.last"
  json_is "$TMP/s29.last" final_status completed || fail "S29 all false, run: the last line is not the summary: $(tail -1 "$TMP/out.s29")"
  ! grep -q ' artifacts=' "$TMP/err.s29" || fail "S29 all false, run: a result line was printed: $(tail -1 "$TMP/err.s29")"

  job_run s29 daemon "$S29_ALL" '' --cmd 'show clock'
  [ "$CODE" -eq 0 ] || fail "S29 all false, daemon: exit $CODE: $(grep -E '^[a-z_]+:' "$TMP/err.s29" | head -1)"
  [ -z "$JOB_DIR" ] || fail "S29 all false, daemon: a job folder was made: $JOB_DIR"
  [ "$(wc -l <"$TMP/out.s29")" -eq 2 ] || fail "S29 all false, daemon: $(wc -l <"$TMP/out.s29") lines displayed, expected the record and the summary"
  tail -1 "$TMP/out.s29" >"$TMP/s29.last"
  json_is "$TMP/s29.last" final_status completed || fail "S29 all false, daemon: the last line is not the summary: $(tail -1 "$TMP/out.s29")"
  ! grep -q ' artifacts=' "$TMP/err.s29" || fail "S29 all false, daemon: a result line was printed: $(tail -1 "$TMP/err.s29")"
  ! grep -q '^warning:' "$TMP/err.s29" || fail "S29 all false, daemon: a warning: $(grep '^warning:' "$TMP/err.s29" | head -1)"

  job_run s29 run '[output.files]
commands-jsonl = false
metrics-json = false' '' --cmd 'show clock'
  [ "$(job_files)" = 'commands.txt errors.jsonl failed-devices.txt manifest.json output.fake-iosxe.txt summary.json ' ] || fail "S29 two false: the folder holds $(job_files)"
  grep -q '^Router#show clock$' "$JOB_DIR/output.fake-iosxe.txt" || fail "S29 two false: the text file is not written without commands.jsonl"
  for s29_path in paths.commands_jsonl paths.metrics output.commands_jsonl; do
    if json_has "$JOB_DIR/summary.json" "$s29_path"; then fail "S29 two false: the summary names $s29_path, a file not written"; fi
  done
  json_is "$JOB_DIR/summary.json" paths.commands_txt "$JOB_DIR/commands.txt" || fail "S29 two false: the summary's paths lack commands_txt"

  # One `show big` is 5,256,000 bytes: its line fits 8 MiB, its text does
  # not (a notice, the job goes on), and the second line does not either.
  COMMAND_TIMEOUT=60s
  job_run s29 run '[output]
max-command-bytes = 6291456
max-job-bytes = 8388608' '-big-lines 72000' --cmd 'show big' --cmd 'show big'
  unset COMMAND_TIMEOUT
  [ "$CODE" -eq 111 ] || fail "S29 limit: exit $CODE, expected 111"
  grep -q '^warning: output_text_write_failed: output.fake-iosxe.txt is not written from record 1 on: output_job_limit_exceeded' "$TMP/err.s29" || fail "S29 limit: no notice for the text"
  grep -q '^output_job_limit_exceeded: .*device fake-iosxe' "$TMP/err.s29" || fail "S29 limit: the code is not on standard error: $(tail -1 "$TMP/err.s29")"
  [ "$(cat "$JOB_DIR/failed-devices.txt")" = fake-iosxe ] || fail "S29 limit: failed-devices.txt is '$(cat "$JOB_DIR/failed-devices.txt")'"
  case $(json_get "$JOB_DIR/summary.json" terminal_causes.0) in
    output:output_job_limit_exceeded*) ;;
    *) fail "S29 limit: the summary's cause is $(json_get "$JOB_DIR/summary.json" terminal_causes.0)" ;;
  esac
  [ "$(wc -l <"$JOB_DIR/commands.jsonl")" -eq 1 ] || fail "S29 limit: commands.jsonl holds $(wc -l <"$JOB_DIR/commands.jsonl") records, expected 1"
  [ ! -e "$JOB_DIR/output.fake-iosxe.txt" ] || fail "S29 limit: a text block past the limit was written"
  echo 'native smoke: S29 output.files: all false, the daemon, two false, the limit: ok'
fi

# S32: the invocation decides the files through the daemon: `run --nof` is
# no folder and the records over the
# socket; `run --of=PATH` writes the folder under PATH; two keys false reach
# the daemon's store; `--nof` with `--detach` or `--exercise` is refused.
if [ -z "${ONLY:-}" ] || [ "${ONLY}" = S32 ]; then
  job_run s32 daemon '' '' --nof --cmd 'show clock' --cmd 'show version'
  [ "$CODE" -eq 0 ] || fail "S32 --nof: exit $CODE: $(grep -E '^[a-z_]+:' "$TMP/err.s32" | head -1)"
  [ -z "$JOB_DIR" ] || fail "S32 --nof: a job folder was made: $JOB_DIR"
  [ "$(wc -l <"$TMP/out.s32")" -eq 3 ] || fail "S32 --nof: $(wc -l <"$TMP/out.s32") lines displayed, expected two records and the summary"
  sed -n 1p "$TMP/out.s32" >"$TMP/s32.first"
  json_is "$TMP/s32.first" status succeeded || fail "S32 --nof: the first record is $(json_get "$TMP/s32.first" status)"
  tail -1 "$TMP/out.s32" >"$TMP/s32.last"
  json_is "$TMP/s32.last" final_status completed || fail "S32 --nof: the last line is not the summary: $(tail -1 "$TMP/out.s32")"
  ! grep -q ' artifacts=' "$TMP/err.s32" || fail "S32 --nof: a result line was printed: $(tail -1 "$TMP/err.s32")"
  job_run s32 daemon '' '' "--of=$TMP/of" --cmd 'show clock'
  [ "$CODE" -eq 0 ] || fail "S32 --of: exit $CODE: $(grep -E '^[a-z_]+:' "$TMP/err.s32" | head -1)"
  [ -z "$JOB_DIR" ] || fail "S32 --of: a folder under the configured root: $JOB_DIR"
  S32_OF=$(ls -d "$TMP"/of/*/* 2>/dev/null | tail -1)
  [ -n "$S32_OF" ] && [ -s "$S32_OF/commands.jsonl" ] && [ -s "$S32_OF/summary.json" ] || fail "S32 --of: no job folder under $TMP/of"
  tail -1 "$TMP/out.s32" >"$TMP/s32.last"
  json_is "$TMP/s32.last" paths.summary "$S32_OF/summary.json" || fail "S32 --of: the summary line does not name $S32_OF: $(tail -1 "$TMP/out.s32")"
  job_run s32 daemon '[output.files]
commands-jsonl = false
metrics-json = false' '' --cmd 'show clock'
  [ "$CODE" -eq 0 ] || fail "S32 two false: exit $CODE"
  [ "$(job_files)" = 'commands.txt errors.jsonl failed-devices.txt manifest.json output.fake-iosxe.txt summary.json ' ] || fail "S32 two false through the daemon: the folder holds $(job_files)"
  [ "$(wc -l <"$TMP/out.s32")" -eq 2 ] || fail "S32 two false: $(wc -l <"$TMP/out.s32") lines displayed, expected the record and the summary"
  for s32_pair in '--nof --detach' '--nof --exercise' '--nof --of'; do
    # shellcheck disable=SC2086
    job_run s32 daemon '' '' $s32_pair --cmd 'show clock'
    [ "$CODE" -eq 4 ] || fail "S32 $s32_pair: exit $CODE, expected the usage refusal"
    [ -z "$JOB_DIR" ] || fail "S32 $s32_pair: a job folder was made"
  done
  grep -q '^output_options_conflict:' "$TMP/err.s32" || fail "S32 --nof --of: $(head -1 "$TMP/err.s32")"
  echo 'native smoke: S32 the invocation decides the files through the daemon: --nof, --of=PATH, two keys false, the refusals: ok'
fi

# S31: the follow stream's frame bound. With daemon.max-ipc-frame-bytes at its minimum, one `show big`
# (1000 lines, 73,000 bytes) is a line past the bound: the follower gets
# the record with `output` empty and the notice follow_output_omitted,
# output_bytes intact; the `show clock` record after it is whole; the file
# holds the whole line; the job succeeds. The big record is the device's
# first, from an emptied store, so host_key_enrolled comes before the follow
# notice, and is the file's record's one notice.
if [ -z "${ONLY:-}" ] || [ "${ONLY}" = S31 ]; then
  job_run s31 daemon '[daemon]
max-ipc-frame-bytes = 65536' '' --cmd 'show big' --cmd 'show clock'
  [ "$CODE" -eq 0 ] || fail "S31: exit $CODE: $(grep -E '^[a-z_]+:' "$TMP/err.s31" | head -1)"
  [ "$(wc -l <"$TMP/out.s31")" -eq 3 ] || fail "S31: $(wc -l <"$TMP/out.s31") lines displayed, expected two records and the summary"
  sed -n 1p "$TMP/out.s31" >"$TMP/s31.big"; sed -n 2p "$TMP/out.s31" >"$TMP/s31.clock"
  json_is "$TMP/s31.big" output '' || fail "S31: the big record's output was not left out"
  json_is "$TMP/s31.big" output_bytes 73000 || fail "S31: the big record's output_bytes is $(json_get "$TMP/s31.big" output_bytes)"
  json_is "$TMP/s31.big" notices.0 "$ENROLLED" || fail "S31: the big record's first notice is $(json_get "$TMP/s31.big" notices)"
  json_is "$TMP/s31.big" notices.1.code follow_output_omitted || fail "S31: the big record's notice is $(json_get "$TMP/s31.big" notices)"
  json_is "$TMP/s31.big" notices.1.details.max_frame_bytes 65536 || fail "S31: the notice's details are $(json_get "$TMP/s31.big" notices.1.details)"
  case $(json_get "$TMP/s31.big" notices.1.message) in
    "output of 73000 bytes left out of the follow stream: the record's "*"-byte line is more than daemon.max-ipc-frame-bytes (65536) allows in a frame; the output is in commands.jsonl") ;;
    *) fail "S31: the notice's message is $(json_get "$TMP/s31.big" notices.1.message)" ;;
  esac
  [ "$(json_get "$TMP/s31.clock" status)" = succeeded ] && [ "$(json_get "$TMP/s31.clock" notices.0.code 2>/dev/null)" = "" ] || fail "S31: the clock record is not whole: $(json_get "$TMP/s31.clock" notices)"
  [ "$(wc -c <"$TMP/s31.big")" -lt 65536 ] || fail "S31: the omitted record is $(wc -c <"$TMP/s31.big") bytes"
  [ "$(sed -n 1p "$JOB_DIR/commands.jsonl" | wc -c)" -gt 65536 ] || fail "S31: commands.jsonl line 1 is $(sed -n 1p "$JOB_DIR/commands.jsonl" | wc -c) bytes; the file must hold the whole output"
  sed -n 1p "$JOB_DIR/commands.jsonl" >"$TMP/s31.file"
  json_is "$TMP/s31.file" notices "[$ENROLLED]" || fail "S31: the file's record carries $(json_get "$TMP/s31.file" notices)"
  echo 'native smoke: S31 the frame bound: the output left out with follow_output_omitted, the file whole: ok'
fi

[ "$(host_store_digest "$OWN_STORE")" = "$OWN_BEFORE" ] || fail "the operator's trust store $OWN_STORE changed"
stop_fake
if pgrep -f "$TMP/bin/fake" >/dev/null 2>&1 || pgrep -f "daemon serve.*$TMP" >/dev/null 2>&1; then
  fail "a fake or a daemon is still running"
fi
echo 'native smoke: pass'

# S33: the collection run. Two rows: the fake,
# and `dead`, an alias of cisco_iosxe on a port nothing listens on. A crun
# with no command sends the platform's crun-commands; the fake's file is
# replaced (the marker, the blank line before the second marker, the
# configuration and the version, mode 0660, a stale temporary swept);
# dead's previous file is kept with nothing left behind; the job folder
# has a commands.PLATFORM.txt per platform name (the alias inherits the
# built-in's list under its own name) and no output.NAME.txt; the summary and the
# result line count both; a rejected statement still replaces; --cd=PATH
# through the daemon; --nof collects.
if [ -z "${ONLY:-}" ] || [ "${ONLY}" = S33 ]; then
  s33_run() {  # tag, activity (run|daemon), karvi args...; sets CODE, JOB_DIR
    s33_tag=$1; s33_act=$2; shift 2
    case $s33_act in daemon) s33_sub=crun ;; *) s33_sub='crun --no-daemon' ;; esac
    rm -rf "${TMP:?}/base" "${TMP:?}/home"; install -d -m 700 "$TMP/base" "$TMP/home"
    CODE=0
    # shellcheck disable=SC2086
    env NETENABLE=en HOME="$TMP/home" NETUSER=netops NETPASS=pw \
      "$KARVI" --config "$TMP/karvi.toml" --set 'platform-resolution.default=""' $s33_sub --format jsonl --transport native "$@" \
      >"$TMP/out.$s33_tag" 2>"$TMP/err.$s33_tag" || CODE=$?
    [ "$s33_act" != daemon ] || stop_daemon
    JOB_DIR=$(ls -d "$TMP"/base/jobs/*/* 2>/dev/null | tail -1)
  }
  printf 'name,management_address,platform\nfake-iosxe,127.0.0.1,cisco_iosxe\ndead,127.0.0.1,dead\n' >"$TMP/inv.csv"
  start_fake -port 0 -hostname fake-iosxe
  : >"$KH"; chmod 600 "$KH"
  rm -rf "${TMP:?}/crun" "${TMP:?}/crun2"; install -d -m 770 "$TMP/crun"
  echo 'old dead' >"$TMP/crun/dead"; : >"$TMP/crun/.fake-iosxe.260924-000000-00"
  write_config accept-new "$BUILTIN
[platform.dead]
driver = \"cisco_iosxe\"
ssh-port = 1
[crun]
directory = \"$TMP/crun\""
  s33_run s33 run --target fake-iosxe --target dead
  [ "$CODE" -eq 101 ] || fail "S33: exit $CODE, expected the partial failure: $(grep -E '^[a-z_]+:' "$TMP/err.s33" | head -1)"
  # The file under cisco_iosxe's built-in crun-filters: the byte count, the clock period, the uptime, and the constant
  # first line dropped; the last-change stamp kept.
  printf '! show running-config\n\n!\n! Last configuration change at 10:00:00 UTC Tue Sep 15 2026\n!\nversion 17.9\nhostname fake-iosxe\n!\ninterface GigabitEthernet1\n ip address 192.0.2.1 255.255.255.0\n!\nend\n\n! show version\nCisco IOS XE Software, Version 17.09.04a\n' >"$TMP/s33.want"
  printf '! show running-config\nBuilding configuration...\n\nCurrent configuration : 512 bytes\n!\n! Last configuration change at 10:00:00 UTC Tue Sep 15 2026\n!\nversion 17.9\nhostname fake-iosxe\n!\ninterface GigabitEthernet1\n ip address 192.0.2.1 255.255.255.0\n!\nntp clock-period 17179869\nend\n\n! show version\nCisco IOS XE Software, Version 17.09.04a\nfake-iosxe uptime is 1 day\n' >"$TMP/s33raw.want"
  cmp -s "$TMP/crun/fake-iosxe" "$TMP/s33.want" || fail "S33: the collection file differs: $(diff "$TMP/s33.want" "$TMP/crun/fake-iosxe" | head -3)"
  [ "$(stat -c %a "$TMP/crun/fake-iosxe")" = 660 ] || fail "S33: the file's mode is $(stat -c %a "$TMP/crun/fake-iosxe"), expected 660"
  [ "$(cat "$TMP/crun/dead")" = 'old dead' ] || fail "S33: dead's previous file was touched"
  [ "$(ls -A "$TMP/crun" | tr '\n' ' ')" = 'dead fake-iosxe ' ] || fail "S33: the directory holds $(ls -A "$TMP/crun" | tr '\n' ' ')"
  [ "$(job_files)" = 'commands.cisco_iosxe.txt commands.dead.txt commands.jsonl errors.jsonl failed-devices.txt manifest.json metrics.json summary.json ' ] || fail "S33: the job folder holds $(job_files)"
  [ "$(json_get "$JOB_DIR/summary.json" collection.replaced)" = 1 ] && [ "$(json_get "$JOB_DIR/summary.json" collection.kept)" = 1 ] || fail "S33: the summary's collection block: $(json_get "$JOB_DIR/summary.json" collection.replaced) replaced, $(json_get "$JOB_DIR/summary.json" collection.kept) kept"
  [ "$(json_get "$JOB_DIR/summary.json" collection.devices.dead.outcome)" = kept ] || fail "S33: dead is $(json_get "$JOB_DIR/summary.json" collection.devices.dead.outcome)"
  # Under jsonl the stream's last line is the summary, which carries the
  # counts; no line follows on stderr (display.collection.footer is text's).
  tail -1 "$TMP/out.s33" >"$TMP/s33.last"
  [ "$(json_get "$TMP/s33.last" collection.replaced)" = 1 ] && [ "$(json_get "$TMP/s33.last" collection.kept)" = 1 ] || fail "S33: the stream's summary: $(cat "$TMP/s33.last" | head -c 300)"
  ! grep -q 'collection=' "$TMP/err.s33" || fail "S33: a collection line on stderr: $(grep 'collection=' "$TMP/err.s33")"
  s33_run s33b run --target fake-iosxe --cmd 'show bogus' --cmd 'show clock'
  [ "$CODE" -eq 101 ] || fail "S33 rejected: exit $CODE"
  printf '! show bogus\n     ^\n%% Invalid input detected at '"'"'^'"'"' marker.\n\n! show clock\n*10:00:00.000 UTC Tue Sep 15 2026\n' >"$TMP/s33b.want"
  cmp -s "$TMP/crun/fake-iosxe" "$TMP/s33b.want" || fail "S33 rejected: the file differs: $(diff "$TMP/s33b.want" "$TMP/crun/fake-iosxe" | head -3)"
  [ -f "$JOB_DIR/commands.txt" ] && [ ! -f "$JOB_DIR/commands.cisco_iosxe.txt" ] || fail "S33 rejected: the command files are $(job_files)"
  s33_run s33c daemon --target fake-iosxe --cd="$TMP/crun2"
  [ "$CODE" -eq 0 ] || fail "S33 daemon --cd: exit $CODE: $(grep -E '^[a-z_]+:' "$TMP/err.s33c" | head -1)"
  cmp -s "$TMP/crun2/fake-iosxe" "$TMP/s33.want" || fail "S33 daemon --cd: the file differs"
  [ "$(stat -c %a "$TMP/crun2")" = 770 ] || fail "S33 daemon --cd: the directory was made at $(stat -c %a "$TMP/crun2"), expected 770"
  [ "$(json_get "$JOB_DIR/summary.json" collection.directory)" = "$TMP/crun2" ] && [ "$(json_get "$JOB_DIR/summary.json" collection.replaced)" = 1 ] || fail "S33 daemon --cd: the summary's collection: $(json_get "$JOB_DIR/summary.json" collection.directory)"
  rm -f "$TMP/crun2/fake-iosxe"
  s33_run s33d run --target fake-iosxe --nof --cd="$TMP/crun2"
  [ "$CODE" -eq 0 ] && [ -z "$JOB_DIR" ] && cmp -s "$TMP/crun2/fake-iosxe" "$TMP/s33.want" || fail "S33 --nof: exit $CODE, folder '$JOB_DIR'"
  # The hook, crun.after: run in the collection
  # directory after the result line with the replaced files on stdin and
  # the job in the environment, on the in-process path and through the
  # daemon; its exit code is a warning and the run's stands; a hook past
  # crun.after-timeout is ended with its child.
  cat >"$TMP/after.sh" <<'EOF_HOOK'
#!/bin/sh
{ echo "pwd=$PWD"; echo "job=$KARVI_JOB_ID dir=$KARVI_JOB_DIR crun=$KARVI_CRUN_DIRECTORY replaced=$KARVI_CRUN_REPLACED kept=$KARVI_CRUN_KEPT exit=$KARVI_EXIT"; echo stdin:; cat; } >"$AFTER_OUT"
[ -z "${AFTER_SLEEP:-}" ] || sleep "$AFTER_SLEEP"
echo 'hook ran'
exit "${AFTER_EXIT:-0}"
EOF_HOOK
  chmod 755 "$TMP/after.sh"; export AFTER_OUT="$TMP/after.out" AFTER_EXIT=0 AFTER_SLEEP=
  write_config accept-new "$BUILTIN
[platform.dead]
driver = \"cisco_iosxe\"
ssh-port = 1
[crun]
directory = \"$TMP/crun\"
after = \"$TMP/after.sh\"
after-timeout = \"1s\""
  s33_run s33e run --target fake-iosxe --target dead
  [ "$CODE" -eq 101 ] || fail "S33 hook: exit $CODE"
  printf 'pwd=%s\njob=%s dir=%s crun=%s replaced=1 kept=1 exit=101\nstdin:\nfake-iosxe\n' "$TMP/crun" "$(basename "$JOB_DIR")" "$JOB_DIR" "$TMP/crun" >"$TMP/after.want"
  cmp -s "$TMP/after.out" "$TMP/after.want" || fail "S33 hook: the hook's input differs: $(diff "$TMP/after.want" "$TMP/after.out" | head -4)"
  grep -q '^hook ran$' "$TMP/err.s33e" && ! grep -q 'crun_after_failed' "$TMP/err.s33e" || fail "S33 hook: stderr is $(tail -3 "$TMP/err.s33e")"
  [ "$(tail -1 "$TMP/err.s33e")" = 'hook ran' ] || fail "S33 hook: the hook's output follows the result line: $(tail -2 "$TMP/err.s33e")"
  ! grep -q 'hook ran' "$TMP/out.s33e" || fail "S33 hook: the hook wrote to stdout"
  grep -q '"event_name":"crun.after.succeeded"' "$TMP/base/audit.jsonl" || fail "S33 hook: no crun.after.succeeded audit event"
  rm -f "$TMP/after.out"
  s33_run s33f daemon --target fake-iosxe
  [ "$CODE" -eq 0 ] || fail "S33 hook daemon: exit $CODE: $(grep -E '^[a-z_]+:' "$TMP/err.s33f" | head -1)"
  grep -q "^job=$(basename "$JOB_DIR") dir=$JOB_DIR crun=$TMP/crun replaced=1 kept=0 exit=0\$" "$TMP/after.out" && grep -q '^hook ran$' "$TMP/err.s33f" || fail "S33 hook daemon: $(cat "$TMP/after.out"; tail -2 "$TMP/err.s33f")"
  export AFTER_EXIT=3
  s33_run s33g run --target fake-iosxe
  [ "$CODE" -eq 0 ] && grep -q "^warning: crun_after_failed: $TMP/after.sh exited 3\$" "$TMP/err.s33g" || fail "S33 hook exit 3: exit $CODE, stderr $(tail -2 "$TMP/err.s33g")"
  grep -q '"event_name":"crun.after.failed"' "$TMP/base/audit.jsonl" || fail "S33 hook exit 3: no crun.after.failed audit event"
  export AFTER_EXIT=0 AFTER_SLEEP=20
  s33_started=$(date +%s)
  s33_run s33h run --target fake-iosxe
  s33_wall=$(( $(date +%s) - s33_started ))
  [ "$CODE" -eq 0 ] && grep -q '^warning: crun_after_failed: .* ran past crun.after-timeout (1s) and was ended$' "$TMP/err.s33h" || fail "S33 hook timeout: exit $CODE, stderr $(tail -2 "$TMP/err.s33h")"
  [ "$s33_wall" -lt 15 ] || fail "S33 hook timeout: the run took ${s33_wall}s, the hook's sleep was not ended"
  ! pgrep -f "sleep 20" >/dev/null || { pkill -f 'sleep 20'; fail 'S33 hook timeout: the hook'"'"'s child outlived it'; }
  unset AFTER_OUT AFTER_EXIT AFTER_SLEEP
  # The drop list: crun-filters = [] on the base table writes the
  # raw file; a site's list replaces the built-in whole; a pattern that does
  # not compile refuses the configuration before any device is contacted.
  write_config accept-new "$BUILTIN
crun-filters = []
[crun]
directory = \"$TMP/crun\""
  s33_run s33i run --target fake-iosxe
  [ "$CODE" -eq 0 ] && cmp -s "$TMP/crun/fake-iosxe" "$TMP/s33raw.want" || fail "S33 filters off: exit $CODE: $(diff "$TMP/s33raw.want" "$TMP/crun/fake-iosxe" | head -3)"
  write_config accept-new "$BUILTIN
crun-filters = ['^hostname ', ' uptime is ']
[crun]
directory = \"$TMP/crun\""
  s33_run s33j run --target fake-iosxe
  [ "$CODE" -eq 0 ] && grep -q '^Building configuration' "$TMP/crun/fake-iosxe" && ! grep -q '^hostname \|uptime is' "$TMP/crun/fake-iosxe" || fail "S33 site filters: exit $CODE: $(cat "$TMP/crun/fake-iosxe" | head -3)"
  [ "$(json_get "$JOB_DIR/manifest.json" plan.platform_filters.cisco_iosxe.0)" = '^hostname ' ] || fail "S33 site filters: the manifest's plan carries $(json_get "$JOB_DIR/manifest.json" plan.platform_filters.cisco_iosxe.0)"
  write_config accept-new "$BUILTIN
crun-filters = ['^ok$', '(']
[crun]
directory = \"$TMP/crun\""
  s33_run s33k run --target fake-iosxe
  [ "$CODE" -eq 2 ] && grep -q '^config_platform_crun_filter_invalid: pattern 2 "(" does not compile: .* for platform.cisco_iosxe.crun-filters at ' "$TMP/err.s33k" || fail "S33 bad pattern: exit $CODE: $(head -2 "$TMP/err.s33k")"
  # The schedule: the packaged cron script packaging/cron/karvi-crun
  # holds a lock for the run's duration, so a tick that starts while the
  # previous runs (the fake's `show slow` takes 3 s) is skipped with one line
  # and exit 75 and submits no job; a tick alone collects. The script takes
  # the executable from KARVI, so a wrapper carries the suite's configuration.
  printf 'name,management_address,platform\nfake-iosxe,127.0.0.1,cisco_iosxe\n' >"$TMP/inv.csv"
  COMMAND_TIMEOUT=5s write_config accept-new "$BUILTIN
[crun]
directory = \"$TMP/crun\""
  rm -rf "${TMP:?}/base" "${TMP:?}/home"; install -d -m 700 "$TMP/base" "$TMP/home"
  printf '#!/bin/sh\nexec env NETENABLE=en HOME=%s NETUSER=netops NETPASS=pw %s --config %s --set '"'"'platform-resolution.default=""'"'"' "$@"\n' "$TMP/home" "$KARVI" "$TMP/karvi.toml" >"$TMP/bin/nd"
  chmod 755 "$TMP/bin/nd"
  rm -f "$TMP/s33l.done"
  # The two ticks are waited for by PID (a bare wait would also wait for the
  # fake, which start_fake runs in the background), and each tick's status
  # is taken through an if, since the subshells inherit set -e.
  s33_tick() {  # label, karvi-crun args...; appends "label status" to s33l.done
    s33_label=$1; shift
    if KARVI="$TMP/bin/nd" KARVI_CRUN_LOCK="$TMP/crun.lock" sh "$ROOT/packaging/cron/karvi-crun" "$@" </dev/null >"$TMP/out.s33l$s33_label" 2>"$TMP/err.s33l$s33_label"; then
      echo "$s33_label 0" >>"$TMP/s33l.done"
    else
      echo "$s33_label $?" >>"$TMP/s33l.done"
    fi
  }
  s33_tick a --transport native --cmd 'show slow' --cmd 'show running-config' &
  s33_pid_a=$!
  sleep 1
  s33_tick b --transport native &
  s33_pid_b=$!
  wait "$s33_pid_a" "$s33_pid_b" || true
  grep -q '^a 0$' "$TMP/s33l.done" && grep -q '^b 75$' "$TMP/s33l.done" || fail "S33 schedule: the ticks ended $(tr '\n' ' ' <"$TMP/s33l.done"), expected a 0 and b 75"
  [ "$(cat "$TMP/err.s33lb")" = "karvi-crun: skipped: the previous collection is still running ($TMP/crun.lock)" ] || fail "S33 schedule: the skipped tick said: $(cat "$TMP/err.s33lb")"
  [ ! -s "$TMP/out.s33lb" ] || fail "S33 schedule: the skipped tick wrote records"
  [ "$(ls -d "$TMP"/base/jobs/*/* | wc -l)" -eq 1 ] || fail "S33 schedule: $(ls -d "$TMP"/base/jobs/*/* | wc -l) jobs, expected the one tick's"
  grep -q '^! show slow$' "$TMP/crun/fake-iosxe" || fail "S33 schedule: the tick's file lacks its block"
  CODE=0; KARVI="$TMP/bin/nd" KARVI_CRUN_LOCK="$TMP/crun.lock" sh "$ROOT/packaging/cron/karvi-crun" --transport native </dev/null >"$TMP/out.s33lc" 2>"$TMP/err.s33lc" || CODE=$?
  tail -1 "$TMP/out.s33lc" >"$TMP/s33lc.last"
  [ "$CODE" -eq 0 ] && [ "$(json_get "$TMP/s33lc.last" collection.replaced)" = 1 ] && cmp -s "$TMP/crun/fake-iosxe" "$TMP/s33.want" || fail "S33 schedule: the tick alone: exit $CODE: $(head -c 300 "$TMP/s33lc.last")"
  stop_fake
  echo 'native smoke: S33 the collection run: the file, the kept device, the folder, the summary, a rejected statement, --cd through the daemon, --nof, the crun.after hook on both paths, its exit and its bound, the crun-filters drop list off, a site'"'"'s, and a bad pattern refused, the packaged cron script'"'"'s guard: ok'
fi

# S34: --cd on run and command. A run's collection file is crun's shape,
# unfiltered (the uptime line kept); the job folder keeps output.NAME.txt;
# a dead device keeps its previous file; a rejected statement keeps the
# file without --continue and replaces it with; the text display ends with
# the footer and display.collection.footer on standard output; crun.after
# does not run, a crun's does after the same ending; command --cd and
# --nof collect; --fs suffixes the file, implying --cd=. on run.
if [ -z "${ONLY:-}" ] || [ "${ONLY}" = S34 ]; then
  s34_run() {  # tag, karvi args...; sets CODE, JOB_DIR
    s34_tag=$1; shift
    rm -rf "${TMP:?}/base" "${TMP:?}/home"; install -d -m 700 "$TMP/base" "$TMP/home"
    CODE=0
    env NETENABLE=en HOME="$TMP/home" NETUSER=netops NETPASS=pw \
      "$KARVI" --config "$TMP/karvi.toml" --set 'platform-resolution.default=""' "$@" \
      >"$TMP/out.$s34_tag" 2>"$TMP/err.$s34_tag" || CODE=$?
    JOB_DIR=$(ls -d "$TMP"/base/jobs/*/* 2>/dev/null | tail -1)
  }
  printf 'name,management_address,platform\nfake-iosxe,127.0.0.1,cisco_iosxe\ndead,127.0.0.1,dead\n' >"$TMP/inv.csv"
  start_fake -port 0 -hostname fake-iosxe
  : >"$KH"; chmod 600 "$KH"
  rm -rf "${TMP:?}/cd"; install -d -m 770 "$TMP/cd"
  echo 'old dead' >"$TMP/cd/dead"
  export AFTER_OUT="$TMP/after34.out"
  printf '#!/bin/sh\n: >"$AFTER_OUT"\necho "hook ran"\n' >"$TMP/after34.sh"; chmod 755 "$TMP/after34.sh"
  write_config accept-new "$BUILTIN
[platform.dead]
driver = \"cisco_iosxe\"
ssh-port = 1
[crun]
after = \"$TMP/after34.sh\""
  s34_run s34 run --no-daemon --transport native --target fake-iosxe --target dead --cmd 'show clock' --cmd 'show version' --cd="$TMP/cd"
  [ "$CODE" -eq 101 ] || fail "S34: exit $CODE: $(grep -E '^[a-z_]+:' "$TMP/err.s34" | head -1)"
  printf '! show clock\n*10:00:00.000 UTC Tue Sep 15 2026\n\n! show version\nCisco IOS XE Software, Version 17.09.04a\nfake-iosxe uptime is 1 day\n' >"$TMP/s34.want"
  cmp -s "$TMP/cd/fake-iosxe" "$TMP/s34.want" || fail "S34: the file differs: $(diff "$TMP/s34.want" "$TMP/cd/fake-iosxe" | head -3)"
  [ "$(cat "$TMP/cd/dead")" = 'old dead' ] || fail "S34: dead's previous file was touched"
  [ -f "$JOB_DIR/output.fake-iosxe.txt" ] && [ -f "$JOB_DIR/output.dead.txt" ] || fail "S34: the folder holds $(ls "$JOB_DIR" | tr '\n' ' ')"
  [ "$(tail -2 "$TMP/out.s34" | head -1 | cut -c1-8)" = '! exit=1' ] && [ "$(tail -1 "$TMP/out.s34")" = "! collection=$TMP/cd replaced=1 kept=1" ] || fail "S34: the display ends $(tail -2 "$TMP/out.s34")"
  ! grep -q 'collection=\|hook ran' "$TMP/err.s34" && [ ! -e "$AFTER_OUT" ] || fail "S34: stderr $(tail -2 "$TMP/err.s34"), or the hook ran"
  [ "$(json_get "$JOB_DIR/summary.json" collection.devices.dead.outcome)" = kept ] || fail "S34: the summary's dead is $(json_get "$JOB_DIR/summary.json" collection.devices.dead.outcome)"
  [ "$(json_get "$JOB_DIR/manifest.json" plan.output.collection.word)" = run ] || fail "S34: the plan's word is $(json_get "$JOB_DIR/manifest.json" plan.output.collection.word)"
  # A rejected statement: kept without --continue, replaced with it.
  s34_run s34b run --no-daemon --transport native --target fake-iosxe --cmd 'show bogus' --cmd 'show clock' --cd="$TMP/cd"
  [ "$CODE" -ne 0 ] && cmp -s "$TMP/cd/fake-iosxe" "$TMP/s34.want" && [ "$(tail -1 "$TMP/out.s34b")" = "! collection=$TMP/cd replaced=0 kept=1" ] || fail "S34 rejected: exit $CODE, $(tail -1 "$TMP/out.s34b")"
  s34_run s34c run --no-daemon --continue --transport native --target fake-iosxe --cmd 'show bogus' --cmd 'show clock' --cd="$TMP/cd"
  printf '! show bogus\n     ^\n%% Invalid input detected at '"'"'^'"'"' marker.\n\n! show clock\n*10:00:00.000 UTC Tue Sep 15 2026\n' >"$TMP/s34c.want"
  cmp -s "$TMP/cd/fake-iosxe" "$TMP/s34c.want" || fail "S34 --continue: the file differs: $(diff "$TMP/s34c.want" "$TMP/cd/fake-iosxe" | head -3)"
  # Through the daemon: the line follows the footer on stdout.
  s34_run s34d run --transport native --target fake-iosxe --cmd 'show clock' --cd="$TMP/cd"
  stop_daemon
  [ "$CODE" -eq 0 ] && [ "$(tail -1 "$TMP/out.s34d")" = "! collection=$TMP/cd replaced=1 kept=0" ] || fail "S34 daemon: exit $CODE, $(tail -1 "$TMP/out.s34d")"
  # command --cd with --nof: no folder, the file and the line.
  s34_run s34e command --nof --transport native --cd="$TMP/cd2" fake-iosxe show clock
  [ "$CODE" -eq 0 ] && [ -z "$JOB_DIR" ] && [ "$(cat "$TMP/cd2/fake-iosxe")" = "$(printf '! show clock\n*10:00:00.000 UTC Tue Sep 15 2026')" ] && [ "$(tail -1 "$TMP/out.s34e")" = "! collection=$TMP/cd2 replaced=1 kept=0" ] || fail "S34 command: exit $CODE, folder '$JOB_DIR', $(tail -1 "$TMP/out.s34e")"
  # A crun's text display ends the same way, and its hook runs after it.
  s34_run s34f crun --no-daemon --transport native --target fake-iosxe --cmd 'show clock' --cd="$TMP/cd"
  [ "$CODE" -eq 0 ] && [ "$(tail -1 "$TMP/out.s34f")" = "! collection=$TMP/cd replaced=1 kept=0" ] && grep -q '^hook ran$' "$TMP/err.s34f" || fail "S34 crun: exit $CODE, $(tail -1 "$TMP/out.s34f"), $(tail -1 "$TMP/err.s34f")"
  rm -f "$AFTER_OUT"; unset AFTER_OUT
  # --fs: on run without --cd the working directory, the suffix on the file
  # alone, another suffix's temporary left by the sweep; on crun alone
  # crun.directory; a suffix holding / refused before any device.
  rm -rf "${TMP:?}/fsw"; install -d -m 770 "$TMP/fsw"
  : >"$TMP/fsw/.fake-iosxe.260924-000000-00"; : >"$TMP/fsw/.fake-iosxe.cfg.260924-000000-00"
  cd "$TMP/fsw"
  s34_run s34g run --no-daemon --transport native --target fake-iosxe --cmd 'show clock' --fs=.cfg
  cd "$ROOT"
  [ "$CODE" -eq 0 ] && [ "$(ls -A "$TMP/fsw" | tr '\n' ' ')" = '.fake-iosxe.260924-000000-00 fake-iosxe.cfg ' ] && [ "$(tail -1 "$TMP/out.s34g")" = "! collection=$TMP/fsw replaced=1 kept=0" ] || fail "S34 --fs: exit $CODE, the directory holds $(ls -A "$TMP/fsw" | tr '\n' ' '), $(tail -1 "$TMP/out.s34g")"
  [ "$(json_get "$JOB_DIR/summary.json" collection.devices.fake-iosxe.file)" = fake-iosxe.cfg ] && [ "$(json_get "$JOB_DIR/manifest.json" plan.output.collection.suffix)" = .cfg ] || fail "S34 --fs: the summary's file $(json_get "$JOB_DIR/summary.json" collection.devices.fake-iosxe.file)"
  write_config accept-new "$BUILTIN
[crun]
directory = \"$TMP/cd\""
  s34_run s34h crun --no-daemon --transport native --target fake-iosxe --cmd 'show clock' --fs=.cfg
  [ "$CODE" -eq 0 ] && [ -f "$TMP/cd/fake-iosxe.cfg" ] && [ ! -e "$TMP/cd.cfg" ] || fail "S34 crun --fs: exit $CODE, $(ls -A "$TMP/cd" | tr '\n' ' ')"
  s34_run s34i run --no-daemon --transport native --target fake-iosxe --cmd 'show clock' --fs=a/b
  [ "$CODE" -eq 4 ] && grep -q '^crun_suffix_invalid: ' "$TMP/err.s34i" || fail "S34 --fs=a/b: exit $CODE, $(head -1 "$TMP/err.s34i")"
  stop_fake
  echo 'native smoke: S34 --cd and --fs on run and command: the unfiltered file, the folder'"'"'s text files, the kept device, a rejected statement with and without --continue, the collection line after the footer on both paths, no hook but crun'"'"'s, --nof, the suffix and the implied working directory, the sweep, crun'"'"'s --fs, a bad suffix: ok'
fi
