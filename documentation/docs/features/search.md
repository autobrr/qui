---
sidebar_position: 16
title: Search Torznab indexers from qui
sidebar_label: Search
description: Search all your Torznab indexers in one table and send results straight to a qBittorrent instance.
---

# Search

The Search page queries your Torznab indexers and displays the combined results in one table. From there you can send any result to a qBittorrent instance, download the `.torrent` file, or open the release page on the indexer.

qui supports indexers from Prowlarr, Jackett, and native tracker endpoints.

## Add indexers

Manage indexers under **Settings > Indexers**.

To import indexers from Prowlarr or Jackett in one step, click **Add indexer**, then **Discover in Jackett or Prowlarr**. Enter the server URL and API key, click **Discover**, and select the indexers to import. qui saves the connection, so future imports from the same server do not ask for the API key again. To add one indexer by hand, click **Add indexer**, then **Add one by URL**.

The indexer timeout sets how long qui waits for a reply from that indexer (30 seconds by default). A search can run for at least as long as the longest timeout of the indexers you selected. Time spent waiting in the queue for a free slot or a rate limit does not count. If you cancel the search, it stops sooner.

Each indexer row has actions to test the connection and to sync its capabilities (the categories and search modes the indexer reports).

## Run a search

1. Open **Search** and pick a search type: **Movies** (the default), **TV**, **Music**, **Books & comics**, **Apps & games**, or **Adult**. The search type sets the Torznab categories that qui sends to the indexers. qui remembers the search type that you pick.
2. Enter a query. qui recognizes IMDb IDs (`tt1234567`) and TVDb IDs (`tvdb 123456`) inside the query and sends them as ID parameters.
3. Click **Search**.

By default, qui selects all enabled indexers. To search only some of them, click the **Indexers** button and pick the indexers in the list.

When you focus the query field, qui suggests recent searches. Before your first search, the page also lists them. Click a recent search to restore the query, the search type, and the indexer selection, and run it again.

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

Results appear in a table with the columns Title, Indexer, Size, Seeders, Leechers, and Age. On a narrow screen, the values that do not fit in a column show under the title. By default, the table sorts by seeders. To change the sort, click a column header. On a narrow screen, use the **Sort** menu.

The filter box narrows the results to the rows that contain each word that you type. It looks at the title, the indexer, the category, and the group.

The buttons next to the filter box narrow the results by **Indexer**, **Category**, **Source**, **Service**, **Group**, or **Freeleech**. Each button opens a list of the values in the results, with the number of results for each value. A button shows only when the results have two or more values for it.

When a filter is on, the count above the table shows how many results match, for example "12 of 340 results". Click the count to clear all filters.

A badge next to the title shows a download discount, for example **Free** or **50%**. Another badge shows an upload bonus, for example **2× upload**.

The table shows 50 results first. To show 50 more, click **Show more** below the table.

## Send a result to an instance

Each row has a plus button (**+**) that adds the result. If you have one qBittorrent instance, the button sends the result to that instance. If you have more than one instance, the button opens a list of your instances. The add-torrent dialog opens with the download URL prefilled. You can set the category, tags, and save path before you add the torrent.

To add more than one result at a time:

1. Click the checkbox of each result that you want, or click the row. To select a range of rows, hold Shift and click the last row of the range. The checkbox in the table header selects all the results that the table shows.
2. In the bar at the bottom of the screen, click **Add**. If you have more than one instance, pick an instance.

The add-torrent dialog opens with the download URLs of all the selected results. To clear the selection, click **Clear selection** in the bar, or press Escape.

If you have no instance, each row has a download button instead of the plus button. It opens the download URL in a new tab, as **Download .torrent** does.

Each row has these actions. On a screen of 1440 pixels or wider, each action is a button in the row. On a narrower screen, the actions are in the **More actions** menu of the row.

- **Download .torrent** opens the download URL in a new tab.
- **Open on indexer** opens the release page on the indexer in a new tab.
- **Copy title** copies the release title.

## Search cache

qui caches search results to reduce indexer load. A badge above the results shows where they came from: **Cached** with the age of the cache, **Live**, or **Cache + live** for a mix. Hover over the badge to see when qui cached the results and when the cache expires. If some indexers did not answer, an **Incomplete** badge shows too.

The **Refresh** button repeats the search and bypasses the cache. A confirmation dialog warns you first, and the button locks for 30 seconds after each refresh.

:::note
A cache bypass sends the request directly to every selected indexer. Use it sparingly to avoid rate limits.
:::

**Settings > Search cache** shows cache statistics and holds the cache configuration.

| Item | Description |
|------|-------------|
| Status | **Enabled** or **Disabled** |
| Entries | Number of cached searches |
| Hits | Total cache hits |
| Approximate size | Approximate size of the cached data |
| Newest entry | Time of the most recent cached search |
| Last used | Time of the most recent cache hit |

To control how long results stay cached, set **Cache TTL (minutes)** under **Cache settings**. The minimum is 1440 minutes (24 hours). Higher values reduce indexer load.

## Search history and activity

When you have at least one indexer, **Settings > Indexers** shows two live sections below the indexer list.

**Scheduler activity** shows the search scheduler in real time: active searches, queued searches, worker usage, and indexers in a rate-limit cooldown with the time until they are ready again.

**Search history** lists the last 50 searches across all sources, such as cross-seed and RSS searches. Each entry shows the indexer, the query, the result count, the duration, and a status: success, failed, skipped, or rate limited. Click an entry to view the full details, such as the exact Torznab parameters and any error message. The section shows the 10 most recent entries. To see the rest, click **Show all**.

## Sonarr and Radarr integrations

**Settings > Integrations** holds your Sonarr and Radarr instances. qui queries them to resolve external IDs, which improves cross-seed searches on indexers that support ID lookups. Enter the base URL and the API key (found in **Settings > General** in Sonarr or Radarr). If you run several instances, set a priority. qui queries higher priority instances first.

Cross-seed uses the same indexers as the Search page. Cross-seed and directory scan searches use the Sonarr and Radarr integrations. The Search page does not. See [Cross-Seed](./cross-seed/overview.md) for details.
