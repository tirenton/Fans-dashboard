#!/usr/bin/env bash
# Sets up (or re-deploys) the dashboard on an Ubuntu VPS. Run as root from the unpacked folder:
#   bash deploy/vps-setup.sh <domain>
# <domain>: your own domain whose DNS points at this VPS, or <ip-with-dashes>.sslip.io (e.g. 203-0-113-10.sslip.io)
# for a free name that always points at the IP. Safe to run again after uploading a newer version: it keeps .env,
# the accounts (config/) and the uploaded data (data/), and only rebuilds and restarts the containers.
set -euo pipefail
cd "$(dirname "$0")/.."
DOMAIN="${1:-}"
[ "$(id -u)" = 0 ] || { echo "run as root (sudo bash deploy/vps-setup.sh <domain>)"; exit 1; }

# Docker from Ubuntu's own packages
if ! command -v docker >/dev/null || ! docker compose version >/dev/null 2>&1; then
  apt-get update
  DEBIAN_FRONTEND=noninteractive apt-get install -y docker.io docker-compose-v2
  systemctl enable --now docker
fi

# secrets: made here once, never leave this server except SYNC_TOKEN (printed below for the PC's uploader)
rand() { openssl rand -base64 32 | tr '+/' '-_' | tr -d '=\n'; }
if [ ! -f .env ]; then
  [ -n "$DOMAIN" ] || { echo "usage: bash deploy/vps-setup.sh <domain>"; exit 1; }
  (umask 077; printf 'DOMAIN=%s\nAPI_TOKEN=%s\nSESSION_SECRET=%s\nSYNC_TOKEN=%s\n' "$DOMAIN" "$(rand)" "$(rand)" "$(rand)" > .env)
elif [ -n "$DOMAIN" ]; then
  sed -i "s|^DOMAIN=.*|DOMAIN=$DOMAIN|" .env
fi
DOMAIN="$(grep '^DOMAIN=' .env | cut -d= -f2-)"

# the containers run as uid 65532 (distroless "nonroot")
mkdir -p data config
chown -R 65532:65532 data config

# firewall: SSH, plus HTTP and HTTPS for Caddy (HTTP is needed for the certificate check and the redirect)
if command -v ufw >/dev/null; then
  ufw allow OpenSSH >/dev/null
  ufw allow 80/tcp >/dev/null
  ufw allow 443 >/dev/null
  ufw --force enable >/dev/null
fi

docker compose -f docker-compose.vps.yml -p nmixx-stats up -d --build

cat <<EOF

Done. The dashboard: https://$DOMAIN   (the HTTPS certificate can take a minute on the first start)

1. Create your admin account if you haven't yet (it asks for the password twice, hidden):
   sudo docker compose -f docker-compose.vps.yml -p nmixx-stats run --rm -it api user add <name> admin

2. On your PC, add these two lines to the dashboard's .env (D:\\work\\nov-dashboard\\.env), keeping the token secret,
   then restart the PC's dashboard: docker compose -p nmixx-fans up -d
   VPS_URL=https://$DOMAIN
   VPS_SYNC_TOKEN=$(grep '^SYNC_TOKEN=' .env | cut -d= -f2-)
EOF
