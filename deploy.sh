#!/usr/bin/env bash
# ──────────────────────────────────────────────────────────
# deploy.sh — Build & deploy Live Oil Prices
# Run as deploy user from /opt/liveoilprices
# Usage: bash deploy.sh [--pull]
# ──────────────────────────────────────────────────────────
set -euo pipefail

export PATH=$PATH:/usr/local/go/bin

APP_DIR="/opt/liveoilprices"
BIN="$APP_DIR/bin/server"
SERVICE="liveoilprices"

cd "$APP_DIR"

echo ""
echo "  ◆ Live Oil Prices — Deploy"
echo "  ──────────────────────────"
echo ""

# Optional: pull latest code from git
if [[ "${1:-}" == "--pull" ]]; then
    echo "==> Pulling latest code..."
    git pull origin main
    echo ""
fi

# Build frontend
echo "==> Building frontend..."
npm install --production=false --silent 2>/dev/null || npm install --silent
npm run build
echo "    ✓ Frontend built"

# Build backend
echo "==> Building backend..."
CGO_ENABLED=0 go build -ldflags="-s -w" -o "$BIN" ./cmd/server
echo "    ✓ Backend built ($(du -h "$BIN" | cut -f1))"

# Restart service
echo "==> Restarting service..."
sudo systemctl restart "$SERVICE"
sleep 2

# Verify
if sudo systemctl is-active --quiet "$SERVICE"; then
    echo "    ✓ Service is running"
else
    echo "    ✗ Service failed to start!"
    sudo systemctl status "$SERVICE" --no-pager
    exit 1
fi

# Health check
echo "==> Health check..."
HEALTH=$(curl -sf http://127.0.0.1:8080/api/health 2>/dev/null || echo "FAIL")
if echo "$HEALTH" | grep -q '"ok"'; then
    echo "    ✓ API healthy"
else
    echo "    ✗ Health check failed: $HEALTH"
    exit 1
fi

echo ""
echo "  ✓ Deploy complete!"
echo ""
