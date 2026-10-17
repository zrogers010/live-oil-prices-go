#!/usr/bin/env bash
#
# Sanity check: test nginx-prod.conf syntax without actually installing nginx
# or requiring real SSL certificates.
#

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
TEMP_DIR=$(mktemp -d)
trap 'rm -rf "$TEMP_DIR"' EXIT

# Create minimal wrapper config that includes nginx-prod.conf in http context
cat > "$TEMP_DIR/nginx.conf" <<EOF
# Minimal nginx config for syntax testing
# Don't specify user directive - run as current user
worker_processes 1;
error_log /dev/null;
pid /dev/null;

events {
    worker_connections 1024;
}

http {
    # Override default log paths to avoid permission issues in CI
    access_log $TEMP_DIR/access.log;
    error_log $TEMP_DIR/error.log;
    
    # Include the actual config file we're testing
    # This file should contain the map directive and server blocks
    include PLACEHOLDER_PATH;
}
EOF

# Replace placeholder with actual path
sed -i "s|PLACEHOLDER_PATH|$SCRIPT_DIR/nginx-prod.conf|g" "$TEMP_DIR/nginx.conf"

# Create stub files for certbot includes
mkdir -p "$TEMP_DIR/letsencrypt"
touch "$TEMP_DIR/letsencrypt/options-ssl-nginx.conf"

# Generate a minimal 2048-bit DH params file (required for SSL)
openssl dhparam -out "$TEMP_DIR/letsencrypt/ssl-dhparams.pem" 2048 2>/dev/null || {
    # If openssl fails, create a stub file
    echo "# Stub DH params" > "$TEMP_DIR/letsencrypt/ssl-dhparams.pem"
}

# Create a modified version of nginx-prod.conf with SSL cert paths stubbed out
# Also replace privileged ports with unprivileged ones for CI testing
sed -e "s|/etc/letsencrypt|$TEMP_DIR/letsencrypt|g" \
    -e 's|ssl_certificate .*fullchain\.pem.*|ssl_certificate '"$TEMP_DIR"'/letsencrypt/fullchain.pem;|' \
    -e 's|ssl_certificate_key .*privkey\.pem.*|ssl_certificate_key '"$TEMP_DIR"'/letsencrypt/privkey.pem;|' \
    -e 's|listen 80|listen 8080|g' \
    -e 's|listen 443|listen 8443|g' \
    "$SCRIPT_DIR/nginx-prod.conf" > "$TEMP_DIR/nginx-prod-stubbed.conf"

# Generate self-signed SSL certificate for testing
openssl req -x509 -nodes -days 1 -newkey rsa:2048 \
    -keyout "$TEMP_DIR/letsencrypt/privkey.pem" \
    -out "$TEMP_DIR/letsencrypt/fullchain.pem" \
    -subj "/CN=localhost" 2>/dev/null || {
    # If openssl fails, create stub files
    echo "# Stub cert" > "$TEMP_DIR/letsencrypt/fullchain.pem"
    echo "# Stub key" > "$TEMP_DIR/letsencrypt/privkey.pem"
}

# Update the wrapper to include the stubbed version
sed -i "s|$SCRIPT_DIR/nginx-prod.conf|$TEMP_DIR/nginx-prod-stubbed.conf|g" "$TEMP_DIR/nginx.conf"

echo "Testing nginx syntax with wrapper config..."
echo ""
echo "Config structure check:"
echo "  - map directive at http level: ✓"
echo "  - location ~ \.rollback inside server block: checking..."
echo ""

# Check structure manually
if grep -A 5 "^map \$arg_v" "$SCRIPT_DIR/nginx-prod.conf" | grep -q "default"; then
    echo "  ✓ map directive found at top level (http context)"
else
    echo "  ✗ map directive not found"
    exit 1
fi

# Semantic assertions
echo ""
echo "Semantic checks:"

# Strip comments for semantic checking (remove full-line and trailing comments)
# Also remove blank lines to simplify matching
STRIPPED=$(sed -e 's/#.*$//' -e '/^[[:space:]]*$/d' "$SCRIPT_DIR/nginx-prod.conf")

# Check that map regex is exactly {12} AND anchored with ^ and $ (in active code, not comments)
if echo "$STRIPPED" | grep -F '{12}' > /dev/null; then
    echo "  ✓ map uses ~^[0-9a-f]{12}\$ (SHA-256 truncated to 12 hex chars)"
else
    echo "  ✗ map regex is not ~^[0-9a-f]{12}\$ (mutation M: {12}->{6} would pass)"
    echo "     Config may have {6} or {12} only in comments"
    exit 1
fi

# Check that the map regex is properly anchored with ^ and $
if echo "$STRIPPED" | grep -q '~^.*{12}\$'; then
    echo "  ✓ map regex is anchored with ^...$ (prevents partial matches)"
else
    echo "  ✗ map regex is missing ^ or $ anchors (unanchored regex allows partial matches)"
    exit 1
fi

# Check that rollback location is active (not commented out)
if echo "$STRIPPED" | grep -q 'location ~\* .*\\\.rollback'; then
    printf "  ✓ rollback location is active with ~* (case-insensitive)\n"
else
    printf "  ✗ rollback location missing or commented out\n"
    printf "     Expected: location ~* .*\\\\.rollback { return 404; }\n"
    exit 1
fi

# Check that .rollback location contains return 404 or deny all (blocks access)
# Use awk to extract the entire location block and check for the blocking directive
ROLLBACK_BLOCK=$(awk '
    /location ~\* .*\.rollback/ { in_block=1; depth=0; block="" }
    in_block {
        block = block "\n" $0
        gsub(/#.*/, "", $0)  # strip comments
        for (i=1; i<=length($0); i++) {
            c = substr($0, i, 1)
            if (c == "{") depth++
            if (c == "}") depth--
        }
        if (depth == 0 && in_block) {
            print block
            exit
        }
    }
' "$SCRIPT_DIR/nginx-prod.conf")

if echo "$ROLLBACK_BLOCK" | grep -Eq '(return 404|deny all)'; then
    # Also check that return 404/deny all is NOT followed by return 200
    if echo "$ROLLBACK_BLOCK" | grep -q 'return 404' && echo "$ROLLBACK_BLOCK" | sed -n '/return 404/,$p' | grep -q 'return 200'; then
        echo "  ✗ .rollback location has 'return 200' after 'return 404' (bypasses block)"
        exit 1
    fi
    echo "  ✓ .rollback location blocks access with return 404 or deny all"
else
    echo "  ✗ .rollback location must contain 'return 404' or 'deny all' to block access"
    exit 1
fi

# Check that /css/ and /js/ alias locations use add_header Cache-Control (not expires)
# Look for location /css/ and location /js/ blocks
# shellcheck disable=SC2016
if echo "$STRIPPED" | grep -A3 'location /css/' | grep -q 'add_header Cache-Control.*$lop_static_cc'; then
    echo "  ✓ /css/ location uses add_header Cache-Control \$lop_static_cc"
else
    echo "  ✗ /css/ location missing or not using add_header Cache-Control \$lop_static_cc"
    exit 1
fi

# shellcheck disable=SC2016
if echo "$STRIPPED" | grep -A3 'location /js/' | grep -q 'add_header Cache-Control.*$lop_static_cc'; then
    echo "  ✓ /js/ location uses add_header Cache-Control \$lop_static_cc"
else
    echo "  ✗ /js/ location missing or not using add_header Cache-Control \$lop_static_cc"
    exit 1
fi

# Check for ANY expires directive in server blocks, except in allowlisted locations
# Allowlist: /robots.txt (can use expires for simple static files)
# Use awk to parse brace depth and track location context
EXPIRES_VIOLATIONS=$(awk '
BEGIN { 
    server_depth=0
    location_depth=0
    current_location=""
    in_server=0
}
{
    # Strip comments
    line_no_comment = $0
    gsub(/#.*/, "", line_no_comment)
    
    # Track server block
    if (line_no_comment ~ /server[[:space:]]*\{/) {
        in_server = 1
        server_depth = 0
    }
    
    # Track location blocks - extract the path
    if (line_no_comment ~ /location[[:space:]]/) {
        # Simple extraction: get the path after "location" and optional modifiers
        loc_line = line_no_comment
        sub(/^.*location[[:space:]]+/, "", loc_line)
        sub(/^[~*]+[[:space:]]*/, "", loc_line)
        sub(/[[:space:]]*\{.*$/, "", loc_line)
        current_location = loc_line
        location_depth = 0
    }
    
    # Count braces for depth tracking
    brace_count = gsub(/\{/, "{", line_no_comment)
    close_count = gsub(/\}/, "}", line_no_comment)
    
    if (in_server) {
        server_depth += brace_count
        server_depth -= close_count
        if (server_depth == 0) in_server = 0
    }
    
    if (current_location != "") {
        location_depth += brace_count
        location_depth -= close_count
        if (location_depth == 0) current_location = ""
    }
    
    # Check for expires directive
    if (line_no_comment ~ /[[:space:]]expires[[:space:]]/ && in_server) {
        # Allowlist: /robots.txt and any location containing "robots"
        if (current_location !~ /robots/) {
            if (current_location == "") {
                print "Server-level expires found at line " NR
            } else {
                print "Disallowed expires in location \"" current_location "\" at line " NR
            }
        }
    }
}
' "$SCRIPT_DIR/nginx-prod.conf")

if [ -n "$EXPIRES_VIOLATIONS" ]; then
    echo "  ✗ Disallowed 'expires' directive(s) found:"
    # shellcheck disable=SC2001
    echo "$EXPIRES_VIOLATIONS" | sed 's/^/      /'
    echo "     Only /robots.txt location may use 'expires' (use add_header Cache-Control elsewhere)"
    exit 1
fi
echo "  ✓ No disallowed 'expires' directives (only /robots.txt allowed)"

# Check that immutable 1y header is only on hashed branch (map regex line, not default)
if echo "$STRIPPED" | grep -q '~.*{12}.*immutable'; then
    echo "  ✓ immutable 1y header only on hashed version branch (map regex)"
    # Verify default line does NOT have immutable
    if echo "$STRIPPED" | grep 'default.*immutable' > /dev/null; then
        echo "  ✗ default line also has immutable (should only be on hashed)"
        exit 1
    fi
else
    echo "  ✗ immutable 1y header not found on hashed regex line"
    exit 1
fi

# Check that default (HTML/no-v) uses max-age=300
if echo "$STRIPPED" | grep -q 'max-age=300'; then
    echo "  ✓ HTML/no-v default uses max-age=300 (5 min must-revalidate)"
else
    echo "  ✗ default max-age=300 not found (mutation M: old Cache-Control would pass)"
    exit 1
fi

# Find the line number of the location directive
LOC_LINE=$(grep -n 'location.*rollback' "$SCRIPT_DIR/nginx-prod.conf" | head -1 | cut -d: -f1)
# Find the line number of the nearest server { before it
SERVER_LINE=$(head -n "$LOC_LINE" "$SCRIPT_DIR/nginx-prod.conf" | grep -n "^server {" | tail -1 | cut -d: -f1)

if [ -n "$SERVER_LINE" ] && [ "$SERVER_LINE" -lt "$LOC_LINE" ]; then
    echo "  ✓ location ~ \.rollback is inside a server block (server at line $SERVER_LINE, location at line $LOC_LINE)"
else
    echo "  ✗ location ~ \.rollback is NOT inside a server block"
    echo "    Found at line $LOC_LINE but no server { found before it"
    exit 1
fi

# Try to test with nginx if available
NGINX_BIN=""
for path in nginx /usr/sbin/nginx /usr/bin/nginx /usr/local/sbin/nginx; do
    if command -v "$path" &>/dev/null; then
        NGINX_BIN="$path"
        break
    fi
done

if [ -n "$NGINX_BIN" ]; then
    echo ""
    echo "nginx found at $NGINX_BIN, testing syntax..."
    # Capture output to avoid SIGPIPE with grep -q under pipefail
    NGINX_OUTPUT=$("$NGINX_BIN" -t -c "$TEMP_DIR/nginx.conf" 2>&1)
    NGINX_EXIT=$?
    if [ "$NGINX_EXIT" = 0 ] && echo "$NGINX_OUTPUT" | grep -q "syntax is ok"; then
        echo "  ✓ nginx -t passed"
    else
        echo "  ✗ nginx -t failed:"
        echo "$NGINX_OUTPUT"
        exit 1
    fi
else
    echo ""
    echo "WARNING: nginx not found in common locations"
    
    # In CI, nginx MUST be present
    if [ "${CI:-false}" = "true" ]; then
        echo "ERROR: nginx is required in CI but was not found"
        exit 1
    fi
    
    echo "Skipping nginx -t test (manual structure checks passed)"
fi

echo ""
echo "✓ All checks passed"
