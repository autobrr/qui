---
sidebar_position: 1
title: qui REST API and API keys
sidebar_label: API
description: Authenticate against the qui REST API with API keys, and browse every endpoint in the built-in Swagger UI.
---

# API overview

## Documentation

qui serves interactive API documentation with Swagger UI at `/api/docs`. You can browse all endpoints, view request and response schemas, and test API calls from your browser.

## API keys

API keys give programmatic access to the qui API without session cookies. Create and manage them in Settings → API Keys. Each key holds a list of Permissions and an optional Instance limit. See [API key permissions](./permissions.md).

qui also has client proxy API keys. Those keys let external applications, for example Sonarr or autobrr, reach your qBittorrent instances through the qui proxy. They do not grant access to the qui API. See [Reverse Proxy](../features/reverse-proxy.md).

Include your API key in the `X-API-Key` header:

```bash
curl -H "X-API-Key: YOUR_API_KEY_HERE" \
  http://localhost:7476/api/instances
```

## Session cookies

The session cookie is for the qui web UI. Use an API key for scripts. See [Sessions](../configuration/reference.md#sessions) for the rules that apply to cookie requests.

## Torrent updates

`GET /api/stream` sends torrent updates through Server-Sent Events. See `/api/docs` for subscription parameters and event examples.

The first event of a stream is `connection`. To add or remove subscriptions without a reconnect, send its `connectionId` to `POST /api/stream/subscriptions`. Each new subscription gets an `init` event before its other events, and the other subscriptions get no new `init`. Only the API key or session that opened the connection can change it.

Keep the version from each accepted snapshot or delta. Apply a delta only when its `baseVersion` matches that version and its `minor` increases by one within the same `major`. If the versions do not match, reconnect for a full snapshot and use REST polling until it arrives.

In a delta, omitted counts and preferences retain their previous values. Explicit `preferences: null` clears preferences. Omitted categories and tags mean empty collections.

## Security notes

- qui shows an API key only once, at creation. Save it in a safe place.
- You can revoke each key on its own. Other keys remain active.
- A key can reach only the routes that its [Permissions](./permissions.md) allow. A Browser-only route refuses every key and returns `403 API key not allowed; use the web UI`. When authentication is disabled, qui ignores API keys, so Permissions, Instance limits, and this refusal do not apply.
