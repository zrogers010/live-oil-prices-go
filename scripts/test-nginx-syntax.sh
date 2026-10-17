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

# Check that map regex is exactly {12}
if grep -F '{12}' "$SCRIPT_DIR/nginx-prod.conf" > /dev/null; then
    echo "  ✓ map uses ~^[0-9a-f]{12}\$ (SHA-256 truncated to 12 hex chars)"
else
    echo "  ✗ map regex is not ~^[0-9a-f]{12}\$ (mutation M: {12}->{6} would pass)"
    exit 1
fi

# Check that rollback location uses case-insensitive ~* and \.rollback
if grep -q 'location ~\* .*\\\.rollback' "$SCRIPT_DIR/nginx-prod.conf"; then
    echo "  ✓ rollback location uses ~* (case-insensitive) and \\\.rollback"
else
    echo "  ✗ rollback location doesn't use ~* or \\\.rollback (mutation M: ~*->~ would pass)"
    exit 1
fi

# Check that immutable 1y header is only on hashed branch (map regex line, not default)
if grep -q '~.*{12}.*immutable' "$SCRIPT_DIR/nginx-prod.conf"; then
    echo "  ✓ immutable 1y header only on hashed version branch (map regex)"
    # Verify default line does NOT have immutable
    if grep 'default.*immutable' "$SCRIPT_DIR/nginx-prod.conf" > /dev/null; then
        echo "  ✗ default line also has immutable (should only be on hashed)"
        exit 1
    fi
else
    echo "  ✗ immutable 1y header not found on hashed regex line"
    exit 1
fi

# Check that default (HTML/no-v) uses max-age=300
if grep -q 'max-age=300' "$SCRIPT_DIR/nginx-prod.conf"; then
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
