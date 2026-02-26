#!/usr/bin/env bash
# ──────────────────────────────────────────────────────────
# EC2 First-Time Setup Script (Amazon Linux 2023)
# Run as root: sudo bash deploy/setup.sh
# ──────────────────────────────────────────────────────────
set -euo pipefail

DOMAIN="liveoilprices.com"
APP_DIR="/opt/liveoilprices"
DEPLOY_USER="deploy"

echo "==> Installing system packages..."
dnf update -y
dnf install -y nginx git

echo "==> Installing Go..."
GO_VERSION="1.23.6"
if ! command -v go &>/dev/null; then
    curl -LO "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz"
    rm -rf /usr/local/go
    tar -C /usr/local -xzf "go${GO_VERSION}.linux-amd64.tar.gz"
    rm -f "go${GO_VERSION}.linux-amd64.tar.gz"
    echo 'export PATH=$PATH:/usr/local/go/bin' > /etc/profile.d/golang.sh
    export PATH=$PATH:/usr/local/go/bin
    echo "Installed Go $(go version)"
else
    echo "Go already installed: $(go version)"
fi

echo "==> Installing Node.js..."
if ! command -v node &>/dev/null; then
    dnf install -y nodejs npm
    echo "Installed Node $(node --version)"
else
    echo "Node already installed: $(node --version)"
fi

echo "==> Installing certbot..."
if ! command -v certbot &>/dev/null; then
    dnf install -y augeas-libs
    python3 -m venv /opt/certbot
    /opt/certbot/bin/pip install --upgrade pip
    /opt/certbot/bin/pip install certbot certbot-nginx
    ln -sf /opt/certbot/bin/certbot /usr/bin/certbot
    echo "Installed certbot"
else
    echo "Certbot already installed"
fi

echo "==> Creating deploy user..."
if ! id "$DEPLOY_USER" &>/dev/null; then
    useradd -r -m -s /bin/bash "$DEPLOY_USER"
    echo "Created user: $DEPLOY_USER"
else
    echo "User $DEPLOY_USER already exists"
fi

echo "==> Creating app directory..."
mkdir -p "$APP_DIR/bin"
chown -R "$DEPLOY_USER:$DEPLOY_USER" "$APP_DIR"

echo "==> Installing systemd service..."
cp deploy/liveoilprices.service /etc/systemd/system/liveoilprices.service
systemctl daemon-reload
systemctl enable liveoilprices

echo "==> Installing nginx config..."
cp deploy/nginx.conf /etc/nginx/conf.d/liveoilprices.conf
rm -f /etc/nginx/conf.d/default.conf

echo "==> Testing nginx config..."
nginx -t

echo "==> Allowing deploy user to manage the service..."
cat > /etc/sudoers.d/deploy-liveoilprices << 'SUDOERS'
deploy ALL=(ALL) NOPASSWD: /bin/systemctl restart liveoilprices
deploy ALL=(ALL) NOPASSWD: /bin/systemctl stop liveoilprices
deploy ALL=(ALL) NOPASSWD: /bin/systemctl start liveoilprices
deploy ALL=(ALL) NOPASSWD: /bin/systemctl status liveoilprices
deploy ALL=(ALL) NOPASSWD: /bin/systemctl reload nginx
SUDOERS
chmod 0440 /etc/sudoers.d/deploy-liveoilprices

echo "==> Starting nginx..."
systemctl enable nginx
systemctl restart nginx

echo ""
echo "============================================"
echo "  Setup complete! (Amazon Linux)"
echo "============================================"
echo ""
echo "  Next steps:"
echo "  1. Point DNS A record for $DOMAIN → this server's IP"
echo "  2. Point DNS A record for www.$DOMAIN → this server's IP"
echo "  3. Run: certbot --nginx -d $DOMAIN -d www.$DOMAIN --agree-tos -m admin@$DOMAIN"
echo "  4. Copy code to $APP_DIR"
echo "  5. Run: su - deploy -c 'cd $APP_DIR && bash deploy.sh'"
echo ""
