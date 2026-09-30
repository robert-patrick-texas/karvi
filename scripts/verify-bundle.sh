#!/bin/sh
# Verify the included executables and their source without rebuilding them.
# The release ships the scrapligo-v1 executable: the
# tests and tools run offline from the vendor tree, and the smoke suites run
# against bin/ as shipped. scripts/verify-release.sh is the verifier that
# builds from source. There is one shape of the source: the scrapligo_v1
# build tag and the dependency-free preview module are gone. The checks of
# the shipped bytes themselves are scripts/verify-shipped.sh, run first; the
# suites are scripts/lib/suites.sh, the one list both verifiers run.
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"

./scripts/verify-shipped.sh

# Lint each script with the interpreter its first line names: bash scripts
# use arrays, which /bin/sh (dash) cannot parse.
for script in scripts/*.sh; do
  case "$(head -1 "$script")" in
    '#!/bin/bash'*|'#!/usr/bin/env bash'*) bash -n "$script" ;;   # either bash shebang: sh -n rejects bash syntax
    *) sh -n "$script" ;;
  esac
done
# Go source formatting: gofmt lists nothing outside the vendor tree.
unformatted=$(find . -path ./vendor -prune -o -name '*.go' -type f -print | xargs gofmt -l)
[ -z "$unformatted" ] || { echo "gofmt would change:" >&2; echo "$unformatted" >&2; exit 1; }
[ -s vendor/modules.txt ] || { echo 'vendor/modules.txt is missing; run make vendor' >&2; exit 1; }
# The host's shared places must be as they were after the tests and the
# suites: no test or suite writes to the host's shared trees or scoreboard
# directory (scripts/lib/host.sh).
. "$ROOT/scripts/lib/host.sh"
host_before=$(host_shared_entries)
GOTOOLCHAIN=local go test -count=1 -mod=vendor ./...
GOTOOLCHAIN=local go vet -mod=vendor ./...

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT HUP INT TERM
GOTOOLCHAIN=local go run -mod=vendor ./tools/configgen \
  -schema "$TMP/config-schema.json" -reference "$TMP/reference.toml"
cmp schema/config-schema.json "$TMP/config-schema.json"
cmp configs/reference.toml "$TMP/reference.toml"
GOTOOLCHAIN=local go run -mod=vendor ./tools/errorcodegen \
  -output "$TMP/ERROR-CODES.md"
cmp docs/ERROR-CODES.md "$TMP/ERROR-CODES.md"
# The suites, the one list of scripts/lib/suites.sh (the canary suite needs
# bin/secret-scan, shipped beside the executables; the parity suite builds the
# fake IOS XE device from the tree).
. "$ROOT/scripts/lib/suites.sh"
run_suites
host_shared_unchanged "$host_before"

echo 'bundle verification: pass (the scrapligo-v1 executable as shipped; devices not qualified here)'
