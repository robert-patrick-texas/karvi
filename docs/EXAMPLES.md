# karvi worked examples

Each chapter records one design session as it was executed against the tree:
what it gains, the rule that was settled, the executed example, what was not
taken, and what it leaves for later. A decision the session settles is also an
entry in `docs/DESIGN.md`, which states the settled decisions of the whole
program. The chapters written before the tree was prepared for public release
are kept, with the decision records they cite, in the operator's private
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
4. **The design distilled.** `docs/DESIGN.md`: the settled decisions in
   present tense, each as the rule, the reason, and the alternatives not
   taken, organised by area, drawn from the seventeen decision records and
   the worked-examples chapters through parallel extraction of the settled
   rules and a check of the doubtful ones against the code.
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
repository is refreshed after every commit, and a file about to leave the
tree is copied into the archive before its removal. A removal is recorded in
the chapter that makes it, with the commit before, and the archive's git
history holds the bytes; there is no running removal log in the tree. The
specification is frozen: no revision follows it, no requirement number is
minted, and a design decision is an entry in `docs/DESIGN.md` with its worked
example here. Verification is proportionate to what a change touches: grep
checks for documents, gofmt and vet and build for comments, `go test ./...`
for code, and the full battery of tests and suites at the boundary of a body
of work or for a change of behaviour.

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

**What it gains.** A public reader of CHANGELOG.md finds what the release
they install contains and what the tree in front of them changes, not
twenty-eight releases of a private line described against a specification
they cannot open; a reader of ROADMAP.md finds what is not built yet in the
order it will be taken up, not 2,400 lines of items, most of them done,
numbered by a session's bookkeeping.

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

**The roadmap.** ROADMAP.md replaces the release notes. Its "Next" list is
the operator's order: the man page, the documentation as HTML, the package's
contents, build numbers. Its "Later" list is every roadmap item of the notes
that is still open, stated as a design question with the relationships the
notes had recorded: macro files, job files, the credential items, the digest
review, a no-algorithm-lists setting, sealed packages, crash recovery, a
follow from the live edge, filter field prefixes. The device-qualification
track keeps its four steps. Items the notes still listed as roadmap but which
were built since (the configuration review, the shorter job ID, the spool and
memory budget) are not carried. The release rules that were the notes'
preamble are either DESIGN.md's (the release's artifacts, the clean-tree
gate, breaking changes accepted) or the project's process (commit on the
operator's word, `dev` and `main`), which CONTRIBUTING takes up at its reset.

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

**The release records.** `release/` (the build result, the release status,
the downloads index, the scrapligo evidence, the manifest, and fourteen
evidence logs), `BUILD-RESULT.md`, and `release-manifest.json` described the
v0.23.0 release as executed from the private history, naming the lab
directory, the operator's home, and the retired patch. They are in the archive
under `release-v0.23.0/` and out of the tree; the next release writes them
anew, and the release tool that writes the evidence now creates its
directory first. The two documents that pointed a reader at them (the build
guide's "review the release boundary" step, the qualification document's
replay sentence) read without them.

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

The second release of the day, on the operator's word, carrying chapter 7's
stream work; and the private line's working tree removed from the host,
its archive checked first.

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
