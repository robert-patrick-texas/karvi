# Command-session behavior

## Grammar

Command mode supports a positional target and positional command words or one
or more explicit commands:

```bash
karvi command router1 show clock
karvi cmd router1 --cmd 'show clock'
karvi command --host router1 --command 'show clock'
karvi command --cmd 'show clock' --target router1 --command 'show version'
```

`--cmd` and `--cf` mix and repeat, and the commands go in the order written: a
`--cmd` its command, a `--cf` its file's lines, as written (`--cf -` reads
standard input, once). A `--cf` given a folder sends each device its own file
in it, `DIR/NAME`, NAME its collection file's
([`docs/OPERATIONS.md`](OPERATIONS.md#a-command-list-per-device)). This sends
`show clock`, the lines of `checks.txt`, and `show users`:

```bash
karvi command router1 --cmd 'show clock' --cf checks.txt --cmd 'show users'
```

`--sudo TEXT` and `--sleep DURATION` are sources in the same order: a command
run as root where the platform runs on the exec channel ([root
commands](#root-commands-on-an-exec-channel)), and a wait before the next
command ([sleeps](#sleeps-between-commands)).

A line's one `--cmd` or `--sudo` may take its command unquoted: the words
right after its value, to the line's end, join it with one space each, as the
freeform command's words do, every word after the first the command's, a
karvi option's name among them. The shell's quotes are gone by then, so a
command whose own words need quotes is quoted whole. A command's own option right after its first word would be read as
karvi's, so after `--cmd --` or `--sudo --` every word to the line's end is
the command; that form may also end a line of other commands:

```bash
karvi command srv1 --sudo systemctl restart nginx
karvi command srv1 --cmd 'systemctl is-active nginx' --sudo -- journalctl -u nginx -n 20
```

Options go before such a command (`--timeout 60s --sudo systemctl restart
nginx`). Unquoted words beside another command, without `--`, are refused
(`cli_command_text_mixed`), and an unknown option right after a command's first
word names both fixes:

```text
cli_option_unknown: unknown option -u in command; for the command's own option write --sudo -- TEXT, or quote the command
```

`--target` and `--host` are target synonyms. `--address` aliases
`--management-address`; neither address option supplies the required target:

```bash
karvi command --target router1 --address 192.0.2.10 \
  --cmd 'show clock'
```

Recognized options and the target may occur before, after, or between command
material. With positional command words, known karvi options are still parsed;
remaining words are joined into one command. Use `--` to protect literal text
that contains a karvi option name:

```bash
karvi command router1 -- show running-config | include --echo
```

## History: one shell, since v0.7.0

The v0.6 command path prepared an OpenSSH ControlMaster and requested a new
session channel per command. Cisco IOS XE accepts the authentication and refuses
the extra channel (`Master refused session request: Permission denied`), which
the text classifier then read as an authentication failure. Since v0.7.0 the
command path opens one fresh interactive shell per device and sends every
command through it; that diagnostic, should it appear, is
`ssh_session_channel_refused` in the `connection` category, never
`authentication_failed`.
[`docs/TRANSPORT-DRIVER-ARCHITECTURE.md`](TRANSPORT-DRIVER-ARCHITECTURE.md)
states the ControlMaster rule. A platform whose `channel` is `exec` (built-in
`linux`) takes a channel per command again, on a device that accepts it,
under its own rules ([The exec channel](#the-exec-channel)); the shell stays
the path for every other platform.

## Lifecycle

The session (`internal/devsession`) is the same on both SSH transports;
only the connection beneath it differs. Each bound is one configuration
key, listed with its relations in [`docs/TIMEOUTS.md`](TIMEOUTS.md).

```text
resolve target/address/credential/transport/platform definition
  -> open the connection, the host-key policy in the handshake
       system: private OpenSSH config, one-use askpass, one fresh ssh -tt process
       native: one x/crypto connection, no subprocess
  -> wait for the first prompt            (execution.prompt-timeout; system adds ssh.connect-timeout)
  -> escalate once when below the level    (execution.enable-timeout, the whole step)
  -> paging commands, each to the prompt   (execution.prompt-timeout each)
  -> session-init profile, if one matches  (its command-timeout or execution.command-timeout each)
  -> for each requested command:           (execution.device-timeout over the whole list)
       write the command + newline (or the blind returns with it)
       answer declared prompts as they appear (--expect)
       read until the prompt returns        (its command timeout, or execution.blind-wait when blind)
       remove the echo and the trailing prompt
       emit one command record
       stop or continue according to the device-error policy
  -> the definition's exit-commands, then close
       (native waits up to 2 s for the shell's end)
  -> remove temporary config and askpass material (system)
```

Keepalives prove the peer alive throughout (`ssh.server-alive-*` on
`system`, `native-ssh.keepalive-*` on `native`); a device gone silent
ends as `session_keepalive_timeout`. A session that has failed (a timeout,
a lost read, a write failure, output over the limit before the prompt
returned) is aborted, not closed: nothing is sent on a desynchronised
shell, no replacement login is made, and the remaining commands receive
explicit not-attempted records.

## One session for command and run

The shell above is the device session for
`command` and for `run` over the system transport alike, and the same
session logic drives the native transport's connection (`internal/devsession`).
The session adds two steps between the first prompt and the first requested
command, both from the platform definition:

1. **Privilege.** The first prompt is matched against the platform's
   prompt levels (`cisco_ios`: `exec` at `>`, `privilege-exec` at `#`,
   `configuration` at `(config)#`). A shell that starts below the
   platform's `privileged-level` gets one `enable`, the enable secret at
   the password prompt, and must show the privileged prompt within
   `execution.enable-timeout` (one deadline across the secret's prompt and
   the level's prompt); anything else is `privilege_failed`, the session ends,
   the first command records `privilege_error` and the rest
   `not_attempted_prior_command_failure`. A shell that starts at the level
   or above it is left where it is; nothing ever steps down. A platform
   without levels (`generic`, `linux_shell`) has no privilege step.
2. **Paging.** The definition's `paging-commands` in order (the platform's start
   statements: `generic` has none until a `[platform.generic]` table gives it
   some, [`docs/OPERATIONS.md`](OPERATIONS.md) ["Start statements for generic
   devices"](OPERATIONS.md#start-statements-for-generic-devices)), each read to
   the prompt; a rejected one is `paging_disable_failed` and the device's first
   record is `device_error`.
3. **Session-init.** A `[session-init.NAME]` profile (`commands`,
   `on-error` = `fail-device` or `continue`, `command-timeout`), selected
   for the device by `[[session-init-map]]`, runs its commands as the
   platform's own before the requested ones. Under `fail-device` a failed
   command ends the device: the requested commands are recorded
   `not_attempted_session_init_failure`. Under `continue` the device goes
   on and its first requested record carries the notice
   `session_init_command_failed` naming the profile, the command's index,
   and the code. A profile's commands are device text alone: an entry
   `--sudo …` or `--sleep …` reaches the device as written and fails there
   (on a server `sh: 1: --sudo: not found`, exit 127), under the profile's
   `on-error`.

Then the requested commands, then the definition's `exit-commands` at close.

## The exec channel

A platform whose `channel` is `exec` (built-in `linux`, or any
`[platform.NAME]` table that says so) gets one connection for its command
list and one exec channel per command on it, in order, with no pty and with
standard input at its end ([`docs/DESIGN.md`, section
5](DESIGN.md#5-transports-and-the-device-session)):

```text
resolve target/address/credential/transport/platform definition
  -> open the connection, the host-key policy in the handshake
       system: one ssh -M -N master per device, its stderr read at DEBUG1;
               ready when it answers -O check
       native: one x/crypto connection; ready when it has authenticated
                                           (ssh.connect-timeout and execution.prompt-timeout)
  -> session-init profile, if one matches  (each command on its own channel)
  -> for each requested command:           (execution.device-timeout over the whole list)
       open an exec channel and start the command
         system: one ssh -S client of the master, LogLevel QUIET, ProxyCommand false
         --sudo: sudo -S -p PROMPT -- TEXT, the password at the prompt;
                 sudo -n -- TEXT without one
       read stdout and stderr, each spooled past the threshold on its own
       wait for the exit status or signal   (its command timeout, from the channel's opening)
       emit one command record
       stop or continue according to the device-error policy
  -> close: system ssh -O exit, the master killed on an abort;
            native closes its connection
```

There is no first prompt, no privilege step but a root command's own `sudo`
([below](#root-commands-on-an-exec-channel)), no paging command, and no exit
command, and nothing carries from one command to the next (`cd`, variables,
`umask`): the remote shell is not interactive. Blind sends, `\r` endings,
`--blind-return`, and `--expect` are refused at planning for an exec target
(`channel_exec_declaration_refused`, exit 4); `--literal`, `--timeout`, and
`--maxbytes` are accepted.

How a command ends is its exit status: 0 is `success` whatever stderr holds,
then the platform's failure patterns are searched in both streams; a non-zero
status is `command_exit_nonzero`, a signal `command_exit_signal`, and a channel
closed without a status on a live connection `command_exit_missing`, each a
device error. On `system` a client's exit of 255 is read from the master's
lines: the command's own 255, a signal (`exit_signal` `unnamed`, since OpenSSH
names none), a channel closed without a status, or the master gone, the
session's failure. A device that refuses the channel or the exec request is
`ssh_session_channel_refused`; the command did not run and the session ends.

A command timeout, a cancel, and the output limit (stdout and stderr counted
together) stop the command and close its channel, and the connection serves
the next command: `native` asks the device for `KILL` first; `system`,
whose client cannot send a signal, kills the client and records the notice
`remote_command_not_stopped`, the command possibly still running on the
device.

## Blind sends and expectations

A command that ends in `\r` sequences, or is given `--blind-return N`, is
sent with that many carriage returns in one write and awaited for
`execution.blind-wait` in place of its command timeout; a prompt that does
not return is a success with the notice `prompt_not_observed_after_blind_send`
and all bytes read, the session closed. `--literal` sends the text as
written. `--expect PATTERN=RESPONSE` answers a device prompt the platform's
pattern does not match, as it appears, consumed once in declared order;
`--blind` declares that the prompt may not return. Every declaration
attaches to the `--cmd` written right before it, or to the freeform command;
one written after a `--cf` is refused (`declaration_with_commands_file`), as a
file line takes `\r` at its end. The waits are
[`docs/TIMEOUTS.md`](TIMEOUTS.md).

## A command's own bounds

`--timeout DURATION` and `--maxbytes BYTES` are declarations too, at most one
of each per command: the command's timeout in place of
`execution.command-timeout` (and over telnet of `telnet.read-timeout`), and
its output limit in place of `output.max-command-bytes`, smaller or larger,
on the shell and on an exec channel alike. The rest of the device's list keeps
the job's values. A limit reached ends the command as the job's limit does,
the first BYTES kept: the shell's session is closed, an exec channel's command
stopped with the connection serving the next. The messages name the source,
`(--timeout)` or `(--maxbytes)` beside the keys' names. A blind command takes
no `--timeout` (its wait is `execution.blind-wait`); a timeout above a set
`execution.device-timeout`, or a limit above `output.max-job-bytes`, is refused
at planning ([`docs/TIMEOUTS.md`](TIMEOUTS.md),
[`docs/OPERATIONS.md`](OPERATIONS.md#long-commands-a-copy-and-a-show-tech)).

## Sleeps between commands

`--sleep DURATION` waits before the next command: it is a command source in
the order written, as `--cmd` and `--cf` are, and the wait begins once the
command before it has returned its prompt (or, before the first command, once
the session is prepared). In a commands file, a line that is `--sleep
DURATION` alone, or `--sleep=DURATION`, spaces around it allowed, is the same
statement; every other line is device text as before, and `--literal` sends
the line as written. This clears a peer, waits a minute, and reads the result:

```bash
karvi command router1 --cmd 'clear ip bgp 192.0.2.1 soft' --sleep 1m --cmd 'show ip bgp summary'
```

and so does a file holding the three lines `clear ip bgp 192.0.2.1 soft`,
`--sleep 1m`, and `show ip bgp summary`. A sleep is 1s..12h; sleeps in a row
add up; a sleep is no command and has no record, and `--debug` writes `device
sleep target=… index=… total=… sleep=…` as each begins. Each is refused before
any connection: one with no command after it (`sleep_without_command`), a
declaration written after one (`declaration_after_sleep`; it belongs right
after its `--cmd`), a file line whose first word is `--sleep` and is not the
statement (`commands_file_sleep_invalid`, naming the file and line, so a
mistyped sleep never reaches the device as text), and freeform text beside one
(`cli_command_text_mixed`). The sleeps count against `execution.device-timeout`:
their total above a set one is refused at planning (`sleep_over_device_timeout`),
and the deadline ends a sleep, the next command then not sent
(`device_timeout`); a cancel ends a sleep at once. A sleep longer than the
device's own idle limit (IOS `exec-timeout`, 10 minutes by default) loses the
session: the transport's keepalives are not input, so the device logs the
session out and the next command fails on it.

## Root commands on an exec channel

`--sudo TEXT` runs one command as root through the server's `sudo`: a command
source in the order written, as `--cmd`, `--cf`, and `--sleep` are,
repeatable, on `command`, `run`, and `crun`, and a stream line. Its text is
written as a `--cmd`'s: quoted, or unquoted as the line's one command, or after
`--sudo --` ([the grammar](#grammar)); `--timeout` and `--maxbytes` written after
a quoted one are its own. In a commands file, a line `--sudo TEXT` or `--sudo=TEXT`, spaces
around it trimmed, is the same statement, and `--literal` sends every line as
written. This checks a service as the login user, restarts it as root, and
checks it again:

```bash
karvi command srv1 --cmd 'systemctl is-active nginx' --sudo 'systemctl restart nginx' --cmd 'systemctl is-active nginx'
```

`--sudo` reaches a target whose platform runs on the exec channel (`linux`,
or a `[platform.NAME]` table on its base): a target on a shell channel, every
network platform and `linux_shell` among them, is refused at planning,
`channel_shell_sudo_refused`, naming the target and the command's index (the
shell's form is not built: [`ROADMAP.md`](../ROADMAP.md), Later). Refused before
any connection too: unquoted words beside another command
(`cli_command_text_mixed`), a blank `--sudo` (`cli_option_value_invalid`), and
a file line whose first word is `--sudo` with no command after it
(`commands_file_sudo_invalid`, naming the file and line).

**The password** is chosen per device at planning, by the source that
answered its credential: a backend's enable field, as it is (blank, none: a
variable never fills it, and a row names one as `env:NETSUDO`); the platform's
fallback, `NETSUDO`, else `NETENABLE` where the fallback holds `netvars`, else
none. Nothing asks at the terminal. `NETSUDO` is read, and an exec device's
enable field carried to the session, only for a device with a `--sudo`
command; a platform on the exec channel with `requires-enable` requires the
password of those devices alone (`credential_enable_missing`). The sources are
[`docs/OPERATIONS.md`](OPERATIONS.md#root-commands-and-their-password)'s.

**On the wire** the command is `sudo -S -p 'karvi-sudo-<16 hex>:' -- TEXT`, the
prompt random per command and the input left open: when the prompt appears on
stderr, karvi writes the password and a line end once and closes the input.
Without a password it is `sudo -n -- TEXT`, the input at its end, and a `sudo`
whose rules want one refuses at once, `command_exit_nonzero` with its message
in stderr (`sudo: a password is required`; sudo-rs, Ubuntu 26.04's `sudo`,
`sudo: interactive authentication is required`). There is no `sh -c`: the
server's rules match the command and its log names it, and the rest of the
line after the first command, a `;`, a pipe, or a redirect, runs as the login
user, as `sudo TEXT` typed at a prompt does (`--sudo 'whoami; whoami'` prints
`root`, then the login user). A whole line as root is `--sudo "sh -c '…'"`, which
the rules must allow for `sh`. Written unquoted, `--sudo sh -c '…'` is two
commands: `-c` is `--cmd`'s alias and begins the next, which runs as the login
user (the dry run counts both, and the display shows the second at `srv1$`).

**The prompt** is removed from the recorded stderr in both forms `sudo` writes
it: classic `sudo`'s, the prompt as given, and sudo-rs's, `[sudo: PROMPT]
Password: ` and its line end. A second prompt says `sudo` refused the
password: the input is closed, the command did not run, and the record is
`privilege_failed`, `sudo`'s own lines in its stderr:

```text
privilege_failed: sudo asked for the password a second time, refusing the one given; the input was closed and the command did not run
```

**What shows it.** A root command is shown at `srv1#`, where a command of the
login user is at `srv1$`: in the text display, `--echo`, `output.TARGET.txt`,
and `--live`, and one not sent is `! not sent: --sudo TEXT`. Its record keeps
the command as written and adds `privilege: "sudo"`, as the audit's action
does, and `karvi-audit`'s row shows `--sudo TEXT`; a collection's marker is `!
--sudo TEXT`; a job's rerun files write it back as `--sudo TEXT`. The dry run names the root
commands and each device's password source:

```text
commands: 3
sudo: commands 2, 3
- name:srv1: planned
  credential: bound ID (policy=default backend=builtin-env-fallback user=netops; value not displayed)
  sudo: password from NETSUDO (value not displayed)
```

or `sudo: password from the enable field (backend=NAME; value not displayed)`,
or `sudo: none, sudo -n`. `--debug` adds `sudo=password` or `sudo=none` to
the command's start line, and `device sudo answered target=… index=…` once
the password was written.

**A caution.** Where a password is held and `sudo` asks for none (a
`NOPASSWD` rule, or a ticket still valid), the input stays open: a command that
reads its input with no file named (`head -c1`, `cat`, a script that asks)
waits for it to its command timeout. Name its input, or bound it with
`--timeout`.

## Prompt detection

The session renders the received bytes as the terminal showed them
(`internal/termtext`, at no width: carriage returns, backspaces, and erases
applied, colours, window titles, and every other sequence dropped) and matches
the unfinished last line, without its trailing blanks, against the platform's
prompt patterns: the levels' patterns for a platform that declares them
(scrapligo's platform definitions, carried in `platform.go` for the
built-ins), `generic`'s prompt pattern otherwise, and a broad line ending
in `#`, `>`, or `$` for a platform with neither. A candidate prompt must
remain the final content for a short settle period so a banner line ending
with a prompt character is not accepted prematurely. The telnet transport
matches the same way, its login's `Username:` and `Password:` questions and
the enable step's `Password:` included: only on the last line, once it has
stood for the settle period. The prompt timeout and command timeout remain
bounded configuration values.

The collected response is the rendered text: one echoed command line and
the trailing prompt removed, every line ending in `\n`, a space the device
wrote kept on an inner line, and the last line before the prompt without its
trailing blanks. The prompt the command was typed at holds its columns on the
first line and is no part of the text. A repeated line matching the
command later in legitimate device output is not silently discarded. A
device-reported command error is the platform definition's failure
patterns (`% Invalid input detected` and its siblings for `cisco_ios`);
`generic` reports none.

## Echo

```bash
karvi command router1 --echo --cmd 'show clock'
```

or:

```toml
[display.command]
echo = true
```

prints the prompt the statement was sent at plus the command before each
result, including a device-reported command error, as the job's text file
shows it (`router1#configure terminal`, then `router1(config)#end`):

```text
router1#show clock
01:20:19.849 EDT Thu Sep 10 2026
```

On an exec target the prompt is inferred from the target's name, since the
device sends none, and stdout comes before stderr, their interleaving lost
(`2>&1` in the command keeps it):

```text
srv1$ echo out; echo err >&2
out
err

srv1$ ls /nonexistent
ls: cannot access '/nonexistent': No such file or directory
karvi: target=srv1 status=device_error error=command_exit_nonzero: exited 2
```

A root command is shown at `srv1#` (`srv1# systemctl restart nginx`).

The command record persists `promptbefore`, `prompt`, `prompt_source`, and
`prompt_observed`; an exec record's are empty, `none`, and false, and it carries
`channel`, `exit_status`, `exit_signal`, and `stderr` with its encoding, count,
and digest instead. `promptbefore` is the prompt the device showed when the
statement was sent and `prompt` the one that came back after it: for `configure
terminal` they are `router1#` and `router1(config)#`. Both are the device's own
bytes, never inferred; `promptbefore` is empty only on a record whose statement
was not sent, and `prompt` is empty when no prompt came back (a timeout, a blind
send). When an error proves the command was sent but prompt metadata is
unavailable, human output uses a deterministic inferred prompt and the record
remains marked inferred. JSONL is never decorated.

## Live view

```bash
karvi command router1 --live --cmd 'show drip'
```

prints the device's bytes on standard output as the session reads them, in
place of the text display: the header when the session opens, then every byte
from the first, the login banner and the setup (`enable`, the paging commands)
included, each command as the device echoes it, and the footer after the
session's end. Nothing waits for a command to complete, so the lines of a long
`show` are seen as they come:

```text
! router1 [192.0.2.1] platform=cisco_ios user=bob backend=env transport=native

Router>enable
Password: <removed>
Router#terminal length 0
Router#terminal width 512
Router#show drip
drip 1
drip 2
drip 3
Router#exit
! exit=0 elapsed=2.1s artifacts=/srv/karvi/jobs/261025/261025-161152-00
```

`run --no-daemon --live` (and `crun`'s) follows each device's session in turn,
each device's header before its bytes: the run is serial, `dispatch.default`
set to `serial` from `--live` when the configuration says another mode, so a
site's lock on the key refuses the run as it refuses `--ds`. `--dispatch
parallel` or `wave`, `dispatch.default` set to either by `--set`, and a run
with no display in the client's process (no `--no-daemon`, `--dry-run`,
`--exercise`, `--follow=false`) are refused beside it. A run through the
daemon has no live view.

It is display only. A record leaves karvi's own lines alone, a failed
command's `karvi: target=…` line on a line of its own after the device's
bytes; the records, the job's files, and the audit are those of the same
command without `--live`, which the plan does not carry, so its digest is the
same. The exact value of each secret the session writes (the enable secret, a
telnet password, a root command's `sudo` password) is shown `<removed>`, found
even when the device echoes it in two pieces; escape sequences follow
`output.ansi`, as the text display's do. An exec target echoes nothing, so
each command is shown at the prompt `--echo` shows it at (`srv1$ uname -a`, a
root command's `srv1#`), its stdout and stderr as they arrive. `--quiet`
drops the header and the footer. Beside `--live`, the options that shape the
display it replaces are refused (`live_option_conflict`): `--format jsonl` or
`json`, `--echo`, `--border`, and `--noborder`; a configured echo or border is
set aside.

## Debugging

```bash
karvi command --debug --host router1 --cmd 'show clock' \
  >command.out 2>command.debug
```

Debug lines come in three families, one prefix each:

- `device …` (the executor): the transport selection, the resolved address
  and its source, the credential backend and device username, the SSH
  algorithm lists with the profile and rule that selected them, the
  capacity admission, the connection opening and ready, each command's
  start (index, hash, size, blind flags, expectation count, and a root
  command's `sudo=password` or `sudo=none`) and completion (status, code,
  output bytes, elapsed, prompt source), each sleep, a root command's
  password written (`device sudo answered`), and how the session ended.
- `system SSH command session …`, `system SSH exec master …`, or `native SSH
  connection …` (the transport): the binary or implementation, the address
  and port, the host-key policy and identity, the process ID or the shell's
  start, an exec master's socket and the method that authenticated, an open
  failure's code and diagnostic, a keepalive expiry.
- `device session …` (the session): the first prompt and level, an
  escalation, each paging command, each send, each expectation answered,
  each completion, a blind send whose prompt did not return; on an exec
  target, each command's start, completion, and spool, and a command given
  up.

No line carries a password, an enable secret, or device output. The
executor's start line shows each command once, as the plan holds it, never
from the device's echo; every other line names a command by its hash and
size.

## Device-error behavior

By default, a device-reported command failure (`device_command_error`,
matched by the platform's failure patterns) stops later commands for that
device and emits explicit `not_attempted_prior_command_failure` records.
Use `--continue-device-on-error` to continue. The policy applies only while
the session is usable: a device error, and output over the limit after the
prompt returned, leave it usable. A command timeout, a write failure, a
lost or failed read, output over the limit before the prompt returned, the
device timeout, or a keepalive expiry ends the session, and nothing more is
sent under any setting; the failed command carries the code and the rest
are not-attempted records. There is no hidden reconnect.

On an exec target the exit outcomes (`command_exit_nonzero`,
`command_exit_signal`, `command_exit_missing`) are device errors under the same
policy, and a command timeout and the output limit leave the session usable,
the connection serving the next command, so the policy applies to them too. A
refused channel or exec request, a lost connection, the device timeout, and a
keepalive expiry end the session.
