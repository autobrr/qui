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
    savingManagement: { labelKey: "preferences.sections.savingManagement" },
    excludedFileNames: { labelKey: "preferences.sections.excludedFileNames" },
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
    // qBittorrent 5.0 renamed start_paused_enabled to add_stopped_enabled. It sends one of the two.
    start_paused_enabled: startPaused,
    add_stopped_enabled: startPaused,
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
    excluded_file_names: { labelKey: "preferences.fields.excludedFileNames" },
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
