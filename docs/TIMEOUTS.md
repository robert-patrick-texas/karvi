# Timeouts and keepalives

What bounds a device session in karvi, which configuration key sets each
bound, how the values relate, what each expiry records, and where each lives
in the code, the blind wait among them. The decisions are `docs/DESIGN.md`;
`docs/ERROR-CODES.md` holds the codes, `configs/reference.toml` the keys.
The `system` transport is OpenSSH's `ssh`; `scrapligo-v1` is karvi's
own connection.

No timer ends an open session on its total age except
`execution.device-timeout`. The others bound one step each, prove the peer
alive, or belong to the operator (a cancel) and the daemon (its shutdown
grace).

## 1. Configuration keys

| Key (environment variable) | Default | Range (refused at load as `config_value_out_of_range`) | Bounds |
|---|---|---|---|
| `ssh.connect-timeout` (`KARVI__SSH__CONNECT_TIMEOUT`) | 10s | 1s–5m | `system`: the TCP connect (OpenSSH `ConnectTimeout`); also part of its first-prompt allowance |
| `native-ssh.connect-timeout` (`KARVI__NATIVE_SSH__CONNECT_TIMEOUT`) | 10s | 1s–5m | scrapligo-v1: the TCP dial |
| `native-ssh.handshake-timeout` (`KARVI__NATIVE_SSH__HANDSHAKE_TIMEOUT`) | 10s | 1s–5m | scrapligo-v1: the SSH handshake, authentication, and the PTY and shell requests |
| `telnet.connect-timeout` (`KARVI__TELNET__CONNECT_TIMEOUT`) | 10s | 1s–5m | telnet: the TCP connect |
| `execution.prompt-timeout` (`KARVI__EXECUTION__PROMPT_TIMEOUT`) | 10s | 1s–5m | the first prompt after login; the prompt after each paging command |
| `execution.enable-timeout` (`KARVI__EXECUTION__ENABLE_TIMEOUT`) | 10s | 1s–5m | one privilege level's whole step, from the escalate command to that level's prompt |
| `execution.command-timeout` (`KARVI__EXECUTION__COMMAND_TIMEOUT`) | 120s | 1s–12h | each requested command |
| `[session-init.NAME] command-timeout` | unset: `execution.command-timeout` | | each command of that session-init profile |
| `execution.device-timeout` (`KARVI__EXECUTION__DEVICE_TIMEOUT`) | 0s (unbounded) | 0, or 1s–7d | one device's whole command list, the session-init profile and the requested commands, from the prepared session |
| `ssh.server-alive-interval` (`KARVI__SSH__SERVER_ALIVE_INTERVAL`), `ssh.server-alive-count-max` (`KARVI__SSH__SERVER_ALIVE_COUNT_MAX`) | 15s, 3 | 0 disables, else 1s–10m; 1–100 | `system`: OpenSSH `ServerAliveInterval` (whole seconds, rounded up) and `ServerAliveCountMax` |
| `native-ssh.keepalive-interval` (`KARVI__NATIVE_SSH__KEEPALIVE_INTERVAL`), `native-ssh.keepalive-count-max` (`KARVI__NATIVE_SSH__KEEPALIVE_COUNT_MAX`) | 15s, 3 | 0 disables, else 1s–10m; 1–100 | scrapligo-v1: karvi's `keepalive@openssh.com` requests |
| `telnet.read-timeout` (`KARVI__TELNET__READ_TIMEOUT`) | 60s | 1s–12h | telnet: each read |
| `execution.blind-wait` (`KARVI__EXECUTION__BLIND_WAIT`; `--blind-wait`) | 10s | 0–10m | the prompt's return after a blind command (`--blind`, `--blind-return N` with 0–20 returns, or a command ending in `\r` sequences), in place of its command timeout; 0 sends and does not wait |

None of these keys but `execution.blind-wait` has a command-line option. Of
the documented duration ranges, `network.ping-timeout`'s is enforced when the
configuration loads and `execution.blind-wait`'s when the plan is drafted
(`execution_plan_invalid`); the others are not enforced.

## 2. How the values relate

| Phase | `system` | `scrapligo-v1` | telnet |
|---|---|---|---|
| Connect | `ssh.connect-timeout` | `native-ssh.connect-timeout`, then `native-ssh.handshake-timeout` | `telnet.connect-timeout` |
| First prompt (login) | `ssh.connect-timeout` + `execution.prompt-timeout`, from the start of the `ssh` process | `execution.prompt-timeout`, after the handshake | `execution.prompt-timeout` |
| Enable, per privilege level | `execution.enable-timeout`: one deadline across the secret's prompt and the level's prompt | the same | the same (the second wait has what remains) |
| Each paging command | `execution.prompt-timeout` | the same | the same |
| Each command | the smaller of its command timeout (the profile's or `execution.command-timeout`) and what remains of `execution.device-timeout` | the same | the smaller of those and `telnet.read-timeout` |
| A blind command | `execution.blind-wait` in place of the command timeout, under what remains of `execution.device-timeout`; any blind returns are written with the command, before any read | the same | the same |
| An answered prompt (`--expect`) | the command's timeout, or the blind wait when the command is blind: one deadline across every prompt and answer, never reset; a pattern that never appears is `command_timeout` naming the last line seen and the count answered | the same | the same |
| The device's whole list | `execution.device-timeout`, from the prepared session | the same | the same |
| A peer gone silent | ended (count-max + 1) × interval after the last data received | ended (count-max + 1) × interval after the last device output | none |

- **The first-prompt allowance differs by transport.** The `ssh` process
  dials, authenticates, and starts the shell in one stretch karvi cannot see
  into, so `system` awaits the first prompt for the connect and prompt
  timeouts together; scrapligo-v1 times its dial and handshake itself and
  awaits the prompt for `execution.prompt-timeout` alone. A device whose
  first prompt is slower than the prompt timeout can open on `system` and be
  `command_session_prompt_timeout` on scrapligo-v1.
- **The device clock** starts when the session is prepared. The ICMP gate,
  the capacity wait, connect, login, enable, and paging are outside it, each
  under its own bound.
- **The longest a list can run** with `execution.device-timeout = 0` is the
  sum of its commands' timeouts; with the key set, that value beside the
  separately bounded open.
- **Liveness and the command timeout:** a silent peer is ended by the
  smaller of (count-max + 1) × interval and the command's deadline; with
  keepalives off, only by the command timeout, as `command_timeout`.
- **The blind wait is not a timeout.** The operator declared the send blind,
  so the prompt's absence at the end of the wait is recorded as a success
  with the notice `prompt_not_observed_after_blind_send`, and the same when
  the stream ends during the wait (a real `reload`). A short command timeout
  does not cut a long confirm: the wait replaces it. The client's effective
  value travels in the execution plan (`blind_wait_ns`), so `--blind-wait`
  reaches the daemon, whose executor reads the other `execution.*` keys from
  its own configuration. Session-init profile commands are never blind.
- **Keepalive timing seen** against the fake with 1s × 2: 3.0s from the
  silence on scrapligo-v1, 5.3–5.6s on `system`, where OpenSSH's own
  whole-second timers are the likely cause. At the defaults the difference
  is small beside the total.
- **The keepalive request** is the global request `keepalive@openssh.com`
  with want-reply on both transports, sent only when nothing has arrived for
  the interval. Any reply counts, a refusal included: no server implements
  the name, and the refusal the protocol requires is the proof of life. The
  name is not configurable.

## 3. What each expiry records

| Expired | Code | Category and record status | Retryable | `command` exit | The rest of the device's list |
|---|---|---|---|---|---|
| First prompt | `command_session_prompt_timeout` | timeout; `connection_error` (a failure at open) | yes | 110 | not attempted |
| `execution.enable-timeout` | `privilege_failed` | device; `privilege_error` | no | 107 | not attempted |
| A paging command's prompt | `paging_disable_failed` | device; `device_error` | no | 107 | not attempted |
| A command's timeout | `command_timeout` | timeout; `timeout` | yes | 107 | not attempted; the session is ended, under `--continue-device-on-error` too |
| `execution.device-timeout` | `device_timeout` | timeout; `timeout` | yes | 107 | `not_attempted_prior_command_failure`; the requested records are `not_attempted_session_init_failure` when a profile command was cut |
| Keepalives unanswered | `session_keepalive_timeout` | connection; `connection_error` | yes | 110 | not attempted |
| scrapligo-v1's dial or handshake | `native_session_open_failed` | connection; `connection_error` | yes | 110 | not attempted |
| `execution.blind-wait` (the prompt absent after a blind command: `--blind`, a count, or the escape; its declared prompts answered as they appeared) | none; the notice `prompt_not_observed_after_blind_send` (`reason` `blind_wait_expired`, or `session_ended` when the stream ended first) | `succeeded`, all bytes read as the output, `prompt_observed` false | — | 0 | `not_attempted_prior_command_failure` naming the blind send, the device `command_session_lost`; as the last command the device succeeds |

- A failed `run` exits 101 whatever the code.
- The earlier of a command's own deadline and the device's names the code. A
  device deadline that has passed between two commands leaves the next one
  unsent under `device_timeout`, and the shell, still in step, is closed
  with the platform's exit commands.
- A timed-out command's record has no output; the session is closed without
  a graceful exit, since a desynchronised shell cannot serve another
  command. After a blind command whose prompt did not return the session is
  likewise closed without the exit commands, the shell being out of step.

## 4. Names in the code

| Setting | Go name | Where |
|---|---|---|
| First prompt | `devsession.Options.LoginTimeout` (fallback 40s) | `internal/devsession/session.go`; filled in `internal/transport/systemssh/systemssh.go` `Prepare` (`connectTimeout + promptTimeout`) and `internal/transport/native/provider_scrapligov1.go` (`promptTimeout`) |
| Enable | `devsession.Options.EnableTimeout` (fallback 10s); `stepCtx` in `escalate` | the same files; telnet: `enableTimeout`, `enableDeadline` in `internal/transport/telnet/telnet.go` |
| Paging | `devsession.Options.PromptTimeout` (fallback 10s) | the same files |
| Command | `platform.Command.Timeout`; the executor's `step.timeout`; `executionplan.SessionInitProfile.CommandTimeoutNS` | `internal/executor/executor.go` `sequence` |
| Blind send | `platform.Command.Blind` (the tolerance; `Timeout` is then the blind wait), `BlindReturns`; the executor's `step.blind`, `step.returns`, `Options.Blind`, `Options.BlindReturns`, `Options.BlindWait`; `executionplan.ExecutionPlan.Blind`, `BlindReturns`, `BlindWaitNS` (`BlindReturnsMax`, `BlindWaitMax`); the client's `Declaration`, `declarationLists`, and `declarations` | `internal/devsession/session.go` `Execute`; `internal/executor/executor.go` `sequence`; `executionplan/plan.go`; `internal/cli/work_commands.go` |
| Expect-and-send | `platform.Command.Expectations` (compiled); the session's `expecter`; the executor's `step.expect`, `Options.Expectations`; `executionplan.ExecutionPlan.Expectations`, `Expectation` (`ExpectationsMax`); the client's `splitExpect`, `Declaration`, `declarationLists`, and `declarations` | the same files; `internal/cli/parse.go` |
| Device | `deviceTimeout`, `deviceDeadline`, `deviceExpired`, `deviceCut`, `skipRest` | `internal/executor/executor.go`, the command loop |
| scrapligo-v1 connect, handshake | `DialRequest.ConnectTimeout`, `DialRequest.HandshakeTimeout` | `internal/adapters/scrapligov1/dial.go` |
| scrapligo-v1 keepalive | `DialRequest.KeepaliveInterval`, `DialRequest.KeepaliveCountMax`; `connection.keepalive`, `connection.lastRead`, `connection.lost`; the constant `keepaliveRequest` | `internal/adapters/scrapligov1/dial.go` |
| `system` keepalive | `processStream.aliveInterval`, `processStream.aliveCountMax`; `classify`'s "not responding" case | `internal/transport/systemssh/stream.go`, `systemssh.go` |
| The keepalive timeout's wording | `devsession.KeepaliveTimeout(count, interval, detail)` | `internal/devsession/session.go` |
| Fixed waits, not configurable | `promptSettleDelay` (100ms, a prompt-shaped last line must stay unchanged); `shellEndGrace` (2s, the shell's end after the exit commands) | `internal/devsession/session.go`; `internal/adapters/scrapligov1/dial.go` |

## 5. Test and fixture labels

| Label | Meaning |
|---|---|
| Fake device flags `-login-delay`, `-enable-delay`, `-secret-delay`, `-slow` | the delay before the first prompt, before `Password:`, before the answer to the enable secret, before `show slow` answers (`cmd/karvi-fake-iosxe`) |
| Fake device command `show mute`; `Server.Keepalives()` | the peer goes silent with the connection up, keepalive requests unanswered; the count of keepalive requests received |
| Fake device commands `clear counters`, `reload` | a `[confirm]` taking one unechoed key (a return or `y` confirms, `<confirm>` among the recorded lines; any other key abandons, `<abandon>`); after `clear counters` the prompt returns, after `reload` nothing more is written, the connection up |
| Fake device command `copy running-config startup-config`; flag `-unsaved` | a value prompt (`Destination filename [startup-config]? `, the trailing space kept) taking one echoed line, `<value:TEXT>` among the recorded lines (`<value:>` for the default), answered `Building configuration...` and `[OK]`; under `-unsaved` each shell starts with the running configuration modified, so `reload` first asks `System configuration has been modified. Save? [yes/no]: ` (`y`/`yes` saves, `n`/`no` skips, anything else re-asks) before its `[confirm]`, and the copy clears the state |
| Parity suite settings `COMMAND_TIMEOUT`, `EXECUTION`, `SSH`, `SHELL_OPENED` | the `[execution]` table's command timeout, further `[execution]` lines, further `[ssh]` lines, a shell that opens and receives no line (`scripts/native-smoke-test.sh`) |
| Parity cases S3, S15, S16, S17, S18, S19, S20, S21, S22, S23, S24 | the command timeout, the login allowance, the enable timeout, the device timeout, the keepalives, a blind send whose prompt returns, one whose prompt never returns under a 2 s blind wait, a value prompt answered by `--expect`, a mistyped pattern under the 2 s command timeout, a blind reload with the save and confirm declarations against an unsaved and a saved device |
| Evidence `ex39`, `ex40`, `ex41`, `ex42` | the timeouts before and after (Q1–Q4, B1–B4); the keepalives (K1, K2); the blind sends before (Q1–Q5) and after (B1–B4); expect-and-send before (A1–A4) and after (B1–B4) |
