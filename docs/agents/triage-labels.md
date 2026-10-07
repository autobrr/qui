# Triage labels

The skills use six triage roles. In this repo, each label name is the same as its role name. The triage rules are in `docs/agents/triage.md`.

| Role | Label | Meaning |
| --- | --- | --- |
| `needs-triage` | `needs-triage` | A maintainer must evaluate the report. |
| `needs-info` | `needs-info` | The reporter must give more information. |
| `ready-for-agent` | `ready-for-agent` | The report is a full specification that an agent can work on without help. |
| `ready-for-human` | `ready-for-human` | A human must do the implementation. |
| `wontfix` | `wontfix` | Nobody will do the work. |
| `needs-grilling` | `needs-grilling` | An idea that the maintainer parked. It waits for `/grill-with-docs` and `/to-spec`. |

`needs-grilling` is for issues that the maintainer files, not for discussions. Triage does not apply it.
