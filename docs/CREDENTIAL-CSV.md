# The credential CSV

A credential CSV is a credential backend of `type = "csv"`: a file of rows, each
saying which devices it serves and what username, password, and enable password
they take. This guide covers declaring one, the file's format and fields, how a
row is chosen, keys and pins (`credkey` in the file, `credkeyref` in inventory),
secrets in the file and in the environment, a formula over a credential CSV, the
file's owner and mode rules, what an inventory file may not hold, and every
error with its remedy. The decisions, the reasoning, and the alternatives not
taken are [`docs/DESIGN.md`](DESIGN.md); [`docs/ERROR-CODES.md`](ERROR-CODES.md)
holds the codes; `examples/config.toml` declares a backend and
`examples/credentials.csv` is its file, commented. Every message and table below
was run on the built binary.

## 1. A file, end to end

`~/.karvi/credentials.csv`, mode `0600`:

```
device_name,address_cidr,platform,site,device_group,credkey,username,password,enable_password
# the core pair, by name
sw-core-0?,,,,,,svc.core,********,********
,,,,,break-glass,emergency,********,********
,,,nyc,!lab,nyc-prod,svc.nyc,********,********
,10.1.0.0/16,,,,,svc.sixteen,********,
,10.0.0.0/8,,,,,svc.ten,********,
*,,,,,,,********,
```

```toml
[credential-backend.site-creds]
type = "csv"
scope = "user"
path = "~/.karvi/credentials.csv"

[credential-policy.default]
backend-sequence = ["site-creds"]
```

`karvi run --dry-run --all --format json` over six inventory rows reports,
under each target's `credential_binding`:

| Device | Row | `device_username` | `matched_on` |
|---|---|---|---|
| `sw-core-01` 10.9.0.1, site `nyc` | line 3 | `svc.core` | `pattern: device_name=sw-core-0?`, `credkey: site-creds:3` |
| `sw-nyc-02` 10.1.2.4, site `NYC`, group `access` | line 5 | `svc.nyc` | `pattern: site=nyc device_group=!lab credkey=nyc-prod`, `credkey: nyc-prod` |
| `sw-nyc-lab` 10.1.9.9, site `nyc`, groups `access;lab` | line 6 | `svc.sixteen` | `pattern: address_cidr=10.1.0.0/16`, `credkey: site-creds:6` |
| `sw-bos-01` 10.2.0.1, site `bos` | line 7 | `svc.ten` | `pattern: address_cidr=10.0.0.0/8`, `credkey: site-creds:7` |
| `sw-oob-01` 192.0.2.10 | line 8 | `netops`, the operator | `pattern: device_name=*`, `credkey: site-creds:8` |
| `sw-nyc-09`, inventory `credkeyref = Break-Glass` | line 4 | `emergency` | `pattern: credkey=break-glass`, `credkey: break-glass` |

Line 3 comes first, so the core pair never reaches the site row. `NYC` matches
`nyc`: case never matters. `sw-nyc-lab` is in the `lab` group, so line 5's
`!lab` excludes it and the next row that matches, the /16, answers. Line 4 holds
a key and nothing else, so it serves only a device pinned to it ([section
5](#5-keys-and-pins-credkey-and-credkeyref)); `sw-nyc-09` is at site `nyc` and
would otherwise have taken line 5. Line 8's blank `username` means the
operator's own name. Each `matched_on` also carries `category: csv_row`, the
file's path as `source`, and the row's `line`.

## 2. Declaring the backend

| Key | Value | Default |
|---|---|---|
| `type` | `"csv"` | |
| `scope` | `"user"` or `"shared"` ([section 8](#8-the-files-owner-and-mode)) | **none: required** |
| `path` | the file; `~/` is allowed under `user` scope, a shared file's path is absolute | **none: required** |
| `required` | a user file only: `true` makes an absent file an error | `false`; a shared file is always required |
| `mode` | `"header"` or `"numeric"` (no header row; every mapping is a column number) | `"header"` |
| `delimiter` | one character | `","` |
| `mappings.<field>` | header mode: a header name or a list of them; numeric mode: a column number from 1 | a field's own name |
| `mandatory-fields` | the fields that must have a column | `["username"]` |
| `transform` | the operator-name transform, as for any backend | |
| `env-indirection.username`, `.password`, `.enable-password` | `true` lets a cell name an environment variable ([section 6](#6-secrets-in-the-environment-instead-of-the-file)) | `false` |

Unlike `cloginrc`, `scope` and `path` have no default. A shared file read
under an assumed user scope would fail its mode check with the advice
`chmod 600`, wrong for a file a group must read; and a guessed path that
happens to exist is a credential source nobody declared.

`karvi config validate` checks the table and never opens the file. Whether
the file exists, passes its checks, and has its mandatory columns is decided
at the first resolution that reaches the backend.

| Code at `config validate` (exit 2) | The table |
|---|---|
| `config_credential_backend_scope_missing` | is `type = "csv"` with no `scope` |
| `config_credential_backend_path_missing` | is `type = "csv"` with no `path` |
| `config_credential_backend_delimiter_invalid` | has a `delimiter` that is not one character |
| `config_credential_backend_key_unsupported` | is another type and sets `mode`, `delimiter`, `mandatory-fields`, or a `mappings.<field>` key |
| `config_credential_backend_shared_path_relative` | is `scope = "shared"` with a path that is not absolute |
| `config_credential_backend_shared_optional` | is `scope = "shared"` with `required = false` |
| `config_enum_value_invalid` | has a `mode` other than `header` or `numeric`, or a `mandatory-fields` entry that is not one of the nine fields |
| `config_type_error` | has a mapping of the wrong type for its mode (a column number under `header`, a header name under `numeric`), or another key of the wrong type |
| `config_unknown_key` | has a `mappings.<name>` key for something that is not one of the nine fields (`mappings.name`: the key is `mappings.device_name`) |

## 3. The file

**Nine fields.** Six select devices and three are the result:

| Field | Matched against, or meaning | Cell |
|---|---|---|
| `device_name` | the device's canonical name (the inventory name after its source's transform; a direct target's token) | a selector |
| `address_cidr` | the address the plan selected for the device | a prefix, `10.1.0.0/16` or `2001:db8::/32`; not a pattern |
| `platform` | the device's platform, as the policy maps match it | a selector |
| `site` | the device's site | a selector |
| `device_group` | any one of the device's groups | a selector |
| `credkey` | the inventory row's `credkeyref` ([section 5](#5-keys-and-pins-credkey-and-credkeyref)); also the row's label in evidence | a literal |
| `username` | the login name; blank means the operator's own name, after the backend's `transform` | trimmed |
| `password` | the login password; may be blank ([section 4](#4-how-a-row-is-chosen)) | **kept exactly as written** |
| `enable_password` | the enable password; may be blank | **kept exactly as written** |

**Headers.** In header mode a column is found under its field's own name,
the name with hyphens (`enable-password`), or a header named by
`mappings.<field>`; case does not matter. `device_name` is also found under
the header `name`. No column is required but those in `mandatory-fields`;
columns karvi does not know are ignored. A file that holds both a field's
own header and a mapped one reads the field's own. In numeric mode there is
no header row and every field karvi is to read needs a
`mappings.<field> = N`.

**Rows.** Blank lines and lines starting with `#` are skipped. A row with
fewer cells than the columns karvi reads is an error, not a skipped row: in
a first-match file a dropped row changes which row wins. A file with a
header and no rows is valid and answers nothing.

**Quoting** is RFC 4180, the inventory reader's: `"New York"` is read as
`New York`; a quoted cell may hold the delimiter (`"p,w"`); a quote inside a
quoted cell is doubled (`"say ""hi"""`). The quote must be the cell's first
character: a space before it, or a quote in the middle of an unquoted cell,
is `credential_csv_malformed`. Each file declares its own `delimiter`, so a
credential CSV and an inventory file need not share one; under
`delimiter = ";"` a comma needs no quotes. A byte-order mark at the start
of the file, a spreadsheet's "CSV UTF-8", is dropped.

**Secret cells are verbatim.** Every other cell is trimmed; a password or
enable cell is never touched, leading and trailing spaces included, since a
password may begin or end with one. A secret cell of white space alone is
blank. karvi holds the two secret cells in its redacting secret type from
the moment the row is read.

## 4. How a row is chosen

1. **Rows are read top to bottom and the first matching row wins.** Nothing else
   orders them: there is no longest-prefix rule, so a /16 that should win over a
   /8 goes above it ([section 1](#1-a-file-end-to-end), lines 6 and 7). The
   policy maps do prefer the longer prefix; a file read top to bottom does not.
2. **A row matches when every filled selector cell agrees.** A blank cell
   says nothing. One cell holds one selector; "or" is another row.
3. **A selector** is the policy maps' grammar: the whole field, `*`, `?`,
   classes (`[0-9]`, `[!a-c]`), a backslash to escape; no regular
   expressions, and an unescaped `^` or `$` is refused. **Case never
   matters**, in names, platforms, sites, groups, and keys.
4. **A leading `!` negates the cell:** the row matches only when the device
   does not. A row needs at least one positive cell; a row of only `!lab`
   is refused when the file is loaded. A negated `address_cidr` excludes
   nothing from a device with no address, and a positive one never matches
   it.
5. **A lone `*` matches a blank field;** a name does not. A device whose
   platform is not set is matched by `platform = *` and by no platform
   name.
6. **The selected row is judged afterwards, and karvi never moves on to a
   later row because the first was incomplete.** A blank password is legal
   in the file; whether it is enough is the resolver's ordinary question:

   | Code (exit 6) | The selected row |
   |---|---|
   | `credential_password_missing` | has no password, which a row always needs (a row holds no key) |
   | `credential_enable_missing` | has no enable password and the device's platform sets `requires-enable = true` |
   | `credential_incomplete` | yields no username after the transforms |

   Blanking a password to retire it therefore stops the devices that took
   the row; it does not hand them to a broader row's credential.
7. **A device no row matches** is a not-found: the policy's next backend is
   asked, and after the last one the platform's fallback, as for any backend
   (`NETUSER`, `NETPASS`, and the prompt on the network built-ins; the
   operator's keys on `linux`).
8. **The whole file is read and checked once,** at the first resolution that
   reaches it, and kept for the run. A bad row on the last line fails a run
   whose only device the first line would have served, so a file never
   passes or fails by which targets were named.

**Evidence.** The plan report, the grant, and each command record show what
matched under `matched_on`: `category: csv_row`; `pattern`, the row's
filled selectors as written; `source`, the file; `line`; and `credkey`, the
row's own key or the label `BACKEND:LINE` for a row without one, something
to say out loud when asking why a device logged in as it did. It is not
`credential_id`, which is the grant's identifier. The evidence describes
the row and never the device, so devices that take one row share one sealed
grant. No secret and no cell of a result column appears anywhere.

## 5. Keys and pins: `credkey` and `credkeyref`

```
name,management_address,platform,site,credkeyref
sw-nyc-09,10.1.2.9,cisco_iosxe,nyc,break-glass
sw-nyc-02,10.1.2.4,cisco_iosxe,nyc,
```

**`credkey`** names a credential row. It is a literal: not blank, no `*`,
`?`, `[`, or `\`, no leading `!`; it cannot be negated; it is unique in its
file, case ignored.

**`credkeyref`** is an inventory column holding the `credkey` of the row
the device is to take. It is found under its own name, or under another
header with `mappings.credkeyref` in the inventory source (a column number
under numeric mode); it is one word on purpose, with no underscore or
hyphen spelling to get wrong. A blank cell pins nothing. A value obeys the
key's rule, and anything else fails the inventory load. The reference is
not a secret: it names a row, and the secrets stay in the credential file.
A direct target (`--host`) has no inventory row and so no pin.

| Code | Exit | What |
|---|---|---|
| `inventory_credkeyref_invalid` | 5 | an inventory row's `credkeyref` is not a legal key; the message names the file, the line, and the column, and never the cell |
| `inventory_conflict` | 5 | two rows for one device pin different keys, or one pins and the other does not |
| `credkeyref_unresolved` | 6 | a pinned device's policy ended without its key |

**A pin is a pin.** A pinned device takes the row whose `credkey` equals
its reference, and nothing else.

- The row's other filled cells still apply: a key beside `site = nyc`
  serves a pinned device only at that site.
- The row is judged by [section 4](#4-how-a-row-is-chosen)'s completeness rules,
  and is never passed over for a later backend.
- The device's policy is selected as usual and its `backend-sequence` is walked
  in order, but only backends that can honour a key are asked: today the `csv`
  type. An `env`, `cloginrc`, Redis, Vault, or formula backend is skipped for a
  pinned device, a formula over a `csv` source included ([section
  7](#7-a-formula-over-a-credential-csv)). A `csv` backend without the key is
  passed over and the next is asked.
- A failure of a keyed backend (an unsafe file, a bad row) stops a pinned
  device as it stops any other.
- If the sequence ends without the key, the device fails with
  `credkeyref_unresolved`. It never falls to a general row, to `NETUSER`
  and `NETPASS`, or to a prompt: a mistyped reference that quietly took a
  general credential would show up as failed logins, on some devices
  lockouts, with nothing naming the cause.

```
credkeyref_unresolved: client planning: 1 of 1 targets failed credential resolution:
name:sw-nyc-09 (device sw-nyc-09: credkeyref "break-glas" is unresolved: no backend of
the policy that can honour a key answered for it (asked: site-creds; skipped, cannot
honour a key: none); the key is absent, or its row's other selectors exclude the
device; a pinned device takes no general credential policy=default)
```

(Run with `NETUSER` and `NETPASS` set and [section 1](#1-a-file-end-to-end)'s
catch-all row in the file.) The remedies are the message's: correct the
reference or the key; add the keyed backend to the policy the device selects
("asked: none" says the policy has none); or clear the cell to unpin the device.
The key is quoted because it came from inventory, which holds no secret, and a
typing mistake shows only when the key does.

**A device with no reference ignores the key column.** A row with a key and
another positive cell serves both kinds: pinned devices by the key, the rest by
its other cells ([section 1](#1-a-file-end-to-end), line 5). A row whose only
positive cell is its key serves pinned devices alone (line 4): otherwise it
would be a catch-all nobody wrote.

**What a pin leaves no trace in.** The execution plan, the manifest, and
the command records do not carry `credkeyref`; the key a device took is
`matched_on.credkey`. A row with a key beside another positive cell gives a
pinned and an unpinned device the same evidence, so the records say which
credential a device took and not whether a pin chose it: that is the
inventory file's to answer, and the plan holds that file's digest. The
plan's aggregate `inventory_digest` does not move when a pin is set or
changed, as it does not for a site or a group.

## 6. Secrets in the environment instead of the file

With `env-indirection.password = true` (and `.username`,
`.enable-password`) on the backend, a cell of the *selected* row that names
a set environment variable is replaced by that variable's value, and the
field's source reads `env:NAME` rather than the file, line, and column. The
name is looked up without a secret cell's surrounding spaces; a variable
that is set and empty counts as set; no variable is read for a row no
device took.

```
site,username,password,enable_password
nyc,,NETPASS_NYC,NETENABLE
bos,,NETPASS,en-bos
```

Such a file can hold no secret at all: it becomes a device-keyed map of
variable names, which the operator-keyed `env` backend cannot be.

> **Warning: a name that is no set variable stays a literal.** With
> `NETPASS_NYC` not exported, the `nyc` devices still bind, the run exits
> 0, and the password sent is the text `NETPASS_NYC`: failed logins, on
> some devices lockouts, and nothing naming the cause. That rule lets one
> file mix names and real passwords and is the same for every backend.
> Until an explicit cell form exists, check that every variable the file
> names is set before a run (`printenv NETPASS_NYC >/dev/null || echo
> unset`). An explicit `env:NAME` cell, which needs no flag and fails with
> its own code when the variable is not set, is on the roadmap.

The dry-run report shows no secret and no field source, so it cannot show
whether a cell was taken from a variable.

## 7. A formula over a credential CSV

A formula backend may name a `csv` backend as its `password-source`. The
formula shapes the username from the operator's name; the CSV supplies what
varies by device.

```toml
[credential-backend.shaped]
type = "formula"
username-template = "%s-adm"
password-source = "site-creds"
```

```
site,username,password,enable_password
nyc,,pw-nyc,en-nyc
bos,svc.bos,pw-bos,en-bos
```

With the sequence `["shaped"]`, the devices of both sites bind as
`netops-adm`, each with its own row's secrets.

- **The row's username is dropped.** A formula takes the password fields
  from its source and never the username: `svc.bos` above is read and
  ignored, with no notice. Leave the column blank in a file only a formula
  reads.
- **A formula never answers a pin.** A pinned device skips it, whatever its
  source, since its answer would be the key's password under a username the
  key's row does not hold. Put the `csv` backend itself in the sequence for
  pinned devices (`["shaped", "site-creds"]`: a pinned device takes its
  row from `site-creds`, its unpinned neighbour takes the formula); with
  the formula alone a pinned device is `credkeyref_unresolved`, "asked:
  none; skipped, cannot honour a key: shaped".
- **A formula's evidence names the formula and its source backend only,**
  not the row: two devices that took different rows show equal
  `matched_on`. (Their grants stay apart, since the secrets differ.)
  Carrying the source's file, line, and `credkey` is on the roadmap.
- A device no row matches is a not-found for the formula, and the walk
  goes on.

## 8. The file's owner and mode

The rules are every credential file's, `.cloginrc` included
([`docs/OPERATIONS.md`, "Credential files"](OPERATIONS.md#credential-files)),
and run before a byte is parsed. karvi opens the file first and checks the file
it opened.

| | `scope = "user"` | `scope = "shared"` |
|---|---|---|
| Owner | the operator | root, or a name in `security.approved-admin-users` |
| Mode | `0600` | `0640` |
| Group | any | `security.shared-group` |
| Path | `~/` allowed | absolute |
| Absent or unreadable | not found, silently, unless `required = true` | `credential_file_unavailable` |
| A symlink | refused unless `security.allow-credential-symlinks = true` | the same |

A file that is present and fails a check fails the run whatever the scope
or `required`; a file that cannot be opened (mode `0000`) is reported by
its check and is not treated as absent.

| Code (exit 6) | The file |
|---|---|
| `credential_file_unavailable` | is absent or unreadable and the backend is required or shared |
| `credential_file_symlink_rejected` | is a symlink and symlinks are not allowed |
| `credential_file_not_regular` | is a directory, a FIFO, a device, or a socket |
| `credential_file_owner_uninspectable` | has an owner the system cannot report |
| `credential_file_owner_mismatch` | is a user file owned by someone else |
| `credential_file_mode_unsafe` | is a user file whose mode is not `0600`; the message gives the `chmod` line |
| `credential_file_shared_mode_invalid` | is a shared file whose mode is not `0640` |
| `credential_file_shared_owner_unapproved` | is a shared file owned by neither root nor an approved administrator |
| `credential_file_shared_group_unknown` | is a shared file and `security.shared-group` names no group |
| `credential_file_shared_group_mismatch` | is a shared file in another group |

## 9. An inventory file holds no secrets

The credential CSV exists so that passwords never sit in inventory, a file
with no owner or mode check that is the likeliest to be mailed, committed,
or opened in a spreadsheet. An inventory file that carries a secret column
does not load, and there is no override.

In a file with a header row, any header, whether karvi reads the column or
not, that equals or ends with `password`, `passwd`, `secret`, `token`,
`private_key`, or `passphrase` is refused (`Password`, `enable-password`,
`api_token`, `ssh_private_key`; not `token_ring` or `password_age`): case
does not matter and a hyphen or a space reads as an underscore. An
attribute mapping with such a name (`mappings.attributes.password`) is
refused at `config validate` in header mode and numeric mode alike, since
attributes travel in the plan and the records.

| Code | Exit | What |
|---|---|---|
| `inventory_secret_column` | 5 | a header names a secret; the message names the source, the file, the column's number, and the word that matched, and never the header's text or a cell |
| `config_inventory_source_secret_mapping` | 2 | `mappings.attributes.NAME` names a secret; reported with the key, the file, and the line |

```
inventory_secret_column: client planning: inventory source ex1: inv.csv: the header of
column 6 names a secret (it is or ends with "password"); an inventory file holds no
secret column: move the secrets to a credential CSV (a credential backend of type
"csv") and delete the column
```

To fix it, move the secrets into a credential CSV, select the rows by
device name, address, platform, site, or group, or pin devices with
`credkeyref`, and delete the column. The message gives a column number
rather than the header because a file with no header row has its first
device row read as the header, and that row is where a password would be.

**What the check does not find.** It reads names and never cells:

- A secret under an innocent header (`notes`), or under an innocent
  attribute name (`mappings.attributes.owner = "pw"`), is not found, and an
  attribute's cell travels in clear in the plan, the manifest, and every
  command record.
- A numeric-mode inventory file has no headers, so only its attribute
  mappings' names are checked.
- A column that holds no secret but is named like one (`rotation_token`)
  is refused all the same, and must be renamed.

The check stops the operator who names a secret and maps it, not one who
hides it; karvi cannot tell a password from a serial number and does not
guess.

## 10. What a bad credential CSV says

Every message names the backend, the file, and the line, and the column
where one is at fault, and **never quotes a cell, a selector included**: a
misplaced delimiter can put a password in any column. Read the line and
column from the message and the value from the file. All are exit 6.

| File | Message, after `credential backend NAME: FILE` |
|---|---|
| a malformed selector, `sw-[nyc` | `credential_csv_row_invalid`: `:2: column device_name: the selector is invalid: a character class is not closed with "]"` |
| a row with every selector blank | `credential_csv_row_invalid`: `:2: the row has no selector: it needs at least one positive selector cell or a credkey` |
| a row whose only selector is `!lab` | `credential_csv_row_invalid`: `:2: the row holds only negated selectors: it needs at least one positive selector cell or a credkey` |
| `username`, `password`, and `enable_password` all blank | `credential_csv_row_invalid`: `:2: the row has no result: username, password, and enable_password are all blank` |
| a `credkey` with a pattern character | `credential_csv_row_invalid`: `:2: column credkey: a key holds no pattern character (*, ?, [, \): a key is a literal` |
| `Core-Admin` on line 2 and `core-admin` on line 3 | `credential_csv_credkey_duplicate`: `: lines 2 and 3 have the same credkey` |
| a row one cell short | `credential_csv_malformed`: `: short CSV row at line 2: need column 6, got 5` |
| no `username` column | `credential_csv_malformed`: `: mandatory field "username" has no mapping` |
| no header row under header mode, a row with two equal cells | `credential_csv_malformed`: `: a header appears twice (a file with no header row needs mode = "numeric")` |
| invalid quoting, an overlong line | `credential_csv_malformed`, naming the line |

A file with no header row, read under header mode, has its first credential
row taken as the header; the usual symptom is the mandatory-field message.
Declare `mode = "numeric"` and the column numbers.

When the failure is one target's, the message is wrapped as `client
planning: N of M targets failed credential resolution: TARGET (...
policy=POLICY backend=BACKEND)`; a run whose every device the file serves
fails the same way, since the file is read once.

`run --dry-run` with no terminal reports `credential_prompt_unavailable`
when no row matched and nothing else in the policy answered: the prompt was
the last resort and there was nowhere to show it.

## 11. Where it lives in the code

| Part | Package |
|---|---|
| The file rules, the availability classes, the per-run cache | `internal/credentialbackend/credfile` |
| The backend: load, row checks, first match, evidence, field sources | `internal/credentialbackend/credcsv` |
| The row evaluator and the key's literal rule (`CompileRow`, `Row.Match`, `CheckKey`, `FoldKey`) | `internal/matching` |
| The shared reader (`Spec.Trim`, `Spec.CheckHeader`, the byte-order mark) | `tabular` |
| Environment indirection | `internal/credentialbackend/envindirect` |
| The keyed capability (`credentials.Keyed`) and the pinned walk (`Resolver.Resolve`) | `credentials`, `internal/credentialbackend` |
| `credkeyref` and the secret-column rule (`inventory.SecretColumnWord`) | `inventory`, `internal/inventoryload` |
| The table's validation and its key allow-list | `internal/configload`, `configschema` |

A later keyed store (a SQLite or JSON credential store) joins the pinned
walk by implementing `credentials.Keyed` and keeping its contract: for a
pinned request, the key's credential or not found, never a general one.
