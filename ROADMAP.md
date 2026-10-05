# Roadmap

What karvi does not do yet and has decided to do, or to consider, in the order
the operator has set. A settled decision is an entry in
[`docs/DESIGN.md`](docs/DESIGN.md); a session that builds an item is a chapter
of [`docs/EXAMPLES.md`](docs/EXAMPLES.md). Nothing here is a promise of a date.

## Next

1. **The package's contents.** The debian rules install the three executables
   and the manual pages alone. The documents go under `/usr/share/doc/karvi`
   (the Markdown, or the HTML `make html` writes, and the `examples/` files);
   the supplemental material (the units and timers, the drop-in and `crun` hook
   examples, the cron scripts, tmpfiles, sysctl, the completion file, the
   reference configuration, the schema) goes to a place of its own under
   `/usr/share/karvi` with a script that installs it for a site that opts into
   that style of operation. The units' `Documentation=` lines and the cron
   scripts' comments then name what the package installs; the control file's
   maintainer and homepage are placeholders until then.
2. **Build numbers in the version.** A build identity beyond the version and
   the commit, for telling two builds of one tree apart.
3. **The packaged user unit's sandbox where it does not apply.** On an Ubuntu
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
- **A credential helper executable.** A configured executable, a site's
  script in any language, that karvi runs and whose standard output sets
  values for its own use: a line `export NETUSER="xxx"` puts `NETUSER` into
  karvi's credential environment as if the operator had exported it, for the
  `NET*` fallback, an `env` backend's templates, or the token a `vault` or
  `redis` backend reads from the environment. The questions: when it runs
  (once per invocation in the client, before resolution, or per device with
  the device's name); which names it may set (the credential names alone, or
  any a backend reads); the grammar of its output, parsed and never evaluated
  by a shell (`export NAME="value"` and `NAME=value`, the double-quoted form's
  escapes, anything else refused naming the line number and never the line);
  its value held as a secret from the read on and never put into the process
  environment a child such as `ssh` inherits; the executable's own checks as a
  credential file's (an absolute path, the owner, the mode, no symlink); its
  environment, timeout, standard error (not echoed, since it may hold a
  secret), and a non-zero exit as a failure with its own code; whether the
  daemon may run it for an unattended job, where no prompt can; and how it
  relates to the environment-indirection helper above and to the helper
  protocols operators know (git's credential helpers, `SSH_ASKPASS`).
- **The key that authenticated.** A record names the method that
  authenticated its session (`credential.auth`) and not which of the
  credential's keys the server accepted. OpenSSH names the key at `DEBUG1`
  (`Server accepts key: PATH TYPE FINGERPRINT`) on the stderr the system
  transport already reads, so a line filter could take it and drop every other
  `debug1:` line before the diagnostics; an exec device's master already runs
  at `DEBUG1` through such a filter, so its stream carries the line, while the
  shell's runs at `VERBOSE`; the native adapter could note the signer that
  signed. The questions: the record's field (path and fingerprint
  beside `auth`), the audit's, and `login`, whose stderr is the operator's
  terminal ([`docs/DESIGN.md`, section 4](docs/DESIGN.md#4-credentials)).
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
- **The resolved path of every place key.** `config show --explain` names
  the path the next activity would use for `basedir` and
  `ssh.known-hosts-file`; the same `resolved:` line for the other keys whose
  `auto` is a chain or whose place depends on what exists: `tempdir`,
  `spooldir`, `output.root`, `transcript.root`, `crun.directory`,
  `watch.directory`, `sessions.shared-capacity-root`, and `daemon.socket`,
  each found without creating anything, so an operator reads each place
  [`docs/FILES.md`](docs/FILES.md) describes from the host itself.
- **ScrapliGo v2.** `github.com/scrapli/scrapligo/v2` (v2.0.0, 2026-10-03) is a
  Go binding, through `ebitengine/purego`, to `libscrapli`, a Zig shared library
  (0.0.1, released the same day; 14.6 MB for x86_64 Linux) that holds the
  session, the channel, and the SSH (libssh2) and Telnet transports. It loads
  the library from `LIBSCRAPLI_PATH`, else from `~/.cache/scrapli`, downloading
  it there from GitHub at first use without checking the published `.sha256`.
  karvi stays on v1.4.2, whose line is maintained, until four questions are
  answered: whether a release can ship the library beside the static executables
  and always names it, so no session reaches the network or writes into the
  operator's home; whether karvi's host-key policy, today inside its own SSH
  handshake under v1's driver and channel ([`SECURITY.md`](SECURITY.md)), has a
  place in v2 beyond a known-hosts file; the adapter's shape, with the session
  in the library and not shared with `system`
  ([`docs/TRANSPORT-DRIVER-ARCHITECTURE.md`](docs/TRANSPORT-DRIVER-ARCHITECTURE.md));
  and what it gains for a fleet karvi qualifies on `cisco_iosxe` over SSH
  (NETCONF, Telnet, more platform definitions). A v2 adapter registers
  `scrapligo-v2` beside `scrapligo-v1`
  ([`docs/SSH-TRANSPORTS.md`](docs/SSH-TRANSPORTS.md)).
- **NETCONF.** NETCONF over SSH (RFC 6242): the `netconf` subsystem on the
  session channel, port 830 by default, in place of a shell or an exec. The
  platform's `channel` word has room for it (`channel = "subsystem"` or
  `"netconf"`), and both transports can ask for a subsystem (`ssh -s`, and
  x/crypto's `RequestSubsystem`). The questions: the hello exchange and the two
  framings (the 1.0 end-of-message marker and 1.1's chunks); an RPC as the
  command and its `<rpc-reply>` as the record, with `<rpc-error>` as the
  device's failure in place of a pattern; how an operator writes an RPC on the
  command line, in a command file, and in `crun`; and which platforms get it
  (IOS XE's `netconf-yang`, Junos, EOS, IOS XR). It waits on the exec channel
  for Linux servers ([`docs/EXAMPLES.md`, chapter
  24](docs/EXAMPLES.md#24-jobs-across-linux-servers-2026-10-04)), whose record
  of a command without a prompt it would build on.

## The device-qualification track

Independent of the items above. The runbook
([`docs/DEVICE-QUALIFICATION-RUNBOOK.md`](docs/DEVICE-QUALIFICATION-RUNBOOK.md))
and the evidence-collecting script (`scripts/device-qualification.sh`) cover
both transports as rows D1 to D14, and the script passes against the fake
device; the laboratory run needs the operator and a device:

1. Validate IPv4 and IPv6 selection and fallback with controlled dual-stack
   DNS.
2. Re-run successful and intentionally invalid multi-command sessions
   against the ISR 4451-X and Catalyst 9300 with `--echo` and safe `--debug`.
3. Qualify paging, privilege, and prompt and error behaviour on both
   transports, `command` and `run` sharing one device session on each.
4. Complete the Catalyst 9300 matrix of
   [`docs/CISCO-IOSXE-QUALIFICATION.md`](docs/CISCO-IOSXE-QUALIFICATION.md) on
   both transports before other IOS XE families; the rows the fake already
   evidences are marked there.
5. Qualify how a login ends (runbook row D15, by hand): whether the
   device sends an exit status when a session ends with `exit`. If it
   does, the fake learns to send one, and nothing changes in karvi. If it
   closes the session without one, as the fake does (OpenSSH exits 255,
   and `karvi login` exits 110 as `ssh_process_failed`), the rule is: an
   interactive login that authenticated and ended with the device closing
   the session is a completed session, exit 0, with the notice
   `login_closed_without_status` in its metadata and audit, "authenticated"
   read from OpenSSH's own log (`-E` at `VERBOSE`, the terminal unchanged);
   a device that dies mid-session then ends 0 too, accepted for an
   interactive session whose operator saw it end.

Production activation of the daemon, the shared trees, the timers, and the
2,500-device accounting gate waits on that run and on review by the network
operations and security owners.
