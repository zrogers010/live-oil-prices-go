#!/usr/bin/env bash
#
# One-time script to move legacy rollback directories from web/static/ to
# /home/deploy/liveoilprices-rollbacks/. This should be run ONCE on the
# production server after PR #43 is deployed, before applying the nginx config.
#
# The script refuses to overwrite existing destinations.
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$PROJECT_DIR"

ROLLBACK_DIR="/home/deploy/liveoilprices-rollbacks"

echo ""
echo "=== Move Legacy Rollback Directories ==="
echo "  Project: $PROJECT_DIR"
echo "  Target: $ROLLBACK_DIR"
echo ""

# Create rollback directory if it doesn't exist
mkdir -p "$ROLLBACK_DIR"

# Track if any moves happened
MOVED=0
SKIPPED=0
ERRORS=0

# Move known rollback directories
for dir in web/static/js.rollback-20261016 \
           web/static/js.rollback-20261017 \
           web/static/js.rollback-20261017b; do
    if [ -d "$dir" ]; then
        # Extract timestamp from directory name
        BASENAME=$(basename "$dir")
        # Convert js.rollback-20261016 to 20261016-manual
        TIMESTAMP="${BASENAME##*rollback-}"
        DEST="$ROLLBACK_DIR/${TIMESTAMP}-manual"
        
        if [ -e "$DEST" ]; then
            echo "  SKIP: $dir"
            echo "        Destination already exists: $DEST"
            SKIPPED=$((SKIPPED + 1))
        else
            echo "  MOVE: $dir"
            echo "     → $DEST"
            if mv -n "$dir" "$DEST" 2>/dev/null; then
                MOVED=$((MOVED + 1))
            else
                echo "        ERROR: Move failed"
                ERRORS=$((ERRORS + 1))
            fi
        fi
    fi
done

# Find and move any other *.rollback-* files/directories under web/static
echo ""
echo "Searching for other rollback files..."
find web/static/js web/static/css -name "*.rollback-*" -print0 2>/dev/null | while IFS= read -r -d '' item; do
    BASENAME=$(basename "$item")
    # Try to extract timestamp or use basename
    TEMP="${BASENAME##*rollback-}"
    TIMESTAMP="${TEMP%%[^0-9a-zA-Z-]*}"
    
    # If no timestamp in name, use current date plus basename
    if [ -z "$TIMESTAMP" ] || [ "$TIMESTAMP" = "$BASENAME" ]; then
        TIMESTAMP="$(date +%Y%m%d)-${BASENAME}"
    fi
    
    DEST="$ROLLBACK_DIR/${TIMESTAMP}-manual"
    
    # Add uniqueness suffix if destination exists
    COUNTER=1
    while [ -e "$DEST" ]; do
        DEST="$ROLLBACK_DIR/${TIMESTAMP}-manual-${COUNTER}"
        COUNTER=$((COUNTER + 1))
    done
    
    echo "  MOVE: $item"
    echo "     → $DEST"
    if mv -n "$item" "$DEST" 2>/dev/null; then
        MOVED=$((MOVED + 1))
    else
        echo "        ERROR: Move failed"
        ERRORS=$((ERRORS + 1))
    fi
done

echo ""
echo "=== Summary ==="
echo "  Moved: $MOVED"
echo "  Skipped: $SKIPPED"
echo "  Errors: $ERRORS"
echo ""

if [ $MOVED -gt 0 ]; then
    echo "Rollback directories moved. Contents of $ROLLBACK_DIR:"
    ls -lh "$ROLLBACK_DIR/"
    echo ""
    echo "Verify rollback paths now return 404 (after Go app is deployed):"
    echo "  curl -I https://www.liveoilprices.com/js.rollback-20261017b/app.js"
    echo "  (Expected: HTTP/1.1 404 Not Found)"
fi

if [ $ERRORS -gt 0 ]; then
    exit 1
fi

echo "Done."
echo ""
