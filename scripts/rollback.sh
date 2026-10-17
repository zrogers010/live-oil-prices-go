#!/usr/bin/env bash
#
# Rollback Live Oil Prices to a previous snapshot.
# Usage: scripts/rollback.sh <timestamp>
# Example: scripts/rollback.sh 20261017-143022
#
set -euo pipefail

if [ $# -ne 1 ]; then
    echo "Usage: $0 <timestamp>"
    echo "Example: $0 20261017-143022"
    echo ""
    echo "Available rollback snapshots:"
    # shellcheck disable=SC2010
    ls -1 /home/deploy/liveoilprices-rollbacks/ 2>/dev/null | grep '^20' || echo "  (none)"
    exit 1
fi

TIMESTAMP="$1"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$PROJECT_DIR"

SERVICE="liveoilprices"
BIN="$PROJECT_DIR/bin/server"
ROLLBACK_DIR="/home/deploy/liveoilprices-rollbacks"
ROLLBACK_PATH="$ROLLBACK_DIR/$TIMESTAMP"

echo ""
echo "=== Live Oil Prices Rollback ==="
echo "  Project: $PROJECT_DIR"
echo "  Rollback: $TIMESTAMP"
echo ""

# Validate snapshot exists
if [ ! -d "$ROLLBACK_PATH" ]; then
    echo "ERROR: Rollback snapshot $ROLLBACK_PATH does not exist"
    echo ""
    echo "Available snapshots:"
    # shellcheck disable=SC2010
    ls -1 "$ROLLBACK_DIR" 2>/dev/null | grep '^20' || echo "  (none)"
    exit 1
fi

# Validate snapshot has required files
MISSING=()
if [ ! -f "$ROLLBACK_PATH/server" ]; then
    MISSING+=("server binary")
fi
if [ ! -d "$ROLLBACK_PATH/templates" ]; then
    MISSING+=("templates/")
fi
if [ ! -d "$ROLLBACK_PATH/js" ]; then
    MISSING+=("js/")
fi

if [ ${#MISSING[@]} -gt 0 ]; then
    echo "ERROR: Rollback snapshot is incomplete. Missing:"
    for item in "${MISSING[@]}"; do
        echo "  - $item"
    done
    exit 1
fi

echo "[1/4] Validating snapshot..."
echo "  ✓ Binary: $ROLLBACK_PATH/server ($(du -h "$ROLLBACK_PATH/server" | cut -f1))"
echo "  ✓ Templates: $ROLLBACK_PATH/templates"
echo "  ✓ JS: $ROLLBACK_PATH/js"
if [ -d "$ROLLBACK_PATH/css" ]; then
    echo "  ✓ CSS: $ROLLBACK_PATH/css"
fi

# Create backup of current state before rollback
echo ""
echo "[2/4] Backing up current state..."
BACKUP_TIMESTAMP=$(date +%Y%m%d-%H%M%S)
BACKUP_PATH="$ROLLBACK_DIR/pre-rollback-$BACKUP_TIMESTAMP"
mkdir -p "$BACKUP_PATH"

if [ -f "$BIN" ]; then
    cp "$BIN" "$BACKUP_PATH/server"
fi
if [ -d "web/templates" ]; then
    cp -r web/templates "$BACKUP_PATH/"
fi
if [ -d "web/static/js" ]; then
    cp -r web/static/js "$BACKUP_PATH/"
fi
if [ -d "web/static/css" ]; then
    cp -r web/static/css "$BACKUP_PATH/"
fi
echo "  Current state backed up to $BACKUP_PATH"

# Restore from snapshot
echo ""
echo "[3/4] Restoring from snapshot..."

# Restore binary
mkdir -p "$(dirname "$BIN")"
cp "$ROLLBACK_PATH/server" "$BIN"
chmod +x "$BIN"
echo "  ✓ Restored binary"

# Restore templates
rm -rf web/templates
cp -r "$ROLLBACK_PATH/templates" web/
echo "  ✓ Restored templates"

# Restore static assets
mkdir -p web/static
rm -rf web/static/js
cp -r "$ROLLBACK_PATH/js" web/static/
echo "  ✓ Restored js"

if [ -d "$ROLLBACK_PATH/css" ]; then
    rm -rf web/static/css
    cp -r "$ROLLBACK_PATH/css" web/static/
    echo "  ✓ Restored css"
fi

# Restart service
echo ""
echo "[4/4] Restarting service..."
sudo systemctl restart "$SERVICE"
sleep 2

if sudo systemctl is-active --quiet "$SERVICE"; then
    echo "  ✓ Service is running"
else
    echo "  ERROR: Service failed to start!"
    sudo systemctl status "$SERVICE" --no-pager
    echo ""
    echo "Attempting to restore from backup..."
    if [ -f "$BACKUP_PATH/server" ]; then
        cp "$BACKUP_PATH/server" "$BIN"
        if [ -d "$BACKUP_PATH/templates" ]; then
            rm -rf web/templates
            cp -r "$BACKUP_PATH/templates" web/
        fi
        sudo systemctl restart "$SERVICE"
    fi
    exit 1
fi

# Health check
echo ""
echo "Running health check..."
RETRIES=5
for i in $(seq 1 $RETRIES); do
    STATUS=$(curl -sf -o /dev/null -w "%{http_code}" "http://127.0.0.1:8080/api/health" 2>/dev/null || echo "000")
    if [ "$STATUS" = "200" ]; then
        echo "  ✓ Health check passed"
        break
    fi
    if [ "$i" = "$RETRIES" ]; then
        echo "  WARNING: Health check failed after $RETRIES attempts (HTTP $STATUS)"
        echo "  Check logs: sudo journalctl -u $SERVICE --no-pager -n 50"
    fi
    sleep 3
done

echo ""
echo "=== Rollback complete ==="
echo "  Rolled back to: $TIMESTAMP"
echo "  Pre-rollback backup: $BACKUP_PATH"
echo ""
