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
    resume_data_storage_type: {
      labelKey: "preferences.fields.resumeDataStorageType",
      descriptionKey: "preferences.fields.requiresRestart",
      options: {
        Legacy: { labelKey: "preferences.fields.resumeDataStorageLegacy" },
        SQLite: { labelKey: "preferences.fields.resumeDataStorageSqlite" },
      },
    },
    torrent_content_remove_option: {
      labelKey: "preferences.fields.torrentContentRemoveOption",
      options: {
        Delete: { labelKey: "preferences.fields.torrentContentRemoveDelete" },
        MoveToTrash: { labelKey: "preferences.fields.torrentContentRemoveMoveToTrash" },
      },
    },
    memory_working_set_limit: {
      labelKey: "preferences.advancedNetwork.workingSetLimit",
      descriptionKey: "preferences.advancedNetwork.workingSetLimitDescription",
    },
    current_network_interface: { labelKey: "preferences.connectionSettings.networkInterface" },
    current_interface_address: { labelKey: "preferences.fields.interfaceAddress" },
    // The network binding widget draws this key. Its read-only text names the saved interface.
    current_interface_name: { labelKey: "preferences.connectionSettings.networkInterface" },
    save_resume_data_interval: { labelKey: "preferences.fields.saveResumeDataInterval" },
    save_statistics_interval: { labelKey: "preferences.fields.saveStatisticsInterval" },
    torrent_file_size_limit: { labelKey: "preferences.fields.torrentFileSizeLimit" },
    recheck_completed_torrents: { labelKey: "preferences.fields.recheckCompletedTorrents" },
    resolve_peer_countries: {
      labelKey: "preferences.networkDiscovery.resolvePeerCountries",
      descriptionKey: "preferences.networkDiscovery.resolvePeerCountriesDescription",
    },
    reannounce_when_address_changed: {
      labelKey: "preferences.connectionSettings.reannounceOnIpChange",
      descriptionKey: "preferences.connectionSettings.reannounceOnIpChangeDescription",
    },
    enable_embedded_tracker: { labelKey: "preferences.fields.enableEmbeddedTracker" },
    embedded_tracker_port: { labelKey: "preferences.fields.embeddedTrackerPort" },
    embedded_tracker_port_forwarding: { labelKey: "preferences.fields.embeddedTrackerPortForwarding" },
    mark_of_the_web: { labelKey: "preferences.fields.markOfTheWeb" },
    ignore_ssl_errors: {
      labelKey: "preferences.fields.ignoreSslErrors",
      descriptionKey: "preferences.fields.ignoreSslErrorsDescription",
    },
    start_paused: {
      labelKey: "preferences.fields.startSessionPaused",
      descriptionKey: "preferences.fields.startSessionPausedDescription",
    },
    shutdown_timeout: {
      labelKey: "preferences.fields.shutdownTimeout",
      descriptionKey: "preferences.fields.shutdownTimeoutDescription",
      unlimited: -1,
    },
    bdecode_depth_limit: {
      labelKey: "preferences.fields.bdecodeDepthLimit",
      descriptionKey: "preferences.fields.bdecodeDepthLimitDescription",
    },
    bdecode_token_limit: {
      labelKey: "preferences.fields.bdecodeTokenLimit",
      descriptionKey: "preferences.fields.bdecodeTokenLimitDescription",
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
    enable_os_cache: { labelKey: "preferences.fields.diskIoModeEnableOsCache" },
    enable_coalesce_read_write: {
      labelKey: "preferences.advancedNetwork.coalesceReadsWrites",
      descriptionKey: "preferences.advancedNetwork.coalesceReadsWritesDescription",
    },
    enable_piece_extent_affinity: { labelKey: "preferences.fields.enablePieceExtentAffinity" },
    enable_upload_suggestions: { labelKey: "preferences.fields.enableUploadSuggestions" },
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
    connection_speed: { labelKey: "preferences.fields.connectionSpeed" },
    seeding_outgoing_connections: { labelKey: "preferences.fields.seedingOutgoingConnections" },
    socket_send_buffer_size: {
      labelKey: "preferences.fields.socketSendBufferSize",
      descriptionKey: "preferences.fields.socketBufferSizeDescription",
    },
    socket_receive_buffer_size: {
      labelKey: "preferences.fields.socketReceiveBufferSize",
      descriptionKey: "preferences.fields.socketBufferSizeDescription",
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
    hostname_cache_ttl: { labelKey: "preferences.fields.hostnameCacheTtl" },
    idn_support_enabled: { labelKey: "preferences.fields.idnSupportEnabled" },
    enable_multi_connections_from_same_ip: {
      labelKey: "preferences.connectionSettings.allowMultiConnections",
      descriptionKey: "preferences.connectionSettings.allowMultiConnectionsDescription",
    },
    enable_multi_connections_from_same_peer_id: { labelKey: "preferences.fields.multiConnectionsSamePeerId" },
    validate_https_tracker_certificate: { labelKey: "preferences.fields.validateHttpsTrackerCertificate" },
    ssrf_mitigation: {
      labelKey: "preferences.fields.ssrfMitigation",
      descriptionKey: "preferences.fields.ssrfMitigationDescription",
    },
    block_peers_on_privileged_ports: {
      labelKey: "preferences.advancedNetwork.blockPrivilegedPorts",
      descriptionKey: "preferences.advancedNetwork.blockPrivilegedPortsDescription",
    },
    upload_slots_behavior: {
      labelKey: "preferences.fields.uploadSlotsBehavior",
      options: {
        0: { labelKey: "preferences.fields.uploadSlotsFixed" },
        1: { labelKey: "preferences.fields.uploadSlotsRateBased" },
      },
    },
    upload_choking_algorithm: {
      labelKey: "preferences.fields.uploadChokingAlgorithm",
      options: {
        0: { labelKey: "preferences.fields.chokingRoundRobin" },
        1: { labelKey: "preferences.fields.chokingFastestUpload" },
        2: { labelKey: "preferences.fields.chokingAntiLeech" },
      },
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
    announce_port: {
      labelKey: "preferences.fields.announcePort",
      descriptionKey: "preferences.fields.announcePortDescription",
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
    max_outstanding_block_requests: { labelKey: "preferences.fields.maxOutstandingBlockRequests" },
    dht_bootstrap_nodes: {
      labelKey: "preferences.fields.dhtBootstrapNodes",
      placeholderKey: "preferences.fields.dhtBootstrapNodesPlaceholder",
    },
    webtorrent_stun_server: { labelKey: "preferences.fields.webTorrentStunServer" },
    i2p_inbound_quantity: { labelKey: "preferences.fields.i2pInboundQuantity" },
    i2p_outbound_quantity: { labelKey: "preferences.fields.i2pOutboundQuantity" },
    i2p_inbound_length: { labelKey: "preferences.fields.i2pInboundLength" },
    i2p_outbound_length: { labelKey: "preferences.fields.i2pOutboundLength" },
    i2p_inbound_length_variance: { labelKey: "preferences.fields.i2pInboundLengthVariance" },
    i2p_outbound_length_variance: { labelKey: "preferences.fields.i2pOutboundLengthVariance" },
  },
}
