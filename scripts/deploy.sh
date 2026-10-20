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
FRONTEND_BUILD_TEMP="$PROJECT_DIR/.build-tmp"
DEPLOY_BRANCH="${DEPLOY_BRANCH:-main}"
SNAPSHOT_COMMIT=""
DEPLOY_FAILED=0
GIT_TREE_CHANGED=0
AUTO_ROLLBACK_HAPPENED=0
ROLLBACK_FAILED=0

# Safety check: ROLLBACK_DIR must be absolute and non-empty
if [ -z "$ROLLBACK_DIR" ] || [[ ! "$ROLLBACK_DIR" = /* ]]; then
    echo "ERROR: ROLLBACK_DIR must be a non-empty absolute path"
    exit 1
fi

# Guard against relative paths (require absolute path)
if [[ "$ROLLBACK_DIR" != /* ]]; then
    echo "ERROR: ROLLBACK_DIR must be an absolute path (got: $ROLLBACK_DIR)"
    exit 1
fi

# Cleanup function to restore state on failure
cleanup_on_failure() {
    if [ "$DEPLOY_FAILED" -eq 1 ]; then
        echo ""
        echo "=== Deploy failed, cleaning up ==="
        
        # Only restore git tree if we actually changed it (after pull)
        # and if on-disk files still match what's running
        # Skip if rollback failed - files are in unknown state
        if [ "$GIT_TREE_CHANGED" -eq 1 ] && [ "$AUTO_ROLLBACK_HAPPENED" -eq 0 ] && [ "$ROLLBACK_FAILED" -eq 0 ]; then
            if [ -n "$RUNNING_COMMIT" ] && git rev-parse --verify "$RUNNING_COMMIT" &>/dev/null 2>&1; then
                echo "  Restoring git tree to $RUNNING_COMMIT..."
                git checkout -f "$RUNNING_COMMIT" 2>/dev/null || true
            fi
        fi
        
        # Remove temp files
        rm -f "$BIN.new"
        rm -rf "$FRONTEND_BUILD_TEMP"
        
        # Report status
        if [ "$ROLLBACK_FAILED" -eq 1 ]; then
            echo "  Cleanup complete. ROLLBACK FAILED - service may be in inconsistent state."
        elif [ "$AUTO_ROLLBACK_HAPPENED" -eq 1 ]; then
            echo "  Cleanup complete. Service was automatically rolled back."
        else
            if [ "$GIT_TREE_CHANGED" -eq 0 ]; then
                echo "  Cleanup complete. No changes were made."
            else
                echo "  Cleanup complete. Partial changes may exist."
            fi
        fi
    fi
}

trap cleanup_on_failure EXIT

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

# Check git status and handle detached HEAD from rollback
if git rev-parse --is-inside-work-tree &>/dev/null 2>&1; then
    CURRENT_COMMIT=$(git rev-parse HEAD 2>/dev/null)
    # Save the running commit BEFORE any checkout operations
    # This represents what's actually deployed and should be in the snapshot
    RUNNING_COMMIT="$CURRENT_COMMIT"
    
    # Check for dirty tree
    if ! git diff --quiet || ! git diff --cached --quiet; then
        echo "ERROR: Working tree is dirty. Commit or stash changes before deploying."
        git status --short
        DEPLOY_FAILED=1
        exit 1
    fi
fi

# ---------- Create rollback snapshot BEFORE git pull ----------
echo "[1/6] Creating rollback snapshot..."
TIMESTAMP=$(date +%Y%m%d-%H%M%S)
mkdir -p "$ROLLBACK_DIR"

# Back up current state (binary, templates, and built assets) if they exist
if [ -f "$BIN" ] || [ -f "web/static/js/app.js" ]; then
    # P2: Create snapshot atomically in temp dir, then rename
    ROLLBACK_TEMP="$ROLLBACK_DIR/.tmp-$TIMESTAMP"
    ROLLBACK_PATH="$ROLLBACK_DIR/$TIMESTAMP"
    
    if ! mkdir -p "$ROLLBACK_TEMP"; then
        echo "ERROR: Failed to create temporary snapshot directory"
        DEPLOY_FAILED=1
        exit 1
    fi
    
    # Copy binary if it exists
    if [ -f "$BIN" ]; then
        if ! cp "$BIN" "$ROLLBACK_TEMP/server"; then
            echo "ERROR: Failed to back up binary"
            rm -rf "$ROLLBACK_TEMP"
            DEPLOY_FAILED=1
            exit 1
        fi
    fi
    
    # Copy templates (tracked in git, needed for restore)
    if [ -d "web/templates" ]; then
        if ! cp -r web/templates "$ROLLBACK_TEMP/"; then
            echo "ERROR: Failed to back up templates"
            rm -rf "$ROLLBACK_TEMP"
            DEPLOY_FAILED=1
            exit 1
        fi
    fi
    
    # Copy built static assets if they exist
    if [ -d "web/static/js" ]; then
        if ! cp -r web/static/js "$ROLLBACK_TEMP/"; then
            echo "ERROR: Failed to back up js"
            rm -rf "$ROLLBACK_TEMP"
            DEPLOY_FAILED=1
            exit 1
        fi
    fi
    
    if [ -d "web/static/css" ]; then
        if ! cp -r web/static/css "$ROLLBACK_TEMP/"; then
            echo "ERROR: Failed to back up css"
            rm -rf "$ROLLBACK_TEMP"
            DEPLOY_FAILED=1
            exit 1
        fi
    fi
    
    # Record the git commit so rollback can restore the matching tree
    # Use RUNNING_COMMIT (what's actually deployed) not HEAD (which may have changed due to checkout)
    if [ -n "$RUNNING_COMMIT" ]; then
        if ! echo "$RUNNING_COMMIT" > "$ROLLBACK_TEMP/COMMIT"; then
            echo "ERROR: Failed to record git commit"
            rm -rf "$ROLLBACK_TEMP"
            DEPLOY_FAILED=1
            exit 1
        fi
    elif git rev-parse HEAD &>/dev/null 2>&1; then
        SNAPSHOT_COMMIT=$(git rev-parse HEAD)
        if ! echo "$SNAPSHOT_COMMIT" > "$ROLLBACK_TEMP/COMMIT"; then
            echo "ERROR: Failed to record git commit"
            rm -rf "$ROLLBACK_TEMP"
            DEPLOY_FAILED=1
            exit 1
        fi
    fi
    
    # Atomically rename to final location
    if ! mv "$ROLLBACK_TEMP" "$ROLLBACK_PATH"; then
        echo "ERROR: Failed to finalize snapshot"
        rm -rf "$ROLLBACK_TEMP"
        DEPLOY_FAILED=1
        exit 1
    fi
    
    echo "  Rollback snapshot saved to $ROLLBACK_PATH"
    
    # Keep only the 10 most recent rollbacks (sort by name for timestamp order)
    # Using find with -print0 for NUL-safe handling
    if [ -n "$ROLLBACK_DIR" ] && [ -d "$ROLLBACK_DIR" ]; then
        find "$ROLLBACK_DIR" -mindepth 1 -maxdepth 1 -type d -name '20*' -print0 \
            | sort -z \
            | head -zn -10 \
            | xargs -0 -r rm -rf
    fi
else
    echo "  No existing binary or assets to back up (first deploy or clean build)."
    ROLLBACK_PATH=""
fi

# ---------- Pull latest code ----------
if git rev-parse --is-inside-work-tree &>/dev/null 2>&1; then
    echo "[2/6] Pulling latest code..."
    
    # Initialize CSS backup variables
    CSS_ROLLBACK_BACKUP=""
    CSS_TEMP_BACKUP=""
    
    # Check if we're in detached HEAD (e.g., after a rollback)
    if ! git symbolic-ref -q HEAD >/dev/null; then
        # Verify this is a known snapshot commit
        ROLLBACK_STATE=false
        if [ -n "$CURRENT_COMMIT" ]; then
            for snapshot in "$ROLLBACK_DIR"/*/COMMIT; do
                if [ -f "$snapshot" ]; then
                    SNAP_COMMIT=$(cat "$snapshot" 2>/dev/null || echo "")
                    if [ "$SNAP_COMMIT" = "$CURRENT_COMMIT" ]; then
                        ROLLBACK_STATE=true
                        break
                    fi
                fi
            done 2>/dev/null || true
        fi
        
        if [ "$ROLLBACK_STATE" = false ]; then
            echo "ERROR: Detached HEAD at unknown commit $CURRENT_COMMIT"
            echo "       Either checkout a branch or rollback to a known snapshot"
            DEPLOY_FAILED=1
            exit 1
        fi
        
        echo "  Detected rollback state (detached at snapshot commit $CURRENT_COMMIT)"
        echo "  Returning to branch $DEPLOY_BRANCH..."
        
        # Back up CSS once before checkout to prevent new CSS going live under old hash
        if [ -f "web/static/css/styles.css" ]; then
            CSS_ROLLBACK_BACKUP="$(mktemp)"
            cp "web/static/css/styles.css" "$CSS_ROLLBACK_BACKUP"
        fi
        
        git checkout -f "$DEPLOY_BRANCH" || {
            echo "ERROR: Failed to checkout $DEPLOY_BRANCH"
            if [ -n "$CSS_ROLLBACK_BACKUP" ] && [ -f "$CSS_ROLLBACK_BACKUP" ]; then
                cp "$CSS_ROLLBACK_BACKUP" "web/static/css/styles.css"
                rm -f "$CSS_ROLLBACK_BACKUP"
            fi
            DEPLOY_FAILED=1
            exit 1
        }
        
        # Mark tree as changed immediately after checkout
        GIT_TREE_CHANGED=1
        
        # Restore old CSS so pull can proceed cleanly
        if [ -n "$CSS_ROLLBACK_BACKUP" ] && [ -f "$CSS_ROLLBACK_BACKUP" ]; then
            cp "$CSS_ROLLBACK_BACKUP" "web/static/css/styles.css"
        fi
    fi
    
    # Snapshot CSS before pull to prevent new CSS going live under old hash
    if [ -f "web/static/css/styles.css" ]; then
        CSS_TEMP_BACKUP="$(mktemp)"
        cp "web/static/css/styles.css" "$CSS_TEMP_BACKUP"
    fi
    
    if ! git pull --ff-only; then
        echo "ERROR: git pull failed. Aborting deploy."
        DEPLOY_FAILED=1
        # Restore CSS if we snapshotted it
        if [ -n "$CSS_TEMP_BACKUP" ] && [ -f "$CSS_TEMP_BACKUP" ]; then
            cp "$CSS_TEMP_BACKUP" "web/static/css/styles.css"
            rm -f "$CSS_TEMP_BACKUP"
        fi
        exit 1
    fi
    
    GIT_TREE_CHANGED=1
    
    # Restore old CSS temporarily (old server still serving with old hash)
    if [ -n "$CSS_TEMP_BACKUP" ] && [ -f "$CSS_TEMP_BACKUP" ]; then
        cp "$CSS_TEMP_BACKUP" "web/static/css/styles.css"
        rm -f "$CSS_TEMP_BACKUP"
    fi
    
    # Clean up rollback CSS backup (P3 fix)
    if [ -n "$CSS_ROLLBACK_BACKUP" ] && [ -f "$CSS_ROLLBACK_BACKUP" ]; then
        rm -f "$CSS_ROLLBACK_BACKUP"
    fi
else
    echo "[2/6] Not a git repo, skipping pull."
fi

# ---------- Build frontend into temp directory ----------
echo "[3/6] Building frontend..."
npm cache clean --force 2>/dev/null || true
rm -rf node_modules

if ! npm ci; then
    echo "ERROR: npm ci failed"
    DEPLOY_FAILED=1
    # Clean up temp CSS backup if it exists
    if [ -n "$CSS_TEMP_BACKUP" ] && [ -f "$CSS_TEMP_BACKUP" ]; then
        rm -f "$CSS_TEMP_BACKUP"
    fi
    exit 1
fi

# Build into temp directory to avoid the old server hash pointing at new bytes
# Get the NEW CSS from git (post-pull) for the build
rm -rf "$FRONTEND_BUILD_TEMP"
mkdir -p "$FRONTEND_BUILD_TEMP/js" "$FRONTEND_BUILD_TEMP/css"

# Get new CSS from git instead of the temporarily-restored web/static version
if git rev-parse --is-inside-work-tree &>/dev/null 2>&1; then
    git show HEAD:web/static/css/styles.css > "$FRONTEND_BUILD_TEMP/css/styles.css" || {
        echo "ERROR: Failed to extract CSS from git"
        DEPLOY_FAILED=1
        # Clean up temp CSS backup
        if [ -n "$CSS_TEMP_BACKUP" ] && [ -f "$CSS_TEMP_BACKUP" ]; then
            rm -f "$CSS_TEMP_BACKUP"
        fi
        exit 1
    }
else
    # Not a git repo, use the current CSS
    if [ -f "web/static/css/styles.css" ]; then
        cp "web/static/css/styles.css" "$FRONTEND_BUILD_TEMP/css/" || {
            echo "ERROR: Failed to copy CSS to temp directory"
            DEPLOY_FAILED=1
            exit 1
        }
    fi
fi

# Clean up temp CSS backup
if [ -n "$CSS_TEMP_BACKUP" ] && [ -f "$CSS_TEMP_BACKUP" ]; then
    rm -f "$CSS_TEMP_BACKUP"
fi

# Build JS bundles using package.json scripts (single source of truth for build flags)
if ! OUTDIR="$FRONTEND_BUILD_TEMP" npm run build:to; then
    echo "ERROR: Frontend build failed"
    DEPLOY_FAILED=1
    exit 1
fi

echo "  Frontend built in temp directory."

# ---------- Lint and test ----------
echo "[4/6] Running go vet and tests..."
if ! go vet ./...; then
    echo "ERROR: go vet failed"
    DEPLOY_FAILED=1
    exit 1
fi

if ! go test ./...; then
    echo "ERROR: go test failed"
    DEPLOY_FAILED=1
    exit 1
fi
echo "  Tests passed."

# ---------- Build backend to temporary location ----------
echo "[5/6] Building backend..."
if ! CGO_ENABLED=0 go build -ldflags="-s -w" -o "$BIN.new" ./cmd/server; then
    echo "ERROR: go build failed"
    DEPLOY_FAILED=1
    exit 1
fi
echo "  Backend built ($(du -h "$BIN.new" | cut -f1))."

# Validate assets and templates against the NEW build before swapping
echo "  Validating new binary against new assets and templates..."
if ! "$BIN.new" --check-assets --static-root="$FRONTEND_BUILD_TEMP" --templates="web/templates" 2>&1; then
    echo "ERROR: Asset validation failed. Nothing swapped."
    if [ -n "$ROLLBACK_PATH" ]; then
        echo "  To rollback: scripts/rollback.sh $TIMESTAMP"
    fi
    DEPLOY_FAILED=1
    exit 1
fi

# ---------- Swap binary and frontend assets, then restart ----------
echo "[6/6] Swapping binary and frontend assets, then restarting service..."

# Atomically swap the new binary into place
mv -f "$BIN.new" "$BIN"
echo "  ✓ Binary swapped"

# Atomically swap the new frontend into place
mkdir -p web/static
if [ -d "$FRONTEND_BUILD_TEMP/js" ]; then
    rm -rf web/static/js
    mv "$FRONTEND_BUILD_TEMP/js" web/static/
fi

if [ -d "$FRONTEND_BUILD_TEMP/css" ]; then
    rm -rf web/static/css
    mv "$FRONTEND_BUILD_TEMP/css" web/static/
fi

# Clean up temp directory
rm -rf "$FRONTEND_BUILD_TEMP"

# Restart service (P1: guard against restart failure)
if ! sudo systemctl restart "$SERVICE" 2>&1; then
    echo "  ERROR: systemctl restart failed!"
    sudo systemctl status "$SERVICE" --no-pager 2>&1 || true
    
    # Auto-rollback on failed restart
    if [ -n "$ROLLBACK_PATH" ] && [ -d "$ROLLBACK_PATH" ]; then
        echo ""
        echo "  Attempting automatic rollback to $TIMESTAMP..."
        if bash "$SCRIPT_DIR/rollback.sh" "$TIMESTAMP"; then
            echo "  Rollback successful, but deploy failed."
            AUTO_ROLLBACK_HAPPENED=1
        else
            echo "  ERROR: Rollback also failed!"
            ROLLBACK_FAILED=1
        fi
    fi
    DEPLOY_FAILED=1
    exit 1
fi

sleep 2

# Check if service started (guard against exit 3 under set -e)
if ! sudo systemctl is-active --quiet "$SERVICE" 2>/dev/null || false; then
    echo "  ERROR: Service failed to start!"
    sudo systemctl status "$SERVICE" --no-pager 2>&1 || true
    
    # Auto-rollback on failed start
    if [ -n "$ROLLBACK_PATH" ] && [ -d "$ROLLBACK_PATH" ]; then
        echo ""
        echo "  Attempting automatic rollback to $TIMESTAMP..."
        if bash "$SCRIPT_DIR/rollback.sh" "$TIMESTAMP"; then
            echo "  Rollback successful, but deploy failed."
            AUTO_ROLLBACK_HAPPENED=1
        else
            echo "  ERROR: Rollback also failed!"
            ROLLBACK_FAILED=1
        fi
    fi
    DEPLOY_FAILED=1
    exit 1
fi

# Health check with increased timeout (overridable for tests)
echo "  Service started. Running health checks..."
HEALTH_CHECK_TIMEOUT="${HEALTH_CHECK_TIMEOUT:-30}"
HEALTH_CHECK_INTERVAL=3
STABILITY_WINDOW="${STABILITY_WINDOW:-6}"
MAX_HEALTH_CHECKS=$((HEALTH_CHECK_TIMEOUT / HEALTH_CHECK_INTERVAL))

# Enforce minimum timeout (M5)
if [ "$HEALTH_CHECK_TIMEOUT" -lt 9 ]; then
    echo "  WARNING: HEALTH_CHECK_TIMEOUT=$HEALTH_CHECK_TIMEOUT too low, using 9"
    HEALTH_CHECK_TIMEOUT=9
    MAX_HEALTH_CHECKS=$((HEALTH_CHECK_TIMEOUT / HEALTH_CHECK_INTERVAL))
fi

FIRST_SUCCESS=0
for i in $(seq 1 $MAX_HEALTH_CHECKS); do
    STATUS=$(curl -s --max-time 5 -o /dev/null -w "%{http_code}" "http://127.0.0.1:8080/api/health" 2>&1) || STATUS="000"
    if [ "$STATUS" = "200" ]; then
        # Also check home page and a commodity page
        HOME_STATUS=$(curl -s --max-time 5 -o /dev/null -w "%{http_code}" "http://127.0.0.1:8080/" 2>&1) || HOME_STATUS="000"
        COMMODITY_STATUS=$(curl -s --max-time 5 -o /dev/null -w "%{http_code}" "http://127.0.0.1:8080/commodity/WTI" 2>&1) || COMMODITY_STATUS="000"
        
        if [ "$HOME_STATUS" = "200" ] && [ "$COMMODITY_STATUS" = "200" ]; then
            if [ "$FIRST_SUCCESS" -eq 0 ]; then
                FIRST_SUCCESS=$i
                echo "  ✓ Initial health check passed at $((i * HEALTH_CHECK_INTERVAL))s. Monitoring stability for ${STABILITY_WINDOW}s..."
            fi
            
            # Check if stability window elapsed
            ELAPSED=$((i - FIRST_SUCCESS))
            ELAPSED_TIME=$((ELAPSED * HEALTH_CHECK_INTERVAL))
            if [ "$ELAPSED_TIME" -ge "$STABILITY_WINDOW" ]; then
                # Final check: ensure service is still active (guard against exit 3)
                if ! sudo systemctl is-active --quiet "$SERVICE" 2>/dev/null || false; then
                    echo "  ERROR: Service became inactive during stability window!"
                    sudo systemctl status "$SERVICE" --no-pager 2>&1 || true
                    STATUS="000"
                    FIRST_SUCCESS=0
                else
                    echo "  ✓ Stability verified after ${ELAPSED_TIME}s."
                    break
                fi
            fi
        else
            echo "  WARNING: /api/health OK but pages not OK (home: $HOME_STATUS, commodity: $COMMODITY_STATUS)"
            STATUS="000"
            FIRST_SUCCESS=0
        fi
    else
        FIRST_SUCCESS=0
    fi
    
    # Check if service crashed (guard against exit 3 under set -e)
    if ! sudo systemctl is-active --quiet "$SERVICE" 2>/dev/null || false; then
        echo "  ERROR: Service crashed during health check!"
        sudo systemctl status "$SERVICE" --no-pager 2>&1 || true
        
        # Auto-rollback on crash
        if [ -n "$ROLLBACK_PATH" ] && [ -d "$ROLLBACK_PATH" ]; then
            echo ""
            echo "  Attempting automatic rollback to $TIMESTAMP..."
            if bash "$SCRIPT_DIR/rollback.sh" "$TIMESTAMP"; then
                echo "  Rollback successful, but deploy failed."
                AUTO_ROLLBACK_HAPPENED=1
            else
                echo "  ERROR: Rollback also failed!"
                ROLLBACK_FAILED=1
            fi
        fi
        DEPLOY_FAILED=1
        exit 1
    fi
    
    if [ "$i" = "$MAX_HEALTH_CHECKS" ]; then
        echo "  ERROR: Health check failed after ${HEALTH_CHECK_TIMEOUT}s (HTTP $STATUS)."
        echo "  Service is running but not responding correctly."
        sudo journalctl -u "$SERVICE" --no-pager -n 50 || true
        
        # Auto-rollback on failed health check
        if [ -n "$ROLLBACK_PATH" ] && [ -d "$ROLLBACK_PATH" ]; then
            echo ""
            echo "  Attempting automatic rollback to $TIMESTAMP..."
            if bash "$SCRIPT_DIR/rollback.sh" "$TIMESTAMP"; then
                echo "  Rollback successful, but deploy failed."
                AUTO_ROLLBACK_HAPPENED=1
            else
                echo "  ERROR: Rollback also failed!"
                ROLLBACK_FAILED=1
            fi
        fi
        DEPLOY_FAILED=1
        exit 1
    fi
    sleep $HEALTH_CHECK_INTERVAL
done

# Success - disable cleanup trap
DEPLOY_FAILED=0

echo ""
echo "=== Deploy complete ==="
echo ""
