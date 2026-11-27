---
status: accepted
date: 2026-11-27
---

# A search asks one Site pick of Torznab indexers and RED/OPS sites

The search dialog, the completion search, and the Library Scan let the user choose the sites that a search asks. Before this change, only the search dialog could choose RED/OPS sites. The completion picker listed only Torznab indexers, and the completion search and the Library Scan asked every RED/OPS site with an API key whatever the user picked. A Gazelle-only completion search could not be set up (#4619).

A Site pick is now one value, `models.SitePick`, with two lists: Torznab indexer IDs and RED/OPS API hosts. Both lists empty means every site that the Trigger can ask. Otherwise the search asks exactly the listed sites, so a pick of Torznab indexers alone asks no RED/OPS site. The completion settings and the Library Scan settings store both lists, and a search request carries both. The Site pick alone decides whether a Library Scan asks Torznab (see the amendment below).

Only `Service.resolveSitePick` and `collectSearchScope` in `search_scope.go` turn a pick into the facts of the Search scope (ADR 0029). Each Trigger passes its stored pick whole. In the web app, `web/src/lib/site-pick.ts` owns the options of each picker and the change from picked values to a pick. A table there says which kinds of site each Trigger can ask. RSS asks only Torznab indexers.

## Considered options

- **Keep the Torznab list, and add a switch for RED/OPS.** Rejected: the user could not pick one RED/OPS site without the other, and every picker would need the switch.
- **A missing RED/OPS list means every RED/OPS site.** Rejected: a stored value can then mean two things, and a picker that sends only the Torznab half brings back the bug. The migration fills the RED/OPS list of each stored Torznab pick instead.

## Consequences

- Migration 130 gives each stored completion and Library Scan pick that lists Torznab indexers both RED/OPS hosts, so it keeps asking RED/OPS as before.
- A manual search request with `indexer_ids` and no `gazelle_sites` now asks no RED/OPS site. The search dialog sends both lists.
- A patch of the Library Scan settings with one list replaces the whole pick.
- The Search scope facts hold the pick only as a `resolvedSitePick`, which only `search_scope.go` builds. forbidigo and the `sitepick` check of `make lint-ratchet` reject a pick built from one list, a read of one half outside `search_scope.go` and the `models` stores, and a `resolvedSitePick` built or filled outside `search_scope.go`. ESLint rejects `api.listTorznabIndexers` and `api.getCrossSeedGazelleSites` outside the module, the Indexers pages, and the Search page.

## Amendment 2026-11-27: no Torznab switch

The Library Scan had a Torznab switch that said the same as a RED/OPS-only pick, so the two settings could disagree. The switch is gone (#4626). The resolved Site pick now decides what the switch decided: a pick of only RED/OPS sites asks no Torznab indexer, keeps an interval down to 5 seconds, and starts no season pack search. A start with no usable site fails with a reason that fits the pick. Migration 131 turns each saved pick with the switch off into a RED/OPS-only pick: it clears the Torznab list and fills both RED/OPS hosts when the RED/OPS list was empty. The settings API, the run request, and the per-torrent search no longer take the switch fields.
