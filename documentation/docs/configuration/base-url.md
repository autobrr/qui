---
sidebar_position: 3
title: Base URL
---

# Base URL Configuration

If you need to serve qui from a subdirectory (e.g., `https://example.com/qui/`), you can configure the base URL.

## Using Environment Variable

```bash
QUI__BASE_URL=/qui/ ./qui
```

## Using Configuration File

Edit your `config.toml`:

```toml
baseUrl = "/qui/"
```

## With Nginx Reverse Proxy

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
