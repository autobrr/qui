---
status: accepted
date: 2026-09-18
---

# Matching rules read releases and file lists only

The cross-seed matching rules are methods on a `matcher` value in `internal/services/crossseed/matching.go`. The value has three fields: `releaseCache`, `stringNormalizer`, and `metrics`. A rule reads the release names and file lists it is given. It never reads an instance, a store, the sync manager, or an indexer. `Service` keeps the same three pointers and hands them over through one accessor, `func (s *Service) matcher() matcher`. The episode map stays an input (ADR 0002): `classifySearchCandidate` takes an `EpisodeMap` and never fetches it. Issue #2748.

## Considered options

- **An interface over the matcher.** Rejected: one implementation and no consumer that swaps it.
- **A `matcher` field on `Service`, wired in `NewService`.** Rejected: the test files build `&Service{stringNormalizer: x}` hundreds of times. Each of those literals would carry a zero matcher that silently falls back to the default normalizer and an empty release cache.
- **Embed `matcher` in `Service`.** Rejected: Go forbids promoted fields in composite literals, so the same literals stop compiling.
- **Pointer receivers.** Rejected: the accessor returns a value, so `s.matcher().releasesMatch(...)` would not compile. Each call copies three pointers.

## Consequences

- A matching rule that reaches for a client, a store, or the sync manager fails to compile. A rule that needs one needs a new ADR.
- Production code no longer builds a throwaway `Service` to call a matching rule. The season pack code calls `s.matcher()`, and `buildSeasonPackPlan`, which has no `Service`, builds a `matcher` from the normalizer it is given.
- A matcher test builds a `matcher{}` with the three inputs the rules read and nothing else.
- `normalizerForService` stays for the callers outside the matcher files. It guards a nil `Service` and returns `s.matcher().normalizer()`, so the default-normalizer fallback has one body.
