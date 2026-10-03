# Changelog

## Unreleased

- **The capacity ledger is shared across operators.** Under a group-shared
  capacity root (setgid, `sessions.shared-capacity-root`), the ledger's
  directory and files take the root's group modes (2770 gives 0660), where
  they were 0700 and 0600 whatever the root: the second operator's every
  device failed with `capacity_admission_failed` (`open …/server.lock:
  permission denied`). An operator's own files left private by an earlier
  release are widened at their next use. The reaper counts another user's
  process as alive, where it reaped every other operator's live lease and
  so let each operator fill the host-wide cap alone. A ledger that cannot
  be read is an error, never an empty ledger whose write would replace
  another operator's leases. A shared root that exists but is closed to
  the operator (the sticky bit on another's directory, or a directory or
  lock it cannot use) is said once as the new `capacity_root_unusable` in
  the fallback warning, and the private root is taken, where every device
  had met the refusal.
- **No operator creates the scratch root.** `/dev/shm/karvi` is the
  site's: an operator's process makes only its own folder inside it and a
  missing `scoreboards` or `capacity` folder in an existing parent (with a
  setgid parent's group bits), and never a missing parent. The first
  operator's run had created `/dev/shm/karvi` at 0700, closing it to every
  other, whose scratch, control sockets, scoreboards, and capacity leases
  then fell to private places, so `karvi watch` showed one operator's work
  and the host-wide cap held per operator. On a host without the scratch
  root the private places are taken without a warning; a shared folder
  that exists but cannot be written is taken with one, naming it. The Go
  tests no longer reach the host's `/dev/shm/karvi`: the test isolation
  gives each test binary a scratch root and a capacity root of its own.
  The documentation of `tempdir`, `ssh.control-path-root`,
  `sessions.shared-capacity-root`, and `watch.directory` says so.
- **`setup shared` makes the scratch root and its boot rule.** Beside the
  shared trees and `users`, `sudo karvi setup shared` makes
  `/dev/shm/karvi` and its `scoreboards` (3770: setgid and sticky, so no
  member removes another's folder or file) and `capacity` (2770, the
  ledger every member rewrites) in the operators' group, repairing a group
  or mode found otherwise, such as the 0700 root an earlier release's run
  left. It writes `/etc/tmpfiles.d/karvi.conf` in that group, so
  systemd-tmpfiles makes the scratch root again at every boot, and reports
  it as created, exists, or updated; a rule karvi did not write is
  reported and left (`setup_tmpfiles_mismatch`), and a host without
  `/etc/tmpfiles.d` is told (`setup_tmpfiles_dir_missing`), the
  directories made either way. `packaging/tmpfiles.d/karvi.conf` is the
  rule for the default group, where it had given `capacity` the sticky
  bit, under which no member could replace a ledger another wrote. The
  setup help says what the command does, which had still described a
  mismatched directory as left alone.
- **The credential prompts edit as stream mode does, and Ctrl-C there
  exits.** The username and password prompts read through the line
  editor stream mode uses (`internal/termline`, shared by both):
  Backspace as DEL or Ctrl-H, the Delete key, the arrows, Ctrl-A, Ctrl-E,
  Ctrl-K, Ctrl-U, Ctrl-W; the password without echo. Ctrl-H and the
  Delete key had gone into the username as bytes. Ctrl-C at a prompt ends
  karvi at once with the new `credential_prompt_interrupted` (exit 113),
  in a stream too, where it had been held until the input ended; an empty
  answer ends it at once with the field's missing code
  (`credential_username_missing`, `credential_password_missing`,
  `credential_enable_missing`), before any later prompt; Ctrl-D on an
  empty line stays `credential_prompt_unavailable`. A prompt that failed
  is not asked again for the targets resolving beside the first, and a
  username and password typed or pasted ahead are both read. In stream
  mode the Delete key deletes forward.
- **Stream mode reads no line ahead.** At a terminal the reader had read
  the next line while a job ran, holding the terminal in raw mode
  through the job, so the job's Ctrl-C was taken as a key (the job ran
  on and the stream with it) and a credential prompt competed with the
  stream for the operator's keys. The reader now reads a line when the
  loop asks for it, so a Ctrl-C during a job is the signal it is: the
  follow stops, a daemon's job continues, and the stream ends.
- **A recorded login names its transcript at the end too, in the
  display's style.** `login --record` prints the transcript's path before
  the session and again as its last line, after the device's last output,
  a failed session included, where the header had been a plain
  `Recording login transcript to PATH` that a long session scrolled away.
  Both lines are display templates, the new `display.record.header` and
  `display.record.footer`, with one default, `! transcript=<transcript>`
  (the new `<transcript>` placeholder), rendered and colored as every
  header and footer and suppressed by `--quiet`; a template that does not
  render refuses the login before a transcript is created. The
  configuration registry moves from 22 to 23.
- **The dispatch reports say what a job runs.** A dry run's `dispatch:`
  line printed the parallel width whatever the mode, `width=4` on a
  4-CPU host for a serial job (one worker) and for a wave job (16 to 32);
  it now reads `serial width=1`, `parallel width=N`, or `wave
  start-width=N max-width=N depth-multiplier=N`, as does the exercise's
  `dispatch_assessment`, and the scoreboard's first snapshot takes the
  width the job starts at. A wave decision held at the ceiling with the
  CPU under the band is `cpu_below_zone_at_ceiling`, and one held at the
  start width over it `cpu_above_zone_at_floor`, where both had read
  `cpu_in_zone`; `metrics.json`'s `wave_decisions` carry the width before
  each decision, which had been 0.
- **`--cd=PATH` on `run` and `command`.** A run or command given `--cd`
  writes each device's collection file into PATH, in `crun`'s shape (a
  `! COMMAND` marker before each command's output and nothing else),
  unfiltered, replaced only when the device succeeds, its
  `--continue-device-on-error` its own; the job folder is the run's,
  `output.NAME.txt` included; `--nof` collects with no folder; no
  `crun.after` hook runs and the watch screen shows `run` or `cmd`. In a
  stream `--cd=PATH` is a line that stays, and a bare `--cd` is dropped
  when read. The execution plan's collection block carries the word that
  asked (`crun`, `run`, or `command`), at plan schema 10; a schema-9 plan
  or daemon is refused.
- **A manual page for `karvi-askpass`.** `packaging/man/karvi-askpass.1`,
  installed as `/usr/share/man/man1/karvi-askpass.1`: what the helper is,
  that `ssh` runs it for karvi and an operator does not, how karvi finds
  it, its environment, its one-use token and socket, and its exits; the
  groff lint now covers every page.
- **A manual page for `karvi-prune`.** `packaging/man/karvi-prune.8`,
  installed by the debian rules as `/usr/share/man/man8/karvi-prune.8`,
  is the terminal reference: what goes and never goes, the flags, where it
  looks, the report, the exits, the files, and examples. Its SYNOPSIS and
  OPTIONS are generated from the helper's flag definition by the new
  `tools/mangen` under `make generate`; `make generated-clean` and the
  bundle verifier compare them, and a test lints the page with groff.
  `make generated-clean` now fails on any stale generated file, where only
  the last one compared (`docs/ERROR-CODES.md`) could fail it.
- **`karvi-prune -h` reads as karvi's help does.** `-h` and `--help` print
  to standard output in karvi's layout: a title line, `Usage:` with the
  synopsis wrapped at 79 columns, and `Options:` with each flag in the
  synopsis's order, its value's name (`--basedir auto|PATH`, `--days N`,
  `--minfree PERCENT`, `--format text|jsonl`) in the option column and its
  description beside it, the default after; coloured at a terminal with
  the display's default roles (the helper reads no configuration, so a
  site's `display.*` does not reach it). Go's own printer had listed
  `-basedir string`, `-days int`, and `-minfree float` alphabetically on
  standard error. A usage error prints its message and the help on
  standard error, exit 2. The names, the order, and the synopsis are
  defined once in the helper's flag definition, which Tab and the man page
  read too; karvi's help is unchanged, its layout now shared
  (`internal/helplayout`).
- **`--fs=SUFFIX`, a suffix on each collection file's name.** On `crun`,
  `run`, and `command`, `--fs=.cfg` writes `NAME.cfg` (and its temporary
  `.NAME.cfg.JOBID`) in the collection directory, never renaming the
  directory; on `run` and `command` without `--cd` it is `--cd=.` as
  well, and a failure of that directory says it was implied by `--fs`; on
  `crun` alone it keeps `crun.directory`. A bare `--fs` or `--fs=` is
  `cli_option_value_missing`; a suffix holding `/`, NUL, or a control
  character is the new `crun_suffix_invalid`. The plan's collection block
  carries `suffix`; the summary, the hook's input, the collision check,
  and the dry run name the suffixed file. The stale-temporary sweep removes
  only `.FILE.` and a job ID's form, compared as strings, where its glob
  `.NAME.*` would have removed a concurrent `--fs=.cfg` run's temporary.
- **The collection line is a display template.** A job with a collection
  ends its text display with `display.collection.footer`, default `!
  collection=<collection> replaced=<replaced> kept=<kept>`, on standard
  output directly after the footer and in its colors, on every path
  (`--no-daemon`, the daemon follow, `job follow`, a replay); `--quiet` or
  an empty template suppresses it. It replaces `crun`'s plain `crun
  JOB-ID exit=… artifacts=… collection=…` line on standard error, which
  was printed under every format: under json and jsonl nothing is printed
  now, and the jsonl summary document and `summary.json` carry the
  counts. The configuration registry moves from 23 to 24.
- **An option that takes its value with `=` alone refuses a detached
  value.** `--of[=PATH]`, `--record[=PATH]`, and `--cd=PATH` are written
  alone or as `--NAME=VALUE`. A bare `--of` or `--record` followed by a
  word of a path's form (beginning with `/`, `~`, `./`, or `../`, or `.`
  or `..`) is the new `cli_option_value_detached`, naming the `=` form,
  where `run --of /x --cmd 'show clock'` had sent `/x --cmd show clock` to
  the device and `command --of /x r1` and `login --record /y r1` had taken
  the path for the device. A stream line `--of PATH` is dropped with its
  number for the same code, where the path had been sent as command text
  at every job. Any other word after a bare `--of` keeps its meaning
  (`command --of r1 'show clock'`), so a relative path is written
  `--of=out`.
- **`--ssh-host-key-policy` and `run`'s Dispatch options say what they
  do.** `login`, `command`, and `run` (and `crun`) share one entry for
  `--ssh-host-key-policy`, naming `ssh.host-key-policy` and the three
  modes with the default; `login`'s separate "Host-key modes" table is
  gone, and the other two had said nothing. `run`'s `--dispatch`,
  `--workers`, `--start-width`, `--max-width`, the two halts, the two wave
  gates, and `--wave-delay` each have an entry with its key and what 0
  means, where eight of them were listed without a word; the percent halt
  is described as built, against the devices ended so far. A test fails
  on a help line in a shape the layout does not know.
- **`run`'s Dispatch options obey their keys' locks and ranges.**
  `--dispatch` (and `--dp`, `--dw`, `--ds`), `--workers`, `--start-width`,
  `--max-width`, the two halts, the two wave gates, and `--wave-delay` set
  their `dispatch.*` keys through the lock-aware layer, as `--order` and
  `--blind-wait` do, where they had gone to the planner beside the
  configuration: a site's lock on any of those keys was passed by the
  option (`--max-width 64` under a lock of 8 ran at 64), and values the
  keys refuse were clamped without a word (`--max-width 600` ran at 512,
  `--start-width 100 --max-width 10` at 10) or accepted (`--wave-delay
  2h`, past the key's `0s..1h`). A locked key now refuses its option
  (`config_lock_violation`, exit 3), and an out-of-range value is the
  key's own error (`config_value_out_of_range`, exit 2), where
  `dispatch_value_negative` and `dispatch_percent_out_of_range` (exit 4)
  are retired; `--set` still outranks an option, and `--halt-on-error-count
  0` now turns a configured halt off. The top help says so for every
  option that stands for a key.
- **Every exit status has a written meaning.** `docs/ERROR-CODES.md`
  opens with an "Exit statuses" table: the 24 statuses, the name the
  records and the audit carry (`ExitPartialFailure`), and what each means,
  with the order in which they apply to a job, where the statuses had names
  alone and only the error codes said which exit they set. The list is
  defined once in `internal/exitcode`, a test holds every constant to it,
  and the error registry refuses a code naming an undefined exit.
- **Manual pages for `karvi` and each of its words.** `karvi.1` and
  `karvi-login.1`, `karvi-command.1`, `karvi-run.1`, `karvi-crun.1`,
  `karvi-stream.1`, `karvi-daemon.1`, `karvi-job.1`, `karvi-config.1`,
  `karvi-setup.1`, `karvi-watch.1`, and `karvi-version.1`, installed in
  section 1 (`man karvi run` finds `karvi-run`). Each page's SYNOPSIS and
  DESCRIPTION are its word's help text, generated by `tools/mangen` through
  the terminal layout's own analysis (`helplayout.Roff`), so a page cannot
  drift from `--help`; `karvi.1`'s EXIT STATUS is the list of
  `internal/exitcode`. A command word without its page fails `make
  generate`, and `make generated-clean` and the bundle verifier compare
  every page. `karvi.1` states once what every word shares: the
  configuration's layers and locks, the values (DURATION, N), the
  environment, the files, and how the exit is chosen; each word's page adds
  what its help does not say (`karvi-run.1` the dispatch modes, the widths
  at 0 by CPU count against the host's cap, the halts, the output, and the
  rehearsal; `karvi-crun.1` the collection; `karvi-login.1` the
  transcripts and the host keys), and the guides it restates say so.
- **The reference configuration loads.** `karvi config generate` and
  `configs/reference.toml` opened `[dispatch]` twice, `[output]` four
  times, and `[display.run]` twice, so `karvi config validate` refused the
  file a site starts from (`table redefined: dispatch`), and the top-level
  keys (`basedir`, `sharedroot`, `tempdir`, `spooldir`, `freecheck`,
  `timezone`) stood under `[config]`, where an edit was refused as
  `config.basedir`. The reference is now written table by table, the
  top-level keys first; no key's value changes, and a test holds that every
  key loads from its own line at its default. The 0.24.0 and 0.25.0
  references have the same fault.
- **`NO_COLOR` turns colour off everywhere under `auto`.** With
  `display.color` at `auto`, the default, a non-empty `NO_COLOR` now keeps
  karvi's colour off on every path: the help, a run's display, `config
  colors`, the watch screen, and `karvi-prune -h`, where the watch screen
  alone had read it. `display.color = "always"` (or `--set`) and the
  watch screen's `--color always` now win over `NO_COLOR`, where the
  screen had dropped colour even under `--color always`.
- **A refused option names itself.** A value an option sets is now
  sourced to the option: `--blind-wait 20m` is `… for execution.blind-wait
  at --blind-wait`, a lock's refusal of `--continue-device-on-error` names
  it (and a `crun`'s, which continues by itself, names `crun`), and
  `config show --explain` says `source: --ipv4`, where every option had been
  `command-line`. An unknown zone is reported against `timezone` at its
  source (`--timezone`, the environment, a file's line), where it was
  `display.timestamp` at `<builtin>`.

## 0.25.0 - 2026-09-30

A minor release on 0.24.0, the same day. Every counter is 0.24.0's (daemon
IPC 10, execution plan 9, scoreboard 3, configuration schema 6, registry
22, command record 2, job 2, credential package 1, plan report 1); a
running 0.24.0 daemon is `compatible: false` by its version alone and is
restarted. The behaviour change is stream mode's, below; `run`, `command`,
and the daemon are 0.24.0's. The module graph gains `golang.org/x/term`.

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
- **Line editing in stream mode.** Typed at a terminal, a line is edited
  with Ctrl-A, Ctrl-E, Ctrl-K, Ctrl-U, Ctrl-W, and the arrows, and the up
  arrow recalls earlier lines (`golang.org/x/term`, vendored, which
  brings `golang.org/x/sys` into `vendor/`; `go mod tidy` also marked
  `golang.org/x/crypto` the direct requirement it is and dropped
  `gopkg.in/yaml.v3`, which nothing required). Piped input is read as
  before.

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
