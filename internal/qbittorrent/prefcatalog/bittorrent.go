// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package prefcatalog

import "slices"

var bittorrentEntries = slices.Concat(
	[]Entry{
		{Key: "dht", Tab: TabBitTorrent, Section: "privacy", Control: ControlSwitch},
		{Key: "pex", Tab: TabBitTorrent, Section: "privacy", Control: ControlSwitch},
		{Key: "lsd", Tab: TabBitTorrent, Section: "privacy", Control: ControlSwitch},
		{Key: "encryption", Tab: TabBitTorrent, Section: "privacy", Control: ControlSelect, OptionSource: SourceFixed, Options: fixedOptions(0, 1, 2)},
		{Key: "anonymous_mode", Tab: TabBitTorrent, Section: "privacy", Control: ControlSwitch},
		{Key: "max_active_checking_torrents", Tab: TabBitTorrent, Section: "queueing", Control: ControlNumber},
		{Key: "queueing_enabled", Tab: TabBitTorrent, Section: "queueing", Control: ControlSwitch},
		{Key: "max_active_downloads", Tab: TabBitTorrent, Section: "queueing", Control: ControlNumber, DependsOn: "queueing_enabled"},
		{Key: "max_active_uploads", Tab: TabBitTorrent, Section: "queueing", Control: ControlNumber, DependsOn: "queueing_enabled"},
		{Key: "max_active_torrents", Tab: TabBitTorrent, Section: "queueing", Control: ControlNumber, DependsOn: "queueing_enabled"},
		{Key: "dont_count_slow_torrents", Tab: TabBitTorrent, Section: "queueing", Control: ControlSwitch, DependsOn: "queueing_enabled"},
		{Key: "slow_torrent_dl_rate_threshold", Tab: TabBitTorrent, Section: "queueing", Control: ControlNumber, WireUnit: "KiB/s", DisplayUnit: "KiB/s", DependsOn: "dont_count_slow_torrents"},
		{Key: "slow_torrent_ul_rate_threshold", Tab: TabBitTorrent, Section: "queueing", Control: ControlNumber, WireUnit: "KiB/s", DisplayUnit: "KiB/s", DependsOn: "dont_count_slow_torrents"},
		{Key: "slow_torrent_inactive_timer", Tab: TabBitTorrent, Section: "queueing", Control: ControlNumber, WireUnit: "s", DisplayUnit: "s", DependsOn: "dont_count_slow_torrents"},
		{Key: "max_ratio_enabled", Tab: TabBitTorrent, Section: "seedingLimits", Control: ControlSwitch},
		{Key: "max_ratio", Tab: TabBitTorrent, Section: "seedingLimits", Control: ControlNumber, DependsOn: "max_ratio_enabled"},
		{Key: "max_seeding_time_enabled", Tab: TabBitTorrent, Section: "seedingLimits", Control: ControlSwitch},
		{Key: "max_seeding_time", Tab: TabBitTorrent, Section: "seedingLimits", Control: ControlNumber, WireUnit: "min", DisplayUnit: "min", DependsOn: "max_seeding_time_enabled"},
		// Add torrent reads these two. The BitTorrent PR decides how the dialog shows them.
		{Key: "add_trackers_enabled", Tab: TabBitTorrent, Section: "autoAddTrackers", Control: ControlSwitch, Mode: ReadOnly, Reason: "pending BitTorrent PR"},
		{Key: "add_trackers", Tab: TabBitTorrent, Section: "autoAddTrackers", Control: ControlList, DependsOn: "add_trackers_enabled", Mode: ReadOnly, Reason: "pending BitTorrent PR"},
	},
	notShown(TabBitTorrent, "pending BitTorrent PR",
		"max_inactive_seeding_time_enabled", "max_inactive_seeding_time", "max_ratio_act", "share_limits_mode",
		"add_trackers_from_url_enabled", "add_trackers_url", "add_trackers_url_list"),
)
