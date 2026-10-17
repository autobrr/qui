/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TabLabels } from "./types"

// i18n-namespace: instances

const startPaused = {
  labelKey: "preferences.fileManagement.startTorrentsPaused",
  descriptionKey: "preferences.fileManagement.startTorrentsPausedDescription",
}

export const downloadsLabels: TabLabels = {
  labelKey: "preferences.dialog.tabs.downloads",
  sections: {
    whenAdding: { labelKey: "preferences.sections.whenAdding" },
    whenAddingDuplicate: { labelKey: "preferences.sections.whenAddingDuplicate" },
    torrentFileBackup: { labelKey: "preferences.sections.torrentFileBackup" },
    savingManagement: { labelKey: "preferences.sections.savingManagement" },
    excludedFileNames: { labelKey: "preferences.sections.excludedFileNames" },
    emailNotification: { labelKey: "preferences.sections.emailNotification" },
    runExternalProgram: {
      labelKey: "preferences.fileManagement.runExternalProgram",
      descriptionKey: "preferences.fileManagement.runExternalProgramDescription",
    },
  },
  fields: {
    torrent_content_layout: {
      labelKey: "preferences.fileManagement.defaultContentLayout",
      descriptionKey: "preferences.fileManagement.defaultContentLayoutDescription",
      options: {
        Original: { labelKey: "preferences.fileManagement.contentLayoutOriginal" },
        Subfolder: { labelKey: "preferences.fileManagement.contentLayoutSubfolder" },
        NoSubfolder: { labelKey: "preferences.fileManagement.contentLayoutNoSubfolder" },
      },
    },
    add_to_top_of_queue: { labelKey: "preferences.fields.addToTopOfQueue" },
    // qBittorrent 5.0 renamed start_paused_enabled to add_stopped_enabled. It sends one of the two.
    start_paused_enabled: startPaused,
    add_stopped_enabled: startPaused,
    torrent_stop_condition: {
      labelKey: "preferences.fields.torrentStopCondition",
      descriptionKey: "preferences.fields.torrentStopConditionDescription",
      options: {
        None: { labelKey: "preferences.fields.torrentStopConditionNone" },
        MetadataReceived: { labelKey: "preferences.fields.torrentStopConditionMetadataReceived" },
        FilesChecked: { labelKey: "preferences.fields.torrentStopConditionFilesChecked" },
      },
    },
    auto_delete_mode: {
      labelKey: "preferences.fields.autoDeleteMode",
      descriptionKey: "preferences.fields.autoDeleteModeDescription",
      options: {
        0: { labelKey: "preferences.fields.autoDeleteModeNever" },
        1: { labelKey: "preferences.fields.autoDeleteModeIfAdded" },
        2: { labelKey: "preferences.fields.autoDeleteModeAlways" },
      },
    },
    merge_trackers: { labelKey: "preferences.fields.mergeTrackers" },
    torrent_files_backup_enabled: { labelKey: "preferences.fields.torrentFilesBackupEnabled" },
    torrent_files_backup_dir: { labelKey: "preferences.fields.torrentFilesBackupDir" },
    torrent_files_finished_backup_dir_enabled: { labelKey: "preferences.fields.torrentFilesFinishedBackupDirEnabled" },
    torrent_files_finished_backup_dir: { labelKey: "preferences.fields.torrentFilesFinishedBackupDir" },
    remove_torrent_file_backup: { labelKey: "preferences.fields.removeTorrentFileBackup" },
    export_dir: { labelKey: "preferences.fields.exportDir", descriptionKey: "preferences.fields.exportDirDescription" },
    export_dir_fin: { labelKey: "preferences.fields.exportDirFin", descriptionKey: "preferences.fields.exportDirDescription" },
    preallocate_all: { labelKey: "preferences.fields.preallocateAll" },
    incomplete_files_ext: { labelKey: "preferences.fields.incompleteFilesExt" },
    use_unwanted_folder: { labelKey: "preferences.fields.useUnwantedFolder" },
    auto_tmm_enabled: {
      labelKey: "preferences.fileManagement.autoTorrentManagement",
      descriptionKey: "preferences.fileManagement.autoTorrentManagementDescription",
    },
    torrent_changed_tmm_enabled: {
      labelKey: "preferences.fileManagement.relocateOnCategoryChange",
      descriptionKey: "preferences.fileManagement.relocateOnCategoryChangeDescription",
    },
    save_path_changed_tmm_enabled: {
      labelKey: "preferences.fileManagement.relocateOnDefaultSavePath",
      descriptionKey: "preferences.fileManagement.relocateOnDefaultSavePathDescription",
    },
    category_changed_tmm_enabled: {
      labelKey: "preferences.fileManagement.relocateOnCategorySavePath",
      descriptionKey: "preferences.fileManagement.relocateOnCategorySavePathDescription",
    },
    use_category_paths_in_manual_mode: {
      labelKey: "preferences.fields.useCategoryPathsInManualMode",
      descriptionKey: "preferences.fields.useCategoryPathsInManualModeDescription",
    },
    use_subcategories: {
      labelKey: "preferences.fileManagement.enableSubcategories",
      descriptionKey: "preferences.fileManagement.enableSubcategoriesDescription",
    },
    save_path: {
      labelKey: "preferences.fileManagement.defaultSavePath",
      descriptionKey: "preferences.fileManagement.defaultSavePathDescription",
      placeholderKey: "preferences.fileManagement.defaultSavePathPlaceholder",
    },
    temp_path_enabled: {
      labelKey: "preferences.fileManagement.useTempPath",
      descriptionKey: "preferences.fileManagement.useTempPathDescription",
    },
    temp_path: {
      labelKey: "preferences.fileManagement.tempDownloadPath",
      descriptionKey: "preferences.fileManagement.tempDownloadPathDescription",
      placeholderKey: "preferences.fileManagement.tempDownloadPathPlaceholder",
    },
    scan_dirs: {
      labelKey: "preferences.fileManagement.watchFolders.title",
      descriptionKey: "preferences.fileManagement.watchFolders.description",
    },
    excluded_file_names_enabled: { labelKey: "preferences.fields.excludedFileNamesEnabled" },
    excluded_file_names: {
      labelKey: "preferences.fields.excludedFileNames",
      descriptionKey: "preferences.fields.excludedFileNamesDescription",
    },
    mail_notification_enabled: { labelKey: "preferences.fields.mailNotificationEnabled" },
    mail_notification_sender: { labelKey: "preferences.fields.mailNotificationSender" },
    mail_notification_email: {
      labelKey: "preferences.fields.mailNotificationEmail",
      descriptionKey: "preferences.fields.mailNotificationEmailDescription",
    },
    mail_notification_smtp: {
      labelKey: "preferences.fields.mailNotificationSmtp",
      descriptionKey: "preferences.fields.mailNotificationSmtpDescription",
      placeholderKey: "preferences.fields.mailNotificationSmtpPlaceholder",
    },
    // qBittorrent 5.3 replaced mail_notification_ssl_enabled with mail_notification_encryption_type.
    mail_notification_ssl_enabled: { labelKey: "preferences.fields.mailNotificationSslEnabled" },
    mail_notification_encryption_type: {
      labelKey: "preferences.fields.mailNotificationEncryptionType",
      descriptionKey: "preferences.fields.mailNotificationEncryptionTypeDescription",
      options: {
        None: { labelKey: "preferences.fields.mailNotificationEncryptionNone" },
        STARTTLS: { labelKey: "preferences.fields.mailNotificationEncryptionStartTls" },
        SMTPS: { labelKey: "preferences.fields.mailNotificationEncryptionSmtps" },
      },
    },
    mail_notification_auth_enabled: { labelKey: "preferences.fields.mailNotificationAuthEnabled" },
    mail_notification_username: { labelKey: "preferences.fields.mailNotificationUsername" },
    mail_notification_password: { labelKey: "preferences.fields.mailNotificationPassword" },
    autorun_on_torrent_added_enabled: {
      labelKey: "preferences.fileManagement.runOnTorrentAdded",
      descriptionKey: "preferences.fileManagement.runOnTorrentAddedDescription",
    },
    autorun_on_torrent_added_program: {
      labelKey: "preferences.fileManagement.command",
      descriptionKey: "preferences.fileManagement.autorunProgramTip",
    },
    autorun_enabled: {
      labelKey: "preferences.fileManagement.runOnTorrentFinished",
      descriptionKey: "preferences.fileManagement.runOnTorrentFinishedDescription",
    },
    autorun_program: {
      labelKey: "preferences.fileManagement.command",
      descriptionKey: "preferences.fileManagement.autorunProgramTip",
    },
  },
}
