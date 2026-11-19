---
status: accepted
date: 2026-11-19
---

# An unfinished feature merges behind an Experimental feature

An unfinished feature merges to `develop` behind a name in one list of Experimental features. While the name is off, the feature stays out of reach, and the gate ticket deletes the name when the feature opens.

## Considered Options

- A constant in the code. Rejected, because it needs a private build, so nobody can run the feature on a real qui before it opens.
- One config key for each feature. Rejected, because each feature adds a documented key and removes it again.
- A library for remote rules. Rejected, because it serves rules to many users and qui has one user per install.

## Consequences

Any user can turn on an unfinished feature, so the user docs name no feature and promise nothing about one.
