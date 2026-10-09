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

The document states both as they are (`docs/FILES.md`, a section removed with
the keys in chapter 39); what to do with them waits on the operator's word.

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

**Built**, each section committed on the operator's word: the design
(`c17548b`); the code (`8923c02`): `osutil.chooseBaseDir` holds the private
root's rule, read-only, with `ResolveBaseDir` making what it picks and
`BaseDirPath` only naming it; `hostkey.Resolve` takes `basedir` and
`hostkey.StorePath` names the store; the two transports, login, and the
exercise pass `basedir`; `Snapshot.Explain` takes a `Resolver` from the cli
layer, which answers for the two keys alone; the registry's text follows;
`host_key_trust_store_candidates_exhausted` is retired at v0.27.0, one
candidate being left; the documents and the suites (`61472e7`): the host-key
guide's table of places and its recipe on the `resolved:` line, SECURITY's
and BUILD-HOWTO's own outdated recipes replaced by a pointer to it, the two
manual pages, `scripts/lib/host.sh`'s `host_own_store` and
`host_store_digest` in place of four copies of the guard, and the host-key
suite's first run under a real `auto`.

**Executed.** On a lab build in a private mount namespace, the home under a
tmpfs, individual mode first and then after `setup shared`:

```text
== individual: before any activity
resolved:   /home/netops/.local/share/karvi
resolved:   /home/netops/.local/share/karvi/known_hosts
  ls: cannot access '/home/netops/.local/share/karvi': No such file or directory
run exit=0
  -rw------- netops:netops /home/netops/.local/share/karvi/known_hosts
== shared: before any activity
resolved:   /opt/karvi/users/netops
resolved:   /opt/karvi/users/netops/known_hosts
  ls: cannot access '/opt/karvi/users/netops': No such file or directory
run exit=0
  -rw------- netops:netops /opt/karvi/users/netops/known_hosts
== an explicit path
resolved:   /home/netops/kh
```

The `resolved:` lines named both places before either existed, and `config
show` made nothing; in shared mode the store was made under `users`, the
individual phase's store in the home left as it was (issue 2). Verification:
gofmt, vet, every Go test, `make generated-clean`, the groff lint, and the
seventeen suites on the lab build (16:17:18 to 16:20:41 UTC), the released
`bin/` unchanged.

**Found on the way.** SECURITY and BUILD-HOWTO each kept an enrollment recipe
of their own that hashed the host field and wrote the address there, where the
guide requires the device's identity; one recipe is left. The guide had
stated the store's directory as `0700` while the code's rule is "not writable
by group or others" (the private root is `0750`). The exercise, described as
local and read-only, calls the host-key resolution that creates a missing
store under `accept-new`, as it did before; noted, not changed. A namespace
script's `kill %1` does nothing in `sh`, which has no job control, so the
fakes outlived the runs until stopped by their process IDs.

## 24. Jobs across Linux servers (2026-10-04)

The operator asked for the design of running jobs across generic Linux
servers, with an option to run each command over an SSH control path (one
connection, a non-interactive exec channel per command) in place of an
interactive login; then, during the session, for the removal of terminal
control characters from `login --record` transcripts, as the Linux shell's
colours must leave a recorded output.

**What it gains.** A command's own verdict: under exec each command returns
an exit status, with stdout and stderr apart, which no failure pattern can
match on a server's free text. Clean output, with no prompt to find, no echo to
remove, and nothing of the terminal or the login shell's start files. One
authentication for many commands. A use for `ssh.control-path-root`, which the
roadmap holds open ([`ROADMAP.md`](../ROADMAP.md), "Control sockets for the
system transport"). And the server fleet gets what the network fleet has (the
plan, the daemon, the records, `errors.jsonl`, the ledger, `watch`, `crun`)
without a second tool. It waits on nothing outside the tree: this host's
OpenSSH serves the executed examples, and the suites need a fake that answers
exec requests, built here. It needs the operator's facts about the servers:
how they authenticate, whether `sudo` is needed, and which shells they run.

**Executed: today.** A lab build under `/tmp/nd.7MvZ` (`basedir`, the trust
store, the spool, the scratch, the scoreboards, and the ledger all under it),
`karvi command 127.0.0.1 --platform linux` against this host's OpenSSH with
four commands. Over the system transport, with `ssh.pubkey-authentication =
true` (off by default), the operator's key logged in and the run exited 0 with
every record `succeeded`:

```text
'command': 'ls /nonexistent', 'status': 'succeeded',
'output': "\x1b[?2004l/usr/bin/ls: cannot access '/nonexistent': No such file or directory\n
           \x1b[?2004h\x1b]0;netops@dev: ~\x07\x1b[01;32mnetops@dev\x1b[00m:\x1b[01;34m~\x1b[00m$\n"
'command': 'false',           'status': 'succeeded'
'command': 'echo status=$?',  'output': '...status=1\n...'
```

Three faults: a failed command is a success (`linux` has no failure patterns,
and the shell hides the exit status); the output keeps bash's bracketed-paste
switches, the window title, and the coloured prompt, the trailing prompt left
in because the bytes carry colours the matched prompt does not, and the
`prompt` fields keep the title; the login shell's aliases apply (`/usr/bin/ls`
in the message). Over `scrapligo-v1` the same run is `authentication_failed`,
exit 108: the adapter offers password and keyboard-interactive alone, and this
host, like most servers, has `passwordauthentication no`, so `run`'s default
transport cannot reach a key-only server.

**Executed: one master, an exec channel per command,** plain OpenSSH with the
lab's trust store:

```text
[uname -s]        exit=0 stdout=Linux| stderr=
[ls /nonexistent] exit=2 stdout= stderr=ls: cannot access '/nonexistent': No such file or directory|
[false]           exit=1 stdout= stderr=
Master running (pid=645286)    -> -O exit, socket gone
```

This host's `sshd` has `MaxSessions 10`, the default: one connection carries at
most ten channels at once, and `linux`'s session cap is also 10.

**The issues, in the order proposed.**

1. Scope: one platform or one per distribution, and the fleet's facts.
2. Terminal text: the transcripts and the shell's output rendered as the
   terminal showed them (inserted at the operator's word; it and the shell
   mode's own output share one renderer, so they are one issue).
3. Where exec lives: a platform property, a transport, or a per-run option,
   and what selects it per device.
4. The record of an exec command: the exit status, stderr, the code of a
   non-zero exit, the device-error policy, the prompt fields.
5. Key authentication on both transports, and what the credential backends
   supply then.
6. The connection beneath exec: OpenSSH's ControlMaster (the sockets' place,
   the sweep, `ControlPersist`, the `~` bug) or the native transport's own
   channels over one connection; for one job or across jobs.
7. Privilege: `sudo` in place of `enable`.
8. What exec keeps of the shell model: blind sends, `--expect`, paging,
   session-init, exit commands, timeouts, cancel, the output limit, the spool.
9. `crun` for servers: collection commands and filters.
10. The fake and the parity run.
11. The daemon, the ledger (`MaxSessions` against the session cap), `watch`.

**Issue 1, agreed.** One built-in `linux` platform, no definition per
distribution; a class of servers that needs other collection commands, caps,
or ports is a `[platform.NAME]` alias with `driver = "linux"`, as `c9300` is
for IOS XE. The fleet's facts (authentication, `sudo`, shells) are still to
come from the operator and are taken where issues 5 and 7 need them.

**Executed: a recorded login.** `karvi login 127.0.0.1 --platform linux
--record`, driven through a pseudo-terminal: a coloured `ls`, a word corrected
with two backspaces, `exit`. The transcript, shown by `cat -v`:

```text
^[[?2004h^[]0;netops@dev: ~^G^[[01;32mnetops@dev^[[00m:^[[01;34m~^[[00m$ ls --color=auto -d /etc /bin^M
^[[?2004l^M^[[0m^[[01;36m/bin^[[0m  ^[[01;34m/etc^[[0m^M
^[[?2004h^[]0;netops@dev: ~^G^[[01;32mnetops@dev^[[00m:^[[01;34m~^[[00m$ echo helo^H^[[K^H^[[Klo^M
^[[?2004l^Mhelo^M
```

Every line ends in a carriage return, and the line editor's corrections are
backspaces and erase-to-end sequences. Deleting the sequences and the control
characters records a command never sent:

```text
netops@dev:~$ echo helolo
```

A prototype that applies them as the terminal does (a carriage return to the
line's start, a backspace one column left, erase to the end of the line, the
cursor moved within the line; every other sequence and control dropped, tabs
kept) gives what the operator saw:

```text
netops@dev:~$ ls --color=auto -d /etc /bin
/bin  /etc
netops@dev:~$ echo helo
helo
netops@dev:~$ exit
logout
```

The tree holds two partial removers and no renderer: `devsession`'s prompt
matcher removes CSI sequences alone (why the `prompt` fields keep the window
title), and `display.StripANSI` removes CSI and OSC for karvi's own colours;
neither applies a backspace, an erase, or a carriage return. The transcript is
rewritten once at the session's end (`transcript.StripScriptMarkers`, then its
SHA-256 for the metadata).

**Executed: the editing keys.** The operator asked whether the rendering
accounts for Ctrl-A and Ctrl-E, Home and End, and the like. The transcript
never holds the keys, only the far end's echo of them, so the question is what
the remote editor sends. A recorded login at 80 columns, each edit ending in an
`echo` whose output proves the command sent:

| Keys | The line model | The width-aware renderer |
|---|---|---|
| Ctrl-A, Ctrl-E, Home, End, the arrows, Delete | right | right |
| Ctrl-K, Ctrl-U, Ctrl-W, history (Up), Ctrl-R | right | right |
| a 95-character line typed through | right | right |
| Ctrl-A and an insert in a line wrapped past 80 | garbled: bash moved up a row (`ESC[A`) | `echo X` and 90 `b`, as its output |
| Ctrl-L, at an empty prompt and inside a typed line | the prompt doubled | `echo cleared` |
| a wrapped line: an insert, Delete, Ctrl-E | | `echo YZ`, 84 `c`, `!`, as its output |

A line longer than the terminal wraps with no control at all, so the width is
needed. `script(1)`'s advanced timing log (`-T FILE -m advanced`, util-linux
2.39 here) records it, with each resize between the output's byte counts:

```text
H 0.000000 COLUMNS 80
O 1.010129 4
S 0.300926 SIGWINCH ROWS=30 COLS=132
O 0.702328 5
```

**Issue 2, agreed.** One renderer of terminal text. It holds the line being
written as rows of the terminal's width: text wraps at the width, a carriage
return goes to the row's start, a newline ends the line on its last row and
moves down a row otherwise, a backspace and the cursor's left, right, and
column moves stay in the row, cursor up and down move between the line's rows,
erase in line, erase below, insert character, and delete character apply, the
screen cleared discards the unfinished line, and any other cursor positioning
ends the line; a finished line is written whole. Every other sequence and
control is dropped, tabs kept. A transcript is rendered at the session's end,
in the rewrite that removes the marker lines and before its digest, its widths
from the timing log, written into the scratch and removed after; no raw copy
is kept, and a session killed before its end keeps its raw bytes. The shell's
output and prompts in `command` and `run` are rendered by the same function on
every platform; the width each transport asks of the far end is checked when
built. The limits are stated in the guide: a key the far end does not echo
cannot appear, a full-screen program is its text in the order drawn, and a
device showing a long line as a scrolled window records the window. The fake
has no line editor, so IOS XE's editing is a laboratory question: a new
runbook row, D16, records it on the ISR and the Catalyst.

**Not taken.** Deleting the sequences (`echo helolo`). A line model without
the width. The operator's keystrokes recorded to rebuild each command
(`script --log-in`), which would write a password typed at a prompt that does
not echo. A raw copy beside the transcript, a switch to turn rendering off,
`TERM=dumb` asked of the server, and a terminal-emulator library.

**Executed: what selects behaviour per device.** The platform definition
already had a field for connection reuse, `control-master`, accepted,
validated (`config_platform_control_master_not_boolean`), and read by
nothing: the system transport always passes `ControlMaster=no`, and
ARCHITECTURE's sentence that a platform's `control-master = true` turns reuse
on is stale. The fake refuses every exec request (it records it and replies
false). A dry run over an inventory of a `linux` server, an alias of it, and an
IOS XE router shows the plan settling each target before any contact:

```text
- name:srv1: planned
  intended: ping=disabled transport=native port=22 platform=linux
- name:app1: planned
  intended: ping=disabled transport=native port=22 platform=appliance
- name:rtr1: planned
  intended: ping=disabled transport=system port=22 platform=cisco_iosxe
```

**Issue 3, agreed.** A platform field, `channel = "shell" | "exec"`: what
karvi asks of the SSH session channel. Built-in `linux` is `exec`, every other
built-in `shell`. Any `[platform.NAME]` table may set either word on any
driver, unchecked: the operator decides what a vendor's later releases accept
(an alias of `arista_eos` or `cisco_iosxr` with `channel = "exec"`), and a
device that refuses the channel fails with `ssh_session_channel_refused`. An
eighth built-in, `linux_shell`, is `linux` with `channel = "shell"`, its base
driver `linux`: an inventory row names it for a server whose security refuses
exec, so one job runs IOS XE devices, `linux` servers, and `linux_shell`
servers together, with no table. A definition that leaves `channel` unset is
`shell`, the case of a custom platform that inherits from no built-in. The
channel is resolved per target at planning, carried in the plan and the
manifest, and shown on the dry run's `intended:` line; no option sets it for
one run. A target over telnet whose platform says `exec` is refused at
planning, naming both (telnet has no exec). `login` stays interactive, `crun`
follows the platform, and `control-master` is removed with its error code.
NETCONF, which the word leaves room for, went to the roadmap at the operator's
word ([`ROADMAP.md`](../ROADMAP.md), "NETCONF").

**Not taken.** Exec as a transport or an implementation ID; a per-run
`--exec`; an admission list for `exec`; `session` as the field's name; a
boolean; `linux_shell` shipped as an alias in a configuration file.

**Executed: what an exec returns.** Through one OpenSSH master and over
x/crypto (the native transport's library), from a probe built outside the
tree:

| Command | OpenSSH through a master | x/crypto |
|---|---|---|
| `echo warn >&2; echo data` | 0, stderr `warn` | 0, stderr `warn` |
| `kill -TERM $$` | 255, as its own failures | status 143, signal `TERM` |
| `exit 300` | 44 | 44 |
| `cat`, no stdin | 0 (end of input) | 0 (end of input) |
| `echo one; echo two >&2; echo three` | stdout `one three`, stderr `two` | |

The order between the two streams is lost, and OpenSSH's client reports a
remote signal as it reports its own failures (held for issue 6). Today's
device error, against the fake, for comparison: `status: device_error`, `code:
device_command_error`, category `device`, exit 107, and in the text file:

```text
Router#show bogus
% Invalid input detected at '^' marker.
! device_command_error: device reported a command error
! not sent: show version
```

**Issue 4, agreed.** Every record carries `channel`; an exec record adds
`exit_status`, `exit_signal`, and `stderr` with its size and digest, spooled
as `output` is, null on a shell record (record schema 3); exec output is kept
as the program wrote it. Exit 0 is `succeeded` whatever stderr holds; a
non-zero exit is `device_error` with `command_exit_nonzero`, a signal
`command_exit_signal`, a channel closed without a status
`command_exit_missing`, all `device`, exit 107. The failure patterns apply to
both streams after the status. The device-error policy is unchanged. The
record's prompt fields are empty with `prompt_source` `none`; the echo and
the text file show the inferred prompt, stdout, stderr, and a `!` line:

```text
srv1$ ls /nonexistent
ls: cannot access '/nonexistent': No such file or directory
! command_exit_nonzero: exited 2
```

A second spool per command reaches the record's write path, so the build is
measured at width (N=32) before it is committed.

**Not taken.** stderr folded into `output`; a pty under exec; a non-zero exit
as a success with a notice; a new status; accepted exit codes per command.

**The fleet's facts, from the operator.** The production fleet needs four
ways to authenticate: (1) the operator's own `~/.ssh/id_*` keys and no
password; (2) a username and a password, as for routers and switches; (3) a
key file mapped to one or more servers through a credential profile; (4) a key
and a password. (5) A key's passphrase is later work. Authentication stays as
flexible as for routers, with servers mapped to credential backends the same
way. The fallbacks: by default `NETUSER`, `NETPASS`, and `NETENABLE` serve no
Linux server; an option lets `NETUSER` and `NETPASS` serve them, with
`NETENABLE` as the `sudo` password when `NETSUDO` is not set; with no other
credential, the operator's username and `~/.ssh/id_*` keys are tried; and
where `sudo` needs a password, `NETSUDO` is tried. The first build is (1),
with `sudo` needing no password, as on this host (`sudo -n true` succeeds);
the rest follow. A credential helper executable (a script whose `export
NAME="value"` lines set karvi's credential environment) went to the roadmap
at the operator's word ([`ROADMAP.md`](../ROADMAP.md), "A credential helper
executable").

**Executed: credentials today.** A policy map rule `platform = "linux"` sent
`srv1` to a `servers` policy; `app1`, an alias with `driver = "linux"`, fell
to `default` (a rule matches the platform's name), and `platform = "app*"`
took it. The `env` backend fills `%s` with the operator's login name, case
kept: `SRV_netops_USER` answered for `srv1` and `SRV_SRV1_USER` did not, while
`examples/config.toml` said the device's name upper-cased; with one of its
variables set it answers, and a password it lacks is
`credential_password_missing`, no later source asked. The documents now
describe it as the code does ([`docs/OPERATIONS.md`, "The environment
backend"](OPERATIONS.md#the-environment-backend)).

**Issue 5 is split.** 5a is the shape of the four ways; 5b the first build,
the operator's keys.

**Issue 5a, agreed.** Servers take policies and backends as routers do. A
credential is a username with a password, a key, or both: the operator's keys
(1), a password (2), a key file a credential CSV row names in a new `keyfile`
column, its selectors assigning the key to one server or many (3), or a key
and a password, the key first and the password on partial success (4). A key
is a reference to its file, never its bytes: the plan, the package, and the
records carry its path and SHA-256 fingerprint, the connecting process reads
the file, and a `keyfile` passes the credential file checks. A key with a
passphrase is skipped with a notice. After the backends comes the platform's
`fallback`, an ordered list of `netvars`, `keys`, and `prompt`: the network
built-ins and `generic` `["netvars", "prompt"]` (today's), `linux` and
`linux_shell` `["keys"]`, an unset field `["netvars", "prompt"]`; the
operator's option is `[platform.linux] fallback = ["netvars", "keys"]`. For a
Linux server the enable field is the `sudo` password (`NETSUDO`, issue 7).
`ssh.pubkey-authentication` is removed: a credential with a key offers it, one
without offers none.

**Not taken.** A key's bytes in the credential package; two booleans in place
of `fallback`; the map matching base drivers; `ssh-agent` in the first build.

**Executed: the operator's keys.** OpenSSH's own default list (`ssh -G`):
`id_rsa`, `id_ecdsa`, `id_ecdsa_sk`, `id_ed25519`, `id_ed25519_sk`,
`id_xmss`, `id_dsa`. Lab keys beside the operator's one key, parsed with
x/crypto:

```text
keys/id_ed25519_enc: passphrase missing; public key from the file: SHA256:J6jEX/+bLIGrueLrn0QT8vdI/Uf87BZlHivwoEE2K5M
keys/id_rsa: ssh-rsa SHA256:tjK81d8rLOzNBc6td9LK3Rj+jI5BtTCmnvrCaaxo3Bc
/home/netops/.ssh/id_ed25519: ssh-ed25519 SHA256:lCkD25f/uZQGbWYmns4BurmVr65NAa+wSHkq5Y/lnVk
```

`ssh` given only named keys (`IdentitiesOnly=yes`, `IdentityAgent=none`, the
password methods off, batch mode) logged in with the operator's key and
refused an unknown one with `Permission denied (publickey,keyboard-interactive)`
and no prompt. A passphrase key the server does not accept is never opened,
so no passphrase is asked; one it accepts would ask the askpass helper, whose
classifier knows a password and refuses a passphrase (not executed: it would
need a key in the operator's `authorized_keys`). With two keys named,
OpenSSH's log at `DEBUG1` shows each offer and the key accepted:

```text
debug1: Offering public key: /tmp/nd.7MvZ/keys/id_rsa RSA SHA256:tjK81d8r… explicit
debug1: Offering public key: /home/netops/.ssh/id_ed25519 ED25519 SHA256:lCkD25f/… explicit
debug1: Server accepts key: /home/netops/.ssh/id_ed25519 ED25519 SHA256:lCkD25f/… explicit
Authenticated to 127.0.0.1 ([127.0.0.1]:22) using "publickey".
```

The server allows six attempts (`MaxAuthTries 6`), each key offered one.

**Issue 5b, agreed, as amended by the operator.** `ssh.identities` names the
operator's keys in order, by default `~/.ssh/id_ed25519`, `~/.ssh/id_ecdsa`,
`~/.ssh/id_rsa` (the operator's priority, strength and speed first), lockable
by a site. Each file present is judged at planning (the operator's own, no
group or other access, no passphrase, no hardware-backed key) and skipped with
`operator_key_skipped` when it fails; none left is
`credential_operator_keys_missing`. The credential is the operator's login name
and the keys' paths (`builtin-operator-keys`), the dry run showing each
fingerprint. The operator asked what a fingerprint comparison at the
connection would gain: nothing (whoever can replace the file holds the account,
and a key rotated during a long job would fail every remaining device), so
there is none; the connecting process reads the files as they are, and the
record names the key that authenticated, from OpenSSH's `DEBUG1` log or the
native adapter's signers. The system transport sets `IdentitiesOnly yes`,
`IdentityAgent none`, an `IdentityFile` per key, and no password methods
without a password; the native adapter offers signers. `sudo -n …` is written
by the operator until issue 7.

**Not taken.** A glob over `id_*`; bare file names; the order in the code; the
operator's own `~/.ssh/config`; `ssh-agent` first; the fingerprint comparison
(`credential_key_changed`).

**Executed: the connection beneath exec.** Ten `true` commands to this host:

| Way | Time, connection included | Authentications | A remote signal |
|---|---|---|---|
| a fresh `ssh` per command | 3,909 ms | 10 | 255; the client's `DEBUG1` log shows `rtype exit-signal` |
| one ControlMaster, an `ssh` client per command | 408 ms | 1 | 255; the client logs nothing, the master's log shows `exit-signal` (its channel numbers repeat) |
| one x/crypto connection, a channel per command | 211 ms | 1 | status 143, signal `TERM` |

A command's own `exit 255` logs `rtype exit-status` and `Exit status 255`; a
failure of `ssh` logs neither. OpenSSH names no signal, even at `DEBUG3`. A
first try showed empty logs: OpenSSH takes the first value of an option, and
an earlier `LogLevel=ERROR` on the same command line won.

**Issue 6, agreed.** One connection per exec device for its command list, a
channel per command in order, closed at the device's end; no `ControlPersist`
and no reuse across jobs, since a reused master would run a later job under the
earlier job's authentication. On `scrapligo-v1`, a session per command on
karvi's connection. On `system`, a ControlMaster per device session as karvi's
own child, an `ssh -S` client per command, `ssh -O exit` at the end; its socket
in `ssh.control-path-root` under a 16-hex-character name, a root too long for
the 108-byte limit refused at planning, dead sockets swept at the daemon's
start and at admission, the `~` bug fixed. On 255 the master's `DEBUG1` log
decides between the command's own 255, a signal (`exit_signal` `unnamed`), and
a connection failure. The roadmap's control-socket item is decided by this and
goes when built.

**Not taken.** A fresh `ssh` per command; masters kept across jobs; exec on
`scrapligo-v1` alone; OpenSSH's `%C` name.

**Executed: `sudo` without a terminal.** On this host `netops` has `(ALL)
NOPASSWD: ALL`. An account whose `sudo` asks was made for the test alone, as
root in a private mount namespace: a tmpfs at `/mnt` holding a copy of
`/etc/shadow` with a lab password hash for `netops.test` and a copy of
`/etc/sudoers` with one line for it, each bind-mounted over the original, and
a tmpfs over `/run/sudo`; nothing outlived the namespace.

| Case | Result |
|---|---|
| `sudo -n id -u`, no password needed (`netops`, over exec) | `0`, exit 0 |
| `sudo -n id -u`, a password needed (`netops.test`) | `sudo: a password is required`, exit 1 |
| `sudo -S -p "karvi-7f3a:" id -u`, nothing on stdin | `karvi-7f3a:`, `no password was provided`, exit 1 |
| the same with the right password on stdin | `karvi-7f3a:`, then `0`, exit 0 |
| `printf SECRET \| sudo -S -p "karvi-7f3a:" cat`, no password needed (over exec) | `SECRET-PASSWORD`: `sudo` asked nothing and the command read it |

**Issue 7, agreed.** The first build adds nothing: the operator writes `sudo
-n`, and a refused one is `command_exit_nonzero` with `sudo`'s message in
stderr. Later, with the password credentials: `--sudo` attaches to its
`--cmd`; the command runs under `sudo -S -p` with a random prompt; the
password is written once, only after that prompt appears on stderr; the prompt
is removed from the record; a second prompt is `privilege_failed`; the
password is the enable field, then `NETSUDO`, then `NETENABLE` under
`netvars`, and with none the command runs under `sudo -n`. The shell channel
keeps `--expect`.

**Not taken.** A password written blindly; rewriting a `sudo` the operator
wrote; a pty for the prompt; `SUDO_ASKPASS`.

**Executed: a command given up at its deadline.** Over x/crypto, a channel
running `sleep 31` closed after a second left the command running on the
server; a channel running `sleep 32` sent a `KILL` signal request first, and
the command was gone; the connection then served `echo still-usable`. Over a
ControlMaster, the `ssh` client running `sleep 47` killed after a second left
the command running, and the master served the next command; OpenSSH's client
cannot send a signal request. (The first probe's `pgrep -f` also listed the
calling shell, whose command line held the pattern; the second ran from a
script file. The leftover commands were ended.)

**Issue 8, agreed.** Under exec there is no first prompt (ready at
authentication or at the master's `-O check`, within the login's bound), and
no privilege step, paging, or exit commands. Session-init runs, a command per
channel, nothing carrying; no state carries between commands. Blind sends,
`\r` endings, `--blind-return`, and `--expect` are refused at planning for
exec targets; `--literal` is accepted. A command timeout: `scrapligo-v1` asks
for `KILL` and closes the channel, `system` kills its client with the notice
`remote_command_not_stopped`; it falls under the device-error policy, the
connection being usable. The device timeout, keepalives, and cancel are
unchanged; the output limit counts both streams, each spooled on its own.
`--sudo` reaches `linux_shell` as `sudo -p` without `-S`, the terminal's echo
off for the password.

**Not taken.** The declarations dropped silently; a timeout ending the device
under every setting; a pty to hang up a command; a remote watchdog.

**Executed: a server's collection.** Today, over the shell, `karvi crun
--target srv1 --cmd 'uname -r' --cmd 'cat /etc/missing' --cmd 'ip -br link
show lo'` replaced the file, and every block carried the shell's sequences and
prompt:

```text
! uname -r
^[[?2004l6.8.0-146-generic
^[[?2004h^[]0;netops@dev: ~^G^[[01;32mnetops@dev^[[00m:^[[01;34m~^[[00m$
```

A candidate list run twice over exec, a few seconds apart, without root: every
command exited 0, and only `uptime`, added as the control, differed. At the
operator's word `/etc/hosts` and `/etc/resolv.conf` left the list, `ip route`
became `ip route show table all` (the IPv6 routes and the `local` table, 18
lines here), and `uname -srm` became `uname -snrm` for the hostname (`Linux
dev.16k.net 6.8.0-146-generic x86_64`); the amended list's two collections were
identical.

**Issue 9, agreed.** `linux` and `linux_shell` collect `cat /etc/os-release`,
`uname -snrm`, `ip -br address`, `ip route show table all`, and `systemctl
list-unit-files --state=enabled --no-pager --no-legend`, with no built-in drop
list. Under exec a block is stdout then stderr; a non-zero exit leaves its error
text and the file is replaced, the status kept in the record. Root's commands
are `sudo -n` in a site's list until `--sudo`.
[`docs/COLLECTION.md`](COLLECTION.md) gains `linux` in its two tables and a
section on servers.

**Not taken.** No built-in list; root-only commands; package lists; the two
`/etc` files; the exit status in the file.

**Executed: the fixtures' ground.** The fake is one x/crypto server whose
IOS XE persona answers a fixed table and refuses every exec request; fifteen
files outside `vendor/` name it. The vendored x/crypto (v0.26.0) has the
server's `PartialSuccessError`, so a key-then-password login can be tested
without a real server. The two transcripts captured in this chapter carry this
host's public IPv4 and IPv6 addresses in the login banner.

**Issue 10, agreed.** One fake, renamed `karvi-fake-device`
(`internal/fakedevice`) in a section of its own, with a Linux persona: an exec
table (the collection list, `fail N`, `both`, `big`, `bigerr`, `slow`, `signal
TERM`, `nostatus`, `sudo -n id -u`, exit 127 for an unknown command), key and
password logins and later both, and a shell for `linux_shell` with bash's
decorations under an option; it records exec lines, pty requests, channels, and
signal requests. The renderer's tests are byte fixtures, the captured
transcripts with documentation addresses and names. The parity run covers exec
on the four combinations with `exit_signal`'s difference pinned, and a
`linux_shell` row. The suites use the fake alone; a server's evidence is the
build's runs against this host's OpenSSH and new runbook rows for a production
server.

**Not taken.** A second fake; the `iosxe` name kept; the suites against the
host's `sshd`; the transcripts as captured; `exit_signal` excluded.

**Executed: a master whose parent dies.** A shell started a master (`ssh -M
-N`, not backgrounded by `-f`) and was killed with `SIGKILL`:

```text
parent 689402, master 689404, socket: /tmp/nd.7MvZ/o.sock
after SIGKILL of the parent: the master is still running, ppid 1
the orphaned master still serves
after SIGTERM of the master: the socket is gone
```

**Issue 11, agreed.** The ledger is unchanged: an exec device session is one
connection and one lease each against the host's and the device's caps,
`linux`'s staying 10; `MaxSessions` is never reached. The daemon (for `run`)
and the client (for `command`) start each master with a death signal from a
goroutine holding its OS thread; the sweep takes a socket left regardless. The
daemon checks the plan's exec rules; the execution plan's schema moves to 11
and the record's to 3. `watch` gains no column, its bytes counting both
streams; the audit names the key that authenticated; `metrics.json` is as it
is.

**Not taken.** A lease per channel; masters left to end with their parent; a
`watch` column.

**The design is complete.** Eleven issues settled, each a DESIGN entry and a
section of this chapter; the chapter stays open until the work is built, in
sections committed one at a time on the operator's word, and closes with the
build's own executed runs.

**During the build: the shell's width.** The renderer needs a width, and the
design said the width each transport asks of the far end would be checked when
built. Both ask for none: `scrapligo-v1` requests a pty of 0 by 0, as `ssh -tt`
does from a pipe. Executed, `ssh -tt` from a pipe to this host:

```text
stty size          ->  0 0
echo $COLUMNS      ->  80
echo rrrr…(100 r)  ->  ...$ echo rrrrrrrrrr…(100 r)^M     one line, no control inside it
```

**Agreed.** The shell's stream in `command` and `run` is rendered at no width
(nothing wraps; cursor up and down do nothing), and the transports keep asking
for no size: a command written whole comes back as plain text whatever width
the far end assumes, and the width matters only to interactive editing, a
recorded login's, whose timing log gives it.

**Not taken.** Rendering at 80; asking the far end for a width (it changes
what devices print); the width of karvi's own terminal.

**During the build: blanks at a line's end.** The renderer as built for
transcripts trims every trailing blank. Over the shell today, and in a recorded
login through the renderer:

```text
printf 'desc \n'      -> record: '\x1b[?2004ldesc \n\x1b[?2004h\x1b]0;netops@dev: ~\x07…$\n'
printf 'abc\b \b\n'   -> record: '\x1b[?2004labc\x08 \x08\n\x1b[?2004h…$\n'
transcript:  desc
             ab
```

Keeping the spaces the far end wrote gives `desc ` and `ab `: the configured
space kept, and the erase idiom's space left. Trimming gives `desc` and `ab`.

**Agreed.** One renderer with one option for a finished line's end: the shell's
output in a record keeps the spaces the far end wrote (a record is the device's
data), a transcript trims every trailing blank (what the operator saw). The
unfinished line, where prompts and declarations are matched, keeps its written
spaces in both.

**Not taken.** One rule for both; recognising the erase idiom.

**During the build: the channel before exec.** The platform's `channel`
arrives with the plan's schema 11, and the exec channel is built later, on
`scrapligo-v1` and then on `system`. Executed, a dry run over this lab's
inventory, with an alias `appliance` (`driver = "linux"`, `channel =
"exec"`):

```text
- name:srv1: planned
  intended: ping=disabled transport=native port=22 platform=linux channel=shell
- name:bast1: planned
  intended: ping=disabled transport=native port=22 platform=linux_shell channel=shell
channel_exec_unavailable: client planning: app1: platform appliance asks for an exec channel, which the scrapligo-v1 transport does not have yet; …
channel_exec_over_telnet: client planning: app1: platform appliance asks for an exec channel and the transport is telnet, which has none; …
```

**Agreed.** Built-in `linux` stays `shell` until exec is built on both
transports; until then a target whose platform says `exec` is refused at
planning with `channel_exec_unavailable`, and the daemon's plan check refuses
it too, so no exec target runs on a shell. The refusal is lifted per
transport as each is built, and the code goes with the last; never released,
it is deleted, not retired.

**Not taken.** `linux` exec from the start (every `linux` target without a
way to run until exec is built); an exec target falling back to the shell (a
record that looks like exec's without an exit status).

**During the build: the credentials' first build.** The platform's
`fallback`, `ssh.identities`, and `builtin-operator-keys`, before the
transports offer the keys. Executed, a dry run over a `linux` server, a
`linux_shell` server, and an IOS XE router with `NETUSER` and `NETPASS` set,
`ssh.identities` naming a lab key with a passphrase, a lab key at `0644`, the
operator's key, and an absent `~/.ssh/id_rsa`:

```text
warning: operator_key_skipped: operator key /tmp/nd.MlEH/keys/id_locked skipped: protected by a passphrase
warning: operator_key_skipped: operator key /tmp/nd.MlEH/keys/id_open skipped: mode 0644, not 0600 or 0400
- name:srv1: planned
  credential: bound 20261005T055915.472140-0400-2zmnvah6w9h7jap1d0fr (policy=default backend=builtin-operator-keys user=netops; value not displayed)
  key: /home/netops/.ssh/id_ed25519 SHA256:lCkD25f/uZQGbWYmns4BurmVr65NAa+wSHkq5Y/lnVk
  intended: ping=disabled transport=native port=22 platform=linux channel=shell
  finding: warning operator_key_skipped operator key /tmp/nd.MlEH/keys/id_locked skipped: protected by a passphrase
  finding: warning operator_key_skipped operator key /tmp/nd.MlEH/keys/id_open skipped: mode 0644, not 0600 or 0400
- name:rtr1: planned
  credential: bound 20261005T055915.472167-0400-04p54axrp4qwdw373htr (policy=default backend=builtin-env-fallback user=netuser; value not displayed)
```

`linux_shell` bound to the same grant as `srv1`. With only the two lab keys
and `id_rsa` listed, `credential_operator_keys_missing`, exit 6, listing each
file and what was found. Under `[platform.linux] fallback = ["netvars",
"keys"]` the server took `NETUSER`; with `NETUSER` alone it took the keys. A
live run over `scrapligo-v1` failed at the handshake ("the credential has no
password") with both notices on the first record and the key's path and
fingerprint in the manifest, as expected until the transports offer keys.

**Agreed.** Six points the build met: `netvars` fills the empty fields and
ends the walk only when they make a whole credential, a following `prompt`
fills the rest, a following `keys` replaces them, and when nothing ends the
walk the failure is the last source's that found something; a backend's
credential needs a password (it holds no key until `keyfile`), and
`config_ssh_auth_mechanisms_disabled` is retired, both password methods off
being a key-only site; a skipped key is also a warning line once at planning;
telnet passes `keys` over (`credential_password_missing`); the key files are
parsed in `internal/adapters/sshkey`, keeping x/crypto out of the credential
code. The operator also settled the system transport's `Include ~/.ssh/config`
for 5b: kept, and the key OpenSSH reports from its log is the one recorded,
since OpenSSH adds the included file's `IdentityFile` lines after karvi's
even under `IdentitiesOnly`.

**Not taken.** A partial `netvars` failing at once; keys over telnet; the
mechanisms check as "a password method must remain"; key parsing in the
credential code; the `Include` left out for a credential with keys.

**During the build: the method, not the key.** DESIGN had the record name the
key that authenticated, from OpenSSH's `DEBUG1` log written with `-E` into the
scratch. Executed, with a key this host refuses:

```text
-E e.log at DEBUG1:   exit 255, stderr: []      (empty)
  e.log:  netops@127.0.0.1: Permission denied (publickey,keyboard-interactive).
today, INFO, no -E:   netops@127.0.0.1: Permission denied (publickey,keyboard-interactive).
```

`-E` moves OpenSSH's errors off the stderr the system transport classifies and
the terminal `login` shows. On that stderr `DEBUG1` names the key
(`debug1: Server accepts key: /home/netops/.ssh/id_ed25519 ED25519
SHA256:lCkD25f/…`, 65 lines and 4.5 KB a session, none for keepalives), and
`VERBOSE` names only the method:

```text
Authenticated to 127.0.0.1 ([127.0.0.1]:22) using "publickey".
Connection to 127.0.0.1 closed.
Transferred: sent 3776, received 4776 bytes, in 0.8 seconds
```

**Agreed.** No `-E` file. The record names the method, `credential.auth`
(`publickey`, `keyboard-interactive`, or `password`), not the key: the system
transport's command sessions run at `VERBOSE`, the `Authenticated to …` line
taken from the stderr and kept from the diagnostics; the native adapter notes
the last of its callbacks called. Both transports try `publickey`, then
`keyboard-interactive`, then `password`, the last two answered with the
password, since some Linux servers allow keyboard-interactive and refuse
password. `login` stays at `ERROR` and names no method. Every
`command_completed` audit event names the method with the device username and
the backend, repeated per command as the policy and the transport are. The key
that authenticated went to the roadmap ([`ROADMAP.md`](../ROADMAP.md), "The
key that authenticated"). Executed after the build, over both transports to
this host, each record's `credential.auth` is `publickey`, and the audit:

```text
1 {"auth": "publickey", "backend": "builtin-operator-keys", "device_username": "netops", "selected_address": "127.0.0.1"}
2 {"auth": "publickey", "backend": "builtin-operator-keys", "device_username": "netops", "selected_address": "127.0.0.1"}
```

**Not taken.** `-E` into the scratch; the key in the first build; the method
as counts in `summary.json` or the job's closing audit event; the method on the
device's first audit event alone; `login` at `VERBOSE`, which would print the
closing lines on the operator's terminal.

**During the build: the fake's Linux persona.** The persona as built, run
from plain OpenSSH through one ControlMaster (`ssh -M -S`, the master at
`DEBUG1` with `-E`), a client per command:

```text
uname -snrm      exit 0    stdout "Linux fake 6.8.0-0-generic x86_64"
both             exit 0    stdout "to stdout"   stderr "to stderr"
fail 3           exit 3    stderr "failing with 3"
signal TERM      exit 255
nostatus         exit 255
sudo -n id -u    exit 0    stdout "0"
ls /nonexistent  exit 127  stderr "sh: 1: ls: not found"
slow, the client killed after 1s: exit 255; the next command served
fake:   left running: "slow"   connections=1 sessions=0   channels=9
master: rtype exit-status for six, rtype exit-signal for signal TERM, none for nostatus
```

One connection carried nine channels; the killed client left `slow` running,
as it left `sleep 47` on this host; and the master's log tells a signal from
a channel closed without a status, the ground of the system transport's 255
decision. Over x/crypto, a `KILL` signal request ended `slow` (status 137,
signal `KILL`) and the connection served the next command. `karvi command`
over `linux_shell` to the shell persona, plain and with bash's decorations,
recorded the same three outputs on both transports, `credential.auth`
`publickey`.

**Agreed.** Three points DESIGN left open: the shell's `exit` writes `logout`
and sends status 0, as a login shell does (the IOS XE persona still closes
without one); a command the shell does not know is bash's `-bash: NAME:
command not found`, while the exec table keeps `sh`'s; the options are
`-sudo-asks` (`sudo -n` refused) and `-decorations` (bash's bracketed paste,
title, and colours). The fake's stderr adds `signal: NAME`, `left running:
"LINE"`, and a closing `channels=N,…` line, the `connections=` line unchanged.

**Not taken.** The shell ending without a status as the IOS XE persona does;
one not-found message for the shell and exec; the channel counts on the
`connections=` line, which the suites match whole.

**During the build: the record's exec fields.** Record schema 3 came first,
before any transport runs exec: every record carries `channel`, a shell
record's exec fields null. Executed, a lab build's `karvi command` over the
fake's shell persona:

```text
{'schema_version': 3, 'command': 'uname -snrm', 'channel': 'shell', 'exit_status': None,
 'exit_signal': None, 'stderr': None, 'stderr_encoding': None, 'stderr_bytes': None,
 'stderr_sha256': None, 'prompt_source': 'observed'}
```

The record's line, which streamed one field from its spool, streams two: an
exec record whose stdout and stderr are each spooled, a large string, or a
small one, in every combination, writes the bytes of `json.Marshal` of the
whole record, compact and indented, and a stderr spool that changed is
`output_spool_mismatch`. At N=32 against the previous commit's build,
`commands.jsonl` grew 4.3 KB (the null fields) and the daemon's peak stayed
level (system 84 and 74 MB, `scrapligo-v1` 164 and 151 MB, noise).

**Agreed.** `stderr` carries `stderr_encoding` (`utf-8` or `base64`), since a
program's stderr can hold bytes that are not UTF-8, as its output can. On an
exec record a null `stderr` says the command did not run (its channel never
opened), an empty one that it ran and wrote nothing there; the text file and
the echo show `! not sent:` by it. The daemon IPC schema stays 10: the follow
frame carries the record as it is.

**Not taken.** stderr always UTF-8 with bytes replaced; a separate `ran`
field; the IPC schema moved with the record's.

**During the build: exec on `scrapligo-v1`.** A platform alias `fexec`
(`driver = "linux"`, `channel = "exec"`) over the native transport, against
this host's OpenSSH by the operator's key, `execution.command-timeout = 2s`,
`--continue-device-on-error`:

```text
hx$ id -un                   netops                         exit 0
hx$ echo out; echo err >&2   stdout out, stderr err         exit 0
hx$ ls /nonexistent          ls: cannot access ...          command_exit_nonzero: exited 2
hx$ kill -TERM $$                                           command_exit_signal: ended by signal TERM
hx$ sudo -n id -u            0                              exit 0
hx$ sleep 32                                                command_timeout; nothing left on the server
hx$ exit 300                                                command_exit_nonzero: exited 44
```

Every record's `credential.auth` was `publickey`. Against the fake's Linux
persona one connection carried a channel per command and no pty; `nostatus`
was `command_exit_missing`; at a 1-second timeout the fake recorded `signal:
KILL` for `slow` and nothing left running, and the next command ran; in the
same `run`, a `linux_shell` target's timeout ended its session as the shell's
rule says. An `--expect` or a trailing `\r` on an exec target stopped at
planning, exit 4; `--literal` passed; `--transport system` is still
`channel_exec_unavailable`. (A first probe for the killed `sleep` matched the
calling shell's own command line; the probe from a script showed it gone.)

**Agreed.** Four points the build met: at the output limit the command is
stopped as at a timeout, the record `output_limit_exceeded` with the first
limit bytes across both streams, the connection serving the next command; a
device that refuses the exec request is `ssh_session_channel_refused` as one
that refuses the channel, and the session ends; a channel closed without a
status is `command_exit_missing` only when the connection answers a keepalive
request within 5 seconds, a lost one keeping the session's codes; the
declarations' refusal is `channel_exec_declaration_refused`, a usage error,
from the client and the daemon's plan check alike.

**Not taken.** The limit ending the session as on a shell; a refused exec
request as a command's failure on a usable session; a missing status always
`command_exit_missing`; the daemon refusing the plan as
`execution_plan_invalid`.

**During the build: exec on `system`, its four points.** Before the build,
plain OpenSSH through one master against the fake's Linux persona, one client
per command (`/tmp/nd.MlEH/s8`). The master's own lines name the method that
authenticated, `Authenticated to 127.0.0.1 ([127.0.0.1]:45853) using
"publickey".` A master killed outright left its socket, and a client given it
logged `Control socket connect(…): Connection refused`, then connected and
authenticated by itself and exited 0 (the fake counted `connections=2`); with
`ProxyCommand=false` the same client exited 255 and the device saw nothing. On
255 with neither `rtype` line:

| Case | Client | The master's `DEBUG1` lines | The master after |
|---|---|---|---|
| `nostatus` | 255 | no `rtype` | answers `-O check` |
| the fake killed under `slow` | 255 | `Exit status -1`; `Connection to 127.0.0.1 closed by remote host.` on its stderr, not in an `-E` file | its socket already gone |
| an exec request refused (the IOS XE persona) | 255, stderr empty | `channel 2: chan_read_failed for istate 3` | answers `-O check` |

At `DEBUG3` the refusal shows as `mux request: exec`: OpenSSH queues `exec
request failed` to the client's stderr and loses it when the channel closes.
DESIGN had the master write its log with `-E` at `DEBUG1` into the scratch; the
operator asked whether the file and the level were needed, since 5c settled
the shell at `VERBOSE` with no file. With the master's stderr read and no
`-E`, every line the file held arrived, and the `closed by remote host` line
too. With the master at `VERBOSE`, `signal TERM`, `nostatus`, and `fail 255`
all exited 255 with no line between them; at `DEBUG1` each differs (`rtype
exit-signal`, none, `rtype exit-status`), and about 3 KB at the login and seven
lines a command follow.

**Agreed.** The master at `DEBUG1` with no `-E` file: karvi reads its stderr,
takes the method from its `Authenticated to …` line, each command's `rtype`
line, and the refusal line, passes OpenSSH's other lines to the bounded
diagnostics, and drops every other `debug1:` line. A client runs at `LogLevel
QUIET` with `ProxyCommand false`, reaching the device only through the master,
its stderr the command's alone. On 255 with neither `rtype` line, decided once
the master has written the command's `free: client-session` line, a master that
answers `-O check` is `command_exit_missing`, as `scrapligo-v1` decides by its
keepalive; `chan_read_failed for istate 3` is a refused exec request,
`ssh_session_channel_refused` with the session ending, as on `scrapligo-v1`, and
without it the case is `command_exit_missing`; a master gone is the session's
failure, its code from the diagnostics. A root longer than 73 bytes (107 less
`/`, the 16-character name, and OpenSSH's 17-character binding suffix) is
`control_path_root_too_long` at planning; a swept socket is logged
`control_socket_abandoned_removed`; a command stopped on `system` carries the
notice `remote_command_not_stopped`. The `~` in `ssh.control-path-root` was
`basedir`'s grandparent (`~/c8` under a lab `basedir` made `/tmp/nd.MlEH/c8`,
and under the default would make `~/.local/c8`), and becomes the home.

**Not taken.** `-E` files for the master and the clients; the master at
`VERBOSE`; a 255 with neither line always a connection failure; a refused exec
request always `command_exit_missing`.

**Executed after 8a, the socket's place.** OpenSSH served a master whose
socket was a 16-character name under a root of 73 bytes, and under 74 refused
it at the bind: `unix_listener: path "…/0123456789abcdef.gx9Z6FBLD6UnSCOE" too
long for Unix domain socket`. karvi with a 74-byte `ssh.control-path-root` and
the exec alias over `system` stopped at planning, exit 2:

```text
control_path_root_too_long: the control-path root /tmp/nd.MlEH/s8/rrrr…r is 74 bytes; a control socket's path must fit OpenSSH's 108-byte limit with its 16-character name and 17-character binding suffix, so the root may be at most 73 bytes; set ssh.control-path-root to a shorter directory
```

A `linux_shell` target under the same root ran, and at 73 bytes the exec
target reached `channel_exec_unavailable`, as 8b lifts. With a dead socket of
karvi's name, one of another name, and a live one in the root, a `command`'s
admission removed the dead one alone (`control_socket_abandoned_removed: removed
the abandoned control socket …/0123456789abcdef` under `--debug`), and a `run`'s
daemon logged the same at its start in `daemon.log`. In a private mount
namespace with a tmpfs over the home, `~/c8` was `/tmp/nd.MlEH/c8` with the
previous build and the home's `c8` with this one. The daemon's sweep first
resolved the private root, and the daemon tests that start a bare server made
`~/.local/share/karvi`; it now reads the places without making them
(`BaseDirPath`, `ControlPathRootPlace`), a root not yet made holding no socket.

**Executed after 8b, exec on `system`.** The alias `fexec` over `system`
against the fake, `execution.command-timeout = 2s`,
`--continue-device-on-error`:

```text
uname -snrm      succeeded                        exit 0
both             stdout "to stdout", stderr "to stderr"   exit 0
fail 3           command_exit_nonzero: exited 3
fail 255         command_exit_nonzero: exited 255
signal TERM      command_exit_signal: ended by signal unnamed
nostatus         command_exit_missing: closed without an exit status
slow             command_timeout, notice remote_command_not_stopped
ls /nonexistent  command_exit_nonzero: exited 127
fake:   connections=1 sessions=0   channels=8   left running: "slow"
```

Every record's `credential.auth` was `publickey`, read from the master's
lines. Against this host's OpenSSH as `hx`, the table of `scrapligo-v1`'s run
above gave the same records but for `kill -TERM $$`, `exit_signal`
`unnamed`, and `sleep 32`, given up at 2 seconds with the notice and found
still running under pid 1 (then ended). The same commands over both
transports to the fake differed in `exit_signal` (`unnamed` and `TERM`) and
the system transport's notice alone. With `karvi command` killed outright
under `slow`, its master ended with it (the death signal) and removed its
socket; with the daemon killed under a `run`'s `slow`, the same. The master
not ready within the login's bound is `command_session_prompt_timeout`, the
shell's code, its cause now naming the master's `-O check`. Lifting the
refusal for `system` left `channel_exec_unavailable` unused, and the code,
never released, is deleted. The roadmap's control-socket item, built, is
removed, and `docs/FILES.md` names the control sockets.

The run at width gained `CHANNEL=exec` (`scripts/output-scale-run.sh`: the
fake's Linux persona answering `big`, the same 5,256,000 bytes, a key of the
run's own as the operator's keys). At N=32, two rounds alternating with the
previous commit's build, the daemon's peak: exec on `system` 75 and 69 MB (the
previous build refuses it), exec on `scrapligo-v1` 95 and 80 MB before, 76
and 93 after; the shell on `system` 84 and 80 before, 83 and 75 after, on
`scrapligo-v1` 154 and 159 before, 130 and 172 after: level, the spread the
runs' own.

**Executed after 8c, built-in `linux` on exec.** The chapter's first run
again, `karvi command srv1` (built-in `linux`) to this host with `id -un`, a
line to each stream, and `ls /nonexistent`, over both transports: `netops`,
`out`, `err`, then `ls: cannot access '/nonexistent': No such file or
directory` and `command_exit_nonzero: exited 2`, exit 107, where the shell had
recorded `succeeded` with bash's sequences and the login shell's
`/usr/bin/ls`. The dry run reads `platform=linux channel=exec`, while
`linux_shell` and IOS XE keep `channel=shell`.

**Executed after section 9, a server's collection.** `karvi crun --target
srv1` (built-in `linux`, this host) and `--target bast1` (`linux_shell`) with
no command each wrote the five blocks, `! cat /etc/os-release` to `! systemctl
list-unit-files --state=enabled --no-pager --no-legend`, and the two files
differed in one line: `ip -br address` pads its columns, exec keeps the
trailing blanks, and the shell's last line before a prompt loses them, the old
rule. A site's list with `cat /etc/missing` replaced the file, its block `cat:
/etc/missing: No such file or directory`, the run exit 101; against the fake,
a list with `signal TERM` kept the previous file (`replaced=0 kept=1`). The
guide's alias example, `[platform.debian]` with `dpkg-query` and `sudo -n nft
list ruleset` added, collected both on this host. A daemon started with
another configuration answered the alias's device `platform_unknown`: the
daemon resolves the definition from its own configuration; with the daemon
stopped, the client's started one served it. `crun_platform_commands_missing`
no longer names `linux`, and `docs/COLLECTION.md` gains the servers' list in
its two tables and section 2.1.

**During the build: the parity run over exec.** Before the suite's cases, the
lab build at `9ca3808` against the fake's Linux persona (a login by key), each
case over `command` and `run` on both transports, compared by
`tools/paritycheck` as it stood:

| Case | The four streams | `paritycheck` |
|---|---|---|
| `uname -snrm`, `both`, `fail 3`, `signal TERM`, `nostatus`, `slow`, `ls /nonexistent`, `sudo -n id -u`, under continue | succeeded twice, `command_exit_nonzero`, `command_exit_signal`, `command_exit_missing`, `command_timeout`, `command_exit_nonzero` (127), succeeded; exit 107 and 101; one connection, 8 channels | record 3's `exit_signal` and message (`unnamed`, `TERM`); record 5's notice `remote_command_not_stopped` on `system` alone |
| `big`, `bigerr` under a 2,000-byte limit, then `uname -snrm` | `output_limit_exceeded` twice, 2,000 bytes on the one stream each, then succeeded; exit 111 | the same notice on records 0 and 1 on `system` |
| both streams past a 4 KB spool threshold | 219,000 bytes on each, succeeded | equal |
| `sudo -n id -u` refused (`-sudo-asks`), under halt | `command_exit_nonzero`, `sudo`'s message in `stderr` | equal |
| `linux_shell` over the shell persona, plain and with bash's decorations | every command succeeded, the outputs alike with and without the decorations, one pty | equal |
| an alias of `cisco_iosxe` with `channel = "exec"` (the IOS XE persona refuses every exec request) | `ssh_session_channel_refused`, then not attempted; exit 110 | record 0: `system` `stderr` `""` with its encoding, count, and digest, `prompt_source` `none`, `prompt_observed` false; `scrapligo-v1` all null |

The last row is a defect: a refused exec request never ran, and the rule is
that a null `stderr` says so. `scrapligo-v1` learns of the refusal when it
starts the channel; `system` learns of it when the client exits and the master
has written its sign, after the session had filled the exec fields.

**Agreed.** A connection error of `ssh_session_channel_refused` at the end of a
command leaves the record as a refused start leaves it, with no exec fields; a
connection lost under a command keeps what it settled. A difference by design
is pinned: `paritycheck -pin N.PATH=TRANSPORT:JSON;TRANSPORT:JSON` names the
record's position from 0, a path and everything under it, and the value each
transport (the record's own `transport`, `system` or `native`) must hold
there, JSON or `absent`; the path is checked in every stream and left out of
the comparison across them. The suite gains `PIN` and `CHANNELS` (the exec
channels on the one connection, and no shell) as per-case settings, and the
cases S35a to S35f: the eight commands with `exit_signal`, its message, and
the notice pinned; the limit with the notice pinned; the two spools; `sudo`
refused; `linux_shell` with bash's decorations; the refused exec request.

**Not taken.** The notice or `exit_signal` excluded; pins keyed by the
implementation's name, which the record does not carry; the refusal of
`--expect` on an exec target as a parity case (it stops at planning, exit 4,
with no record); a plain `linux_shell` row beside the decorated one.

**Executed after 10a, the parity run.** S35a to S35f pass on the tree's build,
each four streams equal with the pins held; the `9ca3808` build fails S35f on
record 0's six fields above. The pins are tested on their own: a wrong value,
a transport the pin does not name, and a pin past the records are each a
finding, and a `;` inside a value does not end it.

**During the build: the runbook rows for a production server.** This host's
OpenSSH stood in for the server, the lab build against it by the operator's
key, with no backend: plain `ssh -v` read `OpenSSH_9.6p1`, `publickey,
keyboard-interactive` offered, `using "publickey"`; `command` and `run
--no-daemon` on both transports gave four equal streams (`uname -snrm`, a line
to each stream, `ls /nonexistent` as `command_exit_nonzero` with its message
in `stderr`, exit 107 and 101); `sleep 32` at a 2-second timeout was
`command_timeout` on both and the connection served the next command, the
notice on `system`, whose `sleep` was found running under pid 1 and ended,
`scrapligo-v1` leaving none; three collections over both transports were
byte-identical; `linux_shell` recorded every command `succeeded` with the
prompt `netops@dev:~$`, the stderr line in the output, and `ls`'s failure as
a success under the login shell's alias (`/usr/bin/ls`). The section's
commands, run as written against this host, passed every row twice.

**Agreed.** Section 9 of
[`docs/DEVICE-QUALIFICATION-RUNBOOK.md`](DEVICE-QUALIFICATION-RUNBOOK.md#9-a-production-server),
"A production server", rows L1 to L6 by hand, each one line in `results.tsv`:
the server's SSH and `sshd -T` (observe), the exec records on four streams,
`sudo -n` (observe), a command given up and what it left, the collection, and
`linux_shell` (observe). The trust store, the private root, and the
configuration are under the evidence directory's `work/`; the scratch and the
control sockets are in a short directory from `mktemp`, since a socket's path
is bounded and an evidence path under a home may pass 73 bytes, removed at the
end; what L4 left is found and ended through karvi, `pkill -u` limited to the
operator's own `sleep 32`.

**Not taken.** The rows in `scripts/device-qualification.sh`, whose rows send
IOS XE commands; the section placed before the existing ones (their anchors
are linked); rows labelled S, as the parity suite's cases are; the sockets
under the evidence path.

**Executed after section 10.** The executor's `device command complete`
debug line printed the result's `prompt_source`, which an exec command leaves
empty, while its record says `none`, set after the line's value was taken. It
prints the record's now. `karvi --debug command srv1 --cmd 'id -un'` to this
host, over both transports alike:

```text
98dae16:     device command complete target="srv1" index=1 status=succeeded error_code="" output_bytes=7 prompt_source=""
this build:  device command complete target="srv1" index=1 status=succeeded error_code="" output_bytes=7 prompt_source="none"
```

`bast1` over the shell reads `observed`, as before. `karvi version` still
reported command record schema 2 and registry schema 24, though `2cccab0` and
`20ae678` had moved them to 3 and 25: its counters are constants in
`internal/buildinfo`, and nothing tied them to the packages that own the
schemas. They read 3 and 25, both verifiers expect them, and a test ties each
counter to its owner (`records` for the command record, the scoreboard, the
audit, and the job; `configschema` for the configuration and the registry;
the daemon IPC's own test in `internal/ipc` already did). Against the old
constants the test fails on those two. README's counters for the tree read
registry 25 and execution plan 11.

At N=32 against `98dae16`'s build, alternating, the daemon's peak in MB:

| Channel and transport | `98dae16` | This build |
|---|---|---|
| exec on `system` | 70, 62, 71, 77 | 73, 72, 71, 77 |
| exec on `scrapligo-v1` | 78, 65, 76, 70 | 98, 78, 76, 80 |
| the shell on `system` | 90, 85 | 69, 76 |
| the shell on `scrapligo-v1` | 156, 194 | 149, 183 |

Exec read higher on this build in the first two rounds, so two more ran, exec
alone with this build first: level. The spread is the runs' own (8b's exec on
`scrapligo-v1` spanned 76 to 95), and the change, one argument of a debug line,
cannot move memory. Every run passed its accounting: 32 records succeeded, every
output 5,256,000 bytes, the spool empty after. Verification: gofmt, vet, every
Go test, `make generated-clean`, and the seventeen suites on the lab build
(22:36:10 to 22:39:50 UTC), the canary's scan finding nothing, the released
`bin/` unchanged.

**Built**, each section committed on the operator's word: the design
(`ae1d059` to `461fcb1`, with `bc80916` the `env` backend as the code reads
it); the fake renamed (`7092ee1`); terminal text rendered as the terminal
showed it (`e875c22`, `3a01fc7`, `76d9136`, and the decisions `343910b`,
`f4e4bfa`, `1874675`); the platform's `channel`, `linux_shell`, and the plan's
schema 11 (`20ae678`); the platform's fallback and the operator's keys
(`e4d82f9`, `b31dcf7`, `fc8585a`) and the method that authenticated
(`f53ccb9`); the fake's Linux persona (`e222067`); record schema 3
(`2cccab0`); exec on `scrapligo-v1` (`d6400c1`), the control socket's place
(`202a992`), and exec on `system` (`585c841`); built-in `linux` on exec
(`05664da`); a server's collection (`9ca3808`); the parity run over exec
(`0075248`); the runbook's rows for a production server (`4086ba7`); the
documents (`6f89387`, `b9c32b8`) and CHANGELOG (`98dae16`); and section 10's
last part, the debug line, the counters, and this close.

**Found on the way.** The qualification script's `statuses()` took every
`"status"` in a stream, the job summary's `recovery.status` among them, so D7
j1 failed on both transports; it reads the records alone (`897390f`). A
refused exec request on `system` recorded an empty `stderr` where it never
ran (10a). `karvi version`'s two counters, above.

## 25. Stream mode: one dash or two, quotes, and the commands that stay (2026-10-05)

The operator asked what it would take for `--command` and its variations
(`-command`, `--cmd`, `-cmd`, `-c`) to mark a command in stream mode, since
operators are in the habit of marking commands that way, while a line with no
dash stays a command; then how stream mode handles quotes (`--cmd "show
clock"`); then for a command given in option form to stay across jobs, with
directives that purge the commands and the targets, and `--exit` beside
`--end` and `--quit`.

**What it gains.** A line typed in a stream means what the same words mean on
a `run` command line, one dash or two and quotes alike, so an operator's
habits carry over instead of reaching a device as a command. A standing list
of commands across jobs (a `--cmd show clock` sent with every batch), and a
way to drop the commands or the targets that stay without retyping the rest.
It waits on nothing outside the tree.

**Executed: the dash today.** A dry run of the lab build at `97942ce` piped
into `karvi stream` (target `srv1`, built-in `linux`, this host), one line per
spelling; the plan's commands:

```text
id -un              -> "id -un"
--cmd uname -s      -> "uname -s"
--command hostname  -> "hostname"
--c date            -> "date"
-c whoami           -> "-c whoami"         sent as written
-cmd pwd            -> "-cmd pwd"          sent as written
-command uptime     -> "-command uptime"   sent as written
-go                 -> "-go"               a command, not the directive
```

On `run`'s command line one dash and two are the same: the parser strips
either before resolving the word. The stream's reader tested for `--` alone
before handing a line to that parser, so a one-dash line was a command.

**Issue 1, agreed.** A line beginning with a dash and one more character is
an option line, one dash or two alike, as on `run`'s command line, the
directives among them (`-go`, `-clear`, `-end`); a line with no leading dash
is a command, and so is `-` alone, which the command line does not take as an
option either. A command that begins with a dash goes as `--cmd`'s value
(`--cmd -v`, `--cmd=-v`).

**Not taken.** The one-dash spelling for the command options alone (an
operator with the habit types `-target` and `-go` too).

**Executed: quotes today.** The same dry run:

| Line | What the plan held |
|---|---|
| `--cmd "show clock"` | `"show clock"`, quotes included |
| `--cmd 'show clock'` | `'show clock'` |
| `--cmd="show clock"` | `"show clock"` |
| `"show clock"` | `"show clock"` |
| `echo "a  b"` | `echo "a  b"` |
| `--target "srv1"` | a second target named `"srv1"`: `dns_nxdomain`, `1 of 2 targets failed address resolution`, exit 7 |

karvi parses no quotes: on a command line the shell removes them, and a
stream line has no shell before it. The option word is cut from its value at
the first space or `=`, and the rest of the line is the value, so a value
with spaces needs no quotes; but an operator used to the command line types
them.

**Issue 2, agreed.** On an option line, a value wholly wrapped in one pair of
quotes, double or single, with no other of that quote inside, loses them as a
shell would remove them; nothing inside is read, no escapes. Every other value
is sent as written: `--cmd echo "a b"` keeps its quotes, an unmatched `"` stays,
`--cmd '"x"'` sends `"x"`, and `--cmd ""` is an empty command, as `run --cmd ""`
is on a command line. A bare line is the device's text and keeps its quotes.

**Not taken.** Shell-style word splitting, which would take the quotes from
the middle of a value (`echo "a  b"` would become `echo a b`) and bring
backslash rules; `"a" "b"` read as one wrapped pair.

**During the build: a line that swallows the commands after it.** The first
test of the dash rule found `- foo` accepted into the draft's options, and
`-typo` behind it. `run` takes freeform command text: the first argument not
spelled as an option begins it, and every argument after it joins it. A stream
line that leaves such an argument among the options passes the parser's check,
since freeform text is valid to `run`, and every command line after it joins
that text. The same shapes on `97942ce`, before any change, with `--dry-run`
set first:

```text
-- foo           -> "commands":["foo --cmd id"]
--no-daemon yes  -> "commands":["yes --cmd id"]
```

Without `--dry-run`, the job would have sent the one line `yes --cmd id` to
every target. The one-dash rule would have added `- foo` to the shapes.

**Agreed.** A line whose check leaves the parse freeform (text no option on
the line takes) is dropped with its number as `cli_positional_unexpected`, the
draft standing: `- foo`, `-- foo`, `--` alone, and a flag given text after a
space alike. The rule stays as Issue 1 states it; the guard is one check of
what the parser already reports, whatever the line's shape.

**Not taken.** `- foo` and `-- foo` as commands, the command line's own
reading of `-` and `--`, which would send quietly a line that is almost
certainly a typo.

**Issue 3, agreed: the commands that stay.** A command given in option form
(`--cmd` and its aliases in any spelling, `--cf`) belongs to the part of the
draft that stays, beside the targets and options; `--go`, `--sendit`, and
`--clear` remove only the commands given as bare lines. The draft's commands
are entries, one command each with the declarations after it and a mark for
whether it stays: the commands run in the order typed, a kept one keeping its
place and a new line following it; a declaration stays or clears with its
command, and one typed after a `--go` attaches to the last command left,
which may be a kept one; a kept `--cf` is read by each job, as each `run`
reads it. With any kept command, `--go` always has something to send; the
notice is printed only when no command of either kind is left. This reverses
chapter 7's rule, under which a `--cmd` line was cleared like a bare line
(it had stayed among the options by accident, re-sent by every job); here
staying is the operator's choice, made by the spelling.

**Issue 4, agreed: the purges and `--exit`.** Removing a kept command needs a
directive that leaves the targets, which `--reset` does not. The operator
named two, each taken by prefix from the shortest that names it alone:
`--purge-commands` (from `--purge-c`) empties every command, kept and bare,
with its declarations; `--purge-targets` (from `--purge-t`) removes every
target input run's table marks (`--target` with its aliases, `--tl`, `--tf`,
`--tfr`, `--site`, `--device-group`, `--all`, `--select-platform`, which
narrows the targets and is part of choosing them) and keeps the other
options. `--purge` and `--purge-` name both and are dropped as
`cli_option_ambiguous`. The other directives stay exact words, so `--c` keeps
meaning `--cmd`. A `--go` with no target left fails as `run` does with no
target input. `--exit` leaves, as `--end` and `--quit` do.

**Not taken.** One `--purge` for every command (the operator's two words keep
the targets' purge apart); `--purge` taken as either; every command cleared by
`--go`.

**Executed after the build.** The lab build against this host, a real
session, `--no-daemon`:

```text
--target srv1 / --no-daemon / --cmd id -un / echo "a  b" / --go
    netops, a  b                                   exit=0
-c 'uname -s' / --sendit
    netops, Linux                                  exit=0   (id -un kept; echo sent once)
--purge-c / printf 'once\n' / --go
    once                                           exit=0
--purge-t / -c true / --go
    inventory_positive_selector_missing: run requires a target input: …
--purge
    stream line 14 dropped: cli_option_ambiguous: --purge is ambiguous in stream: --purge-commands, --purge-targets
--target srv1 / --go
    (true)                                         exit=0
--exit
    the stream's exit 0; the --go after it never read
```

The same input to the `97942ce` build sent `-c 'uname -s'` and `-c true` to the
host as commands (`bash: - : invalid option`, `command_exit_nonzero`, exit 101),
refused `--purge-c`, `--purge-t`, `--purge`, and `--exit` as unknown options,
and read the `--go` after `--exit` (nothing to send). A dry run on the lab
build: `-c id -un`, `-command uname -s`, `--cmd "hostname"`, `--cmd='echo "a
b"'`, and a bare `echo "c  d"` gave `id -un`, `uname -s`, `hostname`, `echo "a
b"`, and `echo "c  d"`; `- foo` and `--no-daemon yes` were dropped with their
numbers; `-go` sent and `--go` after it found nothing to send. `--cmd -v` and
`--cmd=-v` both sent `-v`, and `--target 'srv1'` named `srv1`. Verification:
gofmt, vet, every Go test (the stream tests for each spelling, quote case,
purge, and the swallowing shapes), `make generated-clean` with `karvi-stream.1`
regenerated, and the seventeen suites on the lab build (00:08:23 to 00:12:07
UTC), the released `bin/` unchanged.

**Found on the way.** `stream`'s help gave `--tl "router1 router2"` as an
example and README `--tl "r1 r2"`: before the quote rule both carried the
quotes into the list. On `run`'s command line, a bare `-` begins freeform text
by `run`'s rule, so in `run --target srv1 - --cmd id --dry-run` the dry run
is part of the command text and the job runs; a lab probe sent `- --cmd id
--dry-run …` to this host that way (bash refused it). Noted, not changed: it
is `run`'s documented freeform rule.

**Built** in one section on the operator's word: `internal/cli/stream.go`
(the dash rule, `streamUnquote`, the freeform guard, the draft's entries with
their roles and the kept mark, the purges by prefix, `--exit`), its tests,
`stream`'s help and `karvi-stream.1`, DESIGN's stream entry, OPERATIONS' and
README's stream sections, and CHANGELOG.

## 26. The invocation's bounds on the daemon's path (2026-10-05)

The operator asked how best to raise the output byte limit for one command or
device session, and the timeout for one command along with any overall session
bound, with two cases from the fleet: a `show tech` on IOS XE or Nexus, over a
gigabyte streamed by one command for thirty minutes or more, and a `copy` of an
image to an IOS XE router, thirty minutes or more of one command printing `!`
now and then or nothing. Today's answer is a job-wide `--set` of
`execution.command-timeout` or `output.max-command-bytes`; before any
per-command form, the session found that such a `--set` did not reach a `run`
through the daemon.

**What it gains.** A job bounded as its invocation said on every path: a
`--set`, or a client configuration that differs from the daemon's, means the
same through the daemon as in process, and the per-command declarations that
follow have one path to build on. It waits on nothing outside the tree.

**Executed: the daemon's path today.** The lab build at `1ea54c4`, the lab
configuration's `execution.command-timeout = 2s`, this host as `srv1`:

| Case | Through a daemon already running | `--no-daemon` |
|---|---|---|
| `--set execution.command-timeout=10s`, `sleep 4` | `command_timeout` | succeeded |
| `--set output.max-command-bytes=2048`, 5,000 bytes | succeeded, 5,000 bytes | `output_limit_exceeded` at 2,048 |
| `--set execution.device-timeout=1s`, `sleep 2` | `command_timeout` at the configured 2s | `device_timeout` |
| `--set execution.halt-device-on-command-error=false`, a failure then `echo` | the second `not_attempted_prior_command_failure` | the second succeeded |

A daemon started by an invocation with `--set
execution.command-timeout=10s` kept it: a later plain run's `sleep 4`
succeeded under the configured 2s. The plan already carried
`output.max_command_bytes`, and the store read the plan's `max_job_bytes`;
the transports and the free-space check read the command limit from the
configuration the job ran under, the daemon's on that path. The executor took
the halt rule from its configuration as well, the plan's
`continue_device_on_error` holding only `--continue-device-on-error`.

**The operator's rule.** The daemon is the operator's own, launched by the
operator: it should accept whatever settings the client sends for a job, and
refuse a job only where a setting cannot be honoured, as it does today. A job
under the client's whole configuration is a larger change, every reader on the
job path taking a configuration built per job and a rule for the keys that
belong to the daemon's process, so it went to the roadmap
([`ROADMAP.md`](../ROADMAP.md), "A job under its client's configuration"), and
this section fixes the values the plan carried and nothing read, and the
timeouts that drifted.

**Agreed.** The plan's `execution` block carries
`execution.command-timeout`, `execution.device-timeout`,
`execution.prompt-timeout`, `execution.enable-timeout`, and
`telnet.read-timeout`; the transports and the free-space check read
`output.max_command_bytes`; `continue_device_on_error` is
`--continue-device-on-error` or the halt key set false. The executor and the
transports read them from the plan on every path, through one
`platform.Timeouts` value on each transport's factory, a field left zero
(`login`, a transport's own test) falling back to the configuration. The
daemon's plan check refuses only what no session can run: a timeout at or
below zero, a negative device timeout; the client's load has checked the
ranges, so the daemon repeats none. The plan schema stays 11, unreleased; the
pinned digests moved with the block alone (each recomputed with the
`execution` member removed matched its old pin).

**Not taken.** The client's whole configuration in this section; range
constants in the plan with a test tying them to the registry; the daemon's
connect, handshake, and keepalive settings in the plan (the transport's and
the site's); a plan schema bump.

**Executed after the build.** The same cases, each on a plain daemon started
first and with `--no-daemon`:

| Case | `1ea54c4`, daemon | `1ea54c4`, `--no-daemon` | This build, daemon | This build, `--no-daemon` |
|---|---|---|---|---|
| command timeout 10s, `sleep 4` | `command_timeout` | succeeded | succeeded | succeeded |
| command limit 2,048, 5,000 bytes | succeeded, 5,000 | `output_limit_exceeded`, 2,048 | `output_limit_exceeded`, 2,048 | `output_limit_exceeded`, 2,048 |
| device timeout 1s, `sleep 2` | `command_timeout` | `device_timeout` | `device_timeout` | `device_timeout` |
| halt false, a failure then `echo` | not attempted | succeeded | succeeded | succeeded |

At N=32 against `1ea54c4`'s build, alternating, the daemon's peak in MB:

| Channel and transport | `1ea54c4` | This build |
|---|---|---|
| exec on `system` | 65, 67, 72, 80 | 71, 71, 69, 60 |
| exec on `scrapligo-v1` | 88, 88, 81, 85 | 100, 72, 77, 94 |
| the shell on `system` | 88, 85 | 74, 79 |
| the shell on `scrapligo-v1` | 149, 168 | 166, 139 |

Exec on `system` read higher on this build in the first two rounds, so two more
ran, exec alone with this build first, and there the previous build read higher
in both: the spread is the runs' own. Every run passed its accounting.

Verification: gofmt, vet, every Go test (the plan's vectors for each refused
value, the draft carrying set and default values and the halt key, the executor
obeying the plan's 10s over a configured 1s), `make generated-clean`, and the
seventeen suites on the lab build (01:10:50 to 01:14:34 UTC), the released
`bin/` unchanged.

**Found on the way.** `docs/TIMEOUTS.md` said that only two duration ranges were
enforced; every range in its table is refused at load
(`config_value_out_of_range`), and `execution.device-timeout`'s maximum is
`168h`, the table's `7d` being no duration karvi reads
(`config_duration_error`). The per-command timeout and byte limit, the
operator's next item, build on the plan's block; a `show tech` past the 1 GiB
ceiling of `output.max-command-bytes` is a separate decision (how a record holds
output of several GiB), and until it is taken the device's own redirect to its
flash, then a `copy` off it, is the tool.

## 27. A command's own timeout and byte limit (2026-10-05)

The operator's question of chapter 26, built on its fix: the bound for one
command, a `copy` of an image to an IOS XE router that runs thirty minutes or
more with `!` now and then or nothing, and a `show tech` streaming more than a
gigabyte for as long. Chapter 26 made a job's bounds the invocation's on every
path; this chapter designs the bounds of one command within the job. The build
is the next session's.

**What it gains.** One command gets the bound it needs while the rest of the
job keeps the site's: the `copy` 45 minutes, the `show` commands around it
their 120 seconds, so a hung `show` still fails fast. The plan is the one
path to the daemon since chapter 26. It waits on nothing outside the tree; a
`show tech` past 1 GiB waits on its own decision, how a record holds output of
several GiB.

**Executed: the ground.** On the lab build at `36b553a` against this host:

- run's option table has `--timezone` and `--max-width`, so `--time` and
  `--max` would be ambiguous prefixes; `--timeout` and `--timeo`, `--maxbytes`
  and `--maxb` resolve alone. No size parser with units exists: every byte
  key takes whole bytes.
- `--expect` on a `linux` target is `channel_exec_declaration_refused` (it
  answers a terminal, which an exec channel has none of).
- Today's messages name the value that applied: `command timed out after 2s;
  the command was stopped and its channel closed` (exec), `command timed out
  after 2s while waiting for a returning prompt; the session is closed`
  (shell), `command output exceeded 2048 bytes across stdout and stderr (5000
  observed); the command was stopped`.
- A job folder's `manifest.json` holds the plan, `blind_wait_ns`, the
  profiles' `command_timeout_ns`, and the `execution` block among it.
- Over telnet a command is read under one deadline, the smaller of its timeout
  and `telnet.read-timeout` (60s by default), not reset by output
  (`readUntil` sets one read deadline): telnet's read timeout caps every
  command whole, where `docs/TIMEOUTS.md` said "each read".

**Issue 1, agreed.** `--timeout DURATION` and `--maxbytes BYTES`, declarations
on the `--cmd` before them as `--expect` and `--blind` are, at most one of
each per command, in the forms and ranges of `execution.command-timeout` (1s
to 12h) and `output.max-command-bytes` (whole bytes, 1 KiB to 1 GiB), checked
when parsed. Before any command, or with `--cf`, they are refused as the
declarations are today; they belong to the requested commands, not a
session-init profile's (which has its own `command-timeout`) or a `crun`'s
platform lists; they are accepted on an exec channel. In stream mode they
attach like `--expect`, staying with a kept command. The copy reads:

```text
karvi command rtr1 --cmd 'copy scp://host/image.bin bootflash:' --expect 'Destination filename.*\?=' --timeout 45m
```

**Not taken.** Size suffixes (`512MiB`) on `--maxbytes` while the key takes
whole bytes (suffixes for keys and options together would be their own item);
one combined option (`--limits 45m,2GiB`); the names `--command-timeout` and
`--max-command-bytes`, the keys' own, against the short-word rule.

**Issue 2, agreed.** `--timeout` replaces `execution.command-timeout` for its
command on the shell and on exec, expiring as today's `command_timeout` with
the same rule for the rest of the list. With `--expect` it is the one
deadline across every prompt and answer, never reset; keepalives still end a
dead peer. On a blind command (`--blind`, `--blind-return`) it is refused when
parsed (`timeout_with_blind`): the blind wait is that command's own, and its
success is the prompt's absence. Above a set `execution.device-timeout` it is
refused at planning (`timeout_over_device_timeout`), naming both values and
`--set execution.device-timeout=…`; at the default 0 there is no ceiling, and
a list whose total passes a set ceiling still ends `device_timeout` while it
runs. Over telnet it replaces `telnet.read-timeout` too for its command.

**Not taken.** `--timeout` as a blind command's wait; the device deadline
growing by a declared command's excess (a site's ceiling made advisory);
refusing a list whose timeouts sum past the ceiling (most lists finish far
sooner); telnet's cap left in force under a declaration.

**Issue 3, agreed.** `--maxbytes` replaces `output.max-command-bytes` for its
command, a smaller value allowed; reaching it is today's
`output_limit_exceeded` with the first BYTES kept, the shell's session ended,
the exec channel's command stopped and its connection serving the next, both
streams counted together. Above `output.max-job-bytes` it is refused at
planning (`maxbytes_over_job_limit`), naming both values and the `--set` that
raises the job limit, as the configuration refuses a command limit above the
job's (`config_output_max_job_below_max_command`). The free-space check's
spool term is the job's width × the largest command limit in the job, every
device in flight possibly sending the large one, and the narrowing rule
applies unchanged (`spool_width_narrowed`, `output_preflight_space`). Memory
is unaffected (output past `output.spool-threshold-bytes` spools); the record
is today's, the output whole in a `commands.jsonl` line and a text block, both
counted against the job limit. The ceiling stays 1 GiB.

**Not taken.** The check counting each device's own list (exact, and per
device); a declared limit raising the job limit unseen; `--maxbytes` above 1
GiB before the record decision.

**Issue 4, agreed.** The plan carries `timeouts_ns` and `max_bytes`, each
empty or one entry per command with 0 the job's value, as `blind_returns`
does, under `plan_digest` and in the manifest; the plan schema stays 11
(unreleased); the daemon's plan check refuses only a length that does not
match the commands or a negative entry, the ranges, the blind conflict, the
device ceiling, and the job limit being the client's at parse and planning.
The executor gives each command its timeout as it does now and a byte limit on
`platform.Command`, which each transport takes over the session's. A
timeout's or a limit's message names its source: `command timed out after 45m
(--timeout)`, `… after 2s (execution.command-timeout)`, `command output
exceeded 1048576 bytes (--maxbytes) across stdout and stderr …`. The `device
command start` debug line carries `timeout=` and `maxbytes=`. The documents:
the help and the manual pages of `run` and `command`, TIMEOUTS (the
declaration's row, the telnet cap), OPERATIONS (the `copy` and `show tech`
cases, the device's redirect to its flash for output past 1 GiB), DESIGN,
CHANGELOG, and this chapter's executed runs.

**Not taken.** A record field for each command's bounds (the message and the
manifest carry them); one list of objects in the plan in place of two lists.

**The design is complete.** Four issues settled, recorded in DESIGN ("A
command's own timeout and byte limit, declared"; "Declarations attach
backwards" names the two), and `docs/TIMEOUTS.md`'s telnet row corrected to
what the code does. The build followed in three sections, each committed on
the operator's word: parse and plan (`8f9266e`), execution (`8e862ba`), and
the documents with these runs.

**Executed after the build.** The lab build of `8e862ba`'s tree against this
host, the lab configuration's `execution.command-timeout = 2s`; `srv1` is
`linux` (exec), `bast1` `linux_shell` (the shell). The refusals, each exit 4
before any device:

| Invocation | Code and message |
|---|---|
| `--cmd 'sleep 1' --timeout 13h` | `cli_option_value_invalid: --timeout takes 1s..12h, the range of execution.command-timeout, not 13h` |
| `--cmd 'sleep 1' --maxbytes 512` | `cli_option_value_invalid: --maxbytes takes 1024..1073741824, the range of output.max-command-bytes, not 512` |
| `--maxbytes 1MiB` | `cli_option_value_invalid: --maxbytes takes an integer, not "1MiB"` |
| `--timeout 45m --cmd 'sleep 1'` | `declaration_before_command` |
| `run --cf FILE --timeout 45m` | `declaration_with_commands_file` |
| `--cmd 'sleep 1' --timeout 5m --timeout 10m` | `declaration_repeated: command 1 is given --timeout more than once` |
| `--cmd reload --blind --timeout 5m`, and `--cmd 'clear counters\r' --timeout 5m` | `timeout_with_blind` |
| `--set execution.device-timeout=30m … --timeout 45m` | `timeout_over_device_timeout: command 1: --timeout 45m0s is above execution.device-timeout 30m0s, …; lower the --timeout or raise the ceiling with --set execution.device-timeout=45m0s` |
| `--set output.max-command-bytes=1024 --set output.max-job-bytes=4096 … --maxbytes 8192` | `maxbytes_over_job_limit`, naming `--set output.max-job-bytes=8192` |
| `run … --max 2048` | `cli_option_ambiguous: --max is ambiguous in run: --max-width, --maxbytes` |

The copy's shape, a command printing `!` once a second for four seconds, then
`echo after`:

| Target | 2s configured | `--timeout 10s` |
|---|---|---|
| `srv1` (exec) | `command timed out after 2s (execution.command-timeout); the command was stopped and its channel closed`, 2 bytes kept | both succeeded |
| `bast1` (shell) | `command timed out after 2s (execution.command-timeout) while waiting for a returning prompt; the session is closed`, 3 bytes kept | both succeeded |

The output does not extend the deadline. On both transports and both
channels, `--cmd 'sleep 3' --timeout 10s --cmd 'sleep 3'` succeeded the first
and timed out the second `(execution.command-timeout)`; `--timeout 1s` timed
out `(--timeout)`; a 5,000-byte response under `--maxbytes 2048` kept 2,048
bytes, `command output exceeded 2048 bytes (--maxbytes) across stdout and
stderr, 5000 observed; the command was stopped` on exec and `… before the
prompt returned, 4039 observed; the session is closed` on the shell, the next
command not attempted under the halt rule; without the declaration the same
response succeeded. Through a daemon, `run --target srv1 --target bast1 --cmd
'sleep 3' --timeout 5s --cmd 'seq 1000' --maxbytes 1024
--continue-device-on-error` succeeded the sleeps under the configured 2s and
cut `seq 1000` at 1,024 bytes on both channels. The manifest held the lists as
written (`"timeouts_ns":[0,30000000000],"max_bytes":[4096,0]` for a run
declaring the second command's timeout and the first's limit), on `command`,
`run --no-daemon`, the daemon, and in stream mode, where a kept `--cmd` kept
its `--timeout` into the next job and a bare line's `--maxbytes` went with its
job alone. `--debug` shows `device command start … timeout=45m0s
maxbytes=67108864` and `… timeout=2s maxbytes=4096`. Telnet has no fake in the
lab; its unit test covers the declared timeout over a 300ms read cap, the cap
under a configured timeout, the limit, and the device deadline.

At N=32 against `8f9266e`'s build, alternating, the daemon's peak in MB:

| Channel and transport | `8f9266e` | This build |
|---|---|---|
| exec on `system` | 72, 69 | 67, 74 |
| exec on `scrapligo-v1` | 88, 79 | 80, 68 |
| the shell on `system` | 78, 71 | 78, 77 |
| the shell on `scrapligo-v1` | 178, 190 | 156, 135 |

Every run passed its accounting; the differences are the runs' own spread.

Verification: gofmt, vet, every Go test (the parser's ranges, placement,
repeats, and blind conflicts; the plan's vectors and the largest limit; the
planning refusals; the sessions' limits smaller and larger than the job's and
their messages; telnet's cap, limit, and device deadline; the executor end to
end), `make generated-clean`, the four plan digests (each, recomputed with the
two members stripped, matched its old pin), and the seventeen suites on the
lab build (02:44:20 to 02:48:03 UTC), the released `bin/` unchanged.

**Found on the way.**

- `--time` is not ambiguous after the mode: `--timezone` is a global option,
  taken before the mode alone, so `run … --time 5m` is `--timeout`. `--max`
  is ambiguous in `run` (`--max-width`), as the ground above said, and is
  `--maxbytes` in `command`.
- Over telnet a read cut by `execution.device-timeout` was recorded
  `command_timeout` with the socket's `i/o timeout` as its message, where the
  rule is that the earlier deadline names the code; it is `device_timeout`
  now, and a telnet timeout reads `command timed out after …` with its source.
- Every limit message carries its observed count after a comma (`…, 5000
  observed`), so the source in parentheses stands alone; the spool suite's
  parse follows.
- `docs/TIMEOUTS.md` said a timed-out command's record has no output; it holds
  what settled by the cut, as the spool suite checks. The registry row of
  `execution.device-timeout` still said `1s–7d`; it says `168h`.
- In stream mode a declaration line before any command is dropped as
  `cli_command_text_missing` (the probe has no command), as `--expect` is;
  left as it is.
- A shell response whose last line has no newline shares that line with the
  prompt, and the line leaves with it (`fold`'s last line; the previous build
  alike): the prompt rule, unchanged.

## 28. The resolved path of every place key (2026-10-06)

The roadmap item "The resolved path of every place key", widened by the
operator to directory paths and files generally: `config show --explain`
names, on a `resolved:` line, the path the next activity would use for each
key whose value is a place, as chapter 23 did for `basedir` and
`ssh.known-hosts-file`.

**What it gains.** An operator or an administrator reads each place
[`docs/FILES.md`](FILES.md) describes from the host itself, by the rule that
applies there (shared or individual mode, a closed shared folder, an explicit
setting), where today each chain is rebuilt by hand from the tables, and a
site's scripts take a path from the line instead of repeating a chain in
shell, as the enrollment recipe does since chapter 23. It waits on nothing,
but for `logging.file`, which has no use and waits on its own decision.

**Executed: the ground.** On a lab build of `2cdc5cc` in a private mount
namespace, tmpfs over `/tmp`, `/dev/shm`, `/opt`, `/etc/tmpfiles.d`, and the
home's `.local/share`, a configuration setting only the inventory and
`accept-new`, so every place key at its default. In each mode each key's
`--explain`, then one `command` and one daemon `run` against this host over
`system`, then what existed. Individual mode, before any activity:

```text
  basedir                        "auto"                       -> /home/netops/.local/share/karvi
  ssh.known-hosts-file           "auto"                       -> /home/netops/.local/share/karvi/known_hosts
  output.root                    "auto"                       (no line)
  tempdir                        "auto"                       (no line)
  spooldir                       "auto"                       (no line)
  ssh.control-path-root          "auto"                       (no line)
  watch.directory                "/dev/shm/karvi/scoreboards" (no line)
  sessions.shared-capacity-root  "/dev/shm/karvi/capacity"    (no line)
  daemon.socket                  "auto"                       (no line)
```

The activities then took `<basedir>/jobs`, `<basedir>/tmp`,
`<basedir>/socket/ssh`, `<basedir>/state/scoreboards`,
`<basedir>/state/capacity`, `<basedir>/socket/daemon.sock`, and
`/tmp/karvi-1000`. After `setup shared` the lines were the same but for
`basedir` (`/opt/karvi/users/netops`) and the store, while the places moved:
`/opt/karvi/shared/jobs/261005`, `/dev/shm/karvi/netops`,
`/dev/shm/karvi/netops/sockets`, `/dev/shm/karvi/scoreboards`,
`/dev/shm/karvi/capacity`, the daemon's socket under `users/netops`; the
spool stayed `/tmp/karvi-1000`. Nothing in the view told the two modes apart,
and for the two keys whose default is a literal path the value named the
shared place while the activity used the private fallback.

**Issue 1, which keys, agreed.** `config show --explain` prints `resolved:
PATH` for every key whose value is a place karvi writes or reads: `basedir`
and `ssh.known-hosts-file` as now; the directories karvi writes,
`output.root`, `transcript.root`, `crun.directory`, `tempdir`, `spooldir`,
`ssh.control-path-root`, `watch.directory`,
`sessions.shared-capacity-root`, and `daemon.socket`; and, when the value is
set, the files karvi writes or reads, `audit.file`, `inventory-source.N.path`,
and the credential backends' `path`, `ca-file`, `client-cert-file`, and
`client-key-file`, each the absolute path the activity opens. An explicit
value has the line too, the path as the activity takes it; an empty value has
none. No line for `sharedroot` (each tree asks the roots on its own, so it has
no one answer, and the trees' lines give it), for `logging.file` (no use; its
decision), for `crun.after` (an executable to run, not a place), or for the
fixed files under `basedir` (not keys; the `basedir` line places them). Not
taken: the roadmap's eight alone, which would leave out the control sockets
[`docs/FILES.md`](FILES.md) lists beside them and the files; a line for every
key.

**Action, the operator's: `watch.directory` and the scoreboards should
match.** Revisited as its own issue of this chapter: the place `karvi watch`
reads against the place the activities write (the writer falls back when the
shared folder is closed, the reader only when it is absent, and `watch` makes
the private root to find its fallback), and the names and the default (the
key `watch.directory`, the place FILES calls the scoreboards, `karvi-prune
--scoreboards`, a literal default where the other places say `auto`).

**Issue 2, finding a chain without creating anything, agreed.** The chains
decide by acting: `tempdir` and `spooldir` make each candidate and write a
probe file in it, the three trees write a probe file in the shared tree, and
the scoreboards and the ledger make their folder in an existing parent.
Executed in the namespace after `setup shared`, a judgement by permissions
alone (`test -w` and `test -x`, `access(2)`) against where a `command` then
went:

```text
== case 1: the scratch root present, the operator's folder absent
  exists, writable  /dev/shm/karvi
  absent            /dev/shm/karvi/netops
  command exit=0
  exists, writable  /dev/shm/karvi/netops           <- taken, as judged
== case 2: the operator's scratch folder closed (0500)
  exists, closed    /dev/shm/karvi/netops
  exists, writable  /opt/karvi/users/netops
  absent            /opt/karvi/users/netops/tmp
  command exit=0
  exists, writable  /opt/karvi/users/netops/tmp     <- passed to the next, as judged
== case 3: the shared jobs tree closed to the operator (root's, 0755)
  exists, closed    /opt/karvi/shared/jobs
  command exit=9
  output_directory_not_writable: the shared jobs tree /opt/karvi/shared/jobs exists and the operator cannot create files in it (...)
== case 4: /tmp out of inodes, the spool folder there and open
  free inodes on /tmp: 0
  exists, writable  /tmp/karvi-1000
  a probe file: mktemp: ... No space left on device
  command exit=0
  exists, writable  /var/tmp/karvi-1000             <- passed by; permissions alone said /tmp/karvi-1000
```

The rule: nothing is written; each candidate is judged as the activity judges
it. One that exists must be a real directory, not a link, that the operator
can write and search (`access(2)`), the private places (the control sockets)
with their owner and mode checked too, and one on a filesystem that counts
inodes and has none free is passed by as the probe passes it. One that is
absent is the folder the activity would make, where the activity's own rule
lets it: for the scratch and spool chains (`MkdirAll`) the nearest existing
ancestor a writable real directory; for the scoreboards and the ledger the
parent itself present and writable, an absent parent giving the private
fallback as now. Each chain has one chooser that reads the file system, as
`chooseBaseDir` is the private root's, called by the activity's maker and by
the twin `--explain` uses; the maker keeps its creation and its probe, the
activity's real test unchanged. What only a write shows, a network
filesystem's refusal on the server or a quota per user, can still differ;
[`docs/FILES.md`](FILES.md) says the line is judged by permissions and free
inodes. Not taken: the probe file from `--explain` (it leaves nothing, but it
writes in the shared trees and moves their times, and chapter 23's precedent
creates nothing); the first candidate alone (wrong in cases 2 and 4); the
activities judging by `access(2)` too (the two agree by a weaker test).

**Issue 3, a candidate passed by or refused, agreed.** A chain ends three
ways, executed in the namespace after `setup shared`. Refused: with
`/opt/karvi/users` at 0777 and no folder for the operator, the `command`
exited 9 with `private_directory_not_writable: /opt/karvi/users is writable
by everyone (mode 0777); …`, and the view already printed the same code and
message, `config show` exiting 0:

```text
  resolved:   error: private_directory_not_writable: /opt/karvi/users is writable by everyone (mode 0777); ...
```

Passed by with a warning: with the scoreboards and the ledger closed to the
operator the `command` warned `shared capacity root unavailable
(capacity_root_unusable: …); using private fallback
/opt/karvi/users/netops/state/capacity` and `shared scoreboard directory
unavailable (/dev/shm/karvi/scoreboards: permission denied); using private
fallback /opt/karvi/users/netops/state/scoreboards`, while the view showed
`value: "/dev/shm/karvi/scoreboards"` and no line. Passed by in silence:
issue 2's cases 2 and 4, the scratch and the spool moved and nothing said why.

The rule: a candidate taken is `resolved:   PATH`, as now. A refusal is
`resolved:   error: CODE: message`, the code and message the activity refuses
with, as `basedir`'s is; `config show` exits 0, a view, and a script reading
the line tests for `error:`. A candidate passed by is `resolved:   PATH`, the
one taken, followed by one `passed:` line for each earlier candidate that
exists and was passed by, with its reason: the activity's own words where it
warns, else issue 2's judgement (`not writable by the operator`, `not a real
directory`, `no free inodes`). An absent candidate is not listed: absence is a
host's ordinary state, and the activity passes it in silence too. For the
scoreboards above:

```text
value:      "/dev/shm/karvi/scoreboards"
resolved:   /opt/karvi/users/netops/state/scoreboards
passed:     /dev/shm/karvi/scoreboards: not writable by the operator
```

It answers issue 1's finding too: a literal default's value names a place the
activity does not use, and the lines under it say so. Not taken: the reason on
the `resolved:` line (the enrollment recipe's `sed -n 's/^resolved: *//p'`
would no longer give a path); every candidate listed, the absent ones too;
an exit other than 0 for a refusal (`--explain` without a key prints every
key, and one refused place would fail the view); a warning on standard error
from the view.

**Issue 4, whose configuration, settled by the operator.** Executed in the
namespace: a daemon started under the lab configuration, every place at its
default, then a client setting `tempdir`, `spooldir`, and `output.root` to
lab folders, once through that daemon and once in process:

```text
== a run through that daemon with the same three settings
  run exit=0
  ls: cannot access '/mnt/lab/s28/t4': No such file or directory     <- tempdir ignored
  ls: cannot access '/mnt/lab/s28/sp4': No such file or directory    <- spooldir ignored
  /mnt/lab/s28/o4                                                    <- output.root honoured
== the same settings, in process (command)
  /mnt/lab/s28/o4
  /mnt/lab/s28/sp4
  /mnt/lab/s28/t4
```

The plan carries the trees the client resolved; the daemon resolves
`tempdir`, `spooldir`, `ssh.control-path-root`, `watch.directory`, and
`sessions.shared-capacity-root` from the configuration it started with.
`basedir` and `daemon.socket` choose the daemon the client reaches, so they
agree by construction. The rule: `config show --explain` resolves every line
from the configuration the invocation loads (the site's and the operator's
files, `--config`, `KARVI__`, `--set`), as the next activity in process would
use it, and never consults a running daemon. A daemon started under another
configuration keeps its own places for those five keys until the operator
restarts it: the operator checks a new configuration's places with
`--explain`, then restarts the daemon. [`docs/FILES.md`](FILES.md) and the
view's help say so in one sentence; nothing warns or refuses. The roadmap item
"A job under its client's configuration" gains the places: a job's
directories and files resolved as its client resolved them, which holds while
the client and the daemon share the host, the request travelling over the
operator's Unix socket; the ledger's place moves from the process's side to
the job's, the in-flight limit staying the process's. Not taken: the five
keys carried in the plan now (the roadmap item's work, done once for every
key); `--explain` asking the daemon; a warning when the client's places and
the daemon's differ.

**Issue 5, `~` and a relative path, agreed.** Executed in the namespace, the
account's home (`/home/netops`, the password database's) a tmpfs holding only
a copy of the key, `HOME` a lab folder `h`, the working directory a lab
folder `wd`, each place key set to `~/NAME` and then to a relative `rNAME`
for one `command` in process, `daemon.socket` for a daemon `run`:

| Key | `~/NAME` went to | relative `rNAME` went to |
|---|---|---|
| `tempdir`, `spooldir`, `output.root`, `ssh.control-path-root` | the account's home | the working directory |
| `ssh.known-hosts-file` | the account's home | the account's home |
| `audit.file` | `$HOME` (`h/auditf`) | the working directory |
| `watch.directory`, `sessions.shared-capacity-root` | nowhere: the parent `~` absent, the private fallback taken in silence | the working directory |
| `daemon.socket` | a folder named `~` in the working directory, then exit 112, `ipc_result_malformed: daemon validation: credential_channel.socket must be an absolute path` | `d.sock` in the working directory, then the same |

Four rules, and one key that no value but an absolute path works for; the
files of issue 1 (the inventory, the credentials) already take `~` from the
password database and a relative path from the working directory. The rule,
one function for every place key and every file key of issue 1: `~` and
`~/…` are the operator's home from the password database, `~user` refused
(`path_other_user_home_unsupported`); a relative path is taken from the
invoking client's working directory and made absolute (a daemon a client
started resolves from that client's, issue 4's concern of the operator's);
the `resolved:` line prints the result, the `value:` line what the operator
wrote. It changes `audit.file` (no `$HOME`), a relative trust store (the
working directory, not the home), `watch.directory` and the ledger's root
(`~` honoured), and `daemon.socket` (`~` and a relative path made absolute,
no exit 112). Not taken: refusing a relative path (`--cd ./out` and the
trees' documented rule use it); a path relative to the configuration file
that sets it (a `--set` or `KARVI__` value has no file; includes keep their
own rule); the trust store's relative path left under the home (two rules
again); `$HOME` anywhere.

**Issue 6, the scoreboards, the operator's action, agreed.** Executed in the
namespace, individual mode and then after `setup shared` with
`/dev/shm/karvi/scoreboards` closed to the operator (root's, 0755), each
scoreboard aged three days and `karvi-prune --days 1 --dry-run --verbose`:

```text
== individual: watch before any activity
  watch| (no retained activities)
  /home/netops/.local/share/karvi                          <- watch made the private root
== individual: a command, then watch and prune
  board: /home/netops/.local/share/karvi/state/scoreboards/261006-003336-00.json
  watch|   261006-003336-00  00:33:37  completed   netops    cmd   1/1   0   0  srv1
  prune| walk kind=scoreboard path=/dev/shm/karvi/scoreboards    <- the board aged 3 days, --days 1: not walked
== shared, the scoreboards closed to the operator: a command, then watch and prune
  warning: shared scoreboard directory unavailable (/dev/shm/karvi/scoreboards: permission denied); using private fallback /opt/karvi/users/netops/state/scoreboards
  board: /opt/karvi/users/netops/state/scoreboards/261006-003337-00.json
  watch| (no retained activities)                            <- the operator's own job is invisible
  prune| walk kind=scoreboard path=/dev/shm/karvi/scoreboards
```

`karvi watch`, a view, made the private root (it calls `ResolveBaseDir`);
it read the fallback only when the shared folder was absent, so with the
folder closed the operator's own job, written to the fallback, was not on the
screen; `karvi-prune` walks `--scoreboards` alone, so the fallback is never
pruned, on every host without the scratch root. And the names did not match:
the place is the scoreboards in [`docs/FILES.md`](FILES.md), in
`karvi-prune --scoreboards`, and in `<basedir>/state/scoreboards`, while the
key, which every activity writes by and `watch` only reads, was
`watch.directory`, its default a literal path where the other places say
`auto`.

The rule, in three parts. (a) `watch.directory` becomes the top-level key
`scoreboards` (`KARVI__SCOREBOARDS`) beside `tempdir` and `spooldir`, default
`auto`: `/dev/shm/karvi/scoreboards` where the site's scratch root
`/dev/shm/karvi` exists (the folder made in it, with the root's bits, when
missing), else `<basedir>/state/scoreboards`; a folder present but closed is
passed by with its warning and its `passed:` line (issue 3). An operator's
run never makes `/dev/shm/karvi`: that is `setup shared`'s, and a root an
operator made would close it to the others and take root to repair. An
explicit path replaces the chain, used or refused, as every other place key's
is. `sessions.shared-capacity-root` keeps its name and takes `auto` by the
same shape, `/dev/shm/karvi/capacity` where the scratch root exists, else
`<basedir>/state/capacity`. `[watch]` keeps the viewer's own settings. A
breaking change: the registry's counter moves, the suites' settings follow, no
migration. (b) `karvi watch` makes nothing (`BaseDirPath`) and reads every
place of the chain that exists, the shared folder where it is readable and
the operator's private fallback where it holds files, one row per job, so the
team's jobs and the operator's own both show; under an explicit `scoreboards`
it reads that folder. (c) `karvi-prune` walks, beside `--scoreboards`,
`state/scoreboards` under each private root it walks (the operator's own, or
as root each site user root), its ownership rule unchanged. Not taken: the
key kept and the mismatch documented; `watch` reading only the first usable
place (today's defect in the closed case) or only the private fallback (it
loses the team's view the operator can still read); `karvi-prune` reading the
configuration (DESIGN keeps it free of one).

(d), agreed: no operator's run makes a place `setup shared` makes, whichever
key's path passes through it. Executed in the namespace with no scratch root,
an explicit value under it:

```text
== tempdir=/dev/shm/karvi/x, no scratch root
  command exit=0
  drwx------ netops:netops /dev/shm/karvi
== spooldir=/dev/shm/karvi/x, no scratch root          (the same)
== ssh.control-path-root=/dev/shm/karvi/x, no scratch root   (the same)
== watch.directory=/dev/shm/karvi/x, no scratch root
  stat: cannot statx '/dev/shm/karvi': No such file or directory
```

`MkdirAll` made the scratch root at 0700, the operator's, closed to every
other: what releases before 0.26.0 left and `setup shared` has to repair. The
rule: the scratch root `/dev/shm/karvi`, `/opt/karvi` and `/var/lib/karvi`,
their `users` and `shared`, and the trees under `shared` are made by `setup
shared` alone, whether a path reaches them by `auto` or explicitly. Inside
those that exist an operator's run makes its own folders, as now: its
`<username>` folder and `sockets` in the scratch root, a missing `scoreboards`
or `capacity` with the root's bits, its folder under `users`, the day and job
folders in the trees. An explicit path that would need one made is refused
before any device is contacted, with the registered `shared_directory_absent`,
naming the place, `sudo karvi setup shared`, and the key to set elsewhere;
`--explain` prints the refusal (issue 3). One guard in the one place that
makes the scratch, spool, and private chains' directories finds the missing
components and refuses a setup place among them; it covers all of them, so no
host's permissions are relied on (an operator reaches only `/dev/shm/karvi`
where `/opt` and `/var/lib` are root's). With every key at its default nothing
is refused: in individual mode every place is under `~/.local/share/karvi`,
the home the password database names, but the spool, `/tmp/karvi-<uid>`
(then `/var/tmp/karvi-<uid>`), which must cost disk and is never shared.
Not taken: guarding the scratch root alone (a writable `/opt` would repeat
it); the root made in the operators' group by an operator's run (the group is
the site's word, given to `setup`); such an explicit path passed by to a
fallback (an explicit value replaces its chain).

(e), agreed: `karvi-prune` run by an operator walks every place the
operator's runs could have written. Executed in the namespace:

```text
== a fresh host: prune before any activity
  prune| walk kind=activity path=/home/netops/.local/share/karvi/jobs
  prune| walk kind=scoreboard path=/dev/shm/karvi/scoreboards
  /home/netops/.local/share/karvi                     <- prune made the private root
== after setup shared (the home's job from before still there), prune as the operator
  prune| walk kind=activity path=/opt/karvi/users/netops/jobs
  prune| walk kind=activity path=/opt/karvi/shared/jobs
  prune| walk kind=transcript path=/opt/karvi/users/netops/transcripts
  prune| walk kind=transcript path=/opt/karvi/shared/transcripts
  prune| walk kind=scoreboard path=/dev/shm/karvi/scoreboards
                                                      <- ~/.local/share/karvi not walked
```

It walked the one private root the chain picks, made when missing, so after
`setup shared` the jobs an operator left under the home in individual mode
were never pruned; root's run walked the same. The rule: an operator's run
walks, where each exists and making nothing, the shared trees under
`sharedroot` and the shared scoreboards, as now, and every private root of the
operator's, the site's `users/<user>` under `/opt/karvi` and `/var/lib/karvi`
and the home's `~/.local/share/karvi`, each one's `jobs`, `transcripts`, and
`state/scoreboards`; it removes only what the operator owns, as now;
`--basedir PATH` replaces the private roots with that one. Root's run is
unchanged, the site's `users` roots and the shared trees, with their
`state/scoreboards` by (c); an operator's home is the operator's own run's.
A unit running it as an operator carries the home's root on its
`ReadWritePaths` line. Not taken: the one root the chain picks (today's gap);
root walking every home the password database names (a root job reaching into
the homes, where each operator's own timer covers them).

**Issue 7, every place at once, agreed.** Executed on the lab build:

```text
== every key
1595                          <- lines
177                           <- keys
== two keys
cli_positional_unexpected: config show accepts at most 1 positional argument(s), got ["basedir" "tempdir"]
== a recipe over the whole view, as the lines are today
basedir                  /tmp/nd.MlEH/s28/pb7
ssh.known-hosts-file     /tmp/nd.MlEH/s28/pb7/known_hosts
== a prefix
key: ssh
error: not found
```

The rule: `config show --explain KEY [KEY…]` (and `config show KEY [KEY…]`,
which implies `--explain`) renders each key named, in the order given, an
unknown one `error: not found` among the others, so a script takes the few
places it needs from one load of the configuration. The whole table is a
recipe, not an option: [`docs/FILES.md`](FILES.md), "A quick look at a
host", gives

```bash
karvi config show --explain | awk '/^key:/{k=$2} /^(resolved|passed):/{print k": "$0}'
```

one line per place and per candidate passed by, read from the host, nothing
created, beside the `stat` recipe for the modes; the help and the manual page
say the view takes several keys and that `resolved:` and `passed:` follow
their key. Not taken: a `--places` option or a new command word (chapter 23's
not-taken, and the recipe gives the table with no surface to keep in step with
issue 1's list); a prefix or a pattern (`ssh` is an unknown key and says so,
and a prefix would mix every other key under it); the view without a key
printing only the keys with a line (it changes the full view for every other
use).

**The design closed.** The build follows in sections, each committed on the
operator's word.

**Executed after the build.** The lab build of the tree after section 4
(`7da2d8f`'s content) in a private mount namespace, as for the design: in each
case the view's lines (FILES' recipe, or the keys named), then one `command`
against this host, then where it went:

| Case | The view | The activity |
|---|---|---|
| individual, before any activity | every place under `~/.local/share/karvi`, the spool `/tmp/karvi-1000`; the private root not made | took each, as named |
| after `setup shared`, before the operator's first activity | `/opt/karvi/users/netops`, the shared trees, `/dev/shm/karvi/netops` and its `sockets`, `/dev/shm/karvi/scoreboards` and `capacity` | took each, as named |
| the operator's scratch folder closed (0500) | `tempdir` at `/opt/karvi/users/netops/tmp`, `passed: /dev/shm/karvi/netops: not writable by the operator` | took `users/netops/tmp` |
| the shared jobs tree closed (root's, 0755) | `resolved: error: output_directory_not_writable: …` | exit 9, the same code and message |
| the scoreboards and the ledger closed | the private folders, each with its `passed:` line | the private folders, each with its warning; `watch` showed the operator's jobs |
| `/tmp` out of inodes, the spool folder there and open | `/var/tmp/karvi-1000`, `passed: /tmp/karvi-1000: no free inodes` | took `/var/tmp/karvi-1000` |
| `tempdir=/dev/shm/karvi/x`, no scratch root | `resolved: error: shared_directory_absent: /dev/shm/karvi is absent, …`; `config show` exit 0 | exit 9, the same refusal; nothing made |
| `users` at 0777, no folder for the operator | `basedir`, the store, and `tempdir` each `error: private_directory_not_writable: …` | exit 9, the same message |
| `config show basedir ssh spooldir` | three blocks, `key: ssh` / `error: not found` between | |

Issue 5's run on the build (every key at `~/NAME`, then at `rNAME`, `HOME`
elsewhere): every place, the audit file, the scoreboards, and the ledger
included, went under the account's home, then under the working directory;
`daemon.socket` at `~/d.sock` and at `d.sock` started the daemon on
`/home/netops/d.sock` and on the working directory's, each `run` exit 0, where
the `2cdc5cc` build had exited 112. Issue 6(d)'s, with `/opt` writable by
everyone so nothing but the guard stood in the way: `tempdir`, `spooldir`,
`ssh.control-path-root`, the trust store, and `audit.file` under an absent
`/dev/shm/karvi`, `output.root` under an absent `/opt/karvi`, and
`daemon.socket` under the scratch root, each exit 9 with
`shared_directory_absent` naming its key and nothing made, where the earlier
build had made each root at 0700, the operator's (`transcript.root` is not read
by a `command`). Issue 6's: `karvi watch` before any activity made nothing;
with `/dev/shm/karvi/scoreboards` closed it showed the operator's job written
to the fallback, where it had shown none. Issue 6(e)'s: on a fresh host
`karvi-prune --verbose` walked only the shared scoreboards and made nothing;
after `setup shared`, with the home's job from before,

```text
walk kind=activity path=/opt/karvi/users/netops/jobs
walk kind=activity path=/home/netops/.local/share/karvi/jobs
walk kind=activity path=/opt/karvi/shared/jobs
walk kind=transcript path=/opt/karvi/users/netops/transcripts
walk kind=transcript path=/home/netops/.local/share/karvi/transcripts
walk kind=transcript path=/opt/karvi/shared/transcripts
walk kind=scoreboard path=/opt/karvi/users/netops/state/scoreboards
walk kind=scoreboard path=/home/netops/.local/share/karvi/state/scoreboards
walk kind=scoreboard path=/dev/shm/karvi/scoreboards
```

and root's run walked the site's root, the shared trees, the site root's
`state/scoreboards`, and the shared scoreboards. Verification at each section:
gofmt, vet, every Go test (each twin against what its maker then takes:
absent, closed, explicit, the guard, no free inodes), `make generated-clean`,
the full battery on the section's lab build, and the run at width N=32 against
the previous commit's build after sections 2 and 3, within the noise; the
released `bin/` unchanged.

**Found on the way.** An explicit `ssh.known-hosts-file` under an absent setup
place was refused once per device, the message lost behind `command failed for
name:srv1`; the store's path is now checked at the job's admission with the
scratch and the spool. The ledger never judged its root's own permissions: its
twin, judging by `access(2)`, named the private folder for a root closed to
the operator while `capacity.New` took the root; `ensureRoot` now checks it.
`config show` still took one positional (the parse table's `maxPos: 1`), found
by the first run of several keys. `configs/example.toml` and
`development.toml` set both scratch folders to `~/.local/share/karvi/…`, which
the scoreboards and the ledger had ignored; both settings are removed, `auto`
giving the same private folders. `configload.ResolvePath` and
`systemssh.expandHome` were read by nothing and are removed.

**Built** in four sections on the operator's word: `8fa4c8f` the places'
rules (one path function, the guard, each chain's chooser and twin), `0da4268`
the scoreboards (the key, the `auto` defaults, `watch`'s reading,
`karvi-prune`'s walk, the suites and packaging), `7da2d8f` the view (the
`passed:` lines, every place key, several keys, the help), and the documents:
FILES, OPERATIONS, SCALE, PRUNE, SSH-HOST-KEY-POLICY, `karvi.1`,
`karvi-prune.8`, `karvi-watch.1`, ROADMAP (the item removed), and CHANGELOG.
The chapter is closed.

**Found after the close.** Before the release: the `scoreboards` row carried
no `Since`, so the registry gave it the default `0.2.0` where `ssh.identities`,
new in the same release, says `0.27.0`; and `watch.directory` had no row among
the removed keys, so a configuration setting it met `config_unknown_key` (from
the environment `config_unknown_environment`) where a removed key meets
`config_key_removed` with what replaces it. The row says `Since: "0.27.0"`, and
the key is refused from a file, the environment, and `--set` alike:

```text
config_key_removed: removed in v0.27.0; the scoreboards' folder is the top-level key scoreboards now for watch.directory at --set[3]
```

Registry 26 stays: `Since` is a field of the rows the counter already covers.

## 29. The release 0.27.0 (2026-10-06)

The fourth public release, on the operator's word, carrying chapters 22 to 28;
before the number, the two gaps chapter 28 left in the registry and the
removed keys, closed as a section of their own. The sequence is
[chapter 21](#21-the-release-0260-and-licensemd-2026-10-04)'s, with the same
tools.

**What it gains.** A site installs the Linux servers over the exec channel, the
output as the terminal showed it, a command's own bounds and the daemon's runs
bounded as their invocations said, the stream-mode fixes, and every place
resolved by one rule and named by `config show --explain`, from a published
artifact; and a configuration that still sets `watch.directory` is told what
replaces it.

**Before the number.** The `scoreboards` row carried no `Since`, so the
registry gave it the default `0.2.0`, and `watch.directory` had no row among
the removed keys, so a configuration setting it met `config_unknown_key` (from
the environment `config_unknown_environment`) where `ssh.pubkey-authentication`
meets `config_key_removed` with what replaces it. `afd12df` gives the row
`Since: "0.27.0"` and the key its row, refused from a file, the environment,
and `--set` alike; registry 26 stays, `Since` being a field of the rows the
counter covers. The full battery passed on its lab build, the canary 0 hits.

**The sequence, as run**, 46 minutes 21 seconds from the baseline's start to
the artifacts' end, the stops for the operator's word included:

| Step | Wall (UTC) | Result |
|---|---|---|
| the baseline on a clean clone of `dev` at `afd12df` | 12:32:12 to 12:39:10 | gofmt, make, the release verifier, exit 0 each |
| the number in its eight places; the release-identity build | 12:52:20 | `BUILD_TIME=2026-10-06T00:00:00Z` given to every tool; the changelog's Unreleased block became the release's, under a lead paragraph naming what a site has to change |
| the compatibility example | 12:52:40 | the released v0.26.0 executable, copied out of `bin/` and checked against `CHECKSUMS.sha256` before the rebuild, started its daemon; this client read `compatible: false` on the version alone, its run was refused with `daemon_incompatible` (exit 112) before any job, the client's `daemon stop` ended it |
| 1/3 | 12:55:14 | `60ee9a9` |
| the core evidence | 12:55:19 to 12:57:22 | 875 named tests across 83 packages, vet 0, both socket lengths exit 0 |
| the documents | 13:05:40 | `6b9dcb4` (2/3): `release/` from the v0.26.0 pattern; BUILD-HOWTO's registry line, stale since 0.26.0 |
| the remaining evidence | 13:05:44 to 13:12:15 | the shipped checks exit 0, the release verifier exit 0, the checksums unchanged by its rebuild; the replay skipped |
| 3/3, the tag, `main` | 13:13:17 | `8250167`, `karvi-v0.27.0`, `main` fast-forwarded from `72bf32d` |
| the artifacts | 13:13:27 to 13:18:33 | the bundle reproducible byte for byte and verified from its own archive; 15,760,203 bytes, 0.3 MB more than v0.26.0's |
| the push and the GitHub release | 13:21:27 | `dev`, `main`, and the tag pushed; release `karvi-v0.27.0` with the three assets, marked latest; the asset downloaded back matches |

**Executed.** The section's refusal, the example's turning points, and the
published state:

```text
$ karvi --set 'watch.directory="/tmp/x"' config show basedir    # afd12df's lab build
config_key_removed: removed in v0.27.0; the scoreboards' folder is the top-level key scoreboards now for watch.directory at --set[3]
$ grep -E 'client_version|^compatible|^exit=112|^daemon_incompatible' release/evidence/ipc-schema-compat.log | cut -c1-96
client_version: 0.27.0
compatible: false
exit=112
daemon_incompatible: running daemon version 0.26.0 uses IPC schema 10; karvi 0.27.0 uses schema 10,
$ gh api repos/robert-patrick-texas/karvi/releases/latest --jq '"latest: \(.tag_name) draft=\(.draft) prerelease=\(.prerelease)"'
latest: karvi-v0.27.0 draft=false prerelease=false
$ curl -sL .../karvi-v0.27.0-source-linux-amd64.tar.gz | sha256sum | cut -c1-16; cut -c1-16 karvi-v0.27.0-source-linux-amd64.tar.gz.sha256
b724a3b5149e3acd
b724a3b5149e3acd
```

**Found on the way.** The credential package moved from 1 to 2 with the
operator's keys, a counter the hand-off's list did not name; the lead
paragraph, the build result, and the manifest carry it, read from the source
at both tags. BUILD-HOWTO's §8 still named registry 22, stale since 0.26.0's
24; the 2/3 sweep set it to 26. The scrapligo evidence said the parity suite
ran against the fake IOS XE device; since S35a to S35f it runs the Linux
persona as well, and the sentence names both. The compatibility example's
script and the 2/3 generator of chapter 21 had not been kept; the script was
rewritten and is kept beside the tree with this release's logs. Its rehearsal,
with a lab build still numbered 0.26.0 as the client, found that such a build
passes the daemon's version check against the released 0.26.0 daemon and is
refused one step later, by the daemon's header check (`job_header_invalid:
schema_version: 11 is not the supported version 10`, exit 112); a release's
number makes the pair `compatible: false` before that.

**Not taken.** The plan schema in the daemon's compatibility check: only a
development build shares a released version, and the header check refuses its
job before anything runs. A patch release for the removed-key row: it rides
with the number it names.

**Roadmap.** The outline's items, the scratch sweep first; the package's
contents, the roadmap's first item.

## 30. The scratch sweep (2026-10-06)

The outline's item "the scratch sweep": what a session killed outright leaves
in the scratch, where [`docs/DESIGN.md`](DESIGN.md) says that nothing sweeps
it.

**What it gains.** In individual mode the scratch is `<basedir>/tmp`, on disk,
so what each killed session leaves stays across reboots and grows with every
kill; in shared mode it is `/dev/shm/karvi/<user>`, memory until the next boot.
The files are small and hold no secret (the `ssh` configuration is a kilobyte
of paths and algorithms; the askpass socket is dead; the timing log grows with
the session's output), so the gain is a scratch that stays bounded and the
DESIGN gap closed. The cases found a larger one, issue 2. It waits on nothing.

**Executed: the ground.** On a lab build of `afd12df`, every place under a lab
folder, against this host's OpenSSH by the operator's key: a `command` over
`system` running `sleep 30`, killed with `SIGKILL`, on the shell channel
(`bast1`, `linux_shell`) and on the exec channel (`srv1`, `linux`); a `run`
whose daemon was killed so; and a recorded login whose karvi was killed so.

| Case | Left in the scratch | Left running |
|---|---|---|
| `command`, shell channel, the client killed | `karvi-ssh-*.conf` | its `ssh`, its parent now init, holding the remote login shell two minutes later |
| `command`, exec channel, the client killed | `karvi-ssh-*.conf` | nothing here: the master's death signal ended it and its socket; the remote `sleep` ran on (`remote_command_not_stopped`) |
| `run`, shell channel, the daemon killed | `karvi-ssh-*.conf` | its `ssh`, as in the first case |
| `login --record`, karvi killed | `karvi-ssh-*.conf`, `karvi-script-*.timing`, `askpass-*.sock` | nothing once the terminal closed; the transcript kept raw, as documented |

**Issue 1, the sweep's rule, agreed.** Each scratch file karvi makes carries
its maker's pid in its name, `karvi-ssh-<pid>-*.conf` and
`karvi-script-<pid>-*.timing`, where `CreateTemp`'s random suffix alone named no
owner; such a file is swept by the spool's rule (`output.SweepSpools`): the name
karvi makes, owned by the operator, its pid not alive as a karvi executable.
The askpass socket is swept by the control sockets' rule: the name karvi makes,
owned by the operator, and refusing a connection ([chapter
33](#33-the-askpass-socket-named-by-its-makers-pid-2026-10-07) replaced it by
the maker's pid: the probe took a live broker's one connection). The sweep runs
where those two run, at the daemon's start and at every admission, and at a
login's start, which has no admission, over the scratch the invocation resolves;
anything else in the folder is not karvi's and is not touched. Not taken: a
sweep by age (a recorded login runs for hours); `karvi-prune` (it reads no
configuration, cannot judge an owner alive, and would reach the scratch only by
a walk of its own); every candidate of the chain (the other sweeps take the
place resolved).

**Issue 2, a session's processes end with karvi, agreed.** The ground's first
and third cases left more than files: on the shell channel the system
transport's `ssh` has no death signal, where the exec masters have one, so when
its parent died its standard input reached EOF, which on a pty does not end the
remote shell, and the device's session was held: a vty on a router until its
exec-timeout, on a server without `TMOUT` for good. A plain `login` killed with
its terminal still open left its `ssh` the terminal's foreground job (`S+`),
its parent init, competing with the shell until the terminal closed. The
in-process transports have no such case; their connection ends with the
process. A trial in a scratch worktree started the shell channel's `ssh` as
the exec master is started, then ran the cases again:

| Case | `afd12df` | The trial |
|---|---|---|
| `command`, shell channel, the client killed | `ssh` under init, the remote shell held after two minutes | `ssh` ended at once; no session from 127.0.0.1 left |
| `run`, shell channel, the daemon killed | the same | `ssh` ended at once; no session left |

The rule: every OpenSSH or `script(1)` process karvi starts for a device
session ends when its parent dies: the shell channel's `ssh`, the interactive
`ssh` of a login, and a recorded login's `script(1)`, which ends its own child
on `SIGTERM`. Each is started as the exec master is, `Pdeathsig: SIGTERM`, by
one goroutine that holds its OS thread for the process's life, since the
signal follows the thread that started the child. A killed karvi ends the
device's session as a closed connection does; the remote shell takes its
hangup, so a shell command, unlike an exec one, does not run on. The DESIGN
entry for the masters widens to every session process. The killed sessions
also left an `askpass-*.sock` each, the broker started for every system
session under keys too; issue 1's rule takes them. Not taken: a sweep of
orphaned `ssh` processes at the next start (the session held until karvi next
runs, and another process judged by its command line); `ServerAlive` (the
device answers it, so a live abandoned session never ends); the login left to
the terminal's hangup (a killed karvi under an open terminal leaves an `ssh`
fighting the shell).

**Found on the way: a lost exit status on the system transport's exec
channel.** The full battery on section 2's lab build failed in the parity
suite: S35a's `ls /nonexistent` over `system` was `command_exit_missing`,
"closed without an exit status", where every other stream had exit 127. Run
again and again, the parity suite's S35 cases failed so on that build in the
battery, in one run of the suite alone (S35b), and in 2 of 35 loops; on the
build before it in none of 35 loops and two batteries; and on the released
0.27.0 executables in 1 of 20 loops, S35a's first record, `uname -snrm`, over
`run`: a command that exits 0. Forty runs of S35a's commands per build outside
the suite lost nothing. Every failure is one family: over `system`, on whichever
record, the command's status or signal is lost. karvi says so only when the
command's `ssh -S` client exits 255 or by a signal and the master's lines show
no status for its channel, so the race is in that client's exit or in the
lines' attribution to the command (the settle wait included); it is not the
sweep's, which in the suite finds an empty scratch at admission and touches
neither the client nor the master. Section 2 was committed on the operator's
word after a battery that passed, and the race is an item of its own, taken up
after this effort. Its cause was the fake device's, not karvi's: [chapter
32](#32-the-lost-exit-status-found-in-the-fake-device-2026-10-07).

**Executed after the build.** Each kill case on a lab build of `afd12df` and on
the build of section 1, each from no session (`who` counts the sessions from
127.0.0.1):

| Case | `afd12df` | Section 1 |
|---|---|---|
| `command`, shell channel, the client killed | `ssh` running, the session held | `ssh` ended, no session |
| `run`, shell channel, the daemon killed | `ssh` running, the session held | `ssh` ended, no session |
| `login`, karvi killed, the terminal open | `ssh` running, the session held | `ssh` ended, no session |
| `login --record`, karvi killed, the terminal open | `script(1)` running, the session held | `script(1)` ended, no session |
| `command`, exec channel, the client killed | the master ended, its socket removed, the remote `sleep` running on | the same |

Then on the build of section 2, with a file under an earlier release's name,
`karvi-ssh-1234567.conf`, in the scratch throughout:

| Left by | Swept by | Removed |
|---|---|---|
| a `command` killed on the shell channel | the next `command`'s admission | its `karvi-ssh-<pid>-*.conf` and `askpass-*.sock`, each a `--debug` line `scratch_abandoned_removed: …` |
| the same | the daemon's start | the same two, each a `daemon.log` line `code=scratch_abandoned_removed path=…` |
| the same | a login's start | its configuration |
| a `login --record` killed | the next login's start | its `karvi-script-<pid>-*.timing`, configuration, and socket |
| a `command` still running | another `command`'s admission | nothing |

The earlier release's file was left in every case. Verification at each
section: gofmt, vet, every Go test (`osutil.StartTied`'s, which kills a parent
and fails with the signal removed; the sweep's names, its kept and removed
entries, and the daemon's half), `make generated-clean`, and the full battery
on the section's lab build, run again for section 2 after the race above; the
record and read paths untouched, so no run at width.

**Built** in three sections on the operator's word: `ea88a24` the session
processes tied to karvi (`osutil.StartTied`, the shell channel's `ssh`, a
login's `ssh`, a recorded login's `script(1)`, the exec master), `295e445` the
sweep (the names by pid, `osutil.SweepScratch`, the shared `KarviAlive`,
`ownedBy`, and `socketAbandoned`), and the documents: DESIGN (one entry for
both, the masters' and the transcripts' entries pointing at it), FILES,
OPERATIONS, COMMAND-TROUBLESHOOTING ("A karvi killed outright"),
TRANSPORT-DRIVER-ARCHITECTURE, and CHANGELOG. Chapter 31 was made between the
first two. The chapter is closed.

## 31. `command` over the native transport by default (2026-10-06)

On the operator's word, while [chapter 30](#30-the-scratch-sweep-2026-10-06) was
being built: `command` runs over the native transport unless asked otherwise,
as `run` does, and `login` stays on `system`.

**What it gains.** One transport by default for every non-interactive activity,
the one the parity suite proves against `system`, without an OpenSSH process,
a generated configuration, or an askpass socket per device; `login` keeps
OpenSSH, which attaches the operator's terminal. It waits on nothing: every
build carries `scrapligo-v1`, and since chapter 24 it runs the exec channel and
the operator's keys as `system` does.

**Executed.** On lab builds of `ea88a24` and of the change, a `command` to
`bast1` (`linux_shell`) and to `srv1` (`linux`) on this host, each with no
transport named, then the two ways back to OpenSSH, then a login:

| Case | `ea88a24` | The change |
|---|---|---|
| `command bast1`, `command srv1` | `transport=system`, exit 0 | `transport=native`, exit 0 |
| `command bast1 --transport system` | `transport=system` | `transport=system` |
| `--set 'ssh.command.transport="system"'` | `transport=system` | `transport=system` |
| `login bast1` | `transport=system` | `transport=system` |

**The rule.** `ssh.command.transport = "default"` resolves to `native`, as
`run`'s does; `login`'s resolves to `system`. The key and its default value are
unchanged, so the registry stays 26, and the precedence is as before: the
command line's `--transport`, an inventory row's transport or its source's
default, the mode's key, then the mode's default. A breaking change, in the
changelog; README, the quick start, the transports guide, and the architecture
note say so, and the example configuration's `[ssh.command]`, which spells out
each mode's default, says `native`.

**Found on the way.** The full battery on the change's build stopped twice in
its third lane: the suites that map `system` to a fake `ssh` script and run
`command` with no transport named had relied on `command` meaning `system`, and
the first of them exited 108 at its first `command` without a word, under `set
-e`. Eleven such suites, ten on their command lines and `v080` in its
configuration file, now set `ssh.command.transport = "system"` beside the fake;
`halt` and `transcript` run no `command`, and the device-qualification script
names the transport on every call. The third run passed.

**Not taken.** A new key or a new selector value (the mode's default is what
`default` already names). `login` on the native transport: its interactive
session attaches the terminal to OpenSSH, and the help says a login requires a
system-compatible slot.

## 32. The lost exit status, found in the fake device (2026-10-07)

The race [chapter 30](#30-the-scratch-sweep-2026-10-06) found on the way: over
`system`, on whichever record of the parity suite's S35 cases, a command's
status or signal lost, `command_exit_missing` where the other streams had the
real one. The operator asked for its cause from a failing run with the
evidence kept, before any design.

**What it gains.** A parity suite and a battery that no longer fail at random,
and the knowledge that karvi's exec channel was right: each failure recorded a
channel that did close without a status. It waits on nothing.

**The evidence.** A lab copy of `5f4b71d` logged, with times, every line of the
master's stderr, each `ssh -S` client's start and exit and what the settle wait
returned, and each client's own DEBUG3 log through `-E`, which leaves its stderr
the command's; the suite's copy kept a failing run's work directory and each
combination's `fake.err`. S35 in a loop:

| Series | The lab build | Failed |
|---|---|---|
| `cl` | karvi's lines and the clients' logs | 1 of 21 |
| `d2` | and the master at DEBUG2 | 1 of 12 |
| `d3` | and the fake logging each step of its exec end | 7 of 7 |
| `sf` | `d3`'s, the status sent before the end of output | 0 of 20 |

In `cl`'s failure (S35c over `run`, `both`) the master made channel 2, sent the
command, and freed the channel with no `client_input_channel_req` line between:
it never received a status, so the lines' attribution was not at fault. The
client's log ends `read header failed: Broken pipe`, `Control master terminated
unexpectedly`, its exit 255. In `d2`'s (S35b over `run`, `uname -snrm`, its
output in the record) the master's DEBUG2 lines read `rcvd eof`, `send close
for remote id 0`, `rcvd close`, and no status. In `d3` the fake's own lines
said why:

```text
race: 07:17:29.140823 "uname -snrm" eof sent: <nil>
race: 07:17:29.140993 "uname -snrm" exit-status not sent: EOF
```

**The cause.** The client is started with `-n`, so the channel's side toward
the device is at its end from the start; when the device's end of output
arrives and the output is written, the master closes the channel at once. The
fake sent its output, its end of output, and then the status, each a write of
its own. When the master's close came between the last two, x/crypto answered
it with its own close and marked the channel closed, and the status write
failed with `io.EOF`. The native transport never closes a channel before the
device does, so its streams kept the status; the released executables are
affected only through the fake the suite builds.

**A real server.** This host's `sshd` through the same master and client:
`uname -snrm` and `ls /nonexistent` read `rcvd eof`, the status, `rcvd close`,
the three together. A command that closes its output a second before it ends
(`exec >/dev/null 2>&1 </dev/null; sleep 1; exit 3`) made the gap real: the
master sent its close at the end of output, and `sshd` still sent the status
after it, which the master took; the client exited 3, and 255 with the signal
for `kill -TERM $$`.

**The rule.** The fake's exec end sends the status or the signal before its end
of output, then closes; the comment says why it differs from `sshd`'s order,
and the fake's entry in [`DESIGN.md`](DESIGN.md) holds the rule. On the tree
with the change, S35 in a loop: 0 of 80.

**Not taken.** A fake that holds its close back for the status, as `sshd` does:
x/crypto answers a close itself. A change to karvi's exec channel: it reported
what happened. The evidence is kept beside the tree
(`release-design-evidence/race-2026-10-07`).

**For later.** A real server that behaves as the fake did (the end of output,
then the status, and nothing after a close) would lose the status over `system`
and be recorded `command_exit_missing`, rightly; whether karvi guards against
one, for instance by keeping the client's input open so that the master never
closes first, is an issue of its own.

## 33. The askpass socket named by its maker's pid (2026-10-07)

Found while [chapter
32](#32-the-lost-exit-status-found-in-the-fake-device-2026-10-07) was verified:
the full battery's canary suite failed at `r3`, two `run` clients of one
operator at once through one daemon, each with its own password; one exited
101, "did not authenticate with its own password". The operator asked for its
cause from a failing run with the evidence kept.

**What it gains.** Jobs of one operator that overlap over `system` with a
password authenticate again: in the daemon, or a `run --no-daemon` beside
another job, since they share the scratch. The defect came with the scratch
sweep ([chapter 30](#30-the-scratch-sweep-2026-10-06)) and never shipped. It
waits on nothing.

**The evidence.** The canary suite, `r3` failing:

| Executables | Failed |
|---|---|
| the released 0.27.0 | 0 of 15 |
| `5f4b71d` | 5 of 15 |
| `46859c9`, `ea88a24`, `2029987` | 0 of 10 each |
| `295e445`, the sweep | 2 of 10 |

A copy of the suite kept a failing run's work directory. Job a's record read
`ssh_process_failed`, "exit status 91", the fake `ssh`'s exit when its
`$SSH_ASKPASS` fails; job b was admitted at 41.784 and job a failed by 41.794;
no `scratch_abandoned_removed` line anywhere. A lab test with no suite, a
broker started and the scratch swept before the helper's request:

```text
sweep=false removed=[] socket after: <nil>; the helper's request: secret: login-secret
sweep=true removed=[] socket after: stat …/askpass-89714561b2f7725f.sock: no such file or directory; the helper's request: dial: … connect: no such file or directory
```

**The cause.** The sweep judged an askpass socket by connecting to it and
closing: one that refused was abandoned. The broker serves one connection: it
took the sweep's as its one, failed to read a request from it, closed, and
removed its socket, so the job's helper found none. The sweep removed nothing
and logged nothing, since the socket had answered.

**The rule.** The broker's socket is `askpass-<pid>-<16 hex>.sock`
(`osutil.AskpassSocketName`), named by its maker's pid as the scratch's files
are, and `SweepScratch` removes one whose pid is not alive as a karvi
executable; it connects to no socket. In the daemon the maker is the daemon,
so every job's broker stays. A socket under 0.27.0's name is not touched.
`socketAbandoned` stays with its one user, the control sockets' sweep. The
changelog's entry for the sweep, `FILES.md`'s row, and the sweep's entry in
[`DESIGN.md`](DESIGN.md) say so; `TestSweepScratchConnectsToNoSocket` holds a
live and a dead pid's socket listening and fails if either is connected to.

**Executed.** On a lab build of the change, the canary suite 0 of 20, and the
full battery passed. A karvi killed outright while its fake `ssh` waited
before asking for the password, then the next run:

```text
karvi pid 281855
srw------- askpass-281855-aa00721117be68e6.sock
after kill -9:
srw------- tmp/askpass-281855-aa00721117be68e6.sock
second run exit 0
DEBUG scratch_abandoned_removed: removed the abandoned scratch file …/state/tmp/askpass-281855-aa00721117be68e6.sock
DEBUG scratch_abandoned_removed: removed the abandoned scratch file …/state/tmp/karvi-ssh-281855-4194296846.conf
askpass sockets left: 0
```

The control sockets' probe on this host's OpenSSH master: three connections
made and closed, then `-O check` answered (`Master running`), and a command
ran with its exit 4. A master serves many clients, so that sweep keeps its
probe.

**Found on the way.** One executor test kept its scratch under `t.TempDir()`,
whose path holds the test's name; with the pid the socket's path passed 107
bytes and the broker failed to start (`askpass_start_failed`). Its harness
takes `testsocket.Dir`, as the others do, whose base is now at most 56 bytes
for a name of 37 (an askpass socket under a seven-digit pid). Under this
session's umask 0002 the smoke suite's work directory was made 0775 and karvi
refused the trust store in it (`host_key_directory_permission`); fifteen of the
suites make their work directory by the umask.

**Not taken.** A broker that waits past a connection that sends nothing: its
one-use rule changed, and a probe still races the helper. No sweep of askpass
sockets: a killed karvi's would stay. A guard for a 0.27.0 socket's name: no
migration. The evidence is kept beside the tree
(`release-design-evidence/r3-sweep-2026-10-07`).

**For later.** The scratch has no check that an askpass socket's path fits, as
`ssh.control-path-root` has (`control_path_root_too_long`): a long `tempdir`
fails at the broker's start. The suites' work directories by the umask.

## 34. A `.jsonl` stream's records read by one function (2026-10-07)

The outline's item: the qualification script's `first_code()` and the suites'
greps on `"status"` to `scripts/lib/json.sh`, one tested function.

**What it gains.** One reader for the command records of a `.jsonl` stream in
place of about seventeen text matches in nine scripts and the qualification
script's two helpers. A match like `grep -c '"status":"succeeded"'` holds for
one layout only, the failure `json.sh`'s own header records from the day the
`.json` files were indented; `first_code()` took the first `"code"` anywhere in
the stream, which could be a notice's. It waits on nothing.

**The rule.** `jsonl_records FILE PATH...` prints one line per command record
(a line holding `record_id`; the job summary and any other line are skipped),
the values at the PATHs tab-separated in the order given. A path is
`json_get`'s, the two sharing one walk (`JSON_PY_WALK`); an absent value is
empty; the values `*` matches are joined by commas; a string holding a tab or a
newline prints as JSON writes it, quoted, so a record stays one line and its
fields stay apart. A line that is not JSON, or a file that cannot be read,
exits 2. The assertion stays the script's, on that output:

| Site | Before | After |
|---|---|---|
| counts of a status (halt, hostkey, canary, v0100, the qualification's D8) | `grep -c '"status":"x"'` | `jsonl_records F status \| grep -cx x` |
| an error code (hostkey) | `grep -c '"code":"host_key_changed"'` | `jsonl_records F error.code \| grep -cx host_key_changed` |
| a notice (native S26) | `grep -q '"code":"platform_unknown_fallback"'` | the first record's `notices.*.code` |
| address with status and decision (canary p1) | two greps on one line | `jsonl_records F selected_address status ping.decision`, one `grep -qx` per row |
| status with bytes (the scale run) | one pattern in both orders | `jsonl_records F status output_bytes \| grep -cx` |
| `statuses()` | a loop of `json_has` and `json_get` | `jsonl_records "$1" status \| paste -sd, -` |
| `first_code()` | the first `"code"` anywhere | `jsonl_records "$1" error.code \| grep -m1 .`, the first record's error |
| the daemon status report (canary d1, v0100 d2) | `grep '"status": "running"'` | `json_is F daemons.0.status running` |

Two assertions became stronger: the halt suite's three statuses in their order,
and the smoke suite's two records both succeeded. `CONTRIBUTING.md`'s JSON rule
names the function, and the qualification runbook's `python3` row lists the
script's reads.

**Tab, not comma.** The operator asked whether a comma could separate the
fields. Read from the kept canary streams of [chapter
33](#33-the-askpass-socket-named-by-its-makers-pid-2026-10-07): of their
sixteen records' string values two held a comma and none a tab, an object or
array value prints as compact JSON full of commas, and `*` already joins by
comma:

```text
$ jsonl_records r3a.out status error
connection_error	{"code":"ssh_process_failed","category":"connection","message":"exit status 91",…}
```

The same reading showed a value with a newline breaking a record across lines
(`output`, `canary device output` and the line after it), hence the quoting.
A script writes an expected line with `printf '%s\t%s'`.

**Executed.** On a lab build of the tree, under umask 022: `json-test.sh` 51
cases; the halt, host-key, smoke, canary, and v0100 suites, the parity suite's
S26, and the scale run at N=8 passed; the qualification script with `FAKE=1
CONCURRENCY=2` passed (70 pass, 8 observe, 4 skip), its D7 reading
`host_key_not_enrolled` and `host_key_changed` through `first_code` on both
transports and its D8 `4 records succeeded` on each. A copy of the halt and
host-key suites with a wrong expectation failed where it should:

```text
+ [ succeeded,authentication_error,not_started_halt = succeeded,not_started_halt,authentication_error ]
+ grep -cx host_key_unknown
+ [ 0 -eq 1 ]
```

**Not taken.** `first_code`, `statuses`, and a counter as three functions: one
reader and the shell's own tools cover them. `json_get` taking a whole
`.jsonl`: it reads one document by design, and a stream wants the records'
filter. A comma, or another printable separator: values hold commas, spaces,
and the `|` of a command line.

## 35. The qualification script's scratch in its short directory (2026-10-07)

The outline's item: the qualification script's default evidence path. Run with
`FAKE=1` from the tree, every `system` row had failed `askpass_start_failed`,
and the lab runs since have set `EVIDENCE` to a short path.

**What it gains.** The script runs from where the operator stands, the tree or
a home, with the evidence where it was made, and no `EVIDENCE` needed for a
socket's sake. It waits on nothing.

**The cause.** The evidence directory is
`$PWD/karvi-qualification-<device>-<stamp>`, and karvi's base directory is under
its `work/`; the scratch, `<basedir>/tmp` by default, is where the askpass
broker makes its socket, and a Unix socket's path holds 107 bytes. The daemon's
socket was already in a short directory of the script's own (`SOCKDIR`, from
`mktemp`) for the same reason. Since [chapter
33](#33-the-askpass-socket-named-by-its-makers-pid-2026-10-07) the socket's name
carries the maker's pid, up to seven bytes more, so a run from a directory as
short as a home overflowed too. `FAKE=1 ROWS=D1` from lab directories of the
tree's length (24 bytes) and of `/home/netops`'s (12):

```text
D1 command.system exit: fail (exit 1, expected 107: askpass_start_failed: command failed for name:fake-iosxe)
askpass_start_failed: listen unix /tmp/nd.OWme/karvi-qualification-fake-iosxe-20261007T164808Z/work/base/tmp/askpass-330093-a9ca6fab80ce94ac.sock: bind: invalid argument
```

The second path is 111 bytes; with the earlier name it was 105 and fitted.

**The rule.** The default evidence path stays. The script's configuration names
`tempdir = "$SOCKDIR/tmp"`: the scratch joins the daemon's socket in the short
directory, removed at exit, and its comment says both. Nothing in the scratch
is evidence. `control-path-root` is not moved: the script's rows open no exec
channel, and the runbook's production-server rows make a short directory of
their own.

**Executed.** With the change, `FAKE=1 ROWS=D1` from both directories passed
(`command.system` exit 107, parity 3 records equal in 4 streams), and the full
`FAKE=1` run from the tree's length passed (70 pass, 8 observe, 4 skip), no
`askpass_start_failed`, no short directory left.

**Not taken.** A default evidence path under `/tmp`: evidence is to outlast the
run and be found where it was made, and a reboot empties `/tmp`. A shorter
directory name: a deeper working directory overflows still. karvi refusing a
scratch too long for its socket with a plain code: the outline's item of its
own, which names the failure but does not remove it.

## 36. A recorded login's own lines at the first column (2026-10-07)

The operator's report: `login --record` with `--debug` showed a lot of
whitespace inserted into the display, tabs or spaces, that shifted the
information. The operator asked for it to be looked at.

**What it gains.** A recorded login's screen that reads as an unrecorded
one's: karvi's lines, each at the first column. It waits on nothing.

**The evidence.** A lab build of `4e6d78a`, `login --record --debug` to this
host's OpenSSH under an 80 by 24 pseudo-terminal, every byte the terminal
received kept and the screen drawn from them:

```text
|13:23:56 2026-10-07 DEBUG activity=login config_digest=eff2d2fae74cd0674d9d46140
|b86e33563668493fd737578f1d60d5d6d9f7667 config_sources=""
|                                                         13:23:56 2026-10-07 DEB
|UG login transport selector="system" implementation="system" kind=system
|                                                                        13:23:56
| 2026-10-07 DEBUG login target set candidates=1 selected="127.0.0.1" dispatch_or
```

Every line of karvi's between the two `! transcript=` lines ended in a line
feed alone, where the device's ended `\r\n`. Without `--debug` the login's
header showed it too, its wrapped row starting at the last column (a lone `t`,
then `ransport=system`). An unrecorded `login --debug` ended every line `\r\n`,
and the two `! transcript=` lines, written by the wrapper before `script(1)`
starts and after it ends, were right.

**The cause.** The recorded child writes karvi's lines (the header, a warning,
the ping line, a final error, every `DEBUG` line) to the wrapper's real stderr,
passed as descriptor 3, so that they stay out of the transcript. `script(1)`
holds that terminal raw for the child's whole life, output processing off, so a
line feed moves down a row and keeps the column. OpenSSH's own stderr reaches
the terminal through karvi's pipe, and OpenSSH ends its lines `\r\n` itself.

**The rule, agreed.** Where the recorded child takes descriptor 3 and it is a
terminal, each line feed not after a carriage return is written after one
(`osutil.RawTerminalLines`); a descriptor that is not a terminal, a stderr
redirected to a file, keeps its bytes. One writer at the one place the child
takes its stderr covers every line karvi writes there.
`TestRawTerminalLinesEndsEachLineAtTheFirstColumn` holds the bytes, a carriage
return ending one write counted for the next; the transcript suite's new row
runs a recorded login under `--debug` and asserts the header and every `DEBUG`
line end `\r`, and that none is in the transcript.

**Executed.** On a lab build of the change, the same login drew its twelve
`DEBUG` lines and the header's wrapped row at the first column, and its
transcript held no `DEBUG` line. The transcript suite failed on the
released source's build at the new row and passed on the change's; the full
battery passed. The evidence is kept beside the tree
(`release-design-evidence/record-staircase-2026-10-07`).

**Not taken.** Output processing turned back on under `script(1)`: it changes
the mode of a terminal `script(1)` owns, and the device's bytes it relays. A
carriage return in each of karvi's line writers (the debug logger, `warning`,
the header and ping writers): one rule in many places. The wrapper copying a
pipe to its stderr with the translation: the copy runs behind the child, and the
footer would wait on its draining.

## 37. The first contact said alike over both transports (2026-10-07)

The outline's item: `scrapligo-v1` prints a warning when it enrolls a host's
key at a first contact, and `system` prints nothing.

**What it gains.** Under `accept-new` a first contact is the one moment karvi
trusts a key nobody checked; every transport and every path saying so alike,
where today one transport says it, on one path. It waits on nothing.

**The evidence.** A lab build of `001f12a`, this host's OpenSSH, the lab's
trust store emptied before each run:

| Path | `scrapligo-v1` | `system` |
|---|---|---|
| `command`, in the client | `warning: accepted and stored new SSH host key for 127.0.0.1 in …/known_hosts (ssh-ed25519 SHA256:4C8c…)` | nothing |
| `run --no-daemon` | the same warning | nothing |
| `run` through the daemon | nothing: not on the client, in the records, `daemon.log`, or the audit | nothing |
| `login` | (a login takes `system`) | nothing |

Both transports stored the same line, and neither record carried a notice. The
native adapter's warning ends in the job's `warn`, which writes to the job's
standard error, `io.Discard` in the daemon. OpenSSH writes `Warning:
Permanently added '127.0.0.1' (ED25519) to the list of known hosts.` at
`LogLevel INFO` and above and nothing at `ERROR`: the exec master (`DEBUG1`) and
the shell's command session (`VERBOSE`) receive it and keep it only among the
failure diagnostics, which drop it; a login runs at `ERROR`.

**Issue 1, where it is said, agreed.** The enrollment is a record notice,
`host_key_enrolled`, on the device's first record over both transports, its
message naming the trust store and the fingerprint. The client prints it on
standard error as the `warning:` line the native transport prints today, taken
from the record rather than the adapter's callback, so the same line shows in
the client and through the daemon, and `commands.jsonl` keeps it for whoever
reads the job later; `icmp_packet_loss` rides a device's first record so.
`--quiet` leaves it shown, as warnings are. Amended with issue 2 and by the
operator after issue 4: the line is `! ssh accepted new host key for 127.0.0.1
(ED25519)`, the device and the key type, alike on both transports, in the
client, through the daemon, and in a login; the notice's message is the same
text without the `! `, its details the key type. It goes to standard error, not
with the headers on standard output, so a redirected output holds the device's;
it takes the display's `warning` colour when colour is on; and `--quiet`, which
suppresses the display's `!` lines, leaves it shown: a key trusted unchecked is
not narration. *Not taken:* the job's standard error carried
out of the daemon (a warning outside the record is lost to the
job's later reader); a line under `--debug` alone (a trusted key is not routine
narration).

**Issue 2, how `system` learns of it, agreed as amended.** Four first contacts
at once to this host, the store empty, the sessions overlapping: over
`system`, ten rounds, the store got one line in nine and two identical lines in
one, two masters each writing; plain OpenSSH, four clients at once, three
rounds, one client said `Permanently added` each round, OpenSSH saying it only
when it writes. A comparison of the store before and after the session would
have had all four jobs report the one enrollment. The notice marks the job
whose session wrote the key: over `system` karvi takes OpenSSH's `Warning:
Permanently added '<host>' (<TYPE>) to the list of known hosts.` out of the
standard error it reads line by line, as it takes `Authenticated to …`, in the
exec master and the shell's command session alike; over `scrapligo-v1`
`Enroll` reports whether it wrote, and the notice follows only then, where
`Verify` warns today even when another job wrote the same key under the lock
meanwhile. Two jobs that both write both report it. The operator's amendment:
no fingerprint. The key is in the trust store, which the operator reads when
the key's detail matters, so karvi reads nothing from the store for the notice
and takes no `Server host key:` line. *Not taken:* the store compared before
and after (every overlapping job reports one enrollment); karvi enrolling the
key itself before OpenSSH connects (a second connection, and a key trusted by
a check OpenSSH did not make).

**Issue 3, how a login says it, agreed as amended.** A login runs OpenSSH at
`LogLevel ERROR`, where it writes nothing of a first contact. At `INFO` it
writes the enrollment line, a server's pre-authentication banner (hidden at
`ERROR`: a lab `sshd` on its own port with a `Banner`), and `Connection to …
closed.` at every end, `closed by remote host` before it when the device ends
the session; after authentication it writes to standard error only at the end. A
login now runs OpenSSH at `INFO`, set on its own command line (`-o
LogLevel=INFO`) as the exec master and the command session set theirs, the
generated configuration staying at `ERROR`; `-vvv` from `SSH-TROUBLE.md`'s
wrapper still wins (69 `debug3` lines either way). karvi reads its standard
error line by line from the pipe it already holds: the `Permanently added` line
becomes karvi's `host_key_enrolled` warning, `Connection to … closed.` is
dropped (the footer marks the end), and every other line (the banner, OpenSSH's
warnings and errors, `closed by remote host`) is shown when `ssh.login.stderr`
is true, the default, and not when false, a failure's text still reaching the
operator in karvi's failure line from the diagnostics. The operator's
amendments: a switch, shown by default, since the staff who write a banner
acknowledge it daily; no file to act on; and the switch is OpenSSH's standard
error, not the banner. *Not taken:* `LogLevel VERBOSE` for an exact boundary at
`Authenticated to …` (it adds `Transferred:` lines and whatever a server's
`VERBOSE` carries); `-E` to a file or a FIFO (OpenSSH's log lines apart from the
banner, but a file, and the last `-E` wins over the one `SSH-TROUBLE.md`'s
wrapper sets); staying at `ERROR` and comparing the store before and after (the
warning after the session, and issue 2's finding).

**Issue 4, the audit, agreed.** The audit, which outlives the job folder,
recorded nothing of a first contact. The enrollment goes into the `details` of
the event already written for the device, `host_key_enrolled: "ED25519"`: the
`command_completed` event of the record carrying the notice, and a login's
`login.completed` or `login.errored`. No event name and no audit schema
change: `details` is an open map. *Not taken:* an event `host_key.enrolled` of
its own (a name no consumer knows, outside the device's events).

**S1, the notice, native, the display, the audit.** `hostkey.Enroll` and
`Verify` say whether this call stored the key; `hostkey.TypeLabel` gives
OpenSSH's label of a key type and `hostkey.EnrolledMessage` the one message.
`platform.OpenRequest.HostKeyEnrolled` carries the label from the transport
to the executor, called from the handshake, so a session that stores the key
and then fails to authenticate, which returns no driver, still says so; the
executor puts the notice on the device's first record, the failure record
included, and `command_completed`'s `details` take it from the record. The
renderer writes the line to the client's standard error before the record, in
the client and on a followed job, in every format; a job's directory shown
again does not repeat it. Executed on a lab build, native, the trust store
emptied before each: `command`, `run --no-daemon`, `run` through the daemon,
`--quiet`, and `--format jsonl` each showed `! ssh accepted new host key for
127.0.0.1 (ED25519)` once on standard error, and the audit's first
`command_completed` of each job named `ED25519`, the second none.

**Found at width.** The scale run at N=32, where every identity is enrolled
and every first record carries the notice, put the client's peak at 49 to 56
MB against 41 to 44 on `001f12a`: the jsonl follow decoded each 5 MB line
naming the notice into a whole record. Decoded for its notices alone, the
output skipped, three runs each read 43 to 45 MB on both builds, the daemon's
peak varying as much within a build as between them, and one 51 MB response
read the same on both (the daemon about 26 MB, the client about 17).

**S2, `system`'s exec and command sessions.** One matcher, `enrolledLabel`,
takes OpenSSH's `Warning: Permanently added '<host>' (<TYPE>) to the list of
known hosts.` out of the standard error karvi already reads line by line: the
exec master's lines (`DEBUG1`) and the command session's (`VERBOSE`), beside
`Authenticated to …`, and passes the label to
`platform.OpenRequest.HostKeyEnrolled`; the line no longer reaches the
diagnostics. Executed against this host's OpenSSH on a lab build, the store
emptied before each: `command` to `linux` (the exec master) and to `linux_shell`
(the command session), and `run` to each through the daemon, each showed `! ssh
accepted new host key for 127.0.0.1 (ED25519)` and the audit's first
`command_completed` named `ED25519`; with the key known, nothing. Four first
contacts at once, five rounds: one job said it each round, one line stored. The
host-key suite's new row drives the fake `ssh`'s enrollment line through a
`run`: the line on standard error under `--quiet`, the notice on the first
record alone, the audit's details; it fails on `001f12a`'s build at the line.
The parity suite's streams each start from an emptied store, so every first
record now carries the notice on both transports: S35b's pin of record 0's
notices adds it, S14d and S14e (one transport enrolls, the other reads under
`secure`) pin it to the enrolling transport, and S31's big record holds it
before the follow stream's `follow_output_omitted`, the file's record the
enrollment alone.

**Found by the parity suite: `insecure`.** S14c runs under `insecure` with
another key stored, and `system` reported an enrollment native did not.
`insecure` gives OpenSSH `StrictHostKeyChecking no` and `UserKnownHostsFile
/dev/null`, and OpenSSH then says `Permanently added` of a key it writes to
`/dev/null` (executed with plain `ssh`). The line is an enrollment under
`accept-new` alone, the one policy that stores a key; under the others it is
still kept out of the diagnostics and said nowhere.

**Width.** At N=32 over `system`'s exec channels the daemon's peak averaged 75
MB over ten runs against 70 MB on `001f12a`, and as much with S1 alone, which
adds no work to a `system` session. Two builds of `001f12a` read 74 and 79 MB,
one of them ranging 66 to 86: batches move the peak by about 10 MB, so the
difference is noise. The client's peak was level, 41 to 46 MB, and a 5.26 MB
line decoded for its notices allocates 18 KB.

**Removed: the generated file's digest.** The `system` driver hashed the OpenSSH
configuration it writes for each session (`Driver.ConfigDigest`), and nothing
read it; found when issue 3 was argued from it. The operator's rule: a digest
without a reader is not kept.

**Issue 5, `insecure`'s warnings through the daemon.** The operator asked
whether the policies hold as he states them: `accept-new` accepts a new key
and refuses a different one; `secure` refuses an unknown or a different key;
`insecure` proceeds and warns when a key different from the stored one is
presented; announcing a new key is acceptable under `accept-new` and
`insecure`. Each policy, the store holding another key or none, both
transports, in the client (`command`) and through the daemon (`run`): the
first three held on every path; under `insecure` the client printed the
policy warning and the mismatch warning, and through the daemon there was
nothing, exit 0, no warning, no notice in the record, nothing in the audit.
`insecure`'s three warnings (the policy's, every connection; the mismatch;
a comparison that could not complete) went to the job's standard error,
which the daemon discards, as the native enrollment warning had. *What it
gains:* the mismatch said on every path. It waits on nothing: S1's route
from the daemon to the client.

**Issue 5a, the mismatch, agreed.** Under `insecure` a key different from the
stored one is the notice `host_key_mismatch_accepted` on the device's first
record, the failure record included, over both transports, carried as
`host_key_enrolled` is; the client prints `! ssh host key for 127.0.0.1
differs from the trust store, accepted under insecure` on standard error,
under `--quiet` too, in the warning colour. The notice's details and the
audit's `details` keep the enrolled and presented fingerprints: under
`insecure` the presented key is stored nowhere, so they are its only record
(issue 2's amendment rested on the key being in the store). The request
carries one callback for the transports' host-key notices in place of
`HostKeyEnrolled` alone. A comparison that could not complete is said too, the
notice `host_key_not_compared` and `! ssh host key for 127.0.0.1 not compared
with the trust store: <reason>`: over `system` under `insecure` the trust
store given to OpenSSH is `/dev/null`, so a stored device's key is compared by
an `ssh-keyscan` beside the connection, and when it fails (not installed, past
its five seconds, the device refusing the extra connection as a router with
few vty lines may, no usable key) the session goes on uncompared. Executed
without `ssh-keyscan` on the path, another key stored: exit 0, the command
ran, and the warning `insecure host-key comparison for 127.0.0.1 could not
complete: dependency_ssh_keyscan_unavailable …`; unsaid, it reads as no
mismatch. Native compares in its own handshake and has no such case. *Not
taken:* the job's standard error carried out of the daemon (issue 1's reason).
`insecure` need not announce a new key (the operator). *For later:* the exec
master's `Server host key:` line at `DEBUG1` would let an exec device be
compared without the second connection; the command session and the login do
not receive it.

**Issue 5b, the policy warning, agreed as amended.** It is documented for
every connection and dropped through the daemon the same way, and one line per
device is noise on a run of a thousand: it is said once per job. The premise
that the client knows the policy at planning did not hold: a daemon keeps the
configuration it started with, and its policy governs its jobs (the manifest
holds "the daemon's execution policy"). A daemon started under `accept-new`
ran a `run --ssh-host-key-policy insecure` under `accept-new`, refused
`host_key_changed`; one started under `insecure` ran a plain `run` under
`insecure`, exit 0, nothing said. So the process that holds the job's policy
says it, under `insecure`: the daemon at the job's admission, as admission
warnings, which ride its receipt to the client as `spool_width_narrowed`
does; the client at the same point for its own jobs; a login once before its
session. Two lines on standard error, under `--quiet` too, both in the
warning colour, the second's two spaces marking it a continuation (the
operator's wording, 66 and 65 columns):

```text
! ssh host-key policy insecure: unknown and changed keys accepted;
!  connecting to devices with wrong keys and MITM attacks allowed
```

Both transports' warning at every connection goes; the manifest's
`policy.ssh_host_key_policy` and every audit event's `policy` keep the record.
*Not taken:* the client saying it from its own configuration (wrong both
ways); a notice on every first record (the same line a thousand times).

**Issue 5c, a requested policy the daemon overrides, deferred.** A daemon's
execution policy (`ssh.host-key-policy`, `ssh.known-hosts-file`,
`ssh.halt-run-on-host-key-mismatch`, `security.allow-telnet`) governs its jobs
whatever the client resolves, and the client is not told; the first `run`
starts the daemon with its own options, so one `insecure` run left later runs
under `insecure` until the daemon exited. The proposal was a refusal,
`daemon_policy_differs`, from the policy carried in the daemon's status. The
operator's ruling: the operator sets the policy, a site that forbids
`insecure` locks the key, and a restart of the daemon changes it; the override
ends with the ROADMAP's "A job under its client's configuration", which runs a
job under every key its client resolves, so a refusal now would be undone then.
Until then, with 5a and 5b, the dangerous direction is seen (a daemon under
`insecure` says so at every job's admission, and a mismatch on the device's
first record), the other fails safe (a refused key); `ssh.known-hosts-file`,
the halt, and the Telnet allowance stay silent. The ROADMAP item names the
four.

**5a and 5b, built.** `hostkey.Verify` returns a `Verdict`, the key stored
or, under `insecure`, a `KeyMismatch` with both fingerprints, and warns no
more; `hostkey.CompareInsecure` replaces `WarnInsecureSystem` for `system`,
a mismatch or the reason it could not compare. One request callback,
`platform.OpenRequest.HostKeyNotice`, carries the transports' three findings
in place of `HostKeyEnrolled`, and `executor.HostKeyNotices` makes the
records' notices, a login taking the same for its terminal. The job's
admission adds `host_key_policy_insecure: …` to the admission warnings under
`insecure`, its own policy being the daemon's for a daemon's job, and one
function writes them, the two lines for that code and `warning: …` for any
other, in the client, from the receipt, and at a follow's start. The
transports' `Warn` fields and the executor's had no reader left and are
removed. Executed on a lab build, `insecure`, the store holding another key or
none, both transports, in the client and through the daemon: every path
printed the two lines once per job, and with another key the mismatch line,
the audit's first `command_completed` naming both keys; a login printed them
before its header; a `run` through the daemon with no `ssh-keyscan` on its
path printed the not-compared line with the reason. The parity suite's S14c
expects the new line, and the host-key suite's `insecure` row asserts the
three lines under `--quiet` and the audit's two fingerprints. The mismatch
line for `127.0.0.1` is 82 columns.

**The lines, reviewed by the operator.** Measured at a short and a longer
device name, the mismatch line ran to 82 and 96 columns and the not-compared
line past 150 with the reason in full. The operator's texts, each the
notice's message after `! `, the device's name in the `target` colour (as the
headers draw it) and the rest in the `warning` colour, the mismatch in the
`error` colour:

```text
! ssh accepted new host-key 127.0.0.1 (ED25519)
! ssh host-key mismatch 127.0.0.1 proceeding at risk
! ssh host-key 127.0.0.1 not compared: ssh-keyscan timed out
```

The reason on the terminal is a short cause from a fixed set, `ssh-keyscan not
installed`, `ssh-keyscan timed out`, `ssh-keyscan failed`, `no usable key`,
`trust store unreadable`; the notice's details keep the cause and the whole
reason, and the audit's `details` the reason. `ssh-keyscan` reports a
timeout and a refusal alike (exit 1, no output, nothing on standard error,
against a listener that never answered and a closed port), so a scan that
ends without a key at or past its bound is the timeout. The policy's two
lines stay as agreed, in the `warning` colour.

Built so: `hostkey`'s `Phrase` holds a line's words around the device's name
(`EnrolledPhrase`, `MismatchPhrase`, `NotComparedPhrase`), a notice's message
is its `Text`, and a client draws the name apart; `CompareInsecure` returns a
`NotCompared` with the cause and the reason. Executed on a lab build with
colour forced: a first contact through the daemon drew `! ssh accepted new
host-key ` and ` (ED25519)` bold orange (`ESC[1;38;5;208m`) around
`127.0.0.1` bold yellow (`ESC[1;33m`); an `insecure` mismatch drew the
policy's two lines orange and `! ssh host-key mismatch ` and ` proceeding at
risk` bold red (`ESC[1;31m`) around the yellow name; a `run` through the
daemon without `ssh-keyscan` printed `! ssh host-key 127.0.0.1 not compared:
ssh-keyscan not installed`, the audit's `details` holding the whole reason.

**S3, found: a device's banner in the failure text.** S3 was built as issue 3
first agreed it, the login at `-o LogLevel=INFO`, its standard error read line
by line, the key `ssh.login.stderr`. On a lab build against the lab's `sshd`
with a `Banner`, a first contact showed karvi's line and then the banner, and
`false` hid the banner. A failed login showed it twice: at `INFO` the banner
reached the diagnostics as well as the screen, and karvi's failure line began
with it:

```text
0f12827: authentication_failed: netops@127.0.0.1: Permission denied (publickey).
INFO:    authentication_failed: AUTHORIZED ACCESS ONLY (lab banner)
         netops@127.0.0.1: Permission denied (publickey).
```

A session the device ended (`closed by remote host`, exit 110 on both builds)
carried it the same way. It is older than S3: on `0f12827`, `command
--transport system` with a refused key printed `error=authentication_failed:
AUTHORIZED ACCESS ONLY (lab banner)` and then OpenSSH's line. OpenSSH writes a
server's pre-authentication banner on its standard error at `INFO` and above;
the shell's command session runs at `VERBOSE` and the exec master at `DEBUG1`,
so both have always received it among their diagnostics, which the
classification of a failure reads by substring.

**Issue 6, a device's banner in the failure text, agreed.** On this host every
line OpenSSH writes ends in `\r\n` (`Permanently added`, `Permission denied`,
`closed by remote host`, `closed`), and the lab banner's line in a bare `\n`,
the server's own line end; a `ProxyCommand`'s own lines end in `\n` too (`nc:
connect to … failed: Connection refused`). Proposed: the diagnostics keep only
OpenSSH's `\r\n` lines. The operator ran `ssh -o LogLevel=INFO DEVICE exit`
through `cat -A` on three production devices: an Aruba CX 6300 switch and a
Linux server end their banners' lines in `\n`, a Cisco IOS XE 4451-X router in
`\r\n`, with a blank `\r\n` line before the banner. No byte tells a Cisco
banner from OpenSSH's text, and IOS XE is most of the fleet; none of the three
banners holds a phrase the classification matches, so there the harm is the
failure text, 25 lines of a legal notice in every failed or device-ended
login's line. The operator asked whether hiding the banner, unless under
debug, settles it: it does not, as long as the login runs at `INFO`, since
what is not shown is still in the diagnostics. The ruling: the login stays at
`ERROR`, where OpenSSH writes no banner (issue 3, amended below), so its
failure line is as on `0f12827`. `command` and `run` over `system` keep the
older exposure, recorded in the ROADMAP ("A device's banner kept out of
`system`'s failure text"): the command session and the exec master cannot
leave `VERBOSE` and `DEBUG1`, and the fix waits on an exact separator; the
native transport drops a banner and is their default. *Not taken:* the byte
rule (a Cisco banner's `\r\n`); `INFO` with the banner hidden (it stays in the
diagnostics); `-E` to a FIFO, OpenSSH's log apart from the banner exactly (the
operator ruled it out in issue 3, and it takes `SSH-TROUBLE.md`'s trace).

**Issue 3, amended: the login at `ERROR`, the trust store read.** The login
stays at OpenSSH's `LogLevel ERROR`, as on `0f12827`: no banner, OpenSSH's
errors on the terminal as before, no `ssh.login.stderr` (the registry stays
26). Under `accept-new` karvi learns of a first contact from the trust store:
it reads the device's entry, under its host-key identity, before the session;
when there is none, once more at the session's first askpass request, and
says `! ssh accepted new host-key DEVICE (TYPE)` if the key is there, TYPE the
stored key's. OpenSSH stores the key in its host-key check during key
exchange, before user authentication: executed with plain `ssh` under
`accept-new`, an empty store, and a forced askpass that copied the store when
called, the store held the key at the prompt, so the read is definitive. A
login that asks nothing (by keys alone) or fails is read once more at the
session's end, and the line said then. The operator asked whether karvi could
hang waiting for a store that is never updated, or was updated first by
another login: nothing watches the store or waits on karvi's lock; each read
is one read of the file, and a store never updated is a read that finds no
key. An earlier draft read the store until the key appeared, the session
ended, or a bound passed; the operator's question, why not stop at the
password prompt, gave the exact point instead. The cost: a login whose first
contact overlaps another's within key exchange, the other's key stored after
this login's first read and before its key exchange, says the line although
its own OpenSSH stored nothing (issue 2's finding, narrowed to one device in
about a second). *Not taken:* `INFO` with OpenSSH's standard error sorted
(issue 6); a store watch bounded by time (the prompt is an exact point).

**S3, built.** `askpass.Start` takes a `requested` callback, called when a
request bearing the token arrives, before it is answered. `Driver.Interactive`
takes `firstContact`: nil unless `accept-new` and a store without the
device's key; else a read, done once, from the broker's request or after the
session, whichever is first, that passes the stored key's label to the
request's `HostKeyNotice` as `host_key_enrolled`. The login keeps the notices
it prints, and its `login.completed` or `login.errored` event's `details` take
them through `executor.AuditDetails`, the function a `command_completed`
event's take its record's through, beside `diagnostic`. `safeDiagnostic` no
longer passes over `Permanently added`, which every reader takes out first and
a login at `ERROR` never receives. Executed on a lab build: a first contact by
keys (no prompt) said the line after the session's `logout`, and
`login.completed` named `ED25519`; with the key known, nothing; a first
contact by password to a lab `sshd` that offers the password method and cannot
verify it said the line after the header, before OpenSSH's `Permission denied
(publickey,password).`, and `login.errored` named the key, the failure line as
on `0f12827`; a session the device ended said it at the end, its failure line
`ssh_process_failed: Connection to 127.0.0.1 closed by remote host.` as on
`0f12827`. The transcript suite's row stores the key from a fake `ssh` in the
store the generated configuration names, with and without an askpass request:
the line before the device's output with the prompt and after it without,
once each, nothing with the key known, and the audit's `details`; it fails on
`0f12827`'s build at the line.

**S4, the documents.** `SSH-HOST-KEY-POLICY.md`'s `accept-new` says the first
contact as built, on each path and in a login, where it had the native
adapter print a warning naming the fingerprint; `karvi-login(1)`'s HOST KEYS,
which restates the policies, the same, and `insecure`'s lines in place of "a
warning". DESIGN section 6 takes two entries: what a policy accepts unchecked
is said on every path, and a login learns of a first contact from the trust
store. `host_key_enrolled`'s row in `ERROR-CODES.md` adds the login.
`SSH-TROUBLE.md`'s wrapper paragraph adds that a command's first contact goes
into the trace with `credential.auth`, and a login's is still said. The
Cisco IOS XE qualification's host-key items and the runbook's D7 rows expect
the lines as built, not a warning at every connection.

**Found: D7 f looked for the old wording.** The qualification script's D7 f
grepped standard error for `SSH host key mismatch for DEVICE`, the wording
issue 5 replaced, so from `0f12827` the row failed on every build; no suite
runs the script. It now looks for `! ssh host-key mismatch DEVICE proceeding
at risk`, and D7 a for the first-contact line on the first run and none on
the repeat. `FAKE=1 ROWS=D7` against the fake IOS XE device: 56 passed, both
transports.

**Closed.** A first contact is said alike on both transports, in the client,
through the daemon, and in a login, and `insecure`'s acceptances are said on
every path; each is in the device's first record and the audit. The full
battery passed on S3's build, which S4 changes in no code but the registry's
text. Left for later: the exec master's `Server host key:` line, which would
let an exec device under `insecure` be compared without `ssh-keyscan`'s
second connection (issue 5a); a requested policy the daemon overrides (5c, in
the ROADMAP's "A job under its client's configuration"); and a device's
banner in `system`'s failure text (issue 6, in the ROADMAP). The evidence
is kept beside the tree
(`release-design-evidence/hostkey-first-contact-2026-10-07`).

## 38. The audit's schema version set where its line is written (2026-10-08)

The outline's item: the audit records' `SchemaVersion: 1` is a literal in the
writers, where the other records take their schema's constant.

**What it gains.** An audit line whose `schema_version` follows the audit
schema's constant, so the next change to the audit's shape cannot leave its
lines saying 1. It waits on nothing.

**The evidence.** The tree at `8dfcab7` copied, `records.AuditSchemaVersion`
and `buildinfo.AuditSchema` moved to 2, and built: `karvi version` said
`audit_schema: 2`, and a `command` wrote `command.started`,
`command_completed`, and `command.completed` each with `schema_version` 1.
Four writers set `SchemaVersion: 1` (the executor's `command_completed`, the
login's events, `jobexec`'s activity events, `crun.after`), and the sink,
`audit.Sink.WriteAudit`, the one implementation of `records.AuditSink` and
the one place a line is written, filled a zero with a literal 1.

**What it solves, asked.** The operator asked what problem it solves: none
seen today. Every line says 1 and the constant is 1; nothing in karvi reads
an audit line's `schema_version`, no document names it, and the audit's
version has not moved since the public tree began. It is a trap for whoever
next changes the audit's shape: the constant moved, `karvi version` says 2,
and the lines still say 1 to a consumer telling them apart by the field.

**What else is related, asked.** The version names a shape, so the shape was
read. `schema/audit.schema.json` publishes "audit record schema 1", and
nothing in the tree reads it. Five lab sessions on `456ea6f`'s code (a
`command`, one refused, a `run`, a login, one refused) against it:

| Field | Written |
|---|---|
| `outcome` | the schema's enum is `success`, `failure`, `denied`, `started`, `completed`, `incomplete`, `informational`; `command_completed` writes the record's status (`succeeded`, `authentication_error`), `command.completed` and `login.errored` `errored`, and `crun.after` `succeeded` or `failed` |
| `process` | `command_completed` `{"pid": 0}`; the activity events executable, host, pid, version; the login host, pid, version |
| `source` | `command_completed` `{}`; the others `{"client": "karvi"}` |
| `device` | `command_completed` `canonical_name`, the login `name`, the activity events none |

Proposed: item 5 widened to one shape, true to a schema a test checks against
real lines. *The operator's ruling:* fix the `pid` 0 and stamp the version,
the shape left as it is.

**Built.** `audit.Sink.WriteAudit` sets every record's `SchemaVersion` to
`records.AuditSchemaVersion` before it writes, and the four writers leave the
field out; `command_completed`'s `process.pid` is `os.Getpid()`, the process
that wrote it, as the job's other events name it, where it was 0.
`TestSinkStampsTheSchemaVersion` writes records holding 0 and another version
and reads the constant back from each line; it fails on `8dfcab7`'s sink.
`TestCommandCompletedProcess` writes a `command_completed` through a file
sink and reads the process's id and the version. Executed on a lab build: a
`command` and a `run` through the daemon wrote every line `schema_version` 1,
and each `command_completed` the pid of the job's other events, the client's
for the `command`, the daemon's for the `run`.

**Not taken.** Each writer setting the constant (four places and the fill to
keep right); a check refusing a record whose version differs (it guards a
value no writer sets now); the audit's shape made one (the operator's
ruling). The evidence is kept beside the tree
(`release-design-evidence/audit-schema-literal-2026-10-08`).

## 39. The logging keys and the package's contents (2026-10-08)

The outline's item: the ROADMAP's first Next entry, the package's contents,
with the decision on `logging.file`. Two decisions, taken as two issues, the
logging keys first: whether the package carries anything for a log follows
from them.

**What it gains.** A configuration that does what it says, where a site can
set a log file and get none; a package that installs what a site needs to run
karvi as the documents describe, where it installs the executables and the
manual pages alone and the units name documents nothing installs. It waits on
nothing.

**Issue 1, the logging keys, the evidence.** A lab build of `13a7ee2`,
`command --platform linux 127.0.0.1 true` with `logging.file` a lab path,
`logging.level = "debug"`, and `logging.file-required = true`: exit 0, and no
file. `logging.file-required = true` alone is refused,
`config_logging_file_required_missing`. In the tree `logging.level` is read
only by the registry's table of allowed values, and the other two only by that
one rule; `docs/FILES.md` section 5 said so (chapter 22). The daemon's log is
`<basedir>/logs/daemon.log`, the audit's journald and `audit.file`, and each
invocation's narration its standard error.

**Issue 1, agreed.** The three keys are removed, the operator's rule for a
value nothing reads: a file, an environment variable, or `--set` naming one is
refused with `config_key_removed` and the hint every unread key's removal
carries, as `logging.journald`'s was; the rule and its code
`config_logging_file_required_missing` go with them, `docs/FILES.md`'s section
5, and `logging.file` among DESIGN's places without a resolved line. The
registry moves from 26 to 27 (both verifiers; the K03 draft's digest
re-pinned). *Not taken:* `logging.file` given something to write (the daemon's
log, the audit, and each invocation's standard error each have their place,
and no reader has asked for one file of them).

**Issue 1, built.** Executed on a lab build: `--set logging.file=…`,
`KARVI__LOGGING__LEVEL=debug`, and a file's `[logging]` table were each refused,
`config_key_removed: removed in v0.28.0; the key was read by nothing and has no
replacement; remove it from the configuration for logging.X at SOURCE`; a
plain `command` ran; `karvi version` said `config_registry_schema: 27`.
`TestRemovedKeysRefused` covers each entry of the removed keys' table.

**Issue 2, the package's contents, the evidence.** The package installs the
three executables and the manual pages, and the documents name places nothing
installs, two of them for the same files: `docs/COLLECTION.md` copies the
units from `/usr/share/doc/karvi/packaging/systemd/user/`, and it and
`docs/PRUNE.md` run the cron scripts from `/usr/share/karvi/cron/`; the user
units' `Documentation=` lines name `~/.local/share/doc/karvi/*.md`, the system
unit's `/usr/share/doc/karvi/PRUNE.md`; a dozen other places name the files by
their path in the tree, `packaging/…`. The ROADMAP called the control file's
maintainer and homepage placeholders, and both are set. Taken in three parts:
where the files go (2a), whether a script installs them (2b), and the
documents' and units' paths (2c).

**Built to read it.** With debhelper installed by the operator, the tree at
`af9dab4` copied out, `packaging/debian` moved to `debian/` (where
`dpkg-buildpackage` reads it), and a lab `debian/changelog` written (the tree
has none, and the build needs one), `dpkg-buildpackage -us -uc -b -d` built
the package (`-d`: the control's `golang-1.26-go` is not installed; this host
builds with Go 1.27.1, `go.mod` says 1.26.0); the rules' `make test` ran. The
`.deb` held `/usr/bin/karvi`, `karvi-askpass`, `karvi-prune`, the manual pages
gzipped, and debhelper's `changelog.gz` and `copyright`: nothing else. The
same rules installing `docs/*.md` under `/usr/share/doc/karvi/docs` got 22 of
the 23 documents as `NAME.md.gz` (`dh_compress` gzips what is over 4 KB in
`/usr/share/doc`; `QUICKSTART.md` stayed), which breaks every relative link
and a unit's `file:` path; with `override_dh_compress` running `dh_compress
-X.md` all 23 stayed plain and the manual pages were still gzipped.

**Issue 2a, where the files go, agreed.** Nothing is installed active, under
`/etc` or `/usr/lib/systemd`: each file is a site's choice, the tmpfiles rule
names the group `netops`, the sysctl sets the host's open-file ceiling, the
timers are per operator. `/usr/share/doc/karvi/` holds `README.md`, `CHANGELOG.md`,
`LICENSE.md`, `docs/*.md` under `docs/`, and `examples/`, in the tree's layout
so every relative link between the documents holds; the rules keep `.md` out
of `dh_compress`. `/usr/share/karvi/` holds `packaging/`'s `systemd/`,
`cron/`, `crun/`, `sysctl/`, and `tmpfiles.d/` as they are, with
`reference.toml` and `schema/`. *Not taken:* the HTML `make html` writes (a
step at build for a reader the Markdown serves; it can follow); `docs/`
flattened into `/usr/share/doc/karvi` (the README's `docs/…` links break).
Found for the package's build: the tree has no `debian/changelog`, and its
debian directory sits at `packaging/debian`.

**Issue 2b, an install script, agreed: none.** The ROADMAP had a script under
`/usr/share/karvi` install the material for a site that opts in. What a site
turns on for the host karvi already installs: `sudo karvi setup shared` writes
`/etc/tmpfiles.d/karvi.conf` for the site's group, and `sudo karvi setup tab`
the bash completion. The rest is a choice a script would take as options: the
units and timers per operator or for the site, systemd or cron; the sysctl's
open-file ceiling; the `crun` hooks, examples to adapt. Each is one copy from
`/usr/share/karvi`, given in its guide. The packaged `tmpfiles.d/karvi.conf`
stays as the rule for the group `netops`, as OPERATIONS describes it. *Not
taken:* a shell script (code whose work is `cp`, with an option for each
choice); a `karvi setup` word per piece (it can follow a site's asking).

**Issue 2c, the documents' and units' paths, agreed.** Where a document tells
a site to use a file, it names the installed path, `/usr/share/karvi/…` for
the material and `/usr/share/doc/karvi/…` for the documents; where it
describes the source tree, `packaging/…` stays. The user units'
`Documentation=` lines name `file:/usr/share/doc/karvi/docs/X.md`, the system
`karvi-prune.service`'s `…/docs/PRUNE.md`; `docs/COLLECTION.md`'s `cp` takes
the units from `/usr/share/karvi/systemd/user/`, and it, `docs/PRUNE.md`,
`docs/OPERATIONS.md`, the README's collection paragraph, and `karvi-crun(1)`
name `/usr/share/karvi/…`; `docs/FILES.md` section 4's table gains the two
directories and its paragraph says where the material is; the debian rules
install it as 2a has it, `.md` out of `dh_compress`; the ROADMAP's first Next
entry goes, its placeholder sentence with it. The README's source layout, the
manual pages' sources in `docs/PRUNE.md` and DESIGN, and the release's
`BUILD-RESULT.md` keep the tree's paths; the cron scripts' comments already
name `/usr/share/karvi/cron/…`, and their `docs/X.md` read the same under
`/usr/share/doc/karvi/`. Found in the builds, for after it: the sysctl example
(2d), and building the package from the tree (2e).

**Issue 2d, the sysctl example, agreed: removed.**
`packaging/sysctl/90-karvi.conf` held one line, `fs.file-max = 2097152`, an "example host-wide ceiling for
large maintenance servers", unchanged since the public tree began. On this
host (kernel 7.0, systemd 259) no sysctl file sets `fs.file-max` and the
kernel's default is the maximum, 9223372036854775807: installed, the example
lowers the ceiling to about two million. karvi's bound is per process: it
raises its soft open-file limit to the hard one, warning when it cannot, and
the packaged daemon unit sets `LimitNOFILE=131072`. No document named the
file but `docs/FILES.md`'s "the sysctl example". It leaves the tree, 2a's
`/usr/share/karvi/` list, and the sentences that name it; 2a and 2b above
name it as it stood. *Not taken:* a corrected value (the ceiling is the
host's, and the kernel's default is the maximum); a document for it (it
would document lowering a limit).

**Issue 2e, the package a delivered artifact, agreed.** No release has shipped
a package: a release is its source bundle (the vendored source, the evidence,
the qualified executables of one build) and checksums, and BUILD-HOWTO's §10
installs by copying `bin/`'s three executables to
`/usr/local/lib/karvi/<name>-vX.Y.Z` with symlinks in `/usr/local/bin`.
Nothing in BUILD-HOWTO, the Makefile, the scripts, or `release/` built or
named a `.deb`; the debian rules build the executables a second time; and the
tree could not build it (no `debian/changelog`, the directory at
`packaging/debian`, `Build-Depends: golang-1.26-go` where BUILD-HOWTO's §2
installs Go from upstream). 2a and 2c describe a package install, so a site
installing by §10 would be sent to directories it does not have. The
operator's ruling: the package is delivered, built by one documented step and
published with each release beside the bundle; BUILD-HOWTO's §10 offers it,
and a §10 install finds the same material in the bundle's `packaging/` and
`docs/`. `golang-1.26-go` is referenced no more; where a Go version is named,
it is 1.27, now mainstream. The parts that follow, one at a time: where the
debian directory lives, the changelog, `Build-Depends`, and which executables
the package carries. *Not taken:* the package left unbuilt (2a and 2c would
describe what nobody receives).

**Issue 2e, part 1, where the package is built, agreed.** In the lab builds
`dpkg-buildpackage` began with `dh clean`, which ran `make clean`, and that
target removes `bin/`'s executables (the released ones in this tree, the
qualified ones in a bundle); it wrote `debian/karvi/`, `debian/files`, the
substvars, and `debian/.debhelper/` into the source, and the `.deb`,
`.buildinfo`, and `.changes` into the source's parent. The debian directory
stays at `packaging/debian`, beside the units and the manual pages, and `make
deb` builds a staged copy: the tree as it stands, a git checkout or an
extracted bundle, copied with `bin/` as part 4 decides into a fresh build
directory, `packaging/debian` moved to `debian/` there, `dpkg-buildpackage -us
-uc -b` run, the `.deb` copied to `dist/` (ignored by git), and the build
directory removed. *Not taken:* `debian/` at the root built in place (the
usual layout, but `dh clean` removes `bin/` unless the rules override it, and
the build's files land in the tree and its parent).

**Issue 2e, part 2, the package's changelog, agreed.** A release's number is
assigned in eight places in one commit (`60ee9a9`, "the number assigned
(release 1/3)"), where CHANGELOG.md's block is dated: at a release's tag its
first heading is `## 0.27.0 - 2026-10-06`, on the dev line `## Unreleased`.
`make deb` writes `debian/changelog` into its staged copy, and none is kept in
the tree: one entry, its version `VERSION` when CHANGELOG.md's first heading is
`## VERSION - DATE` and `VERSION+dev` when it is `## Unreleased` (after the
release and before the next, so a dev package never passes for the release);
dated the heading's date at 00:00 UTC, so a release's package repeats, or the
build's time for `+dev`; the maintainer the control file's; the text one line
pointing to `/usr/share/doc/karvi/CHANGELOG.md`. A heading naming another
version than `VERSION` is refused. *Not taken:* a kept `debian/changelog` (a
ninth place for each number); the dev line refused (no package to try before
a release).

**Issue 2e, part 3, `Build-Depends` and the Go named, agreed.** This host's
archive has no `golang-1.27-go`, and its `golang-go` offers 1.26; the Go that
builds karvi is upstream's 1.27.1 in `/usr/local/go`, as BUILD-HOWTO's §2
installs it, so a Go build dependency is met by no package and the lab built
with `-d`. The control file's `Build-Depends` is `debhelper-compat (= 13)`
alone: the toolchain is §2's, on `PATH`, and `go.mod` states the minimum. The
operator's ruling on the version applies to the other references: `go.mod`'s
`go 1.27.0`, BUILDING.md's "Go 1.27 or later", BUILD-QUALIFICATION's "Go
1.27+". *Not taken:* `golang-go (>= 2:1.27)` (no archive here meets it, and
every build would skip the check); `go 1.26.0` kept (a minimum the ruling
retired).

**Issue 2e, part 4, the executables the package carries, agreed.** The lab
package's `karvi`, built by the rules' `make build`, said `commit:
development` and `build_time: 1970-01-01T00:00:00Z`, the Makefile's defaults;
the released `bin/karvi` says `source-release-v0.27.0` and `2026-10-06`, its
sha256 the one `CHECKSUMS.sha256` lists. The package carries `bin/`'s
executables as they are; the rules build and test nothing (the qualified build
was tested by the release verifier). `make deb` checks `bin/` against
`CHECKSUMS.sha256`: under a release's heading the three must match, so the
package holds the published executables; under `## Unreleased` they must not,
since matching would be the release's executables labelled `+dev`, and it
names `make build` as the step before. `dh_strip` and `dh_dwz` do nothing: the
executables are built `-s -w`, and a further strip would change the bytes the
sums promise. *Not taken:* the rules' `make build` given `COMMIT` and
`BUILD_TIME` (a second, untested build of a release beside the qualified
one); `bin/` taken unchecked (whatever sits there would be packaged).

**Issue 2, built.** The debian rules install as 2a has it and build, test,
clean, and strip nothing; `dh_compress` leaves `.md` alone; the control's
`Build-Depends` is `debhelper-compat (= 13)`. `scripts/build-deb.sh`, run by
`make deb`, reads `VERSION` and CHANGELOG.md's first heading, checks `bin/`
against `CHECKSUMS.sha256` as part 4 has it, copies the tree without `.git`
and `dist/` to a fresh directory, moves `packaging/debian` to `debian/` there,
writes the changelog, runs `dpkg-buildpackage -us -uc -b`, and copies the
package to `dist/`. `packaging/sysctl/` is removed (archived first). The units'
`Documentation=` lines, COLLECTION, PRUNE, OPERATIONS, the README, and
`karvi crun --help` (so `karvi-crun(1)`) name `/usr/share/karvi/…`;
`docs/FILES.md`'s §4.7 lists the two directories; BUILD-HOWTO gains §4.3,
`make deb`, and §10 offers the package before the copy and symlinks; the
ROADMAP's first Next entry is gone; `go.mod` says `go 1.27.0` (`vendor/` and
`go.sum` unchanged), BUILDING and BUILD-QUALIFICATION Go 1.27.

**Found in the build: links to documents 2a left out.** The first package
held `README.md`, `CHANGELOG.md`, `LICENSE.md`, `docs/`, and `examples/`, and
its documents linked to `BUILD-HOWTO.md`, `BUILDING.md`, `ROADMAP.md`, and
`release/BUILD-RESULT.md`, which it did not hold. To keep 2a's own rule, that
every relative link holds, the package installs every top-level document and
`release/`'s records; 277 relative links in the installed documents resolve.

**Executed.** In the tree, where `bin/` holds the released executables and
CHANGELOG.md is headed `## Unreleased`, `make deb` refused, naming `make
build`, and made nothing. A copy with its own build of the tree gave
`karvi_0.27.0+dev_amd64.deb`: the three executables byte-identical to `bin/`,
the 23 guides plain, each unit's `Documentation=` file in the package, the
cron scripts executable, the changelog `karvi (0.27.0+dev)`. A copy headed
`## 0.27.0 - 2026-10-06` with the released `bin/` gave `karvi_0.27.0_amd64.deb`
twice, byte-identical, its executables the sums `CHECKSUMS.sha256` lists; with
one dev executable in `bin/` it refused, and under a heading naming 0.26.0 it
refused. The evidence is kept beside the tree
(`release-design-evidence/package-and-logging-2026-10-08`).

## 40. The scratch bounded for the askpass socket (2026-10-08)

The outline's item: nothing checks that the askpass broker's socket,
`<tempdir>/askpass-<pid>-<16 hex>.sock`, fits a Unix socket's path.

**What it gains.** A `tempdir` too long for the socket refused once, before
any device, naming the length, where the job is admitted and every device over
`system` fails with `bind: invalid argument`, which names none. It waits on
nothing.

**The evidence.** A lab build of `cf24c10`, this host's OpenSSH, `command
--transport system --platform linux 127.0.0.1 'echo ok'` with `tempdir` an
existing 0700 directory of a given length:

| `tempdir` | Result |
|---|---|
| 60 and 70 bytes | ran, exit 0 |
| 71, 72, 73, 75, 80 bytes | exit 1, `status=connection_error error=askpass_start_failed: listen unix …/askpass-713590-b8ff19963b319986.sock: bind: invalid argument` |
| 71, `run` through the daemon | exit 101, the same failure on each device |
| 71, `login` | exit 1, the same text after the header |
| `auto`, no `/dev/shm/karvi`, a 67-byte `basedir` | the chain took `<basedir>/tmp` (71 bytes); every `system` device failed, exit 1; native ran |

The socket's name is 30 bytes and the pid's digits, and a path socket holds at
most 107 bytes. The runs' pids had six digits, so 70 + 1 + 36 = 107 passed;
this host's `pid_max` is 4194304, and at a seven-digit pid the 70-byte
directory fails too. Every `system` session starts a broker, whatever its
credentials, and so does every login; native binds nothing in the scratch.
`auto`'s chain takes a candidate whatever its length.

**Issue 1, the bound and where it is checked, agreed.** The bound is a
constant beside `MaxControlPathRoot`, 107 − 1 − 37 = 69 bytes, 37 the longest
name: `askpass-`, a seven-digit pid (Linux's `PID_MAX_LIMIT`, 4194304), `-`, 16
hex, `.sock`. An explicit `tempdir` longer is refused where the scratch is
resolved, `tempdir_too_long` (config, exit 2), naming the path, its length,
and the bound, as `control_path_root_too_long` reads: at every job's
admission, in the client or the daemon under the configuration that governs
the job, and at a login's start, each before any device. A native-only job is
refused too, as an unwritable `tempdir` is today: admission resolves the
scratch for every job. `tempdir`'s registry text and `ERROR-CODES.md` state
the bound. *Not taken:* a check at planning in the client, as the control-path
root has (the client's configuration is not the daemon's, chapter 37's 5c);
refusing only where a target runs over `system` (admission resolves the
scratch for every job); a shorter name (the pid is the sweep's, the 16 hex
guard a collision, and any name leaves a bound); the bound judged by the
running pid's length (the 70-byte failure at a seven-digit pid); an abstract
socket (no path, but no 0600 file, the token its only guard).

**Issue 2, `auto`'s chain, agreed.** Today `config show --explain` resolves
`tempdir` to the 71-byte `<basedir>/tmp` without a word. The scratch's chain
carries issue 1's bound: a candidate longer than 69 bytes is passed by and the
chain moves on, the job not refused. Only `<basedir>/tmp` can be, under a
`basedir` over 65 bytes: `/dev/shm/karvi/<username>` is at most 47 bytes with a
32-byte name, `/tmp/karvi-<uid>` and `/var/tmp/karvi-<uid>` about 20. `config
show --explain` lists it on its `passed:` line with the reason, present or
not: a chain lists only a present candidate passed by, absence being a host's
ordinary state, and a length is not the host's state but why the chain moved.
At the job it is silent, as a candidate passed by is, the scratch landing in
`/tmp/karvi-<uid>`, as private. The spool's chain binds no socket and takes no
bound. No candidate fitting is the chain's refusal,
`scratch_directory_unavailable`, unreachable by length alone. *Not taken:*
refusing `auto` under a long `basedir` (`auto` finds a place that works, and a
later candidate does); a warning at every job (the place is as private, and the
operator chose none).

**Found: the daemon's socket.** `daemon.socket` has no length check either:
under a 100-byte `basedir` a `run` waited out the daemon's start and exited
112, `daemon_start_failed: … context deadline exceeded`, the cause,
`daemon_serve_failed: listen unix …/daemon.sock: bind: invalid argument`, in
`daemon.log` alone. The operator's ruling: to the ROADMAP ("The daemon's
socket path bounded").

**Built.** `osutil.MaxScratchDir` is 69, beside `AskpassSocketName`; the
scratch's chain carries it, `place` naming a longer candidate on its `passed:`
list, present or not, and `make` skipping it; an explicit `tempdir` longer is
`tempdir_too_long` (config, exit 2) from both, before anything is made. Every
caller already resolves through them: a job's admission, a login's start, a
recorded login's timing log, and the daemon's sweep at its start, which logs
the code. `TestScratchBoundedForTheAskpassSocket` binds the longest name under
69 bytes and fails to bind one byte more, and drives the chain and the
explicit path at the bound and one over, under a short directory of its own so
that the maker never reaches the host's `/tmp/karvi-<uid>`.

**Executed.** On a lab build, the same commands: a 69-byte `tempdir` ran; a
70-byte one was refused, exit 2, before any device and leaving no job
directory, alike over native, through the daemon (`tempdir_too_long: daemon
validation: …`), and in a login:

```text
tempdir_too_long: the scratch directory /tmp/nd.OoUn/item7/vvv…v is 70 bytes;
an askpass socket's path must fit 107 bytes with its name of up to 37, so the
directory may be at most 69 bytes; set tempdir to a shorter directory
```

Under the 67-byte `basedir` with `auto`, the `system` command ran in
`/tmp/karvi-1000`, and `config show --explain` read:

```text
resolved:   /tmp/karvi-1000
passed:     /tmp/nd.OoUn/item7/byyy…y/tmp: 71 bytes, longer than the askpass socket allows (69)
```

**Closed.** The full battery passed on the lab build. The daemon's socket waits
in the ROADMAP. The evidence is kept beside the tree
(`release-design-evidence/askpass-socket-length-2026-10-08`).

## 41. The suites' work directories made 0700 (2026-10-08)

The outline's item: under the operator's umask 0002 suites fail, karvi
refusing the trust store in their work directory.

**What it gains.** The battery under the operator's own umask (0002 here, the
default for a user-private group), without the `umask 022` every battery
command has carried, and a suite run by its shebang as it is; the work
directories, which hold the fake's secret digest, the trust store, and the
state tree, not writable by the group. It waits on nothing.

**The evidence.** Each of the 17 suites alone against a lab build of
`790ac63`:

| Run | Result |
|---|---|
| umask 022, `FORCE_COLOR=3` | all 17 passed |
| umask 0002, `FORCE_COLOR` unset | 10 failed: `smoke`, `halt`, `hostkey`, `transcript`, `display`, `v060`, `v061`, `v070`, `v080`, `v090` (exit 109 or 1) |
| umask 0002, the twelve pid-named work directories made by `mktemp -d` (a copy of the tree) | all 12 passed |

`FORCE_COLOR` reaches no suite since `json-test.sh` was made immune
(`0554f7d`). Twelve suites name their work directory
`${TMPDIR:-/tmp}/karvi-NAME-$$` and never make it themselves: `install -d -m
700 "$BASE" …` makes it as a parent, at the umask's mode, 0775 under 0002, and
`host_trust_store "$TMP"` puts the trust store in it, which karvi refuses:
`host_key_directory_permission: known_hosts=…/known_hosts: directory mode is
0775; group or others may write it; expected 0700 or 0750`. The native and
spool suites passed, their stores in `$TMP/store`, made 0700; the other five
make theirs by `mktemp -d`, 0700. Chapter 33 counted fifteen; the tree then
held these twelve, and `output-scale-run.sh`, pid-named too, makes its own
0700. karvi said the refusal, under `--quiet` too, on its standard error and
in the result line; the suites redirect it into their work directory under
`set -e`, die with karvi's status, and their cleanup removes the files, so
most ended with no line.

**Asked: the trust store in shared mode.** The operator asked whether karvi
takes the store under `/opt/karvi/users/<user>` at 0770 or 0750. The rule is
that the store's directory is not writable by its group or others: 0700 and
0750 pass, 0770 and 0775 are refused. The operator's first activity makes the
private root 0750, so the store under `auto` passes as built. Executed with
`basedir` an existing directory: at 0750 the store was enrolled, 0600; at 0770
exit 109, `host_key_directory_permission`. The message's "expected 0700 or
0750" names two of the modes the rule accepts (0755 passes too).

**Issue 1, how a suite makes its work directory, agreed.** Every suite makes
it by `TMP=$(mktemp -d "${TMPDIR:-/tmp}/karvi-NAME-XXXXXX")`: 0700 whatever the
umask, unique without the pid, and named, so a leftover traces to its suite.
The twelve pid-named suites change, and the five bare `mktemp -d` take the same
template, one form for all seventeen; nothing reads the pid in those names,
and the `SECRET` of the smoke and halt suites keeps its `$$`. The battery runs
under the host's umask, `FORCE_COLOR` as it is. *Not taken:* `umask 077` or
`022` set in the suites or by `run_suites` (karvi would run at a umask not the
operator's, and a suite by its shebang would still depend on its caller); a
function in `scripts/lib/host.sh` that makes the directory and places the
store (it would run in `$(…)`, a subshell that cannot export the store's
variable, and the work is one `mktemp`).

**Found: a suite's failure says nothing.** The operator's ruling: to the
ROADMAP ("A suite's failure that names its call").

**Built and executed.** The seventeen suites make their work directory by the
template, each named for its suite. ShellCheck gives the same findings before
and after. The battery, `run_suites` against a lab build of `790ac63`, passed
under the shell's own umask 0002 with `FORCE_COLOR=3`, no prefix; a first run
was set aside, since the tree was changed while it ran (a suite starting then
reads the old script), and repeated with the tree unchanged throughout.

**The refusal's wording, on the operator's word.** The message no longer names
two modes the rule accepts among others: `host_key_directory_permission:
known_hosts=…/known_hosts: directory mode is 0770; group or others must not
write it`, executed on a lab build (exit 109), and the code's description
says the same. The policy test checks the wording at each refused mode.

**Closed.** The battery runs under the operator's umask and colour settings as
they are. The evidence is kept beside the tree
(`release-design-evidence/suite-work-dirs-2026-10-08`).

## 42. The example configurations in the package (2026-10-08)

The point raised after chapter 39: `configs/` holds files the package does not
install, and documents name them.

**What it gains.** A packaged install that holds what its own documents name:
`reference.toml` and `config generate`'s output send the reader to
`configs/example.toml`, as COLLECTION and CREDENTIAL-CSV do, and five guides
name `configs/reference.toml`, none of them under `/usr/share/doc/karvi`. It
waits on nothing.

**The evidence.** `configs/` holds five files:

| File | What it is | Packaged |
|---|---|---|
| `reference.toml` | generated, every key with its default | `/usr/share/karvi/reference.toml` |
| `example.toml` | a commented example of the dynamic sections: session-init and its map, `[ssh-algorithms-profile]` and its map, platform aliases and resolution, `crun`, three credential backends; its inventory `examples/inventory.csv`, relative to the working directory | no |
| `development.toml` | a local evaluation overlay, validated by BUILD-HOWTO §6 and `verify-shipped.sh` | no |
| `ssh-legacy.conf`, `ssh-ancient.conf` | inert OpenSSH `Host` snippets adding `ssh-rsa`, SHA-1 key exchange, and CBC ciphers | no |

`examples/config.toml`, a separate file, is the curated site configuration of
the example set, packaged under `/usr/share/doc/karvi/examples/`.

**Issue 1, where `configs/` goes, agreed.** The operator asked whether
`/usr/share/karvi/configs` or another path. `configs/` is installed as
`/usr/share/doc/karvi/configs/`, by chapter 39's 2a, the documents and examples
under `/usr/share/doc/karvi` in the tree's layout, so that every `configs/…` a
document names resolves under `/usr/share/doc/karvi/` as `docs/X.md` does:
`example.toml` and `development.toml` as files, and `reference.toml` a link to
`/usr/share/karvi/reference.toml`, one copy under both names. `example.toml`'s
`examples/inventory.csv` resolves from `/usr/share/doc/karvi`, the source root
its header asks for, and the pointers in `reference.toml`, `config generate`'s
output, and the guides read as they are. *Not taken:*
`/usr/share/karvi/configs/` (`/usr/share/karvi` holds material a site copies
into place, the example's relative inventory would not resolve, and the
documents' `configs/` names would not resolve beside them); `example.toml`
merged into `examples/config.toml` (one example, but a documentation change of
its own, which can follow).

**Issue 2, the two OpenSSH snippets, agreed: removed.**
`configs/ssh-legacy.conf` and `ssh-ancient.conf` are relics of
`ssh.legacy-hosts`, removed in v0.12.0; nothing in the tree names them, and they
reach none of karvi's sessions. Executed with `ssh -G` on a configuration shaped
like karvi's, its four algorithm lists first and the snippet included after, as
`ssh.include-user-config` places the operator's file: the ciphers and key
exchange were karvi's exactly, the snippet's `aes128-cbc` and
`diffie-hellman-group-exchange-sha1` appearing only with the snippet read alone.
A legacy device is reached by `[ssh-algorithms-profile.NAME]` and
`[[ssh-algorithms-map]]`, which `example.toml` shows, the configuration the
record of the exception. They are archived and removed, the operator's rule for
what nothing reads. *Not taken:* packaged with a note that karvi ignores them
(two files whose purpose would be to say not to use them).

**Built.** The debian rules install `configs/example.toml` and
`development.toml` under `/usr/share/doc/karvi/configs/`, and `karvi.links`
makes `reference.toml` there a link to `/usr/share/karvi/reference.toml`; the
two snippets are archived (`archive-2026-10-08/untracked/configs/`) and
removed. FILES' table of the package and the CHANGELOG say so.

**Found in the build: `example.toml` gzipped.** The first lab package held
`configs/example.toml.gz`: `dh_compress` gzips a file over 4 KB under
`/usr/share/doc` (10,146 bytes), and leaves `examples/` alone, which is why
`examples/config.toml` (4,836 bytes) had stayed plain. The rules' `dh_compress
-X.md` takes `-X.toml` as well.

**Executed.** `make deb` in a copy of the tree with its own `make build`: the
package holds `/usr/share/doc/karvi/configs/development.toml` and
`example.toml`, plain, and `reference.toml -> ../../../karvi/reference.toml`;
its other contents are as before. Extracted, from its `usr/share/doc/karvi`,
the packaged `karvi config validate` passed `configs/example.toml`,
`development.toml`, and `reference.toml` through the link, and `config show
--explain` resolved `example.toml`'s inventory to
`…/usr/share/doc/karvi/examples/inventory.csv`. The evidence is kept beside the
tree (`release-design-evidence/example-configs-2026-10-08`).

## 43. The release with the Debian package (2026-10-08)

The point raised after chapter 39: the release steps build no package, while
BUILD-HOWTO's §10 offers it as the install.

**What it gains.** A site installs karvi by `dpkg -i` from a published,
checksummed artifact: the executables, the manual pages, the documents, and
the material chapters 39 and 42 laid out. It waits on nothing.

**The evidence.** Rehearsed in clones of `dev`, never the tree:

| Rehearsal | Result |
|---|---|
| (A) a clone made a 0.28.0: `VERSION`, the dated heading, `make build checksums` under the release identity; the bundle, extracted, and `make deb` there twice | `karvi_0.28.0_amd64.deb`, the same bytes twice, its three executables the bundle's `CHECKSUMS.sha256`; the extraction gained no `dist/` |
| (B) a clone of `dev`: `make build checksums`, as `baseline.sh` runs them, then `make deb` | refused, `bin/ holds the executables CHECKSUMS.sha256 lists for 0.27.0`, of the dev build itself; a rebuild with another `BUILD_TIME` passed, `0.27.0+dev` |

`package-source-bundle.sh` excludes only `.git`, and `artifacts.sh`'s
clean-tree gate reads `git status`, which ignores `dist/`: a package built in
the tree would ship in the next bundle. Taken in four issues: where the release
builds the package, how it is published, `make deb`'s rule on the dev line
that (B) shows refusing a dev build, and whether the baseline and the release
verifier build one.

**Issue 1, where the release builds the package, agreed.** `artifacts.sh`
gains a step after [2], where the bundle is extracted and its own verifier
passes: `make deb` in that extraction twice, into the work directory
(`DIST=$W/deb1`, `$W/deb2`), the two the same bytes, `make deb` itself checking
that the bundle's `bin/` is the qualified build `CHECKSUMS.sha256` lists under
the release's heading; the package copied beside the bundle in the tools'
parent as `karvi_X.Y.Z_amd64.deb`, Debian's naming. Built from the bundle, the
published bundle is shown to yield the published package, as BUILD-HOWTO §4.3
says it does; nothing is written in the tree, so no `dist/` reaches a later
bundle; the tag, the clean-tree gate, and the bundle's reproducibility stay as
they are. *Not taken:* `make deb` in the tagged tree (its `dist/`, ignored by
git, would ship in the next bundle); built in `evidence-rest.sh` before the tag
(the package would precede its commit's tag); a signed package (unsigned,
`-us -uc`, and checksummed as the bundle is; a signed apt repository is a later
question).

**Issue 2, how it is published and installed, agreed.** Executed in a
throwaway `ubuntu:24.04` container, the rehearsal's packages mounted read-only:
`dpkg -i` failed on the bare image, `karvi depends on openssh-client; however:
Package openssh-client is not installed.`; `apt-get install
./karvi_0.28.0_amd64.deb` resolved it and installed, exit 0; the image's dpkg
excludes `/usr/share/doc/*` and `/usr/share/man/*`, so `/usr/share/doc/karvi`
held `changelog.gz` and `copyright` alone and no manual page was installed,
while the 24 files under `/usr/share/karvi` were; `apt` installing the older
`+dev` package refused without `--allow-downgrades` (exit 100) and took it
with, and `dpkg -i` went forward again with the dependency present. The
release's assets gain `karvi_X.Y.Z_amd64.deb` and its `.sha256`, made as the
bundle's is, and the aggregate `karvi-vX.Y.Z-artifacts.sha256` lists both
artifacts, five assets where there were three; DOWNLOADS lists the package and
what it installs; BUILD-HOWTO §10 downloads it, checks it with `sha256sum -c`,
installs it by `sudo apt install ./karvi_X.Y.Z_amd64.deb` where it showed
`dpkg -i`, rolls back by `sudo apt install --allow-downgrades
./karvi_<older>_amd64.deb`, and says in one sentence that a minimized image
installs no documents or manual pages, only the executables and
`/usr/share/karvi`, the documents being in the bundle and the repository. `gh
release create` and the check of the assets downloaded back stay manual steps
of the sequence, the check taking the package's sum beside the bundle's. *Not
taken:* the documents moved out of `/usr/share/doc` to survive minimization
(the image's exclusion is its administrator's choice, `unminimize` restores
them, and Debian puts documents there); `dpkg -i` kept (it fails without
`openssh-client`); the aggregate alone for the package (the bundle has its own
`.sha256`, one form for both).

**Issue 3, `make deb`'s rule on the dev line, agreed.** Chapter 39's part 4
took `bin/` matching `CHECKSUMS.sha256` for the release's build, which holds
only while the file is the release's: `make build` on `dev` is deterministic
(`COMMIT=development`, `BUILD_TIME` 1970), so after `make checksums`, which
`baseline.sh` and the verifier's last step run, a dev build matches, and (B)
was refused as the release's. The executables say what they are, `version
--format json`'s `commit`: `source-release-v0.27.0` for the released `bin/`,
`source-release-v0.28.0` for the rehearsal's release build, `development` for
`make build`, the git hash for the release verifier's, `790ac63-lab` for a lab
build. Under `## Unreleased` `build-deb.sh` reads that `commit` through
`scripts/lib/json.sh`'s `json_get` and refuses only `source-release-v<VERSION>`,
a released build that would be labelled `+dev`; any other is `VERSION+dev`,
whatever `CHECKSUMS.sha256` holds. Under the release's heading the rule stays:
`bin/` the executables `CHECKSUMS.sha256` lists. The refusal names the identity
it read, and BUILD-HOWTO §4.3's dev-line sentence says the same. *Not taken:*
the `CHECKSUMS.sha256` committed at `HEAD` through git (a dev tree need not be
a checkout, and the identity is what a package's reader sees); the dev-line
check dropped (the released `bin/` in this tree, under `## Unreleased`, would
be a `0.27.0+dev` package of 0.27.0's bytes); the text form's `commit:` line
(the JSON is the contract, `json_get` the scripts' one reader).

**Issue 4, the release verifier builds and checks the package, agreed.**
`verify-release.sh` runs `make deb` into a temporary `DIST` after the suites,
before its closing `make checksums`, and removes it, leaving nothing in the
tree: at the baseline the build's identity is the git hash, so issue 3 makes it
`VERSION+dev`; in the release's evidence, under the release's heading, the
verifier's rebuild is the `CHECKSUMS.sha256` written before it, so the package
is the release's. One checker, `scripts/check-deb.sh PACKAGE TREE`, run by the
verifier and by `artifacts.sh`'s step on the release's package: the three
executables are `bin/`'s bytes; every document and example configuration the
rules install from the tree is in the package under its own name, uncompressed
(it would have caught `example.toml.gz`); each unit's `Documentation=file:`
target is in the package; `configs/reference.toml` reaches
`/usr/share/karvi/reference.toml`. BUILD-HOWTO §1 lists `dpkg-dev` and
`debhelper` for a release builder, and the verifier fails without them rather
than skip what it checks. *Not taken:* `baseline.sh` building it too (it runs
the verifier); `verify-bundle.sh` building it (issue 1 builds it from the
bundle at the tag); every relative link in the installed documents checked
(the documents' own link check holds in the tree, and the checker's list says
each is installed under its name).

**Issue 5, lintian with the two deliberate findings overridden, agreed.**
Lintian 2.129.0 on the rehearsal's package: `E: statically-linked-binary` for
`usr/bin/karvi`, `karvi-askpass`, and `karvi-prune`, and `W:
script-not-executable` for the two `usr/share/karvi/crun/*.example` hooks.
`scripts/check-deb.sh` runs `lintian --fail-on error,warning`, so an error or
warning not overridden fails the verifier and the release's step. The package
carries `packaging/debian/karvi.lintian-overrides`, installed by debhelper as
`/usr/share/lintian/overrides/karvi`, each with its reason: the static
executables are built with `CGO_ENABLED=0` by design, so one qualified build
runs on any amd64 host whatever its C library; the hook examples are inert
until a site copies one without `.example` at mode 0755 (COLLECTION §5).
Informational tags do not fail: the one shown, `extra-license-file`, is
`LICENSE.md` among the documents, which link to it. BUILD-HOWTO §1 lists
`lintian` for a release builder. Executed: the rehearsal's bundle with the
overrides gave a package holding them, and `lintian --fail-on error,warning`
exited 0 with nothing printed. *Not taken:* the hook examples installed 0755
(an executable under `/usr/share` invites running it in place, and the
`.example` suffix with 0644 says copy it first); no lintian (Debian's own
checker, seconds); informational tags failing too (`extra-license-file` is
deliberate, and informational tags move between lintian releases more than
errors do).

**S1, the tree, built.** `build-deb.sh` takes issue 3's rule: under `##
Unreleased` it reads `bin/karvi-linux-amd64`'s `commit` by `json_get` and
refuses `source-release-vVERSION`, naming it (`bin/ holds the released 0.27.0
executables (commit source-release-v0.27.0); build the dev line first: make
build`, the tree's own `bin/`). `scripts/check-deb.sh PACKAGE TREE` makes issue
4's checks and issue 5's `lintian --fail-on error,warning`;
`packaging/debian/karvi.lintian-overrides` holds the five overrides with their
reasons; `verify-release.sh` builds the tree's package into a temporary `DIST`
after the suites and before `make checksums`, checks it, and removes it.
BUILD-HOWTO §1 installs `dpkg-dev`, `debhelper`, and `lintian`, §4.3 states the
identity rule, `DIST`, and the checker, and §10 installs the published package
by `apt` with its checksum, the rollback, and the minimized image; FILES lists
the overrides file. Executed in copies of the tree with their own builds: a
dev package passed `check-deb.sh`; with `dh_compress -X.md` alone it failed,
`usr/share/doc/karvi/configs/example.toml is missing or differs from
configs/example.toml`; without the overrides it failed on lintian's three
errors and two warnings; a copy given the release's identity under `##
Unreleased` was refused by name; the same copy headed `## 0.28.0 - 2026-10-08`
with its checksums, bundled and extracted, gave `karvi_0.28.0_amd64.deb`, which
passed against the extraction, and the extraction gained no `dist/`. S2, the
release tools' artifact step (issues 1 and 2), lies outside the tree, in
`release-tools/`.

**S1, verified.** What `baseline.sh` runs, `make build checksums tools-build
generated-clean` and `verify-release.sh`, on a copy of the working tree with
its `.git`, under the shell's umask 0002: the first run failed in the spool
suite's S9, `output_preflight_space`, before the package step, since `/tmp`, a
4.9 GB tmpfs, held a 1.4 GB lab where S9 needs about 3.2 GB free; with the lab
trimmed, the second passed (13:54:51 to 14:01:11 UTC), its new step building
`karvi_0.27.0+dev_amd64.deb` and `check-deb.sh` passing on it, the copy
gaining no `dist/` and no temporary directory left.

**S2, the release tools' artifact step, issue 1: the step stops on a failure,
agreed.** Rehearsed in a clone of `dev` outside the tree and `/tmp`, made a
0.28.0 with its number in six of its seven files and the changelog's heading,
`verify-shipped.sh` left at 0.27.0 as a place missed, the release-identity
build, a commit, and a local tag `karvi-v0.28.0`, the tools copied beside it so
that `OUT` and `ROOT` resolve into the rehearsal: the tools' `artifacts.sh`,
under `set -u` alone, printed `archive verify-bundle exit 1`, wrote the
aggregate, said `done`, and exited 0; `[1]`'s `cmp … && echo` would have gone on
as well had the two bundles differed. `artifacts.sh` now runs under
`set -euo pipefail` and stops at the first failed check with exit 1, naming its
step on standard error: `[1]` the bundle packaged twice differing, `[2]`
`verify-bundle.sh` failing in the extraction (the log's last lines shown), and
the package step, either `make deb` failing, the two packages differing, or
`check-deb.sh` failing. The package and the aggregate are written only after
every check before them passed, and an earlier run's package and aggregate are
removed at the start, so the aggregate with `done` and exit 0 is the sign the
artifacts may be published; the bundle `[1]` wrote stays for the rerun to
replace. *Not taken:* the bundle removed on a failure (the aggregate's absence
already says so); the new step alone made strict (the run shows the same gap in
`[1]` and `[2]`). On that failure `bundle-verify.log` was empty:
`verify-shipped.sh`'s `[ "$(cat VERSION)" = "0.27.0" ]` fails under `set -e`
without a word, the gap of the [ROADMAP](../ROADMAP.md#later)'s "A suite's
failure that names its call", where it is noted.

**S2, built and rehearsed.** `release-tools/artifacts.sh` (outside the tree,
archived first in `archive-2026-10-08/untracked/release-tools-before-s2/`) gains
`[3]`: in the verified extraction, `DIST=$W/deb1 make deb` and
`DIST=$W/deb2 make deb`, the two compared, `./scripts/check-deb.sh` on the first
against the extraction, the package copied beside the bundle as
`karvi_X.Y.Z_amd64.deb` with its `.sha256` written as the bundle's; `[4]`, the
aggregate, lists both. The rehearsal's tag made whole, its eighth place given
0.28.0, the run passed in 5 minutes 23 seconds (14:59:58 to 15:05:21 UTC): the
bundle reproducible, `verify-bundle.sh` exit 0 in its extraction,
`karvi_0.28.0_amd64.deb` the same bytes twice (5,712,762), `check-deb.sh`
passed, `sha256sum -c` of the aggregate and of the package's `.sha256` OK, the
package's three executables the bundle's `CHECKSUMS.sha256`, and the extraction
gained no `dist/`. With a stand-in `lintian` first on `PATH` printing one error,
the run stopped at `[3]`,
`artifacts: [3] check-deb.sh failed on karvi_0.28.0_amd64.deb`, exit 1,
lintian's line shown, and no package, `.sha256`, or aggregate was left beside
the bundle. The [release gates](BUILD-QUALIFICATION.md#release-gates) state
the step's rule beside the clean-tree gate.

## 44. The release 0.28.0 (2026-10-08)

The fifth public release, on the operator's word and number, carrying chapters
37 to 43 and the first with the Debian package; before the number, the tests'
scratch made short whatever `TMPDIR` is, as a section of its own. The sequence
is [chapter 29](#29-the-release-0270-2026-10-06)'s, with the artifact step of
chapter 43's S2.

**What it gains.** A site installs karvi by `apt install` from a published,
checksummed package built from the published bundle, with the documents and the
material chapters 39 and 42 laid out; and every change since 0.27.0, `command`
over the native transport by default, the session processes ending with karvi,
the scratch swept and bounded, a first contact and `insecure` said on every
path, from a published artifact.

**Before the number.** The first 1/3 (`f906f8b`, kept as
`archive/rel-0.28.0-1of3-first` in the archive's mirror) was followed by the
core evidence, whose socket-length run exited 1 at both `TMPDIR` lengths, where
0.27.0's had passed. The whole tree at both lengths named five tests, all of
item 7's bound meeting `t.TempDir()`, which lies under `TMPDIR` and appends the
test's name: `TestServerSweepsScratchAtStart`'s explicit `tempdir` was 68 to 70
bytes under the 25-byte `TMPDIR`, so it failed by chance, and the sweep rightly
left a `tempdir` too long alone; `TestScratchRootNeverCreated`'s and
`TestScratchPlaceNamesWhatTheMakerTakes`'s `<basedir>/tmp` were 74 to 87 bytes,
the latter 64 to 66 under plain `/tmp`, three bytes from the bound; at 145 bytes
`TestPlaceResolver` and `TestResolveSpoolDir` as well. Under the long `TMPDIR`
`auto` passed `<basedir>/tmp` by and fell through to the host's
`/tmp/karvi-<uid>`, and the maker's call in `TestScratchRootNeverCreated` made
`/tmp/karvi-1000` on the host, empty, 0700; it was removed. The product was
right; the tests were not. Builds on this host with the default `TMPDIR` had not
fallen through: the baseline and the core run left no `/tmp/karvi-1000`.

**The rule, agreed.** No test's scratch, nor a `basedir` whose `tmp` is the
scratch's `auto` candidate, derives from `TMPDIR`: each takes its directory from
one helper, `testdir.Short(t)` in the new leaf package `internal/testdir`, a
directory under `/tmp` removed at the test's end, which `osutil`'s own tests can
import where `osutiltest`, importing `osutil`, cannot; so `auto` takes
`<basedir>/tmp` at any `TMPDIR` length, and `/tmp/karvi-<uid>` is reached only
when no other candidate fits, never by a build on this host. The five tests take
it, and `systemssh`'s command-session test, which wrote the same
`os.MkdirTemp("/tmp", …)` inline, takes it too. Both verifiers' list of the
host's places (`scripts/lib/host.sh`) gains whether `/tmp/karvi-<uid>` and
`/var/tmp/karvi-<uid>` exist, by existence alone, since the operator's own
daemon keeps its spool in the first: a run that makes either fails the verifier.
*Not taken:* `auto`'s `/tmp` and `/var/tmp` candidates made a variable the tests
redirect, as `SpoolRoots` is (with short directories no test reaches them, and
the product stays as it is); the 145-byte run dropped (a release gate).
Executed: the whole tree at `TMPDIR` lengths 25 and 145 and at the default
passed, 61 packages, neither directory made; `host_shared_unchanged` failed
naming `/tmp/karvi-1000` when it appeared between the two lists. The first 1/3
was reset, `dev` taking the fix on `bb5b68f`, and the baseline ran again before
the number.

**The sequence, as run**, from the first baseline's start at 15:36:37 to the
published check at 18:42, the fix before the number and the stops for the
operator's word included:

| Step | Wall (UTC) | Result |
|---|---|---|
| the baseline on a clean clone of `dev` at `bb5b68f` | 15:36:37 to 15:43:23 | gofmt, make, the release verifier with its package step, exit 0 each |
| the first 1/3, the core evidence | 15:45:21, 15:45:28 to 15:47:19 | `f906f8b`; 902 named tests, vet 0; the socket-length run exit 1 at both lengths |
| before the number | to 17:48:19 | the first 1/3 archived and reset; `71b00a2`, the tests' short scratch and the host check |
| the baseline again, at `71b00a2` | 17:48:27 to 17:54:49 | exit 0 each; neither host scratch directory made |
| the number, the compatibility example, 1/3 | 17:55:54, 17:56:05 | `ec3e7da`, the first 1/3's bytes; the released v0.27.0 daemon `compatible: false`, the run refused with `daemon_incompatible` (exit 112) before any job |
| the core evidence | 17:56:10 to 17:57:59 | 902 named tests across 84 packages, vet 0, both socket lengths exit 0 |
| the documents | 17:59:03 | `a9f80eb` (2/3): `release/` from the v0.27.0 pattern, five assets; README's Go 1.26, stale |
| the remaining evidence | 17:59:18 to 18:05:44 | the shipped checks exit 0, the release verifier exit 0 with the release's package checked, the checksums unchanged by its rebuild; the replay skipped |
| 3/3, the tag, `main` | 18:06:25 | `53984e7`, `karvi-v0.28.0`, `main` fast-forwarded from `8250167` |
| the artifacts | 18:06:33 to 18:11:47 | the bundle and the package each reproducible byte for byte; the bundle verified from its own archive, the package checked against its extraction; 15,862,053 and 5,715,154 bytes |
| the package installed in a throwaway `ubuntu:24.04` | 18:13 | `apt install` exit 0 with `openssh-client`; the installed executables `CHECKSUMS.sha256`'s |
| the push and the GitHub release | 18:41:44 | `dev`, `main`, and the tag pushed; release `karvi-v0.28.0` with the five assets, marked latest; the assets downloaded back match |

**Executed.** The published state:

```text
$ gh api repos/robert-patrick-texas/karvi/releases/latest --jq '"latest: \(.tag_name) draft=\(.draft) prerelease=\(.prerelease) assets=\(.assets|length)"'
latest: karvi-v0.28.0 draft=false prerelease=false assets=5
$ sha256sum -c karvi-v0.28.0-artifacts.sha256    # the assets downloaded back
karvi-v0.28.0-source-linux-amd64.tar.gz: OK
karvi_0.28.0_amd64.deb: OK
```

**Found on the way.** README still named Go 1.26 for the full build, stale since
`go.mod` named 1.27.0; the 2/3 sweep set it to 1.27. The module metadata's
`DefaultGODEBUG` line, Go 1.26's compatibility settings, is gone from the
executables with `go.mod`'s `go 1.27.0`, so `go-version-m.txt` moved in 3/3
where 0.27.0's had not. The first 1/3's branch, kept in the tree while the fix
went in, was deleted after the release: the archive's mirror and patch keep the
commit.

**Not taken.** A patch release of 0.27.0 for the tests' scratch: the defect is
in tests and the verifiers, not in a shipped executable, and it rides with the
number it was found at.

**Roadmap.** ROADMAP's Next was build numbers in the version, then the
packaged user unit's sandbox; after the release the operator moved build
numbers to Later, so Next is the user unit's sandbox, and added to Later the
three items recorded only in chapters 32, 37, and 42: a server that closes
before its exit status, `insecure`'s comparison from the exec master's own
line, and one example configuration; and moved "A job under its client's
configuration" from Later to Next, after the sandbox, so that the daemon runs
each job with the settings active at its client.

## 45. The user units without a sandbox (2026-10-08)

ROADMAP's first Next item: on an Ubuntu host a user unit given `PrivateTmp=yes`,
`ProtectSystem=strict`, and `ProtectHome=read-only` ran in the client's mount
namespace ([chapter 12](#12---cd-and---fs-for-run-and-command-2026-10-03),
"Found on the way"). The question was what the three packaged user units get,
what the documents promise, and what the units become.

**What it gains.** The units and the documents stop promising a confinement the
host does not give, and a daemon's files land where the operator expects on
every host; the next item, a job under its client's configuration, may rely on
a path meaning the same to the daemon as to the client. It waits on nothing.

**The evidence.** On this host, Ubuntu 26.04.1, systemd 259, kernel 7.0.0,
`kernel.apparmor_restrict_unprivileged_userns = 1`; a probe shell, then the lab
daemon (`b4c5f88-lab`, every place under `/dev/shm/kl`, outside `/tmp`,
`/var/tmp`, and the home) under `karvi-daemon.service`'s `[Service]` lines:

| Case | Mount namespace | `/` and the home | `/tmp` |
|---|---|---|---|
| user manager, the three options | the client's | writable | the host's |
| the same with `PrivateUsers=yes` or `=self` | the client's | writable | the host's |
| system manager, `User=netops`, the same options | its own | read-only | private |

The kernel's audit names the cause at each start under the user manager:

```text
apparmor="AUDIT" operation="userns_create" info="Userns create - transitioning profile"
  profile="unconfined" ... execpath="/usr/lib/systemd/systemd-executor"
apparmor="DENIED" operation="capable" profile="unprivileged_userns" ... capname="sys_admin"
```

The executor's user namespace is moved to the restricting profile, which denies
the capability a mount namespace needs; systemd starts the unit anyway and the
user journal says nothing (systemd.exec(5): sandboxing is "gracefully turned
off" where the mechanism is unavailable). `PrivateUsers=` asks for the very
namespace denied. The denial comes before the unit's command, so
`karvi-prune.service` and `karvi-crun.service` get the daemon's result. Where
the sandbox applied, the system unit, chapter 12's limit ran as predicted:

```text
$ k.sh run --target 127.0.0.1 --platform linux --cmd 'echo ok' --cd=/tmp/kl-D
! collection=/tmp/kl-D replaced=1 kept=0          exit 0
  in the client's /tmp: ls: cannot access '/tmp/kl-D': No such file or directory
  in the daemon's /tmp: 127-0-0-1
$ k.sh run ... --cd=/home/netops/kl-probe-D
crun_directory_not_writable: daemon validation: mkdir /home/netops/kl-probe-D:
  read-only file system; a shared collection directory needs mode 2770 or 2775
  (group write and search, setgid, no sticky bit)  exit 9
```

**Issue 1, what the user units are, agreed.** The three user units carry no
file-system sandbox: no `PrivateTmp`, `ProtectSystem`, `ProtectHome`, or
`ReadWritePaths`; `NoNewPrivileges`, the umask, and the limits stay. A user unit
runs with the operator's own view of the files, as the client does, and writes
wherever the operator may; confinement is the system units', and the site's
`karvi-prune.service` keeps its sandbox, which applies there. The daemon's
drop-in example (`karvi-daemon.service.d/crun.conf.example`), whose only content
was a `ReadWritePaths` line, is removed with its install line, and OPERATIONS,
COLLECTION, PRUNE, FILES, and DESIGN say the rule in place of the drop-in and
the limit. The cost, stated: on a host where the sandbox would apply, a
compromised daemon may write where the operator may, the home's startup files
included. *Not taken:* keeping the lines and stating the limit (the behaviour
differing by host, and the `/tmp` divergence exactly where the sandbox works);
a system unit per operator with `User=` (the sandbox applies, the divergence
and the client's own launch stay); an AppArmor profile granting the executor a
user namespace (the host's restriction weakened for every user manager);
`PrivateUsers=`.

**Found on the way.** `crun_directory_not_writable` misstates its cause, with
no read-only mount involved. Executed with the released 0.28.0, `--no-daemon`:

```text
--cd=/usr/kl-x      cannot create /usr/kl-x: mkdir /usr/kl-x: permission denied;
                    a shared collection directory needs mode 2770 or 2775 (…)
--cd=…/afile        (a regular file) …/afile exists and is not a folder;
                    a shared collection directory needs mode 2770 or 2775 (…)
--cd=…/afile/sub    (sub absent, its parent a file) …/afile/sub exists and is
                    not a folder; a shared collection directory needs …
```

`EnsureCollectionDirectory` appends the shared directory's mode hint to every
failure, where it fits only a present directory the operator cannot create
files in and the sticky case; and `EnsureOutputDirectory` names the path given
to `mkdir` as the one that is not a folder, where a parent is. The operator
agreed to fix both in a section of their own after this chapter's commit,
before the next item.

The evidence, with the probe and the scripts, is in
`release-design-evidence/user-unit-sandbox-2026-10-08/`; the lab was removed.

## 46. The collection directory's refusal names its cause (2026-10-08)

Found in [chapter 45](#45-the-user-units-without-a-sandbox-2026-10-08):
`crun_directory_not_writable` misstated its cause with no read-only mount
involved; fixed in a section of its own before the next item, on the operator's
word.

**What it gains.** An operator whose `--cd` or `crun.directory` is refused reads
the cause, and the shared directory's mode only where the mode is the remedy.
It waits on nothing.

**The rule, agreed.** Two defects, both in `internal/osutil/paths.go`:

1. `EnsureCollectionDirectory` appended the shape a shared directory needs
   (mode 2770 or 2775) to every failure. It is appended now only where the
   shape is the remedy: a present real directory refused by permission (the
   operator cannot create files in it) and the sticky case; any other cause is
   named alone, the code unchanged.
2. `mkdirs` stopped at an `lstat` that failed with `ENOTDIR` and returned that
   error, so `EnsureOutputDirectory` named the path given to it as the one
   that "exists and is not a folder" when a folder above it was the file. The
   walk now goes up past `ENOTDIR` and names the component that is a file; the
   job, transcript, and collection trees all take it.

The registry's description of the code and OPERATIONS's shared-directory
paragraph say which causes carry the shape; `docs/ERROR-CODES.md` is
regenerated.

**Executed**, a lab build of the tree with the change, `--no-daemon`, every
place under a short lab directory:

```text
--cd=/usr/kl-x            cannot create /usr/kl-x: mkdir /usr/kl-x: permission denied
--cd=…/afile              …/afile exists and is not a folder
--cd=…/afile/sub          …/afile exists and is not a folder
--cd=…/locked (0500)      …/locked is not writable by the operator: open
                          …/locked/.karvi-write-…: permission denied; a shared
                          collection directory needs mode 2770 or 2775 (…)
--cd=…/locked/crun        cannot create …/locked/crun: mkdir …: permission denied
```

each `crun_directory_not_writable`, exit 9. The tests state each cause and
whether the shape appears; against the code before the change,
`TestEnsureOutputDirectory` and `TestEnsureCollectionDirectory` fail on the
file above the path and on the shape after a file.

## 47. A job under its client's configuration (2026-10-08)

ROADMAP's first Next item: the daemon runs every job under the configuration it
loaded at its own start, the first launcher's `--config` and `--set`, its
environment and working directory, while the plan carries a handful of keys in
its `execution`, `output`, `ping`, and `dispatch` blocks.

**What it gains.** Every job runs under the configuration its invocation
resolved, through a daemon or not: a later client's `--set`, `--config`,
environment, and working directory reach its own job; the execution policy's
silent override, deferred in [chapter
37](#37-the-first-contact-said-alike-over-both-transports-2026-10-07)'s issue
5c, ends; a job records one configuration digest where it records two (the
plan's the client's, the manifest's and the audit's the daemon's); and the
checks made twice, the control-path root's length, the Telnet gate, the
transport and platform tables, the timeouts' fallback, give one answer. It
waits on nothing since [chapter 45](#45-the-user-units-without-a-sandbox-2026-10-08)
gave the daemon the client's view of the files.

**The evidence.** A lab build of `cf1a0f7`, every place under a short lab
directory, this host's OpenSSH on 127.0.0.1:

```text
$ karvi --set ssh.known-hosts-file=kh1/known_hosts run … --cmd 'echo one'
daemon started …   ! ssh accepted new host-key 127.0.0.1 (ED25519)
$ karvi --set ssh.known-hosts-file=kh2/known_hosts run … --cmd 'echo two'
two                                    (no first contact: the daemon used kh1)
$ karvi --set ssh.known-hosts-file=kh2/known_hosts config show ssh.known-hosts-file
value: "/dev/shm/kl/kh2/known_hosts"   source: --set[7]
  kh1: 1 line; kh2: absent
$ karvi --set ssh.known-hosts-file=kh2/known_hosts run --no-daemon … 'echo three'
! ssh accepted new host-key 127.0.0.1 (ED25519)    kh2: 1 line
```

The job takes `Config: s.Config`, the daemon's snapshot (`daemon/prepare.go`);
the client launches the daemon with `--config`, `--set`, `--quiet`, `--debug`,
`--timezone`, and `--ansi` alone (`cli/daemon_command.go`'s `globalArgs`), so a
command's own flags (`--ssh-host-key-policy`, the dispatch options, `--ping`)
never reach it, and the daemon's working directory is its first launcher's.
About eighty keys the job reads come from the daemon. A resolved configuration
is about 8.7 KB as JSON (`config show --format json`); a frame is bounded at
8 MiB by default.

**Issue 1, what the client sends, agreed.** The client sends its resolved
values, the key and value set its digest covers, never its inputs; the daemon
builds the job's configuration from them and reads none of its own for the job.
configload keeps a path as written and its readers resolve it against the
process's working directory, so the client makes every path-valued key absolute
(its `~` and its working directory) before sending; `auto` stays `auto`, which
the daemon resolves by the same chain for the same user on the same host, now
seeing the same files, so it is the place `config show --explain` names to the
client. The ROADMAP's "resolved as the client resolved them" narrows to this:
the client resolves what depends on itself, `auto` stays the chain's. *Not
taken:* the inputs (files read again by the daemon, perhaps changed, in an
environment not the client's); the `auto` chains' outcomes sent as well (a
choice of the moment frozen, for no gain on one host).

**Issue 2, the keys that belong to the process, agreed.** Executed with one
daemon started by the first run:

```text
$ karvi --set audit.file=au/a1.jsonl run … 'echo one'      daemon started …/base/socket/daemon.sock
$ karvi --set audit.file=au/a2.jsonl run … 'echo two'
  a1.jsonl: 6 audit lines   (a2 absent: the second job's audit went to a1)
$ karvi --set basedir=base2 --set audit.file=au/a2.jsonl run … 'echo three'
  daemon started …/base2/socket/daemon.sock   a2.jsonl: 3 lines   daemons: 2
```

The daemon's own configuration keeps what exists because there is a daemon
process: the seven `daemon.*` keys (`socket`, `max-ipc-frame-bytes`,
`max-accepted-jobs`, `shutdown-idle-timer`, `shutdown-grace-seconds`,
`forced-grace-seconds`, and `start-timeout`, the last read by the client that
launches or stops one); its own `basedir` for its socket, `state/daemon.json`,
and `logs/daemon.log`; and the sweeps it runs once at its start. Everything a
job reads is the job's: the `audit.*` keys (a job's events go to the sink its
client names, as `--no-daemon` does; `cancel_requested`, about a job, goes to
that job's sink, and the daemon opens no sink of its own);
`dispatch.server-max-inflight` and `sessions.shared-capacity-root`, the host's
cap and the operators' shared ledger, which a `--no-daemon` job already applies
from its own client, a site wanting one value locking the key in the global
file every client loads; and the job's `basedir`, for its state tree and the
`auto` scratch candidate, which agrees with the daemon's while `daemon.socket`
is `auto`, since another `basedir` reaches another daemon. *Not taken:* the
audit and capacity keys kept the daemon's as host-wide (the daemon path
differing from `--no-daemon` for one invocation, the inconsistency the item
removes); a job refused for a `basedir` other than the daemon's (reachable only
with an explicit socket, and nothing breaks).

**Issue 3, where it travels and how the job records it, agreed for now.** The
plan carries the configuration as a `configuration` block of issue 1's values,
covered by `plan_digest`, so it travels in the draft at `prepare_job` and in the
final plan at `commit_job`, and prepare's own checks (the DNS timeout, the
execution policy, the readiness line's policy digest) take the job's
configuration too. `sources.config_digest` becomes the digest of that block,
computed as configload computes it (sha256 of `CanonicalJSON`); the daemon
rebuilds the configuration from the block, recomputes the digest, and refuses a
plan whose two differ as invalid; the audit's `policy.config_digest`, the debug
line, and the manifest name that one digest where today they name the daemon's.
The record is the manifest, which already holds the whole plan, so every value
the job ran under (about 8.7 KB beside a manifest of about 8 KB). Which file or
`--set` set a key is not carried: that is the client's `config show --explain`.
Paths are made absolute in configload at load, so the client's `config show`
prints the path and the digest the job records; a relative path already meant
the working directory, so no reader's place changes. *Not taken:* the
configuration beside the plan in the commit request (outside `plan_digest`, and
absent from the draft); the digest alone (a job's folder could not say what it
ran under); absolute paths made only for sending (`config show` and the job
disagreeing on the digest whenever a path is relative). The operator is not
sure a digest is needed here; the issue stands for now, and the digests are the
question of ROADMAP's "A review of every digest".

**Issue 4, the per-key carriage and the daemon's launch, agreed.** A plan field
that copies a key's value leaves the plan, and the daemon and the in-process
path read the key from the job's configuration: the `execution` block, the
`ping` block (the probe count a constant), `blind_wait_ns`, `output`'s
`max_command_bytes`, `max_job_bytes`, `persist`, `files`, `crop_to_dot`, and
`collection.file_mode`, and `dispatch`'s halt and gate counts, percentages, and
delay; `crun`'s rule that it writes no `output.NAME.txt` is applied where
`files` is read, from the word. What the client decided or computed for the job
stays: the targets and commands, each target's transport, port, and channel,
the reserved `output.root`, the resolved `collection.directory`, the word and
the suffix, `format`, `follow`, and `dispatch`'s mode, widths (with their
CPU-derived defaults), order, and shuffle key. The options the client already
writes to keys through its lock-aware flag layer (`--ping`,
`--ssh-host-key-policy`, `--ssh-known-hosts-file`, `--order`, the dispatch
options) reach the daemon with the configuration; `--continue-device-on-error`
becomes one of them, a flag-origin `execution.halt-device-on-command-error =
false`, `crun` the word that implies it, so a lock on the key refuses the
option as it refuses `--ping`; `--echo`, `--border`, and `--noborder` stay in
the plan, display choices with no key that `job follow` takes as well. The
client launches a daemon as today, `--config`, `--set`, and the `KARVI__*`
environment included, since they may place `basedir` or set a `daemon.*` key;
the daemon reads only the process's keys from them. The manifest's `policy`
block ("the daemon's own SSH and Telnet policy at acceptance", written and never
read) and the readiness line's `policy_digest` (printed by `--exercise` alone)
are removed: the first becomes a copy of four keys in the plan, the second a
digest of the job's own keys and no fact of the daemon. *Not taken:* the blocks
kept beside the configuration (two sources for one value); keys for `--echo`
and the borders; a daemon launched without `--set` (a `basedir` placed by
`--set` would put the daemon where its client does not look).

**Issue 5, the request's size, agreed.** Measured on the lab: a resolved
configuration of the built-in defaults is 8,768 bytes as JSON, 11,332 with the
shipped `configs/example.toml`; a one-target plan 2,853 bytes compact, of which
the target 831; that job's `manifest.json` 8,314. A request is one frame,
bounded on each side by its own `daemon.max-ipc-frame-bytes` (8 MiB by default,
64 KiB to 64 MiB): at the default about 10,000 targets fit and the
configuration costs the room of about 14; at the minimum about 75 fit, about 62
with it. The configuration rides inside that bound, with no bound or key of its
own; a plan too large fails as today, `ipc_frame_too_large` when the client
writes the frame; the plan still travels twice, issue 3 needing the draft's
configuration at prepare. Each job's manifest gains about 9 to 11 KB, about
1 MB a day for a `crun` every 15 minutes and 31 MB per operator at the 31-day
retention. In a shared job tree the manifest is 0640, so the tree's group reads
the job's configuration: paths, backend names, environment variable names,
platform tables, macros, and no secret, since no key holds one (none marked
sensitive; the credential backends name variables and files, never values).
*Not taken:* the configuration trimmed to the keys a job reads (a list kept
beside the registry, and the digest no longer `config show`'s); a bound of its
own; a compressed request (the frame stays readable JSON, for some 10 KB).

**Issue 6, locks, agreed.** Executed in a private mount namespace where a tmpfs
over `/opt` held a global file existing nowhere else, karvi run as the
operator:

```text
/opt/karvi/config.toml:  [ssh] host-key-policy = "secure"   [config-lock] "ssh.host-key-policy" = true
$ karvi config show ssh.host-key-policy
value: "secure"   source: /opt/karvi/config.toml:2
$ karvi run --ssh-host-key-policy insecure --target 127.0.0.1 …
config_lock_violation: write blocked by lock "ssh.host-key-policy" declared at /opt/karvi/config.toml:4 … at --ssh-host-key-policy     exit 3
$ karvi --set ssh.host-key-policy=insecure run --no-daemon …
config_lock_violation: … at --set[9]     exit 3
  (no daemon socket: refused before any daemon was reached)
```

The client's load enforces the locks, as for every invocation today; the
daemon takes the configuration it receives and checks it against no global file
of its own; the configuration block carries values, not lock declarations. The
client and its daemon are one operator, so what a client could send that
operator could run with `--no-daemon`: a lock guards a site against a mistaken
setting, not one process of an operator against another. The daemon's global
file is the one it read at its start, so a re-check would judge a fresh client
by a stale reading in either direction; and the stale case of today ends, a
daemon started before a site added a lock no longer running jobs under the
value it loaded. *Not taken:* the daemon re-checking locked keys against its own
global file; the lock declarations carried for the record (`config show
--explain` names them). *Found on the way:* `config show` prints `reload:
next-job` for every key, the registry stamping it on all, while the daemon never
reloads; under the item it becomes true of every job key, and the process's
keys take effect at the daemon's next start. The stamp is corrected in the
build.

**Issue 7, the schema numbers, agreed.** Each counter against what issues 1 to
6 change:

| Counter | Today | Change | Moves |
|---|---|---|---|
| execution plan | 11 | the `configuration` block added; the `execution` and `ping` blocks, `blind_wait_ns`, `output`'s copied fields, `collection.file_mode`, and `dispatch`'s halt and gate fields removed | 12 |
| daemon IPC | 10 | `prepare_job` and `commit_job` carry the plan at 12; the readiness `policy_digest` removed | 11 |
| job | 2 | the manifest's `policy` block removed | 3 |
| plan report | 1 | `daemons[].execution_policy_digest` removed | 2 |
| registry | 27 | no key or table field changes; the process's keys change `reload_class` in `schema/config-schema.json` | 28 |
| configuration | 6 | the file's format unchanged | no |
| audit | 1 | `policy.config_digest` in its place, naming the job's configuration | no |

The other counters (command record 3, scoreboard 3, credential package 2,
metrics 1, inventory 1) are untouched. The registry moves because the artifact
it numbers changes under it, and its rule's comment widens to a key's reload
class; the audit stays, its field in place and the change said in CHANGELOG. A
running 0.28.0 daemon is `compatible: false` by its version, and now by its
schema too. The release that carries the item resumes the lifecycle replay,
`release-tools/common.sh`'s `PRIOR_*` values set to v0.28.0 after the tools are
archived. *Not taken:* the registry left at 27 (the artifact changed under an
unchanged number); the audit counter moved (no field changes).

**The build, agreed.** In sections, each committed on the operator's word: S1,
path-valued keys made absolute at load and the process's keys' reload class
(registry 28); S2, the plan's `configuration` block (plan 12, IPC 11) and the
daemon handing the job the configuration built from it, the behaviour change,
with this chapter's first example run again; S3, the plan's copied fields
removed and `--continue-device-on-error` a flag-origin key write; S4, the
daemon reading only the process's keys, `cancel_requested` in the job's sink,
the manifest's `policy` block (job 3) and the readiness `policy_digest` (plan
report 2) removed; S5, the documents and the close.

**S1, how configload knows a path, agreed.** Executed on a lab build of
`aa71bf8` from a working directory `w`, configload keeping every value as
written and each reader resolving it against its own working directory:

```text
$ karvi --set crun.directory=rel config show --explain crun.directory
value:      "rel"                       resolved:   …/w/rel
$ karvi config show ssh.identities
value:      ["~/.ssh/id_ed25519", "~/.ssh/id_ecdsa", "~/.ssh/id_rsa"]   source: <builtin>
$ karvi --set audit.file=auto config show --explain audit.file
value:      "auto"                      resolved:   …/w/auto
$ karvi --set basedir=none config show --explain basedir
value:      "none"                      resolved:   …/w/none
$ karvi --set sharedroot=rel config show --explain sharedroot
value:      "rel"                       (no resolved line)
$ karvi --set ssh.transports.system=bin/ssh config show --explain ssh.transports.system
transport_system_executable_unavailable: … stat …/b/bin/ssh: no such file or directory
```

Only the CLI's `placeResolver` knew which keys are paths, and it missed
`sharedroot` and `ssh.identities`. The registry now marks them, and both read
the mark: a fixed entry's `place`, set on the thirteen string keys `basedir`,
`sharedroot`, `tempdir`, `spooldir`, `scoreboards`, `ssh.known-hosts-file`,
`ssh.control-path-root`, `sessions.shared-capacity-root`, `daemon.socket`,
`output.root`, `crun.directory`, `transcript.root`, and `audit.file`, and on the
list `ssh.identities`; a dynamic table's `places`, naming its path fields,
`inventory-source.N.path` and `credential-backend.X.path`, `ca-file`,
`client-cert-file`, and `client-key-file`; and a marked key's `words`, the
values that are not paths, `auto` for the eleven other keys whose default is
`auto`, `auto` and `none` for `sharedroot`, none for the others, an empty value
being unset for every key. Words are per key because a word shared by all would
keep `basedir=none` and `audit.file=auto`, paths today, as written for a daemon
to resolve against its own directory. At the end of the load, after the macros
and the validation and before the digest, a marked value neither empty nor one
of its key's words has `~` expanded from the load's home and is made absolute
against the working directory; validation still judges the value as written (a
shared credential file's absolute path, `ssh.identities`' absolute or `~/`
form), and a `~user` or a `~` with the home unknown fails at the load under its
own code, naming the key and its source. `config show` prints the absolute path
from the layer that set it, the built-in `ssh.identities` with the home
expanded, `--explain`'s `resolved:` line for an explicit path equals the value,
and the digest covers the absolute paths; no reader's place changes. *Not
taken:* a `path` kind (every switch on the kind changed for a string parsed like
any other); `auto` and `none` as words of every key; `sharedroot` and
`ssh.identities` left unmarked (a daemon in another directory reading another
`sharedroot`). *Found on the way:* `ssh.transports.*` is an executable
specification, not a path, a relative one with a separator taken from karvi's
own directory; a bare `ssh.transports.system = "ssh"` is found on the daemon's
`PATH`, its first launcher's environment, an input the client does not send,
which this item does not close.

**S1, the working directory's refusal, agreed.** Executed from a working
directory removed, the build before S1 and the step as first built:

```text
== before
--set crun.directory=rel config show crun.directory   exit 0, "rel"
--set crun.directory=rel crun --no-daemon …           crun_directory_unavailable: getwd: no such file or directory   exit 2
--set output.root=jobs run --no-daemon …               output_root_unavailable: getwd: no such file or directory      exit 9
run --no-daemon --fs=.txt …                            crun_directory_unavailable: getwd: … (the working directory, implied by --fs)
== the step as first built
each of the four                                       path_other_user_home_unsupported: getwd: … for crun.directory at --set[10]   exit 2
```

The refusal moves to the load, for every command and `config show` too, naming
the key and the option that set it; it takes a code of its own,
`config_working_directory_unavailable` (config, exit 2: a configured relative
path cannot be made absolute, the working directory unreadable).
`crun_directory_unavailable`, whose two causes (`~` without a home, a relative
path from an unreadable working directory) now fail at the load, is retired
for it: the draft's fallback is `crun_directory_not_writable`, the code
`resolveTree` gives the collection tree, and `impliedDirectory` checks that one
alone, the load's refusal naming `--fs` as its source. `output_root_unavailable`
stays, a fallback `reserve.go` and `jobdir.go` share. *Not taken:*
`path_other_user_home_unsupported` widened (one code for two causes);
`crun_directory_unavailable` kept for the refusal (a collection's code for any
path key).

**S1, the reload class, agreed.** Executed on the lab, the idle check on a
minute ticker, so a 1m timer stops a daemon at its second tick:

```text
$ karvi --set daemon.shutdown-idle-timer=10m daemon start
$ karvi --set daemon.shutdown-idle-timer=1m run … 'echo one'
  150 s idle later: status: running
$ karvi --set daemon.shutdown-idle-timer=1m run … 'echo one'           (no daemon running)
  daemon started …   the daemon's command line: … --set daemon.shutdown-idle-timer=1m daemon serve
  150 s idle later: daemon_unreachable …   daemon.log: "stopping: idle" idle=1m59s value=1m0s
$ KARVI__DAEMON__SHUTDOWN_IDLE_TIMER=1m karvi run … 'echo two'       (no daemon running)
  150 s idle later: daemon_unreachable …   daemon.log: "stopping: idle" idle=1m59s value=1m0s
```

A daemon's settings are the launching invocation's, by `--config`, `--set`,
`KARVI__*`, or the files, for its life; a later client's value reaches it only
by a restart, while `config show --explain` printed `reload: next-job` for
every key. Two classes: `daemon-start`, a running daemon keeping the value it
started with and a later one applying at its next start, for
`daemon.max-accepted-jobs`, `daemon.shutdown-idle-timer`, and
`daemon.shutdown-grace-seconds`, read by the daemon alone, and
`daemon.max-ipc-frame-bytes` and `daemon.forced-grace-seconds`, read by every
client for its own side too (its frame bound, `daemon stop`'s wait), the class
naming the daemon's side; and `next-job`, the default, read by the invocation
that sets the key or, from S2, by the job it submits, `daemon.start-timeout`
(read by the client that launches or stops a daemon), `daemon.socket`, and
`basedir` among them, the next invocation reaching or starting the daemon at
the new place (issue 2's `basedir=base2`). `daemon.socket`'s text states the
edge: an explicit socket with another `basedir` reaches the same daemon, its
own state and log under the `basedir` it started with. The five rows carry the
class, `Entries()` filling `next-job` where a row says nothing; `--explain`'s
`reload:` line and `schema/config-schema.json` carry it; the registry moves to
28, its rule's comment widened to a key's path mark and reload class. Within
`dev`, `next-job` runs ahead of the code until S2; the artifact ships in the
release that carries S2 to S4. *Not taken:* a class of their own for
`daemon.socket` and `basedir` (the next invocation takes the new value, the
daemon it reaches differs); a third class, `next-invocation`, for the
client-only keys (no reader acts on the difference). *Raised, not taken here:*
a client could compare its `daemon.*` values with a running daemon's and say
where they differ, the daemon reporting its values in the status reply; an item
of its own if wanted.

**S2, the job under its client's configuration.** *What it gains:* the behaviour
change itself, a later client's `--set`, `--config`, environment, and working
directory reaching its own job. *It waits on* S1. The daemon reads its own
configuration on the job path in three places, `resolver.PrepareDaemonWith` at
prepare (the DNS timeout and family), `jobexec.Policy`, and the job's `Config`
at commit; everything downstream reads the job's.

**S2.1, the block's form and its check, agreed.** `config show --format json`
prints the resolved values as an object:

```text
$ karvi config show --format json
{ "config_schema_version": 6, "digest": "3fae7423…", "sources": null,
  "values": { "audit.enabled": true, "audit.file": "/dev/shm/kl/base/audit.jsonl", … } }
```

The plan's `configuration` is that `values` object, `{key: value}`, covered by
`plan_digest`; `sources.config_digest` stays the client's digest, since S1 that
of these values. The block decodes with numbers as written, an integer literal
an int64 and any other a float64, and `configload.FromValues` builds the job's
configuration from it, the fixed keys normalized by their registry kind, its
digest computed as the load computes it (sha256 of `CanonicalJSON`), with no
sources, warnings, or locks. `Validate` refuses a missing or empty block at both
stages (`execution_plan_invalid: configuration: is empty`); the daemon builds
the configuration at prepare from the draft and at commit from the final plan,
through one helper, and refuses a digest other than `sources.config_digest` as
`execution_plan_invalid`. *Not taken:* the `[{key, value}]` list (the digest's
encoding, not the configuration's form); the check inside `Validate` (the public
plan package depending on the loader); the block decoded as a plain map (every
integer a float64, the dynamic tables' readers switching on int64).

**S2.2, the in-process job's configuration, agreed.** Beyond the values, the
in-process job reads its snapshot's warnings and sources
(`jobexec/activity.go`); with `warn.toml` (`reject-unknown-env = false`) and
`KARVI__NOPE=1`:

```text
$ karvi --config warn.toml --debug run --no-daemon … 'echo x'
  warning: ignored unknown environment variable KARVI__NOPE
  activity=run activity_id=261008-234450-00 config_digest=b7408e16… config_sources="/dev/shm/kl/cfg/warn.toml"
$ karvi --config warn.toml --debug run … 'echo x'          (through a daemon)
  (neither line: the daemon's job writes to io.Discard)
```

No reader on the job path uses the locks or the provenance. The `--no-daemon`
job keeps the client's loaded snapshot: the block is written from its values,
so it runs under the block's values and digest, and what the snapshot adds, its
warnings and sources, belongs to the load, its own invocation's. A test pins the
two as one configuration: a loaded snapshot, the built-in defaults and
`configs/example.toml`, to the plan's block, through JSON, through
`FromValues`, its values deep-equal and its digest equal. *Not taken:* the
in-process job rebuilt from the block too (one path, the warnings and the debug
line's sources dropped or moved, for a configuration that is the block's
source).

**S2.3, the load's warnings on the daemon path, agreed.** Executed with
`warn.toml` and `KARVI__NOPE=1`:

```text
run --no-daemon …              warning: ignored unknown environment variable KARVI__NOPE
run … (through a daemon)       nothing
config show basedir            nothing
config validate                warnings: 1        (counted, not said)
```

The load's two warnings ("optional include missing", "ignored unknown
environment variable") were printed by the in-process job alone. The client of
the daemon path prints its load's warnings itself, after its load and before it
drafts, the same `warning: …` lines on its standard error; the in-process job
keeps its line, so a `run` or `command` says the same through a daemon or not;
the daemon's job, its configuration from the block, has none. *Not taken in
S2:* every invocation printing its load's warnings once at the load (`login`,
`inspect`, `config show`, `job follow`, the daemon's own load into
`daemon.log`, and `--quiet` reached), an item of its own if wanted.

**S2, built.** The plan's `configuration` block (plan 12, IPC 11), the daemon
building the job's configuration from it at prepare and at commit, the client
of the daemon path printing its load's warnings. The chapter's first example,
run again on a lab build of each section, one daemon started by the first run,
from a working directory under the lab:

```text
== S1's build
$ karvi --set ssh.known-hosts-file=kh1/known_hosts run … 'echo 1'
  daemon started …   ! ssh accepted new host-key 127.0.0.1 (ED25519)
$ karvi --set ssh.known-hosts-file=kh2/known_hosts run … 'echo 2'
  kh1: 1 line; kh2: 0 lines
  the second client's config digest ba07b1403fb7; the second job's audit policy.config_digest 0b8c37d4000c
== S2's build
$ karvi --set ssh.known-hosts-file=kh1/known_hosts run … 'echo 1'
  daemon started …   ! ssh accepted new host-key 127.0.0.1 (ED25519)
$ karvi --set ssh.known-hosts-file=kh2/known_hosts run … 'echo 2'
  ! ssh accepted new host-key 127.0.0.1 (ED25519)
  kh1: 1 line; kh2: 1 line
  the second client's config digest ce99d906890c; the second job's audit policy.config_digest ce99d906890c
```

and `KARVI__NOPE=1` with `warn.toml` through a daemon now prints `warning:
ignored unknown environment variable KARVI__NOPE`. *Found in the build:* the
round trip of the block showed that the load keeps an integer literal of a
number key as an int64, and JSON cannot carry an integral value's float type,
so `FromValues` keeps an integral number an int64, which `Snapshot.Float`
reads as the load's float64, and refuses a fraction on an integer key; the
loader's path rule is its own (`absolutePath`, a test holding it equal to
`osutil.ResolvePath`), since the loader importing `osutil` closed an import
cycle through `records`; the exercise's test of a platform the daemon's
configuration lacked now shows the client's table reaching the daemon's
exercise, the target ready, and whether the executor's and the exercise's
`platform_unknown` backstop stays, its case gone, is S3's question with the
other checks made twice; `scripts/daemon-upgrade-smoke-test.sh` reads the
executable's version and IPC schema in place of 0.28.0 and 10.

**S3, the per-key carriage removed.** *What it gains:* one source for every
value a job reads, the plan carrying only what the client decided or computed
for the job, so `config show`, the plan, and the job cannot disagree on a key.
*It waits on* S2. Mapped: the `execution` block (the executor and the
transports), the `ping` block (the job, the exercise, the executor, the plan
report), `blind_wait_ns`, `output`'s two byte limits, `persist`, `files`, and
`crop_to_dot` (the store, the executor, the Telnet factory, the draft's
file-name check), `collection.file_mode`, and `dispatch`'s halt and gate
values with `continue_device_on_error` leave the plan with their rules in
`Validate`, the keys' registry ranges guarding the values; plan 12 is
unreleased, so no number moves, and the reports keep their shapes, fed from
the configuration. *Found:* `--continue-device-on-error` and the `crun` word
already write `execution.halt-device-on-command-error = false` as flag-origin
values (`continueOptions`); what goes is the duplicate, the plan's field and
the option's `ContinueDeviceOnError`.

**S3.1, the checks made twice, agreed.** With one configuration each pair gives
one answer, and the second can fire only on a plan whose own block does not
support its targets, which no client of this version writes. Executed on S2's
build, `security.allow-telnet` false:

```text
$ karvi run --no-daemon --transport telnet --target 127.0.0.1 …
  telnet_not_allowed: device 127.0.0.1 selects Telnet but security.allow-telnet is false   exit 9
$ karvi run --transport telnet --target 127.0.0.1 …
  telnet_not_allowed: client planning: device 127.0.0.1 selects Telnet …   exit 9   (no daemon started)
$ karvi run --exercise --transport telnet --target 127.0.0.1 …
  telnet_not_allowed: client planning: …   exit 9
```

The executor's platform lookup and the exercise's finding (`platform_unknown`)
and the Telnet transport's refusal in `Open` stay as they are, their comments
naming the case they now cover, a plan this client did not write: the platform
check sits on a lookup the executor needs anyway, and the Telnet one is the
transport's own refusal at the point of use, whoever calls it (`login` is SSH
alone). The control-path root is checked for length by the client and resolved
by the job where it is used, no second check. *Not taken:* removing them (the
executor reaching a device with no definition, the transport opening whatever
its caller asks).

**S3, built.** The copied fields left the plan and their rules `Validate`; the
job, the exercise, the executor, the plan report, and the transports read the
keys of the job's configuration (`platform.Timeouts` and `Pick` removed with
the factories' `Timeouts`); `output.SkippedFiles` is the one rule of the eight
file switches, `--nof`, and `crun`'s `output.NAME.txt`, read by the planner and
the job; `jobexec.IntendedPing` is the plan report's and the exercise's gate;
the option field `ContinueDeviceOnError` went, `continueOptions` writing the
key; `CollectionFileModes` and `BlindWaitMax` lost their last reader and were
removed. Executed on S2's build and S3's, through a daemon, a `crun` and a
second client setting `execution.command-timeout=2s`:

```text
== S2's build
  job files: commands.jsonl commands.txt errors.jsonl failed-devices.txt manifest.json metrics.json summary.json
  collection: 640 cd/127-0-0-1
  plan keys: … blind_wait_ns … dispatch execution … output ping …
  output: collection crop_to_dot dynamic_border echo files follow format max_command_bytes max_job_bytes no_border persist root
  dispatch: continue_device_on_error dispatch_order halt_error_count … wave_gate_timed_delay_ns width
  second client, command-timeout 2s, 'sleep 4': exit 101 command_timeout
== S3's build
  job files: (the same)   collection: 640 cd/127-0-0-1
  plan keys: … configuration dispatch expectations … output plan_digest …
  output: collection dynamic_border echo follow format no_border root
  dispatch: dispatch_order max_width mode start_width width
  second client, command-timeout 2s, 'sleep 4': exit 101 command_timeout
```

The behaviour is the same and the plan carries no copy of a key. The tests that
stated a removed split went with it (the plan's command timeout against the
configuration's); the device-deadline test forces its passed deadline on the
job's configuration through `FromValues`, as it forced it on the plan's block.

**S4, the daemon keeps only the process.** *What it gains:* the daemon holds
only what exists because there is a daemon process, and every record of a job
lands where its client said, the cancel record among them; the manifest's
`policy` block (a copy of four keys now in the block) and the readiness
`policy_digest` (a digest of the job's keys, no fact of the daemon's) go. *It
waits on* S2 and S3. Mapped: the daemon's own configuration feeds the seven
`daemon.*` keys, its `basedir` (socket, state, log), and the start sweeps (its
`spooldir`, `tempdir`, and control-path root), which stay as issue 2 agreed;
besides them it opened its own audit sink, solely for `cancel_requested`, and
`jobexec.Policy` fed the manifest's block and the readiness digest.

**S4.1, the cancel record's sink, agreed.** Executed on S3's build, a daemon
started by a client whose `audit.file` is `a1.jsonl`, a second client's detached
job with `a2.jsonl`, cancelled by that client:

```text
$ karvi --set audit.file=a2.jsonl job cancel 261009-013912-00
  cancel requested: job 261009-013912-00; …
  a1.jsonl: 1 line of the job;  events: run.cancel_requested
  a2.jsonl: 3 lines of the job; events: run.started command_completed run.completed
```

The record goes through a sink opened from the job's configuration, the one
built from the block, at the first cancel request, and closed once written: the
sink definition the job writes through, so the record lands beside the job's
others; it is written once, by the request that cancelled, and a sink that
cannot be opened is a warning in `daemon.log` as before. The daemon opens no
sink of its own. Two sinks on one file are safe: the file sink appends one line
per write, and journald takes each record whole. *Not taken:* the job's open
sink handed to the daemon's job entry (a lifetime shared between the job and
the cancel handler, for one record); the job writing the record when its
context ends with the cancel cause (the record's time and order moving to when
the job notices).

**S4, built.** The cancel record goes through a sink from the job's
configuration and the daemon opens none of its own; the manifest's `policy`
block, `records.ExecutionPolicy`, and `jobexec.Policy` are removed (job 3, the
manifest and the summary), the manifest's builder losing its only error; the
readiness `execution_policy_digest` is removed from the daemon row and from
`--exercise`'s line (plan report 2); the daemon's own configuration is read for
the grace keys, its `basedir`, and the start sweeps alone. Executed on S3's
build and S4's:

```text
== S3's build    a1.jsonl (the daemon's launcher): run.cancel_requested
                 a2.jsonl (the job's client):      run.started command_completed run.completed
== S4's build    a1.jsonl: nothing of the job
                 a2.jsonl: run.started run.cancel_requested command_completed run.completed
== S3's build    daemon local: running pid=… ipc_schema=11 policy_digest=0e118e09…46f1e30 socket=…
                 manifest schema 2 keys: … operator plan policy schema_version selection
== S4's build    daemon local: running pid=… ipc_schema=11 socket=…
                 manifest schema 3 keys: … operator plan schema_version selection
```

A test puts the cancel record in a client's file other than the daemon's and
fails on the daemon's own sink. README's counter line names the tree's
counters once.

**S5, the documents and the close.** DESIGN states the rule in one entry, "A
job runs under its client's configuration", in place of the entry that carried
the timeouts, the byte limit, and the halt rule in the plan, and corrects the
entries that gave the daemon's configuration as a reason (the session-init
table, the place view, the scratch's check, the idle exit, the manifest, the
ICMP gate, the output files); OPERATIONS gains "The daemon and the job's
configuration" with the chapter's first example and the reload class, and its
idle exit names the executable and the `daemon.*` settings; ARCHITECTURE,
FILES, TIMEOUTS, SSH-HOST-KEY-POLICY, `karvi-config(1)` (from its help text),
and `karvi-daemon(1)`'s FILES say the same; CHANGELOG's `## Unreleased` gains
four entries, the behaviour change and the schemas first; ROADMAP's Next 1 is
removed, Next now empty for the operator to choose from Later. The counters
moved: registry 28, execution plan 12, daemon IPC 11, job 3, plan report 2;
the configuration, command record, credential package, scoreboard, audit,
metrics, and inventory counters are 0.28.0's. Raised in the build and not
taken, for the operator to place: every invocation printing its load's warnings
(S2.3); a client comparing its `daemon.*` values with a running daemon's (S1's
reload class); `ssh.transports.system` found on the daemon's `PATH`, its first
launcher's environment (S1). The release's tools (`release/`,
`scripts/verify-shipped.sh`, `scripts/verify-release.sh`, BUILD-HOWTO §8)
still name 0.28.0's counters and move with the release, archived first.

**After the close, the operator's word.** `dev` pushed. The first two points
raised in the build go to the end of ROADMAP's Later: every invocation saying
its configuration's warnings, and a client told when a running daemon's
`daemon.*` settings differ from its own. The third is not an item:
`ssh.transports.system` found on the daemon's `PATH` needs no change, since a
site that needs one OpenSSH for every job sets the key to an absolute path, by
`--set` or the configuration, which the job's configuration carries.
