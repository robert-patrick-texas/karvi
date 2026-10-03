# The collection run: `karvi crun`

`karvi crun` collects the output of one or more commands from one or more
devices and stores, per device, one file named by the device in one flat
directory, replaced only when that device's collection succeeded. It is
karvi's replacement for `rancid-run` and for the Oxidized collector: one
directory of one file per device, ready for `git diff`, a cron, and an
operator's before-and-after look around a change. The design is
`docs/DESIGN.md`, the collection run; the operator's short form is
`docs/OPERATIONS.md` "The collection run". This document is the guide to
what to collect: the command
lists RANCID and Oxidized send to each platform, consolidated per karvi
platform as examples, and how a site sets its own lists per platform and
per device model.

## 1. What a collection is

```bash
karvi crun --all                                   # each device its platform's list
karvi crun --select-platform cisco_nxos            # one platform
karvi crun --target core-nyc-01.example.net        # one device, before a change
karvi crun --target core-nyc-01.example.net --cmd 'show running-config'
karvi crun --all --cf site-collect.txt             # one list for every device
karvi crun --all --dry-run                         # each device's list, no device contacted
```

- **The file** is `DIR/NAME`: the device name's first label lowercased
  (`core-nyc-01.example.net` → `core-nyc-01`; `output.crop-to-dot = false`
  keeps the whole name), or an address with its dots and colons as hyphens.
  `--fs=SUFFIX` appends a literal suffix to every file's name (`--fs=.cfg`
  writes `core-nyc-01.cfg`), never to the directory's; it holds no `/`,
  NUL, or control character (`crun_suffix_invalid`). Two devices that
  would share a file name are refused at planning.
- **The content** is each command's output under a marker line
  `! COMMAND`, a blank line before every marker but the first, and nothing
  else: no header, no prompt, no sent statement, no timestamp, no error
  line. A rejected statement's block is the device's own error text.
- **Replacement only on success.** The file is written as a hidden
  temporary and renamed into place when every command of the device came
  back; a device not reached, timed out, halted, or cancelled keeps its
  previous file and leaves nothing behind. The display ends, after the
  footer, with `! collection=DIR replaced=N kept=M`
  (`display.collection.footer`; under jsonl the summary document carries
  the counts); `failed-devices.txt` in the job folder is the rerun.
- **The directory** is `crun.directory`: `auto` is the site's shared
  `/opt/karvi/shared/crun` when `sudo karvi setup shared` made it, else
  `<basedir>/crun`; `--cd=PATH` names another for one run. A shared
  directory has mode `2770` or `2775` in the operators' group, and every
  member replaces every file.
- **The commands** are the line's `--cmd`, `--cf`, or device text, sent to
  every device; with none, each device is sent its platform's
  `crun-commands` list, and a device whose platform has no list is refused
  at planning. The job folder holds `commands.PLATFORM.txt` per list sent.
- **The job folder** is the collection's audit: `commands.jsonl` with every
  record, `summary.json` with the `collection` block naming each device's
  file and outcome, `manifest.json` with the plan. `--nof` collects with no
  job folder.
- **The drop list** `crun-filters` of the device's platform leaves out
  of the collection file the output lines that change at every collection
  without the device having changed (section 6); the record keeps them.
- **The hook** `crun.after` is an executable the client runs once the
  collection has ended and its display is printed (section 5): in the
  collection directory, the replaced files' names on stdin, the job in the
  environment. A failure is a warning; the run's exit code stands.

### 1.1 A run's collection: `--cd` on `run` and `command`

`run` and `command` given `--cd=PATH` write the same file into PATH beside
their usual job folder, for an operator's capture rather than the site's
nightly collection:

```bash
cd ~/change-4411
karvi run --site nyc --cmd 'show running-config' --cmd 'show version' --cd=.
karvi command --cd=. core-nyc-01 show ip route summary
```

- The file's name, shape, and replacement are the collection's above;
  `--cd=PATH` is resolved as `crun.directory` is (`~` expanded, a relative
  path from the working directory, `auto` the collection tree) and checked
  once before any device, and a site that locks `crun.directory` refuses
  it.
- The file is **unfiltered**: `crun-filters` are `crun`'s, so the uptime
  and byte-count lines a `crun` drops are kept. A run's file written into
  the `crun` tree therefore differs from the next `crun`'s by those lines.
- `--continue-device-on-error` stays the run's own: without it a rejected
  statement ends the device, its later commands are not attempted, and
  the previous file is kept; with it the file is replaced, the rejection
  in its block. A `crun` always continues.
- The job folder is the run's as without `--cd`, `output.NAME.txt`
  included, so a device whose file was kept has the folder's text file
  saying what happened this time; `--nof --cd` collects with no folder.
- The display ends with the same collection line after the footer; no
  `crun.after` hook runs, whatever the directory; the watch screen shows
  `run` or `cmd`.
- `--fs=SUFFIX` without `--cd` is `--cd=.` as well: `karvi run --site nyc
  --cmd 'show running-config' --fs=.cfg` writes `core-nyc-01.cfg` and its
  neighbours into the working directory. A failure to resolve or prepare
  that directory says "the working directory, implied by --fs". On `crun`,
  `--fs` alone keeps `crun.directory`, so a scheduled `crun --all
  --fs=.cfg` stays in the site's tree.
- In a stream, `--cd=PATH` and `--fs=SUFFIX` are option lines that stay,
  `--clear` keeping them and `--reset` removing them; a later job to a
  device replaces the file an earlier job wrote, and a new `--fs` line
  keeps the two apart:

  ```text
  --target core-nyc-01
  --fs=.ver
  show version
  --go
  --fs=.run
  show running-config
  --go
  ```

  leaves `core-nyc-01.ver` and `core-nyc-01.run` in the working directory.

## 2. The built-in lists

Every built-in platform with a configuration ships the shortest list that
makes a collection worth committing: the configuration, then the version.

| Platform | Built-in `crun-commands` |
|---|---|
| `cisco_iosxe` | `show running-config`, `show version` |
| `cisco_iosxr` | `show running-config`, `show version` |
| `cisco_nxos` | `show running-config`, `show version` |
| `juniper_junos` | `show configuration`, `show version` |
| `arista_eos` | `show running-config`, `show version` |
| `generic`, `linux` | none: a `crun` over such a device names its commands |

A site's `[platform.NAME] crun-commands` replaces the built-in list whole;
the lists below are what to replace it with.

## 3. What RANCID and Oxidized collect, per karvi platform

The lists are taken from RANCID's `rancid.types.base` (the `cisco`,
`cisco-nx`, `cisco-xr`, `junos`, and `arista` types) and Oxidized's models
(`ios`, `nxos`, `iosxr`, `junos`, `eos`) as they stand in 2026. Oxidized
collects little beyond the configuration and keeps the rest as commented
context; RANCID collects the hardware and file-system state in detail and
carries a long tail of commands for hardware it once supported (every
`dir /all` variant of a 7500 or 6500, `show c7200`, `show spe version`),
which is left out here. Each list is in the order the two tools send it:
the version and inventory first, the configuration last, so that a device
whose configuration is long fails late and the file that was kept is the
whole picture. A command a device does not know is a rejected statement:
its block is the device's error text and the file is still replaced, so a
list can carry a command only some models answer, at the cost of one error
block in those that do not.

### 3.1 `cisco_iosxe` (Cisco IOS and IOS XE)

```toml
[platform.cisco_iosxe]
crun-commands = [
  "show version",                 # both tools
  "show inventory",               # both tools (RANCID: show inventory raw)
  "show redundancy",              # RANCID (dual supervisor / dual RP)
  "show install active",          # RANCID (IOS XE install mode)
  "show boot",                    # RANCID (show bootvar on older IOS)
  "show license summary",         # RANCID (show license udi, show license feature)
  "show env all",                 # RANCID
  "show module",                  # RANCID (chassis; show switch detail on a stack)
  "show vtp status",              # both tools
  "show vlan",                    # RANCID
  "show sdm prefer",              # RANCID (Catalyst)
  "show system mtu",              # RANCID (Catalyst)
  "show diag",                    # RANCID
  "dir /all bootflash:",          # RANCID (dir /all nvram:, flash:, disk0:, harddisk: as fitted)
  "show debug",                   # RANCID
  "show running-config",          # both tools; RANCID also 'show running-config view full'
]
```

Not carried from the two tools: RANCID's `show idprom backplane`,
`show rsp chassis-info`, `show gsr chassis`, `show controllers`,
`show diagbus`, `show c7200`, `show spe version`, `show cellular 0
profile`, `show hw-programmable all`, `show dot1x`, `show activation-key`,
`show shun`, and `show capture` (the last three are ASA/PIX), and the
thirty `dir /all` variants; Oxidized's `show running-config view full`
(role-based CLI only).

### 3.2 `cisco_nxos` (Cisco Nexus)

```toml
[platform.cisco_nxos]
crun-commands = [
  "show version",                        # both tools
  "show version build-info all",         # RANCID
  "show inventory all",                  # Oxidized (RANCID: show inventory)
  "show module",                         # RANCID
  "show module xbar",                    # RANCID (modular chassis)
  "show module fex",                     # RANCID (fabric extenders)
  "show fex",                            # RANCID
  "show system redundancy status",       # RANCID
  "show license usage",                  # RANCID (show license, show license host-id)
  "show boot",                           # RANCID
  "show environment power",              # RANCID (also clock, fan, temperature)
  "show interface transceiver",          # RANCID
  "show vlan",                           # RANCID
  "show vtp status",                     # RANCID, marked "drop?"
  "show cores vdc-all",                  # RANCID
  "show debug",                          # RANCID
  "dir bootflash:",                      # RANCID (dir logflash:, usb1:, volatile: as fitted)
  "show running-config",                 # both tools
]
```

RANCID first sends `term no monitor-force`, a session setting, not a
collection command; karvi's platform table holds such lines in
`paging-commands`. Not carried: `show processes log vdc-all`, `show
environment fex all fan`, `dir debug:`, `dir slot0:`, `dir usb2:`.

### 3.3 `cisco_iosxr` (Cisco IOS XR)

```toml
[platform.cisco_iosxr]
crun-commands = [
  "show version",                        # RANCID: admin show version
  "show inventory all",                  # Oxidized (RANCID: admin show inventory raw)
  "show platform",                       # Oxidized
  "show redundancy",                     # RANCID
  "show install active",                 # RANCID (admin show install active as well)
  "show license udi",                    # RANCID (admin)
  "show hw-module fpd location all",     # RANCID (admin; line-card firmware)
  "show environment all",                # RANCID (admin show env all)
  "show vlan",                           # RANCID
  "show rpl maximum",                    # RANCID
  "show debug",                          # RANCID
  "dir /all harddisk:",                  # RANCID (dir /all disk0:, bootflash:, nvram: as fitted)
  "admin show running-config",           # RANCID (admin show running)
  "show running-config",                 # both tools
]
```

RANCID sends `terminal no-timestamp` or `terminal exec prompt no-timestamp`
first, a session setting; on karvi it is a `paging-commands` line for a
site that wants it. The `admin` forms differ between 32-bit IOS XR and
IOS XR 64-bit (eXR), where `admin show …` is a separate mode; a site
keeps the forms its images answer.

### 3.4 `juniper_junos` (Juniper Junos)

```toml
[platform.juniper_junos]
crun-commands = [
  "show version",                              # both tools (RANCID: show version detail)
  "show version invoke-on other-routing-engine", # RANCID (dual RE)
  "show chassis hardware",                     # both tools (RANCID: detail, and models)
  "show chassis routing-engine",               # RANCID
  "show chassis fpc detail",                   # RANCID
  "show chassis firmware",                     # RANCID
  "show chassis environment",                  # RANCID
  "show chassis alarms",                       # RANCID
  "show system alarms",                        # RANCID
  "show system license",                       # both tools (Oxidized: show system license keys as well)
  "show system boot-messages",                 # RANCID
  "show system core-dumps",                    # RANCID
  "show configuration",                        # both tools (Oxidized: | display omit)
]
```

Oxidized adds `show chassis fabric reachability` on an MX960, `show
virtual-chassis` on EX and QFX, and `show chassis cluster status` on an
SRX: model lists, which is what section 4's sub-platform tables are for.
Not carried: RANCID's `show chassis clocks`, `show chassis scb`, `sfm`,
`ssb`, `feb`, `cfeb` (M and T series hardware).

### 3.5 `arista_eos` (Arista EOS)

```toml
[platform.arista_eos]
crun-commands = [
  "show version",                        # RANCID
  "show inventory",                      # both tools (Oxidized: | no-more)
  "show boot-config",                    # RANCID
  "show boot-extensions",                # RANCID
  "show extensions",                     # RANCID
  "show env all",                        # RANCID
  "dir flash:",                          # RANCID
  "diff startup-config running-config",  # RANCID
  "show running-config",                 # both tools (Oxidized: | no-more | exclude ! Time:)
]
```

Oxidized's `| exclude ! Time:` drops the stamp line on the device; on
karvi the built-in `crun-filters` of `arista_eos` drop it in the file
(section 6).

## 4. A site's lists: per platform, and per model as sub-platforms

A `[platform.NAME]` table for a built-in name overrides that platform's
fields; a table for any other name is an alias that names its base with
`driver = "<built-in>"` and inherits the whole built-in definition (the
prompts, the enable step, the paging commands, the built-in command list
and drop list; not the site's table for the base name). An alias is a
sub-platform: a name for one model, or one role, of a base platform, with
its own `crun-commands`. This is the recommended way to collect what each
model answers without sending every model every command:

```toml
# Catalyst 9300 stacks: the stack, the SDM template, the licences
[platform.c9300]
driver = "cisco_iosxe"
crun-commands = [
  "show version", "show inventory", "show switch detail", "show sdm prefer",
  "show license summary", "show vlan", "show vtp status", "show running-config",
]

# Catalyst 9500 with StackWise Virtual
[platform.c9500]
driver = "cisco_iosxe"
crun-commands = [
  "show version", "show inventory", "show stackwise-virtual", "show redundancy",
  "show module", "show license summary", "show running-config",
]

# ISR and Catalyst 8000 edge routers: install mode and the platform
[platform.c8300]
driver = "cisco_iosxe"
crun-commands = [
  "show version", "show inventory", "show platform", "show install active",
  "show license summary", "show running-config",
]

# Nexus 9500 modular: the modules, the fabric, the power
[platform.nx9500]
driver = "cisco_nxos"
crun-commands = [
  "show version", "show inventory all", "show module", "show module xbar",
  "show system redundancy status", "show environment power",
  "show interface transceiver", "show license usage", "show running-config",
]

# Nexus 9300 fixed: no xbar, no redundancy
[platform.nx9300]
driver = "cisco_nxos"
crun-commands = [
  "show version", "show inventory all", "show module", "show license usage",
  "show interface transceiver", "show running-config",
]

# ASR 9000: the line cards' firmware and the admin plane
[platform.asr9k]
driver = "cisco_iosxr"
crun-commands = [
  "show version", "show inventory all", "show platform", "show redundancy",
  "show install active", "show hw-module fpd location all",
  "admin show running-config", "show running-config",
]

# MX480: dual routing engines and the fabric
[platform.mx480]
driver = "juniper_junos"
crun-commands = [
  "show version", "show version invoke-on other-routing-engine",
  "show chassis hardware", "show chassis routing-engine", "show chassis fpc detail",
  "show chassis fabric reachability", "show system license", "show configuration",
]

# EX4300 virtual chassis
[platform.ex4300]
driver = "juniper_junos"
crun-commands = [
  "show version", "show chassis hardware", "show virtual-chassis",
  "show system license", "show configuration",
]

# SRX cluster
[platform.srx]
driver = "juniper_junos"
crun-commands = [
  "show version", "show chassis hardware", "show chassis cluster status",
  "show system license", "show configuration",
]

# Arista 7050 leaf
[platform.dcs7050]
driver = "arista_eos"
crun-commands = [
  "show version", "show inventory", "show boot-config", "show extensions",
  "show running-config",
]
```

How the sub-platform reaches a device:

1. **The inventory names it.** The inventory row's platform field carries
   the sub-platform's name (`c9300`, `nx9500`) where it carried the base;
   the alias inherits everything else from its driver, so nothing about
   the session changes. A site whose inventory already holds a model
   column can map that column as the platform field
   (`[inventory-source.mappings] platform = ["model"]`) and name the alias
   tables after the model values, provided each value is a valid platform
   name (compared without regard to case, no glob character, not
   beginning with `!`).
2. **`--platform NAME` on the line** runs one device or one invocation as
   a sub-platform without touching the inventory (`karvi crun --target
   sw-lab-01 --platform c9300`); `--select-platform 'c9*'` selects every
   device of the 9000-series aliases for one collection.
3. **The plan carries a list per platform name**, the alias under its own
   name, so a `crun --all` over a fleet of ten sub-platforms sends each
   device its model's list, and the job folder holds
   `commands.c9300.txt`, `commands.nx9500.txt`, and so on. `--dry-run`
   shows each device's list before any device is contacted.
4. **A record says which platform ran** (`platform` in `commands.jsonl`),
   so the audit names the sub-platform, and the file in the collection
   directory is named by the device as before: the model does not appear
   in the file name.

Two rules keep the lists honest. An alias inherits the built-in
definition of its driver, not a site's `[platform.cisco_iosxe]` table, so
an alias without `crun-commands` of its own sends the built-in list (the
configuration and the version), whatever the site set on the base; a
model's table names its whole list. A command only some models answer
belongs on the model's table, not on a base table that other models share,
so that no device collects an error block every day.

## 5. The commit and the diff mail: the hook

`crun.after` names one executable the client runs once a `crun` has ended
and its display is printed, on the in-process path and through the
daemon alike, never for `--detach` (a detached run has no client at its
end; a cron does not detach). The hook runs in the collection directory,
reads the replaced files' names on standard input, one per line, sorted
(empty when nothing was replaced), and finds the job in its environment:

| Variable | Value |
|---|---|
| `KARVI_JOB_ID` | the job's ID |
| `KARVI_JOB_DIR` | the job folder (empty under `--nof`) |
| `KARVI_CRUN_DIRECTORY` | the collection directory, the working directory too |
| `KARVI_CRUN_REPLACED`, `KARVI_CRUN_KEPT` | the counts of the collection line |
| `KARVI_EXIT` | the run's exit code |

Its output, both streams, goes to karvi's stderr after the display, so a
`--format jsonl` stdout stays records. A hook that cannot start, exits
non-zero, or runs past `crun.after-timeout` (`5m` by default, `1s` to
`1h`; the hook's process group is sent SIGTERM, then killed) is the warning
`crun_after_failed`; the collection stands and the run's exit code is
unchanged, and the audit holds a `crun.after.succeeded` or
`crun.after.failed` event with the path, the exit code, and the duration.

Two hooks ship as examples under `packaging/crun/`, for a git repository
the site made once over the collection directory (`git init` there, the
operators' group able to write `.git`):

- `crun-git-commit.example`: `git add` the files on stdin and commit with
  the job ID and the counts as the message; nothing staged, nothing
  committed. A device kept (not reached) leaves its previous file in the
  working tree, so every commit holds the last good collection of every
  device.
- `crun-diff-mail.example`: the same commit, then `git show --stat -p` of it
  to `sendmail`, one mail per collection with the subject in `rancid-run`'s
  form (`HOST config diffs`), the devices kept listed first from the job
  folder's `failed-devices.txt`; `KARVI_CRUN_MAIL_TO` names the recipient.

```toml
[crun]
after = "/etc/karvi/crun-diff-mail"     # copied without .example, mode 0755
after-timeout = "5m"
```

```text
$ karvi crun --all
…
! exit=101 elapsed=4m12s artifacts=/opt/karvi/shared/jobs/260926/260926-103409-00
! collection=/opt/karvi/shared/crun replaced=212 kept=3
$ git -C /opt/karvi/shared/crun log --oneline -1
10738a1 crun 260926-103409-00: 212 replaced, 3 kept, exit 101
```

The schedule that runs the collection every night is section 7; the hook
runs in the tick's process, so git's identity and the mail transport are
the collecting operator's.

## 6. The volatile lines: the drop list

A collected file holds lines that change at every collection without the
device having changed: on IOS XE the byte count (`Current configuration :
512 bytes`), `ntp clock-period`, and the uptime; on NX-OS `!Time:`; on EOS
`! Time:`, the uptime, and the free memory. Left in, every diff, commit,
and mail carries them. A platform's `crun-filters` is its drop list:
regular expressions in Go's syntax, one per entry, each matched against an
output line of a block without its terminator; a line that matches any
entry is left out of the collection file and nowhere else. The `! COMMAND`
marker lines are never matched, so an emptied block keeps its marker; the
record, `commands.jsonl`, and `output.NAME.txt` keep every line, and the
plan carries the lists per platform, so a run through the daemon drops the
same lines and the manifest shows which patterns applied.

The built-in lists drop only lines that carry no information about the
device, and never the stamp that says when, and on many platforms by whom,
the configuration last changed (`! Last configuration change at …`,
`!Running configuration last done at:`, `## Last commit:`):

| Platform | Built-in `crun-filters` |
|---|---|
| `cisco_iosxe` | `^Building configuration\.\.\.$`, `^Current configuration : \d+ bytes$`, `^ntp clock-period \d+$`, ` uptime is `, `^Load for five secs`, `^Time source is ` |
| `cisco_iosxr` | `^Building configuration\.\.\.$`, ` uptime is ` |
| `cisco_nxos` | `^!Time: `, ` uptime is ` |
| `arista_eos` | `^! Time: `, `^Uptime: `, `^Free memory: ` |
| `juniper_junos` | none: `show version` has no uptime, and `## Last commit:` is a stamp worth keeping |
| `generic`, `linux` | none |

A site's array replaces the built-in list whole, `crun-filters = []` turns
the filter off for that platform, and an alias (a model table of section
4) inherits its driver's built-in list unless it sets its own. Write the
patterns in TOML literal strings, so a backslash stays one:

```toml
[platform.cisco_iosxe]
crun-filters = [
  '^Building configuration\.\.\.$',
  '^Current configuration : \d+ bytes$',
  '^ntp clock-period \d+$',
  ' uptime is ',
  '^Load for five secs',
  '^Time source is ',
  '^ip route-cache flow$',          # the site's own addition
]

[platform.c9300]
driver = "cisco_iosxe"
crun-filters = []                   # this model's files raw
```

A pattern that does not compile refuses the configuration before any
device is contacted (`config_platform_crun_filter_invalid`, exit 2, naming
the table and the entry). A list only some models need belongs on the
model's table, as the command lists do. The reference for what other
tools drop is RANCID's per-type filters and Oxidized's model clean-ups;
karvi drops less by design, since the stamps
are the history a diff wants.

## 7. The schedule

A recurring collection is the site's systemd timer or cron over `karvi
crun --all`, with the hook doing the rest. It is not a configuration key
and not a schedule the daemon keeps: a resident
schedule pulls against the daemon's idle exit, the hook runs in the
client, and the credentials are resolved per job, while systemd and cron
give the calendar, the catch-up after downtime, the jitter, one instance
at a time, and the journal. Two forms ship under `packaging/`.

**The systemd user timer**, on the same pattern as `karvi-prune.timer`:

```bash
# as the collecting operator, whose credential backend answers without a
# prompt (docs/CREDENTIAL-CSV.md); a git identity in ~/.gitconfig for the hook
install -d ~/.config/systemd/user
cp /usr/share/doc/karvi/packaging/systemd/user/karvi-crun.service \
   /usr/share/doc/karvi/packaging/systemd/user/karvi-crun.timer ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now karvi-crun.timer
loginctl enable-linger "$USER"          # the timer runs with no session open
systemctl --user list-timers karvi-crun.timer
journalctl --user -u karvi-crun.service --since today
```

`karvi-crun.timer` fires at 02:15 with up to fifteen minutes of random
delay, and `Persistent=true` runs a collection the host missed at its next
start. `karvi-crun.service` is a oneshot over `karvi crun --all
--no-daemon --format jsonl`: systemd never starts it while the previous
run is still active, and a tick that elapses meanwhile is dropped, not
queued, so two collections never overlap in one directory. `--no-daemon`
is deliberate: a oneshot's control group ends with its main process, and
a daemon the tick launched would be terminated with it; a site whose
daemon runs under `karvi-daemon.service` may drop the option and submit
to it instead. The unit's `ReadWritePaths` cover the basedir candidates,
the shared trees under both system roots, and the scoreboards, each path
with the dash that ignores an absent one; a collection directory elsewhere
is added there the same way (`docs/OPERATIONS.md` "Retention" says what
the sandbox is). The hook runs inside the unit, so a git
repository over the collection directory is written there too.

**The cron**, where a site schedules with cron:

```text
# /etc/cron.d/karvi-crun, as the collecting operator
15 2 * * *  netops  /usr/share/karvi/cron/karvi-crun >> /var/log/karvi-crun.jsonl 2>&1
```

`karvi-crun` runs `karvi crun --all --no-daemon --format jsonl` with the
arguments appended (`--select-platform`, `--cd=PATH`, `--transport`),
under a lock it holds for the run's duration (`flock`, per user under
`TMPDIR`; `KARVI_CRUN_LOCK` names another path). A cron starts a tick
whatever the previous one is doing, so a tick that finds the lock held is
skipped with one line on stderr and exit 75, never queued:

```text
karvi-crun: skipped: the previous collection is still running (/tmp/karvi-crun.1000.lock)
```

**What karvi does not do.** It does not refuse a second `crun` into one
directory. Every file is whole, since it is renamed into place; the only
harm of an overlap is one device collected by two runs landing in finish
order, which the scheduler prevents at the source; and an operator's
`crun --target core-nyc-01` before a change, while the nightly run is
collecting, must not be turned away. `crun --every DURATION` under the
daemon is on the roadmap and unlikely to be pursued, for the reasons
above.

## 8. Related documents

- `docs/DESIGN.md`, the collection run: the design questions, the hook,
  the drop list, and the schedule, with the reasons.
- `docs/OPERATIONS.md` "The collection run" and "The shared trees".
- `karvi-crun(1)` (`man karvi crun`), COLLECTION: the terminal's
  restatement of section 1, the hook's input, and the drop list; a change
  to one changes both.
- `configs/example.toml`: the `[platform.NAME]` tables' shape.
- `packaging/systemd/user/karvi-crun.service` and `karvi-crun.timer`,
  `packaging/cron/karvi-crun`: the schedule's two forms.
