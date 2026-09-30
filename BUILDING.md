# Building karvi

See `BUILD-HOWTO.md` for Ubuntu host preparation, offline builds, installation, and rollback.

## The release build

The release ships the scrapligo-v1 executable. The source pins
`github.com/scrapli/scrapligo v1.4.2`, and `go.mod`, `go.sum`, and `vendor/`
are committed, so the build needs Go 1.26 or later and no network:

```bash
./scripts/verify-shipped.sh     # the shipped executables as bytes, in seconds
./scripts/verify-bundle.sh      # the shipped executables and source, not rebuilt
make build COMMIT=source-release-v$(cat VERSION) BUILD_TIME=2026-09-30T00:00:00Z
sha256sum -c CHECKSUMS.sha256   # the rebuild reproduces the shipped bytes
./scripts/verify-release.sh     # build from source, race run, every suite
```

`make deps` (download and verify) is optional, on a connected host or
through an internal module proxy. No build tag is involved: the
`scrapligo_v1` tag and the dependency-free preview module were removed, and
every build carries the adapter. Its text version output MUST include:

```text
scrapligo: 1.4.2 (id=scrapligo-v1, linkage=compiled-in)
```

The suites run in three lanes at once (`scripts/lib/suites.sh`);
`KARVI_SUITE_LANES=1` runs them one after
another with the output live.

`scripts/verify-release.sh` rebuilds `bin/` with the identity in `COMMIT`
and `BUILD_TIME` (a development identity if they are unset) and rewrites
`CHECKSUMS.sha256`; pass the release's values to keep the shipped bytes.

A compiled adapter is not device qualification. Complete the Cisco IOS XE
qualification plan (`docs/CISCO-IOSXE-QUALIFICATION.md`) before relying on
the native transport in production.
