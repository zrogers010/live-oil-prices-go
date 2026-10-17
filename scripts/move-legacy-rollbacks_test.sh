#!/usr/bin/env bash
#
# Test move-legacy-rollbacks.sh behavior
#

set -euo pipefail

echo "Testing move-legacy-rollbacks.sh..."

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
TEST_DIR=$(mktemp -d)
OUTPUT1=$(mktemp)
OUTPUT2=$(mktemp)
trap 'rm -rf "$TEST_DIR" "$OUTPUT1" "$OUTPUT2"' EXIT

# Create a mock project structure
mkdir -p "$TEST_DIR/web/static/js"
mkdir -p "$TEST_DIR/web/static/css"
mkdir -p "$TEST_DIR/scripts"

# Create a modified version of the script for testing
# Replace hardcoded paths with test directory paths
sed -e "s|ROLLBACK_DIR=\"/home/deploy/liveoilprices-rollbacks\"|ROLLBACK_DIR=\"$TEST_DIR/rollbacks\"|g" \
    -e "s|PROJECT_DIR=.*|PROJECT_DIR=\"$TEST_DIR\"|g" \
    -e "s|cd \"\$PROJECT_DIR\"|# cd suppressed for test|g" \
    "$SCRIPT_DIR/move-legacy-rollbacks.sh" > "$TEST_DIR/scripts/move-legacy-rollbacks.sh"
chmod +x "$TEST_DIR/scripts/move-legacy-rollbacks.sh"

cd "$TEST_DIR"

echo ""
echo "=== Test 1: Normal move (no collisions) ==="

# Create legacy rollback directories
mkdir -p "web/static/js.rollback-20261016"
echo "old js" > "web/static/js.rollback-20261016/app.js"

# Run script
if bash scripts/move-legacy-rollbacks.sh > "$OUTPUT1" 2>&1; then
    echo "✓ Script exited 0 (success)"
else
    echo "✗ Script exited non-zero unexpectedly"
    cat "$OUTPUT1"
    exit 1
fi

# Verify moved
if [ -d "rollbacks/legacy-20261016" ]; then
    echo "✓ Directory moved to rollbacks/legacy-20261016"
else
    echo "✗ Directory not moved"
    exit 1
fi

# Verify source removed
if [ ! -d "web/static/js.rollback-20261016" ]; then
    echo "✓ Source directory removed"
else
    echo "✗ Source directory still exists"
    exit 1
fi

echo ""
echo "=== Test 2: SKIP path (destination collision) ==="

# Create another rollback directory
mkdir -p "web/static/js.rollback-20261017"
echo "newer js" > "web/static/js.rollback-20261017/app.js"

# Pre-create destination to simulate collision
mkdir -p "rollbacks/legacy-20261017"
echo "existing content" > "rollbacks/legacy-20261017/app.js"

# Run script - should exit 1 due to SKIP
if bash scripts/move-legacy-rollbacks.sh > "$OUTPUT2" 2>&1; then
    echo "✗ Script exited 0 (should have failed on SKIP)"
    cat "$OUTPUT2"
    exit 1
else
    EXIT_CODE=$?
    if [ "$EXIT_CODE" = 1 ]; then
        echo "✓ Script exited 1 on SKIP (as required)"
    else
        echo "✗ Script exited $EXIT_CODE (expected 1)"
        exit 1
    fi
fi

# Verify output mentions SKIP
if grep -q "SKIP" "$OUTPUT2" && grep -q "Destination already exists" "$OUTPUT2"; then
    echo "✓ Output mentions SKIP and collision"
else
    echo "✗ Output doesn't mention SKIP properly"
    cat "$OUTPUT2"
    exit 1
fi

# Verify source NOT moved (collision prevented overwrite)
if [ -d "web/static/js.rollback-20261017" ]; then
    echo "✓ Source directory preserved (not overwritten)"
else
    echo "✗ Source directory was removed despite collision"
    exit 1
fi

# Verify destination unchanged
if [ -f "rollbacks/legacy-20261017/app.js" ]; then
    CONTENT=$(cat "rollbacks/legacy-20261017/app.js")
    if [ "$CONTENT" = "existing content" ]; then
        echo "✓ Destination unchanged (collision prevented overwrite)"
    else
        echo "✗ Destination was overwritten"
        exit 1
    fi
else
    echo "✗ Destination file missing"
    exit 1
fi

echo ""
echo "✓ All move-legacy-rollbacks.sh tests passed!"
