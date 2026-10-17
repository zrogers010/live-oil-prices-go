# Apex -> www redirect (server-side change, apply at deploy)

`deploy.sh` does not manage nginx, so this change has to be made on the
server by hand. Nothing here has been applied to prod.

## Current state (checked read-only)

- `/etc/nginx/conf.d/liveoilprices.conf` is the certbot-managed version of
  `scripts/nginx.conf`. Its 443 server block has
  `server_name liveoilprices.com www.liveoilprices.com;`, so both hosts get
  `200` over HTTPS and both are indexable.
- Port 80 sends each host to HTTPS on the same host (certbot's `if` blocks).
- The certificate `/etc/letsencrypt/live/liveoilprices.com/` has SANs
  `liveoilprices.com` and `www.liveoilprices.com`, so the apex redirect can
  reuse it. Renewal uses the nginx authenticator.
- The app now emits `https://www.liveoilprices.com/...` for canonical, og:url,
  JSON-LD, sitemap.xml and robots.txt, so redirect and canonical agree.

## Change

`scripts/nginx-prod.conf` is the full target file. Compared with the live
file it has exactly two edits:

1. The main 443 block answers only `www.liveoilprices.com`.
2. A new 443 block for `liveoilprices.com` reuses the same certificate and does
   `location / { return 301 https://www.liveoilprices.com$request_uri; }`.
   `$request_uri` keeps path and query string. The redirect is inside
   `location /` so the exact-match `/.well-known/acme-challenge/<token>`
   locations certbot inserts during renewal still take priority.

The port 80 block is left unchanged, so `http://liveoilprices.com/x` goes to
`https://liveoilprices.com/x` and then to `https://www.liveoilprices.com/x`
(two hops). That keeps the ACME HTTP-01 path exactly as it is today.

## Apply (Zach, after approval)

```sh
# on the EC2 host
sudo cp -a /etc/nginx/conf.d/liveoilprices.conf /root/liveoilprices.conf.bak-$(date +%Y%m%d%H%M%S)
# copy scripts/nginx-prod.conf from this repo to /tmp/liveoilprices.conf first, then:
sudo install -m 0644 -o root -g root /tmp/liveoilprices.conf /etc/nginx/conf.d/liveoilprices.conf
sudo nginx -t                      # must say "syntax is ok" and "test is successful"
sudo systemctl reload nginx        # only if nginx -t passed
```

Keep the backup outside `/etc/nginx/conf.d/` because every `*.conf` in that
directory is included.

## Verify

```sh
curl -sI 'https://liveoilprices.com/commodity/BRENT?x=1'   # 301, Location: https://www.liveoilprices.com/commodity/BRENT?x=1
curl -sI 'https://www.liveoilprices.com/'                  # 200
curl -sI 'http://liveoilprices.com/charts?x=1'             # 301 -> https://liveoilprices.com/charts?x=1 (then 301 to www)
curl -sI 'http://www.liveoilprices.com/'                   # 301 -> https://www.liveoilprices.com/
sudo certbot renew --dry-run                               # renewal still works for both names
```

## Rollback

```sh
sudo cp -a /root/liveoilprices.conf.bak-<timestamp> /etc/nginx/conf.d/liveoilprices.conf
sudo nginx -t && sudo systemctl reload nginx
```
