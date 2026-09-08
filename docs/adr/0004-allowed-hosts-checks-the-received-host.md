# Allowed Hosts checks the name received by qui

qui will offer an optional host restriction because an allowed peer IP can admit direct requests to a backend with authentication disabled.
It will check the received Host for all requests on the main HTTP listener because qui has no trusted-proxy policy for X-Forwarded-Host.
Only direct loopback health probes bypass the list, so Docker probes continue without granting unrestricted local access.
