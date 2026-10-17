#!/usr/bin/env bash
#
# Test that rollback.sh can restore an old snapshot
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
TEST_DIR=$(mktemp -d)
trap 'rm -rf "$TEST_DIR"' EXIT

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

# Mock systemctl - handle is-active and restart
cat > "$TEST_DIR/systemctl" <<'EOF'
#!/usr/bin/env bash
echo "systemctl $@" >&2
case "$1" in
    is-active)
        exit 0
        ;;
    restart)
        echo "Mock restart: $2"
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
echo "Running rollback to 20261017-120000..."
bash "$TEST_DIR/rollback-test.sh" 20261017-120000

echo ""
echo "Verifying restored files..."

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

# Check that a pre-rollback backup was created
BACKUP_COUNT=$(find "$TEST_DIR/rollbacks" -type d -name "pre-rollback-*" | wc -l)
if [ "$BACKUP_COUNT" -eq 1 ]; then
    echo "✓ Pre-rollback backup created"
else
    echo "✗ Pre-rollback backup NOT created (count: $BACKUP_COUNT)"
    exit 1
fi

echo ""
echo "✓ All rollback tests passed!"
