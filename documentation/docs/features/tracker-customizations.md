---
sidebar_position: 8
title: Tracker Customizations
description: Give trackers friendly display names and merge multiple announce domains into one entry.
---

# Tracker Customizations

qui identifies trackers by their announce domain (`tracker.example.com`). A tracker customization maps one or more domains to a friendly display name (`MyTracker`), which qui then uses in place of the raw domain.

Display names apply across all your instances — you only set them up once.

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

The merge dialog marks the first domain **Primary**. Its torrents always count toward the group's Dashboard statistics. The other domains start unticked and do not count until you tick them.

That default avoids double-counting. Trackers often announce the same torrents on several domains, so counting every domain would count those torrents twice and inflate your upload and ratio figures. Tick a domain only if it holds torrents the primary one doesn't.

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
      "includedInStats": ["tracker2.example.com"]
    }
  ]
}
```

The first entry in `domains` is the primary one and always counts toward Dashboard statistics. `includedInStats` is optional and lists any of the other domains you also want counted; leave it out and only the primary domain counts.

## Where Display Names Are Used

- **Dashboard** statistics and tracker breakdown.
- **[Automations](./automations.md)**: the **Tracker** condition matches your display name as well as the raw URL or domain, tag actions can tag torrents with it, and move paths can use it via `{{.Tracker}}`.
- **[Cross-seed link directories](./cross-seed/link-directories.md)**: the `by-tracker` preset uses the display name for folder names.
