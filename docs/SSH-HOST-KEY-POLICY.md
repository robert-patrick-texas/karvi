# SSH host-key policy

karvi retains one host-key policy and one karvi-owned trust store for `login`,
`command`, and `run`, independent of whether the implementation is system
OpenSSH or a configured native adapter. Transport selection is documented in
[`SSH-TRANSPORTS.md`](SSH-TRANSPORTS.md).

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

Unknown and changed keys are accepted, and nothing is stored. A job under
`insecure` says so once, at its admission, in two lines on standard error in
the warning colour, `--quiet` included; through the daemon they ride the
job's receipt (a daemon's job runs under the daemon's own policy). A login
says them before its contact.

```text
! ssh host-key policy insecure: unknown and changed keys accepted;
!  connecting to devices with wrong keys and MITM attacks allowed
```

When the store holds the device under another key, the device's first record
carries the notice `host_key_mismatch_accepted` with both keys' public
fingerprints, the client shows `! ssh host-key mismatch DEVICE proceeding at
risk` in the error colour, and the audit event's `details` name both keys. A
new key is not announced.

System OpenSSH receives `StrictHostKeyChecking no` and isolated known-hosts
inputs; when the store holds the device, karvi runs a bounded `ssh-keyscan`
comparison beside the connection (the one `ssh-keyscan` left). When it cannot
complete (not installed, past its five seconds, the device refusing the extra
connection, no usable key) the session goes on uncompared, and the first
record carries `host_key_not_compared` with a short cause and the whole
reason, shown as `! ssh host-key DEVICE not compared: CAUSE`, one of
`ssh-keyscan not installed`, `ssh-keyscan timed out`, `ssh-keyscan failed`,
`no usable key`, `trust store unreadable`. On `scrapligo-v1`
the handshake callback accepts the key and compares it to the enrolled one
itself.

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
offer the names they implement ([`docs/SSH-TRANSPORTS.md`](SSH-TRANSPORTS.md)).

## Trust-store selection and permissions

`known-hosts-file = "auto"` selects `<basedir>/known_hosts`, the operator's
private root
([`docs/FILES.md`, section 1](FILES.md#1-how-karvi-chooses-each-place)):

| The host | The store under `auto` |
|---|---|
| the site made `/opt/karvi/users` (`sudo karvi setup shared`) | `/opt/karvi/users/<user>/known_hosts` |
| the site made `/var/lib/karvi/users` alone | `/var/lib/karvi/users/<user>/known_hosts` |
| neither | `~/.local/share/karvi/known_hosts` |
| `basedir` set to a path | `<that path>/known_hosts` |

There is no fallback location, and a store elsewhere, such as one an earlier
release made in the home on a host with `users`, is not read.
`karvi config show --explain ssh.known-hosts-file` names the file on its
`resolved:` line, before anything exists.

In `accept-new` policy, karvi creates the store. In `secure` policy, absence is
a hard failure. The containing directory must be real, operator-owned, and
writable by neither group nor others (the private root karvi makes is `0750`);
the file must be regular, operator-owned, and non-symlink. One store for every
operator cannot pass these checks, so each operator has an own store.

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

An explicit path may be configured. `~` is the operator's home from the
password database, `~user` is refused, and a relative path is taken from the
working directory of the invocation and made absolute, as in every place key
([`docs/FILES.md`](FILES.md#1-how-karvi-chooses-each-place)); a path whose
folder would be a place `sudo karvi setup shared` makes is refused before any
device (`shared_directory_absent`):

```toml
[ssh]
known-hosts-file = "~/trust/known_hosts"
```

## Controlled enrollment

The line must carry the device's identity (above), not the address
`ssh-keyscan` prints, or neither transport will find it. Scan the address
and port, rewrite the host field, and verify the fingerprint before
appending:

```bash
STORE=$(karvi config show --explain ssh.known-hosts-file | sed -n 's/^resolved: *//p')
mkdir -p -m 0700 "$(dirname "$STORE")"   # an existing one is left as it is
[ -e "$STORE" ] || install -m 0600 /dev/null "$STORE"

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
cat "$TMP" >>"$STORE"
chmod 0600 "$STORE"
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
karvi config show ssh.host-key-policy ssh.known-hosts-file ssh.halt-run-on-host-key-mismatch
STORE=$(karvi config show --explain ssh.known-hosts-file | sed -n 's/^resolved: *//p')
stat -c '%U %a %n' "$(dirname "$STORE")" "$STORE"
```
