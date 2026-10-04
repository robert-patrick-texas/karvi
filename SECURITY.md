# karvi security

## Reporting a vulnerability

Report a suspected vulnerability privately to rob-karvi@rpatrick.com, not in
a public issue. Say what you observed, the release or commit you ran, and how
to reproduce it; do not include device credentials or transcripts from a
production network. Reports are acknowledged and answered by mail, and a fix
ships as a release whose changelog names the problem once the fix is
available. The sections below describe the security model the program
follows.


## SSH host-key policy

`ssh.host-key-policy` is the single host-identity policy for `karvi login`,
`karvi command`, and `karvi run`. It applies to both system OpenSSH and the
native ScrapliGo v1 adapter.

### `auto` / `default` — recommended general default

- A previously unknown key is accepted and persisted on first use.
- A later mismatch fails the connection.
- System OpenSSH is configured with `StrictHostKeyChecking accept-new`.
- Native SSH performs a public-key preflight, race-safe enrollment, then opens
  ScrapliGo with strict verification against the same file.
- First-use acceptance is vulnerable to an attacker already positioned in the
  path. Organizations requiring authenticated enrollment must use `secure`.

### `secure` — pre-enrollment required

- The trust file must already exist and contain a matching key.
- Unknown and changed keys fail before authentication.
- karvi does not create an empty trust file as a substitute for enrollment.
- Administrators should verify fingerprints through an independent trusted
  channel before adding a key.

### `insecure` — explicit risk acceptance

- Unknown and changed keys are accepted.
- A prominent machine-in-the-middle warning is written to stderr for every
  connection.
- When a karvi trust-store entry exists, karvi attempts a public-key comparison;
  a mismatch receives a distinct warning but remains allowed.
- A failed best-effort comparison also warns and remains allowed by policy.
- `insecure` MUST NOT be selected merely to work around an unexplained key
  change. It is intended only for explicitly approved environments whose risk
  model accepts host impersonation.

`insecure` disables both user and global known-hosts inputs for the actual
system-OpenSSH connection so a changed key cannot unexpectedly disable password
authentication while the operator has explicitly selected this mode. The
karvi-owned file remains available only as a comparison source for warnings.

## Trust-store ownership and permissions

The automatic location is `known_hosts` in the operator's private root
(`basedir`), and nowhere else: `/opt/karvi/users/<user>/known_hosts` where the
site made the operators' roots, `~/.local/share/karvi/known_hosts` otherwise.
`karvi config show --explain ssh.known-hosts-file` names it on its `resolved:`
line.

The immediate directory must be a real, operator-owned directory that neither
group nor others can write. The file must be a real, operator-owned,
non-symlink regular file with mode `0600`. An explicit path is subject to the
same checks. Existing but invalid state is a hard error rather than a fallback
trigger.

## Secure enrollment

The recipe is
[`docs/SSH-HOST-KEY-POLICY.md`, "Controlled enrollment"](docs/SSH-HOST-KEY-POLICY.md#controlled-enrollment):
it finds the store by the `resolved:` line, writes each key under the device's
identity, and has the fingerprint verified before the line is appended.

`ssh-keyscan` discovers a key but does not authenticate it.

## Key rotation

A mismatch in `auto` or `secure` is a security event, not an automatic rotation.
Confirm the device change through an independent channel, remove only the old
entry, and enroll the verified replacement. Do not switch to `insecure` as a
silent rotation mechanism.

## Run failure isolation

A host-key failure belongs to one target. A multi-target run writes the failure
record and proceeds with other devices. Normal run-wide halt thresholds and
wave gates still apply. This isolation prevents one replaced or compromised
host from suppressing evidence for the rest of a fleet operation.

## Credential boundary

Passwords are resolved per operator and are not put in SSH arguments.
System OpenSSH receives them through the authenticated, one-use
`karvi-askpass` callback. The transport/driver adapter receives short-lived
copies through karvi-owned secret wrappers. Audit records contain the operator,
device-facing username, backend, and safe match metadata, never secret values.

## Legacy cryptography

Legacy SSH algorithms are enabled only for explicitly scoped host patterns.
All-host patterns are rejected. Host-key policy and algorithm compatibility are
separate controls: accepting a key does not authorize weak algorithms, and
allowing a legacy algorithm does not bypass host identity verification.

## Telnet

Telnet requires both an explicit transport selection and
`security.allow-telnet=true`. It is never an SSH fallback and provides no host
identity or confidentiality.
