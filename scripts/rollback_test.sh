#!/usr/bin/env bash
#
# Test that rollback.sh can restore an old snapshot
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
TEST_DIR=$(mktemp -d)

cleanup() {
    # Kill any mock-service processes and all child processes
    if [ -f "$TEST_DIR/service.pid" ]; then
        SERVICE_PID=$(cat "$TEST_DIR/service.pid" 2>/dev/null || echo "")
        if [ -n "$SERVICE_PID" ]; then
            kill "$SERVICE_PID" 2>/dev/null || true
        fi
    fi
    
    # Kill entire process group to catch any orphans
    # Send TERM to all children, then wait briefly
    pkill -P $$ 2>/dev/null || true
    sleep 0.1
    
    # Force kill any stragglers
    pkill -9 -P $$ 2>/dev/null || true
    
    rm -rf "$TEST_DIR"
}

trap cleanup EXIT

echo "Testing rollback restore functionality..."
echo "Test directory: $TEST_DIR"

# Create a mock project structure
mkdir -p "$TEST_DIR/project/bin"
mkdir -p "$TEST_DIR/project/web/templates"
mkdir -p "$TEST_DIR/project/web/static/js"
mkdir -p "$TEST_DIR/project/web/static/css"
mkdir -p "$TEST_DIR/rollbacks/20261017-120000"

# Create "old" snapshot
echo "old-binary" > "$TEST_DIR/rollbacks/20261017-120000/server"
mkdir -p "$TEST_DIR/rollbacks/20261017-120000/templates"
echo "old-template" > "$TEST_DIR/rollbacks/20261017-120000/templates/test.html"
mkdir -p "$TEST_DIR/rollbacks/20261017-120000/js"
echo "old-js" > "$TEST_DIR/rollbacks/20261017-120000/js/app.js"
mkdir -p "$TEST_DIR/rollbacks/20261017-120000/css"
echo "old-css" > "$TEST_DIR/rollbacks/20261017-120000/css/styles.css"

# Create "current" files (different content)
echo "new-binary" > "$TEST_DIR/project/bin/server"
echo "new-template" > "$TEST_DIR/project/web/templates/test.html"
echo "new-js" > "$TEST_DIR/project/web/static/js/app.js"
echo "new-css" > "$TEST_DIR/project/web/static/css/styles.css"

# Create a long-lived mock process to simulate a running service
cat > "$TEST_DIR/mock-service" <<'EOF'
#!/usr/bin/env bash
# Simulate a long-running service binary
while true; do
    sleep 1
done
EOF
chmod +x "$TEST_DIR/mock-service"

# Mock systemctl - handle is-active, restart, and stop
# Also manages the mock service process
cat > "$TEST_DIR/systemctl" <<EOF
#!/usr/bin/env bash
SERVICE_PID_FILE="$TEST_DIR/service.pid"

case "\$1" in
    is-active)
        # Check if service is running
        if [ -f "\$SERVICE_PID_FILE" ] && kill -0 \$(cat "\$SERVICE_PID_FILE") 2>/dev/null; then
            exit 0
        else
            exit 3
        fi
        ;;
    restart|start)
        # Stop old process if running
        if [ -f "\$SERVICE_PID_FILE" ]; then
            OLD_PID=\$(cat "\$SERVICE_PID_FILE")
            kill "\$OLD_PID" 2>/dev/null || true
            rm -f "\$SERVICE_PID_FILE"
        fi
        # Start new process in background, detached from output pipes
        "$TEST_DIR/mock-service" >/dev/null 2>&1 &
        echo \$! > "\$SERVICE_PID_FILE"
        echo "Mock restart/start: \$2" >&2
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
        echo "Mock service is running"
        exit 0
        ;;
esac
EOF
chmod +x "$TEST_DIR/systemctl"

# Mock sudo - handle systemctl specially
cat > "$TEST_DIR/sudo" <<'EOF'
#!/usr/bin/env bash
# $1 is the command after 'sudo'
if [ "$1" = "systemctl" ]; then
    shift
    systemctl "$@"
else
    "$@"
fi
EOF
chmod +x "$TEST_DIR/sudo"

# Mock curl
cat > "$TEST_DIR/curl" <<'EOF'
#!/usr/bin/env bash
echo "200"
EOF
chmod +x "$TEST_DIR/curl"

# Create a modified rollback.sh that uses our test paths
# Need to be careful with the order of replacements
sed -e "s|ROLLBACK_DIR=\"/home/deploy/liveoilprices-rollbacks\"|ROLLBACK_DIR=\"$TEST_DIR/rollbacks\"|g" \
    -e "s|PROJECT_DIR=.*|PROJECT_DIR=\"$TEST_DIR/project\"|g" \
    -e "s|cd \"\$PROJECT_DIR\"|# cd suppressed for test|g" \
    "$SCRIPT_DIR/rollback.sh" > "$TEST_DIR/rollback-test.sh"
chmod +x "$TEST_DIR/rollback-test.sh"

# Run the rollback with mocked commands
cd "$TEST_DIR/project"
export PATH="$TEST_DIR:$PATH"

echo ""
echo "Running rollback with service stopped..."
bash "$TEST_DIR/rollback-test.sh" 20261017-120000

echo ""
echo "Simulating a running service for rollback test..."
# Start the mock service
"$TEST_DIR/systemctl" start liveoilprices

# Give it a moment to start
sleep 0.5

# Copy a "new" binary content to test the replacement
echo "new-binary-content" > "$TEST_DIR/project/bin/server"

# Now try rollback while service is "running"
echo "Attempting rollback while service is running..."
ROLLBACK_OUTPUT=$(bash "$TEST_DIR/rollback-test.sh" 20261017-120000 2>&1)
if echo "$ROLLBACK_OUTPUT" | grep -q "pre-rollback"; then
    echo "✓ Rollback succeeded even with service running (temp + mv)"
else
    echo "✗ Rollback failed with service running"
    echo "Rollback output was:"
    echo "$ROLLBACK_OUTPUT" | head -20
    exit 1
fi

echo ""
echo "Verifying restored files..."

# Clean up any running mock service (will also be cleaned in trap)
if [ -f "$TEST_DIR/service.pid" ]; then
    SERVICE_PID=$(cat "$TEST_DIR/service.pid")
    kill "$SERVICE_PID" 2>/dev/null || true
    rm -f "$TEST_DIR/service.pid"
fi

# Kill any remaining children
pkill -P $$ 2>/dev/null || true
sleep 0.2
pkill -9 -P $$ 2>/dev/null || true

# Verify no child processes remain from mock service (write to file to avoid counting subshell)
pgrep -P $$ > "$TEST_DIR/children.txt" 2>/dev/null || true
REMAINING=$(wc -l < "$TEST_DIR/children.txt" 2>/dev/null || echo "0")
if [ "$REMAINING" -gt 0 ]; then
    echo "WARNING: $REMAINING child processes still running after cleanup"
    pkill -9 -P $$ 2>/dev/null || true
fi

# Check that old content was restored
if grep -q "old-binary" "$TEST_DIR/project/bin/server"; then
    echo "✓ Binary restored"
else
    echo "✗ Binary NOT restored"
    exit 1
fi

if grep -q "old-template" "$TEST_DIR/project/web/templates/test.html"; then
    echo "✓ Templates restored"
else
    echo "✗ Templates NOT restored"
    exit 1
fi

if grep -q "old-js" "$TEST_DIR/project/web/static/js/app.js"; then
    echo "✓ JS restored"
else
    echo "✗ JS NOT restored"
    exit 1
fi

if grep -q "old-css" "$TEST_DIR/project/web/static/css/styles.css"; then
    echo "✓ CSS restored"
else
    echo "✗ CSS NOT restored"
    exit 1
fi

# Check that pre-rollback backups were created (2 from our 2 rollback runs)
BACKUP_COUNT=$(find "$TEST_DIR/rollbacks" -type d -name "pre-rollback-*" | wc -l)
if [ "$BACKUP_COUNT" -eq 2 ]; then
    echo "✓ Pre-rollback backups created (2)"
else
    echo "✗ Expected 2 pre-rollback backups, got $BACKUP_COUNT"
    exit 1
fi

echo ""
echo "✓ All rollback tests passed!"
