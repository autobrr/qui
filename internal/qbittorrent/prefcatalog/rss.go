// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package prefcatalog

import "slices"

var rssEntries = slices.Concat(
	[]Entry{
		{Key: "rss_processing_enabled", Tab: TabRSS, Section: "reader", Control: ControlSwitch},
		{Key: "rss_refresh_interval", Tab: TabRSS, Section: "reader", Control: ControlNumber, WireUnit: "min", DisplayUnit: "min", DependsOn: "rss_processing_enabled"},
		{Key: "rss_max_articles_per_feed", Tab: TabRSS, Section: "reader", Control: ControlNumber, DependsOn: "rss_processing_enabled"},
		{Key: "rss_auto_downloading_enabled", Tab: TabRSS, Section: "autoDownloader", Control: ControlSwitch},
		{Key: "rss_download_repack_proper_episodes", Tab: TabRSS, Section: "smartEpisodeFilter", Control: ControlSwitch},
	},
	notShown(TabRSS, "pending RSS PR", "rss_fetch_delay", "rss_smart_episode_filters"),
)
