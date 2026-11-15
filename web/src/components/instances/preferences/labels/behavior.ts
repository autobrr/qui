/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TabLabels } from "./types"

// i18n-namespace: instances

export const behaviorLabels: TabLabels = {
  labelKey: "preferences.dialog.tabs.behavior",
  sections: {
    logFile: { labelKey: "preferences.sections.logFile" },
  },
  fields: {
    file_log_enabled: { labelKey: "preferences.fields.fileLogEnabled" },
    file_log_path: { labelKey: "preferences.fields.fileLogPath" },
    file_log_backup_enabled: { labelKey: "preferences.fields.fileLogBackupEnabled" },
    file_log_max_size: {
      labelKey: "preferences.fields.fileLogMaxSize",
      descriptionKey: "preferences.fields.fileLogMaxSizeDescription",
    },
    file_log_delete_old: { labelKey: "preferences.fields.fileLogDeleteOld" },
    file_log_age: { labelKey: "preferences.fields.fileLogAge" },
    file_log_age_type: {
      labelKey: "preferences.fields.fileLogAgeType",
      options: {
        0: { labelKey: "preferences.fields.fileLogAgeDays" },
        1: { labelKey: "preferences.fields.fileLogAgeMonths" },
        2: { labelKey: "preferences.fields.fileLogAgeYears" },
      },
    },
  },
}
