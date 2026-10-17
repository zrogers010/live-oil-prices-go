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
ROLLBACK_DIR="/home/deploy/liveoilprices-rollbacks"

# Guard against accidental rm -rf in empty ROLLBACK_DIR
if [ -z "$ROLLBACK_DIR" ] || [ "$ROLLBACK_DIR" = "/" ]; then
    echo "ERROR: ROLLBACK_DIR is empty or root"
    exit 1
fi

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

# ---------- Create rollback snapshot ----------
echo "[1/5] Creating rollback snapshot..."
TIMESTAMP=$(date +%Y%m%d-%H%M%S)
ROLLBACK_PATH="$ROLLBACK_DIR/$TIMESTAMP"
mkdir -p "$ROLLBACK_PATH"

# Snapshot current state BEFORE git pull
if [ -f "$BIN" ]; then
    cp "$BIN" "$ROLLBACK_PATH/server"
    echo "  Snapshotted binary"
else
    echo "  No existing binary to snapshot"
fi

if [ -d "web/templates" ]; then
    cp -r web/templates "$ROLLBACK_PATH/templates"
    echo "  Snapshotted templates"
fi

if [ -d "web/static/js" ]; then
    cp -r web/static/js "$ROLLBACK_PATH/js"
    echo "  Snapshotted js"
fi

if [ -d "web/static/css" ]; then
    cp -r web/static/css "$ROLLBACK_PATH/css"
    echo "  Snapshotted css"
fi

# Record git commit
if git rev-parse --is-inside-work-tree &>/dev/null 2>&1; then
    git rev-parse HEAD > "$ROLLBACK_PATH/COMMIT"
    echo "  Recorded commit: $(cat "$ROLLBACK_PATH/COMMIT")"
fi

echo "  Rollback snapshot saved to $ROLLBACK_PATH"

# Prune old rollbacks (keep 10 newest by name sort)
if [ -d "$ROLLBACK_DIR" ]; then
    find "$ROLLBACK_DIR" -mindepth 1 -maxdepth 1 -type d -name '20*' -print0 | \
        sort -zr | tail -zn +11 | xargs -0r rm -rf --
fi

# ---------- Pull latest code ----------
if git rev-parse --is-inside-work-tree &>/dev/null 2>&1; then
    echo "[2/5] Pulling latest code..."
    if ! git pull --ff-only; then
        echo "ERROR: git pull failed. Aborting deploy."
        exit 1
    fi
else
    echo "[2/5] Not a git repo, skipping pull."
fi

# ---------- Build frontend ----------
echo "[3/5] Building frontend..."
npm cache clean --force 2>/dev/null || true
rm -rf node_modules
npm ci
npm run build
echo "  Frontend built."

# ---------- Lint and test ----------
echo "[4/5] Running go vet and tests..."
go vet ./...
go test ./...
echo "  Tests passed."

# ---------- Build backend ----------
echo "[5/5] Building backend..."
CGO_ENABLED=0 go build -ldflags="-s -w" -o "$BIN" ./cmd/server
echo "  Backend built ($(du -h "$BIN" | cut -f1))."

# ---------- Restart & verify ----------
echo "Restarting service..."
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
echo "  To rollback: restore from $ROLLBACK_PATH manually"
echo ""
