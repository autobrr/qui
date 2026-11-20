---
status: accepted
date: 2026-11-16
---

# Community themes live in the qui repo

The community themes are files in `internal/themes/assets/premium/` of the qui repo. They still need the license, and their MIT license file stays beside them. This replaces ADR 0038, in which each build fetched the themes from `main` of `autobrr/qui-community-themes`.

With the fetch, a qui commit did not pin its themes, a build without network left them out with only a warning, and a theme change skipped the CI that checks the theme colors. The separate repo got few pull requests from outside contributors, so the move costs little. A theme author now opens a pull request on qui. The reasons in ADR 0038 to build the themes in, and not download them at runtime, still hold.
