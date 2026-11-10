# Changelog

A release's block holds its entries under New features, Bugs fixed, and
Breaking changes, each heading there with "None." when it has no entry, and
under Other notes, when there is one, the changes to the suites, the tests,
and the build's tooling that reach no site. A table of the release's numbers
ends the block: each value that moved since the release before (Go, a module
`go.mod` requires, a schema counter), and two counts, "Lines of Code: Go",
the lines of the tracked `.go` files outside `vendor/`, tests included, and
"Lines of Code: Other", the lines of the other tracked programs outside
`vendor/` (a file whose first line starts `#!`, or one named `Makefile`).
Every value the table does not show is the previous release's; the release
notes and the release's `release/BUILD-RESULT.md` show every value. The
block headed Unreleased takes its table, and "None." under an empty heading,
when the release is made. The blocks before the first table keep the form
they were published in.

## Unreleased

### New features

- **A run's text display can be gathered: `--gather`,
  `display.run.gather`.** Set true (false by default), a run's text display
  is shown at the job's end, every block at once: a block per requested
  command, headed by its marker line (`! show version`, `! --sudo COMMAND`),
  and in it a group per answer, the devices whose records have the same
  status, error code, and output (on the exec channel the same exit status,
  signal, and stderr too) under one header, `display.gather.header`, by
  default `! <targets> (<count>)` with the devices folded into one `--tl`
  value (`! router[01-04,10] (5)`), the succeeded groups first, the largest
  first. A failed group shows its output, then its failure line, the message
  too when the group is one device; a failed session-init command is a block
  of its own ahead of the others, `! session_init=PROFILE: COMMAND`. While
  the job runs the client holds no output, each group's place in the job's
  `commands.jsonl` and each device's name; at the end it reads each group's
  answer back from that file, streamed as today's display streams a spooled
  answer. It applies to `run` and its stream lines and `crun`, in process
  and through the daemon, and to `job follow` through the daemon or from the
  job's folder, each by the configuration of the invocation showing it, so
  `job follow` with the key set gathers a job run without it. `command`
  gathers under its own key, `display.command.gather`, as
  `display.command.echo` mirrors `display.run.echo`; `job follow` shows any
  job, a command's too, by the `display.run` keys. Under `--format jsonl` or
  `json`, under `--live`, under `--nof`, and where
  `output.files.commands-jsonl` is false the display is the ordinary one;
  under `--follow=false` nothing is shown and nothing is read back.
  `--gather` sets the key for the invocation, and `--gather=false` turns a
  site's true off, as `--order` sets `dispatch.order` (a site's lock on the
  key refuses either form, and `--set` beats it): on `run`, `crun`, a
  stream's lines, and `job follow` (any job, a command's too), and on
  `command` its own key. The option is refused, `gather_option_conflict`
  (exit 4), beside what leaves the gathered display nowhere to show it or
  nothing to read back: `--format jsonl` or `json`, `--echo`, `--detach`,
  `--follow=false`, and `--nof`, and a configuration that keeps no
  `commands.jsonl`, the key named with where it was set (`--gather and
  output.persist-command = false at /etc/karvi/config.toml:12 are mutually
  exclusive; --of`, or `output.files.commands-jsonl = false`), `; --of` only
  where `--of` would take effect (not after a `--set` of the key, which
  beats it, nor under a site's lock); beside `--live` it is
  `live_option_conflict`. `--dry-run` and `--exercise` take it, a rehearsal
  of the real line, and the key set true is never refused. On `job follow`
  it is refused beside `--format jsonl` or `json` and `--echo`, and for a
  job that kept no `commands.jsonl`, before anything is shown, the job
  untouched: `gather_option_conflict: --gather needs the job's
  commands.jsonl; job ID kept none (output.files.commands-jsonl was false)`,
  `(output.persist-command was false)` for a job without a folder; the key
  set true shows such a job as before. A `commands.jsonl` that cannot be
  read at the end, or whose line is no longer the answer the job recorded
  (its device, command, status, error, output, or an exec command's exit and
  stderr differ), is `run_output_records_unreadable`, exit 111: through the
  daemon the job is unaffected, and the line before the error says so, `job
  ID ended exit=N; artifacts DIR`; under `--no-daemon` the job's summary and
  scoreboard carry the exit, as a footer that cannot be written does. While
  it waits, when standard error is a terminal (judged by itself, as colour
  is), one line there, `gather: done=N/M fail=F`, job list's DONE and FAIL
  over the job's devices, is drawn as the display starts, rewritten as each
  device ends, other lines on standard error written above it, and cleared
  before the blocks; none is written to a pipe or a file, or under
  `--quiet`, and it is drawn only while karvi is the terminal's foreground
  job and the terminal is wider than the line (a job sent to the background
  writes none over the shell). A Ctrl-C stops the display at once: while the
  job runs nothing gathered is shown (in process the job is cancelled and
  its footer follows; through the daemon the job continues, as before, and
  the line that says so names `karvi job follow --gather JOB-ID`, on `job
  follow`'s own interrupt too, plain under a site's lock on the key), and
  during the read-back at the end the blocks stop between pieces, in process
  (`--no-daemon`, `command`) the footer after with exit 113, through the
  daemon the line `job ID ended exit=N; artifacts DIR` and then `cancelled:
  the display of job ID was interrupted`, exit 113; `karvi job follow
  --gather JOB-ID` shows the job whole later.
- **Line mode: `--prefix`, `display.run.prefix`.** Set true (false by
  default), a run's text display writes every line as `NAME: line`, the
  device's name first, as pdsh and clush write it, an empty line `NAME: `
  too, so `grep` and `sort` read a fleet's answers by device: the answer
  (stdout then stderr on the exec channel), the `--echo` line, and the
  failure and not-attempted lines without the `target=` the name repeats
  (`router09: karvi: status=connection_error error=...`, `router09: karvi:
  status=not_attempted_prior_command_failure command=2/2`). No device
  header, no border, and no ICMP gate's line (nor its `--debug` lines), as
  the gathered display: a skipped device shows by its failure line. The
  records stream as written, nothing held; an answer is prefixed as it
  streams, a long one never held whole. With `display.run.gather` each line
  of a group is under the group's folded names, `edge[1-2],router[01-04,10]:
  line`, the block's marker unprefixed, no group header. karvi's own lines
  (the marker, and by the default templates the footer and a `crun`'s
  collection line after it) begin with `!`. At a terminal the name is in the
  target colour, `: ` in the label colour, a failure line in the error
  colour, the device's output uncoloured. It reaches the paths
  `display.run.gather` reaches, each by the configuration of the invocation
  showing it; `command` has its own key, `display.command.prefix`, and `job
  follow` shows any job by `display.run.prefix`. Under `--format jsonl` or
  `json`, and under `--live`, it has no effect. `--prefix` sets the key for
  the invocation, and `--prefix=false` turns a site's true off, on the words
  `--gather` reaches, `command` under its own key; it is refused,
  `prefix_option_conflict` (exit 4), beside what line mode cannot take:
  `--format jsonl` or `json`, `--border`, `--noborder`, `--detach`, and
  `--follow=false`; beside `--live` it is `live_option_conflict`. `job
  follow --prefix` shows any job so, a command's too, refused beside
  `--format jsonl` or `json`, `--border`, and `--noborder`. Line mode needs
  no file, so `--prefix` is taken beside `--nof`, and beside `--echo` its
  line prefixed; the key set true is never refused.

### Bugs fixed

- **A followed job's display failing at its end no longer says the job
  continues.** Through the daemon (`run`, `crun`, `job follow`), a display
  that failed once the job had ended, its footer or summary line not written
  to a closed standard output, printed `job ID continues in the daemon;
  artifacts DIR` before the error; it now prints `job ID ended exit=N;
  artifacts DIR` with the job's exit. `job follow` from the job's folder,
  which printed no line before the same failure, prints it too.
- **`crun.after` runs when a `crun`'s display fails at the job's end
  through the daemon.** A `crun` followed through the daemon whose display
  failed once the job had ended (its footer, or now its gathered blocks,
  not written to a closed standard output, as under `karvi crun … | head`)
  skipped the site's `crun.after` program, though the collection was
  complete; it now runs, told the invocation's exit (`KARVI_EXIT=111`), as
  under `--no-daemon`. A standard output closed while the job still runs
  ends the follow as before: the job continues in the daemon, and no
  program runs.
- **A job whose `failed-devices.txt` cannot be written no longer leaves its
  record files open.** At the job's end the store returned at that failure
  without closing `commands.jsonl` and `errors.jsonl`, so the daemon kept two
  descriptors open for each such job until it ended. It now closes every
  file and reports the first failure, as before (`output_store_close_failed`).
- **`job follow` from a job's folder names a gone `commands.jsonl` once.**
  The error read `open PATH: open PATH: no such file or directory`; it reads
  `open PATH: no such file or directory`.
- **A failed command's record names its error code once.** A command
  whose own run failed with a coded cause (its session lost, among others)
  kept the code at the head of its record's message, so the display's
  failure line read `error=command_session_lost: command_session_lost: the
  device session ended`; the message is now the cause alone, as every
  other failure record's is: `error=command_session_lost: the device
  session ended`.
- **`job follow` from a job's folder names a `summary.json` it cannot open
  once.** The error read `read PATH: open PATH: permission denied`; it reads
  `open PATH: permission denied`.
- **`job follow` refuses `--border` and `--noborder` together.** It took
  both, `--noborder` silently winning, where `run` and `command` refuse
  the pair; it now refuses it as they do, `border_options_conflict: --border
  and --noborder are mutually exclusive` (exit 4), before any other check.
- **A followed job without a folder names `output.persist-command` in its
  not-kept warning.** Through the daemon, a follow of a job run with `--nof`
  (or under `output.persist-command = false`), which keeps no folder, warned
  `records 1 to N were before this follow and are not kept
  (output.files.commands-jsonl is false)`, naming a key the job did not
  set; it now ends `(output.persist-command is false)`. A job with a folder
  that kept no `commands.jsonl` keeps the first wording.
- **An interrupted `crun` no longer warns that its `crun.after` "was
  interrupted after 0s".** A `crun` run in process (`--no-daemon`) and
  interrupted before its hook started, while the job ran or during its
  display, kept its collection summary, so the hook was asked for under the
  interrupted invocation, where it could not start: karvi warned
  `crun_after_failed: PATH was interrupted after 0s`, and the audit held a
  `crun.after` event, outcome `fail`, `exit_code` 0, for a hook that never
  ran. An interrupt that comes before the hook starts now runs none, with no
  warning and no audit event, on either path, as through the daemon. A
  Ctrl-C while the hook runs still ends it (`was interrupted after Ns`), and
  a hook that cannot start still warns.

### Breaking changes

- **The registry artifact's keys no longer carry `since`.** Each key in
  `schema/config-schema.json` carried a `since`, meant as the release it first
  shipped in; nothing in karvi read it, and a key whose row set none said
  `0.2.0`: 101 of 0.41.0's 179, every key added since 0.33.0 among them. The
  field and its default are removed.

### Other notes

- **`tools/release-numbers` prints the release's numbers and checks the
  documents that carry them.** `go run ./tools/release-numbers` prints a
  table of every number a release carries, the previous release's value
  beside this one's: Go, each module `go.mod` requires (the indirect ones
  included), the twelve schema counters, each read from its owner's
  constant, and two counts, "Lines of Code: Go" (the tracked `.go` files
  outside `vendor/`, tests included) and "Lines of Code: Other" (the other
  tracked programs outside `vendor/`: a file whose first line starts `#!`,
  or a file named `Makefile`). A moved version or counter is bold, the two
  counts never; a module one release lacks is "—" there. `-changelog`
  prints what moved and the two counts, then the line saying every other
  value is the previous release's. `-check` compares, for the release
  `VERSION` names, the CHANGELOG's block, `release/BUILD-RESULT.md`'s table,
  and the release manifest's counters, Go, modules, and Go lines, and a
  notes draft's table when one is named; the first difference stops it,
  the row named and the two values stacked. This release is `VERSION`, read
  from the working tree; the previous one is the highest `karvi-v` tag below
  it (`-previous TAG`), read from the tag through git. It needs git and the
  tags, so it runs in the release tree, not in a bundle; its tests, which
  run everywhere, check the counter list against the constants karvi
  imports and that the tree holds no `SchemaVersion` constant the list
  lacks.

## 0.41.0 - 2026-11-08

A minor release on 0.40.0 for numeric ranges in every selector
(`router[10-12]`), targets that create the names a range spells, runs limited
to the inventory's devices (`targets.inventory-only`, and `--inventory`,
`--inv`, or `--i`, with files given on the line), the removal of
`security.require-fips` and of `karvi version`'s `fips_mode`, a
`karvi-sysutil` that never acts as root on what the service account owns, the
release's number written once in `VERSION`, and the shell-script review's
fixes to the suites and scripts. The configuration registry moves to 34 and
the audit to 4; every other counter is 0.40.0's (configuration schema 6,
execution plan 17, command record 5, plan report 4, scoreboard 5, daemon IPC
12, job manifest and summary 5, credential package 2, metrics 1, inventory 1).
Go and every module are 0.40.0's. A site has something to change where a
configuration file, `--set`, or a `KARVI__` variable names
`security.require-fips`; a tool reads the audit policy's `fips_required`,
reads the audit trail as schema 3, or reads `fips_mode` from `karvi version`
or a job manifest's `app.build`; a selector, a credential CSV row, or a map
rule holds a bracket of digits meant as a one-character class (`[123]`,
`[05]`, `[1,3]`) or one now refused (`[1-]`, `[10, 12]`); or a target holding
a range was meant to select from the inventory alone, as it now does under
`--i` or `targets.inventory-only`. An operator's 0.40.0 daemon, of another
build, is drained by the first submission after the upgrade.

- **The release's number is written once, in `VERSION`.** A package at the
  module root embeds the file, and `karvi version` reports it through
  `buildinfo.Version`, so `go build`, `go test`, and `make build` give the same
  number. The Makefile's `VERSION`, which nothing read, and each verifier's
  typed copy of the number are gone: `scripts/verify-shipped.sh` checks the
  shipped executable's version and the manifest's against the file, as
  `scripts/verify-release.sh` checks its build's. A release changes `VERSION`,
  the CHANGELOG, and `CHECKSUMS.sha256`. [`docs/DESIGN.md`, section
  15](docs/DESIGN.md#15-configuration-and-errors), has the rule.
- **The daemon-upgrade suite builds its fixture before it starts it.**
  `scripts/daemon-upgrade-smoke-test.sh` ran the lifecycle fixture through
  `go run` and gave it two seconds to bind its socket, so on a cold build cache
  the compile took them and the suite stopped with `schema-2 fixture did not
  start`, the rest of its lane unrun. It builds `tools/daemon-fixture` once into
  its work directory and starts the executable, so the wait measures the start
  alone, and the pid it holds is the fixture's own.
- **The spool suite asks no floor of the host and says when S9's narrowing
  row cannot run.** `scripts/spool-smoke-test.sh` ran under the default 2 GiB
  `output.min-free-bytes-after-job`, so a volume short of that and the
  suite's own writes refused a row with `output_preflight_space` (S6, with
  2.8 GB free when the suite began), and S9's narrowing row, a job at a 1 GiB
  command limit, asked a further GiB (S9, with 3.4 GB free when the suite
  began, asking 3,228,434,432 bytes), stopping its lane before the job list
  and audit suites. The suite sets the floor to 0, as
  the other suites do, S10 still setting its own; S9 reads the floor from
  karvi and the volume's free space at the row, and runs the row when it holds
  the floor and two limits (one for the job at width one, one for its other
  files and the other lanes' writes), else prints a line naming both figures.
- **Four more suites ask no floor of the host.** The audit, job list, parity,
  and transcript suites ran their jobs and recorded logins under the default
  2 GiB `output.min-free-bytes-after-job`, so on a volume short of it each
  stopped at its first job with `output_preflight_space`, the transcript suite
  with no line. Each sets the floor to 0, as the other suites do, through the
  places file every suite now passes first (the transcript suite's floor rows
  still set their own).
- **Breaking: `security.require-fips` is removed; registry 34, audit schema
  4.** Its documentation said "Startup verifies build/runtime mode", and
  nothing read it but the audit, which copied it into each event's policy as
  `fips_required`: a site that set it to true ran exactly as before, its audit
  saying FIPS was required. A configuration file or `--set` naming the key is
  refused, `config_unknown_key`, and `KARVI__SECURITY__REQUIRE_FIPS`,
  `config_unknown_environment`; the audit's policy no longer carries
  `fips_required`. karvi has no FIPS 140-3 mode; what one waits on is in
  [`ROADMAP.md`](ROADMAP.md).
- **Breaking: `karvi version` has no `fips_mode` line.** The value was the
  Makefile's `FIPS_MODE` word, stamped in at build time and checked against
  nothing: `false` for a build with Go's FIPS 140-3 mode on (`GOFIPS140`, or
  `GODEBUG=fips140=on`), `true` for an ordinary build made with
  `FIPS_MODE=true`. The line, the version JSON's `fips_mode`, a job manifest's
  `app.build.fips_mode`, and the Makefile's `FIPS_MODE` are gone; the job
  schema stays 5, the manifest's `app` being free-form.
- **`karvi-sysutil` never acts as root on what the service account owns.**
  The account owns its home and could replace the home, `.config`, or
  `config.toml` with a link. `daemon install`'s repair then gave the link's
  target to the account as root: a `config.toml` linked to `/etc/shadow` made
  it the account's, mode 0600, and a `.config` linked to `/etc` set `/etc` to
  mode 0700, closing it to every other user, and gave the account the global
  layer.
  `daemon remove` removed `config.toml` through such a link, taking
  `/etc/karvi/config.toml`, the site's global file, with it. Root now makes
  only the home itself, and the configuration and its folders are made,
  repaired, and removed as the account. Before anything changes, both refuse
  a home the install did not make, and a link, another type, or another
  owner's in place of the home, `.config`, `.config/karvi`, or the
  configuration, what was found shown over what is wanted. The account joins
  the operators' group before its home is made, since it reaches the home
  through that group's `users` directory, and the record lists `membership`
  before `home`. A removal the account cannot carry out (removed by hand, or
  out of the operators' group) is refused; `daemon install` takes the
  account back first. `scripts/sysutil-smoke-test.sh` plants each link and
  checks that the link's target and the host stay as they were.
- **`karvi-sysutil` stops when it cannot write its record.** A refusal to
  write `/etc/karvi/service-karvi.installed` ended only a pipeline, so a full
  or read-only `/etc/karvi` let the install go on and exit 0, the record
  short of what it made; the refusal now ends the run, exit 1, and removes
  its temporary file. A removal that empties the record, after an install
  refused at the account step with `/etc/karvi` already there, no longer
  reports that it cannot write it or leaves a `.service-karvi.*` file behind.
- **`karvi-sysutil` reads `daemon status` without colour.** Under a global
  file's `display.color = "always"`, the install read a painted
  `compatible:` value and warned of "another build" on every run.
- **The scripts end at a signal, their cleanup run once.** Twenty-four
  scripts set their cleanup with `trap ... EXIT HUP INT TERM`, so a Ctrl-C,
  TERM, or HUP ran the cleanup and the script went on, running it again at its
  end: a Ctrl-C during `scripts/device-qualification.sh` against the fake
  stopped the fake, and the run went on through 77 more rows, 46 of them
  failing, to exit 1. `make generated-clean`'s trap was on EXIT alone, which
  dash (`/bin/sh`) does not run when a signal ends the shell, so a Ctrl-C left
  its scratch folder in `/tmp`. Every script and the recipe set their cleanup
  through `on_exit` in `scripts/lib/exit.sh`: the cleanup runs once, and HUP,
  INT, and TERM end the script with 129, 130, and 143. The five bash scripts
  whose trap was on EXIT alone use it too, so a TERM sent to one of them alone
  no longer runs its cleanup while the command it waits on is still running.
  `scripts/lib/exit-test.sh`, which `run_suites` runs first with the JSON
  reader's test, ends a model script under sh and bash with each signal, and
  fails when a script sets a trap on EXIT any other way. ROADMAP's item for it
  is closed.
- **A Ctrl-C stops every lane of the suites.** `run_suites` started its three
  lanes as asynchronous commands, which a script starts with INT ignored, so a
  Ctrl-C ended the verifier and left the lanes running: 45 s into a run, the
  verifier ended at once and, 15 s later, the v0100 and smoke suites and their
  daemons were still running, the lane folder left in TMPDIR. Each lane runs
  in a session of its own (`setsid`); HUP, INT, and TERM while the lanes run
  send TERM to each lane's group, so each suite runs its cleanup, wait up to
  30 s for the groups to empty, a second signal meanwhile ignored (a second
  TERM would cut the suites' cleanups short), then print the lanes' output,
  remove the lane folder, and end the verifier with 129, 130, or 143. The same
  Ctrl-C now ends the run in under a second with nothing left. Once the lanes
  have ended, the signal traps are `on_exit`'s again, through `signal_exits`
  in `scripts/lib/exit.sh`, so `run_suites` refuses a caller that has not
  sourced `exit.sh` before anything runs, and `scripts/verify-release.sh`
  sources it before the suites. `scripts/lib/suites-test.sh`, which `run_suites`
  runs first with the other library tests, runs a model verifier under sh and
  bash over model suites in a scratch root. The library tests start their
  models as a terminal starts a job, through `terminal_job` (`setsid env
  --default-signal=INT`), so `scripts/lib/exit-test.sh` no longer needs
  python3; BUILD-HOWTO §1 names `setsid(1)` among util-linux's tools.
- **A suite's cleanup stops its daemon with `--force` and always reaches its
  removal.** The parity and spool suites stopped the daemon in their cleanup
  through a function whose `fail` is `exit 1`, so a daemon still listening
  after its stop ended the cleanup there: against a model daemon that
  ignores the stop, a suite whose rows had passed ended 1 after 10 s, its
  work folder and its fake left. The smoke, v080, v090, and canary suites and
  the lifecycle replay stopped it without `--force`, which a daemon holding a
  job refuses (`daemon_active_jobs`, exit 112), so after a Ctrl-C in the
  middle of a row the folder went and the daemon ran on: against the released
  build, it finished its job and wrote the removed work folder again (its
  scoreboard, capacity lock, and the job's summary). Every suite and lab that
  starts a daemon or a fake stops it through `scripts/lib/lab.sh`:
  `daemon_stop_force SOCKET COMMAND...` runs `daemon stop --force`, waits up
  to 15 s for the socket to go, and when it stays names it on stderr and
  returns 1, never exiting; `fake_stop VAR` stops the fake and empties its
  pid. A cleanup goes on to its removal (the same model: the suite's own 0,
  the line, the folder removed, the fake stopped; the same job: stopped in
  0.6 s, nothing left), and a row that needs the daemon gone fails on the
  status, among them smoke's stop before its idle-timer daemon, whose status
  was ignored. The device qualification's stop after each daemon row waits
  for the socket before the row's folder moves into the evidence, and records
  a daemon that stays as a failed check. `scripts/lib/lab-test.sh`, which
  `run_suites` runs with the other library tests, runs both functions under
  sh and bash against a model daemon, and fails when a script ignores a
  `daemon stop`'s status or stops a daemon in its cleanup other than through
  `daemon_stop_force`.
- **The checks that karvi writes nothing under the home watch the passwd
  entry's home.** `scripts/verify-shipped.sh`'s stateless `--help` and
  `version` and the completion suite's hidden words looked for writes under a
  fresh `$HOME`, but karvi takes its home from the passwd entry, so a write
  into the operator's own `~/.local/share/karvi` passed them: a model
  executable that made its private root under a folder standing for the
  passwd home passed verify-shipped's lines as they were. Both compare
  karvi's places under the passwd entry's home (`~/.local/share/karvi`,
  `~/.config/karvi`, by their existence) and the host's shared places before
  and after, through `host_places` and `host_places_unchanged` in
  `scripts/lib/host.sh`; verify-shipped makes its work folder, its cleanup
  set, before the stateless runs, so a failed check or a signal leaves none
  of its folders behind, and ROADMAP's item for it is closed.
  `scripts/lib/host-test.sh`, which `run_suites` runs with the other library
  tests, runs both functions under sh and bash over a scratch home against a
  model executable, never the operator's home.
- **A guard of several tests fails on any of them.** `set -e` ignores a
  failure before the last command of an `&&` list, so two guards checked only
  their last test: `scripts/verify-shipped.sh` passed whole with
  `bin/karvi-askpass-linux-amd64` at mode 0644, and the daemon-upgrade suite,
  given a `version` without its `daemon_ipc_schema` line, went past its guard
  and stopped rows later with no message. Each of verify-shipped's four tests
  is its own line; the suite's guard names the version, IPC schema, and build
  when one is missing.
- **Every must-not-appear check stops its suite.** The v061 suite's check was
  a bare `! grep`, which `set -e` ignores, so a help naming `--address` as the
  alias for `--target` passed it. It goes through `absent PATTERN FILE`, now
  in `scripts/lib/lab.sh`, whose five copies in the transcript, display,
  v070, v080, and v090 suites are gone; `absent` also stops at a file it
  cannot read, which the copies passed. `scripts/lib/lab-test.sh` runs it
  under sh and bash and fails on a copy of it or a bare negated pipeline in a
  script.
- **The parity suite's last leak check sees a daemon left running.** It
  looked for `daemon serve.*$TMP`, but a daemon's command line holds its
  `--config` and `--set` before `daemon serve`, so a model daemon left
  running passed it. It looks for `--config $TMP/karvi.toml .*daemon serve`.
- **The transcript suite's first refusal row runs under the suite's places.**
  The `--debug-show-secrets` row ran karvi without the suite's places and fake
  transport, so a regression of the refusal would have recorded under the
  operator's basedir and reached the host's port 22. It runs through the
  suite's `$COMMON`, as the rows after it.
- **The spool suite runs no karvi-prune.** Its S5 ran `karvi-prune --days 0`,
  refused at its arguments since 0.39.0 (exit 2), its status unread, so the
  row held because nothing ran. The row is removed, not repaired: karvi-prune
  walks the jobs, transcripts, and scoreboard folders, never the spool, and
  the Go tests cover what it leaves inside them. S5 keeps the admission
  sweep, and the suite no longer needs karvi-prune beside the executable.
- **`scripts/lib/json.sh`'s examples count the outcome karvi writes.** Its
  two examples counted `run.end` with `completed`; the word is `complete`.
- **`karvi-crun`'s lock is under the operator's private root.** It was
  `${TMPDIR:-/tmp}/karvi-crun.<uid>.lock`, a name another user can make
  first: on a shared jump box, a file alice made at bob's name refused every
  tick of bob's (`cannot create ... Permission denied`, exit 2, under the
  host's `protected_regular`), and on a host without it a file she held
  locked would skip them all; a hand run with `TMPDIR` set took another lock
  than the cron's and ran beside the tick. The lock is
  `<basedir>/state/crun.lock`, the private root karvi resolves from the
  passwd home (`config show --explain basedir`), so the cron, a hand run, and
  `sudo -u` take one lock. A first tick makes the root's missing folders as
  karvi's first activity does (0750, `state` 0700) and never makes an
  explicit `basedir`; a basedir karvi refuses, or an explicit one that is not
  a directory, ends the tick with exit 1 and one line. `KARVI_CRUN_LOCK`
  still names another path. The parity suite's lone tick takes the default
  lock.
- **The source bundle and the package stop when tar fails.**
  `scripts/package-source-bundle.sh` piped tar into gzip and
  `scripts/build-deb.sh` tar into tar, under `/bin/sh` without pipefail, so a
  file tar could not read was left out and the script went on: in a clone
  holding an unreadable file, the bundle script wrote a bundle without it and
  printed its checksum, exit 0, and `make deb` built a package from the short
  copy, exit 0. Each writes tar's archive to a file first and stops with exit
  1 when tar fails; the bundle script removes its output first, so no bundle
  is left.
- **The source bundle's bytes no longer follow the checkout's umask.** Its
  owner, group, and mtime were fixed and its modes were not: clones made
  under umask 0022, 0002, 0027, and 0077 gave four bundles (the 0.40.0 bundle
  carries 0664 and 0775). The bundle's tar sets every mode from the owner's
  execute bit alone (`--mode=a+rX,u+w,go-w`: files 0644, executables and
  folders 0755, the two modes git keeps) and names its format (`gnu`, tar's
  default), so all four give one; the next release's bundle carries 0644 and
  0755, so its bytes differ from what the same tree gave before.
- **The Makefile's smoke targets are removed.** `smoke`, `halt-smoke`,
  `k03-smoke`, `v0100-smoke`, `native-smoke`, and `canary-smoke` each
  depended on `build`, so running one overwrote `bin/`'s executables, a
  release's qualified build among them. A suite runs by its shebang against
  `bin/` or the executable `KARVI` names, and `run_suites` runs them all;
  `build`, and `all` through it, alone writes `bin/`'s executables.
  `tools-build` (`bin/secret-scan`) stays and joins `.PHONY`, which also
  lacked `v0100-smoke` and `canary-smoke`. BUILD-HOWTO and BUILD-QUALIFICATION
  name the suite's own command.
- **karvi-prune(8) names the final statuses karvi writes.** It listed
  `completed`, `halted`, `errored`, `cancelled`, and `exercised`; the words
  are `complete`, `halt`, `error`, `cancel`, `incomplete`, and `exercise`
  (`records.FinalStatuses`), as `docs/PRUNE.md` has them.
- **The spool suite's cut rows cut inside the stream.** S4's cancel and lost
  session sent SIGINT to the client, and killed the fake device, 0.7 s after
  the client started. On the build host the 65.7 MB answer had not begun by
  then, so both records held 0 bytes and the rows passed, their status,
  digest, and count checks holding for an empty output; a client started a
  second late took the SIGINT before karvi ran and exited 0. Each row now
  waits for a beat of the scoreboard counting the target's bytes (up to 20
  s), then cuts, and checks that the record holds bytes: 12 to 31 MB on the
  build host.
- **Three more suites wait on what a row needs, not a fixed time.** The job
  list suite killed its run 2 s after the start, so a run slow to reserve
  its folder left the exercise's folder taken for it; it waits for the run's
  folder and the summary written at acceptance. Its folder without a summary
  was the last ID of the killed run's second, so a run made in that second
  sorted below it and was missed; the folder is the ID after the killed
  run's. The v0100 suite's i1 interrupted a one-second command a second
  after the client started, 0.3 s to spare; the device holds it three
  seconds, as for the shutdown rows. The sysutil suite checked 13 s after
  the operator's logout for the file logind removes 10 s after it; it waits
  up to 30 s and reports the time.
- **The ICMP rows skip only on a host karvi reports without a method.** The
  canary suite's p1 to p3 and the v0100 suite's g1 to g7 read `icmp_method`
  from `karvi version --format json` with sed, and skipped when the read
  found nothing, so the version JSON laid out with a space before each colon,
  or the key moved into an object, skipped the rows and both suites passed on
  every host. They read it with `json_get`, skip on `unavailable` alone, and
  fail on any other answer, the key's absence included.
- **The suites read JSON by its path, not its text.** Forty-one lines of
  eleven scripts (the canary, k03, v0100, transcript, v080, display, parity,
  and sysutil suites, the qualification, the output scale run, and
  `scripts/verify-shipped.sh`) matched a document's text with grep or sed,
  which holds for one layout and finds a key at any depth: the canary
  suite's audit check took any field named `username` for the device's user,
  and its report checks a key of the name anywhere in the report.
  Each reads through `scripts/lib/json.sh`, a JSON Lines stream line by line,
  the transcript suite's start record field by field; the sysutil suite
  reads the job's summary on the host, the container having no python3. A
  daemon's pid is read through `daemon_pid` in `scripts/lib/lab.sh` (the
  canary and v0100 suites, the output scale run, the replay).
  `scripts/lib/json-test.sh` fails on a grep, sed, or awk whose pattern
  names a JSON key, or a JSON stream piped into one, and
  `scripts/lib/lab-test.sh` on a daemon's pid read outside `daemon_pid`.
- **The suites refuse a host whose configuration reaches karvi.** A karvi
  run reads the site's global file (`/etc/karvi/config.toml` or
  `/opt/karvi/config.toml`) and the operator's (`~/.config/karvi/config.toml`
  under the home of the passwd entry) beneath a suite's `--config` and
  `--set`, and an inherited `KARVI__` variable above them, so on a configured
  host the suites tested the site's configuration: a global file's
  `[telnet] read-timeout = "120s"` failed the v090 suite's default row, its
  `[config-lock]` on `sharedroot` refused the suite's `--set` with
  `config_lock_violation`, and an inherited `KARVI__` variable that names no
  key failed `scripts/verify-shipped.sh`'s removed-key rows with
  `config_unknown_environment`. `host_config_clean` in `scripts/lib/host.sh`
  refuses such a host before a script makes anything, naming on its own line
  each file in the `sources` of `karvi config show --format json` and each
  inherited `KARVI__` variable; every suite and lab, the three verifiers, and
  `run_suites` before its lanes call it. A configured host verifies the
  bundle on a build host or in a container with an init (`docker run
  --init`; [`docs/BUILD-QUALIFICATION.md`](docs/BUILD-QUALIFICATION.md)), and
  `scripts/device-qualification.sh` runs from a host and account where karvi
  reads no configuration ([`docs/DEVICE-QUALIFICATION-RUNBOOK.md`](docs/DEVICE-QUALIFICATION-RUNBOOK.md)).
  `scripts/lib/host-test.sh` checks the function against a fake karvi and
  fails on a script sourcing host.sh, or a suite, that does not call it
  first; `scripts/lib/suites-test.sh` checks that `run_suites` refuses before
  any lane starts, and runs its model verifier, as host-test.sh runs its
  rows, under `env -i`.
- **The library tests pass on a host with no shared entry.** On such a host
  (a container, where the new rule sends a configured host's verification)
  `host_shared_entries` lists nothing, and `host_entries_diff` wrote the
  empty listing as a blank line, so every difference also named a blank
  entry and six of `scripts/lib/host-test.sh`'s rows failed, stopping
  `run_suites` before its first suite; the build host passed only because
  `/tmp/karvi-<uid>` was there. An empty listing is no line.
- **The suites keep their places in one file.** Fourteen suites and labs
  listed the private and shared roots, the spool, the scoreboards, the
  capacity root, the audit file, and the floor as `--set` arguments, and
  eight in their own configuration files, each in its own layout; the floor
  alone took three mechanisms, the parity suite's riding the environment
  layer because its rows write their own `[output]` tables. `lab_places` in
  `scripts/lib/lab.sh` writes them into one file, everything under the
  suite's base, and every suite passes it first with `--config`, its own
  file and `--set` rows after: each `--config` is its own layer, so a later
  one outranks it key by key and a suite's own tables never meet a second
  header. A row whose subject is another root or another floor keeps its
  own setting and says so on its line. The replay passes the file to the
  prior release too; v0.28.0 and v0.29.0 read every key in it. The output
  scale run, the replay, and `scripts/device-qualification.sh` take the same
  file, so they too ask no floor of the host beyond each job's estimate; the
  qualification keeps it in its evidence as `places.toml`, beside each run's
  own `TAG.karvi.toml`, and each run's base now holds its scoreboards and
  spool ([`docs/DEVICE-QUALIFICATION-RUNBOOK.md`](docs/DEVICE-QUALIFICATION-RUNBOOK.md)).
  `scripts/lib/lab-test.sh` fails on a place key named outside the file.
- **The suites share their builds, their fake device's start, their newest
  job, and their row runner.** Thirteen build lines in eight scripts, six
  copies of the fake device's start and port wait, fourteen lookups of the
  newest job folder, and three row runners with their failure counters
  are `lab_build`, `fake_start`, and `newest_job` in `scripts/lib/lab.sh`
  and `row`, `row_open`, `fail`, and `finish` in the new
  `scripts/lib/rows.sh`, each checked by the test beside it under sh and
  bash. Every helper is built as the Makefile builds the release, static
  and with `-trimpath`; a lookup that finds no job folder stops its suite
  with a line saying so, where an empty path failed later at a file it
  could not read; the k03 suite prints a line per row, as the canary and
  v0100 suites do, and ends `k03-smoke-test: pass (N rows)`; the
  completion suite and the replay count their failures through `fail` and
  `finish`. The library's lines print their paths, patterns, and messages
  with `printf`, since dash's `echo` read a backslash in them as an escape
  (`absent` named a pattern `\b` as a backspace). `scripts/lib/lab-test.sh`
  and `scripts/lib/rows-test.sh` fail on a copy outside the library.
- **The bundle and release verifiers watch the operator's own places.**
  Around the Go tests and the suites they compared the host's shared
  places alone, so a test or a suite that wrote the operator's private
  root or configuration under the passwd home (`.local/share/karvi`,
  `.config/karvi`) passed them. Both compare `host_places` of the passwd
  home, as `scripts/verify-shipped.sh` and the completion suite do, and
  `host_shared_unchanged`, which nothing else called, is removed;
  `scripts/lib/host-test.sh` fails on either verifier comparing less.
- **Breaking: a bracket of digits is a numeric range in every selector.** A
  bracket holding only digits, hyphens, and commas was a one-character class,
  so a credential CSV row `router[10-12]` matched none of router10 to
  router12, which took the next row's account with no warning, and `--site
  'pod[1-12]'` selected pod1 and pod2 and missed pod10 to pod12. Such a
  bracket is a list of numbers and ranges (`[1-12]`, `[01-12]`,
  `[1,4,7-9]`), and a pattern matches exactly the names it spells, a range
  written at its lower bound's width: `[01-12]` is 01 to 12, `[1-12]` is 1 to
  12 unpadded, and `router[1-12]` does not match `router012`. Every selector
  takes it: `--target`, `--site`, `--device-group`, `--select-platform`,
  `--exclude`, target files, `job list --target`, the credential CSV's
  selector columns, and the rules of `credential-policy-map`,
  `session-init-map`, and `ssh-algorithms-map`; a `.cloginrc` pattern and a
  configuration lock keep their own grammars. A single-digit range (`[0-9]`,
  `[1-3]`) and a bracket holding any other character (`[a-c]`, `[!0-3]`) mean
  what they did; a multi-digit bound or a list of digits does not: `[123]` is
  123, not 1, 2, or 3, `[05]` is 05, and `[1,3]` is 1 or 3, never a comma. A
  bracket of those characters that is no such list (`[1-]`, `[-5]`,
  `[1,,3]`), or that also holds white space beside a digit (`[10, 12]`, a
  one-character class that took router1 and router2), is refused as a
  reversed range is: `target_selector_pattern_invalid` on the command line,
  `credential_csv_row_invalid` in a credential file, and
  `config_match_rule_pattern_invalid` in a map; `[1\-]` is the class of `1`
  and `-`.
- **Breaking: a target holding a numeric range creates the names it
  spells.** A `--target` holding a range selected from the inventory alone,
  so `--tl 'switch0[1-3]'` with no switch01 there stopped with
  `inventory_empty_selection`, and a device not yet listed was typed out by
  name. A `--target`, a
  `--tl` name, a positional device, and a target-file or standard-input line
  holding a range stand for the names it spells, in written order, as if
  each were typed in its place: an inventory device or, absent from it, a
  direct target, recorded under the name as typed (`Switch0[1-2]` records
  `input_target` `Switch01` and `Switch02`, the job manifest keeping the
  value); a name holding a glob as well selects (`router[1-2]*` is
  `router1*`, then `router2*`). A `!` value, `--exclude`, and the inventory
  selectors match a range, as before. The new
  `targets.max-created-names` (1 to 100,000, default 1000, lockable) bounds
  the names one value creates, brackets multiplying: a command-line value
  over it is refused after the configuration is read and before any target
  file, the inventory, or DNS, a file's or standard input's line as it is
  read, with `target_created_names_exceeded` (exit 4), the value named and
  the count stacked over the limit. `--tl` splits at commas and white space
  outside brackets only, so `--tl 'router[1,4]'` is one name where it was
  split and refused, and a backslash keeps the character after it
  (`a\,b`). The help's `--target` and `--tl` lines say to single-quote a
  pattern or list, and its selector paragraph names the range.
- **`targets.inventory-only` runs only the inventory's devices.** The new
  boolean key (default false, lockable, `KARVI__TARGETS__INVENTORY_ONLY`)
  makes a name the inventory lacks a refusal where it was a direct target.
  Under it a plain target name the inventory lacks (a `--target`, a `--tl`
  name, a positional device, or a target-file or standard-input line) stops
  the job after the inventory is read and before planning, with
  `target_not_in_inventory` (exit 5): every such name once, as first typed,
  one per line under the count, after ten the count left, ahead of
  `inventory_empty_selection`. A name holding a range or a glob creates
  nothing: it selects the inventory's devices it matches, in inventory order,
  at any size, so `--tl 'router0[1-5]'` runs the ones of router01 to
  router05 the inventory holds, with no direct router04, and
  `targets.max-created-names` is not applied. A `!` value and `--exclude` only remove, as before.
- **`--inventory`, `--inv`, and `--i` run only the inventory's devices, from
  files given on the line.** login, command, run, crun, and stream lines take
  the option, one dash or two alike (`-i` was refused as ambiguous between
  `--ipv4` and `--ipv6`). Bare, it is `targets.inventory-only` for the
  invocation, through the lock-aware layer, so a site's lock refuses it as it
  refuses `--order`. Each `--inventory=FILE`, in command-line order, is a CSV
  read with the loader's defaults (a header row, karvi's own column names):
  together they are the run's whole inventory, the configured sources unread,
  and the key is on as well. A file is a source named as typed
  (`--inv=boxes.csv`, `--inv='pods/*.csv'`), its `~` and glob resolved by
  karvi as a configured path's; a missing file or a glob matching none is
  `inventory_missing`; a file reached twice, named again or by a glob, is read
  once, under the first value, where two configured sources on one file
  coalesce each device with a warning. The value attaches with `=` alone:
  `--inv ./boxes.csv` is `cli_option_value_detached`, and an empty `--inv=`
  (what `--inv=$F` gives with F unset) is `cli_option_value_missing`, never
  read as a bare `--inv`, which would put the configured inventory in the
  file's place. A stream's `--inv` line stays from job to job and `--reset`
  clears it. `target_created_names_exceeded` gains a last line, `with --i it
  selects from the inventory`; the help lists the option, and its selector
  paragraph says a range under `--i` selects.
- **A configured inventory source named `direct` is an inventory source.**
  karvi told a direct target by its source name, `direct`, so a source given
  that name lost what only a direct target should: its devices without a
  platform had no `platform_not_set` notice, and `karvi DEVICE` refused an
  inventory name one slip from a command word (`karvi lgoin`) as
  `cli_command_near_miss`. A direct target is told by its source path, `cli`,
  which no inventory path can be (the loader makes each absolute), so a
  source's name, and an `--inventory` value, is a label alone.

## 0.40.0 - 2026-11-05

A minor release on 0.39.0 for root commands on the exec channel (`--sudo`),
sleeps between commands (`--sleep`), a device of `-` read from standard input,
the SSH algorithms `--debug` names, each schema counter written once, and a
release verifier that reads karvi's output whole. The execution plan moves to
17, the command record to 5, and the audit to 3; every other counter is 0.39.0's
(configuration schema 6, configuration registry 33, plan report 4, scoreboard 5,
daemon IPC 12, job manifest and summary 5, credential package 2, metrics 1,
inventory 1). Go and every module are 0.39.0's. A site has something to change
where a commands-file line or a platform's `crun-commands` entry that begins
`--sleep` or `--sudo` was meant as device text (`--literal` still sends it as
written); a tool reads a command record's `command_sha256`, reads the audit
trail as schema 2, or finds a schema file by an `$id` that named its counter; or
a device named `-` is given on the command line, which now reads standard input
(a target file still names it). An operator's 0.39.0 daemon, of another build,
is drained by the first submission after the upgrade.

- **The release verifier reads karvi's output whole before checking it.**
  `scripts/verify-release.sh` checked `karvi --version` through `grep -q` under
  `pipefail`; `--version` writes its transports a line at a time, so a `grep`
  that stopped at the native line left karvi's next write to a closed pipe,
  and on a loaded host karvi's SIGPIPE failed the verifier (103 of 1000 runs
  beside eight busy loops). It now reads `version --format json` and
  `--version` once each, and checks the version against `VERSION` and both
  transports through `scripts/lib/json.sh`.
- **Breaking: a schema file's title and `$id` name no counter; each counter
  is written once.** The audit's, the command record's, the job manifest's and
  summary's, the plan report's, and the scoreboard's files in `schema/` were
  `NAME-vN.json` and "schema N", and four named a counter their own
  `schema_version` had left (audit 2, command record 4, scoreboard 4, plan
  report 3); each `$id` is now
  `https://robert-patrick-texas.github.io/karvi/schema/NAME.json`, as the
  execution plan's and the daemon IPC's were, so a tool that finds a schema by
  its `$id` takes the new one. `karvi version` reads each counter from the
  package that owns its schema. `man karvi-audit` and OPERATIONS no longer say
  audit schema 2: an event of another schema than `karvi-audit`'s own, the one
  `karvi version` names, has no row. README and BUILD-HOWTO point at `karvi
  version` for the counters, `scripts/verify-shipped.sh` compares the shipped
  executable's counters with `release/release-manifest.json` where it pinned
  five numbers, and `scripts/verify-release.sh` pins none.
  [`docs/DESIGN.md`, section
  15](docs/DESIGN.md#15-configuration-and-errors), "A counter is written once",
  has the rule.
- **Breaking: `--sudo TEXT` runs a command as root on the exec channel;
  execution plan 17, audit schema 3.** `command`, `run`, and `crun` take
  `--sudo TEXT` (`--sudo=TEXT`), a command source in the order written beside
  `--cmd`, `--cf`, and `--sleep`, written as a `--cmd` is, `--timeout` and
  `--maxbytes` after it its own; a commands-file line `--sudo TEXT` is the same
  statement, where it was device text (`--literal` still sends it as written),
  and so is a stream line. It reaches a platform on the exec channel alone: a
  target on a shell is refused at planning, `channel_shell_sudo_refused` (new),
  and a file line `--sudo` with no command, `commands_file_sudo_invalid` (new);
  a blank `--sudo` is `cli_option_value_invalid`. The command is sent as `sudo
  -S -p 'karvi-sudo-<16 hex>:' -- TEXT`, with no `sh -c`, so a server's rules
  match it and its log names it; the password is written once, at the prompt,
  and the input closed; without one it is `sudo -n -- TEXT`. The prompt is
  removed from the recorded stderr in both forms `sudo` writes it, classic
  `sudo`'s and sudo-rs's; a second prompt is `privilege_failed`, its
  description widened. The password is the answering backend's enable field
  (blank: none), or the fallback's `NETSUDO`, else `NETENABLE` under `netvars`,
  else none, never asked at the terminal; `NETSUDO` is read, and a server's
  enable field carried to the daemon, only for a device with a `--sudo`
  command, and `requires-enable` on a platform on the exec channel requires it
  of those devices alone. The record adds `privilege: "sudo"`, as the audit's
  action does (audit schema 2 -> 3); the plan carries a `sudo` flag per command,
  its platform lists taking a device list's shape (plan 16 -> 17). A root
  command is shown at `srv1#` in the text display, `--echo`,
  `output.TARGET.txt`, and `--live`, one not sent as `! not sent: --sudo TEXT`;
  its collection marker is `! --sudo TEXT`, and the rerun files and
  `karvi-audit`'s rows write it as `--sudo TEXT`. `--dry-run` names the root commands
  (`sudo: commands 2, 3`) and each device's source (`sudo: password from
  NETSUDO`, `sudo: none, sudo -n`), the JSON report as
  `credential_binding.sudo`; `--debug` adds `sudo=password|none` to a command's
  start and `device sudo answered`. `NETSUDO` joins the variables the daemon's
  `daemon_environment_secret_variable` names and the `crun.after` hook's
  environment drops. `karvi-fake-device`'s Linux persona plays `sudo` in both
  forms (`-sudo-rs` for sudo-rs's), `-sudo-asks` making it want `-enable`, and
  answers `whoami` and `id -u`. [`docs/COMMAND-SESSION.md`, "Root commands on an exec
  channel"](docs/COMMAND-SESSION.md#root-commands-on-an-exec-channel) has the
  rules, and [`docs/OPERATIONS.md`, "Root commands and their
  password"](docs/OPERATIONS.md#root-commands-and-their-password) the sources.
- **A line's one `--cmd` or `--sudo` takes the words after it; after `--cmd --`
  or `--sudo --` every word is the command.** `karvi command srv1 --sudo
  systemctl restart nginx` is one root command, as the freeform command's words
  are one command, every word after the first the command's; `--cmd show ip
  interface brief` alike. After `--cmd --` or `--sudo --` every word to the
  line's end is the command, a dash among them (`--sudo -- journalctl -u
  nginx`), and that form may end a line of other commands. Unquoted words beside
  another command stay `cli_command_text_mixed`, its message now `freeform text
  "…" joins only a line's one --cmd or --sudo, right after its value; quote each
  command`, and an unknown option right after a command's first word says
  `for the command's own option write --sudo -- TEXT, or quote the command`.
  [`docs/COMMAND-SESSION.md`, "Grammar"](docs/COMMAND-SESSION.md#grammar).
- **Breaking: a platform's `crun-commands` entries are read as commands-file
  lines.** An entry `--sleep DURATION` or `--sudo TEXT` is the statement, where
  it was device text; a malformed one is `commands_file_sleep_invalid` or
  `commands_file_sudo_invalid` naming `platform P crun-commands entry N`, and a
  trailing sleep or a sleep total over `execution.device-timeout` is refused
  naming the platform. An entry's trailing `\r` stays its text, as before, and
  `--literal` sends every entry as written.
- **Breaking: the command record has no `command_sha256`; command record
  5.** Each record's `command_sha256`, the SHA-256 of its `command`, was read
  by nothing in karvi, and the record holds the command itself; a tool that
  wants the digest computes it from `command`, as the audit's statements
  replaced its digests ([`docs/EXAMPLES.md`, chapter
  63](docs/EXAMPLES.md#63-the-audit-trails-names-and-fields-2026-10-17)).
- **Breaking: `--sleep DURATION` waits before the next command; execution
  plan 16.** `command`, `run`, and `crun` take `--sleep` (1s..12h) as a command
  source in the order written, beside `--cmd` and `--cf`, and a commands-file
  line that is `--sleep DURATION` alone is the same statement, where it was
  device text (`--literal` still sends it as written). The sleep is waited once
  the command before it returns; sleeps in a row add up; a stream line takes
  it too. Refused before any connection: a sleep with no command after it
  (`sleep_without_command`), a declaration after one
  (`declaration_after_sleep`), a file line beginning `--sleep` that is not one
  (`commands_file_sleep_invalid`), and a device's sleeps above a set
  `execution.device-timeout` (`sleep_over_device_timeout`). The plan carries
  `sleeps_ns`; `--dry-run` names each sleep, and `--debug` writes `device
  sleep` as each begins. A job's `commands.txt`, `commands.PLATFORM.txt`, and
  `commands/NAME` write each sleep back as `--sleep` lines before its
  command, so a rerun from them waits as the job did.
  [`docs/COMMAND-SESSION.md`](docs/COMMAND-SESSION.md) has the rules.
- **The device-qualification script's parity rows pass on a device, and the
  runbook gives the run on the production network.** D1, D2, and D4 compared
  four streams of which the first was the device's first contact, whose first
  record alone carries `host_key_enrolled`, so their parity failed on every
  device, and on the fake, since the notice was added; each row now stores the
  device's key first. D7 h failed at times over `system`, where OpenSSH writes
  the trust store without a lock and two first contacts may append the same
  line; a duplicate there is observed, while over `native`, which enrols under
  karvi's lock, one still fails the row.
  [`docs/DEVICE-QUALIFICATION-RUNBOOK.md`](docs/DEVICE-QUALIFICATION-RUNBOOK.md)
  gains section 10, for the operators on the production network: the run at a
  glance, the rows never run on a production device, every command that
  reaches the device, and what to bring back.
- **Breaking: a device of `-` reads standard input.** `-t -`, `-t=-`,
  `--target -`, and `--host -`, a `-` among `--tl`'s names (`-tl -`,
  `-tl=r1,-`), and `-` as the positional device of `command` and `login` read
  the devices from standard input, one per line, as `--tf -` does, at their
  place on the line, so an operator who types `-t` and `-tl` by habit pipes a
  list without changing to `--tf -`. Standard input is still read by one input
  (`stdin_source_repeated`, whose message names the new forms), a stream line
  may not give `-` to `--target` or `--tl`, and `job list --target -` is
  refused (`target_source_stdin_invalid`), its `--target` being a filter of
  jobs. A device named `-` is reached through a target file alone.
- **`--debug` names the SSH algorithms the native transport negotiated.** Once
  its handshake completes, the native transport writes `native SSH connection
  negotiated target=… address=… kex=… hostkey=… cipher=… mac=…`: the key
  exchange, host-key algorithm, cipher, and MAC both sides use, an AEAD
  cipher's MAC as `<implicit>`, as OpenSSH's `-v` writes it, and a cipher or
  MAC whose two directions differ as `to-device/from-device`. The line before
  it names only what karvi offered. A handshake that fails names none; a
  device chooses the same at every connection, so its next completed one
  names it. The system transport is unchanged.
  [`docs/SSH-TROUBLE.md`](docs/SSH-TROUBLE.md) section 3 adds how to find the
  key exchange at fault in a failure only some connections meet.

## 0.39.0 - 2026-11-02

A minor release on 0.38.0 for the fixes of the code review of 2026-10-31. The
configuration registry moves to 33; every other counter is 0.38.0's
(configuration schema 6, execution plan 15, plan report 4, scoreboard 5,
daemon IPC 12, command record 4, job manifest and summary 5, credential
package 2, audit 2, metrics 1, inventory 1). goldmark, which builds the
documents alone, moves to v1.8.7; Go and the other modules are 0.38.0's. A
site has something to change where a trust store holds `@revoked` or
`@cert-authority` lines; a script gives `karvi-prune --days` more than 999,
counts on its exit 0 for a tree it cannot read, or looks for
`askpass_enable_password_unset`, retired for `privilege_failed`; `run
--exercise` runs under a configuration that keeps no job folder; the
configuration's TOML holds a string or a number TOML reads otherwise than Go
did; `display.timestamp` holds `mm` between a date token and a time token; two
inventory rows for one device differ in any field; a `.cloginrc` counts on a
reading clogin's Tcl does not give; a link sits at a day folder's, a job
folder's, a capacity ledger's lock's, or its `devices` folder's name; a
`[platform.NAME]` table holds a field of the wrong kind or `legacy-class`; a
locked key takes a macro from a lower layer; or a configuration holds what it
was refused only later. An operator's 0.38.0 daemon, of another build, is
drained by the first submission after the upgrade.

- **The scratch and spool chains take only the operator's own directory.**
  A present candidate of `tempdir`'s or `spooldir`'s `auto` chain
  (`/dev/shm/karvi/<username>`, `<basedir>/tmp`, `/tmp/karvi-<uid>`,
  `/var/tmp/karvi-<uid>`) was taken whoever owned it and whatever its mode, so
  another user who made `/tmp/karvi-<uid>` first could replace the system
  transport's ssh configuration or a spool. Such a candidate is now passed by
  unless the operator owns it with no group or other access, judged again once
  made, and `config show --explain` names why; an explicit `tempdir` or
  `spooldir` is the site's choice, taken as before. Each spool is read through
  one descriptor opened without following a link, so a file put at its path
  after the digest's pass never reaches `output.TARGET.txt` or a collection
  ([`docs/FILES.md`](docs/FILES.md)).
- **A record that cannot be written leaves the job output whole.** A record
  whose line failed partway, or whose write or sync failed, left a torn line
  in `commands.jsonl` or `errors.jsonl` and offsets past it, took a sequence
  number, so that a following `job follow` refused the next record
  (`ipc_result_malformed`), and in a `crun` let the device's partial
  collection replace its previous file. The failed line is now cut back out of
  its file, the sequence goes to the next record, a record already in
  `commands.jsonl` reaches its followers even when its `errors.jsonl` line
  fails, and the device keeps its previous collection, as a device not reached
  does; a line that cannot be cut back fails every later record of the job
  `output_write_failed` ([`docs/COLLECTION.md`](docs/COLLECTION.md)).
- **Breaking: a revoked host key is refused, and a marked line is no enrolled
  key.** The trust store's `@revoked` and `@cert-authority` lines were read as
  enrolled keys: a revoked key matched, and a certificate authority's key made
  the device's own key a change. A presented key the store marks `@revoked`
  for the device is now `host_key_revoked` under every policy, `secure`,
  `accept-new`, and `insecure`, on both transports (the system transport's
  OpenSSH refusal included, and under `insecure` the comparison beside the
  connection), and under `ssh.halt-run-on-host-key-mismatch` it halts the run
  as a changed key does (exit 114); a `@cert-authority` line, or one with
  another marker, is passed by as OpenSSH passes it, so a device it names
  alone is not enrolled. An enrolled key is appended on a line of its own when
  the store's last line lacks its newline
  ([`docs/SSH-HOST-KEY-POLICY.md`](docs/SSH-HOST-KEY-POLICY.md)).
- **Breaking: `karvi-prune --days` takes one to 999, and a tree it cannot read
  fails the run.** A retention age of 106,752 days or more overflowed and put
  the cutoff in the future, so every job went, running ones included; `--days`
  above 999 is now a usage error (exit 2), the cutoff counted in calendar
  days, and `--minfree NaN` is refused too. A root that cannot be read is a
  `failed kind=tree` line and exit 1, where it was a skipped line and exit 0;
  a folder below it that cannot be read is a `skipped` line and the walk goes
  on, where one such folder dropped the whole tree; and a shared scoreboards
  folder the operator cannot use is passed by, as the shared jobs and
  transcripts trees are. Day folders are read in the local zone they are named
  in, not as UTC, and an empty one goes only when its whole day ended before
  the cutoff ([`docs/PRUNE.md`](docs/PRUNE.md)).
- **Breaking: askpass answers the login's username and password alone, and a
  login resolves no enable secret.** The askpass broker answered an enable
  password, and its prompt matching read the user and host in OpenSSH's
  prompt, so a device or user whose name held `enable` drew the enable secret
  where a password was asked, and one holding `password` drew the password at
  a keyboard-interactive question. The broker now answers the username and the
  password, judging a prompt's own words without its `user@host` part, and
  refuses every other question, the host-key one included
  (`askpass_prompt_unrecognized`); `askpass_enable_password_unset` is retired
  for `privilege_failed`, a command session sending the enable secret in-band
  as before. `karvi login`, whose operator types `enable` at the device, asks
  for no enable secret, reads no `NETENABLE`, and refuses no credential
  without one.
- **Device credentials stay where they are needed.** The `crun.after` hook has
  the operator's shell environment without `NETUSER`, `NETPASS`, and
  `NETENABLE` ([`docs/COLLECTION.md` section
  5](docs/COLLECTION.md#5-the-commit-and-the-diff-mail-the-hook)); a
  credential plan that fails to bind a target wipes the secrets of the targets
  after it; a credential package that fails to decode wipes what it decoded;
  and `secrets.Value.Hint`, which showed a short secret's last bytes and had no
  caller, is gone.
- **The Redis credential backend's handshake is bounded, and its refusals say
  why.** A TLS handshake to a store that accepts and never answers waited for
  ever; it now ends at the backend's connect timeout (`redis_unavailable`).
  Only a reply refusing `AUTH` is `redis_auth_failed`; a closed connection or a
  timeout is `redis_unavailable`, and a reply that is not Redis's
  `redis_protocol_error`.
- **Telnet takes the device's prompt from the last line alone.** The telnet
  transport took any line ending in `>`, `#`, or `$`, anywhere in what it had
  read, for the prompt: a banner line before `Username:` made it type `enable`
  as the username and send the enable secret as the login password, and an
  output line ending so cut a response short, the rest landing in the next
  record. It now matches as the device session does: the last line alone,
  against the platform's prompt patterns, once it has stood for the settle
  period, its login's `Username:` and `Password:` questions included, so a
  platform needs its prompt pattern over telnet as over SSH; a login that asks
  only `Password:` gets the login password there and the enable secret only at
  the privilege step. A read scans only its new bytes and the last line (a
  4 MiB response read 4 KiB at a time took minutes), and a cancel or a forced
  stop ends a blocked read at once
  ([`docs/COMMAND-SESSION.md`](docs/COMMAND-SESSION.md)).
- **A recorded login keeps to the two files it claimed.** The transcript
  day folder of a shared root is group-writable without the sticky bit, so
  another member could put a link at a session's name after its claim: the
  render republished through it, and the end record was appended wherever it
  pointed. Every later open of the transcript and its metadata now refuses a
  link or a file other than the one the claim made
  (`transcript_create_failed`), nothing written through it, and script(1) is
  given the transcript by the wrapper's descriptor, never by a name. A device
  name holding `.meta.` pairs its own transcript and metadata (the login and
  `karvi-prune` alike), and a header that cannot be written after
  `login.start` ends the login through `login.end` with a final scoreboard
  ([`docs/LOGIN-TRANSCRIPTS.md`](docs/LOGIN-TRANSCRIPTS.md)).
- **An audit sink that is not required only warns, and an event of any size
  reaches journald.** A write that failed on a sink not required
  (`audit.journald-required`, `audit.file-required` false) failed the job
  `audit_write_failed`; it is now one warning per sink in the summary's
  `audit_sink_status.warnings`, a required sink's failure as before. An event
  over the journal socket's datagram bound (a job's `start` with a few
  thousand statements) was refused `message too long`, exit 111, before any
  device; it now goes as journald's protocol passes a large entry, in a sealed
  memfd over the socket, and `karvi-audit` reads a field over 4,096 bytes
  whole (journalctl `--all`), where it counted such an event as not audit
  schema 2. ROADMAP's Later item for it is closed.
- **The built-in masks hide more secrets.** A table of 179 secret-bearing
  lines across the five built-in lists found 39 left in clear or masked at the
  wrong word; each is now masked: on `cisco_ios` `server-private … key`, a
  keyring's `pre-shared-key address … key`, MSDP and OSPFv3 IPsec keys, named
  EIGRP, GLBP, LISP, and Easy VPN keys; on `cisco_xr` `server-private` and
  keyring forms and OSPFv3 IPsec keys; on `cisco_nxos` SNMP hosts without
  `traps`, MACsec, BFD, and MSDP keys; on `arista_eos` type 8a, OSPFv3
  passphrases, and TerminAttr's `-cvauth`; on `juniper_junos` manual IPsec and
  MACsec keys. The lists hold 21, 16, 14, 15, and 10 patterns; a site's copy of
  a list takes them from `platform/masks.go`
  ([`docs/COLLECTION.md` section 6.1](docs/COLLECTION.md#61-the-secrets-the-mask)).
- **The daemon and its jobs end cleanly where they hung or leaked.** A run
  through the daemon refused at client planning left the job folder it had
  reserved; it is released. An idle connection, or one stopped mid-line, held
  the daemon's stop for ever; the stop now ends every waiting read, and a
  credential frame is read within the preparation channel's window. A commit
  whose job could not be opened after it started left the job running; it is
  cancelled. `daemon restart` over a socket nothing listens on (a daemon killed
  outright) was refused `daemon_unreachable`; it starts a daemon. A daemon call
  without a deadline ignored its context's cancel; it ends at it. An accept
  error on either socket ended that loop without a word; a temporary one
  (descriptors exhausted) is retried, logged, from 5 ms doubling to 1 s, and a
  fatal one runs the daemon's normal shutdown.
- **Breaking: `run --exercise` refuses a configuration that keeps no job
  folder.** With `output.persist-command = false` from the configuration, an
  exercise wrote `exercise.json` in the daemon's working directory; it is now
  refused `run_mode_conflict`, by the client before a credential is asked and
  by the daemon's job, as `--nof --exercise` is. An exercise's early return
  stops its scoreboard's heartbeat; `job follow` says the admission warnings
  once, not again at each resume; and a job without a folder is named
  `artifacts none` on its cancel and follow lines, not a bare `artifacts`.
- **A device's escapes and a colleague's files reach no terminal raw and grow
  no memory.** A device's escape sequence could make the screen renderer open
  millions of cells from a few bytes (`ESC[999999999C`, repeated cursor moves
  or inserts with no width, cursor down far past the last row); a parameter
  is now capped and held as it is read, moves and inserts with no width reach
  at most column 4,096 (an insert there does nothing), and cursor down stops
  at the last row. Every cell of `karvi-audit`, `watch` (its screen, pane, and
  `--format table`), and `job list` shows its control characters, C1 among
  them, escaped as Go quotes them, where only `karvi-audit`'s COMMAND was;
  `watch` reads a scoreboard entry only when it is a regular file, through one
  descriptor that follows no link and waits on no FIFO, at most 64 MiB of it;
  and the pane no longer panics on a target whose key is longer than the
  width. A device session that fails or closes while its output queue is full
  no longer leaves its reader blocked for ever.
- **The system transport judges a failure by OpenSSH's own lines.** OpenSSH
  prints a device's banner on standard error, and karvi matched words anywhere
  in it: a banner holding `Permission denied` made a later failure an
  authentication one, one holding `Warning: Permanently added` said
  `host_key_enrolled` of a device already enrolled, one holding host-key words
  made an authentication failure `host_key_changed` (a halt under
  `ssh.halt-run-on-host-key-mismatch`), and one over 64 KiB pushed OpenSSH's
  failure line out of the message. A failure is now decided by the last line
  in one of OpenSSH's own forms, each matched whole, the diagnostics kept to
  their last 64 KiB and emptied once authenticated; the enrollment line counts
  only once and only for a device the store held nothing for. Every ssh
  command line ends its options with `--` before the device, so a command
  beginning with `-` is the device's, not ssh's; a failed `ssh -Q` or `ssh -G`
  is asked again rather than kept for the process or the job.
- **A name lookup asks the fallback family in its own time, and a ping that
  cannot run says so.** `name.dns-timeout` bounded both address families'
  lookups together, so when the preferred family (IPv6 by default) timed out
  the other was never really asked; each family now has its own timeout, so a
  lookup can take twice it. The system ping marked each probe failed when ping
  could not run at all (not installed, its socket refused, a usage error),
  which skipped the device `icmp_unreachable`; it is now
  `icmp_capability_unavailable`, the device's own failure kept for a probe
  that was sent.
- **Breaking: the configuration's TOML reads strings and numbers as TOML
  does.** A multi-line basic string decoded its escapes as Go does: a
  backslash ending a line became a literal `\n` rather than joining the lines,
  and Go's own escapes (`\x41`, `\101`, `\a`, `\v`) were accepted, while a
  lone `"` inside the string was refused; a backslash ending a line also hid
  the next line's `include` directive from the include scan. Strings now take
  TOML's escapes alone, a backslash ending a line drops the line break and the
  white space after it, and a `\u` or `\U` that is not a Unicode character is
  refused in every string, and one or two quotes just before a multi-line
  string's closing delimiter belong to the string, as TOML says, where they
  were refused. An integer was read as Go reads one, so `010` was 8, `0022`
  18, and `1__2`, `_1`, `-0x10`, and `0X10` were accepted; an integer is now
  decimal with no leading zero, or hexadecimal, octal, or binary only after a
  lowercase `0x`, `0o`, or `0b`, an underscore only between two digits. A
  float was read as Go reads one too, so `010.5`, `.5`, `5.`, `1__0.5`, and
  `0x1.8p1` were accepted; a float is now decimal as TOML writes it, and
  `inf`, `nan`, and a value beyond a 64-bit float are refused, as no key
  takes one. A file that used any of these is refused `config_toml_syntax`
  (`--set` and the environment by their own codes).
- **Breaking: `display.timestamp` writes what its pattern says.** A lowercase
  `mm` with a date token on one side and a time token on the other was taken
  as minutes, so `dd-mm hh:mm` showed the minutes where the month belonged; it
  is now `display_timestamp_mm_ambiguous` (`MM` is the month, `hh:mm:ss` the
  minutes). Literal text went through Go's time layout, so `(UTC+1)` printed
  `(UTC+10)` and words such as `Jan` or `MST` printed as dates and zones, and
  `sss` after anything but `.` printed `000`; each token now writes its own
  digits and literal text is written as it stands.
- **Breaking: two inventory rows for one device become one only when they
  agree on every field.** Rows naming the same device were taken as one when
  they agreed on the address, platform, transport, port, site, groups, and
  key, and the first row's session cap, name transform, risk tier, deployment
  ring, topology domain, and attributes stood for both, so the device's
  values depended on the file's order. Any difference is now
  `inventory_conflict` naming both lines; a row naming its transport and a
  row leaving it to the mode's default differ, and a key still compares
  folded. The source's digest and its rows come from one read, so the digest
  names the bytes the devices came from.
- **Breaking: `.cloginrc` reads lines, words, patterns, and passwords as
  clogin's Tcl does.** A line ended only at a newline, a lone carriage return
  read as white space; a line ending in a backslash was refused as a
  continuation line; and `add`, `include`, and the directive were taken whatever
  their case, so karvi used an `add PASSWORD` line clogin never finds. A line is
  now what clogin's `gets` reads (a newline, a carriage return and a newline, or
  a carriage return alone ends it), read alone with no continuation (a trailing
  backslash part of its last word), and `add`, `include`, and the directive
  match case and all: `ADD` is an unsupported statement (`cloginrc_malformed`),
  `add PASSWORD` an unknown directive (`cloginrc_unknown_directive`). A braced
  word dropped each backslash; inside braces a backslash and the character after
  it are now kept, as Tcl reads a braced word, and in a braced pattern a
  backslash escapes the glob character after it. `$`, `[`, `]`, and `;` inside
  braces were refused, though Tcl takes them as written, so a file clogin reads
  (`{pa$$w0rd}`) was refused whole; they are now taken as written, and refused
  only in a bare word, the message naming the character. A bare word took the
  character after a backslash as it stood, a double-quoted word was split at its
  spaces, text after a closing brace began another word, and a `#` anywhere
  began a comment; a word is now read as Tcl reads one: backslash sequences are
  substituted in a bare or a quoted word (`pa\tss` holds a tab, `\x41` is `A`),
  a quoted word is one word (`"pa ss"`) in which `$` and `[` are refused, text
  right after a closing brace or quote is refused `cloginrc_malformed` (a `;`
  there `cloginrc_tcl_substitution_forbidden`), and a `#` begins a comment only
  as a line's first word, so a comment after a line's values is read as more
  values. An `add password` line's password and enable password were taken as
  the word reads, where clogin reads each again as a Tcl list and joins its
  words with nothing between; karvi now does the same (`{pass one}` is
  `passone`, `{pa\ss}` is `pass`, `{"a b"}` is `a b`) and refuses a value that
  is not a Tcl list (an unmatched quote or brace) `cloginrc_malformed`, naming
  the file and line; `add user` and `add userpassword` are taken as their words
  read. A pattern was matched by Go's rules, `[!x]` and `[^x]` read as negation
  and `*` stopping at `/`, so a line written for clogin matched other devices in
  karvi, or none; a pattern now matches as clogin's `string match -nocase` does:
  a class has no negation (`[!x]` matches `!` or `x`), `*` and `?` cross `/`, a
  range reads either way round, and a class left open runs to the end of the
  pattern. A pattern no name can match (a trailing `\`, or a class that lists
  nothing, `[]`) was passed by, so a later `*` line answered; the file is now
  refused `cloginrc_malformed` naming the line. An unsupported statement, a
  wrong count of values, an unknown directive, and a line of 1 MiB or more are
  refused naming the file and line too
  ([`docs/OPERATIONS.md`](docs/OPERATIONS.md)).
- **A credential backend's refusal writes its code once, and its message stands
  apart from its fields.** The CSV and `.cloginrc` backends' refusals, and
  Redis's for a coded reply, wrote the code twice in their text
  (`credential_file_mode_unsafe: credential_file_mode_unsafe: …`); the text now
  follows the code once. A refusal's message ran on into its `policy=` and
  `backend=` fields (`…unmatched open quote policy=default backend=clogin`); a
  `;` now sets them apart
  (`…unmatched open quote; policy=default backend=clogin`).
- **Breaking: a link at a day folder's or a job folder's name is refused.**
  A shared job or transcript tree's day folders are every member's to write,
  with no sticky bit, and karvi followed a link planted at a day folder's
  name when it made a job folder there, so every member's jobs that day were
  made where the link pointed, a folder the planter could read; a link at a
  day folder being made was refused, but as `private_directory_not_real`.
  Below a tree's root, a day folder, a job folder, and its `commands` folder
  must now be real folders: a link there is refused
  `output_directory_not_writable` or `transcript_directory_not_writable`, and
  nothing is made where it points. A link at `output.root` or
  `transcript.root`, or above either, is the site's and is followed.
- **karvi-prune reads a summary, a scoreboard file, and a transcript's
  metadata only from a regular file.** The helper read them by name, so a
  FIFO there held the run until a writer came and a link was read through.
  Each is now read through one descriptor, opened without following a link
  and without waiting on a FIFO, only when it is a regular file of at most
  64 MiB (a summary or a scoreboard file) or 1 MiB (metadata). Anything else
  under a summary's or a scoreboard file's name is kept as `unreadable`, as
  a file that does not decode is, and holds no orphan live; under a metadata
  file's name it is kept as `live`, as a transcript without its end record
  is ([`docs/PRUNE.md`](docs/PRUNE.md)).
- **`job list`, `job follow`, and the capacity ledger read a shared file only
  from a regular file.** They read a job's `summary.json` and a ledger's file by
  name, so a FIFO a member put there held karvi until a writer came, and a link
  was read through; `job follow` opened `commands.jsonl` without following a
  link but waited on a FIFO there. Each is now read as karvi-prune reads one: a
  summary of at most 64 MiB, a ledger of at most 16 MiB, through one descriptor
  that follows no link and waits on no FIFO, and `commands.jsonl` opened the
  same way before its owner and mode are checked. Anything else under a
  summary's name is a summary that cannot be read (`job list`'s `?` row,
  `run_output_records_unreadable`), a dangling link included, where it read as
  no summary; under a ledger's name it is an error, never an empty ledger; under
  `commands.jsonl`'s, `run_output_records_unreadable`. The daemon reopened its
  own job's `commands.jsonl` by name for each follower, where a member who can
  write the job's folder (`output.directory-mode = 0770`) could put another file
  or a FIFO; it now reads the file the job's store opened beside its writer,
  held until the job leaves the daemon's table of 1,024 jobs or the daemon stops
  ([`docs/OPERATIONS.md`](docs/OPERATIONS.md),
  [`docs/FILES.md`](docs/FILES.md)).
- **Breaking: a link at a capacity ledger's lock or its `devices` folder is
  refused.** A shared capacity root is every member's to write, with no sticky
  bit, and karvi opened a ledger's lock and made or set its `devices` folder by
  name, following a link a member put there: a link to a colleague's private
  file widened its mode at the colleague's next lease, a dangling one made its
  target, and a link at `devices` took every lease's files where it pointed. The
  lock is now opened without following a link, its mode set through the open
  file, and `devices` must be a real folder: a link at either is refused
  (`capacity_root_unusable` when the root is prepared,
  `capacity_admission_failed` at a lease), the message naming the link to
  remove, and nothing is made or changed where it points. A link at the capacity
  root, or above it, is the site's and is followed. A ledger that does not
  decode is named with its `.json` (`capacity_ledger_malformed`)
  ([`docs/FILES.md`](docs/FILES.md)).
- **`karvi exercise` names a capacity root it cannot read
  `capacity_root_unusable`.** A ledger's lock or file the operator could not
  open, lock, or read was reported `capacity_ledger_malformed`, whose cause is a
  ledger that does not decode; it is now `capacity_root_unusable`, and a ledger
  that does not decode keeps its own code.
- **A `karvi exercise` finding's message is its error's whole text, without its
  code.** The message was the error's first inner text: a host-key finding
  dropped the trust store it names (`known_hosts=PATH: …`), and a dispatch or
  transport-selection finding repeated its code
  (`transport_mapping_missing: selector "lab" has no …`), as did the job's
  `exercise_not_ready` line that quotes it. It is now the text after the code,
  which the finding's `code` field carries.
- **`config show --explain` judges the capacity root and the scoreboards as a
  run does.** A link at the capacity root, which a run follows as the site's,
  was passed by as "not a real directory", so `--explain` named the private
  fallback while a run used the shared root. A root whose `server.lock` was a
  link, a folder, or closed to the operator, or whose `server.json` was a link,
  not a regular file, too large, unreadable, or not valid JSON, was shown as the
  place taken, while a run passed it by for the private fallback (or refused a
  named root). `--explain` now follows a link at the root and judges what it
  points to, and passes by, or refuses, a root whose server files a run could
  not use, naming the file. For both folders, a named path whose parent was
  absent was shown as taken, where a run, which makes only the last folder,
  refused it; it is now refused, `parent absent`. A shared folder the operator
  could not make in the scratch root was passed by in silence while a run warned
  of it; it is now listed on a `passed:` line with its reason.
- **A scoreboards or capacity folder on a filesystem with no free inodes is
  passed by.** A run took the shared folder, which `config show --explain`
  passed by, and then failed each lease's or activity's file in it; under
  `auto` it is now passed by with the run's warning for the private folder, and
  a named one is refused (`capacity_root_unusable`,
  `scoreboard_root_invalid`).
- **A capacity root's server ledger that does not decode is refused under one
  code.** The refusal read `capacity_root_unusable: capacity_ledger_malformed:
  …/server.json: …; a shared capacity root needs mode 2770 …`, two codes and a
  remedy no mode fits; it is now `capacity_root_unusable: …/server.json: …`.
- **A scratch folder's fallback warning names the folder once, and the capacity
  root's carries its code.** Under `auto`, a shared folder the operator could
  not make in the scratch root was warned of as `shared capacity root
  unavailable (mkdir …/capacity: permission denied)`, without the
  `capacity_root_unusable` every other such warning carries, and as `shared
  scoreboard directory unavailable (…/scoreboards: mkdir …/scoreboards:
  permission denied)`, the folder twice. The first now carries the code; the
  second, and a named scoreboards folder's refusal, name the folder once and
  then the cause, `(…/scoreboards: permission denied)`.
- **A login whose `login.start` cannot be written ends its scoreboard.** The
  snapshot written just before `login.start` stayed `running` when that
  audit write failed; the login now writes its final snapshot, at `error`
  with the target failed and `audit_write_failed` in its errors.
- **Breaking: a `[platform.NAME]` table's fields take the registry's kinds.**
  A field of the wrong kind was passed by or misread: `requires-enable =
  "true"` (a string), `privileged-level = 15` and `default-transport = 1`
  (numbers), and `ssh-port = 2222.0` loaded, and `telnet-port = 70023` wrapped
  to port 4487. Every field present is now checked against its kind in the
  registry (`config_type_error`), `channel` included, and `telnet-port` must
  be 1..65535 (`config_platform_telnet_port_out_of_range`, new) as `ssh-port`
  must; a plan's platform never takes a port outside the range or a fraction.
- **Breaking: a locked key takes its macros from the global configuration.**
  A locked key's value could take a macro that the user's file, `--config`,
  or `--set` defined, so a lower layer changed the locked value through the
  macro; such a value is now refused `config_lock_violation`, naming the
  macro, where it was set, and the lock (`%operator%` is no layer's). An
  alias that arrived through a macro was not turned into the word it stands
  for, so `source = "%src%"` with `src = "auto"` was refused; a filled value
  is now stored as one written out is. The keys are filled in sorted order,
  so a configuration with two faults names the same one on every run, and
  `config show --explain` lists every macro a key takes.
- **Breaking: the configuration refuses what it would refuse later.** A
  formula backend whose `password-source` names another formula backend
  loaded and failed at the first device; it is now
  `config_credential_formula_source_invalid` at load.
  `display.record.header` and `display.record.footer` were checked only when
  a login used them; they are now checked at load with the other templates.
  A name or credential transform's operation ignored a key it does not take,
  so a misspelled `replacment` read as no replacement; it is now
  `config_unknown_key`, a text parameter of another kind (`replacement = 5`,
  a pair's `to = 5`) read as an empty one and is now `config_type_error`, and
  a `replace-chars` pair that is not an inline table of `from` and `to` is
  `transform_pair_rune_invalid`. `config = {
  includes = false }` written inline was not judged by the include rules (a
  root's directives were followed, an include's own `config.includes` and
  one after the first directive were not refused), nor was an inline
  `schema-version` checked; both are now judged as when written out.
- **Breaking: a `[platform.NAME]` table takes no `legacy-class`; registry
  33.** The field took any word and nothing read it; it is now
  `config_unknown_key`.
- **The newest goldmark.** `github.com/yuin/goldmark` v1.8.7 (from v1.8.6);
  x/crypto, x/sys, x/term, and Go 1.27.3 were the newest already. goldmark is
  vendored for `tools/md-to-html` alone, and no executable contains it. Its
  changes bound the parser's work where it searched the same text again (an
  unclosed HTML comment, a long footnote label, many headings of one name)
  and rework how an autolink's trailing punctuation and an indented code block
  are read; `make html` builds the same 40 pages, byte for byte.

## 0.38.0 - 2026-10-28

A minor release on 0.37.0 for the Cisco platforms' names, `cisco_ios` and
`cisco_xr`. Every counter is 0.37.0's (configuration schema 6, registry 32,
execution plan 15, plan report 4, scoreboard 5, daemon IPC 12, command record 4,
job manifest and summary 5, credential package 2, audit 2, metrics 1,
inventory 1), and Go and the modules are 0.37.0's. The old names are read
wherever a platform name is read. A site has something to change where a script
or a tool looks for `cisco_iosxe` or `cisco_iosxr` in a record, a job folder, or
output, which carry `cisco_ios` and `cisco_xr`; where a glob was written for an
old name; or where a configuration holds `[platform.cisco_ios]` beside
`[platform.cisco_iosxe]` or `[platform.cisco_xe]`, refused
`config_platform_name_invalid`. An operator's 0.37.0 daemon, of another build,
is drained by the first submission after the upgrade.

- **Breaking: the Cisco platforms take the names the market uses.** The built-in
  `cisco_iosxe` is `cisco_ios`, for IOS and IOS XE alike, and `cisco_iosxr` is
  `cisco_xr`, the names Netmiko and NTC's TextFSM templates give them; their
  definitions are unchanged, `--pi` and `--pr` are short for them, and
  `platform-resolution.default` ships as `cisco_ios`. The other spellings in
  use, `cisco_xe` and `cisco_iosxe` for `cisco_ios` and `cisco_iosxr` for
  `cisco_xr`, are read as the built-in's name wherever a platform name is read:
  an inventory row, a source default, `--platform`, `--select-platform`,
  `platform-resolution.default` and `platform-resolution.unknown-fallback`, a
  `[platform.NAME]` table's name and driver, and a map rule's or a credential
  row's `platform`. Records, job folders, and output carry the built-in's name
  where they carried `cisco_iosxe` or `cisco_iosxr`; a glob matches built-in
  names alone; and a configuration holding `[platform.cisco_ios]` beside
  `[platform.cisco_iosxe]` or `[platform.cisco_xe]` is refused
  (`config_platform_name_invalid`), the two being one platform. The
  configuration registry stays at 32 ([`docs/EXAMPLES.md`, chapter
  86](docs/EXAMPLES.md#86-platform-names-the-markets-2026-10-28)).

## 0.37.0 - 2026-10-27

A minor release on 0.36.0 for the package's update from GitHub, `karvi-sysutil
package update`, and the native transport as karvi's own, without scrapligo.
The configuration registry moves to 32; every other counter is 0.36.0's
(configuration schema 6, execution plan 15, plan report 4, scoreboard 5,
daemon IPC 12, command record 4, job manifest and summary 5, credential
package 2, audit 2, metrics 1, inventory 1). scrapligo, gotextfsm, and pty
leave the build; Go and the other modules are 0.36.0's. A site has something
to change where a configuration sets `ssh.transports.native` or maps a custom
slot to `builtin:ID` or a bare ID (each refused `transport_mapping_invalid`),
a script selects `--transport preferred`, reads the native transport's ID
`scrapligo-v1` (now `native`) or the `--debug` transport line's
`implementation=`, counts on `config show KEY` printing `error: not found` for
a key it cannot show, gives `config show` `--format` beside keys or
`--explain`, reads the registry artifact's `sensitive`, or looks for
`native_transport_unavailable` or `transport_implementation_invalid`, both
retired. An operator's 0.36.0 daemon, of another build, is drained by the
first submission after the upgrade.

- **`karvi-sysutil package update`, the package's update from GitHub.**
  `sudo karvi-sysutil package update` finds the latest release from the
  redirect of its page, `releases/latest`, and when that release's package is
  newer than the installed one fetches `karvi_X.Y.Z_amd64.deb` and its
  `.sha256` with curl, checks the one against the other, and installs it with
  `apt-get`; `--check` only reports. `--proxy URL` reaches GitHub through an
  `http://` or `https://` proxy, and `-k` (`--insecure`) accepts any
  certificate, GitHub's and the proxy's, for a network that inspects TLS. It
  is refused without curl, beside a bundle install's `/usr/local/bin/karvi`,
  and without the package. A running daemon of `karvi-daemon@.service` is
  named for its restart, which stays the administrator's; an operator's own
  daemon is drained by the operator's next submission. The checksum finds a
  damaged download and proves nothing of the release's origin, which the
  release's signature, a ROADMAP item, is to
  ([`docs/OPERATIONS.md`, "Upgrade"](docs/OPERATIONS.md#upgrade)).
- **Breaking: the native transport is `native`, and `ssh.transports` maps
  programs alone; registry 32.** The native transport is karvi's own
  `x/crypto/ssh` connection, which scrapligo only passed through, and it has
  no implementation ID apart from its name: `karvi version` lists `native:
  compiled-in` over `system: external-executable`, its JSON and a job
  manifest's `app.build.ssh_transports` carry `"id": "native"` (the job
  schema stays 5), and the `--debug` transport line drops `implementation=`.
  `ssh.transports` keeps `system`, an OpenSSH executable, and a custom slot's
  `exec:PATH`; the key `ssh.transports.native` is removed (registry 32, the
  configuration schema 6). The new code `transport_mapping_invalid` (exit 2)
  refuses at load `ssh.transports.native` or `ssh.transports.telnet`,
  whatever the value, and a custom slot's `builtin:ID` or bare ID, such as
  `alt = "scrapligo-v1"`; `native_transport_unavailable` and
  `transport_implementation_invalid` are retired. The selector `preferred`
  and an implementation ID given as a selector are gone: each is an ordinary
  slot name, and `--transport preferred` is `transport_mapping_missing` (exit
  8) unless a slot maps it. `run --exercise` shows `transport: native
  available` and `transport: system available (/usr/bin/ssh)`, its finding
  reads `native transport available` (and `system transport available`), and
  its details drop `implementation`, which an askpass failure's details
  replace with `kind`. Login's refusal of the native transport names it
  `native`; `config init`'s starter no longer writes `native =
  "scrapligo-v1"`; the run and crun help drop the native status paragraph.
- **scrapligo leaves the build.** `github.com/scrapli/scrapligo` v1.4.3 is
  removed, and with it `github.com/sirikothe/gotextfsm` v1.2.0 and
  `github.com/creack/pty` v1.1.24, which only scrapligo imported; `vendor/`
  loses 140 files (x/crypto's `ssh/knownhosts`, which only scrapligo used, among
  them), and the executable about 18 KB. goldmark, x/crypto, x/sys, and x/term
  are unchanged. Nothing the native transport sends or reads changes. The
  release drops what checked scrapligo alone: `release/SCRAPLIGO-EVIDENCE.md`,
  the module pin and the import boundary in `verify-release.sh`, and
  BUILD-QUALIFICATION's rule that the version output names ScrapliGo; the checks
  that the native transport verifies the host key in its own handshake and runs
  no subprocess stay, on `internal/transport/native`.
- **Breaking: `config show KEY` refuses a key it cannot show.** A name that is
  no key is refused as `--set` refuses it, `config_unknown_key` (or
  `config_key_removed`), and a key a table could hold that the configuration
  does not set, a transport slot, a platform's field, or a macro, with the new
  code `config_key_unset`; each exits 2, on standard error, before any key is
  printed. Each was a block reading `error: not found` on standard output among
  the keys shown, exit 0, so a script's misspelt `karvi config show
  ssh.transprots.alt && ...` went on.
- **Breaking: `config show` refuses `--format` beside keys or `--explain`.**
  `--format` encodes the full view alone, the values as TOML lines or a JSON
  object; beside keys or `--explain`, whose view is text, it was ignored, and
  `config show --format json display.color` printed text. It is now refused
  before the configuration loads with the new code `config_show_format_conflict`
  (exit 4). The registry artifact's keys no longer carry `sensitive`, a mark set
  on none, and the JSON view's `<redacted>` branch, which nothing reached, goes
  with it.

## 0.36.0 - 2026-10-26

A minor release on 0.35.0 for a command list per device, `--cf DIR`, and a
live view of a session, `--live`. Three counters move, execution plan 15,
plan report 4, and scoreboard 5; every other is 0.35.0's (configuration schema
6, registry 31, daemon IPC 12, command record 4, job manifest and summary 5,
credential package 2, audit 2, metrics 1, inventory 1). scrapligo moves to
v1.4.3, and Go is 0.35.0's. A site has something to change where a script
reads `command_plan_digest` (removed; `plan_digest` covers the commands) or
the plan's `commands_file` or the scoreboard's `commands.file` (now the lists
`commands_files` and `commands.files`), counts on every `--cmd` going before a
`--cf` file's lines, writes a declaration after a `--cf`, abbreviates
`--literal` as `--li`, or looks for `commands_file_repeated` or
`commands_file_is_directory`, both retired. An operator's 0.35.0 daemon, of
another build, is drained by the first submission after the upgrade.

- **`--echo` shows the prompt a statement was sent at.** The display of
  `command`, `run`, and `job follow` echoed each statement at the prompt
  that came back after it, so a statement that changes the mode read
  `router1(config)#configure terminal` and `router1#end`; it now reads
  `router1#configure terminal` and `router1(config)#end`, the record's
  `promptbefore`, as `output.TARGET.txt` always had it. The records are
  unchanged.
- **A live view, `--live`, for `command`, and for `run` and `crun` with
  `--no-daemon`.** `--live` prints the device's bytes on standard output as
  the session reads them, in place of the text display: the header when the
  session opens, every byte from the first, the setup (`enable`, the paging
  commands) included, and the footer after the session's end, so a long
  `show` is seen as it comes and not when it completes. A run with `--live`
  is serial and follows each device's session in turn: `dispatch.default`
  is set to `serial` from `--live` when the configuration says another mode,
  so that a site's lock on the key refuses it as it refuses `--ds`. A record
  leaves karvi's own lines alone (a failed command's `karvi: target=…`); the
  records, the job's files, the audit, and the plan's digest are those of
  the same job without it. The exact value of each secret the session
  writes (the enable secret, a telnet password) is shown `<removed>`, even
  when the device echoes it in two pieces; escape sequences follow
  `output.ansi`; an exec target's command is shown at the prompt `--echo`
  shows it at. Refused with the new code `live_option_conflict` (exit 4):
  `--format jsonl` or `json`, `--echo`, `--border`, or `--noborder` beside
  it; on a run, no `--no-daemon`, `--dry-run`, `--exercise`,
  `--follow=false`, `--dispatch parallel` or `wave`, or `dispatch.default`
  set to either by `--set`. A configured echo or border is set aside. On
  `command`, `run`, and `crun`, `--li` no longer abbreviates `--literal`;
  `--lit` does. The fake device answers `show drip` with three lines,
  `-drip` apart.
- **Breaking: `command_plan_digest` removed; execution plan 15, plan report
  4.** The plan, the job header, and the dry-run and exercise reports no
  longer carry `command_plan_digest`, which nothing read but checks against
  the same message; `plan_digest` covers the plan, its commands included, and
  a job's `manifest.json` holds the plan and the header without it. The dry
  run's `commands:` line holds the count alone.
- **Breaking: a run's commands go in the order written, and `--cf`
  repeats.** `command`, `run`, `crun`, and a stream send each `--cmd` and
  each `--cf` file's lines in the order the line gives them, where every
  `--cmd` went first wherever `--cf` stood: `--cf checks.txt --cmd 'show
  clock'` now sends the file first. `--cf` may be given more than once, each
  file read in its place; a second `--cf -` is `stdin_source_repeated` (exit
  4), and `commands_file_repeated` is retired. A declaration (`--expect`,
  `--blind`, `--blind-return`, `--timeout`, `--maxbytes`) attaches to the
  `--cmd` written right before it: one written after a `--cf`, which went to
  an earlier `--cmd`, is refused `declaration_with_commands_file` (exit 4),
  and a stream drops such a line; one before every command is
  `declaration_before_command`, with or without a `--cf`. A stream takes
  several `--cf` lines, each in the order typed.
- **Breaking: the plan's `commands_files` and the scoreboard's
  `commands.files`; scoreboard 5.** The plan names every `--cf` by its base
  name in the order given, `-` for standard input, in place of
  `commands_file` (execution plan 15); the scoreboard's `commands.files` in
  place of `commands.file` does the same, and the watch screen's pane line
  joins them: `run: 3 statements from one.txt, two.txt`.
- **`--cf` takes a folder: each device its own list.** On `command`, `run`,
  `crun`, and a stream, `--cf DIR` sends each selected device the file
  `DIR/NAME`, NAME its collection file's (`core-nyc-01`, the whole name under
  `output.crop-to-dot = false`), read as a `--cf` file in its place among the
  line's commands; a file no selected device is named for is not read. A
  selected device without its file is refused at planning with the new code
  `commands_file_missing` (exit 5), naming the folder and up to ten devices
  with the file each lacks, and two selected devices with one file name with
  the new code `commands_file_name_collision` (exit 5), whatever files the
  job writes; an empty or unreadable file, or a folder in its place, is
  `commands_file_empty` or `commands_file_unreadable` (exit 4), and
  `commands_file_is_directory` is retired. The plan carries each device's
  list with its declarations, `target_commands` (execution plan 15);
  `--dry-run` shows each device's list on its `commands:` line; a stream
  reads the folder as each job is planned.
- **`ipc_frame_too_large` gives the size over the bound.** A request over
  `daemon.max-ipc-frame-bytes` is refused with its size over the bound,
  stacked, where the message named the bound alone, and a frame read over it
  names the key. A plan carrying many devices' lists can reach the bound:
  about 9 MB for 2,500 devices of 40-line lists, over the 8 MiB default
  ([`docs/OPERATIONS.md`, "A command list per
  device"](docs/OPERATIONS.md#a-command-list-per-device)).
- **A collection never writes over the run's own `--cf` lists.** `crun`, and
  `run` or `command` given `--cd` or `--fs`, replaced the lists they had read
  with the devices' output, exit 0, when the collection directory was a `--cf`
  folder of the run or a `--cf` file was a selected device's collection file;
  either is now refused at planning with the new code
  `commands_file_in_collection` (exit 4), the `--cf` path over the
  collection's, links followed, with or without `--fs`.
- **The job folder's command copies send what was sent.** `commands.txt`
  writes each command's blind returns back as its trailing `\r` sequences,
  where they were dropped and a rerun from the copy timed out at the prompt
  the returns had answered; beside a `--cf` folder the job folder keeps each
  device's list as `commands/NAME`, so `karvi run --tf
  J/failed-devices.txt --cf J/commands` reruns the failures with their own
  lists. A copy is read without `--literal`.
- **`summary.json` names `commands.txt` only when it is written.** A `crun`
  with the platform's lists and a run with a `--cf` folder write none, and
  the summary's `paths` named it.
- **The audit's job `start` beside lists per platform or per device.** It
  carried `command_count` 0 and an empty `commands`; the count is now the
  longest list's, as the metrics give it, and `commands` is left out, each
  device's command events naming what it was sent.
- **A job manifest's initial states are checked against each device's own
  list.** The manifest's validation, which `tools/schema-check` runs on a job
  folder, refused a `crun` with the platform's lists and a run with a `--cf`
  folder (`manifest_invalid`, a command index out of range of the plan's
  empty one list).
- **The newest scrapligo.** `github.com/scrapli/scrapligo` v1.4.3 (from
  v1.4.2); the other six modules and Go 1.27.3 were the newest already. Its
  one change is to the NETCONF driver's hello under NETCONF 1.1, which
  `karvi` links and never runs; nothing the native transport sends or reads
  changes. `karvi version` and the transport's status in the help name
  scrapligo 1.4.3.

## 0.35.0 - 2026-10-21

A minor release on 0.34.1 for a recorded login into the operator's folder,
`--record=PATH`; every counter is 0.34.1's (configuration schema 6, registry
31, execution plan 14, daemon IPC 12, command record 4, job manifest and
summary 5, plan report 3, credential package 2, scoreboard 4, audit 2, metrics
1, inventory 1), and the module graph and Go are 0.34.1's. A site has
something to change where a script looks for a `--record=PATH` session's files
under `PATH/YYMMDD/`: they are `PATH/<device>.log` and its metadata, or
`PATH/<device>-YYMMDD-HHMMSS` when that name was taken. An operator's 0.34.1
daemon, of another build, is drained by the first submission after the
upgrade.

- **`--record=PATH` records into PATH under the device's name.** A recorded
  login given a folder writes into it, with no day folder, PATH and its
  parents created when absent (`--record=.` is the working directory):
  `router1.log` beside `router1.meta.jsonl`. When either name is taken, both
  take the session's start, `router1-261020-184652.log` and its metadata;
  when either of those is taken too, the login is refused with
  `transcript_name_taken` (exit 111) and nothing is written. A file of the
  operator's in PATH is never changed. A bare `--record` keeps
  `transcript.root`'s day folder and `<device>-HHMMSS`.
  `transcript_path_not_directory` reads `--record=PATH; name a folder`.

## 0.34.1 - 2026-10-20

A patch release on 0.34.0 for karvi under a shorter name, the newest modules,
and Go 1.27.3, which change no record, file, key, or schema; every counter is
0.34.0's (configuration schema 6, registry 31, execution plan 14, daemon IPC
12, command record 4, job manifest and summary 5, plan report 3, credential
package 2, scoreboard 4, audit 2, metrics 1, inventory 1), five modules move
to their newest versions, and a site has nothing to change. An operator's
0.34.0 daemon, of another build, is drained by the first submission after the
upgrade.

- **karvi under a shorter name keeps its scratch files.** A karvi started
  through a symbolic link of another name, a site's `/usr/bin/k`, was judged
  dead by the scratch and spool sweeps, which read the name it was started by:
  a recorded login's own child removed its wrapper's timing log at once, and
  the transcript was rendered without its resizes under
  `transcript_timing_unreadable`; another karvi's sweep could take a running
  one's askpass socket, `ssh` configuration, or spool alike. A pid is now alive
  as karvi when the file it runs is named `karvi…`. A shorter name is a link or
  an alias; the executable itself keeps its name and place.

- **The release verifier removes its package folder when it fails.**
  `scripts/verify-release.sh` removes
  `${TMPDIR:-/tmp}/karvi-verify-deb-<random>` at its exit, passed, failed, or
  interrupted; a step after the package's build that failed, or a Ctrl-C, left
  the folder and the package in it.

- **The newest x/crypto, x/sys, x/term, pty, and gotextfsm.**
  `golang.org/x/crypto` v0.58.0 (from v0.26.0), `golang.org/x/sys` v0.49.0
  (v0.23.0), `golang.org/x/term` v0.47.0 (v0.23.0), `github.com/creack/pty`
  v1.1.24 (v1.1.23), and `github.com/sirikothe/gotextfsm` v1.2.0 (a 2020
  snapshot); scrapligo v1.4.2 and goldmark v1.8.6 were the newest already.
  `govulncheck` finds ten advisories against `x/crypto/ssh` on `karvi`'s call
  paths at the old modules (GO-2025-3487, GO-2026-5013, GO-2026-5015,
  GO-2026-5017 to GO-2026-5021, GO-2026-6354, GO-2026-6355), and none at the
  new; the helpers had none. The native transport now offers
  `mlkem768x25519-sha256`, first among karvi's default key exchanges, which
  x/crypto now implements. A negotiation failure's message and the Delete key
  at a prompt are as they were.

- **Go 1.27.3.** The executables are built with Go 1.27.3, where 0.34.0's
  were built with Go 1.27.2. Go 1.27.3 carries security fixes to
  `crypto/x509`, which `karvi` links and the three helpers do not.

## 0.34.0 - 2026-10-19

A minor release on 0.33.0 for a device as the first word, `karvi DEVICE`;
every counter is 0.33.0's (configuration schema 6, registry 31, execution plan
14, daemon IPC 12, command record 4, job manifest and summary 5, plan report 3,
credential package 2, scoreboard 4, audit 2, metrics 1, inventory 1), and the
module graph is 0.33.0's. A site has something to change where a script takes
a first word that is no command for `cli_command_unknown` at once: a plain
word now names a device, and a word one slip from a command word is
`cli_command_near_miss`. An operator's 0.33.0 daemon, of another build, is
drained by the first submission after the upgrade.

- **A device as the first word, `karvi DEVICE`.** A first word that is no
  command and is a plain name, none of `* ? [ \ ^ $` and no leading `!`, names
  a device: `karvi router1` is `karvi login router1`, and `karvi router1 show
  clock` is `karvi command router1 show clock`, as is a line with `--` and
  words after it or an option only `command` takes (`--cmd`, `--cf`, `--echo`,
  ...); `karvi router1 --help` prints the mode's help. A command word, alias,
  or prefix wins, and `karvi login NAME` reaches a device named as one. A word
  one slip from a command word of four letters or more, ignoring case, that no
  inventory names is refused as `cli_command_near_miss` (exit 4) before any
  claim or lookup; a name DNS says does not exist is `cli_command_unknown`
  (exit 4), neither a command nor a target, where `karvi login NAME` keeps
  `dns_nxdomain` (exit 7); a timeout or a server failure keeps its code. A
  plain first word that was `cli_command_unknown` at once now costs the
  configuration read, the inventory load, and, unless the inventory or an
  address answers it, a DNS lookup.

## 0.33.0 - 2026-10-18

A minor release on 0.32.1 for one activity ID for every activity, a `cmd`
job's `job_id`, and the six longest messages shorter; every counter is
0.32.1's (configuration schema 6, registry 31, execution plan 14, daemon IPC
12, command record 4, job manifest and summary 5, plan report 3, credential
package 2, scoreboard 4, audit 2, metrics 1, inventory 1), and the module graph
is 0.32.1's. A site has something to change where a script lists the
scoreboards folder, which holds each activity's claim `<ID>.id` whatever
`watch.enabled` says; takes a `cmd` job's `job_id` for empty or absent; or
matches the old words of the six messages. An operator's 0.32.1 daemon, of
another build, is drained by the first submission after the upgrade.

- **One activity ID for every activity, never two alike.** Every `cmd`,
  `run`, `crun`, run of a `stream`, dry run, exercise, and login claims its
  ID by creating an empty file `<ID>.id` in the scoreboards folder before
  anything else, a job that writes files making its job directory under the
  same ID; a name taken in either moves to the next sequence, and the claim
  is never handed back. In shared mode the ID is unique on the host, and per
  operator otherwise: two jobs under `--nof` in one second, a dry run beside a
  job, and a login beside a job no longer share an ID, a job's scoreboard no
  longer replaces a login's, and a second run under `--nof` through one daemon
  in the same second runs, where it was refused (`idempotency_conflict`). The
  ID's form is unchanged. A claim that cannot be created stops the activity
  (`activity_id_generation_failed`); a site that wants no fallback from the
  shared folder names it in `scoreboards` and locks the key.
- **A `cmd` job's `job_id` is its ID** in its summary, its records, its
  scoreboard, and `job list --format json`, as a `run`'s is; it was empty in
  the summary and absent from the records and the scoreboard.
- **`watch.max-files` counts the scoreboard files alone,** not the claims
  beside them; an empty scoreboard file is a row in error.
- **The six longest messages are shorter,** under one rule (DESIGN): the
  subject by its value, no word that adds nothing, the reason left to the
  code's description, a remedy last. `credkeyref_unresolved`,
  `control_path_root_too_long` (its length over its limit on lines of their
  own), `setup_tmpfiles_dir_missing`, `setup_tmpfiles_mismatch`,
  `inventory_secret_column`, and `config_inventory_source_secret_mapping`
  keep their codes and exits; a script that matched their old words changes
  with them.

## 0.32.1 - 2026-10-18

A patch release on 0.32.0 for the one-letter command words and `karvi-audit`'s
wider DEVICE column, which change no record, file, key, or schema; every
counter is 0.32.0's (configuration schema 6, registry 31, execution plan 14,
daemon IPC 12, command record 4, job manifest and summary 5, plan report 3,
credential package 2, scoreboard 4, audit 2, metrics 1, inventory 1), and the
module graph is 0.32.0's. A site has something to change only where a script
reads `karvi-audit`'s table by position. An operator's 0.32.0 daemon, of
another build, is drained by the first submission after the upgrade.

- **One-letter command words: `c` for `command`, `r` for `run`, `l` for
  `login`, `s` for `stream`.** Each is an alias, so it matches exactly:
  `karvi c r1 show clock` is `karvi command` and `karvi s` is `karvi stream`,
  where `c` had been refused as ambiguous (`command`, `config`, `crun`) and `s`
  likewise (`setup`, `stream`); `r` and `l`, which had reached `run` and
  `login` as prefixes, stay theirs whatever word is added. `co` stays
  ambiguous, `cr` stays `crun`, and `se` stays `setup`. The help and
  `karvi(1)` list them beside `cmd` and `-`.
- **`karvi-audit`'s DEVICE column is 30 cells, from 20,** so the extended
  hostnames of switches, routers, and servers show whole: a canonical name of
  up to 30 characters whole, a longer one cut with `…`; COMMAND starts
  ten cells later, and at a terminal DEVICE is whole and centred from 99
  columns (89 before). The lines to a pipe, which a script may read, move
  with it.

## 0.32.0 - 2026-10-17

A minor release on 0.31.1. The configuration registry moves from 30 to 31, the
execution plan from 13 to 14, the command record from 3 to 4, the job manifest
and summary from 4 to 5, the plan report from 2 to 3, the scoreboard from 3 to
4, and the audit from 1 to 2; every other counter is 0.31.1's (configuration
schema 6, daemon IPC 12, credential package 2, metrics 1, inventory 1), and an
operator's 0.31.1 daemon, of another build, is drained by the first submission
after the upgrade. `karvi-audit` is a fourth executable. A site has something
to change where a parser reads the audit file, the journal, or the syslog it
forwards (the events' names, fields, and line, and the statements, masked, in
place of their digests); where a script reads an outcome, a status, or a count
by its past-tense word, or the collection's `replaced` and `kept`; where a
display template names `<replaced>` or `<kept>` (refused at load); where a
`crun.after` hook reads `KARVI_CRUN_REPLACED` or `KARVI_CRUN_KEPT`; and where a
host holds the tab completion file from before (`sudo karvi setup tab` once
more). The module graph is 0.31.1's.

- **Breaking: the audit trail's names, links, and line; audit schema 2.**
  Every audit event is named `<kind>.<event>`, the kind `cmd`, `run`, or
  `crun` as `job list` shows it (`action.mode` the same word): `start` and
  `end`, once each per job; `command`, once per device per command, a command
  never sent included, in place of `command_completed`; `cancel` when a cancel
  is asked, in place of `run.cancel_requested`; `login.start` and `login.end`;
  and the collection hook's `crun.after`. An exercise ends in `<kind>.end`
  with the outcome `exercise`, in place of `run.exercised`. The outcome is in
  its own field alone, never in the name (`login.errored` and
  `crun.after.failed` are gone), and a `start` and a `cancel` carry none.
  journald's fields: `KARVI_JOB_ID` on every event of a job, a `cmd` job's
  included; `KARVI_LOGIN_ID` on a login's; `KARVI_DEVICE`, the device's
  canonical name, on each `command` event and a login's; `KARVI_OPERATOR`, the
  account the job or the login ran as, on every event; and `KARVI_LOGINUSER`
  when the kernel's login UID names another account, the person behind
  `sudo`, a job through the daemon taking its client's from the client's
  process. `KARVI_ACTIVITY_ID` is gone, and the JSON follows: `job_id` or
  `login_id` in place of `activity_id`, `device.canonical_name` in every
  event (a login's `device.name` renamed), and `loginuser`. The message is
  one line of `key=value` pairs, `karvi.audit=<kind>.<event>`, `operator`,
  `loginuser` when it differs, `outcome`, `job` or `login`, `device`, and
  `command="…"`, quoted always, in place of `karvi audit EVENT
  outcome=OUTCOME` ([`docs/OPERATIONS.md`, "The audit
  trail"](docs/OPERATIONS.md#the-audit-trail)). A site's parser of the audit
  file, the journal, or the syslog it forwards changes with it.
- **Breaking: the audit records the statements, not their digests.** A
  `command` event carries its statement as sent, `action.command` and the
  line's `command="…"`; a job's `start` carries its statements in order,
  `action.commands`, which its `cancel` and `end` do not repeat. A
  session-init statement has its own `command` event, `action.command_kind`
  `session_init`, and stays off the `start`'s list. `action.command_sha256`,
  `action.command_sha256_array`, `action.selector_digest`,
  `action.plan_digest`, `policy.config_digest`, and the empty command lists
  of a login's and `crun.after`'s events are gone. An `--expect` response is
  never in the audit.
- **The mask: `masks` and `crun.mask`; execution plan 14, registry 31.**
  `[platform.NAME] masks` is a platform's mask list, regular expressions in
  Go's syntax: in a statement or a line, the text of every capturing group of
  every match is replaced by `<removed>`, text two groups cover once, a group
  that matched nothing left alone. `cisco_iosxe` (19 patterns), `cisco_iosxr`
  (15), `cisco_nxos` (13), `arista_eos` (13), and `juniper_junos` (10) have
  built-in lists, in force by default; a site's array replaces a list whole,
  `[]` turns it off, and a pattern that does not compile or holds no group
  refuses the configuration, `config_platform_mask_invalid` (exit 2). Every
  statement the audit records is masked, in journald and `audit.file` alike,
  by the lists of all the job's platforms together; the job's folder keeps
  the statements as sent. `crun.mask` (`false` by default) masks every
  collection file by its device's own platform's list, a `crun`'s and a
  `run`'s or `command`'s given `--cd`, in each line kept and each `! COMMAND`
  marker's statement; `--set crun.mask=false` keeps one run's files whole
  unless the site locks the key. The plan carries each target platform's list
  as `platform_masks` ([`docs/COLLECTION.md` section
  6.1](docs/COLLECTION.md#61-the-secrets-the-mask)).
- **`karvi-audit`, the audit trail as a table.** A fourth executable beside
  `karvi`, `karvi-prune`, and `karvi-askpass`, in the package
  (`/usr/bin/karvi-audit`, `man karvi-audit`) and the bundle (`install.sh`
  links it from `/usr/local/bin`). It runs `journalctl` with the trail's
  identifier and message ID and shows each event as one row: JOB-ID,
  OPERATOR (`OPERATOR/LOGINUSER` under `sudo`), MODE, EVENT, OUTCOME, DEVICE,
  and COMMAND at fixed widths, the line cut with `…` at a terminal, nothing
  cut to a pipe or a file. `-S`, `-U`, `-n`, `-g`, `-r`, and `-f` are
  journalctl's, passed as given; `-o NAME` is the operator or the login user,
  `-d NAME` the device, each repeatable. OUTCOME is green, orange, or red by
  its word; the colour goes to a terminal and through a pipe, never into a
  file (`--nocolor`, `NO_COLOR`). Nothing matched is `karvi-audit: no
  matches` on standard error, exit 0; a value journalctl refuses is its own
  message, exit 1; a usage error exits 2; an event of another audit schema has
  no row, and one line counts them. It reads no configuration. Tab completes
  its flags from the same file as karvi's; a host that holds the file from
  before runs `sudo karvi setup tab` once more ([`docs/OPERATIONS.md`, "The
  audit trail"](docs/OPERATIONS.md#the-audit-trail)).
- **The audit suite.** `scripts/audit-smoke-test.sh` reads the jobs it makes
  against the fake device back from the host's journal with `karvi-audit`,
  each row checked against the event `audit.file` holds, in the suites' third
  lane; on a host without journald its journal rows are skipped with the
  reason.
- **Breaking: an outcome or a status is one present-tense word in every
  record; command record 4, job manifest and summary 5, scoreboard 4, plan
  report 3.** `succeeded` is `success`, `completed` `complete`, `errored`
  `error`, `failed` `fail`, `cancelled` `cancel`, `halted` `halt`, and
  `exercised` `exercise`, in the command record's `status`, the summary's
  `final_status` and `devices`, the scoreboard's `status` and target states,
  `job list` and `watch`, a login's status, and the audit's outcomes. The
  counts named after outcomes take their words, `device_counts` and the
  scoreboard's `counts`: `done` (the devices at an end, `job list`'s DONE,
  was `completed`), `success`, `fail`, and `cancel`; `total`, `not_started`,
  `incomplete`, and `in_flight` stay. The ping gate's counts in the summary's
  `ping.devices` are `proceed`, `proceed_degraded`, `skip`, and
  `capability_unavailable` (`gated` stays), and its display line ends
  `proceed` or `skip`; the plan report's `outcome` is `plan` or `exercise`.
  The error codes (`cancelled`, every `*_failed`), the halt reasons drawn
  from them, `unfinished`, and the compound causes
  (`not_attempted_prior_command_failure`) stay, so a cancelled record reads
  `status: cancel` beside `error.code: cancelled`. The scoreboard's schema
  file names its version 4 and declares the targets' `bytes` and the
  metrics' `in_flight_bytes`, which it left out. A record's `<status>` in the
  display takes the success role for `success` and `complete`, where a
  `succeeded` record showed in the error role.
- **Breaking: the collection's `replace` and `keep`.** The summary's and the
  scoreboard's `collection` counts and each device's outcome, and
  `crun.after`'s details, say `replace` and `keep`; the display placeholders
  are `<replace>` and `<keep>`, `display.collection.footer`'s default `!
  collection=<collection> replace=<replace> keep=<keep>`, and a template
  naming `<replaced>` or `<kept>` is refused,
  `display_placeholder_unsupported` (exit 2). The hook's environment is
  `KARVI_CRUN_REPLACE` and `KARVI_CRUN_KEEP`, in place of
  `KARVI_CRUN_REPLACED` and `KARVI_CRUN_KEPT`; the shipped examples in
  `/usr/share/karvi/crun/` follow, and a site's copy of one changes with it.

## 0.31.1 - 2026-10-16

A patch release on 0.31.0 for the `crun.after` hook's bound, which changes no
record, file, key, or schema; every counter is 0.31.0's (configuration schema
6, registry 30, execution plan 13, daemon IPC 12, command record 3, job
manifest and summary 4, plan report 2, credential package 2, scoreboard 3,
audit 1, metrics 1, inventory 1), the module graph is 0.31.0's, and a site has
nothing to change. An operator's 0.31.0 daemon, of another build, is drained by
the first submission after the upgrade.

- **A `crun.after` hook's process group is gone when `crun` returns.** At
  `crun.after-timeout`, or when the run is interrupted, the hook's process
  group is sent SIGTERM and, five seconds later, SIGKILL to what is left of
  it, and `crun` returns once the group is gone, giving up five seconds after
  the SIGKILL on a process in uninterruptible sleep. The SIGKILL reached the
  hook's shell alone, so a child that ignored SIGTERM outlived `crun`, where
  [`docs/COLLECTION.md`](docs/COLLECTION.md) and `crun.after-timeout`'s
  documentation said the group was killed. A hook that exits by itself is not
  followed: what it leaves in its group is its own.
- **The parity suite's hook check looks for the hook's own process group.**
  `scripts/native-smoke-test.sh`'s S33 matched `sleep 20` across the host after
  the hook's bound and ended every match, so another's process failed it, and
  was ended; it failed the 0.31.0 bundle's verifier once. The hook records its
  process group, the check looks for that group alone, and the hook's child
  ignores SIGTERM, so the suite proves the kill.

## 0.31.0 - 2026-10-16

A minor release on 0.30.1. The configuration registry moves from 29 to 30 and
the job manifest and summary from 3 to 4; every other counter is 0.30.1's
(configuration schema 6, execution plan 13, daemon IPC 12, command record 3,
plan report 2, credential package 2, scoreboard 3, audit 1, metrics 1,
inventory 1), and an operator's 0.30.1 daemon, of another build, is drained by
the first submission after the upgrade. A site has something to change where
its configuration names `output.files.summary-json` (refused at load), where a
script took a job folder's `summary.json` for the job's end (a job has ended
when its summary names a `final_status`), and where a script typed `--pl NAME`
for `--platform NAME` (`--pl` is `--platform linux`). The module graph is
0.30.1's.

- **Breaking: a job's summary is written at its acceptance and at its end;
  job manifest and summary schema 4.** `summary.json` is written when the job
  is accepted, after its manifest, naming the job, its plan, its kind, the
  files it writes, and its devices, with none of the result fields, and
  written whole again at the job's end. Its presence no longer says the job
  has ended: a job has ended when its summary names a `final_status`, which
  `job follow` and `karvi-prune` read. Two fields are new: `devices`, a list
  of `{name, outcome}` in the plan's order, as the scoreboard's `targets`, the
  outcome the `device_counts` category the device is counted in (an
  exercise's `ready` or `not_ready`) and absent at acceptance; and `kind`,
  `cmd`, `run`, or `crun`, a job of `stream` being `run` and an exercise
  keeping its kind, beside `mode` (`live` or `exercise`). The manifest moves
  to 4 with the summary, its content unchanged. `karvi-prune` takes a folder
  whose summary names no final status as it took a folder without one, an
  orphan retired by age alone. The daemon IPC schema stays 12.
- **Breaking: `output.files.summary-json` is removed; every job folder holds
  its summary.** A configuration naming the key is refused at load,
  `config_unknown_key` (exit 2), and the configuration registry moves from 29
  to 30. The seven other `output.files.*` switches false leave a folder
  holding its summary alone; a job writes no folder only under `--nof` or
  `output.persist-command = false`.
- **`karvi job list`.** The jobs of the output root from their summaries,
  needing no daemon or credential and making nothing: in shared mode every
  job of the shared tree, every operator's and a service account's; in
  individual mode the operator's own. With no option it lists the newest 20
  jobs by job ID, whatever their day. `--last N` (0 every job kept),
  `--failed` (the finished jobs whose exit is not 0), `--target NAME` (`run`'s
  rule over each job's devices: a glob, repeatable, a leading `!` removing;
  `!` values alone are refused, `inventory_positive_selector_missing`, exit
  5), and `--operator NAME` (repeatable) each judge a job before the count.
  The table is `watch`'s without ACTV: JOB-ID, TIME (the job's duration),
  STATUS, OPERATOR (the summary's owner), MODE (the kind), DONE, FAIL, and
  TARGET, the line fitted to the terminal, each device named whole while it
  fits and the rest counted as `+N`, nothing cut to a pipe or a file; under
  `--target` one row per matching device, OUTCOME the device's own. An
  unfinished job shows `unfinished`, TIME blank, and `u` in DONE and FAIL, in
  the warning colour; a folder or a day folder that cannot be read is a `?`
  row, `invalid`; a list with no row prints `(no jobs)`, exit 0. `--format
  json` prints an array of the rows, each the summary's fields with `path` and
  `operator` (`{username, uid}`) ([`docs/OPERATIONS.md`, "The list of
  jobs"](docs/OPERATIONS.md#the-list-of-jobs)).
- **`job follow` follows any job from its folder, across the shared tree's
  members.** A job no daemon of the caller's holds, finished or live (a `run
  --no-daemon` of another terminal, another member's, a service account's),
  is followed from its folder: each `commands.jsonl` record as it is written,
  then the display's end once its summary names a final status, where only a
  finished job was read from its folder. In the shared jobs tree a file
  another member owns is read when its group is the tree's and its mode 0640
  or stricter; one of another group is refused `run_output_records_unreadable`
  (exit 111), its group over the tree's. While the summary is not final, the
  job's scoreboard decides: one that is gone, stale by `watch.stale-after`, or
  naming the job's end that long ago is `job_orphaned` (exit 112), where every
  job without a summary that no daemon held was; under `watch.enabled = false`
  another member's live job is `job_orphaned` at once. A job that keeps no
  `commands.jsonl` is followed to its end with one warning, `the job's records
  are not kept (output.files.commands-jsonl is false)`. A finished job
  followed from its folder prints the `! ssh accepted new host-key …` line, as
  the daemon's follow does. A job whose day folder cannot be read is
  `run_output_records_unreadable` (exit 111), where it was `job_unknown`
  (112).
- **`watch --all-retained` is gone.** The option, specified and refused as
  not available (`cli_option_unavailable`), is now unknown
  (`cli_option_unknown`); `job list --last 0` lists every job kept.
- **The job list suite.** `scripts/job-list-smoke-test.sh` runs `job list` on
  the jobs of a lab output root made against the fake device (a command, a
  run with an unreachable device, a `crun`, an exercise, a killed run, an
  empty folder, and folders that cannot be read), in the suites' third lane.
- **`--pl` is `--platform linux`, `--ps` is `--platform linux_shell`.**
  `login`, `command`, and `run` take them beside `--pi`, `--pn`, `--pr`,
  `--pj`, `--pa`, and `--pg`. `--pl` is a whole word, so it is no longer a
  prefix of `--platform`: a script that typed `--pl NAME` for
  `--platform NAME` now gets `linux` and `NAME` as a target or command word
  (`--pla` is still `--platform`).
- **Every HTML page links the repository.** The left column of each page of
  `make html`, the index too, links
  <https://github.com/robert-patrick-texas/karvi> under the name.
- **Tab completion for a shorter name.** `docs/OPERATIONS.md` "Tab
  completion" and `karvi-setup(1)` FILES say how a site that links a shorter
  name such as `k` to karvi binds it to the same completion
  (`complete -F _karvi k`, in a file of its own); nothing in karvi changed.

## 0.30.1 - 2026-10-13

A patch release on 0.30.0 for the Go toolchain, which changes no record, file,
key, or schema; every counter is 0.30.0's (configuration schema 6, registry 29,
execution plan 13, daemon IPC 12, command record 3, job manifest and summary 3,
plan report 2, credential package 2, scoreboard 3, audit 1, metrics 1,
inventory 1), the module graph is 0.30.0's, and a site has nothing to change.

- **Go 1.27.2.** The executables are built with Go 1.27.2, where 0.30.0's
  were built with Go 1.27.1; no code changed. Go 1.27.2 carries security fixes
  to `crypto/tls`, `net/http`, `net/textproto`, and `os`, which `karvi` links
  (`karvi-askpass` and `karvi-prune` link `os` alone), and to `html/template`
  and the `go` command, which no executable links. The rebuilt executables are
  another build: an operator's daemon of the previous build is drained by the
  first submission after the upgrade, and a service account's daemon takes its
  unit's restart ([`docs/OPERATIONS.md`,
  "Upgrade"](docs/OPERATIONS.md#upgrade)).

## 0.30.0 - 2026-10-13

A minor release on 0.29.0. The configuration registry moves from 28 to 29, the
execution plan from 12 to 13, and the daemon IPC schema from 11 to 12; every
other counter is 0.29.0's (configuration schema 6, command record 3, job
manifest and summary 3, plan report 2, credential package 2, scoreboard 3,
audit 1, metrics 1, inventory 1). A daemon serves only a client of its own
build, and a client that launches its own daemon drains one of another build
and starts its own; a service account's daemon runs under the package's system
template, set up by `karvi-sysutil`. A site has something to change where its
configuration names `daemon.socket` (now the directory `daemon.sockets`),
`name.default-transform`, `env-indirection`, the `sqlite` backend type, a key
its credential backend's type does not read, or a transform table that does
not build (each refused at load); where a credential template holds `%s` (now
text; `%operator%` is the operator's name) or a CSV cell or a Redis or Vault
field begins with `env:` (now a variable's value); where a Redis or Vault
`ca-file` was meant to add to the host's trust (it is the whole trust); where a
script reads `daemon status`'s `version:` or `ipc_schema_version`, `karvi
version`'s `build_time`, or colour escapes from a file; where it used the
packaged user unit (removed) or installed the bundle by hand (`sudo
./scripts/install.sh` now); and where it relied on a daemon's stop grace of 120
seconds or its idle hour (`daemon.shutdown-grace-seconds` 60 and
`daemon.shutdown-idle-timer` `10m` now). The module graph is 0.29.0's,
`golang.org/x/sys` now a direct requirement.

- **Breaking: a daemon serves its own build; a client drains another's.** A
  daemon serves a client only when its IPC schema and its build, the last part
  of the Go build ID its executable carries, equal the client's, where the
  version and the IPC schema were compared and every release needed `karvi
  daemon restart`. Every submission (`run`, `crun`, `--exercise`, `--detach`,
  a stream's job) and `daemon start` drains a daemon of another build at its
  own schema: holding no job, it exits and the client starts one of its own
  build in the same invocation (`daemon drained version=… build=…`, then
  `daemon started`); holding jobs, it finishes them and exits after the last,
  each submission meanwhile refused `daemon_draining` (exit 112), the daemon's
  build over the client's. A daemon of a newer schema is refused
  `daemon_incompatible`, the schemas stacked. The dry run drains nothing: an
  idle daemon of another build is the info finding `daemon_other_build`, a
  busy one the error `daemon_draining`. A process that cannot read its own
  build stops, `build_unreadable`: `daemon serve` refuses to start, and a
  client to submit through a daemon. An interrupt while the client waits for a
  drained daemon to go or a launched one to answer is `cancelled` (exit 113),
  nothing submitted, where it was `daemon_start_failed` (112).
- **Breaking: `daemon status` stacks the daemon's values over the client's.**
  `daemon_version:` over `client_version:`, `daemon_commit:` over
  `client_commit:`, `daemon_ipc_schema:` over `client_ipc_schema:`, and
  `daemon_build:` over `client_build:`, above `compatible:`, each pair coloured
  by whether its values agree; `version:` is now `daemon_version:`, and the
  JSON takes the same keys, where it had `version` and `ipc_schema_version`.
- **Breaking: IPC schema 12 and execution plan 13.** A request carries no
  `client_version` or `client_pid`, and the job header's `client` holds the
  client's pid and hostname alone: the daemon checks nothing of the client's.
  The prepare answer's `daemon` block names its `build` and `commit`, and a
  client abandons a preparation of another build's daemon before any
  credential is sent and runs its sequence once more; a second such answer is
  refused `daemon_incompatible`.
- **Breaking: a drained daemon exits after its last job, and every stop names
  its mode.** A drained daemon admits no job, finishes those it holds with no
  time limit, and exits after the last, at once with none (`stopping:
  drained`, exit 0). `daemon stop --grace` drains and waits for the daemon to
  exit, and `--after=DURATION` drains, waits up to the limit, and forces; both
  drain any daemon, at its own schema, where `--grace` did not drain a daemon
  of another version. A wait begins with `daemon draining: N jobs running; …`,
  which `--quiet` leaves out, and an interrupt ends the wait alone, the daemon
  finishing its jobs (`daemon_stop_wait_interrupted`). Without an option a stop
  is `if_idle`, which the daemon, counting its own jobs, refuses while any
  runs; a stop naming no mode is taken as `if_idle`.
- **Breaking: `daemon.shutdown-grace-seconds` defaults to 60**, where it was
  120, under the 90 s in which systemd stops a login session's scope.
- **Breaking: the service account's daemon under a system template; the user
  daemon unit removed.** The package installs the template
  `/usr/lib/systemd/system/karvi-daemon@.service` (`User=%i`, after the
  network, `KillMode=mixed`, `TimeoutStopSec=infinity`, `Restart=on-failure`,
  the idle timer off, no file-system sandbox) and `/usr/sbin/karvi-sysutil`
  with `karvi-sysutil(8)`, reloads systemd at its install, upgrade, and
  removal, enables nothing, and depends on `adduser`.
  `/usr/share/karvi/systemd/user/karvi-daemon.service` is removed. `sudo
  karvi-sysutil daemon install [--group NAME]` makes the system user `karvi`
  with its home `/opt/karvi/users/karvi`, adds it to the operators' group,
  writes its configuration with `daemon.launch = "systemd"`, and enables
  `karvi-daemon@karvi.service`, each part entered in the record
  `/etc/karvi/service-karvi.installed`; run again it repairs what has drifted.
  `daemon remove` undoes what the record lists, keeping the account and its
  home, and `--purge` gives the account's files in the shared trees to root
  and removes the account. After an upgrade or a rollback the administrator
  runs `sudo systemctl restart karvi-daemon@karvi.service`. The template's
  `ExecStart` names `karvi` bare, which systemd finds in `/usr/local/bin`
  before `/usr/bin`, and `karvi-sysutil` runs the same.
- **Breaking: the bundle installs itself, `sudo ./scripts/install.sh`.** In
  place of BUILD-HOWTO section 10's commands by hand: the four executables
  under versioned names in `/usr/local/lib/karvi`, `karvi`, `karvi-askpass`,
  and `karvi-prune` linked from `/usr/local/bin` and `karvi-sysutil` from
  `/usr/local/sbin`; `karvi-sysutil(8)` in `/usr/local/share/man/man8`; the
  service account's template in `/usr/local/lib/systemd/system`; and systemd
  reloaded, nothing enabled or started. An earlier bundle's `install.sh` rolls
  back. A site that chooses the service account's daemon runs `sudo
  karvi-sysutil daemon install` under either install.
- **`daemon.launch`, who starts the account's daemon.** `"client"`, the
  default, the client launching its own and draining one of another build; or
  `"systemd"`, the daemon the unit `karvi-daemon@USER.service`'s, which no
  client drains or starts: a daemon of another build or a newer schema refuses
  a submission and `daemon start` naming `systemctl restart` of the unit, none
  refuses them `daemon_unreachable` naming `systemctl start`, and `daemon stop`
  and `daemon restart` are refused with the new code `daemon_managed` (exit
  112).
- **Breaking: a build names its build and its commit, and no build time.**
  `karvi version` gains `build:` after `commit:`, and with it the manifest's
  `app.build`; `build_time` is removed from both, text and JSON. `make build`
  takes its commit from `COMMIT`, else `git describe --always --dirty` in a git
  checkout's root, else `development`, and `BUILD_TIME` is gone: `make build
  COMMIT=source-release-v$(cat VERSION)` reproduces a release's bytes, and
  `make html`'s footers name the same commit. `scripts/verify-shipped.sh` and
  `scripts/verify-release.sh` check that `karvi version` names a build, and
  `verify-release.sh` tests the package in an Ubuntu container under Docker
  (`scripts/sysutil-smoke-test.sh`), which it now needs.
- **`job follow` and `job cancel` across builds.** They use a daemon of any
  version or build that speaks the client's IPC schema, so after an upgrade
  the jobs a draining daemon finishes are followed and cancelled one at a
  time; a daemon of another schema refuses them, `daemon_incompatible`, naming
  the karvi that speaks it. A job a running daemon holds is no longer reported
  `job_orphaned`: the daemon is judged before the job's directory.
- **A daemon idle ten minutes stops itself.** `daemon.shutdown-idle-timer`
  defaults to `10m`, where it was `1h`: after an upgrade or a downgrade an
  operator's idle daemon of the old build is gone within some ten minutes of
  its last job, and the next run launches the installed karvi (some 80 ms).
  The range, `0` for never, and the template unit's `0` are unchanged.
- **Breaking: `daemon.socket` replaced by the socket directory
  `daemon.sockets`.** The daemon binds `d.sock` and the credential channel's
  `c.sock`, until now `daemon.sock` and `credentials.sock`, in
  `daemon.sockets`, whose `auto` is `<basedir>/socket` as before; two
  `basedir`s naming one directory reach one daemon. The directory may be at
  most 100 bytes, so that each socket's path fits 107 (under `auto`, a
  `basedir` of at most 93): a longer one is refused with
  `daemon_socket_too_long` (exit 2) wherever the daemon is reached or started,
  before anything is made, where a run waited out `daemon.start-timeout` and
  exited 112 with the cause in `daemon.log` alone. A file, an environment
  variable, or `--set` naming `daemon.socket` is refused with
  `config_key_removed`. The lifecycle replay gives the older daemon
  `daemon.socket` at the new client's `d.sock`. The configuration registry
  moves from 28 to 29.
- **One example configuration.** `configs/example.toml` is removed, and with
  it `/usr/share/doc/karvi/configs/example.toml` from the package;
  `examples/config.toml` is the one example. It takes the other's content
  where a site would use it: platform resolution and the collection run,
  active at karvi's defaults, and, commented, a shared `.cloginrc`, a drop
  list, start statements for generic devices, session-init, algorithm
  profiles, and an audit file without journald. Its effective configuration is
  unchanged. `reference.toml`, `config generate`'s starter, and the guides
  name it.
- **Every invocation says its configuration's warnings.** A load's warnings
  (an `@include?` file missing, a `KARVI__` variable ignored) are said once on
  standard error by every command that loads a configuration, `config show`,
  `config validate`, `login`, `run --dry-run`, `job`, `daemon`, and `watch`
  among them, where a `run`, `command`, or `crun` alone said them and a
  `stream` said them once per job; `--quiet` and the machine formats keep
  them. `config validate`'s report is unchanged. A recorded login says them
  once, and `daemon serve` logs its own as `level=WARN msg="configuration
  warning"` in `daemon.log`.
- **A configuration load about thirteen times cheaper.** The configuration
  registry finds a key through an index built once, where each lookup copied
  the whole registry and a load looks up each key at least twice: one load
  measured 11.8 ms and 16.5 MB of allocation, now 0.9 ms and 300 KB, and
  `config show` takes about a third of the CPU it took.
- **One read of the configuration per invocation.** An invocation reads its
  configuration once, its files, its environment, and its options together,
  where a run through a daemon loaded it three times and `crun` and `run --tf`
  four: a `crun`'s hook is the one its job's configuration named, its audit
  event under the job's digest, where an edit made during the job ran another
  hook. A `stream` reads as it starts and runs every job under that reading with
  the job's own option lines, so an edit reaches the next stream; a
  configuration that does not load refuses the stream once, before a line is
  read, where each `--go` was refused. A mistake on the command line is said
  before one in the configuration, with `--tf` as without; `job follow` and `job
  cancel` refuse a malformed ID with `job_request_malformed` (exit 112), a
  daemon running or not, where `job cancel` said `daemon_unreachable` without
  one. `run --dry-run`'s configuration failure loses its `client planning:`
  prefix, and its report's `client_config_ns` is the read's time, `total_ns`
  running from it. A recorded login reads twice, in its wrapper and in its
  child, where it read four times.
- **Every warning line in the warning colour.** A `warning: …` line on standard
  error takes the `warning` role's colour, the whole line and bold as the `!`
  lines, when colour is on by `display.color` and `NO_COLOR`, and stays plain on
  a pipe or in a file; the words are unchanged. A recorded login's lines at its
  wrapper's terminal take that terminal's colour, its `!` host-key lines among
  them; the transcript is unchanged.
- **A stream's `--restart`.** A `stream` directive that empties the draft, its
  commands and its targets, and reads the configuration again as the stream's
  start does, under the stream's command-line options and `--set`, its warnings
  said again and, at a terminal, the opening line; the jobs after it run under
  the new reading, so an edit is taken without leaving the stream. A reading
  that fails ends the stream with its code and exit, 2, or 3 for a lock
  violation, after `stream line N:`. The line count, the last exit, and the
  recalled lines go on through it.
- **A type or enum error said once.** A configuration value of the wrong type,
  or outside its key's values, says its fault once, `config_type_error: must be
  an integer for dispatch.parallel-workers at FILE:2`, where the text named the
  key twice and repeated itself after the source. The codes and exits are
  unchanged.
- **The transports' own SSH algorithm lists.** `ssh-algorithms.source =
  "transport"` gives each transport its own key exchanges, ciphers, and MACs:
  karvi writes none of those lines into the generated OpenSSH configuration,
  so OpenSSH takes its compiled defaults or an included `~/.ssh/config`'s, and
  leaves x/crypto's unset; the names karvi refuses or allows only in a profile
  come with them (OpenSSH's `umac-64`, x/crypto's `hmac-sha1-96`, AES-128).
  The host-key list stays `ssh-algorithms.host-key`, narrowed to the stored key
  types as before. `karvi` (alias `auto`) is the default and karvi's lists. An
  `ssh-algorithms-profile` may set its own `source`; one that sets a key
  exchange, cipher, or MAC list while its devices take the transport's is
  refused with `config_ssh_algorithms_profile_lists_unread` (exit 2), and one
  setting only `source` is no longer `config_ssh_algorithms_profile_empty`. The
  device's debug line says `source=`, and a negotiation failure under the
  setting names `native offered …` or `system offered …` where it said `karvi
  offered …`, the system transport's list read by `ssh -G` at most once per job.
  The configuration registry stays at 29.
- **An alias stored as its value.** `dispatch.order = "name"` is stored as
  `sorted`, so `config show` says `sorted` and the configuration digest is
  `sorted`'s, where the two spellings gave two digests;
  `ssh-algorithms.source`'s `auto` is stored as `karvi` alike.
- **Breaking: an explicit `env:NAME` cell in the credential CSV.** A `username`,
  `password`, or `enable_password` cell `env:NAME`, the cell's surrounding
  spaces and the prefix's case ignored, takes the variable's value, with no
  flag; an unset or empty variable refuses the run at planning with
  `credential_env_missing` (exit 6), naming the file, the line, the column, and
  the variable, before any device is reached, where under `env-indirection` an
  unset variable's name was sent as the password. A name that is no variable
  name is `credential_csv_row_invalid` when the file loads.
  `env-indirection.username`, `.password`, and `.enable-password` are removed
  from every backend (below): a plain cell is a literal whatever variables are
  set, and a literal password that begins with `env:` is given through a
  variable. The cell reads the resolver's one lookup, its input
  provider's, which the `NET*` fallback reads too: `credentials.ResolveRequest`
  carries it as `LookupEnv`, and `credentials.FieldSource` and
  `Credential.FieldSources`, which nothing read, are removed.
- **Breaking: a credential backend takes its type's keys alone.** A key its type
  does not read is refused at load, `config_credential_backend_key_unsupported`
  (exit 2), naming the types that read it, where a Redis backend took Vault's
  `mount`, or a CSV `token-env`, and read neither;
  `config_credential_backend_scope_unsupported` is retired into it.
- **Breaking: the `sqlite` credential backend type removed.** It had no driver,
  and a run reaching it failed with `sqlite_backend_unavailable`, now retired;
  `type = "sqlite"` is refused as an unsupported type, and its keys `table`,
  `operator-column`, `username-column`, `password-column`, `enable-column`, and
  `busy-timeout` with `config_key_removed`.
- **Breaking: transforms checked at load.** `config validate` builds every
  `[credential-transform.NAME]` and `[name-transform.NAME]` table with the
  builder the run uses, a table nothing names included, and refuses a list that
  does not build under the code a run gave it, naming the list and its line
  (`transform_operation_unknown`, `transform_suffix_empty`, an operation that is
  not an inline table); a credential backend's `transform` naming no table is
  `credential_transform_unknown`, and an inventory source's `name-transform`
  naming none `name_transform_profile_unknown`, exit 2, where such a
  configuration validated and every run refused it at planning (an inventory
  source's with exit 5). A credential transform's `operator` or `username` that
  is not an array, which a run read as no operation, is refused with
  `credential_transform_operations_not_array`.
- **Breaking: `name.default-transform` removed.** It was read by nothing: a
  device whose row and source name no name transform takes
  `[name-transform.default]`, whatever the key said. It is refused with
  `config_key_removed` from a file, `KARVI__NAME__DEFAULT_TRANSFORM`, and
  `--set`; define `[name-transform.default]` to change the default.
- **A credential helper.** `creds.helper` names an executable, by default
  `karvi-credh` beside the karvi executable, that an invocation resolving
  credentials (`login`, `command`, `run` and its dry run, `crun`) runs once
  before any backend is asked, and a `stream` at its first job, holding its
  values until `--restart`. Its standard output, `export NAME="value"` lines
  read by a parser and never by a shell, sets values for karvi's credential
  readers alone, ahead of the process environment: the `NET*` fallback, an
  `env:` cell or field, an env backend's templates, Redis's login variables,
  and Vault's token. It runs with karvi's environment, standard input
  `/dev/null`, and its standard error discarded, after a credential file's
  checks (its owner, no write by group or others, no symlink unless allowed).
  `creds.helper-unavailable` (`skip`, `warn`, or `fail`) says what a helper
  that cannot be run does, and one that fails always refuses the run:
  `credential_helper_unavailable`, `credential_helper_failed`, and
  `credential_helper_output_invalid` (exit 6). `creds.helper-timeout` (`60s`)
  ends it and the processes it started. `--no-credh` on `login`, `command`,
  `run`, `crun`, and `stream` skips it for the invocation.
  `examples/karvi-credh` is a POSIX `sh` helper over `pass show`; the package
  installs none.
  `golang.org/x/sys` becomes a direct requirement. The configuration registry
  stays at 29.
- **Breaking: `%operator%` in a credential template, and `%s` is text.**
  `%operator%` is a built-in configuration macro, the operator's login name,
  filled in every value and kept for the backend in the env backend's three
  templates, Redis's `key-template`, Vault's `path-template`, and a formula's
  and a policy's `username-template`, filled there with the name after the
  backend's transform; the defaults are `karvi:credential:%operator%` and
  `karvi/%operator%`. These templates were Go's `printf` over `%s`: `%s` is now
  text, so a Redis key or Vault path holding it finds no record, a formula
  sends it, and an env template holding it is refused at load (below).
  `[macros] operator` is refused with `config_macro_name_reserved` (exit 2).
- **Breaking: `env-indirection` removed; a Redis or Vault record names a
  variable as `env:NAME`.** `env-indirection.username`, `.password`, and
  `.enable-password` are refused on every backend with `config_key_removed`,
  its hint naming each type's way, where under a flag a field whose variable
  was unset was sent as the password.
  A Redis or Vault record's username, password, or enable field `env:NAME`
  takes the variable's value, an unset or empty one `credential_env_missing`
  (exit 6) naming the backend, the record, the field, and the variable; any
  other value is a literal. An env backend's variable's value is the value.
- **Breaking: every credential reader on the one lookup.** The env, Redis, and
  Vault backends read the environment through the resolver's lookup, where the
  helper's values are. A variable Redis's or Vault's configuration names
  (`auth-username-env`, `auth-password-env`, `token-env`), unset or empty, is
  `credential_env_missing` (exit 6) before any connection, `credential backend
  v: token-env: VAULT_TOKEN not set`, where Redis sent `AUTH` with an empty
  password; `redis_auth_missing` and `vault_token_missing` are retired.
- **Breaking: a store backend's keys checked at load.** The env, Redis, and
  Vault backends' keys have kinds (`tls` and `tls-verify` booleans, `database`
  an integer of 0 or more, `connect-timeout` a duration above zero); `address`
  is required and well formed, `config_credential_backend_address_invalid`,
  retiring `redis_address_missing` and `vault_address`; `token-env`,
  `auth-username-env`, `auth-password-env`, and the env templates are portable
  variable names, `config_credential_backend_variable_invalid`; and a key the
  backend's other keys leave unread is
  `config_credential_backend_key_unsupported` (`auth-username-env` alone, half
  a client pair, TLS keys without TLS, `ca-file` under `tls-verify = false`),
  retiring `redis_client_cert_key_unpaired` and
  `vault_client_cert_key_unpaired`. Each is exit 2, where these validated and
  fell to a default or a refusal at the run. `""` is none for a key whose
  default is none and refused where the default is a value. An env template
  that the operator's transformed name makes no variable name stops the device
  at planning, naming `replace-chars`.
- **Breaking: a store's `ca-file` is its whole trust.** A Redis or Vault
  backend's `ca-file`, when given, is the backend's whole trust, where it was
  added to the host's. A new key, `tls-verify` (`true`), verifies the server's
  certificate chain and name, and `false` checks neither. A client certificate
  file's further certificates are sent as its chain.
  [`docs/OPERATIONS.md`](docs/OPERATIONS.md) gains the Redis and Vault
  backends, how to trust a self-signed store, and the configuration's macros.
- **A store asked once.** An env, Redis, or Vault backend is asked once per
  invocation, or per job of a stream, its answer held for every device: forty
  devices made one read where they made forty, and a store that never answers
  costs one timeout. Vault's defaults, written by every device's resolution
  while devices resolved in parallel, are set once.
- **Redis's `matched_on.source` is the key read**, where it was the text
  `redis-key-digest-not-implemented`. `value_digest`, set by nothing, leaves
  the credential package's schema; the package counter stays at 2.
- **Refusals in colour, and `--nocolor`.** At a terminal a refusal, its code
  and message, is bold in the `error` role's colour, as a device's failure line
  is, and a line a stream drops and goes on from bold in the `warning` role's
  colour; a refusal before the configuration is read takes the built-in
  settings. `--nocolor`, a global option before the command word, sets
  `display.theme = "nocolor"` for the invocation.
- **Breaking: no colour in a file.** A regular file is written plain whatever
  `display.color` says, and `always` colours a terminal and a pipe, where it
  coloured a file too: a script that read escapes from a file reads them
  through a pipe.

## 0.29.0 - 2026-10-09

A minor release on 0.28.0. The configuration registry moves from 27 to 28, the
execution plan from 11 to 12, the daemon IPC schema from 10 to 11, the job
schema from 2 to 3, and the plan report from 1 to 2; every other counter is
0.28.0's (configuration schema 6, command record 3, credential package 2,
scoreboard 3, audit 1, metrics 1, inventory 1), and a running 0.28.0 daemon is
`compatible: false` by its version and its IPC schema and is restarted. A site
has something to change where its jobs relied on a daemon's own `--config`,
`--set`, environment, or working directory (each job now runs under its
client's), where it reads a job's audit records, the cancel record included,
from the sink the daemon's launcher named (they go where the job's client's
`audit.*` says), where its configuration names a path relative to a working
directory or under `~user` (a relative path resolves at the client's load and
`config show` prints it absolute; `~user` is refused), where it reads the
manifest's `policy` block or the readiness `policy_digest` (removed), and where
it copied `karvi-daemon.service.d/crun.conf.example` (removed, no longer
needed). The module graph is 0.28.0's.

- **A job runs under its client's configuration.** A daemon ran every job under
  the configuration it loaded at its start, its first launcher's `--config`,
  `--set`, `KARVI__*` variables, and working directory, so a later client's
  settings missed its own job without a word, the host-key policy among them.
  The client now sends its resolved values in the plan's `configuration`
  block and the daemon builds the job's configuration from it, refusing a
  block whose digest is not the plan's `sources.config_digest`
  (`execution_plan_invalid`); the daemon's own configuration serves its process
  alone (the `daemon.*` keys, its `basedir`, the sweeps at its start), and a
  site's locks are enforced by each client's load. A job's audit records go to
  the sinks its configuration names, `run.cancel_requested` included, and the
  daemon opens no audit sink of its own; the audit's `policy.config_digest`
  names the job's configuration, where it named the daemon's. The client of a
  daemon-backed run prints its load's warnings, which no daemon-backed run
  printed.
- **The plan carries no copy of a key; the schemas move.** Execution plan 12:
  the `configuration` block is added, and the `execution` and `ping` blocks,
  `blind_wait_ns`, `output`'s `max_command_bytes`, `max_job_bytes`, `persist`,
  `files`, and `crop_to_dot`, the collection's `file_mode`, and `dispatch`'s
  halt and gate values with `continue_device_on_error` are removed, the job
  reading those keys from its configuration. Daemon IPC 11 carries it. Job 3:
  the manifest's `policy` block is removed (the summary moves with it). Plan
  report 2: the daemon row's `execution_policy_digest` is removed, and
  `--exercise`'s daemon line no longer prints `policy_digest`. A running 0.28.0
  daemon is `compatible: false` and is restarted.
- **Path-valued keys are made absolute when the configuration loads.** The
  registry marks every key whose value is a path (`place`, with the key's
  `words` such as `auto` kept as written, and a dynamic table's `places`); `~`
  is the operator's home and a relative path is taken from the working directory
  at the load, so `config show` prints the path a job records and the digest
  covers it, the built-in `ssh.identities` included. `~user` fails the load; a
  relative path from a working directory that cannot be read is
  `config_working_directory_unavailable`, and `crun_directory_unavailable`,
  whose causes now fail at the load, is retired. Registry 28.
- **The reload class says which keys wait for a daemon restart.** `config show
  --explain` printed `reload: next-job` for every key;
  `daemon.max-accepted-jobs`, `daemon.shutdown-idle-timer`,
  `daemon.shutdown-grace-seconds`, and the daemon's side of
  `daemon.max-ipc-frame-bytes` and `daemon.forced-grace-seconds` are now
  `daemon-start`: a running daemon keeps the value it started with.
- **The user units carry no file-system sandbox.** `karvi-daemon.service`,
  `karvi-prune.service`, and `karvi-crun.service` under
  `/usr/share/karvi/systemd/user/` no longer set `PrivateTmp`,
  `ProtectSystem`, `ProtectHome`, or `ReadWritePaths`: a user manager on a host
  that restricts unprivileged user namespaces (Ubuntu's
  `kernel.apparmor_restrict_unprivileged_userns = 1`) started them unsandboxed
  without a word, and where the sandbox applied a `/tmp` path the client named
  landed in the daemon's private `/tmp`. Each runs with the operator's own view
  of the files, as the client does. The daemon's drop-in example
  (`karvi-daemon.service.d/crun.conf.example`) is removed; a copied drop-in
  is no longer needed. The site's `karvi-prune.service` keeps its sandbox.
- **`crun_directory_not_writable` names its cause.** The shape a shared
  collection directory needs (mode 2770 or 2775) is in the message only where it
  is the remedy, a present directory the operator cannot create files in and the
  sticky case, and no longer after a file in the way or a directory that cannot
  be made. A path whose folder above is a file names that file, where it named
  the path itself as the one that "exists and is not a folder"; the job and
  transcript trees' codes take the same naming.
- **The lifecycle replay keeps the older daemon under its base.**
  `scripts/daemon-lifecycle-replay.sh` gave the older release's daemon only
  `basedir` and `scoreboards`, taking it not to know `sharedroot`, `spooldir`,
  or `platform-resolution.default`; a 0.28.0 daemon knows them, so its start
  resolved `spooldir = auto` and made `/tmp/karvi-<uid>` on the host, and
  consulted the host's shared root. Both executables now take the same keys,
  every tree and the spool under the replay's base.

## 0.28.0 - 2026-10-08

A minor release on 0.27.0. The configuration registry moves from 26 to 27; the
daemon IPC schema and every other counter are 0.27.0's (execution plan 11,
command record 3, credential package 2, scoreboard 3, configuration schema 6,
job 2, audit 1, plan report 1), and a running 0.27.0 daemon is `compatible:
false` by its version alone and is restarted. A site has something to change
where it relied on `command` reaching OpenSSH by default (now `native`;
`ssh.command.transport = "system"` keeps OpenSSH), where it set
`logging.level`, `logging.file`, or `logging.file-required` (removed and
refused), where it installed `packaging/sysctl/90-karvi.conf` or used
`configs/ssh-legacy.conf` or `configs/ssh-ancient.conf` (removed), and where
its `tempdir` is longer than 69 bytes (refused); a build takes Go 1.27. The
release publishes a Debian package beside the bundle. The module graph is
0.27.0's.

- **Breaking: `command` runs over the native transport by default.**
  `ssh.command.transport = "default"` resolves to `native` (`scrapligo-v1`), as
  `run`'s does, where it resolved to `system`; `login` keeps `system`, which
  attaches the operator's terminal to OpenSSH. A site that wants OpenSSH for
  `command` sets `ssh.command.transport = "system"` or passes `--transport
  system`; an inventory row's transport, or its source's default, still comes
  first. The key and its default value are unchanged.
- **Breaking: `logging.level`, `logging.file`, and `logging.file-required`
  removed.** They were validated and read by nothing: a configured file was
  never written. A file, an environment variable, or `--set` naming one is
  refused with `config_key_removed`, and `config_logging_file_required_missing`
  is gone with them. The daemon's log is `<basedir>/logs/daemon.log`, the
  audit's journald and `audit.file`. The configuration registry moves from 26
  to 27.
- **The Debian package, built by `make deb`, carries the documents and the
  material a site installs.** Besides the executables and the manual pages,
  it installs the documents under `/usr/share/doc/karvi` (the tree's layout,
  uncompressed, so their links and the units' `Documentation=` paths hold) and
  the units and timers, cron scripts, `crun` hook examples, tmpfiles rule,
  reference configuration, and schemas under `/usr/share/karvi`, none of them
  active; the documents and the units name those paths, where they named the
  tree's `packaging/` or places nothing installed. `make deb` builds a copy of
  the tree into `dist/` with a changelog written from `VERSION` and
  `CHANGELOG.md`'s first heading, and carries `bin/`'s executables as they
  are: under a release's heading they must be the ones `CHECKSUMS.sha256`
  lists, and under `## Unreleased` the package is `VERSION+dev` and they must
  not be. The package names no Go in `Build-Depends`; BUILD-HOWTO offers it as
  the install. The example configurations are under
  `/usr/share/doc/karvi/configs/`, uncompressed, where the documents' and
  `config generate`'s `configs/example.toml` and `configs/reference.toml`
  resolve.
- **The release publishes the package, built from its bundle and checked.**
  `karvi_X.Y.Z_amd64.deb` and its `.sha256` are release assets beside the
  bundle, listed in the aggregate checksums, built from the verified bundle
  twice to the same bytes; BUILD-HOWTO §10 installs it by `apt install
  ./karvi_X.Y.Z_amd64.deb`, which brings `openssh-client` where `dpkg -i`
  refused without it. `scripts/check-deb.sh` checks a package against its tree
  (the executables, every document and example configuration under its own
  name, the units' documentation, `lintian` with the package's overrides), and
  the release verifier builds and checks the tree's package, so it needs
  `dpkg-dev`, `debhelper`, and `lintian`. On the development line `make deb`
  refuses only executables whose identity is the release's
  (`source-release-vVERSION`), where it had refused any build matching
  `CHECKSUMS.sha256`, which a dev build does after `make checksums`.
- **The tests pass at any `TMPDIR` length, and the verifiers fail when a run
  makes the host's scratch.** Five tests put the scratch, or a `basedir` whose
  `tmp` is `auto`'s candidate, under `TMPDIR`, which with the test's name passed
  the askpass socket's 69-byte bound under the release's 25-byte and 145-byte
  `TMPDIR` runs, so `auto` fell through to `/tmp/karvi-<uid>` and one of them
  made it on the host; each takes a short directory under `/tmp` from one
  helper, `internal/testdir`. Both verifiers now count `/tmp/karvi-<uid>` and
  `/var/tmp/karvi-<uid>` among the host's places a run must leave as they were.
- **`configs/ssh-legacy.conf` and `configs/ssh-ancient.conf` removed.** Two
  inert OpenSSH `Host` snippets left from `ssh.legacy-hosts`; karvi's own
  algorithm lists come before any operator's file, so they reached no
  session. A legacy device is reached by `[ssh-algorithms-profile.NAME]` and
  `[[ssh-algorithms-map]]`, shown in `configs/example.toml`.
- **Breaking: `packaging/sysctl/90-karvi.conf` removed.** Its one line,
  `fs.file-max = 2097152`, lowered the ceiling on current kernels, whose
  default is the maximum; karvi's bound is its per-process open-file limit,
  which it raises itself and the daemon's unit sets.
- **Go 1.27.** `go.mod` names `go 1.27.0`, and the build documents Go 1.27 or
  later.
- **Every session process ends with karvi.** The system transport's `ssh` on
  the shell channel, a login's `ssh`, and a recorded login's `script(1)` are
  started with `SIGTERM` as their parent-death signal, as the exec masters were.
  A karvi, client or daemon, killed outright had left them under init, holding
  the device's session: a router's vty until its exec-timeout, a server's shell
  for good, and a killed login's `ssh` competing with the shell for the
  terminal.
- **The scratch is swept.** The system transport's configuration, a
  recorded login's timing log, and the askpass socket are named by their
  maker's pid, `karvi-ssh-<pid>-*.conf`, `karvi-script-<pid>-*.timing`, and
  `askpass-<pid>-<16 hex>.sock`. One whose maker is not alive as karvi is
  removed at every admission, at the daemon's start, and at a login's start,
  logged `scratch_abandoned_removed`, where what a killed karvi left had
  stayed for good, on disk under `<basedir>/tmp` in individual mode. The sweep
  connects to no socket. Files under an earlier release's names are not
  touched.
- **The scratch fits the askpass socket.** A `tempdir` longer than 69 bytes
  is refused, `tempdir_too_long`, at every job's admission and a login's
  start, naming its length, where the job had been admitted and every device
  over the system transport failed with `askpass_start_failed: … bind: invalid
  argument`: a Unix socket's path holds 107 bytes and the askpass socket's
  name up to 37. A 70-byte directory had worked only while the pids had six
  digits. `tempdir = "auto"` passes a candidate too long by, as `<basedir>/tmp`
  under a long `basedir`, and takes the next; `config show --explain` names it
  on a `passed:` line.
- **A first contact is said on every path.** Under `ssh.host-key-policy =
  "accept-new"`, a session that stores a device's host key, unknown until
  then, says so on either transport: `! ssh accepted new host-key DEVICE
  (TYPE)` on the client's standard error, under `--quiet` too, in the
  display's warning colour, the device's name in the target colour; the
  device's first record carries the notice
  `host_key_enrolled` with the key type, and its `command_completed` audit
  event `details.host_key_enrolled`. `scrapligo-v1`'s warning `accepted and
  stored new SSH host key …` had been printed only by a job in the client and
  dropped by the daemon, and `system` said nothing: OpenSSH's `Permanently
  added` line is now taken from the standard error karvi reads. A job that
  finds the key stored meanwhile by another says nothing. A login, whose
  OpenSSH says nothing of it at its log level, reads the trust store at its
  first password prompt, or at its end when it asks none, says the same line,
  and names the key type in its `login.completed` or `login.errored` audit
  event's `details`.
- **`insecure` is said on every path.** A job under `ssh.host-key-policy =
  "insecure"` says so once at its admission, two lines on standard error,
  where both transports had warned at every connection; a key differing from
  the stored one is the notice `host_key_mismatch_accepted` on the device's
  first record with both keys' fingerprints, shown as `! ssh host-key mismatch
  DEVICE proceeding at risk` in the error colour and named in the audit's
  `details`; a `system` comparison that could not complete is
  `host_key_not_compared`, shown as `! ssh host-key DEVICE not compared:
  CAUSE`. Through the daemon all three warnings had been
  dropped, so a `run` under `insecure` accepted a changed key in silence.
- **A recorded login's own lines start at the first column.** Under `login
  --record`, karvi's lines on the terminal, the login's header, a warning, an
  error, and under `--debug` every `DEBUG` line, are written while `script(1)`
  holds the terminal raw, where a line feed alone keeps the column; each line
  began where the one before it ended, so the screen read as shifted by
  whitespace. Each line now ends in a carriage return and a line feed when the
  stream is a terminal; a redirected stderr keeps its bytes, and the transcript
  is unchanged.
- **`command_completed` names its process.** The audit event written for each
  device's record carried `process.pid` 0; it carries the id of the process
  that wrote it, the client's for a job run in the client and the daemon's for
  a daemon's job, as the job's other events do. Every audit line's
  `schema_version` is now set where the line is written, from the audit
  schema's version, where four writers each wrote a literal `1`.
- **`docs/SSH-TROUBLE.md`, a session that never reaches the prompt.** A guide
  for a `login` that hangs after `login interactive session starting`, or a
  `command` that ends in a prompt timeout: OpenSSH at `-vvv` under karvi through
  a wrapper named by `ssh.transports.system`, its trace's last lines read
  against where the session waits, what `--debug`, the footer, the generated
  configuration, and a recorded login's transcript show, `command` over both
  transports beside the login, and the device's side.

## 0.27.0 - 2026-10-06

A minor release on 0.26.0. The configuration registry moves from 24 to 26, the
execution plan from 10 to 11, the command record from 2 to 3, and the
credential package from 1 to 2; the daemon IPC schema and every other counter
are 0.26.0's (scoreboard 3, configuration schema 6, job 2, audit 1, plan report
1), and a running 0.26.0 daemon is `compatible: false` by its version alone and
is restarted. A site has something to change where it set `watch.directory`
(now the top-level `scoreboards`), `ssh.pubkey-authentication`, or a platform's
`control-master` (both removed; a backend row without a password is refused);
where it relied on `NETUSER` and `NETPASS` reaching a `linux` target (now the
platform's `fallback`), on a `linux` target's shell (now the exec channel;
`linux_shell` keeps it), on `audit.file`'s `~` as `$HOME`, on a relative trust
store under the home, or on an explicit place under an absent `/dev/shm/karvi`,
`/opt/karvi`, or `/var/lib/karvi`; where its operators' trust stores are under
the home on a host with `setup shared`'s roots (the store follows the private
root); and where it reads the command records (schema 3) or sends a stream-mode
line beginning with one dash. The module graph is 0.26.0's.

- **Breaking: `watch.directory` is now `scoreboards`, and both scratch folders
  default to `auto`.** The top-level `scoreboards` (`KARVI__SCOREBOARDS`) is
  where every activity writes its scoreboard: `auto` is
  `/dev/shm/karvi/scoreboards` where the site's scratch root exists (made in it
  with the root's bits when missing), else `<basedir>/state/scoreboards`; a
  shared folder present but not writable by the operator is passed by with its
  warning; a path is used or refused. A configuration that sets
  `watch.directory`, from a file, the environment, or `--set`, is refused at
  load (`config_key_removed`), the message naming `scoreboards`; `[watch]` keeps
  the screen's own settings. `sessions.shared-capacity-root` defaults to `auto`
  by the same shape (`/dev/shm/karvi/capacity`, else
  `<basedir>/state/capacity`), and the ledger passes by a root it cannot write.
  A `~` in either, which both had ignored for the private fallback, is the home.
  `karvi watch` makes nothing and reads every place that exists, the shared
  folder and the operator's own, one row per job: it had made the private root,
  and with the shared folder closed it did not show the operator's own jobs
  written to the fallback. The configuration registry moves to 26.
- **Breaking: one rule for `~` and a relative path.** In every place key and
  every file key, `~` and `~/…` are the operator's home from the password
  database, `~user` is refused (`path_other_user_home_unsupported`), and a
  relative path is taken from the invocation's working directory and made
  absolute. `audit.file`'s `~` had been `$HOME`; a relative
  `ssh.known-hosts-file` had been under the home and is now under the working
  directory; `daemon.socket`'s `~` and a relative path are made absolute, where
  they had made a folder named `~` or a socket in the working directory and the
  run exited 112 (`ipc_result_malformed`); an inventory source's or a credential
  file's `~user` is refused, where it had been taken as a name.
- **Breaking: no operator's run makes a place `setup shared` makes.** An
  explicit `tempdir`, `spooldir`, `ssh.control-path-root`, `scoreboards`, ledger
  root, trust store, `audit.file`, `daemon.socket`, or tree under an absent
  `/dev/shm/karvi`, `/opt/karvi`, or `/var/lib/karvi`, their `users` and
  `shared`, or a tree under `shared`, had made it, 0700 and the operator's,
  closed to every other until root repaired it. It is now refused before any
  device is contacted, `shared_directory_absent`, naming the place, `sudo karvi
  setup shared`, and the key to set elsewhere; under `auto` nothing changes. The
  trust store's path is checked at the job's admission, so a path refused there
  is the job's refusal, not each device's.
- **`karvi-prune` walks every private root of the operator's, making nothing.**
  An operator's run walks each private root that exists, the site's
  `/opt/karvi/users/<user>` and `/var/lib/karvi/users/<user>` and the home's
  `~/.local/share/karvi`, each one's `jobs`, `transcripts`, and
  `state/scoreboards`, with the shared trees and the shared scoreboards, so the
  jobs left under the home before the site made `users`, and the scoreboards
  that fell to a private folder, are pruned too; `--basedir PATH` replaces the
  private roots with that one. It no longer makes the private root, and it
  judges a shared tree by its permissions without writing a probe file there.
  Root's run adds each site root's `state/scoreboards`. The units' and the cron
  script's comments say so.
- **`setup shared` makes the ledger's `devices` folder, and the boot rule
  remakes it.** `/dev/shm/karvi/capacity/devices` is made at 2770, root's, in
  the operators' group, after `capacity`, and `/etc/tmpfiles.d/karvi.conf`
  (and `packaging/tmpfiles.d/karvi.conf`) names it, where the first activity
  after each boot had made it, owned by that operator, who could close it to
  the rest.
- **The trust store follows the private root.** Under `ssh.known-hosts-file =
  "auto"` the store is `<basedir>/known_hosts`: on a host where the site made
  the operators' roots (`sudo karvi setup shared`),
  `/opt/karvi/users/<user>/known_hosts`, where it had been
  `~/.local/share/karvi/known_hosts` whatever `basedir` was; on any other host
  the same file as before. A store an earlier release made in the home is not
  read on such a host: `accept-new` enrolls each device again, and `secure`
  needs its store in the new place. An explicit path is unchanged.
  `host_key_trust_store_candidates_exhausted` is retired; a store that cannot
  be made reports its own step's code.
- **`config show --explain` names every place.** Every key whose value is a
  place karvi writes or reads has `resolved: PATH`, the path the next activity
  in process would use, found by the activity's own chooser without creating
  anything (a candidate judged by `access(2)`, its owner and mode where it is
  private, and its filesystem's free inodes): `basedir`, the trust store,
  `output.root`, `transcript.root`, `crun.directory`, `tempdir`, `spooldir`,
  `ssh.control-path-root`, `scoreboards`, `sessions.shared-capacity-root`,
  `daemon.socket`, and, when set, `audit.file`, an inventory source's `path`,
  and a credential backend's `path`, `ca-file`, `client-cert-file`, and
  `client-key-file`. A candidate present on the host and passed by follows as
  `passed: PATH: reason`; a place the activity would refuse is `resolved: error:
  CODE: message`, and the view exits 0. `karvi config show KEY…` takes several
  keys, in their order, an unknown one `error: not found` among them, where a
  second key was `cli_positional_unexpected`. `docs/FILES.md` gives the recipe
  for every place at once; the host-key guide's enrollment recipe takes the
  store from the line. The lines come from the invocation's configuration: a
  running daemon keeps its scratch, spool, control sockets, scoreboards, and
  ledger until it is restarted.
- **`docs/FILES.md`, every place karvi uses.** One reference for the
  directories and files karvi reads and writes in shared and in individual
  mode, each with its mode, owner, group, writer, and purpose, and the rule by
  which each place is chosen (the shared candidate first, what a missing or an
  unusable one does), as karvi 0.26.0 leaves them on a host with two
  operators. It names a setting with no use in this release, `logging.file`,
  to which nothing is written.
- **A platform's `channel`, and the built-in `linux_shell`.** A
  `[platform.NAME]` table takes `channel = "shell"` or `"exec"`, what karvi asks
  of the SSH session channel; built-in `linux` is `exec`, every other built-in
  `shell`, and a table that leaves it unset is `shell`; another word is
  `config_platform_channel_invalid`. Each target's channel is resolved at
  planning, carried in the plan (execution plan schema 11) and the manifest, and
  shown on the dry run's `intended:` line (`channel=shell`). A target whose
  platform says `exec` over telnet, which has no exec, is refused at planning
  with `channel_exec_over_telnet`. An eighth built-in, `linux_shell`, is `linux`
  on the shell channel with `linux` as its base, so it is admitted wherever
  `linux` is and its records name `linux_shell`. The platform field
  `control-master`, read by nothing, is removed: a table that sets it is refused
  as `config_unknown_key`, and `config_platform_control_master_not_boolean` is
  retired. The configuration registry moves to 25.
- **A platform's `fallback`, and the operator's own keys for servers.** What
  follows a credential policy's backends is the platform's `fallback`, an
  ordered list of `netvars` (`NETUSER`, `NETPASS`, `NETENABLE`), `keys`, and
  `prompt` (another word, or one twice, is `config_platform_fallback_invalid`):
  the network built-ins and `generic` are `["netvars", "prompt"]`, as before;
  `linux` and `linux_shell` are `["keys"]`, so the variables an operator exports
  for routers no longer reach a server (a site that wants them writes
  `[platform.linux] fallback = ["netvars", "keys"]`). `keys` is the operator's
  login name and the files of the new `ssh.identities`, by default
  `~/.ssh/id_ed25519`, `~/.ssh/id_ecdsa`, `~/.ssh/id_rsa`, each an absolute path
  or one beginning with `~/` (`config_ssh_identities_invalid` otherwise), judged
  at planning: a file that is not the operator's own, has group or other access,
  holds a passphrase, or is hardware-backed is skipped with the notice
  `operator_key_skipped`, and with none left the device fails with
  `credential_operator_keys_missing` (exit 6). The credential
  (`builtin-operator-keys`) carries each key's path and fingerprint, never its
  bytes, in the grant, the credential package (schema 2), and the manifest, and
  the dry run shows a `key:` line per key. Both transports offer the keys in
  order, read at the connection: the system transport with `IdentitiesOnly yes`,
  `IdentityAgent none`, and an `IdentityFile` per key, `scrapligo-v1` as
  signers, so `run`'s default transport now reaches a key-only server; a
  credential with keys and no password offers no password method. Both try the
  methods in the order `publickey`, `keyboard-interactive`, `password`
  (`scrapligo-v1` had tried password first). Each record's `credential.auth`
  names the method that authenticated the session, and every `command_completed`
  audit event names it with the device username and the credential backend. The
  fake device takes `-authorized-keys PATH` and `-no-keyboard-interactive`.
  `ssh.pubkey-authentication` is removed and refused at load
  (`config_key_removed`): a backend's credential holds no key and needs a
  password, so a backend row without one is `credential_password_missing` where
  that setting had let it pass; `config_ssh_auth_mechanisms_disabled` is
  retired.
- **The shell's output and prompts are the text the terminal showed.** In
  `command`, `run`, and `crun`, on every platform and over SSH and telnet, a
  command's output and the prompts in its record are rendered as the terminal
  showed them, where only carriage returns were dropped and only CSI sequences
  were removed from the prompt line: colours, window titles, terminal modes,
  and bash's bracketed-paste switches are gone, a backspace or an erase is
  applied, and a carriage return returns to the line's start. A record of a
  Linux server's shell that held `\x1b[?2004l…\x1b]0;netops@dev: ~\x07…` now
  holds the command's text alone, and its `prompt` is `netops@dev:~$`, no
  longer carrying the window title. The stream is rendered at no width, as the
  transports ask the far end for no size; a space the device wrote is kept on
  an inner line. Records and their digests move wherever a device sent such
  bytes; the IOS XE fake sends none, and its records are unchanged.
- **A recorded login's transcript is the text the terminal showed.** It had been
  the bytes the terminal was sent, colours, window titles, and the line editor's
  controls included, so a word corrected with two backspaces read `echo helolo`
  once the controls were deleted. At the session's end `login --record` now
  removes `script(1)`'s two lines and renders the rest before the metadata's
  digest: the corrections applied, colours, titles, and terminal modes dropped,
  a line longer than the terminal one line, every line ending in a newline with
  no carriage return. The widths come from `script(1)`'s timing log (`-T FILE -m
  advanced`, the scratch's `karvi-script-*.timing`, removed after). A timing log
  that cannot be read leaves the transcript rendered at the starting columns,
  under the notice `transcript_timing_unreadable`. No raw copy is kept; a
  session killed before its end keeps its bytes as `script(1)` wrote them. The
  runbook gains row D16, IOS XE's line editor in a recorded login, done by hand.
- **The fake device is `karvi-fake-device`, with a Linux persona.** The test
  fixture's package is `internal/fakedevice` and its command
  `cmd/karvi-fake-device`, renamed from `fakeiosxe` and `karvi-fake-iosxe`; the
  IOS XE persona stays the default, and the suites' device `fake-iosxe` keeps
  its name. `-persona linux` answers exec requests from a fixed table (the
  servers' collection list, `fail N`, `both`, `big` and `bigerr`, `slow`,
  `signal TERM`, `nostatus`, `sudo -n id -u`, and `sh`'s not-found message with
  exit 127 for any other command) and has a shell for `linux_shell`
  (`netops@fake:~$`, bash's decorations under `-decorations`); `-sudo-asks`
  makes `sudo -n` refuse. Its standard error adds `signal: NAME`, `left running:
  "LINE"`, and a closing `channels=N` line beside the unchanged `connections=`
  line.
- **Command record schema 3: every record's channel, an exec command's exit and
  stderr.** Every record carries `channel` (`shell` or `exec`); an exec record
  adds `exit_status`, `exit_signal` (the signal's name, `unnamed` where the
  transport gives none), and `stderr` with `stderr_encoding`, `stderr_bytes`,
  and `stderr_sha256`, the stream spooled past `output.spool-threshold-bytes` as
  `output` is. On a shell record the new fields are null; on an exec record a
  null `stderr` says the command did not run (its channel never opened, or the
  device refused the exec request), an empty one that it wrote nothing there,
  and `prompt_source` is the new value `none`. A consumer that checks
  `schema_version` reads 3, and every line of `commands.jsonl` gains the null
  fields; the daemon IPC schema stays 10. A record that breaks the new fields'
  rules is `record_channel_invalid` or `record_stderr_encoding_invalid`.
- **The exec channel, on both transports.** A target whose platform's `channel`
  is `exec` gets one connection for its command list and one exec channel per
  command, without a pty, with standard input at its end, and with no prompt,
  privilege step, paging command, or exit command. Exit 0 is `succeeded`
  whatever stderr holds; a non-zero exit is `command_exit_nonzero`, a signal
  `command_exit_signal`, and a channel closed without a status on a live
  connection `command_exit_missing`, each a device error (exit 107); a device
  that refuses the channel or the exec request is `ssh_session_channel_refused`,
  and its session ends. A command timeout, a cancel, and the output limit
  (stdout and stderr counted together) stop the command, and the connection
  serves the next under the device-error policy. Blind sends, `\r` endings,
  `--blind-return`, and `--expect` are refused at planning for an exec target
  (`channel_exec_declaration_refused`, exit 4), by the client and the daemon's
  plan check alike. On `scrapligo-v1` each command is a session channel on
  karvi's connection, and a command given up is asked to end with `KILL`. On
  `system` the connection is an OpenSSH ControlMaster (`ssh -M -N`, the client's
  child for `command` and the daemon's for `run`, started with a death signal)
  and each command an `ssh -S` client of it at `LogLevel QUIET` with
  `ProxyCommand false`; the master runs at `LogLevel DEBUG1`, its stderr read
  for the method that authenticated, each command's exit status or signal, and a
  refused exec request. OpenSSH names no signal, so `exit_signal` is `unnamed`
  there, and its client cannot ask the device to end a command, so a command
  given up carries the notice `remote_command_not_stopped` and may still be
  running. A master not ready within the login's bound is
  `command_session_prompt_timeout`, its cause naming the master's `-O check`.
- **The control sockets' place.** `ssh.control-path-root`, read by nothing
  before, holds the exec masters' sockets: `/dev/shm/karvi/<user>/sockets` where
  the site's scratch root exists, else `<basedir>/socket/ssh`, one
  16-hex-character name per device session, removed when the session ends. `~`
  in the key is the home the password database names, where it had been
  `basedir`'s grandparent. A root longer than 73 bytes, which OpenSSH's socket
  path cannot hold, is `control_path_root_too_long` (exit 2) at planning for an
  exec target over `system`. A socket a killed master left is removed at the
  daemon's start and at each job's admission, only a name karvi makes and only
  when nothing answers, logged `control_socket_abandoned_removed`.
- **Built-in `linux` runs on the exec channel.** A `linux` target's commands run
  as exec commands on both transports, each with its exit status and its stderr
  apart: `ls /nonexistent` is `command_exit_nonzero` (exit 107) where the shell
  had recorded `succeeded` with bash's bracketed-paste switches, the window
  title, and the login shell's aliases in the output. A server that refuses exec
  requests takes `linux_shell`, which keeps the shell. `docs/OPERATIONS.md`
  gains "Linux servers", and `docs/COMMAND-SESSION.md` "The exec channel".
- **A server's collection.** `linux` and `linux_shell` carry a built-in
  `crun-commands`: `cat /etc/os-release`, `uname -snrm`, `ip -br address`, `ip
  route show table all`, and `systemctl list-unit-files --state=enabled
  --no-pager --no-legend`, so `crun` over a fleet with servers no longer refuses
  them with `crun_platform_commands_missing`. Under exec a block is the
  command's stdout, then its stderr; a non-zero exit leaves its error text in
  the block and the file is replaced, the status kept in the record.
  `docs/COLLECTION.md` gains section 2.1, the servers.
- **The parity suite covers the exec channel.** `scripts/native-smoke-test.sh`
  runs S35a to S35f over the fake's Linux persona (each way a command ends, the
  output limit, both spools, a refused `sudo`, a refused exec request, and a
  `linux_shell` row with bash's decorations). `tools/paritycheck -pin
  N.PATH=TRANSPORT:JSON;…` states a difference between the transports by design,
  checked in every stream and left out of the comparison: the exec cases pin
  `exit_signal` and the notice `remote_command_not_stopped`.
- **A production server's qualification rows.**
  `docs/DEVICE-QUALIFICATION-RUNBOOK.md` gains section 9, rows L1 to L6 run by
  hand against one server of each kind: the server's SSH, the exec records on
  four streams, `sudo -n`, a command given up and what it left, the collection,
  and `linux_shell`.
- **Stream mode: one dash or two, quotes, and the commands that stay.** A line
  beginning with a dash and one more character is a run option with one dash or
  two alike, as on a `run` command line: `-c show clock`, `-target r1`, and
  `-go` mean what `--cmd`, `--target`, and `--go` mean, where a one-dash line
  had been sent to the devices as a command (`-` alone still is one). An
  option's value wholly wrapped in one pair of quotes loses them as a shell
  would remove them (`--cmd "show clock"`, `--target 'r1'`), where the quotes
  had reached the command or the target's name; every other quote, and every
  quote on a bare line, is sent as written. A command given in option form
  (`--cmd` and its aliases, `--cf`) now stays in the draft after `--go`,
  `--sendit`, and `--clear`, with its declarations, and is sent by every later
  job; a bare line is sent once. `--purge-commands` (from `--purge-c`) empties
  every command and `--purge-targets` (from `--purge-t`) every target input;
  `--purge` alone is refused as `cli_option_ambiguous`. `--exit` leaves as
  `--end` and `--quit` do.
- **Stream mode: a line no longer swallows the commands after it.** A flag given
  text after a space (`--no-daemon yes`), or a `--` line (`-- foo`), put text no
  option takes into the draft, which `run` read as freeform command text: every
  later command line joined it, and the job sent the one line `yes --cmd …` to
  its targets. Such a line is now dropped with its number as
  `cli_positional_unexpected`, the draft standing.
- **A run through the daemon is bounded as its invocation said.** The daemon's
  job took `execution.command-timeout`, `execution.device-timeout`,
  `execution.prompt-timeout`, `execution.enable-timeout`, `telnet.read-timeout`,
  `output.max-command-bytes`, and `execution.halt-device-on-command-error` from
  the daemon's configuration, so a `--set` of any of them, or a client
  configuration that differed, did not reach a `run` through the daemon, while
  the daemon kept the `--set` of whichever invocation had started it for every
  later job. The plan carries them (a new `execution` block; the output block's
  `max_command_bytes`, carried and not read until now; the dispatch block's
  `continue_device_on_error`), and the executor and the transports read them
  from the plan on every path. The plan schema stays 11.
- **`--timeout` and `--maxbytes`, one command's own bounds.** On `run`,
  `command`, `crun`, and in stream mode, `--timeout DURATION` and `--maxbytes
  BYTES` are declarations on the `--cmd` before them, as `--expect` is: the
  command's timeout in place of `execution.command-timeout` (over telnet, of
  `telnet.read-timeout` too), and its output limit in place of
  `output.max-command-bytes`, in those keys' forms and ranges (1s to 12h; whole
  bytes, 1024 to 1 GiB), at most one of each per command
  (`declaration_repeated`). A copy of an image can run 45 minutes beside `show`
  commands that keep their 120 seconds. `--timeout` on a blind command is
  `timeout_with_blind`; above a set `execution.device-timeout` it is
  `timeout_over_device_timeout`, and a `--maxbytes` above
  `output.max-job-bytes` is `maxbytes_over_job_limit`, each naming both values
  and the `--set` that raises the ceiling. The plan carries `timeouts_ns` and
  `max_bytes` (schema 11 stays); the free-space check's spool term is the
  width times the largest command limit in the job. A timeout's or a limit's
  message names its source: `command timed out after 45m0s (--timeout)`, `…
  after 2s (execution.command-timeout)`, `command output exceeded 2048 bytes
  (--maxbytes) across stdout and stderr, 5000 observed`; the observed count of
  every limit message now follows a comma. The debug line `device command
  start` carries `timeout=` and `maxbytes=`.
- **Over telnet, the device deadline names its cut.** A command cut by
  `execution.device-timeout` over telnet was recorded `command_timeout`; it is
  `device_timeout`, as on SSH, and a telnet timeout's message reads `command
  timed out after …` naming the bound that expired, where it was the socket's
  `i/o timeout`.

## 0.26.0 - 2026-10-04

A minor release on 0.25.0. The configuration registry moves from 22 to 24
and the execution plan from 9 to 10; the daemon IPC schema and every other
counter are 0.25.0's (scoreboard 3, configuration schema 6, command record
2, job 2, credential package 1, plan report 1), and a running 0.25.0 daemon
is `compatible: false` by its version alone and is restarted. A site has
something to change where it set `output.files.failures-jsonl` (now
`output.files.errors-jsonl`, the job's `errors.jsonl`), relied on `run`'s
Dispatch options passing a lock or a range, or read `crun`'s result line on
standard error (now the display's collection footer). The module graph gains
`github.com/yuin/goldmark`, for `tools/md-to-html` alone.

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
- **A manual page for `karvi-prune`.** `packaging/man/karvi-prune.8`, installed
  by the debian rules as `/usr/share/man/man8/karvi-prune.8`, is the terminal
  reference: what goes and never goes, the flags, where it looks, the report,
  the exits, the files, and examples. Its SYNOPSIS and OPTIONS are generated
  from the helper's flag definition by the new `tools/mangen` under `make
  generate`; `make generated-clean` and the bundle verifier compare them, and a
  test lints the page with groff. `make generated-clean` now fails on any stale
  generated file, where only the last one compared
  ([`docs/ERROR-CODES.md`](docs/ERROR-CODES.md)) could fail it.
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
- **Every exit status has a written meaning.**
  [`docs/ERROR-CODES.md`](docs/ERROR-CODES.md) opens with an ["Exit
  statuses"](docs/ERROR-CODES.md#exit-statuses) table: the 24 statuses, the name
  the records and the audit carry (`ExitPartialFailure`), and what each means,
  with the order in which they apply to a job, where the statuses had names
  alone and only the error codes said which exit they set. The list is defined
  once in `internal/exitcode`, a test holds every constant to it, and the error
  registry refuses a code naming an undefined exit.
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
- **The device-qualification runbook records how a login ends.** Row D15,
  by hand: plain `ssh`'s exit and `karvi login`'s after `exit` on each
  laboratory device. Against the fake, which sends no exit status, a login
  ended with `exit` exits 110 (`ssh_process_failed`); the roadmap holds the
  rule to take if a device does the same.
- **The failures file is `errors.jsonl`.** A job folder's file of the
  records that did not succeed, device and command errors alike, is
  `errors.jsonl`, where it was `failures.jsonl`; its switch is
  `output.files.errors-jsonl` (`KARVI__OUTPUT__FILES__ERRORS_JSONL`), the
  execution plan's field `errors_jsonl`, and the summary's path
  `errors_jsonl`. `output.files.failures-jsonl` is refused from a file,
  the environment, and `--set` with `config_key_removed`, naming the new
  key.
- **The documents link to each other.** A document named in another,
  with or without a section, is a relative link to it or to the heading,
  where it was a code span; numbered sections and chapters link to their
  headings. The documents' prose is wrapped at 80 columns, ERROR-CODES'
  in its generator.
- **The README's image is in the tree.**
  `images/karvi-viking-fleet-command.png`, where the README had named GitHub's
  attachment storage.
- **The documentation as HTML.** `make html` writes every Markdown file of the
  tree as a page, an index with the documents by group, and the images into
  `../html`, beside the tree, by the new `tools/md-to-html`; every link is
  relative and every link and anchor is checked before the directory is
  replaced. Each directory below the root has an `index.html` that sends the
  browser to the parent's, so a server lists none. goldmark is vendored for the
  tool alone.
- **The licence is `LICENSE.md`.** The MIT License, where it was `LICENSE`,
  with its title a heading, so the HTML has it as a page; README's License
  section links to it, and `NOTICE` names it.

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

The first release from the public repository, on 0.23.0. No behaviour of the
executables changed, and every counter is 0.23.0's (daemon IPC 10, execution
plan 9, scoreboard 3, configuration schema 6, registry 22, command record 2, job
2, credential package 1, plan report 1); a running 0.23.0 daemon is `compatible:
false` by its version alone and is restarted. The tree was prepared for public
release. The cumulative patch stream shipped beside each release's bundle since
0.10.0 is retired: a release is the source bundle, its checksum, and an
aggregate checksum file, and the release tools refuse a tree that holds anything
git does not track. The specification the program was built from is frozen and
archived with its earlier revisions, the decision records, the worked design
sessions, and the session hand-offs; [`docs/DESIGN.md`](docs/DESIGN.md) states
the settled decisions and why, and the code and its tests are the reference.
Every pointer at the archived documents was removed from the tree, the module
path is `github.com/robert-patrick-texas/karvi`, and the public history begins
at the root commit of this tree. Three messages that named a specification
section now say "a valid identifier", and the error catalogue's retired codes
name the release span that retired them.

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
