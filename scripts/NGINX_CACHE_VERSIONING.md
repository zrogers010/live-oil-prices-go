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
+# Asset versioning: versioned requests (?v=hash) get long immutable cache,
+# unversioned requests get short cache. conf.d files are included in http
+# context, so this map is valid here.
+map $arg_v $lop_static_cc {
+    default "public, max-age=31536000, immutable";
+    ""      "public, max-age=300";
+}
+
 server {
     server_name www.liveoilprices.com;

     gzip on;
     gzip_types text/plain text/css application/json application/javascript text/xml application/xml text/javascript image/svg+xml;
     gzip_min_length 256;
     gzip_vary on;

+    # Block any path containing .rollback (e.g. /js.rollback-20261017b/app.js).
+    # Regex location beats prefix locations /css/ and /js/, so it covers
+    # /js.rollback-*/... paths.
+    location ~ \.rollback {
+        return 404;
+    }
+
     location /css/ {
         alias /home/deploy/liveoilprices/web/static/css/;
-        expires 7d;
-        add_header Cache-Control "public, immutable";
+        add_header Cache-Control $lop_static_cc always;
         access_log off;
     }

     location /js/ {
         alias /home/deploy/liveoilprices/web/static/js/;
-        expires 7d;
-        add_header Cache-Control "public, immutable";
+        add_header Cache-Control $lop_static_cc always;
         access_log off;
     }
```

**Important:** The `map` directive is placed at the top level of the conf.d
file, which is included in nginx's `http` context, so it's valid there.

The `location ~ \.rollback` block is placed inside the main server block as
the **first** location directive, before `/css/`, `/js/`, and `/robots.txt`.
Regex locations (using `~`) have higher precedence than prefix locations, so
this catches any path containing `.rollback` anywhere, including
`/js.rollback-20261017b/app.js` and `/js/x.rollback-1/app.js`.

## Apply (after Go deployment, PR #43 merged and deployed)

### 1. Move existing rollback directories out of web/static/

These commands must be run **before** applying the nginx config change,
while the rollback paths are still accessible:

```bash
# On the EC2 host, as deploy user
cd /home/deploy/liveoilprices
sudo -u deploy mkdir -p /home/deploy/liveoilprices-rollbacks

# Move the three known rollback directories
if [ -d "web/static/js.rollback-20261016" ]; then
    sudo -u deploy mv web/static/js.rollback-20261016 /home/deploy/liveoilprices-rollbacks/20261016-manual
fi

if [ -d "web/static/js.rollback-20261017" ]; then
    sudo -u deploy mv web/static/js.rollback-20261017 /home/deploy/liveoilprices-rollbacks/20261017-manual
fi

if [ -d "web/static/js.rollback-20261017b" ]; then
    sudo -u deploy mv web/static/js.rollback-20261017b /home/deploy/liveoilprices-rollbacks/20261017b-manual
fi

# Find and move any other *.rollback-* files/directories under web/static
find web/static/js web/static/css -name "*.rollback-*" -print0 2>/dev/null | while IFS= read -r -d '' item; do
    basename_item=$(basename "$item")
    sudo -u deploy mv "$item" "/home/deploy/liveoilprices-rollbacks/${basename_item}"
done

ls -lh /home/deploy/liveoilprices-rollbacks/
```

### 2. Back up current nginx config

```bash
# On the EC2 host
sudo cp -a /etc/nginx/conf.d/liveoilprices.conf /root/liveoilprices.conf.rollback-$(date +%Y%m%d-%H%M%S)
```

The backup is saved outside `web/static/` so it's not served.

### 3. Apply the new config

Copy `scripts/nginx-prod.conf` from the repo to the server, then:

```bash
# Copy to temp location first
# (manually scp or copy scripts/nginx-prod.conf to /tmp/liveoilprices.conf)

# Install the new config
sudo install -m 0644 -o root -g root /tmp/liveoilprices.conf /etc/nginx/conf.d/liveoilprices.conf

# Test the configuration
sudo nginx -t

# If test passes, reload nginx
sudo systemctl reload nginx
```

### 4. Verify the fix

```bash
# Test rollback paths return 404
curl -I https://www.liveoilprices.com/js.rollback-20261017b/app.js
# Expected: HTTP/1.1 404 Not Found

curl -I https://www.liveoilprices.com/js.rollback-20261016/app.js
# Expected: HTTP/1.1 404 Not Found

# Test versioned assets get long immutable cache
curl -I "https://www.liveoilprices.com/js/app.js?v=abc123"
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

### 5. Verify the rollback directories moved correctly

```bash
ls -lh /home/deploy/liveoilprices-rollbacks/
# Should show: 20261016-manual, 20261017-manual, 20261017b-manual

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
