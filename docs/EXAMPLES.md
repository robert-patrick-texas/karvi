# karvi worked examples

Each chapter records one design session as it was executed against the tree:
what it gains, the rule that was settled, the executed example, what was not
taken, and what it leaves for later. A decision the session settles is also an
entry in [`docs/DESIGN.md`](DESIGN.md), which states the settled decisions of
the whole program. The chapters written before the tree was prepared for public
release are kept, with the decision records they cite, in the operator's private
archive; this file starts where that preparation stands.

## 1. The public-repository preparation (2026-09-30)

The tree was built as a private effort from a specification and a series of
decision records, with a worked-examples document that recorded every design
session and a hand-off for every session of work. To publish it, the operator
chose to keep that past whole in a private archive and to carry forward only
what a reader of the code needs: the program, its tests, the operator guides,
and one document that says why karvi does what it does.

**What it gains.** A public reader meets a tree that stands on its own. No
document points at a specification that is not there, no comment cites a
decision record a reader cannot open, and no hand-off written for the next
session of a private effort sits under `docs/`. The history that was the
effort's own is kept, privately and whole, with a bare mirror of the
repository as it stood before each step, so nothing is lost and nothing
irrelevant is published.

**The steps, in order.**

1. **The cumulative patch stream retired.** Every release had shipped a
   source patch beside its bundle, qualified by re-applying it to the
   previous release's bundle. No site built from a patch, every release was
   installed as new, and between two public tags the diff is one `git diff`;
   the release tools ship the bundle, its checksum, and an aggregate
   checksum file, and refuse a release tree that holds anything git does not
   track (the one thing the patch's check had proved).
2. **The specification frozen and archived.** The specification, its earlier
   revisions, the removed-content log, the traceability and status documents
   that mapped the tree to it, and the tool that checked them left the tree;
   the code and its tests are the reference for what karvi does.
3. **The hand-offs archived.** Every session hand-off and the start prompt,
   written for one operator and one assistant.
4. **The design distilled.** [`docs/DESIGN.md`](DESIGN.md): the settled
   decisions in present tense, each as the rule, the reason, and the
   alternatives not taken, organised by area, drawn from the seventeen decision
   records and the worked-examples chapters through parallel extraction of the
   settled rules and a check of the doubtful ones against the code.
5. **The citations swept.** Every pointer at the archived documents, the
   specification's identifiers, the records' sections, the release gates,
   the session dates, and the hand-offs removed from the living tree, about
   1,200 sites in 392 files, facts kept, release identifiers kept, code and
   every compared string unchanged; verified by the full battery on a lab
   build.
6. **The records archived.** The seventeen decision records, the
   worked-examples document of thirty-four chapters, and the release-gate
   list left the tree, their archive copies checked byte for byte first;
   this file begins.

**The rules from here.** Archive first, then remove: a bare mirror of the
repository is refreshed after every commit, and a file about to leave the tree
is copied into the archive before its removal. A removal is recorded in the
chapter that makes it, with the commit before, and the archive's git history
holds the bytes; there is no running removal log in the tree. The specification
is frozen: no revision follows it, no requirement number is minted, and a design
decision is an entry in [`docs/DESIGN.md`](DESIGN.md) with its worked example
here. Verification is proportionate to what a change touches: grep checks for
documents, gofmt and vet and build for comments, `go test ./...` for code, and
the full battery of tests and suites at the boundary of a body of work or for a
change of behaviour.

**Executed.** The check that the archive holds what leaves, then the
removal, then the pointer sweep over what remains:

```text
$ for f in docs/ADR-*.md; do cmp -s "$f" "$ARCHIVE/docs-decisions/$(basename $f)" || echo "DIFFERS $f"; done
$ cmp docs/EXAMPLES.md "$ARCHIVE/EXAMPLES.md" && cmp docs/NEXT-RELEASE-GATES.md "$ARCHIVE/NEXT-RELEASE-GATES.md" && echo identical
identical
$ git rm -q docs/ADR-*.md docs/EXAMPLES.md docs/NEXT-RELEASE-GATES.md
$ git grep -cE 'EXAMPLES|ADR-0[0-9]{3}|NEXT-RELEASE-GATES' -- . ':!vendor' ':!CHANGELOG.md' ':!NEXT-RELEASE-NOTES.md' ':!release/' ':!BUILD-RESULT.md' ':!release-manifest.json' ':!docs/EXAMPLES.md' | wc -l
0
```

**Not taken.** Carrying the decision records frozen under `docs/` (14,000
lines of past pointing into an archive the reader cannot open). Carrying the
old worked-examples document (its chapters cite the records, the
specification, and commit hashes that a fresh history will not hold). A
public hand-off convention. Rewriting the git history in place (the archive
holds it; the public tree starts fresh at the next step).

**Roadmap.** The resets: the changelog from the current release with one
paragraph for what came before; the release notes replaced by a roadmap of
the open items; CONTRIBUTING for an outside contributor; NOTICE with the
vendored licences; SECURITY with a reporting section; the module path and
the package maintainer; the personal paths and names in what remains. Then
the fresh history and the first public release.

## 2. The resets: the changelog and the roadmap (2026-09-30)

The two documents that carried the effort's history as living text were
reset: the changelog to the current release, and the release notes to a
roadmap of what is open.

**What it gains.** A public reader of [CHANGELOG.md](../CHANGELOG.md) finds what
the release they install contains and what the tree in front of them changes,
not twenty-eight releases of a private line described against a specification
they cannot open; a reader of [ROADMAP.md](../ROADMAP.md) finds what is not
built yet in the order it will be taken up, not 2,400 lines of items, most of
them done, numbered by a session's bookkeeping.

**The changelog.** The top block is "Unreleased" and says what the
preparation changed in the tree (the patch stream retired, the specification
and records archived, the pointers swept, three messages reworded, the
retired codes' era labels) and that no executable's behaviour changed. The
0.23.0 block is kept as the release shipped it, with its citations removed
and its sentences about the patch dropped, since the patch it announced is
retired. One closing section says that releases 0.1.0 to 0.22.0 were made
before the tree was prepared for public release, that their changelog is in
the operator's private archive, and that every release is installed as new.
The archive holds the changelog as it was.

**The roadmap.** [ROADMAP.md](../ROADMAP.md) replaces the release notes. Its
["Next"](../ROADMAP.md#next) list is the operator's order: the man page, the
documentation as HTML, the package's contents, build numbers. Its
["Later"](../ROADMAP.md#later) list is every roadmap item of the notes that is
still open, stated as a design question with the relationships the notes had
recorded: macro files, job files, the credential items, the digest review, a
no-algorithm-lists setting, sealed packages, crash recovery, a follow from the
live edge, filter field prefixes. The device-qualification track keeps its four
steps. Items the notes still listed as roadmap but which were built since (the
configuration review, the shorter job ID, the spool and memory budget) are not
carried. The release rules that were the notes' preamble are either
[DESIGN.md](DESIGN.md)'s (the release's artifacts, the clean-tree gate, breaking
changes accepted) or the project's process (commit on the operator's word, `dev`
and `main`), which CONTRIBUTING takes up at its reset.

**Found on the way.** Two bullets of OPERATIONS still said a run through the
daemon writes `commands.jsonl` and `summary.json` whatever their keys say,
and that the daemon's configuration decides a daemon job's files. Both ended
when records began to travel over the socket and the plan began to carry the
invocation's switches and root; the bullets now state the rule as built, and
point at the not-kept notice and the orphan rule.

**Executed.** Class: documentation, so the pointer sweep:

```text
$ git grep -n 'NEXT-RELEASE-NOTES' -- . ':!vendor' ':!release/' ':!BUILD-RESULT.md' ':!release-manifest.json' ':!docs/EXAMPLES.md' | wc -l
0
$ grep -c '^## ' CHANGELOG.md ROADMAP.md docs/EXAMPLES.md
CHANGELOG.md:3
ROADMAP.md:3
docs/EXAMPLES.md:2
```

**Not taken.** Keeping the whole changelog with its citations swept (the
sweep would have been the work of the whole line for releases nobody
outside can install). Carrying the notes' done items as history (the
changelog and the archive hold them). A roadmap with dates or numbers (the
order is the commitment).

**Roadmap.** CONTRIBUTING for an outside contributor; NOTICE with the six
vendored licences; SECURITY with a reporting section; the module path and
the package maintainer; the personal paths in what remains; the fresh
history.

## 3. The resets: the module path, the package metadata, NOTICE, SECURITY, and CONTRIBUTING (2026-09-30)

The tree's identity for a public host, and the four documents a public
reader opens first, brought to the state the publication needs.

**What it gains.** `go install` and `go get` resolve the module path against
the repository that will exist; the debian control names a maintainer who
answers and the page the package comes from; NOTICE says what third-party
code ships in the tree and under which licence, where it had said there was
none; SECURITY says how to report a vulnerability, which is what a host reads
a file of that name for; CONTRIBUTING tells an outside contributor how to
build, test, and change the program, where before it presumed the archived
process.

**The module path.** `github.com/robert-patrick-texas/karvi`, the operator's
account, in place of an organisation that does not exist: `go.mod`, the
Makefile's module variable that stamps the build identity, every import in
306 Go files, the debian control's homepage and the copyright file's source,
and the nine hand-written schemas' `$id` URLs, which now name a page under
that account (`https://robert-patrick-texas.github.io/karvi/schema/...`).
The change is a text substitution and nothing else: gofmt found no import
block to reorder, the generators produced the same files, and the tests
pass.

**The package metadata.** Maintainer `Robert Patrick <rob-karvi@rpatrick.com>`
in place of the placeholder; the homepage the GitHub page.

**NOTICE.** karvi under MIT; the five vendored modules with their licences
and licence paths (scrapligo and creack/pty under MIT, gotextfsm under
Apache 2.0, x/crypto and x/sys under BSD 3-Clause); yaml.v3 named as a
requirement of the module graph that is not vendored, since no karvi package
imports it; the OpenSSH sentence kept. The file had said the bundle has no
third-party dependencies, a sentence from before the native transport was
vendored.

**SECURITY.** A first section, "Reporting a vulnerability": privately by
mail, what to include, what not to include, and what happens; the security
model follows under its existing headings.

**CONTRIBUTING.** Rewritten for an outside contributor: what karvi is and
where the why and the how are; the principles a change must keep and the
four things never added without a recorded decision; building and testing
(the make targets, the suites under `KARVI`, the two verifiers, verification
by class); writing code (documentation, tests, the design entry and the
chapter, the error-code and configuration registries, the script rules);
branches and releases (`dev` and `main`, the release's artifacts, breaking
changes accepted before 1.0, the roadmap).

**Executed.** Class: code (the import paths and the generators' inputs), so
gofmt, build, vet, generate and generated-clean, and `go test ./...`:

```text
$ gofmt -l $(git ls-files '*.go' | grep -v '^vendor/') | wc -l
0
$ go build ./... && go vet ./... && make generate && make generated-clean && echo ok
ok
$ go test ./... | grep -v '^ok'; echo exit $?          # 06:39:41 to 06:40:36 UTC
exit 0
$ git grep -l 'karvi-project' -- . ':!vendor' ':!release/' | wc -l; git grep -l 'maintainers@example.invalid' -- . ':!vendor' ':!release/' ':!docs/EXAMPLES.md' | wc -l
0
0
```

**Not taken.** An organisation on GitHub for the module path (a second
account to keep for one repository). Renaming the schemas' `$id` files to
match today's counters (`daemon-ipc-v7.json` names schema 10, `scoreboard-v2`
schema 3; the identifiers are stable names, and a rename is a contract
change to make on its own). A vulnerability-disclosure timeline in SECURITY
(a promise the project cannot yet keep; the section says what happens).

**Roadmap.** The personal paths and names in what remains; the fresh
history; the first public release.

## 4. The personal data and the release records (2026-09-30)

The last sweep before the fresh history: what in the living tree named the
operator, his host, or his home, and the records of the release made from
the private history.

**What it gains.** A public tree names no person but the maintainer, no host
but example hosts, and no path but example paths; and it carries no record of
a release that the public history did not make.

**What was found.** A survey for the operator's home directory, his name,
his account, and this host's name over the living tree found two things. The
watch screen's test fixtures used his surname as an operator name in the
sample scoreboard and this host's name as the producer, and two suites used
`svc.<surname>` as a device username; they now use `quinlan`, `ops.example`,
and `svc.quinlan`. The name was chosen to sort where the old one did among
the fixture's four operators (`lee`, `netops`, `quinlan`, `sanchez`) and to
be seven letters, so the frame assertions on column layout and the sort test
hold unchanged; the prompt test types the name letter by letter and its typed
prefixes moved with it. The word `netops` stays: it is the fixture operator
in many packages, the fake device's default user, and the shipped default of
`security.shared-group`, a team name by shape and the program's own default,
not a person.

**The release records.** `release/` (the build result, the release status, the
downloads index, the scrapligo evidence, the manifest, and fourteen evidence
logs), [`BUILD-RESULT.md`](../release/BUILD-RESULT.md), and
`release-manifest.json` described the v0.23.0 release as executed from the
private history, naming the lab directory, the operator's home, and the retired
patch. They are in the archive under `release-v0.23.0/` and out of the tree; the
next release writes them anew, and the release tool that writes the evidence now
creates its directory first. The two documents that pointed a reader at them
(the build guide's "review the release boundary" step, the qualification
document's replay sentence) read without them.

**Executed.** Class: code (test fixtures) and scripts (two suites' executed
username), so the package's tests and the two suites on a lab build:

```text
$ git grep -n 'patrick\|16k\.net' -- . ':!vendor' | grep -v 'robert-patrick-texas\|rob-karvi@rpatrick\|Robert Patrick <' | wc -l
0
$ go test ./internal/watchui/...
ok  	github.com/robert-patrick-texas/karvi/internal/watchui	0.078s
$ ./scripts/v070-smoke-test.sh | tail -1; ./scripts/v090-smoke-test.sh | tail -1
v0.7.0 command-session, display-color, and debug smoke: pass
v0.9.0 family-preference, border, and error-echo smoke: pass
```

**Found on the way.** A blind substitution of the surname also rewrote the
new module path inside the same test files' imports (`robert-patrick-texas`
holds the surname); the package failed to build until the imports were
restored. A name that is also a path component is substituted by whole word.

**Not taken.** Renaming `netops` (a team name, and the program's default
group). Keeping the release records with their paths scrubbed (records of a
release the public history did not make). A `release/` placeholder (git
holds no empty directory; the tool creates it).

**Roadmap.** The fresh history: an orphan root commit from this tree under
the maintainer's identity, `main` as the default branch with `dev` for work,
the tag at the first public release, and the archive's mirror refreshed a
last time from the old history before it is left behind.

## 5. The fresh history (2026-09-30)

The public repository begins here: one root commit holding the prepared tree,
and no history before it. The private line, 467 commits from the first
sketch to this tree, is kept whole in the operator's archive as a bare
mirror, refreshed a last time from the commit this tree was exported from.

**What it gains.** A public reader clones a history whose every commit is
one they can read: no commit message names a hand-off, a specification
revision, or a decision record; no hash cited anywhere in the tree points at
a commit the repository does not hold; the author identity is the
maintainer's public one. The archive keeps the way back to any earlier state,
privately, for the one person who may need it.

**The rule.** The tree is exported from the private line's last commit with
`git archive` (the tracked files, nothing else: the built executables under
`bin/` were never tracked and stay out, now by a `.gitignore` of one line),
into a directory named `karvi`, plainly, since a public repository's checkout
carries no version in its name. A new repository is initialised there with
`main` as its default branch, the maintainer's identity set locally, every
file added, and one commit made; `dev` is branched from it for work, and
`main` moves only at a release, as before. The release tools beside the tree
learn the new root once; nothing else about a release changes, and the first
release from this history takes the number 0.24.0 when its sequence assigns
it (the changelog's "Unreleased" block is that release's).

**What is not carried.** The old history's tags (`karvi-v0.9.1` to
`karvi-v0.23.0`) name commits the public repository does not have and are not
recreated; a release before 0.24.0 exists only as an archived bundle. The old
working tree stays where it was until the operator removes it; the release
tools point at the new one.

**Executed.**

```text
$ git -C karvi-v0.23.0 rev-list --count HEAD; git -C karvi-v0.23.0 ls-files | wc -l
467
727
$ git -C karvi-v0.23.0 archive HEAD | tar -x -C karvi && printf '/bin/\n' > karvi/.gitignore
$ cd karvi && git init -q -b main && git config user.name 'Robert Patrick' && git config user.email 'rob-karvi@rpatrick.com'
$ git add -A && git commit -q -m 'karvi: the public source tree' && git branch dev
$ git log --oneline | wc -l; git ls-files | wc -l; git status --short | wc -l
1
728
0
$ go build ./... && make generated-clean && echo ok
ok
```

**Not taken.** Rewriting the old history in place with a filter (every cited
hash would move and the past would still be there, only edited). Recreating
the old tags in the new repository (they would point at nothing). Carrying
`bin/` (a release's executables are built by the release, and a checkout's
`bin/` is the operator's own build). A version in the directory's name.

**Roadmap.** The repository on GitHub under the maintainer's account, `main`
protected, `dev` for work; the first public release, 0.24.0, by the release
sequence: the baseline on a clean clone, the number, the documents, the
evidence, the tag, the bundle as the release's asset. Then the roadmap's
first item, the man page.

## 6. The first public release, 0.24.0 (2026-09-30)

The release sequence run for the first time from the public history: the
fresh tree's first tag, and its bundle the GitHub release's asset.

**What it gains.** A site installs karvi from a published artifact whose
source, evidence, and executables it can verify against a public tag, and
the release records under `release/` describe a release the public history
made. The tree's `bin/` holds released executables again, so the suites'
default is the release and a lab build lives elsewhere.

**The sequence, as run**, twenty-two minutes ten seconds from the
baseline's start to the artifacts' end, every commit on the operator's
"proceed":

| Step | Wall (UTC) | Result |
|---|---|---|
| the baseline on a clean clone of `dev` at `7018f3f` | 07:33:53 to 07:39:43 | gofmt, make, the release verifier, exit 0 each; the tree after it as at v0.23.0 |
| the number in its eight places; the release-identity build | 07:40 | no removed-key row awaited the number; `bin/` was empty, so nothing was protected |
| the compatibility example | 07:41 | the released v0.23.0 executable, its bytes checked against the old tree's `CHECKSUMS.sha256`, started its daemon; this client read `compatible: false` on the version alone, its run was refused with `daemon_incompatible` (exit 112) before any job, the jobs tree stayed empty, the client's `daemon stop` ended it |
| 1/3 | 07:41 | `89b8abf` |
| the core evidence | 07:42 to 07:43:52 | 727 named tests across 75 packages, vet 0, both socket lengths 0 |
| the documents | 07:45 | `453d45b` (2/3) |
| the remaining evidence | 07:45:34 to 07:51:08 | the shipped checks exit 0, the release verifier exit 0, the checksums unchanged by its rebuild; the replay skipped, no release differing in daemon IPC schema |
| 3/3, the tag, `main` | 07:51 | `99607bd`, `karvi-v0.24.0`, `main` at the tag |
| the artifacts | 07:51:50 to 07:56:03 | the bundle reproducible byte for byte and verified from its own archive with its own verifier; the aggregate lists the bundle |
| the push and the GitHub release | 07:58 | `dev`, `main`, and the tag pushed; release `karvi-v0.24.0` with the bundle, its checksum, and the aggregate as assets; the asset downloaded back matches |

**What the release settled.** The records are `release/` alone: the build
result, the artifact index, the scrapligo evidence, the manifest, and
`evidence/`; no copies at the root, and no status document (the old one
was 689 lines of status against a gates document that is archived). The
manifest keeps the identity, the counters, the build, the statistics, the
executables, the upgrade recovery, and the qualification rows, and loses
the specification block, the record list, the pointers at the status and
gates documents, and the patch and baseline fields, since none of those
exist. The compatibility example's log is in `evidence/` as well as beside
the tree, because a public reader cannot open the directory beside the
tree; the baseline logs stay beside it, since the after-check names the
operator's home. The named-test count fell from 731 to 727 and the package
count from 76 to 75: the removed traceability tool's package. The stale
sentences settled at the documents step: a traceability check named in
the build guide and the qualification document, a `v1.4.1` grep, daemon
IPC 9 and registry 17 in two places, the build identity's date (2026-09-20
in three documents, 2026-09-23 in the fourth) to this release's, the bundle
name and install paths, the runbook's directory, and a patch mention in
BUILDING.

**Executed.** The example's turning points and the published asset:

```text
$ grep -E 'client_version|^compatible|^exit=112|^daemon_incompatible' release/evidence/ipc-schema-compat.log | cut -c1-96
client_version: 0.24.0
compatible: false
exit=112
daemon_incompatible: running daemon version 0.23.0 uses IPC schema 10; karvi 0.24.0 uses schema 10,
$ sha256sum -c karvi-v0.24.0-artifacts.sha256
karvi-v0.24.0-source-linux-amd64.tar.gz: OK
$ curl -sL https://github.com/robert-patrick-texas/karvi/releases/download/karvi-v0.24.0/karvi-v0.24.0-source-linux-amd64.tar.gz | sha256sum | cut -c1-16; cut -c1-16 karvi-v0.24.0-source-linux-amd64.tar.gz.sha256
3d2c17339b1fd830
3d2c17339b1fd830
```

**Found on the way.** The example script's first form built the client's
argument list with `eval`, which split the quoted command into two words
(`cli_command_text_mixed`) and left the credential variables unexported
(`credential_prompt_unavailable`); the script now uses two shell functions
and exports them. That failed run also showed that a run refused at
credential resolution leaves the job directory it reserved before the
draft, where the daemon-incompatible refusal leaves none; noted, not
changed. `gh release create --verify-tag` must run inside the repository.

**Not taken.** A status document. Root copies of the build result and the
manifest. The baseline logs in the tree. Recreating the old tags in the
public repository. A pre-release flag on the GitHub release. Protecting
`main`, which stays on the operator's word.

**Roadmap.** The man page, the roadmap's first item; `main` protected
when the operator chooses; the old tree removed when he chooses.

## 7. Stream mode reviewed: the draft's two parts, `--clear`, and line editing (2026-09-30)

A review of the stream reader after the release, on the operator's request,
and the fixes agreed one by one; then line editing at a terminal.

**What it gains.** A command typed the way `run` takes it (`--cmd show
clock`) is sent once, where it had been re-sent by every later job of the
session without a message; the reader refuses what it cannot serve in
every spelling; a stream that fails to read says so instead of ending as
if complete; and an operator at a terminal edits a line before sending it
and recalls the lines before.

**The review.** Four flaws executed and confirmed, in the order of their
weight: a `--cmd`, `--command`, or `--cf` line landed among the options,
which `--go` keeps, so every later job re-sent it; `--tf=-` and `--cf=-`
passed the check written for the spaced form and reached the run, which
then read the stream's own standard input; a read error or a line over the
scanner's buffer ended the stream silently with exit 0, the lines behind it
never read; and `--go` on an empty draft turned the parser's refusal into
the stream's exit though no job ran. Smaller: a Ctrl-C with lines already
read raced them, a command line kept its trailing blanks, a parse error at
`--go` named no line, the reader goroutine outlives the loop.

**The rules settled.** The draft has two parts, the targets and options
that stay and the commands that are the job's, and an option line's word
is resolved through run's own table, so the command option and its
aliases, the commands file, and the three declarations belong to the
commands, an abbreviation or an `=` spelling means what it means on a
command line, and an `=` value runs to the end of the line (the old reader
split `--command=show version` at the space). `--clear` empties the
commands and keeps the rest, what `--go` does without sending; `--reset`
empties everything. `--go` and `--sendit` with nothing to send print a
notice and run nothing, so the exit stays the last job's. `--cf`, `--tf`,
and `--tfr` may not name `-` in any spelling. A read failure or a line over
1 MiB ends the stream with `stream_input_read_failed` (exit 1) naming the
line: the operator asked whether a generic error with detail would serve;
the program's rule is one registered code per failure path with the detail
in the message, and a code costs one registry row, so the rule held.

**Line editing.** The operator asked whether an operator at a terminal
could edit a line, with Ctrl-A and Ctrl-E among the keys. Three answers
were weighed: the terminal's own cooked mode already gives backspace,
Ctrl-U, and Ctrl-W, and nothing more; `rlwrap` gives GNU readline around
the unchanged program, at no cost to the tree; `golang.org/x/term` gives
the key set named, the arrows, and per-session history, at the cost of a
vendored module. The operator chose x/term. The design: the terminal
reader is used when standard input is a terminal and the controlling
terminal opens for the echo, the scanner otherwise; the terminal is in raw
mode for one line's read alone, through the tree's existing raw-mode
helper, and back in its own mode for every message and every job, so a
job's display and its Ctrl-C are as they were; Ctrl-C and Ctrl-D at the
line end the stream as the input's end does. Vendoring x/term v0.23.0,
already in the module cache, brought `golang.org/x/sys` into `vendor/`
(its unix, windows, and plan9 packages, about 9 MB in the tree), and `go
mod tidy` corrected two things beside it: `golang.org/x/crypto` was a
direct import marked indirect, and `gopkg.in/yaml.v3` was a requirement
nothing needed; NOTICE and the build guide follow.

**Executed.** The two parts on a lab build, then the editor through a
pseudo-terminal, where `sion`, Ctrl-A, `show ver`, Ctrl-E, Enter must plan
the same job as the plain line, and two up arrows recall it:

```text
$ printf -- '--target 192.0.2.1\n--dry-run\n--cmd show clock\n--go\n--command=show version\n--go\nshow ip route\n--clear\n--go\n--end\n' | karvi --quiet stream 2>&1 | grep -E 'commands|stream line'
commands: 1 (command_plan_digest 96920875…a51a2e6)
commands: 1 (command_plan_digest af8efe9a…a20989e)
stream line 9: nothing to send
$ printf -- '--tf=-\n--cf -\n--tfr -\n--cf=-\n' | karvi stream
stream line 1: standard input is the stream; --tf - is not accepted
stream line 2: standard input is the stream; --cf - is not accepted
stream line 3: standard input is the stream; --tfr - is not accepted
stream line 4: standard input is the stream; --cf - is not accepted
$ (printf -- '--target 192.0.2.1\n'; head -c 1100000 /dev/zero | tr '\0' 'x'; printf '\nshow clock\n--go\n') | karvi stream; echo "exit=$?"
stream_input_read_failed: stream line 2 is longer than the 1048576-byte limit
exit=1
$ printf -- '--target 192.0.2.1\n--dry-run\nsion\x01show ver\x05\n--go\n\x1b[A\x1b[A\n--go\n--end\n' | script -qfc "karvi stream" /dev/null | grep -E 'commands:|stream line'
commands: 1 (command_plan_digest af8efe9a…a20989e)
commands: 1 (command_plan_digest af8efe9a…a20989e)
$ printf -- '--target 192.0.2.1\n--dry-run\nshow clock\n\\r\nshow version\n--go\n--end\n' | karvi --quiet stream 2>&1 | grep commands
commands: 3 (command_plan_digest b8f7b353…571aa45)
$ printf -- '--target 192.0.2.1\n--dry-run\nshow clock\n\\r\nshow version\n--go\n--end\n' | script -qfc "karvi --quiet stream" /dev/null | grep commands
commands: 3 (command_plan_digest b8f7b353…571aa45)
```

The last two lines answer the operator's question: a line of `\r` is two
printable characters that stay in the command text and send a blank line
as before, by pipe or by editor, while the translation touches one control
byte, the Enter, which the editor consumes; three commands and one digest
by both paths.

**Found on the way.** The first pseudo-terminal run merged every line into
one and waited: script(1) fed the whole input while the terminal was still
in its own mode, which turns Enter into `\n`, and x/term's editor takes
`\r` alone as Enter. An operator meets the same thing by typing ahead
during a job. A reader in front of the editor now translates `\n` to `\r`,
and the test feeds one line each way. The first recall example read the
last line, `--go`, as history should; the record uses two up arrows.

**Not taken.** A prompt (one constant, when wanted). Raw mode held across
a job (its Ctrl-C would become a byte to read, as the watch screen reads
it). GNU readline (not reachable without cgo; `rlwrap` stays an operator's
choice). A generic error for the read failure. Echo on standard output (a
redirected `karvi stream > out` would hide what is typed).

**Roadmap.** The man page, the roadmap's first item; `main` protected and
the old tree removed when the operator chooses.

## 8. The release 0.25.0, and the old tree removed (2026-09-30)

The second release of the day, on the operator's word, carrying [chapter
7](#7-stream-mode-reviewed-the-drafts-two-parts---clear-and-line-editing-2026-09-30)'s
stream work; and the private line's working tree removed from the host, its
archive checked first.

**What it gains.** A site installs the stream fixes and the line editor
from a published artifact the same day they were made, and the host
carries one working tree, the public one, with the private line reachable
only through its archive.

**The old tree.** Before its removal the rule was applied: the archive's
bare mirror holds the same twenty-eight refs at the same objects as the
tree (`for-each-ref` on both, diffed empty), the tree was clean at
`3f9b608`, and its only untracked content was `bin/`, the released v0.23.0
executables that the v0.23.0 bundle beside the tree holds (its checksum
verified, eight `bin/` entries listed). Then `rm -rf`. The compatibility
example no longer needs it: the released previous executable is the
public tree's own `bin/`, checked against `CHECKSUMS.sha256` and copied
out before the rebuild.

**The sequence, as run**, nineteen minutes twenty-two seconds from the
baseline's start to the artifacts' end:

| Step | Wall (UTC) | Result |
|---|---|---|
| the baseline on a clean clone of `dev` at `dcc7a61` | 09:44:00 to 09:49:52 | gofmt, make, the release verifier, exit 0 each |
| the number in its eight places; the release-identity build | 09:51 | the changelog's Unreleased block became the release's, under a lead paragraph |
| the compatibility example | 09:51 | the released v0.24.0 executable from `bin/` started its daemon; this client read `compatible: false` on the version alone, its run was refused with `daemon_incompatible` (exit 112) before any job, the client's `daemon stop` ended it |
| 1/3 | 09:51 | `efa04c6` |
| the core evidence | 09:51 to 09:52:27 | 730 named tests across 75 packages, vet 0, both socket lengths 0 |
| the documents | 09:52 | `4af15ff` (2/3): `release/` from the v0.24.0 pattern by a generator reading the counts from the core log; the manifest lists the vendored modules |
| the remaining evidence | 09:52:56 to 09:58:26 | the shipped checks exit 0, the release verifier exit 0, the checksums unchanged; the replay skipped |
| 3/3, the tag, `main` | 09:59 | `74df534`, `karvi-v0.25.0`, `main` at the tag |
| the artifacts | 09:59:03 to 10:03:22 | the bundle reproducible and verified from its own archive; 12,655,199 bytes, 1.3 MB more than v0.24.0's for the vendored x/term and x/sys |
| the push and the GitHub release | 10:04 | `dev`, `main`, and the tag pushed; release `karvi-v0.25.0` with the three assets, marked latest; the asset downloaded back matches |

**Executed.** The archive check and the published state:

```text
$ diff <(git --git-dir=$A/karvi-v0.23.0.git for-each-ref --format='%(refname) %(objectname)' | sed 's#refs/heads/##') <(git -C $OLD for-each-ref --format='%(refname) %(objectname)' | sed 's#refs/heads/##') && echo identical
identical
$ git -C $OLD status --short --ignored
!! bin/
$ gh api repos/robert-patrick-texas/karvi/releases/latest --jq '"latest: \(.tag_name) draft=\(.draft) prerelease=\(.prerelease)"'
latest: karvi-v0.25.0 draft=false prerelease=false
$ curl -sL .../karvi-v0.25.0-source-linux-amd64.tar.gz | sha256sum | cut -c1-16; cut -c1-16 karvi-v0.25.0-source-linux-amd64.tar.gz.sha256
702cee0bc0023080
702cee0bc0023080
```

**Found on the way.** Two releases in one UTC day share a build time
(`2026-09-30T00:00:00Z`); the build identity still differs by its commit
string, and the checksums differ. `gh release view` has no `isLatest`
field; the latest release is read from the releases API.

**Not taken.** A patch number for a release whose only behaviour change
is stream mode's (the version line's rule is minor for a behaviour
change). Keeping the old tree for the compatibility example (the public
tree's `bin/` serves it).

**Roadmap.** The man page, the roadmap's first item; `main` protected when
the operator chooses.

## 9. The scratch root shared: the capacity ledger, the root's creation, and `setup shared` (2026-10-03)

The first of five objectives the operator set after the release 0.25.0
(the credential prompt's line editing, the `--record` footer, `--cd` for
`run` and `command`, and `--fs` follow): scratch directories under
`/dev/shm/karvi` made with the group and modes that let every member of
the operators' group work there, and a helper that makes them again at
every boot.

**What it gains.** On a host the site prepared, every operator's
scoreboards reach the one watch screen and every operator's sessions count
against the one host-wide cap, where the second operator's devices had
failed, or had fallen silently to private places; a reboot keeps the
layout; and on a host the site did not prepare, an operator's run no
longer claims the scratch root for itself.

**The review.** What existed: `setup shared` made the trees under
`/opt/karvi` and nothing on `/dev/shm`; `packaging/tmpfiles.d/karvi.conf`
existed, named by no document, installed by nothing, its group fixed and
its `capacity` line carrying the sticky bit. Executed with the lab's
second account `netops.test` in the operators' group, three faults
showed, then two more under the first fixes:

1. the first operator's run created `/dev/shm/karvi` at 0700 (the scratch
   chain and the control path made the missing parent with the leaf);
2. the capacity ledger made its lock and ledger files 0600 and `devices`
   0700 whatever its root, so under a root made right (2770 in the group)
   the second operator's every device failed with
   `capacity_admission_failed: open …/server.lock: permission denied`;
3. the tmpfiles line's sticky bit on `capacity` would refuse a member's
   rename over a ledger another wrote;
4. the ledger's reaper read the signal check's EPERM, the answer for
   another user's live process, as death, so each operator removed every
   other's leases and filled the cap alone;
5. a ledger the operator could not read was taken as empty, and the write
   after it would have replaced another operator's leases.

**The rules settled.** The ledger's directories and files take its root's
group modes under a setgid root (2770 gives 0660), the operator's own
files left private being widened at their next use; another user's
process is alive; an unreadable ledger is an error; a shared root present
but closed to the operator is said once (`capacity_root_unusable`) and the
private root taken. The operator agreed that a missing shared directory
means the private place without a warning and one that exists but cannot
be used means the private place with one. The rule was then refined before
it was recorded, since the suites point `watch.directory` and the capacity
root at folders of their work directories that nothing makes beforehand:
an operator's process never creates a missing parent, and makes a
directory only inside a parent that exists, so it never creates the
scratch root, makes its own `<username>` folder in it, and makes a
missing `scoreboards` or `capacity` folder with a setgid parent's bits.
`setup shared` makes the root and its two folders and writes the tmpfiles
rule from the same list of places, in the group it is given.

**The sections, as committed.** A (`a14bae0`) the ledger; B (`fd5acf3`)
no operator creates the scratch root; C (`db157e1`) setup and the boot
rule; D the documents and this chapter.

**Executed.** The ledger, operator A as `netops` and B as `netops.test`
sharing one 2770 root with a host cap of one session (A holds `show
slow`, four seconds; B starts a second later):

```text
released v0.25.0:  B  karvi: target=fake status=connection_error error=capacity_admission_failed: open cap/server.lock: permission denied
                      ! exit=101 elapsed=51ms
lab, A's 0600 files:  B  warning: shared capacity root unavailable (capacity_root_unusable: cap/devices: permission denied; a shared capacity root needs mode 2770 in the operators' group …); using private fallback t/b/state/capacity
lab, together:        B  ! exit=0 elapsed=4.31s   (B done +5.37s, A done +4.72s: B waited for A's lease)
lab without EPERM:    B  ! exit=0 elapsed=677ms   (B done +1.73s, A done +4.76s: A's live lease reaped)
```

The scratch root, with the scratch, socket, scoreboard, and capacity keys
at their defaults:

```text
$ karvi run ...            # released v0.25.0, no /dev/shm/karvi before
drwx------ netops:netops /dev/shm/karvi
drwxrwx--- netops:netops /dev/shm/karvi/scoreboards
drwx------ netops:netops /dev/shm/karvi/capacity
drwx------ netops:netops /dev/shm/karvi/netops
$ karvi run ...            # the lab build, no /dev/shm/karvi before
! exit=0 elapsed=715ms
ls: cannot access '/dev/shm/karvi': No such file or directory
$ karvi run ...            # B, scoreboards remade 0700 by A
warning: shared scoreboard directory unavailable (/dev/shm/karvi/scoreboards: permission denied); using private fallback t/b/state/scoreboards
```

`setup shared` in a private mount namespace (`sudo unshare --mount`, a
tmpfs over `/opt`, `/dev/shm`, and `/etc/tmpfiles.d`), so the host was
not touched; then the boot rule, then both operators:

```text
$ sudo karvi setup shared
created  /opt/karvi/shared  group netops  mode 2770
created  /opt/karvi/shared/jobs  group netops  mode 2770
created  /opt/karvi/shared/crun  group netops  mode 2770
created  /opt/karvi/shared/transcripts  group netops  mode 2770
created  /opt/karvi/users  group netops  mode 1770
created  /dev/shm/karvi  group netops  mode 3770
created  /dev/shm/karvi/scoreboards  group netops  mode 3770
created  /dev/shm/karvi/capacity  group netops  mode 2770
created  /etc/tmpfiles.d/karvi.conf  mode 0644
$ rm -rf /dev/shm/karvi; systemd-tmpfiles --create /etc/tmpfiles.d/karvi.conf
drwxrws--T root:netops /dev/shm/karvi
drwxrws--- root:netops /dev/shm/karvi/capacity
drwxrws--T root:netops /dev/shm/karvi/scoreboards
$ karvi watch --format table      # B, after a run by A and one by B
  JOB-ID            TIME      STATUS      OPERATOR     MODE        DONE   FAIL  ACTV  TARGET
  261003-024044-00  02:40:44  completed   netops.test  run         1/1       0     0  fake
  261003-024043-00  02:40:44  completed   netops       run         1/1       0     0  fake
$ sudo karvi setup shared         # over the root a v0.25.0 run left
repaired /dev/shm/karvi  group netops  mode 3770  (was group netops mode 0700)
repaired /dev/shm/karvi/scoreboards  group netops  mode 3770  (was group netops mode 0750)
repaired /dev/shm/karvi/capacity  group netops  mode 2770  (was group netops mode 0700)
created  /etc/tmpfiles.d/karvi.conf  mode 0644
```

**Found on the way.** The Go tests reached the host's `/dev/shm/karvi`
(the `cli` and `daemon` packages created it with a capacity ledger and a
socket folder), and one daemon test passed only because of it: without
it, its askpass socket under Go's long test directory passed the socket
path limit (`bind: invalid argument`). The test isolation now gives each
test binary a short scratch root and a capacity root of its own, and the
`cli` package's re-executed daemon removes what its isolation made. The
setup help still described a mismatched directory as left alone, though
setup had repaired it for some time. A repaired scratch root keeps the
owner who made it until the boot rule makes it root's; noted, not changed.

**Not taken.** The root created from an operator's run with a group mode
(the group is the site's word, given to setup). A warning on a host
without the root. A packaged tmpfiles file installed with a fixed group.
The sticky bit on `capacity`. Never creating an explicit path's folder
(the suites' parents exist; the rule is about parents). Chowning a
repaired scratch root to root at setup.

**Roadmap.** The credential prompt's line editing and Ctrl-C, then the
`--record` footer, then `--cd` and `--fs` for `run` and `command` (one
design, one execution-plan schema change), then the man page.

## 10. The credential prompts: line editing, Ctrl-C, and the stream reader (2026-10-03)

The second of the five objectives: the username and password prompts edit
as a stream line does, and Ctrl-C at either returns to the shell.

**What it gains.** An operator who mistypes a username fixes it with the
keys a terminal offers everywhere else, Ctrl-H and the Delete key
included, where those had gone into the username as bytes and failed the
login; an operator who presses Ctrl-C at a prompt has the shell back at
once, in a stream too, where karvi had held the key until the input ended;
and a Ctrl-C during a stream's job stops the job's follow, as it does for
`run`, where it had been lost.

**The review.** The prompt read the terminal in its own mode with echo off
for a password: Backspace as DEL worked there, but Ctrl-H, the Delete
key, and the arrows went into the answer as bytes; and the interrupt was
caught by the process's signal context while the read went on, so Ctrl-C
did nothing until the input ended, then gave `credential_prompt_unavailable`
(`EOF`).

**The rules settled.** The operator agreed: Ctrl-D on an empty prompt
keeps `credential_prompt_unavailable`; no job can run without the
credential, so an empty answer or Ctrl-C ends karvi. The prompt reads
through one line editor with stream mode (`internal/termline`, x/term in
raw mode for one line's read), the Delete key as a forward delete and
Ctrl-C told apart from Ctrl-D, one editor on the controlling terminal for
the process so a password typed ahead is read. Ctrl-C is the new
`credential_prompt_interrupted` (exit 113, the registry's cancelled exit);
since raw mode takes the key from the terminal's signal, it is given back
to the process as the interrupt, and the run stops as at any other moment.
An empty answer is the field's missing code at once
(`credential_username_missing`, `credential_password_missing`,
`credential_enable_missing`), so a password is not asked after an empty
username. A prompt that failed is not asked again for the targets
resolving beside the first.

**Found on the way.** The first stream example lost the Ctrl-C typed at
the prompt: the stream's reader read the next line while each job ran,
holding the terminal in raw mode through the job, so its editor took the
prompt's keys and a job's Ctrl-C as a key. Chapter 7's rule, raw mode for
one line's read alone, had not held since the reader was written; the
released executable shows it (below). The reader now reads a line when
the loop asks for it; a test counts the lines read at each job's start
(3 and 5, where the old loop had read all 6 before the first job).

**Executed.** Through `script(1)` against the fake device (user `abd`,
password `xy`), no credential in the environment; the keys arrive 1.5
seconds after the start:

```text
                         released v0.25.0                             lab
Ctrl-H: abc^Hd           user=abc^Hd, exit 110                        user=abd, the login succeeds
Delete: abdd ← ← Delete  user=abdd[D[D[3~, exit 110                   user=abd
Ctrl-C at the username   karvi-exit=6 after 14.5s (the input's end)  karvi-exit=113 after 1.50s
Ctrl-C at the password                                                karvi-exit=113 after 2.50s
an empty username                                                     credential_username_missing, no password prompt, exit 6
Ctrl-D at the username                                                credential_prompt_unavailable (the username prompt was ended by Ctrl-D or the terminal's end), exit 6
abd⏎xy⏎ in one write                                                  both read; show clock runs, exit 0
```

```text
$ karvi stream        # --target fake, "show clockx" ← Delete, --go, then Ctrl-C at the prompt
show clock
Username for fake:
credential_prompt_interrupted: client planning: 1 of 1 targets failed credential resolution: name:fake (interrupted at the username prompt policy=default)
karvi-exit=113 after 5.51s
$ karvi stream        # show slow, --go, Ctrl-C two seconds into the job, then --end; released v0.25.0
! fake [127.0.0.1] platform=cisco_iosxe user=abd backend=builtin-env-fallback transport=native
slow output
! exit=0 elapsed=4.66s
karvi-exit=0 after 7.23s
$ karvi stream        # the same, lab
^Cinterrupted: job 261003-071933-00 continues in the daemon; …
cancelled: the follow of job 261003-071933-00 was interrupted; the job continues
karvi-exit=113 after 4.50s
```

**Not taken.** Asking again after an empty answer. A generic code for the
ways a prompt ends. Ctrl-D as an interrupt. A flag set by the reader
rather than a count of Ctrl-C keys (a key typed ahead would be charged to
the wrong read). Raw mode held across a job, as before.

**Roadmap.** The `--record` footer, then `--cd` and `--fs` for `run` and
`command`, then the man page.

## 11. The `--record` footer (2026-10-03)

The third of the five objectives: a recorded login names its transcript
when the session ends, as its header did when it began.

**What it gains.** An operator leaving a long recorded session reads where
it was kept on the last line, where the header had scrolled away; and the
two lines read as karvi's other headers and footers do, in their style and
colors, set and silenced the same way.

**The rule.** The first form printed a plain `Login transcript saved to
PATH` after the plain header `Recording login transcript to PATH`. The
operator asked whether the footer matched the header exactly, prefixed
with `! ` and colored like the other headers and footers; it did not, and
neither line followed the display. The rule agreed: the two lines are
display templates, `display.record.header` before the session and
`display.record.footer` after its last output (the marker lines stripped,
the metadata's end record written), with one default,
`! transcript=<transcript>`, rendered by the display's formatter in the
login header's roles, on standard error, never into the transcript; a
failed session is named too, `--quiet` suppresses both, an empty template
prints nothing, and a template that does not render refuses the login
before a transcript is claimed. The registry moves from 22 to 23 and the
K03 draft's golden digest, which covers the configuration digest, is
re-pinned.

**Executed.** A recorded login with `display.color=always` against a fake
`ssh` that prints a prompt and exits: the two record lines are the same
bytes, in the roles of the login's header (`label` for the literals,
`value` for the path):

```text
^[[34m! transcript=^[[0m^[[37mstate/transcripts/261003/router01-075320.log^[[0m
^[[34m! ^[[0m^[[1;33mrouter01^[[0m^[[34m [^[[0m^[[1;35m127.0.0.1^[[0m^[[34m] platform=^[[0m^[[37mgeneric^[[0m …
router01#
^[[34m! transcript=^[[0m^[[37mstate/transcripts/261003/router01-075320.log^[[0m
```

The transcript suite asserts the header first and the footer last, a
session failing with 255 named, `--quiet` naming nothing, and an unknown
placeholder refused with no transcript made; the released v0.25.0 fails
it at the header:

```text
$ KARVI=lab/bin/karvi ./scripts/transcript-smoke-test.sh
transcript smoke: pass
$ sh -x ./scripts/transcript-smoke-test.sh    # the released v0.25.0
+ [ Recording login transcript to …/transcript-device-075355.log = ! transcript=…/transcript-device-075355.log ]
```

**Not taken.** Two different texts for the two lines (the operator asked
for one); the lines outside the display's templates (they could not be
styled, silenced, or set as the others are); naming the metadata file
beside the transcript (they sit side by side).

**Roadmap.** `--cd` and `--fs` for `run` and `command`, then the man page.

## 12. `--cd` and `--fs` for `run` and `command` (2026-10-03)

The fourth and fifth of the five objectives, taken together as one design
and one execution-plan schema change: `--cd=PATH` on `run` and `command`,
writing one file per device in the collection's shape, and `--fs=PATTERN`,
a suffix on that file's name.

**What it gains.** An operator's ad hoc capture, `karvi run --site X --cmd
'show run' --cd=.`, lands as one clean file per device in a directory of
their choosing, in the shape a `crun` writes, where the output reached only
`output.NAME.txt` inside a job folder under a header; with `--fs=.cfg` the
files carry a suffix an editor or a diff tool recognises. It waits on
nothing outside the tree: the daemon receives every job as a `run` and took
any plan with a collection for a `crun`, so the plan must say which word
asked for it (execution plan schema 9 to 10).

**The review.** Against the tree at `ddb1a82`, a lab build of the four
executables, the fake `r1` and `dead`, an alias of `cisco_iosxe` on port 1:

```text
$ cd crun; karvi crun --no-daemon --target r1 --target dead --cmd 'show clock' --cmd 'show version' --cd=.
crun 261003-085053-00 exit=ExitPartialFailure(101) artifacts=… collection=/tmp/nd.8Nm1/crun replaced=1 kept=1
$ cat r1                       # the uptime line dropped by cisco_iosxe's crun-filters
! show clock
*10:00:00.000 UTC Tue Sep 15 2026

! show version
Cisco IOS XE Software, Version 17.09.04a
$ karvi run --no-daemon --target r1 --cmd 'show clock' --cd=.
cli_option_unknown: unknown option --cd in run                      (exit 4)
$ karvi command --cd=. r1 'show clock'
cli_option_unknown: unknown option --cd in command                  (exit 4)
$ karvi command r1 'show clock' --cd=.      # after the device, it is command text
"command":"show clock --cd=."  → device_command_error, exit 107
$ karvi crun … --fs=.cfg
cli_option_unknown: unknown option --fs in crun                     (exit 4)
```

A rejected statement in a `run`, as the records have it; `crun` turns
`--continue` on, so the second line is a `crun`'s:

```text
run --cmd 'show bogus' --cmd 'show clock'             device_error, not_attempted_prior_command_failure
run --continue --cmd 'show bogus' --cmd 'show clock'  device_error, succeeded
```

**The rules settled.** The operator had agreed before the session that
`--fs` is the option alone, with no configuration key. Then, one issue at a
time:

1. *What a run's file holds and when it is replaced.* The operator agreed:
   the collection's shape exactly (a `! COMMAND` marker before each
   requested command's block, a blank line before every marker but the
   first, nothing else), named as a `crun` names it, a collision refused at
   planning (`output_file_name_collision`), written as the hidden temporary
   `.NAME.JOBID` and renamed at the device's last record; no `crun-filters`,
   so `run --cmd 'show version' --cd=.` keeps `r1 uptime is 1 day` where a
   `crun` drops it; replaced only when every record succeeded or is a
   statement the device rejected, the previous file untouched otherwise
   (connection, login, timeout, not attempted, halt, cancel, interrupt, a
   block that cannot be written); and `--continue` the run's own, so without
   it a rejected statement keeps the previous file and with it the file is
   replaced with the rejection in its block. Not taken: the filters (a `run
   --cd` into the `crun` tree then differs from the next `crun` by the
   dropped lines, accepted); `--cd` turning `--continue` on; a failed
   device's partial output.
2. *The job folder, `--nof`, and the report.* Executed: a `crun`'s folder
   has no `output.NAME.txt`, a `run`'s has; `crun --nof` collects with
   `artifacts=none`; the result line is a plain line beside the colored
   footer:

   ```text
   ^[[34m! exit=^[[0m^[[37m0^[[0m^[[34m elapsed=^[[0m^[[37m705ms^[[0m^[[34m artifacts=^[[0m^[[37m…/261003-090625-00^[[0m
   crun 261003-090625-00 exit=ExitSuccess(0) artifacts=…/261003-090625-00 collection=/tmp/nd.8Nm1/crun replaced=1 kept=0
   ```

   The operator agreed: the run's folder stays as it is without `--cd`,
   `output.NAME.txt` included (a kept file is the previous one, so the folder's
   text file is the one place that says what happened this time); `summary.json`
   and the jsonl summary document carry the `collection` block for `run` and
   `command` as for `crun`; `--nof --cd` collects with no folder. The operator
   then asked whether the result line was prefixed with `! ` and colored; it was
   neither, the gap [chapter 11](#11-the---record-footer-2026-10-03) closed for
   the record lines, and the rule was amended the same way: the line becomes the
   template `display.collection.footer`, default `! collection=<collection>
   replaced=<replaced> kept=<kept>`, `<collection>` the absolute collection
   directory (`--cd=.` from `/tmp/nd.8Nm1/crun` gives
   `collection=/tmp/nd.8Nm1/crun`), in the footer's roles, after the footer on
   every text path, never under jsonl (the summary document carries the block),
   `--quiet` or an empty template suppressing it; the exit, the folder, and the
   word, which the footer already says, leave it. `crun`'s plain result line
   goes with it (breaking, accepted); the registry moves from 23 to 24. Not
   taken: the plain line kept beside the footer; a `<collection>` placeholder in
   the run footer.

   Corrected when section A was begun: the rule had put the line on
   standard error, but the footer is on standard output, on the
   in-process and the daemon path alike (`2>/dev/null` kept the header,
   the output, and the footer; `2>&1 >/dev/null` kept `crun`'s plain
   line), and under `--format json` the footer is not printed either,
   which the rule had not considered. The operator agreed: the line is
   written by the footer's renderer to standard output directly after it,
   under text alone, never under json or jsonl, accepting that a `crun`
   under json or jsonl prints the counts nowhere but its summary. Not
   taken: a plain counts line on standard error under json and jsonl.
3. *What stays `crun`'s.* Executed: the watch screen showed MODE `crun`
   for every plan with a collection, since the daemon receives every job as
   a `run`, and `RunCollectionHook` ran for any result with a collection
   summary. The operator agreed: no `crun.after` for `run` or `command`,
   whatever the directory (a run's file in the `crun` tree reaches the next
   `crun`'s commit); MODE the operator's word, the audit names unchanged;
   `crun-commands` and `crun-filters` `crun`'s alone, so `run --cd` still
   needs a command; the plan's collection block carries `word` (`crun`,
   `run`, `command`) at execution plan schema 10, which MODE and the hook
   read. Not taken: the hook for every collection; MODE `crun` for any.
4. *Stream mode.* The operator asked whether `stream`, a loop handing each
   draft to `run`, gains the options. It does by construction: the reader
   resolves an option line through `run`'s table and executes `run`'s
   handler. Agreed: `--cd` and `--fs` are lines of the part that stays,
   kept by `--go` and `--clear`, removed by `--reset`, the last value
   winning; `--cd=.` is the stream's working directory; a later job to a
   device replaces the earlier job's file, and `--fs` keeps them apart
   (`--fs=.ver`, `show version`, `--go`, `--fs=.run`, `show
   running-config`, `--go` leaves `r1.ver` and `r1.run`).
5. *The directory, the modes, the keys, and the codes.* Executed:
   `--cd=…/new/deep` made both missing folders at 0770. The operator
   agreed: `--cd` resolved by the client as `crun.directory` is (`~`,
   relative to the working directory, `auto` the collection tree), checked
   once before any device with `crun.directory-mode` for what it makes,
   `crun.file-mode` for the files, `crun_directory_unavailable` and
   `crun_directory_not_writable` as for a `crun`; the `crun.*` keys and
   codes keep their names and document every collection. Not taken:
   renaming them `collection.*` and `collection_directory_*`.

   *Found on the way.* The hand-off had a known limit: under the packaged
   user unit a home directory is hidden, so `--cd=.` from a home through
   the daemon fails. Run under `systemd-run --user -p PrivateTmp=yes -p
   ProtectSystem=strict -p ProtectHome=read-only`, the lab daemon wrote
   `--cd=/tmp/…` and a folder in the home as if unsandboxed, in the
   client's own mount namespace (`mnt:[4026531841]` for both), on a host
   with `kernel.apparmor_restrict_unprivileged_userns = 1`. The limit is
   recorded for a sandbox that applies (a home refused loudly, a `/tmp`
   path landing in the daemon's private `/tmp` silently), and the unit's
   sandbox on such hosts went to the roadmap. The probe's folder in the
   home was removed at once.
6. *`=` in every mode.* Checking the stream rule as restated, two of its
   claims failed: a value check made by the handler is reported at `--go`
   without the line's number and stays in the draft, repeated at every
   `--go` until `--reset`
   (`--halt-on-error-percent 200` twice, then a dry run); and a stream line
   `--of PATH` sent the path to the device as command text at every job.
   On the command line the same:

   ```text
   $ karvi run --no-daemon --target r1 --of /tmp/nd.8Nm1/x --cmd 'show clock'
     sent: "/tmp/nd.8Nm1/x --cmd show clock"    → device_command_error, exit 101
   $ karvi command --of /tmp/nd.8Nm1/x r1 show clock
     dns_nxdomain: … name:/tmp/nd.8nm1/x
   $ karvi login --record /tmp/nd.8Nm1/y r1
     dns_nxdomain: lookup /tmp/nd.8nm1/y …
     ! transcript=…/transcripts/261003/_tmp_nd.8nm1_y-094125.log
   ```

   The first proposal gave the stream's spaced text to the option as its
   value. The operator asked instead for `=` in every mode: `--of` alone a
   switch, `--of=PATH` the switch and the root, never `--of PATH`. Agreed:
   an `=`-only option refuses a detached value with the new
   `cli_option_value_detached` (`--of takes its PATH with =:
   --of=/tmp/nd.8Nm1/x`), on a stream line any text after a space, on the
   command line a word of a path's form after a bare `--of` or `--record`;
   other words keep their position's meaning, so `--of out` still reaches
   the device and the documents say `--of=out`. The earlier entry refusing
   a slash heuristic stands, since the form decides a refusal and never a
   meaning (the proposal did not cite that entry; it was found when this
   item was recorded). In a stream the value checks of `--cd` and `--fs`
   run when the line is read, a bad line dropped with its number (`--fs`'s
   need of `--cd`, first left for `--go`, was withdrawn at item 7). Built
   first, as its own section A0.
7. *`--fs`.* Executed: the stale-temporary sweep's glob `.NAME.*` matched a
   concurrent `--fs=.cfg` run's temporary (`.r1.cfg.261003-094501-00`
   under `ls -A .r1.*`). Agreed: `--fs=SUFFIX` (the operator chose
   `SUFFIX` over `PATTERN`), a literal suffix on each device's file name
   and on nothing else, `=` alone, `crun_suffix_invalid` for an empty value
   or one holding `/`, NUL, or a control character (a newline would break
   the hook's one name per line); `suffix` in the plan's collection block;
   the suffixed name in the collision check, the summary, the hook's input,
   and the dry run; the sweep tightened to `.FILE.` and a job ID's form.
   The operator asked whether `crun --fs` renames the directory: it does
   not, the suffix goes on the files inside it. The operator then asked for
   a smart default: `--fs` without `--cd` is `--cd=.`, where the first
   proposal had refused it (`crun_suffix_unpaired`, withdrawn). Checked
   against the earlier items, one conflict: `crun` always has a directory,
   and `--cd=.` implied there would move a scheduled `crun --all
   --fs=.cfg` out of the site's tree into the scheduler's working
   directory; so the default is the word's own, `crun.directory` for
   `crun`, the working directory for `run` and `command`. The directory
   messages say "the working directory, implied by `--fs`" when it was.
   Not taken: a key; a template; the suffix on `output.NAME.txt`; refusing
   `.` and `..` (a suffix follows a name); a length bound; `--cd=.` implied
   on `crun`.

**Executed.** Section A0, the lab build against the fake `r1`:

```text
$ karvi run --no-daemon --target r1 --of /tmp/nd.8Nm1/x --cmd 'show clock'
cli_option_value_detached: --of takes its PATH with =: --of=/tmp/nd.8Nm1/x       (exit 4)
$ karvi command --of .. r1 show clock
cli_option_value_detached: --of takes its PATH with =: --of=..
$ karvi login --record /tmp/nd.8Nm1/y r1
cli_option_value_detached: --record takes its PATH with =: --record=/tmp/nd.8Nm1/y
$ karvi crun --all --cd /tmp/nd.8Nm1/c
cli_option_value_detached: --cd takes its PATH with =: --cd=/tmp/nd.8Nm1/c
$ karvi command --of r1 show clock                    # the switch, then the device
! exit=0 elapsed=900ms artifacts=/tmp/nd.8Nm1/base/jobs/261003/261003-101849-00
$ printf -- '--target r1\n--no-daemon\n--of /tmp/nd.8Nm1/x\n--of=/tmp/nd.8Nm1/of\nshow clock\n--go\n--end\n' | karvi stream
stream line 3 dropped: cli_option_value_detached: --of takes its PATH with =: --of=/tmp/nd.8Nm1/x
! r1 [127.0.0.1] platform=cisco_iosxe user=netops backend=builtin-env-fallback transport=native
! exit=0 elapsed=666ms artifacts=/tmp/nd.8Nm1/of/261003/261003-101850-00
```

No `x`, `y`, or `c` was made. A Go test of the transcript wrapper had
written `login --record DIR --platform cisco_iosx r1`, the detached form;
it now writes `--record=DIR`, which is what it meant.

Section A, `--cd` on `run` and `command`, the lab build against the fake
`r1` and `dead`, with `old r1` and `old dead` in the directory before:

```text
$ cd cap; karvi run --no-daemon --target r1 --target dead --cmd 'show clock' --cmd 'show version' --cd=.
…
! exit=101 elapsed=900ms artifacts=/tmp/nd.8Nm1/base/jobs/261003/261003-103623-00
! collection=/tmp/nd.8Nm1/cap replaced=1 kept=1
$ cat r1                                   # unfiltered: the uptime line kept
! show clock
*10:00:00.000 UTC Tue Sep 15 2026

! show version
Cisco IOS XE Software, Version 17.09.04a
fake-iosxe uptime is 1 day
$ cat dead
old dead
$ ls …/261003-103623-00                    # the run's folder, output.NAME.txt kept
commands.jsonl commands.txt errors.jsonl failed-devices.txt manifest.json metrics.json output.dead.txt output.r1.txt summary.json
$ karvi run --no-daemon --target r1 --cmd 'show bogus' --cmd 'show clock' --cd=.
! collection=/tmp/nd.8Nm1/cap replaced=0 kept=1
$ karvi run --no-daemon --continue --target r1 --cmd 'show bogus' --cmd 'show clock' --cd=.
! collection=/tmp/nd.8Nm1/cap replaced=1 kept=0      # r1: "! show bogus", the rejection, "! show clock", the clock
$ karvi command --nof --cd=../cmdcap r1 show clock
! exit=0 elapsed=815ms artifacts=none
! collection=/tmp/nd.8Nm1/cmdcap replaced=1 kept=0
$ karvi run --target r1 --cmd 'show clock' --cd=.          # through the daemon
! exit=0 elapsed=669ms artifacts=…/261003-103639-00
! collection=/tmp/nd.8Nm1/cap replaced=1 kept=0
$ karvi job follow 261003-103640-00                         # a detached run's
! exit=0 elapsed=672ms artifacts=…/261003-103640-00
! collection=/tmp/nd.8Nm1/cap replaced=1 kept=0
$ karvi crun --no-daemon --format jsonl … 2>&1 >/dev/null   # nothing on stderr
$ printf -- '--target r1\n--no-daemon\n--cd\n--cd=.\nshow clock\n--go\n--end\n' | karvi stream
stream line 3 dropped: cli_option_value_missing: --cd takes its PATH with =: --cd=PATH (the next word may be a device)
…
! collection=/tmp/nd.8Nm1/cap replaced=1 kept=0
$ karvi --set display.color=always run … --cd=. | tail -2 | cat -v
^[[34m! exit=^[[0m^[[37m0^[[0m^[[34m elapsed=^[[0m^[[37m682ms^[[0m^[[34m artifacts=^[[0m^[[37m…^[[0m
^[[34m! collection=^[[0m^[[37m/tmp/nd.8Nm1/cap^[[0m^[[34m replaced=^[[0m^[[37m1^[[0m^[[34m kept=^[[0m^[[37m0^[[0m
$ karvi watch --format table
  JOB-ID            TIME      STATUS      OPERATOR  MODE        DONE   FAIL  ACTV  TARGET
  261003-103642-00  10:36:42  completed   netops    crun        1/1       0     0  r1
  261003-103639-00  10:36:40  completed   netops    run         1/1       0     0  r1
  261003-103631-00  10:36:32  completed   netops    cmd         1/1       0     0  r1
```

The native suite's S33 had asserted `crun`'s plain line on standard error
under jsonl; it now reads the counts from the stream's summary document,
and the new S34 asserts the run's file, folder, kept device, rejected
statement both ways, the line after the footer on both paths, and that
only a `crun` runs the hook. The packaged cron script and timer run
`crun --format jsonl`, so their mail carries the summary document's
counts where it carried the plain line.

One consequence surfaced while building, and the operator agreed to it:
`--cd` is `crun.directory` as a flag-origin value on every word, as
`--of=PATH` is `output.root`, so a site that locks `crun.directory`
refuses `run --cd` and `command --cd` as it refuses `crun --cd`.

Section B, `--fs`, the lab build:

```text
$ cd fs; karvi run --no-daemon --target r1 --cmd 'show clock' --fs=.cfg     # --cd=. implied
! exit=0 elapsed=719ms artifacts=…/261003-111101-00
! collection=/tmp/nd.8Nm1/fs replaced=1 kept=0
$ karvi command --fs=.txt r1 show version
! collection=/tmp/nd.8Nm1/fs replaced=1 kept=0
$ ls -A
r1.cfg r1.txt
$ karvi crun --no-daemon --target r1 --cmd 'show clock' --fs=.cfg          # crun.directory kept
! collection=/tmp/nd.8Nm1/base/crun replaced=1 kept=0                     # base/crun/r1.cfg
$ karvi run --dry-run --no-daemon --target r1 --cmd 'show clock' --fs=.cfg | grep collection
collection: /tmp/nd.8Nm1/fs (file mode 0660, suffix .cfg)
$ karvi run --no-daemon --target r1 --cmd 'show clock' --fs=a/b
crun_suffix_invalid: --fs="a/b" holds /; the suffix is appended as written to each collection file's name, so it is not empty and holds no /, NUL, or control character      (exit 4)
$ cd ro; karvi run --no-daemon --target r1 --cmd 'show clock' --fs=.cfg      # ro is 0500
crun_directory_not_writable: /tmp/nd.8Nm1/ro is not writable by the operator: … (the working directory, implied by --fs)      (exit 9)
$ ls -A                                    # two stale temporaries, then run --fs=.cfg
.r1.261003-000000-00 .r1.cfg.261003-000000-00 r1.cfg r1.txt
.r1.261003-000000-00 r1.cfg r1.txt                                        # only r1.cfg's swept
$ printf -- '--target r1\n--no-daemon\n--fs=.ver\nshow version\n--go\n--fs=.clk\nshow clock\n--go\n--end\n' | karvi stream
… r1.clk r1.ver beside the others
```

One point of the agreed rule changed in the build, and the operator
accepted it as built: `--fs=` with nothing after the sign is
`cli_option_value_missing`, as `--cd=` is, rather than
`crun_suffix_invalid`, since the parser gives a bare `--fs` and `--fs=`
the same empty value; `crun_suffix_invalid` covers `/`, NUL, and control
characters. The sweep compares names as strings, never as a
glob, since a suffix may hold a glob's metacharacters (`--fs='[x]*'` is
tested). The unused `output.CollectionLabel`, the plain line's tail, is
removed.

**The sections, as committed.** The design (`55f2f03`, the DESIGN entries
and this chapter's items 1 to 7); A0 (`efef847`) an `=`-only option
refuses a detached value; A (`7f17885`) `--cd` on `run` and `command`, the
plan's word at schema 10, the collection line a display template; B
(`a2ed6e4`) `--fs=SUFFIX` and the sweep by job ID; C the documents read
against the build (the `job follow` help and four comments still named a
result line; the DESIGN entries now name `--fs` among the `=`-only
options and say the client, not the plan, gates the hook) and this
chapter's close. Batteries: the seventeen suites on lab builds after A
(14:40:51 to 14:44:10 UTC) and after B (15:13:12 to 15:16:31 UTC), pass;
`go test ./...`, vet, gofmt, and `make generated-clean` at each section.

**Not taken.** The filters on a run's file; `--cd` turning `--continue`
on; a failed device's partial output; turning a run's `output.NAME.txt`
off; the hook for every collection; MODE `crun` for any collection; the
`collection.*` keys and `collection_directory_*` codes; a plain counts
line on standard error under json and jsonl; the spaced value on a stream
line; refusing every word after a bare switch; a configuration key, a
template, or a length bound for the suffix; `--fs` implying `--cd=.` on
`crun`; a lock on `crun.directory` that binds `crun` alone.

**Roadmap.** The man page next (`karvi-prune.8`, then `karvi.1` and
`karvi-askpass.1`); the packaged user unit's sandbox on hosts where it
does not apply went to the roadmap as its own item. At the next release
(0.26.0 expected): the registry is 24 and the execution plan 10, and the
two `crun.*` codes' and keys' documentation names every collection.

## 13. The man page: `karvi-prune.8` (2026-10-03)

The roadmap's first item, after objectives 4 and 5: a manual page
installed with the distribution packages, `karvi-prune.8` first.

**What it gains.** On a host with the package, `man karvi-prune` gives an
operator the flags, what goes, the report, and the exits offline, beside
the one command they run under `sudo`, where that reference lived in
[`docs/PRUNE.md`](PRUNE.md) in the source tree alone and the package installs no
document. It waits on nothing outside the tree: groff is on the build
host, and the package needs one install line.

**The review.** A throwaway generator over `prune.DefineFlags` rendered an
OPTIONS section that `groff -man -ww` passed without a warning, and showed
the one gap: Go's flag package names the values `string`, `int`, and
`float`, where the synopsis says `auto|PATH`, `N`, and `PERCENT`.

```text
OPTIONS
       --basedir string
              The private root whose jobs and transcripts trees are pruned:
              auto (the operator's own, as karvi resolves it) or a path. Default: auto.
       --days int
              The retention age in days, one or more. Default: 31.
```

**The rules settled.**

1. *How the page is made.* The operator agreed: one committed file,
   `packaging/man/karvi-prune.8`, roff by hand, its OPTIONS section
   between two comment markers rewritten by `tools/mangen` from the flag
   set (escaped, every other byte kept, a missing or doubled marker
   refused); `make generate` in place, `make generated-clean` and the
   bundle verifier by comparison; a Go test running `groff -man -ww -z`,
   skipping without groff; one install line in the debian rules,
   `dh_compress` gzipping. Not taken: a `.8.in` template; a
   Markdown-to-roff converter; generating the whole page.
2. *The value names in one place.* The helper named each flag's value in
   four places that disagreed: the synopsis constant (`auto|PATH`, `N`,
   `PERCENT`), `-h` through Go's `PrintDefaults` (`-basedir string`,
   `-days int`, `-minfree float`), the throwaway page (the same), and the
   completion table (words and a path, no names). The operator agreed: the
   completion table gains a name for a value that is neither words nor a
   path, `FlagOrder` keeps the synopsis's order, and the synopsis, `-h`
   (double dash, placeholder, unquoted default), the page's OPTIONS, and Tab
   read them; tests hold them to the defined flags. Not taken: backquoted
   value names in the usage strings; parsing the synopsis; alphabetical
   order.
3. *The hand-written sections.* The package installs no documents yet, and the
   units' `Documentation=` lines name a [`PRUNE.md`](PRUNE.md) an installed host
   does not have. The operator agreed: the page is the terminal reference, NAME
   to SEE ALSO, stating the rules, the report, and the exits in full;
   [PRUNE.md](PRUNE.md) stays the guide with the reasons and the schedules and
   says that the page restates them; the SYNOPSIS is a second generated region
   (amending item 1's "the only generated part"), from `prune.Usage()`. Not
   taken: either document pointing to the other for the rules; a list of the
   helper's words for a test.
4. *The header.* Executed: `.TH KARVI-PRUNE 8 "" "karvi"` gives the footer
   `karvi … KARVI-PRUNE(8)` with a blank centre, and a version and a date
   fill it; groff passes both. The operator agreed: no version or date in
   any page, the package and `karvi version` naming the release. Not taken:
   editing them at each release; stamping them at the build (the packaging
   has no `debian/changelog` to take a date from).

**Executed.** Section M1, the value names, the lab build:

```text
$ karvi-prune -h
usage: karvi-prune [--basedir auto|PATH] [--sharedroot auto|none|PATH] [--scoreboards PATH] [--days N] [--minfree PERCENT] [--dry-run] [--verbose] [--format text|jsonl]
  --basedir auto|PATH
        the private root whose jobs and transcripts trees are pruned: auto (the operator's own, as karvi resolves it) or a path (default auto)
  …
  --days N
        the retention age in days, one or more (default 31)
  --minfree PERCENT
        the free-space floor in percent, under which the oldest eligible items go before their age; 0 turns pressure off (default 5)
  --dry-run
        report what would go and remove nothing
  …
$ karvi-prune --days 0
karvi-prune: --days takes one or more, not 0          (exit 2)
$ karvi-prune __complete 2 --days ''                  # a number: nothing offered, as before
```

The synopsis built from the order and the placeholders is the former
constant byte for byte (a test pins it); the completion suite passes
unchanged.

5. *The helper's help in karvi's shape.* The operator asked whether
   `karvi-prune -h` and `--help` print as karvi's help does, in its layout
   and colours. They did not: one lowercase `usage:` line, each
   description on its own line unwrapped (one 150 columns), no colour, on
   standard error, where karvi's help has a bold `Usage:` heading, option
   words in a 31-column field in the accent colour, descriptions wrapped at
   79, colour from `display.*`, on standard output. Agreed: the layout
   moves into `internal/helplayout` for both executables, karvi's output
   unchanged by a byte; the helper's text has karvi's shape, built from
   the flag definition; `-h` to standard output, a usage error with the
   text to standard error; the colour decided without configuration (the
   defaults: on at a terminal, the dark theme's roles). Not taken: a colour
   flag; reading `display.*` in the helper. Built as section M1b, after M1.

Section M1b, the helper's help in karvi's shape, the lab build:

```text
$ script -qfec "karvi-prune --help" /dev/null | cat -v | head -9
karvi-prune ^[[34m- remove finished karvi work older than the retention age^[[0m
^[[1mUsage:^[[0m
  karvi-prune [--basedir auto|PATH] [--sharedroot auto|none|PATH]
              [--scoreboards PATH] [--days N] [--minfree PERCENT] [--dry-run]
              [--verbose] [--format text|jsonl]
^[[1mOptions:^[[0m
  ^[[36m--basedir auto|PATH^[[0m            The private root whose jobs and transcripts
                                 trees are pruned: auto (the operator's own, as
$ karvi-prune -h | grep -c $'\x1b'           # a pipe: plain
0
$ karvi-prune --bogus >/dev/null              # exit 2, the help on stderr
flag provided but not defined: -bogus
karvi-prune - remove finished karvi work older than the retention age
$ karvi-prune --days 0
karvi-prune: --days takes one or more, not 0
karvi-prune - remove finished karvi work older than the retention age
```

Before the layout moved, every karvi help text (the top and 29 command
paths, colour on and off, 60 outputs) was captured from the build at
`b50d227`; after the move, `diff -r` found them identical. The unknown
flag's line is the flag package's own, `flag provided but not defined:
-bogus`, as before.

Section M2, the page, the lab:

```text
$ go run ./tools/mangen; groff -man -Tutf8 -ww -z packaging/man/karvi-prune.8      # no warning
$ man -l packaging/man/karvi-prune.8
KARVI-PRUNE(8)              System Manager's Manual              KARVI-PRUNE(8)
NAME
       karvi-prune - remove finished karvi work older than the retention age
SYNOPSIS
       karvi-prune [--basedir auto|PATH] [--sharedroot auto|none|PATH]
       [--scoreboards PATH] [--days N] [--minfree PERCENT] [--dry-run]
       [--verbose] [--format text|jsonl]
…
OPTIONS
       --basedir auto|PATH
              The private root whose jobs and transcripts trees are pruned:
              auto (the operator's own, as karvi resolves it) or a path.
              Default: auto.
…
karvi                                                             KARVI-PRUNE(8)
$ install -D -m 0644 packaging/man/karvi-prune.8 ROOT/usr/share/man/man8/karvi-prune.8; gzip -9n …   # the rules' line, then dh_compress
$ man -M ROOT/usr/share/man karvi-prune                                                              # found, the same page
```

The first rendering hyphenated paths and options across lines
(`De‐fault`, `~/.lo‐cal`, `--ver‐bose`) and stretched lines to the margin;
the page sets `.nh`, and `.ds AD l` with `.ad l`, since groff's man macros
restore the adjustment at every paragraph from `AD`.

*Found on the way.* `make generated-clean` ran its comparisons as one shell line
without `set -e`, so its exit was the last `cmp`'s alone: with
`configs/reference.toml` made stale it printed the difference and exited 0, and
only [`docs/ERROR-CODES.md`](ERROR-CODES.md), compared last, could fail it. With
`set -e` a stale reference, error-code table, or generated page line each exits
2; a hand-written line of the page is kept by the generator and is not
staleness. The bundle verifier runs under `set -eu` and was not affected.

**The sections, as committed.** M1 (`b50d227`) the value names and the order
defined once, with the design records; M1b (`ec019b4`) the helper's help in
karvi's layout, the layout shared; M2 `tools/mangen`, the page, `make generate`
and `generated-clean` (with `set -e`), the bundle verifier, the groff lint, the
install line, the notes in [PRUNE.md](PRUNE.md) and OPERATIONS, and this
chapter. Battery: the seventeen suites on a lab build after M2 (16:59:22 to
17:02:46 UTC), pass; `go test ./...`, vet, gofmt, and `make generated-clean` at
each section.

**Not taken.** A `.8.in` template; a Markdown-to-roff converter;
generating the whole page; Go's backquoted value names; alphabetical
order; the page or the guide pointing to the other for the rules; a
version or a date in `.TH`; a colour flag on the helper; reading
`display.*` in the helper.

**Roadmap.** `karvi.1` in the same form, its option sections from the
help constants, in the next session; `karvi-askpass.1` followed in this
one (item 6 below).

6. *`karvi-askpass.1`.* The operator asked for a simple page for the
   askpass helper, `karvi.1` left for the next session. Executed: run by
   hand, with or without `--help`, the helper prints nothing and exits 2.
   Agreed: a page written by hand alone (NAME, SYNOPSIS, DESCRIPTION,
   ENVIRONMENT, SECURITY, EXIT STATUS, FILES, SEE ALSO), the helper taking
   no flags so no region is generated; the groff lint over every page in
   `packaging/man/`; one install line to `man1`; the executable unchanged.
   Not taken: a `--help` on the helper. Built in one section:

   ```text
   $ man -l packaging/man/karvi-askpass.1
   KARVI-ASKPASS(1)            General Commands Manual           KARVI-ASKPASS(1)
   NAME
          karvi-askpass - deliver one credential field from karvi to OpenSSH
   SYNOPSIS
          karvi-askpass [PROMPT]

          ssh(1) runs it for karvi; an operator does not.
   …
   EXIT STATUS
          0      The answer was written.
          2      The socket or the token is not set.
   …
   $ man -M ROOT/usr/share/man -w karvi-askpass karvi-prune     # after the rules' lines and gzip
   ROOT/usr/share/man/man1/karvi-askpass.1.gz
   ROOT/usr/share/man/man8/karvi-prune.8.gz
   ```

   The page's claims were read against `internal/askpass` and the system
   transport: one broker per session open, one accepted connection, a
   five-minute wait and a ten-second exchange, a five-second dial; the lint
   was shown to fail on a page with an undefined macro.

## 14. The man page: `karvi.1` (2026-10-03)

The roadmap's first item, in the form `karvi-prune.8` set: roff by hand,
the generated regions written by `tools/mangen`, no version or date, the
install in the debian rules.

**What it gains.** On a host with the package, `man karvi` answered "No
manual entry"; the page gives the command words, the global options, each
word's options as `--help` prints them, and the prose that lived in
README and OPERATIONS alone, which the package does not install. It waits
on nothing outside the tree.

**The review.** Against the tree at `9e144cd`, a lab build of the four
executables:

```text
help outputs (the top and 11 words)   803 lines; 401 repeat a line printed elsewhere
                                      (the target-input, platform, and transport blocks
                                      of login, command, run, crun); crun's target
                                      inputs are run's byte for byte
option entries                        login 22, command 38, run 47, crun 47
the smallest                          version 5 lines, config 11, job 19, daemon 21
```

man-db 2.12 joins the words, on a throwaway tree holding `karvi.1`,
`karvi-run.1`, and `karvi-setup.8`:

```text
$ man -w karvi run          → man1/karvi-run.1
$ man -w karvi crun         → man1/karvi.1, then "No manual entry for crun"   (exit 16)
$ man -w karvi setup        → man8/karvi-setup.8
```

For scale, rendered at 80 columns: `ssh(1)` 1056 lines, `git(1)` 1541,
`systemctl(1)` 2263.

**The rules settled.**

1. *One page or a page per word.* The operator agreed: a page per help
   text of the parser table, `karvi.1` from the top help and
   `karvi-WORD.1` from each word's (login, command, run, crun, stream,
   daemon, job, config, setup, watch, version), all in section 1, each the
   terminal reference for its word, `man karvi WORD` reaching it through
   man-db; no page for an alias; the cost twelve files, some thin. Not
   taken: one page (the shared blocks once per word, or a structure the
   help texts do not have); pages for the four large words alone; section
   8 for `karvi-setup`; a page per alias.
2. *The generated regions.* Executed: the twelve help texts classified by
   `helplayout`'s line classes; config, daemon, job, setup, version, and
   watch have no option entry (their options are in the Usage lines and the
   prose), and the others hold rules in prose between their entries. A
   throwaway converter over the captured texts (the Usage lines as the
   SYNOPSIS; the rest as the DESCRIPTION, headings as `.SS`, entries as
   `.TP` with the words bold, prose as paragraphs) gave twelve pages that
   `groff -man -ww -z` passed without a warning:

   ```text
   SYNOPSIS
          karvi run [target inputs] [options] <device command words...>
   …
   DESCRIPTION
      Target inputs (at least one; command-line order is kept, the first
          occurrence of a name keeps its position, names are lowercase)
          --target NAME, --host, --t
                 Repeatable inventory or direct target, or glob
   …
   rendered lines before any hand section: top 83, login 123, command 200,
   run 230, crun 247, stream 49, setup 50, watch 34, daemon 31, job 30,
   config 21, version 13
   ```

   The operator agreed: two generated regions per page, both from the
   word's help text, the SYNOPSIS (the Usage lines, the action words bold)
   and the DESCRIPTION (the rest, laid out by the help's own line classes,
   the words byte for byte); NAME and every other line by hand; `run`'s
   adapter status (`scrapligo 1.4.2 compiled in`) on the page as in the
   help. Not taken: an OPTIONS region from the entries alone (empty on six
   pages, the prose rules lost); the whole page generated; a hand
   DESCRIPTION with OPTIONS generated where there are entries.

   Found by the converter, two help defects a page would inherit, taken
   each as its own issue: `login`'s "Host-key modes" lines and `run`'s
   Dispatch options without a description.
3. *`login`'s Host-key modes.* Executed: the section's three lines are the
   only lines of any help text with a gap anywhere but the description
   column, so the terminal shows them as prose (no accent colour) and the
   converter ran them into one paragraph; `--ssh-host-key-policy` has no
   description in `login`, `command`, or `run`, though the registry calls
   `ssh.host-key-policy` the unified policy of the three. The operator
   agreed: one shared entry, `hostKeyPolicyHelp`, in the three texts
   (`crun` through `run`'s), naming the key and the modes with the default,
   the section removed; a test failing on a help line with a gap anywhere
   but the column; the 60 help outputs captured before and after, only
   `login`, `command`, `run`, and `crun` changing. Through the converter:

   ```text
   --ssh-host-key-policy accept-new|secure|insecure
          The host-key policy (ssh.host-key-policy): accept-new accepts
          and persists a new key and rejects a changed one (the default);
          secure requires a matching pre-enrolled key before access;
          insecure accepts unknown or changed keys with prominent warnings
   ```

   Not taken: the section rewritten at the column in `login` alone; a
   second column width in the classifier.
4. *`run`'s Dispatch options.* The block listed eight options without a
   description (`--start-width N --max-width N --halt-on-error-count N` on
   one line), and their keys' registry entries give ranges alone.
   Executed on the fake `r1` and an unreachable `d1`, `--no-daemon --nof
   --cmd 'show clock'`:

   ```text
   --tl 'd1 r1 r2 r3 r4 r5' --halt-on-error-percent 50          (serial)
      d1 connection_error; r1..r5 not_started_halt                       exit 103
   --tl 'r1 r2 d1 r3 r4 r5' --halt-on-error-percent 50
      no halt (1 failed of 3 ended)                                      exit 101
   --tl 'r1 r2 d1 r3 r4 r5' --dp --workers 2 --halt-on-error-count 1
      d1 connection_error; r3, in flight, finished; r4 r5 not_started_halt
   --tl 'd1 r1 …' --dw --start-width 1 --max-width 1 --wave-gate-error-count 1
      the wave of 4 ran out; r4 r5 not_started_wave_gate                 exit 104
   --wave-delay 30        cli_option_value_invalid: --wave-delay takes a duration such as 30s or 5m, not "30"
   --wave-delay 1d        the same; 1.5s, 500ms, 2m, 1h30m, 100us planned
   ```

   The operator agreed to an entry per option, its key, and what 0 means,
   the percent halt described as built, and asked for more: what unit a
   `--wave-delay` duration is in, with three values; the auto formulas of
   the start and the ceiling, with the widths on 4, 8, and 32 logical CPUs;
   and how they compare with the host's maximum. Read in the code: the cap
   `dispatch.server-max-inflight` at 0 is `min(256, max(32, 8*CPUs))` over
   the effective CPUs, the ceiling's own formula. The draft:

   ```text
     --start-width N                A wave job's first width, and its floor
                                    (dispatch.wave-start-width; 0 is
                                    min(64, max(16, 4*CPUs)), CPUs the host's
                                    logical CPUs)
     --max-width N                  A wave job's widest wave
                                    (dispatch.wave-max-width; 0 is
                                    min(256, max(32, 8*CPUs)))
   …
     --wave-delay DURATION          A pause between waves
                                    (dispatch.wave-gate-timed-delay; 0s none): a
                                    number and its unit, h, m, s, ms, us, or ns,
                                    such as 500ms, 45s, or 5m, the parts
                                    combinable (1m30s); a bare number is refused
     At auto the wave widths follow the host's logical CPUs: with 4 a wave job
     starts at 16 devices and widens to at most 32; with 8, from 32 to 64; with
     32 or more, from 64 to 256. The ceiling's formula is also the host's cap,
     dispatch.server-max-inflight at 0 (32, 64, and 256 on those hosts): the
     device sessions in flight at once across every job and operator on the
     host. One wave job at its ceiling can fill the cap alone; beside other
     jobs its workers past the cap wait for a lease before they connect, so a
     ceiling above the cap adds workers and no sessions.
   ```

   The operator then set the additions aside until the hand-written
   sections are consolidated (item 7): the help keeps the entries first
   proposed, each option's key and what 0 means (the widths at 0 "from the
   host's CPUs", the delay "0s none"), and the draft above is the material
   for that item. Not taken: a pointer to [SCALE.md](SCALE.md) in place of the
   entries; changing the percent rule in a help change.

   Found while checking the words, taken as its own issue: the options do
   not hold the keys' ranges. `--wave-delay 2h` is planned where `--set
   dispatch.wave-gate-timed-delay=2h` is `config_value_out_of_range`
   (`0s..1h`); `--wave-delay -1s` reaches `execution_plan_invalid`;
   `--max-width 600` runs at 512 where `--set dispatch.wave-max-width=600`
   is `config_dispatch_wave_max_exceeds_absolute`; `--start-width 100
   --max-width 10` starts at 10 without a word.
5. *How `tools/mangen` reaches `internal/cli`.* Executed: a probe test
   added through `go test -overlay` (the tree untouched) walked
   `commandTable`: the top help and the eleven visible top-level words, 192
   to 11094 bytes, every subcommand of daemon, job, config, and setup
   sharing its word's text, the hidden `help` no page; exactly the twelve
   pages of item 1. `internal/cli` has no init function, and importing it
   takes `tools/mangen` from 14 of the module's packages to 59;
   `helplayout` exports `Style` and `Layout` alone. The operator agreed:
   `cli.HelpPages()` returns the twelve as `{Word, Text}` in the table's
   order; `helplayout.Roff` renders a help text's SYNOPSIS and DESCRIPTION
   with the terminal layout's classifier and holds the one roff escape,
   which prune's regions use too; `tools/mangen` works from a page table
   (path, marker source, regions), a word without its page refused, `-dir
   DIR` writing every generated page for `generated-clean` and the
   verifier. Not taken: the twelve constants exported; the generator as a
   test inside `internal/cli`; a built executable's `--help` as the
   source; prune's page rebuilt from its help text.
6. *The options and their keys' ranges and locks.* Executed: a global
   `/opt/karvi/config.toml` placed only inside `sudo unshare --mount` (a
   tmpfs over `/opt`; the host has no `/opt/karvi`), locking
   `dispatch.wave-max-width = 8` and `dispatch.wave-gate-timed-delay =
   "0s"`, the operator's runs:

   ```text
   --set dispatch.wave-max-width=64        config_lock_violation: … declared at /opt/karvi/config.toml:5
   --max-width 64                          planned: dispatch: wave start-width=16 max-width=64
   --set dispatch.wave-gate-timed-delay=5m config_lock_violation: … declared at /opt/karvi/config.toml:6
   --wave-delay 5m                         planned: "wave_gate_timed_delay_ns": 300000000000
   ```

   and an option already in the lock-aware layer:

   ```text
   --blind-wait 20m     config_value_out_of_range: … must be 0s..10m for execution.blind-wait at command-line
   ```

   The nine Dispatch options went to the planner beside the configuration,
   so a lock was passed and the ranges were clamped or left to the plan's
   validation (item 4's findings). The operator agreed: every option that
   stands for one key is that key's override in the lock-aware layer, the
   Dispatch options and `--dispatch` with its shortcuts among them; a lock
   refuses the option; the planner reads the keys alone; the two command
   line dispatch codes retired to `config_value_out_of_range` (their exit
   moving from usage to configuration, breaking, accepted);
   `--address-authority` outside the rule, a device's authority above its
   row. Not taken: the command line checking the ranges itself; clamping
   with a notice; `--dispatch` outside the rule. Found, for later: an
   override's error names its source `command-line` and not the option.
7. *The hand-written sections.* Executed and read: `internal/exitcode`
   defines 24 exit statuses with names alone, and no document says what
   each means (ERROR-CODES maps codes to exits); six options in four words
   take a DURATION (`--blind-wait`, `--wave-delay`, `daemon --after` and
   `--start-timeout`, `watch --refresh` and `--stale-after`) through one
   parser rule; karvi reads `/etc/karvi/config.toml` then
   `/opt/karvi/config.toml` (the only place for locks), `KARVI__SECTION__KEY`,
   `NETUSER`, `NETPASS`, `NETENABLE`, `NO_COLOR`, `TERM`, `COLORFGBG`, and
   `HOME`, none of them in a help text; README and OPERATIONS hold the
   per-word material, and the package installs neither. The operator
   agreed: `karvi.1` states once what the words share (CONFIGURATION,
   VALUES with DURATION and N, ENVIRONMENT, FILES, EXIT STATUS); the top
   help gains the sentence on key-backed options; each word's page writes
   only what its help does not state, ends with SEE ALSO naming `karvi(1)`,
   and restates its guide's section, which says so; `karvi-run.1`'s
   DISPATCH carries item 4's set-aside material (the auto widths and their
   values on 4, 8, and 32 logical CPUs, the cap, the halts and gates as
   built, the delay); the per-word sections drafted against the build.
   Not taken: the shared sections on every page; the duration's form in
   each DURATION entry; width tables in the help; pointers to the guides
   in place of the facts; pages for the guides' other material.
8. *The EXIT STATUS.* Executed:

   ```text
   $ karvi command --nof d1 show clock            exit 110   (the device's cause)
   $ karvi run --no-daemon --nof --tl d1 --cmd …  exit 101   (every device failed; a run is 101 still)
   $ karvi run --bogus                            exit 4
   ```

   and read: the 24 statuses have constants and names (the names in the
   records and the audit) and no meaning; the registry's codes set 2 (151
   codes), 4 (65), 6 (62), 112 (42, everything between the client and the
   daemon), and the rest; `determineExit` takes 111, then 106, 113, 114,
   102, 103, 104, 105, then 0, then 101 for a `run`, then another word's
   first failure with an exit. The operator agreed: `internal/exitcode`
   defines each status once with its name and a one-line meaning, a test
   holding the constants to it; `karvi.1`'s EXIT STATUS a third generated
   region from it, the choice of exit by hand above; `tools/errorcodegen`
   writing the same list into ERROR-CODES. Not taken: the meanings by hand
   in the page; the precedence generated; renaming `ExitPartialFailure`.

**Executed.** Section H, the help changes (items 3 and 4; item 7's sentence
moved to section K, where it becomes true for the Dispatch options). The
shape test, `helplayout.Irregular` over every help text, run through an
overlay against the committed `help.go`, failed on the three Host-key mode
lines and also on `  --dispatch serial|parallel|wave  --workers N` in `run`
and `crun`, which the review's scan had passed over (it skipped lines
beginning with a dash); both are gone in H. The 60 help outputs captured
from the lab build before and after: `login`, `command`, `run`, and `crun`
differ, under both colours, and nothing else:

```text
$ diff before/login.never after/login.never
65a66,72
>                                  The host-key policy (ssh.host-key-policy):
>                                  accept-new accepts and persists a new key and
…
85,89d91
< Host-key modes:
<   accept-new  Accept and persist a new key; reject a changed key (default)
…
$ grep -- --wave-delay after/run.always | cat -v
  ^[[36m--wave-delay DURATION^[[0m          A pause between waves
```

Section K, the Dispatch options as their keys' overrides (item 6), with
item 7's sentence in the top help. Under a global file placed inside
`sudo unshare --mount` as in item 6, the lab build:

```text
== locked: wave-max-width = 8, wave-gate-timed-delay = 0s
--dw --max-width 64       config_lock_violation: … lock "dispatch.wave-max-width" declared at /opt/karvi/config.toml   exit 3
--dw --wave-delay 5m      config_lock_violation: … lock "dispatch.wave-gate-timed-delay" …                             exit 3
--dw --max-width 8        config_lock_violation (an equal value too, as --set dispatch.default=serial is)              exit 3
== locked: default = serial
--dp, --dispatch=wave, --ds                         config_lock_violation: … lock "dispatch.default" …                exit 3
```

and without a lock:

```text
--dw --wave-delay 2h                      config_value_out_of_range: … must be 0s..1h        exit 2
--dw --wave-delay -1s                     config_duration_negative                           exit 2
--dw --max-width 600                      config_dispatch_wave_max_exceeds_absolute          exit 2
--dw --start-width 100 --max-width 10     config_dispatch_wave_max_below_start               exit 2
--halt-on-error-percent 101               config_value_out_of_range: … must be 0..100        exit 2
--set dispatch.wave-max-width=20 … --dw --max-width 64    dispatch: wave start-width=16 max-width=20
KARVI__DISPATCH__HALT_ON_ERROR_COUNT=1, --tl d1,r1        exit 102; with --halt-on-error-count 0, exit 101
a stream: --workers -1, show clock, --go   config_value_out_of_range, the next job run at --workers 1
```

The flag layer took Go `int` values as `config_type_error`; the new
configuration test found it before the lab did, and the command line
writes the integer keys as `int64`. `--halt-on-error-count 0` now turns a
configured halt off, where a 0 had fallen back to the configuration. A
lock refuses an option whose value equals the locked one, as it refuses
`--set` with it. The two command-line dispatch codes are retired at
v0.26.0, the expected next minor, as the registry's new rows are. Battery:
the seventeen suites on a lab build after K (19:13:59 to 19:17:23 UTC),
pass; `go test ./...`, vet, gofmt, and `make generated-clean`.

Section E, the exit statuses (item 8). The meanings were checked against
each status's codes in the registry before they were written, and three
drafts widened: 4 covers a file the command line names
(`commands_file_unreadable`), 5 a target file, an unknown platform, and an
empty selection as well as the inventory, 9 an action that needs root or
is not allowed (`setup_requires_root`, `telnet_not_allowed`). The test
parses `exitcode.go` for its constants and holds `Statuses` to them; run
against a copy without 114's entry it fails (`23 statuses, 24
constants`). `errorcodes.Validate` refuses a code whose exit is not
defined. [`docs/ERROR-CODES.md`](ERROR-CODES.md) opens with the table:

```text
| 101 | `ExitPartialFailure` | A run in which one or more devices failed or did not start, no halt or gate applying. |
| 102 | `ExitHaltErrorCount` | A run stopped starting devices at its failure count (dispatch.halt-on-error-count). |
…
```

`karvi.1`'s EXIT STATUS region is section G's, with the page table.
`go test ./...`, vet, gofmt, and `make generated-clean`.

Section G, the accessor, the renderer, the page table, and the twelve
frames. `Layout`'s line analysis moved into `analyze`, which `Roff` reads
too; the 60 help outputs and `karvi-prune -h` on a pipe and at a terminal,
captured before and after, are identical. `tools/mangen` takes `-src` and
`-dir` in place of `-input` and `-output`; `karvi-prune.8` regenerates byte
for byte, its markers now built from each region's source. The frames are
NAME, the marker pairs, and SEE ALSO, `.TH KARVI-RUN 1 "" "karvi"` as
prune's. Every page passes `groff -man -ww -z`. Rendered:

```text
$ man -l packaging/man/karvi.1
SYNOPSIS
       karvi [global-options] <command> [options] [payload]
DESCRIPTION
   Commands
       login  Open one interactive SSH session
       command, cmd
              Run commands on one device
…
EXIT STATUS
       0      ExitSuccess: Every device succeeded, or the command did what it
              was asked.
…
$ man -l packaging/man/karvi-watch.1        # a Usage line continued
SYNOPSIS
       karvi watch [--format tui|table|json] [--theme auto|dark|light|nocolor]
       [--color auto|always|never] [--refresh DURATION] [--stale-after
       DURATION] [--filter TEXT] [--sort KEY] [--once] [--help]
$ man -l packaging/man/karvi-setup.1
SYNOPSIS
       sudo karvi setup shared [--group NAME] [--mode 2770|2775]
       sudo karvi setup tab
```

The rules' lines into a simulated package root, gzipped:

```text
$ install -D -m 0644 -t ROOT/usr/share/man/man1 packaging/man/*.1      # thirteen pages
$ man -M ROOT/usr/share/man -w karvi run        → man1/karvi-run.1.gz
$ man -M ROOT/usr/share/man -w karvi setup      → man1/karvi-setup.1.gz
$ man -M ROOT/usr/share/man -w karvi-prune      → man8/karvi-prune.8.gz
```

`make generated-clean` with one generated line of `karvi-config.1`
edited: `packaging/man/karvi-config.1 … differ: byte 861, line 24`, exit
2; restored, exit 0. Tests: the page table and a missing page refused by
name (`karvi-watch.1 does not exist`); `Roff` over a text of every shape;
`HelpPages` against the table, every subcommand sharing its word's text.
`go test ./...`, vet, gofmt, and `make generated-clean`.

Section P, the hand-written sections, part 1: `karvi.1`. An opening
paragraph before the generated DESCRIPTION; CONFIGURATION, VALUES,
ENVIRONMENT, and FILES; the choice of exit above the generated list. Each
claim was read in the code and executed on the lab build:

```text
--config file, then KARVI__DISPATCH__ORDER=shuffle      value "shuffle", overridden: <builtin>, k2.toml:2
KARVI__… shuffle, run --order sorted                      dispatch: … order=sorted
KARVI__… shuffle, --set dispatch.order=random, --order sorted   order=random
KARVI__SPOOLDIR=…/sp                                      source: KARVI__SPOOLDIR
KARVI__BOGUS_KEY=1                                        config_unknown_environment
--config DIR (a.toml inside)                              source: …/cdir/a.toml:2
config show --explain dispatch.wave-gate-timed-delay      validation: 0–1h. lock: none
command --nof nosuchdevice.invalid show clock             exit 7
run --tl nosuchdevice.invalid,r1                          dns_nxdomain, exit 7 (planning refuses the set)
```

The layers are the defaults, the first existing global file (the only
place for locks), the operator's `~/.config/karvi/config.toml` unless the
global file sets `config.allow-user-layer = false`, each `--config` root (a
file, or a directory of `.toml` files), the environment, the options, and
`--set`; the home is the password database's. The private root is
`/opt/karvi/users/USER`, then `/var/lib/karvi/users/USER` where the site
made `users`, else `~/.local/share/karvi`; the trust store's `auto` is
`~/.local/share/karvi/known_hosts`. The first draft said `login` and
`command` exit 107 to 110; an unresolvable name exits 7 before the device,
so the paragraph says that any word exits with an earlier stage's status.

*Found, for later.* `NO_COLOR` is read by the watch screen alone: at a
terminal under `display.color = "auto"`, `NO_COLOR=1 karvi --help` prints
in colour. The page states it as built.

Part 2: `karvi-run.1` (DISPATCH, OUTPUT, REHEARSAL, EXAMPLES),
`karvi-crun.1` (COLLECTION, EXAMPLES), and `karvi-command.1` (OUTPUT,
EXAMPLES). DISPATCH carries item 4's set-aside material: the widths at 0
with their formulas and their values on 4, 8, and 32 logical CPUs, the cap
and how the ceiling compares with it, and the delay as a DURATION of at
most `1h`. Executed on the lab build:

```text
run --no-daemon --tl d1,r1 --cmd 'show clock'      exit 101; the folder's eight kinds of file
run --nof --tf DIR/failed-devices.txt --cf DIR/commands.txt   d1 alone; artifacts=none
run --tl r1 --cmd 'show clock' --dry-run           address, credential binding, transport, port,
                                                   "commands: 1 (command_plan_digest …)", dispatch line;
                                                   no daemon started
run --tl r1 --cmd 'show clock' --exercise          status: exercised, exit 0, exercise.json kept
run --tl r1,r2 --cmd 'show clock' --detach         exit 0, job_id and artifact_dir; job follow renders, exit 0
run --tl r1 --cmd 'show slow', SIGINT at 1.2s      client exit 113; the job completed, exit 0
run --tl r1,r2 --cmd 'show running-config' --fs=.cfg   r1.cfg, r2.cfg, "Building configuration..." kept
crun --no-daemon --tl r1,d1 --cd=. --fs=.cfg, crun.after a script
                                                   exit 101; replaced=1 kept=1; d1.cfg the old file;
                                                   the platform's list, the byte count and uptime dropped,
                                                   "Last configuration change" kept; commands.cisco_iosxe.txt,
                                                   no output.NAME.txt; the hook in the directory, r1.cfg on
                                                   stdin, KARVI_CRUN_*, KARVI_EXIT=101, KARVI_JOB_*
command --nof r1 --echo --cmd 'show clock'         "r1#show clock" before the block
cmd --nof r1 --cmd 'copy running-config startup-config' --expect 'Destination filename='
                                                   [OK], exit 0
```

The first draft said the dry run lists each device's commands; it lists
their count and digest, and the page says so. OPERATIONS "Rehearsal,
detach, and follow", "Dispatch", "The job's output files", and "The
collection run", SCALE "Related", and COLLECTION's related documents say
that the pages restate them.

Part 3: the remaining eight pages. `karvi-login.1` TRANSCRIPTS and HOST
KEYS; `karvi-stream.1` and `karvi-job.1` EXAMPLES; `karvi-daemon.1`,
`karvi-config.1`, `karvi-setup.1`, and `karvi-watch.1` FILES;
`karvi-version.1` nothing more. Executed on the lab build:

```text
login --record=DIR r1, driven under script     ! transcript=DIR/261003/r1-161737.log before and after;
                                               the .log and .meta.jsonl at 0640, folders 0750;
                                               no "Script started" line left
the trust store after accept-new               [r1]:38933 ssh-ed25519 …, mode 0600
ssh-keyscan -p PORT 127.0.0.1 | sed "s/^[^ ]* /[r1]:PORT /"   the line accept-new wrote
command r1 with another key for [r1]:PORT      host_key_changed, exit 109
--set ssh.host-key-policy=secure, command --address 127.0.0.1 newdev
                                               host_key_not_enrolled, exit 109; nothing enrolled
stream: --target r1, show clock, --go          one job; --quiet stream <file: the output alone
job follow JOB --format jsonl | tail -n 1      the summary document
job cancel JOB --reason '…' --follow           exit 113; "reason": "wrong change window"
config generate | config generate --minimal    199 key lines | 20 lines; to a file 0600, again
                                               config_generate_destination_unavailable, --force writes
daemon start --set daemon.shutdown-idle-timer=1m   msg="stopping: idle" idle=1m58s
                                               key=daemon.shutdown-idle-timer; the socket removed
sudo unshare --mount (tmpfs over /opt, /dev/shm, /etc/tmpfiles.d,
/etc/bash_completion.d): setup shared --group netops; setup tab
                                               /opt/karvi 755, shared and its three trees 2770,
                                               users 1770, /dev/shm/karvi 3770, scoreboards 3770,
                                               capacity 2770, the two files 644
--set watch.directory=/dev/shm/karvi/scoreboards, no scratch root
                                               the scoreboard in BASEDIR/state/scoreboards;
                                               watch --format table shows it; nothing on /dev/shm
```

The first draft of `karvi-setup.1` gave the mode to the three trees alone
and not to `shared` or the scratch root; both are stated now. LOGIN-
TRANSCRIPTS, SSH-HOST-KEY-POLICY, and OPERATIONS "The daemon's idle exit",
"The watch screen", and "Tab completion" say that the pages restate them.

*Found, for later.* (1) A login to the fake ended with `exit` exits 110
(`ExitConnectionFailure`): the fake closes the channel without an exit
status, OpenSSH reports a closed connection, and the login's classifier
takes it as a connection failure; whether an IOS XE device does the same is
a question for the device-qualification track, and the page states no exit
for a login. (2) The full reference configuration does not load: `config
validate` on `config generate`'s output, and on `configs/reference.toml`,
is `config_toml_syntax … table redefined: dispatch`, the renderer starting
`[dispatch]` again where the registry's dispatch rows are interrupted
(`platform-resolution` among them); the released v0.25.0 executable
generates the same.

**The sections, as committed.** The design records (`19b0119`); H
(`da8d049`) the host-key policy and the Dispatch options in the help, the
shape test; K (`c8fbac5`) the Dispatch options as their keys' overrides,
the top help's sentence; E (`599b6f6`) the exit statuses defined once; G
(`166fcd4`) `HelpPages`, `helplayout.Roff`, the page table, the twelve
frames, the install line; P in three parts (`3fa8bde` `karvi.1`, `2059f95`
run, crun, and command, `aa21556` the rest); and the close: README's
documents, the roadmap, and the changelog. Battery: the seventeen suites
on lab builds after K (19:13:59 to 19:17:23 UTC) and at the close
(20:31:10 to 20:34:29), pass; `go test ./...`, vet, gofmt, and `make
generated-clean` at each section.

**Not taken.** One page for every word; pages for the large words alone; section
8 for `karvi-setup`; a page per alias; an OPTIONS region from the entries alone;
the whole page generated; the twelve constants exported; the generator as a test
inside `internal/cli`; a built executable's `--help` as the source; the Host-key
modes kept as a section; a second column width; a pointer to
[SCALE.md](SCALE.md) in place of the Dispatch entries; the command line checking
the keys' ranges itself; clamping with a notice; the shared sections on every
page; the duration's form in each entry; the meanings of the exits written by
hand; the precedence generated.

**Roadmap.** `karvi.1` leaves Next; the reference configuration's doubled
`[dispatch]` table (found in part 3) enters it first. Found and left for
their own issues: `NO_COLOR` read by the watch screen alone; an override's
error naming `command-line` and not the option; a login to the fake ended
with `exit` reporting 110.

## 15. The reference configuration that does not load (2026-10-03)

The roadmap's first item, found while `karvi-config.1` was written ([chapter
14](#14-the-man-page-karvi1-2026-10-03), part 3): `karvi config generate` writes
a file `karvi config validate` refuses.

**What it gains.** A site's documented starting point is `karvi config
generate`, the full reference with every key, its default, and its
comment; it was refused at its first load. It waits on nothing outside the
tree.

**The review.** Against the tree at `3dca45c`, a lab build:

```text
$ karvi config validate configs/reference.toml        (config generate's output, the same bytes)
config_toml_syntax: … TOML line 244 column 1: table redefined: dispatch
the committed reference at 010a963 (the public root), karvi-v0.24.0, karvi-v0.25.0: the same refusal
config generate --minimal, configs/example.toml: load, warnings 0
```

`RenderReference` opened a `[table]` whenever the table changed in the
registry's row order. Two faults follow: the tables whose rows lie apart
are opened more than once (`[dispatch]` twice, `[output]` four times,
`[display.run]` twice), which TOML refuses; and the six top-level keys
(`basedir`, `sharedroot`, `tempdir`, `spooldir`, `freecheck`, `timezone`)
come after `[config]`, silently, until the first fault is gone:

```text
[config] … basedir = "/srv/karvi"   →  config_unknown_key: … config.basedir
```

No test loaded the reference; `generated-clean` compares it with the
generator alone. `configschema/registry_data.go`'s first line said "Code
generated from the fixed-key table by tools/configgen; DO NOT EDIT", where
its rows are written by hand (`7f17885` and before).

A renderer grouping the rows by table, run through `go test -overlay` (the
tree untouched):

```text
config validate grouped.toml                          warnings: 0   (37 tables, none twice; [config] at line 47)
config show --format json, loaded vs defaults alone   177 keys, equal
config show --explain basedir                         source: grouped.toml:6
                      dispatch.shuffle-key            source: grouped.toml:219
                      display.run.border              source: grouped.toml:488
```

**The rule settled.** The operator agreed: the reference is rendered table
by table, the top-level keys first and each table once, in the order of
its first row, with all its rows in registry order; a test in `configload`
loads it and holds every registry key read from its own line at its
default; `configs/reference.toml` regenerated (keys move, no value
changes); the registry's header says the rows are written by hand. Not
taken: reordering the registry (the next row added splits a table again);
dotted keys with no tables; a suite running `config validate` on the
generated file. Built as one section.

**Executed.** The renderer groups the rows (the overlay's code, now in
`internal/configload/render.go`); `configs/reference.toml` regenerated is
the overlay's output byte for byte, its lines the former ones sorted, less
five table headers and their five blank lines (42 headers to 37); the
schema is unchanged. The new test, run through an overlay against the
committed renderer, fails as the review did (`the reference does not
load: … TOML line 244`). On the lab build:

```text
$ karvi config generate site.toml; karvi config validate site.toml
warnings: 0
$ sed -i 's|^basedir = "auto"|basedir = "/srv/karvi"|' site.toml
$ karvi --config site.toml config show --explain basedir
value:      "/srv/karvi"
source:     site.toml:6
```

`go test ./...`, vet, gofmt, `make generated-clean`, and the k03 suite
(the one suite that runs `config generate`), pass. The roadmap's first
item leaves Next.


## 16. `NO_COLOR` (2026-10-03)

Found while `karvi.1`'s ENVIRONMENT was written ([chapter
14](#14-the-man-page-karvi1-2026-10-03), part 1): the watch screen alone reads
`NO_COLOR`.

**What it gains.** `NO_COLOR` is the common convention (no-color.org) by
which a terminal user turns colour off in every program at once; an
operator who set it got plain output from the watch screen and colour from
everything else. It waits on nothing outside the tree.

**The review.** Against the tree at `aded4f3`, a lab build, at a terminal
(`script`, the live screen at 40×120), `display.color` at its default
`auto`, colour sequences counted:

```text
                                   no NO_COLOR   NO_COLOR=1   NO_COLOR=1 + color always
karvi --help                            27            27              27
run --no-daemon / through the daemon     2 / 2         2 / 2           2 / 2
config colors                           24            24              24
karvi-prune --help                      11            11               –
watch (live screen)                     74             0               0   (--color always)
```

Every colour decision is `display.ColorEnabled(mode, theme, terminal)`,
which read no `NO_COLOR`; `watch_command.go` set the theme to `nocolor`
when it was set, which beat an explicit `--color always`. The convention
asks a program that colours by default to honour a non-empty `NO_COLOR`,
and lets a configuration or a per-invocation option override it.

**The rule settled.** The operator agreed: under `display.color = "auto"`,
a non-empty `NO_COLOR` turns colour off, in `display.ColorEnabled`, so
every path follows, `karvi-prune -h` (mode `auto` alone, still reading no
configuration) among them; `always` and `never` are explicit choices it
does not override, watch's `--color always` among them, its own override
removed; the `display.color` row's documentation, DISPLAY-CONFIGURATION,
and `karvi.1`'s ENVIRONMENT say so; a test of `ColorEnabled` per mode with
and without `NO_COLOR`; the matrix again after the change. `config colors`
under `NO_COLOR` shows each role's escape as text and no colour;
`--set display.color=always` shows them. Not taken: `NO_COLOR` over
`always`; `NO_COLOR` as a configuration layer; a variable of karvi's own.
Built as one section.

**Executed.** The rule in `display.ColorEnabled`; watch's own override
removed; the `display.color` row's documentation (the reference and the
schema regenerated); DISPLAY-CONFIGURATION states the rule, which its
`config colors` paragraph had called "the rule above" with no rule above;
`karvi.1`'s ENVIRONMENT entry. The matrix on the new build:

```text
                                   no NO_COLOR   NO_COLOR=1   NO_COLOR=1 + color always
karvi --help                            27             0              27
run --no-daemon / through the daemon     2 / 2         0 / 0           2 / 2
config colors                           24             0              24
karvi-prune --help                      11             0               –
watch (live screen)                    109             0             109   (--color always)
NO_COLOR= (empty), --help               27
```

The test of `ColorEnabled` covers each mode with and without `NO_COLOR`;
`go test ./...` passes with `NO_COLOR=1` in the environment as well, so no
test depends on the host's setting. Battery: the seventeen suites on a lab
build (21:03:02 to 21:06:20 UTC), pass; vet, gofmt, and `make
generated-clean`.


## 17. An override's error names its option (2026-10-03)

Found while section K was built ([chapter
14](#14-the-man-page-karvi1-2026-10-03), item 6): an option's value refused by
its key's range or lock is reported `at command-line`.

**What it gains.** An operator whose option is refused learns which option
it was. It waits on nothing outside the tree.

**The review.** Against the tree at `4eb961c`, a lab build:

```text
run … --blind-wait 20m           … must be 0s..10m for execution.blind-wait at command-line
run … --dw --wave-delay 2h       … must be 0s..1h for dispatch.wave-gate-timed-delay at command-line
run … --dp --workers 5000        … must be 0..4096 for dispatch.parallel-workers at command-line
--ipv4 config show --explain name.address-family-preference      source: command-line
under a global lock (sudo unshare --mount, a tmpfs over /opt):
run … --blind-wait 9s                  write blocked by lock "execution.blind-wait" … at command-line
run … --continue-device-on-error       write blocked by lock "execution.halt-device-on-command-error"
                                       … for execution.halt-device-on-command-error at command-line
--timezone Mars/Olympus …        display_timezone_invalid: … for display.timestamp at <builtin>
```

The flag layer recorded one source, `command-line`, for the 21 options
that set a key (`root.go`, `work_commands.go`, `activity.go`), where
`--set` names itself (`--set[2]`) and a file its line. The timezone is
checked inside the timestamp formatter's check, and its failure was
reported against `display.timestamp` and that key's source.

**The rule settled.** The operator agreed: the flag layer carries each
value with its option, named by its long name (`--fs` implying `--cd=.`
is `--fs`; `--dp` is `--dispatch`; `--continue-device-on-error` names
itself), in every message and in `config show`; an unknown zone is
reported against `timezone` at its source; tests for an option's source,
a lock's refusal, `--continue-device-on-error`, and `--timezone`. Not
taken: a second map of option names; each message in the option's words;
both sides of a cross-key check; `--dp` naming itself. Built as one
section.

**Executed.** `configload.FlagValue` carries each value with its option, and
the cli layer's writers go through one helper, `setKey`, with the option's
long name (`longName`); `--continue-device-on-error`'s write moved from
`prepareConfig` (which loses its parameter) to the command line,
`continueOptions`, where the operator's option and the `crun` word are told
apart: a `crun` under a lock of `execution.halt-device-on-command-error`
names `crun`, as it never typed the option. The zone's failure is the
`timezone` key's. On the lab build:

```text
run … --blind-wait 20m           … must be 0s..10m for execution.blind-wait at --blind-wait
run … --dw --wave-delay 2h       … must be 0s..1h for dispatch.wave-gate-timed-delay at --wave-delay
run … --dp --workers 5000        … must be 0..4096 for dispatch.parallel-workers at --workers
--timezone Mars/Olympus …        display_timezone_invalid: … for timezone at --timezone
KARVI__TIMEZONE=Mars/Olympus     … for timezone at KARVI__TIMEZONE
timezone = "Mars/Olympus" in a file   … for timezone at …/tz.toml:1
--ipv4 config show --explain name.address-family-preference      source: --ipv4
under the global lock:
run … --blind-wait 9s                  … for execution.blind-wait at --blind-wait
run … --continue-device-on-error       … for execution.halt-device-on-command-error at --continue-device-on-error
crun …                                 … for execution.halt-device-on-command-error at crun
```

Tests: an option's source in the snapshot, a range error, a lock's
refusal, and the zone (`configload`); the global options,
`--continue-device-on-error`, `crun`, a run without the option, and every
Dispatch option by its long name, `--dw` as `--dispatch` (`cli`). Battery:
the seventeen suites on a lab build (21:27:14 to 21:30:36 UTC), pass;
`go test ./...`, vet, gofmt, and `make generated-clean`.


## 18. How a login ends (2026-10-03)

Found while `karvi-login.1` was written ([chapter
14](#14-the-man-page-karvi1-2026-10-03), part 3): a login to the fake ended with
`exit` exits 110.

**What it gains.** A login the operator ended normally is not recorded as
a failure in its exit, its transcript's metadata, its audit record, and
the watch screen. It waits on one fact outside the tree: whether IOS XE
sends an exit status when a session ends with `exit`.

**The review.** Against the tree at `f275725`, a lab build, the fake, and
OpenSSH 9.6p1:

```text
ssh (plain) … then exit                    "Connection to 127.0.0.1 closed."   SSH_EXIT=255
karvi --debug login r1 … then exit         system SSH interactive session failed code=ssh_process_failed
                                           ssh_process_failed: exit status 255          KARVI_EXIT=110
ssh -E log -o LogLevel=VERBOSE:
  1 exit typed                    255   Authenticated to …; Transferred: sent 2364, received 1516 bytes
  2 the device's process killed   255   Authenticated to …; Transferred: sent 2316, received 1396 bytes
  3 nothing listening             255   connect to host 127.0.0.1 port 1: Connection refused
```

The fake sends no exit status when the session ends; OpenSSH treats a
channel closed so as 255; the login's classifier maps an unexplained 255
to `ExitConnectionFailure`. A device closing the session on `exit` and a
device dying mid-session are the same to the client, OpenSSH's own
verbose log included; only a failure before authentication differs. The
interactive login runs `ssh` at `LogLevel ERROR`, and the runbook had no
login row.

**The rule settled.** The operator agreed: a login's normal end is
qualified on the devices first. Row D15 (by hand) records plain `ssh`'s
exit after `exit` and `karvi login`'s, with the transcript's
classification. If the device sends a status, the fake learns to send one
and karvi is unchanged; if it closes without one, an interactive login
that authenticated and ended with the device closing the session is a
completed session, exit 0, with the notice `login_closed_without_status`,
"authenticated" read from OpenSSH's log (`-E` at `VERBOSE`), a device dying
mid-session ending 0 too; no code change now. Not taken: deciding without
the device; every 255 a success; the fake changed now.

**Executed.** The runbook's D15 (the steps, the evidence `D15/`, the account's
row in [section 1](DEVICE-QUALIFICATION-RUNBOOK.md#1-what-the-run-needs)), the
qualification matrix's session line, and the roadmap's device-qualification item
5 with the rule; DESIGN's entry in [section
5](DESIGN.md#5-transports-and-the-device-session). The operator then asked that
the instructions carry every point of the review: D15 now keeps OpenSSH's own
log at `VERBOSE` in a file (the lines the rule reads), karvi's debug stream in
`karvi.err`, a scratch trust store, the reading of each file, the fake's figures
for comparison, the line for `results.tsv`, and both laboratory devices. Its
steps, executed against the fake under `script`:

```text
ssh -tt -E ./ssh-verbose.log -o LogLevel=VERBOSE … ; exit     ssh.exit 255
ssh-verbose.log        Authenticated to 127.0.0.1 …; Transferred: sent 3380, received 2756 bytes
karvi --debug login --record=./transcripts … 2>karvi.err      karvi.exit 110, the session usable at the terminal
karvi.err              code=ssh_process_failed; ssh_process_failed: exit status 255
transcripts/…meta.jsonl  "exit_classification":"ExitConnectionFailure"
```

## 19. `errors.jsonl` (2026-10-03)

The operator's word: the job folder's file of device and command failure
details is `errors.jsonl`, where it was `failures.jsonl`, in the code,
the documentation, and the manual pages.

**What it gains.** The file's name says what an operator opens it for:
the errors of the job, device and command alike. Nothing waits on it.

**The rule.** One name per file, carried by everything shaped after it:
the file `errors.jsonl`, its switch `output.files.errors-jsonl`
(`KARVI__OUTPUT__FILES__ERRORS_JSONL`, `Since` 0.26.0), the execution
plan's `output.files.errors_jsonl`, the summary's `paths.errors_jsonl`,
and the Go names (`ErrorsJSONL`). The lists ordered by name take the new
name's place (before `failed-devices`). The old key is refused from every
layer with `config_key_removed`, as every removed key is, its hint naming
the new one. The registry stays 24 and the plan schema 10: both are
unreleased since v0.25.0 (registry 22, plan 9), so the rename rides on
their numbers; the plan's pinned digests moved. Not taken: the old key
kept as an alias (no backwards compatibility); the key left as
`failures-jsonl` beside a file named `errors.jsonl` (each key is shaped
like its file's name); a plan schema bump for an unreleased number. Left
as they were: the specification in the archive and the v0.24.0 release
evidence (`release/evidence/default-config.toml`), which record their
releases.

**Executed.** Against a lab build of the tree, the fake, and the
unreachable `dead`:

```text
$ karvi run --no-daemon --target r1 --target dead --cmd 'show bogus' --cmd 'show clock'   # exit 101
$ ls …/261003-182508-00
commands.jsonl commands.txt errors.jsonl failed-devices.txt manifest.json metrics.json output.dead.txt output.r1.txt summary.json
$ jq -c '{device: .device.name, command, status, code: .error.code}' errors.jsonl
{"device":"r1","command":"show bogus","status":"device_error","code":"device_command_error"}
{"device":"r1","command":"show clock","status":"not_attempted_prior_command_failure","code":null}
{"device":"dead","command":"show bogus","status":"connection_error","code":"native_session_open_failed"}
{"device":"dead","command":"show clock","status":"not_attempted_prior_command_failure","code":null}
$ jq -c '.paths | keys' summary.json
["commands_jsonl","commands_txt","errors_jsonl","failed_devices","manifest","metrics","summary"]
$ karvi --set output.files.errors-jsonl=false run --no-daemon --target r1 --cmd 'show bogus'   # no errors.jsonl
commands.jsonl commands.txt failed-devices.txt manifest.json metrics.json output.r1.txt summary.json
$ karvi --set output.files.failures-jsonl=false config show                                  # exit 2
config_key_removed: removed in v0.26.0; the file is errors.jsonl now; its switch is output.files.errors-jsonl for output.files.failures-jsonl at --set[2]
```

The environment variable and a file's `[output.files] failures-jsonl` are
refused the same way, at `KARVI__OUTPUT__FILES__FAILURES_JSONL` and at the
file's line. Changed: `internal/output` (the file and the store's handle),
`executionplan`, `internal/planner`, `internal/jobexec`, the registry row and
the removed-key table, the plan schema, the generated reference and
configuration schema, the native and k03 suites, OPERATIONS, DESIGN,
ARCHITECTURE, the runbook, `karvi-run.1`, and [chapter
12](#12---cd-and---fs-for-run-and-command-2026-10-03)'s listing.

## 20. The documentation as HTML: the converter and the links (2026-10-03)

The roadmap's Next item 1, on the operator's word: a script that converts
the documentation to HTML with an index and a left column of links, the
output a build product.

**What it gains.** The documents read in a browser on any host, a site's
intranet or a laptop without GitHub or a Markdown viewer. The package's
contents (Next 2) wait on it, to install the Markdown or the HTML under
`/usr/share/doc/karvi`. It waits on nothing.

**The review.** Against the tree at `f456a0f`: 32 tracked Markdown files
outside `vendor/` (seven at the root, 21 in `docs/`, `examples/README.md`,
three in `release/`), 323 headings, about 1,100 table lines, raw HTML only in
the README's banner `<img>` and ERROR-CODES' generated comment, and no
Markdown link at all: a document cited another as a code span,
`` `docs/OPERATIONS.md` `` (166 times), 25 of them with a section (a quoted
heading, "section N", "§N"), beside a few bare names in prose. The host has
pandoc and no Python Markdown; no vendored module converts Markdown.
goldmark v1.8.6 with its GitHub extension, in a scratch module, converted
all 28 root and `docs/` documents:

```text
CREDENTIAL-CSV.md   tables=11  rows=87   (10 by a grep for separators: one table is indented in a list item)
SCALE.md            tables=8   rows=51   (7 by the grep: the CPU band's table is in a list item)
ERROR-CODES.md      tables=6   rows=713
…                   no paragraph begins with "|", no "**" left unrendered
```

**The converter, agreed.** goldmark vendored (MIT, pure Go, no
dependencies of its own, about 13,700 lines), used by a command under
`tools/` alone, never by a shipped executable. Not taken: pandoc (not a Go
build dependency, its output varies by version); a hand-written converter
(tables in list items, fenced code in lists); the HTML committed. A module
nothing imports is dropped by `go mod tidy`, so goldmark enters with the
converter's section.

**The scope, agreed.** Every tracked Markdown file; `vendor/`'s five are the
modules' own and stay out.

**The links, agreed.** The operator asked that the span references become proper
links and that the documentation cross-reference where it applies. The rule
([DESIGN section 16](DESIGN.md#16-build-verification-and-release)): a reference
to a document is a relative link with the span as its text; a name with a
section links to that heading's GitHub id, the qualifier inside the link; a
numbered section or chapter links to its heading; fenced code, a document naming
itself, a manual section (`karvi-setup` in section 8), and the archive
(`REMOVED-LOG.md`) stay text. Not taken: links added by the converter alone;
every key and error code linked to its entry.

**Executed.** A script turned 112 paragraphs' references into links (a span may
cross a line, so it worked by paragraph), then 38 numbered references by
hand-checked list: each a document's own (COLLECTION's, CREDENTIAL-CSV's,
EXAMPLES' chapters) or another's (OPERATIONS' table of CREDENTIAL-CSV sections,
ARCHITECTURE's "section 11", DOWNLOADS' "§10" of BUILD-HOWTO, [chapter
18](#18-how-a-login-ends-2026-10-03)'s DESIGN "section 5" and runbook "section
1"). A check resolves every relative link to a tracked file and every anchor to
a heading id:

```text
214 relative links, 0 unresolved
README.md with #upgrade made #upgrades, docs/SCALE.md made SCALES.md:
BAD anchor README.md docs/OPERATIONS.md#upgrades
BAD file README.md docs/SCALE.md
214 relative links, 2 unresolved
```

A section named in quotes after its document's link was a second form the
first pass had not taken (SCALE's `docs/OPERATIONS.md` ("The job's output
files", "The spool directory", …)): 12 more, each linked to its heading,
ROADMAP's "Next" and "Later" among them.

**The width.** The links had lengthened lines past the width the documents
keep (DESIGN's 99th percentile is 80 columns, OPERATIONS' 79): prose lines
over 80, outside code, tables, and HTML, were 70 in 15 files before and 213
in 30 after. On the operator's word, every paragraph and list item holding a
line over 80 is rewrapped to 80, those from before the links included: the
list item's indentation kept, a link's target never broken, no line begun
with a word Markdown would read as a list marker, heading, or quote, and a
paragraph with a hard break or a code span holding two spaces left (none
was). ERROR-CODES' one long line is rewrapped in its generator
(`internal/errorcodes`). pandoc rendered every file before and after the
rewrap; the two are equal with whitespace collapsed, and a wrap made to open
a list in a copy is told:

```text
rewrapped units: 183, then 7 after the quoted titles
render check: 31 files equal; SCALE.md with "\n- the" made: differs
prose lines over 80: 70 before the links, 213 after, 8 now
relative links: 229, 0 unresolved
```

The eight are four headings (chapters 3, 7, 9, and 10 here; a heading is
one line, and its words are its anchor) and four lines that are one link
longer than the line (chapter 7's anchor, and three document links at
their indentation). The checks become the converter's own in its section,
the links against the ids the HTML carries.

**The converter's first decisions, agreed.** With the links pushed
(`5027666`), GitHub's Markdown API (`POST /markdown`, mode `markdown`)
rendered the 32 documents: its 304 heading ids equal the rule the links
were made by, every one. goldmark's own ids differ in 6, an underscore
made a hyphen (`31-cisco_iosxe-…`, chapter 16's `16-no_color-2026-10-03`),
so the converter gives the headings GitHub's ids itself, pinned by a table
of awkward headings with the ids the API gave. The command is
`tools/md-to-html`; its output is the tree's parent's `html/`
(`/home/netops/karvi/html` beside the tree `/home/netops/karvi/karvi`),
never the tree; the pages mirror the tree (`html/docs/OPERATIONS.html`), so
a relative link means the same in both with `.md` made `.html`; images go to
`html/images/`; the main page is an index of its own, not the README, and
carries the README's image.

**The image.** It was a 2,498,962-byte PNG (1733 × 907) on GitHub's
attachment storage alone. A page that fetches it from there breaks offline
and asks GitHub at every reading, so its bytes are in the tree:
`images/karvi-viking-fleet-command.png`, as GitHub served them, mirrored to
`html/images/`, and the README's `src` is that relative path, which GitHub
renders the same. The source bundle packages the tree, so it carries the
image (the v0.25.0 bundle was 12,655,199 bytes). Not taken: fetching it at
build time (the network); the remote URL kept in the HTML; re-encoding the
artwork; `docs/images/`, whose paths would be rewritten on the way out.

**The rest, agreed.** The index's content (the image, the name, the README's
opening paragraph, the 32 documents in six groups with a line each from a
table in the tool, which refuses a tree whose Markdown files are not its
own); the page (the left column with every document and the current page's
sections, folded above the page on a narrow screen, one stylesheet, system
fonts, a dark set, no script, the README's `<img>` let through, a `#` link
on each heading, a footer naming the source and the commit); the
replacement (built and checked beside `html/`, then renamed into place;
only a missing, empty, or marked `html/` is replaced); the check (every
link of the written pages, the index and the column included, a file of
the site and an id on its page, in `go test` too). The operator added:
every link and image relative, so the directory can be copied anywhere,
`index.html` the way into the documents and `images/`, and an `index.html`
in `images/` so a server lists no files.

**Executed.** goldmark v1.8.6 vendored (`vendor/github.com/yuin`, 12,840
lines; `go list -deps ./cmd/...` names none of it); `tools/md-to-html`
built, then `make html`:

```text
md-to-html: 34 pages and 3 other files in ../html
the heading ids of 32 pages against the 304 GitHub's API gave: 0 files differ
a second run: html/ a new directory (inode 2384004 to 2384047), a stale page gone,
  no html.new-* or html.old-* beside it
HTMLDIR=…/foreign (a file of its own, no marker):
  md-to-html: …/foreign holds files and was not made by md-to-html (no
  .md-to-html); it is not replaced: move it away or name another -out
```

Its tests pin the ids to GitHub's for 17 headings (underscores, code
spans, emphasis, a link, accents, a tab, three repeats and a "Repeated 1");
show the check reporting a missing anchor, a missing page, a missing image,
an absolute path, a file URL, a link out of the site, a link to a Markdown
file, a missing same-page anchor, and a repeated id; and show the
replacement refusing a foreign directory and a symbolic link, using an
empty one at 0755, and leaving the site as it was after a failed check. Made
to fail, they did: the id rule with an underscore made a hyphen
(`` "16. `NO_COLOR` (2026-10-03)": id "16-no-color-2026-10-03", GitHub's
"16-no_color-2026-10-03" ``), and SCALE's link to OPERATIONS' "Retention"
broken (`docs/SCALE.html: href "OPERATIONS.html#retentions" names no
heading of docs/OPERATIONS.html`). The roadmap's item is closed; the
package's contents (now its Next item 1) choose between the Markdown and
this HTML.

**Every directory a page.** The operator then asked that each directory of
the site below its root hold a minimal `index.html` sending the browser to
the parent, by a refresh and a short script in the head, with a blank body
for a crawler, so no server lists a directory and no browser shows an
error; `images/`' page, which had linked back, became the same. The target
is the parent's `index.html`, not the bare directory: a copy opened from
the disk (`file://`) sent to `../` shows the browser's own listing of it.
The page is also `noindex`. The tool makes one for every directory its
files fall in (a deeper one climbs a level at a time), refuses to put one
over a document's page named `index.html`, and its check reads the
refresh's target as a link. The index's footer link to `images/`, now a
way back to the index, was removed; the index shows the image.

```text
md-to-html: 37 pages and 3 other files in ../html
docs/, examples/, release/, images/: the same index.html, byte for byte:
  <meta http-equiv="refresh" content="0; url=../index.html">
  <meta name="robots" content="noindex">
  <script>location.replace("../index.html");</script>
  <body><p>&nbsp;</p></body>
```

## 21. The release 0.26.0, and `LICENSE.md` (2026-10-04)

The third public release, on the operator's word, carrying chapters 9 to 20;
before it, the licence made a document of the site. The release date is the
build identity's UTC date: the session ran on the evening of 2026-10-03 in the
operator's zone and past midnight in UTC.

**What it gains.** A site installs the shared scratch root, the prompts' line
editing, `--cd` and `--fs`, the manual pages, and the rest of chapters 9 to 20
from a published artifact, with the documentation and its licence readable as
HTML beside the tree.

**The licence, as done.** The operator asked for `LICENSE.md` in the HTML and a
link to it from README's License section. `LICENSE` was renamed `LICENSE.md`,
not copied: two copies of one text would drift, and GitHub reads
`LICENSE.md` as the licence as it read `LICENSE`. The converter titles a page
from its first top-level heading, so the text's first line, `MIT License`,
became `# MIT License`; the rest is the text unchanged. Its row in
`tools/md-to-html/docs.go` is in "The project", README's line is ``MIT. See
[`LICENSE.md`](LICENSE.md).``, and `NOTICE` names it. Nothing else named the
file: the debian `copyright` carries the text itself, and no release tool
copies it.

**The sequence, as run**, 51 minutes 31 seconds from the baseline's start to
the artifacts' end, about 27 of them the stop for the operator's review:

| Step | Wall (UTC) | Result |
|---|---|---|
| the baseline on a clean clone of `dev` at `3ab5a74` | 00:28:36 to 00:34:51 | gofmt, make, the release verifier, exit 0 each |
| the number in its eight places; the release-identity build | 00:35 | `BUILD_TIME=2026-10-04T00:00:00Z` given to every tool; the changelog's Unreleased block became the release's, under a lead paragraph naming what a site has to change |
| the compatibility example | 00:35:35 | the released v0.25.0 executable, copied out of `bin/` and checked against `CHECKSUMS.sha256` before the rebuild, started its daemon; this client read `compatible: false` on the version alone, its run was refused with `daemon_incompatible` (exit 112) before any job, the client's `daemon stop` ended it |
| 1/3 | 00:35:42 | `ee089d5` |
| the core evidence | 00:35:46 to 00:38:35 | 783 named tests across 79 packages, vet 0; the socket tests at a 25-byte `TMPDIR` exit 0, at 145 bytes **exit 1**: the release stopped for the operator |
| the fix, on the operator's word | 01:06 | `d4b89d1`, below |
| the core evidence again | to 01:08:01 | the same counts, both socket lengths exit 0 |
| the documents | 01:09:16 | `f1f1b6b` (2/3): `release/` from the v0.25.0 pattern; README's counter line, stale since registry 24 |
| the remaining evidence | 01:09:20 to 01:15:15 | the shipped checks exit 0, the release verifier exit 0, the checksums unchanged by its rebuild; the replay skipped |
| 3/3, the tag, `main` | 01:15:31 | `72bf32d`, `karvi-v0.26.0`, `main` at the tag |
| the artifacts | 01:15:34 to 01:20:07 | the bundle reproducible byte for byte and verified from its own archive; 15,449,238 bytes, 2.8 MB more than v0.25.0's for goldmark's vendored tree and `images/` |
| the push and the GitHub release | 01:33 | `dev`, `main` (a fast-forward from `39b2831`, the README image commit), and the tag pushed; release `karvi-v0.26.0` with the three assets, marked latest; the asset downloaded back matches |

**The socket-length failure.** Four `internal/cli` tests and sixteen
`internal/daemon` tests failed at the 145-byte `TMPDIR` with
`askpass_start_failed` (`listen unix …/askpass-….sock: bind: invalid argument`)
for a socket under `/tmp/yyy…/karvi-test-scratch-N/netops/`, its path past the
kernel's limit. [Chapter
9](#9-the-scratch-root-shared-the-capacity-ledger-the-roots-creation-and-setup-shared-2026-10-03)
(`fd5acf3`) had given each test binary a scratch root of its own, made by
`os.MkdirTemp("", …)` and so under `TMPDIR`, where the tests had used the host's
`/dev/shm/karvi`; its comment called the root short, which a long `TMPDIR` makes
false. The executables were not touched by it: their `tempdir` chain
(`/dev/shm/karvi/<user>`, `<basedir>/tmp`, `/tmp/karvi-<uid>`,
`/var/tmp/karvi-<uid>`) never reads `TMPDIR`. The release stopped at the
finding, as the operator had asked of any error; a trial on the tree, then
reverted, showed the four socket packages passing at 145 bytes with the root
made under `/tmp`, and on his word that change was committed between 1/3 and
2/3. The executables and `CHECKSUMS.sha256` did not move, since the package is a
test helper.

**Executed.** The finding, the trial, and the published state:

```text
$ grep -E '^\[TMPDIR|^exit=' socket-tmpdir-qualification.log    # before d4b89d1
[TMPDIR length 25]
exit=0
[TMPDIR length 145]
exit=1
$ TMPDIR=$d go test -count=1 -mod=vendor ./internal/askpass ./internal/cli ./internal/ipc ./internal/daemon    # ${#d} = 145, the root under /tmp
ok  	github.com/robert-patrick-texas/karvi/internal/askpass	0.008s
ok  	github.com/robert-patrick-texas/karvi/internal/cli	13.947s
ok  	github.com/robert-patrick-texas/karvi/internal/ipc	0.056s
ok  	github.com/robert-patrick-texas/karvi/internal/daemon	26.073s
$ gh api repos/robert-patrick-texas/karvi/releases/latest --jq '"latest: \(.tag_name) draft=\(.draft) prerelease=\(.prerelease)"'
latest: karvi-v0.26.0 draft=false prerelease=false
$ curl -sL .../karvi-v0.26.0-source-linux-amd64.tar.gz | sha256sum | cut -c1-16; cut -c1-16 karvi-v0.26.0-source-linux-amd64.tar.gz.sha256
83408fcf77d2531b
83408fcf77d2531b
$ gh api repos/robert-patrick-texas/karvi/license --jq '"\(.path) \(.license.spdx_id)"'
LICENSE.md MIT
```

**Found on the way.** The compatibility example's script, rewritten from
[chapter 6](#6-the-first-public-release-0240-2026-09-30)'s shape, first ran
without making the base directory, and every command answered
`base_directory_unavailable` (exit 9); the script now makes it at 0700 before
the daemon starts. README's counter line still read registry 22 and plan 9 after
both had moved; the 2/3 sweep settled it, as it settles the release documents.
The suites' count is still seventeen.

**Not taken.** A copy of the licence beside `LICENSE`. A patch number for the
socket fix, or rebuilding 1/3 to put the fix before it: the fix is a test
helper's, the executables did not change, and the history shows where it was
found. Making the long-`TMPDIR` run optional: it is the evidence that every
Unix socket the tests open stays within the limit.

**Roadmap.** The package's contents, the roadmap's first item.

## 22. `docs/FILES.md`: every directory and file, shared and individual (2026-10-04)

The operator asked what `server.json` and `server.lock` under
`/dev/shm/karvi/capacity` are, whether there is one pair for the host or one
per operator, and what their permissions should be; then for one document
listing every directory and file karvi uses in shared and in individual mode,
with mode, owner, and group, and how karvi chooses the mode, or advice if such
a document already existed.

**What it gains.** An operator or an administrator looks up any path karvi
made, or should have made, in one place, and tells from a host's directories
which mode each place is in, without reading the code or six guides.

**The review.** The facts were spread: the shared trees, the scratch root, and
the private roots in [`docs/OPERATIONS.md`](OPERATIONS.md); the job's files
and the spool there too; the sockets and the state modes in
[`docs/ARCHITECTURE.md`](ARCHITECTURE.md); the storage layout in
[`docs/SCALE.md`](SCALE.md); the credential files' rules in
[`docs/CREDENTIAL-CSV.md`](CREDENTIAL-CSV.md); and a FILES list of nine
entries in `karvi.1`. None gave a file's owner and group, and none stated the
selection as one rule. A new document under "Reference", beside
[`docs/ERROR-CODES.md`](ERROR-CODES.md), was the answer, linked from README
and from OPERATIONS' shared trees.

**The rule, as the document states it.** There is no shared switch: each place
is chosen at each activity, its shared candidate first. A missing candidate is
passed by silently; one present but unusable is refused for the private root
and the three trees (the site made them for the operator, and a fall-through
would split the work), and passed by with one warning for the scratch root's
`scoreboards` and `capacity`. The spool, the trust store, and the daemon's
sockets are never shared. The operator's home is the password database's, not
`$HOME`.

**Executed.** The modes were taken from runs, not from the code: a lab build
of the four executables under `/var/tmp/nd.thgz` and one script as root in a
private mount namespace (`sudo -n unshare --mount --propagation private`),
with a tmpfs over `/opt`, `/dev/shm`, `/tmp`, `/etc/tmpfiles.d`, and
`/etc/bash_completion.d`. Each mode ran a `command`, a daemon `run`, a `crun`,
a `login --record`, and a system-transport `run` held three seconds, listed
during it; shared mode followed `karvi setup shared --group netops`, and the
second operator, `netops.test` (in the group `netops`), ran a job and a
collection after the first. The ledger after both:

```text
-rw-rw---- netops:netops 0 /dev/shm/karvi/capacity/server.lock
-rw-rw---- netops.test:netops 3 /dev/shm/karvi/capacity/server.json
drwxr-x--- netops:netops 140 /opt/karvi/users/netops
drwxr-x--- netops.test:netops.test 140 /opt/karvi/users/netops.test
drwx--S--- netops.test:netops 60 /dev/shm/karvi/netops.test
drwx--S--- netops.test:netops 40 /dev/shm/karvi/netops.test/sockets
```

One pair for the host, both `0660` in the operators' group: `server.json` is
rewritten through a temporary file and renamed, so it takes the last writer as
its owner, while `server.lock` is never replaced and keeps its creator. An
operator's private root under `users` is in the operator's primary group (the
`users` directory has the sticky bit, not setgid), and the operator's scratch
folder inherits the setgid bit and the operators' group from
`/dev/shm/karvi`.

**Found on the way.** Three things:

1. **The lab reached the real homes.** The first run set `HOME` to a lab
   directory; karvi takes the home from the password database, so the
   individual-mode activities made `/home/netops/.local/share/karvi` (jobs, a
   trust store, the daemon's log) and the shared-mode trust store made
   `/home/netops.test/.local`, neither of which existed before (their birth
   times were the run's). Both were listed and copied into the lab directory;
   the removal was refused by the session's permission check and left to the
   operator, who removed them. The later runs mounted a tmpfs over
   `/home/netops/.local/share` and `/home/netops.test` inside the namespace,
   so the defaults acted as they do and nothing reached the disk; a lab that
   needs the default places must cover the account's real home, since `HOME`
   moves nothing.
2. **`ssh.control-path-root` has no use.** Its directory is made at every
   activity and stays empty: the system transport runs `ssh` with
   `ControlMaster no` and `ControlPath none`, and the factory's `ControlRoot`
   field is read by nothing.
3. **`logging.file` and `logging.level` have no use.** Both are validated, and
   `logging.file-required` requires a path, but nothing opens the file or reads
   the level.

The document states both as they are ([`docs/FILES.md`, section
5](FILES.md#5-places-made-and-not-used)); what to do with them waits on the
operator's word.

**Not taken.** A section of OPERATIONS in place of a document: the guide
explains how to operate, and an index of paths is looked up, not read. Modes
written from the code alone: the inherited setgid bits, the ledger's owners,
and the private root's group are what the kernel and the order of the writers
make, and only a run shows them. A longer FILES section in `karvi.1`: the man
page names the places, and the document holds the table.

**Roadmap.** The two settings with no use, removed or given their use, one
issue at a time.

## 23. The trust store under `basedir` (2026-10-04)

The operator asked whether the trust store is always the `known_hosts` file,
whether it is always made under the home in both modes, and what it takes to
put it in the operator's directory under `/opt/karvi/users` on a shared host;
then for the design of the first remedy offered, `auto` preferring
`<basedir>/known_hosts`.

**What it gains.** In shared mode an operator's private state already lives
under `/opt/karvi/users/<user>`: the daemon's sockets, state, and log, and the
fallbacks of the scoreboards and the ledger. The trust store was the one piece
left in the home. Under `basedir` the site sees, backs up, and for the `secure`
policy provisions every operator's store where it keeps the operators' roots,
and the home holds nothing of karvi's but the configuration. It waits on
nothing.

**The answers, as found.** The store is one file in OpenSSH's format, the only
one either transport trusts: the system transport's `ssh` configuration sets
`UserKnownHostsFile` to it and `GlobalKnownHostsFile` to `/dev/null`, and the
native handshake checks the same file; under `insecure` nothing is trusted and
the store is read only to warn of a change. Under `auto` it was
`~/.local/share/karvi/known_hosts` in both modes, `basedir` notwithstanding,
and it is made only under `accept-new` (at the first SSH activity), never
under `secure` (absent is `host_key_not_enrolled`) or `insecure`. A per-user
path took each operator's own setting, since `~` and a relative path resolve
against the home and the key has no `<user>` placeholder, and one store for
every operator is refused by the owner checks. Executed in a private mount
namespace after `setup shared`, the home under a tmpfs:

```text
[1] KARVI__SSH__KNOWN_HOSTS_FILE in the operator's environment; a daemon run
exit=0
drwxr-x--- netops:netops /opt/karvi/users/netops
-rw------- netops:netops /opt/karvi/users/netops/known_hosts
the home's karvi folder:
[2] one store for every operator in the shared tree (root's, 0660)
host_key_directory_owner: command failed for name:fake
```

and, before any setting, chapter 22's shared-mode run had left the second
operator's store alone in the home while its `basedir` was
`/opt/karvi/users/netops.test`.

**Issue 1, the rule, agreed.** Under `ssh.known-hosts-file = "auto"` the trust
store is `<basedir>/known_hosts`, `basedir` resolved as for every activity:
`/opt/karvi/users/<user>` or `/var/lib/karvi/users/<user>` where the site made
`users`, else `~/.local/share/karvi`, or an explicit `basedir` as given. An
explicit `ssh.known-hosts-file` is unchanged, `~` still the home. In
individual mode the file is the same as before. The directory rule stands
(operator-owned, not writable by group or others): `users/<user>` at 0750 and
the XDG root at 0750 pass, and a site-made `basedir` writable by its group is
refused for the store as it was when named explicitly. The execution policy
records the configured word, not the path, so no plan, digest, or schema
moves. `hostkey.Resolve` takes `basedir` for `auto`, the home only for `~`;
its callers (the two transports, login, the exercise) pass it.

**Issue 2, a store left in the home, settled by the operator.** Nothing is
done about it: no refusal, no move, no fallback. The operator removes the
earlier stores on the production host, installs the release, and lets
`accept-new` enroll the devices afresh, which this stage accepts; breaking
changes are not a cost until the operator says so.

**Issue 3, finding the path, agreed.** `karvi config show --explain basedir`
and `karvi config show --explain ssh.known-hosts-file` each add `resolved:
PATH`, the path the next activity would use, found by the same rule without
creating anything (the `basedir` resolver makes the folder it picks, and the
store's resolver creates the file under `accept-new`, so each gets a twin that
only names the path, the candidates shared). The view had nothing to show:

```text
$ karvi config show --explain basedir
key:        basedir
value:      "auto"
source:     <builtin>
default:    "auto"
...
```

The enrollment recipe of
[`docs/SSH-HOST-KEY-POLICY.md`](SSH-HOST-KEY-POLICY.md) takes its path from
the line:

```bash
STORE=$(karvi config show --explain ssh.known-hosts-file | sed -n 's/^resolved: *//p')
```

Limited to the two keys; the same line for every other place key (`tempdir`,
`spooldir`, the three trees, the scoreboards, the ledger, the daemon's socket)
is a roadmap item, so that an operator reads from the host itself each place
[`docs/FILES.md`](FILES.md) describes.

**Not taken.** A `<user>` placeholder in `ssh.known-hosts-file`, set once in
the global file: it would leave each site to write the rule `basedir` already
applies. A new command word for paths. The rule repeated in shell in each
site's scripts. A refusal, a move, or a fallback for a store an earlier
release left in the home (issue 2).

**Status.** Designed; built in sections: the code (the rule, the callers, the
non-creating twins, the `resolved:` line, the registry's text), the documents
([`docs/FILES.md`](FILES.md), the host-key guide and its recipe, SECURITY,
BUILD-HOWTO, README, the manual pages, the changelog), and the verification.
