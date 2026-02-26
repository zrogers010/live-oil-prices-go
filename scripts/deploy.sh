#!/usr/bin/env bash
#
# Deploy Live Oil Prices to production.
# Run from the project root directory as the deploy user.
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$PROJECT_DIR"

export PATH=$PATH:/usr/local/go/bin

SERVICE="liveoilprices"
BIN="$PROJECT_DIR/bin/server"

echo ""
echo "=== Live Oil Prices Deploy ==="
echo "  Project: $PROJECT_DIR"
echo ""

# ---------- Pre-flight checks ----------
if ! command -v go &>/dev/null; then
    echo "ERROR: Go is not installed. Run scripts/setup.sh first."
    exit 1
fi

if ! command -v node &>/dev/null; then
    echo "ERROR: Node.js is not installed. Run scripts/setup.sh first."
    exit 1
fi

# ---------- Pull latest code ----------
if git rev-parse --is-inside-work-tree &>/dev/null 2>&1; then
    echo "[1/4] Pulling latest code..."
    git pull --ff-only || {
        echo "  WARNING: git pull failed (maybe not on a tracked branch). Continuing..."
    }
else
    echo "[1/4] Not a git repo, skipping pull."
fi

# ---------- Build frontend ----------
echo "[2/4] Building frontend..."
npm install --silent 2>/dev/null
npm run build
echo "  Frontend built."

# ---------- Build backend ----------
echo "[3/4] Building backend..."
CGO_ENABLED=0 go build -ldflags="-s -w" -o "$BIN" ./cmd/server
echo "  Backend built ($(du -h "$BIN" | cut -f1))."

# ---------- Restart & verify ----------
echo "[4/4] Restarting service..."
sudo systemctl restart "$SERVICE"
sleep 2

if sudo systemctl is-active --quiet "$SERVICE"; then
    echo "  Service is running."
else
    echo "  ERROR: Service failed to start!"
    sudo systemctl status "$SERVICE" --no-pager
    exit 1
fi

# Health check
RETRIES=5
for i in $(seq 1 $RETRIES); do
    STATUS=$(curl -sf -o /dev/null -w "%{http_code}" "http://127.0.0.1:8080/api/health" 2>/dev/null || echo "000")
    if [ "$STATUS" = "200" ]; then
        echo "  Health check passed."
        break
    fi
    if [ "$i" = "$RETRIES" ]; then
        echo "  WARNING: Health check failed after $RETRIES attempts (HTTP $STATUS)."
        echo "  Check logs: sudo journalctl -u $SERVICE --no-pager -n 50"
    fi
    sleep 3
done

echo ""
echo "=== Deploy complete ==="
echo ""
