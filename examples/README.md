# examples

One commented example of each file karvi reads, in the shape a site keeps
them. Every comment explains the line it precedes; the guides they point
at are under `docs/`.

| File | What it is | Comments |
|---|---|---|
| `config.toml` | a site configuration: an inventory source, three credential backends and the policy that orders them, a platform alias, the transports, dispatch, display, and the job's folder mode | `#` anywhere (TOML) |
| `inventory.csv` | an inventory file: a header the configuration maps, then one device per row with its address, platform, transport, site, groups, cap, and a `credkeyref` pin | a line whose first byte is `#` |
| `credentials.csv` | a credential CSV: six selector columns, a key, and the three result columns, first matching row wins | a line whose first byte is `#` |
| `cloginrc` | a RANCID `.cloginrc`, read as data: `add user` and `add password` lines | `#` to the end of the line |
| `targets.txt` | a target file for `--tf`: one device name per line | a line beginning with `#` or `!` |
| `commands.txt` | a commands file for `--cf`: one device command per line, sent as written | none: every line is sent to the device, so a comment line would be too |

The passwords in `credentials.csv` and `cloginrc` are placeholders. A
user-scoped credential file is refused unless the operator owns it at mode
0600, so copy them to their places first:

```sh
install -m 0600 examples/credentials.csv ~/.karvi/credentials.csv
install -m 0600 examples/cloginrc ~/.cloginrc
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
