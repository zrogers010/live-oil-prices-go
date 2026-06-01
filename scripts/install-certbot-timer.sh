#!/usr/bin/env bash
#
# Install a systemd timer that auto-renews the Let's Encrypt cert.
#
# The pip-installed certbot in /opt/certbot does NOT ship with a renewal
# timer (only the OS package version does). Without this, certs silently
# expire after 90 days. This script is idempotent — safe to re-run.
#
# Run as root on the production server: sudo bash scripts/install-certbot-timer.sh
#
set -euo pipefail

if [[ $EUID -ne 0 ]]; then
    echo "ERROR: must run as root (try: sudo bash $0)" >&2
    exit 1
fi

if ! command -v certbot &>/dev/null; then
    echo "ERROR: certbot not found on PATH. Install it first via scripts/setup.sh." >&2
    exit 1
fi

echo "Installing certbot-renew systemd unit + timer..."

cat > /etc/systemd/system/certbot-renew.service << 'EOF'
[Unit]
Description=Renew Let's Encrypt certificates
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
ExecStart=/usr/bin/certbot renew --quiet --deploy-hook "systemctl reload nginx"
EOF

cat > /etc/systemd/system/certbot-renew.timer << 'EOF'
[Unit]
Description=Run certbot renew twice daily

[Timer]
OnCalendar=*-*-* 03,15:00:00
RandomizedDelaySec=1h
Persistent=true

[Install]
WantedBy=timers.target
EOF

systemctl daemon-reload
systemctl enable --now certbot-renew.timer

echo ""
echo "Done. Verify with:"
echo "  systemctl list-timers certbot-renew.timer"
echo "  systemctl status certbot-renew.timer"
echo ""
echo "Force a dry-run renewal to confirm everything works:"
echo "  sudo certbot renew --dry-run"
