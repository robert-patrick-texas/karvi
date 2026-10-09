# Files and directories

Every place karvi reads or writes on a host: which directories it uses in
shared mode and in individual mode, how it chooses between them, and each file
with its mode, owner, group, writer, and purpose. The modes and owners below
are the ones karvi left on a host, observed in both modes with two
operators (`netops` and `netops.test`, both in the group `netops`).

Two facts govern the rest:

- **The operator's home is the password database's**, not `$HOME`. Every `~`
  below, and the private root under `~/.local/share/karvi` with the trust
  store in it, are the home `getent passwd USER` names, whatever `HOME` holds.
- **There is no shared switch.** Each place is chosen at the start of each
  activity by looking at what exists ([section
  1](#1-how-karvi-chooses-each-place)). A site turns shared mode on by making
  the shared places once, with `sudo karvi setup shared`; a host without them is
  in individual mode, and a host can be in shared mode for some places and
  individual for others.

## 1. How karvi chooses each place

For each place karvi looks for the shared candidate first. A candidate that
does not exist is passed by without a word. One that exists but that the
operator cannot use is refused, warned about, or passed by, as the last
column says: the trees and the private root are refused, since the site made
them for the operator and a silent fall-through would split the operator's
work across two places; the scratch root's two folders are passed by with
one warning, since a job can run without them.

| Place | Key | Shared candidates, in order | Otherwise | A candidate present but unusable |
|---|---|---|---|---|
| Private root | `basedir` | `/opt/karvi/users/<user>`, `/var/lib/karvi/users/<user>` (only where the site made `users`) | `~/.local/share/karvi` | refused: `private_directory_not_real`, `private_directory_not_writable` |
| Job tree | `output.root` | `jobs` under `sharedroot`: `/opt/karvi/shared`, then `/var/lib/karvi/shared` | `<basedir>/jobs` | refused: `output_directory_not_writable` |
| Transcript tree | `transcript.root` | `transcripts` under `sharedroot`, the same roots | `<basedir>/transcripts` | refused: `transcript_directory_not_writable` |
| Collection directory | `crun.directory` | `crun` under `sharedroot`, the same roots | `<basedir>/crun` | refused: `crun_directory_not_writable` |
| Scratch | `tempdir` | `/dev/shm/karvi/<user>` (only where `/dev/shm/karvi` exists) | `<basedir>/tmp`, then `/tmp/karvi-<uid>`, then `/var/tmp/karvi-<uid>` | the next candidate |
| Control sockets | `ssh.control-path-root` | `/dev/shm/karvi/<user>/sockets` | `<basedir>/socket/ssh` | the fallback |
| Scoreboards | `scoreboards` | `/dev/shm/karvi/scoreboards` | `<basedir>/state/scoreboards` | the fallback, with one warning: `shared scoreboard directory unavailable` |
| Session ledger | `sessions.shared-capacity-root` | `/dev/shm/karvi/capacity` | `<basedir>/state/capacity` | the fallback, with one warning: `capacity_root_unusable` |
| Spool | `spooldir` | none: never shared | `/tmp/karvi-<uid>`, then `/var/tmp/karvi-<uid>` | the next candidate |
| Trust store | `ssh.known-hosts-file` | none: never shared; it follows the private root, `<basedir>/known_hosts` | `<basedir>/known_hosts` | `known_hosts` errors |
| Daemon socket | `daemon.socket` | none: never shared | `<basedir>/socket/daemon.sock` | the daemon does not start |

An explicit value of any key replaces its chain: the path is used as given,
or refused if it cannot be. `sharedroot = "none"` keeps the three trees under
`basedir` whatever exists; `sharedroot = PATH` consults that root alone. The
private root, the spool, the trust store, and the daemon's sockets are always
one operator's: `basedir` is never shared, since its `socket` and `state` are
one operator's daemon.

The scratch holds the askpass socket, whose path a Unix socket bounds at 107
bytes with a name of up to 37, so the scratch may be at most 69 bytes: `auto`
passes a longer candidate by, named on `config show --explain`'s `passed:`
line, and an explicit `tempdir` longer is refused, `tempdir_too_long`, at every
job's admission and a login's start.

Every place key and every file key (`audit.file`, an inventory source's
`path`, a credential file) takes a path by one rule: `~` and `~/…` are the
operator's home from the password database, `~user` is refused
(`path_other_user_home_unsupported`), and a relative path is taken from the
working directory of the invocation and made absolute.

No operator's run makes a place `setup shared` makes: the scratch root
`/dev/shm/karvi`, `/opt/karvi` and `/var/lib/karvi`, their `users` and
`shared`, and the trees under `shared`, whether a path reaches them by `auto`
or explicitly, since the first operator to make one would close it to every
other. An explicit value that would need one made is refused before any
device is contacted, `shared_directory_absent`, naming the place, `sudo karvi
setup shared`, and the key to set elsewhere. Inside the places that exist an
operator's run makes its own folders: its `<user>` folder in the scratch root,
a missing `scoreboards` or `capacity` folder in the root's group, its folder
under `users`, the day and job folders in the trees. `/dev/shm` is emptied at
every boot, and `systemd-tmpfiles` makes the scratch root again from the rule
`setup shared` writes; a host where the rule did not run is in individual mode
for the scratch, the scoreboards, and the ledger until it does.

**A quick look at a host.** `karvi config show KEY…` names on each key's
`resolved:` line the place the next activity would use, by the rule above and
without creating anything: `karvi config show basedir ssh.known-hosts-file`
names the operator's private root and trust store. A candidate present on the
host and passed by follows on a `passed:` line with its reason, and a place the
activity would refuse is `resolved:   error: CODE: message`. Each candidate is
judged as the activity judges it, by its permissions (`access(2)`), its owner
and mode where it is private, and the free inodes of its filesystem; what only
a write shows, a network filesystem's refusal or a quota, can still differ.
Every place at once, one line for each key that names one and for each
candidate passed by, from the configuration this invocation loads, here on a
host after `setup shared`, before the operator's first activity:

```bash
karvi config show --explain | awk '/^key:/{k=$2} /^(resolved|passed):/{print k": "$0}'
```

```text
basedir: resolved:   /opt/karvi/users/netops
crun.directory: resolved:   /opt/karvi/shared/crun
daemon.socket: resolved:   /opt/karvi/users/netops/socket/daemon.sock
inventory-source.0.path: resolved:   /mnt/lab/sb/inv.csv
output.root: resolved:   /opt/karvi/shared/jobs
scoreboards: resolved:   /dev/shm/karvi/scoreboards
sessions.shared-capacity-root: resolved:   /dev/shm/karvi/capacity
spooldir: resolved:   /tmp/karvi-1000
ssh.control-path-root: resolved:   /dev/shm/karvi/netops/sockets
ssh.known-hosts-file: resolved:   /opt/karvi/users/netops/known_hosts
tempdir: resolved:   /dev/shm/karvi/netops
transcript.root: resolved:   /opt/karvi/shared/transcripts
```

The lines come from the invocation's configuration: a daemon already running
keeps its `tempdir`, `spooldir`, `ssh.control-path-root`, `scoreboards`, and
`sessions.shared-capacity-root` until it is restarted, so a new configuration's
places are checked with the view and the daemon restarted after. The shared
places and their modes, here as `sudo karvi setup shared --group netops` leaves
them (`--mode 2775` gives `drwxrwsr-x` to the four 2770 directories under
`/opt/karvi`):

```bash
stat -c '%A %U:%G %n' /opt/karvi /opt/karvi/users /opt/karvi/shared \
  /opt/karvi/shared/* /var/lib/karvi/users /var/lib/karvi/shared \
  /dev/shm/karvi /dev/shm/karvi/* 2>/dev/null
```

```text
drwxr-xr-x root:root /opt/karvi
drwxrwx--T root:netops /opt/karvi/users
drwxrws--- root:netops /opt/karvi/shared
drwxrws--- root:netops /opt/karvi/shared/crun
drwxrws--- root:netops /opt/karvi/shared/jobs
drwxrws--- root:netops /opt/karvi/shared/transcripts
drwxrws--T root:netops /dev/shm/karvi
drwxrws--- root:netops /dev/shm/karvi/capacity
drwxrws--T root:netops /dev/shm/karvi/scoreboards
```

Lines missing from the output are places the host does not have, which the
operators take in individual mode. The operator's own folder
(`/dev/shm/karvi/<user>`) appears after the operator's first activity. `sudo
karvi setup shared` reports a directory found with another group or mode as
`repaired` and sets it right, so running it again is also the repair
([`docs/OPERATIONS.md`, "The shared trees"](OPERATIONS.md#the-shared-trees)).

## 2. Directories in shared mode

The group is the operators' group (`netops` here), `<user>` the operator's
username, `<group>` the operator's primary group. Setgid on a directory makes
what is created in it take the directory's group, which is how every file in
a shared tree stays in the operators' group; the sticky bit (`T`) keeps a
member from removing or renaming another's entry.

| Directory | Mode | Owner | Group | Made by | Holds |
|---|---|---|---|---|---|
| `/opt/karvi` | `0755` | root | root | `setup shared` | the shared trees and the operators' roots; the global configuration, if the site keeps it here |
| `/opt/karvi/shared` | `2770` | root | operators | `setup shared` | the three trees |
| `/opt/karvi/shared/jobs` | `2770` | root | operators | `setup shared` | the job tree: one day folder per day |
| `jobs/YYMMDD` | `2770` (the tree's own bits) | the first operator that day | operators | the first job of the day | that day's job folders |
| `jobs/YYMMDD/<job-id>` | `2750` (`output.directory-mode`, setgid inherited) | the job's operator | operators | each job | the job's files ([section 4.3](#43-a-jobs-files)) |
| `/opt/karvi/shared/transcripts` | `2770` | root | operators | `setup shared` | the transcript tree: one day folder per day |
| `transcripts/YYMMDD` | `2770` | the first operator that day | operators | the first recorded login of the day | that day's transcripts |
| `/opt/karvi/shared/crun` | `2770` | root | operators | `setup shared` | the collection files, one per device |
| `/opt/karvi/users` | `1770` | root | operators | `setup shared` | the operators' private roots |
| `/opt/karvi/users/<user>` | `0750` | the operator | `<group>` | the operator's first activity | the private root (`basedir`) and the trust store |
| `<basedir>/socket` | `0700` | the operator | `<group>` | the private root's first use | the daemon's two sockets |
| `<basedir>/state` | `0700` | the operator | `<group>` | the same | the daemon's state file |
| `<basedir>/logs` | `0750` | the operator | `<group>` | the same | the daemon's log |
| `<basedir>/jobs`, `<basedir>/transcripts` | `0750` | the operator | `<group>` | the same | nothing while the shared trees exist |
| `/dev/shm/karvi` | `3770` | root | operators | `setup shared`; at boot, `systemd-tmpfiles` | the scratch root |
| `/dev/shm/karvi/scoreboards` | `3770` | root | operators | the same | every operator's scoreboards, what `karvi watch` reads |
| `/dev/shm/karvi/capacity` | `2770` | root | operators | the same | the host's session ledger |
| `capacity/devices` | `2770` | root | operators | `setup shared`; at boot, `systemd-tmpfiles` | one ledger per device |
| `/dev/shm/karvi/<user>` | `2700` (setgid inherited) | the operator | operators | the operator's first activity | the operator's scratch (`tempdir`) |
| `/dev/shm/karvi/<user>/sockets` | `2700` | the operator | operators | the same | the exec devices' control sockets |
| `/tmp/karvi-<uid>` | `0700` | the operator | `<group>` | each activity | the output spool |

In shared mode the operator's home holds nothing of karvi's but the operator's
configuration (`~/.config/karvi/config.toml`), when there is one.

`/var/lib/karvi` takes the place of `/opt/karvi` throughout on a site that
made its trees and `users` there by hand; `setup shared` makes `/opt/karvi`.
A site that made `shared` and not `users` keeps every operator's private root
under the home while the trees are shared; the reverse keeps the trees under
each private root.

## 3. Directories in individual mode

Every place is under the operator's private root, except the spool. The
group is the operator's primary group throughout.

| Directory | Mode | Made by | Holds |
|---|---|---|---|
| `~/.local/share/karvi` | `0750` | the first activity | the private root (`basedir`) and the trust store |
| `<basedir>/jobs` | `0750` (`output.directory-mode`) | the first activity | the job tree |
| `jobs/YYMMDD` | `0750` | the first job of the day | that day's job folders |
| `jobs/YYMMDD/<job-id>` | `0750` | each job | the job's files ([section 4.3](#43-a-jobs-files)) |
| `<basedir>/transcripts` | `0750` | the first activity | the transcript tree |
| `transcripts/YYMMDD` | `0750` | the first recorded login of the day | that day's transcripts |
| `<basedir>/crun` | `0770` (`crun.directory-mode`) | the first collection | the collection files |
| `<basedir>/socket` | `0700` | the first activity | the daemon's two sockets |
| `<basedir>/socket/ssh` | `0700` | the first activity | the exec devices' control sockets |
| `<basedir>/state` | `0700` | the first activity | the daemon's state file, the scoreboards, the ledger |
| `<basedir>/state/scoreboards` | `0700` | the first activity | the operator's scoreboards |
| `<basedir>/state/capacity`, `capacity/devices` | `0700` | the first activity | the operator's session ledger |
| `<basedir>/logs` | `0750` | the first activity | the daemon's log |
| `<basedir>/tmp` | `0700` | the first activity | the scratch (`tempdir`) |
| `/tmp/karvi-<uid>` | `0700` | each activity | the output spool |

A private root the site made by hand is taken as it is, whatever its mode; an
existing `socket` or `state` must be a real directory the operator owns with
no group or other access, or the activity is refused.

## 4. Files

The mode, owner, and group of each file karvi writes. "Shared" and
"individual" give the mode in each; the owner is the operator whose activity
wrote it unless the row says otherwise, and the group is the operators' group
in a shared place and the operator's primary group in a private one.

### 4.1 What karvi reads

| File | Where | Required owner and mode | Purpose |
|---|---|---|---|
| The global configuration | `/etc/karvi/config.toml`, else `/opt/karvi/config.toml` (the first that exists) | the site's | the site's settings; the only file that may declare locks (`[config-lock]`) |
| The operator's configuration | `~/.config/karvi/config.toml` | the operator's | the operator's settings, unless the global file sets `config.allow-user-layer = false` |
| `--config PATH` | as given: a file, or a directory whose `.toml` files are read | the operator's | read after the two above, in order |
| An included file or directory | as a configuration's `include` directives name it | regular files; no directory symlinks | part of the including file |
| An inventory source | `[[inventory-source]] path` | readable | the devices: name, address, platform, groups |
| A credential file (CSV, `.cloginrc`) | its backend's `path` | user scope: the operator, `0600`; shared scope: root or an approved admin, `0640`, `security.shared-group` | the devices' credentials ([`docs/CREDENTIAL-CSV.md`, section 8](CREDENTIAL-CSV.md#8-the-files-owner-and-mode)) |
| A command or target file | `--cf PATH`, `--tf PATH`, `--tfr PATH` | readable | the commands or targets of one run |
| The trust store | `<basedir>/known_hosts` | written by karvi, `0600` | read before every SSH session (4.2) |

### 4.2 The operator's own files

| File | Where | Shared | Individual | Lifetime | Purpose |
|---|---|---|---|---|---|
| `known_hosts` | `<basedir>/` (`ssh.known-hosts-file`) | `0600` | `0600` | kept | the trust store: every host key karvi accepted ([`docs/SSH-HOST-KEY-POLICY.md`](SSH-HOST-KEY-POLICY.md)) |
| `daemon.sock` | `<basedir>/socket/` | `0600` socket | `0600` socket | while the daemon runs | the client's requests to the daemon; peer credentials checked |
| `credentials.sock` | `<basedir>/socket/` | `0600` socket | `0600` socket | while the daemon runs | the credential package, one frame per connection under a one-use token |
| `daemon.json` | `<basedir>/state/` | `0600` | `0600` | while the daemon runs | the running daemon's status (pid, socket, version, schema, jobs), removed when it stops |
| `daemon.log` | `<basedir>/logs/` | `0600`, appended | `0600`, appended | kept | one line per request outcome, the token redacted |
| `karvi-ssh-<pid>-*.conf` | the scratch (`tempdir`) | `0600` | `0600` | one system-transport session; one a karvi killed outright left is swept at the next admission, daemon start, or login | the `ssh` configuration karvi writes for the session, named by its maker's pid |
| `askpass-<pid>-<16 hex>.sock` | the scratch | `0600` socket | `0600` socket | one authentication; one a karvi killed outright left is swept as above | where `karvi-askpass` fetches the secret `ssh` asks for, named by its maker's pid |
| `<16 hex>` | the control sockets (`ssh.control-path-root`) | `0600` socket | `0600` socket | one exec device session | an exec device's OpenSSH ControlMaster, a client of it per command; one a killed master left is swept at the next daemon start or admission |
| `karvi-script-<pid>-*.timing` | the scratch | `0600` | `0600` | one recorded login | `script(1)`'s timing log, the terminal's widths, read when the transcript is rendered at the session's end and removed then; a session killed before its end leaves it, swept as above |
| `<activity>.<device>.<index>.<pid>.spool` | the spool (`spooldir`) | `0600` | `0600` | one command | a response past `output.spool-threshold-bytes`, removed once its record is written; one left by a process that died is swept at the next daemon start or admission |
| The audit file | `audit.file`, when set | `0600`, appended | `0600`, appended | kept | the audit events, beside journald (always written) |

### 4.3 A job's files

In the job folder `jobs/YYMMDD/<job-id>`, every file `0640`: in shared mode
in the operators' group, so any member reads another's job (`karvi job
follow ID`). Each is written as the job runs, so a job cut short leaves what it
had; each has its switch, `output.files.*`, and `--nof` writes no folder
([`docs/OPERATIONS.md`, "The job's output files"](OPERATIONS.md#the-jobs-output-files)).

| File | Switch | Purpose |
|---|---|---|
| `manifest.json` | `manifest-json` | the job as it was accepted: the plan, the targets, the configuration that mattered |
| `commands.jsonl` | `commands-jsonl` | every command's record, one JSON line each; what `job follow` replays |
| `commands.txt` | `commands-txt` | the job's commands, as a command file for a rerun (`--cf`) |
| `commands.<platform>.txt` | `commands-txt` | a collection's commands, one file per platform |
| `errors.jsonl` | `errors-jsonl` | the records that did not succeed (each is also in `commands.jsonl`) |
| `failed-devices.txt` | `failed-devices-txt` | the devices that failed, as a target file for a rerun (`--tf`) |
| `output.<target>.txt` | `output-txt` | each device's session as text |
| `metrics.json` | `metrics-json` | the job's timings, widths, and wave decisions |
| `summary.json` | `summary-json` | the job's result: the counts, the exit, the paths; written last |

### 4.4 Transcripts and collection files

| File | Where | Shared | Individual | Owner | Purpose |
|---|---|---|---|---|---|
| `<device>-HHMMSS.log` | `transcripts/YYMMDD/` | `0640` | `0640` | the operator who logged in | a recorded login (`login --record`), as the terminal showed it ([`docs/LOGIN-TRANSCRIPTS.md`](LOGIN-TRANSCRIPTS.md)) |
| `<device>-HHMMSS.meta.jsonl` | the same | `0640` | `0640` | the same | the recording's metadata |
| `<device>[SUFFIX]` | the collection directory | `0660` (`crun.file-mode`) | `0660` | the operator who last collected the device | the device's collected output, replaced only when the device succeeds ([`docs/COLLECTION.md`](COLLECTION.md)) |
| `.<file>.<job-id>` | the same | `0660` | `0660` | the collecting operator | the replacement while it is written, renamed over the file; a stale one is swept by its job ID |

### 4.5 The shared scratch: scoreboards and the session ledger

| File | Where | Shared | Individual | Owner | Purpose |
|---|---|---|---|---|---|
| `<job-id>.json` | the scoreboards | `0640` | `0600` | the job's operator | the job's live progress, what `karvi watch` reads; one per activity |
| `server.json` | the ledger | `0660` | `0600` | **the operator who last changed it** | the host's leases: one entry per device session in flight, across every operator in shared mode |
| `server.lock` | the ledger | `0660` | `0600` | the operator who first made it, kept | the lock every writer holds (`flock`) while it rewrites `server.json`; its content is never read |
| `devices/<sha256>.json` | the ledger | `0660` | `0600` | the last writer | one device's leases, against its session cap; named by the SHA-256 of the device's name |
| `devices/<sha256>.lock` | the ledger | `0660` | `0600` | the first creator, kept | that ledger's lock |

A ledger file is rewritten whole, through a temporary file set to its mode
and renamed over it, so its owner becomes the operator who last wrote it and a
reader never sees half a file; its group is the directory's. A lease names its
job, process, start time, and boot, and an entry whose process is gone is
dropped at the next change, so a stopped job or a reboot leaves no lease.
Between runs, with no session active, the ledger holds no lasting state; it is
never removed while jobs run, since a lock file removed mid-run would let two
writers race.

The shared modes come from the folder: under a folder with the setgid bit, the
ledger's files take the folder's permission bits without search (`2770` gives
`0660`) and its folders the folder's bits with setgid, so every member takes
and releases leases in files another member made. A file at `0600`, or outside
the operators' group, in a shared ledger is one an earlier release left: other
operators meet `capacity_admission_failed … permission denied` until its
owner's next run sets it right, or the site resets it.

### 4.6 What the site's commands write

| File | Mode | Owner | Written by | Purpose |
|---|---|---|---|---|
| `/etc/tmpfiles.d/karvi.conf` | `0644` | root | `sudo karvi setup shared` | the rule that makes `/dev/shm/karvi`, `scoreboards`, and `capacity` at every boot, in the group and modes setup used; a file there without karvi's first line is the site's and is left |
| `/etc/bash_completion.d/karvi` | `0644` | root | `sudo karvi setup tab` | Tab completion for bash |

### 4.7 What the package installs

| File | Mode | Purpose |
|---|---|---|
| `/usr/bin/karvi` | `0755` | the program |
| `/usr/bin/karvi-askpass` | `0755` | the helper `ssh` runs for a secret; never run by an operator |
| `/usr/bin/karvi-prune` | `0755` | retention: removes old job and transcript folders ([`docs/PRUNE.md`](PRUNE.md)) |
| `/usr/share/man/man1/karvi*.1`, `/usr/share/man/man8/karvi-prune.8` | `0644` | the manual pages |
| `/usr/share/doc/karvi/` | `0644` | the top-level documents (`README.md`, `CHANGELOG.md`, `LICENSE.md`, `BUILD-HOWTO.md`, …), `docs/`, `release/`'s records, `examples/`, and `configs/` (`example.toml`, `development.toml`, and `reference.toml` a link to `/usr/share/karvi/reference.toml`), in the source tree's layout so their links and the `configs/` files they name hold |
| `/usr/share/karvi/systemd/user/`, `/usr/share/karvi/systemd/system/` | `0644` | the units and timers, per operator and for the site |
| `/usr/share/karvi/cron/` | `0755` | the cron scripts `karvi-crun` and `karvi-prune` |
| `/usr/share/karvi/crun/` | `0644` | the `crun.after` hook examples |
| `/usr/share/karvi/tmpfiles.d/karvi.conf` | `0644` | the tmpfiles rule for the group `netops`; `sudo karvi setup shared` writes the live one |
| `/usr/share/karvi/reference.toml`, `/usr/share/karvi/schema/` | `0644` | every configuration key with its default, and the records' schemas |
| `/usr/share/lintian/overrides/karvi` | `0644` | the package's two deliberate `lintian` findings with their reasons: static executables, and hook examples not executable |

Nothing under `/usr/share/karvi` is active until the site copies it into place,
each guide giving the copy; a host installed from the release bundle
([`BUILD-HOWTO.md` section 10](../BUILD-HOWTO.md#10-install-and-roll-back))
finds the same files in the bundle's `packaging/`, `configs/`, `schema/`, and
`docs/`. The cron script `karvi-crun` holds a lock at
`${TMPDIR:-/tmp}/karvi-crun.<uid>.lock` (`KARVI_CRUN_LOCK`) for the length of a
collection.

## 5. Signs a host is not as intended

- A shared directory without its setgid bit or outside the operators' group,
  or `/dev/shm/karvi/capacity` with the sticky bit: run `sudo karvi setup
  shared` again, which reports what it repaired.
- `/dev/shm/karvi` at `0700` and owned by an operator: a release before 0.26.0
  made it on that operator's first run; `setup shared` repairs it.
- An operator's job, transcript, or collection under the private root on a
  host with the shared trees: that activity ran with `sharedroot = "none"` or
  an explicit `output.root`, `transcript.root`, or `crun.directory`.
- `<basedir>/state/scoreboards` or `<basedir>/state/capacity` filling on a host
  with the scratch root: the operator's runs passed the shared folders by, each
  with its warning ([section 1](#1-how-karvi-chooses-each-place)), or ran before
  the boot rule made them.
- A `known_hosts` or a private root missing where `HOME` points: karvi takes
  the home from the password database, so a run under another `HOME` still
  writes to the account's own.
