---
status: accepted
date: 2026-11-11
---

# A torrent counts toward one Tracker group

The Tracker Breakdown card and the Prometheus exporter counted the full bytes of a torrent under each of its tracker domains. The "counts toward statistics" tick boxes of a tracker customization let the user correct one merged tracker by hand. Now one module maps a torrent to one Tracker group: a tracker customization, one domain, Public, or No tracker. Statistics and the exporter read that module, and the tick boxes are gone.

A torrent that is not private goes to Public, because its tracker list does not show where its bytes went.

## Consequences

- The tracker rows of a period add up to the period total.
- The tracker metrics of the exporter change one time at the upgrade, and the series can be added together.
- Domains of one tracker that the user did not merge show as one row under one of the domains. A merge gives the row its name.
