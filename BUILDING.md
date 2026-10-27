# Building karvi

See [`BUILD-HOWTO.md`](BUILD-HOWTO.md) for Ubuntu host preparation, offline
builds, installation, and rollback.

## The release build

`go.mod`, `go.sum`, and `vendor/` are committed, so the build needs Go 1.27 or
later and no network:

```bash
./scripts/verify-shipped.sh     # the shipped executables as bytes, in seconds
./scripts/verify-bundle.sh      # the shipped executables and source, not rebuilt
make build COMMIT=source-release-v$(cat VERSION)
sha256sum -c CHECKSUMS.sha256   # the rebuild reproduces the shipped bytes
./scripts/verify-release.sh     # build from source, race run, every suite
```

`make deps` (download and verify) is optional, on a connected host or
through an internal module proxy. No build tag is involved, and every build
carries the native transport. Its text version output MUST include:

```text
  native: compiled-in
```

The suites run in three lanes at once (`scripts/lib/suites.sh`);
`KARVI_SUITE_LANES=1` runs them one after
another with the output live.

A build is stamped with its version and its commit, and no build time: `make
build` takes `COMMIT` from the command line or the environment, else `git
describe --always --dirty` in a git checkout's root, else `development`.
`scripts/verify-release.sh` rebuilds `bin/` with that commit and rewrites
`CHECKSUMS.sha256`, so pass `COMMIT=source-release-v$(cat VERSION)` to keep
the shipped bytes. It also tests the tree's package as root in an Ubuntu
container, which needs Docker.

A compiled adapter is not device qualification. Complete the Cisco IOS XE
qualification plan
([`docs/CISCO-IOSXE-QUALIFICATION.md`](docs/CISCO-IOSXE-QUALIFICATION.md))
before relying on the native transport in production.
