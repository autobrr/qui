# Allowed Hosts checks the name received by qui

qui will offer an optional host restriction because an allowed peer IP can admit direct requests to a backend with authentication disabled.
It will check the received Host for all requests on the main HTTP listener because qui has no trusted-proxy policy for X-Forwarded-Host.
Only GET and HEAD requests to the built-in health endpoints bypass the list when the immediate connection peer is a loopback address.
This includes external health requests forwarded by a local reverse proxy.
To restrict health endpoints to local probes, block external health requests at the proxy.
Docker probes continue through this exemption.
