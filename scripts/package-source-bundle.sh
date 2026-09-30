#!/bin/sh
# Create a byte-reproducible source bundle rooted at karvi-v<VERSION>,
# independent of the name of the working directory. Git metadata is excluded.
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
NAME="karvi-v$(cat "$ROOT/VERSION")"
OUT=${1:-$(dirname "$ROOT")/$NAME-source-linux-amd64.tar.gz}
RELEASE_MTIME=${RELEASE_MTIME:-2026-09-25T00:00:00Z}
cd "$ROOT"
tar --sort=name --mtime="$RELEASE_MTIME" --owner=0 --group=0 --numeric-owner \
  --exclude=./.git --transform="s|^\\.|$NAME|S" \
  -cf - . | gzip -n > "$OUT"
sha256sum "$OUT"
