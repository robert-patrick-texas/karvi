#!/bin/sh
# Create a byte-reproducible source bundle rooted at karvi-v<VERSION>,
# independent of the name of the working directory and of the checkout's
# umask: every mode is set from the owner's execute bit alone (files 0644,
# executables and folders 0755), and the format is named. Git metadata is
# excluded. tar writes a file first and gzip reads it after, so a tar that
# fails (an unreadable file) stops the script with exit 1 and leaves no
# bundle.
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
NAME="karvi-v$(cat "$ROOT/VERSION")"
OUT=${1:-$(dirname "$ROOT")/$NAME-source-linux-amd64.tar.gz}
RELEASE_MTIME=${RELEASE_MTIME:-2026-09-25T00:00:00Z}
psb_tar=$(mktemp "${TMPDIR:-/tmp}/karvi-bundle.XXXXXX")
cleanup() { rm -f "${psb_tar:?}"; }
. "$ROOT/scripts/lib/exit.sh"; on_exit cleanup
rm -f -- "$OUT"
cd "$ROOT"
tar --format=gnu --sort=name --mtime="$RELEASE_MTIME" --owner=0 --group=0 --numeric-owner \
  --mode=a+rX,u+w,go-w --exclude=./.git --transform="s|^\\.|$NAME|S" \
  -cf "$psb_tar" . || { echo "package-source-bundle: tar failed; no bundle written" >&2; exit 1; }
gzip -n <"$psb_tar" >"$OUT"
sha256sum "$OUT"
