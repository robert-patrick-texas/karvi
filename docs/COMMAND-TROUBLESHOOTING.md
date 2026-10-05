# Command-mode troubleshooting

## Recommended first capture

```bash
karvi command --debug --host DEVICE --cmd 'show clock' \
  >command.out 2>command.debug
```

Keep stdout and stderr separate. Stdout contains generated human display and
device results; stderr contains safe karvi diagnostics and mandatory warnings.

## What debug contains

- configuration digest and loaded source list;
- target, canonical name, scope position, and dispatch context;
- transport selector, resolved implementation, and implementation kind;
- selected management address, source, candidate count, and resolution time;
- device-facing username, credential backend, policy, and match provenance;
- capacity admission and wait time;
- SSH binary, address, port, host-key policy, and session lifecycle;
- observed prompt, command index, command byte count, command SHA-256 prefix,
  output byte count, elapsed time, and stable error code.

It does not contain password values, enable-password values, raw command text,
or device output. Before sharing a diagnostic capture, still review it for
organization-sensitive hostnames, addresses, usernames, paths, and policy
names.

## `Master refused session request: Permission denied`

This diagnostic indicates that an already authenticated OpenSSH master could
not open an additional session channel. It is not evidence that the password
was rejected. Karvi v0.7.0 command mode avoids that topology by opening one
fresh interactive shell with ControlMaster and ControlPath disabled, and
reuse is off for every mode.

When this text appears from another path, karvi classifies it as
`ssh_session_channel_refused` in the connection category. Do not rotate
credentials solely on this message.


## Prompt/command missing from a device error

Version 0.9.0 renders the effective prompt and exact sent command when `--echo`
is enabled, even for a device-reported command error. Recognized karvi options
may follow positional command words. Use `--` when a literal device command
contains a token such as `--echo`, `--border`, or another karvi option name.

## Prompt timeout

A `command_session_prompt_timeout` means OpenSSH started but karvi did not
observe a final prompt ending in `#`, `>`, or `$` before the bounded timeout.
Collect debug output and verify:

1. the device is actually presenting an interactive shell;
2. no legal/banner workflow is waiting for additional input;
3. the prompt fits the supported engineering-candidate pattern;
4. `execution.prompt-timeout` is reasonable for the device; and
5. inventory platform metadata is correct.

## Platform appears as `generic`

Generic mode can run simple commands, but platform-specific paging suppression,
privilege acquisition, and error patterns require a qualified platform mapping.
For Cisco IOS XE inventory, set the canonical platform to `cisco_iosxe` before
relying on those behaviors.

## Output truncation or timeout

Large commands should be tested separately before wide fan-out. Use a command
timeout appropriate for the device, inspect the retained command record, and
avoid enabling paging. The production large-output spooling gate remains open
in this engineering candidate.
