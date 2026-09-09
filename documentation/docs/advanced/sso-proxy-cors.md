---
sidebar_position: 3
title: SSO Proxies and CORS
description: Run qui behind an SSO proxy and handle CORS.
---

# SSO proxies and CORS

When qui runs behind an SSO proxy, such as Cloudflare Access or Pangolin, an expired session redirects API `fetch()` calls to the proxy auth origin. If the **proxy** does not send CORS headers, browsers block cross-origin redirects. You then see errors like "CORS request did not succeed" or "NetworkError". If you use a same-origin setup, qui needs no CORS configuration and keeps CORS disabled.

## What qui does

- qui detects likely SSO or CORS failures on `/api/*` requests.
- qui performs a single top-level navigation so the SSO login completes.

## What you must configure

- If possible, keep the auth flow same-origin.
- Configure CORS **on the SSO proxy**, not in qui, for the auth endpoints.
- If required, allow credentials and handle the `OPTIONS` preflight.

## Optional qui allowlist

If another trusted website in the user's browser must call qui from a different origin, set an explicit allowlist:

```bash
QUI__CORS_ALLOWED_ORIGINS=https://panel.example.com
```

qui accepts only explicit origins (`http(s)://host[:port]`). qui rejects wildcards and values with a path, query, or fragment.

If you still get CORS errors after you configure the proxy, capture the browser console error and open an issue.

## Real-time updates and reverse-proxy buffering

qui pushes live torrent, stats, and instance-health updates to the UI over a Server-Sent Events (SSE) stream at `GET /api/stream`. The RSS view uses a similar stream. SSE is a long-lived HTTP response that the server flushes incrementally. Some reverse proxies, nginx included, **buffer responses by default**. If a proxy buffers responses, the UI freezes or stays stuck on "reconnecting" until the buffer fills.

If the dashboard and torrent list do not update in real time behind your proxy, disable response buffering and allow long-lived connections for the stream endpoint:

- **nginx**: for the qui location, or specifically `~ ^/api/stream` and `~ ^/api/instances/[0-9]+/rss/events` (prefix both with your base URL if you set one):
  ```nginx
  proxy_buffering off;
  proxy_cache off;
  proxy_read_timeout 1h;
  proxy_set_header Connection "";   # keep the upstream connection open
  proxy_http_version 1.1;
  ```
  qui already sends `X-Accel-Buffering: no`, but `proxy_buffering off` is the reliable switch.
- **Traefik**: SSE works without buffering by default. Ensure that no `buffering` middleware (`maxResponseBodyBytes` / `memResponseBodyBytes`) applies to the qui router.
- **Caddy**: `reverse_proxy` streams responses without buffering by default. Caddy needs no extra configuration.

Set any idle or read timeout on the proxy to more than a few seconds. qui sends a heartbeat every 5 seconds, and the client reconnects automatically. If the proxy timeout is too short, the proxy triggers unnecessary reconnects. Do not apply compression middleware to `text/event-stream` responses.
