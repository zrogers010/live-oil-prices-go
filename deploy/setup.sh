#!/usr/bin/env bash
# ──────────────────────────────────────────────────────────
# EC2 First-Time Setup Script
# Run as root: sudo bash deploy/setup.sh
# ──────────────────────────────────────────────────────────
set -euo pipefail

DOMAIN="liveoilprices.com"
APP_DIR="/opt/liveoilprices"
DEPLOY_USER="deploy"

echo "==> Installing system packages..."
apt-get update -y
apt-get install -y nginx certbot python3-certbot-nginx golang-go nodejs npm git

echo "==> Creating deploy user..."
if ! id "$DEPLOY_USER" &>/dev/null; then
    useradd -r -m -s /bin/bash "$DEPLOY_USER"
    echo "Created user: $DEPLOY_USER"
fi

echo "==> Creating app directory..."
mkdir -p "$APP_DIR/bin"
chown -R "$DEPLOY_USER:$DEPLOY_USER" "$APP_DIR"

echo "==> Installing systemd service..."
cp deploy/liveoilprices.service /etc/systemd/system/liveoilprices.service
systemctl daemon-reload
systemctl enable liveoilprices

echo "==> Installing nginx config..."
cp deploy/nginx.conf /etc/nginx/sites-available/liveoilprices
ln -sf /etc/nginx/sites-available/liveoilprices /etc/nginx/sites-enabled/liveoilprices
rm -f /etc/nginx/sites-enabled/default

echo "==> Testing nginx config..."
nginx -t

echo "==> Obtaining TLS certificate..."
echo "  NOTE: Make sure DNS for $DOMAIN and www.$DOMAIN points to this server first."
echo "  Run this manually when DNS is ready:"
echo "    certbot --nginx -d $DOMAIN -d www.$DOMAIN --non-interactive --agree-tos -m admin@$DOMAIN"
echo ""

echo "==> Allowing deploy user to manage the service..."
cat > /etc/sudoers.d/deploy-liveoilprices << 'SUDOERS'
deploy ALL=(ALL) NOPASSWD: /bin/systemctl restart liveoilprices
deploy ALL=(ALL) NOPASSWD: /bin/systemctl stop liveoilprices
deploy ALL=(ALL) NOPASSWD: /bin/systemctl start liveoilprices
deploy ALL=(ALL) NOPASSWD: /bin/systemctl status liveoilprices
deploy ALL=(ALL) NOPASSWD: /bin/systemctl reload nginx
SUDOERS
chmod 0440 /etc/sudoers.d/deploy-liveoilprices

echo "==> Restarting nginx..."
systemctl restart nginx

echo ""
echo "============================================"
echo "  Setup complete!"
echo "============================================"
echo ""
echo "  Next steps:"
echo "  1. Point DNS A record for $DOMAIN → this server's IP"
echo "  2. Point DNS A record for www.$DOMAIN → this server's IP"
echo "  3. Run: certbot --nginx -d $DOMAIN -d www.$DOMAIN"
echo "  4. Copy code to $APP_DIR"
echo "  5. Run: su - deploy -c 'cd $APP_DIR && bash deploy.sh'"
echo ""
