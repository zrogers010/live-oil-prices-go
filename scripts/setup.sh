#!/usr/bin/env bash
#
# First-time EC2 setup for Live Oil Prices (Amazon Linux 2023).
# Run as root: sudo bash scripts/setup.sh
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

DOMAIN="liveoilprices.com"
DEPLOY_USER="deploy"

echo ""
echo "=== Live Oil Prices — Server Setup ==="
echo "  Project: $PROJECT_DIR"
echo ""

# ---------- System packages ----------
echo "[1/7] Installing system packages..."
dnf update -y
dnf install -y nginx git

# ---------- Go ----------
echo "[2/7] Installing Go..."
GO_VERSION="1.23.6"
if ! command -v /usr/local/go/bin/go &>/dev/null; then
    curl -LO "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz"
    rm -rf /usr/local/go
    tar -C /usr/local -xzf "go${GO_VERSION}.linux-amd64.tar.gz"
    rm -f "go${GO_VERSION}.linux-amd64.tar.gz"
    echo 'export PATH=$PATH:/usr/local/go/bin' > /etc/profile.d/golang.sh
    echo "  Installed Go $(/usr/local/go/bin/go version)"
else
    echo "  Go already installed: $(/usr/local/go/bin/go version)"
fi

# ---------- Node.js ----------
echo "[3/7] Installing Node.js..."
if ! command -v node &>/dev/null; then
    dnf install -y nodejs npm
    echo "  Installed Node $(node --version)"
else
    echo "  Node already installed: $(node --version)"
fi

# ---------- Certbot ----------
echo "[4/7] Installing certbot..."
if ! command -v certbot &>/dev/null; then
    dnf install -y augeas-libs python3-pip
    python3 -m venv /opt/certbot
    /opt/certbot/bin/pip install --upgrade pip
    /opt/certbot/bin/pip install certbot certbot-nginx
    ln -sf /opt/certbot/bin/certbot /usr/bin/certbot
    echo "  Installed certbot."
else
    echo "  Certbot already installed."
fi

# ---------- Deploy user ----------
echo "[5/7] Creating deploy user..."
if ! id "$DEPLOY_USER" &>/dev/null; then
    useradd -r -m -s /bin/bash "$DEPLOY_USER"
    echo "  Created user: $DEPLOY_USER"
else
    echo "  User $DEPLOY_USER already exists."
fi

# Sudoers for service management
SYSTEMCTL_PATH=$(which systemctl)
cat > /etc/sudoers.d/deploy-liveoilprices << SUDOERS
deploy ALL=(ALL) NOPASSWD: $SYSTEMCTL_PATH restart liveoilprices
deploy ALL=(ALL) NOPASSWD: $SYSTEMCTL_PATH stop liveoilprices
deploy ALL=(ALL) NOPASSWD: $SYSTEMCTL_PATH start liveoilprices
deploy ALL=(ALL) NOPASSWD: $SYSTEMCTL_PATH status liveoilprices
deploy ALL=(ALL) NOPASSWD: $SYSTEMCTL_PATH reload nginx
SUDOERS
chmod 0440 /etc/sudoers.d/deploy-liveoilprices

# ---------- Systemd service ----------
echo "[6/7] Installing systemd service..."
cp "$SCRIPT_DIR/liveoilprices.service" /etc/systemd/system/liveoilprices.service
systemctl daemon-reload
systemctl enable liveoilprices

# ---------- Nginx ----------
echo "[7/7] Installing nginx config..."
cp "$SCRIPT_DIR/nginx.conf" /etc/nginx/conf.d/liveoilprices.conf
rm -f /etc/nginx/conf.d/default.conf
nginx -t
systemctl enable nginx
systemctl restart nginx

echo ""
echo "=== Setup complete! ==="
echo ""
echo "  Next steps:"
echo "  1. Clone repo to deploy user home:"
echo "     sudo su - deploy"
echo "     git clone <repo-url> ~/liveoilprices"
echo ""
echo "  2. First deploy:"
echo "     cd ~/liveoilprices"
echo "     bash scripts/deploy.sh"
echo ""
echo "  3. Point DNS A records for $DOMAIN and www.$DOMAIN to this server"
echo ""
echo "  4. SSL (after DNS propagates):"
echo "     exit  # back to ec2-user"
echo "     sudo certbot --nginx -d $DOMAIN -d www.$DOMAIN --agree-tos -m admin@$DOMAIN"
echo ""
