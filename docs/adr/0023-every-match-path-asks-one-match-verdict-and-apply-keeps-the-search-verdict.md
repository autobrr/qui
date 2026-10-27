---
status: accepted
date: 2026-10-23
---

# Every Match path asks one Match verdict, and apply keeps the search verdict

Each Match path gives the Match verdict four inputs: the source (the local torrent), the candidate (the torrent that qui would add), the Match evidence, and the Match policy. One compare returns the full set of fields that differ, as a typed value. The relaxations are rules over that set. They are the web source relabel, exact size, title rescue, and the bracket group fallback. A table gives the reason text, so the cross-seed dialog and the logs keep their wording. Apply keeps the verdict that search stored for the local torrent that search compared, and does not decide again. Two Match paths differ only in their Match policy and in the Match evidence that they lack. Issue #3037.

## Considered options

- **Each Match path keeps its own wrapper and switches, the status quo.** Rejected: the paths called the compare with fewer inputs and their own settings, so one pair got different answers on different paths.
- **Keep the replay at apply.** Rejected: apply ran the search decision again through provenance flags, and it passed the arguments of the file check in swapped order.
- **Relaxations that find the next difference.** Rejected: they copied a release, cleared one field, and ran the strict compare again. Control flow read the reason text, so a reworded reason changed the logic.

## Consequences

- A new relaxation is a rule over the difference set. It does not run the strict compare again.
- No code reads a reason string to decide control flow.
- The file layout check stays a separate step after the Match verdict. Its inputs have fixed roles, the local files and the new files.
- The Match verdict always rejects a new season pack against one local episode. Only a Manual match skips this rule.
- The rollout in #3037 moves the Match paths one step at a time. Until the last step, the paths that compare two local torrents keep their own wrappers. From step 2, apply keeps the search verdict.
