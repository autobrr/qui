---
status: accepted
date: 2026-11-20
---

# Tracker icon requests reach only public addresses

qui downloads an icon for each tracker. The author of a torrent chooses the tracker host, and the page of the tracker chooses the icon URLs and the redirects. A plain HTTP client would let a torrent make qui send requests into the local network of the server.

The icon service uses a transport that connects only to public addresses. A direct request checks the address in the dialer, so each connection meets the check, also the connection of a redirect. A request through `HTTP_PROXY` or `HTTPS_PROXY` dials only the proxy, which the operator configured and which is often on the local network. For such a request qui first resolves the target host and refuses a private address.

## Consequences

- qui downloads no icon for a tracker on a private address.
- The proxy resolves the target again. A DNS server that gives qui and the proxy different answers gets past the check for proxied requests. This gap is accepted.

## Considered options

- **No restriction.** Rejected: a torrent could then probe the local network of the server.
