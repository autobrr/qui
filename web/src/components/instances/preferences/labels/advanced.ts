/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TabLabels } from "./types"

// i18n-namespace: instances

const osCacheOptions = {
  0: { labelKey: "preferences.fields.diskIoModeDisableOsCache" },
  1: { labelKey: "preferences.fields.diskIoModeEnableOsCache" },
}

export const advancedLabels: TabLabels = {
  labelKey: "preferences.dialog.tabs.advanced",
  sections: {
    qbittorrent: { labelKey: "preferences.sections.qbittorrent" },
    libtorrent: { labelKey: "preferences.sections.libtorrent" },
  },
  fields: {
    memory_working_set_limit: {
      labelKey: "preferences.advancedNetwork.workingSetLimit",
      descriptionKey: "preferences.advancedNetwork.workingSetLimitDescription",
    },
    current_network_interface: {
      labelKey: "preferences.connectionSettings.networkInterface",
      placeholderKey: "preferences.connectionSettings.autoDetect",
    },
    current_interface_address: {
      labelKey: "preferences.fields.interfaceAddress",
      placeholderKey: "preferences.connectionSettings.autoDetect",
    },
    resolve_peer_countries: {
      labelKey: "preferences.networkDiscovery.resolvePeerCountries",
      descriptionKey: "preferences.networkDiscovery.resolvePeerCountriesDescription",
    },
    reannounce_when_address_changed: {
      labelKey: "preferences.connectionSettings.reannounceOnIpChange",
      descriptionKey: "preferences.connectionSettings.reannounceOnIpChangeDescription",
    },
    async_io_threads: {
      labelKey: "preferences.advancedNetwork.asyncIoThreads",
      descriptionKey: "preferences.advancedNetwork.asyncIoThreadsDescription",
    },
    hashing_threads: {
      labelKey: "preferences.advancedNetwork.hashingThreads",
      descriptionKey: "preferences.advancedNetwork.hashingThreadsDescription",
    },
    file_pool_size: {
      labelKey: "preferences.advancedNetwork.filePoolSize",
      descriptionKey: "preferences.advancedNetwork.filePoolSizeDescription",
    },
    checking_memory_use: {
      labelKey: "preferences.advancedNetwork.checkingMemoryUse",
      descriptionKey: "preferences.advancedNetwork.checkingMemoryUseDescription",
    },
    disk_cache: {
      labelKey: "preferences.advancedNetwork.diskCacheSize",
      descriptionKey: "preferences.advancedNetwork.diskCacheSizeDescription",
    },
    disk_cache_ttl: {
      labelKey: "preferences.advancedNetwork.diskCacheTtl",
      descriptionKey: "preferences.advancedNetwork.diskCacheTtlDescription",
    },
    disk_queue_size: {
      labelKey: "preferences.advancedNetwork.diskQueueSize",
      descriptionKey: "preferences.advancedNetwork.diskQueueSizeDescription",
    },
    disk_io_type: {
      labelKey: "preferences.fields.diskIoType",
      options: {
        0: { labelKey: "preferences.fields.diskIoTypeDefault" },
        1: { labelKey: "preferences.fields.diskIoTypeMmap" },
        2: { labelKey: "preferences.fields.diskIoTypePosix" },
        3: { labelKey: "preferences.fields.diskIoTypeSimplePreadPwrite" },
        4: { labelKey: "preferences.fields.diskIoTypePreadPwrite" },
      },
    },
    disk_io_read_mode: { labelKey: "preferences.fields.diskIoReadMode", options: osCacheOptions },
    disk_io_write_mode: {
      labelKey: "preferences.fields.diskIoWriteMode",
      options: { ...osCacheOptions, 2: { labelKey: "preferences.fields.diskIoModeWriteThrough" } },
    },
    enable_coalesce_read_write: {
      labelKey: "preferences.advancedNetwork.coalesceReadsWrites",
      descriptionKey: "preferences.advancedNetwork.coalesceReadsWritesDescription",
    },
    send_buffer_watermark: {
      labelKey: "preferences.advancedNetwork.sendBufferWatermark",
      descriptionKey: "preferences.advancedNetwork.sendBufferWatermarkDescription",
    },
    send_buffer_low_watermark: {
      labelKey: "preferences.advancedNetwork.sendBufferLowWatermark",
      descriptionKey: "preferences.advancedNetwork.sendBufferLowWatermarkDescription",
    },
    send_buffer_watermark_factor: {
      labelKey: "preferences.advancedNetwork.watermarkFactor",
      descriptionKey: "preferences.advancedNetwork.watermarkFactorDescription",
    },
    socket_backlog_size: {
      labelKey: "preferences.advancedNetwork.socketBacklogSize",
      descriptionKey: "preferences.advancedNetwork.socketBacklogSizeDescription",
    },
    outgoing_ports_min: {
      labelKey: "preferences.connectionSettings.outgoingPortsMin",
      descriptionKey: "preferences.connectionSettings.outgoingPortsMinDescription",
    },
    outgoing_ports_max: {
      labelKey: "preferences.connectionSettings.outgoingPortsMax",
      descriptionKey: "preferences.connectionSettings.outgoingPortsMaxDescription",
    },
    upnp_lease_duration: { labelKey: "preferences.connectionSettings.upnpLeaseDuration" },
    peer_tos: {
      labelKey: "preferences.advancedNetwork.peerTosByte",
      descriptionKey: "preferences.advancedNetwork.peerTosDescription",
    },
    utp_tcp_mixed_mode: {
      labelKey: "preferences.connectionSettings.utpTcpMixedMode",
      descriptionKey: "preferences.connectionSettings.utpTcpMixedModeDescription",
      options: {
        0: { labelKey: "preferences.connectionSettings.preferTcp" },
        1: { labelKey: "preferences.connectionSettings.peerProportional" },
      },
    },
    enable_multi_connections_from_same_ip: {
      labelKey: "preferences.connectionSettings.allowMultiConnections",
      descriptionKey: "preferences.connectionSettings.allowMultiConnectionsDescription",
    },
    block_peers_on_privileged_ports: {
      labelKey: "preferences.advancedNetwork.blockPrivilegedPorts",
      descriptionKey: "preferences.advancedNetwork.blockPrivilegedPortsDescription",
    },
    announce_to_all_trackers: {
      labelKey: "preferences.networkDiscovery.announceToAllTrackers",
      descriptionKey: "preferences.networkDiscovery.announceToAllTrackersDescription",
    },
    announce_to_all_tiers: {
      labelKey: "preferences.networkDiscovery.announceToAllTiers",
      descriptionKey: "preferences.networkDiscovery.announceToAllTiersDescription",
    },
    announce_ip: {
      labelKey: "preferences.advancedNetwork.announceIp",
      descriptionKey: "preferences.advancedNetwork.announceIpDescription",
      placeholderKey: "preferences.connectionSettings.autoDetect",
    },
    max_concurrent_http_announces: {
      labelKey: "preferences.advancedNetwork.maxHttpAnnounces",
      descriptionKey: "preferences.advancedNetwork.maxHttpAnnouncesDescription",
    },
    stop_tracker_timeout: {
      labelKey: "preferences.advancedNetwork.stopTrackerTimeout",
      descriptionKey: "preferences.advancedNetwork.stopTrackerTimeoutDescription",
    },
    peer_turnover: {
      labelKey: "preferences.advancedNetwork.peerTurnover",
      descriptionKey: "preferences.advancedNetwork.peerTurnoverDescription",
    },
    peer_turnover_cutoff: {
      labelKey: "preferences.advancedNetwork.turnoverCutoff",
      descriptionKey: "preferences.advancedNetwork.turnoverCutoffDescription",
    },
    peer_turnover_interval: {
      labelKey: "preferences.advancedNetwork.turnoverInterval",
      descriptionKey: "preferences.advancedNetwork.turnoverIntervalDescription",
    },
    request_queue_size: {
      labelKey: "preferences.advancedNetwork.requestQueueSize",
      descriptionKey: "preferences.advancedNetwork.requestQueueSizeDescription",
    },
  },
}
