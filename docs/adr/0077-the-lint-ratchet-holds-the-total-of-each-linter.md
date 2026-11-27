---
status: accepted
date: 2026-11-23
---

# The lint ratchet holds the total of each linter, so code can move between files

`go run ./scripts/ratchet` compares `scripts/ratchet/allowlist.json` with the allowlist on the base branch. The total of each linter must not be higher than its total on the base branch. The entry of one file can grow, as long as the total of its linter does not.

Before this decision, no entry could be higher than the same entry on the base branch. Only the `comments` linter (ADR 0070) and the suppressed ESLint rules (ADR 0073) used a total. A Go function with a `gocognit` or `contextcheck` report could not move to another file: the entry of the new path grew from 0, and the check failed although no report was new. A maintainer had to merge such a pull request past the failed check. An agent cannot tell a failed check that the maintainer accepts from one that it must fix, so it left the code in the large file.

The two rules for one file stay: a file reports no more problems than its entry allows, and no entry is higher than the number that the file reports. So the allowlist still names the file of each held report.

When a total grows, the message names each entry that grew, so that the author finds the new report.

## Consequences

- A split or a rename of a Go file with held reports passes when the same change moves the counts to the new paths.
- A change can fix a report in one file and add a report of the same linter in another file, and the check passes. The suppressed ESLint rules already allow this trade. A reviewer sees it in the diff of the allowlist.
- The `go` lane of the bot (ADR 0020) only lowers entries, so it does not change.

## Considered options

- **Keep the rule for each file, and let a maintainer merge a move past the failed check.** Rejected, for the reason above: the failed check stops the move before a maintainer sees it.
- **A flag or a label that skips the rule for a move.** Rejected. The command cannot prove that a change only moves code, so the flag would skip the rule for each change.
- **Find the move in the rename data of git.** Rejected. Git reports a rename of a whole file. A function that moves into a new file is no rename.
