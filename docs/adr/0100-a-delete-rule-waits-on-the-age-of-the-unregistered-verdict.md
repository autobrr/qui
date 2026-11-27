---
status: accepted
date: 2026-11-27
---

# A delete rule waits on the age of the unregistered verdict

A tracker can report healthy torrents as unregistered by mistake. In November 2026 one tracker did, and a delete rule removed about 330 torrents in 35 minutes. qui therefore keeps the time from which it has seen each torrent as unregistered, and a delete rule chooses a torrent for that verdict only after the Delete wait of the rule, 7 days by default. A working tracker, a stop of the torrent, or an edit of its trackers starts the time over.

The time belongs to the verdict and not to the rule. The verdict is the fact that a tracker can take back, one time serves every rule and the warning in the manual delete dialog, and a rule that does not read the verdict stays as it was. A rule deletes a torrent only when its condition matches both while the torrent counts as unregistered for as long as it has a time, and while it counts as unregistered only once the wait is over and a tracker says so now. The wait therefore never makes a rule delete a torrent that it would keep with the verdict of today.

The wait covers deletes only. A tag or a pause can be undone, so those actions act on the first match. A rule that reads the raw tracker message has no verdict and gets no wait.

The package `internal/services/automations/verdict` is the one place where a delete reads the verdict. Its waited verdict has unexported fields, and the Deletion selection takes that type, so no delete can be planned from the raw verdict. A forbidigo rule keeps the rest of the automations package away from the tracker health snapshot. A later witness, such as the API of a tracker, joins there.

## Considered Options

- **A time for each rule and torrent, counted from the first match.** Rejected: every delete rule would need it, a rule edit would start it over, and the manual delete dialog would still need the time of the verdict.
- **A hold when many torrents of one tracker turn unregistered together.** Rejected: it needs a threshold, and a fault that hits few torrents or spreads slowly stays under it.
- **A limit on the deletes of one run.** Rejected: a slow fault stays under the limit, and a large real cleanup hits it.
- **Keep the time while a torrent is stopped.** Rejected: qBittorrent forgets the tracker answer on a stop, so a torrent that starts a month later could be deleted on the first answer of the tracker.

## Consequences

- After the upgrade to the first version with the wait, a rule with the default wait deletes no torrent for the unregistered verdict for 7 days, because qui has no earlier time.
- A fault that lasts longer than the wait still deletes. The notification when torrents start to wait is what gives the user time to act.
- A torrent that waits is still deleted as the cross-seed, the hardlink copy, or the group member of another Delete trigger.
- A stopped torrent never becomes due. A rule that pauses unregistered torrents keeps a delete rule from deleting them.
- On qBittorrent 5.2 and later, qui fetches the trackers of each torrent with a stored time as it does for a flagged torrent. This adds to the fetch rules of ADR 0019.
