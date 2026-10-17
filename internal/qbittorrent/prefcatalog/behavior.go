// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package prefcatalog

import "slices"

var behaviorEntries = slices.Concat(
	[]Entry{
		{Key: "file_log_enabled", Tab: TabBehavior, Section: "logFile", Control: ControlSwitch},
		{Key: "file_log_path", Tab: TabBehavior, Section: "logFile", Control: ControlPath, OptionSource: SourcePathAutocomplete, DependsOn: "file_log_enabled"},
		{Key: "file_log_backup_enabled", Tab: TabBehavior, Section: "logFile", Control: ControlSwitch, DependsOn: "file_log_enabled"},
		{Key: "file_log_max_size", Tab: TabBehavior, Section: "logFile", Control: ControlNumber, WireUnit: "KiB", DisplayUnit: "KiB", DependsOn: "file_log_backup_enabled"},
		{Key: "file_log_delete_old", Tab: TabBehavior, Section: "logFile", Control: ControlSwitch, DependsOn: "file_log_enabled"},
		{Key: "file_log_age", Tab: TabBehavior, Section: "logFile", Control: ControlNumber, DependsOn: "file_log_delete_old"},
		// 0 is days, 1 is months, and 2 is years.
		{Key: "file_log_age_type", Tab: TabBehavior, Section: "logFile", Control: ControlSelect, OptionSource: SourceFixed, Options: fixedOptions(0, 1, 2), DependsOn: "file_log_delete_old"},
	},
	// The search keys keep the search tabs of the qBittorrent WebUI and desktop app, which qui does not use.
	notShown(TabBehavior, reasonWebUIOnly,
		"locale", "confirm_torrent_deletion", "status_bar_external_ip", "performance_warning",
		"store_search_jobs", "store_search_job_results"),
)
