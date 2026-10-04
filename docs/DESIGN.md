# karvi design: why it does what it does

This document states the settled design decisions of karvi in present tense:
what the program does, why that was chosen, and what was not taken. It is the
distillation of the decision records and worked design sessions that built the
program; those records are kept whole in the operator's private archive and are
not needed to read this. [`docs/ARCHITECTURE.md`](ARCHITECTURE.md) describes the
structure, [`docs/OPERATIONS.md`](OPERATIONS.md) and the guides beside it
describe how to operate, and the code and its tests are the reference for what
karvi does.

Each entry gives the rule, the reason, and the alternatives rejected. Where a
decision was amended, the entry states the rule as it stands.

## 1. Principles

**Truthful accounting.** Every device and every command ends in exactly one
recorded state, and the counts partition the total. A command whose outcome is
unknown (sent, then the job was cancelled or the daemon stopped) is recorded
`cancelled` or `incomplete_shutdown`, never `succeeded` or `errored`, and is
excluded from the error count. *Why:* an operator reading a summary must be
able to trust that "failed" means the device said no and "completed" means
every command was answered. *Not taken:* letting the driver's error stand for
a command the cancel interrupted; counting an unfinished device as failed.

**No silent policy bypass.** A configuration value the reader cannot honour
is refused at load, never ignored. A removed key is refused from every layer
with a code naming the key, the release that removed it, and the replacement.
A malformed selector pattern is refused, never an empty selection. A
credential file that fails its permission check fails the run even when the
backend is optional. *Why:* dropping a site's intent silently is worse than
stopping; the fault is found where it was made. *Not taken:* warnings for
removed keys; documented no-op keys; an empty match for a bad pattern.

**One mechanism per concern.** One host-key policy for every mode and
transport; one selector grammar for the command line and every map; one
session engine over two byte streams; one record writer that every consumer
streams from; one list of final statuses; one function that names a device's
file. *Why:* two mechanisms for one concern drift, and each drift found in
this program's history was of that kind (two matchers with departing
grammars, two session engines with different escalation, three copies of the
status list with the sixth status in two). *Not taken:* conformance suites
holding two implementations equal.

**Secret-safe by construction.** The types that hold secrets refuse every
sink (`fmt`, JSON, gob, `slog`, templates), and the contracts that must not
carry a secret are proven unable to by a reflection walk and an import list,
not by review. The daemon never loads inventory, expands selectors, or
resolves credentials, and a transitive import test makes that a property of
the tree. *Why:* a property stated in a test cannot drift from the code.
*Not taken:* review-based assurance; a deny-list of environment variables.

**Deterministic selection, bounded concurrency.** Every non-default order is
reproducible from the record; every queue, table, and frame has a fixed bound
read before allocation; a shuffle is a hash of a recorded key. *Why:* a run
must be repeatable from what it wrote, and no input may size memory.

**Breaking changes accepted.** The program is pre-1.0 and its operator has
chosen speed over compatibility: renamed options and removed keys are not
aliased, every release restarts the daemon, and a digest is kept only where a
consumer requires it. *Why:* aliases reintroduce the ambiguities a rename
removed and a compatibility lane is a maintenance line nobody asked for.

## 2. The command line

**Unique-prefix abbreviation.** Every karvi word, a command word, a
subcommand, or an option name, may be shortened to any prefix that identifies
exactly one word valid at that position; a full name or alias always matches
even when it is a prefix of a longer name (`--t` is `--target`). An ambiguous
prefix is a usage error naming every candidate. Abbreviation never touches
device text. *Why:* the operator required it, and no off-the-shelf flag
library provides it, so the parser is karvi's own. *Not taken:* Cobra and
pflag.

**The freeform boundary.** In `command`, options end at the first freeform
word; in `run`, at the first positional word. After that karvi recognises no
option, no abbreviation, no `--`, and no help flag: every remaining argument
reaches the device unchanged, `-4` and `-h` included. Freeform forms exactly
one device command; several commands take repeated `--cmd` or a `--cf` file,
and mixing freeform with either is refused. *Why:* the earlier parser
captured karvi options inside device text, so `traceroute -4 host` lost its
`-4` and a trailing `-h` printed help. *Not taken:* recognising options after
positional words.

**Spelling.** One or two leading dashes are equivalent, `=` attaches a value
in either form, and aliases match exactly (`--target`/`--host`/`-t`,
`--cmd`/`--command`/`-c`, `--address`/`-a`, `-4`/`-6`). The option table
lists every specified option, built or not, so an abbreviation unique today
stays unique when a feature ships; an option the executable does not
implement is refused with `cli_option_unavailable`. Enumerated values are
declared in the table, so an unlisted value is a parser diagnostic. *Why:*
abbreviation stability across releases; one place declares what a value may
be.

**One error code per parser rule.** Each rule the parser applies has its own
code, all exiting as usage errors before any connection; ambiguity messages name
every candidate. *Why:* the unique-error-code rule of the whole program (see
[§15](#15-configuration-and-errors)). *Not taken:* a single `cli_usage` code.

**Shortcuts stand for an option with its value.** `--pi`, `--pn`, `--pr`,
`--pj`, `--pa`, `--pg` stand for `--platform` with a built-in platform name;
`--dp`, `--dw`, `--ds` stand for `--dispatch parallel|wave|serial`. A
shortcut is a whole word of the table that carries the value, so no handler
knows the shortcuts exist and the last spelling on a line wins. `--tl LIST`
adds each comma- or whitespace-separated name as a `--target` at the list's
position, so the plan and manifest record target inputs and never a list.
*Why:* the operator's most typed values; nothing after the parser changes.
*Not taken:* aliases (an alias still takes a value); a list kind in the plan.

**Declarations attach backwards.** `--expect`, `--blind`, and `--blind-return`
attach to the nearest preceding `--cmd`, or to the one freeform command; a
declaration before the first command, or with `--cf` alone, is a usage error.
*Why:* position is meaningful in one direction only, so a reader never
guesses which command a leading declaration meant.

**`--record[=PATH]` and `--of[=PATH]` take their path with `=` only.** A bare
`--record` or `--of` is the switch; a word after it is the device. *Why:*
executed examples showed `cmd --of xe-1 show clock` misread as a folder
`xe-1` and a device `show`, and a heuristic ("a path if it has a slash")
would make one line mean two things. *Not taken:* the space form; a new
parser kind.

**An option that takes its value with `=` alone refuses a detached value.**
`--of[=PATH]`, `--record[=PATH]`, `--cd=PATH`, and `--fs=SUFFIX` are written
alone (`--cd` and `--fs` never) or as `--NAME=VALUE`. On a stream line, which
holds one option, any text after a space is refused, the line dropped with its
number; on the command line, a bare `--of`, `--record`, `--cd`, or `--fs`
followed by a word of a path's form (`/`, `~`, `./`, `../` at its start, or `.`
or `..` whole) is refused before any device is contacted or transcript claimed.
Both are `cli_option_value_detached`, the message naming `--NAME=WORD`; a bare
`--cd` or `--fs` stays `cli_option_value_missing`. Any other word keeps its
position's meaning (`command --of r1 'show clock'`), so a relative path without
a prefix still reaches the device as text, and the documents say to write
`--of=out`. *Why:* executed, `run --of /x --cmd 'show clock'` sent `/x --cmd
show clock` to the device, `command --of /x r1` and `login --record /y r1` took
the path for the device (the latter naming a transcript after it), and a
stream's `--of PATH` line did the first at every job. The path form decides a
refusal, never a meaning, so the entry above stands: a line still means one
thing. *Not taken:* refusing every word after a bare switch (`command --of r1
…` would break); the spaced value on a stream line.

**The `job` word.** Every operation on one accepted job lives under `job`:
`job follow JOB-ID` and `job cancel JOB-ID`, each taking one ID and never
launching a daemon. *Why:* a verb on one job belongs to a `job` word, and
the `daemon` word keeps the daemon's lifecycle. *Not taken:* `run --follow
JOB-ID` (`--follow` is a boolean); `watch JOB-ID` (the watch screen shows
counts, never records); `daemon cancel`.

**`crun` is a command word sharing `run`'s grammar.** It takes `run`'s option
slice, `--cd=PATH` and `--fs=SUFFIX` among them, may name no command, and
dispatches to `run`'s handler with the collection set. The record's activity
type stays `run`. *Why:* the site's most frequent invocation deserves its own
verb rather than a 44th option, and one shared slice means a new `run` option
reaches `crun` by construction. *Not taken:* `run --collect`; a new activity
type.

**Stream mode.** `karvi stream` (alias `karvi -`) reads standard input line
by line: a `--` line is one run option, any other line is one command sent as
written, blank and `!`/`#` lines are skipped. The draft has two parts: the
targets and options, which stay from one job to the next, and the commands,
which are the job's; an option line's word is resolved through run's own
table, so `--cmd`, `--command`, `--cf`, and the `--expect`/`--blind`/
`--blind-return` declarations belong to the commands, an abbreviation or an
`=` spelling means what it means on a command line, and an `=` value runs to
the end of the line. `--go` (or `--sendit`) executes the draft as `run` would
and clears the commands, with a notice and no job when there is nothing to
send; `--clear` empties the commands alone; `--reset` empties the draft;
`--end`, `--quit`, EOF, or Ctrl-C leave, a Ctrl-C outranking lines already
read. A bad line is reported with its number and dropped, the draft
standing; `--cf`, `--tf`, and `--tfr` may not name `-` in any spelling, since
standard input is the stream; a read failure or a line over 1 MiB ends the
stream with `stream_input_read_failed`. The exit is the last job's, 0 when
none ran. `--cd` and `--fs` are option lines of the part that stays: `--go`
and `--clear` keep them, `--reset` removes them, a later line replaces the
value (the last spelling wins), and `--cd=.` is the stream's working directory;
one file per device per directory means a later job to a device replaces the
earlier job's file, which `--fs` keeps apart (`--fs=.ver`, then `--fs=.run`).
An `=`-only option line takes its value with `=` alone, text after a space
refused when the line is read; the value checks of `--cd` and `--fs` run when
the line is read too, so a bad line is dropped with its number and the draft
stands; at `--go`, a draft with `--fs` and no `--cd` takes the stream's working
directory.
Typed at a terminal, a line is edited with the usual keys, the
Delete key among them, and the up arrow recalls earlier lines (the line
editor `internal/termline` over `golang.org/x/term`, vendored, which the
credential prompts share): the terminal is in raw mode for one line's read
alone, through the tree's one raw-mode helper, and back in its own mode for
every message and every job, so a job's display, its Ctrl-C, and its
credential prompt are unchanged; the reader reads a line when the loop asks
for it and never beside a job, which would hold the raw mode through it;
the editing echoes on the controlling terminal, and a line typed ahead
during a job, which the terminal's own mode ends with `\n`, is translated
to the Enter the editor takes. *Why:* a job
composed line by line at a terminal or piped from a script, reusing the table
parser and the run path so no rule lives twice; a typo must not cost the
draft; a command typed the way `run` takes it must be sent once, not by every
later job. *Not taken:* directives as table options; shell-style splitting of
an option line (`--tl r1 r2` gives the case); a prompt; raw mode held across
a job (the job's Ctrl-C would become a byte to read), which a reader
reading ahead had done until the credential prompts showed it; GNU readline (not
reachable without cgo; `rlwrap` remains an operator's choice); a generic
error for the read failure (every path carries its own code).

**Help is laid out at print time.** The help texts are constants kept in step
with the parser table by a drift test; a pass at print time adds blank lines
and colour (headings bold, option words in the `accent` role, the action word
of every `Usage:` line in the `success` role, the title's tag in `label`),
following the display's colour rule and off under a pipe or `--ansi strip`.
*Why:* readable on a terminal while the constants, the drift test, and pipes
carry no escapes. *Not taken:* escapes in the constants; a `--color` option
for help alone.

**Tab completion answers from the parser table.** A hidden word,
`karvi __complete CURSOR WORD...`, walks the table to the word under the
cursor and prints candidates: command and option words, enum values, known
platforms, registry keys for `--set`, device names from the inventory, job
IDs from the scoreboard directory, and `:files` where bash should complete
paths. It creates nothing, contacts no daemon, and prints nothing on any
failure. One bash function serves `karvi` and `karvi-prune`; `sudo karvi
setup tab` installs it. *Why:* the grammar is data the drift test already
keeps honest, so a word added to the table is completed by construction.
*Not taken:* a generated static script (stale at the next change, blind to
inventory); listing aliases (the list would double).

## 3. Targets, inventory, and platforms

**Target names are lowercase identities.** Every name is lowercased on input,
and the lowercase name is the identity for duplicate removal, `--exclude`,
ordering, and the shuffle rank. *Why:* every comparison already ignored case;
one normalisation makes the identity explicit.

**One target set, assembled in command-line order, in every mode.** `login`,
`command`, and `run` assemble one list from a positional device, repeatable
`--target` (globs allowed), `--tl`, `--tf FILE`, `--tf DIR`, `--tfr PATH`, and
the inventory selectors `--site`, `--device-group`, `--select-platform`,
`--all`, each contributing at its position; the first occurrence of a name
keeps its place, later duplicates are removed, then `--exclude` matches, then
`dispatch.order` orders the result. `login` and `command` connect to the first
device of the set, and refuse a literal `--address` when the set holds more
than one. *Why:* inventory matches had been placed before direct targets
regardless of the line, a defect; one rule covers every source and mode, and
an operator can pick a device from a file without a script.

**Negation on the command line removes.** A `--target`, `--site`,
`--device-group`, or `--select-platform` value beginning with an unescaped `!`
removes matching devices from the set the other inputs selected; alone it
selects nothing. `--exclude` matches names only and refuses a double negative.
In a target file a line beginning with `!` is a comment. *Why:* the operator's
direction: negation behaves as `--exclude` does.

**Target folders and recursive folders.** `--tf DIR` reads the regular files
directly in a folder in byte order, skipping dot files, `readme*`, `disabled*`,
and editor backup names; `--tfr PATH` descends depth-first to
`targets.recursion-max-depth` (default 3), a folder beyond the limit an error
never silently skipped. `targets.empty-source` (`warn` or `error`, default
`error`) governs a source that yields nothing; a missing or unreadable source
is always an error, and an empty final set always is. *Why:* hierarchical
host-list folders without scripts, with hidden and backup files never read by
accident, and no source silently ignored.

**Commands files are literal.** `--cf` reads one file and sends its contents
as written: blank and comment lines are sent, only the line terminator is
removed, and a zero-byte file is an error. *Why:* karvi does not alter device
text.

**Dispatch order is reproducible from the record.** `dispatch.order` is
`default` (as encountered), `sorted`, `shuffle` (each device ranked by
SHA-256 of `dispatch.shuffle-key`, a NUL, and the lowercase name, a permanent
contract), or `random` (`shuffle` with epoch seconds as the key). The order and
the key are recorded in the manifest, the command record, and the login
metadata, so `shuffle` with the recorded key reproduces a `random` run.
*Why:* the previous manifest recorded neither order nor seed. *Not taken:* a
date in the shuffle.

**Alternates are recorded, never retried.** karvi probes only the selected
address; inventory alternates exist for audit, exercise, and a future explicit
failover. *Why:* implicit failover would repeat a non-idempotent command on a
device the operator did not choose.

**The known platform set has one definition.** A known platform is one of the
seven built-ins (`generic`, `cisco_iosxe`, `cisco_iosxr`, `cisco_nxos`,
`juniper_junos`, `arista_eos`, `linux`) or a configured `[platform.NAME]`
table; names compare after trimming and case folding and are recorded
lowercase. A table for a built-in overrides that built-in's allowed fields; a
table for a new name is an alias whose `driver` must name a built-in and which
inherits that built-in's whole compiled definition (prompt levels, prompt and
failure patterns, paging, exit commands, ports, session cap), its own fields
overriding. A table name is a literal (no glob character, no leading `!`), and
prompt and error patterns are compiled data, never configuration. *Why:*
before this rule a table's `driver` was only a label: a new name ran as
`generic`, so a rejected command was recorded as succeeded. Reviewed data, not
ad hoc configuration, decides parser behaviour. *Not taken:* lowercasing table
names in the loader (it would change the configuration digest for a naming
rule the platform package owns).

**A device's platform is resolved once, at planning, in a fixed order.**
`--platform NAME` on the command line, else the inventory row's value, else
the source's `defaults.platform`, else `platform-resolution.default`, whose
shipped value is `cisco_iosxe`; an empty value gives `generic`. A platform from
the first three steps is *set*; one from the default is *not set*, and the
distinction is what selectors and maps match on. *Why:* production fleets are
Cisco-dominated, and an unlisted target that ran as `generic` sent no enable,
no paging, and recorded a rejected command as succeeded, or the operator wrote
`--platform cisco_iosxe` on every line; karvi infers nothing from a name or an
address. *Not taken:* inferring a vendor; a prompt; keeping `generic`.

**An unknown row platform refuses the activity.** Under the default
`platform-resolution.on-unknown = "fail"`, an inventory row whose platform is
not known, for a device in the target set, refuses the run at planning with one
error naming every unknown value, its source and line, and the known names;
`warn` lets the device proceed as `platform-resolution.unknown-fallback` with
a notice on its first record. A configuration value that names an unknown
platform always fails validation. *Why:* silently driving an unknown name as
`generic` recorded rejected commands as succeeded; failing over the whole set
shows every typo at once; a configuration typo is the operator's own and is
never softened.

**`--platform` overrides; `--select-platform` selects.** `--platform NAME` is
the platform the invocation's devices run as, in all three modes and for every
device of `run`'s set, direct targets included; `--select-platform GLOB` is the
inventory selector, a target input of all three modes, matching set platforms
only, and its value must reach a known platform. The selector matches first on
what the inventory gave; the override acts afterwards on the assembled set.
*Why:* an operator who knew `command --platform` read `run --platform` as an
override and got a selector that silently selected nothing beside a direct
target. One word means one thing in every mode. *Not taken:* `--as-platform`
beside the old selector; applying the override before the selector matched.

**Notices explain a platform the row did not name.** `platform_not_set` and
`platform_unknown_fallback` are printed once per distinct value per source at
planning and carried in the plan target to the device's first record, whatever
that record's status; a dry run shows them as findings. *Why:* the operator
must see, before and after the fact, why a device ran as a platform its row
did not name, and a connection failure must not lose that explanation.

**An unknown name never reaches a session.** The executor, `login`, and the
exercise look a platform up with a function that says whether the name is
known and refuse an unknown one before any connection. *Why:* a daemon whose
configuration lacks the client's alias table used to run the alias as
`generic` under the alias's name.

**An inventory file holds no secret column.** A header that is or ends with
`password`, `passwd`, `secret`, `token`, `private_key`, or `passphrase` is
refused, naming the column number and the word and never the header text; an
attribute mapping of such a name is refused at `config validate`. *Why:* an
inventory has none of a credential file's protections and is the file most
likely to be mailed or committed; a headerless file read in header mode has a
device row as its "header", so quoting it could print a password. *Not taken:*
a warning; an override key; scanning cells.

**The inventory `credkeyref` pin.** A device row may carry `credkeyref`, a
literal key compared case-folded; it is outside the inventory digest (which
covers identity and connection facts alone) and outside the plan, and the key a
pinned device took appears in the grant's match evidence. Rows for one device
coalesce when their pins agree and conflict when they differ. *Why:* the policy
map selects a device's policy, and a second way to choose it would need its
own precedence rule; coalescing conflicting pins would let file order pick the
credential.

## 4. Credentials

**Secrets live in one type that refuses every sink.** `credentials.SecretString`
and `SecretBytes` are value types holding one pointer to the internal secret
value; `String`, `GoString`, `Format` on every verb, and `LogValue` print
`<redacted>`, and JSON, text, gob, and binary marshalling return
`secret_serialization_refused`. Bytes are read only inside a scoped callback
that wipes its copy; `Destroy` wipes the shared value so every copy reads as
unset. Refusal canaries prove every sink for every secret-bearing type. *Why:*
a by-value copy of a pointer-receiver type would print its bytes under `fmt`
and marshal as `{}`; `fmt.Formatter` is the only hook covering every verb; a
public hint method would be callable from any template or log line.

**Credential files obey one set of rules on the opened descriptor.** A file is
opened first, without following a symlink and without blocking, and the checks
run on the descriptor that will be read. Under `user` scope the owner is the
operator and the mode `0600`; under `shared` scope the mode is `0640`, the
owner root or an approved administrator, and the group the site's shared group.
The codes are generic (`credential_file_symlink_rejected`,
`credential_file_mode_unsafe`, `credential_file_shared_mode_invalid`, and
their kin) whatever the format. Scope is declared, never inferred; a `shared`
backend is always required. `config validate` never opens a credential file.
*Why:* inspecting a path and then opening it leaves a gap a file swap falls
between; a shared file read under a defaulted `user` scope would fail with
`chmod 600` as the remedy, the wrong advice for a file a group must read.
*Not taken:* a second code family per format; a default CSV path.

**The credential CSV: first match wins, a row is an AND of its filled cells.**
Six selector columns (`device_name`, `address_cidr`, `platform`, `site`,
`device_group`, `credkey`) match the device; a blank cell is an absent
dimension, a negated cell must not match, a row needs a positive cell, and OR
is more rows. Rows are read top to bottom and the first match wins, for CIDR
prefixes too. The matching row is chosen by its selectors and judged
afterwards: a row whose password is missing where required fails, and karvi
does not look further down. The whole file is read and every row validated at
first use and cached for the resolver's lifetime. *Why:* in a file an operator
reads top to bottom, "the first match wins" is the whole rule, and one
exception for one column is discovered during an outage; letting cell contents
decide the winner would let a blanked password silently hand the device to a
broader row's credential; a file checked only when a device reached a bad row
would pass on Monday and fail on Tuesday. *Not taken:* longest-prefix
selection; "the first complete matching row".

**Secret cells are verbatim; no message quotes a cell.** A secret cell is
never trimmed, goes into a secret value as the row is read, and may be
replaced by an environment variable's value under the backend's indirection
flags. No credential CSV message quotes any cell, a selector included; a
duplicate key names two lines and not the key. *Why:* a password may begin or
end with a space; a misplaced delimiter shifts every cell one column along,
and then the "selector" in a message is the password.

**A pin is a pin.** A device with `credkeyref` matches only rows whose key
equals it, walks its policy's sequence asking only backends that declare the
keyed capability, skips a formula whatever its source, and never falls to a
catch-all row, the environment fallback, or a prompt; the failure
`credkeyref_unresolved` quotes the key and lists the backends asked and
skipped. A key-only row serves pinned devices alone. *Why:* a mistyped
reference that fell through would take a general credential and produce failed
logins and lockouts with nothing naming the cause; a formula asked for a pinned
device would return the key's password under a username the row does not
hold. *Not taken:* a type switch in the resolver (a capability lets a new store
join by one method); a request field every backend must remember.

**Match evidence describes the criterion, not the device.** For the file
backends `matched_on` carries the category, pattern, file, and line, and no
value naming the device, so devices sharing one line share one sealed grant;
grants are merged when secrets, policy, backend, evidence, transport, and port
are all equal. *Why:* at 2,500 devices on one shared line, per-device evidence
meant 2,500 sealed copies of one secret; two lines holding the same secret must
not merge into a grant that can name only one.

**The enable secret is optional unless the platform requires it.** No built-in
requires one; `[platform.NAME] requires-enable = true` requires it for that
platform, read from the platform the device actually runs as. When optional,
resolution uses a secret if one is resolved, never prompts for one, and never
fails for its absence; the session's one attempt then decides. *Why:* many
organisations deliver privilege 15 at login or configure enable without a
secret.

**Prompting once per invocation, and a prompt left unanswered ends the
run.** The client's input provider caches by field: the first target
needing a username, password, or enable secret asks, naming the device or
the count, and every later target reuses the answer, or the failure; the
daemon has no provider and prompts for nothing. The prompt reads through
the line editor stream mode uses. Ctrl-C at it is
`credential_prompt_interrupted` (exit 113), the key given back to the
process as the interrupt it would have been outside raw mode, so the run
stops as at any other moment and a stream ends; an empty answer is the
field's missing code at once, before any later prompt; Ctrl-D on an empty
line stays `credential_prompt_unavailable`. *Why:* under the daemon there
is no terminal; under the client, per-target prompting would ask once per
target; a prompt is asked only for a field the device needs, so no job can
run past one left unanswered, and the operator who presses Ctrl-C there
expects the shell back, where the prompt had held the key until the input
ended. *Not taken:* asking again after an empty answer; a generic code for
the three ways a prompt ends (each is its own cause); Ctrl-D as an
interrupt (it is the input's end, as everywhere else).

**The credential CSV has its own guide.**
[`docs/CREDENTIAL-CSV.md`](CREDENTIAL-CSV.md) is the text an operator works from
with the file open; the shared file rules stay in OPERATIONS with a table saying
where in the guide to look. *Why:* two texts stating the same rules would have
to be kept equal.

## 5. Transports and the device session

**One session engine over two byte streams.** `internal/devsession` implements
the whole device session once (first prompt, privilege, paging, session-init,
command execution, failure detection, output normalisation, close) over a
`Stream` of `Read`, `Write`, `Close`, and `Abort`. The system transport
supplies the stream from an `ssh -tt` process's pipes; scrapligo-v1 supplies it
from karvi's own SSH connection behind scrapligo's transport wrapper. Each
command is written once and read until the prompt; the two transports' records
differ only in `transport`, timings, and identifiers, and a parity suite runs
every case as four combinations (`command` and `run` over both) and compares
the records path by path. *Why:* two engines drift, as the pre-record system
session (no escalation, no paging) against scrapligo's driver (extra returns, a
secret sent nine times) showed; with one engine each transport is only a
connection. *Not taken:* two engines held equal by a conformance suite.

**One connection and one shell per device.** Each device gets one connection
and one interactive shell for its whole command list, on both transports and in
`command` and `run` alike; OpenSSH ControlMaster reuse is off. *Why:* Cisco IOS
XE rejects secondary session channels; the contract the operator agreed is one
connection, no probes, no extra sessions. *Not taken:* a process per command;
mandatory ControlMaster reuse.

**The platform definition is the authority for privilege and paging.** Every
built-in carries its privilege levels, prompt pattern, failure patterns, paging
commands, and exit commands as compiled data; the session validates the
definition before any connection. Privilege is one attempt per level along the
`previous` chain, `enable`, the secret at the escalate prompt, the privileged
prompt within `execution.enable-timeout`, and anything else is
`privilege_failed` with no command sent and never the secret in the message;
there is no de-escalation. Paging follows privilege; the session-init profile
follows paging; then the requested commands. *Why:* scrapligo's escalation loop
sent a wrong secret nine times; the contract fixes one attempt. *Not taken:*
loading patterns from scrapligo's assets at run time.

**A session-init profile is chosen by the client and carried in the plan.**
`[session-init.NAME]` profiles (commands, `on-error` of `fail-device` or
`continue`, an optional command timeout) are selected per target by the
`session-init-map` at the step that binds the target's credential; the plan
carries the selected profiles and each target's choice, the daemon validates and
never evaluates the map. Profile commands run through the same `Execute` as
requested commands and write `session_init` records in the same file with their
own index; under `continue` a profile failure does not fail the device and puts
a notice on the first requested record; under `fail-device`, or after a
session-ending failure, the requested commands are
`not_attempted_session_init_failure`. *Why:* a daemon keeps the configuration it
started with, so an edited profile or a client's `--set` would not be what runs,
and the plan would not show what was sent; the administrator who writes
`continue` declared the profile optional. *Not taken:* the name alone in the
plan with the daemon looking the commands up.

**A session-ending failure ends the device's commands under every setting.** A
timeout, a lost stream, or output over the limit before the prompt makes the
session unusable, and the executor asks the session rather than inferring from
codes; the remaining commands are `not_attempted_prior_command_failure` even
under `--continue-device-on-error`. `generic` detects no command errors.
*Why:* a desynchronised shell cannot serve the next command.

**Timeouts, each wait its own.** Connect (`ssh.connect-timeout`,
`native-ssh.connect-timeout`), handshake (`native-ssh.handshake-timeout`),
first prompt (`execution.prompt-timeout`), enable (`execution.enable-timeout`),
per command (`execution.command-timeout`, a profile's override), and the whole
list (`execution.device-timeout`, default unbounded), the earlier deadline
naming the code. Keepalives on karvi's connection send `keepalive@openssh.com`
with want-reply after `native-ssh.keepalive-interval` of silence, and
`native-ssh.keepalive-count-max` unanswered close the connection with
`session_keepalive_timeout`; the system transport reads OpenSSH's own timeout
into the same code. *Why:* a slow authentication must not be charged to the
prompt timeout on one transport only; a server must refuse an unknown global
request that asks for a reply, so the refusal is proof of life.

**Blind sends and expect-and-send.** A command with trailing `\r` escapes, or
`--blind` / `--blind-return N`, is written with its returns in one write and
awaited for `execution.blind-wait`; if no prompt returns the record succeeds
with the bytes read and the notice `prompt_not_observed_after_blind_send`, and
the session ends. `--expect PATTERN=RESPONSE` (at most twenty per command)
answers a non-standard prompt: after each chunk the unconsumed declarations are
tried in order on the last line since the mark, the first match is answered
with the response and one carriage return and consumed, under the command's
one deadline. A command carries blind returns or expectations, never both.
Responses are device text in clear in the plan and manifest; a password prompt
is never answered with `--expect`. *Why:* a `[confirm]` consumes a return
typed ahead in the same write; a device that reloads drops the connection, and
the operator who declared the send blind accepted that; a pattern tried on the
whole buffer would fire again on its own answer's echo. *Not taken:* `\r`
interpreted anywhere in the text; a heuristic refusing patterns that name a
password; draining stray prompts between commands.

**scrapligo-v1 runs on karvi's own SSH connection.** The adapter supplies
scrapligo with a custom transport: an x/crypto dial with karvi's host-key
callback, a PTY and shell, the password read inside the authentication
callbacks at authentication time. No subprocess, no `ssh-keyscan`, one TCP
connection per open. *Why:* scrapligo's standard transport checks a known-hosts
file strictly or not at all and cannot enroll a first-seen key; the earlier
pre-scan made every open six connections.

**Platform admission is a table per native implementation; every built-in is
admitted.** Each native registration lists what it admits by the definition's
base driver, never a table's label; a platform not admitted is
`native_platform_not_qualified` before any connection. *Why:* the table shape
serves a later connector with its own admission list; with every built-in's
patterns compiled in and one session layer, nothing holds a built-in back.

**The fake IOS XE device is the engineering fixture.** It models what the
session layer sees (prompts, enable, paging, confirm and value prompts, a slow
command, a large one, silence, reload, configuration mode, a syntax error) and
records every line it receives; a command is added when a suite sends it, with
a unit test. The real device matrix is the only proof of interoperability.
*Why:* a fake that imitates state invites tests that pass against the
imitation.

**A login's normal end is qualified on the devices before karvi classifies it
differently.** A device that ends a session on `exit` without an exit status,
as the fake does, makes OpenSSH exit 255, and `karvi login` reports it as
`ssh_process_failed`, exit 110, `ExitConnectionFailure` in its transcript's
metadata and audit; from the client such an end and a device dying mid-session
are the same, even in OpenSSH's own verbose log. The runbook's row D15 records,
on each laboratory device, plain `ssh`'s exit after `exit` and `karvi login`'s.
If the device sends a status, the fake learns to send one and karvi is
unchanged; if it closes without one, an interactive login that authenticated
and ended with the device closing the session is a completed session, exit 0,
with the notice `login_closed_without_status`, "authenticated" read from
OpenSSH's log (`-E` at `VERBOSE`), and a device that dies mid-session ends 0
too. *Why:* either change made without the device is a guess: the classifier
loosened for a behaviour IOS XE may not have, or the fake changed to match a
device nobody observed. *Not taken:* every 255 a success (a refused or lost
connection is 255 as well); the fake changed now.

## 6. Host keys and SSH algorithms

**One host-key policy for every mode and transport.** `ssh.host-key-policy` is
`accept-new` (the default: enroll an unknown key, reject a changed one),
`secure` (require a pre-enrolled key), or `insecure` (accept and warn); each
transport translates the one enum into its own options. Failures are per
device, not per run. A site locks the key in the global configuration where
weakening is not allowed; the global file is the only place a lock may be
declared. *Why:* separate system and native settings caused policy drift; the
three approaches serve organisations with different risk tolerances behind a
safe default that cannot be weakened silently. *Not taken:* aliases `auto` and
`default` (removed outright).

**A karvi-owned trust store, created 0600, never repaired.** The store is
`~/.local/share/karvi/known_hosts` under `auto`, resolved from the passwd
entry, in an operator-owned directory group and others cannot write; karvi
creates it exclusively at first enrollment and never changes the mode of a
store it did not make: a wrong mode halts `accept-new` and `secure` with the
remedy in the message. *Why:* `accept-new` used to repair a 0644 store silently
while `insecure` under an explicit path refused it, so the paths disagreed;
a legacy `~/karvi` candidate once won the chain by accident and split the
operator's state. *Not taken:* repairing with a warning; a shared trust store.

**The host-key identity is the canonical name and, off port 22, the port.**
Both transports enroll and look up `name` on port 22 and `[name]:PORT`
elsewhere; the address is not part of it. *Why:* different ports on one host
may be different SSH servers with different keys, and the two transports had
used different entry forms.

**Host-key algorithms strongest first, filtered to the enrolled types.** One
preference list serves both transports (ed25519, the ECDSA curves, RSA with
SHA-2, `ssh-rsa` last); with entries for the identity the offer is filtered to
the enrolled key types, and a device offering none is `host_key_changed`.
*Why:* x/crypto's default order preferred ECDSA and gave a false
`host_key_changed` against a store holding the device's ed25519 key.

**SSH algorithms are configured once for both transports.** `[ssh-algorithms]`
holds ordered `host-key`, `kex`, `ciphers`, and `macs` lists with defaults
strongest first (AES by key size, GCM then CTR then CBC; AES-192 and AES-128
out of the defaults); weak names are refused everywhere and some names are
allowed only in a per-host profile. `[ssh-algorithms-profile.NAME]` sets or
appends lists, and `[[ssh-algorithms-map]]` rules select a profile by name,
CIDR, platform, site, or group; a legacy device is reached through a profile
the operator configured, which is itself the record of the exception. Each
transport offers the names it implements, in the configured order, and a list
with none is refused before connecting. *Why:* the operator's order, and the
exception's record in configuration rather than a per-device audit field.
*Not taken:* the removed `ssh.legacy-hosts` classes with fixed algorithm
blocks; a `crypto_exception` field on every record.

## 7. The plan, the daemon, and the credential channel

**The client plans; the daemon validates and executes.** The client owns
configuration, inventory, target assembly, address resolution under client
authority, and credential resolution; the daemon receives an immutable, fully
enumerated execution plan and a separately protected, job-scoped credential
package, and it never loads inventory, expands a selector, or reads a
credential backend. A transitive import test holds the daemon and executor
packages away from the loaders. *Why:* the daemon must be unable to do what
the client is responsible for, and a property of the import graph cannot drift.
*Not taken:* an orchestrating client inside the daemon package.

**The plan is a complete, hashed statement of intent.** The execution plan
carries the operator, the exact ordered targets (each a device projection with
its address plan, credential binding, and platform), the commands and their
digest, and typed effective dispatch, output, ping, and session-init settings,
so the daemon never re-reads configuration to interpret it. It carries no job
ID and no mode, so an exercise and its live submission hash identically. Its
digest is over the canonical JSON, deterministic by field order; the plan and
IPC schemas are closed (`additionalProperties: false`), the report schema open.
*Why:* the daemon judges a plan it can only validate; an unknown field would be
content the daemon cannot judge that still affects the digest. *Not taken:*
reusing the inventory device type in the plan (address facts twice).

**Address authority is `client` or `daemon`, and one selection rule serves
both.** A literal address wins, then the inventory management address, then the
alternates; DNS runs only with no literal. Under client authority the client
resolves at planning; under daemon authority the client fills the query and
leaves the daemon's fields empty, and the daemon fills them at prepare and
signs them into the plan's preparation evidence. Any `daemon` target while
`name.allow-daemon-resolution` is false fails planning as a policy refusal
before any work. Planning failures abort before a job exists, listing the
failing targets. *Why:* a literal the client cannot reach is the bastion case a
remote daemon exists for; letting the daemon resolve is a site policy; a target
cannot be dropped without a terminal state. *Not taken:* continuing the run
with per-target failure records.

**Credentials bind after the authoritative address is known.** One credential
planner call binds every target that has a selected address and no grant, so
it serves the client-authority pass before prepare and the daemon-authority
pass after the evidence returns; an `address-cidr` rule sees the authoritative
address. *Why:* the rule may depend on the daemon's selection, and typing time
must not eat the preparation window.

**The credential package never has a wire form of its own.** Grants and the
package are secret-bearing values that refuse serialization; the package's
digest never covers secret bytes (under `local-peer` it is over the safe
projection, which is what manifests and audit carry). Lifetimes are constants:
a ten-minute acceptance window and a 24-hour grant validity. The `sealed`
protection is schema and interface only until a channel carries it, added as a
reviewed table entry, never a registration function. *Why:* a digest over the
plaintext package would be an offline-guessable password fingerprint; a key
would be revisited only after measuring provide-to-commit times. *Not taken:*
configuration keys for the bounds.

**Two sockets: the JSON envelope and the credential frame.** The
newline-delimited JSON envelope on `socket/daemon.sock` carries `prepare_job`,
`commit_job`, `follow_job`, `cancel_job`, and the lifecycle operations. A
second owner-only socket, `socket/credentials.sock`, carries one length-prefixed
binary frame per connection and one receipt back; its reader never parses an
envelope and the envelope reader never opens it, so exclusion of the secret
from request logging is a property of the code structure. The frame's bounds
are constants read before any allocation. *Why:* a JSON request followed by a
binary frame on one connection would put every logging hook one branch from
the secret bytes. *Not taken:* a mixed-mode connection.

**The channel token is a one-use capability.** Thirty-two random bytes, hex on
the wire, redacted under `fmt` and `slog`, marked used on the first frame read
whether or not the frame validates, expiring with the preparation; peer UID is
still checked on both sockets. *Why:* consuming before verification closes
replay; the token gates the frame but is not itself a secret worth refusing
serialization.

**The frame is refused early with its rule named; commit validates in full.**
The frame handler checks peer UID, decodes, finds the preparation, consumes the
token, unprotects, then runs every package rule that does not need the final
plan; a parity test asserts the same vectors are refused at both stages except
the two that need the plan. `commit_job` verifies the plan digest, the package
reference, and the whole package before running the job; the package lives in
daemon memory only from the frame to the job's end. A repeated commit under the
same key and digest replays the receipt; a different digest is a conflict.
*Why:* the daemon verifies rather than trusts; a package the commit would refuse
is refused at the frame.

**Two jobs of one operator against one device are isolated by structure.**
Target IDs are deterministic, so concurrent jobs share one; isolation comes
from the per-job package, and every cross-use (another job's package, token, or
preparation) is refused at its earliest step. A structural test fails on any
package-level variable holding a secret type in the daemon, executor, or
transport packages. *Why:* nothing may hold a secret outside a job's lifetime.

**The daemon's launch environment is an allow-list.** The client passes
`daemon serve` an explicit environment built by the one filter the system
transport applies to its `ssh` children (`HOME`, `USER`, `LOGNAME`, `PATH`,
`security.child-environment-allowlist`, plus every `KARVI__*` variable, `TZ`,
`TMPDIR`); a daemon that finds `NETUSER`, `NETPASS`, or `NETENABLE` in its
own environment writes one notice per name and does not refuse. *Why:* one
filter cannot drift into two; the daemon has not read the built-in variables
since planning moved to the client. *Not taken:* a deny-list; re-executing
with a scrubbed environment; refusing to start.

**The daemon leaves by itself when idle.** `daemon.shutdown-idle-timer`
(default `1h`; `0` never) ends a daemon with no active job and no live
preparation after the timer since its last request other than `ping` or
`status`; the client, finding the socket gone at its first request, launches
once more and repeats the request with the plan it already made. The packaged
unit starts the daemon with the timer off. *Why:* a team host gets one daemon
per operator and some sit for months, running the configuration and the
executable of that day; the cost is staleness, not memory; a unit's idle exit
is a clean exit `Restart=on-failure` would not restart. *Not taken:* default
off; counting `status` as activity (a dashboard would keep every daemon
alive); a daemon that reloads its configuration.

**The manifest is the record of intent.** It holds the commit header, the
final plan, the safe package projection, and the daemon's execution policy
(host-key policy, known-hosts file, Telnet allowance) as typed fields, and the
operator's selection inputs; duplicates of plan content are gone, and a
validator checks the plan's digest, the header against the plan, and every
record's device against a plan target. *Why:* the plan and header are the
record; duplicating them invites disagreement.

## 8. Rehearsal, follow, cancel, and the daemon's lifecycle

**`--dry-run` shares the live run's first half and calls only the probe.** The
inspection drafts the plan exactly as a live run does, binds client-selected
targets, probes the daemon without launching it, and renders a report to
stdout alone: no job, no preparation, no artifact. Where a live run would
abort before submission, the dry run aborts the same way. *Why:* sharing the
first half means the rehearsal cannot drift from what a live run drafts.

**An exercise is a job that never builds the executor.** `--exercise` runs the
full submission (prepare, frame, commit) and the daemon runs every local,
read-only check the executor would (binding, transport, askpass, host key
enrollment, ICMP capability, output preflight, dispatch arithmetic, capacity)
and writes `exercise.json` in the job folder; no session, probe, or command
path exists in exercise mode, and the report has no command status field, so
it cannot claim success by construction. Status `exercised`, exit 0 or the
first error finding's exit. *Why:* the proof of "no device contact" is
structural; the exercise predicts the live refusal exactly.

**`commit_job` answers at acceptance; the job runs on its own goroutine.**
Acceptance is the moment the manifest is durable and the start audit record is
written; the job slot is released when the run returns, so `drain` and `stop`
still see the job. *Why:* one client path and one daemon path for a followed
run, a detached run, and an exercise.

**The follow stream carries the records themselves.** `follow_job` answers one
request with a start, zero or more `record` frames each holding the record's
`commands.jsonl` line without its LF, and a terminal with the whole summary;
the cursor is the sequence alone. The client opens no file, and `--format
jsonl` output equals the file byte for byte. A record whose frame would pass
`daemon.max-ipc-frame-bytes` (default 8 MiB) is sent without its output and
with the notice `follow_output_omitted` naming where the output is. Followers
are subscribed first and then caught up from the daemon's own reading of the
file; a follower's queue is bounded at 1,024 and a slow one is dropped to
resume from its cursor. *Why:* when the record is the frame, the file becomes
only the persisted copy and a job that writes no file can be followed;
subscribe-then-catch-up meets without a gap or a duplicate; a slow terminal
must never block the record writer. *Not taken:* chunking a record over
frames; backpressure on the executor (a paused terminal would hold device
sessions open).

**`--detach` returns at acceptance; `--follow=false` waits and renders nothing;
an interrupted client never cancels the job.** Ctrl-C during a follow ends the
client's follow alone with a line naming the job, the folder, and `karvi job
follow`; from the frame through the commit the client runs without
cancellation so a package is never left without a decision. A broken stdout
ends the follow with `terminal_write_failed` (exit 111) and the job continues.
*Why:* cancellation proper is `cancel_job`; a client's disappearance must not
change the job; the job still produces output the client would lose.

**`job follow` renders the whole job as the foreground run would have.** It
follows from the zero cursor; the daemon catches it up and feeds it live; the
display ends with the footer or the jsonl summary line, and the exit is the
job's own. A job the daemon does not hold is read from its directory, located
as the day folder holding a subdirectory named by the ID; a directory without
`summary.json` and no daemon holding the job is `job_orphaned`. *Why:* the
operator who detached wants the job; the files are the canonical record and
the daemon serves the stream from them.

**Cancellation is a job-scoped cause with shutdown's accounting.** `cancel_job`
cancels one job's context with a cause carrying the reason, requester, and
time; the daemon answers `cancelling` or `terminal` at once and never waits. A
command in flight is recorded `cancelled`, a device any of whose units the
cancel interrupted counts `cancelled` (neither completed nor failed), the run
ends `cancelled` at exit 113. An exercise is never `cancelled`: the request is
accepted, changes nothing, and is recorded in the audit. The cancelled line is
printed only for a job whose final status is `cancelled`. *Why:* a command
already sent must never be recorded as succeeded or errored; a cancelled
exercise would save a fraction of a second and lose the report the operator
asked for; a reader would otherwise see "cancelled" over a job that ended
`exercised` at exit 0.

**Shutdown is a cause, not a cancel, and forcing never skips accounting.** A
forced stop or grace expiry cancels every job's context with a shutdown cause;
in-flight and unstarted units are `incomplete_shutdown`, the run exits 106
with status `incomplete`, and the daemon waits for every job goroutine, bounded
by `daemon.forced-grace-seconds` with a two-second floor, before removing its
sockets and state. `daemon serve` handles SIGTERM and SIGINT itself: the first
drains and waits `daemon.shutdown-grace-seconds`, a second shortens the wait.
*Why:* before this the job goroutine was never awaited and units owed a
terminal record received none; a forced stop must be distinguishable from a
device failure and an operator cancel.

**`daemon stop` and `daemon restart` refuse an active daemon without an
option.** With active jobs and no option they refuse naming `--grace` (drain
and wait), `--after=X` (drain, then force), and `--force`; Ctrl-C during the
wait ends only the wait. *Why:* the earlier `stop` stopped without checking.

**A client keeps lifecycle access to a daemon of another release, and
compatibility is the pair.** `ping`, `status`, `stop`, and `restart` reach a
same-UID daemon launched by another executable; a run is refused before any job
with `daemon_incompatible` unless the daemon's version and IPC schema both
equal the client's, and every release restarts the daemon. *Why:* an upgrade
never requires the old executable and never kills an incompatible daemon
automatically; the execution plan moved twice under an unchanged IPC schema
while the status said compatible, so the version catches every release and the
schema catches a pre-release client beside a released daemon. *Not taken:* the
version alone; comparing the commit; naming the plan schema on the status line.

## 9. Dispatch and the ICMP gate

**Dispatch is `serial`, `parallel`, or `wave`, bounded by a shared cap.**
`serial` runs one device at a time (the default), `parallel` a fixed width,
`wave` a bounded cohort to completion before the next, widened or narrowed by
the CPU sampler under the `dispatch.wave-*` keys. `dispatch.server-max-inflight`
is the host's cap on device sessions in flight across every job, held as leases
in a capacity ledger; `0` means `min(256, max(32, 8 × CPU))`, and the range is
0 to 4096. Halt-on-error count and percent and the wave gates stop scheduling;
a halted or gated device is `not_started_halt` or `not_started_wave_gate`.
*Why:* a fleet run must never exceed what the host, the devices, and AAA carry,
and the cap is shared so several operators' daemons compose.

**The ICMP gate is an operator-selected admission check, two probes, carried
in the plan.** `network.ping-targets` (default off), `--ping`/`--noping` as
lock-aware writes to it, two probes with `network.ping-timeout` each; two
replies proceed, one proceeds with the notice `icmp_packet_loss`, none skips
the device as `icmp_unreachable` before any capacity lease, open, or askpass.
The plan carries enabled and timeout, so the flag reaches the daemon's
executor. *Why:* a target that does not answer two pings should not consume a
session, and the plan is the only channel from the flag to the executor. *Not
taken:* the daemon's own configuration governing the gate.

**Two pure-stdlib pingers, detected once per job.** A datagram ICMP socket
pinger and a system `ping` adapter, chosen per job by what the host allows;
neither method available is `icmp_capability_unavailable` before any directory
or record exists. Operators are advised to widen `net.ipv4.ping_group_range`.
*Why:* a hardened host may grant neither socket while the system `ping` carries
the capability; a setuid helper is forbidden; per-job detection lets a sysctl
change apply without a daemon restart. *Not taken:* the adapter alone; caching
detection for the daemon's lifetime.

**The gate's line is a template.** `display.ping.header` renders once per
gated device before its first record, `! <target> [<address>] ping(1) <rtt1>,
ping(2) <rtt2>, <result>` by default; an empty template prints no line. The
record carries a `ping` object with both outcomes, and the summary a `ping`
block. *Why:* the line printed plain above a header carrying the `! ` prefix
and the roles' colours; as a template it reads as the header it precedes and a
site can reorder it. *Not taken:* a boolean beside a header key (TOML cannot
hold both spellings).

## 10. Output: records, files, and the spool

**Every activity's artifacts live below `<output root>/YYMMDD/<id>/`.** The
job ID is the client's clock to the second in the effective timezone plus a
two-character sequence (`YYMMDD-HHMMSS-xx`), so the day folder is the ID's
prefix by construction; a login's activity ID takes the same form. The client
reserves the ID by creating the job directory exclusively (a login by its
scoreboard file), bumps the sequence on collision, and releases the reservation
if the activity fails before submission, so a refused run leaves no folder;
the daemon refuses an ID it already holds. *Why:* the ID is the one identifier
an operator types and reads; a fan-out from a cron is the same-second case;
the reservation is what lets the ID be short. *Not taken:* a random pair;
microseconds; a separate short directory name beside a long ID.

**Folders and files karvi creates, and never alters.** Folders it creates take
`output.directory-mode` (`0750` default) and keep an inherited setgid bit;
files are `0640`, created exclusively without following symlinks; an existing
folder is accepted when writable and never changed in mode or group. A day
folder under a setgid root takes the root's own bits so a second operator's
first job of the day is not refused. *Why:* teams read output through group
membership and setgid folders with no copying and no karvi-managed ACLs; karvi
sets permissions only on what it creates.

**The record is the authority; every file derives from it.** `commands.jsonl`
holds one record per command, `errors.jsonl` the non-succeeded ones,
`output.TARGET.txt` one readable terminal session per device (header, prompt
and statement on one line, the answer, karvi's own lines as `!` comments), and
`tools/textfile` derives the text file from the records. Every command record
carries `promptbefore`, the prompt the device showed when the statement was
sent. A text block that cannot be written is a notice, not a failure, and the
device's file ends early. *Why:* the text is the copy and the copy gives way
first; the returned prompt is the wrong one whenever a statement changes the
mode. *Not taken:* device output inside `commands.txt` (it is the rerun's
input); building the text at the job's end.

**The set-up lines are in the text file; the enable secret is never kept.**
The text file opens as the session did, `enable` and the paging commands at
their prompts, and nothing read between sending the secret and the next
prompt is kept anywhere. *Why:* the plainest rule that makes "never shown"
hold without matching the secret's bytes against output; a device that echoes
the secret has put it in exactly those bytes.

**One boolean key per output file; the invocation decides on every path.**
`output.files.*` has a key per file, all true by default; each skips its own
file and no other; all eight false is `--nof`, which writes no folder. The
plan's output block carries `persist`, the eight switches, and the resolved
root, so the daemon writes what and where the invocation said, and `job follow`
looks under the same root. `cmd --nof` and `cmd --of[=PATH]` are flag-origin
values of `output.persist-command` and `output.root`, so a lock on the key
refuses the option by the lock alone. *Why:* a site chooses which files it
keeps; the daemon's store stays the one writer while the client's root
travels in the plan. *Not taken:* the daemon's configuration as the authority
for the files (two roots could disagree).

**`output.max-job-bytes` counts the lines and the text blocks; the record
decides.** A line that would pass the limit fails the append with
`output_job_limit_exceeded` and ends the job as an output failure; a text
block that would pass it is not written under the notice rule. *Why:* a job
passed its limit because only `commands.jsonl` counted.

**Indented `.json`, compact `.jsonl`.** Every `.json` file karvi writes is
indented; every `.jsonl` file is one compact object per line. Scripts read JSON
through one parser (`scripts/lib/json.sh`), never by matching text. *Why:*
nothing digests a `.json` file's bytes, and every reader decodes; text matches
held for one layout only.

**A response is cleaned as it arrives into settled bytes, held in memory to a
threshold and spooled above it.** Carriage returns and the echo are dropped
chunk by chunk; the first `output.spool-threshold-bytes` (default 1 MiB, range
0 to 1 GiB) stay in memory and the byte that would cross opens one spool file
per command under `spooldir` and moves the head into it. The session holds per
command only the settled bytes to the threshold, a 4 KiB tail where prompts and
declarations are matched, small carries, and the running digest. The output
limit counts settled bytes. *Why:* the daemon's memory was in-flight responses
times output size times several whole copies; with the spool it is width times
the threshold plus small windows, a figure of keys the operator has, and a
device dumping one 60 MB line is the case the spool exists for. *Not taken:*
spooling the wire bytes and cleaning at the write; an unbounded last line; a
memory budget key or a check against available memory.

**One output source every consumer streams from, verified before commit.** The
record's output is a source (the string, plain text, or the spool file with its
count and digest) that the `commands.jsonl` line, the `errors.jsonl` line, the
text block, the collection block, and the counting pass stream through, escaping
32 KiB at a time; the measuring pass hashes a spool as it reads and refuses the
record with `output_spool_mismatch` before any byte of the line is committed.
The executor removes the spool once, after the append and the display's
hand-over, on every ending. Every ending before the prompt returns (timeouts, a
lost session, a cancel, a forced stop, the limit) records the settled bytes with
its status. *Why:* five readers of a string became five callers of one method;
verifying on the measuring pass is free; a `show tech-support` a timeout cut is
what the operator wants to see. *Not taken:* a record type that carries the
file; recording nothing at a cut.

**The spool lives under `spooldir`, never the scratch directory, named for its
owner process.** `auto` tries `/tmp/karvi-<uid>` then `/var/tmp/karvi-<uid>`;
`tempdir`'s tmpfs choice is by design and a spool under it would put the
response back into RAM. A spool is `<activity>.<device>.<index>.<pid>.spool`,
and at daemon start and every admission a file of that shape whose owner
process is dead is removed and logged; anything else in the directory is not
touched. *Why:* the name is the metadata, readable by `ls` and by the sweep
without opening a file; a signature keyed by nothing proves nothing about a
same-uid file in a 0700 directory. *Not taken:* the job's own folder (no folder
under `--nof`); metadata inside the file; a sweep by age.

**One free-space check of every volume the activity writes, before any device.**
The output root, a collection directory, `spooldir`, and a transcripts root are
grouped by volume and each volume read once. `freecheck` is `auto` (the sum of
what the places will hold plus `output.min-free-bytes-after-job`, 2 GiB, once; a
volume short only by the spool's term narrows the job's width with the warning
`spool_width_narrowed`), `always` (the floor alone), or `never`. A refusal names
every path on the short volume. The audit file, scoreboard, and state root take
no check. *Why:* a full disk on the collection or transcripts volume was found
by the write, after the devices had run; narrowing keeps the common job running
on a small `/tmp`; the small writers' first write lands before any device. *Not
taken:* a key per place; refusing whenever the worst case does not fit;
`fallocate` per command.

**The daemon formats nothing; the in-process renderer streams.** Under the
daemon the renderer keeps the counts the summary needs and returns before the
format step; in process the renderer streams a spooled record in every format
from the source. The follower's queue carries no output: the live feed reads
the durable line at its offset from the file. *Why:* the daemon paid a whole
escape per record for a writer that discarded it; the queue of records with
output was the memory the spool removed, back.

**The watch screen shows the bytes in flight.** The scoreboard's target row
carries the settled bytes of the running command from one atomic per device,
summed on the metrics line, and a heartbeat rewrites the snapshot every
`watch.refresh`. *Why:* reassurance that a long command is moving, at no cost
per byte; a follow event would move the IPC schema and message every
subscriber per interval.

## 11. The collection run

**A collection is one flat directory of device files, replaced only on
success.** `crun` writes each device's output to `crun.directory` (default the
shared `crun` tree when usable, else `<basedir>/crun`) under one file name per
device, the canonical name's first label lowercased under `output.crop-to-dot`,
an address with its separators as hyphens, no suffix; two devices whose names
collide end the draft at planning. The file is written as a hidden temporary
and renamed into place at the device's last record; any status other than
succeeded or a device-rejected statement leaves the previous file untouched.
*Why:* RANCID and Oxidized write no suffix; a reader never sees a partial file,
and one statement a platform lacks must not cost the rest. *Not taken:* the
crop in the name-transform (it would rename the device everywhere); a `.cfg`
suffix; success as "the running-config alone succeeded".

**The collection file is output only, with one `! COMMAND` marker per block.**
No header, set-up lines, prompt, timestamp, or error line; a marker before each
block so a diff names the command that changed and an emptied block stays
visible. Lines matching a platform's `crun-filters` (Go regular expressions,
built-in lists dropping byte counts, uptimes, and NTP clock periods) are
dropped from the collection file alone. *Why:* a collection differs at every
run before anything changed, and with the hook that noise is a commit and a
mail a day; a change stamp says when and by whom, and is kept. *Not taken:*
masking in place of dropping; secret masking (the site's `.gitattributes`
filter at the commit).

**A `run` or `command` with `--cd=PATH` writes the collection file, unfiltered,
replaced only when the device succeeds.** The file is the collection's: one per
device, named as a `crun` names it (a collision ends the draft with
`output_file_name_collision`), a `! COMMAND` marker before each requested
command's block and a blank line before every marker but the first, no header,
set-up lines, prompt, timestamp, or error line; written as the hidden temporary
`.NAME.JOBID` and renamed into place at the device's last record. A platform's
`crun-filters` are not applied: the file holds every line the device sent. It
is replaced only when every record of the device succeeded or is a statement
the device rejected; any other outcome (connection, login, timeout, a command
not attempted, a halt, a cancel, an interrupt, a block that cannot be written)
leaves the previous file untouched and removes the temporary. `--continue`
stays the run's own: without it a rejected statement ends the device, its later
commands are not attempted, and the file is kept; with it the file is replaced,
the rejection in its block. *Why:* an operator's ad hoc capture is one file per
device in a directory of their choosing, in the shape a collection already
has; the filters exist to quiet a nightly diff, while a run's file says what the
device said; one success rule for every collection. *Not taken:* applying
`crun-filters` (a `run --cd` into the `crun` tree then differs from the next
`crun` by the dropped lines, accepted); `--cd` turning `--continue` on, as
`crun` does; writing a failed device's partial output.

**A run's collection leaves the run's job folder as it was.** A `run` or
`command` with `--cd` writes the folder it writes without one, `output.NAME.txt`
included under `output.files.output-txt` (a `crun` writes none); `summary.json`
and the jsonl summary document carry the `collection` block (the directory,
`replaced`, `kept`, each device's file and outcome) for every collection; under
`--nof` the collection is written and no folder (`artifacts=none`); `--nof
--detach` stays refused. *Why:* a kept collection file is the previous one, so
the folder's text file, with its header and error line, is the one place that
says what happened this time. *Not taken:* turning `output.NAME.txt` off as a
`crun` does.

**`--cd` gives `run` and `command` the file alone; the rest of a collection is
`crun`'s.** `crun.after` runs after a `crun` alone, never after a `run` or
`command` with `--cd`, whatever the directory (a file a run wrote into the
`crun` tree reaches the next `crun`'s commit); the watch screen's MODE is the
operator's word (`run`, `cmd`, `crun`, `exercise` first as before) and the audit
names are unchanged; `crun-commands` and `crun-filters` stay `crun`'s, so `run
--cd` and `command --cd` still need a command. The plan says which word asked:
the collection block carries `word` (`crun`, `run`, or `command`; execution plan
schema 10), any other value `execution_plan_invalid`, and no older plan or
daemon is accepted; MODE and the filters' validator read it, and the client
runs the hook for the `crun` word alone. *Why:* the daemon
receives every job as a `run`, and the code had taken a collection for a
`crun`; the hook is a site's nightly commit or mail, not an operator's capture.
*Not taken:* the hook for every collection; MODE `crun` for any collection.

**Every collection directory is resolved, made, and checked as a `crun`'s.**
The client resolves `--cd=PATH` before planning (`~` expanded, a relative path
made absolute against its working directory, `auto` the collection tree on
every word), `crun_directory_unavailable` when it cannot,
`cli_option_value_missing` for a bare `--cd`; the directory is checked once
before any device, a missing one made with its missing parents at
`crun.directory-mode`, and one that is not a folder, cannot take a new file, or
has the sticky bit and another owner is `crun_directory_not_writable`; files
take `crun.file-mode` and, under setgid, the directory's group. The `crun.*`
keys and the `crun_directory_*` codes keep their names and document every
collection; `--cd` is `crun.directory` as a flag-origin value on every word, so
a site that locks the key refuses `run --cd` and `command --cd` as it refuses
`crun --cd`. Under a daemon sandbox that applies, the daemon writes only where
`ReadWritePaths` allows: a home directory is refused loudly, a `/tmp` path
lands in the daemon's private `/tmp` silently; the remedy is the unit's drop-in
or `--no-daemon`, and `command` never meets it. *Why:* one rule for one kind of
directory. *Not taken:* `collection.*` keys and `collection_directory_*` codes
(every site's configuration broken for a name).

**`--fs=SUFFIX` appends a literal suffix to each collection file's name; on
`run` and `command` it implies `--cd=.`.** The value is appended as written to
the device's file name (`r1` and `--fs=.cfg` give `r1.cfg`, its temporary
`.r1.cfg.JOBID`) and to nothing else: not the directory, the job folder, or
`output.NAME.txt`. It is written with `=` alone; bare or empty (`--fs=`, which
the parser cannot tell from bare) it is `cli_option_value_missing`, as `--cd`
is, and holding `/`, NUL, or a control character it is `crun_suffix_invalid`,
checked before planning and when a stream line is read. On `run` and `command`,
`--fs` without `--cd` is `--cd=.`, the client's working directory made absolute
before planning (and a stream's working directory at `--go`); on `crun`, `--fs`
alone takes `crun.directory`. The directory messages say "the working
directory, implied by `--fs`" when it was. The plan's collection block carries
`suffix` beside `word` (schema 10), validated alike (`execution_plan_invalid`,
`output.collection.suffix`); the collision check, the summary's
`devices[].file`, the hook's standard input, and the dry run's `collection:`
line carry the suffixed name. The stale-temporary sweep removes only `.FILE.`
followed by a job ID's form, `FILE` the suffixed name; two concurrent runs over
one file can still sweep each other's temporary, the swept device
`collection_write_failed`. *Why:* a suffix an editor or a diff tool recognises;
an operator who asks for suffixed files with no directory wants them here; a
scheduled `crun` must stay in the site's tree. *Not taken:* a configuration
key; a template; the suffix on `output.NAME.txt`; refusing `.` and `..` (a
suffix follows a name); a length bound; `crun_suffix_unpaired`; `--fs` implying
`--cd=.` on `crun`.

**The command list per platform lives on the platform table.** `crun-commands`
is a string array beside `paging-commands`, with built-in lists for the
platforms that have a configuration; a `crun` that names no command takes each
device's platform list, so `karvi crun --all` is the whole nightly invocation.
*Why:* the platform table is the one place a platform is described. *Not
taken:* a list under `[crun]` keyed by platform; a per-device inventory column.

**Replacement needs the directory's write bit only; the directory is checked
once.** An operator replaces a file another wrote whatever its mode, because
the write is a new temporary and one rename; the directory must be a real
directory the runner can create files in, and a sticky bit the runner does not
own is refused once before any device (`crun_directory_not_writable`, naming
mode 2770 or 2775). Files take `crun.file-mode` (default `0660`) and the
directory's group under setgid. *Why:* under the sticky bit every rename over
another operator's file would fail one device at a time. *Not taken:* chowning
the file; tolerating the sticky bit.

**The client runs one hook after the collection.** `crun.after` names a site
executable run once when the summary is written, in the collection directory,
with the replaced files' names on stdin and the counts in its environment,
bounded by `crun.after-timeout`; two example hooks ship (a git commit and a
diff mail). *Why:* the client has the operator's environment (git identity,
mail transport) while the daemon under the packaged unit is sandboxed; git and
mail are a site's choices. *Not taken:* a built-in `crun.git`; the daemon
running the hook; mail sent by karvi.

**The schedule is the scheduler's.** A packaged systemd timer and a cron line
under `flock` run `crun --all --no-daemon`; karvi holds no directory lock and
has no schedule key. *Why:* systemd never starts a service while the previous
instance is active, and a directory lock in karvi would refuse the overlap a
site wants (an operator's `crun --target X` beside the nightly `--all`). *Not
taken:* `crun --every DURATION` under the daemon.

## 12. Display and help

**Templates and roles.** Headers and footers are templates per mode
(`display.command.header`, `display.run.footer`, and the rest), enabled by
default, suppressed by `--quiet` or an empty template, and every literal in
them takes the `label` role; the defaults begin with `! ` so a reader and a
filter tell karvi's lines from the device's as the text file's `!` lines are
told. Twelve colour roles under a dark and a light theme, `display.theme`
`auto` resolved by the terminal, `karvi config colors` showing each role in its
colour under both themes. *Why:* one display mechanism rather than parallel
banner switches; a site that wants another mark writes its templates. *Not
taken:* `--command-headers`; a key for the prefix.

**Every run ends with the footer; jsonl ends with the summary document.** Text
mode ends with `display.run.footer` on every run path, rendered from the job's
summary, with no result line on standard error; `--format jsonl` ends with the
summary document as the stream's last line, so a script reads one stream, the
records then the summary. A run with a collection (`crun`, or `run` or
`command` with `--cd`) adds one line after the footer, the template
`display.collection.footer` (default `! collection=<collection>
replaced=<replaced> kept=<kept>`, `<collection>` the absolute collection
directory), in the footer's roles, written by the footer's renderer to standard
output directly after it on every text path (`--no-daemon`, the daemon follow,
`job follow`, a finished job's replay), and under json and jsonl never, as the
footer is not (the jsonl summary document and `summary.json` carry the
collection block); `--quiet` or an empty template suppresses it. *Why:* a run
through the daemon ended with a plain stderr line while a `command` ended in
the footer's colours; values belong on stdout in the chosen format; `crun`'s
plain result line repeated the footer's exit and folder and could be neither
styled nor set; on the footer's stream the line cannot part from it. *Not
taken:* the result line beside the footer; a plain counts line on standard
error under json and jsonl (the unstyled line replaced); a `<collection>`
placeholder in the run footer (empty for every run without one).

**The helper's help is karvi's layout, coloured without configuration.**
`internal/helplayout` holds the one help layout (the title tag in the label
colour, headings in bold, option words in the accent colour, a Usage line's
action word in the success colour), which karvi and `karvi-prune` both use;
karvi decides its colour from `display.*` as before. `karvi-prune -h` and
`--help` print to standard output, exit 0: a title line, `Usage:` with the
synopsis wrapped at 79, and `Options:` with each flag in `FlagOrder`, its
placeholder in a 31-column field and its usage string beside it, the default
after; a usage error prints its message and the text on standard error, exit
2. The helper reads no configuration, so its colour is the defaults' (on when
the stream is a terminal, the dark theme's roles) and a site's `display.*` does
not reach it. *Why:* one look across karvi's executables; the helper's
no-configuration rule stands. *Not taken:* Go's `PrintDefaults` form; a colour
flag on the helper; reading `display.*` in the helper.

**An option's values are explained in its own entry.** A help text has two line
shapes for an option: an entry, its words then the description at column 33
(or under them when the words are too long), and prose; no line separates words
by two or more spaces anywhere but at that column, and a test holds every help
text to it. `--ssh-host-key-policy` has one shared entry (`hostKeyPolicyHelp`)
in `login`, `command`, and `run`, naming `ssh.host-key-policy` and the three
modes with the default, where `login` alone had a "Host-key modes:" section in a
12-column field and the other two said nothing. *Why:* the policy is one key
for the three words; a line in a shape the layout does not know is prose to the
terminal (no accent colour) and to the man page (three modes run into one
paragraph). *Not taken:* the section kept in the entry shape (the modes read as
options, and `command` and `run` still say nothing); a second column width in
the classifier.

**`run`'s Dispatch options each have an entry.** `--dispatch`, `--workers`,
`--start-width`, `--max-width`, the two halts, the two wave gates, and
`--wave-delay` each have an entry at the column saying what the option does, its
key, and what 0 means (the widths at 0 from the host's CPUs, the halts and gates
off, the delay none). The percent halt is stated as built: against the devices
ended so far, checked as each ends, so a first device's failure halts at any N.
What a duration's units are, the auto widths' formulas with their values by CPU
count, and how the ceiling compares with the host's cap
`dispatch.server-max-inflight` are placed with the man pages' hand-written
sections. *Why:* the block listed eight options with no word of what they do,
their keys' registry entries give ranges alone, and [SCALE.md](SCALE.md), which
explains them, is not installed. *Not taken:* the compact lines with a pointer
to [SCALE.md](SCALE.md); changing the percent rule in a help change.

**`NO_COLOR` turns colour off under `auto`, on every path.** `display.color`
is `auto` (the default), `always`, or `never`; under `auto`, colour is on at a
terminal unless `NO_COLOR` is set and not empty. The rule is
`display.ColorEnabled`'s, so the help, a run's display on both paths,
`config colors`, the watch screen, and `karvi-prune -h` (whose mode is `auto`
alone) follow it; `always` and `never` are explicit choices `NO_COLOR` does
not override, watch's `--color` among them. *Why:* the convention (no-color.org)
asks a program that colours by default to honour a non-empty `NO_COLOR` and
lets a configuration or a per-invocation option override it; the watch screen
alone read it, beating even its own `--color always`, and every other path
ignored it. *Not taken:* `NO_COLOR` over `always`; `NO_COLOR` as a
configuration layer setting `display.color = "never"` (locks, and an order
above the files); a karvi variable of its own.

**Debug output shows each command once and never a payload.** Debug never
contains passwords, tokens, or device output; it shows each command sent
exactly once, from the plan and never from the device's echo, with a marked
secret redacted. *Why:* operators need to see what was sent while the stream
stays payload-free.

## 13. The watch screen

**A diffed frame on the alternate screen, karvi's own.** `karvi watch` renders
a frame in memory, diffs it against what the terminal shows, and rewrites only
the changed lines in one write; raw mode makes Ctrl-C a key; no terminal
library is vendored. *Why:* a full rewrite each second flickers; a line is the
unit a scoreboard change moves; quitting must restore the shell.

**The scoreboard names the work, never the command text.** A snapshot carries
the mode, each target's state, the invocation's inputs, the command count and
file name, the collection's counts, the metrics, and whether the daemon ran it.
*Why:* the directory is readable by the whole group while a job's folder is its
operator's; the statements are read in the folder.

**Running rows above a rule, finished below it newest first, two lines that
never leave.** Nine columns; running rows pinned between the heading's line and
the rule; only the finished rows scroll; a stale running row is flagged and
muted; the table shrinks column by column as the terminal narrows. Enter opens
a detail pane in the lower third following the selection, which is a job ID
and so survives reorders. *Why:* the running rows always on screen are the
deconfliction the screen exists for; sorting had scrolled the rule off and the
running rows with it.

**The `/` filter is words over five fields; `s` cycles the sort in the header's
order; `S` reverses.** Each term is a case-insensitive substring that must
occur in one of operator, job ID, target, mode, or status; a trailing space
changes nothing. `--filter` and `--sort` are the script forms, refused with
`--format json`. *Why:* one substring matched as typed made `cmd ` hide every
row; the cycle should follow the displayed field order and `S` names the
direction. *Not taken:* field prefixes; a regular expression; a persistent
filter key.

## 14. Storage: roots, shared trees, transcripts, and retention

**The private root is the operator's, by username, where the site provisioned
it.** `basedir = "auto"` resolves `/opt/karvi/users/<username>`, then
`/var/lib/karvi/users/<username>`, then `~/.local/share/karvi`; an existing
leaf is taken as it is, a present `users` directory has the leaf created in it
at `0750`, a present but wrong `users` directory hard-fails with no
fall-through, and `users` itself is created only by `sudo karvi setup shared`
or the administrator. The socket and state subtrees are `0700`; the daemon's
socket and state are one operator's. *Why:* a site keeps every operator's
private state under its backups and quotas with one root step and none per
operator; an operator's state is never split between a home fallback and a
root provisioned later. *Not taken:* creating `users` from an operator's run; a
uid leaf; a shared `basedir`; a legacy `~/karvi` candidate.

**The shared trees are the default when they exist.** `sharedroot` (`auto`
consults `/opt/karvi/shared` then `/var/lib/karvi/shared`) holds the three
trees `jobs`, `crun`, and `transcripts`; with the corresponding setting at
`auto` the first root holding a usable tree is the default, a tree that exists
but cannot be used is refused naming the two ways out, and an explicit setting
never asks. `sudo karvi setup shared` creates the layout in the site's group
with the setgid bit and repairs what it made. The plan carries the resolved
root so the daemon writes where the client decided. *Why:* a team keeps one job
tree so an operator reads another's job with the same `job follow` and a lead
finds the shift's work in one place; an operator outside the group is told, not
diverted to a private tree. *Not taken:* a silent fall back; a `[shared]`
table; moving a site's old trees.

**The scratch root is the site's; an operator makes only its own folder
in it.** `sudo karvi setup shared` makes `/dev/shm/karvi` and its
`scoreboards` at 3770 and its `capacity` at 2770 in the operators' group,
and writes `/etc/tmpfiles.d/karvi.conf` from the same list of places, so
the boot makes them again on the emptied tmpfs. An operator's process
never creates a missing parent: it makes its own `<username>` folder
(0700) inside an existing root, and a missing `scoreboards` or `capacity`
folder inside an existing parent with a setgid parent's bits. A root that
is absent gives the private places under `basedir` without a word; a
folder that exists but is closed to the operator gives them with one
warning naming it. The capacity ledger's files take its root's group modes
(2770 gives 0660), another user's process is a live lease holder, and a
ledger that cannot be read is an error. *Why:* the first operator's run
had created the root at 0700 and closed it to every other, whose
scoreboards and leases fell silently to private places, so the team's
watch showed one operator and the host's cap held per operator; under a
root made right, the ledger's private files refused the second operator's
every device, and the reaper, reading EPERM as death, took every other
operator's live lease; a boot empties `/dev/shm`, so a one-time setup
alone would last until the next. *Not taken:* creating the root from an
operator's run with a group mode (the group is the site's word, given to
setup); a warning on a host without the root (nothing the site made is
wrong there); a packaged tmpfiles file with a fixed group (the site's group
is setup's `--group`); the sticky bit on `capacity` (a member could not
replace a ledger another wrote); a suite-wide rule that an explicit path is
never created (the suites point these keys at folders of their work
directory, whose parent exists).

**Login transcripts record the device stream only.** `--record[=PATH]` writes
`<device>-<HHMMSS>.log` and its metadata side by side in a day folder,
exclusively created and bumped together on collision; the transcript holds what
the device returned and never operator keystrokes, so a password typed at a
non-echoing prompt is not written. `transcript.format` values other than text
are refused rather than silently falling back. The terminal names the file
before the session and again as its last line, a failed session included,
with one display template each (`display.record.header`,
`display.record.footer`) whose defaults match, `! transcript=<transcript>`,
rendered and colored as every header and footer.
*Why:* the operator declined recording the input stream; a JSON document is
valid only when complete; the header scrolls away in a long session, and
the operator leaving it wants the path where it ends.

**Retention is a helper of its own, walking `YYMMDD` day folders under both
roots.** `karvi-prune` reads no configuration; its eight flags carry the
settings' names (`--basedir`, `--sharedroot`, `--scoreboards`, `--days`,
`--minfree`, `--dry-run`, `--verbose`, `--format text|jsonl`). It walks `jobs`
and `transcripts` under the private and the shared root, never `crun`;
ownership decides (a run removes what the invoking user owns and passes the
rest by; root removes everything eligible); a removal that fails is one line
and the run goes on; the free-space floor is judged per filesystem. Empty day
folders older than the age are removed by their name's date; an orphan folder
without a summary and a stale non-terminal scoreboard retire by age alone,
never under pressure. The six final statuses are one list. *Why:* the helper
had matched a day-folder layout no writer used, so nothing it walked was ever
pruned; a colleague's folder in a shared tree would otherwise stop a daily run
at the first failure; three shapes stood forever. *Not taken:* a `karvi prune`
word; pruning by group membership; touching `crun`.

**Every unit's writable paths carry the dash and name what the command
touches.** `ReadWritePaths` entries are `-`-prefixed so an absent path does not
fail the unit, and each unit's line names the roots, shared trees, and
scoreboard directory its command walks; a site that gives a unit another path
adds it to the line. *Why:* the shipped line failed every unit on a host lacking
one of its paths; a place the helper walks that is not on the line is a failed
line per item, daily.

## 15. Configuration and errors

**The registry is the source; everything else is generated.** Every fixed key is
a row of the configuration registry with its kind, default, lock eligibility,
environment name, documentation, and range; `configs/reference.toml`,
`schema/config-schema.json`, and the key table are generated from it and
compared byte for byte by the verifiers. Layers apply in order: files, the
`KARVI__` environment, flags, `--set`. Locks may be declared only by the
auto-discovered global configuration. *Why:* the registry is the operator's view
of what karvi reads, and a row nothing reads misleads.

**The reference configuration is rendered table by table, and it loads.**
`configs/reference.toml` and `karvi config generate` give the top-level keys
first, with no table, then each table once, in the order of its first row in
the registry, with all its rows in registry order, each key under its
documentation. A test loads the rendered reference and holds that every
registry key is read from it, from its own line, at its default. The registry's
rows are written by hand in `configschema/registry_data.go`, and its header
says so. *Why:* the renderer opened a table whenever the table changed in row
order, so `[dispatch]`, `[output]`, and `[display.run]`, whose rows lie apart,
were opened more than once and TOML refused the file, and the top-level keys
after `[config]` were read as `config.basedir` and the rest; the reference had
not loaded since the public tree began, and `generated-clean` compared it with
the generator alone. *Not taken:* reordering the registry (the next row splits
a table again); dotted keys without tables; a suite running `config validate`
on the generated file.

**Every documented range is enforced by one loop.** A row carries `Min`,
`Max`, and `ZeroDisables`; one loop applies them to every bounded row after
the type check, refusing as `config_value_out_of_range` in the row's words.
*Why:* nine of ten out-of-range durations loaded as valid and the readers
disagreed on what an unbounded value meant. *Not taken:* hand-written rules
beside the loop.

**An option that stands for a key is that key's override.** Every command-line
option that stands for one configuration key is applied as an override of the
key in the lock-aware layer (`ConfigFlags`), so the key's type, range, cross-key
checks, and lock apply to it as to `--set`: `--order`, `--blind-wait`,
`--ssh-host-key-policy`, `--ssh-known-hosts-file`, `--ping` and `--noping`,
`--4` and `--6`, `--of` and `--nof`, and `--cd` as before, and `run`'s Dispatch
options with them: `--dispatch` and `--dp`, `--dw`, `--ds` (`dispatch.default`),
`--workers` (`dispatch.parallel-workers`), `--start-width` and `--max-width`
(`dispatch.wave-start-width`, `dispatch.wave-max-width`), the two halts and the
two wave gates (`dispatch.halt-on-error-*`, `dispatch.wave-gate-error-*`), and
`--wave-delay` (`dispatch.wave-gate-timed-delay`). A locked key refuses its
option (`config_lock_violation`), an out-of-range value is the key's own error,
and the planner reads the keys alone; the plan's dispatch block is unchanged.
The command line's own dispatch checks, `dispatch_value_negative` and
`dispatch_percent_out_of_range`, are retired to `config_value_out_of_range`.
`--address-authority` is not one: it names a device's authority, above the
inventory row, and `name.default-address-authority` is the default beneath both.
*Why:* the Dispatch options went to the planner beside the configuration, so a
site's lock on any of the nine lock-eligible keys was passed by the option
(`--max-width 64` planned under a lock of 8; `--wave-delay 5m` under a lock of
`0s`), and values the keys refuse were clamped without a word or reached the
plan's validation. *Not taken:* the command line checking each range itself (a
second copy of the registry's ranges, the locks still passed); clamping with a
notice; `--dispatch` outside the rule.

**A value an option sets is sourced to that option.** The flag layer carries
each value with the option that set it, so a message and `config show` name the
option by its long name where they named `command-line`: `… for
execution.blind-wait at --blind-wait`, `source: --ipv4`; `--fs` implying
`--cd=.` is `--fs`, `--dp`, `--dw`, and `--ds` are `--dispatch` (the parser
hands the option stood for), and `--continue-device-on-error`, which writes
`execution.halt-device-on-command-error`, names itself. An error is reported
against the key whose value failed: an unknown zone is `timezone`'s, at its
source (`--timezone`, a file's line, the environment), where the timestamp
formatter's check had reported it against `display.timestamp`. *Why:* one
source, `command-line`, stood for 21 options, so a lock's refusal of
`--continue-device-on-error` named a key the operator never typed and nothing
they did; `--set` and the files already named themselves. *Not taken:* a second
map of option names beside the values; each message in the option's own words
(the range and the lock are the key's); both sides of a cross-key check; `--dp`
naming itself.

**A removed key is refused from every layer.** One table of removed keys, one
code (`config_key_removed`), the message naming the key, the release, and the
replacement; the environment form is refused even when unknown variables are
tolerated. *Why:* ignoring a removed key would drop a site's intent silently.
*Not taken:* a code per removal; documented no-op keys.

**Every distinct error cause has its own stable code, and the registry is
enforced.** Two causes never share a code, a code is never reused, a retired
code stays with a named successor, and a fallback code is registered as
unclassified. `internal/errorcodes` holds every code with kind, status,
category, exit, and cause; [`docs/ERROR-CODES.md`](ERROR-CODES.md) is generated
from it; a test parses every Go file and fails when a code is emitted but not
registered or registered but not emitted; another fails on a new uncoded error.
Process exits come from the registry. *Why:* the catalogue and the source had
drifted, and nothing prevented two causes sharing a code; a table generated from
one registry cannot drift from behaviour. *Not taken:* a hand-maintained table.

**Schema counters are independent and never reused.** Each serialized
contract (configuration, command record, scoreboard, audit, job, daemon IPC,
registry, execution plan, credential package, plan report) keeps its own
counter below 100 until 1.0, when every counter resets to 100; `karvi version`
reports them all. *Why:* released daemons are identified by IPC schema, so
reusing a low number would collide with a released protocol.

## 16. Build, verification, and release

**One shape of the source, one build, vendored.** The scrapligo-v1 adapter is
compiled into every executable with no build tag; `vendor/` is part of the
tree so a build host needs no network; `make build` from the vendored tree is
the release. A native slot naming an unregistered implementation fails closed.
*Why:* a tag-gated preview shape rotted unless something compiled it and once
shipped the wrong executable.

**Verification runs the battery once per bytes.** The release verifier builds
from source and runs the tests, the race detector, and every suite; the bundle
verifier checks the shipped executables as bytes (checksums, counters,
transports, help, removed keys) and then the same battery from the vendored
tree; the suites are one list run in three lanes with separate work
directories, sockets, and scoreboards, never touching the host's shared
places. *Why:* the proof that two trees hold the same bytes is stronger than
running the same tests on both; the list cannot drift between verifiers.

**A release is a source bundle, its checksum, and an aggregate checksum
file.** The bundle is packaged twice for byte reproducibility and verified from
its own archive with its own verifier; the release tree is clean at the tag
(nothing untracked, no empty directory) and the tools refuse otherwise; the tag
is `karvi-vX.Y.Z` and `main` moves only to a tagged commit. *Why:* every release
is installed as new from the bundle, and the archive's own verification is the
claim a site repeats. *Not taken:* a cumulative patch beside the bundle (no
site built from one, and the diff between two tags is one command).

**Suites and tests keep off the host.** Every suite sets its trust store,
base directory, spool directory, scoreboard directory, and `sharedroot none`
under its own work directory; every test binary that reaches the shared-tree
resolver isolates it, with a scratch root of its own made under `/tmp`, never
`TMPDIR`, so a socket in it stays within the path limit at the release's
145-byte `TMPDIR`; a must-not-appear check stops the script. *Why:* fifteen
scripts once enrolled into the operator's real trust store, and a verifier run
left job folders in a site's shared tree; a root under `TMPDIR` failed twenty
socket tests at the 0.26.0 release.

**A man page is roff written by hand, its SYNOPSIS and OPTIONS generated.**
`packaging/man/karvi-prune.8` is one committed file; between the comment lines
`.\" BEGIN GENERATED SYNOPSIS: tools/mangen from internal/prune; edit there`
and `.\" END GENERATED SYNOPSIS`, and the same pair for OPTIONS, `tools/mangen`
writes the synopsis from `prune.Usage()`'s parts and one entry per flag from
the helper's flag set (the placeholder, the usage string, and the default, roff
escaped: `-` as `\-`, `\` as `\e`, a leading `.` or `'` behind `\&`) and leaves
every other byte as it was, refusing a page with a pair missing, doubled, or
out of order. `make
generate` rewrites it in place; `make generated-clean` and the bundle verifier
regenerate into a temporary file and compare. A Go test runs `groff -man -ww
-z` on the page and fails on a warning, skipping without groff; the debian
rules install it to `/usr/share/man/man8/`, gzipped by `dh_compress`. *Why:*
the flags are defined once and the page cannot drift from them, as the
registry's reference cannot; the prose is not in any definition. *Not taken:*
a `.8.in` template beside a generated `.8`; a Markdown-to-roff converter (a
vendored dependency, left to the HTML documents); generating the whole page.

**The helper's value names and flag order are defined once.** `internal/prune`
holds each flag's completion words, whether it takes a path, and, for a value
that is neither, its name (`days` `N`, `minfree` `PERCENT`); the placeholder is
the words joined with `|` and `|PATH`, or the name, none for a switch; and
`FlagOrder` is the synopsis's order. The synopsis (`prune.Usage()`, pinned to
the former constant), `-h` (`prune.PrintUsage`, double dash, the placeholder,
the usage string, an unquoted default; replacing Go's `PrintDefaults`), the man
page's OPTIONS, and Tab all read them; tests hold the order and the table to
the defined flags. *Why:* the synopsis said `auto|PATH`, `N`, and `PERCENT`
where `-h` said `string`, `int`, and `float` with one dash, and a page
generated from the flag set alone would have said the latter. *Not taken:* Go's
backquoted value names in the usage strings; parsing the hand-written
synopsis; alphabetical order.

**The page is the terminal reference; the guide stays the guide.**
`karvi-prune.8` states in full what an operator needs at the shell: NAME,
SYNOPSIS, DESCRIPTION (what goes and never goes, no configuration read, the
ownership rule, a failure as a line), OPTIONS, WHAT GOES (the six kinds and the
pressure floor), WHERE IT LOOKS (as an operator and as root), OUTPUT (the event
words, the kept reasons, the summary, jsonl), EXIT STATUS, FILES, EXAMPLES, and
SEE ALSO; [`docs/PRUNE.md`](PRUNE.md) keeps the same facts with the reasons and
the schedules, and says the page restates the rules, the report, and the exits,
so a change to the helper changes both. No test holds the page to the helper's
words, which are literals with no list. *Why:* an installed host has the page
and not the guide; GitHub renders the guide and not the page. *Not taken:* the
page pointing to the guide for the rules; the guide pointing to the page; a list
of the helper's words for a test.

**A man page names no version and no date.** The header is `.TH KARVI-PRUNE 8 ""
"karvi"` (and `.TH KARVI 1 "" "karvi"`, `.TH KARVI-ASKPASS 1 "" "karvi"` after
it): the date empty, the source `karvi`, groff supplying the manual's name; no
hand, generator, or install step writes either, and the installed package and
`karvi version` identify the release. *Why:* a version in a living page is
wrong from the day after its release until it is edited. *Not taken:* the
version and date edited at each release; a build step stamping them (no
release date exists in the packaging until it has a `debian/changelog`).

**karvi's manual is a page per help text.** The parser table's help texts are
the page set: `karvi.1` from the top help (the command words, the global
options, the abbreviation rule) and `karvi-WORD.1` from each command word's
help (`karvi-login.1`, `karvi-command.1`, `karvi-run.1`, `karvi-crun.1`,
`karvi-stream.1`, `karvi-daemon.1`, `karvi-job.1`, `karvi-config.1`,
`karvi-setup.1`, `karvi-watch.1`, `karvi-version.1`), all in section 1, each the
terminal reference for its word; an alias has no page. `man karvi WORD` reaches
a word's page through man-db's joining of the words, as `man karvi-WORD` does.
*Why:* each help text is one page, so a page is the unit the help already has
and a new word without a page is found; one page would print the target-input,
platform, and transport blocks that `login`, `command`, `run`, and `crun` share
once per word, or need a structure the help texts do not have. *Not taken:* one
page for every word; pages for the large words alone with the small ones in
`karvi.1`; `karvi-setup` in section 8 (it is a word of `/usr/bin/karvi`, where
`karvi-prune` is a program of its own run by root's timer); a page per alias.

**A word's page generates its SYNOPSIS and DESCRIPTION from its help text.**
Each of karvi's pages has two generated regions, both from the one help text
its word prints: the SYNOPSIS, the Usage lines one invocation to a line with
their action words in bold (the words the terminal layout colours), the rest as
written; and the DESCRIPTION, the help's body after the Usage block laid out by
the help's own line classes (`internal/helplayout`: a heading as a subsection,
an entry as a tagged paragraph with its option words in bold and its
description refilled, a prose run as a paragraph), the words byte for byte and
the line breaks groff's. The top help's title line is NAME's, written by hand,
as is everything outside the two regions. Build-dependent help text (`run`'s
native adapter status) is the page's as it is the help's, regenerated with it.
*Why:* six of the twelve help texts (config, daemon, job, setup, version,
watch) have no option entries and carry their options in the Usage lines and
the prose, and the others hold rules in prose between their entries (the GLOB
syntax, the `\r` rule), so the help is the unit, not its entries; one
classifier serves the terminal and the page, so they cannot read a line
differently. *Not taken:* an OPTIONS region from the entries alone (empty on
six pages, the prose rules lost); generating the whole page; a hand-written
DESCRIPTION with OPTIONS generated on the pages with entries alone.

**The generator reaches the help through one accessor and one renderer.**
`internal/cli` exports `HelpPages()`, the parser table's help texts in its order
as `{Word, Text}`: the top help first (Word empty), then each visible top-level
word with its `help()` (a subcommand shares its word's text), the constants
staying unexported, so the table is the page set. `internal/helplayout` renders
a help text to roff (`Roff`: the SYNOPSIS and DESCRIPTION bodies) with the
classifier its terminal layout uses, and holds the one roff escape (`\` as `\e`,
`-` as `\-`, a leading `.` or `'` behind `\&`), which the prune regions use too.
`tools/mangen` works from a page table, each page with its path, its marker
source, and its regions: `karvi-prune.8` from `internal/prune` as before, and
`karvi.1` and `karvi-WORD.1` from `HelpPages()` with the markers `.\" BEGIN
GENERATED SYNOPSIS: tools/mangen from internal/cli; edit there` and
`DESCRIPTION`; a word without its page file is refused, naming the file; `-dir
DIR` writes every generated page into DIR for `make generated-clean` and the
bundle verifier to compare, and without it the pages are rewritten in place.
`karvi-askpass.1` is outside the table. *Why:* the help texts and the classifier
are unexported, and a new word becomes a new page with no list to keep by hand.
*Not taken:* exporting the twelve constants (a list in the generator kept by
hand); the generator as a test inside `internal/cli` (a test that changes the
tree); reading a built executable's `--help` (a build first, and the terminal
layout and the configuration's colour between the text and the page); prune's
page rebuilt from its help text (a page already agreed).

**`karvi.1` states what the words share; a word's page adds only what its help
does not state.** `karvi.1` writes by hand, around its generated regions, an
opening paragraph and CONFIGURATION (the global files `/etc/karvi/config.toml`
then `/opt/karvi/config.toml`, the only place locks are declared; `--config`,
`KARVI__SECTION__KEY`, and `--set`; `config show --explain`), VALUES (DURATION:
a number and its unit, `h`, `m`, `s`, `ms`, `us`, `ns`, such as `500ms`, `45s`,
or `5m`, the parts combinable, `1h30m`, a bare number or `d` refused, each key's
range applying; N: a whole number whose 0 each option defines), ENVIRONMENT,
FILES, EXIT STATUS, and SEE ALSO. The top help gains one sentence: an option
that names a configuration key sets it through the lock-aware layer, and a lock
refuses it; the key-backed entries name their key in parentheses. Each word's
page has NAME, its two generated regions, the hand sections whose facts its help
does not state, and SEE ALSO naming `karvi(1)`, to which it refers for the exit
statuses; it restates the facts of its guide's section, and that section says
so, as [`docs/PRUNE.md`](PRUNE.md) does. `karvi-run.1` has a DISPATCH section:
the modes, the auto start width `min(64, max(16, 4*CPUs))` and ceiling `min(256,
max(32, 8*CPUs))` with their values on 4, 8, and 32 logical CPUs (16 to 32, 32
to 64, 64 to 256), the ceiling's formula being the host's cap
`dispatch.server-max-inflight` at 0 (one wave job at its ceiling can fill the
cap alone; workers past it wait for a lease), the halts as built and the gates,
and the delay; `karvi-crun.1` refers to it. The per-word sections, drafted
against the build with their claims executed: run DISPATCH, OUTPUT, REHEARSAL,
EXAMPLES; command OUTPUT, EXAMPLES; crun COLLECTION, EXAMPLES; login
TRANSCRIPTS, HOST KEYS; stream and job EXAMPLES; daemon, config, setup, and
watch FILES; version nothing more. *Why:* six options in four words take a
duration through one parser rule; the configuration's files, layers, locks,
environment, and exit statuses are the same for every word and stated in no
help; an installed host has the pages and not the guides. *Not taken:* the
shared sections on every word's page; the duration's form in each DURATION
entry; tables of widths in the help; the word pages pointing to the guides;
pages for the guides' other material (the spool, the shared trees in full, the
error codes).

**Each exit status is defined once, with its meaning.** `internal/exitcode`
holds the statuses as one ordered list of code, name, and a one-line meaning;
`ExitName` reads it, and a test holds every constant to exactly one entry.
`karvi.1`'s EXIT STATUS is a third generated region, one entry per status (the
number, the name the records and the audit carry, the meaning), with the choice
of exit written by hand above it: configuration loading exits 2, or 3 for a
lock, whatever the cause; a `run` exits 101 when any device failed or did not
start, even every device; another word takes its first failure's status (107 to
110); and when more than one applies, 111, then 106, 113, 114, 102, 103,
104, 105. `tools/errorcodegen` writes the same list as the "Exit statuses"
section of [`docs/ERROR-CODES.md`](ERROR-CODES.md), before the code tables.
*Why:* the 24 statuses had names and no meaning anywhere, while the error
registry said which code sets which exit, so an operator holding an exit had no
page to read it in. *Not taken:* the meanings written by hand in the page; the
precedence generated (it is `determineExit`'s code, and the paragraph is checked
by execution); renaming `ExitPartialFailure` for a run in which every device
failed (the names are in the records; the meaning says one or more).

**A helper without flags has a page written by hand alone.**
`packaging/man/karvi-askpass.1` (`.TH KARVI-ASKPASS 1 "" "karvi"`, `.nh`, `AD
l`) states what the helper is, that `ssh` runs it and an operator does not, how
karvi finds it, its environment, its security, its exits, and its socket; no
region is generated, and `tools/mangen` does not read it. The groff lint covers
every page in `packaging/man/`, and the debian rules install it to
`/usr/share/man/man1/`. The executable is unchanged: started without its
environment it prints nothing and exits 2, as OpenSSH needs of a helper. *Why:*
an administrator who meets the executable learns from the page what it is. *Not
taken:* a `--help` on the helper.

**The documentation cross-references by relative links.** The documentation is
every Markdown file the tree tracks outside `vendor/` (the root's, `docs/`,
`examples/README.md`, and `release/`). A document named in another's prose is
a relative link, its code span the link's text
(`` [`docs/OPERATIONS.md`](OPERATIONS.md) `` from `docs/`); a name with a
section (a quoted heading, "section N", "§N") links to that heading, the
qualifier inside the link; a numbered section or chapter of a document links
to its heading. The anchor is the heading's id as GitHub derives it. Left as
text: names in fenced code, a document naming itself without a section, a
manual page's section, and what the tree does not hold (the archived
specification and records). *Why:* a reader follows a reference where it is
made, on GitHub and in the HTML alike, and one form serves both. *Not taken:*
links added by the converter alone (GitHub's reader would not have them);
every key and error code linked to its entry (a wider pass, not made).

**The documents' prose wraps at 80 columns.** A paragraph or list item is
wrapped to 80, its list indentation kept, a link's target never broken, and no
line begun with a word Markdown would read as a block's opening. Code, tables,
HTML, and headings are not wrapped, and a link longer than the line stands on
its own. ERROR-CODES' prose is wrapped in its generator. *Why:* the documents
are read in a terminal and an editor as well as rendered, and a rewrap must
leave the rendering as it was. *Not taken:* a gate on the width.

**The documentation as HTML is a build product beside the tree.**
`tools/md-to-html` (`make html`) converts every Markdown file of the tree with
goldmark, vendored for the tool alone, in GitHub's dialect; no executable
contains it. Its table names each document once with its group and a line, and
it refuses a tree whose Markdown files are not the table's. The output is the
tree's parent's `html/` (`HTMLDIR`), never the tree: a page per document at the
tree's path with `.html`, `index.html` (the README's image and opening
paragraph, then the documents by group), `images/` as the tree's, and one
stylesheet. *Why:*
the documents read in a browser on any host, and the package can install them.
*Not taken:* pandoc (not a Go build dependency); a converter written here; the
HTML committed; the README as the index.

**Headings take GitHub's ids, from their rendered text.** The converter sets
each heading's id itself: the text as it reads, lowercased, every character but
a letter, digit, mark, hyphen, underscore, or space removed, each space a
hyphen, a repeat numbered as GitHub numbers it. A test pins the rule to the ids
GitHub's API gave a table of awkward headings. *Why:* a link written for GitHub
reaches the same heading in the HTML; goldmark's own ids, made from the source
line, turn an underscore into a hyphen.

**The site is relative and needs nothing outside itself.** Every link and image
between its files is a relative path, a document's link names its page (`.md`
made `.html`), the tree's image is a file of the site, and no document page
loads a script, a font, or anything from the network. Every directory below the
root holds an `index.html` that sends the browser to the parent's `index.html`
(a refresh and one line of script, both relative) over a blank body marked
`noindex`. The parent's page and not the bare directory, so a copy opened from
the disk reaches a page too. The left column holds the groups and every
document, the current page's sections under it; a narrow screen folds the same
links into "Documents" above the page. *Why:* the directory can be copied
anywhere and read offline, and a server shows a reader of a directory a page,
never a listing or an error. *Not taken:* syntax highlighting (a second module);
a stylesheet in every page; a search box.

**The site is replaced whole, and only when it is the tool's.** The tool writes
the site beside `html/`, checks it there, and renames it into place, the old
one renamed aside and removed; `html/` is replaced when it is missing, empty, or
carries the tool's `.md-to-html` file, and any other directory, a symbolic
link, or a failed build or check leaves it as it was. *Why:* `html/` is always
one whole build, and a directory the tool did not make is never removed; what
it removes is made again by `make html`.

**Every link of the site resolves, or nothing is published.** Before the swap
the tool reads every page it wrote, the index and the column included: a
relative link must name a file of the site and its anchor an id on that page;
an absolute path or another scheme is refused; http, https, and mailto are not
fetched; an id given twice is reported. The same conversion runs in `go test`,
so a document edit that breaks a link fails the tests. *Why:* a reader never
meets a dead link, and the tool's own rewriting is checked with the documents.
*Not taken:* a check of the Markdown alone; fetching external links.

**The licence is one document, `LICENSE.md`.** The MIT text is the tree's
`LICENSE.md`, its first line the heading `# MIT License` that titles its page,
the rest the text unchanged; README's License section links to it and `NOTICE`
names it. *Why:* the site carries the licence like any document, GitHub reads
`LICENSE.md` as the repository's licence, and one copy cannot drift from
another. *Not taken:* a `LICENSE.md` beside `LICENSE`; a converter exception
for a file without a heading.

## 17. What this document is not

It is not the operator's how ([`docs/OPERATIONS.md`](OPERATIONS.md) and the
guides), not the structure ([`docs/ARCHITECTURE.md`](ARCHITECTURE.md)), and not
the record of what changed when ([`CHANGELOG.md`](../CHANGELOG.md)). The
requirement identifiers, decision numbers, and worked sessions behind these
entries are in the operator's private archive; a reader who needs a rule's
evidence finds it in the code and its tests, which are the reference.
