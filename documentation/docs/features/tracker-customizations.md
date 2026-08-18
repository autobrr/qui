---
sidebar_position: 8
title: Tracker Customizations
description: Give trackers friendly display names and merge multiple announce domains into one entry.
---

# Tracker Customizations

qui identifies trackers by their announce domain (`tracker.example.com`). A tracker customization maps one or more domains to a friendly display name (`MyTracker`), which qui then uses in place of the raw domain.

Customizations are global — they apply to every instance, not per instance.

## Where to Find It

Tracker customizations live on the **Dashboard**, in the **Tracker Breakdown** section. There is no Settings page for them.

Expand **Tracker Breakdown** to see the table of trackers; the rename, merge, edit, and delete actions are the row actions in that table.

## Rename a Tracker

1. Open the **Dashboard** and expand **Tracker Breakdown**.
2. Hover the tracker row (on mobile, tap the row to open its drawer).
3. Click the pencil icon (**Rename**).
4. Enter a **Display Name** and save.

## Merge Trackers

Trackers that announce on several domains can be combined into a single entry:

1. Tick the checkbox on each tracker row you want to combine.
2. Click the link icon on one of the selected rows (**Merge selected trackers into this group**).
3. Enter the **Display Name** for the merged entry and save.

In the merge dialog, the first domain is the **Primary** one and always counts toward Dashboard statistics. Every other domain in the group starts excluded — tick its checkbox to add its stats to the group total.

This is deliberate: when the same torrents are reachable through more than one domain of the same tracker, including all of them would count those torrents twice. Only tick a secondary domain when it carries torrents the primary domain does not.

To add another domain to an existing group later, select the domain and click the link icon on the group's row.

## Edit or Delete

Rows that already have a customization show a pencil (**Edit**) and a trash icon (**Delete**) on hover. Deleting a customization reverts those trackers to their raw domains.

## Import and Export

The **Tracker Breakdown** header has import and export buttons.

**Export** copies all customizations to the clipboard as JSON. **Import** accepts the same JSON and reports new entries, conflicts, and unchanged entries before applying; for each conflict you choose **Skip** or **Overwrite**.

```json
{
  "comment": "qui tracker customizations for Dashboard",
  "trackerCustomizations": [
    {
      "displayName": "MyTracker",
      "domains": ["tracker.example.com", "tracker2.example.com"],
      "includedInStats": ["tracker.example.com"]
    }
  ]
}
```

`includedInStats` is optional and lists the **secondary** domains that count toward Dashboard statistics. The primary domain (`domains[0]`) is always counted and does not need to be listed. When `includedInStats` is omitted, only the primary domain counts.

## Where Display Names Are Used

- **Dashboard** statistics and tracker breakdown.
- **[Automations](./automations.md)**: the `Tracker` condition matches the display name in addition to the raw URL/domain, tag actions can tag by display name via **Use Display Name**, and the `.Tracker` template variable resolves to it (only when the rule also uses a **Tracker** condition or a **Use Tracker as Tag** + **Use Display Name** tag action; otherwise `.Tracker` falls back to the raw domain).
- **[Cross-seed link directories](./cross-seed/link-directories.md)**: the `by-tracker` preset uses the display name for folder names.
