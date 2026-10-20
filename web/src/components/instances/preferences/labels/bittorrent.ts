/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TabLabels } from "./types"

// i18n-namespace: instances

export const bittorrentLabels: TabLabels = {
  labelKey: "preferences.dialog.tabs.bittorrent",
  sections: {
    privacy: { labelKey: "preferences.networkDiscovery.securityPrivacy" },
    queueing: { labelKey: "preferences.dialog.sections.queueManagement.title" },
    seedingLimits: { labelKey: "preferences.dialog.sections.seedingLimits.title" },
    autoAddTrackers: { labelKey: "preferences.sections.autoAddTrackers" },
    autoAddTrackersFromUrl: { labelKey: "preferences.sections.autoAddTrackersFromUrl" },
  },
  fields: {
    dht: {
      labelKey: "preferences.networkDiscovery.enableDht",
      descriptionKey: "preferences.networkDiscovery.enableDhtDescription",
    },
    pex: {
      labelKey: "preferences.networkDiscovery.enablePex",
      descriptionKey: "preferences.networkDiscovery.enablePexDescription",
    },
    lsd: {
      labelKey: "preferences.networkDiscovery.enableLsd",
      descriptionKey: "preferences.networkDiscovery.enableLsdDescription",
    },
    encryption: {
      labelKey: "preferences.networkDiscovery.protocolEncryption",
      descriptionKey: "preferences.networkDiscovery.encryptionDescription",
      options: {
        0: { labelKey: "preferences.networkDiscovery.preferEncryption" },
        1: { labelKey: "preferences.networkDiscovery.requireEncryption" },
        2: { labelKey: "preferences.networkDiscovery.disableEncryption" },
      },
    },
    anonymous_mode: {
      labelKey: "preferences.networkDiscovery.enableAnonymousMode",
      descriptionKey: "preferences.networkDiscovery.enableAnonymousModeDescription",
    },
    max_active_checking_torrents: {
      labelKey: "preferences.queueManagement.maxCheckingTorrents",
      descriptionKey: "preferences.queueManagement.maxCheckingTorrentsDescription",
      unlimited: -1,
    },
    queueing_enabled: {
      labelKey: "preferences.queueManagement.enableQueueing",
      descriptionKey: "preferences.queueManagement.enableQueueingDescription",
    },
    max_active_downloads: {
      labelKey: "preferences.queueManagement.maxActiveDownloads",
      descriptionKey: "preferences.queueManagement.maxActiveDownloadsDescription",
      unlimited: -1,
    },
    max_active_uploads: {
      labelKey: "preferences.queueManagement.maxActiveUploads",
      descriptionKey: "preferences.queueManagement.maxActiveUploadsDescription",
      unlimited: -1,
    },
    max_active_torrents: {
      labelKey: "preferences.queueManagement.maxActiveTorrents",
      descriptionKey: "preferences.queueManagement.maxActiveTorrentsDescription",
      unlimited: -1,
    },
    dont_count_slow_torrents: {
      labelKey: "preferences.queueManagement.dontCountSlowTorrents",
      descriptionKey: "preferences.queueManagement.dontCountSlowTorrentsDescription",
    },
    slow_torrent_dl_rate_threshold: { labelKey: "preferences.fields.slowTorrentDownloadRate" },
    slow_torrent_ul_rate_threshold: { labelKey: "preferences.fields.slowTorrentUploadRate" },
    slow_torrent_inactive_timer: {
      labelKey: "preferences.fields.slowTorrentInactiveTimer",
      descriptionKey: "preferences.queueManagement.slowTorrentInactiveTimerDescription",
    },
    max_ratio_enabled: {
      labelKey: "preferences.seedingLimits.enableShareRatioLimit",
      descriptionKey: "preferences.seedingLimits.enableShareRatioLimitDescription",
    },
    max_ratio: {
      labelKey: "preferences.seedingLimits.maxShareRatio",
      descriptionKey: "preferences.seedingLimits.maxShareRatioDescription",
      unlimited: -1,
      whenEnabled: 1,
    },
    max_seeding_time_enabled: {
      labelKey: "preferences.seedingLimits.enableSeedingTimeLimit",
      descriptionKey: "preferences.seedingLimits.enableSeedingTimeLimitDescription",
    },
    max_seeding_time: {
      labelKey: "preferences.fields.maxSeedingTime",
      descriptionKey: "preferences.seedingLimits.maxSeedingTimeDescription",
      unlimited: -1,
      whenEnabled: 1440,
    },
    max_inactive_seeding_time_enabled: { labelKey: "preferences.fields.maxInactiveSeedingTimeEnabled" },
    max_inactive_seeding_time: {
      labelKey: "preferences.fields.maxInactiveSeedingTime",
      descriptionKey: "preferences.fields.maxInactiveSeedingTimeDescription",
      unlimited: -1,
      whenEnabled: 1440,
    },
    share_limits_mode: {
      labelKey: "preferences.workflowDialog.shareLimitsMode.label",
      options: {
        MatchAny: { labelKey: "preferences.workflowDialog.shareLimitsMode.matchAny" },
        MatchAll: { labelKey: "preferences.workflowDialog.shareLimitsMode.matchAll" },
      },
    },
    max_ratio_act: {
      labelKey: "preferences.workflowDialog.shareLimitAction.label",
      options: {
        0: { labelKey: "preferences.workflowDialog.shareLimitAction.stop" },
        1: { labelKey: "preferences.workflowDialog.shareLimitAction.remove" },
        3: { labelKey: "preferences.workflowDialog.shareLimitAction.removeWithContent" },
        2: { labelKey: "preferences.workflowDialog.shareLimitAction.enableSuperSeeding" },
      },
    },
    add_trackers_enabled: { labelKey: "preferences.fields.addTrackersEnabled" },
    add_trackers: { labelKey: "preferences.fields.addTrackers" },
    add_trackers_from_url_enabled: { labelKey: "preferences.fields.addTrackersFromUrlEnabled" },
    add_trackers_url: { labelKey: "preferences.fields.addTrackersUrl" },
    add_trackers_url_list: {
      labelKey: "preferences.fields.addTrackersUrlList",
      readOnlyNoteKey: "preferences.fields.addTrackersUrlListNote",
    },
  },
}
