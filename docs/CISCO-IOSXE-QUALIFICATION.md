# Cisco IOS XE qualification

## Scope

The laboratory qualifies `cisco_iosxe` first, on both transports together:
one device session (`internal/devsession`) drives the system OpenSSH
process and the `scrapligo-v1` connection alike, so every row below is run
over `system` and `scrapligo-v1` and the two records compared. Every
built-in platform is admitted on `scrapligo-v1`; IOS XE is the platform
qualified first. The intended device families are:

- Cisco Catalyst 9000-series switches;
- Cisco Catalyst 3650-series switches;
- Cisco ISR 4451-X routers;
- Cisco ASR 1000-series routers;
- Cisco Catalyst 8300 routers; and
- Cisco Catalyst 8500 routers.

Catalyst 9300 is the first focused no-production-impact test model. Listing
a family here records intended scope; it does not claim completed
qualification. The executable under test is the official build (`make
build`, `scripts/verify-release.sh` passed), on which both transports open,
escalate once, and record alike.

## What the fixture already shows

The engineering fixture is the fake SSH device (`internal/fakedevice`,
`cmd/karvi-fake-device`), whose default persona answers as IOS XE. It models the
`>` and `#` prompts, `enable` with and without a secret, the paging commands,
the `% Invalid input` line, delays before the first prompt, the secret's answer,
and a command's answer, a peer gone silent (`show mute`), the `[confirm]` of
`reload`, the `Destination filename` value prompt of `copy`, the `Save?
[yes/no]:` prompt of an unsaved configuration, global configuration mode with
its `(config)#` prompt (`configure terminal`, `end`; no configuration line is
modelled), `show privilege`, `show users`, a large output (`show big`), a fixed
or changed host key, extra key types, a SHA-1-only RSA key, and a restricted
algorithm offer. It refuses exec requests and records every PTY request and
every line received; exec belongs to its Linux persona (`-persona linux`).

The fixture grows on demand and only as far as the prompt: a command is
added when a suite or a script
row sends it, with a unit test, and it models what the session layer
sees (prompts, paging, echo, error text), never the device's state. No
configuration is stored, no user table is kept, and nothing is added for
completeness; the laboratory run stays the authority on a device.

The parity suite `scripts/native-smoke-test.sh` (run by
`scripts/verify-release.sh`) runs twenty-six cases against it, each as
`command` and `run` over both transports, and `tools/paritycheck` compares
the requested-command records path by path:

| Cases | What they evidence |
|---|---|
| S1–S3 | success, a rejected command under halt and under continue, a command timeout; the daemon path |
| S4–S5 | a session-init profile under `fail-device` and under `continue` |
| S6, S25, S26 | an alias of `cisco_iosxe`, an alias with its own paging command, an unknown row platform under `warn` |
| S7–S9 | privilege 15 at login (nothing sent), a secret asked with none resolved, a wrong secret |
| S10 | `generic` records a rejected command as it comes |
| S11 | the output limit |
| S12 | the legacy device under the defaults (`ssh_algorithm_negotiation_failed`) and through an algorithm profile |
| S13 | a rejected password |
| S14 | `secure` with an empty store and with another key enrolled; `insecure` with the mismatch warning; one transport's enrollment read by the other |
| S15–S18 | the login allowance, the enable timeout, the device timeout, the keepalive expiry |
| S19–S24 | a `[confirm]` by the escape with and without a returning prompt, a value prompt by `--expect`, a mistyped pattern, a blind reload with the save and confirm declarations, unsaved and saved |

Unit tests cover the identity per port, enrollment under the file lock, the
enrolled-type filter, the trust store's permission rules, ControlMaster off
on every system command line, and cancellation mid-command. A row marked
**fake** below is evidenced there; the laboratory confirms it on the device.
A row marked **device** is laboratory work only.

## The laboratory run

`scripts/device-qualification.sh` runs the rows marked **device** below against
one laboratory device on both transports and collects the evidence of "Required
evidence" into one directory;
[`docs/DEVICE-QUALIFICATION-RUNBOOK.md`](DEVICE-QUALIFICATION-RUNBOOK.md) says
what the run needs, maps each script row (D1 to D14) to its line here, and lists
the rows done by hand (D15 and D16 among them). With `FAKE=1` the same rows run
against the fake, which is how the script itself is checked before a laboratory
run. Every mismatch row writes a wrong entry into a scratch trust store; the
device's key and the operator's own store are never touched.

## Host-key matrix

For each model and software train, and for name and literal-address
targeting, test on both transports:

1. `accept-new`: empty trust file, first acceptance, repeated match, changed
   key (**fake**); an entry enrolled by one transport accepted by the other
   (**fake**); concurrent first enrollment from two clients (**device**);
   hashed entries written by hand (**device**).
2. The identity ([`docs/SSH-HOST-KEY-POLICY.md`](SSH-HOST-KEY-POLICY.md)): a
   device on port 22 is enrolled under its canonical name, a device on another
   port as `[name]:PORT`, on both transports (**fake**); a manual enrollment
   under the identity accepted under `secure` (**device**); an entry left from
   an earlier release for a non-22 port not matched (**device**).
3. `secure`: missing file, unknown host, matching host, changed key
   (**fake**); wrong owner, symlink, non-`0700` directory, non-`0600` file
   (unit tests; **device** for the message an operator sees).
4. The enrolled-type filter: a device presenting a key type the store does
   not hold under its identity is `host_key_changed` before authentication
   (**fake**); a device that rotates from RSA to ECDSA (**device**).
5. `insecure`: unknown key warning, matching-key warning, mismatch-specific
   warning, scan failure warning, and successful access after warning
   (**fake**, but the scan-failure warning **device**).
6. Fleet isolation: one mismatch produces one failed device while subsequent
   targets continue unless a configured halt or gate trips; the run-wide
   halt under `ssh.halt-run-on-host-key-mismatch` (**device**).

Never manufacture a mismatch on a production management plane. Use an
isolated fixture, a controlled alternate SSH endpoint, or a temporary lab
key.

## Session and command matrix

Record evidence, on both transports, for:

- password and enable-password authentication; a rejected password
  (**fake**);
- the first prompt at user and at privileged level; one `enable` from the
  user prompt, the secret at its prompt, nothing sent when the shell is
  already at or above the level, never a `disable` (**fake**);
- a wrong enable secret and an absent one, each `privilege_failed`, one
  attempt (**fake**);
- the paging commands (`terminal length 0`, `terminal width 512`) accepted
  (**fake**); a rejected one `paging_disable_failed` (**device**: a
  restricted user);
- a session-init profile under `fail-device` and under `continue`
  (**fake**);
- show commands and configuration-mode transitions, the `(config)#` prompt
  level observed (**device**);
- device-reported syntax failures (**fake**); authorization and
  configuration failures (**device**);
- multi-command stop and `--continue-device-on-error` (**fake**);
- a `[confirm]` answered by the escape and by `--blind-return`; a value
  prompt answered by `--expect`; a blind `reload` with the save and confirm
  declarations, unsaved and saved (**fake**; the reload itself **device**,
  on a lab unit);
- a mistyped `--expect` pattern waiting out the command timeout (**fake**);
- the command, enable, and device timeouts, the keepalive expiry
  (**fake**); cancellation and the daemon's shutdown grace (unit tests and
  the v0100 suite; **device** for the shell's end);
- the legacy device under the defaults and through an algorithm profile
  (**fake**; a real SHA-1-only train **device**);
- ANSI and line-ending handling from a real terminal (**device**);
- an interactive `login` ended with `exit`: whether the device sends an
  exit status when the session ends (plain `ssh`'s exit), and `karvi
  login`'s exit and the transcript's classification (**device**, D15 by
  hand; the fake sends none, so its login ends 110);
- a recorded login's transcript against the line editor's keys, a line wider
  than the terminal included: each command line as the device received it
  (**device**, D16 by hand; the fake has no line editor);
- typical output and output at the limit (`output.max-command-bytes`, 64 MiB
  by default; **fake** at a lowered limit, **device** at the default);
- concurrent sessions up to the configured session cap (**device**);
- `--echo` reproducing the observed prompt plus the sent command, `--debug`
  exposing hashes, timing, and session state and no password or command
  text (**fake**);
- the two transports' requested-command records equal path by path but for
  identifiers, timing, and the transport (**fake**; the real device
  **device**); and
- exact JSONL, failure subset, audit, metrics, summary, and scoreboard
  counts (**device**).

## Required evidence

For each qualified combination retain the karvi version and commit, the Go
version, the ScrapliGo version, the fake's commit where a row was confirmed
against it, the device model, the IOS XE release, the command plan digest,
sanitized records and debug output, resource measurements, and the pass/fail
disposition (the script's evidence directory,
[`docs/DEVICE-QUALIFICATION-RUNBOOK.md`
§5](DEVICE-QUALIFICATION-RUNBOOK.md#5-the-evidence)). Production activation
requires review by the network operations and security owners; the open gates
are listed in [`ROADMAP.md`](../ROADMAP.md).
