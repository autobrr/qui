/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TabLabels } from "./types"

// i18n-namespace: instances

export const rssLabels: TabLabels = {
  labelKey: "preferences.dialog.tabs.rss",
  sections: {
    reader: { labelKey: "preferences.sections.rssReader" },
    autoDownloader: { labelKey: "preferences.sections.rssAutoDownloader" },
    smartEpisodeFilter: { labelKey: "preferences.sections.rssSmartEpisodeFilter" },
  },
  fields: {
    rss_processing_enabled: { labelKey: "rss:warnings.enableRss" },
    rss_refresh_interval: { labelKey: "rss:settingsPopover.refreshInterval" },
    rss_fetch_delay: { labelKey: "preferences.fields.rssFetchDelay" },
    rss_max_articles_per_feed: { labelKey: "rss:settingsPopover.maxArticles" },
    rss_auto_downloading_enabled: { labelKey: "rss:settingsPopover.autoDownload" },
    rss_download_repack_proper_episodes: { labelKey: "rss:settingsPopover.downloadRepack" },
    rss_smart_episode_filters: {
      labelKey: "preferences.fields.rssSmartEpisodeFilters",
      descriptionKey: "preferences.fields.rssSmartEpisodeFiltersDescription",
    },
  },
}
