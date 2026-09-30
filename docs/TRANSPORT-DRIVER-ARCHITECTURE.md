# Transport and driver architecture

## Karvi-owned contracts

Three layers, each owned by karvi. The public `platform` package defines
the driver contract execution consumes: `platform.Driver` (`Prepare`,
`Execute`, `Usable`, `Close`), `platform.Command` and `platform.Result`,
and `platform.Definition`, the platform's prompt levels, paging commands,
and failure patterns, reached through `platform.Lookup`. The
session layer `internal/devsession` drives one device shell over a byte
stream: it waits for the first prompt, escalates once to the definition's
privileged level, sends the paging commands and the session-init profile,
then each requested command, and classifies what comes back. A driver may
also implement `platform.SetupReporter`: after
`Prepare`, `SetupLines()` is the set-up it sent (the escalate command and
the paging commands, each with the prompt it was sent at and the answer,
never the enable secret or anything read after it), which the executor
hands to the output store for the device's `output.TARGET.txt`. The
transports beneath it supply the stream. Core code never
imports ScrapliGo; third-party and operating-system adapters live under
`internal/`.

```text
executor -> platform.Driver
              |-- internal/transport/systemssh ---.
              |                                    |-- internal/devsession over a Stream
              |-- internal/transport/native -------'      (one ssh process; one x/crypto connection)
              |        `-- internal/adapters/scrapligov1
              `-- internal/transport/telnet (its own driver, no session layer)
```

The executor, both SSH transports, telnet, and the session work on
`platform`'s types; the earlier library-neutral contracts of a public
`transport` package were imported by nothing and were removed.

## Named selection layer

`internal/transportselect` maps operator selectors through configuration:

```text
CLI/inventory override
  -> ssh.<mode>.transport
  -> default (login/command=system, run=native)
  -> ssh.transports.<slot>
  -> executable or compiled implementation
```

`preferred` maps to `native`. A compiled ID may be selected directly. No
fallback occurs after a selected adapter is unavailable.

## System OpenSSH

The system adapter (`internal/transport/systemssh`) receives a resolved
executable path and karvi configuration. It owns the managed SSH
configuration, askpass, the algorithm lists it writes as `HostKeyAlgorithms`,
`KexAlgorithms`, `Ciphers`, and `MACs`, the host-key identity
(`HostKeyAlias`), and the classification of OpenSSH's own diagnostics at
open. `command` and `run` open one fresh interactive `ssh -tt` process per
device with ControlMaster, ControlPath, and ControlPersist off on the
command line and in the managed configuration (the platform table's
`control-master` and the `ssh.control-*` keys are inert), and hand that
process to the session as its stream; prompts, privilege, paging,
session-init, failure detection, blind sends, output normalisation, and
the keepalive expiry are the session's. Interactive login is system-only:
it hands the terminal to OpenSSH.

## ScrapliGo v1

The provider file in `internal/transport/native` registers implementation
ID `scrapligo-v1` at init in every build (the `scrapligo_v1` build tag that
once made it optional is gone). `internal/adapters/scrapligov1` is karvi's connection: an
x/crypto SSH connection through scrapligo's transport wrapper, with the
unified host-key policy as the handshake's callback, karvi's algorithm
lists, the connect and handshake timeouts, and the keepalives, exposed as a
`devsession.Stream`. scrapligo's channel and network driver are not used,
and no subprocess runs (no `ssh`, `ssh-keyscan`, or askpass). The provider
in `internal/transport/native` opens the session on that stream, so both
transports give the same records for the same commands. The provider
admits every built-in platform: `generic`, `cisco_iosxe`, `cisco_iosxr`,
`cisco_nxos`, `juniper_junos`, `arista_eos`, and `linux`; an alias table
resolves to its built-in, so `native_platform_not_qualified` is unreachable
on `scrapligo-v1`. `docs/SSH-TRANSPORTS.md` holds the build composition and
the import boundary.

## Build identity

`internal/buildinfo` has a deterministic registry separate from configuration.
System is always listed as an external executable. Build-tagged composition
files register compiled adapters and exact versions. Future adapters use unique
IDs, allowing multiple implementations in one binary.

## Extension rule

A new SSH library brings a stream, not a session. It requires:

1. a `devsession.Stream` (read, write, close; an `Aborter` when the
   transport can be cut at once; the keepalive expiry returned as
   `devsession.KeepaliveTimeout`) with the host-key policy and the
   algorithm lists applied in its handshake;
2. a build-composition registration file under `internal/buildinfo`;
3. a provider registration under `internal/transport/native` naming the
   platforms it admits;
4. configuration mapping and validation tests;
5. host-key, algorithm-negotiation, connect-timeout, and open-failure
   classification tests against the fake (`internal/fakeiosxe`);
6. a parity run (`scripts/native-smoke-test.sh`) showing its records equal
   the system transport's path by path;
7. binary version evidence; and
8. platform qualification before becoming the `native` default.

Prompt, privilege, paging, session-init, timeout, cancel, and blind-send
behaviour is the session's and is tested once, in `internal/devsession`,
not per library.
