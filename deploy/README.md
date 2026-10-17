# Deployment Configuration

This directory contains deployment configuration files for the Live Oil Prices production server.

## Nginx Configuration

### Current Setup

The production server currently serves static assets through the Go application's `http.FileServer`, which sets `Cache-Control: max-age=604800, public, immutable` headers. This causes stale JavaScript to be cached for up to 7 days.

### Proposed Fix

The nginx configuration in `nginx/liveoilprices.conf` implements proper cache headers:

- **Versioned assets** (with `?v=<hash>` query param): `max-age=31536000, public, immutable`
- **Unversioned assets**: `max-age=300, public` (5 minutes)
- **HTML pages**: `Cache-Control: no-cache`
- **Blocks `.rollback` paths**: Returns 404 for any path containing `.rollback`

### One-Time Server Commands

**IMPORTANT: These commands should be run ONCE on the production server by the deploy user. DO NOT run them in CI or automatically.**

#### 1. Move Existing Rollback Directories

The production server has rollback directories inside `web/static/` that are currently served publicly. Move them to the new rollback location:

```bash
# Run as deploy user on production server
cd /home/deploy/liveoilprices

# Create rollback directory
mkdir -p /home/deploy/liveoilprices-rollbacks

# Move existing rollback directories
if [ -d "web/static/js.rollback-20261016" ]; then
    mv web/static/js.rollback-20261016 /home/deploy/liveoilprices-rollbacks/20261016-manual
fi

if [ -d "web/static/js.rollback-20261017" ]; then
    mv web/static/js.rollback-20261017 /home/deploy/liveoilprices-rollbacks/20261017-manual
fi

if [ -d "web/static/js.rollback-20261017b" ]; then
    mv web/static/js.rollback-20261017b /home/deploy/liveoilprices-rollbacks/20261017b-manual
fi

# Find and move any other .rollback files/directories
find web/static -name "*\.rollback*" -print0 2>/dev/null | while IFS= read -r -d '' item; do
    basename_item=$(basename "$item")
    mv "$item" "/home/deploy/liveoilprices-rollbacks/${basename_item}"
done

echo "Rollback directories moved to /home/deploy/liveoilprices-rollbacks/"
ls -lh /home/deploy/liveoilprices-rollbacks/
```

#### 2. Deploy Nginx Configuration

If the production server uses nginx (confirm with `nginx -v`), deploy the nginx configuration:

```bash
# Copy the nginx config to the appropriate location
sudo cp deploy/nginx/liveoilprices.conf /etc/nginx/conf.d/liveoilprices.conf

# Or if using sites-available/sites-enabled:
# sudo cp deploy/nginx/liveoilprices.conf /etc/nginx/sites-available/liveoilprices.conf
# sudo ln -sf /etc/nginx/sites-available/liveoilprices.conf /etc/nginx/sites-enabled/

# Test the configuration
sudo nginx -t

# If the test passes, reload nginx
sudo systemctl reload nginx

# Verify nginx is running
sudo systemctl status nginx
```

#### 3. Verify the Fix

After deploying:

```bash
# Test that rollback paths return 404
curl -I https://www.liveoilprices.com/js.rollback-20261017b/app.js
# Should return: HTTP/1.1 404 Not Found

# Test that versioned assets get long cache
curl -I https://www.liveoilprices.com/js/app.js?v=abc123
# Should return: Cache-Control: max-age=31536000, public, immutable

# Test that unversioned assets get short cache
curl -I https://www.liveoilprices.com/js/app.js
# Should return: Cache-Control: max-age=300, public

# Test that HTML gets no-cache
curl -I https://www.liveoilprices.com/
# Should return: Cache-Control: no-cache
```

## Rollback Process

With this PR merged, rollback snapshots are saved to `/home/deploy/liveoilprices-rollbacks/<timestamp>/` instead of inside `web/static/`.

To roll back to a previous deploy:

```bash
# List available rollback snapshots
ls -lt /home/deploy/liveoilprices-rollbacks/

# Restore from a specific snapshot
ROLLBACK_DATE="20261017-143022"  # Use actual timestamp from ls output
cd /home/deploy/liveoilprices

# Back up current state first
cp -r web/static/js web/static/js.backup-$(date +%Y%m%d-%H%M%S)
cp bin/server bin/server.backup-$(date +%Y%m%d-%H%M%S)

# Restore from rollback
cp -r /home/deploy/liveoilprices-rollbacks/$ROLLBACK_DATE/js web/static/
cp -r /home/deploy/liveoilprices-rollbacks/$ROLLBACK_DATE/css web/static/
cp /home/deploy/liveoilprices-rollbacks/$ROLLBACK_DATE/server bin/server

# Restart the service
sudo systemctl restart liveoilprices
```

## Notes

- The deploy script (`scripts/deploy.sh`) now automatically creates rollback snapshots in `/home/deploy/liveoilprices-rollbacks/<timestamp>/` before each build.
- The 10 most recent rollback snapshots are kept; older ones are automatically deleted.
- The Go application blocks any HTTP request containing `.rollback` in the path (returns 404).
- Asset versioning is handled by the Go application computing SHA256 hashes at startup and rendering URLs like `/js/app.js?v=abc123def456` in templates.
