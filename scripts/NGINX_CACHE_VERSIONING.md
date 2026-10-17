# Asset Versioning & Rollback Blocking (nginx change, apply at deploy)

`deploy.sh` does not manage nginx, so this change has to be made on the
server by hand after the Go code is deployed and tested. Nothing here has
been applied to prod.

## Problem

Production nginx serves `/js/` and `/css/` with `expires 7d;` and
`add_header Cache-Control "public, immutable";`, causing returning visitors
to run stale JavaScript for up to 7 days. After PR #42 changed the polling
logic, users with cached bundles continue polling every 2s, generating ~470
req/min to `/api/charts/WTI` and `/api/charts/BRENT`.

Additionally, rollback directories `web/static/js.rollback-20261016/`,
`js.rollback-20261017/`, and `js.rollback-20261017b/` are publicly accessible.

## Solution

The Go application now computes content hashes at startup and renders
versioned asset URLs in templates: `/js/app.js?v=abc123def456`. Nginx must:

1. Serve versioned requests (`?v=<hash>`) with long immutable cache
2. Serve unversioned requests with short cache (5 minutes)
3. Return 404 for any path containing `.rollback`

## Exact Diff Against Live Config

This diff applies to `/etc/nginx/conf.d/liveoilprices.conf` as it exists
after the apex redirect change (scripts/NGINX_APEX_REDIRECT.md).

```diff
 # Reference copy of /etc/nginx/conf.d/liveoilprices.conf on prod AFTER the
-# apex -> www redirect change. It is the live certbot-managed file plus two
-# edits (see scripts/NGINX_APEX_REDIRECT.md): the main server block answers
-# only www.liveoilprices.com, and a new 443 block 301s the apex to www.
-# deploy.sh does not install this file; apply it by hand per that doc.
+# apex -> www redirect change and asset versioning fix. It is the live
+# certbot-managed file plus edits documented in scripts/NGINX_APEX_REDIRECT.md
+# and scripts/NGINX_CACHE_VERSIONING.md.
+# deploy.sh does not install this file; apply it by hand per those docs.
+
+# Asset versioning: versioned requests with valid hash (?v=12-hex-chars) get
+# long immutable cache, everything else gets short cache. conf.d files are
+# included in http context, so this map is valid here.
+map $arg_v $lop_static_cc {
+    "~^[0-9a-f]{12}$" "public, max-age=31536000, immutable";
+    default           "public, max-age=300";
+}
+
 server {
     server_name www.liveoilprices.com;

     gzip on;
     gzip_types text/plain text/css application/json application/javascript text/xml application/xml text/javascript image/svg+xml;
     gzip_min_length 256;
     gzip_vary on;

+    # Block any path containing .rollback (case-insensitive, e.g.
+    # /js.rollback-20261017b/app.js or /css/x.ROLLBACK-1/styles.css).
+    # Regex location beats prefix locations /css/ and /js/, so it covers
+    # /js.rollback-*/... paths.
+    location ~* \.rollback {
+        return 404;
+    }
+
     location /css/ {
         alias /home/deploy/liveoilprices/web/static/css/;
-        expires 7d;
-        add_header Cache-Control "public, immutable";
+        add_header Cache-Control $lop_static_cc;
         access_log off;
     }

     location /js/ {
         alias /home/deploy/liveoilprices/web/static/js/;
-        expires 7d;
-        add_header Cache-Control "public, immutable";
+        add_header Cache-Control $lop_static_cc;
         access_log off;
     }
```

- The `map` directive validates that `$arg_v` is exactly 12 hexadecimal characters (matching the Go asset hasher's output). Only valid hashes get the long immutable cache; everything else (including empty, garbage, or wrong-length values) gets the short 300-second cache.
- The `map` is placed at the top level of the conf.d file, which is included in nginx's `http` context, so it's valid there.
- The `add_header` directive no longer has `always`, so 404 responses don't get cached for a year.
- The `location ~* \.rollback` block uses `~*` for case-insensitive matching and is placed inside the main server block as the **first** location directive. Regex locations have higher precedence than prefix locations, so this catches any path containing `.rollback` anywhere (case-insensitive), including `/js.rollback-20261017b/app.js`, `/js/x.rollback-1/app.js`, and `/css/styles.ROLLBACK-2`.

## Apply (after Go deployment, PR #43 merged and deployed)

### 0. Create manual snapshot of 268dd9b (before first deploy with new deploy.sh)

**Important:** The first deploy with the new `deploy.sh` (from PR #43) needs a baseline snapshot to roll back to. Create this manually **before** running the new deploy.sh:

```bash
# On the EC2 host, as deploy user
cd /home/deploy/liveoilprices
ROLLBACK_DIR="/home/deploy/liveoilprices-rollbacks"
TIMESTAMP=$(date +%Y%m%d-%H%M%S)
SNAPSHOT="$ROLLBACK_DIR/$TIMESTAMP"

# Create the snapshot directory
mkdir -p "$SNAPSHOT"

# Snapshot the current running state (from 268dd9b)
cp bin/server "$SNAPSHOT/server"
cp -r web/templates "$SNAPSHOT/templates"
cp -r web/static/js "$SNAPSHOT/js"
cp -r web/static/css "$SNAPSHOT/css"

# Record the git commit (268dd9b is the base)
echo "268dd9b73a5395090b11c02335b3539711bd2c6e" > "$SNAPSHOT/COMMIT"

echo "Manual snapshot created at $SNAPSHOT"
ls -lh "$SNAPSHOT"
```

**To restore this manual snapshot:**

```bash
# Stop the service
sudo systemctl stop liveoilprices

# Restore the files
cd /home/deploy/liveoilprices
cp "$SNAPSHOT/server" bin/server
cp -r "$SNAPSHOT/templates" web/templates
cp -r "$SNAPSHOT/js" web/static/js
cp -r "$SNAPSHOT/css" web/static/css

# Restore the git commit
git checkout -f 268dd9b73a5395090b11c02335b3539711bd2c6e

# Restart the service
sudo systemctl start liveoilprices

# Verify health
curl http://127.0.0.1:8080/api/health
```

### 1. Move existing rollback directories out of web/static/

**Use the provided script** to safely move legacy rollback directories. The script refuses to overwrite existing destinations and validates each move:

```bash
# On the EC2 host, as deploy user
cd /home/deploy/liveoilprices

# Run the one-time migration script
bash scripts/move-legacy-rollbacks.sh
```

The script moves:
- `web/static/js.rollback-20261016` → `/home/deploy/liveoilprices-rollbacks/legacy-20261016`
- `web/static/js.rollback-20261017` → `/home/deploy/liveoilprices-rollbacks/legacy-20261017`
- `web/static/js.rollback-20261017b` → `/home/deploy/liveoilprices-rollbacks/legacy-20261017b`
- Any other `*.rollback-*` files under `web/static/js` or `web/static/css`

The script will not overwrite existing destinations and reports a summary of moved/skipped/error items.

### 2. Back up and apply nginx config

Copy `scripts/nginx-prod.conf` from the repo to the server, then:

```bash
# On the EC2 host
# 1. Back up the live config
BACKUP="/root/liveoilprices.conf.bak-$(date +%Y%m%d-%H%M%S)"
sudo cp -a /etc/nginx/conf.d/liveoilprices.conf "$BACKUP"
echo "Backup saved to $BACKUP"

# 2. Copy candidate config to temp location
# (manually scp or copy scripts/nginx-prod.conf to /tmp/liveoilprices.conf)

# 3. Diff the candidate against the live certbot-managed config
diff -u /etc/nginx/conf.d/liveoilprices.conf /tmp/liveoilprices.conf | less
# Review the diff to ensure only the expected changes are present

# 4. Install the candidate config
sudo install -m 0644 -o root -g root /tmp/liveoilprices.conf /etc/nginx/conf.d/liveoilprices.conf

# 5. Test and automatically restore on failure
if ! sudo nginx -t; then
    echo "nginx -t failed, restoring backup"
    sudo cp "$BACKUP" /etc/nginx/conf.d/liveoilprices.conf
    exit 1
fi

# 6. Reload nginx
sudo systemctl reload nginx
echo "nginx reloaded successfully"
```

### 3. Verify the fix

```bash
# Test rollback paths return 404
curl -I https://www.liveoilprices.com/js.rollback-20261017b/app.js
# Expected: HTTP/1.1 404 Not Found

curl -I https://www.liveoilprices.com/js.rollback-20261016/app.js
# Expected: HTTP/1.1 404 Not Found

# Test versioned assets get long immutable cache (use a real 12-hex hash)
curl -I "https://www.liveoilprices.com/js/app.js?v=067f895cae30"
# Expected: Cache-Control: public, max-age=31536000, immutable

# Test unversioned assets get short cache
curl -I https://www.liveoilprices.com/js/app.js
# Expected: Cache-Control: public, max-age=300

# Test HTML pages get no-cache (from Go, not nginx)
curl -I https://www.liveoilprices.com/
# Expected: Cache-Control: no-cache

# Verify no duplicate Cache-Control headers
curl -sI https://www.liveoilprices.com/js/app.js | grep -i cache-control
# Should show exactly one Cache-Control header

curl -sI "https://www.liveoilprices.com/js/app.js?v=test" | grep -i cache-control
# Should show exactly one Cache-Control header

# Verify no Expires header on /js/ or /css/ (we removed expires directive)
curl -sI https://www.liveoilprices.com/js/app.js | grep -i expires
# Should return nothing (no Expires header)
```

### 4. Verify the rollback directories moved correctly

```bash
ls -lh /home/deploy/liveoilprices-rollbacks/
# Should show: legacy-20261016, legacy-20261017, legacy-20261017b

# Verify they are not in web/static anymore
ls web/static/ | grep rollback
# Should return nothing
```

## Rollback

If something breaks:

```bash
sudo cp -a /root/liveoilprices.conf.rollback-<timestamp> /etc/nginx/conf.d/liveoilprices.conf
sudo nginx -t && sudo systemctl reload nginx
```

If you moved the `map` to `/etc/nginx/nginx.conf`, restore the previous
version of that file as well.

## Rate Limiting (Not Implemented)

The user requested optional per-IP 429 rate limiting for `/api/charts/*` to
prevent hot loops. This is **not included** in this change because:

1. Without production traffic patterns, it's unclear what threshold would
   constitute "far above normal polling"
2. Enabling it risks breaking legitimate users
3. The asset versioning fix should eliminate the stale-JS hot loops

If rate limiting is needed later, add these blocks to the nginx config:

```nginx
# At http level (in nginx.conf or at the top of liveoilprices.conf)
limit_req_zone $binary_remote_addr zone=api_charts_limit:10m rate=10r/s;

# Inside the www.liveoilprices.com server block, before location /
location ~ ^/api/charts/ {
    limit_req zone=api_charts_limit burst=20 nodelay;
    limit_req_status 429;
    add_header Retry-After "10" always;
    
    proxy_pass http://127.0.0.1:8080;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
}
```

This would allow 10 requests/second per IP with a burst of 20, returning 429
for excess requests with a 10-second Retry-After header.
