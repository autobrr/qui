---
status: accepted
date: 2026-09-11
---

# Allowed Hosts checks the received Host

An allowed peer IP admits direct requests to a backend with authentication disabled, and a DNS-rebound browser is such a peer. qui offers an optional Allowed Hosts list and checks the received Host for every request on the main HTTP listener. It ignores X-Forwarded-Host because qui has no trusted-proxy policy. Only GET and HEAD requests to the built-in health endpoints bypass the list, and only when the immediate connection peer is a loopback address. A configured list always admits `localhost`, the loopback addresses, and the machine hostname, as Sonarr and Radarr do.

## Considered options

- **Trust X-Forwarded-Host from configured proxy networks**, as Sonarr does. Rejected: qui has no trusted-network setting, and a misconfigured proxy would let the header bypass the list.
- **Reload the list on config change.** Rejected: the HTTP server snapshots the list at startup so that the general OPTIONS handler and the guard agree. A restart keeps both in one state.
- **No implicit local names.** Rejected: a list that names only the public hostname would lock out the user on the machine itself.

## Consequences

- External health requests forwarded by a local reverse proxy bypass the list. To restrict health endpoints to local probes, block them at the proxy. Docker probes continue through the exemption.
- The list is read once. A change to `allowedHosts` needs a restart.
- The list blocks DNS rebinding and nothing more. A request that uses a listed hostname is admitted and still meets authentication and the IP allowlist.
