---
status: accepted
date: 2026-11-23
---

# No code file crosses 5,000 tokens, and the files that are already larger are held

`.file-size.json` at the root of the repo sets a size limit for a code file: 20,000 bytes, which is about 5,000 tokens at 4 bytes for each token. `go run ./scripts/ratchet` fails a file that crosses the limit, and a new file that starts over it. The files that are over the limit on the base branch are held: such a file can still grow, and a rename of it passes.

An agent reads a whole file into its context before it changes the file. A large file leaves less room for the task. The name of a file is also how an agent finds code, and the name of a large file says little about what it holds. On 2026-11-23, 267 of the 2,304 code files were over the limit, and they held 51.5% of the code. The largest had 228,632 bytes, about 57,000 tokens.

- **Files.** Go, TypeScript, and scripts (`.js`, `.mjs`, `.py`, `.sh`) count as code, and test files count. `extensions` in the settings file holds the list. Docs, locale files, SQL, and workflow files do not count.
- **Generated files.** `skip` in the settings file lists them, because nobody edits them by hand.
- **Size.** The command counts bytes, and a CRLF line end as one byte, so a checkout on Windows gets the same result.
- **The move.** The message of the check tells the author to move one whole responsibility to a file in the same folder, in a commit of its own, and to ask a maintainer when the move needs a new folder or a new exported name.
- **A looser limit.** The command fails a change that raises `max_bytes`, adds a `skip` entry, or removes an extension. Only a maintainer makes such a change, by hand, and merges it past the failed check.

The limit is in a settings file with three plain keys and not in the Go code, so that a tool outside the repo, such as a hook of a coding agent, can apply the same limit when a file is edited.

## Consequences

- No new file adds to the code in large files, and a file under the limit stays there. The 267 held files do not get smaller by this rule.
- An author whose change brings a file over the limit moves code first. On the 150 changes that merged from 2026-11-14 to 2026-11-23, the rule fails 24.
- A move can take held lint reports to another file. ADR 0077 lets that pass.
- A maintainer who adds a skip entry merges a pull request with a failed check.
- The byte count is an estimate. Code with long names or much non-ASCII text has a different number of tokens for each byte.

## Considered options

- **Also fail a held file that grows.** Rejected. On the same 150 changes this fails 82. A change that adds a few lines to a held file would then have to split it first.
- **A limit on lines, with a rule in ESLint and one in golangci-lint.** Rejected. That needs two settings that can drift apart, it does not cover the scripts, and a line count says less about tokens than a byte count.
- **A list of the held files with their sizes, which only shrinks.** Rejected. Each change to a large file would have to edit the list. The base branch already says which files were over the limit.
- **The limit as constants in the ratchet command.** Rejected, for the reason above: a tool outside the repo could not read them.
