#!/bin/sh
# check-deb.sh PACKAGE TREE: the Debian package `make deb` built from TREE
# (a checkout or an extracted release bundle), checked against it
# (docs/EXAMPLES.md chapter 43). The release verifier runs it on the package
# of the tree it verifies, and the release's artifact step on the package of
# the bundle:
#   - the three executables are TREE's bin/ bytes;
#   - every document and example configuration packaging/debian/rules
#     installs is in the package under its own name, the same bytes, so none
#     was gzipped or left out;
#   - every unit's Documentation=file: target is in the package;
#   - configs/reference.toml under the documents reaches the one copy under
#     /usr/share/karvi;
#   - lintian reports no error or warning that the package does not
#     override (packaging/debian/karvi.lintian-overrides).
# POSIX sh: no local; the functions' variables carry the cd_ prefix.
set -eu

[ $# -eq 2 ] || { echo "usage: check-deb.sh PACKAGE TREE" >&2; exit 2; }
PKG=$1
TREE=$(CDPATH='' cd -- "$2" && pwd)
command -v dpkg-deb >/dev/null 2>&1 || { echo "check-deb: dpkg-deb not found (the dpkg-dev package)" >&2; exit 2; }
command -v lintian >/dev/null 2>&1 || { echo "check-deb: lintian not found (the lintian package)" >&2; exit 2; }

cd_failed=0
fail() { echo "check-deb: $*" >&2; cd_failed=1; }

cd_work=$(mktemp -d "${TMPDIR:-/tmp}/karvi-check-deb-XXXXXX")
trap 'rm -rf "${cd_work:?}"' EXIT HUP INT TERM
R=$cd_work/root
dpkg-deb -x "$PKG" "$R"
DOC=$R/usr/share/doc/karvi

for cd_exe in karvi karvi-askpass karvi-prune; do
  cmp -s "$TREE/bin/$cd_exe-linux-amd64" "$R/usr/bin/$cd_exe" ||
    fail "usr/bin/$cd_exe is not $TREE/bin/$cd_exe-linux-amd64"
done

# same_file SOURCE INSTALLED: the tree's file installed as itself.
same_file() {
  cmp -s "$1" "$2" || fail "${2#"$R"/} is missing or differs from ${1#"$TREE"/}"
}
for cd_f in "$TREE"/*.md; do same_file "$cd_f" "$DOC/${cd_f##*/}"; done
for cd_f in "$TREE"/docs/*.md; do same_file "$cd_f" "$DOC/docs/${cd_f##*/}"; done
for cd_f in "$TREE"/release/*.md; do same_file "$cd_f" "$DOC/release/${cd_f##*/}"; done
for cd_f in "$TREE"/examples/*; do same_file "$cd_f" "$DOC/examples/${cd_f##*/}"; done
same_file "$TREE/configs/development.toml" "$DOC/configs/development.toml"

cd_docs=$(grep -rh '^Documentation=file:' "$R/usr/share/karvi/systemd" | sed 's/^Documentation=file://' | tr ' ' '\n' | sort -u)
[ -n "$cd_docs" ] || fail "no unit under usr/share/karvi/systemd names a Documentation=file: target"
for cd_d in $cd_docs; do
  [ -f "$R$cd_d" ] || fail "a unit's Documentation=file:$cd_d is not in the package"
done

[ "$(readlink "$DOC/configs/reference.toml" 2>/dev/null)" = ../../../karvi/reference.toml ] ||
  fail "usr/share/doc/karvi/configs/reference.toml is not the link to ../../../karvi/reference.toml"
same_file "$TREE/configs/reference.toml" "$R/usr/share/karvi/reference.toml"

lintian --fail-on error,warning "$PKG" >"$cd_work/lintian.txt" 2>&1 ||
  fail "lintian: $(cat "$cd_work/lintian.txt")"

[ "$cd_failed" -eq 0 ] || exit 1
echo "check-deb: ${PKG##*/} matches ${TREE}: executables, documents, units' documentation, reference link, lintian"
