# Contributing

karvi is a Go program for network operations against Cisco IOS XE and similar
devices: interactive login, one-device commands, and governed concurrent runs
through a per-operator daemon. `docs/DESIGN.md` states the settled decisions
and why; `docs/OPERATIONS.md` and the guides beside it say how it is used;
the code and its tests are the reference for what it does.

## Principles a change must keep

Truthful accounting (every device and command ends in exactly one recorded
state, and the counts partition the total), no silent policy bypass (a value
the reader cannot honour is refused, never ignored), one mechanism per
concern, secret-safe diagnostics (no secret in a log, a record, a message,
or a debug line), deterministic selection, and bounded concurrency.
`docs/DESIGN.md` section 1 spells each out with its reasons.

Do not add a credential fallback, a transport fallback, an automatic command
retry, or a compatibility alias without a decision recorded in
`docs/DESIGN.md`. Do not log a secret to make a test easier. Fixtures use
conspicuous non-production sentinels.

## Building and testing

Go is the only build dependency; `vendor/` carries every module, so the
build needs no network. `make build` produces the executables under `bin/`;
`make test` and `make vet` run the Go tests and vet; `make generate` writes
the generated files (the reference configuration, the configuration schema,
the error catalogue) from the registries, and `make generated-clean` fails
when they are stale. `make fmt` formats.

The executable suites live under `scripts/` and run against a built
executable named by `KARVI` (default: the tree's `bin/karvi-linux-amd64`);
`scripts/lib/suites.sh` runs them all in three lanes with `run_suites`. Every
suite keeps its state under its own work directory and never touches the
host's shared places. `scripts/verify-release.sh` is the release's verifier
and `scripts/verify-bundle.sh` verifies a shipped bundle as bytes.

Verification is proportionate to the change: gofmt, vet, and build for a
comment; `go test ./...` and `generated-clean` for code or generator inputs;
the full battery of tests and suites for a change of behaviour or at the
end of a body of work.

## Writing code

Exported identifiers carry useful Go documentation. Non-obvious invariants,
security boundaries, record cardinality, and failure precedence carry
comments that explain why, not only what. New behaviour comes with
table-driven tests and, where it settles a design question, an entry in
`docs/DESIGN.md` and a chapter in `docs/EXAMPLES.md` recording the session
that built it: what it gains, the rule, the executed example, what was not
taken.

Every distinct error cause has its own registered code in
`internal/errorcodes`; a test fails the build when a code is emitted but not
registered or registered but not emitted. A configuration key is a row of
the registry in `configschema`, with its range on the row; a removed key is
a row of the removed-keys table and is refused from every layer.

A script reads JSON through `scripts/lib/json.sh` (`json_get`, `json_has`,
`json_is`) and never by matching a file's text; a must-not-appear check
stops the script rather than relying on a negated pipeline. A Go test that
needs a value from a `.json` file decodes it.

## Branches and releases

Development happens on `dev`; `main` moves only at a release, to the tagged
commit `karvi-vX.Y.Z`. A release ships a source bundle, its checksum, and an
aggregate checksum file, and the release tools refuse a tree that holds
anything git does not track. Breaking changes are accepted before 1.0: a
renamed option or removed key is not aliased, and every release restarts the
daemon. `ROADMAP.md` lists what is open, in order.
