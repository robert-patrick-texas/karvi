# SSH transports and selection

## Goals

karvi owns the transport contract. CLI, inventory, credentials, dispatch,
records, and audit code do not import ScrapliGo or any future SSH library. A
build-composition registry exposes exactly which adapters are present in the
running executable.

## Version inventory

Text output:

```bash
karvi --version
```

contains a repeatable section:

```text
ssh_transports:
  system: external (id=system, linkage=external-executable)
  scrapligo: 1.4.2 (id=scrapligo-v1, linkage=compiled-in)
```

The second line appears only in a native-enabled build. JSON consumers use
`ssh_transports[]`, whose fields are `id`, `name`, optional `version`, and
`linkage`. The inventory describes executable composition and is independent
of configuration preferences.

A future ScrapliGo v2 adapter registers a different ID, for example
`scrapligo-v2`, so v1 and v2 can coexist in one binary during qualification.

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
native = "scrapligo-v1"
# alternate1 = "scrapligo-v2"
# alternate2 = "exec:./future-device-handler"
```

Mode selector resolution is:

| Mode | `default` result |
|---|---|
| `login` | `system` |
| `command` | `system` |
| `run` | `native` |

`preferred` is an operator-facing synonym for the current `native` slot. A
literal compiled implementation ID such as `scrapligo-v1` may be selected
directly, but named slots are preferred because governance can remap them.

## Mapping values

- `ssh.transports.system = "ssh"` resolves `ssh` through `PATH`.
- A value containing `/` is a path. An absolute path is used as written. A
  relative path is resolved from the directory containing the resolved karvi
  executable, not from the caller's current directory.
- `ssh.transports.native = "scrapligo-v1"` selects that compiled implementation.
- Another built-in implementation uses its registered ID.
- An external alternate is explicit: `exec:PROGRAM` or `exec:PATH`.
- `telnet` remains an explicit secondary selector and does not participate in
  SSH version inventory.

A configured executable must be a regular executable file. An explicitly
configured built-in must be present in this exact binary. Validation fails
before any network connection with the code for the specific cause — for example
`transport_mapping_missing`, `transport_system_executable_unavailable`, or
`native_transport_unavailable`. [`docs/ERROR-CODES.md`](ERROR-CODES.md) lists
every code.

One exception keeps an executable usable when its default native
implementation has no registered provider (no shipped executable is in that
state now that every build carries scrapligo-v1): the
built-in defaults `native = "scrapligo-v1"` and mode `transport = "default"`
are lazy. They are allowed at config-validation time, but selecting native (the
default of `command` and `run`) fails explicitly when the adapter is absent. An
operator-authored non-default unavailable mapping is never lazy.

## Selection precedence

For one device, the resolver applies:

1. CLI transport override.
2. Explicit inventory transport selector.
3. Mode-specific `ssh.<mode>.transport`.
4. Mode default.
5. Named-slot mapping to an executable or compiled adapter.

The selected implementation is written to command records. There is no silent
fallback from a missing native adapter to system OpenSSH.


## The device session

The platform's `channel` decides what a device's session asks of SSH, on
either transport ([`docs/COMMAND-SESSION.md`](COMMAND-SESSION.md)).

- **The shell** (every built-in but `linux`, `linux_shell` among them).
  `command` and `run` over the system implementation open one fresh interactive
  `ssh -tt` process per device, with ControlMaster, ControlPath, and
  ControlPersist pinned off on the command line. One karvi session
  (`internal/devsession`) drives that shell: it waits for the first prompt,
  reaches the platform's privileged level with one enable attempt when the shell
  starts below it, sends the platform's paging commands, then sends every
  requested command and reads until the prompt returns. The same session drives
  scrapligo-v1's connection, so the two transports give the same records for the
  same commands. This avoids Cisco IOS XE servers that authenticate a master but
  refuse another session channel, and it never requests a second session channel
  or a replacement login between commands.
- **The exec channel** (built-in `linux`, and any table that sets `channel =
  "exec"`). One connection per device for its command list and one exec channel
  per command: on `system` an OpenSSH ControlMaster, its socket in
  `ssh.control-path-root` (at most 73 bytes, `control_path_root_too_long` at
  planning otherwise), and an `ssh -S` client per command; on `scrapligo-v1` a
  session channel per command on karvi's own connection. One exec session
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
  callback in x/crypto's handshake on `scrapligo-v1`, which runs no
  `ssh-keyscan` pre-scan and no subprocess.
  [`docs/SSH-HOST-KEY-POLICY.md`](SSH-HOST-KEY-POLICY.md).
- **Algorithms.** Both transports offer karvi's lists under `[ssh-algorithms]`,
  strongest first, with per-host profiles (`[ssh-algorithms-profile.NAME]`)
  selected by `[[ssh-algorithms-map]]` rules for devices that need an
  allowed-only name; each transport offers the names it implements.
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
  `platform.Lookup`; `scrapligo-v1` admits every built-in
  platform, so `native_platform_not_qualified` is unreachable on it. An
  unknown platform is refused before any connection (`platform_unknown`).
- **Parity.** `scripts/native-smoke-test.sh` runs every case as `command`
  and `run` over both transports against the fake, its IOS XE persona and its
  Linux persona for the exec channel and `linux_shell`, and compares the
  requested-command records path by path (`tools/paritycheck`); a difference
  by design is pinned per transport (`-pin`), not excluded. The cases are
  listed in the script.

## Build composition

There is one build, from the committed `go.mod`, `go.sum`, and `vendor/`
The `scrapligo_v1` build tag and the
dependency-free preview module that once gave a `system`-only executable
were removed:

```bash
make build
```

What every build carries:

| Package | Holds |
|---|---|
| `internal/adapters/scrapligov1` | karvi's connection: x/crypto SSH through scrapligo's transport wrapper, the host-key policy in the handshake, the algorithm lists, as a session stream |
| `internal/transport/native` | the `scrapligo-v1` provider built on the session layer, and the registry that says which built-in implementations are present and which platforms each admits |
| `internal/devsession` | the session both transports drive (not tagged; the system transport uses it too) |
| `internal/fakedevice`, `cmd/karvi-fake-device` | the fake SSH device the tests and the parity suite run against |

`internal/adapters/scrapligov1` is the only package that may import
`github.com/scrapli/scrapligo`; `scripts/verify-release.sh` fails on any
other import. scrapligo's channel and network driver are not used, and
`scrapligo-v1` runs no subprocess (no `ssh`, `ssh-keyscan`, or askpass).

## Current limitations

Interactive login requires a system-compatible slot because it hands the
terminal to OpenSSH; another kind is refused as
`login_transport_not_interactive`. Every built-in platform is admitted on
`scrapligo-v1`; its engineering evidence is the fake and the parity suite, and
what stands between the build and production use is the laboratory matrix
([`docs/CISCO-IOSXE-QUALIFICATION.md`](CISCO-IOSXE-QUALIFICATION.md)) and the
open production gates of [`ROADMAP.md`](../ROADMAP.md). External `exec:` slots
use the system process contract; a future device-handler protocol will require
its own versioned interface before general third-party handlers are advertised.
