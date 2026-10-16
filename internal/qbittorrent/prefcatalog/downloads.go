// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package prefcatalog

import "slices"

var downloadsEntries = slices.Concat(
	[]Entry{
		{Key: "torrent_content_layout", Tab: TabDownloads, Section: "whenAdding", Control: ControlSelect, OptionSource: SourceFixed, Options: fixedOptions("Original", "Subfolder", "NoSubfolder")},
		// qBittorrent 5.0 renamed start_paused_enabled to add_stopped_enabled.
		{Key: "start_paused_enabled", Tab: TabDownloads, Section: "whenAdding", Control: ControlSwitch},
		{Key: "add_stopped_enabled", Tab: TabDownloads, Section: "whenAdding", Control: ControlSwitch},
		{Key: "auto_tmm_enabled", Tab: TabDownloads, Section: "savingManagement", Control: ControlSwitch},
		{Key: "torrent_changed_tmm_enabled", Tab: TabDownloads, Section: "savingManagement", Control: ControlSwitch},
		{Key: "save_path_changed_tmm_enabled", Tab: TabDownloads, Section: "savingManagement", Control: ControlSwitch},
		{Key: "category_changed_tmm_enabled", Tab: TabDownloads, Section: "savingManagement", Control: ControlSwitch},
		{Key: "use_subcategories", Tab: TabDownloads, Section: "savingManagement", Control: ControlSwitch},
		{Key: "save_path", Tab: TabDownloads, Section: "savingManagement", Control: ControlPath, OptionSource: SourcePathAutocomplete},
		{Key: "temp_path_enabled", Tab: TabDownloads, Section: "savingManagement", Control: ControlSwitch},
		{Key: "temp_path", Tab: TabDownloads, Section: "savingManagement", Control: ControlPath, OptionSource: SourcePathAutocomplete, DependsOn: "temp_path_enabled"},
		{Key: "scan_dirs", Tab: TabDownloads, Section: "savingManagement", Control: ControlWatchFolders},
		// Add torrent reads these two. The Downloads PR decides how the dialog shows them.
		{Key: "excluded_file_names_enabled", Tab: TabDownloads, Section: "excludedFileNames", Control: ControlSwitch, Mode: ReadOnly, Reason: "pending Downloads PR"},
		{Key: "excluded_file_names", Tab: TabDownloads, Section: "excludedFileNames", Control: ControlList, DependsOn: "excluded_file_names_enabled", Mode: ReadOnly, Reason: "pending Downloads PR"},
		{Key: "autorun_on_torrent_added_enabled", Tab: TabDownloads, Section: "runExternalProgram", Control: ControlSwitch},
		{Key: "autorun_on_torrent_added_program", Tab: TabDownloads, Section: "runExternalProgram", Control: ControlText, DependsOn: "autorun_on_torrent_added_enabled"},
		{Key: "autorun_enabled", Tab: TabDownloads, Section: "runExternalProgram", Control: ControlSwitch},
		{Key: "autorun_program", Tab: TabDownloads, Section: "runExternalProgram", Control: ControlText, DependsOn: "autorun_enabled"},
	},
	notShown(TabDownloads, "pending Downloads PR",
		"add_to_top_of_queue", "torrent_stop_condition", "merge_trackers", "auto_delete_mode",
		"preallocate_all", "incomplete_files_ext", "use_unwanted_folder", "delete_torrent_content_files",
		"use_category_paths_in_manual_mode", "export_dir", "export_dir_fin",
		"torrent_files_backup_enabled", "torrent_files_backup_dir",
		"torrent_files_finished_backup_dir_enabled", "torrent_files_finished_backup_dir", "remove_torrent_file_backup"),
	notShown(TabDownloads, "pending email notification PR",
		"mail_notification_enabled", "mail_notification_sender", "mail_notification_email", "mail_notification_smtp",
		"mail_notification_ssl_enabled", "mail_notification_encryption_type", "mail_notification_auth_enabled",
		"mail_notification_username", "mail_notification_password"),
)
