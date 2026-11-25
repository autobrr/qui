---
status: accepted
date: 2026-11-25
---

# The season pack endpoints stay as a frozen adapter over the webhook check and apply

The webhook check and apply own the season pack path, so one autobrr filter covers episodes, movies, and season packs. `POST /api/cross-seed/season-pack/check` and `/apply` stay for good, because autobrr filters on the machines of users call them, and qui cannot change those filters. They are an adapter: they decode the old request, call the same season pack step as the webhook check and apply, and write the old answer.

What is frozen is the wire format: the status for each outcome, the field names, the reason strings, and the message formats, and which Season pack run each outcome writes. The matching is not frozen. It belongs to the season pack step, and a fix there changes both paths.

Two checks hold the freeze. The contract test compares each outcome of both routes with stored fixtures. A forbidigo rule rejects any call of the adapter and any use of its request and response types outside the adapter files. Test files from before the rule are exempt by name.

## Consequences

- New season pack behavior goes into the webhook check and apply.
- A matching fix that changes an answer changes the fixtures in the same pull request, with the reason in its body.
- The lint rule cannot see an edit inside the adapter files, so a new field there can pass it. The contract test catches a changed answer, and the review catches the rest.
- Without Held titles, only these endpoints find season packs, with the full torrent list. The webhook check does not.
