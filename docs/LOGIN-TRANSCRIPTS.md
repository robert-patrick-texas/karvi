# Login transcript behavior

`karvi login --record HOST` and `karvi login HOST --record` are equivalent, and
`--rec` is an alias. PATH is given only with `=`, so `karvi login --rec
router01` records a session with `router01`. The v0.9.1 spellings
`--record-file` and `--record-path` are not accepted.

A recorded login runs through a real pseudo-terminal owned by a small parent
karvi process, the recording wrapper, which uses util-linux `script(1)`. The
child login performs configuration, inventory, credential resolution, auditing,
and system-OpenSSH execution exactly as an unrecorded login does; it uses the
session ID the wrapper chose, so the metadata, audit, and scoreboard records
agree. The session ID is the login's activity ID in the job form,
`YYMMDD-HHMMSS-xx`, the form every `run` and `cmd` has: the wrapper reserves
it by creating the scoreboard file exclusively before the child runs, the
child writes its snapshots into that file, and an unrecorded login reserves
the same way for itself.

## Destination and layout

- Without PATH, sessions record below `transcript.root` (default
  `<basedir>/transcripts`). `--record=PATH` uses PATH in its place; when
  `transcript.root` is locked, `--record=PATH` is refused with
  `transcript_root_locked`. PATH attaches with `=` only: `--record /x r1`
  is refused as `cli_option_value_detached`, where the path had been taken
  for the device. No environment variable selects a destination.
- A session writes into `<destination>/YYMMDD/`, named from the start
  date in the effective `timezone`; a session that crosses midnight stays in
  its start-day folder. `--record=session.log` therefore creates a folder
  named `session.log`, and a PATH that exists and is not a folder is refused
  with `transcript_path_not_directory`.
- The transcript `<device>-<HHMMSS>.log` and its metadata
  `<device>-<HHMMSS>.meta.jsonl` sit side by side, where `<device>` is the
  lowercase target name with every character other than letters, digits,
  `.`, `_`, and `-` replaced by `_`. When either name is taken, both are
  bumped together: `router01-143005.1.log` and
  `router01-143005.1.meta.jsonl`, then `.2`.
- The terminal names the transcript twice, with one line in the display's style:
  `display.record.header` before the session and `display.record.footer` as the
  last line, after the device's last output, a session that failed included.
  Both default to `! transcript=<transcript>`, so the two lines match, colored
  as the login's header is
  ([`docs/DISPLAY-CONFIGURATION.md`](DISPLAY-CONFIGURATION.md) ["The recorded
  login's lines"](DISPLAY-CONFIGURATION.md#the-recorded-logins-lines));
  `--quiet` suppresses them. A login refused before the recording starts (a
  free-space floor, a refused option) names none, since none was written.

  ```text
  ! transcript=/opt/karvi/users/netops/transcripts/261003/router01-143005.log
  ! router01 [10.0.0.1] platform=cisco_iosxe user=netops backend=interactive-tty transport=system
  ...
  router01#exit
  ! transcript=/opt/karvi/users/netops/transcripts/261003/router01-143005.log
  ```
- Folders karvi creates take `output.directory-mode` (default `0750`); files
  are `0640`, created exclusively without following links. An existing folder
  is accepted when the operator can create files in it, else
  `transcript_directory_not_writable`.

## Formats

`transcript.format` is `text`; the values `jsonl` and `json` name the
transcript event recorder, which this release does not implement, and are
refused with `transcript_format_unavailable`. `transcript.metadata-format`
accepts `jsonl` (default: a `start` line at session begin and an `end` line at
the end, so a killed session still leaves a readable record), `json` (one end
document written by atomic replacement), or `text` (`key: value` lines).
Metadata records the session ID, operator, target as typed, device, transport,
dispatch order and shuffle key, candidate count, transcript file and format,
terminal size, start and end times, the exit classification, and the SHA-256
of the transcript.

## Content

The transcript holds only the stream the device returns, as the terminal
showed it: device output, echoed characters, and prompts, which show the
commands sent. Operator keystrokes are not recorded, so a password typed at a
prompt that does not echo is not written. Karvi's own header, footer, and
warnings go to the terminal, not the transcript.

When the session ends, karvi rewrites the file once, before taking its SHA-256
for the metadata. It removes the `Script started` and `Script done` lines
`script(1)` writes into the file, and renders the bytes between them as the
terminal showed them: the line editor's corrections are applied (`echo helo`
corrected by two backspaces is recorded as sent, not as `echo helolo`), colours,
window titles, and terminal modes are dropped, a line longer than the terminal
is one line, its wrapped rows joined, and every line ends in a newline, with no
carriage return. The widths are `script(1)`'s timing log's (`-T FILE -m
advanced`): the terminal's columns at the start and each resize. The log is
written into the scratch (`tempdir`) and removed after; should it be unreadable,
the transcript is rendered at the columns the session started with, with the
warning `transcript_timing_unreadable`. No raw copy is kept. A session killed
before its end keeps the bytes `script(1)` wrote, its two lines included, and
leaves its timing log in the scratch.

```text
$ cat -v raw          the bytes the terminal was sent
^[[?2004h^[]0;netops@dev: ~^G^[[01;32mnetops@dev^[[00m:^[[01;34m~^[[00m$ echo helo^H^[[K^H^[[Klo^M
^[[?2004l^Mhelo^M
$ cat transcript      the transcript
netops@dev:~$ echo helo
helo
```

The limits are the terminal's: a key the far end does not echo cannot appear,
a full-screen program (an editor, `top`) comes out as its text in the order it
was drawn, and a device that shows a long line as a scrolled window records the
window. The runbook's row D16 records what IOS XE's line editor gives
([`docs/DEVICE-QUALIFICATION-RUNBOOK.md`, section
4](DEVICE-QUALIFICATION-RUNBOOK.md#4-the-rows)).

Cleanup: `karvi-prune` recognizes an ended session by the end record in its
metadata file and removes the transcript and metadata together.

`karvi-login(1)` (`man karvi login`), TRANSCRIPTS, restates the
destination, the names, the two lines, and what the transcript holds for
the terminal; a change to one changes both.
