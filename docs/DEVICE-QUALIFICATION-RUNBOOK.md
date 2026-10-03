# Device qualification runbook

The laboratory run of `docs/CISCO-IOSXE-QUALIFICATION.md`'s rows marked
**device**, on both transports, with `scripts/device-qualification.sh`
collecting the evidence (the production gates listed in
`ROADMAP.md`). The Catalyst 9300 is run first, then the
ISR 4451-X, then the other approved families; one run qualifies one device
on one software release.

The script is checked against the fake IOS XE device before every lab run
(`FAKE=1`, step 2): every row it sends to a device has first passed there,
so a failure in the laboratory is the device's answer and not the script's.

## 1. What the run needs

| Need | For | Notes |
|---|---|---|
| A host that reaches the device's management address over SSH, with the released bundle unpacked and `scripts/verify-bundle.sh` passed | every row | The OpenSSH client for `system`; `go` only to build `tools/paritycheck` (without it the comparison is marked skip and is run afterwards over the kept streams) |
| `python3` | the script's one JSON read (`scripts/lib/json.sh`: the executable's transports) | A prerequisite of `BUILD-HOWTO.md` §1; the script stops at its start without it |
| `bin/secret-scan` (`make tools-build`) | the final scan | Without it the scan is marked skip and the evidence must not leave the host unreviewed |
| The device's inventory name, management address, and SSH port | every row | `DEVICE`, `ADDRESS`, `PORT` |
| A laboratory account that reaches privilege 15 by `enable`, and its enable secret | D1–D4, D7–D15 | `NETUSER`, `NETPASS`, `NETENABLE` in the environment. If the account lands at privilege 15 at login, leave `NETENABLE` unset: that is the matrix's "nothing sent" row |
| A restricted account refused `terminal length 0` (for example privilege 1 with no `terminal` command authorized) | D5 | `RESTRICTED_USER`, `RESTRICTED_PASS`; the row is skipped without them |
| The number of vty lines free on the device | D8 | `CONCURRENCY` at or below it; a 9300's default is 16 (`line vty 0 15`), less the operator's own sessions |
| A read-only command with a large output | D9 | `BIG_COMMAND="show tech-support"` runs for minutes and produces megabytes; the row's command timeout is 600 s |
| A DNS name for the device with both A and AAAA records, and IPv6 reachability from the host | D12 | `DUALSTACK_NAME` |
| Agreement that `configure terminal` followed by `end` may be sent | D4 | `CONFIG_ROW=1`; nothing is sent between the two |
| A laboratory unit that may be reloaded, and the time for it to return | D13 | `RELOAD="scrapligo-v1"` or both transports, one reload each; a save prompt is answered `no` |
| The device's model and software release, as the change record names them | the evidence | The script records what `show version` answers; the reviewers compare |

What the run never needs: a changed host key on the device (every
mismatch row writes a wrong entry into a scratch trust store, the device
presents its own key, and karvi refuses before authentication), the
operator's own trust store, configuration, or state (all under the
evidence directory's `work/` while the run lasts), or a credential on a
command line or in a file.

## 2. Before the laboratory: the script against the fake

```bash
cd karvi-v0.25.0
sha256sum -c CHECKSUMS.sha256          # the released executables
make tools-build                       # bin/secret-scan
FAKE=1 CONFIG_ROW=1 RELOAD="system scrapligo-v1" scripts/device-qualification.sh
```

The last line must end `device qualification: pass`. Executed
on the released executable: 71 pass, 10 observe, 2 skip (D5
and D12, which need an account and a name the fake does not have), none
failed; both blind reloads pass with the
`prompt_not_observed_after_blind_send` notice. The log and the evidence
directory are beside the tree in `device-qualification-evidence/`.

## 3. The run

```bash
export NETUSER=labuser
read -rs NETPASS;   export NETPASS
read -rs NETENABLE; export NETENABLE

# First pass: read-only rows only.
DEVICE=c9300-lab ADDRESS=192.0.2.10 scripts/device-qualification.sh

# Second pass, as each need of section 1 is met:
DEVICE=c9300-lab ADDRESS=192.0.2.10 \
  CONCURRENCY=12 BIG_COMMAND="show tech-support" \
  DUALSTACK_NAME=c9300-lab.lab.example CONFIG_ROW=1 \
  scripts/device-qualification.sh

# The reload, alone, on the laboratory unit:
DEVICE=c9300-lab ADDRESS=192.0.2.10 ROWS=D13 RELOAD="scrapligo-v1 system" \
  scripts/device-qualification.sh
```

Each invocation writes a new evidence directory,
`karvi-qualification-DEVICE-UTCSTAMP/`, and refuses an existing one.
`ROWS="D1 D7"` runs only the rows named.

## 4. The rows

Each row runs over `system` and `scrapligo-v1`. `both` means `command` and
`run --no-daemon` on each transport, four streams compared path by path by
`tools/paritycheck` (the matrix's "records equal path by path" row on the
device).

| Row | Matrix line | What is sent | Pass |
|---|---|---|---|
| D1 | show commands; device-reported syntax failure | both: `show clock`, the invalid command, `show version`, under `--continue-device-on-error` | succeeded, `device_command_error`, succeeded; exit 107 (`command`), 101 (`run`); parity |
| D2 | multi-command stop | both: the same list under halt | the third `not_attempted_prior_command_failure`; parity |
| D3 | first prompt, one `enable`, paging, `--echo`, `--debug` | `command --debug --echo`: `show clock`, `show privilege`, in text | exit 0; the streams observed (banner, prompt, the enable, the paging lines) |
| D4 | the `(config)#` level | both: `configure terminal`, `end`, `show clock` (opt-in) | three succeeded; the first record's `prompt` is the `(config)#` prompt; parity |
| D5 | a rejected paging command; authorization failure | `show clock`, `show running-config` as the restricted account (opt-in) | observed: the expectation is `paging_disable_failed` |
| D7 a | `accept-new`: empty store, first acceptance, repeat | `show clock` twice | the store holds the identity (`name`, or `[name]:PORT`) and the repeat leaves it byte-identical |
| D7 b | one transport's enrollment read by the other | `run` under `secure` on the other transport | exit 0 |
| D7 c | hashed entries written by hand | `ssh-keygen -H` over the store, then `secure` | exit 0 |
| D7 d, e | `secure`: unknown host; changed key | an empty store; another key under the identity | 109 `host_key_not_enrolled`; 109 `host_key_changed` |
| D7 f | `insecure`: the mismatch warning, then access | the same wrong entry | exit 0 and the warning on stderr |
| D7 g | literal-address targeting | `--target ADDRESS` | the store holds the address identity |
| D7 h | concurrent first enrollment from two clients | two `command` processes at once on an empty store | both exit 0; no entry twice |
| D7 i | a non-`0600` store is refused or ignored, never repaired | mode 0644 under `secure`, `accept-new`, then `insecure` | `secure` and `accept-new`: 109 `host_key_trust_store_permission` with the `chmod 600` remedy; `insecure`: 0; the mode still 0644 after each |
| D7 j | fleet isolation; the run-wide halt | two names for the device, the first with a wrong entry, serial | `connection_error,succeeded`; with `ssh.halt-run-on-host-key-mismatch` observed (`not_started_halt`, exit 114 on the fake) |
| D8 | concurrent sessions to the cap | `CONCURRENCY` names for the device through the daemon, parallel: `show clock`, `show users` | every record succeeded |
| D9 | typical output and output at the limit | `BIG_COMMAND` under the default 64 MiB, then under `BIG_LIMIT` | exit 0 and `output_bytes` observed; then 111 `output_limit_exceeded` |
| D12 | IPv4/IPv6 selection | `--ipv4`, then `--ipv6`, with `--debug`, on the dual-stack name | exit 0 each; the debug stream's `device resolved … address=` line names an address of the family asked for |
| D13 | the reload itself | `reload --blind` with the save (`no`) and confirm declarations; then `show version` every 30 s | exit 0 with the blind notice; the device answers within `RELOAD_WAIT` |
| D14 | the shell's end | `show users` | observed: no session of the run remains on a vty line |
| ALL | ANSI and line endings; counts; secrets | every collected record and job directory | no `\r` or escape byte in any recorded output; every job directory holds records and summary; `secret-scan` finds neither secret; the operator's trust store unchanged |

Rows the script does not run, and how they are done:

- **A device that rotates from RSA to ECDSA**, and **a real SHA-1-only
  train**: they need that device. With one, D7 a's store shows the key
  types enrolled; regenerate the key on the laboratory unit
  (`crypto key generate ec keysize 256`, then remove the RSA key), run
  `ROWS=D7`, and the expectation is `host_key_changed` before
  authentication (the enrolled-type filter). For the legacy train, add the
  algorithm profile of `docs/SSH-TRANSPORTS.md` to a copy of a row's
  `karvi.toml` kept in the evidence and run the row's command by hand.
- **Wrong owner, symlink, non-`0700` directory**: unit tests hold the
  rules; the operator's message for mode is D7 i. The others need root or
  a second account and are read from the unit tests.
- **The insecure scan-failure warning**: needs an endpoint that refuses
  the scan and accepts the session; not reproducible on a healthy device.
- **Cancellation at the device**: during D9's `BIG_COMMAND` press Ctrl-C
  in a hand-run `karvi command`, then run `ROWS=D14`: the vty line must be
  free.
- **An entry left from an earlier release for a non-22 port**: only where
  `PORT` is not 22; write `ADDRESS ssh-ed25519 KEY` (the old form) into a
  scratch store and run D7 d's command: `host_key_not_enrolled`.
- **D15, how a login ends.** Whether the device sends an exit status when
  a session ends with `exit`, and what `karvi login` makes of it. `login` is
  interactive and runs on the system transport alone, and the script drives
  no interactive session, so the row is done by hand, on each laboratory
  device of the run (the ISR 4451-X and the Catalyst 9300), as the
  laboratory account, in the evidence directory:

  ```bash
  mkdir D15 && cd D15
  # 1. The device, karvi aside: plain OpenSSH, its own log at VERBOSE in a
  #    file (the terminal unchanged), a scratch trust store.
  ssh -tt -E ./ssh-verbose.log -o LogLevel=VERBOSE \
    -o UserKnownHostsFile=./known_hosts -o StrictHostKeyChecking=accept-new \
    -p "$PORT" "$NETUSER@$ADDRESS"
  # at the device: show clock, then exit
  echo $? >ssh.exit
  # 2. karvi: the same session, recorded, its debug stream in a file.
  karvi --debug login --record=./transcripts "$DEVICE" 2>karvi.err
  # at the device: show clock, then exit
  echo $? >karvi.exit
  ```

  Read, and write one line `D15  login-end  observe  ssh.exit=N
  karvi.exit=M` into `results.tsv` by hand:

  - `ssh.exit`: 0 is a device that sends an exit status when the session
    ends; 255 is one that closes the session without it.
  - `ssh-verbose.log`: the `Authenticated to …` and `Transferred: …` lines,
    which the roadmap's rule reads to tell an authenticated session's end
    from a failure before authentication.
  - `karvi.exit`, `karvi.err` (the `system SSH interactive session` lines,
    and `ssh_process_failed: exit status 255` when it fails so), and the
    `exit_classification` in the end line of the transcript's
    `.meta.jsonl` under `transcripts/`.

  The fake, for comparison, closes without a status: `ssh.exit` 255, both
  log lines present, `karvi.exit` 110, `ssh_process_failed`,
  `ExitConnectionFailure`. A device that sends a status should give 0 and 0;
  the fake is then corrected to send one, and karvi is unchanged. A device
  that gives the fake's figures takes the rule on the roadmap's
  device-qualification track (`ROADMAP.md`, item 5): an authenticated
  login the device closed is a completed session, exit 0, with the notice
  `login_closed_without_status`.

## 5. The evidence

```text
karvi-qualification-c9300-lab-20260921T140000Z/
  identification.txt    date, host, OpenSSH, the executable's SHA-256, `karvi version`
  version.json          the same, with the ScrapliGo version
  device-release.txt    the release line of D1's `show version`
  results.tsv           row, check, disposition (pass, fail, skip, observe), detail
  D1/ ... D14/          per invocation: TAG.out, TAG.err, TAG.exit, TAG.karvi.toml,
                        and TAG.base/ (audit.jsonl, score/, and the job directory:
                        commands.jsonl, output.TARGET.txt (the session as text, on a
                        build that writes it), errors.jsonl, failed-devices.txt,
                        summary.json, metrics.json, manifest.json with the digests)
  D7/*.known_hosts      the scratch trust store as each step left it
  D15/                  by hand: ssh.exit, ssh-verbose.log, known_hosts (the
                        scratch store), karvi.exit, karvi.err, transcripts/
                        (the transcript and its .meta.jsonl)
  SHA256SUMS            every file above
```

Against "Required evidence": the karvi version and commit, the Go and
ScrapliGo versions (`identification.txt`, `version.json`); the device
model and release (`device-release.txt` and D1's records); the command
plan digest (each `manifest.json`); sanitized records and debug output
(the final scan is the sanitization check: a hit fails the run and the
directory must not be shared); resource measurements (each record's
timing; the daemon's memory is the output-scale run's subject, section
6); the disposition (`results.tsv`). A row confirmed against the fake
names the fake and its commit in `identification.txt`.

An `observe` line is a question for the reviewers, not a pass: read the
file it names. Host names, addresses, the account name, and device output
are in the evidence in clear; they are laboratory values, and the network
operations and security owners review the directory before it leaves the
laboratory.

## 6. The output-scale run

Not a device run: `scripts/output-scale-run.sh` has N fake devices answer
a 5 MiB `show big` at once through one daemon and reads the daemon's
resident memory from `/proc`. The measurements and what they show are
`docs/SCALE.md`.

```bash
N=32 TRANSPORT=scrapligo-v1 scripts/output-scale-run.sh
```

## 7. When a row fails

1. Read the `fail` line's detail, then `ROW/TAG.err` and `ROW/TAG.out`.
2. A parity finding is in `ROW/parity.findings`, one line per path that
   differs between the transports.
3. Run the row alone: `ROWS=D7 ...`. Every invocation's configuration is
   kept as `TAG.karvi.toml`; with the scratch store recreated, the same
   command can be repeated by hand with `--debug`.
4. `docs/COMMAND-TROUBLESHOOTING.md` reads the codes. A prompt the
   platform's patterns miss shows as `command_session_prompt_timeout` with
   the last line seen: keep that line; it is what the device teaches.
5. Keep the failed evidence directory. A re-run is a new directory, and
   the pair is the record.

## 8. What the script will and will not do to the device

The script is a test harness run
by hand against a laboratory device from the operator's own account; it
is no part of karvi at run time. These are its limits, and an edit to the
script that breaks one is a defect:

1. **The device's host key is never touched.** A mismatch row writes a
   wrong entry into a scratch trust store under the evidence directory;
   the device presents its own key and karvi refuses before
   authentication. No key is regenerated and no second endpoint is used
   (a real rotation stays the hand step of section 4).
2. **The operator's files and the device's state are never changed.**
   Every karvi call names a configuration, a trust store
   (`ssh.known-hosts-file`), and a base directory under `work/`; the
   operator's `known_hosts` is compared before and after and a difference
   fails the run. No configuration line is sent and nothing is saved.
3. **Credentials live only in the environment** (`NETUSER`, `NETPASS`,
   `NETENABLE`, and D5's restricted pair). The script writes them nowhere,
   and its last step fails the run if `secret-scan` finds either secret
   in the evidence, so an evidence directory that passed may leave the
   host.
4. **Only read-only show commands are sent unless a row is opted in by
   name.** `CONFIG_ROW=1` sends `configure terminal` and `end` with
   nothing between; `RELOAD="<transports>"` reloads the unit once per
   named transport and answers `no` to a save prompt. Unset, both rows
   print `skip`.
5. **An `observe` row asserts nothing.** It records what happened and
   cannot fail. It becomes a pass-or-fail check only after a laboratory
   run has shown what a device does, and the check is written from that
   evidence, not from the fake or from reading the code.

