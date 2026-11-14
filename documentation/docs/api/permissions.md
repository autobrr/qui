---
sidebar_position: 2
title: API key permissions and instance limits
sidebar_label: API key permissions
description: Give each qui API key only the areas, levels, and instances that it needs. Lists every area and level, Browser-only routes, and 403 messages.
---

# API key permissions

Each API key holds a list of Permissions and an optional Instance limit. You set both in **Settings → API Keys** when you create a key. To change them later, click the edit button on the key. An edit applies on the next request of the key, and the key itself does not change.

A key must hold at least one Permission. `POST /api/api-keys` returns `400` to a request with no Permission list.

Keys that existed before the upgrade to this feature hold every Permission and have no Instance limit. Edit each of these keys to remove the access that the key does not need.

When authentication is disabled, qui ignores API keys. Permissions and Instance limits then do not restrict a request.

## Levels

A Permission is one area and one level, for example `Torrents: read`. There are three levels:

- `read` shows data. It does not start work.
- `run` starts work that is already set up. It does not include `read`, because settings hold credentials.
- `write` changes data. It includes `read` and `run`.

The level is a property of the route. It does not come from the HTTP method. For example, `POST /api/instances/{instanceID}/torrents/check-duplicates` needs `Torrents: read`.

## Areas

Each area offers only the levels in this table.

| Area | `read` | `run` | `write` |
|---|---|---|---|
| Instances | List instances, preferences, capabilities, transfer information | Not offered | Add, edit, and delete instances, change preferences, test a connection |
| Torrents | List torrents, read their details, download a content file, read the transfer history | Not offered | Add, change, and delete torrents, export a `.torrent` file, download a `.torrent` file from the torrent creator |
| Cross-seed | Read settings, status, routes, the blocklist, and run history, search for matches, analyze a torrent, read match proposals | The autobrr apply, the webhook check, the season-pack check and apply, start and stop a run | Change settings, routes, and the blocklist, apply search results, apply a manual match, assemble a pack by hand |
| Dir Scan | Read settings, directories, status, and runs | The webhook scan, start and stop a scan | Change settings and directories |
| Automations | Read rules and activity, preview a rule | Apply now, apply one rule, dry run | Create, edit, reorder, and delete rules, delete activity |
| RSS | Read feeds, rules, and events | Refresh a feed, reprocess rules | Change feeds, folders, and rules |
| Backups | Read settings, runs, and manifests, preview a restore | Start a backup | Change settings, import a manifest, restore, delete runs, download a run, download a torrent from a run |
| Orphan scan | Read settings and runs | Start and cancel a scan | Change settings, confirm a deletion |
| External programs | List programs | Run a program | Not offered. See [Browser-only routes](#browser-only-routes) |
| Indexers | Read indexers and their health, search | Not offered | Add, edit, delete, and test indexers, change the search cache settings |
| Arr | Read Arr instances, resolve an external ID | Not offered | Add, edit, delete, and test Arr instances |
| Notifications | Read targets and events | Not offered | Add, edit, delete, and test targets |
| Settings | Read client settings, tracker customizations, the dashboard, filter views, and the database shrink status | Not offered | Change these items, the theme settings, and the Statistics settings. Dismiss the result of a database shrink and the "Shrink failed" notice |
| Logs | Read log settings, exclusions, files, and the log stream | Not offered | Change log settings and exclusions |
| System | Application information | Not offered | Not offered |

A `.torrent` file and a backup hold tracker passkeys, so their downloads need `write`. A key without `Torrents: write` also gets tracker URLs with the passkey hidden.

Each key can reach these routes with any Permission: the current user, tracker icons, peer-client icons, the theme list, the custom theme list, the version, the latest version, and the event-only stream. A torrent stream needs `Torrents: read`.

## Instance limit

An Instance limit is a list of instances that the key can reach. A key with no Instance limit reaches every instance, also the ones that you add later.

A key with an Instance limit follows these rules:

- It reaches a route under an instance only when that instance is in the limit.
- It sees only the instances in the limit in `GET /api/instances`.
- It gets a `403` from cross-seed and Dir Scan routes, and from routes that span instances. These are the cross-instance torrent list, the torrent archive export, the delete-cross-seeds check, the instance order, the list of instances with transfer history, the Statistics settings, and the database shrink status. These routes do not narrow their results to the limit.
- It gets a `403` when a stream asks for another instance. The check also applies when an open stream adds an instance.
- It gets a `403` when an automation exports to another instance.

The create and edit dialogs grey out the Cross-seed and Dir Scan rows when you pick an instance. qui does not save a Permission on a greyed-out row.

A new instance does not join an existing limit. A deleted instance stays in the stored list and matches nothing. If a limit holds only deleted instances, the key reaches no instance, and Settings shows "No instances" for it.

## Browser-only routes

A Browser-only route refuses every API key, also a key that holds every Permission. It accepts only a signed-in browser.

When authentication is disabled, qui ignores API keys, so this refusal does not apply. API key management, change password, and logout still return `403 Endpoint disabled when authentication is disabled`. The other Browser-only routes accept the request.

The Browser-only routes are:

- API key management
- Client proxy API key management
- System update and restart, and the request to shrink the database file
- The SSH credential and host-key routes of an instance
- Change password and logout
- The license routes
- External program create, update, and delete

## 403 messages

A key that cannot use a route gets one of these `403` bodies:

| Body | Meaning | Fix |
|---|---|---|
| `API key needs Torrents: write` | The key does not hold this Permission. | Add the Permission to the key. |
| `API key cannot reach instance 2` | The Instance limit of the key does not hold this instance. | Change the Instance limit, or change the request. |
| `API key with an instance limit cannot use this route` | This is a cross-seed or Dir Scan route, or a route that spans instances. | Use a key with no Instance limit. |
| `API key not allowed; use the web UI` | This is a Browser-only route. | Use the web UI. No Permission opens this route. |

These checks apply only to API keys. A signed-in browser has full access, and a `403` from a key check does not sign you out.

## Permissions for integrations

| Integration | Permission |
|---|---|
| [autobrr cross-seed](../features/cross-seed/autobrr.md) | `Cross-seed: run` |
| [Season packs](../features/cross-seed/season-packs.md), check and apply | `Cross-seed: run` |
| [Season packs](../features/cross-seed/season-packs.md), run list | `Cross-seed: read` |
| [Dir Scan webhook](../features/cross-seed/dir-scan.md) | `Dir Scan: run` |
| [External programs](../features/external-programs.md), run a program | `External programs: run` |
| A dashboard that shows torrents | `Torrents: read`. Add `Instances: read` if the dashboard lists the instances |

The cross-seed and Dir Scan integrations need a key with no Instance limit.
