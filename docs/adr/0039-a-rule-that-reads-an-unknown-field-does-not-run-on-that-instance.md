---
status: accepted
date: 2026-11-04
---

# A rule that reads an unknown field does not run on that instance

qBittorrent before 5.0 does not send Private, Popularity, Reannounce In and Comment in the torrent list, and before 5.2 it does not send Created By. go-qbittorrent decodes a missing value as 0, "" or false. When an Automation rule reads such a field, in a condition or in a sort, qui gives the rule no Actions on that instance and writes one activity entry. The dry run and the previews show the same skip, because they make the same check. A torrent whose own value is unknown, such as a magnet link without metadata, drops out of the rule, and the rule still runs for the other torrents. Issue #3763.

We considered two other designs. A condition that never matches on an unknown field is not safe, because the "not" switch of a condition turns it into a condition that always matches. With it, a rule "Private is false, then delete" deletes every private torrent. A warning in the rule builder alone does not stop a rule that was saved earlier, imported, or kept after a qBittorrent downgrade.

The skip is only correct while qui knows the current version. qui reads the version again on each full sync update, before any consumer reads the update. qBittorrent sends a full update after each restart, so an upgrade or downgrade takes effect before the next run of a rule.

## Consequences

- The run decides the skip before it evaluates a torrent, and writes the activity entry at once, so a failure for another rule does not hide the entry. ADR 0028 still holds: no Action reaches a torrent outside the plan step.
- A grouped delete, category or move is all or none, so a group with a member whose own value is unknown skips that action. It runs after the metadata of the member arrives.
- A rule without a version requirement on any field that it reads runs as before. A field with an access requirement, such as hardlink scope, keeps its own unknown value for each torrent.
- With an unknown version, every version requirement is unmet, so the rule skips. Each run reads a missing version again, so one failed read does not stop a rule until qui restarts.
- A scheduled run writes the activity entry once for each rule on each instance. A run that a person starts always writes it, because the person waits for the reason.
