/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TabLabels } from "./types"

// i18n-namespace: instances

export const connectionLabels: TabLabels = {
  labelKey: "preferences.dialog.tabs.connection",
  sections: {
    protocol: { labelKey: "preferences.connectionSettings.protocolSettings" },
    listeningPort: { labelKey: "preferences.connectionSettings.listeningPort" },
    connectionLimits: { labelKey: "preferences.connectionSettings.connectionLimits" },
    ipFiltering: { labelKey: "preferences.connectionSettings.ipFiltering" },
  },
  fields: {
    bittorrent_protocol: {
      labelKey: "preferences.connectionSettings.bittorrentProtocol",
      descriptionKey: "preferences.connectionSettings.protocolDescription",
      options: {
        0: { labelKey: "preferences.connectionSettings.protocolTcpUtp" },
        1: { labelKey: "preferences.connectionSettings.protocolTcp" },
        2: { labelKey: "preferences.connectionSettings.protocolUtp" },
      },
    },
    listen_port: {
      labelKey: "preferences.connectionSettings.portForIncoming",
      descriptionKey: "preferences.connectionSettings.portDescription",
    },
    upnp: {
      labelKey: "preferences.connectionSettings.enableUpnp",
      descriptionKey: "preferences.connectionSettings.enableUpnpDescription",
    },
    max_connec: {
      labelKey: "preferences.connectionSettings.globalMaxConnections",
      descriptionKey: "preferences.connectionSettings.globalMaxConnectionsDescription",
      unlimited: -1,
    },
    max_connec_per_torrent: {
      labelKey: "preferences.connectionSettings.maxConnectionsPerTorrent",
      descriptionKey: "preferences.connectionSettings.maxConnectionsPerTorrentDescription",
      unlimited: -1,
    },
    max_uploads: {
      labelKey: "preferences.connectionSettings.globalMaxUploadSlots",
      descriptionKey: "preferences.connectionSettings.globalMaxUploadSlotsDescription",
      unlimited: -1,
    },
    max_uploads_per_torrent: {
      labelKey: "preferences.connectionSettings.maxUploadSlotsPerTorrent",
      descriptionKey: "preferences.connectionSettings.maxUploadSlotsPerTorrentDescription",
      unlimited: -1,
    },
    ip_filter_enabled: {
      labelKey: "preferences.connectionSettings.enableIpFiltering",
      descriptionKey: "preferences.connectionSettings.enableIpFilteringDescription",
    },
    ip_filter_path: {
      labelKey: "preferences.connectionSettings.ipFilterPath",
      descriptionKey: "preferences.connectionSettings.ipFilterPathDescription",
      placeholderKey: "preferences.connectionSettings.ipFilterPathPlaceholder",
    },
    ip_filter_trackers: {
      labelKey: "preferences.connectionSettings.applyIpFilterToTrackers",
      descriptionKey: "preferences.connectionSettings.applyIpFilterToTrackersDescription",
    },
    banned_IPs: {
      labelKey: "preferences.connectionSettings.bannedIps",
      descriptionKey: "preferences.connectionSettings.bannedIpsDescription",
      placeholderKey: "preferences.connectionSettings.bannedIpsPlaceholder",
    },
  },
}
