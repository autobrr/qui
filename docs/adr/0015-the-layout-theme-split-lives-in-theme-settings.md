---
status: accepted
date: 2026-10-05
---

# The layout theme split lives in theme_settings

The mobile theme is a second row in `theme_settings`, keyed by slot. It is not one more client setting. The login page reads the theme before authentication, and client settings require authentication. A client setting would make the login page show the desktop theme on a phone.

The mobile row is also the opt-in. When it exists, the split is on. This removes a flag column and a state where the flag and the row disagree. The cost is that turning the split off deletes the mobile choice.

Issue #3019.
