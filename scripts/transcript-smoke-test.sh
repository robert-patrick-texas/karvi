#!/bin/sh
# Login recording against
# the delivered binary: destination and day folder, side-by-side transcript
# and metadata, modes, the script(1) marker lines stripped, karvi's own lines
# kept out of the transcript, the metadata formats, and the refusals.
set -eu

# absent PATTERN FILE: the file must not match. A bare "! grep" line is exempt
# from set -e (POSIX: errexit ignores a negated pipeline) and asserts nothing,
# so every must-not-appear check goes through here.
absent() {
  if grep -q -- "$1" "$2"; then
    echo "$0: $2 must not match: $1" >&2
    exit 1
  fi
}

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
KARVI=${KARVI:-$ROOT/bin/karvi-linux-amd64}
. "$ROOT/scripts/lib/json.sh"
TMP=${TMPDIR:-/tmp}/karvi-transcript-smoke-$$
. "$ROOT/scripts/lib/host.sh"; host_trust_store "$TMP"   # the trust store under the work directory, never the operator's
HOME_DIR=$TMP/home
BASE=$TMP/state
FAKE_SSH=$TMP/fake-ssh
MARKER=KARVI_TRANSCRIPT_SMOKE_MARKER
DAY=$(date +%y%m%d)

cleanup() { rm -rf "$TMP"; }
trap cleanup EXIT HUP INT TERM

install -d -m 700 "$HOME_DIR" "$BASE"
cat > "$FAKE_SSH" <<EOF_INNER
#!/bin/sh
printf '%s\r\n' '$MARKER'
exit 0
EOF_INNER
chmod 755 "$FAKE_SSH"

COMMON="HOME='$HOME_DIR' NETUSER=smoke NETPASS=not-a-real-secret KARVI_TRANSCRIPT_DIR='$TMP/ignored' '$KARVI' --set 'basedir=\"$BASE\"' --set 'sharedroot=\"none\"' --set 'spooldir=\"$BASE/spool\"' --set 'platform-resolution.default=\"\"' --set 'watch.directory=\"$BASE/scoreboards\"' --set 'sessions.shared-capacity-root=\"$BASE/capacity\"' --set 'ssh.transports.system=\"$FAKE_SSH\"' --set display.color=never --set audit.journald-required=false --set 'audit.file=\"$BASE/audit.jsonl\"'"

# recorded NAME EXTRA_GLOBALS LOGIN_ARGS: runs a recorded login under a pty.
recorded() {
  name=$1; extra=$2; login_args=$3
  timeout 20s script -qefc "$COMMON $extra login $login_args" /dev/null >"$TMP/$name.out" 2>"$TMP/$name.err"
}

# The transcripts root's free space: a recorded
# login asks its root's volume for the floor before the recorder starts, so
# a floor a byte above the free space refuses it with output_preflight_space
# naming the root and no transcript is created; `never` reads nothing and
# the login is recorded. A login without --record writes no transcript and
# asks nothing (the rows below).
# The floor sits a GiB above the free space read here: the suites run in
# lanes beside one another and free space moves by more than a byte.
FLOOR=$(( $(df --output=avail -B1 "$BASE" | tail -1) + 1073741824 ))
if [ "$FLOOR" -lt 1099511627776 ]; then
  recorded floor "--set output.min-free-bytes-after-job=$FLOOR" "--record --address 127.0.0.1 transcript-device" && code=0 || code=$?
  [ "$code" -eq 111 ]
  # The wrapper's refusal reaches the recorder's pseudo-terminal, so it is
  # read from both streams with the terminal's carriage returns dropped.
  cat "$TMP/floor.out" "$TMP/floor.err" | tr -d '\r' | grep -q "output_preflight_space: $BASE/transcripts: need $FLOOR bytes free, only [0-9]* free"
  [ ! -d "$BASE/transcripts/$DAY" ]
  absent '! transcript=' "$TMP/floor.out"                     # nothing recorded, nothing named
  recorded floornever "--set output.min-free-bytes-after-job=$FLOOR --set 'freecheck=\"never\"'" "--record --address 127.0.0.1 transcript-device"
  [ "$(ls "$BASE/transcripts/$DAY"/transcript-device-*.log | wc -l)" = 1 ]
  rm -rf "${BASE:?}/transcripts"
fi

# A pair is <device>-<HHMMSS>.log beside <device>-<HHMMSS>.meta.jsonl in the
# day folder of transcript.root; both are 0640, the day folder 0750.
recorded before "" "--record --address 127.0.0.1 transcript-device"
dir=$BASE/transcripts/$DAY
[ -d "$dir" ]
[ "$(stat -c '%a' "$dir")" = 750 ]
[ "$(ls "$dir"/transcript-device-*.log | wc -l)" = 1 ]
transcript=$(ls "$dir"/transcript-device-*.log)
meta=${transcript%.log}.meta.jsonl
[ -f "$meta" ]
[ "$(stat -c '%a' "$transcript")" = 640 ]
[ "$(stat -c '%a' "$meta")" = 640 ]
[ ! -e "$TMP/ignored" ]                                   # no environment variable
grep -q "$MARKER" "$transcript"                            # the device stream
absent '^Script started on' "$transcript"               # script(1) markers stripped
absent 'Script done on' "$transcript"
absent 'transcript-device \[127\.0\.0\.1\]' "$transcript"   # karvi's header is not device stream
grep -q 'transcript-device \[127\.0\.0\.1\] platform=generic user=smoke' "$TMP/before.out"   # it went to the terminal
# display.record.header names the transcript first, before the login's
# header, and display.record.footer, the same line, last, after the
# device's output.
[ "$(tr -d '\r' <"$TMP/before.out" | head -1)" = "! transcript=$transcript" ]
[ "$(tr -d '\r' <"$TMP/before.out" | tail -1)" = "! transcript=$transcript" ]
[ "$(wc -l <"$meta")" = 2 ]
head -1 "$meta" | grep -q '^{"schema_version":1,"record":"start","session_id":"[^"]*","operator":{"username":"[^"]*","uid":[0-9]*},"input_target":"transcript-device","device":{"name":"transcript-device","canonical_name":"transcript-device","platform":"generic"},"transport":"system","dispatch_order":"default","candidate_count":1,"transcript_file":"'"$(basename "$transcript")"'","transcript_format":"text","terminal":{"rows":[0-9]*,"columns":[0-9]*},"started_at":"'
tail -1 "$meta" | grep -q '"record":"end"'
tail -1 "$meta" | grep -q '"exit_classification":"ExitSuccess"'
tail -1 "$meta" | grep -q '"recording_failed":false'
sum=$(sha256sum "$transcript" | cut -d' ' -f1)
tail -1 "$meta" | grep -q "\"transcript_sha256\":\"$sum\""
session=$(head -1 "$meta" | sed 's/.*"session_id":"\([^"]*\)".*/\1/')
grep -q "\"activity_id\":\"$session\"" "$BASE/audit.jsonl"   # the child used the wrapper's session ID
# The session ID is the job form, reserved by the scoreboard file the child
# wrote: every job ID on the watch screen has
# one form.
echo "$session" | grep -q '^[0-9]\{6\}-[0-9]\{6\}-[0-9a-z]\{2\}$'
[ "$(json_get "$BASE/scoreboards/$session.json" activity_type)" = login ]
[ "$(json_get "$BASE/scoreboards/$session.json" activity_id)" = "$session" ]

# --record after the target, and a second session claims a new name.
recorded after "" "--management-address 127.0.0.1 transcript-device --record"
[ "$(ls "$dir"/transcript-device-*.log | wc -l)" = 2 ]
[ "$(ls "$dir"/transcript-device-*.meta.jsonl | wc -l)" = 2 ]

# --record=PATH records into <PATH>/YYYY-MM-DD/ and creates PATH with
# output.directory-mode.
recorded path "--set 'output.directory-mode=\"0700\"'" "--record=$TMP/rec --address 127.0.0.1 transcript-device"
[ "$(ls "$TMP/rec/$DAY"/transcript-device-*.log | wc -l)" = 1 ]
[ "$(stat -c '%a' "$TMP/rec")" = 700 ]
[ "$(stat -c '%a' "$TMP/rec/$DAY")" = 700 ]
grep -q "$MARKER" "$TMP/rec/$DAY"/transcript-device-*.log

# Metadata formats: text appends key: value lines, json is one end document.
recorded text "--set 'transcript.metadata-format=\"text\"'" "--record=$TMP/rec-text --address 127.0.0.1 transcript-device"
tmeta=$(ls "$TMP/rec-text/$DAY"/transcript-device-*.meta.txt)
grep -q '^record: start$' "$tmeta"; grep -q '^ended_at: ' "$tmeta"; grep -q '^device.canonical_name: transcript-device$' "$tmeta"
# A session that fails keeps its transcript, and the footer names it: the
# device answered, then the connection ended with 255.
cat > "$TMP/fake-ssh-fail" <<EOF_INNER
#!/bin/sh
printf '%s\r\n' '$MARKER'
exit 255
EOF_INNER
chmod 755 "$TMP/fake-ssh-fail"
recorded failed "--set 'ssh.transports.system=\"$TMP/fake-ssh-fail\"'" "--record=$TMP/rec-failed --address 127.0.0.1 transcript-device" && code=0 || code=$?
[ "$code" -ne 0 ]
failed=$(ls "$TMP/rec-failed/$DAY"/transcript-device-*.log)
grep -q "$MARKER" "$failed"
[ "$(tr -d '\r' <"$TMP/failed.out" | tail -1)" = "! transcript=$failed" ]
# --quiet suppresses both lines, as every header and footer; a record
# template that does not render refuses the login before a transcript.
recorded quiet "--quiet" "--record=$TMP/rec-quiet --address 127.0.0.1 transcript-device"
absent '! transcript=' "$TMP/quiet.out"
[ "$(ls "$TMP/rec-quiet/$DAY"/transcript-device-*.log | wc -l)" = 1 ]
recorded badtemplate "--set 'display.record.footer=\"! <nosuch>\"'" "--record=$TMP/rec-bad --address 127.0.0.1 transcript-device" && code=0 || code=$?
[ "$code" -ne 0 ]
cat "$TMP/badtemplate.out" "$TMP/badtemplate.err" | grep -q 'config_display_template_invalid\|display_placeholder_unsupported'
[ ! -e "$TMP/rec-bad" ]

recorded json "--set 'transcript.metadata-format=\"json\"'" "--record=$TMP/rec-json --address 127.0.0.1 transcript-device"
jmeta=$(ls "$TMP/rec-json/$DAY"/transcript-device-*.meta.json)
grep -q '"record":"end"' "$jmeta"; [ "$(wc -l <"$jmeta")" = 1 ]

# Refusals happen before any file or child process exists.
set +e
HOME="$HOME_DIR" "$KARVI" --debug --debug-show-secrets login --record="$TMP/blocked" --management-address 127.0.0.1 transcript-device >"$TMP/blocked.out" 2>"$TMP/blocked.err"; code=$?
set -e
[ "$code" -eq 4 ]; grep -q "^record_debug_show_secrets_conflict: " "$TMP/blocked.err"; [ ! -e "$TMP/blocked" ]
: >"$TMP/session.log"
set +e
eval "$COMMON login --record=$TMP/session.log --address 127.0.0.1 transcript-device" >"$TMP/notdir.out" 2>"$TMP/notdir.err"; code=$?
set -e
[ "$code" -eq 4 ]; grep -q "^transcript_path_not_directory: " "$TMP/notdir.err"
set +e
eval "$COMMON --set 'transcript.format=\"jsonl\"' login --record --address 127.0.0.1 transcript-device" >"$TMP/fmt.out" 2>"$TMP/fmt.err"; code=$?
set -e
[ "$code" -eq 8 ]; grep -q "^transcript_format_unavailable: " "$TMP/fmt.err"
[ "$(ls "$dir"/transcript-device-*.log | wc -l)" = 2 ]     # nothing was created by the refusals

echo 'transcript smoke: pass'
