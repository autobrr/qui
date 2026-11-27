---
status: accepted
date: 2026-11-27
---

# A step of the cross-seed Service that tests replace is a required part

The cross-seed Service had ten function fields that only tests set. Production checked each one for nil and took the other branch, so a test that set a field ran a path that no user runs. 57 tests replaced the Injection this way and 12 ran it. RSS looked for candidates twice, and no RSS test ran the second lookup.

Production code now has no branch on a field that only test code sets. A step that tests must replace is a part that the constructor fills, and no code checks it for nil: the Injection, the completion search, the season pack add from the Injection, and the MediaInfo read. A Trigger test hands in a stand-in that records the request and scripts the result. Each Trigger also keeps one test that runs the real Injection against the qBittorrent stub. A part that production can lack keeps its nil check: the reflink check on a platform without reflinks, and the Sonarr lookup with no Sonarr. A torrent download in a test goes through the Torznab client against a local server. Issue #4632.

## Considered options

- **No seam: every Trigger test runs the real Injection.** Rejected: 57 tests script a result, such as one instance added and one failed, to check how a Trigger reports it. Each would need a real torrent file and stub state to check what the stand-in checks well.
- **The recording stand-in alone.** Rejected: it has the coverage of the hooks, so the path from a Trigger into the Injection stays unrun.
- **Move the Injection into its own type now.** Rejected for now: it reaches 73 of the 332 methods of the Service, and 84 test files set the fields that would move, 1,363 times. #4633 leaves one constructor first. The move continues in #4639.
- **Let the types hold the rule.** Rejected: a test in the package can build a Service with any field missing.
- **The settings read in the same change.** Rejected: it holds 335 of the 488 tests that use a hook. It is #4640.

## Consequences

- The lint ratchet reports a nil check in production code on a function field that no production code sets. Its message names the constructor.
- A `forbidigo` rule blocks a direct call of the Injection inside the cross-seed package. Manual match and Transfer reach it from their own packages.
- One table test has a row for each Trigger and fails when a Trigger has none.
- A test Service that is built by hand without a required part crashes at first use.
- The Service holds a reference to itself until #4639.
- Two hooks stay on the allowlist of the ratchet: the settings read (#4640) and the season pack link create (#4138).
- A change that adds a nil-checked field that only tests set, or that removes a row of the table test, reverses this decision and needs a new ADR.
