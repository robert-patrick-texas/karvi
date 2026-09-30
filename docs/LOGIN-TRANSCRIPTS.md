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
  `transcript_root_locked`. No environment variable selects a destination.
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

The transcript holds only the stream the device returns: device output,
echoed characters, and prompts, which show the commands sent. Operator
keystrokes are not recorded, so a password typed at a prompt that does not
echo is not written. Karvi's own header, footer, and warnings go to the
terminal, not the transcript. `script(1)` writes its own `Script started` and
`Script done` lines into the file; karvi removes them when the session ends,
so a session killed before then keeps those two lines.

Cleanup: `karvi-prune` recognizes an ended session by the end record in its
metadata file and removes the transcript and metadata together.
