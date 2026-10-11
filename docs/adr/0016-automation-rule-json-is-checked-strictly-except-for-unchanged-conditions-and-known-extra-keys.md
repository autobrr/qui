---
status: accepted
date: 2026-10-10
---

# Automation rule JSON is checked strictly, except for unchanged conditions and known extra keys

qui rejects unknown keys, fields, operators and enum values in automation rule JSON, because before this check a typo saved a rule that never matched. Two exceptions keep existing rules and clients working. On update, the field, operator and enum checks run only when `conditions` changed, so a stored rule with a bad operator can still be switched on or off and renamed. The checks cover the score conditions in `sortingConfig` only when the sorting changed. The server-set keys `id`, `instanceId`, `createdAt` and `updatedAt`, and the legacy keys `trackerDomains`, `conditions.tag` and `TRACKERS`, are accepted, because the on/off switch and older exports send them. The cost is a short list of keys that the check lets through. Removing a legacy key is a separate change.

Issue #3106.
