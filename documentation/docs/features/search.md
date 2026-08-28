---
sidebar_position: 16
title: Search
description: Search your Torznab indexers and send results to a qBittorrent instance.
---

# Search

The Search page queries your Torznab indexers and displays the combined results in one table. From there you can send any result to a qBittorrent instance, download the `.torrent` file, or open the release page on the indexer.

qui supports indexers from Prowlarr, Jackett, and native tracker endpoints.

## Add indexers

Manage indexers under **Settings > Indexers**.

Click **Discover** to import indexers from Prowlarr or Jackett in one step. Enter the server URL and API key, click **Discover**, and select the indexers to import. qui saves the connection, so future imports from the same server do not ask for the API key again. Click **Add single** to add one indexer by hand.

Each indexer row has actions to test the connection and to sync its capabilities (the categories and search modes the indexer reports).

## Run a search

1. Open **Search** and pick a search type: **Movies** (the default), **TV**, **Music**, **Books & comics**, **Apps & games**, or **Adult**. The search type sets the Torznab categories that qui sends to the indexers.
2. Enter a query. qui recognizes IMDb IDs (`tt1234567`) and TVDb IDs (`tvdb 123456`) inside the query and sends them as ID parameters.
3. Click **Search**.

By default, qui selects all enabled indexers. Click the **Indexers** button to open the selection sheet and narrow the search to specific indexers.

When you focus the query field, qui suggests recent searches. Click a suggestion to restore the query, the search type, and the indexer selection, and run it again.

### Advanced parameters

Click **Advanced** to send extra Torznab parameters. A search with advanced parameters does not need a query.

| Parameter | Purpose |
|-----------|---------|
| IMDb ID | Search by IMDb ID, for example `tt1234567` |
| TVDb ID | Search by TVDb ID |
| Year | Release year |
| Season | Season number |
| Episode | Episode number |
| Artist | Music artist |
| Album | Music album |
| Limit | Maximum results in the response. qui also sends it to each indexer |
| Offset | Skip this many results |

## Results

Results appear in a table with the columns Title, Indexer, Size, Seeders, Category, Source, Collection, Group, Freeleech, and Published. By default, the table sorts by seeders. Click a column header to change the sort.

The filter box narrows the visible results by title, indexer, category, source, collection, or group. Column filters provide per-column matching and include a freeleech filter for free and partial-discount results.

## Send a result to an instance

Pick a target instance from the instance selector at the top of the page. qui remembers the choice for the current browser tab.

Select a result row, then click **Add to (instance name)**. The add-torrent dialog opens with the download URL prefilled. You can set the category, tags, and save path before you add the torrent. The dropdown next to the button adds the result to a different instance.

Each row also has two direct actions:

- **Download .torrent** opens the download URL in a new tab.
- **View details** opens the release page on the indexer in a new tab.

## Search cache

qui caches search results to reduce indexer load. A badge above the results shows where they came from: **Cache hit**, **Live fetch**, or **Cache + live** for a mix.

The **Refresh from indexers** button repeats the search and bypasses the cache. A confirmation dialog warns you first, and the button locks for 30 seconds after each refresh.

:::note
A cache bypass sends the request directly to every selected indexer. Use it sparingly to avoid rate limits.
:::

**Settings > Search Cache** shows cache statistics and holds the cache configuration.

| Item | Description |
|------|-------------|
| Entries | Number of cached searches |
| Hit Count | Total cache hits |
| Approx. Size | Approximate size of the cached data |
| TTL | Current cache lifetime |
| Newest Entry | Time of the most recent cached search |
| Last Used | Time of the most recent cache hit |

Set **Cache TTL (minutes)** to control how long results stay cached. The minimum is 1440 minutes (24 hours). Higher values reduce indexer load.

## Search history and activity

**Settings > Indexers** shows two live panels above the indexer list.

**Search History** lists the last 50 searches across all sources, such as cross-seed and RSS searches. Each entry shows the indexer, the query, the result count, the duration, and a status: success, failed, skipped, or rate limited. Click an entry to view the full details, such as the exact Torznab parameters and any error message.

**Scheduler Activity** shows the search scheduler in real time: active searches, queued searches, worker usage, and indexers in a rate-limit cooldown with the time until they are ready again.

## Sonarr and Radarr integrations

**Settings > Integrations** holds your Sonarr and Radarr instances. qui queries them to resolve external IDs, which improves cross-seed searches on indexers that support ID lookups. Enter the base URL and the API key (found in **Settings > General** in Sonarr or Radarr). If you run several instances, set a priority. qui queries higher priority instances first.

Cross-seed uses the same indexers as the Search page. Cross-seed and directory scan searches use the Sonarr and Radarr integrations. The Search page does not. See [Cross-Seed](./cross-seed/overview.md) for details.
