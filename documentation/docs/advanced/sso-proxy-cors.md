---
sidebar_position: 50
title: SSO Proxies and CORS
---

# SSO Proxies and CORS

When qui is behind an SSO proxy (Cloudflare Access, Pangolin, etc.), expired sessions can redirect API `fetch()` calls to the proxy's auth origin. Browsers block cross-origin redirects unless the **proxy** sends CORS headers, so you may see errors like "CORS request did not succeed" or "NetworkError". In normal same-origin setups, qui does not need any CORS configuration.

## What qui does

- Detects likely SSO/CORS failures on `/api/*` requests.
- Performs a single top-level navigation so the SSO login can complete.

## What you must configure

- Keep the auth flow same-origin if possible.
- Configure CORS **on the SSO proxy** (not in qui) for the auth endpoints.
- Allow credentials and handle `OPTIONS` preflight when required.

If you still hit CORS errors after proxy configuration, capture the browser console error and open an issue.
