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
    i2p: { labelKey: "preferences.sections.i2p" },
    proxy: { labelKey: "preferences.sections.proxy" },
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
    ssl_enabled: {
      labelKey: "preferences.fields.sslEnabled",
      descriptionKey: "preferences.fields.sslEnabledDescription",
    },
    ssl_listen_port: { labelKey: "preferences.fields.sslListenPort" },
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
    i2p_enabled: { labelKey: "preferences.fields.i2pEnabled" },
    i2p_address: { labelKey: "preferences.fields.i2pAddress" },
    i2p_port: { labelKey: "preferences.fields.i2pPort" },
    i2p_mixed_mode: {
      labelKey: "preferences.fields.i2pMixedMode",
      descriptionKey: "preferences.fields.i2pMixedModeDescription",
    },
    i2p_pex_enabled: {
      labelKey: "preferences.fields.i2pPexEnabled",
      descriptionKey: "preferences.fields.requiresRestart",
    },
    proxy_type: {
      labelKey: "preferences.fields.proxyType",
      options: {
        None: { labelKey: "preferences.fields.proxyTypeNone" },
        SOCKS4: { labelKey: "preferences.fields.proxyTypeSocks4" },
        SOCKS5: { labelKey: "preferences.fields.proxyTypeSocks5" },
        HTTP: { labelKey: "preferences.fields.proxyTypeHttp" },
        0: { labelKey: "preferences.fields.proxyTypeNone" },
        1: { labelKey: "preferences.fields.proxyTypeHttp" },
        2: { labelKey: "preferences.fields.proxyTypeSocks5" },
        3: { labelKey: "preferences.fields.proxyTypeHttpAuth" },
        4: { labelKey: "preferences.fields.proxyTypeSocks5Auth" },
        5: { labelKey: "preferences.fields.proxyTypeSocks4" },
      },
    },
    proxy_ip: { labelKey: "preferences.fields.proxyIp" },
    proxy_port: { labelKey: "preferences.fields.proxyPort" },
    proxy_hostname_lookup: { labelKey: "preferences.fields.proxyHostnameLookup" },
    proxy_auth_enabled: { labelKey: "preferences.fields.proxyAuthEnabled" },
    proxy_username: { labelKey: "preferences.fields.proxyUsername" },
    proxy_password: {
      labelKey: "preferences.fields.proxyPassword",
      descriptionKey: "preferences.fields.proxyPasswordDescription",
    },
    proxy_bittorrent: { labelKey: "preferences.fields.proxyBittorrent" },
    proxy_peer_connections: { labelKey: "preferences.fields.proxyPeerConnections" },
    proxy_torrents_only: { labelKey: "preferences.fields.proxyTorrentsOnly" },
    proxy_rss: { labelKey: "preferences.fields.proxyRss" },
    proxy_misc: { labelKey: "preferences.fields.proxyMisc" },
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
