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
# Must match exactly \.rollback (not .rollback2 or other variants)
if echo "$STRIPPED" | grep -qE 'location[[:space:]]+~\*[[:space:]]+.*\\\.rollback[[:space:]]*\{'; then
    printf "  ✓ rollback location is active with ~* (case-insensitive)\n"
else
    printf "  ✗ rollback location missing or commented out\n"
    printf "     Expected: location ~* .*\\\\.rollback { return 404; }\n"
    exit 1
fi

# Check that .rollback location's FIRST return/deny directive is 404 or deny all
# Extract the location block, tokenize, and find the first return/deny statement
ROLLBACK_CHECK=$(awk '
BEGIN {
    in_block = 0
    depth = 0
    found_first = 0
}
{
    # Match location exactly with \.rollback (not .rollback2)
    if ($0 ~ /location[[:space:]]+~\*[[:space:]]+.*\\\.rollback[[:space:]]*\{/) {
        in_block = 1
        depth = 0
        found_first = 0
    }
    
    if (in_block) {
        original = $0
        gsub(/#.*/, "")  # strip comments
        
        # Tokenize on whitespace and ; to handle multi-statement lines
        n = split($0, tokens, /[[:space:];]+/)
        
        for (i = 1; i <= n; i++) {
            token = tokens[i]
            
            # Track braces
            if (token ~ /\{/) depth++
            if (token ~ /\}/) {
                depth--
                if (depth == 0) {
                    if (!found_first) {
                        print "NO_DIRECTIVE"
                    }
                    exit
                }
            }
            
            # Look for first return/deny/allow directive
            if (!found_first && depth > 0) {
                if (token == "return") {
                    # Next token should be the status code
                    next_token = tokens[i+1]
                    if (next_token == "404") {
                        print "GOOD: return 404"
                        found_first = 1
                    } else if (next_token ~ /^[0-9]+$/) {
                        print "BAD: return " next_token
                        found_first = 1
                    }
                } else if (token == "deny") {
                    next_token = tokens[i+1]
                    if (next_token == "all") {
                        print "GOOD: deny all"
                        found_first = 1
                    } else {
                        print "BAD: deny " next_token
                        found_first = 1
                    }
                } else if (token == "allow") {
                    # Allow directive before deny/return 404 is wrong
                    next_token = tokens[i+1]
                    print "BAD: allow " next_token
                    found_first = 1
                }
            }
        }
    }
}
' "$SCRIPT_DIR/nginx-prod.conf")

if echo "$ROLLBACK_CHECK" | grep -q "^GOOD:"; then
    echo "  ✓ .rollback location blocks access with return 404 or deny all (first directive)"
elif echo "$ROLLBACK_CHECK" | grep -q "^BAD:"; then
    # shellcheck disable=SC2001
    DIRECTIVE=$(echo "$ROLLBACK_CHECK" | sed 's/^BAD: //')
    echo "  ✗ .rollback location's first return/deny is not blocking: $DIRECTIVE"
    exit 1
else
    echo "  ✗ .rollback location missing return 404 or deny all directive"
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

# Check for ANY expires directive at any depth, except in allowlisted locations
# Allowlist: only "location /robots.txt" or "location = /robots.txt" (exact matches)
# Tokenize on whitespace, ;, {, } to handle all cases (multi-statement lines, if blocks, etc.)
EXPIRES_VIOLATIONS=$(awk '
BEGIN {
    depth = 0
    location_depth = -1
    current_location = ""
    in_location_line = 0
    location_modifier = ""
}
{
    original_line = $0
    # Strip comments
    gsub(/#.*/, "")
    
    # Tokenize: split on whitespace, braces, semicolons
    # Process character by character to handle all cases
    line_chars = $0
    n = length(line_chars)
    token = ""
    
    for (i = 1; i <= n; i++) {
        c = substr(line_chars, i, 1)
        
        # Token delimiters: whitespace, {, }, ;
        if (c ~ /[[:space:];{}]/) {
            if (token != "") {
                # Process token
                if (token == "location") {
                    in_location_line = 1
                    location_modifier = ""
                    current_location = ""
                } else if (in_location_line) {
                    # Capture modifier (= or ~*) or the path
                    if (token == "=" || token == "~" || token == "~*") {
                        location_modifier = token
                    } else if (current_location == "") {
                        current_location = token
                    }
                } else if (token == "expires") {
                    # Check if we are in an allowlisted location
                    allowed = 0
                    if (location_depth >= 0) {
                        # We are inside a location block
                        # Allow only: "location /robots.txt" or "location = /robots.txt"
                        if ((location_modifier == "" || location_modifier == "=") && current_location == "/robots.txt") {
                            allowed = 1
                        }
                    }
                    if (!allowed) {
                        if (location_depth >= 0) {
                            print "Disallowed expires in location \"" current_location "\" (modifier: \"" location_modifier "\") at line " NR
                        } else if (depth > 0) {
                            print "Disallowed expires at server/if/nested context at line " NR
                        } else {
                            print "Disallowed expires at http/top level at line " NR
                        }
                    }
                }
                token = ""
            }
            
            # Process delimiter
            if (c == "{") {
                if (in_location_line) {
                    location_depth = depth
                    in_location_line = 0
                }
                depth++
            } else if (c == "}") {
                depth--
                if (location_depth == depth) {
                    location_depth = -1
                    current_location = ""
                    location_modifier = ""
                }
            }
        } else {
            token = token c
        }
    }
    
    # Process final token if any
    if (token != "") {
        if (token == "location") {
            in_location_line = 1
            location_modifier = ""
            current_location = ""
        } else if (in_location_line) {
            if (token == "=" || token == "~" || token == "~*") {
                location_modifier = token
            } else if (current_location == "") {
                current_location = token
            }
        } else if (token == "expires") {
            allowed = 0
            if (location_depth >= 0) {
                if ((location_modifier == "" || location_modifier == "=") && current_location == "/robots.txt") {
                    allowed = 1
                }
            }
            if (!allowed) {
                if (location_depth >= 0) {
                    print "Disallowed expires in location \"" current_location "\" at line " NR
                } else if (depth > 0) {
                    print "Disallowed expires at nested context at line " NR
                } else {
                    print "Disallowed expires at top level at line " NR
                }
            }
        }
    }
}
' "$SCRIPT_DIR/nginx-prod.conf")

if [ -n "$EXPIRES_VIOLATIONS" ]; then
    echo "  ✗ Disallowed 'expires' directive(s) found:"
    # shellcheck disable=SC2001
    echo "$EXPIRES_VIOLATIONS" | sed 's/^/      /'
    echo "     Only 'location /robots.txt' or 'location = /robots.txt' may use 'expires'"
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

# Find the line number of the location directive (match exactly \.rollback)
LOC_LINE=$(grep -nE 'location[[:space:]]+~\*[[:space:]]+.*\\\.rollback[[:space:]]*\{' "$SCRIPT_DIR/nginx-prod.conf" | head -1 | cut -d: -f1)
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
