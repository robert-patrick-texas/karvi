# Changelog

## Unreleased

- **Stream mode's draft.** A `--cmd`, `--command`, or `--cf` line is a
  command like a bare line, cleared by `--go` with the rest, where it had
  stayed among the options and been re-sent by every later job. `--clear`
  empties the commands and keeps the targets and options. `--go` and
  `--sendit` with nothing to send print a notice and run nothing, so a
  stream that sent no job exits 0. `--cf`, `--tf`, and `--tfr` naming `-`
  are refused in every spelling (`--tf=-` had reached the run). A read
  failure or a line over 1 MiB ends the stream with the new
  `stream_input_read_failed` (exit 1) naming the line, where it had ended
  silently with exit 0. A Ctrl-C outranks lines already read; a command
  line loses its trailing blanks; a parse error at `--go` names its line.

## 0.24.0 - 2026-09-30

The first release from the public repository, on 0.23.0. No behaviour of
the executables changed, and every counter is 0.23.0's (daemon IPC 10,
execution plan 9, scoreboard 3, configuration schema 6, registry 22,
command record 2, job 2, credential package 1, plan report 1); a running
0.23.0 daemon is `compatible: false` by its version alone and is
restarted. The tree was prepared for public release. The cumulative patch
stream shipped beside each release's bundle since 0.10.0 is retired: a
release is the source bundle, its checksum, and an aggregate checksum
file, and the release tools refuse a tree that holds anything git does not
track. The specification the program was built from is frozen and archived
with its earlier revisions, the decision records, the worked design
sessions, and the session hand-offs; `docs/DESIGN.md` states the settled
decisions and why, and the code and its tests are the reference. Every
pointer at the archived documents was removed from the tree, the module
path is `github.com/robert-patrick-texas/karvi`, and the public history
begins at the root commit of this tree. Three messages that named a
specification section now say "a valid identifier", and the error
catalogue's retired codes name the release span that retired them.

## 0.23.0 - 2026-09-29

A minor release on 0.22.0. **Breaking in three places:** under
`--format jsonl` a run's stream ends with the job's summary document, so a
consumer that took every line for a record reads the last line as the
summary; a run prints no result line on standard error (the job folder is
the footer's `artifacts=` on standard output or the summary line's `paths`;
`--detach` keeps its acceptance line and `crun` its result line); and the
boolean `display.ping` is removed and refused at load naming
`display.ping.header`, which moves the configuration registry from 21 to
22. Every other counter is 0.22.0's (daemon IPC 10, execution plan 9,
scoreboard 3, configuration schema 6, command record 2, job 2, credential
package 1, plan report 1); a running 0.22.0 daemon is `compatible: false`
by its version alone and is restarted.

- **The examples folder.** `examples/` holds one commented example of each
  file karvi reads: `config.toml`, `inventory.csv`, `credentials.csv`,
  `cloginrc`, `targets.txt`, `commands.txt`, and a README with the dry run
  that tries them.
- **`--tl LIST`, and `--dp`, `--dw`, `--ds`.** In `command` and `run`,
  `--tl LIST` takes target names separated by commas or whitespace in any
  mix, each a `--target` at the list's position (an empty list
  `cli_device_empty`, a bad name refused naming `--tl`); in `run`, the three
  shortcuts stand for `--dispatch parallel`, `wave`, and `serial`, the last
  on a line winning.
- **The watch screen's `/` filter as words.** The text is split at
  whitespace and every term must occur, as a case-insensitive substring, in
  one of the five fields; a trailing space changes nothing and a second word
  narrows (before, one literal substring, and a text ending in a space hid
  every row). `--filter TEXT` takes the same rule.
- **`display.ping.header`.** The ICMP gate's line is a template rendered as
  the headers are, `! ` prefixed and coloured by role, with `<rtt1>`,
  `<rtt2>`, and `<result>` (`proceeding` in the success role, `skipped` in
  the error role); an empty template prints no line. `login` renders the
  same template. The boolean `display.ping` cannot stand beside a table of
  the same name in TOML and is removed; a configuration that sets it is
  refused at load as `config_key_removed` naming the template.
- **The run footer on every path, and the jsonl summary line.** A run's
  text display ends with `display.run.footer` from the job's summary on the
  daemon follow, `--no-daemon`, `job follow`, and a finished job's replay
  alike; under `--format jsonl` the summary document is the stream's last
  line; no result line on standard error for a run, and `--follow=false`
  prints nothing (the exit is the job's). A follow whose stream is lost
  closes the format without a footer; the footer's artifacts label `none`
  for a run that keeps no files.
- **The help's wording.** The title line reads `karvi - run the fleet,
  gather the output`; `--tf`'s placeholder is `FILE|PATH|-` in `run`,
  `command`, and `login`.
- **Stream mode, `karvi stream` and `karvi -`.** A run read line by line
  from standard input: blank, `!`, and `#` lines skipped; a `--` line one run
  option, the word and the rest of the line its value; any other line one
  command as written, `\r` as `--cmd` reads it, the declarations attaching
  to the command before them; `--cf -` and `--tf -` refused. `--go` or
  `--sendit` executes the draft as `run` would, keeping the targets and
  options and clearing the commands; `--reset` empties it; `--end`,
  `--quit`, EOF, or Ctrl-C leave without executing; a line the parser
  refuses is reported by its number and dropped; the exit is the last
  executed job's. The draft is a `run` argument list parsed and run by
  run's own code, so the job, its records, and its display are a run's.

## Earlier releases

Releases 0.1.0 to 0.22.0 (2026-09-08 to 2026-09-28) were made before the
tree was prepared for public release; their changelog is kept in the
operator's private archive. Their executables are not installed anywhere
outside the operator's own environment, and every release is installed as
new.
