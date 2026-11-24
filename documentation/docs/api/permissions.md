---
sidebar_position: 2
title: API key permissions and instance limits
sidebar_label: API key permissions
description: Give each qui API key only the areas, levels, and instances that it needs. Lists every area and level, Browser-only routes, and 403 messages.
---

# API key permissions

Each API key holds a list of Permissions and an optional Instance limit. You set both in **Settings → API keys** when you create a key. To change them later, click the edit button on the key. An edit applies on the next request of the key, and the key itself does not change.

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
| Settings | Read client settings, tracker customizations, the dashboard, filter views, and the database shrink status | Not offered | Change these items, the theme settings, the custom themes, and the Statistics settings. Dismiss the result of a database shrink and the "Shrink failed" notice |
| Logs | Read log settings, exclusions, files, and the log stream | Not offered | Change log settings and exclusions, except the log file path. See [Browser-only routes](#browser-only-routes) |
| System | Application information | Not offered | Not offered |

A `.torrent` file and a backup hold tracker passkeys, so their downloads need `write`. A key without `Torrents: write` also gets tracker URLs and web seed links with the passkey hidden: each one shows only its scheme and host. In indexer search results, the same key gets an empty download link, the details link as its scheme and host, and a hash in place of the GUID. A cross-seed apply accepts that hash.

The download link and the page link of an RSS article show only their scheme and host too. Each other field of an RSS feed or article that holds a link or HTML, such as the description of an article or the title of a feed, shows at most the scheme and host of a link, while the text a user types into a rule, its name and its filters, shows as it is. An RSS feed URL, a feed or folder that is named by its URL, and an article ID that holds a link show the scheme and host and a short code, such as `https://tracker.example#1a2b3c4d`, so that two feeds on one tracker stay apart. A key with `RSS: write` can send this short form back to refresh, move, remove, or mark as read, to change the URL of a feed, also to a feed URL in the short form, or to add a feed or folder in a folder: qui finds the feed, folder, article, or feed URL that it stands for, and a short form that matches none, or more than one, gets a 400 error. A rule lists its feeds in the same short form as the feed list shows their URLs, so a key with `RSS: write` can make a rule from the feed list or save a rule back as it got it: qui puts the full feed URLs back, and a short form that matches no feed, or more than one, gets a 400 error.

A notification target URL holds the token of the service, so a key without `Notifications: write` gets each target URL as its scheme only, such as `discord://`.

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
- The Transfer routes under `/api/transfers`
- The Share routes under `/api/shares`, also the Share settings and the download of the `.torrent` file of a Share

The log file path is a Browser-only setting on a route that API keys can use. `PUT /api/log-settings` accepts a key with `Logs: write` for Log level, max size, and max backups. It returns `403 API key not allowed; use the web UI` and changes nothing when a key sends a `path` that differs from the current one. A key can send the current `path` back unchanged.

## 403 messages

A key that cannot use a route gets one of these `403` bodies:

| Body | Meaning | Fix |
|---|---|---|
| `API key needs Torrents: write` | The key does not hold this Permission. | Add the Permission to the key. |
| `API key cannot reach instance 2` | The Instance limit of the key does not hold this instance. | Change the Instance limit, or change the request. |
| `API key with an instance limit cannot use this route` | This is a cross-seed or Dir Scan route, or a route that spans instances. | Use a key with no Instance limit. |
| `API key not allowed; use the web UI` | This is a Browser-only route, or the request changes the log file path. | Use the web UI. No Permission allows this. |

These checks apply only to API keys. A signed-in browser has full access, and a `403` from a key check does not sign you out.

## Permissions for integrations

The create and edit dialogs in **Settings → API keys** include a **Template** picker with templates for the first six integrations below. Picking a template fills in the Permissions that the tool needs, clears the Instance limit when the tool's routes refuse one, and lets you change any box afterwards.

| Integration | Permissions | Instance limit |
|---|---|---|
| [autobrr](../features/cross-seed/autobrr.md) cross-seed and [season-pack](../features/cross-seed/season-packs.md) webhooks | `Cross-seed: run` | Not allowed |
| [dashbrr](https://github.com/autobrr/dashbrr) (0.5.0 or later) | `Instances: read` | Allowed |
| [Homepage](https://gethomepage.dev/widgets/services/qui/) qui widget | `Torrents: read` | Only with `instance` set |
| Sonarr, Radarr, Lidarr, Readarr ([Dir Scan webhook](../features/cross-seed/dir-scan.md) only) | `Dir Scan: run` | Not allowed |
| [qui-extension](https://github.com/s0up4200/qui-extension) | `Instances: read`, `Torrents: write` | Allowed |
| [qui-extension](https://github.com/s0up4200/qui-extension) with cross-seed (manual match and season-pack assembly) | `Instances: read`, `Torrents: write`, `Cross-seed: write` | Not allowed |
| [Season packs](../features/cross-seed/season-packs.md), run list | `Cross-seed: read` | Not allowed |
| [External programs](../features/external-programs.md), run a program | `External programs: run` | Allowed |

- **Download clients:** autobrr, Sonarr, Radarr, Lidarr, and Readarr use a separate Client proxy key when they connect to qBittorrent through qui. The templates above are only for webhooks (External filter and webhook actions in autobrr, and **Settings → Connect → Webhook** in the Arr apps). See [Reverse Proxy](../features/reverse-proxy.md).
- **dashbrr:** Use dashbrr 0.5.0 or later. Older versions read totals from the torrent list and show session totals instead of all-time totals.
- **Homepage:** The template keeps your Instance limit. With `instance` set, limit the key to that instance. Without `instance`, the widget reads all instances through the cross-instance torrent route, which refuses a key with an Instance limit, so leave the limit empty.
