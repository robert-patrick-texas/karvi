# Quick start

## Inspect the executable

```bash
./bin/karvi --version
./bin/karvi login --help
./bin/karvi command --help
./bin/karvi run --help
```

The included executable lists `system` and `scrapligo-v1`. `command` and
`run` use the native transport by default and `login` uses `system`; pass
`--transport` to choose.

## After upgrading from an older daemon

```bash
karvi daemon status
karvi daemon restart
```

A schema mismatch blocks job submission but does not require the older binary:
karvi can explicitly stop released older lifecycle schemas. Normal runs never
replace an incompatible daemon automatically.

## Minimal configuration

```toml
[config]
schema-version = 6

[ssh.run]
transport = "system"
```

Use `NETUSER`, `NETPASS`, and when required `NETENABLE` for the built-in
zero-configuration credential fallback of the network platforms; at a
terminal, karvi asks for what they do not give, and Ctrl-C at the prompt
returns to the shell ([`docs/OPERATIONS.md` "The credential
prompts"](OPERATIONS.md#the-credential-prompts)). A `linux` server takes the
operator's own keys instead ([`docs/OPERATIONS.md`, "The platform's fallback
and the operator's
keys"](OPERATIONS.md#the-platforms-fallback-and-the-operators-keys)).

## Login

```bash
karvi login router1
karvi login --4 --host router1
karvi login --record --host router1
```

## One-device commands

```bash
karvi cmd router1 --cmd 'show clock' --cmd 'show version'
karvi command --ipv6 --host router1 --echo --cmd 'show clock'
karvi command --border --host router1 --cmd 'show clock' --cmd 'show version'
karvi command --noborder --host router1 --cmd 'show clock'
```

## Fleet run

```bash
karvi run --target router1 --target router2 --transport system \
  --dispatch parallel --workers 2 --cmd 'show clock'

karvi run --4 --tf routers.txt --transport system --border \
  --cmd 'show version'

# Rehearse first: plan on the client only, then validate in the daemon
karvi run --dry-run --tf routers.txt --transport system --cmd 'show version'
karvi run --exercise --tf routers.txt --transport system --cmd 'show version'

# Submit and return at once; read the summary when the job ends
karvi run --detach --tf routers.txt --transport system --cmd 'show version'

# Probe each target twice with ICMP first; skip any that answers neither
karvi run --ping --tf routers.txt --transport system --cmd 'show version'
```

## Output formats

```bash
karvi command --host router1 --format text --cmd 'show clock'
karvi command --host router1 --format jsonl --cmd 'show clock'
karvi command --host router1 --format json --cmd 'show clock'
```
