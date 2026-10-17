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

# Safety check: ROLLBACK_DIR must be absolute and non-empty
if [ -z "$ROLLBACK_DIR" ] || [[ ! "$ROLLBACK_DIR" = /* ]]; then
    echo "ERROR: ROLLBACK_DIR must be a non-empty absolute path"
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

# Check git status - refuse to deploy from a dirty tree
if git rev-parse --is-inside-work-tree &>/dev/null 2>&1; then
    # Allow a dirty tree only if we're in detached HEAD at a snapshot commit
    # (i.e., after a rollback that restored tracked files)
    if ! git diff --quiet || ! git diff --cached --quiet; then
        CURRENT_COMMIT=$(git rev-parse HEAD 2>/dev/null)
        DETACHED=false
        git symbolic-ref HEAD &>/dev/null || DETACHED=true
        
        # Check if this is a rollback state (detached at a recorded snapshot commit)
        ROLLBACK_STATE=false
        if [ "$DETACHED" = true ] && [ -n "$CURRENT_COMMIT" ]; then
            # Look for a snapshot with this commit
            for snapshot in "$ROLLBACK_DIR"/20*/COMMIT 2>/dev/null; do
                if [ -f "$snapshot" ]; then
                    SNAPSHOT_COMMIT=$(cat "$snapshot" 2>/dev/null || echo "")
                    if [ "$SNAPSHOT_COMMIT" = "$CURRENT_COMMIT" ]; then
                        ROLLBACK_STATE=true
                        break
                    fi
                fi
            done
        fi
        
        if [ "$ROLLBACK_STATE" = true ]; then
            echo "  Detected rollback state (detached at snapshot commit $CURRENT_COMMIT)"
            echo "  Resetting to clean state and returning to branch..."
            # Get the branch name from the most recent snapshot
            git checkout - 2>/dev/null || git checkout main 2>/dev/null || true
            git reset --hard HEAD
        else
            echo "ERROR: Working tree is dirty. Commit or stash changes before deploying."
            git status --short
            exit 1
        fi
    fi
fi

# ---------- Create rollback snapshot BEFORE git pull ----------
echo "[1/6] Creating rollback snapshot..."
TIMESTAMP=$(date +%Y%m%d-%H%M%S)
mkdir -p "$ROLLBACK_DIR"

# Back up current state (binary, templates, and built assets) if they exist
if [ -f "$BIN" ] || [ -f "web/static/js/app.js" ]; then
    ROLLBACK_PATH="$ROLLBACK_DIR/$TIMESTAMP"
    mkdir -p "$ROLLBACK_PATH"
    
    # Copy binary if it exists
    if [ -f "$BIN" ]; then
        cp "$BIN" "$ROLLBACK_PATH/server" || {
            echo "ERROR: Failed to back up binary"
            exit 1
        }
    fi
    
    # Copy templates (tracked in git, needed for restore)
    if [ -d "web/templates" ]; then
        cp -r web/templates "$ROLLBACK_PATH/" || {
            echo "ERROR: Failed to back up templates"
            exit 1
        }
    fi
    
    # Copy built static assets if they exist
    if [ -d "web/static/js" ]; then
        cp -r web/static/js "$ROLLBACK_PATH/" || {
            echo "ERROR: Failed to back up js"
            exit 1
        }
    fi
    
    if [ -d "web/static/css" ]; then
        cp -r web/static/css "$ROLLBACK_PATH/" || {
            echo "ERROR: Failed to back up css"
            exit 1
        }
    fi
    
    # Record the git commit so rollback can restore the matching tree
    if git rev-parse HEAD &>/dev/null 2>&1; then
        git rev-parse HEAD > "$ROLLBACK_PATH/COMMIT" || {
            echo "ERROR: Failed to record git commit"
            exit 1
        }
    fi
    
    echo "  Rollback snapshot saved to $ROLLBACK_PATH"
    
    # Keep only the 10 most recent rollbacks (sort by name, not mtime)
    # Using find with -print0 for safety against spaces in names
    find "$ROLLBACK_DIR" -mindepth 1 -maxdepth 1 -type d -name '20*' -print0 \
        | sort -zr \
        | tail -zn +11 \
        | xargs -0r rm -rf --
else
    echo "  No existing binary or assets to back up (first deploy or clean build)."
    ROLLBACK_PATH=""
fi

# ---------- Pull latest code ----------
if git rev-parse --is-inside-work-tree &>/dev/null 2>&1; then
    echo "[2/6] Pulling latest code..."
    if ! git pull --ff-only; then
        echo "ERROR: git pull failed. Aborting deploy."
        exit 1
    fi
else
    echo "[2/6] Not a git repo, skipping pull."
fi

# ---------- Build frontend into temp directory ----------
echo "[3/6] Building frontend..."
npm cache clean --force 2>/dev/null || true
rm -rf node_modules
npm ci

# Build into temp directory to avoid the old server hash pointing at new bytes
# This includes CSS (git-tracked) to ensure the swap is atomic
rm -rf "$FRONTEND_BUILD_TEMP"
mkdir -p "$FRONTEND_BUILD_TEMP/js" "$FRONTEND_BUILD_TEMP/css"

# Copy CSS BEFORE building JS, so we have the post-pull CSS ready for swap
if [ -f "web/static/css/styles.css" ]; then
    cp web/static/css/styles.css "$FRONTEND_BUILD_TEMP/css/" || {
        echo "ERROR: Failed to copy CSS to temp directory"
        exit 1
    }
fi

# Build JS bundles using package.json scripts (single source of truth for build flags)
OUTDIR="$FRONTEND_BUILD_TEMP" npm run build:to

echo "  Frontend built in temp directory."

# ---------- Lint and test ----------
echo "[4/6] Running go vet and tests..."
go vet ./...
go test ./...
echo "  Tests passed."

# ---------- Build backend ----------
echo "[5/6] Building backend..."
CGO_ENABLED=0 go build -ldflags="-s -w" -o "$BIN" ./cmd/server
echo "  Backend built ($(du -h "$BIN" | cut -f1))."

# Validate assets and templates before swapping (fail-fast)
echo "  Validating assets and templates..."
if ! "$BIN" --check-assets 2>&1; then
    echo "ERROR: Asset validation failed. Not swapping files."
    if [ -n "$ROLLBACK_PATH" ]; then
        echo "  To rollback: scripts/rollback.sh $TIMESTAMP"
    fi
    exit 1
fi

# ---------- Swap frontend assets and restart ----------
echo "[6/6] Swapping frontend assets and restarting service..."

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

sudo systemctl restart "$SERVICE"
sleep 2

# Check if service started
if ! sudo systemctl is-active --quiet "$SERVICE"; then
    echo "  ERROR: Service failed to start!"
    sudo systemctl status "$SERVICE" --no-pager
    
    # Auto-rollback on failed start
    if [ -n "$ROLLBACK_PATH" ] && [ -d "$ROLLBACK_PATH" ]; then
        echo ""
        echo "  Attempting automatic rollback to $TIMESTAMP..."
        if "$SCRIPT_DIR/rollback.sh" "$TIMESTAMP"; then
            echo "  Rollback successful, but deploy failed."
        else
            echo "  ERROR: Rollback also failed!"
        fi
    fi
    exit 1
fi

# Health check with increased timeout
echo "  Service started. Running health checks..."
HEALTH_CHECK_TIMEOUT=30
HEALTH_CHECK_INTERVAL=3
HEALTH_CHECKS=0
MAX_HEALTH_CHECKS=$((HEALTH_CHECK_TIMEOUT / HEALTH_CHECK_INTERVAL))

for i in $(seq 1 $MAX_HEALTH_CHECKS); do
    STATUS=$(curl -sf -o /dev/null -w "%{http_code}" "http://127.0.0.1:8080/api/health" 2>/dev/null || echo "000")
    if [ "$STATUS" = "200" ]; then
        echo "  ✓ Health check passed after $((i * HEALTH_CHECK_INTERVAL))s."
        HEALTH_CHECKS=$i
        break
    fi
    
    # Check if service crashed
    if ! sudo systemctl is-active --quiet "$SERVICE"; then
        echo "  ERROR: Service crashed during health check!"
        sudo systemctl status "$SERVICE" --no-pager
        
        # Auto-rollback on crash
        if [ -n "$ROLLBACK_PATH" ] && [ -d "$ROLLBACK_PATH" ]; then
            echo ""
            echo "  Attempting automatic rollback to $TIMESTAMP..."
            if "$SCRIPT_DIR/rollback.sh" "$TIMESTAMP"; then
                echo "  Rollback successful, but deploy failed."
            else
                echo "  ERROR: Rollback also failed!"
            fi
        fi
        exit 1
    fi
    
    if [ "$i" = "$MAX_HEALTH_CHECKS" ]; then
        echo "  ERROR: Health check failed after ${HEALTH_CHECK_TIMEOUT}s (HTTP $STATUS)."
        echo "  Service is running but not responding correctly."
        sudo journalctl -u "$SERVICE" --no-pager -n 50
        
        # Auto-rollback on failed health check
        if [ -n "$ROLLBACK_PATH" ] && [ -d "$ROLLBACK_PATH" ]; then
            echo ""
            echo "  Attempting automatic rollback to $TIMESTAMP..."
            if "$SCRIPT_DIR/rollback.sh" "$TIMESTAMP"; then
                echo "  Rollback successful, but deploy failed."
            else
                echo "  ERROR: Rollback also failed!"
            fi
        fi
        exit 1
    fi
    sleep $HEALTH_CHECK_INTERVAL
done

echo ""
echo "=== Deploy complete ==="
echo ""
