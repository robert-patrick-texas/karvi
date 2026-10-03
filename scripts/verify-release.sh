#!/usr/bin/env bash
# Build and verify an official karvi v0.25.0 release (both transports) from an
# authenticated vendored dependency tree.
set -euo pipefail
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"

command -v go >/dev/null 2>&1 || { echo 'go is required' >&2; exit 1; }
[[ "$(cat VERSION)" == '0.25.0' ]]
[[ -s go.sum ]] || { echo 'go.sum is missing; run make deps on an approved connected builder' >&2; exit 1; }
[[ -s vendor/modules.txt ]] || { echo 'vendor/modules.txt is missing; run make vendor' >&2; exit 1; }
grep -Eq '^# github.com/scrapli/scrapligo v1\.4\.2$' vendor/modules.txt

violations=$(grep -RIl 'github.com/scrapli/scrapligo' --include='*.go' . 2>/dev/null \
  | grep -v '^./vendor/' \
  | grep -v '^./internal/adapters/scrapligov1/' || true)
if [[ -n "$violations" ]]; then
  echo 'ScrapliGo import boundary violation:' >&2
  echo "$violations" >&2
  exit 1
fi

# scrapligo-v1 is karvi's connection with the host-key policy in the
# handshake, under karvi's device session, with no ssh-keyscan.
grep -q 'WithCustomTransport' internal/adapters/scrapligov1/dial.go
grep -q 'HostKeyCallback' internal/adapters/scrapligov1/dial.go
grep -q 'hostkey.Verify' internal/adapters/scrapligov1/dial.go
grep -q 'devsession.Open' internal/transport/native/provider_scrapligov1.go
if grep -Eq 'PrepareNative|InspectRemote|ssh-keyscan|os/exec' internal/adapters/scrapligov1/*.go internal/transport/native/provider_scrapligov1.go; then
  echo 'scrapligo-v1 path runs a subprocess or ssh-keyscan' >&2
  exit 1
fi

# Go source formatting: gofmt lists nothing outside the vendor tree.
unformatted=$(find . -path ./vendor -prune -o -name '*.go' -type f -print | xargs gofmt -l)
[[ -z "$unformatted" ]] || { echo "gofmt would change:" >&2; echo "$unformatted" >&2; exit 1; }

go mod verify
# The host's shared places must be as they were after the tests and the
# suites (scripts/lib/host.sh).
. "$ROOT/scripts/lib/host.sh"
host_before=$(host_shared_entries)
go test -count=1 -mod=vendor ./...
go vet -mod=vendor ./...
go test -race -count=1 -mod=vendor ./...
BUILD_TIME=${BUILD_TIME:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}
COMMIT=${COMMIT:-$(git rev-parse --short=12 HEAD 2>/dev/null || printf source-build)}
make build COMMIT="$COMMIT" BUILD_TIME="$BUILD_TIME"

# The executable's module metadata names the pinned dependency. (Held in a
# variable: the verifier leaves no by-product in the tree, and grep -q on a
# pipe could end the writer early under pipefail.)
metadata=$(GOTOOLCHAIN=local go version -m bin/karvi-linux-amd64)
grep -Eq 'dep[[:space:]]+github.com/scrapli/scrapligo[[:space:]]+v1\.4\.2' <<<"$metadata"
./bin/karvi-linux-amd64 version --format json | grep -q '"version": "0.25.0"'
./bin/karvi-linux-amd64 version --format json | grep -q '"config_schema_version": 6'
./bin/karvi-linux-amd64 version --format json | grep -q '"command_record_schema_version": 2'
./bin/karvi-linux-amd64 version --format json | grep -q '"daemon_ipc_schema_version": 10'
./bin/karvi-linux-amd64 version --format json | grep -q '"job_schema_version": 2'
./bin/karvi-linux-amd64 version --format json | grep -q '"config_registry_schema_version": 23'
./bin/karvi-linux-amd64 version --format json | grep -q '"id": "system"'
./bin/karvi-linux-amd64 version --format json | grep -q '"id": "scrapligo-v1"'
./bin/karvi-linux-amd64 --version | grep -q '^  scrapligo: 1.4.2 (id=scrapligo-v1, linkage=compiled-in)$'
for command in login command cmd run daemon config watch version; do
  ./bin/karvi-linux-amd64 "$command" --help >/dev/null
done
# The suites, the one list of scripts/lib/suites.sh: the canary suite needs
# bin/secret-scan, so the tools build comes first; the parity suite builds
# the fake IOS XE device from the tree.
make tools-build
. "$ROOT/scripts/lib/suites.sh"
run_suites
host_shared_unchanged "$host_before"
make checksums
printf 'karvi native release verification passed\n'
