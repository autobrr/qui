---
status: accepted
date: 2026-10-30
---

# A cross-seed route needs a link mode

Some checks look at other instances: the automations delete modes that preserve or include cross-seeds (ADR 0033), the orphan scan peer rule, and HARDLINK_SCOPE_CROSS. They see only instances with local file access and trusted file identity. A Cross-seed instance needs no filesystem access, so these checks do not see it. The plain delete with files mode of automations checks no other instance. A routed cross-seed in regular mode points at the files of the Source instance. A rule or a scan on the Source instance can then delete files that the Cross-seed instance seeds, and nobody sees a warning.

A route works only with hardlink or reflink mode. A routed add never falls back to regular mode. The link settings of the Source instance decide the mode and the base folder. The orphan scan and the hardlink scope of the Source instance read the files of the Cross-seed instance, mapped back to Source instance paths. A Cross-seed instance that is turned off in qui counts as unavailable. The orphan scan of the Source instance then fails with a message to enable it or clear the route, and hardlink scope reads unknown and holds deletes. A route is one hop, and several Source instances can share one Cross-seed instance. Neither instance can be a remote instance. Issue #3256.

## Considered options

- **Allow regular mode and make every delete path look at the Cross-seed instance.** Rejected. ADR 0033 makes two automations delete modes look at other instances, but only at instances with local file access, because qui cannot trust the file identity that a remote host reports. A Cross-seed instance needs no file access, so qui cannot find its files on disk. ADR 0033 also does not cover the plain delete with files mode of automations, orphan scan, or hardlink scope. Every future delete path then has to remember the route.
- **Allow regular mode with a docs warning only.** Rejected. A warning does not stop a rule or a scan from deleting the files that the Cross-seed instance seeds.
- **Ignore a Cross-seed instance that is turned off in qui, as ADR 0033 does for automations.** Rejected. A route names one exact instance, and the user can clear the route in one step. If qui ignores the turned-off Cross-seed instance, the orphan scan of the Source instance can delete reflinked files, or files that the Cross-seed instance downloaded.

## Consequences

- The Source instance needs the Write capability in local mode, so an instance without local filesystem access cannot route. Remote instances have Write since #3238, but a remote Source instance stays out until it gets its own decision. The #2916 revisit list holds it.
- The Cross-seed instance needs no filesystem access, and it cannot be in remote mode.
- A change that removes protection from routed torrents asks the user to confirm.
- Related to ADR 0033. The route does not rely on its checks of other instances, and it does not follow its rule for a turned-off instance.
- Related to ADR 0022. The routed add goes through the Adder, and the route gives the Adder facts.
