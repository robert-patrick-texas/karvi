GO ?= go
# The commit a build and the HTML footers name: COMMIT when given, on the
# command line or in the environment (a release passes
# source-release-vVERSION); else the checkout's own, git describe --always
# --dirty, when .git is in the tree's root; else development. Taken once.
ifeq ($(origin COMMIT),undefined)
COMMIT := $(shell if [ -e .git ]; then git describe --always --dirty 2>/dev/null || echo development; else echo development; fi)
endif
HTMLDIR ?= ../html
GOOS ?= linux
GOARCH ?= amd64

MODULE := github.com/robert-patrick-texas/karvi
LDFLAGS := -s -w \
	-X $(MODULE)/internal/buildinfo.Commit=$(COMMIT)

.PHONY: all fmt generate generated-clean deps vendor test vet build checksums html deb tools-build clean

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
# its exit would otherwise be the last command's alone. The scratch folder
# goes at any end, a Ctrl-C included (scripts/lib/exit.sh).
generated-clean:
	@set -e; . ./scripts/lib/exit.sh; tmp=$$(mktemp -d); on_exit 'rm -rf "$$tmp"'; \
	$(GO) run ./tools/configgen -schema "$$tmp/config-schema.json" -reference "$$tmp/reference.toml"; \
	$(GO) run ./tools/errorcodegen -output "$$tmp/ERROR-CODES.md"; \
	$(GO) run ./tools/mangen -dir "$$tmp/man"; \
	cmp schema/config-schema.json "$$tmp/config-schema.json"; \
	cmp configs/reference.toml "$$tmp/reference.toml"; \
	cmp docs/ERROR-CODES.md "$$tmp/ERROR-CODES.md"; \
	for f in "$$tmp"/man/*; do cmp "packaging/man/$${f##*/}" "$$f"; done

# The documentation as HTML (tools/md-to-html): every Markdown file of the
# tree, an index, and images/, in HTMLDIR beside the tree, replaced whole.
# The footers name COMMIT, as a build does.
html:
	$(GO) run ./tools/md-to-html -src . -out "$(HTMLDIR)" -commit "$(COMMIT)"

# The Debian package of the tree as it stands, bin/'s executables as they
# are, into dist/ (scripts/build-deb.sh).
deb:
	./scripts/build-deb.sh

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

# build, and all through it, alone writes bin/'s executables: no suite is a
# target. A suite runs by its shebang against bin/, or the executable KARVI
# names, and scripts/lib/suites.sh's run_suites runs them all.
build:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build -buildvcs=false -trimpath -ldflags '$(LDFLAGS)' -o bin/karvi-$(GOOS)-$(GOARCH) ./cmd/karvi
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build -buildvcs=false -trimpath -ldflags '$(LDFLAGS)' -o bin/karvi-askpass-$(GOOS)-$(GOARCH) ./cmd/karvi-askpass
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build -buildvcs=false -trimpath -ldflags '$(LDFLAGS)' -o bin/karvi-prune-$(GOOS)-$(GOARCH) ./cmd/karvi-prune
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build -buildvcs=false -trimpath -ldflags '$(LDFLAGS)' -o bin/karvi-audit-$(GOOS)-$(GOARCH) ./cmd/karvi-audit
	ln -sfn karvi-$(GOOS)-$(GOARCH) bin/karvi
	ln -sfn karvi-askpass-$(GOOS)-$(GOARCH) bin/karvi-askpass
	ln -sfn karvi-prune-$(GOOS)-$(GOARCH) bin/karvi-prune
	ln -sfn karvi-audit-$(GOOS)-$(GOARCH) bin/karvi-audit

checksums:
	@test -x bin/karvi-$(GOOS)-$(GOARCH)
	@test -x bin/karvi-askpass-$(GOOS)-$(GOARCH)
	@test -x bin/karvi-prune-$(GOOS)-$(GOARCH)
	@test -x bin/karvi-audit-$(GOOS)-$(GOARCH)
	sha256sum bin/karvi-$(GOOS)-$(GOARCH) \
		bin/karvi-askpass-$(GOOS)-$(GOARCH) \
		bin/karvi-prune-$(GOOS)-$(GOARCH) \
		bin/karvi-audit-$(GOOS)-$(GOARCH) > CHECKSUMS.sha256

# The scan the canary suite and device-qualification.sh run,
# bin/secret-scan, which CHECKSUMS.sha256 does not list.
tools-build:
	mkdir -p bin
	CGO_ENABLED=0 GOTOOLCHAIN=local $(GO) build -buildvcs=false -trimpath -o bin/secret-scan ./tools/secret-scan

clean:
	rm -f bin/karvi bin/karvi-askpass bin/karvi-prune bin/karvi-audit
	rm -f bin/karvi-$(GOOS)-$(GOARCH) bin/karvi-askpass-$(GOOS)-$(GOARCH) bin/karvi-prune-$(GOOS)-$(GOARCH) bin/karvi-audit-$(GOOS)-$(GOARCH)
