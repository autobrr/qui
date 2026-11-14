---
sidebar_position: 10
title: Tracker display names
sidebar_label: Tracker Customizations
description: Give trackers friendly display names and merge several announce domains into one tracker entry.
---

# Tracker customizations

qui identifies trackers by their announce domain (`tracker.example.com`). A tracker customization maps one or more domains to a friendly display name (`MyTracker`). qui then shows the display name in place of the raw domain.

Display names apply across all your instances. You configure them once.

## Where to find it

Open **Settings** and click **Tracker Customizations**. The list has one row for each tracker customization, and one row for each tracker domain of your instances that has no customization. The rename, merge, edit, and delete actions are the row actions in that list.

## Rename a tracker

1. Open **Settings** and click **Tracker Customizations**.
2. On a computer, hover the tracker row. On a phone, the actions show on each row.
3. Click the pencil icon (**Rename**).
4. Enter a **Display Name** and save.

## Merge trackers

If a tracker announces on several domains, you can combine the domains into a single entry:

1. Tick the checkbox on each tracker row that you want to combine.
2. Click the link icon on one of the selected rows (**Add to merge**).
3. Enter the **Display Name** for the merged entry and save.

The merge dialog marks the first domain **Primary**. qui shows the icon of that domain for the entry.

If you want to add another domain to an existing entry later, select the domain and click the link icon on the row of the entry.

[Statistics](./statistics.md) counts a torrent toward one tracker, also when the torrent announces on several domains. A merge thus does not count a torrent two times.

## Edit or delete

Rows that already have a customization show a pencil (**Edit**) and a trash icon (**Delete**). If you delete a customization, those trackers revert to their raw domains.

## Import and export

The header of the **Tracker Customizations** section has import and export buttons.

**Export** copies all customizations to the clipboard as JSON. **Import** accepts the same JSON and reports new entries, conflicts, and unchanged entries before it applies them. For each conflict, you choose **Skip** or **Overwrite**.

```json
{
  "comment": "qui tracker customizations",
  "trackerCustomizations": [
    {
      "displayName": "MyTracker",
      "domains": ["tracker.example.com", "tracker2.example.com"]
    }
  ]
}
```

An export from an older version of qui can have an `includedInStats` field. The import accepts it, and qui ignores the field.

## Where display names are used

- **[Statistics](./statistics.md)**: the tracker rows of the table and the tracker series of the chart.
- **[Automations](./automations.md)**: the **Tracker** condition matches your display name as well as the raw URL or domain, tag actions can tag torrents with it, and Move and Export to Instance save paths can use it with `{{.Tracker}}`.
- **[Cross-seed link directories](./cross-seed/link-directories.md)**: the `by-tracker` preset uses the display name for folder names.
