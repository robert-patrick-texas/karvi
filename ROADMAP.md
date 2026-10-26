<img width="1672" height="941" alt="A karvi longship sailing a fjord toward glowing network islands" src="images/karvi-voyage-across-network-fjords.png" />

# Roadmap

What karvi does not do yet and has decided to do, or to consider, in the order
the operator has set. A settled decision is an entry in
[`docs/DESIGN.md`](docs/DESIGN.md); a session that builds an item is a chapter
of [`docs/EXAMPLES.md`](docs/EXAMPLES.md). Nothing here is a promise of a date.

## Next

1. **Shorter output strings.** A review of every error and warning message
   karvi writes for the operator, shortening each where a word adds nothing so
   that it still conveys its meaning in fewer words, as chapter 56 left the
   credential sources' `NETPASS_NYC not set` and `NETPASS_NYC empty`; and the
   output formatted for alignment and colour as appropriate. Not the help, the
   header and footer templates, or the daemon log. The rule is stated in
   [`docs/DESIGN.md`](docs/DESIGN.md), and the six longest messages follow it
   ([`docs/EXAMPLES.md`, chapter
   69](docs/EXAMPLES.md#69-the-six-longest-messages-shorter-2026-10-18)); the
   rest are reviewed under it, longest first, from a list ranked from the
   source, the tests, suites, and documents that quote a message changed with
   it. The questions still open: the stage prefixes (`client planning:`) and
   Go's own error text in a chain; the configuration errors' order (`MESSAGE
   for KEY at SOURCE`); and the generated error-code descriptions
   ([`docs/ERROR-CODES.md`](docs/ERROR-CODES.md)) under the same rule.
   Chapter 63's words reach it too: the messages built from them
   (`job_orphaned`'s "its scoreboard ended error") and the help's and the
   registry's prose ("halted terminal status", "replaced only when the
   device's collection succeeded").

## Later

- **Macro files, `--mf PATH`.** A command file with plain text substitution
  and pattern replacement, for what a site needs at the least effort, with no
  tool outside karvi: values from the command line, the file, or the target's
  inventory row, expanded in the client before the plan, so the plan, its
  digest, `commands.txt`, and every record are those of an ordinary job; the
  templates stay a tool's ([`docs/DESIGN.md`, section
  7](docs/DESIGN.md#7-the-plan-the-daemon-and-the-credential-channel)). A
  value that differs per target makes the list per target, the command file
  per target ([Next](#next)). A macro file may also define a secret or link a
  reference to a credential; a secret is the exception to the expansion
  before the plan, a reference until the session (secrets in statements,
  below).
- **Secrets in statements.** A statement names a secret instead of holding it
  (`snmp-server community %secret:snmp-ro% RO`, the spelling open): the client
  resolves the reference as it resolves a login's password, or from a macro
  file that defines the secret or links the reference to a credential; the
  value travels to the daemon in the credential package and the session
  writes it to the device alone. The plan, its digest, every record, the job's
  files, the audit, and the display keep the reference, never the literal
  secret, and the session removes the exact value from what the device sends
  back, as the live view's `platform.SecretFilter` removes a secret the
  session writes from the screen. Today a statement's secret is masked in the
  audit by the platform's `masks` and kept as sent in the job's files
  (`commands.txt`, `commands.jsonl`, `errors.jsonl`, `manifest.json`,
  `output.TARGET.txt`), in the `--echo` and `--live` displays, and in
  `--debug`'s command start line; until this is
  built, a tool that renders
  configuration renders no secret in clear, the device's own encrypted forms
  where it has them. The questions: the reference's spelling, and whether it
  is declared (a command file is literal, and a `%` in a banner is device
  text); where a named secret lives in each backend, per target or per site; a
  macro file that holds a secret, and the permission check a credential file
  has; a reference that finds no value (refused at planning); and the stream.
  Masking the job's files by the platform's `masks` was weighed and set aside:
  the patterns are best effort, a masked `commands.txt` would send `<removed>`
  on a rerun, the manifest's plan would no longer match its digest, and a
  record's `command_sha256` beside a masked line is a fingerprint to guess.
- **A daemon run's live view.** `--live` runs in the client's process alone
  ([`docs/DESIGN.md`, section 12](docs/DESIGN.md#12-display-and-help)).
  Through the daemon (`run` without `--no-daemon`, `job
  follow`), the device's bytes would reach the follower as frames of their own
  on the IPC, the job serial as in the client's process. For consideration;
  nothing is decided.
- **Job files, `--jf PATH`.** A saved `run` invocation (targets, commands or
  macros, options) that must plan to the same job as the command line it
  replaces. The logic it was to carry is settled elsewhere: a decision taken
  from a device's answer is taken between jobs, by a tool outside karvi
  ([`docs/DESIGN.md`, section
  7](docs/DESIGN.md#7-the-plan-the-daemon-and-the-credential-channel)). A job
  file could also carry each command's declarations (`--expect`, `--blind`,
  `--blind-return`, `--timeout`, `--maxbytes`), as a stream's lines do and a
  command file cannot, its lines being statements alone
  ([`docs/EXAMPLES.md`, chapter
  79](docs/EXAMPLES.md#79-a-command-file-per-target-2026-10-25)).
- **A suffix on a `--cf` folder's names.** A folder's files are named exactly
  as the targets' collection files (`core-nyc-01`), so a tool that writes
  `core-nyc-01.cfg` renames them. A suffix of the input's own is for
  consideration, apart from `--fs`, which is the collection's and on a run
  turns a collection on ([`docs/EXAMPLES.md`, chapter
  79](docs/EXAMPLES.md#79-a-command-file-per-target-2026-10-25)); nothing is
  decided.
- **Credentials.** A formula's match evidence carrying its source's file,
  line, and key; a command-line option to pin a direct target to a key;
  further backends, each with its own type, keys, and driver: a SQLite store
  holding device or operator data or both, and a JSON store, a device-keyed
  one honouring a pin through the keyed capability; in the shared tabular
  reader, an explicit mapping outranking a field's own header.
- **`sudo` on an exec channel: `--sudo` and `NETSUDO`.** A command over the
  exec channel that needs `sudo` runs only as written, `sudo -n …`, and a
  refused one records `command_exit_nonzero` with `sudo`'s message; nothing
  reads `NETSUDO`. The design is recorded ([`docs/DESIGN.md`, section
  5](docs/DESIGN.md#5-transports-and-the-device-session)): `--sudo` attaches to
  the `--cmd` it follows and runs it as `sudo -S -p 'karvi-sudo-<16 hex>:' --
  sh -c '<the command>'`, the password written once and only after that prompt
  appears on standard error, a second appearance `privilege_failed`; the
  password is the credential's enable field, then `NETSUDO`, then `NETENABLE`
  where the platform's fallback holds `netvars`, and with none the command runs
  under `sudo -n` and never waits. The questions: the run-wide spelling for
  command files; and whether `NETSUDO` and `NETENABLE` fill a backend's blank
  field, which the `NET*` variables never do for a device a backend answered
  ([`docs/EXAMPLES.md`, chapter
  56](docs/EXAMPLES.md#56-the-credential-helper-2026-10-11)).
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
  the development use, comparing two outputs, stays. The audit's are gone
  (chapter 63, its statements in their place), and so is the plan's
  `command_plan_digest`, read only by checks against the same message that
  `plan_digest` already binds (chapter 79); `plan_digest`, which the daemon's
  commit, the idempotency key, and the credential package read, stays.
- **`Sealed` credential packages.** The envelope schema accepts the
  protection and the `Sealer` interface is fixed, but no channel carries it
  and no reviewed provider exists; it waits for the stage that brings a
  remote channel. The contract's one known fault, that the envelope's
  ciphertext digest cannot be compared with the projection's own sum, is
  recorded for that stage: under authenticated encryption a successful open
  is the validity check.
- **Crash recovery.** A daemon journal, reconstruction of an unfinished job
  after a daemon crash or restart, and an `incomplete_daemon_recovery`
  terminal status. Today a job whose daemon died leaves its summary without
  a final status, `job follow` answers `job_orphaned` for it once its
  scoreboard is stale or gone, the pruner retires it by age, and an
  abandoned spool's name carries what a reconstruction would need.
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
  karvi stays on v1, whose line is maintained, until four questions are
  answered: whether a release can ship the library beside the static executables
  and always names it, so no session reaches the network or writes into the
  operator's home; whether karvi's host-key policy, today inside its own SSH
  handshake under v1's transport wrapper ([`SECURITY.md`](SECURITY.md)), has a
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
- **A device that is not a line CLI: a WTI DSM console server.** A WTI DSM
  answers SSH at `DSM>` (its Scripting Options can make the prompt the site
  ID) with slash commands (`/J`, `/RN`), asks `Sure?` of a switching command
  unless `,Y` follows it, and sets its parameters in menus (`/N`, `/P`, `/F`):
  an item's number, its value, Escape until `Saving Configuration` and the
  prompt; the status screen `/S` is left by Escape, and some forms move by the
  arrows and Tab. The device session sends a statement as a line, ends a
  response at the platform's prompt on the last rendered line, writes an
  `--expect` response with a carriage return, and renders a line, not a
  screen: a cursor positioned anywhere ends the line
  ([`docs/COMMAND-SESSION.md`](docs/COMMAND-SESSION.md)). What it gains: the
  DSM's settings collected and compared by `crun` as a router's are, its menus
  walked by a command, and a method for the next device of its kind. The
  parts, each taken only where a recording shows it needed: a
  `[platform.NAME]` table admitting the definition's prompt pattern, exit
  commands, and failure patterns (`generic`'s prompt pattern matches `DSM>`
  today, but an alias of it closes with `exit` for `/X` and reports no rejected
  command); keys in a statement and in an `--expect` response (Escape, Tab, the
  arrows, Enter, Ctrl-X), a response that ends in a key written without the
  carriage return, so the chain of expectations walks a menu as one statement;
  a screen mode for a form that positions the cursor, a terminal of a fixed
  size matched as a whole once it is quiet, its record the final screen; and a
  recording that becomes the model: `transcript.format = "jsonl"`, the event
  recorder named and refused today, keeping both directions with their times
  (`script(1)`'s advanced log), a printable key the device does not echo
  recorded `<removed>` and a named key by its name; a step that drafts from it
  the prompt, the exit, and each response's last line paired with the keys
  typed at it, as a `[platform.NAME]` table and a command file the operator
  edits; and the fake device replaying the recording, so the draft is tested
  without the device before the
  [device-qualification track](#the-device-qualification-track) qualifies it on
  one. Two ways beside it: WTI's REST API (JSON over HTTPS from its firmware
  6.51: status, serial ports, users), a channel karvi does not have, NETCONF's
  (above) the nearest; and a target reached through a console port (`/C 3`,
  the attached device's platform after the hop, `^X` at close), out-of-band
  access when the management network is down, an item of its own that needs
  the keys. It waits on a unit: its firmware, `karvi command dsm1 /J --live`
  under `generic`, and one session recorded raw in both directions under
  `script -m advanced` (`/J`, `/RN`, `/S` and Escape, `/N` into an item and out
  unchanged, `/X`, no secret typed), which shows what a login shows first, the
  menus' wording, and whether a form positions the cursor. The questions: the
  keys' spelling and the table's key names; a menu walk as one record or one
  per screen; the screen mode's size and its record; where the recording's
  secret rule fails (a device that echoes a password, a key typed ahead); and
  whether the drafting step is karvi's or a tool's. For consideration; nothing
  is decided.
- **Platforms beyond the built-ins.** A device of another vendor has no
  definition: a `[platform.NAME]` table is an alias of a built-in or
  `generic`'s definition, and `generic`'s prompt matches neither a Huawei VRP
  nor an HPE Comware prompt (`<HUAWEI>`, `[HUAWEI]`), so the session never finds
  one. scrapligo's YAML definitions (v1.4.3 embeds seventeen, eleven beyond
  karvi's: Aruba WLC, Cumulus Linux and vtysh, HPE Comware, Huawei VRP, IP
  Infusion OcNOS, Nokia SR Linux, SR OS, and SR OS classic, Palo Alto PAN-OS,
  Ruijie, and VyOS) drive scrapligo's channel and network driver, which karvi
  does not run, and the scrapli community's platforms are Python modules,
  several with code of their own. The YAML maps onto karvi's definition nearly
  field for field (the privilege levels, the desired level,
  `failed-when-contains`, the paging commands sent at open, the exit at close),
  short of a level's `not-contains`, a raw write at open, variants, and driver
  options; karvi adds the collection list, its drop list, the masks, and the
  session cap. Two parts, in order. First, a `[platform.NAME]` table admitting
  the prompt pattern, the privilege levels, the failure patterns, and the exit
  commands (the part the WTI item above names), so a site reaches a vendor
  without a release; it reverses the rule that prompt and error patterns are
  compiled data, never configuration ([`docs/DESIGN.md`, section
  3](docs/DESIGN.md#3-targets-inventory-and-platforms)), at the risk that a
  wrong failure pattern records a rejected command as succeeded, and with it
  comes a step that drafts such a table from a scrapligo YAML for the operator
  to edit. Then, a vendor the fleet runs made a built-in from its reviewed
  definition, as the network built-ins were made from scrapligo's, with its
  collection list, drop list, masks, tests against a fake persona or a recorded
  session, and the [device-qualification
  track](#the-device-qualification-track); the upstream data is a draft, not a
  reference (scrapligo's VRP configuration prompt misses VRP 8's `[~HUAWEI]`).
  Not taken: loading YAML at run time, a dependency and a format karvi would
  half implement, which still misses the Python platforms. It waits on a device
  of another vendor in the fleet.
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
- **A suite's failure that names its call.** A suite under `set -eu` that runs
  karvi with its output redirected into the work directory dies at a refused
  call with karvi's exit status and prints nothing, and its cleanup removes the
  files that held karvi's message: under umask 0002 ten suites ended so, most
  with exit 109 and no line, while karvi had written
  `host_key_directory_permission` and the reason on its standard error
  ([`docs/EXAMPLES.md`, chapter
  41](docs/EXAMPLES.md#41-the-suites-work-directories-made-0700-2026-10-08)).
  The bundle's verifier has the same gap: `verify-shipped.sh`'s checks are bare
  tests under `set -eu`, so a release missing its number in that script ended
  `verify-bundle.sh` with exit 1 and an empty log ([chapter
  43](docs/EXAMPLES.md#43-the-release-with-the-debian-package-2026-10-08), S2).
  A suite's own start budget stops a lane too, though with its line:
  `daemon-upgrade-smoke-test` gives `go run ./tools/daemon-fixture` two seconds
  to bind its socket, and the first run after a change to the packages it builds
  compiles them first (2.4 seconds on the operator's host, 0.18 with the cache
  warm), so a battery on a cold cache stopped there with `schema-2 fixture did
  not start` and the rest of its lane unrun ([chapter
  55](docs/EXAMPLES.md#55-an-explicit-envname-cell-2026-10-10), S4). The
  questions: one function in `scripts/lib` that runs a call and, when it fails
  unexpectedly, names it and prints what karvi wrote before the suite exits; and
  the fixture built before its wait begins, so the wait measures its start and
  not a compile.
- **A client told when a running daemon's settings differ from its own.** A
  daemon keeps the `daemon.*` values of the invocation that launched it, and a
  later client's `--set daemon.*` reaches it only at its next start, without
  a word (chapter 47, S1's reload class, executed with
  `daemon.shutdown-idle-timer`). The item: the daemon reports its `daemon.*`
  values in its status reply, and a client whose own differ prints a notice
  naming the keys and `karvi daemon restart`. The questions: which keys (the
  `daemon-start` class alone), the notice's wording and its code, and whether
  `--quiet` keeps it.
- **No job directory for a run refused at planning.** A run through the
  daemon whose credential resolution fails in the client
  (`credential_env_missing`, `credential_password_missing`, exit 6) leaves an
  empty job directory, which the daemon made for the job before the client's
  resolution failed; the same run without the daemon makes none
  ([`docs/EXAMPLES.md`, chapter
  55](docs/EXAMPLES.md#55-an-explicit-envname-cell-2026-10-10)). The question:
  the directory made when the job is submitted, or removed when the client
  abandons it.
- **`watch`'s OPERATOR from the file's owner.** `watch` shows a scoreboard's
  `operator.username`, which the job's own process writes at its start, and
  never reads who owns the file, so a scoreboard in the shared folder (3770,
  every member writing into it) naming another operator is shown under that
  name; `job list` names a job's operator from its summary's owner, the file
  system's word ([`docs/EXAMPLES.md`, chapter
  61](docs/EXAMPLES.md#61-a-list-of-jobs-karvi-job-list-2026-10-15)). The
  item: `watch`'s OPERATOR, its `/` filter, and its operator sort from the
  file's owner, as `job list`'s. The questions: a file whose field and owner
  differ, shown as the owner or marked; and the JSON form, which prints each
  snapshot as written.
- **The tests' events out of the host's journal.** The audit's sink writes
  to journald whatever the configuration, so the Go tests and the suites
  write their events into the host's journal: `internal/audit`'s `test`
  events, a collection hook's test's `crun.after` events, and every suite's
  jobs, the 170,844 `command_completed` events of thirty days among them
  ([`docs/EXAMPLES.md`, chapter
  63](docs/EXAMPLES.md#63-the-audit-trails-names-and-fields-2026-10-17)). An
  audit review would show them as work done. The item: no test or suite
  writes to the host's journal. The questions: how a test's sink is kept
  from journald, how a suite's is, and the audit suite's, whose subject is
  the round trip through journald, its jobs at devices named for its run
  alone ([`docs/EXAMPLES.md`, chapter
  64](docs/EXAMPLES.md#64-karvi-audit-the-audit-trail-as-a-table-2026-10-17)).
- **An audit event over journald's datagram bound.** The audit writes each
  event to journald as one datagram, and the socket refuses one over its send
  buffer (212,992 bytes on the development host, `net.core.wmem_default`): a
  job whose `start` event carries one list of 4,000 statements, 296,785 bytes,
  is refused at its acceptance, `audit_write_failed: … message too long`, exit
  111, before any device is contacted ([`docs/EXAMPLES.md`, chapter
  79](docs/EXAMPLES.md#79-a-command-file-per-target-2026-10-25)). The item: an
  event of any size the audit accepts reaches journald, as `sd_journal_send`
  passes a large entry in a sealed memfd over the socket. The questions: the
  largest event karvi writes, and whether one is bounded; the audit file
  beside it; a device command event of that size.
- **The spool suite's timeout row waiting on a condition.** The spool suite's S4
  cuts `show big` at a one-second command timeout and needs 1 MiB settled by the
  cut. The fake builds its 65.7 MB answer before it sends it, and what settles
  by the cut follows the disk under the spool: of 48 cuts at the fake with the
  spool on `/var/tmp`, all but one settled 12 to 48 MB and one 141,422 bytes,
  and a battery failed the row with nothing settled, the suite alone once in
  three ([`docs/EXAMPLES.md`, chapter
  79](docs/EXAMPLES.md#79-a-command-file-per-target-2026-10-25)). The item: the
  row asserts the cut's settled bytes whatever the disk's pace. The questions: a
  cut timed from the answer's first byte rather than from the send; the row's
  threshold; and the rows that time a fixed cut beside it (the cancel and the
  lost session at 0.7 s).
- **`verify-shipped.sh` reviewed for what it leaves.** Its `STATELESS`
  folder, made to show that `--help` and `version` create no home, is removed
  only when that check passes, so a failing check leaves it in `/tmp`. The
  release verifier's package folder had the same gap: a failed baseline left it
  ([`docs/EXAMPLES.md`, chapter
  73](docs/EXAMPLES.md#73-the-release-0340-2026-10-19)), and
  `verify-release.sh` now removes it on a trap at its exit. The item: every
  folder and file the script makes removed at its exit, passed, failed, or
  interrupted.
- **The scripts' traps on the signals.** Twenty-four scripts set their
  cleanup with `trap ... EXIT HUP INT TERM`, the trap not ending the script:
  sent TERM or HUP alone, such a script runs its cleanup and goes on, where
  without the trap it would end. A draft of `verify-release.sh`'s trap in that
  form, sent TERM during the container test, removed the package folder and
  went on to print `karvi native release verification passed`, exit 0; on
  `EXIT` alone bash runs the cleanup when the signal ends the script (TERM 143,
  HUP 129, a Ctrl-C 130). The item: each of the twenty-four on `EXIT` alone,
  as `verify-release.sh`'s is. The questions: a cleanup that stops a daemon or
  a container, run once at the exit, checked under each signal; and a check
  that keeps the other form out of the tree.

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
