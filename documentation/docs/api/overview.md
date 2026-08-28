---
sidebar_position: 1
title: API
description: Authenticate against the qui API with API keys.
---

# API overview

## Documentation

qui serves interactive API documentation with Swagger UI at `/api/docs`. You can browse all endpoints, view request and response schemas, and test API calls from your browser.

## API keys

API keys give programmatic access to the qui API without session cookies. Create and manage them in Settings → API Keys.

qui also has client proxy API keys. Those keys let external applications, for example Sonarr or autobrr, reach your qBittorrent instances through the qui proxy. They do not grant access to the qui API. See [Reverse Proxy](../features/reverse-proxy.md).

Include your API key in the `X-API-Key` header:

```bash
curl -H "X-API-Key: YOUR_API_KEY_HERE" \
  http://localhost:7476/api/instances
```

## Security notes

- qui shows an API key only once, at creation. Save it in a safe place.
- You can revoke each key on its own. Other keys remain active.
- A key has the same permissions as the main user account.
