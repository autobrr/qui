---
sidebar_position: 50
title: SSO Proxies and CORS
---

# SSO Proxies and CORS

When qui is behind an SSO proxy (Cloudflare Access, Pangolin, etc.), expired sessions can redirect API `fetch()` calls to the proxy's auth origin. Browsers block cross-origin redirects unless the **proxy** sends CORS headers, so you may see errors like "CORS request did not succeed" or "NetworkError". In normal same-origin setups, qui does not need any CORS configuration and keeps CORS disabled.

## What qui does

- Detects likely SSO/CORS failures on `/api/*` requests.
- Performs a single top-level navigation so the SSO login can complete.

## What you must configure

- Keep the auth flow same-origin if possible.
- Configure CORS **on the SSO proxy** (not in qui) for the auth endpoints.
- Allow credentials and handle `OPTIONS` preflight when required.

## Optional qui allowlist

If another trusted website running in the user's browser must call qui from a different origin on the user's behalf, set an explicit allowlist:

```bash
QUI__CORS_ALLOWED_ORIGINS=https://panel.example.com
```

Only explicit origins are accepted (`http(s)://host[:port]`). Wildcards and path/query/fragment values are rejected.

If you still hit CORS errors after proxy configuration, capture the browser console error and open an issue.

## Real-time updates and reverse-proxy buffering

qui pushes live torrent, stats, and instance-health updates to the UI over a
Server-Sent Events (SSE) stream at `GET /api/stream` (the RSS view uses a similar
stream). SSE is a long-lived HTTP response that the server flushes incrementally.
Most reverse proxies **buffer responses by default**, which holds events back and
makes the UI look frozen or stuck on "reconnecting" until the buffer fills.

If the dashboard and torrent list do not update in real time behind your proxy,
disable response buffering and allow long-lived connections for the stream
endpoint:

- **nginx** — for the qui location (or specifically `~ ^/api/stream`):
  ```nginx
  proxy_buffering off;
  proxy_cache off;
  proxy_read_timeout 1h;
  proxy_set_header Connection "";   # keep the upstream connection open
  proxy_http_version 1.1;
  ```
  qui already sends `X-Accel-Buffering: no` style flushing, but `proxy_buffering off`
  is the reliable switch.
- **Traefik** — SSE works without buffering by default; just ensure no
  `buffering` middleware (`maxResponseBodyBytes` / `memResponseBodyBytes`) is
  applied to the qui router.
- **Caddy** — `reverse_proxy` streams responses without buffering by default; no
  extra configuration is required.

Also make sure any idle/read timeout on the proxy is comfortably longer than a few
seconds. qui sends a heartbeat every 5s and the client reconnects automatically,
but an aggressive proxy timeout will cause unnecessary reconnects. Compression
middlewares should not be applied to `text/event-stream` responses.
