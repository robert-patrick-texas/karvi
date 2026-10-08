# Changelog

## Unreleased

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
  the install.
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
