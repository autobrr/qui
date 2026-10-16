// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package prefcatalog

import "slices"

var behaviorEntries = slices.Concat(
	notShown(TabBehavior, reasonWebUIOnly,
		"locale", "confirm_torrent_deletion", "status_bar_external_ip", "performance_warning"),
	notShown(TabBehavior, "pending Behavior PR",
		"store_search_jobs", "store_search_job_results",
		"file_log_enabled", "file_log_path", "file_log_backup_enabled", "file_log_max_size",
		"file_log_delete_old", "file_log_age", "file_log_age_type"),
)
