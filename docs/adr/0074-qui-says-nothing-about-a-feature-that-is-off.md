---
status: accepted
date: 2026-11-22
---

# qui says nothing about a feature that is off

qui writes no log line at DEBUG or above, no run summary count, no notification line, and no dialog row about a feature that is off or not set up. DEBUG is the default log level, so each of these reaches the user. A user with RED/OPS off and no account on either site saw both sites in a Library Scan run line, read it as "qui sends my data to these trackers", and stopped the scan (#4379).

Two states are exceptions, because the user must act. A feature that is on and cannot work gets one message, for example RED/OPS on with no API key. A safety setting that is off gets a warning, for example authentication. A settings screen is also exempt, because it is where the user turns the feature on.

For RED/OPS, a site gets a RED/OPS lookup only when RED/OPS is on and the site has an API key. The Search scope holds no entry for another site, so no output that reads the lookups can name it. The Problem report still names each site with `disabled` or `no_api_key`, because a maintainer reads it to learn the setup.

## Considered options

- **Keep one lookup for each site, and filter in each output.** Rejected: the search dialog, the Library Scan summary, the completion log, and the notification all read the list, and the summary shipped without the filter.
- **A System notice for RED/OPS on with no API key.** Rejected for now: the search dialog is where the user finds that RED/OPS asks nothing, and one line there is enough.

## Consequences

- A path that runs without the user gets a test with the feature off that reads its real output. `logbudget.AssertSilent` checks the log, and the test checks the run summary and the notification.
- The Library Scan runs that v1.36.0 stored hold counts for the two retired reasons. The store drops those counts when it reads a run.
- A new output about a feature that is off reverses this decision and needs a new ADR, unless it is one of the exceptions above.
