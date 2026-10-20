#!/usr/bin/env bash
#
# Run mutation tests to verify test suite catches regressions
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
RESULTS_FILE="/tmp/mutation_test_results.md"

echo "# Mutation Test Results" > "$RESULTS_FILE"
echo "" >> "$RESULTS_FILE"
echo "Each mutation should cause at least one test to fail." >> "$RESULTS_FILE"
echo "" >> "$RESULTS_FILE"

run_mutation() {
    local mutation_name="$1"
    local file_to_mutate="$2"
    local old_string="$3"
    local new_string="$4"
    local backup_file="${file_to_mutate}.backup"
    
    echo "" >> "$RESULTS_FILE"
    echo "## Mutation: $mutation_name" >> "$RESULTS_FILE"
    echo "" >> "$RESULTS_FILE"
    
    # Create backup
    cp "$file_to_mutate" "$backup_file"
    
    # Apply mutation
    if echo "$old_string" | grep -q '\$'; then
        # Use perl for complex replacements with $
        perl -i -pe "s/\Q$old_string\E/$new_string/" "$file_to_mutate"
    else
        sed -i "s|$old_string|$new_string|" "$file_to_mutate"
    fi
    
    # Run tests
    echo "Testing mutation: $mutation_name"
    if bash "$SCRIPT_DIR/deploy_test.sh" > /tmp/mutation_test_output.txt 2>&1; then
        echo "**FAIL**: Tests still passed with this mutation (tests are not detecting the bug)" >> "$RESULTS_FILE"
        echo "  ❌ Tests passed (mutation not caught)"
    else
        echo "**PASS**: Tests correctly failed with this mutation" >> "$RESULTS_FILE"
        echo "  ✅ Tests failed (mutation caught)"
    fi
    
    # Restore original
    mv "$backup_file" "$file_to_mutate"
}

# Mutation 1: Remove || true from deploy.sh systemctl is-active (line ~342)
run_mutation \
    "M1: Remove || true from first systemctl check" \
    "$SCRIPT_DIR/deploy.sh" \
    'if ! sudo systemctl is-active --quiet "$SERVICE" 2>/dev/null || false; then' \
    'if ! sudo systemctl is-active --quiet "$SERVICE" 2>/dev/null; then'

# Mutation 2: Remove || true from deploy.sh systemctl status (line ~344)
run_mutation \
    "M2: Remove || true from first systemctl status" \
    "$SCRIPT_DIR/deploy.sh" \
    'sudo systemctl status "$SERVICE" --no-pager 2>&1 || true' \
    'sudo systemctl status "$SERVICE" --no-pager 2>&1'

# Mutation 3: Remove || true from second systemctl check in health loop
run_mutation \
    "M3: Remove || true from health check systemctl" \
    "$SCRIPT_DIR/deploy.sh" \
    'if ! sudo systemctl is-active --quiet "$SERVICE" 2>/dev/null || false; then' \
    'if ! sudo systemctl is-active --quiet "$SERVICE" 2>/dev/null; then'

# Mutation 4: Revert to direct cp in rollback.sh (remove temp+mv pattern)
run_mutation \
    "M4: Revert to direct cp onto binary (ETXTBSY)" \
    "$SCRIPT_DIR/rollback.sh" \
    'TEMP_BIN="$(dirname "$BIN")/.server.tmp.$$"
cp "$ROLLBACK_PATH/server" "$TEMP_BIN"
chmod +x "$TEMP_BIN"
mv -f "$TEMP_BIN" "$BIN"' \
    'cp "$ROLLBACK_PATH/server" "$BIN"
chmod +x "$BIN"'

# Mutation 5: Skip template restore in rollback.sh
run_mutation \
    "M5: Skip template restore in rollback" \
    "$SCRIPT_DIR/rollback.sh" \
    'rm -rf web/templates
cp -r "$ROLLBACK_PATH/templates" web/' \
    '# Templates skipped'

# Mutation 6: Make snapshot copy non-fatal in deploy.sh
run_mutation \
    "M6: Make binary snapshot copy non-fatal" \
    "$SCRIPT_DIR/deploy.sh" \
    'cp "$BIN" "$ROLLBACK_PATH/server" || {
            echo "ERROR: Failed to back up binary"
            exit 1
        }' \
    'cp "$BIN" "$ROLLBACK_PATH/server" || true'

# Mutation 7: Remove rollback call on health check failure
run_mutation \
    "M7: Remove rollback call on health check failure" \
    "$SCRIPT_DIR/deploy.sh" \
    'if "$SCRIPT_DIR/rollback.sh" "$TIMESTAMP"; then' \
    'if true; then'

# Mutation 8: Remove || true from rollback.sh systemctl check
run_mutation \
    "M8: Remove || true from rollback systemctl status" \
    "$SCRIPT_DIR/rollback.sh" \
    'sudo systemctl status "$SERVICE" --no-pager 2>&1 || true' \
    'sudo systemctl status "$SERVICE" --no-pager 2>&1'

# Mutation 9: Remove || false guard from rollback.sh systemctl is-active
run_mutation \
    "M9: Remove || false guard from rollback systemctl is-active" \
    "$SCRIPT_DIR/rollback.sh" \
    'if sudo systemctl is-active --quiet "$SERVICE" 2>/dev/null || false; then' \
    'if sudo systemctl is-active --quiet "$SERVICE" 2>/dev/null; then'

# Mutation 10: Remove ROLLBACK_DIR guard from prune in deploy.sh
run_mutation \
    "M10: Remove ROLLBACK_DIR guard from prune" \
    "$SCRIPT_DIR/deploy.sh" \
    'if [ -n "$ROLLBACK_DIR" ] && [ -d "$ROLLBACK_DIR" ]; then
        find "$ROLLBACK_DIR"' \
    'find "$ROLLBACK_DIR"'

echo ""
echo "Mutation test results saved to: $RESULTS_FILE"
cat "$RESULTS_FILE"
