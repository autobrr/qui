/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TabLabels } from "./types"

// i18n-namespace: instances

export const speedLabels: TabLabels = {
  labelKey: "preferences.dialog.tabs.speed",
  sections: {
    globalRateLimits: { labelKey: "preferences.sections.globalRateLimits" },
    alternativeRateLimits: { labelKey: "preferences.sections.alternativeRateLimits" },
    rateLimitsSettings: { labelKey: "preferences.sections.rateLimitsSettings" },
  },
  fields: {
    up_limit: { labelKey: "preferences.speedLimits.uploadLimit", unlimited: 0 },
    dl_limit: { labelKey: "preferences.speedLimits.downloadLimit", unlimited: 0 },
    alt_up_limit: { labelKey: "preferences.speedLimits.altUploadLimit", unlimited: 0 },
    alt_dl_limit: { labelKey: "preferences.speedLimits.altDownloadLimit", unlimited: 0 },
    scheduler_enabled: { labelKey: "preferences.speedLimits.scheduleAltLimits" },
    schedule_from_hour: { labelKey: "preferences.fields.scheduleFromHour" },
    schedule_from_min: { labelKey: "preferences.fields.scheduleFromMinute" },
    schedule_to_hour: { labelKey: "preferences.fields.scheduleToHour" },
    schedule_to_min: { labelKey: "preferences.fields.scheduleToMinute" },
    scheduler_days: {
      labelKey: "preferences.speedLimits.when",
      options: {
        0: { labelKey: "preferences.speedLimits.everyDay" },
        1: { labelKey: "preferences.speedLimits.everyWeekday" },
        2: { labelKey: "preferences.speedLimits.everyWeekend" },
        3: { labelKey: "preferences.speedLimits.monday" },
        4: { labelKey: "preferences.speedLimits.tuesday" },
        5: { labelKey: "preferences.speedLimits.wednesday" },
        6: { labelKey: "preferences.speedLimits.thursday" },
        7: { labelKey: "preferences.speedLimits.friday" },
        8: { labelKey: "preferences.speedLimits.saturday" },
        9: { labelKey: "preferences.speedLimits.sunday" },
      },
    },
    limit_utp_rate: {
      labelKey: "preferences.advancedNetwork.limitUtpProtocol",
      descriptionKey: "preferences.advancedNetwork.limitUtpConnectionsDescription",
    },
    limit_tcp_overhead: {
      labelKey: "preferences.advancedNetwork.limitTcpOverhead",
      descriptionKey: "preferences.advancedNetwork.limitTcpOverheadDescription",
    },
    limit_lan_peers: { labelKey: "preferences.fields.limitLanPeers" },
  },
}
