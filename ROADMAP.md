<img width="1672" height="941" alt="A karvi longship sailing a fjord toward glowing network islands" src="images/karvi-voyage-across-network-fjords.png" />

# Roadmap

What karvi does not do yet and has decided to do, or to consider, in the order
the operator has set. A settled decision is an entry in
[`docs/DESIGN.md`](docs/DESIGN.md); a session that builds an item is a chapter
of [`docs/EXAMPLES.md`](docs/EXAMPLES.md). Nothing here is a promise of a date.

## Next

None chosen: the operator names the next item, from "Later" or anew.

## Later

- **Shorter output strings.** A review of every error and warning message
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
  on a rerun, and the manifest's plan would no longer match its digest.
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
- **karvi's statements in session-init profiles.** A `[session-init.NAME]`
  profile's commands are device text: an entry `--sudo …` or `--sleep …`
  reaches the device as written and fails there under the profile's
  `on-error`. A later profile may take karvi's statements and declarations,
  `--sudo`, `--sleep`, `--expect`, and the others, as a commands file's lines
  do ([`docs/EXAMPLES.md`, chapter
  96](docs/EXAMPLES.md#96-root-commands-on-the-exec-channel-2026-11-04), issue
  6).
- **`--sudo` on a shell channel.** `--sudo` reaches the exec channel alone; a
  target on a shell, `linux_shell` among them, is refused at planning
  (`channel_shell_sudo_refused`). The shell's form types `sudo -p '<prompt>' --
  TEXT` without `-S`, so `sudo` reads the terminal with its echo off, and
  answers after the prompt as on exec (chapter 24, issue 8). Built when
  operators need `sudo` in a `linux_shell` session, it may revisit chapter 96's
  issues 1 to 4: the refusal, the statement, the password's sources, and the
  wire.
- **The key that authenticated.** A record names the method that
  authenticated its session (`credential.auth`) and not which of the
  credential's keys the server accepted. OpenSSH names the key at `DEBUG1`
  (`Server accepts key: PATH TYPE FINGERPRINT`) on the stderr the system
  transport already reads, so a line filter could take it and drop every other
  `debug1:` line before the diagnostics; an exec device's master already runs
  at `DEBUG1` through such a filter, so its stream carries the line, while the
  shell's runs at `VERBOSE`; the native transport could note the signer that
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
  the development use, comparing two outputs, stays. The audit records no
  digest (chapter 63), and the plan's command lists have none of their own
  (chapter 79); `plan_digest`, over the whole plan, which the daemon's commit,
  the idempotency key, and the credential package read, stays.
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
  karvi's native transport is its own `x/crypto/ssh` connection, with no
  scrapligo ([`docs/EXAMPLES.md`, chapter
  82](docs/EXAMPLES.md#82-the-native-transport-without-scrapligo-2026-10-27)),
  and taking v2 waits on four questions: whether a release can ship the library
  beside the static executables and always names it, so no session reaches the
  network or writes into the operator's home; whether karvi's host-key policy,
  today inside karvi's own SSH handshake ([`SECURITY.md`](SECURITY.md)), has a
  place in v2 beyond a known-hosts file; the adapter's shape, with the session
  in the library and not shared with `system`
  ([`docs/TRANSPORT-DRIVER-ARCHITECTURE.md`](docs/TRANSPORT-DRIVER-ARCHITECTURE.md));
  and what it gains for a fleet karvi qualifies on `cisco_ios` over SSH
  (NETCONF, Telnet, more platform definitions). v2 would be a transport with
  its own selector beside `system`, `native`, and `telnet`; a mapping from a
  slot to an implementation comes back only if a transport ever has two
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
- **Jump hosts over the system transport.** A `ProxyJump` in `~/.ssh/config`
  (`ssh.include-user-config`) makes the hop a child `ssh -W` that inherits the
  askpass environment, so the jump host's prompts reach the broker and are
  answered from the device's credential; the hop authenticates first, so it
  takes the fields it asks for, and the device's own ask of one of them then
  ends the connection as a second ask (`authentication_failed`; outside a login
  such a bastion works today only where the device then takes a key). To
  decide: whether a
  hop's prompts are answered at all (the broker could require the device's own
  HostKeyAlias in OpenSSH's `user@host`, refusing the jump host's, which would
  end a bastion that shares the device's credential today) or from a credential
  of the hop's own; and a change prompt or a second ask at the jump host ending
  the hop's process: the broker's hook ends the device's OpenSSH, and the hop's
  own process still sends the jump host OpenSSH's empty answer. In `karvi login`
  the operator answers a second ask, a change prompt, and a prompt no list
  matches at the terminal instead, the jump host's and the device's alike
  (reasoned, not executed). The fake device cannot yet be a jump host.
- **Authentication in stages.** A server may require methods in turn
  (OpenSSH's `AuthenticationMethods`, each stage a partial success): a Linux
  server a key and then one or more username or password answers, to pursue
  (Rob), or a password and then a second factor. Executed against a scratch
  device, karvi logs in on no such chain over either transport, where plain
  `ssh` does: a credential with keys holds no password, so after the key
  neither transport has a method left; a later stage asking for the password
  or a code is a second ask (`device refused password`); and OpenSSH, at
  `NumberOfPasswordPrompts 1` and its count kept across a partial success,
  does not run keyboard-interactive a second time, so in `karvi login` the
  operator is not asked. The transports differ only there: native runs it
  again and answers a prompt no list matches empty, a failed attempt the
  device counts; system sends nothing, a command's error led by OpenSSH's
  `Authenticated using "keyboard-interactive" with partial success.` After a
  refusal neither runs keyboard-interactive again. Waits on the key and
  password credential agreed in [`docs/EXAMPLES.md`, chapter
  24](docs/EXAMPLES.md#24-jobs-across-linux-servers-2026-10-04) (the key
  first, the password on partial success; the credential CSV's `keyfile`),
  not built; the tree's fake device answers no partial success.
- **One message for a refused authentication on both SSH transports.** A
  refused key, or a password credential with both password methods off, fails
  `authentication_failed` (exit 108) on both transports, each in its own words:
  over `system` OpenSSH's
  `Permission denied (password,publickey,keyboard-interactive).`, over `native`
  x/crypto's `attempted methods [none publickey], no supported methods remain`
  for the key (the parity suite's S13c), `[none]` with both methods off
  ([`docs/EXAMPLES.md`, chapter
  107](docs/EXAMPLES.md#107-prompt-answers-on-both-ssh-transports-2026-11-11),
  case 1). A refused password already reads alike, `device refused password`.
  The question: one short message for both, OpenSSH the benchmark.
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
  The question: one function in `scripts/lib` that runs a call and, when it
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
  jobs (in thirty days the suites and the labs left 170,844 per-command
  events there: [`docs/EXAMPLES.md`, chapter
  63](docs/EXAMPLES.md#63-the-audit-trails-names-and-fields-2026-10-17)). An
  audit review would show them as work done. The item: no test or suite
  writes to the host's journal. The questions: how a test's sink is kept
  from journald, how a suite's is, and the audit suite's, whose subject is
  the round trip through journald, its jobs at devices named for its run
  alone ([`docs/EXAMPLES.md`, chapter
  64](docs/EXAMPLES.md#64-karvi-audit-the-audit-trail-as-a-table-2026-10-17)).
- **The spool suite's timeout row waiting on a condition.** The spool suite's S4
  cuts `show big` at a one-second command timeout and needs 1 MiB settled by the
  cut. The fake builds its 65.7 MB answer before it sends it, and what settles
  by the cut follows the disk under the spool: of 48 cuts at the fake with the
  spool on `/var/tmp`, all but one settled 12 to 48 MB and one 141,422 bytes,
  and a battery failed the row with nothing settled, the suite alone once in
  three ([`docs/EXAMPLES.md`, chapter
  79](docs/EXAMPLES.md#79-a-command-file-per-target-2026-10-25)). The item: the
  row asserts the cut's settled bytes whatever the disk's pace. The questions: a
  cut timed from the answer's first byte rather than from the send; and the
  row's threshold. The cancel and the lost session beside it cut once the
  scoreboard counts the answer's bytes and check that some settled.
- **The release's signature.** `karvi-sysutil package update` checks the
  package against its `.sha256`, which comes from the same release over the
  same connection: it finds a damaged download, and proves nothing of where
  the package came from. Under `-k`, a proxy that inspects TLS, or anything
  that answers for github.com inside the network, can serve another package
  with a checksum that matches it, and `apt-get` runs its maintainer scripts as
  root. The item: the release signs `karvi-vX.Y.Z-artifacts.sha256` with an
  SSH key (`ssh-keygen -Y sign`) and publishes the `.sig` beside it; the package
  ships the public key as `/usr/share/karvi/release-signers`, in
  `allowed_signers` form; and the update verifies the signature with
  `ssh-keygen -Y verify` (`openssh-client` is already a dependency) before it
  reads the package's sum from the signed file. Trust begins at the first
  package installed by hand, which brings the key. A lab of 2026-10-27 signed
  0.36.0's aggregate checksums with a throwaway key: the file verified, exit
  0, and the same file with one sum changed failed, `incorrect signature`,
  exit 255. The questions: where the key is kept and the release step that
  signs, in `release-tools`; a key's change (a package carrying both keys
  before the old one goes); and whether the bundle's `verify-bundle.sh` checks
  the signature too. *Not taken:* GPG or minisign (another tool and its
  keyring), GitHub's attestations (`gh` on every host).
- **The package update on a timer.** `karvi-sysutil package update` runs when
  the administrator runs it. A timer under `/usr/share/karvi`, inactive as the
  others are, would run it on a schedule. The questions: whether the timer
  installs or only reports (`--check`); where its `--proxy` and `-k` are kept,
  a timer having no command line (a file under `/etc/karvi`); and whether an
  unattended install restarts the instances of `karvi-daemon@.service` it
  names, a restart cancelling what outlasts the grace. It waits on the
  release's signature: an install no one watches should not rest on a
  checksum alone.
- **A FIPS 140-3 mode, once Go's module performs AES-256-GCM for SSH.** karvi
  leads with the strongest algorithms its transports implement. FIPS 140-3 is
  not the strongest set: it is the set one implementation, submitted and
  passed in testing for federal data protection, performs in its approved
  mode. An option for it waits until that set holds AES-256-GCM. Go 1.27's
  certified module (`v1.0.0-c2097c7c`) has an SSH form of AES-GCM
  (`NewGCMForSSH`, RFC 5647's nonce) inside the standard library alone;
  x/crypto's SSH package (v0.58.0) calls `crypto/cipher.NewGCM`, which the
  module counts as unapproved under `GODEBUG=fips140=on` and refuses under
  `fips140=only`. The approved cipher left is AES-CTR, which karvi does not
  lead with. The item opens when a `GOFIPS140=certified` build negotiates
  `aes256-gcm@openssh.com` under `fips140=only`; what the design then starts
  from is in [`docs/EXAMPLES.md`, chapter
  101](docs/EXAMPLES.md#101-a-fips-140-3-mode-waits-on-aes-256-gcm-for-ssh-2026-11-05).
- **Every shell script in bash, under one header.** The scripts stay POSIX sh
  after the shell-script review, most under `#!/bin/sh` (dash), eight under
  bash, two of them through `/usr/bin/env`; the review's fixes put their
  shared code in `scripts/lib` functions whose bodies a conversion changes and
  whose callers it does not ([`docs/EXAMPLES.md`, chapter
  102](docs/EXAMPLES.md#102-the-shell-script-review-of-2026-11-06-and-its-fixes-2026-11-07)).
  The item: every script under `#!/bin/bash` (not `/usr/bin/env bash`: the
  PATH would choose the interpreter of root's and cron's scripts, and lintian
  refuses env in a maintainer script, `unknown-control-interpreter`), `set
  -euo pipefail`, and `shopt -s inherit_errexit`, without which bash's `$( ...
  )` ignores `set -e` where dash's stops. Its test: the battery passes, and
  every suite still fails a broken expected value. Its method: four batches of
  about a quarter of the lines each; per batch a converter in its own
  worktree, changing the header and what it forces alone, with three hazards
  checked in each script (a `$( ... )` of several commands, a pipe into `grep
  -q` or `head` under pipefail, an `echo` reading escapes), and a different,
  adversarial reviewer given the diff alone, who breaks one expected value per
  suite and writes a case per hazard site; the suites run under three `flock`
  slots; the orchestrator checks every issue, merges, and runs the battery,
  the container suite, and the release path in a copy.
- **A placeholder set per template.** Every display template is checked at
  load against one set of placeholders, and a placeholder its line has no
  value for renders empty, `0` for a count or an exit code, `0s` for
  `<elapsed>`, and the time now for `<timestamp>`: `"! <target> (<count>)"`
  as `display.gather.header` draws `!  (2)`, the name empty
  ([`docs/DISPLAY-CONFIGURATION.md`, "The gathered display's
  header"](docs/DISPLAY-CONFIGURATION.md#the-gathered-displays-header)). The
  item: each template its own set, and a placeholder the template can never
  fill refused at load as an unknown one is, `display_placeholder_unsupported`,
  a hard stop. What it gains: a mistyped template refused, not drawn blank.
  It changes the rule of every template, the eleven
  `internal/configload/validate.go` checks, the released ones among them: a
  design of its own.

## Competitive features to consider for later

What other network tools do that karvi does not, set down at the operator's
word for later consideration, in no order: none is chosen or designed. Two
such capabilities are items under [Later](#later) already: platforms beyond
the built-ins (Netmiko and Oxidized each reach far more device types than
karvi's seven) and NETCONF. An item that would reopen a settled decision of
[`docs/DESIGN.md`](docs/DESIGN.md) says so.

- **Structured output.** Netmiko and Nornir hand a command's text to a parser
  (TextFSM templates, TTP, Cisco's Genie) and return its fields; NAPALM's
  getters return a device's facts, interfaces, and neighbours as data. karvi
  records a command's text as the device sent it (`commands.jsonl`,
  `schema/command-record.schema.json`), and a tool outside karvi parses it.
  What it gains: a record a script or a report reads without a parser of its
  own. It would reopen the rule that the daemon never interprets output
  ([`docs/DESIGN.md`, section
  7](docs/DESIGN.md#7-the-plan-the-daemon-and-the-credential-channel)); a
  parse in the client, after the records are durable, would leave the daemon
  as it is. The questions: the parser and its templates (NTC's TextFSM
  templates are the common set, written for Python's TextFSM); where the
  fields go, a field of the record or a file beside it; and a template that
  no longer matches a release's changed output.
- **Checks before and after a change.** pyATS (Genie's `learn` and `diff`)
  takes a snapshot of a device's state and compares two; Ansible asserts on a
  result. karvi compares configurations as text (`crun`, its filters, and the
  `crun.after` hook's git diff), and nothing compares operational state
  (neighbours, routes, interface states) or fails a run on a check. What it
  gains: a change window's proof that the network came back as it was. The
  plainest form from what exists: two jobs of the same commands, before and
  after, compared per device and command from their records. The questions: a
  karvi command or a tool's (the records are already the input); the lines
  that change at every run (the collection's filters are the precedent); and
  whether a failed check holds the next wave.
- **Desired-state configuration.** Ansible's `ios_config` reads the running
  configuration and sends only the lines it lacks, and its check mode shows
  that difference without a change; NAPALM's `compare_config` shows a
  candidate's difference before the commit. karvi sends the commands the plan
  lists, and `--dry-run` shows those commands, not what they would change on
  the device. What it gains: a change sent twice that does nothing the second
  time, and a review of the device's own difference. The questions: where the
  running configuration comes from (the session's own `show running-config`,
  or the device's collection file); IOS's sections and indentation, which
  tell a line missing under its parent; and how a plan that depends on the
  device's answer stays a complete statement of intent (section 7's rule,
  reopened).
- **Configuration rollback.** NAPALM replaces a candidate configuration and
  rolls it back, Cisco NSO's transactions roll back, and Junos's `commit
  confirmed` reverts unless confirmed. IOS XE has `configure replace` and the
  archive's `configure terminal revert timer` with `configure confirm`; karvi
  sends them as any command and has no workflow around them. What it gains: a
  change that reverts itself when the device stops answering, the one undo a
  remote change has. The questions: the IOS XE form (the revert timer, or a
  replace file); the confirmation as the job's last command, after a check;
  what a device not reached in time records; and whether the archive's
  settings are the site's configuration or karvi's to set.
- **Templates and decisions within a run.** Ansible and Nornir render Jinja2
  templates over a device's variables and branch on a result (`when`,
  `register`). karvi renders no template, and a decision from a device's
  answer is taken between jobs by a tool outside karvi ([`docs/DESIGN.md`,
  section
  7](docs/DESIGN.md#7-the-plan-the-daemon-and-the-credential-channel));
  macro files (`--mf`, under [Later](#later)) are the planned middle ground,
  plain substitution from the inventory row. The item would reopen that
  decision. It waits on `--mf`, and on a change a second job cannot serve.
- **Inventory from a source of truth.** Nornir's and Ansible's inventory
  plugins read NetBox or Nautobot. karvi's inventory sources are CSV files
  ([`docs/DESIGN.md`, section
  3](docs/DESIGN.md#3-targets-inventory-and-platforms)). What it gains: one
  list of devices, kept where the site keeps it, with no export between. The
  plainest form needs nothing of karvi: the site's scheduled export to CSV;
  the item is a source type that reads the API. The questions: the API's
  token among the credentials, a run while the API is down, and the mapping
  of its fields onto karvi's columns. It waits on a site that runs one.
- **RESTCONF, gNMI, telemetry, and SNMP.** IOS XE answers RESTCONF (YANG
  over HTTPS), gNMI, and SNMP, and streams model-driven telemetry. karvi
  speaks SSH and Telnet, NETCONF being an item under [Later](#later). What it
  gains: state read as data without a parser, and counters at an interval no
  CLI session gives. The questions: which first (RESTCONF needs an HTTPS
  client and the device's certificate trust, gNMI gRPC); whether telemetry is
  karvi's at all, or a collector's into a time-series store; and SNMP's
  place beside the CLI. It waits on NETCONF, the nearest.
- **File transfer and image upgrades.** Netmiko's `file_transfer`, Ansible's
  `net_put`, and NAPALM copy files to a device over SCP; an IOS XE upgrade is
  a copy, a verify (`verify /md5`), an install (`install add file ...
  activate commit`), and a reload. karvi has no file transfer. What it gains:
  upgrades, a large share of a fleet's change windows, under karvi's waves,
  records, and audit. The questions: SCP from karvi, or the device's own
  `copy` from a server, sent as a command; the image's checksum as a gate;
  the reload, and the session that waits through it; and the free space on
  the device's flash.
- **A web interface, roles, approvals, and an API.** AWX (Ansible's
  automation controller) and Cisco NSO give a browser view, roles per team,
  an approval before a job runs, and a REST API a ticketing system calls.
  karvi is the command line, the watch screen, and the audit trail; who may
  run what is the Unix groups of the shared trees and the service platform's
  account ([`docs/OPERATIONS.md`](docs/OPERATIONS.md)). What it gains: a
  change approved and run from its ticket, and staff who do not use a shell.
  The questions: an API over the daemon's IPC, local and per user today, or a
  service beside it; authentication and roles; and what a remote channel asks
  of the credential package (`Sealed` credential packages, under
  [Later](#later)).
- **Configuration compliance.** SolarWinds NCM checks configurations against
  a site's rules, and Batfish analyses them offline (reachability, ACLs).
  karvi collects configurations and its hook diffs them; no rule is checked.
  What it gains: a report of the devices that break a standard (the NTP
  servers, AAA, the banner) from files karvi already writes. The plainest
  form: a `crun.after` hook running the site's checks over the collection
  directory, which needs nothing of karvi but a shipped example. The
  questions: the rules' form, a karvi command or an example hook, and the
  report's place.
- **ClusterShell's node sets and gathered output, beyond karvi's.**
  ClusterShell's `clush` runs a command over a node set and, under `-b`,
  gathers the nodes that answered alike under one header naming the folded
  node set and its count; `clubak` gathers `NAME: line` output read from
  another tool, `-bL` writing each line after its group's folded names. karvi
  takes that much, the gathered display and line mode
  ([`docs/OPERATIONS.md`, "Gathered output and line
  mode"](docs/OPERATIONS.md#gathered-output-and-line-mode)), and leaves the
  rest: `--diff`, a unified diff between the gathered outputs, where karvi
  shows each group whole and a diff is post-processing or a wrapper reading
  line mode's `NAME: line`, split at the first `: `; the node-set operator
  `&`, keeping the nodes two sets share, where karvi's selectors add up in
  command-line order and a `!` value removes, so no line keeps only the
  devices two selectors share ([`docs/DESIGN.md`, section
  3](docs/DESIGN.md#3-targets-inventory-and-platforms)); node groups in the
  header (`-r`, `--regroup`, writes `@group` for a group's nodes), where
  karvi's header names devices; two digit runs folded at once (`nodeset -f`
  writes `pod[1-2]-sw[1-2]`), where karvi folds one,
  `pod1-sw[1-2],pod2-sw[1-2]`, a rule of the fold this would reopen
  ([`docs/DESIGN.md`, section
  12](docs/DESIGN.md#12-display-and-help)); `--pick N`, N nodes of the set at
  random, where karvi orders a set at random (`--order random`) and runs
  every device of it; and the fold outside the display, where `job list`'s
  TARGET column names devices whole and counts the rest `+N` and the watch
  screen's names a target file, then the devices in flight, then the rest.
  What it gains: what differs between two groups, seen without reading both;
  a selection by two inventory fields in one line; shorter headers on a fleet
  named by group or on two axes; a sample of a fleet for a check; a job's
  whole scope in one column. The questions: a diff's reference (the largest
  group, or the first), and a karvi option or a shipped wrapper; the AND
  operator's spelling on karvi's line and its order beside `!`; the inventory
  field a group's name comes from, and whether a header naming a group still
  reads back as one `--tl` value; when two runs fold, and in which order; a
  pick recorded, so a rerun picks the same devices, as the recorded
  `dispatch.shuffle-key` reproduces a `random` order; and a fold that is
  never cut in a column that cuts at the terminal's width.

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
