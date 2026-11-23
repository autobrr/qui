---
status: accepted
date: 2026-11-23
---

# A pull request that adds lint exceptions needs the approval of a maintainer

An exception is a comment that hides the report of a check on its line: `//nolint`, `#nosec`, `//gosec:disable`, and `//exhaustive:ignore` in Go, and `eslint-disable`, `@ts-expect-error`, `@ts-ignore`, and `@ts-nocheck` in the frontend. Two rules now apply to each exception:

1. It gives its reason on the line. golangci-lint already demanded that. In the frontend, the new ESLint rule `qui/disable-reason` demands `eslint-disable-next-line <rule> -- <reason>`.
2. A pull request that adds a new exception fails the check `New lint exceptions`, which lists the new ones. An exception is new when the pull request removes no comment with the same text. The check passes when a maintainer sets the label `exceptions-approved` on the pull request.

Most changes to qui come from coding agents. An agent that gets a lint report can fix the code or write an exception, and the exception is less work. Until now nobody but the author decided that an exception was right. On 2026-11-23 the Go code held 139 exceptions, each with a reason. The frontend held 15 `eslint-disable` comments in files that people edit, and 13 of them gave no reason. Of the 150 changes that merged from 2026-11-14 to 2026-11-23, 11 added new exceptions, 32 in all, and 14 of those 32 turn off a report of gosec, the security linter.

- **What is new.** `.github/scripts/count_exceptions.py` lists the Go, TypeScript, and JavaScript files that the pull request changes, and reads each one whole, as it is on the merge base and on the head. Two helpers list the comments of each file with a real parser: `scripts/exceptions` with `go/scanner` for Go, and `.github/scripts/exceptions` with the TypeScript parser and `forEachComment` of ts-api-utils for the frontend. A string, a template, a regular expression, or JSX text is therefore never read as a comment. On both sides, each line of a comment that starts with a directive is an exception: the first line after `//` or `/*`, and a later line of a block comment after its stars and spaces. That is more than the tools honour, so a doubt asks for the label. The script first drops each comment that both sides of a file hold with the same whole text. So an exception that stays in the file is not new, and a directive that the change brings into effect is new, also when its own line did not change. Then an exception of the head is new, unless the pull request removes a comment with the same whole text, in that file or in another one. One removed comment pays for one added comment, so an exception that moves with its code is not new. The script does not need to know which form of a comment each tool reads: a removed comment pays only for a comment with the same text. When the parser reports a syntax error on either side of a file, the script misread that file: it counts each directive text on the lines of the head that the base does not have, wherever it stands, and no removed comment pays for them. The TypeScript parser and ts-api-utils are pinned in their own lockfile, `.github/scripts/exceptions/package-lock.json`, at the versions that `web/` uses. A file with newer syntax than the pinned parser knows counts as misread until the pin moves.
- **The check cannot be changed by the pull request.** `.github/workflows/exceptions.yml` runs on `pull_request_target`, checks out the base branch, and fetches the commits of the pull request as objects only. The helpers read the files of the pull request as text, and no code from the pull request runs. The workflow has read access to the contents and no other permission.
- **The label.** GitHub lets only a person with triage rights set a label, so the author of a pull request from a fork cannot set it. The workflow runs again when a label is set or removed.
- **The reason rule is a local ESLint rule.** Its message tells the author to fix the report first, and to write a reason only when the code is right on purpose.
- **The old comments are held.** The 13 `eslint-disable` comments without a reason are entries of `qui/disable-reason` in `web/eslint-suppressions.json`, like the reports of the other suppressed rules.

## Consequences

- About 1 pull request in 14 needs the label before its checks are green. A pull request of the lint ratchet bot that adds a `nolint` comment is one of them.
- A pull request that puts one exception in the place of another needs the label. So does one that changes the text of an exception, also when it only rewords the reason. None of the 150 changes did only that.
- The same text can have another effect in another place: `// @ts-nocheck` works only above the code of a file. A pull request that moves it there from below the code turns the type check of the file off, and the check does not see a new exception.
- An exclusion in `.golangci.yml` or a rule that `web/eslint.config.js` turns off is not counted. A reviewer sees such a change in the diff of a config file.
- A coding agent that works with the login of a maintainer has the rights to set the label. Each maintainer keeps their agent from doing so, with a hook or a rule on their own machine.
- A change to the script or the workflow has no effect on the pull request that makes it.
- `qui/disable-reason` cannot report `/* eslint-disable */` without a rule name, because that comment turns the rule off too. The check counts that comment.

## Considered options

- **An expiry date on each exception.** Rejected. A reason such as "BitTorrent v1 pieces are SHA-1" stays true, and a date that passes fails the pull request of the next author, who did not write the exception.
- **Only the reason rule.** Rejected. The author of the code would still be the only one who decides that the exception is right.
- **The check as a rule of `go run ./scripts/ratchet`.** Rejected. That command runs the code of the pull request, so a pull request could change the check, and the lint workflow would have to run again for each label.
- **An approving review in place of the label.** Rejected. GitHub does not let a maintainer approve their own pull request.
- **The rule `require-description` of `@eslint-community/eslint-plugin-eslint-comments`.** Rejected. Its message says only that a description is missing, and it cannot be changed. The local rule has about 20 lines and no dependency.
- **A lexer written by hand.** Rejected. The first version of the check found comments with a small lexer of its own. Five review rounds found 12 inputs that it read wrong, among them a regular expression literal, a comment inside `${...}`, JSX text, `n++ / 2`, `obj.return / 2`, and a string continued with a backslash. Each gap let an exception pass without the label, and a lexer for these languages cannot be complete without becoming a parser.
- **A count: fail when a pull request adds more exceptions than it removes.** Rejected. An earlier version did this. A removed exception then paid for an unrelated new one, so the script had to know that the removed comment was in effect. Three review findings named a form that it got wrong: `// * @ts-ignore`, a `/* @ts-ignore` comment over two lines, and a `// @ts-nocheck` below the code. On the 150 changes, the count failed 10 and the rule by text fails 11: #4242 removed 3 exceptions and added 3 other ones.
