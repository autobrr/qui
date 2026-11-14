---
status: accepted
date: 2026-11-01
---

# Peer client logos are downloaded at runtime and never shipped

qui shows a logo for each peer client in the Peers tab. The logos have many licenses: GPL-2.0, GPL-3.0, LGPL-3.0, MIT, BSD-3-Clause, and trademark terms. If qui shipped GPL-3.0 files, each qui release would fall under GPL-3.0 terms.

qui ships a list of URLs in `internal/services/peerclienticons/icons.go`. Each URL contains a full commit SHA, and each entry has the SHA-256 hash of the file. Each qui server downloads a logo the first time it needs it, makes sure that the hash matches, and keeps the file in its data directory. The license of a logo thus never applies to a qui release, and a change of an upstream license needs no action. Do not add the logo files to the repository.

## Consequences

- Each server downloads each logo one time.
- A server with no internet access, or with `trackerIconsFetchEnabled` off, shows a letter in place of the logo.
- To move a logo to a newer commit, change its URL and its hash in a release. The file name on disk contains the hash, so each server downloads the new file.

## Considered options

- **Bundle the six logos with GPL-2.0-compatible licenses, and show letters for the others.** Rejected: Deluge, rTorrent and the other GPL-3.0 clients would show no logo.
- **Change the license of qui releases to GPL-3.0.** Rejected: a license change for the whole release to show a few icons.
