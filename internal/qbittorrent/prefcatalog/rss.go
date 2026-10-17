// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package prefcatalog

var rssEntries = []Entry{
	{Key: "rss_processing_enabled", Tab: TabRSS, Section: "reader", Control: ControlSwitch},
	{Key: "rss_refresh_interval", Tab: TabRSS, Section: "reader", Control: ControlNumber, WireUnit: "min", DisplayUnit: "min", DependsOn: "rss_processing_enabled"},
	{Key: "rss_fetch_delay", Tab: TabRSS, Section: "reader", Control: ControlNumber, WireUnit: "s", DisplayUnit: "s", DependsOn: "rss_processing_enabled"},
	{Key: "rss_max_articles_per_feed", Tab: TabRSS, Section: "reader", Control: ControlNumber, DependsOn: "rss_processing_enabled"},
	{Key: "rss_auto_downloading_enabled", Tab: TabRSS, Section: "autoDownloader", Control: ControlSwitch},
	{Key: "rss_download_repack_proper_episodes", Tab: TabRSS, Section: "smartEpisodeFilter", Control: ControlSwitch},
	{Key: "rss_smart_episode_filters", Tab: TabRSS, Section: "smartEpisodeFilter", Control: ControlList},
}
