# Architecture of the karvi implementation

The executable is a thin process boundary over reusable application services.
Core packages depend on karvi-owned contracts; operating-system and third-party
implementations remain under `internal/`.

```text
cmd/karvi
  -> internal/cli
  -> internal/app            configuration, target assembly, direct modes,
     -> internal/planner     orchestration of the planners and the daemon client
        -> configload / inventoryload / resolver / transportselect
        -> credentialbackend -> executionplan / credentialpackage
     -> internal/daemon (client primitives)
     -> internal/jobexec     the runner of an accepted job
        -> executor -> dispatch -> transportselect -> platform.Driver
           -> systemssh / native registry / telnet
        -> output / audit / scoreboard / metrics / capacity
        -> display / safe debug projection
internal/daemon (server)   -> jobexec / ipc / credentialframe / resolver
```

The sections below describe the implementation: address and credential
resolution happen in the submitting client, and the daemon validates and
runs an immutable plan.

## Planning boundary

Every source-reading and secret-resolving activity is in the submitting
client. The daemon is a validator and executor of an immutable plan and
receives secrets only through a separate job-scoped channel. The `sealed`
protection, the daemon-process canary row, inspection, exercise, and the
ICMP gate rest on this boundary.

```text
client (karvi run)                                   per-UID daemon
  config + lock evaluation
  inventory load, selector expansion                 (no inventory interface)
  target validation, dedup, ordering
  client DNS: selected address + alternates
  ExecutionPlan (non-secret)       --prepare_job-->  validate identity, schemas, limits,
                                                     routing, policy, transports;
                                                     DNS only for address_authority=daemon
                                   <--evidence----   preparation digest + DNS evidence
  final credential policy selection
  CredentialGrant / TargetCredentialBinding
  plan digest                      --commit_job--->  accept only if digests match
  CredentialPackage                ==local-peer==>   SO_PEERCRED-checked, one-use,
                                                     memory-only, job-scoped frames
                                                     dispatch -> executor -> driver
```

Boundary rules: ordinary job requests contain no plaintext credentials; the
daemon never inherits device secrets from its startup environment; named,
durable, queue, or network handoffs require authenticated envelope encryption.
`run --dry-run` stops at client planning plus an observational daemon probe;
`run --exercise` stops immediately before target network contact. The reasoning
is [`docs/DESIGN.md`](DESIGN.md) (the plan, the daemon, and the credential
channel).

**Where a platform is resolved.** A device's platform has two values with one
boundary between them. The *set* platform is what the inventory row, the
source's `defaults.platform`, or `--platform` gave, normalised (trimmed,
lowercase) by the loader, `inventory.Direct`, and the `--platform` override
(`app.withPlatform`: the first device in `login` and `command`, every device of
the assembled set in `run`, `app.overridePlatform`); a device without one stays
blank, and the loader never reads the resolution keys. What a device is *matched
on* is `planner.SetPlatformFunc` over the set platform: blank for a blank field
and, under `platform-resolution.on-unknown = "warn"`, for an unknown name; the
target-set assembly's `--select-platform` selector and the credential planner's
device view (which the credential-policy and session-init maps read) are its two
readers, so a not-set or fallen-back device is matched by no `--select-platform`
selector and no map `platform` rule. The two readers stand on either side of the
override: the selector matches while the set is assembled, on what the inventory
gave, and `--platform` acts on the assembled set, so the maps, the plan, and
every record see the platform the device runs as (`--select-platform generic
--platform cisco_iosxe` runs the inventory's generic rows as IOS XE). The
*platform used* is produced once, over the whole set, by
`planner.ResolvePlatforms`, first in the plan draft before the daemon gate, the
transport preflight, and the address plan: a known set platform as named; an
unknown one refused with `platform_unknown` under `fail`, every unknown value in
one message, or run as `platform-resolution.unknown-fallback` under `warn`; a
blank one, and a direct target without `--platform`, as
`platform-resolution.default` (`cisco_iosxe` as shipped), `generic` when the key
is empty. Each resolution carries at most one notice (`platform_not_set` for an
inventory row, none for a direct target; `platform_unknown_fallback`), which the
draft puts on the plan target's `notices` (present only when nonempty) and
prints as a grouped warning line through the draft's `Warn` hook on every client
path. The plan's `device.platform` and effective port carry the platform used to
the daemon, the executor, the SSH algorithms map, and every record; the executor
writes the target's notices on the device's first record whatever its kind or
status, and the dry-run and exercise reports show them as warning findings,
stage `platform`. The credential resolver's enable rule reads the platform used
through the device view's `PlatformUsed`, not the set platform. `login` resolves
at the same point of its own sequence, before the definition is fetched, and the
`login --record` wrapper for its metadata. The executor, `login`, and the
exercise refuse a platform their configuration does not know (`platform.Lookup`,
`platform_unknown`), the backstop behind the planner's resolution for a plan
whose own configuration does not know its platform. Two checks run before the
inventory is read: `--platform` in `login`, `command`, and `run` must name a
known platform (`app.CheckPlatformOption`; the shortcuts `--pi`, `--pn`, `--pr`,
`--pj`, `--pa`, and `--pg` are that option with its value, made so by the parser
table's `standsFor`, which also makes `--dp`, `--dw`, and `--ds` `--dispatch`
with a value; `--tl LIST` is each of its names as a `--target` at the list's
position, `parser.addTarget`), and each `--select-platform` selector must reach
one (`checkPlatformSelectors`), the known set being `platform.KnownNames` over
the configured tables; the three `platform-resolution` keys and a source's
`defaults.platform` are validated against the same set at load, whatever
`on-unknown` says.

Stream mode (`karvi stream`, `karvi -`; `internal/cli/stream.go`) is a
front to `run`: the lines of standard input build a `run` argument list,
each option line checked by `Parse` as it arrives and dropped with its
number when refused, and `--go` hands the list to `run`'s handler, so the
job, its records, and its display are a run's and no rule lives twice.

## Configuration and address-family override

`configschema` generates `schema/config-schema.json` and
`configs/reference.toml`. Each source file is parsed independently, includes
are deterministic, every write crosses the config-lock gate, macros expand
after merge, and semantic validation completes before network access.

A key the registry once held and has since dropped is refused at load from
every layer: `configload/removed.go` is the one table of removed keys
(path, environment name, release, code, hint), consulted by the file and
`--set` path through `removedKeyByPath` and by the environment path through
`removedKeyByEnvironment`, before the registry index would call the name
unknown. The removed content itself, with diff blocks and the commit that
still holds it, is logged in the archived `REMOVED-LOG.md`; nothing else in the
tree names a removed key.

A registry row's range is data on the row: `Min` and `Max` literals in
the row's syntax (a leading `>` or `<` for an exclusive bound) and
`ZeroDisables` where 0 means off. `configload.validateRanges`
(`ranges.go`) is the one rule that applies them, refusing as
`config_value_out_of_range`; the rules that relate two keys stay
hand-written after it. The artifact and the reference carry the bounds,
so a document that states a range copies the row.

Configuration schema 6 retains `name.address-family-preference` as the one
resolver mechanism. CLI family flags are represented as high-precedence
configuration writes, so locks and provenance apply identically:

```text
--ipv4 / --4 -> name.address-family-preference=ipv4
--ipv6 / --6 -> name.address-family-preference=ipv6
```

The resolver applies inventory literal-address precedence, then preferred DNS
family, fallback family, and numeric-lowest selection.

## Daemon upgrade boundary

`ipc.Call` returns a structured schema-mismatch error. `daemon.Probe` may use
the stable, credential-free lifecycle envelope of every released schema from
`MinLifecycleSchema` to the current one to read status, and it judges
compatibility by the pair: the daemon's version and its IPC schema both equal
this executable's, either difference incompatible.
`daemon.StopCompatible` is reachable only through explicit
operator lifecycle commands. Submit paths always use the current schema and
never translate or omit request fields. `ensureDaemon` recognizes a live
incompatible daemon and returns `daemon_incompatible` before opening the
daemon log or starting a child process.


## Execution

The client drafts the plan (`internal/planner.Draft`): one execution target
per device of the assembled set with the selection kind and effective port,
the address plan of every target (a literal wins, then the inventory
management address, then the alternates; DNS only without a literal), and
every dispatch and output value as the effective one. Credentials bind after
the authoritative address is known and become grants in a package. The
runner (`internal/jobexec.Run`) executes the final plan: the dispatcher sends
one target to the executor, which resolves only the transport implementation
from the executing process's configuration, takes the address from the plan
and the credential from the grant provider, acquires the capacity lease,
opens one driver, executes commands in order, persists one terminal record
per command, then closes the driver. There is no automatic retry and no
retry against an alternate address. Halt/gate paths create explicit terminal
records for untouched work. `command` and `run --no-daemon` take the same
path with the local process as the execution endpoint.

Direct system `command` mode uses one fresh `ssh -tt` process with
`ControlMaster=no`, `ControlPath=none`, and `ControlPersist=no` for a shell
device; all commands use that one authenticated interactive shell. An exec
device (`linux`) has one OpenSSH ControlMaster for its session, the client's
(`command`) or the daemon's (`run`) child, and an `ssh -S` client per command
([`docs/TRANSPORT-DRIVER-ARCHITECTURE.md`](TRANSPORT-DRIVER-ARCHITECTURE.md)).
Login remains operator-attached system OpenSSH, optionally behind the transcript
PTY; a recorded session's transcript is rendered at its end as the terminal
showed it (`internal/termtext`), at the widths `script(1)`'s timing log records.

## Display pipeline

`internal/display` owns templates, human timestamps, themes, colors, visible
width, and border expansion. Machine records bypass this path.

The text renderer defers each border until another record arrives. This makes a
border a true separator and permits `last-border=false` without buffering
command output:

```text
record 1 -> remember separator
record 2 arrives -> emit separator -> emit record 2 -> remember separator
end -> discard pending separator, or emit it when last-border=true
```

Command and run share this mechanism. `--border` selects a dynamic dash line;
`--noborder` disables both static and dynamic forms. Device output and echoed
prompt/command text are never cropped.

## Transport composition

`internal/transportselect` resolves CLI/inventory override, mode setting, mode
default, named slot, and concrete adapter. The executor sees only a
`platform.Driver`. `internal/transport/native` is keyed by stable implementation
IDs; build-tagged providers register adapters. `internal/buildinfo` separately
reports the exact transport composition of the executable.

Only `internal/adapters/scrapligov1` imports ScrapliGo. The source admission
gate currently allows canonical `cisco_iosxe`; compilation does not constitute
platform qualification.

### One shape of the source

Every build carries the scrapligo-v1 adapter: the provider file in
`internal/transport/native` registers it at init, `internal/buildinfo`
reports it, and `command` and `run` resolve their `native` default to it.
Earlier a build tag, `scrapligo_v1`, selected between this shape and a
dependency-free one (`build/go.preview.mod`) whose executable reported only
`system`; releases through v0.11.0 shipped that preview, v0.12.0 shipped
the native executable with the tag still in place, and the tag and the
module were then removed.

What the second shape had been kept for was the adapter boundary: if the
rest of the source compiled with no dependency present, nothing outside the
adapter had come to depend on ScrapliGo. That proof now rests on
`scripts/verify-release.sh` alone, which fails on any import of
`github.com/scrapli/scrapligo` outside `internal/adapters/scrapligov1`. The
registry keyed by implementation ID stays, as the place a later adapter
registers, and with it the fail-closed path: a native slot naming an
implementation without a registered provider is
`native_transport_unavailable` and never falls back to OpenSSH
(`TestUnregisteredNativeImplementationFailsClosed`).

## Daemon and storage

The daemon is per effective UID, uses two owner-only Unix sockets in the
`socket` subtree, and checks peer credentials on both. Protocol schema 9 carries
`prepare_job`, `commit_job`, `follow_job`, and `cancel_job` on `d.sock`
beside the lifecycle operations of schemas 3 and 4, and the credential package
crosses `c.sock` as one length-prefixed frame per connection under a
one-use token; `submit_job` is gone. The daemon holds at most 128 preparations
for ten minutes each, retains 1,024 commit receipts for idempotent replay, logs
one `slog` line per request outcome to `logs/daemon.log` with the token
redacted, and runs every job under its own configuration and operator, but for
what the plan carries from the invocation (the job's files, the ICMP gate, the
timeouts and the command byte limit, each command's own among them, and the
halt rule). It stops
itself after `daemon.shutdown-idle-timer` (default one hour;
`Server.IdleTimeout`) with no active job, no live preparation, and no request
but `ping` and `status`: the check `stopIfIdle` runs on the minute ticker that
sweeps preparations, takes `beginStop(if_idle)` under the admission lock as
`daemon stop` does, and cancels the serve, so the drain and the accounting are
the ordinary ones and the exit is 0. The idle clock (`lastActive`) is restarted
by every request that is not `ping` or `status` and by a job's end. On the
client's side `RunViaDaemon` takes an `ensure` step and calls it just before
`prepare_job`, after planning, and once more with the same plan if that request
finds the socket gone or the daemon draining (`daemonGone`), so the timer has
the least room to end a daemon between the probe and the request. Every error
returned to a client carries a registered code (`internal/errorcodes`).
`commit_job` answers at acceptance and the job runs on its own goroutine; the
client follows it through `follow_job`. The `socket` and `state` subtrees are
mode 0700; a state root and `logs` are 0750; job and transcript folders take
`output.directory-mode` (default 0750) and their files are 0640. `cancel_job`
cancels one accepted job through its own context; crash reconstruction is a
roadmap capability: a job whose daemon died is `job_orphaned` to `job follow`,
as is a finished job whose invocation had `output.files.summary-json` false.

**A command's output is bounded in memory from the device's first byte to the
record's write.** The device session (`internal/devsession/settle.go`) cleans
the response as it arrives into settled bytes, the recorded bytes: carriage
returns dropped per chunk, the echoed first line dropped at its newline, the
returned prompt and the trailing blanks given up at the end. What settles goes
to memory up to `output.spool-threshold-bytes` (1 MiB by default) and from the
byte that would cross it to the command's spool, one file
`<activity>.<device>.<index>.<pid>.spool` under `spooldir` (never the scratch
directory: `tempdir`'s chain prefers a tmpfs by design), the settled head
written first so the file holds the response whole. The session holds per
command the settled bytes up to the threshold, the unsettled tail of at most 4
KiB where a prompt or a declaration is matched, the failure-pattern and UTF-8
carries, the running SHA-256, and its read chunks; the limit
`output.max-command-bytes` counts settled bytes, and every ending, a prompt's
return or a cut, hands the executor what settled and where it is. The record's
output has one source (`output.Source`): the record's string, or the spool file
with its count, digest, and encoding. The store streams every file from it (the
`commands.jsonl` and `errors.jsonl` lines escaped 32 KiB at a time, the text
block, the collection block), verifying the spool's bytes against the reader's
digest on the measuring pass before any byte of the line reaches a file
(`output_spool_mismatch`); the follower's queue carries the line's offset and
length and is fed from the file; the in-process renderer streams every format
from the source; the daemon formats nothing; and the executor removes the spool
once the record is durable and handed on, on every return of the command's path.
Abandoned spools (a process that died mid-command) are swept at the daemon's
start and at every admission by the name's pid and owner. At admission the
volumes behind the job's folder, a `crun`'s collection directory, and `spooldir`
(and, for a recorded login, the transcripts root) are read once each and judged
by `freecheck` (`auto`, the default: the sum of the places' finished sizes plus
the 2 GiB floor, the spool's width narrowed to what fits with
`spool_width_narrowed` on the receipt; `always`: the floor alone; `never`), one
`statfs` per volume (`internal/output/preflight.go`). The scoreboard's target
row carries the running byte count and the snapshot is rewritten every
`watch.refresh` while an activity runs, so the watch screen shows a response
growing. Measured (`scripts/output-scale-run.sh`): the daemon's peak at 128
responses of 5 MiB in flight is 287 MiB on the native transport and 130 MiB on
the system transport (1.39 and 1.43 GiB before the spool), one 65.7 MB response
28 and 23 MiB (284 and 302 MiB before), and the responses are on disk under
`spooldir` while they arrive, at most the bytes in flight. So the daemon's
memory for output is the width in flight times the threshold plus the windows, a
figure of two keys; there is no memory key and no memory check. The width in
flight is `dispatch.server-max-inflight`, by default `min(256, max(32, 8×CPU))`.

**Which files a job writes is one value, `output.FileSet`,** one field per file,
the zero value writing them all. The invocation decides it on every path: the
plan's `output` carries `persist` (`output.persist-command`), `files` (the eight
`output.files.*` switches), and `root` (`output.root`, resolved by the planner
against the site's shared root, `sharedroot`, and the client's basedir and
home), and `output.SkippedFiles` is the one function from the job's
configuration to the set, read by the planner and by `jobexec.Run`, the one
place it is decided: `--nof` and all eight keys false are the same value,
`output.AllFiles`, for which the store makes no folder, and a `crun` skips every
`output.NAME.txt`, on `cmd`, `run --no-daemon`, and a run through the daemon
alike. The daemon reads no `output.*` key of its own for a job. The command line
reaches these as flag-origin configuration values, in one function,
`cli.outputOptions`: `--nof` and `--of` are `output.persist-command`, and
`--of=PATH` is `output.root` for that invocation, on `cmd` and `run`; `run`
refuses `--nof` with `--detach` (nothing persisted and nobody following) and
with `--exercise` (the report is a file). A job that keeps no folder has an
empty `artifact_dir` in its receipt and its follow start, and its footer says
`artifacts=none`. The store writes every file from memory and none from another,
so no key depends on another; `Store.Paths` names only the files written, and
the summary's `paths` is made from it (`jobexec.summaryFiles`, shared by the job
and the exercise). A record is handed to the daemon's followers by the store
itself, under its lock, once the `commands.jsonl` line is written and before the
text block (`output.Options.OnDurable`, the record with its notice): the
followers refuse a frame that does not follow the last, and only the lock gives
that order at width. `output.max-job-bytes` is held by the store over the
`commands.jsonl` lines and the text blocks together; a record that cannot be
appended is kept as `Store.Err`, which `jobexec.Run` reads after the devices are
done and ends as an output failure, since a device's dispatch result carries its
code to no operator.

**Every `.json` file is indented and every `.jsonl` file is compact.**
`osutil.AtomicJSON` is the one
writer of whole `.json` files (temporary file, `fsync`, rename, directory
`fsync`) and indents with `osutil.JSONIndent`; the capacity ledger, which
writes under its own lock, uses the same constant. The layout is for the
operator who opens the file; no reader may depend on it, which is why the
scripts parse (`scripts/lib/json.sh`) and nothing digests a `.json`
file's bytes.

**Permissions are set only on what karvi creates.** karvi gives a mode to
the files, directories, and
sockets it makes and never changes the mode of anything that already
exists; where an existing file's mode is unacceptable it refuses with a
registered code and changes nothing. A survey of every `Chmod` on that
date found two exceptions, both since removed: the trust store under
`accept-new` with an explicit path (`internal/hostkey/policy.go`,
`ensureTrustFile`, which now creates with `O_EXCL` and sets the mode
through the new file's handle, and otherwise only validates), and the
password helper's directory (`internal/askpass/askpass.go`), which is the
scratch directory and so, with `tempdir` set, the operator's own. The
pattern for a directory is `osutil.MakeDirectories`: `Mkdir`, then `Chmod`
on what that `Mkdir` made, existing components untouched. A new `Chmod`
on a path, as opposed to a handle karvi has just created, is a review
question.

Retention is outside the daemon: `karvi-prune` (`cmd/karvi-prune`,
`internal/prune`) is one executable behind the per-operator timer, the site's
root timer, the cron script, and the hand run, and it reads no configuration. It
walks the `jobs` and `transcripts` trees under the operator's basedir and under
the site's shared root (the `sharedroot` resolution of
`osutil.ResolveSharedTree`, the same the writers use), never the collection
directory, and the scoreboard directory; a root run walks the private roots the
site provisioned under the system roots (`osutil.SiteUserRoots`) in place of its
own. It shares with the readers the one list of final statuses
(`records.FinalStatuses`) and the one day-folder matcher (`osutil.IsDayFolder`),
so what the watch screen calls finished and what the tree calls a day are what
the prune removes: finished jobs, ended transcripts, and terminal scoreboard
files older than the retention age (or the oldest of them under free-space
pressure, judged per filesystem), orphans and stale snapshots by age alone, and
then the day folders left empty. Ownership decides what a run may remove, and a
failure is a line, never the end of the run. [`docs/PRUNE.md`](PRUNE.md) is the
guide; [`docs/DESIGN.md`](DESIGN.md) the record.

## Credential delivery

The package crosses to the daemon through a `credentialpackage.Protector`:
the client's `Protect` derives the envelope and the canonical bytes from one
package, and the daemon's `Unprotect` requires the `local-peer` protection,
a valid envelope naming the daemon's audience, and a body whose decoded
projection matches the envelope. `ProtectorFor(sealed)` answers
`protection_unsupported`; the `Sealer` contract fixes what a reviewed
provider would bind (`AssociatedData`). The frame handler then runs the
provided stage of the package validator against the preparation's header
and the draft's target scopes: every package rule except the two that need
the final plan, `plan_digest` and `binding_mismatch`, which commit checks
with the full `Validate`. A faulty package is refused at the frame with its
rule and stored nowhere; the token is spent, so a retry needs a new
preparation.

The launcher hands `daemon serve` an allow-listed environment through
`osutil.ChildEnvironment`, the same filter the system transport applies to
ssh, plus karvi's own `KARVI__*` configuration variables,
`KARVI_ASKPASS_PATH`, `TZ`, and `TMPDIR`; the operator's shell secrets never
reach the daemon process, and canary row r2 scans that process. A daemon
started outside the launcher logs a notice per inherited built-in credential
variable, name only. Two jobs of one operator hold two packages: each commit
hands `jobexec.Run` its own package as the grant provider, nothing in the
daemon, the job executor, the executor, or the transports holds a secret at
package level, and the concurrent-job test proves each session authenticates
with its own job's password.

Credential files: a `cloginrc` backend declares `scope` (`user` | `shared`)
and `required`; the scope selects the owner, mode, and group checks, a
relative include resolves beside the including file and inherits the scope,
and an unavailable optional user file is a silent not-found while a
required or shared one fails the run before a job exists.

The tabular reader (`tabular.CSVReader`) is shared by inventory and the
credential CSV backend. The caller chooses trimming
(`Spec.Trim`): inventory trims every cell; a credential file does not, and
trims its selector, username, and key cells itself, so a secret keeps its
leading and trailing spaces. A byte-order mark at the start of the stream
is stripped before the CSV parser, in header and numeric mode alike. The
short-row policy is also the caller's: inventory warns and skips, the
credential CSV refuses (`ShortRowPolicy = "error"`), since in a first-match
file a skipped row changes which row wins.

The credential CSV's selectors are evaluated by `internal/matching`
(`row.go`), beside the maps' `EvaluateRule` and on the same compiled
selectors and prefixes, so the grammar lives in one package. `CompileRow`
checks a row once, at load; `Row.Match` evaluates it against the device
view (`RowFields`: the maps' five fields and the inventory's `credkeyref`);
`FirstRow` is file order and nothing else. The maps' selection (`Select`:
the longest prefix wins over an earlier rule) is not used for rows.

The file rules are one package, `internal/credentialbackend/credfile`,
shared by every file backend: `Rules` (the backend's
name, scope, `required`, the operator's home and uid, and the three
`security.*` settings, built by the resolver's `fileRules`), `Rules.Open`
(open without following a symlink and without blocking, `fstat` the
descriptor, run the scope's check set, return the descriptor and the
canonical path), `Rules.Classify` (the availability classes, with the
backend's own parse-failure code for an uncoded error), and the generic
`Cache[T]` (one read per resolver, a failure cached, a cancelled context
not). A backend keeps its parser: `cloginrc` calls `Open` for its root
file and for every include, so an included file is checked as its own
file under the inherited scope. The checks run on the descriptor so that
the file inspected is the file read.

A `csv` backend's configuration is admitted at two
layers. `configschema.backendField` is the key allow-list for every
`[credential-backend.NAME]` table; the csv reader keys join it through
`configschema.CredentialCSVKey`, and `configschema.CredentialCSVFields` is
the one list of the nine logical fields that the allow-list, validation,
and the backend share, so a `mappings.` key for anything else is
`config_unknown_key`. The keys are fields of a dynamic table, not
builtin-layer keys, so the registry version does not move.
`configload.validateDynamic` ties keys to types: `scope` and `required` to
the file backends, the reader keys to `csv`
(`config_credential_backend_key_unsupported`), an explicit `scope` and a
`path` for `csv`, and `validateCredentialCSV` types the reader keys for the
declared mode. Nothing at validation touches the file.

The credential CSV backend is `internal/credentialbackend/credcsv` (the
operator's guide is [`docs/CREDENTIAL-CSV.md`](CREDENTIAL-CSV.md), whose
[section 11](CREDENTIAL-CSV.md#11-where-it-lives-in-the-code) maps each rule to
its package), and it owns little: `credfile.Rules.Open` and `Classify` for the
file, `credfile.Cache[*table]` for the one read, `tabular.CSVReader` with `Trim:
false` and `ShortRowPolicy: "error"` for the parse, `matching.CompileRow` and
`FirstRow` for the selectors. `New` builds the backend from the validated table.
The load checks every row (the selectors, a result of some kind, a unique folded
`credkey`) and moves the secret cells into `secrets.Value` before anything else
can see them; a reader failure is reported under `credential_csv_malformed` with
the reader's reason and without its `tabular_*` code, since those codes are
inventory's (category and exit). `Resolve` builds the device view the policy map
is matched against, takes the first matching row, and answers with a copy of its
secrets, so destroying the material after sealing leaves the cache whole.
Completeness is the resolver's `finalize`, as for every backend. No message
quotes a cell, which is why `matching.RowError` reasons are value-free and the
reader's duplicate-header reason is restated. Environment indirection for a new
backend is `internal/credentialbackend/envindirect`; the env, Redis, and Vault
backends still hold their private copies (a roadmap line).

The inventory's `credkeyref` column is a
logical field of `internal/inventoryload` like any other
(`mappings.credkeyref` in `configschema.inventoryField`, a field of the
dynamic table, so no registry number moves). `rowDevice` checks a filled
cell with `matching.CheckKey`, the one legality rule the credential CSV's
`credkey` column obeys, and fails the load with
`inventory_credkeyref_invalid`; `sameDevice` compares pins folded
(`matching.FoldKey`), so duplicate rows that pin different keys are an
`inventory_conflict`. The device carries it as `inventory.Device.CredKeyRef`
(`credkeyref`, omitted when blank). It is deliberately absent from two
places: the target's `source_digest`, which is the source file's own,
and the plan's `DeviceProjection` (`excludedDeviceFields` in
`executionplan/projection_test.go`), because credentials are resolved on
the client and the daemon has no use for the pin; the key a pinned device
took is the grant's `matched_on.credkey`. (An inventory digest over every
loaded device was recorded and never read until plan schema 4 removed it;
the archived `REMOVED-LOG.md`.) The planner's device view
(`CredentialPlanner.deviceView`) starts from the inventory device, so the
pin reaches the resolver without passing through the plan.

The pin itself is two small things. `credentials.Keyed`
(`HonoursCredKey() bool`) is an optional interface beside
`credentials.Backend`: the capability of answering for a key, which a
backend declares and a type does not imply. `credcsv.Backend` declares it
and passes `Device.CredKeyRef` into the row view, where `matching.Row.Match`
already implements the pin. `Resolver.Resolve` walks a pinned device's
sequence asking only keyed entries (`Resolver.keyed`; a formula is never
keyed, whatever its source, because its username is the template's and not
the row's), continues on `NotFound`, returns a keyed backend's failure as it
would for any device, judges the key's row with `finalize`, and at the end
of the sequence returns `credkeyref_unresolved` before the built-in
environment fallback and the prompt are reached. An unpinned device's walk
is unchanged. A later keyed store joins by implementing the one method and
honouring its contract: for a pinned request, the key's credential or
`NotFound`, never a general one.

The inventory secret-column refusal is one rule in one place,
`inventory.SecretColumnWord` (the six
words, matched at the end of a normalised name), asked from two. The shared
reader gained `tabular.Spec.CheckHeader`, a hook that sees every header of
a header-mode file, mapped or not, before the columns are mapped, and whose
error is returned unchanged; the reader itself knows nothing of secrets and
the credential CSV, whose headers are *meant* to say `password`, does not
set it. `inventoryload` sets it (`secretColumn`,
`inventory_secret_column`). `configload`'s inventory-source validation asks
the same function of every `mappings.attributes.NAME`
(`config_inventory_source_secret_mapping`), in both modes, since an
attribute is copied into the plan and the records. Neither message prints
the header's text or a cell.

Match evidence is `credentials.Match`, a comparable struct the planner compares
whole when it merges grants; it gained `CredKey` (`credkey`, omitted when
blank), which travels in the plan report, the grant, and the package's wire form
(`schema/credential-package-envelope.schema.json`, an optional property). The
command record's `matched_on` is a map the executor writes by hand: its five
keys are always present, and `credkey` is added only when set.

## Inspection, exercise, and follow

Every daemon-backed `run` begins with one client half, `draftClient`:
configuration, the target set, the draft plan with client-authority DNS,
and the credential planner run on the draft, which binds exactly the targets
whose address the client selected. `run --dry-run` stops there: it probes
the daemon's control plane for 500 ms without launching one, builds the
inspection `PlanReport` (client targets `planned` and bound,
daemon-authority targets `deferred_to_exercise_or_live_prepare`), prints it
to stdout in the run's format, and writes nothing under the jobs tree. A
failure before the draft exists aborts as a live run would, the stage named
after the code.

A live or exercise run continues: prepare, the frame, and `commit_job`. The
commit answers with the receipt the moment the job is accepted (`jobexec.Run`'s
`OnAccepted`, after the manifest is durable and the started audit record
written), and the job runs on its own goroutine. The daemon keeps a bounded job
table (`internal/daemon/jobs.go`): the receipt, the path of the canonical
`commands.jsonl`, the durable edge, the outcome when done, and the live
subscribers. It holds no record history. **Since daemon IPC schema 8 the records
themselves travel in the follow stream**: `follow_job` validates the cursor, a
sequence, sends `FollowStart` (the job's folder, the edge, and the first
sequence the stream carries), subscribes the follower, catches it up from the
daemon's own file to the edge at subscription by reading each line and sending
it as a `record` frame, feeds it the live records `OnDurable` hands over (the
record with the store's notice, after the durability barrier), and ends with
`stream_terminal` carrying the outcome and the summary. A record frame is
written in pieces through the one record-line writer (`output.WriteRecordJSON`
inside `ipc.WriteRecordFrame`), never marshalled whole, and its `record` is the
`commands.jsonl` line without its LF, so a follower's `jsonl` output equals the
file. The frame is bounded by `daemon.max-ipc-frame-bytes`
(`ipc.RecordFrameSize`, exact): a record whose frame would pass the bound is
sent with its output left out and the notice `follow_output_omitted` in its
place, `output_bytes` and `output_sha256` intact (`omitOutput`, on the live path
and the catch-up path alike); the text renderer prints the notice's message
where the output would stand, `jsonl` carries the record as sent, and
`commands.jsonl` holds the whole line. The subscriber's queue holds the records,
bounded by 1,024 of them and by the 30 second write timeout; a follower dropped
for either resumes from its last delivered sequence, the daemon reading the
records after it from the file; a finished job is served the same way. A job
that writes no `commands.jsonl` (`job.open` keeps no path) has no catch-up: the
start's `first_sequence` is the edge plus one, the follower that started with
the job had every record live, and a later or resumed follower prints one line
naming the records that were before it and are not kept (`app.NotKeptLine`). The
client (`internal/app/follow.go`) opens no file: it checks each frame's
continuity and renders its record through `jobexec.RunRenderer`. `--detach`
returns at acceptance with the receipt; `--follow=false` follows for the
terminal only. `karvi job follow JOB-ID` runs the same follow loop from the zero
cursor for a job named by its ID, so the daemon catches it up from the file and
feeds it live; a job the daemon no longer holds is read from its directory,
located by the unique ID under the output root (`internal/app/jobdir.go`, with
the checked open of the file), and `job_orphaned` names a directory without a
summary. `karvi job cancel JOB-ID` sends `cancel_job`: each job runs under its
own child of the daemon's job context, the request cancels that child with a
cause carrying the reason, time, and requester, every unfinished unit is
recorded `cancelled` (a command already sent is never recorded succeeded or
errored), the devices are counted `cancelled`, the summary carries a
`cancellation` block, the audit trail a `run.cancel_requested` record, and the
follow stream's terminal delivers the outcome to any following client. The block
records an accepted request, not its effect: the exercise branch reads its
context once, for the block, and never observes it, so an exercise never ends
`cancelled`, and a live job whose work had finished keeps its result. Clients
therefore print the cancelled line from `records.Summary.CancelledBy`, which
answers the block only under the final status `cancelled`.

In exercise mode `jobexec.Run` branches after acceptance
(`internal/jobexec/exercise.go`) and builds no executor: per target it
checks the binding (through `ForTarget` and the safe projection only), the
transport selection with the executor's own call plus the located askpass
helper for the system kind, and the host-key policy and enrollment; per job
the dispatch arithmetic (`dispatch.Plan.Check`) and a read-only capacity
assessment (`capacity.Manager.Assess`). It writes `exercise.json`, leaves
`commands.jsonl` empty, finalizes the summary and scoreboard as `exercised`
with the first error finding's exit when a target is `not_ready`, and
writes the `run.exercised` audit record with `device_contacted=false`. The
client reads the report under the canonical-file discipline and renders
it. An interrupt before the frame aborts `cancelled` with its stage named;
the frame and the commit run without cancellation; an interrupt during the
follow leaves the job running and exits `cancelled`; a closed stdout ends
the follow with exit 111 (SIGPIPE is ignored on the daemon-backed run).

## The ICMP gate

`internal/icmpgate` is the two-probe reachability gate: one interface,
`Pinger`, and two implementations. The ping-socket
pinger opens a Linux datagram ICMP socket per family (the kind the kernel
grants to the groups in `net.ipv4.ping_group_range`), sends sequence 1,
waits up to `network.ping-timeout`, then sequence 2, and matches replies on
sequence and a nonce-plus-digest payload while the kernel matches the
identifier. The system adapter runs the `ping` executable with the interval
and the wait set to the timeout and parses its reply and error lines. `Detect`
chooses the method once per job among those `network.ping-socket` and
`network.ping-system` allow: socket, then system, then unavailable with a
reason naming every cause. `karvi version` reports the host's method.

The gate's keys are the job's configuration (`--ping` and `--noping` write
`network.ping-targets` through the cli layer), the invocation's on every path,
the method switches among them. `jobexec.Run` detects before any directory or
record exists and refuses an enabled job with no method as
`icmp_capability_unavailable`. In `executor.Execute` the gate runs after the
credential grant's safe projection and before the capacity lease: two replies
proceed, one proceeds with the `icmp_packet_loss` notice on the device's first
record, none emits the failure set with `icmp_unreachable` on command 1 and the
rest not attempted, before the open request and its password callbacks exist.
Every record of a gated device carries the same `ping` object and `ping_ns`; the
summary carries the block; the `ping` stage feeds the metrics histogram. `login`
runs the same gate after credential resolution and before the open. The two
rehearsals detect the capability without probing and report it as
`intended_ping.capability` and `method`. Text output prints one line per gated
device from the `display.ping.header` template, with details and the notice
under `--debug`.
