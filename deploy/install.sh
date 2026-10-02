#!/usr/bin/env bash
# Installs or upgrades Viceroy as the systemd service "viceroy" (see docs/DEPLOY.md).
# Run from the repo after `make build`:  sudo deploy/install.sh [--lan CIDR]
#
# Safe to re-run: it replaces the binary and unit, but never overwrites an existing
# /etc/viceroy/viceroy.toml or anything in /var/lib/viceroy.
#   --lan CIDR   on first install, listen on all interfaces and allow this subnet
#                (e.g. 192.168.0.0/24) besides localhost.
set -euo pipefail

lan=""
while [ $# -gt 0 ]; do
  case "$1" in
    --lan) lan="$2"; shift 2 ;;
    *) echo "usage: sudo $0 [--lan CIDR]" >&2; exit 2 ;;
  esac
done

[ "$(id -u)" -eq 0 ] || { echo "run with sudo" >&2; exit 1; }
root="$(cd "$(dirname "$0")/.." && pwd)"
bin="$root/bin/viceroy"
[ -x "$bin" ] || { echo "$bin not found; run make build first" >&2; exit 1; }
cfg=/etc/viceroy/viceroy.toml

if ! id viceroy >/dev/null 2>&1; then
  useradd --system --home-dir /var/lib/viceroy --shell /usr/sbin/nologin viceroy
  echo "created user viceroy"
fi

install -m 755 "$bin" /usr/local/bin/viceroy.new
mv /usr/local/bin/viceroy.new /usr/local/bin/viceroy
echo "installed $(/usr/local/bin/viceroy version)"

install -d -m 750 -g viceroy /etc/viceroy
if [ ! -e "$cfg" ]; then
  /usr/local/bin/viceroy -config "$cfg" init >/dev/null
  if [ -n "$lan" ]; then
    sed -i "s|^listen = .*|listen = \"0.0.0.0:8420\"|; s|^allowed_cidrs = .*|allowed_cidrs = [\"127.0.0.1\", \"::1\", \"$lan\"]|" "$cfg"
  fi
  echo "wrote $cfg"
else
  echo "kept existing $cfg"
fi
chgrp viceroy "$cfg"
chmod 640 "$cfg"

install -m 644 "$root/deploy/viceroy.service" /etc/systemd/system/viceroy.service
systemctl daemon-reload
systemctl enable viceroy >/dev/null 2>&1
systemctl restart viceroy

for _ in 1 2 3 4 5 6 7 8 9 10; do
  systemctl is-active --quiet viceroy && curl -fsS -m 2 "http://127.0.0.1:8420/api/health" >/dev/null 2>&1 && break
  sleep 1
done
if systemctl is-active --quiet viceroy; then
  grep -E '^(listen|allowed_cidrs)' "$cfg"
  echo "viceroy is running. Logs: journalctl -u viceroy -f"
else
  echo "viceroy failed to start:" >&2
  journalctl -u viceroy -n 30 --no-pager >&2
  exit 1
fi
