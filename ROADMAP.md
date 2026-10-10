# Roadmap

What karvi does not do yet and has decided to do, or to consider, in the order
the operator has set. A settled decision is an entry in
[`docs/DESIGN.md`](docs/DESIGN.md); a session that builds an item is a chapter
of [`docs/EXAMPLES.md`](docs/EXAMPLES.md). Nothing here is a promise of a date.

## Next

In the operator's order: the stream's restart first, in the code chapter 52
rebuilt ([`docs/EXAMPLES.md`, chapter
52](docs/EXAMPLES.md#52-one-read-of-the-configuration-per-invocation-2026-10-10));
the setting without algorithm lists, which touches nothing else; the two
credential items together, the explicit cell before the helper whose values it
will read; build numbers last, beside the next release, which exercises the
release tools they change.

1. **A stream's `--restart`.** A stream reads its configuration once, as it
   starts, and runs every job under that reading, so an edit reaches the next
   stream and not the next job. The item: a directive, `--restart`, that
   empties the draft, its commands and its targets, as `--reset` does, and
   starts the stream again under the options its command line gave, the
   configuration read again. The questions: a reading that fails (the stream
   ending, or going on under the reading it had); what else starts again (the
   line count, the last exit, the terminal's recalled lines, the opening
   line); and a new reading's warnings.
2. **A setting under which karvi defines no SSH algorithm lists**, so both
   transports keep their own defaults. The questions: the host-key list, which
   karvi narrows to the key types the trust store holds for the device so that
   the host-key policy compares the right key; the names the transports'
   defaults hold that karvi allows only in a profile (AES-128 in both) or
   refuses (`hmac-sha1-96` in x/crypto's); the profiles and the map under the
   setting; and what the debug stream says was offered.
3. **An explicit `env:NAME` cell** for the credential CSV, which needs no flag
   and fails with its own code when the variable is unset, where a cell naming
   an unset variable under the indirection flags stays a literal
   ([`docs/CREDENTIAL-CSV.md`, section
   6](docs/CREDENTIAL-CSV.md#6-secrets-in-the-environment-instead-of-the-file)).
   The questions: a literal value that begins with `env:`; the code, and when
   it is said; and the field source.
4. **A credential helper executable.** A configured executable, a site's
   script in any language, that karvi runs and whose standard output sets
   values for its own use: a line `export NETUSER="xxx"` puts `NETUSER` into
   karvi's credential environment as if the operator had exported it, for the
   `NET*` fallback, an `env` backend's templates, or the token a `vault` or
   `redis` backend reads from the environment. Its first part is one
   environment-indirection helper: the env, Redis, and Vault backends each
   read the process environment themselves, with a private copy of the rule
   `envindirect` holds for the CSV, and the `NET*` fallback reads it through
   the credential input, so that the helper's values reach every reader
   through one lookup. The questions: when it runs (once per invocation in the
   client, before resolution, or per device with the device's name); which
   names it may set (the credential names alone, or any a backend reads); the
   grammar of its output, parsed and never evaluated by a shell (`export
   NAME="value"` and `NAME=value`, the double-quoted form's escapes, anything
   else refused naming the line number and never the line); its value held as
   a secret from the read on and never put into the process environment a
   child such as `ssh` inherits; the executable's own checks as a credential
   file's (an absolute path, the owner, the mode, no symlink); its
   environment, timeout, standard error (not echoed, since it may hold a
   secret), and a non-zero exit as a failure with its own code; whether the
   daemon may run it for an unattended job, where no prompt can; and how it
   relates to the helper protocols operators know (git's credential helpers,
   `SSH_ASKPASS`).
5. **Build numbers in the version.** A build identity beyond the version and
   the commit, for telling two builds of one tree apart. A release build's
   commit is `source-release-v` and its number, and its build time the day's
   midnight, so two release builds of one tree on one day are alike; a lab
   build carries its commit and its second. The questions: what the number is
   and who assigns it; what reads it (`karvi version` shows the commit and the
   build time, while the records' producer and `daemon status` name the version
   alone); and the controlled build a release reproduces
   ([`BUILD-HOWTO.md`](BUILD-HOWTO.md)).

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
- **Credentials.** A formula's match evidence carrying its source's file,
  line, and key; a command-line option to pin a direct target to a key;
  further keyed backends (a SQLite or JSON store) through the keyed
  capability; policing every backend type's keys by type; in the shared
  tabular reader, an explicit mapping outranking a field's own header.
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
- **A login's OpenSSH log level.** A `login` runs OpenSSH at `LogLevel ERROR`,
  fixed in the configuration karvi generates, so a session that never reaches
  the prompt says nothing of how far it got;
  [`docs/SSH-TROUBLE.md`](docs/SSH-TROUBLE.md) restores OpenSSH's account
  through a wrapper named by `ssh.transports.system`. The item: a login run so
  that OpenSSH writes a higher level to a file (`-E`), the terminal staying the
  operator's, and the file's path named on the debug stream. The questions:
  the level (`VERBOSE`, as `command` runs it, or `DEBUG1` to `DEBUG3`); what
  turns it on (`--debug`, a key, an option of `login`); where the file lives
  and for how long (the scratch under the sweep's rule, or beside a recorded
  login's transcript); whether karvi reads it back at the session's end to
  classify a failure as it does for `command`; and what it holds that is
  sensitive (the address, the user name, the host key, never a password).
- **A review of every digest.** No digest over an output that does not
  declare one as required for its consumption. The review lists each digest
  the program computes or records with who computes it, who reads it, and
  what breaks without it, and removes those nothing consumes in production;
  the development use, comparing two outputs, stays.
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
- **A device's banner kept out of `system`'s failure text.** OpenSSH writes a
  device's pre-authentication banner on its standard error at `LogLevel INFO`
  and above, among its own lines; the shell's command session (`VERBOSE`) and
  the exec master (`DEBUG1`) receive it, so a failure over `system` carries the
  banner in its error text and records, and the classification of the failure
  reads it by substring, where a banner's words could name the code. No byte
  tells it apart: a Cisco IOS XE banner ends its lines in `\r\n`, as OpenSSH's
  own lines do ([`docs/EXAMPLES.md`, chapter
  37](docs/EXAMPLES.md#37-the-first-contact-said-alike-over-both-transports-2026-10-07)).
  A login, at `LogLevel ERROR`, receives no banner, and the native transport
  drops one. The question: an exact separator, `-E` to a FIFO for OpenSSH's
  log, which takes `docs/SSH-TROUBLE.md`'s `-vvv` trace from its file into
  karvi's; the same `-E` "A login's OpenSSH log level" asks of a login.
- **`insecure`'s comparison from the exec master's own line.** Over `system`
  under `ssh.host-key-policy = "insecure"` the trust store given to OpenSSH is
  `/dev/null`, so a stored device's key is compared by an `ssh-keyscan` beside
  the connection, and when that second connection fails (not installed, past its
  five seconds, a router with few vty lines refusing it) the session goes on
  uncompared, `host_key_not_compared`. The exec master runs at `DEBUG1`, where
  OpenSSH writes `Server host key: TYPE FINGERPRINT`, so an exec device could be
  compared from that line without a second connection; the shell's command
  session and a login do not receive it
  ([`docs/EXAMPLES.md`, chapter
  37](docs/EXAMPLES.md#37-the-first-contact-said-alike-over-both-transports-2026-10-07),
  issue 5a).
- **A server that closes before its exit status.** The system transport's exec
  master closes the channel at the end of output; `sshd` still sends the status
  after that close, and the master takes it, but a server that sends its end of
  output, then its status, and nothing after a close, as the fake device did,
  would lose the status over `system`, recorded `command_exit_missing`, rightly
  ([`docs/EXAMPLES.md`, chapter
  32](docs/EXAMPLES.md#32-the-lost-exit-status-found-in-the-fake-device-2026-10-07)).
  The question: whether karvi guards against one, for instance by keeping the
  client's input open so that the master never closes first, and what that costs
  a device that waits for its input to end.
- **A suite's failure that names its call.** A suite under `set -eu` that
  runs karvi with its output redirected into the work directory dies at a
  refused call with karvi's exit status and prints nothing, and its cleanup
  removes the files that held karvi's message: under umask 0002 ten suites
  ended so, most with exit 109 and no line, while karvi had written
  `host_key_directory_permission` and the reason on its standard error
  ([`docs/EXAMPLES.md`, chapter
  41](docs/EXAMPLES.md#41-the-suites-work-directories-made-0700-2026-10-08)).
  The bundle's verifier has the same gap: `verify-shipped.sh`'s checks are
  bare tests under `set -eu`, so a release missing its number in that script
  ended `verify-bundle.sh` with exit 1 and an empty log ([chapter
  43](docs/EXAMPLES.md#43-the-release-with-the-debian-package-2026-10-08),
  S2). The question: one function in `scripts/lib` that runs a call and, when it
  fails unexpectedly, names it and prints what karvi wrote before the suite
  exits.
- **A client told when a running daemon's settings differ from its own.** A
  daemon keeps the `daemon.*` values of the invocation that launched it, and a
  later client's `--set daemon.*` reaches it only at its next start, without
  a word (chapter 47, S1's reload class, executed with
  `daemon.shutdown-idle-timer`). The item: the daemon reports its `daemon.*`
  values in its status reply, and a client whose own differ prints a notice
  naming the keys and `karvi daemon restart`. The questions: which keys (the
  `daemon-start` class alone), the notice's wording and its code, and whether
  `--quiet` keeps it.

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
