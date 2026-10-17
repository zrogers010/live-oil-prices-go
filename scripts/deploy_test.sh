#!/usr/bin/env bash
#
# Test deploy.sh auto-rollback scenarios
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
TEST_DIR=$(mktemp -d)

cleanup() {
    # Kill any mock processes
    pkill -P $$ 2>/dev/null || true
    sleep 0.1
    pkill -9 -P $$ 2>/dev/null || true
    rm -rf "$TEST_DIR"
}

trap cleanup EXIT

echo "Testing deploy.sh rollback scenarios..."
echo "Test directory: $TEST_DIR"

# Create a mock git repository
mkdir -p "$TEST_DIR/repo"
cd "$TEST_DIR/repo"
git init -q
git config user.email "test@example.com"
git config user.name "Test User"

# Create project structure
mkdir -p bin web/templates web/static/js web/static/css frontend/src
mkdir -p "$TEST_DIR/rollbacks"

# Create initial files
echo "package main; func main() {}" > cmd/server/main.go
echo "old-template" > web/templates/index.html
echo "old-js" > web/static/js/app.js
echo "old-css" > web/static/css/styles.css
echo "console.log('frontend')" > frontend/src/index.js
echo '{"scripts":{"build":"mkdir -p dist/js dist/css && cp src/index.js dist/js/app.js && echo \"body{}\" > dist/css/styles.css"}}' > frontend/package.json
mkdir -p frontend/node_modules

# Initial commit
git add -A
git commit -q -m "initial"
INITIAL_COMMIT=$(git rev-parse HEAD)

# Create initial binary
echo "old-binary" > bin/server
chmod +x bin/server

# Mock binaries
cat > "$TEST_DIR/go" <<'EOF'
#!/usr/bin/env bash
# Mock go compiler
case "$1" in
    build)
        # Find the -o flag
        OUTPUT=""
        for ((i=1; i<=$#; i++)); do
            if [ "${!i}" = "-o" ]; then
                ((i++))
                OUTPUT="${!i}"
                break
            fi
        done
        
        if [ -n "$OUTPUT" ]; then
            # Check if we should fail (for asset check failure test)
            if [ -f "$TEST_DIR/fail_asset_check" ]; then
                echo "binary-with-missing-assets" > "$OUTPUT"
            else
                echo "new-binary" > "$OUTPUT"
            fi
            chmod +x "$OUTPUT"
        fi
        exit 0
        ;;
    vet)
        exit 0
        ;;
    test)
        exit 0
        ;;
esac
EOF
chmod +x "$TEST_DIR/go"

cat > "$TEST_DIR/npm" <<'EOF'
#!/usr/bin/env bash
case "$1" in
    ci)
        exit 0
        ;;
    run)
        if [ "$2" = "build" ]; then
            # Build to .build-tmp (deploy.sh's temp location)
            mkdir -p .build-tmp/js .build-tmp/css
            echo "new-js" > .build-tmp/js/app.js
            echo "new-css" > .build-tmp/css/styles.css
        fi
        exit 0
        ;;
esac
EOF
chmod +x "$TEST_DIR/npm"

cat > "$TEST_DIR/sudo" <<EOF
#!/usr/bin/env bash
if [ "\$1" = "systemctl" ]; then
    shift
    "$TEST_DIR/systemctl" "\$@"
else
    "\$@"
fi
EOF
chmod +x "$TEST_DIR/sudo"

cat > "$TEST_DIR/systemctl" <<EOF
#!/usr/bin/env bash
SERVICE_PID_FILE="$TEST_DIR/service.pid"

case "\$1" in
    is-active)
        # Check for crash loop marker
        if [ -f "$TEST_DIR/crash_loop" ]; then
            exit 3
        fi
        
        if [ -f "\$SERVICE_PID_FILE" ] && kill -0 \$(cat "\$SERVICE_PID_FILE") 2>/dev/null; then
            exit 0
        else
            exit 3
        fi
        ;;
    restart|start)
        # Stop old process
        if [ -f "\$SERVICE_PID_FILE" ]; then
            OLD_PID=\$(cat "\$SERVICE_PID_FILE")
            kill "\$OLD_PID" 2>/dev/null || true
            rm -f "\$SERVICE_PID_FILE"
        fi
        # Start mock service
        sleep 99999 &
        echo \$! > "\$SERVICE_PID_FILE"
        exit 0
        ;;
    stop)
        if [ -f "\$SERVICE_PID_FILE" ]; then
            PID=\$(cat "\$SERVICE_PID_FILE")
            kill "\$PID" 2>/dev/null || true
            rm -f "\$SERVICE_PID_FILE"
        fi
        exit 0
        ;;
    status)
        echo "Mock service status"
        exit 0
        ;;
esac
EOF
chmod +x "$TEST_DIR/systemctl"

cat > "$TEST_DIR/curl" <<'EOF'
#!/usr/bin/env bash
# Check for health check failure marker
if [ -f "$TEST_DIR/health_fail" ]; then
    echo "500"
    exit 0
fi
echo "200"
exit 0
EOF
chmod +x "$TEST_DIR/curl"

cat > "$TEST_DIR/journalctl" <<'EOF'
#!/usr/bin/env bash
echo "Mock journal output"
exit 0
EOF
chmod +x "$TEST_DIR/journalctl"

# Create deploy script with test paths
sed -e "s|ROLLBACK_DIR=\"/home/deploy/liveoilprices-rollbacks\"|ROLLBACK_DIR=\"$TEST_DIR/rollbacks\"|g" \
    -e "s|PROJECT_DIR=\$(cd \"\$SCRIPT_DIR/..\" && pwd)|PROJECT_DIR=\"$TEST_DIR/repo\"|g" \
    "$SCRIPT_DIR/deploy.sh" > "$TEST_DIR/deploy-test.sh"
chmod +x "$TEST_DIR/deploy-test.sh"

# Create rollback script with test paths
sed -e "s|ROLLBACK_DIR=\"/home/deploy/liveoilprices-rollbacks\"|ROLLBACK_DIR=\"$TEST_DIR/rollbacks\"|g" \
    -e "s|PROJECT_DIR=.*|PROJECT_DIR=\"$TEST_DIR/repo\"|g" \
    -e "s|cd \"\$PROJECT_DIR\"|# cd suppressed for test|g" \
    "$SCRIPT_DIR/rollback.sh" > "$TEST_DIR/rollback-test.sh"
chmod +x "$TEST_DIR/rollback-test.sh"

export PATH="$TEST_DIR:$PATH"
export HEALTH_CHECK_TIMEOUT=6

cd "$TEST_DIR/repo"

# Start mock service
"$TEST_DIR/systemctl" start liveoilprices
sleep 0.2

echo ""
echo "=== Test (a): Missing asset fails check-assets ==="
touch "$TEST_DIR/fail_asset_check"

# Make a change to trigger a deploy
echo "change1" >> cmd/server/main.go
git add -A
git commit -q -m "change for test a"

INITIAL_BINARY=$(cat bin/server)
INITIAL_COMMIT=$(cat "$TEST_DIR/rollbacks"/*/COMMIT 2>/dev/null | head -1 || echo "$INITIAL_COMMIT")

if bash "$TEST_DIR/deploy-test.sh" 2>&1 | grep -q "Asset validation failed"; then
    # Check binary unchanged
    CURRENT_BINARY=$(cat bin/server)
    if [ "$CURRENT_BINARY" = "$INITIAL_BINARY" ]; then
        echo "✓ Binary unchanged"
    else
        echo "✗ Binary was modified"
        exit 1
    fi
    
    # Check git tree restored
    CURRENT_COMMIT=$(git rev-parse HEAD)
    if [ "$CURRENT_COMMIT" = "$INITIAL_COMMIT" ]; then
        echo "✓ Git tree restored"
    else
        echo "✗ Git tree not restored (expected $INITIAL_COMMIT, got $CURRENT_COMMIT)"
        exit 1
    fi
    
    # Check no server.new left behind
    if [ ! -f bin/server.new ]; then
        echo "✓ Temp binary cleaned up"
    else
        echo "✗ Temp binary not cleaned up"
        exit 1
    fi
    
    echo "✓ Test (a) passed"
else
    echo "✗ Test (a) failed - deploy did not fail on asset check"
    exit 1
fi

rm -f "$TEST_DIR/fail_asset_check"

echo ""
echo "=== Test (b): Health check failure triggers rollback ==="

# Reset to clean state
git reset --hard "$INITIAL_COMMIT" -q
echo "old-binary" > bin/server
echo "old-js" > web/static/js/app.js

# Make a change
echo "change2" >> cmd/server/main.go
git add -A
git commit -q -m "change for test b"

# Deploy should succeed first, then we'll test health check failure
rm -rf "$TEST_DIR/rollbacks"/*
bash "$TEST_DIR/deploy-test.sh" > /dev/null 2>&1

# Now set up for health check failure on next deploy
touch "$TEST_DIR/health_fail"
echo "old-binary" > bin/server
echo "old-js" > web/static/js/app.js
rm -rf "$TEST_DIR/rollbacks"/*
bash "$TEST_DIR/deploy-test.sh" > /dev/null 2>&1  # Create snapshot

echo "change3" >> cmd/server/main.go
git add -A
git commit -q -m "change for test b health fail"

if ! bash "$TEST_DIR/deploy-test.sh" 2>&1 | grep -q "Attempting automatic rollback"; then
    echo "✗ Test (b) failed - no auto-rollback on health check failure"
    exit 1
fi

# Check binary restored
if grep -q "old-binary" bin/server; then
    echo "✓ Binary rolled back"
else
    echo "✗ Binary not rolled back"
    exit 1
fi

# Check assets restored
if grep -q "old-js" web/static/js/app.js; then
    echo "✓ Assets rolled back"
else
    echo "✗ Assets not rolled back"
    exit 1
fi

echo "✓ Test (b) passed"

rm -f "$TEST_DIR/health_fail"

echo ""
echo "=== Test (c): Crash loop (is-active false) triggers rollback ==="

git reset --hard "$INITIAL_COMMIT" -q
echo "old-binary" > bin/server
rm -rf "$TEST_DIR/rollbacks"/*
bash "$TEST_DIR/deploy-test.sh" > /dev/null 2>&1

touch "$TEST_DIR/crash_loop"

echo "change4" >> cmd/server/main.go
git add -A
git commit -q -m "change for test c"

if ! bash "$TEST_DIR/deploy-test.sh" 2>&1 | grep -q "Service failed to start"; then
    echo "✗ Test (c) failed - no error on crash loop"
    exit 1
fi

# Check rollback happened
if grep -q "old-binary" bin/server; then
    echo "✓ Binary rolled back on crash loop"
else
    echo "✗ Binary not rolled back on crash loop"
    exit 1
fi

echo "✓ Test (c) passed"

rm -f "$TEST_DIR/crash_loop"

echo ""
echo "=== Test (d): Successful deploy exits 0 ==="

git reset --hard "$INITIAL_COMMIT" -q
rm -rf "$TEST_DIR/rollbacks"/*

echo "change5" >> cmd/server/main.go
git add -A
git commit -q -m "change for test d"

if bash "$TEST_DIR/deploy-test.sh" > /dev/null 2>&1; then
    echo "✓ Successful deploy exits 0"
    
    # Check files were updated
    if grep -q "new-binary" bin/server; then
        echo "✓ Binary updated"
    else
        echo "✗ Binary not updated"
        exit 1
    fi
    
    if grep -q "new-js" web/static/js/app.js; then
        echo "✓ Assets updated"
    else
        echo "✗ Assets not updated"
        exit 1
    fi
    
    echo "✓ Test (d) passed"
else
    echo "✗ Test (d) failed - successful deploy did not exit 0"
    exit 1
fi

echo ""
echo "=== Test (e): Deploy after rollback (clean detached HEAD) succeeds ==="

# Rollback to a known snapshot
# shellcheck disable=SC2012
SNAPSHOT_TS=$(ls "$TEST_DIR/rollbacks" | head -1)
bash "$TEST_DIR/rollback-test.sh" "$SNAPSHOT_TS" > /dev/null 2>&1

# Verify we're in detached HEAD
if git symbolic-ref HEAD &>/dev/null; then
    echo "✗ Not in detached HEAD after rollback"
    exit 1
fi

echo "  In detached HEAD at $(git rev-parse HEAD)"

# Now try to deploy - should detect rollback state and succeed
echo "change6" >> cmd/server/main.go
git add -A
git commit -q -m "change for test e"

if bash "$TEST_DIR/deploy-test.sh" 2>&1 | grep -q "Detected rollback state"; then
    echo "✓ Rollback state detected"
    
    # Should be back on main branch
    if git symbolic-ref HEAD 2>/dev/null | grep -q "refs/heads/main"; then
        echo "✓ Returned to main branch"
    else
        echo "✗ Not on main branch after deploy"
        git symbolic-ref HEAD || echo "(still detached)"
        exit 1
    fi
    
    # Deploy should succeed
    if grep -q "new-binary" bin/server; then
        echo "✓ Deploy after rollback succeeded"
    else
        echo "✗ Deploy after rollback failed"
        exit 1
    fi
    
    echo "✓ Test (e) passed"
else
    echo "✗ Test (e) failed - rollback state not detected"
    exit 1
fi

echo ""
echo "✓ All deploy tests passed!"
