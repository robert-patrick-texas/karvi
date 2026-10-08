# SSH troubleshooting

What to look at when an SSH session through karvi does not reach the device's
prompt: a `login` that sits after `login interactive session starting` with
nothing on the screen until a timeout ends it, or a `command` that ends in a
prompt timeout. The worked case is a Cisco Catalyst 9800-40 Wireless LAN
Controller, IOS XE like a Catalyst switch, on a production network; the methods
apply to any device.

A login gives the least to see. Past that debug line karvi hands the terminal to
OpenSSH (the `system` transport; a login takes no other) and runs it at
`LogLevel ERROR`, set in the configuration karvi generates for the session, so
OpenSSH's own account of how far it got is suppressed; and a login detects no
prompt, since the terminal is the operator's. Method 1 restores OpenSSH's
account; the others say what karvi chose and what the device sent.

## 1. OpenSSH at `-vvv` under karvi

`ssh.transports.system` names the executable the `system` transport runs, and
any executable will do ([`docs/SSH-TRANSPORTS.md`](SSH-TRANSPORTS.md)): point it
at a wrapper that runs OpenSSH with its debug on and its trace in a file.

```sh
#!/bin/sh
# OpenSSH at -vvv for one karvi session; the trace goes to a file, not the
# terminal, so the login's screen stays as it was.
exec /usr/bin/ssh -vvv -E "$HOME/karvi-ssh-debug.$$.log" "$@"
```

```bash
chmod 755 ~/bin/ssh-debug
karvi --debug --set 'ssh.transports.system="/home/OPERATOR/bin/ssh-debug"' login WLC
```

The path is absolute, or relative with a slash (from the karvi executable's
directory); a bare name is looked up on `PATH`. Everything else is karvi's own
invocation: the generated configuration, the algorithms, the credential through
the askpass broker, the trust store. `-vvv` on the command line wins over the
generated file's `LogLevel ERROR`, and `-E` keeps the trace off the terminal.
The same setting serves `command` over `--transport system` to a shell device.
Not an exec device (`linux`): karvi reads its master's own debug lines from the
master's standard error, which `-E` takes away. Not a `run` through the daemon
either: the daemon resolves the transport from the configuration it started
with. Under the wrapper OpenSSH writes its messages to the file and not to
karvi, so a command's record loses `credential.auth`, a first contact's
`host_key_enrolled`, and karvi's classification of what OpenSSH said: the trace
has them all. A login's first contact is still said, read from the trust store
([`docs/SSH-HOST-KEY-POLICY.md`](SSH-HOST-KEY-POLICY.md)).

Where the trace stops says which side is waiting:

| The trace's last lines | Waiting on | Usual causes |
|---|---|---|
| `Connecting to …`, no `Connection established` | TCP | the wrong address or port (see what `--debug` resolved: on a 9800 the wireless management interface or the service port, `GigabitEthernet0` in `Mgmt-intf`); an ACL or firewall |
| `Connection established`, no `Remote protocol version` | the SSH banner | the device's SSH server, its vty access class |
| `expecting SSH2_MSG_KEX_ECDH_REPLY` | key exchange | path MTU, or a middlebox dropping larger packets |
| `Next authentication method: keyboard-interactive` or `password`, then nothing | authentication | the device waiting on RADIUS or TACACS+ that does not answer, before it falls back to its local users |
| `Authenticated to …` and `channel 0: new session`, then nothing | the shell | the vty's exec side: the line's configuration, an `autocommand`, a login block, its exec timeout |
| `Timeout, server … not responding` | a session gone quiet | the keepalive: `ssh.server-alive-interval` × `ssh.server-alive-count-max`, 15 s × 3 by default |

What ends a stall: `ssh.connect-timeout` (10 s) bounds the TCP connection, the
handshake, and the key exchange (OpenSSH's `ConnectTimeout`); the keepalive runs
only once the session is up. Nothing on karvi's side bounds authentication: a
stall there lasts until the device gives up, on IOS XE its SSH authentication
timeout (`ip ssh time-out`).

The trace holds the address, the user name, the algorithms, and the host key,
never the password. Keep it private and remove it when done; a wrapper of this
kind is for the session being diagnosed, not for production use.

## 2. What karvi shows

- **`--debug` before the session:** the resolved address and where it came from,
  the port, the platform, the credential backend and the device user name, the
  SSH algorithm profile, the ICMP gate's result, and `system SSH interactive
  session starting binary=… address=… port=… host_key_policy=…`. Check the
  address and the port first.
- **`--debug` and the footer after it:** `system SSH interactive session failed
  code=… diagnostic=…` carries what OpenSSH wrote at `ERROR`, classified
  (`authentication_failed`, `connection_timeout`, `session_keepalive_timeout`,
  `host_key_changed`, …, each in [`docs/ERROR-CODES.md`](ERROR-CODES.md)); the
  footer's exit status names the same code.
- **While it hangs:** `ps -ef | grep karvi-ssh-` shows the exact `ssh` command
  line; the generated configuration is readable for the session's life in the
  scratch (`karvi config show tempdir` names it), `karvi-ssh-<pid>-*.conf`
  ([`docs/FILES.md`](FILES.md)); `karvi watch` shows the login running.
- **`login --record`:** the transcript holds every byte the device sent
  ([`docs/LOGIN-TRANSCRIPTS.md`](LOGIN-TRANSCRIPTS.md)). Whether anything came
  back at all, a banner or an escape sequence that drew nothing, is there; a
  session ended before its end keeps the raw bytes `script(1)` wrote.
- **The audit:** every login writes a `started` event and a `completed` or
  `errored` one with its code.

## 3. `command` beside the login

```bash
karvi --debug command WLC --cmd 'show clock' --transport system
karvi --debug command WLC --cmd 'show clock' --transport native
```

`command` shows more than a login. Over `system` it runs OpenSSH at `VERBOSE`
and classifies what OpenSSH says; its record's `credential.auth` names the
method that authenticated; and it detects the prompt, a missing one being
`command_session_prompt_timeout`. Raising `--set
'execution.prompt-timeout="60s"'` tells a slow prompt (a slow AAA answer) from
none. `native` is `scrapligo-v1`, another SSH stack altogether: when it reaches
the prompt and `system` does not, the difference is OpenSSH's or the generated
configuration's, and a plain `ssh -vvv` beside `ssh -vvv -F` a copy of karvi's
configuration finds the setting.
[`docs/COMMAND-TROUBLESHOOTING.md`](COMMAND-TROUBLESHOOTING.md) reads a
command's capture.

## 4. On the device

From the console or a session that works: `show users` and `show line vty` (is
the session there, and in what state), `show ip ssh`, `show aaa servers`, `show
logging`, and `debug ip ssh detail` during an attempt. They say whether the
device got past authentication on its side.
