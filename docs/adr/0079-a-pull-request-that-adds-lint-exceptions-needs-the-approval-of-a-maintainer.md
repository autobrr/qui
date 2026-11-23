---
status: accepted
date: 2026-11-23
---

# A pull request that adds lint exceptions needs the approval of a maintainer

An exception is a comment that hides the report of a check on its line: `//nolint`, `#nosec`, `//gosec:disable`, and `//exhaustive:ignore` in Go, and `eslint-disable`, `@ts-expect-error`, `@ts-ignore`, and `@ts-nocheck` in the frontend. Two rules now apply to each exception:

1. It gives its reason on the line. golangci-lint already demanded that. In the frontend, the new ESLint rule `qui/disable-reason` demands `eslint-disable-next-line <rule> -- <reason>`.
2. A pull request that adds more exceptions than it removes fails the check `New lint exceptions`, which lists the new ones. The check passes when a maintainer sets the label `exceptions-approved` on the pull request.

Most changes to qui come from coding agents. An agent that gets a lint report can fix the code or write an exception, and the exception is less work. Until now nobody but the author decided that an exception was right. On 2026-11-23 the Go code held 139 exceptions, each with a reason. The frontend held 15 `eslint-disable` comments in files that people edit, and 13 of them gave no reason. The 150 changes that merged from 2026-11-14 to 2026-11-23 added 37 exceptions and removed 77. Ten of those changes added more than they removed, and 14 of their 29 new exceptions turn off a report of gosec, the security linter.

- **The count.** `.github/scripts/count_exceptions.py` reads the diff between the base branch and the pull request. It reads each changed file whole with a small lexer, which knows comments, strings, template strings, and regular expressions, and counts a directive only at the start of a comment. When the lexer ends a file inside a string or a comment, it misread that file, and the script counts each directive text on the changed lines of that file, wherever it stands. A text that is not an exception then counts too, which only asks for the label. An exception that moves with its code is on a removed line and on an added line, so it adds nothing. On a removed line, a directive in a block comment counts only on the line where the comment opens: ESLint ignores a directive after other text of the comment, and golangci-lint ignores `nolint` on a later line, so the removal of such a text must not make room for a new exception. A directive on a later line of a block comment therefore asks for the label when it moves.
- **The check cannot be changed by the pull request.** `.github/workflows/exceptions.yml` runs on `pull_request_target`, checks out the base branch, and fetches the commits of the pull request as objects only. No code from the pull request runs. The workflow has read access to the contents and no other permission.
- **The label.** GitHub lets only a person with triage rights set a label, so the author of a pull request from a fork cannot set it. The workflow runs again when a label is set or removed.
- **The reason rule is a local ESLint rule.** Its message tells the author to fix the report first, and to write a reason only when the code is right on purpose.
- **The old comments are held.** The 13 `eslint-disable` comments without a reason are entries of `qui/disable-reason` in `web/eslint-suppressions.json`, like the reports of the other suppressed rules.

## Consequences

- About 1 pull request in 15 needs the label before its checks are green. A pull request of the lint ratchet bot that adds a `nolint` comment is one of them.
- A pull request that removes one exception and adds another one passes without the label.
- An exclusion in `.golangci.yml` or a rule that `web/eslint.config.js` turns off is not counted. A reviewer sees such a change in the diff of a config file.
- A coding agent that works with the login of a maintainer has the rights to set the label. Each maintainer keeps their agent from doing so, with a hook or a rule on their own machine.
- A change to the script or the workflow has no effect on the pull request that makes it.
- `qui/disable-reason` cannot report `/* eslint-disable */` without a rule name, because that comment turns the rule off too. The count includes that comment.

## Considered options

- **An expiry date on each exception.** Rejected. A reason such as "BitTorrent v1 pieces are SHA-1" stays true, and a date that passes fails the pull request of the next author, who did not write the exception.
- **Only the reason rule.** Rejected. The author of the code would still be the only one who decides that the exception is right.
- **The count as a rule of `go run ./scripts/ratchet`.** Rejected. That command runs the code of the pull request, so a pull request could change the count, and the lint workflow would have to run again for each label.
- **An approving review in place of the label.** Rejected. GitHub does not let a maintainer approve their own pull request.
- **The rule `require-description` of `@eslint-community/eslint-plugin-eslint-comments`.** Rejected. Its message says only that a description is missing, and it cannot be changed. The local rule has about 20 lines and no dependency.
