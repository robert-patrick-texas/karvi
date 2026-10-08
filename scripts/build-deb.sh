#!/bin/sh
# make deb: the Debian package of the tree as it stands, a git checkout or an
# extracted release bundle (docs/EXAMPLES.md chapters 39 and 43). The package
# carries bin/'s executables as they are: under a release's CHANGELOG heading
# ("## VERSION - DATE") they must be the ones CHECKSUMS.sha256 lists; under
# "## Unreleased" the package is VERSION+dev, and karvi's own identity (its
# version's commit) must not be the release's, source-release-vVERSION. The tree is
# copied to a fresh directory, packaging/debian becomes debian/ there with a
# changelog written for this build, and dpkg-buildpackage runs in the copy,
# so neither the tree nor its parent gains a file but the package in DIST
# (dist/ by default).
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
DIST=${DIST:-$ROOT/dist}

die() { echo "build-deb: $*" >&2; exit 1; }

command -v dpkg-buildpackage >/dev/null 2>&1 || die "dpkg-buildpackage not found (the dpkg-dev and debhelper packages)"
bd_version=$(cat "$ROOT/VERSION")
bd_heading=$(sed -n '/^## /{p;q;}' "$ROOT/CHANGELOG.md")
case $bd_heading in
  "## Unreleased")
    bd_package=$bd_version+dev
    bd_when=$(LC_ALL=C date -u '+%a, %d %b %Y %H:%M:%S +0000')
    bd_release=no
    ;;
  "## $bd_version - "????-??-??)
    bd_package=$bd_version
    bd_when=$(LC_ALL=C date -u -d "${bd_heading#"## $bd_version - "}" '+%a, %d %b %Y %H:%M:%S +0000') ||
      die "CHANGELOG.md's heading \"$bd_heading\" holds no date"
    bd_release=yes
    ;;
  *) die "CHANGELOG.md's first heading is \"$bd_heading\"; a package needs \"## $bd_version - YYYY-MM-DD\" or \"## Unreleased\"" ;;
esac

for bd_exe in karvi karvi-askpass karvi-prune; do
  [ -f "$ROOT/bin/$bd_exe-linux-amd64" ] || die "bin/$bd_exe-linux-amd64 is missing; build the executables first: make build"
done
if [ "$bd_release" = yes ]; then
  (cd "$ROOT" && sha256sum --check --status CHECKSUMS.sha256 2>/dev/null) ||
    die "bin/ does not hold the executables CHECKSUMS.sha256 lists; the package of $bd_version carries the release's qualified build"
else
  # A dev build may match CHECKSUMS.sha256 (make checksums after make build);
  # what marks the release's executables is their identity.
  . "$ROOT/scripts/lib/json.sh"
  bd_commit=$("$ROOT/bin/karvi-linux-amd64" version --format json | json_get - commit) ||
    die "bin/karvi-linux-amd64 version --format json could not be read"
  [ "$bd_commit" != "source-release-v$bd_version" ] ||
    die "bin/ holds the released $bd_version executables (commit $bd_commit); build the dev line first: make build"
fi

bd_maintainer=$(sed -n 's/^Maintainer: //p' "$ROOT/packaging/debian/control")
bd_work=$(mktemp -d "${TMPDIR:-/tmp}/karvi-deb.XXXXXX")
trap 'rm -rf "${bd_work:?}"' EXIT HUP INT TERM
bd_src=$bd_work/karvi-$bd_package
mkdir "$bd_src"
(cd "$ROOT" && tar --exclude=./.git --exclude=./dist -cf - .) | (cd "$bd_src" && tar -xf -)
mv "$bd_src/packaging/debian" "$bd_src/debian"
cat >"$bd_src/debian/changelog" <<EOF
karvi ($bd_package) unstable; urgency=medium

  * See /usr/share/doc/karvi/CHANGELOG.md.

 -- $bd_maintainer  $bd_when
EOF
(cd "$bd_src" && dpkg-buildpackage -us -uc -b) >"$bd_work/build.log" 2>&1 || {
  cat "$bd_work/build.log" >&2
  die "dpkg-buildpackage failed"
}
mkdir -p "$DIST"
cp "$bd_work/karvi_${bd_package}_amd64.deb" "$DIST/"
echo "$DIST/karvi_${bd_package}_amd64.deb"
