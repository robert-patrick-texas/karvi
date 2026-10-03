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

Two figures bound how many devices a job speaks to at once, and they are
not the same thing:

- **The job's workers**, set by its dispatch mode: one for `serial` (and
  for every `command`), a fixed pool for `parallel`, and for `wave` a
  width that starts at one figure and moves between waves within a
  ceiling. A worker takes one device at a time and holds it to its end.
- **The host's cap**, `dispatch.server-max-inflight`: the device sessions
  in flight at once on the host, across every job, every operator's
  daemon, and in-process runs. It is enforced per session, not by
  trimming a job's workers: each worker takes a lease in the capacity
  ledger before it connects, and while the ledger is full it waits there,
  polling every 50 to 250 ms (`dispatch.admission-poll-min`, `-max`), its
  device counted in the record's `server_capacity_wait_ns`. A device also
  takes a lease of its own, against the inventory row's `session_cap` or
  else its platform's `session-cap` (3 by default), so jobs together never
  hold more sessions on one device than that.

So the sessions a job holds at once are at most the smallest of its
workers, its device count, and what the cap leaves after every other job's
leases. The keys:

| Key | Default | Range | Meaning |
|---|---|---|---|
| `dispatch.default` | `serial` | `serial`, `parallel`, `wave` | The mode a `run` takes without `--dispatch` (`--dp`, `--dw`, `--ds`); `serial` is one worker whatever the host has; `command` is always serial. |
| `dispatch.parallel-workers` | `0` | 0 to 4096 | The parallel pool (`--workers N`); `0` is the logical CPU count. |
| `dispatch.wave-start-width` | `0` | `0` auto, else 1 to the ceiling | The wave's first width and its floor (`--start-width N`); `0` is `min(64, max(16, 4 × CPU))`. |
| `dispatch.wave-max-width` | `0` | `0` auto, else start to `absolute-max-width` | The wave's ceiling (`--max-width N`); `0` is `min(256, max(32, 8 × CPU))`, the cap's own default, so one wave job may fill the host. |
| `dispatch.server-max-inflight` | `0` | 0 to 4096 | The host's cap on device sessions in flight across every job, every operator's daemon, and in-process runs, held as leases in the capacity ledger under `sessions.shared-capacity-root`, across operators where the site made the scratch root (`sudo karvi setup shared`) and per operator where it did not; `0` is `min(256, max(32, 8 × CPU))`. |
| `dispatch.absolute-max-width` | `512` | 1 to 4096 | The hard ceiling on any job's workers: a parallel pool above it is cut to it, a wave ceiling above it is refused at load. |

The defaults by host:

| Logical CPUs | `parallel` workers | `wave` start → ceiling | `server-max-inflight` |
|---:|---:|---:|---:|
| 4 | 4 | 16 → 32 | 32 |
| 8 | 8 | 32 → 64 | 64 |
| 16 | 16 | 64 → 128 | 128 |
| 32 | 32 | 64 → 256 | 256 |
| 64 | 64 | 64 → 256 | 256 |
| 128 | 128 | 64 → 256 | 256 |
| 512 | 512 | 64 → 256 | 256 |

### `parallel`: a fixed pool

The job starts its workers once and feeds them one queue, the devices in
the dispatch order (`dispatch.order`). A worker that finishes a device
takes the next at once, so there are no batches and no pauses: a slow
device holds its own worker and nobody else, and the job ends when the
last device does. The pool's size never changes during the job, whatever
the host's load; only the cap's leases can hold a worker back. A halt
(`--halt-on-error-count`, `--halt-on-error-percent`) stops the queue: no
further device starts, and the devices in flight finish.

### `wave`: cohorts, and a width that follows the host's CPU

The job runs its devices in waves. A wave takes the next *depth* devices
in the dispatch order, depth being the wave's width times
`dispatch.wave-depth-multiplier` (4), or the devices left if fewer, and
runs them with *width* workers as the parallel pool does. The wave ends
when its last device ends, so a slow device holds the next wave back;
then, before the next wave:

1. **The error gate.** When the wave's failures reach
   `dispatch.wave-gate-error-count` or `dispatch.wave-gate-error-percent`
   (both off by default), the job stops, the devices not started
   recorded `not_started_wave_gate`. A halt stops a wave job as it stops
   a parallel one, at once.
2. **The timed delay**, `dispatch.wave-gate-timed-delay` (`--wave-delay`,
   none by default), a pause between waves for a change window.
3. **The width for the next wave**, from the host's CPU: the busy share of
   every CPU on the host from `/proc/stat` (all processes, not karvi's
   alone), sampled every `metrics.process-sample-interval` (1 s),
   averaged over its first `dispatch.wave-cpu-warmup-samples` (5) and
   then smoothed with a half-life of `dispatch.wave-cpu-half-life` (15 s).
   Around `dispatch.wave-cpu-threshold-percent` (75) a band of
   `dispatch.wave-cpu-target-zone-percent` (10) either side, 65 to 85 by
   default:

   | The CPU signal | The next width | Then |
   |---|---|---|
   | below the band (under 65 %) | up by `wave-step-up-percent` (50 %) of the width, rounded up, at most the ceiling | |
   | in the band (65 % to 85 %) | unchanged | |
   | above the band (over 85 %) | down by `wave-step-down-percent` (10 %), rounded up, at least the start width | the next `wave-cooldown-waves` (2) decisions keep the width |

The start width is also the floor: a wave job never runs narrower than it
began. Each decision is in `metrics.json` (`wave_decisions`: the wave, the
signal, the width before and after, and the reason: `cpu_below_zone`,
`cpu_below_zone_at_ceiling`, `cpu_in_zone`, `cpu_above_zone`,
`cpu_above_zone_at_floor`, or `cooldown`), a dry run's `dispatch:` line
names the wave's start, ceiling, and depth multiplier, and every record
names its wave,
width, depth, and worker (`dispatch.wave_number`, `wave_width`,
`wave_depth`, `worker_id`). The right-sizing reads the host's CPU only:
not the cap's ledger, not other jobs' widths, not the devices' or the
network's latency; a host whose CPU stays low runs a wave job at its
ceiling, and the cap's leases then decide how many of those workers
connect.

Against `parallel` at the same width, a wave job pays the waits at its
wave boundaries (each wave waits for its slowest device); it buys the
ramp from a cautious start, the error gate between waves, and the timed
delay.

### 100 devices, worked

On the reference host of these figures (4 logical CPUs; the fake device
answering `show clock`, a session of 0.61 s on average), 100 devices with
every setting at its default but the mode:

| | `--dispatch parallel` | `--dispatch wave` |
|---|---|---|
| Workers | 4, for the whole job | 16 in wave 1, 24 in wave 2 |
| The queue | one: 100 devices, a free worker takes the next | wave 1: devices 1 to 64 (16 × 4); wave 2: devices 65 to 100 (36 left, under 24 × 4 = 96) |
| Waves | none | 2; CPU 12.9 % after wave 1 (under 65 %), so 16 + 8 = 24 |
| Sessions in flight, most | 4 | 16, then 24 |
| The host's cap | 32, not reached | 32, not reached |
| Device-times in turn | 25 (100 ÷ 4) | 4 in wave 1 (64 ÷ 16), 2 in wave 2 (36 ÷ 24) |
| Executed | 17.2 s | 4.7 s (wave 1 +0.0 to +3.1 s, wave 2 +3.2 to +4.7 s) |

The same 100 devices on larger hosts, by the defaults' arithmetic:

| Logical CPUs | `parallel`: workers, device-times | `wave`: waves (width × devices) |
|---:|---|---|
| 8 | 8, 13 (12 full turns and 4) | 1 (32 × 100: depth 128 holds every device), 4 device-times |
| 16 | 16, 7 (6 full turns and 4) | 1 (64 × 100), 2 device-times |
| 32 and up | the CPU count, 4 or fewer | 1 (64 × 100), 2 device-times |

On 8 logical CPUs and up a 100-device wave job is one wave, so it never
changes width: the ramp needs more devices than the first wave's depth.
A larger job on the reference host, 1,000 devices under `--dispatch
wave`, executed: widths 16, 24, then 32 (the ceiling) for seven waves, the
CPU between 13 % and 20 % throughout, 9 waves in 33 s:

| Wave | Width | Depth (devices) | Window | CPU after | Next width |
|---:|---:|---:|---|---:|---:|
| 1 | 16 | 64 | +0.0 to +3.7 s | 13.3 % | 24 |
| 2 | 24 | 96 | +3.7 to +7.5 s | 14.7 % | 32 |
| 3 to 8 | 32 | 128 each | +7.6 to +30.3 s | 15.9 % to 19.5 % | 32 (`cpu_below_zone_at_ceiling`) |
| 9 | 32 | 72 | +30.4 to +32.7 s | 19.7 % | — |

Had the CPU risen above 85 % after a wave at 32, the next would have run
at 28 (32 less 10 %, rounded up to 4), and the two decisions after it kept
28; it would never have gone under 16.

**The cap in action.** The same 100 devices under `--dispatch parallel
--workers 64` on the reference host, whose cap is 32: 64 workers, never
more than 32 sessions in flight; 68 devices waited for a lease, the
longest 1.9 s; 3.1 s in all. A second job beside a wave job at its
ceiling meets the same: the default ceiling is the default cap, so the
two share the host's 32 and each waits on the other's leases.

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
check per volume); `docs/PRUNE.md`. `karvi-run(1)` (`man karvi run`),
DISPATCH, restates "The width" for the terminal (the modes, the widths at
0, the cap, the halts, and the gates); a change to one changes both.
