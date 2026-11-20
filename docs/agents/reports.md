# Problem report playbook

How to triage a Problem report: a message that a user sends from Report a problem in qui. qui's server sends it to brrbin at `bin.getqui.com`, where only the maintainers can read it. A local report triage session obeys this file. The triage workflow does not read reports.

A report is triaged by the rules of Discussions triage in `docs/agents/triage.md`. This file names only what differs: a report has no thread, so the reporter cannot answer a question, and the only answer that the reporter sees is the Report status in Sent reports: Received, Became issue #N, or Closed with a reason.

## Read a report

1. Run `brrbin reports list --status received --json` for the reports to triage. It needs a maintainer sign-in, as "Log links" in `docs/agents/issue-tracker.md` describes.
2. Run `brrbin reports fetch <id>`. It writes the report files to `~/.cache/brrbin/<id>/` and prints the paths. Read every file.

A report holds the facts that triage otherwise asks for: the qui version, the time qui started, the newer release that qui's update check found, the OS, the database, the Log level, the Experimental features that are on, each instance's qBittorrent and libtorrent versions, state, and torrent count, the open System notices, the page and browser, the requests that failed, and the log lines of the scope that the reporter picked. A report from "Report this search" also holds the Search breakdown, the Match policy, and the Search scope.

Note what the facts leave out. A log scope of "Warnings and errors", or a Log level above DEBUG, has no debug lines. A cross-seed report from the menu has no Search breakdown.

## Steps

1. Read the report as "Read a report" describes. Triage a report only while its Report status is Received.
2. Do steps 2 to 5 of "Steps" in `docs/agents/triage.md`. Also compare the qui version of the report with the current release.
3. Pick one outcome from "Outcomes". For a report about existing behavior, use the category test in "Bug or enhancement" in `docs/agents/triage.md`.
4. Apply the outcome. "Maintainer's OK" says which actions wait.

## Maintainer's OK

These actions need no OK:

- Link the report to an issue or a discussion. qui shows "Became issue #N".
- Close the report with a reason. qui shows the reason.
- Delete the report.

brrbin has no command for them yet. Until it has, end the session with a list for the maintainer: each report id, its action, and the issue URL or the close reason. The maintainer applies them.

These actions wait for the maintainer's OK in the session:

- A new issue. Show the maintainer the title, the labels, and the full body. Create the issue only after the OK, then link the report to it.
- Mark a report as spam, or ban its Sender.

The decisions in `docs/agents/triage.md` that belong to a human stay with the maintainer: a rejection that is not a mechanical wontfix, an enhancement, and a design question. Give the maintainer the agent brief with its **Why a human:** line, and leave the report Received. After the maintainer decides, apply the action of that decision.

## Outcomes

| Outcome | Action |
| --- | --- |
| A confirmed `bug` with no open issue | Draft an issue as "Issue from a report" describes. After the OK, create it and link the report. |
| The same problem as an open issue or discussion | Link the report to it. Link each later report of the same fault to the same issue. |
| Fixed in a release | Close with the reason. |
| Fixed on `develop`, not released | Close with the reason. A report linked to a closed issue shows only "Issue #N closed", so close the report with the reason. |
| Already implemented | Close with the reason. |
| Matches a concept in `.out-of-scope/` | Close with the reason. |
| A bug in another program | Close with the reason. |
| The code path changed in a release after the qui version of the report, and no merged pull request fixed it | Close with the reason: ask the reporter to update and send again. |
| A fact is missing, and the fact decides the outcome | Close with the reason: name the fact and how to send it. If the Sender signed in, first ask the maintainer whether they want to contact the reporter. |
| An `enhancement`, a deliberate behavior, an intent conflict, or a design question | Leave it to the maintainer, as "Maintainer's OK" describes. |
| The report holds a secret that the key removal missed | Close with the reason. Draft a `bug` issue for the miss: describe the format of the secret, never its value. Do not delete the report: a delete removes the Report status before qui can show the reason. |
| An empty or test report | Delete it. |
| Spam or abuse | Ask the maintainer to mark it as spam, or to ban a repeat Sender. |

## Close reasons

The reporter reads the reason in Sent reports in qui. It is the only answer they get. Write one or two short sentences for that person: what happened, and what they can do now. Use "you", plain words, and a version as `v1.36.0`. Leave out issue numbers that the reporter cannot act on, and every detail from another report.

| Outcome | Reason |
| --- | --- |
| Fixed in a release | `Already fixed in v1.36.0. Update qui to get the fix.` |
| Fixed on `develop`, not released | `Fixed for the next qui release.` |
| Already implemented | `Already in v1.36.0: Settings, Cross-seed, Title rescue.` |
| Out of scope | `qui does not plan to add this: <the reason from .out-of-scope/, in a few words>.` |
| A bug in another program | `This is a bug in qBittorrent, not in qui.` |
| Deliberate, after the maintainer decides | `qui does this on purpose: <the reason>.` |
| The code path changed | `This part of qui changed after v1.30.0. Update to v1.36.0, and send a new report if it still happens.` |
| A missing fact | `We need <the fact> to find the cause. <How to send it.>` |
| A missed secret | `Your report holds a password or a key that qui did not remove. Change it where you made it.` |

For a missing fact, name the action in qui that sends it:

- A cross-seed search with no Search breakdown: `Run the search again, and use Report this search in the Search breakdown.`
- Debug lines that are missing: `Set the Log level to DEBUG, do it again, then send a new report with Everything recent.`
- A fault that the reporter did not describe: `Send a new report with the steps that cause it.`

## Issue from a report

- The body is the agent brief in "Agent brief" in `docs/agents/triage.md`, with its brief rules.
- In **Logs:**, write `Problem report <id>: read it with brrbin reports fetch <id>`. Add each Paste link that the report text names, as the reporter wrote it.
- Pick `ready-for-agent` or `ready-for-human` with the design check in `docs/agents/triage.md`. Add `anime` and `rls` by the rules in that file, with the rls-review mention on the issue. Add the area labels that fit. The state labels are the role names in `docs/agents/triage-labels.md`. The other labels are in "Labels" in `docs/agents/triage.md`.

## Privacy

A report is private. An issue, a discussion, a pull request, and a commit are public.

- Summarize the report text and the log lines. Never quote them in public text.
- Build each release name per the naming rule in `AGENTS.md` before it goes in public text: keep each token that matters, change the title and the group, and check that the built name still causes the bug. Discussions triage describes only the shape, because the workflow cannot run that check. A local session can.
- Write "the reporter" in public text. The GitHub or Discord name of a Sender who signed in stays private.
- Leave hostnames, paths, and instance names from the report out of public text, and never attach the report image.
- qui removes keys from the text and the log lines before Send, but the removal can miss a secret, and qui never scans the image. Treat every report as private.
