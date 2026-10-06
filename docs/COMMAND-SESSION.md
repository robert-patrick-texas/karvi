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
       scrapligo-v1: one x/crypto connection, no subprocess
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
       (scrapligo-v1 waits up to 2 s for the shell's end)
  -> remove temporary config and askpass material (system)
```

Keepalives prove the peer alive throughout (`ssh.server-alive-*` on
`system`, `native-ssh.keepalive-*` on `scrapligo-v1`); a device gone silent
ends as `session_keepalive_timeout`. A session that has failed (a timeout,
a lost read, a write failure, output over the limit before the prompt
returned) is aborted, not closed: nothing is sent on a desynchronised
shell, no replacement login is made, and the remaining commands receive
explicit not-attempted records.

## One session for command and run

The shell above is the device session for
`command` and for `run` over the system transport alike, and the same
session logic drives scrapligo-v1's connection (`internal/devsession`). The
session adds two steps between the first prompt and the first requested
command, both from the platform definition:

1. **Privilege.** The first prompt is matched against the platform's
   prompt levels (`cisco_iosxe`: `exec` at `>`, `privilege-exec` at `#`,
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
   and the code.

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
       scrapligo-v1: one x/crypto connection; ready when it has authenticated
                                           (ssh.connect-timeout and execution.prompt-timeout)
  -> session-init profile, if one matches  (each command on its own channel)
  -> for each requested command:           (execution.device-timeout over the whole list)
       open an exec channel and start the command
         system: one ssh -S client of the master, LogLevel QUIET, ProxyCommand false
       read stdout and stderr, each spooled past the threshold on its own
       wait for the exit status or signal   (its command timeout, from the channel's opening)
       emit one command record
       stop or continue according to the device-error policy
  -> close: system ssh -O exit, the master killed on an abort;
            scrapligo-v1 closes its connection
```

There is no first prompt, no privilege step, no paging command, and no exit
command, and nothing carries from one command to the next (`cd`, variables,
`umask`): the remote shell is not interactive. Blind sends, `\r` endings,
`--blind-return`, and `--expect` are refused at planning for an exec target
(`channel_exec_declaration_refused`, exit 4); `--literal`, `--timeout`, and
`--maxbytes` are accepted.

How a command ends is its exit status: 0 is `succeeded` whatever stderr holds,
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
the next command: `scrapligo-v1` asks the device for `KILL` first; `system`,
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
attaches to the `--cmd` it follows or to the freeform command. The waits
are [`docs/TIMEOUTS.md`](TIMEOUTS.md).

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

## Prompt detection

The session renders the received bytes as the terminal showed them
(`internal/termtext`, at no width: carriage returns, backspaces, and erases
applied, colours, window titles, and every other sequence dropped) and matches
the unfinished last line, without its trailing blanks, against the platform's
prompt patterns: the levels' patterns for a platform that declares them
(scrapligo-v1's platform definitions, carried in `platform.go` for the
built-ins), `generic`'s prompt pattern otherwise, and a broad line ending
in `#`, `>`, or `$` for a platform with neither. A candidate prompt must
remain the final content for a short settle period so a banner line ending
with a prompt character is not accepted prematurely. The prompt timeout and
command timeout remain bounded configuration values.

The collected response is the rendered text: one echoed command line and
the trailing prompt removed, every line ending in `\n`, a space the device
wrote kept on an inner line, and the last line before the prompt without its
trailing blanks. The prompt the command was typed at holds its columns on the
first line and is no part of the text. A repeated line matching the
command later in legitimate device output is not silently discarded. A
device-reported command error is the platform definition's failure
patterns (`% Invalid input detected` and its siblings for `cisco_iosxe`);
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

prints the effective prompt plus command before each result, including a
device-reported command error:

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
  start (index, hash, size, blind flags, expectation count) and completion
  (status, code, output bytes, elapsed, prompt source), and how the
  session ended.
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
