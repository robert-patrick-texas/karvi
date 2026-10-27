<img width="1672" height="941" alt="Two Vikings on a winter dock watching karvi longships come in at dawn" src="../images/karvi-longships-at-dawn.png" />

# SSH transports and selection

## Goals

karvi owns the transport contract. Its SSH transports are `system`, the
OpenSSH executable, and `native`, karvi's own `x/crypto/ssh` connection in
every executable; Telnet is a gated third. `karvi version` lists the SSH
transports the running executable carries.

## Version inventory

Text output:

```bash
karvi --version
```

contains a repeatable section:

```text
ssh_transports:
  native: compiled-in
  system: external-executable
```

JSON consumers use `ssh_transports[]`, whose fields are `id`, `name`, and
`linkage`. The list is the same in every executable and independent of the
configuration.

## Configuration model

```toml
[ssh.login]
transport = "default"

[ssh.command]
transport = "default"

[ssh.run]
transport = "default"

[ssh.transports]
system = "ssh"
# alternate1 = "exec:./future-device-handler"
```

Mode selector resolution is:

| Mode | `default` result |
|---|---|
| `login` | `system` |
| `command` | `native` |
| `run` | `native` |

`native` and `telnet` name karvi's built-in transports; every other selector
names a slot under `ssh.transports`.

## Mapping values

- `ssh.transports.system = "ssh"` resolves `ssh` through `PATH`.
- A value containing `/` is a path. An absolute path is used as written. A
  relative path is resolved from the directory containing the resolved karvi
  executable, not from the caller's current directory.
- Any other slot is an OpenSSH-compatible program, written `exec:PROGRAM` or
  `exec:PATH`. `default` belongs to the `system` slot alone: a mode that
  selects another slot set to `default` is refused
  (`transport_custom_slot_default`).
- No slot takes a built-in transport's name: `ssh.transports.native` and
  `ssh.transports.telnet` are refused at load, as is any other slot whose
  value is not `exec:` (`transport_mapping_invalid`).
- `telnet` remains an explicit secondary selector and does not participate in
  the SSH version inventory.

A configured executable must be a regular executable file. Validation fails
before any network connection with the code for the specific cause — for example
`transport_mapping_missing`, `transport_mapping_invalid`, or
`transport_system_executable_unavailable`.
[`docs/ERROR-CODES.md`](ERROR-CODES.md) lists every code.

## Selection precedence

For one device, the resolver applies:

1. CLI transport override.
2. Explicit inventory transport selector.
3. Mode-specific `ssh.<mode>.transport`.
4. Mode default.
5. `native` or `telnet`, or a named slot's executable.

The selected transport's kind (`native` or `system`) is written to command
records. Nothing falls back from one transport to another.


## The device session

The platform's `channel` decides what a device's session asks of SSH, on
either transport ([`docs/COMMAND-SESSION.md`](COMMAND-SESSION.md)).

- **The shell** (every built-in but `linux`, `linux_shell` among them).
  `command` and `run` over the system transport open one fresh interactive
  `ssh -tt` process per device, with ControlMaster, ControlPath, and
  ControlPersist pinned off on the command line. One karvi session
  (`internal/devsession`) drives that shell: it waits for the first prompt,
  reaches the platform's privileged level with one enable attempt when the shell
  starts below it, sends the platform's paging commands, then sends every
  requested command and reads until the prompt returns. The same session drives
  the native transport's connection, so the two transports give the same records
  for the same commands. This avoids Cisco IOS XE servers that authenticate a
  master but refuse another session channel, and it never requests a second
  session channel or a replacement login between commands.
- **The exec channel** (built-in `linux`, and any table that sets `channel =
  "exec"`). One connection per device for its command list and one exec channel
  per command: on `system` an OpenSSH ControlMaster, its socket in
  `ssh.control-path-root` (at most 73 bytes, `control_path_root_too_long` at
  planning otherwise), and an `ssh -S` client per command; on `native` a session
  channel per command on karvi's own connection. One exec session
  (`internal/devsession`) settles both streams and classifies each command by
  its exit status, so the two transports give the same records but for what
  OpenSSH's client cannot do: name a signal (`exit_signal` `unnamed` on
  `system`) and ask the device to end a command given up (the notice
  `remote_command_not_stopped`). Telnet has no exec channel, and a target
  planned over telnet whose platform says `exec` is refused at planning.

What the session does beyond that is documented once, elsewhere; this
document only points there:

- **Host keys.** The unified policy (`ssh.host-key-policy`) runs in the
  handshake on both transports: OpenSSH's own check on `system`, karvi's
  callback in x/crypto's handshake on `native`, which runs no
  `ssh-keyscan` pre-scan and no subprocess.
  [`docs/SSH-HOST-KEY-POLICY.md`](SSH-HOST-KEY-POLICY.md).
- **Algorithms.** Both transports offer karvi's lists under `[ssh-algorithms]`,
  strongest first, with per-host profiles (`[ssh-algorithms-profile.NAME]`)
  selected by `[[ssh-algorithms-map]]` rules for devices that need an
  allowed-only name; each transport offers the names it implements. Under
  `ssh-algorithms.source = "transport"`, or a profile's `source`, a device's
  key exchanges, ciphers, and MACs are its transport's own (OpenSSH's compiled
  defaults or an included `~/.ssh/config`'s, x/crypto's), while the host-key
  list stays karvi's.
  `configs/reference.toml` holds the lists and their comments;
  [`docs/SSH-HOST-KEY-POLICY.md`](SSH-HOST-KEY-POLICY.md) the rules.
- **Timeouts and keepalives.** [`docs/TIMEOUTS.md`](TIMEOUTS.md); one
  command's own `--timeout` and `--maxbytes` are
  [`docs/COMMAND-SESSION.md`](COMMAND-SESSION.md)'s.
- **Blind sends and expectations.** A command ending in `\r` sequences or given
  `--blind-return` is sent and waited for `execution.blind-wait`; an `--expect`
  declaration answers a prompt the platform's pattern does not match.
  [`docs/TIMEOUTS.md`](TIMEOUTS.md) for the wait;
  [`docs/COMMAND-SESSION.md`](COMMAND-SESSION.md) for the declarations.
- **Session-init.** A `[session-init.NAME]` profile, selected for the device
  by `[[session-init-map]]`, runs after paging and before the requested
  commands ([`docs/COMMAND-SESSION.md`](COMMAND-SESSION.md)).
- **Platform admission.** The platform definition comes from
  `platform.Lookup`; `native` admits every built-in platform, so
  `native_platform_not_qualified` is unreachable on it. An
  unknown platform is refused before any connection (`platform_unknown`).
- **Parity.** `scripts/native-smoke-test.sh` runs every case as `command`
  and `run` over both transports against the fake, its IOS XE persona and its
  Linux persona for the exec channel and `linux_shell`, and compares the
  requested-command records path by path (`tools/paritycheck`); a difference
  by design is pinned per transport (`-pin`), not excluded. The cases are
  listed in the script.

## Build composition

There is one build, from the committed `go.mod`, `go.sum`, and `vendor/`:

```bash
make build
```

What every build carries:

- `internal/transport/native`: karvi's connection, x/crypto SSH with the
  host-key policy in the handshake and the algorithm lists, as a session
  stream, and the platforms the native transport admits.
- `internal/devsession`: the session both transports drive.
- `internal/fakedevice` and `cmd/karvi-fake-device`: the fake SSH device the
  tests and the parity suite run against.

The native transport runs no subprocess (no `ssh`, `ssh-keyscan`, or
askpass); `scripts/verify-release.sh` fails if its package names
`ssh-keyscan` or `os/exec`.

## Current limitations

Interactive login requires a system-compatible slot because it hands the
terminal to OpenSSH; another kind is refused as
`login_transport_not_interactive`. Every built-in platform is admitted on
`native`; its engineering evidence is the fake and the parity suite, and
what stands between the build and production use is the laboratory matrix
([`docs/CISCO-IOSXE-QUALIFICATION.md`](CISCO-IOSXE-QUALIFICATION.md)) and the
open production gates of [`ROADMAP.md`](../ROADMAP.md). External `exec:` slots
use the system process contract; a future device-handler protocol will require
its own versioned interface before general third-party handlers are advertised.
