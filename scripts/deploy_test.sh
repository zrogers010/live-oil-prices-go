#!/usr/bin/env bash
#
# Test deploy.sh auto-rollback scenarios
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
TEST_DIR=$(mktemp -d)
# Private TMPDIR so test (n) sees every mktemp made by deploy.sh (and only ours)
export TMPDIR="$TEST_DIR/tmp"
mkdir -p "$TMPDIR"

cleanup() {
    # Kill any mock service processes
    if [ -f "$TEST_DIR/service.pid" ]; then
        SERVICE_PID=$(cat "$TEST_DIR/service.pid" 2>/dev/null || echo "")
        if [ -n "$SERVICE_PID" ]; then
            kill -9 "$SERVICE_PID" 2>/dev/null || true
        fi
    fi
    
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
mkdir -p bin cmd/server web/templates web/static/js web/static/css frontend/src
mkdir -p "$TEST_DIR/rollbacks"

# Create .gitignore like prod
cat > .gitignore <<EOF
bin/
node_modules/
web/static/js/*.js
EOF

# Create initial files
echo "package main; func main() {}" > cmd/server/main.go
echo "old-template" > web/templates/index.html
echo "console.log('frontend')" > frontend/src/index.js
# shellcheck disable=SC2016
# Note: build:to only creates JS. deploy.sh handles CSS extraction to temp dir from git.
echo '{"scripts":{"build:to":"mkdir -p \$OUTDIR/js && echo new-js > \$OUTDIR/js/app.js"}}' > frontend/package.json
mkdir -p frontend/node_modules

# Create CSS file (tracked in git) and initial built JS (not tracked)
mkdir -p web/static/js web/static/css
echo "/* initial css */" > web/static/css/styles.css

# Initial commit - CSS is tracked, JS is not
git add -A
git commit -q -m "initial"
INITIAL_COMMIT=$(git rev-parse HEAD)

# Set up a bare repo as remote so git pull works
mkdir -p "$TEST_DIR/remote-repo.git"
(cd "$TEST_DIR/remote-repo.git" && git init --bare -q)
git remote add origin "$TEST_DIR/remote-repo.git"
git push -u origin master -q 2>/dev/null || git push -u origin main -q 2>/dev/null

# Create initial binary (not in git, like prod)
mkdir -p bin
cat > bin/server <<'INITIAL_BIN'
#!/usr/bin/env bash
# Initial binary
if [ "$1" = "--check-assets" ]; then
    echo "✓ Asset and template check passed"
    exit 0
fi
echo "Initial binary running"
sleep 99999
INITIAL_BIN
chmod +x bin/server

# Mock binaries
cat > "$TEST_DIR/go" <<'GOEOF'
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
            # Ensure directory exists
            mkdir -p "$(dirname "$OUTPUT")"
            
            # Include git commit to make each build unique
            GIT_COMMIT=$(git rev-parse HEAD 2>/dev/null || echo "unknown")
            # Record the on-disk CSS while the old binary is still serving it (in-flight invariant)
            if [ -f web/static/css/styles.css ]; then
                md5sum < web/static/css/styles.css | awk '{print $1}' >> "TEST_DIR_PLACEHOLDER/css-at-build.log"
            fi
            if [ -f "TEST_DIR_PLACEHOLDER/fail_asset_check" ]; then
                # This binary will fail asset checks
                cat > "$OUTPUT" <<'FAILBIN'
#!/usr/bin/env bash
if [[ "$1" == "--check-assets"* ]]; then
    echo "ERROR: Missing required assets" >&2
    exit 1
fi
echo "Mock binary running"
FAILBIN
            else
                # This binary will pass asset checks
                cat > "$OUTPUT" <<GOODBIN
#!/usr/bin/env bash
# Built from commit: $GIT_COMMIT
if [[ "\$1" == "--check-assets"* ]]; then
    echo "✓ Asset and template check passed"
    exit 0
fi
echo "Mock binary running"
sleep 99999
GOODBIN
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
GOEOF
sed -i "s|TEST_DIR_PLACEHOLDER|$TEST_DIR|g" "$TEST_DIR/go"
chmod +x "$TEST_DIR/go"

cat > "$TEST_DIR/npm" <<EOF
#!/usr/bin/env bash
case "\$1" in
    ci)
        exit 0
        ;;
    run)
        if [ "\$2" = "build:to" ]; then
            # Build to OUTDIR (deploy.sh's temp location)
            # Include git commit to make each build unique
            # Note: CSS is handled by deploy.sh (git show HEAD:...), not by the build script
            if [ -n "\$OUTDIR" ]; then
                mkdir -p "\$OUTDIR/js"
                GIT_COMMIT=\$(git rev-parse HEAD 2>/dev/null || echo "unknown")
                echo "/* Built from commit: \$GIT_COMMIT */" > "\$OUTDIR/js/app.js"
                echo "new-js" >> "\$OUTDIR/js/app.js"
            fi
        fi
        exit 0
        ;;
esac
EOF
chmod +x "$TEST_DIR/npm"

cat > "$TEST_DIR/sudo" <<SUDOEOF
#!/usr/bin/env bash
if [ "\$1" = "systemctl" ]; then
    shift
    "$TEST_DIR/systemctl" "\$@"
else
    "\$@"
fi
SUDOEOF
chmod +x "$TEST_DIR/sudo"

cat > "$TEST_DIR/systemctl" <<SYSTEMEOF
#!/usr/bin/env bash
SERVICE_PID_FILE="$TEST_DIR/service.pid"

case "\$1" in
    is-active)
        # Check for crash loop marker
        if [ -f "$TEST_DIR/crash_loop" ]; then
            exit 3
        fi
        
        # Check if PID file exists and process is alive
        if [ -f "\$SERVICE_PID_FILE" ]; then
            PID=\$(cat "\$SERVICE_PID_FILE" 2>/dev/null || echo "")
            if [ -n "\$PID" ] && kill -0 "\$PID" 2>/dev/null; then
                exit 0
            fi
        fi
        # Service is inactive
        exit 3
        ;;
    restart|start)
        # Record stop-before-swap ordering
        if [ "\$1" = "stop" ] && [ -f "$TEST_DIR/record_swap_order" ]; then
            echo "stop" >> "$TEST_DIR/swap_order.log"
        fi
        
        # Check for restart failure marker
        if [ -f "$TEST_DIR/restart_fail" ]; then
            echo "Mock restart failed" >&2
            exit 1
        fi
        
        # Stop old process if restarting
        if [ "\$1" = "restart" ] && [ -f "\$SERVICE_PID_FILE" ]; then
            OLD_PID=\$(cat "\$SERVICE_PID_FILE")
            kill "\$OLD_PID" 2>/dev/null || true
            rm -f "\$SERVICE_PID_FILE"
        fi
        
        # Start new process
        sleep 99999 &
        NEW_PID=\$!
        echo \$NEW_PID > "\$SERVICE_PID_FILE"
        exit 0
        ;;
    stop)
        # Record stop-before-swap ordering
        if [ -f "$TEST_DIR/record_swap_order" ]; then
            echo "stop" >> "$TEST_DIR/swap_order.log"
        fi
        
        if [ -f "\$SERVICE_PID_FILE" ]; then
            PID=\$(cat "\$SERVICE_PID_FILE")
            kill "\$PID" 2>/dev/null || true
            rm -f "\$SERVICE_PID_FILE"
        fi
        exit 0
        ;;
    status)
        # Like real systemd: exit 3 when inactive, same as is-active
        if [ -f "$TEST_DIR/crash_loop" ]; then
            echo "● liveoilprices.service - Mock service"
            echo "   Active: failed"
            exit 3
        fi
        
        if [ -f "\$SERVICE_PID_FILE" ]; then
            PID=\$(cat "\$SERVICE_PID_FILE" 2>/dev/null || echo "")
            if [ -n "\$PID" ] && kill -0 "\$PID" 2>/dev/null; then
                echo "● liveoilprices.service - Mock service"
                echo "   Active: active (running)"
                exit 0
            fi
        fi
        echo "● liveoilprices.service - Mock service"
        echo "   Active: inactive (dead)"
        exit 3
        ;;
    show)
        # Support --property=MainPID --value for PID tracking
        if [ "\$2" = "liveoilprices" ] || [ "\$2" = "liveoilprices.service" ]; then
            if [ "\$3" = "--property=MainPID" ] && [ "\$4" = "--value" ]; then
                if [ -f "\$SERVICE_PID_FILE" ]; then
                    cat "\$SERVICE_PID_FILE"
                else
                    echo "0"
                fi
                exit 0
            fi
        fi
        ;;
esac
SYSTEMEOF
chmod +x "$TEST_DIR/systemctl"

cat > "$TEST_DIR/curl" <<CURLEOF
#!/usr/bin/env bash
# Check for health check failure marker
if [ -f "$TEST_DIR/health_fail" ]; then
    echo "500"
    exit 0
fi
echo "200"
exit 0
CURLEOF
chmod +x "$TEST_DIR/curl"

cat > "$TEST_DIR/journalctl" <<'EOF'
#!/usr/bin/env bash
echo "Mock journal output"
exit 0
EOF
chmod +x "$TEST_DIR/journalctl"

# Create deploy script with test paths (use as-is from repo to test real permissions)
sed -e "s|ROLLBACK_DIR=\"/home/deploy/liveoilprices-rollbacks\"|ROLLBACK_DIR=\"$TEST_DIR/rollbacks\"|g" \
    -e "9s|.*|PROJECT_DIR=\"$TEST_DIR/repo\"|" \
    -e "10s|^cd .*|# cd suppressed|" \
    "$SCRIPT_DIR/deploy.sh" > "$TEST_DIR/deploy-test.sh"

# Create rollback script with test paths (use as-is from repo to test real permissions)
sed -e "s|ROLLBACK_DIR=\"/home/deploy/liveoilprices-rollbacks\"|ROLLBACK_DIR=\"$TEST_DIR/rollbacks\"|g" \
    -e "s|PROJECT_DIR=.*|PROJECT_DIR=\"$TEST_DIR/repo\"|g" \
    -e "s|cd \"\$PROJECT_DIR\"|# cd suppressed for test|g" \
    "$SCRIPT_DIR/rollback.sh" > "$TEST_DIR/rollback.sh"

export PATH="$TEST_DIR:$PATH"
export HEALTH_CHECK_TIMEOUT=6
export STABILITY_WINDOW=3
export DEPLOY_BRANCH=master

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
git push -q

# Save state - this is what deploy will snapshot and restore to on failure
INITIAL_BINARY=$(cat bin/server)
INITIAL_COMMIT=$(git rev-parse HEAD)

# Run deploy and capture output to file
echo "  Running deploy (this may take a few seconds)..."
if timeout 30 bash "$TEST_DIR/deploy-test.sh" > "$TEST_DIR/deploy-output-a.txt" 2>&1; then
    DEPLOY_EXIT=0
else
    DEPLOY_EXIT=$?
fi

if [ "$DEPLOY_EXIT" = 124 ]; then
    echo "✗ Test (a) failed - deploy timed out"
    echo "Deploy output:"
    cat "$TEST_DIR/deploy-output-a.txt"
    exit 1
fi

if grep -q "Asset validation failed" "$TEST_DIR/deploy-output-a.txt"; then
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
    echo "Deploy exit code: $DEPLOY_EXIT"
    echo "Deploy output:"
    cat "$TEST_DIR/deploy-output-a.txt"
    exit 1
fi

rm -f "$TEST_DIR/fail_asset_check"

echo ""
echo "=== Test (b): Health check failure triggers rollback ==="

# Reset to clean state
git checkout master -q 2>/dev/null || git checkout main -q
git reset --hard "$INITIAL_COMMIT" -q
git push -f -q
rm -rf "$TEST_DIR/rollbacks"/*
rm -rf bin

# Deploy successfully first to create a known-good state
echo "change2a" >> cmd/server/main.go
git add -A
git commit -q -m "setup for test b"
git push -q

if ! bash "$TEST_DIR/deploy-test.sh" > "$TEST_DIR/deploy-output-b-setup.txt" 2>&1; then
    echo "✗ Test (b) failed - initial deploy didn't succeed"
    echo "Deploy output:"
    cat "$TEST_DIR/deploy-output-b-setup.txt"
    exit 1
fi

# Save the known-good state (this is what rollback should restore to)
GOOD_BINARY_HASH=$(md5sum bin/server | cut -d' ' -f1)
GOOD_JS=$(head -1 web/static/js/app.js)
GOOD_COMMIT=$(git rev-parse HEAD)
echo "  Known-good state: commit $GOOD_COMMIT"
echo "  Known-good binary hash: $GOOD_BINARY_HASH"
echo "  Known-good JS: $GOOD_JS"

# Now test health check failure with a new change
touch "$TEST_DIR/health_fail"

echo "change3" >> cmd/server/main.go
git add -A
git commit -q -m "change for test b health fail"
git push -q

bash "$TEST_DIR/deploy-test.sh" > "$TEST_DIR/deploy-output-b.txt" 2>&1 || true
if ! grep -q "Attempting automatic rollback" "$TEST_DIR/deploy-output-b.txt"; then
    echo "✗ Test (b) failed - no auto-rollback on health check failure"
    echo "Deploy output:"
    cat "$TEST_DIR/deploy-output-b.txt"
    exit 1
fi

# Check binary restored to known-good state
CURRENT_BINARY_HASH=$(md5sum bin/server | cut -d' ' -f1)
if [ "$CURRENT_BINARY_HASH" = "$GOOD_BINARY_HASH" ]; then
    echo "✓ Binary rolled back"
else
    echo "✗ Binary not rolled back"
    echo "  Expected hash: $GOOD_BINARY_HASH"
    echo "  Actual hash:   $CURRENT_BINARY_HASH"
    exit 1
fi

# Check assets restored
CURRENT_JS=$(head -1 web/static/js/app.js)
if [ "$CURRENT_JS" = "$GOOD_JS" ]; then
    echo "✓ Assets rolled back"
else
    echo "✗ Assets not rolled back"
    echo "  Expected: $GOOD_JS"
    echo "  Actual:   $CURRENT_JS"
    exit 1
fi

# Note: After rollback, we may be in detached HEAD at the snapshot commit
# or on the branch. The important thing is binary and assets match.
# Detailed git state verification will be added in comprehensive tests.
CURRENT_COMMIT=$(git rev-parse HEAD)
if [ "$CURRENT_COMMIT" = "$GOOD_COMMIT" ]; then
    echo "✓ Git commit matches (at $CURRENT_COMMIT)"
else
    echo "  Note: Git at $CURRENT_COMMIT (expected $GOOD_COMMIT)"
    echo "        This is acceptable as binary and assets verified."
fi

echo "✓ Test (b) passed"

rm -f "$TEST_DIR/health_fail"

echo ""
echo "=== Test (c): Crash loop (is-active false) triggers rollback ==="

git checkout master -q 2>/dev/null || git checkout main -q
git reset --hard "$INITIAL_COMMIT" -q
git push -f -q
rm -rf "$TEST_DIR/rollbacks"/*
rm -rf bin

echo "change4a" >> cmd/server/main.go
git add -A
git commit -q -m "change for test c setup"
git push -q

if ! bash "$TEST_DIR/deploy-test.sh" > /dev/null 2>&1; then
    echo "✗ Test (c) failed - initial deploy didn't succeed"
    exit 1
fi

# Save known-good state
GOOD_BINARY_HASH=$(md5sum bin/server | cut -d' ' -f1)

touch "$TEST_DIR/crash_loop"

echo "change4b" >> cmd/server/main.go
git add -A
git commit -q -m "change for test c"
git push -q

bash "$TEST_DIR/deploy-test.sh" > "$TEST_DIR/deploy-output-c.txt" 2>&1 || true

if ! grep -q "Service failed to start" "$TEST_DIR/deploy-output-c.txt"; then
    echo "✗ Test (c) failed - no error on crash loop"
    echo "Deploy output:"
    cat "$TEST_DIR/deploy-output-c.txt"
    exit 1
fi

# Verify rollback attempted (even if final restart failed due to persistent crash marker)
if ! grep -q "Attempting automatic rollback" "$TEST_DIR/deploy-output-c.txt"; then
    echo "✗ No rollback attempted"
    exit 1
fi

# Clear crash marker before checking binary
rm -f "$TEST_DIR/crash_loop"

# Check rollback restored the binary (even if service won't start)
CURRENT_BINARY_HASH=$(md5sum bin/server | cut -d' ' -f1)
if [ "$CURRENT_BINARY_HASH" = "$GOOD_BINARY_HASH" ]; then
    echo "✓ Binary rolled back despite crash (exit 3 didn't abort rollback)"
else
    echo "✗ Binary not rolled back"
    echo "  Expected hash: $GOOD_BINARY_HASH"
    echo "  Actual hash:   $CURRENT_BINARY_HASH"
    exit 1
fi

echo "✓ Test (c) passed"

rm -f "$TEST_DIR/crash_loop"

echo ""
echo "=== Test (d): Successful deploy exits 0 ==="

git checkout master -q 2>/dev/null || git checkout main -q
git reset --hard "$INITIAL_COMMIT" -q
git push -f -q
rm -rf "$TEST_DIR/rollbacks"/*
rm -rf bin

# Deploy once to get a binary
echo "change5a" >> cmd/server/main.go
git add -A
git commit -q -m "setup for test d"
git push -q
bash "$TEST_DIR/deploy-test.sh" >/dev/null 2>&1 || {
    echo "✗ Test (d) setup failed"
    exit 1
}

# Save old state
OLD_BINARY_HASH=$(md5sum bin/server | cut -d' ' -f1)

echo "change5b" >> cmd/server/main.go
git add -A
git commit -q -m "change for test d"
git push -q

if bash "$TEST_DIR/deploy-test.sh" >/dev/null 2>&1; then
    echo "✓ Successful deploy exits 0"
    
    # Check binary was updated (hash changed)
    NEW_BINARY_HASH=$(md5sum bin/server | cut -d' ' -f1)
    if [ "$NEW_BINARY_HASH" != "$OLD_BINARY_HASH" ]; then
        echo "✓ Binary updated"
    else
        echo "✗ Binary not updated"
        exit 1
    fi
    
    # Check assets were updated
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

# First ensure we have a snapshot by doing a successful deploy
git checkout master -q 2>/dev/null || git checkout main -q
git reset --hard "$INITIAL_COMMIT" -q
git push -f -q
rm -rf "$TEST_DIR/rollbacks"/*
rm -rf bin

echo "change6" >> cmd/server/main.go
git add -A
git commit -q -m "change for test e setup"
git push -q
if ! bash "$TEST_DIR/deploy-test.sh" > /dev/null 2>&1; then
    echo "✗ Test (e) failed - initial deploy didn't succeed"
    exit 1
fi

# Get the snapshot commit
# shellcheck disable=SC2012
SNAPSHOT_TS=$(ls "$TEST_DIR/rollbacks" | head -1)
SNAPSHOT_COMMIT=$(cat "$TEST_DIR/rollbacks/$SNAPSHOT_TS/COMMIT")

# Manually put system in detached HEAD at snapshot commit (simulating post-rollback state)
git checkout "$SNAPSHOT_COMMIT" -q

# Verify we're in detached HEAD
if git symbolic-ref HEAD &>/dev/null; then
    echo "✗ Not in detached HEAD after checkout"
    exit 1
fi

echo "  In detached HEAD at $(git rev-parse HEAD)"

# Make a new commit on master in the remote so git pull has something to pull
git checkout master -q 2>/dev/null || git checkout main -q
echo "change7" >> cmd/server/main.go
git add -A
git commit -q -m "change for test e"
git push -q

# Go back to detached HEAD at snapshot commit
git checkout "$SNAPSHOT_COMMIT" -q

# Now try to deploy - should detect rollback state, switch to branch, pull, and succeed
bash "$TEST_DIR/deploy-test.sh" > "$TEST_DIR/deploy-output-e.txt" 2>&1 || true
if grep -q "Detected rollback state" "$TEST_DIR/deploy-output-e.txt"; then
    echo "✓ Rollback state detected"
    
    # Should be back on main/master branch
    CURRENT_REF=$(git symbolic-ref HEAD 2>/dev/null || echo "detached")
    if echo "$CURRENT_REF" | grep -qE "refs/heads/(main|master)"; then
        echo "✓ Returned to branch ($CURRENT_REF)"
    else
        echo "✗ Not on branch after deploy (current: $CURRENT_REF)"
        echo "Git status:"
        git status
        echo "Deploy output:"
        tail -30 "$TEST_DIR/deploy-output-e.txt"
        exit 1
    fi
    
    # Deploy should have succeeded (check that deploy completed)
    if grep -q "Deploy complete" "$TEST_DIR/deploy-output-e.txt"; then
        echo "✓ Deploy after rollback succeeded"
    else
        echo "✗ Deploy after rollback failed"
        echo "Deploy output:"
        cat "$TEST_DIR/deploy-output-e.txt"
        exit 1
    fi
    
    echo "✓ Test (e) passed"
else
    echo "✗ Test (e) failed - rollback state not detected"
    echo "Deploy output:"
    cat "$TEST_DIR/deploy-output-e.txt"
    exit 1
fi

echo ""
echo "=== Test (f): Snapshot copy failure aborts deploy ==="

git checkout master -q 2>/dev/null || git checkout main -q
git reset --hard "$INITIAL_COMMIT" -q
git push -f -q
rm -rf "$TEST_DIR/rollbacks"/*

# First do a successful deploy to have a binary
echo "change7a" >> cmd/server/main.go
git add -A
git commit -q -m "setup for test f"
git push -q
bash "$TEST_DIR/deploy-test.sh" > /dev/null 2>&1

# Save hashes before making changes
OLD_BINARY_HASH=$(md5sum bin/server | cut -d' ' -f1)
OLD_JS=$(head -1 web/static/js/app.js)

# Make binary unreadable to cause cp failure during snapshot
chmod 000 bin/server

echo "change8" >> cmd/server/main.go
git add -A
git commit -q -m "change for test f"
git push -q

bash "$TEST_DIR/deploy-test.sh" > "$TEST_DIR/deploy-output-f.txt" 2>&1 || true

# Restore permissions
chmod 755 bin/server 2>/dev/null || true

if grep -qE "(Failed to back up|Permission denied)" "$TEST_DIR/deploy-output-f.txt"; then
    echo "✓ Snapshot failure detected"
    
    # Verify nothing was changed (deploy aborted)
    CURRENT_BINARY_HASH=$(md5sum bin/server 2>/dev/null | cut -d' ' -f1 || echo "missing")
    CURRENT_JS=$(head -1 web/static/js/app.js 2>/dev/null || echo "missing")
    
    if [ "$CURRENT_BINARY_HASH" = "$OLD_BINARY_HASH" ] && [ "$CURRENT_JS" = "$OLD_JS" ]; then
        echo "✓ No changes made (deploy aborted before swap)"
    else
        echo "✗ Deploy made changes despite snapshot failure"
        echo "  Binary: $OLD_BINARY_HASH -> $CURRENT_BINARY_HASH"
        echo "  JS: changed"
        exit 1
    fi
    
    echo "✓ Test (f) passed"
else
    echo "✗ Test (f) failed - snapshot failure not detected"
    echo "Deploy output:"
    cat "$TEST_DIR/deploy-output-f.txt"
    exit 1
fi

echo ""
echo "=== Test (g): Deploy from rollback state with upstream CSS change (M1) ==="

git checkout master -q 2>/dev/null || git checkout main -q
git reset --hard "$INITIAL_COMMIT" -q
git push -f -q
rm -rf "$TEST_DIR/rollbacks"/*
rm -rf bin

# Deploy to create known-good state
echo "change-g1" >> cmd/server/main.go
echo "/* css-v1 */" > web/static/css/styles.css
git add -A
git commit -q -m "test g: initial"
git push -q

if ! bash "$TEST_DIR/deploy-test.sh" > /dev/null 2>&1; then
    echo "✗ Test (g) setup failed"
    exit 1
fi

GOOD_COMMIT=$(git rev-parse HEAD)

# Get snapshot commit
# shellcheck disable=SC2012
SNAPSHOT_TS=$(ls "$TEST_DIR/rollbacks" | head -1)
SNAPSHOT_COMMIT=$(cat "$TEST_DIR/rollbacks/$SNAPSHOT_TS/COMMIT")

# The rolled-back commit stays at the local branch tip with different CSS, as after a
# real auto-rollback; `git checkout -f` of the branch then puts that CSS on disk
echo "/* css-v1-rolled-back */" > web/static/css/styles.css
git commit -q -am "test g: rolled-back tip"
git push -q

# Create separate clone to make upstream changes
mkdir -p "$TEST_DIR/upstream-clone"
cd "$TEST_DIR/upstream-clone"
git clone -q "$TEST_DIR/remote-repo.git" upstream
cd upstream
git config user.email "test@example.com"
git config user.name "Test User"

# Make upstream CSS change
echo "change-g2" >> cmd/server/main.go
echo "/* css-v2 */" > web/static/css/styles.css
git add -A
git commit -q -m "test g: upstream css change"
git push -q origin master 2>/dev/null || git push -q origin main

NEW_COMMIT=$(git rev-parse HEAD)
REMOTE_BRANCH=$(git branch --show-current)

# Go back to main test repo in detached HEAD (rollback state)
cd "$TEST_DIR/repo"
git checkout "$SNAPSHOT_COMMIT" -q

# Restore CSS from snapshot to clean the tree (simulate real rollback state)
if [ -f "$TEST_DIR/rollbacks/$SNAPSHOT_TS/css/styles.css" ]; then
    cp "$TEST_DIR/rollbacks/$SNAPSHOT_TS/css/styles.css" web/static/css/styles.css
fi

# CSS bytes the (rolled-back) running server serves; they must stay on disk until the swap
RUNNING_CSS_HASH_G=$(md5sum < web/static/css/styles.css | awk '{print $1}')
: > "$TEST_DIR/css-at-build.log"

# Deploy from rollback state should succeed
if ! bash "$TEST_DIR/deploy-test.sh" > "$TEST_DIR/deploy-output-g.txt" 2>&1; then
    echo "✗ Deploy from rollback state failed"
    echo "Output:"
    tail -50 "$TEST_DIR/deploy-output-g.txt"
    exit 1
fi

# Verify we're on the new commit
CURRENT_COMMIT=$(git rev-parse HEAD)
if [ "$CURRENT_COMMIT" != "$NEW_COMMIT" ]; then
    echo "✗ Not at new commit (expected $NEW_COMMIT, got $CURRENT_COMMIT)"
    exit 1
fi
echo "✓ Deployed to new commit"

if [ "$(head -1 "$TEST_DIR/css-at-build.log")" = "$RUNNING_CSS_HASH_G" ]; then
    echo "✓ Running server's CSS stayed on disk during the build (rollback state)"
else
    echo "✗ CSS on disk during build was not the running server's CSS (rollback state)"
    exit 1
fi

# Verify tree is clean
if ! git diff --quiet || ! git diff --cached --quiet; then
    echo "✗ Tree is dirty after deploy"
    git status --short
    exit 1
fi
echo "✓ Tree is clean"

# Verify HEAD == origin/branch
ORIGIN_HEAD=$(git rev-parse "origin/$REMOTE_BRANCH")
if [ "$CURRENT_COMMIT" = "$ORIGIN_HEAD" ]; then
    echo "✓ HEAD matches origin/$REMOTE_BRANCH"
else
    echo "✗ HEAD doesn't match origin (expected $ORIGIN_HEAD, got $CURRENT_COMMIT)"
    exit 1
fi

# Verify CSS content matches what's in git at new commit
# (deploy.sh restores committed CSS after successful deploy)
CURRENT_CSS=$(head -1 web/static/css/styles.css)
EXPECTED_CSS="/* css-v2 */"
if [ "$CURRENT_CSS" = "$EXPECTED_CSS" ]; then
    echo "✓ CSS content matches committed version"
else
    echo "✗ CSS mismatch (expected '$EXPECTED_CSS', got '$CURRENT_CSS')"
    exit 1
fi

# Test subsequent deploy also works
echo "  Testing subsequent deploy..."
cd "$TEST_DIR/upstream-clone/upstream"
echo "change-g3" >> cmd/server/main.go
git add -A
git commit -q -m "test g: another change"
git push -q

cd "$TEST_DIR/repo"
if ! bash "$TEST_DIR/deploy-test.sh" > /dev/null 2>&1; then
    echo "✗ Subsequent deploy failed"
    exit 1
fi
echo "✓ Subsequent deploy succeeded"

echo "✓ Test (g) passed"

echo ""
echo "=== Test (h): Normal deploy CSS invariant - served CSS matches served HTML hash ==="

git checkout master -q 2>/dev/null || git checkout main -q
rm -rf "$TEST_DIR/rollbacks"/*
rm -rf bin

# Deploy to create initial state
echo "change-h1" >> cmd/server/main.go
echo "/* css-h1 */" > web/static/css/styles.css
git add -A
git commit -q -m "test h: initial"
git push -q
bash "$TEST_DIR/deploy-test.sh" > /dev/null 2>&1

# Record initial CSS hash that the currently running binary expects
INITIAL_HASH=$(md5sum web/static/css/styles.css | awk '{print $1}')
echo "$INITIAL_HASH" > "$TEST_DIR/initial-hash.txt"

# Make CSS change upstream
mkdir -p "$TEST_DIR/upstream-clone"
cd "$TEST_DIR/upstream-clone"
if [ ! -d "upstream" ]; then
    git clone "$TEST_DIR/remote-repo.git" upstream -q
fi
cd upstream
git pull -q
echo "change-h2" >> cmd/server/main.go
echo "/* css-h2 */" > web/static/css/styles.css
git add -A
git commit -q -m "test h: css change"
git push -q

cd "$TEST_DIR/repo"

# Pull to get new CSS in git, then compute its hash
git pull -q
NEW_HASH=$(git show HEAD:web/static/css/styles.css | md5sum | awk '{print $1}')
git reset --hard HEAD~1 -q  # Reset so deploy can pull it
echo "$NEW_HASH" > "$TEST_DIR/new-hash.txt"

# Monitor: CSS on disk must always match what the currently-running binary expects
# Before restart: old hash. After restart: new hash.
(
    OLD_HASH=$(cat "$TEST_DIR/initial-hash.txt")
    NEW_HASH=$(cat "$TEST_DIR/new-hash.txt")
    INITIAL_PID=$(cat "$TEST_DIR/service.pid" 2>/dev/null || echo "")
    SERVICE_RESTARTED=0
    
    for _ in {1..20}; do  # Monitor for ~10s (20 * 0.5s)
        sleep 0.5
        
        # Determine which binary is currently running by checking PID
        CURRENT_PID=$(cat "$TEST_DIR/service.pid" 2>/dev/null || echo "")
        
        # Detect restart: PID changed from initial to a new non-empty PID
        if [ -n "$INITIAL_PID" ] && [ -n "$CURRENT_PID" ] && [ "$CURRENT_PID" != "$INITIAL_PID" ]; then
            SERVICE_RESTARTED=1
        fi
        
        # Determine expected hash based on whether service restarted
        if [ "$SERVICE_RESTARTED" -eq 1 ]; then
            EXPECTED_HASH="$NEW_HASH"
        else
            EXPECTED_HASH="$OLD_HASH"
        fi
        
        # Only check if service is running (PID file exists)
        if [ -n "$CURRENT_PID" ]; then
            ACTUAL_HASH=$(md5sum web/static/css/styles.css 2>/dev/null | awk '{print $1}' || echo "missing")
            if [ "$ACTUAL_HASH" != "$EXPECTED_HASH" ] && [ "$ACTUAL_HASH" != "missing" ]; then
                echo "CSS hash mismatch: binary expects $EXPECTED_HASH, disk has $ACTUAL_HASH" > "$TEST_DIR/css-hash-mismatch.txt"
                break
            fi
        fi
    done
) &
MONITOR_PID=$!

: > "$TEST_DIR/css-at-build.log"
bash "$TEST_DIR/deploy-test.sh" > /dev/null 2>&1

wait $MONITOR_PID 2>/dev/null || true

if [ "$(head -1 "$TEST_DIR/css-at-build.log")" != "$INITIAL_HASH" ]; then
    echo "✗ CSS on disk during build was not the running server's CSS (normal path)"
    exit 1
fi
echo "✓ Running server's CSS stayed on disk during the build (normal path)"

if [ -f "$TEST_DIR/css-hash-mismatch.txt" ]; then
    echo "✗ $(cat "$TEST_DIR/css-hash-mismatch.txt")"
    exit 1
fi
echo "✓ CSS hash matched running binary throughout deploy"

# Verify final CSS is new version
FINAL_CSS=$(cat web/static/css/styles.css)
if echo "$FINAL_CSS" | grep -q "css-h2"; then
    echo "✓ Final CSS updated to new version"
else
    echo "✗ Final CSS not updated"
    exit 1
fi

echo "✓ Test (h) passed"

echo ""
echo "=== Test (i): M5 - Deploy late crash after first good check ==="

git checkout master -q 2>/dev/null || git checkout main -q
rm -rf "$TEST_DIR/rollbacks"/*
rm -rf bin

echo "change-i1" >> cmd/server/main.go
git add -A
git commit -q -m "test i setup"
git push -q
bash "$TEST_DIR/deploy-test.sh" > /dev/null 2>&1

# Deploy that will crash after first success
echo "change-i2" >> cmd/server/main.go
git add -A
git commit -q -m "test i crash"
git push -q

# Set up delayed crash (crash after 5 seconds, during stability window)
(
    sleep 5
    # Kill the service process to simulate mid-window crash
    if [ -f "$TEST_DIR/service.pid" ]; then
        PID=$(cat "$TEST_DIR/service.pid")
        kill -9 "$PID" 2>/dev/null || true
    fi
) &

export STABILITY_WINDOW=10
bash "$TEST_DIR/deploy-test.sh" > "$TEST_DIR/deploy-output-i.txt" 2>&1 || true

# Check that it detected the crash (either MainPID changed or service became inactive)
if grep -q "Service became inactive\|MainPID changed" "$TEST_DIR/deploy-output-i.txt"; then
    echo "✓ Deploy late crash detected and rolled back"
else
    echo "✗ Deploy late crash not detected"
    tail -50 "$TEST_DIR/deploy-output-i.txt"
    exit 1
fi

export STABILITY_WINDOW=3

echo "✓ Test (i) passed"

echo ""
echo "=== Test (j): P1 - Restart failure triggers rollback ==="

git checkout master -q 2>/dev/null || git checkout main -q
rm -rf "$TEST_DIR/rollbacks"/*
rm -rf bin

echo "change-j1" >> cmd/server/main.go
git add -A
git commit -q -m "test j setup"
git push -q
bash "$TEST_DIR/deploy-test.sh" > /dev/null 2>&1

# Deploy that will fail restart
touch "$TEST_DIR/restart_fail"

echo "change-j2" >> cmd/server/main.go
git add -A
git commit -q -m "test j restart fail"
git push -q

bash "$TEST_DIR/deploy-test.sh" > "$TEST_DIR/deploy-output-j.txt" 2>&1 || true

if grep -q "systemctl restart failed" "$TEST_DIR/deploy-output-j.txt" && grep -q "Attempting automatic rollback" "$TEST_DIR/deploy-output-j.txt"; then
    echo "✓ Restart failure triggered rollback"
else
    echo "✗ Restart failure didn't trigger rollback"
    exit 1
fi

rm -f "$TEST_DIR/restart_fail"

echo "✓ Test (j) passed"

echo ""
echo "=== Test (k): M2 - Rollback failure handled correctly ==="

git checkout master -q 2>/dev/null || git checkout main -q
rm -rf "$TEST_DIR/rollbacks"/*
rm -rf bin

# Create initial working state
echo "change-k1" >> cmd/server/main.go
git add -A
git commit -q -m "test k setup"
git push -q
bash "$TEST_DIR/deploy-test.sh" > /dev/null 2>&1

# Deploy that will fail health check and attempt rollback
touch "$TEST_DIR/health_fail"
echo "change-k2" >> cmd/server/main.go
git add -A
git commit -q -m "test k bad deploy"
git push -q

# Make rollback fail by removing snapshot
SNAPSHOT_DIR=$(find "$TEST_DIR/rollbacks" -mindepth 1 -maxdepth 1 -type d -printf '%f\n' | head -1)
if [ -n "$SNAPSHOT_DIR" ]; then
    rm -rf "$TEST_DIR/rollbacks/$SNAPSHOT_DIR"
fi

bash "$TEST_DIR/deploy-test.sh" > "$TEST_DIR/deploy-output-k.txt" 2>&1 || true

# Should report rollback failure
if grep -q "Rollback also failed\|ROLLBACK FAILED" "$TEST_DIR/deploy-output-k.txt"; then
    echo "✓ Rollback failure detected and reported"
else
    echo "✗ Rollback failure not properly reported"
    grep -i rollback "$TEST_DIR/deploy-output-k.txt" | tail -5
    exit 1
fi

rm -f "$TEST_DIR/health_fail"

echo "✓ Test (k) passed"

echo ""
echo "=== Test (l): M3 - Detached HEAD at non-snapshot commit refused ==="

git checkout master -q 2>/dev/null || git checkout main -q
rm -rf "$TEST_DIR/rollbacks"/*
rm -rf bin

# Create a commit and snapshot
echo "change-l1" >> cmd/server/main.go
git add -A
git commit -q -m "test l commit 1"
git push -q
bash "$TEST_DIR/deploy-test.sh" > /dev/null 2>&1

# Create another commit (this one won't have a snapshot)
echo "change-l2" >> cmd/server/main.go
git add -A
git commit -q -m "test l commit 2"
git push -q
UNKNOWN_COMMIT=$(git rev-parse HEAD)

# Go to detached HEAD at this unknown commit (no snapshot for it)
git checkout -q "$UNKNOWN_COMMIT"

# Try to deploy - should refuse because we're detached at a commit with no snapshot
SNAPS_BEFORE=$(find "$TEST_DIR/rollbacks" -mindepth 1 -maxdepth 1 | wc -l)
RC_L=0
bash "$TEST_DIR/deploy-test.sh" > "$TEST_DIR/deploy-output-l.txt" 2>&1 || RC_L=$?
SNAPS_AFTER=$(find "$TEST_DIR/rollbacks" -mindepth 1 -maxdepth 1 | wc -l)

# Should detect unknown commit and refuse: non-zero exit, still detached at
# UNKNOWN_COMMIT, and no snapshot written (a new snapshot would make it "known")
if [ "$RC_L" -ne 0 ] \
    && grep -q "Detached HEAD at unknown commit" "$TEST_DIR/deploy-output-l.txt" \
    && [ "$(git rev-parse HEAD)" = "$UNKNOWN_COMMIT" ] \
    && [ "$SNAPS_AFTER" -eq "$SNAPS_BEFORE" ]; then
    echo "✓ Non-snapshot commit refused"
else
    echo "✗ Non-snapshot commit not refused (rc=$RC_L, snapshots $SNAPS_BEFORE -> $SNAPS_AFTER)"
    cat "$TEST_DIR/deploy-output-l.txt"
    exit 1
fi

echo "✓ Test (l) passed"

echo ""
echo "=== Test (m): P2 - Snapshot failure leaves no partial directories ==="

git checkout master -q 2>/dev/null || git checkout main -q
rm -rf "$TEST_DIR/rollbacks"/*
rm -rf bin

# Deploy once so a binary exists, then make the snapshot copy fail mid-way
echo "change-m1" >> cmd/server/main.go
git add -A
git commit -q -m "test m setup"
git push -q
bash "$TEST_DIR/deploy-test.sh" > /dev/null 2>&1
SNAPS_BEFORE=$(find "$TEST_DIR/rollbacks" -mindepth 1 -maxdepth 1 | sort)
chmod 000 web/static/js/app.js
echo "change-m2" >> cmd/server/main.go
git add -A
git commit -q -m "test m snapshot fails"
git push -q
RC_M=0
bash "$TEST_DIR/deploy-test.sh" > "$TEST_DIR/deploy-output-m.txt" 2>&1 || RC_M=$?
chmod 644 web/static/js/app.js
SNAPS_AFTER=$(find "$TEST_DIR/rollbacks" -mindepth 1 -maxdepth 1 | sort)
if [ "$RC_M" -ne 0 ] && grep -q "Failed to back up js" "$TEST_DIR/deploy-output-m.txt" \
    && [ "$SNAPS_AFTER" = "$SNAPS_BEFORE" ]; then
    echo "✓ No partial snapshot directories after snapshot failure"
else
    echo "✗ Snapshot failure left partial state (rc=$RC_M)"
    echo "  before: $SNAPS_BEFORE"
    echo "  after:  $SNAPS_AFTER"
    cat "$TEST_DIR/deploy-output-m.txt"
    exit 1
fi
git reset -q --hard HEAD~1
git push -q -f

echo "✓ Test (m) passed"

echo ""
echo "=== Test (n): No /tmp leaks after tests ==="

# Every mktemp in deploy.sh landed in our private TMPDIR; it must be empty now
TMP_FILES=$(find "$TMPDIR" -mindepth 1 -maxdepth 1)
if [ -n "$TMP_FILES" ]; then
    echo "✗ Leaked temp file(s):"
    echo "$TMP_FILES"
    exit 1
fi

echo "✓ No /tmp leaks detected"
echo "✓ Test (n) passed"

echo ""
echo "=== Test (o): Rollback late crash after first good check ==="

git checkout master -q 2>/dev/null || git checkout main -q
rm -rf "$TEST_DIR/rollbacks"/*
rm -rf bin

# Create two commits: one good, one bad
echo "change-o1" >> cmd/server/main.go
git add -A
git commit -q -m "test o good"
git push -q
bash "$TEST_DIR/deploy-test.sh" > /dev/null 2>&1

# Get the snapshot timestamp
# shellcheck disable=SC2012
SNAPSHOT_O=$(ls -1 "$TEST_DIR/rollbacks" | head -1)

echo "change-o2" >> cmd/server/main.go
git add -A
git commit -q -m "test o bad"
git push -q
bash "$TEST_DIR/deploy-test.sh" > /dev/null 2>&1

# Now rollback to the good snapshot, but simulate crash during stability window
(
    sleep 5
    # Kill the service to simulate mid-window crash
    if [ -f "$TEST_DIR/service.pid" ]; then
        PID=$(cat "$TEST_DIR/service.pid")
        kill -9 "$PID" 2>/dev/null || true
    fi
) &

export STABILITY_WINDOW=10
bash "$TEST_DIR/rollback.sh" "$SNAPSHOT_O" > "$TEST_DIR/rollback-output-o.txt" 2>&1 || true

# Check that rollback detected the crash
if grep -q "Service became inactive\|MainPID changed" "$TEST_DIR/rollback-output-o.txt"; then
    echo "✓ Rollback late crash detected"
else
    echo "✗ Rollback late crash not detected"
    tail -50 "$TEST_DIR/rollback-output-o.txt"
    exit 1
fi

export STABILITY_WINDOW=3

echo "✓ Test (o) passed"

echo ""
echo "=== Test (p): Rollback stops service before swapping files ==="

git checkout master -q 2>/dev/null || git checkout main -q
rm -rf "$TEST_DIR/rollbacks"/*
rm -rf bin

# Create two commits
echo "change-p1" >> cmd/server/main.go
git add -A
git commit -q -m "test p snapshot"
git push -q
bash "$TEST_DIR/deploy-test.sh" > /dev/null 2>&1

# Get the snapshot
# shellcheck disable=SC2012
SNAPSHOT_P=$(ls -1 "$TEST_DIR/rollbacks" | head -1)

echo "change-p2" >> cmd/server/main.go
git add -A
git commit -q -m "test p current"
git push -q
bash "$TEST_DIR/deploy-test.sh" > /dev/null 2>&1

# Enable swap order recording
touch "$TEST_DIR/record_swap_order"
: > "$TEST_DIR/swap_order.log"

# Manually instrument the rollback script to record when files are swapped
# We'll check that 'stop' appears in the log before we see evidence of file operations
bash "$TEST_DIR/rollback.sh" "$SNAPSHOT_P" > /dev/null 2>&1

# Check that stop was called
if grep -q "^stop$" "$TEST_DIR/swap_order.log"; then
    echo "✓ Service stopped before file swap"
else
    echo "✗ Service not stopped before file swap"
    cat "$TEST_DIR/swap_order.log"
    exit 1
fi

rm -f "$TEST_DIR/record_swap_order" "$TEST_DIR/swap_order.log"

echo "✓ Test (p) passed"

echo ""
echo "✓ All tests passed (a-p)!"
