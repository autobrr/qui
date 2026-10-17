## Description

<!--- Summarize the change and the motivation. --->

Fixes # (issue)

## Breaking change

<!--- Write "No breaking change." unless existing users must act after they update, or something they rely on stops working. Examples: an API field or a setting is removed, saved data can't go back to an older version. Otherwise say what they must do, and a maintainer adds the BREAKING CHANGE label. --->

## Behaviour change

<!--- Changes that existing users notice after they update but need no action for, for example a new sort order or label. Write "No behaviour change." if there is none. --->

## How has this been tested?

<!--- Only manual or live verification that CI cannot do. Delete this section if CI covers everything. --->

## Performance

<!--- Follow docs/agents/performance-checks.md.
      Include before/after measurements and a conclusion for changes that can affect performance.
      Describe the workload and measurement method, or link to the results.
      If no performance risk applies, explain why. Keep this section. --->

## Screenshots (for UI changes)

<!--- Delete this section if your change has no visible UI.
      Screenshots and recordings on GitHub are public. Do not show private tracker names,
      tracker URLs, torrent names, or save paths in them.
      Turn on incognito mode before you capture, or blur these columns.
      Mask passkeys, announce URLs, and API keys in every capture.
      To keep real torrent data private, link a Paste from https://bin.getqui.com. --->

## Checklist

- [ ] My PR title follows the [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/#summary) format (it becomes the squashed commit message)
- [ ] If this changes the database schema, I have added migrations for both SQLite and PostgreSQL
- [ ] I completed the [mandatory performance checks](https://github.com/autobrr/qui/blob/develop/docs/agents/performance-checks.md) and recorded the outcome in Performance

## AI disclosure

<!--- Please do not lie about the AI usage in this PR. --->

Was any AI used in this PR? Roughly how much of the code was AI-written, and what model/tool?
