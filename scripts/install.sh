#!/bin/sh
# install.sh: install karvi from the release bundle into /usr/local, as root,
# the bundle's counterpart of the package (BUILD-HOWTO.md section 10;
# docs/EXAMPLES.md chapter 57, issue 19). Run it from the extracted bundle
# after scripts/verify-bundle.sh has passed:
#
#   sudo ./scripts/install.sh
#
# The four executables go under versioned names in /usr/local/lib/karvi:
# karvi, karvi-askpass, and karvi-prune linked from /usr/local/bin, and
# karvi-sysutil from /usr/local/sbin, as the package has it in /usr/sbin;
# karvi-sysutil's manual page in /usr/local/share/man/man8; the service
# account's template unit in /usr/local/lib/systemd/system; and systemd
# reloaded. It enables, starts, and restarts nothing: neither the package nor
# the bundle sets up a model, and a site that chooses the service account's
# daemon runs `sudo karvi-sysutil daemon install`, as under the package. An
# older bundle's install.sh rolls back: the links repointed, that release's
# template back, systemd reloaded; the administrator restarts the unit where
# one runs.
set -eu
ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
LIB=/usr/local/lib/karvi

# Colour on standard error at a terminal, as karvi's: errors bold red;
# NO_COLOR or --nocolor turns it off.
colour=yes
[ -n "${NO_COLOR:-}" ] && colour=no
die() {
  if [ "$colour" = yes ] && [ -t 2 ]; then
    printf '\033[1;31m%s\033[0m\n' "install.sh: $1" >&2
  else
    printf '%s\n' "install.sh: $1" >&2
  fi
  exit 1
}
usage() { echo "usage: sudo ./scripts/install.sh [--nocolor]" >&2; exit 2; }

for arg in "$@"; do
  case $arg in
    --nocolor) colour=no ;;
    -h | --help) echo "usage: sudo ./scripts/install.sh [--nocolor]"; exit 0 ;;
    *) usage ;;
  esac
done

[ "$(id -u)" -eq 0 ] || die "run as root: sudo ./scripts/install.sh"
VERSION=$(cat "$ROOT/VERSION")
for f in bin/karvi-linux-amd64 bin/karvi-askpass-linux-amd64 bin/karvi-prune-linux-amd64 \
  packaging/sbin/karvi-sysutil packaging/man/karvi-sysutil.8 packaging/systemd/system/karvi-daemon@.service; do
  [ -f "$ROOT/$f" ] || die "$ROOT/$f missing; run this from the extracted bundle"
done

# link TARGET LINK: LINK names TARGET, replaced in one rename so that it
# never names nothing.
link() {
  ln -sfn "$1" "$2.install.$$"
  mv -Tf "$2.install.$$" "$2"
  echo "$2 -> $1"
}

install -d -m 0755 "$LIB" /usr/local/bin /usr/local/sbin /usr/local/share/man/man8 /usr/local/lib/systemd/system
for name in karvi karvi-askpass karvi-prune; do
  install -m 0755 "$ROOT/bin/$name-linux-amd64" "$LIB/$name-v$VERSION"
  link "$LIB/$name-v$VERSION" "/usr/local/bin/$name"
done
install -m 0755 "$ROOT/packaging/sbin/karvi-sysutil" "$LIB/karvi-sysutil-v$VERSION"
link "$LIB/karvi-sysutil-v$VERSION" /usr/local/sbin/karvi-sysutil
install -m 0644 "$ROOT/packaging/man/karvi-sysutil.8" /usr/local/share/man/man8/karvi-sysutil.8
echo "/usr/local/share/man/man8/karvi-sysutil.8"
install -m 0644 "$ROOT/packaging/systemd/system/karvi-daemon@.service" /usr/local/lib/systemd/system/karvi-daemon@.service
echo "/usr/local/lib/systemd/system/karvi-daemon@.service"
# As the package's maintainer scripts: only where systemd runs.
if [ -d /run/systemd/system ]; then
  systemctl --system daemon-reload
  echo "systemd reloaded"
fi
echo "karvi $VERSION installed; nothing enabled or started"
