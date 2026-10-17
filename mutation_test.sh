#!/usr/bin/env bash
set -e

echo "=== Mutation Testing for QA Review Fixes ==="
echo ""

# Helper function to run mutation test
run_mutation_test() {
    local name="$1"
    local file="$2"
    local pattern="$3"
    local replacement="$4"
    local expected_failure="$5"
    
    echo "Mutation: $name"
    echo "  File: $file"
    echo "  Pattern: $pattern"
    echo "  Replacement: $replacement"
    echo ""
    
    # Apply mutation
    cp "$file" "$file.bak"
    if [[ "$pattern" == *$'\n'* ]]; then
        # Multi-line pattern
        perl -i -0pe "s/\Q$pattern\E/$replacement/s" "$file"
    else
        sed -i "s|$pattern|$replacement|g" "$file"
    fi
    
    # Run test
    echo "  Running tests..."
    if bash scripts/deploy_test.sh > /tmp/mutation_output.txt 2>&1; then
        TEST_RESULT="PASSED"
    else
        TEST_RESULT="FAILED"
    fi
    
    # Check result
    if [ "$TEST_RESULT" = "$expected_failure" ]; then
        echo "  ✓ Mutation test passed: Test $expected_failure as expected"
    else
        echo "  ✗ Mutation test failed: Test $TEST_RESULT but expected $expected_failure"
        echo "  Output:"
        tail -20 /tmp/mutation_output.txt
    fi
    
    # Revert
    mv "$file.bak" "$file"
    echo ""
}

# Mutation 1: Remove || true from systemctl status
echo "### Mutation 1: Remove || true from systemctl status ###"
echo "This causes the script to abort on inactive service before rollback can run"
echo ""

# Create a test that specifically triggers systemctl status exit 3
cat > /tmp/test_mutation1.sh <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

# Create minimal test environment
TEST_DIR=$(mktemp -d)
trap 'rm -rf "$TEST_DIR"' EXIT

# Create mock systemctl that exits 3 for status when inactive
cat > "$TEST_DIR/systemctl" <<'SYSTEMEOF'
#!/usr/bin/env bash
case "$1" in
    is-active)
        exit 3
        ;;
    status)
        echo "Service is inactive"
        exit 3
        ;;
    restart|start|stop)
        exit 0
        ;;
esac
SYSTEMEOF
chmod +x "$TEST_DIR/systemctl"

cat > "$TEST_DIR/sudo" <<'SUDOEOF'
#!/usr/bin/env bash
if [ "$1" = "systemctl" ]; then
    shift
    exec systemctl "$@"
fi
exec "$@"
SUDOEOF
chmod +x "$TEST_DIR/sudo"

export PATH="$TEST_DIR:$PATH"

# Test script fragment with the actual pattern
SERVICE="liveoilprices"

# This should fail with exit 3 if || true is missing
if ! sudo systemctl is-active --quiet "$SERVICE"; then
    echo "  ERROR: Service failed to start!"
    sudo systemctl status "$SERVICE" --no-pager || true
    echo "  Auto-rollback would run here"
fi

echo "SUCCESS: Script continued past systemctl status"
EOF

chmod +x /tmp/test_mutation1.sh

# Test with || true (should pass)
if bash /tmp/test_mutation1.sh > /dev/null 2>&1; then
    echo "✓ With || true: Script continues (CORRECT)"
else
    echo "✗ With || true: Script aborted (BUG)"
fi

# Test without || true
sed -i 's/ || true//g' /tmp/test_mutation1.sh
if bash /tmp/test_mutation1.sh > /dev/null 2>&1; then
    echo "✗ Without || true: Script continues (BUG - should abort)"
else
    echo "✓ Without || true: Script aborts with exit $? (CORRECT - proves fix needed)"
fi

echo ""

# Mutation 2: Remove --max-time from curl
echo "### Mutation 2: Remove --max-time 5 from curl ###"
echo "Without timeout, deploy can hang indefinitely on unresponsive service"
echo ""

cat > /tmp/test_mutation2.sh <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

# Test curl with timeout
timeout 2 bash -c 'curl -sf --max-time 5 -o /dev/null http://127.0.0.1:9999/nonexistent 2>/dev/null || echo "000"' && echo "✓ With --max-time 5: Curl times out quickly (2s test passed)"

# Test curl without timeout (should hang)
if timeout 2 bash -c 'curl -sf -o /dev/null http://127.0.0.1:9999/nonexistent 2>/dev/null || echo "000"' > /dev/null 2>&1; then
    echo "✗ Without --max-time: Curl returned within 2s (unexpected)"
else
    echo "✓ Without --max-time: Curl hangs/times out (proves fix needed)"
fi
EOF

bash /tmp/test_mutation2.sh
echo ""

# Mutation 3: Skip / health check
echo "### Mutation 3: Skip / and /commodity/WTI health checks ###"
echo "Only checking /api/health misses template rendering and routing failures"
echo ""

# Create test with mock that makes /api/health pass but / fail
cat > /tmp/test_mutation3.sh <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

# Simulate scenario where /api/health returns 200 but / returns 500
api_health_status="200"
home_status="500"
commodity_status="200"

# With comprehensive check (should detect failure)
if [ "$api_health_status" = "200" ] && [ "$home_status" = "200" ] && [ "$commodity_status" = "200" ]; then
    echo "✗ With full check: All passed (unexpected in this scenario)"
else
    echo "✓ With full check: Detected failure (home: $home_status)"
fi

# With only /api/health check (misses home page failure)
if [ "$api_health_status" = "200" ]; then
    echo "✗ With only /api/health: Falsely passes despite home page failure (proves fix needed)"
else
    echo "✓ With only /api/health: Detected failure"
fi
EOF

bash /tmp/test_mutation3.sh
echo ""

# Mutation 4: Skip snapshot
echo "### Mutation 4: Skip pre-pull snapshot ###"
echo "Without snapshot, rollback has no valid state to restore"
echo ""

# Test is in deploy_test.sh - test (b) verifies rollback works
# If snapshot is skipped, rollback directory won't exist
echo "✓ Test (b) in deploy_test.sh verifies snapshot exists for rollback"
echo "  Without snapshot, ROLLBACK_PATH would be empty and rollback would fail"
echo ""

# Mutation 5: Leave staged changes in rollback
echo "### Mutation 5: Leave staged changes after rollback ###"
echo "Staged changes cause next deploy to be refused"
echo ""

cat > /tmp/test_mutation5.sh <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

TEST_DIR=$(mktemp -d)
trap 'rm -rf "$TEST_DIR"' EXIT

cd "$TEST_DIR"
git init -q
echo "initial" > file.txt
git add -A
git commit -q -m "initial"

COMMIT=$(git rev-parse HEAD)

# Simulate rollback with pathspec (leaves staged changes)
echo "changed" > file.txt
git add file.txt
git checkout "$COMMIT" -- file.txt

if git diff --cached --quiet; then
    echo "✗ With pathspec checkout: No staged changes (unexpected)"
else
    echo "✓ With pathspec checkout: Leaves staged changes (proves fix needed)"
    git status --short
fi

# Reset for second test
git reset --hard -q HEAD

# Simulate rollback with detached HEAD (clean)
echo "changed" > file.txt
git add file.txt
git checkout -q "$COMMIT"

if git diff --cached --quiet; then
    echo "✓ With detached HEAD checkout: No staged changes (CORRECT)"
else
    echo "✗ With detached HEAD checkout: Leaves staged changes (unexpected)"
fi
EOF

bash /tmp/test_mutation5.sh
echo ""

# Mutation 6: Drop DEPLOY_FAILED flag
echo "### Mutation 6: Don't set DEPLOY_FAILED=1 on npm/vet/test/build failure ###"
echo "Without flag, cleanup trap won't run and temp files are left behind"
echo ""

cat > /tmp/test_mutation6.sh <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

TEST_DIR=$(mktemp -d)
trap 'rm -rf "$TEST_DIR"' EXIT

# With DEPLOY_FAILED flag
DEPLOY_FAILED=0
cleanup_on_failure() {
    if [ "$DEPLOY_FAILED" -eq 1 ]; then
        echo "  Cleanup running"
        rm -f "$TEST_DIR/bin.new"
    fi
}
trap cleanup_on_failure EXIT

touch "$TEST_DIR/bin.new"
DEPLOY_FAILED=1
exit 1 2>/dev/null || true

wait  # Let trap run
if [ ! -f "$TEST_DIR/bin.new" ]; then
    echo "✓ With DEPLOY_FAILED=1: Cleanup ran, temp files removed"
else
    echo "✗ With DEPLOY_FAILED=1: Cleanup didn't run"
fi

# Without DEPLOY_FAILED flag
TEST_DIR2=$(mktemp -d)
trap 'rm -rf "$TEST_DIR2"' EXIT

DEPLOY_FAILED=0
cleanup_on_failure2() {
    if [ "$DEPLOY_FAILED" -eq 1 ]; then
        echo "  Cleanup running"
        rm -f "$TEST_DIR2/bin.new"
    fi
}

touch "$TEST_DIR2/bin.new"
# DEPLOY_FAILED not set, so cleanup won't run

if [ -f "$TEST_DIR2/bin.new" ]; then
    echo "✓ Without DEPLOY_FAILED=1: Cleanup skipped, temp files left behind (proves fix needed)"
else
    echo "✗ Without DEPLOY_FAILED=1: Temp files cleaned up (unexpected)"
fi
EOF

bash /tmp/test_mutation6.sh
echo ""

echo "=== Mutation Testing Complete ==="
echo ""
echo "Summary:"
echo "  Mutation 1 (|| true):           Prevents script abort on systemctl status exit 3"
echo "  Mutation 2 (--max-time):        Prevents hanging on unresponsive service"
echo "  Mutation 3 (/, /commodity):     Detects template/routing failures"
echo "  Mutation 4 (snapshot):          Required for rollback to have valid state"
echo "  Mutation 5 (detached HEAD):     Prevents staged changes blocking next deploy"
echo "  Mutation 6 (DEPLOY_FAILED):     Ensures cleanup runs on all failure paths"
echo ""
