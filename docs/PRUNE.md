# karvi-prune: retention

`karvi-prune` is the retention helper: the one executable behind the
per-operator timer, the site's root timer, the cron script, and the hand run. It
removes finished work older than the retention age from the trees karvi writes
and leaves everything else where it is. This guide says what it removes and why,
where it looks and in which order, how a site schedules it, and how an operator
runs it by hand. The design, with the decision that keeps retention outside the
daemon, is [`docs/DESIGN.md`](DESIGN.md).

## The short form

```bash
karvi-prune --dry-run            # what would go, as this operator: nothing removed
karvi-prune                      # remove it
karvi-prune --dry-run --verbose  # also why each item stays, and the places looked at
sudo karvi-prune --dry-run       # the site: every operator's, as root
sudo karvi-prune
```

A site that does not install the timer or the cron, or simply wants to
clean up by hand, needs nothing more: `sudo karvi-prune` is sufficient
for the default practice on a shared or site install, and any operator
runs `karvi-prune` to clean their own folders. `--dry-run` first is the
habit worth keeping.

## Why it is a helper of its own

Long-running karvi processes never delete retained history: the daemon
writes, the helper removes, and each can be
reasoned about alone. The helper reads no configuration; its flags carry
the settings' words, and a site's values live on the unit's line. It
shares two things with karvi so that the three cannot disagree: the one
list of final statuses (`records.FinalStatuses`, the list the watch
screen and the scoreboard reader use), and the one day-folder matcher
(`osutil.IsDayFolder`, beside the one layout the writers use). Both
schedules and the hand run invoke the same executable with the same
flags, so they make the same decisions.

## What goes, and when

Everything below is judged against the retention age, `--days` (31 by
default), the cutoff being now less that many days.

| Kind | What it is | Goes when | Under pressure |
|---|---|---|---|
| `activity` | a job folder whose `summary.json` has a final status: `completed`, `halted`, `errored`, `cancelled`, `incomplete`, or `exercised` | `ended_at` is before the cutoff | yes, oldest first |
| `transcript` | a recorded login: the metadata file with its end record and the transcript that shares its name | `ended_at` is before the cutoff | yes |
| `scoreboard` | a scoreboard file at a final status | `last_updated_at` is before the cutoff | yes |
| `orphan` | a job folder without `summary.json`: a job the daemon's death cut short, or one run with `output.files.summary-json` false | its day folder's date is before the cutoff, nothing in it is newer, and its scoreboard file, if any, is final or older than the age | never |
| `stale-scoreboard` | a scoreboard file at a running status that nothing has touched for the age (the file a crashed daemon's job leaves) | `last_updated_at` is before the cutoff | never |
| `day` | a `YYMMDD` day folder that holds nothing | its date is before the cutoff; removed after the run's other removals, with a plain `rmdir`, so a folder that gained a job in the meantime stays | never |

What never goes: a running job (its summary is not final, or its
scoreboard file is live), a transcript still being recorded (no end
record), a folder under a name that is not a `YYMMDD` day folder, a
symbolic link, a file the helper cannot read, the collection directory
(`crun`; the latest file per device is not retention), the audit log,
and anything in journald. Long-term audit retention is journald's and the
central forwarder's, never this helper's.

**Pressure.** `--minfree` (5 by default) is a free-space floor in
percent. A tree whose filesystem is under the floor gives up its oldest
finished items before their age, one at a time until the floor is met,
the newest kept. The floor is judged on each tree's own
filesystem, so a private root on one disk and a shared tree on another
are judged apart. Orphans, stale snapshots, and empty folders are never
taken under pressure: their state is not a finished job's. `--minfree 0`
turns pressure off.

**Ownership.** A run removes what the invoking user owns, wherever it is,
and passes every other user's item by, counted as `not_owned`. A root run
removes everything eligible. This is the rule the trees already make
real: in a shared tree a job folder is mode 2750 in its operator's name,
so a colleague can read it and cannot empty it, while root can. One
executable therefore serves both a per-operator timer and a site's root
timer.

**Failure is a line, not the end.** A removal that fails (a folder made
unwritable, a file system gone read-only) is reported as `failed` with
its reason and the run goes on to the next item; the summary counts it
and the exit is 1. A tree that cannot be walked is reported the same way.

## Where it looks, in sequence

A run walks its trees in this order and reports each under `--verbose`
as a `walk` line. It makes nothing: a place that does not exist is not
walked. The defaults, with nothing given:

**As an operator (`karvi-prune`).**

1. Every private root of the operator's that exists, `--basedir auto`, in
   this order, so what a run left under one before the site made another
   is pruned too:
   1. `/opt/karvi/users/<username>` (where the site has provisioned
      `/opt/karvi/users`, [`docs/OPERATIONS.md` "The shared
      trees"](OPERATIONS.md#the-shared-trees))
   2. `/var/lib/karvi/users/<username>` (the same under that root)
   3. `~/.local/share/karvi`

   The home is the passwd entry's, as for karvi: `HOME` does not move it.
   Under each root: `jobs/`, `transcripts/`, and `state/scoreboards/`.
2. The shared trees, `--sharedroot auto`, chosen as karvi chooses them for
   `sharedroot`, judged by permissions without writing: for each tree the
   first root that holds it,
   1. `/opt/karvi/shared/jobs`, else `/var/lib/karvi/shared/jobs`
   2. `/opt/karvi/shared/transcripts`, else `/var/lib/karvi/shared/transcripts`

   A shared tree the operator cannot write to is reported as skipped and
   the run goes on. `crun` is never walked.
3. The shared scoreboards, `--scoreboards`: `/dev/shm/karvi/scoreboards`.

The walk is by kind: the job trees (private, then shared), the transcript
trees (private, then shared), then the scoreboards (private, then shared).
Candidates from every tree are then removed oldest first.

**As root (`sudo karvi-prune`).** Root's own basedir holds no operator's
jobs, so a root run walks instead:

1. The private roots under the system roots' `users` directories, every
   username present, in this order:
   1. `/opt/karvi/users/<username>` for each
   2. `/var/lib/karvi/users/<username>` for each

   Under each: `jobs/`, `transcripts/`, and `state/scoreboards/`.
2. The shared trees, as above.
3. The shared scoreboards, as above.

A private root under an operator's home (`~/.local/share/karvi`) is
that operator's own: their `karvi-prune`, their
timer, or their cron prunes it, and a root run does not look there.

`--basedir PATH` replaces the private roots with that one, and
`--sharedroot PATH|none` the shared root, for a run over one root, as the
suites and the examples do.

## The report

One line per item on standard output, the summary last, so `> file` and
`| logger` each hold the whole; standard error carries only a usage error
or an error that stops the run.

```text
removed kind=activity path=/opt/karvi/shared/jobs/260817/260817-153859-00 age=40d bytes=14151
removed kind=transcript path=/opt/karvi/shared/transcripts/260817/core-01-153900.meta.jsonl transcript=/opt/karvi/shared/transcripts/260817/core-01-153900.log age=40d bytes=1220
removed kind=orphan path=/opt/karvi/shared/jobs/260810/260810-090000-00 age=47d bytes=10221
removed kind=stale-scoreboard path=/dev/shm/karvi/scoreboards/260817-172446-00.json age=40d bytes=1316
removed kind=day path=/opt/karvi/shared/jobs/260801 age=56d bytes=0
failed kind=activity path=/opt/karvi/shared/jobs/260817/260817-153859-02 error=unlinkat …/summary.json: permission denied
examined=31 removed=15 skipped=4 not_owned=0 failed=1 bytes=137313 dry_run=false
```

`would-remove` replaces `removed` under `--dry-run`. The age is whole
days, hours under a day, minutes under an hour; a job's age counts from
its end, an orphan's from the newest thing in it, a day folder's from
its date. Under `--verbose` every examined item that stays has a `kept`
line with one reason: `young` (within the age), `live` (a running job's
summary or snapshot, a transcript without its end record, an orphan whose
snapshot is live), `not-owned`, `not-a-day-folder`, `symlink`,
`unreadable`; and every place looked at has a `walk` line.

The summary: `examined` records read, `removed` items removed (or that
would be), `skipped` entries passed by (a name that is not a day folder,
a live record, a tree not walked), `not_owned` items another user owns,
`failed` removals that returned an error, `bytes` the size removed.

**`--format jsonl`** writes the same report as one JSON document per line
for a site's log pipeline: the event word under
`event` (`removed`, `would-remove`, `failed`, `kept`, `skipped`, `walk`,
and `summary` for the last line), the fields under the keys the text form
prints, the age as `age_days` (a whole number), an error as its text, the
counts and `dry_run` as numbers and a boolean. Nothing else changes: one
line per item, the summary last, standard error for a usage error alone.

```text
{"event":"would-remove","kind":"activity","path":"/opt/karvi/shared/jobs/260817/260817-153859-00","age_days":40,"bytes":14151}
{"event":"kept","kind":"activity","path":"/opt/karvi/shared/jobs/260926/260926-153859-00","reason":"young"}
{"event":"summary","examined":31,"removed":15,"skipped":4,"not_owned":0,"failed":1,"bytes":137313,"dry_run":true}
```

## The exits

| Exit | Meaning |
|---|---|
| 0 | everything eligible went (or would, under `--dry-run`) |
| 1 | a removal failed or a tree could not be walked; the report says which |
| 2 | a usage error: an unknown flag, a positional argument, `--days` below one, `--minfree` outside 0 to 100, `--format` neither `text` nor `jsonl` |

## The flags

```text
karvi-prune [--basedir auto|PATH] [--sharedroot auto|none|PATH] [--scoreboards PATH]
            [--days N] [--minfree PERCENT] [--dry-run] [--verbose] [--format text|jsonl]
```

| Flag | Default | Meaning |
|---|---|---|
| `--basedir` | `auto` | the private roots; `auto` is every root of the operator's that exists, or the site's provisioned roots for a root run; a path is that root alone |
| `--sharedroot` | `auto` | the site's shared root; `auto` consults the two system roots, `none` consults nothing, a path consults that root |
| `--scoreboards` | `/dev/shm/karvi/scoreboards` | the shared scoreboards, walked beside each private root's `state/scoreboards` (the `scoreboards` key's shared place) |
| `--days` | `31` | the retention age in days, one or more |
| `--minfree` | `5` | the free-space floor in percent; 0 turns pressure off |
| `--dry-run` | off | report what would go and remove nothing |
| `--verbose` | off | add the `walk` and `kept` lines |
| `--format` | `text` | the report's form: `text`, or `jsonl` for one JSON document per line with the same fields |

Tab completes the flags, their words, and a path once the site has run `sudo
karvi setup tab` ([`docs/OPERATIONS.md` "Tab
completion"](OPERATIONS.md#tab-completion)); the candidates come from the
helper's own flag set, so they are these and no others.

## The schedules

The release ships three forms over the one executable; a site picks one
per host, or none and runs the helper by hand.

**Per operator, systemd.** `/usr/share/karvi/systemd/user/karvi-prune.service`
and `karvi-prune.timer`: install under `~/.config/systemd/user/` and `systemctl
--user enable --now karvi-prune.timer`. Daily, persistent across a missed day, a
randomized delay of up to thirty minutes. The unit carries no file-system
sandbox and removes only what this operator owns ([`docs/OPERATIONS.md`
"Retention"](OPERATIONS.md#retention) says why a user unit has none).

**The site, systemd.** `/usr/share/karvi/systemd/system/karvi-prune.service`
and `karvi-prune.timer`: install under `/etc/systemd/system/` and
`systemctl enable --now karvi-prune.timer`. The same line as root, so the
site's provisioned private roots, the shared trees, and the scoreboards
are pruned whoever owns the item.

**cron.** `/usr/share/karvi/cron/karvi-prune`, one line per identity:

```text
17 3 * * *  root    /usr/share/karvi/cron/karvi-prune 2>&1 | logger -t karvi-prune
23 3 * * *  netops  /usr/share/karvi/cron/karvi-prune 2>&1 | logger -t karvi-prune
```

`KARVI_PRUNE` names another executable; arguments are appended to the
line (`--dry-run`, `--verbose`, `--days 14`).

**By hand.** `karvi-prune --dry-run`, then `karvi-prune`; `sudo
karvi-prune --dry-run`, then `sudo karvi-prune` for the site. Both
helpers install at `/usr/bin` beside `karvi`, so the word is on every
operator's path.

## Related documents

- `packaging/man/karvi-prune.8`, installed as `man karvi-prune`: the
  terminal reference. It restates this guide's rules, the report, and the
  exits (not the schedules), so a change to the helper changes both; its
  SYNOPSIS and OPTIONS are generated from the helper's flag definition
  (`make generate`).
- [`docs/OPERATIONS.md` "Retention"](OPERATIONS.md#retention): the operator's
  short form; ["Tab completion"](OPERATIONS.md#tab-completion) for the helper's
  flags under Tab.
- [`docs/DESIGN.md`](DESIGN.md): the retention rules and the reasons behind
  them.
- [`docs/OPERATIONS.md` "The shared trees"](OPERATIONS.md#the-shared-trees): the
  shared root and the modes the ownership rule rests on.
- [`docs/COLLECTION.md` section 7](COLLECTION.md#7-the-schedule): the collection
  run's schedule, the same timer pattern; the collection directory is not
  pruned.
- [`docs/ARCHITECTURE.md` "Daemon and
  storage"](ARCHITECTURE.md#daemon-and-storage): the helper's place beside the
  daemon.
