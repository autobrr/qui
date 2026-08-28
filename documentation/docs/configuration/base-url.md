---
sidebar_position: 3
title: Base URL
description: Serve qui from a subdirectory behind a reverse proxy.
---

# Base URL Configuration

If you serve qui from a subdirectory (for example `https://example.com/qui/`), configure the base URL.

qui normalizes the value at startup. `/qui/`, `/qui`, and `qui` all become `/qui/`. Restart qui after you change the base URL.

## Environment variable

```bash
QUI__BASE_URL=/qui/ ./qui serve
```

## Configuration file

Edit your `config.toml`:

```toml
baseUrl = "/qui/"
```

## Nginx reverse proxy

```nginx
# Redirect /qui to /qui/ for proper SPA routing
location = /qui {
    return 301 /qui/;
}

location /qui/ {
    proxy_pass http://localhost:7476/qui/;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
}
```
