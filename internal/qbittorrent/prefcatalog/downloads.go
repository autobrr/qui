// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package prefcatalog

import "slices"

var downloadsEntries = slices.Concat(
	[]Entry{
		{Key: "torrent_content_layout", Tab: TabDownloads, Section: "whenAdding", Control: ControlSelect, OptionSource: SourceFixed, Options: fixedOptions("Original", "Subfolder", "NoSubfolder")},
		{Key: "add_to_top_of_queue", Tab: TabDownloads, Section: "whenAdding", Control: ControlSwitch},
		// qBittorrent 5.0 renamed start_paused_enabled to add_stopped_enabled.
		{Key: "start_paused_enabled", Tab: TabDownloads, Section: "whenAdding", Control: ControlSwitch},
		{Key: "add_stopped_enabled", Tab: TabDownloads, Section: "whenAdding", Control: ControlSwitch},
		{Key: "torrent_stop_condition", Tab: TabDownloads, Section: "whenAdding", Control: ControlSelect, OptionSource: SourceFixed, Options: fixedOptions("None", "MetadataReceived", "FilesChecked")},
		// Never, if the torrent was added, and always. The qBittorrent WebUI has a checkbox for the
		// first two only, and saves a stored 2 back as 1.
		{Key: "auto_delete_mode", Tab: TabDownloads, Section: "whenAdding", Control: ControlSelect, OptionSource: SourceFixed, Options: fixedOptions(0, 1, 2)},
		{Key: "merge_trackers", Tab: TabDownloads, Section: "whenAddingDuplicate", Control: ControlSwitch},
		// qBittorrent 5.3 replaced export_dir and export_dir_fin with the torrent_files_*backup* keys.
		{Key: "torrent_files_backup_enabled", Tab: TabDownloads, Section: "torrentFileBackup", Control: ControlSwitch},
		{Key: "torrent_files_backup_dir", Tab: TabDownloads, Section: "torrentFileBackup", Control: ControlPath, OptionSource: SourcePathAutocomplete, DependsOn: "torrent_files_backup_enabled"},
		{Key: "torrent_files_finished_backup_dir_enabled", Tab: TabDownloads, Section: "torrentFileBackup", Control: ControlSwitch},
		{Key: "torrent_files_finished_backup_dir", Tab: TabDownloads, Section: "torrentFileBackup", Control: ControlPath, OptionSource: SourcePathAutocomplete, DependsOn: "torrent_files_finished_backup_dir_enabled"},
		{Key: "remove_torrent_file_backup", Tab: TabDownloads, Section: "torrentFileBackup", Control: ControlSwitch},
		// An empty folder turns the copy off.
		{Key: "export_dir", Tab: TabDownloads, Section: "torrentFileBackup", Control: ControlPath, OptionSource: SourcePathAutocomplete},
		{Key: "export_dir_fin", Tab: TabDownloads, Section: "torrentFileBackup", Control: ControlPath, OptionSource: SourcePathAutocomplete},
		{Key: "preallocate_all", Tab: TabDownloads, Section: "files", Control: ControlSwitch},
		{Key: "incomplete_files_ext", Tab: TabDownloads, Section: "files", Control: ControlSwitch},
		{Key: "use_unwanted_folder", Tab: TabDownloads, Section: "files", Control: ControlSwitch},
		{Key: "auto_tmm_enabled", Tab: TabDownloads, Section: "savingManagement", Control: ControlSwitch},
		{Key: "torrent_changed_tmm_enabled", Tab: TabDownloads, Section: "savingManagement", Control: ControlSwitch},
		{Key: "save_path_changed_tmm_enabled", Tab: TabDownloads, Section: "savingManagement", Control: ControlSwitch},
		{Key: "category_changed_tmm_enabled", Tab: TabDownloads, Section: "savingManagement", Control: ControlSwitch},
		{Key: "use_category_paths_in_manual_mode", Tab: TabDownloads, Section: "savingManagement", Control: ControlSwitch},
		{Key: "use_subcategories", Tab: TabDownloads, Section: "savingManagement", Control: ControlSwitch},
		{Key: "save_path", Tab: TabDownloads, Section: "savingManagement", Control: ControlPath, OptionSource: SourcePathAutocomplete},
		{Key: "temp_path_enabled", Tab: TabDownloads, Section: "savingManagement", Control: ControlSwitch},
		{Key: "temp_path", Tab: TabDownloads, Section: "savingManagement", Control: ControlPath, OptionSource: SourcePathAutocomplete, DependsOn: "temp_path_enabled"},
		{Key: "scan_dirs", Tab: TabDownloads, Section: "savingManagement", Control: ControlWatchFolders},
		{Key: "excluded_file_names_enabled", Tab: TabDownloads, Section: "excludedFileNames", Control: ControlSwitch},
		{Key: "excluded_file_names", Tab: TabDownloads, Section: "excludedFileNames", Control: ControlList, DependsOn: "excluded_file_names_enabled"},
		{Key: "mail_notification_enabled", Tab: TabDownloads, Section: "emailNotification", Control: ControlSwitch},
		{Key: "mail_notification_sender", Tab: TabDownloads, Section: "emailNotification", Control: ControlText, DependsOn: "mail_notification_enabled"},
		{Key: "mail_notification_email", Tab: TabDownloads, Section: "emailNotification", Control: ControlText, DependsOn: "mail_notification_enabled"},
		{Key: "mail_notification_smtp", Tab: TabDownloads, Section: "emailNotification", Control: ControlText, DependsOn: "mail_notification_enabled"},
		// qBittorrent 5.3 replaced the SSL switch with a choice of encryption.
		{Key: "mail_notification_ssl_enabled", Tab: TabDownloads, Section: "emailNotification", Control: ControlSwitch, DependsOn: "mail_notification_enabled"},
		{Key: "mail_notification_encryption_type", Tab: TabDownloads, Section: "emailNotification", Control: ControlSelect, OptionSource: SourceFixed, Options: fixedOptions("None", "STARTTLS", "SMTPS"), DependsOn: "mail_notification_enabled"},
		{Key: "mail_notification_auth_enabled", Tab: TabDownloads, Section: "emailNotification", Control: ControlSwitch, DependsOn: "mail_notification_enabled"},
		{Key: "mail_notification_username", Tab: TabDownloads, Section: "emailNotification", Control: ControlText, DependsOn: "mail_notification_auth_enabled"},
		{Key: "mail_notification_password", Tab: TabDownloads, Section: "emailNotification", Control: ControlPassword, DependsOn: "mail_notification_auth_enabled"},
		{Key: "autorun_on_torrent_added_enabled", Tab: TabDownloads, Section: "runExternalProgram", Control: ControlSwitch},
		{Key: "autorun_on_torrent_added_program", Tab: TabDownloads, Section: "runExternalProgram", Control: ControlText, DependsOn: "autorun_on_torrent_added_enabled"},
		{Key: "autorun_enabled", Tab: TabDownloads, Section: "runExternalProgram", Control: ControlSwitch},
		{Key: "autorun_program", Tab: TabDownloads, Section: "runExternalProgram", Control: ControlText, DependsOn: "autorun_enabled"},
	},
	// The default of the "delete files" checkbox in the delete dialog of qBittorrent. qui has its own dialog.
	notShown(TabDownloads, reasonWebUIOnly, "delete_torrent_content_files"),
)
