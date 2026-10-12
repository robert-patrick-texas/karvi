# examples

One commented example of each file karvi reads, in the shape a site keeps
them. Every comment explains the line it precedes; the guides they point
at are under `docs/`.

| File | What it is | Comments |
|---|---|---|
| `config.toml` | a site configuration: an inventory source, three credential backends and the policy that orders them, a platform alias, platform resolution, the collection run, the transports, dispatch, display, and the job's folder mode; commented, the shapes a site adds: a shared `.cloginrc`, a Vault store with its TLS trust, a credential helper, a drop list, start statements for generic devices, session-init, algorithm profiles, and an audit file without journald | `#` anywhere (TOML) |
| `inventory.csv` | an inventory file: a header the configuration maps, then one device per row with its address, platform, transport, site, groups, cap, and a `credkeyref` pin | a line whose first byte is `#` |
| `credentials.csv` | a credential CSV: six selector columns, a key, and the three result columns, first matching row wins | a line whose first byte is `#` |
| `cloginrc` | a RANCID `.cloginrc`, read as data: `add user` and `add password` lines | `#` to the end of the line |
| `targets.txt` | a target file for `--tf`: one device name per line | a line beginning with `#` or `!` |
| `commands.txt` | a commands file for `--cf`: one device command per line, sent as written | none: every line is sent to the device, so a comment line would be too |
| `karvi-credh` | a credential helper: a POSIX `sh` script that prints `NETUSER`, `NETPASS`, and `NETENABLE` from `pass show` as `export` lines, with commented lines for `secret-tool`, `vault kv get`, and `git credential fill` | `#` to the end of the line (`sh`) |

The passwords in `credentials.csv` and `cloginrc` are placeholders. A
user-scoped credential file is refused unless the operator owns it at mode
0600, so copy them to their places first:

```sh
install -m 0600 examples/credentials.csv ~/.karvi/credentials.csv
install -m 0600 examples/cloginrc ~/.cloginrc
```

The helper runs where `creds.helper` names it, or as `karvi-credh` beside the
karvi executable; the package installs none. karvi refuses a helper that its
group or others may write, so install it where it is to run, test it as karvi
reads it, and name it in the configuration:

```sh
install -m 0755 examples/karvi-credh /usr/local/lib/karvi/credh
eval "$(/usr/local/lib/karvi/credh)" && echo "$NETUSER"
```

```toml
[creds]
helper = "/usr/local/lib/karvi/credh"
```

Try the set from the source root, where the relative paths resolve, without
touching a device:

```sh
karvi --config examples/config.toml config validate
karvi --config examples/config.toml run --dry-run --tf examples/targets.txt --cf examples/commands.txt
```

The dry run plans on the client: it reads the inventory, selects the targets,
resolves each device's credential row, and stops before any network contact,
reporting per device which row answered and why (`--format json` for the
record). [`docs/QUICKSTART.md`](../docs/QUICKSTART.md) takes it from there.
