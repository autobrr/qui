---
status: accepted
date: 2026-11-19
---

# A Problem report leaves qui through its server, and only on Send

qui sends Problem reports to brrbin from its server, never from the browser. The browser shows what gets sent, the user presses Send, and the server removes keys from every text part again before the request, also every secret that qui stores. qui contacts brrbin only on Send, on sign-in and sign-out, when the report dialog or Sent reports opens with at least one report, and on Delete: no background job, no automatic retry and no queue. The Install key and the sign-in token stay on the server, encrypted, and one module owns key removal, so a new fact added to reports passes through the same filter.

## Considered Options

- Sending from the browser. Rejected, because it puts the token in browser storage and the CORS rules of brrbin in the way, and splits key removal between two languages.
- A queue for reports that could not be sent. Rejected, because it sends data later without a fresh press of Send.

## Consequences

A report can only leave a running qui, and a draft that brrbin refused waits in the browser until the user sends it again.
