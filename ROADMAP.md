# Roadmap

What karvi does not do yet and has decided to do, or to consider, in the
order the operator has set. A settled decision is an entry in
`docs/DESIGN.md`; a session that builds an item is a chapter of
`docs/EXAMPLES.md`. Nothing here is a promise of a date.

## Next

1. **A man page installed with the distribution packages.** `karvi-prune.8`
   first, as roff written directly (the build host's groff renders it, no
   converter is needed), its flag section generated from the helper's one
   flag definition under `make generate` and checked by `make generated-clean`,
   one install line in the debian rules; then `karvi.1` and `karvi-askpass.1`,
   whose option sections come from the help constants already kept in step
   with the parser table, so the page cannot drift from `--help`.
2. **The documentation as HTML.** A script that converts `docs/*.md` to
   `.html` with an index page and a left column of links, links between
   documents rewritten, the output a build product and never committed. The
   converter is a vendoring decision (a Go Markdown library under `tools/`
   keeps the build self-contained) shared with the man page if one is used
   there.
3. **The package's contents.** The debian rules install the three executables
   alone. The documents go under `/usr/share/doc/karvi` (the Markdown, or the
   HTML of item 2, and the `examples/` files); the supplemental material (the
   units and timers, the drop-in and `crun` hook examples, the cron scripts,
   tmpfiles, sysctl, the completion file, the reference configuration, the
   schema) goes to a place of its own under `/usr/share/karvi` with a script
   that installs it for a site that opts into that style of operation. The
   units' `Documentation=` lines and the cron scripts' comments then name
   what the package installs; the control file's maintainer and homepage are
   placeholders until then.
4. **Build numbers in the version.** A build identity beyond the version and
   the commit, for telling two builds of one tree apart.
5. **The packaged user unit's sandbox where it does not apply.** On an Ubuntu
   24.04 host (systemd 255, `kernel.apparmor_restrict_unprivileged_userns=1`)
   a user unit given `PrivateTmp=yes`, `ProtectSystem=strict`, and
   `ProtectHome=read-only` ran in the client's own mount namespace and wrote
   into the home and `/tmp` as if unsandboxed. The question is what the
   packaged `karvi-daemon.service` and `karvi-prune.service` actually get on
   such hosts, what the documents promise, and whether the units need
   `PrivateUsers=`, a system unit, or a statement of the limit.

## Later

- **Macro files, `--mf PATH`.** A command file with a parser: variable
  substitution from the command line, the file, or the device's inventory
  row, expanded in the client before the plan, so the plan, its digest,
  `commands.txt`, and every record are those of an ordinary job. A value that
  differs per device makes the command list per device, which the plan holds
  today only for a collection's platform lists; that is the design question.
- **Job files, `--jf PATH`.** A saved `run` invocation (targets, commands or
  macros, options) that must plan to the same job as the command line it
  replaces, plus logic. The logic's first question is where it runs: a
  decision taken from a device's answer is taken in the session, per device,
  while the command plan is fixed and digested before the daemon accepts the
  job; `--expect` and the blind returns are the precedent to grow from.
- **Credentials.** An explicit `env:NAME` cell form for the credential CSV
  that needs no flag and fails with its own code when the variable is unset;
  a formula's match evidence carrying its source's file, line, and key; a
  command-line option to pin a direct target to a key; further keyed
  backends (a SQLite or JSON store) through the keyed capability; policing
  every backend type's keys by type; one environment-indirection helper for
  the env, Redis, Vault, and CSV backends; in the shared tabular reader, an
  explicit mapping outranking a field's own header.
- **A review of every digest.** No digest over an output that does not
  declare one as required for its consumption. The review lists each digest
  the program computes or records with who computes it, who reads it, and
  what breaks without it, and removes those nothing consumes in production;
  the development use, comparing two outputs, stays.
- **A setting under which karvi defines no SSH algorithm lists** and both
  transports keep their own defaults.
- **`Sealed` credential packages.** The envelope schema accepts the
  protection and the `Sealer` interface is fixed, but no channel carries it
  and no reviewed provider exists; it waits for the stage that brings a
  remote channel. The contract's one known fault, that the envelope's
  ciphertext digest cannot be compared with the projection's own sum, is
  recorded for that stage: under authenticated encryption a successful open
  is the validity check.
- **Crash recovery.** A daemon journal, reconstruction of an unfinished job
  after a daemon crash or restart, and an `incomplete_daemon_recovery`
  terminal status. Today a job whose daemon died leaves its directory
  without a summary, `job follow` answers `job_orphaned` for it, the pruner
  retires it by age, and an abandoned spool's name carries what a
  reconstruction would need.
- **A `--from-now` follow** starting at the live edge, with its own header
  saying what it left out.
- **Field prefixes in the watch screen's filter** (`op:`, `mode:`), if the
  word filter proves too broad.

## The device-qualification track

Independent of the items above. The runbook (`docs/DEVICE-QUALIFICATION-RUNBOOK.md`)
and the evidence-collecting script (`scripts/device-qualification.sh`) cover
both transports as rows D1 to D14, and the script passes against the fake
device; the laboratory run needs the operator and a device:

1. Validate IPv4 and IPv6 selection and fallback with controlled dual-stack
   DNS.
2. Re-run successful and intentionally invalid multi-command sessions
   against the ISR 4451-X and Catalyst 9300 with `--echo` and safe `--debug`.
3. Qualify paging, privilege, and prompt and error behaviour on both
   transports, `command` and `run` sharing one device session on each.
4. Complete the Catalyst 9300 matrix of `docs/CISCO-IOSXE-QUALIFICATION.md`
   on both transports before other IOS XE families; the rows the fake
   already evidences are marked there.

Production activation of the daemon, the shared trees, the timers, and the
2,500-device accounting gate waits on that run and on review by the network
operations and security owners.
