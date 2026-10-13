#!/bin/bash
# sysutil-smoke-test.sh PACKAGE: karvi-sysutil, the package's template unit
# and maintainer scripts, and the service account's daemon, tested as root
# in an Ubuntu 26.04 container under Docker, never on this host
# (docs/EXAMPLES.md chapter 57, issue 16). The container boots systemd as its
# first process without --privileged (one capability, AppArmor unconfined,
# its cgroup remounted writable), with logind, and no network: the job runs
# on karvi's fake device and the operator logs in through the container's
# sshd, both on its 127.0.0.1. Docker stops it as a host shuts down, with
# systemd's halt signal. The steps:
#   1. the refusals, each exit 1 with the accounts and karvi's places as
#      before: not root, no template, the group missing, a karvi account not
#      a system user, a configuration the record does not list;
#   2. the install, each part created, daemon.launch systemd for the account,
#      the unit's stop settings and order, the daemon running, and daemon
#      stop refused daemon_managed;
#   3. the install again, each part exists and nothing changed;
#   4. the repair: the membership, the modes, and the instance broken, then
#      the account removed, each part repaired or created, the uid the same;
#   5. a --group other than the record's, refused;
#   6. the shutdown: a 12-second job, docker stop 3 s into it, and after
#      docker start the job's summary completed, its folder's modes FILES';
#   7. logind's RemoveIPC: an operator's login and logout leave the account's
#      files under /dev/shm;
#   8. the upgrade: the installed template changed and the package installed
#      again, the instance needing no reload and its daemon's pid the same;
#   9. the removal: the account and its home kept, a part gone reported
#      absent, a second removal, an install taking the account back, --purge
#      giving the account's shared files to root, their modes the same, and a
#      removal without a record refused;
#  10. dpkg -r: the template gone and systemd reloaded;
#  11. the bundle on the host without the package (chapter 57, issue 19):
#      scripts/install.sh with the package's executables, the links, the
#      page, and the template placed and systemd reloaded; the install run by
#      karvi-sysutil from /usr/local/sbin, the daemon running the bundle's
#      karvi; install.sh again as an upgrade, the reload done and the
#      daemon's pid the same; and the removal with --purge.
# The container and its image are removed at the end; nothing else of
# Docker's is touched. Needs Docker, the network to build the image, and Go
# to build the fake device from the tree.
set -u -o pipefail

[ $# -eq 1 ] && [ -f "$1" ] || { echo "usage: sysutil-smoke-test.sh PACKAGE" >&2; exit 2; }
ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
PKG=$(realpath -- "$1")
. "$ROOT/scripts/lib/json.sh"
command -v docker >/dev/null 2>&1 || { echo "sysutil-smoke: docker not found" >&2; exit 2; }

TAG=karvi-sysutil-test-$$
TMP=$(mktemp -d "${TMPDIR:-/tmp}/karvi-sysutil-test.XXXXXX")
cleanup() {
  docker rm -f "$TAG" >/dev/null 2>&1
  docker rmi -f "$TAG" >/dev/null 2>&1
  rm -rf "${TMP:?}"
}
trap cleanup EXIT HUP INT TERM

UNIT=karvi-daemon@karvi.service
HOME_DIR=/opt/karvi/users/karvi
CONFIG=$HOME_DIR/.config/karvi/config.toml
RECORD=/etc/karvi/service-karvi.installed
TEMPLATE=/usr/lib/systemd/system/karvi-daemon@.service

fail() { echo "sysutil-smoke: FAIL: $*" >&2; exit 1; }
ok() { echo "sysutil-smoke: $*"; }
x() { docker exec -w / "$TAG" "$@"; }
xsh() { docker exec -w / "$TAG" bash -c "$1"; }
# as_karvi CMD...: as the account, as its unit's daemon runs.
as_karvi() { docker exec -w / "$TAG" runuser -u karvi -- "$@"; }
# has TEXT LINE: TEXT holds LINE at a line's start.
has() { [[ $'\n'$1 == *$'\n'"$2"* ]]; }
# snap: the accounts, karvi's places, and the instance's state, digested.
snap() {
  xsh 'cat /etc/passwd /etc/group /etc/shadow /etc/gshadow
    find /opt/karvi /etc/karvi -printf "%p %u %g %m\n" 2>/dev/null | sort
    systemctl is-enabled karvi-daemon@karvi.service 2>/dev/null' | sha256sum
}
# refused NAME WANT CMD...: CMD exits 1 saying WANT, and snap is unchanged.
refused() {
  local name=$1 want=$2 before after out rc
  shift 2
  before=$(snap)
  out=$("$@" 2>&1)
  rc=$?
  after=$(snap)
  [ "$rc" -eq 1 ] || fail "$name: exit $rc, want 1: $out"
  [[ $out == *"$want"* ]] || fail "$name: the refusal lacks \"$want\": $out"
  [ "$before" = "$after" ] || fail "$name: the host changed"
  ok "$name: refused, nothing changed"
}
sysutil() { x karvi-sysutil --nocolor daemon "$@"; }
record() { xsh "grep -v '^#' $RECORD"; }
prop() { x systemctl show -p "$1" --value "$UNIT"; }
# booted: systemd's start-up finished, its state on standard output; its
# socket first, which systemctl needs.
booted() {
  for _ in $(seq 1 100); do x test -S /run/systemd/private 2>/dev/null && break; sleep 0.1; done
  x timeout 60 systemctl is-system-running --wait
}
running() { # wait for systemd's start-up and the daemon's answer
  booted >/dev/null
  for _ in $(seq 1 50); do as_karvi karvi daemon status >/dev/null 2>&1 && return 0; sleep 0.2; done
  fail "the daemon does not answer after the boot"
}
boot() {
  docker run -d -t --name "$TAG" --hostname karvi-test --cgroupns=private \
    --cap-add SYS_ADMIN --security-opt apparmor=unconfined \
    --tmpfs /run --tmpfs /run/lock --shm-size=256m --stop-timeout 120 \
    --network none --entrypoint /bin/sh "$TAG" \
    -c 'mount -o remount,rw /sys/fs/cgroup && exec /sbin/init' >/dev/null || fail "the container did not start"
  [ "$(booted)" = running ] || fail "systemd is not running in the container"
}

# The image, the fake device, and the container.
docker build -q -t "$TAG" - >/dev/null <<'EOF' || fail "the image did not build"
FROM ubuntu:26.04
RUN apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
    systemd systemd-sysv dbus libpam-systemd adduser sudo \
    openssh-server openssh-client ca-certificates \
 && apt-get clean && rm -rf /var/lib/apt/lists/*
STOPSIGNAL SIGRTMIN+3
EOF
(cd "$ROOT" && CGO_ENABLED=0 GOTOOLCHAIN=local go build -mod=vendor -trimpath -o "$TMP/karvi-fake-device" ./cmd/karvi-fake-device) ||
  fail "the fake device did not build"
boot
docker cp "$PKG" "$TAG:/root/karvi.deb" >/dev/null && docker cp "$TMP/karvi-fake-device" "$TAG:/usr/local/bin/" >/dev/null ||
  fail "copy into the container"
x dpkg -i /root/karvi.deb >/dev/null || fail "dpkg -i ${PKG##*/}"
ok "$(xsh '. /etc/os-release; echo "$PRETTY_NAME"'), $(x systemctl --version | sed -n '1p'), ${PKG##*/} installed"

# 1. The refusals.
refused "not root" "run as root" docker exec -u nobody -w / "$TAG" karvi-sysutil daemon install
xsh "mv $TEMPLATE /root/"
refused "no template" "karvi-daemon@.service not installed; install the karvi package, or the bundle with scripts/install.sh" sysutil install
xsh "mv /root/karvi-daemon@.service $TEMPLATE"
refused "the group missing" "group netops does not exist" sysutil install
x groupadd netops
x useradd -u 1500 -M -s /bin/bash karvi
refused "an ordinary karvi account" "account karvi exists and is not a system user (uid 1500)" sysutil install
x userdel karvi
xsh "install -d $HOME_DIR/.config/karvi && echo '# the site' >$CONFIG"
refused "a configuration not the install's" "$CONFIG exists and is not the install's" sysutil install
xsh 'rm -rf /opt/karvi'

# 2. The install.
out=$(sysutil install 2>&1) || fail "install: $out"
for line in "created  /opt/karvi/shared" "created  account karvi  uid " "created  home $HOME_DIR  mode 0750" \
  "created  membership karvi in netops" "created  config $CONFIG  mode 0600" \
  "created  instance $UNIT  enabled, active" "running  daemon pid "; do
  has "$out" "$line" || fail "install: no \"$line\" in: $out"
done
KUID=$(x id -u karvi)
[ "$KUID" -lt 1000 ] && [ "$(xsh 'getent passwd karvi | cut -d: -f6,7')" = "$HOME_DIR:/usr/sbin/nologin" ] ||
  fail "the account: uid $KUID, $(xsh 'getent passwd karvi')"
[ "$(x stat -c '%U:%G %a' "$HOME_DIR")" = "karvi:karvi 750" ] && [ "$(x stat -c '%U:%G %a' "$CONFIG")" = "karvi:karvi 600" ] ||
  fail "the home's or the configuration's owner and mode"
want=$(printf '%s\n' "dir /etc/karvi made" "group netops found" "account karvi made $KUID" "home $HOME_DIR made" \
  "membership netops made" "config $CONFIG made" "instance $UNIT made")
[ "$(record)" = "$want" ] || fail "the record: $(record)"
[ "$(as_karvi karvi config show daemon.launch | sed -n 's/^value: *//p')" = '"systemd"' ] || fail "daemon.launch for the account"
[ "$(prop KillMode)" = mixed ] && [ "$(prop TimeoutStopUSec)" = infinity ] && [ "$(prop User)" = karvi ] ||
  fail "the unit's settings: $(x systemctl show -p KillMode -p TimeoutStopUSec -p User "$UNIT")"
[[ " $(prop Wants) " == *" network-online.target "* ]] && [[ " $(prop After) " == *" network-online.target "* ]] ||
  fail "the unit's order: Wants=$(prop Wants) After=$(prop After)"
stop=$(as_karvi karvi daemon stop 2>&1)
[ $? -eq 112 ] && [ "$stop" = "daemon_managed: daemon.launch is systemd; systemctl stop $UNIT" ] || fail "daemon stop as the account: $stop"
ok "install: each part created, the account uid $KUID, launch systemd, the unit mixed and infinity after the network, daemon stop refused"

# 3. The install again.
before=$(snap) && record_before=$(record) && pid=$(prop MainPID)
out=$(sysutil install 2>&1) || fail "install again: $out"
while IFS= read -r line; do
  case $line in exists\ *|running\ *|"  "*) ;; *) fail "install again: \"$line\" in: $out" ;; esac
done <<<"$out"
[ "$(snap)" = "$before" ] && [ "$(record)" = "$record_before" ] && [ "$(prop MainPID)" = "$pid" ] ||
  fail "install again changed the host"
ok "install again: each part exists, nothing changed"

# 4. The repair.
xsh "gpasswd -d karvi netops >/dev/null && chmod 0644 $CONFIG && chmod 0755 $HOME_DIR && systemctl disable --quiet --now $UNIT"
out=$(sysutil install 2>&1) || fail "repair: $out"
for line in "repaired home $HOME_DIR" "  found karvi:karvi 0755" "repaired membership karvi in netops" \
  "repaired config $CONFIG" "  found karvi:karvi 0644" "repaired instance $UNIT" "  found disabled, inactive"; do
  has "$out" "$line" || fail "repair: no \"$line\" in: $out"
done
xsh "systemctl disable --quiet --now $UNIT && userdel karvi"
out=$(sysutil install 2>&1) || fail "repair of the account: $out"
has "$out" "created  account karvi  uid $KUID" && has "$out" "repaired membership karvi in netops" && has "$out" "repaired instance $UNIT" ||
  fail "repair of the account: $out"
[ "$(x stat -c '%U:%G %a' "$HOME_DIR")" = "karvi:karvi 750" ] && [ "$(x stat -c '%U:%G %a' "$CONFIG")" = "karvi:karvi 600" ] ||
  fail "the home's or the configuration's owner after the account came back"
ok "repair: membership, modes, and instance set back; the account made again with uid $KUID"

# 5. Another group.
x groupadd ops2
refused "another group" $'the install\'s group is another; nothing changed; karvi-sysutil daemon remove first\n  installed group netops\n  given     group ops2' \
  sysutil install --group ops2

# 6. The shutdown: a 12-second job on the fake device, docker stop 3 s in.
docker exec -d "$TAG" karvi-fake-device -port 2223 -host-key-file /root/fake.key -password S3cret -enable en -slow 12s
xsh 'for _ in $(seq 1 50); do (exec 3<>/dev/tcp/127.0.0.1/2223) 2>/dev/null && exit 0; sleep 0.1; done; exit 1' || fail "the fake device"
xsh 'install -d -m 0755 /opt/karvi-test && printf "name,management_address,port,platform\nd1,127.0.0.1,2223,cisco_iosxe\n" >/opt/karvi-test/inv.csv
  printf "[[inventory-source]]\nname = \"lab\"\ntype = \"csv\"\npath = \"/opt/karvi-test/inv.csv\"\nrequired = true\n\n[inventory-source.mappings]\nname = [\"name\"]\nmanagement_address = [\"management_address\"]\nplatform = [\"platform\"]\nport = [\"port\"]\n" >/opt/karvi-test/c.toml
  chmod 0644 /opt/karvi-test/*'
job() { # COMMAND [--detach]: a job on d1 as the account
  docker exec -w / "$TAG" runuser -u karvi -- env NETUSER=netops NETPASS=S3cret NETENABLE=en \
    karvi --config /opt/karvi-test/c.toml --set 'platform-resolution.default=""' run --transport native --t d1 "${@:2}" "$1"
}
receipt=$(job 'show slow' --detach 2>&1) || fail "the job: $receipt"
art=$(sed -n 's/^artifact_dir: //p' <<<"$receipt")
sleep 3
t0=$(date +%s.%N)
docker stop -t 120 "$TAG" >/dev/null || fail "docker stop"
took=$(awk -v a="$t0" -v b="$(date +%s.%N)" 'BEGIN { printf "%.1f", b - a }')
docker start "$TAG" >/dev/null || fail "docker start"
running
docker cp "$TAG:$art/summary.json" "$TMP/summary.json" >/dev/null 2>&1 || fail "the job has no summary after the shutdown ($art)"
[ "$(json_get "$TMP/summary.json" final_status)" = completed ] && [ "$(json_get "$TMP/summary.json" primary_exit_code)" = 0 ] ||
  fail "the job after the shutdown: $(json_get "$TMP/summary.json" final_status) exit $(json_get "$TMP/summary.json" primary_exit_code)"
[ "$(x stat -c '%a %U %G' "$art")" = "2750 karvi netops" ] && [ "$(x stat -c '%a %U %G' "$art/summary.json")" = "640 karvi netops" ] ||
  fail "the job folder's modes: $(x stat -c '%n %a %U %G' "$art" "$art/summary.json")"
[ "$(prop ActiveState)" = active ] && x test -d /dev/shm/karvi || fail "after the boot: the instance $(prop ActiveState), /dev/shm/karvi"
ok "shutdown: docker stop took $took s, the job completed exit 0, its folder 2750 and files 640 in netops; the instance back at the boot"

# 7. RemoveIPC: an operator's login and logout leave the account's files.
docker exec -d "$TAG" karvi-fake-device -port 2223 -host-key-file /root/fake.key -password S3cret -enable en -slow 12s
xsh 'for _ in $(seq 1 50); do (exec 3<>/dev/tcp/127.0.0.1/2223) 2>/dev/null && exit 0; sleep 0.1; done; exit 1' || fail "the fake device again"
job 'show clock' >/dev/null 2>&1 || fail "a job before the login"
n=$(xsh "find /dev/shm/karvi -uid $KUID | wc -l")
[ "$n" -gt 0 ] || fail "the account has no file under /dev/shm"
xsh 'adduser --quiet --disabled-password --comment "" op1 >/dev/null 2>&1 && adduser --quiet op1 netops
  install -d -m 0700 /root/.ssh /home/op1/.ssh && ssh-keygen -q -t ed25519 -N "" -f /root/.ssh/id_ed25519
  cp /root/.ssh/id_ed25519.pub /home/op1/.ssh/authorized_keys && chown -R op1:op1 /home/op1/.ssh'
x ssh -q -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null op1@127.0.0.1 'touch /dev/shm/op1-file' || fail "the operator's login"
sleep 13
x test ! -e /dev/shm/op1-file || fail "logind kept the operator's file: RemoveIPC is not in force"
[ "$(xsh "find /dev/shm/karvi -uid $KUID | wc -l")" = "$n" ] || fail "the account's files under /dev/shm changed after the operator's logout"
ok "RemoveIPC: the operator's file gone 13 s after the logout, the account's $n kept"

# 8. The upgrade: the template changed, the package installed again.
pid=$(prop MainPID)
xsh "echo '# changed by hand' >>$TEMPLATE"
[ "$(prop NeedDaemonReload)" = yes ] || fail "the changed template needs no reload"
x dpkg -i /root/karvi.deb >/dev/null || fail "dpkg -i again"
[ "$(prop NeedDaemonReload)" = no ] && [ "$(prop MainPID)" = "$pid" ] ||
  fail "after the upgrade: NeedDaemonReload=$(prop NeedDaemonReload) MainPID=$(prop MainPID), was $pid"
ok "upgrade: the package's postinst reloaded systemd, the daemon pid $pid the same"

# 9. The removal.
x rm "$CONFIG"
out=$(sysutil remove 2>&1) || fail "remove: $out"
for line in "removed  instance $UNIT  stopped, disabled" "absent   config $CONFIG" "removed  membership karvi in netops" "kept     account karvi"; do
  has "$out" "$line" || fail "remove: no \"$line\" in: $out"
done
[ "$(record)" = "$(printf '%s\n' "dir /etc/karvi made" "account karvi made $KUID" "home $HOME_DIR made")" ] || fail "the record after remove: $(record)"
x getent passwd karvi >/dev/null && x test -d "$HOME_DIR" && [ "$(x systemctl is-enabled "$UNIT")" = disabled ] || fail "after remove"
out=$(sysutil remove 2>&1) || fail "remove again: $out"
[ "$out" = "kept     account karvi and its home $HOME_DIR; karvi-sysutil daemon remove --purge removes them" ] || fail "remove again: $out"
out=$(sysutil install 2>&1) || fail "install after remove: $out"
has "$out" "exists   account karvi  uid $KUID" && has "$out" "created  config $CONFIG  mode 0600" && has "$out" "created  instance $UNIT  enabled, active" ||
  fail "install after remove: $out"
running
xsh "find /opt/karvi/shared /dev/shm/karvi -uid $KUID -printf '%p %m\n' | sort" >"$TMP/owned"
[ -s "$TMP/owned" ] || fail "the account owns nothing in the shared trees"
out=$(sysutil remove --purge 2>&1) || fail "remove --purge: $out"
has "$out" "removed  account karvi" && has "$out" "removed  home $HOME_DIR" && has "$out" "removed  record $RECORD" || fail "remove --purge: $out"
[ -z "$(xsh "find /opt/karvi/shared /dev/shm/karvi -uid $KUID")" ] || fail "files of uid $KUID remain"
while read -r path mode; do
  [ "$(x stat -c '%a %U' "$path")" = "$mode root" ] || fail "$path: $(x stat -c '%a %U' "$path"), want $mode root"
done <"$TMP/owned"
! x getent passwd karvi >/dev/null && x test ! -e "$HOME_DIR" && x test ! -e /etc/karvi || fail "after --purge"
refused "remove without a record" "no service installed: $RECORD absent" sysutil remove
ok "remove: the account kept, a gone part absent, taken back by install; --purge gave $(wc -l <"$TMP/owned") files to root, modes the same"

# 10. dpkg -r: the template gone and systemd reloaded.
mark=$(x date +%s)
x dpkg -r karvi >/dev/null || fail "dpkg -r"
[ -z "$(x systemctl list-unit-files --no-legend karvi-daemon@.service)" ] || fail "the template is still known"
[[ $(x journalctl -b _PID=1 --since "@$mark" -o cat) == *Reloading* ]] || fail "systemd did not reload at dpkg -r"
ok "dpkg -r: the template gone, systemd reloaded"

# 11. The bundle: install.sh with the package's executables, on the host
# without the package.
VERSION=$(cat "$ROOT/VERSION")
B=$TMP/bundle
mkdir -p "$B/bin" "$B/scripts" "$B/packaging/sbin" "$B/packaging/man" "$B/packaging/systemd/system" "$TMP/pkg"
dpkg-deb -x "$PKG" "$TMP/pkg" || fail "dpkg-deb -x ${PKG##*/}"
for name in karvi karvi-askpass karvi-prune; do cp "$TMP/pkg/usr/bin/$name" "$B/bin/$name-linux-amd64"; done
cp "$ROOT/VERSION" "$B/" && cp "$ROOT/scripts/install.sh" "$B/scripts/" && cp "$ROOT/packaging/sbin/karvi-sysutil" "$B/packaging/sbin/" &&
  cp "$ROOT/packaging/man/karvi-sysutil.8" "$B/packaging/man/" && cp "$ROOT/packaging/systemd/system/karvi-daemon@.service" "$B/packaging/systemd/system/" ||
  fail "the bundle's files"
docker cp "$B" "$TAG:/root/bundle" >/dev/null || fail "copy the bundle into the container"
mark=$(x date +%s)
out=$(x /root/bundle/scripts/install.sh --nocolor 2>&1) || fail "install.sh: $out"
LIB=/usr/local/lib/karvi
for line in "/usr/local/bin/karvi -> $LIB/karvi-v$VERSION" "/usr/local/bin/karvi-askpass -> $LIB/karvi-askpass-v$VERSION" \
  "/usr/local/bin/karvi-prune -> $LIB/karvi-prune-v$VERSION" "/usr/local/sbin/karvi-sysutil -> $LIB/karvi-sysutil-v$VERSION" \
  "/usr/local/share/man/man8/karvi-sysutil.8" "/usr/local/lib/systemd/system/karvi-daemon@.service" "systemd reloaded" \
  "karvi $VERSION installed; nothing enabled or started"; do
  has "$out" "$line" || fail "install.sh: no \"$line\" in: $out"
done
[ "$(x readlink /usr/local/sbin/karvi-sysutil)" = "$LIB/karvi-sysutil-v$VERSION" ] && x test ! -e /usr/bin/karvi &&
  [ -n "$(x systemctl list-unit-files --no-legend karvi-daemon@.service)" ] || fail "after install.sh: the links or the template"
[[ $(x journalctl -b _PID=1 --since "@$mark" -o cat) == *Reloading* ]] || fail "systemd did not reload at install.sh"
out=$(sysutil install 2>&1) || fail "install from the bundle: $out"
has "$out" "created  instance $UNIT  enabled, active" && has "$out" "running  daemon pid " || fail "install from the bundle: $out"
running
# The unit's bare karvi found in /usr/local/bin, the host's one karvi: the
# daemon answers the bundle's karvi as its own build (the daemon's
# /proc/PID/exe is closed to other processes).
pid=$(prop MainPID)
status=$(as_karvi karvi daemon status 2>&1)
x test ! -e /usr/bin/karvi && [ "$(x readlink -f /usr/local/bin/karvi)" = "$LIB/karvi-v$VERSION" ] && has "$status" "compatible: true" ||
  fail "the unit's daemon is not the bundle's karvi: $status"
xsh "echo '# changed by hand' >>/usr/local/lib/systemd/system/karvi-daemon@.service"
[ "$(prop NeedDaemonReload)" = yes ] || fail "the bundle's changed template needs no reload"
x /root/bundle/scripts/install.sh --nocolor >/dev/null 2>&1 || fail "install.sh again"
[ "$(prop NeedDaemonReload)" = no ] && [ "$(prop MainPID)" = "$pid" ] ||
  fail "after install.sh again: NeedDaemonReload=$(prop NeedDaemonReload) MainPID=$(prop MainPID), was $pid"
out=$(sysutil remove --purge 2>&1) || fail "remove --purge from the bundle: $out"
has "$out" "removed  account karvi" || fail "remove --purge from the bundle: $out"
ok "bundle: install.sh placed the four executables, the page, and the template, systemd reloaded; the daemon set up ran $LIB/karvi-v$VERSION; install.sh again reloaded, the pid $pid the same; removed"
ok "pass"
