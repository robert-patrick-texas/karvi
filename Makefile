GO ?= go
VERSION ?= 0.28.0
COMMIT ?= development
BUILD_TIME ?= 1970-01-01T00:00:00Z
HTMLDIR ?= ../html
FIPS_MODE ?= false
GOOS ?= linux
GOARCH ?= amd64

MODULE := github.com/robert-patrick-texas/karvi
LDFLAGS := -s -w \
	-X $(MODULE)/internal/buildinfo.Commit=$(COMMIT) \
	-X $(MODULE)/internal/buildinfo.BuildTime=$(BUILD_TIME) \
	-X $(MODULE)/internal/buildinfo.FIPSMode=$(FIPS_MODE)

.PHONY: all fmt generate generated-clean deps vendor test vet build native-build checksums html deb smoke halt-smoke k03-smoke native-smoke clean

all: generate test vet build

# Fetch and authenticate the exact dependencies declared by go.mod. Run this
# only on an approved Internet-connected build host or through an internal Go
# module proxy.
deps:
	$(GO) mod download
	$(GO) mod verify

# Materialize an offline dependency tree for a release source bundle.
vendor: deps
	$(GO) mod vendor

fmt:
	find . -path './vendor' -prune -o -name '*.go' -type f -print0 | xargs -0 gofmt -w

generate:
	$(GO) run ./tools/configgen
	$(GO) run ./tools/errorcodegen
	$(GO) run ./tools/mangen

# Every comparison counts: set -e, since the recipe is one shell line and
# its exit would otherwise be the last command's alone.
generated-clean:
	@set -e; tmp=$$(mktemp -d); trap 'rm -rf "$$tmp"' EXIT; \
	$(GO) run ./tools/configgen -schema "$$tmp/config-schema.json" -reference "$$tmp/reference.toml"; \
	$(GO) run ./tools/errorcodegen -output "$$tmp/ERROR-CODES.md"; \
	$(GO) run ./tools/mangen -dir "$$tmp/man"; \
	cmp schema/config-schema.json "$$tmp/config-schema.json"; \
	cmp configs/reference.toml "$$tmp/reference.toml"; \
	cmp docs/ERROR-CODES.md "$$tmp/ERROR-CODES.md"; \
	for f in "$$tmp"/man/*; do cmp "packaging/man/$${f##*/}" "$$f"; done

# The documentation as HTML (tools/md-to-html): every Markdown file of the
# tree, an index, and images/, in HTMLDIR beside the tree, replaced whole.
# The footers name the checkout's commit, or COMMIT outside a git checkout.
html:
	$(GO) run ./tools/md-to-html -src . -out "$(HTMLDIR)" -commit "$$(git describe --always --dirty 2>/dev/null || echo '$(COMMIT)')"

# The Debian package of the tree as it stands, bin/'s executables as they
# are, into dist/ (scripts/build-deb.sh).
deb:
	./scripts/build-deb.sh

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

build:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build -buildvcs=false -trimpath -ldflags '$(LDFLAGS)' -o bin/karvi-$(GOOS)-$(GOARCH) ./cmd/karvi
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build -buildvcs=false -trimpath -ldflags '$(LDFLAGS)' -o bin/karvi-askpass-$(GOOS)-$(GOARCH) ./cmd/karvi-askpass
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build -buildvcs=false -trimpath -ldflags '$(LDFLAGS)' -o bin/karvi-prune-$(GOOS)-$(GOARCH) ./cmd/karvi-prune
	ln -sfn karvi-$(GOOS)-$(GOARCH) bin/karvi
	ln -sfn karvi-askpass-$(GOOS)-$(GOARCH) bin/karvi-askpass
	ln -sfn karvi-prune-$(GOOS)-$(GOARCH) bin/karvi-prune

native-build: deps generate test vet build

checksums:
	@test -x bin/karvi-$(GOOS)-$(GOARCH)
	@test -x bin/karvi-askpass-$(GOOS)-$(GOARCH)
	@test -x bin/karvi-prune-$(GOOS)-$(GOARCH)
	sha256sum bin/karvi-$(GOOS)-$(GOARCH) \
		bin/karvi-askpass-$(GOOS)-$(GOARCH) \
		bin/karvi-prune-$(GOOS)-$(GOARCH) > CHECKSUMS.sha256

# The smoke suites run against the executable make build produces, which
# carries both transports (the scrapligo_v1 build tag and the dependency-free
# preview module were removed after v0.12.0).
smoke: build
	./scripts/lib/json-test.sh
	./scripts/smoke-test.sh

halt-smoke: build
	./scripts/halt-smoke-test.sh

k03-smoke: build
	./scripts/k03-smoke-test.sh

v0100-smoke: build
	./scripts/v0100-smoke-test.sh

# The parity suite needs the native executable.
native-smoke: build
	GO=$(GO) ./scripts/native-smoke-test.sh

tools-build:
	mkdir -p bin
	CGO_ENABLED=0 GOTOOLCHAIN=local $(GO) build -buildvcs=false -trimpath -o bin/secret-scan ./tools/secret-scan

canary-smoke: build tools-build
	./scripts/canary-smoke-test.sh

clean:
	rm -f bin/karvi bin/karvi-askpass bin/karvi-prune
	rm -f bin/karvi-$(GOOS)-$(GOARCH) bin/karvi-askpass-$(GOOS)-$(GOARCH) bin/karvi-prune-$(GOOS)-$(GOARCH)
