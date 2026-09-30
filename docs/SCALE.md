# Scaling karvi up

This guide is for a site that runs karvi against a large network from a
host with many cores: what grows, which settings bound it, and what the
free-space and memory arithmetic asks of the host. The settings are the
configuration reference's (`configs/reference.toml`); the
figures below are from the shipped defaults, executed on the tree's build.

## What grows

Three things grow with a site, and karvi bounds each with its own keys:

- **The width**, how many device sessions are in flight at once on the
  host. It costs SSH sessions, AAA transactions per second on the site's
  authentication servers, and the daemon's memory for output in flight.
- **The job's size**, how many devices a job names and how much each
  answers. It costs disk under the job's folder (and the collection
  directory for a `crun`) when the job has finished.
- **One response's size**, the bytes one command returns. It costs
  memory up to a threshold and disk under the spool directory above it,
  while the command is in flight.

karvi makes no attempt to size itself to the network: the site sets the
width it can carry, and karvi's checks refuse or narrow a job the host
cannot hold, before any device is contacted.

## The width

A job's width is the smallest of three figures: the mode's width, the
device count, and the host's cap.

| Key | Default | Range | Meaning |
|---|---|---|---|
| `dispatch.default` | `serial` | `serial`, `parallel`, `wave` | The mode a job runs in; `serial` is width 1 whatever the host has. |
| `dispatch.parallel-workers` | `0` | 0 to 4096 | The parallel width; `0` is the logical CPU count. |
| `dispatch.wave-start-width`, `dispatch.wave-max-width` | `0` | `0` auto, else 1 to the ceiling | The wave mode's start and ceiling. |
| `dispatch.server-max-inflight` | `0` | 0 to 4096 | The host's cap on device sessions in flight across every job, every operator's daemon, and in-process runs, held as leases in the capacity ledger under `sessions.shared-capacity-root`; `0` is `min(256, max(32, 8 × CPU))`. |
| `dispatch.absolute-max-width` | `512` | 1 to 4096 | The hard ceiling the dispatcher applies to any job's width; a wave ceiling above it is refused at load. |

The cap's default by host, and the width a job gets under `parallel`
with `parallel-workers = 0`:

| Logical CPUs | `server-max-inflight` at `0` | A 10-device job | A 1,000-device job |
|---:|---:|---:|---:|
| 4 | 32 | 4 | 4 |
| 16 | 128 | 10 | 16 |
| 64 | 256 | 10 | 64 |
| 128 | 256 | 10 | 128 |
| 512 | 256 | 10 | 256 |

A site whose devices and AAA carry more than 256 sessions at once sets
`dispatch.server-max-inflight` to the figure it wants (512, 1024, up to
4096), together with the mode's width and, above 512,
`dispatch.absolute-max-width`. A job opens one session per device
(`command` and `run` share one device session), so the width is also the
number of devices spoken to at once. AAA throughput is the site's to
judge; karvi's cap is about the host.

## The free-space check

At admission, before any device is contacted, karvi reads the free space
of every volume an activity writes, once per volume, and judges it by
`freecheck` (the operations guide, "The spool directory"). The places: the
job's folder, a `crun`'s collection
directory, the spool directory, and a recorded login's transcripts root.
The paths are grouped by the device behind each (`stat`), and one
`statfs` is read per group, about a microsecond each, so a host whose
places share one volume reads it once.

| Key | Default | Meaning |
|---|---|---|
| `freecheck` | `auto` | `auto`: each volume must hold the sum of its places' finished sizes plus the floor once; `always`: the floor alone; `never`: no read. |
| `output.min-free-bytes-after-job` | 2 GiB | The floor, asked once of every volume. |
| `output.expected-bytes-per-device` | 256 KiB | The estimate of one device's output, per file the job's folder writes (`commands.jsonl` and the text file, two by default; one for the collection directory). |
| `output.reserve-multiplier` | 1.25 | The margin on the estimate. |
| `output.max-command-bytes` | 64 MiB | One command's limit, and the spool's per-command term. |
| `output.spool-threshold-bytes` | 1 MiB | Above it a response continues into the spool; below it stays in memory. |
| `spooldir` | `auto` | `/tmp/karvi-<uid>`, then `/var/tmp/karvi-<uid>`; a disk, never a tmpfs. |

Under `auto` the ask of one volume that holds the job's folder and the
spool is the floor, plus 640 KiB per device for the folder (256 KiB × two
files × 1.25), plus 64 MiB per command in flight for the spool. The
width is the one above; the device count enters twice, in the folder's
term and as a bound on the width:

| Host and mode | 1 device | 10 devices | 1,000 devices |
|---|---:|---:|---:|
| Any host, `serial` (the shipped default) | 2.06 GiB | 2.07 GiB | 2.67 GiB |
| 4 CPUs, `parallel` | 2.06 GiB | 2.26 GiB | 2.86 GiB |
| 16 CPUs, `parallel` | 2.06 GiB | 2.63 GiB | 3.61 GiB |
| 64 CPUs, `parallel` | 2.06 GiB | 2.63 GiB | 6.61 GiB |
| 128 CPUs, `parallel` | 2.06 GiB | 2.63 GiB | 10.61 GiB |
| 512 CPUs, `parallel`, cap 256 | 2.06 GiB | 2.63 GiB | 18.61 GiB |

The spool's term is the worst case, every command in flight answering
with the full limit at once; the measured 128-wide run of 5 MiB responses
spooled 632 MiB at its peak. When the spool's term alone is short but at
least one command fits above the folder's term and the floor, the job
runs narrower with the warning `spool_width_narrowed` (on standard error
in process, on the receipt and the follow start under the daemon); when
the folder's term or the floor is short, or not one command fits, the job
is refused with `output_preflight_space` naming the volume's paths and
both figures. A `crun` adds one copy of the device estimate for its
collection directory, on that directory's volume. A recorded login asks
its transcripts root's volume for the floor alone: a session's length is
nobody's estimate. A `--nof` command has no folder and asks the spool's
volume alone.

A wide host with a small volume has three plain knobs: `always` for the
flat floor, a lower `output.max-command-bytes`, or `auto`'s narrowing,
which runs the job narrower rather than refusing it. The audit file, the
scoreboard directory, and the state root are not checked: their first
write lands before any device is contacted, they write kilobytes after,
and the scoreboard directory is a tmpfs by default where a 2 GiB floor
would refuse a small host.

**Monitoring is the site's.** The check is a guard against writing into
a full disk at the moment a job starts, not an alert. Watch the volumes
behind `basedir`, `sharedroot`, `spooldir`, and `watch.directory` with
the site's monitoring, and let `karvi-prune --minfree` (`docs/PRUNE.md`)
age the job and transcript trees under a free-space floor of its own.

## The memory budget

The daemon's memory for output is the width in flight times
`output.spool-threshold-bytes`, plus a few windows of at most 4 KiB per
command (`docs/ARCHITECTURE.md`): there is no memory key. At
the default threshold, 128 commands in flight hold at most 128 MiB of
response in memory; the measured peak of the whole daemon at 128 responses
of 5 MiB in flight was 287 MiB on the native transport and 130 MiB on the
system transport, against 1.39 and 1.43 GiB before the spool. A site that
wants less memory lowers the threshold (0 spools every response from its
first byte); one that wants less disk raises it, up to 1 GiB. The spool
directory must be a disk: `tempdir`'s chain prefers `/dev/shm` by design
for small scratch files, and a spool on a tmpfs would cost the memory the
threshold saved.

## Storage layout

- Put `spooldir` on the volume with the room, not under `basedir` if the
  private root is small; `auto` chooses `/tmp` then `/var/tmp`.
- Put `sharedroot` (the site's `jobs`, `crun`, and `transcripts` trees)
  on a volume sized for the retention age times the daily output;
  `karvi-prune` walks it.
- Keep `watch.directory` on its tmpfs (`/dev/shm/karvi/scoreboards`): the
  scoreboard files are kilobytes and the watch screen reads them every
  `watch.refresh`.
- A job's records stream to disk as they arrive; nothing waits for the
  job's end to be written, so a job that is cut short leaves what it had.

## Related

`docs/OPERATIONS.md` ("The job's output files", "The spool directory",
"The shared trees", "Retention"); `docs/ARCHITECTURE.md` (the output
path); `docs/DESIGN.md` (the spool and the memory budget, the free-space
check per volume); `docs/PRUNE.md`.
