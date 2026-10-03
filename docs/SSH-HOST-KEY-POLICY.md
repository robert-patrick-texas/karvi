# SSH host-key policy

karvi retains one host-key policy and one karvi-owned trust store for
`login`, `command`, and `run`, independent of whether the implementation is
system OpenSSH or a configured native adapter. Transport selection is documented in `SSH-TRANSPORTS.md`.

## Configuration

```toml
[ssh]
host-key-policy = "accept-new"
known-hosts-file = "auto"
halt-run-on-host-key-mismatch = false
```

`host-key-policy` and `known-hosts-file` are lock-eligible. When the policy is
not locked, any operator may select `insecure`; lock the policy to forbid it.

## Policy modes

### `accept-new` — default

- A previously unseen public host key is accepted and added to the karvi trust
  store.
- A matching enrolled key is accepted.
- A changed key is rejected as `host_key_changed`.

System OpenSSH receives:

```text
StrictHostKeyChecking accept-new
UserKnownHostsFile "<karvi-known-hosts>"
GlobalKnownHostsFile "/dev/null"
```

On `scrapligo-v1`, karvi's callback in the SSH handshake reads the same
file: a key found under the device's identity must match; an unknown device
is enrolled as one line under the file lock (re-read while locked, so two
concurrent connections enrol once) and the acceptance is printed as a
warning naming the fingerprint. No `ssh-keyscan` runs.

### `secure`

A matching key must already exist. Unknown keys fail as
`host_key_not_enrolled`; mismatches fail as `host_key_changed`.

System OpenSSH receives `StrictHostKeyChecking yes`. On `scrapligo-v1` the
handshake callback refuses an unknown device as `host_key_not_enrolled` and
a changed key as `host_key_changed`; the connection is closed before
authentication. A missing trust store is `host_key_not_enrolled` before any
connection.

### `insecure`

Unknown and changed keys are accepted. karvi emits a prominent warning for
every connection. When an enrolled comparison key exists and differs, karvi
also emits a mismatch-specific warning containing only public fingerprints.

System OpenSSH receives `StrictHostKeyChecking no` and isolated known-hosts
inputs; when the store holds the device, karvi runs a bounded `ssh-keyscan`
comparison beside the connection to produce the mismatch warning (the one
`ssh-keyscan` left). On `scrapligo-v1` the handshake callback accepts the
key, prints the warning, and compares it to the enrolled one itself.

This mode permits machine-in-the-middle impersonation and should be locked out
in governed production environments.

## The identity a key is enrolled under

Both transports enrol and check a device's key under one identity, not
under its address:

| Device | Identity |
|---|---|
| canonical name `router1`, port 22 | `router1` |
| canonical name `router1`, port 2222 | `[router1]:2222` |

Another port on the same host may be another SSH server with another key,
so the port is part of the identity. The system transport passes the
identity to OpenSSH as `HostKeyAlias`, so OpenSSH neither looks up nor
writes the address; `scrapligo-v1` looks up only the identity. A line
`accept-new` writes reads

```text
[router1]:2222 ssh-ed25519 AAAA... karvi-auto-enrolled
```

Entries enrolled before this rule for a port other than 22, under the bare
name by system OpenSSH or as `[name]:port,[address]:port` by the earlier
native adapter, no longer match: `accept-new` enrols the device again and
`secure` refuses it until it is enrolled under the identity. Port 22
entries are unchanged.

## Which key types are offered

Under `accept-new` and `secure`, a device the store already holds is
offered only the host-key algorithms of its enrolled key types, so the
match is decided on a key the store knows rather than on whichever type the
device prefers. A store that holds only types the configured list does not
offer is `host_key_changed` before any connection. Under `insecure`, or for
an unknown device, the whole configured list is offered. The list is
`[ssh-algorithms] host-key` in `configs/reference.toml`; both transports
offer the names they implement (`docs/SSH-TRANSPORTS.md`).

## Trust-store selection and permissions

`known-hosts-file = "auto"` selects:

1. `~/.local/share/karvi/known_hosts`, the operator's own root as the
   path resolver creates it (mode 0750; the directory rule refuses one
   that group or others can write). There is no fallback location.

In `accept-new` policy, karvi creates the preferred usable location. In `secure`
policy, absence is a hard failure. The containing karvi directory must be real,
operator-owned, and mode `0700`; the file must be regular, operator-owned,
and non-symlink.

karvi sets permissions only on what it creates and never changes the
permissions of a file that exists: an unacceptable mode is refused, not
repaired. For the store's mode that gives, the same with `"auto"` as with an
explicit path:

| Policy | No store | A store of mode `0600` | A store of any other mode |
|---|---|---|---|
| `accept-new` | created `0600` (its directory `0700` if missing) | used | refused: `host_key_trust_store_permission`, the message names `chmod 600`; the file is not changed |
| `secure` | refused: `host_key_not_enrolled` | used | refused the same way |
| `insecure` | continues; nothing is created | read, to warn of a mismatch | continues: it takes no trust from the store |

The directory, the file's type, and its owner are checked under all three
policies. A symbolic link at the store's path is refused and nothing is
created through it.

An explicit path may be configured:

```toml
[ssh]
known-hosts-file = "~/.local/share/karvi/known_hosts"
```

## Controlled enrollment

The line must carry the device's identity (above), not the address
`ssh-keyscan` prints, or neither transport will find it. Scan the address
and port, rewrite the host field, and verify the fingerprint before
appending:

```bash
install -d -m 0700 "$HOME/.local/share/karvi"
install -m 0600 /dev/null "$HOME/.local/share/karvi/known_hosts"

ADDRESS=192.0.2.10
PORT=22
IDENTITY=router1            # the inventory's canonical name on port 22;
                            # "[router1]:2222" for a device on port 2222
TMP="$(mktemp)"
ssh-keyscan -T 5 -p "$PORT" "$ADDRESS" | sed "s/^[^ ]* /$IDENTITY /" >"$TMP"
ssh-keygen -lf "$TMP"
```

Verify the fingerprint through an independent authoritative channel before
accepting it:

```bash
cat "$TMP" >>"$HOME/.local/share/karvi/known_hosts"
chmod 0600 "$HOME/.local/share/karvi/known_hosts"
rm -f "$TMP"
```

`ssh-keyscan` discovers the key presented on the network; it does not prove the
identity of that endpoint. karvi reads plain and hashed host fields; the
identity is a name the operator chose, so there is nothing to hide by
hashing it.

## Changed-key behavior in direct modes

`login` and `command` reject a mismatch under `accept-new` or `secure`, return
`ExitHostKeyFailure` (109), and print a stable diagnostic even when routine
narration is quiet.

Investigate replacement, reimaging, DNS/inventory changes, NAT/bastion changes,
or possible interception before changing the enrolled key.

## Changed-key behavior in fleet runs

By default, a mismatch is a device-local failure. It is recorded as:

```json
{
  "status": "connection_error",
  "error": {
    "code": "host_key_changed",
    "category": "connection",
    "external": true
  }
}
```

Other devices continue unless an ordinary run halt or wave gate trips.

To make a mismatch a dedicated run-wide scheduling halt:

```toml
[ssh]
halt-run-on-host-key-mismatch = true
```

When enabled:

1. the first terminal `host_key_changed` result sets halt reason
   `host_key_mismatch`;
2. no new device is admitted;
3. all devices already in flight finish;
4. never-started command-plan entries are written as `not_started_halt` with
   reason `halt_host_key_mismatch`; and
5. the process returns `ExitHaltHostKeyMismatch` (114).

This control is orthogonal to `dispatch.halt-on-error-count`,
`dispatch.halt-on-error-percent`, and wave-local gates. It is checked as a
specific error-code halt before generic count/percentage evaluation.

`karvi-login(1)` (`man karvi login`), HOST KEYS, restates the policies,
the trust store, the identity, and the controlled enrollment for the
terminal; a change to one changes both.

## Breaking changes in v0.10.0

Configuration schema 6 renames the default policy from `auto` to `accept-new`.
The values `auto` and `default` are rejected with no alias; replace them with
`accept-new`. Configuration files that declare `schema-version = 5` must be
updated to `6`.

## Breaking changes in v0.5.0

Configuration schema 2 carries no host-key migration aliases. These old keys
are unknown and fail validation:

```text
ssh.strict-host-key-checking
ssh.user-known-hosts-file
native-ssh.strict-host-key-checking
native-ssh.known-hosts-file
```

Use only:

```text
ssh.host-key-policy
ssh.known-hosts-file
ssh.halt-run-on-host-key-mismatch
```

## Diagnostics

```bash
karvi config validate /path/to/config.toml
karvi config show --explain ssh.host-key-policy
karvi config show --explain ssh.known-hosts-file
karvi config show --explain ssh.halt-run-on-host-key-mismatch
stat -c '%U %a %n' "$HOME/.local/share/karvi" \
  "$HOME/.local/share/karvi/known_hosts"
```
