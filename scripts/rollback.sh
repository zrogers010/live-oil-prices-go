#!/usr/bin/env bash
#
# Rollback Live Oil Prices to a previous snapshot.
# 
# IMPORTANT: Snapshot <timestamp> contains the state BEFORE deploy <timestamp>.
# When you rollback to a snapshot, you restore the state that existed before
# that deploy ran.
#
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
    if ! cp "$BIN" "$BACKUP_PATH/server"; then
        echo "ERROR: Failed to backup binary" >&2
        exit 1
    fi
fi
if [ -d "web/templates" ]; then
    if ! cp -r web/templates "$BACKUP_PATH/"; then
        echo "ERROR: Failed to backup templates" >&2
        exit 1
    fi
fi
if [ -d "web/static/js" ]; then
    if ! cp -r web/static/js "$BACKUP_PATH/"; then
        echo "ERROR: Failed to backup js" >&2
        exit 1
    fi
fi
if [ -d "web/static/css" ]; then
    if ! cp -r web/static/css "$BACKUP_PATH/"; then
        echo "ERROR: Failed to backup css" >&2
        exit 1
    fi
fi
echo "  Current state backed up to $BACKUP_PATH"

# Prune old pre-rollback backups, keeping only the 3 newest
if [ -n "$ROLLBACK_DIR" ] && [ -d "$ROLLBACK_DIR" ]; then
    find "$ROLLBACK_DIR" -mindepth 1 -maxdepth 1 -type d -name 'pre-rollback-*' -print0 \
        | sort -z \
        | head -zn -3 \
        | xargs -0 -r rm -rf
fi

# Restore from snapshot
echo ""
echo "[3/4] Restoring from snapshot..."

# Verify git commit exists before swapping files (M3)
SNAPSHOT_COMMIT=""
if [ -f "$ROLLBACK_PATH/COMMIT" ]; then
    SNAPSHOT_COMMIT=$(cat "$ROLLBACK_PATH/COMMIT")
    if git rev-parse --is-inside-work-tree &>/dev/null 2>&1; then
        if ! git cat-file -e "$SNAPSHOT_COMMIT^{commit}" 2>/dev/null; then
            echo "ERROR: Snapshot commit $SNAPSHOT_COMMIT not found in git history"
            exit 1
        fi
    fi
fi

restore_from_backup() {
    echo "  Attempting to restore from pre-rollback backup..."
    if [ -f "$BACKUP_PATH/server" ]; then
        TEMP_BIN="$(dirname "$BIN")/.server.tmp.$$"
        cp "$BACKUP_PATH/server" "$TEMP_BIN" && chmod +x "$TEMP_BIN" && mv -f "$TEMP_BIN" "$BIN"
        [ -d "$BACKUP_PATH/templates" ] && rm -rf web/templates && cp -r "$BACKUP_PATH/templates" web/
        [ -d "$BACKUP_PATH/js" ] && rm -rf web/static/js && cp -r "$BACKUP_PATH/js" web/static/
        [ -d "$BACKUP_PATH/css" ] && rm -rf web/static/css && cp -r "$BACKUP_PATH/css" web/static/
    fi
}

# Restore binary (copy to temp then mv to avoid "Text file busy" if service is running)
mkdir -p "$(dirname "$BIN")"
TEMP_BIN="$(dirname "$BIN")/.server.tmp.$$"
cp "$ROLLBACK_PATH/server" "$TEMP_BIN"
chmod +x "$TEMP_BIN"
mv -f "$TEMP_BIN" "$BIN"
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

# Restore git commit if recorded
if [ -n "$SNAPSHOT_COMMIT" ]; then
    echo "  Snapshot commit: $SNAPSHOT_COMMIT"
    
    if git rev-parse --is-inside-work-tree &>/dev/null 2>&1; then
        echo "  Checking out commit $SNAPSHOT_COMMIT in detached HEAD..."
        # Use -f to force checkout over modified tracked files
        # deploy.sh will detect this and checkout the branch before pulling
        if ! git checkout -f "$SNAPSHOT_COMMIT" 2>/dev/null; then
            echo "  ERROR: Failed to checkout $SNAPSHOT_COMMIT"
            restore_from_backup
            exit 1
        fi
        
        # Verify HEAD matches snapshot commit
        CURRENT_HEAD=$(git rev-parse HEAD)
        if [ "$CURRENT_HEAD" != "$SNAPSHOT_COMMIT" ]; then
            echo "  ERROR: HEAD is $CURRENT_HEAD but expected $SNAPSHOT_COMMIT"
            restore_from_backup
            exit 1
        fi
        
        # Verify tree is clean
        if ! git diff --quiet || ! git diff --cached --quiet; then
            echo "  ERROR: Working tree is dirty after checkout"
            git status --short
            restore_from_backup
            exit 1
        fi
        
        echo "  ✓ Git tree restored to $SNAPSHOT_COMMIT"
    else
        echo "  WARNING: Not a git repo, tracked files restored from snapshot only"
    fi
fi

# Restart service
echo ""
echo "[4/4] Restarting service..."
if ! sudo systemctl restart "$SERVICE" 2>&1; then
    echo "  ERROR: systemctl restart failed!"
    sudo systemctl status "$SERVICE" --no-pager 2>&1 || true
    echo ""
    echo "Failed to restart after rollback."
    echo "Manual intervention required."
    exit 1
fi

sleep 2

# Check service status (guard against exit 3 under set -e)
if sudo systemctl is-active --quiet "$SERVICE" 2>/dev/null || false; then
    echo "  ✓ Service is running"
else
    echo "  ERROR: Service failed to start after rollback!"
    sudo systemctl status "$SERVICE" --no-pager 2>&1 || true
    echo ""
    echo "Rolled-back version also fails to start."
    echo "Manual intervention required."
    exit 1
fi

# Health check with stability window (aligned with deploy.sh)
echo ""
echo "Running health check..."
HEALTH_CHECK_TIMEOUT="${HEALTH_CHECK_TIMEOUT:-30}"
HEALTH_CHECK_INTERVAL=3
STABILITY_WINDOW="${STABILITY_WINDOW:-15}"

# Get initial service state for restart detection
INITIAL_PID=""
if sudo systemctl is-active --quiet "$SERVICE" 2>/dev/null || false; then
    INITIAL_PID=$(sudo systemctl show "$SERVICE" --property=MainPID --value 2>/dev/null || echo "")
fi

FIRST_SUCCESS_TIME=0
CURRENT_TIME=0
MAX_TIME_FOR_FIRST_SUCCESS=$HEALTH_CHECK_TIMEOUT
i=1

while true; do
    CURRENT_TIME=$((i * HEALTH_CHECK_INTERVAL))
    
    STATUS=$(curl -s --max-time 5 -o /dev/null -w "%{http_code}" "http://127.0.0.1:8080/api/health" 2>&1) || STATUS="000"
    if [ "$STATUS" = "200" ]; then
        if [ "$FIRST_SUCCESS_TIME" -eq 0 ]; then
            FIRST_SUCCESS_TIME=$CURRENT_TIME
            echo "  ✓ Initial health check passed at ${CURRENT_TIME}s. Monitoring stability for ${STABILITY_WINDOW}s..."
        fi
        
        # Check if stability window elapsed
        ELAPSED=$((CURRENT_TIME - FIRST_SUCCESS_TIME))
        if [ "$ELAPSED" -ge "$STABILITY_WINDOW" ]; then
            # Final checks - service still active and hasn't restarted
            if ! sudo systemctl is-active --quiet "$SERVICE" 2>/dev/null || false; then
                echo "  ERROR: Service became inactive during stability window!"
                sudo systemctl status "$SERVICE" --no-pager 2>&1 || true
                FIRST_SUCCESS_TIME=0
            else
                # Check if service restarted (PID changed)
                CURRENT_PID=$(sudo systemctl show "$SERVICE" --property=MainPID --value 2>/dev/null || echo "")
                if [ -n "$INITIAL_PID" ] && [ -n "$CURRENT_PID" ] && [ "$CURRENT_PID" != "$INITIAL_PID" ]; then
                    echo "  ERROR: Service restarted during stability window (PID $INITIAL_PID -> $CURRENT_PID)!"
                    sudo systemctl status "$SERVICE" --no-pager 2>&1 || true
                    echo ""
                    echo "=== Rollback failed (service restarted) ==="
                    exit 1
                else
                    echo "  ✓ Stability verified after ${ELAPSED}s."
                    break
                fi
            fi
        fi
    else
        FIRST_SUCCESS_TIME=0
    fi
    
    # Check if service crashed
    if ! sudo systemctl is-active --quiet "$SERVICE" 2>/dev/null || false; then
        echo "  ERROR: Service crashed during health check at ${CURRENT_TIME}s!"
        sudo systemctl status "$SERVICE" --no-pager 2>&1 || true
        echo ""
        echo "=== Rollback failed (service crashed) ==="
        exit 1
    fi
    
    # Check timeout (only applies before first success)
    if [ "$FIRST_SUCCESS_TIME" -eq 0 ] && [ "$CURRENT_TIME" -ge "$MAX_TIME_FOR_FIRST_SUCCESS" ]; then
        echo "  ERROR: Health check failed after ${HEALTH_CHECK_TIMEOUT}s (HTTP $STATUS)"
        echo "  Check logs: sudo journalctl -u $SERVICE --no-pager -n 50"
        echo ""
        echo "=== Rollback failed (unhealthy) ==="
        exit 1
    fi
    
    sleep $HEALTH_CHECK_INTERVAL
    i=$((i + 1))
done

echo ""
echo "=== Rollback complete ==="
echo "  Rolled back to: $TIMESTAMP"
echo "  Pre-rollback backup: $BACKUP_PATH"
echo ""
