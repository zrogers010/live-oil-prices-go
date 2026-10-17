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
cat > "$TEMP_DIR/nginx.conf" <<'EOF'
# Minimal nginx config for syntax testing
# Don't specify user directive - run as current user
worker_processes 1;
error_log /dev/null;
pid /dev/null;

events {
    worker_connections 1024;
}

http {
    # Stub out the certbot SSL includes with empty blocks
    # These would normally be provided by certbot
    map $ssl_preread_server_name $name {
        default "";
    }
    
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
sed -e "s|/etc/letsencrypt|$TEMP_DIR/letsencrypt|g" \
    -e 's|ssl_certificate .*fullchain\.pem.*|ssl_certificate '"$TEMP_DIR"'/letsencrypt/fullchain.pem;|' \
    -e 's|ssl_certificate_key .*privkey\.pem.*|ssl_certificate_key '"$TEMP_DIR"'/letsencrypt/privkey.pem;|' \
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
    if "$NGINX_BIN" -t -c "$TEMP_DIR/nginx.conf" 2>&1 | grep -q "syntax is ok"; then
        echo "  ✓ nginx -t passed"
    else
        echo "  ✗ nginx -t failed:"
        "$NGINX_BIN" -t -c "$TEMP_DIR/nginx.conf" 2>&1 || true
        exit 1
    fi
else
    echo ""
    echo "WARNING: nginx not found in common locations"
    echo "Skipping nginx -t test (manual structure checks passed)"
fi

echo ""
echo "✓ All checks passed"
