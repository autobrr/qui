// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package prefcatalog

import "slices"

var connectionEntries = slices.Concat(
	[]Entry{
		{Key: "bittorrent_protocol", Tab: TabConnection, Section: "protocol", Control: ControlSelect, OptionSource: SourceFixed, Options: fixedOptions(0, 1, 2)},
		{Key: "listen_port", Tab: TabConnection, Section: "listeningPort", Control: ControlNumber},
		{Key: "upnp", Tab: TabConnection, Section: "listeningPort", Control: ControlSwitch},
		{Key: "max_connec", Tab: TabConnection, Section: "connectionLimits", Control: ControlNumber},
		{Key: "max_connec_per_torrent", Tab: TabConnection, Section: "connectionLimits", Control: ControlNumber},
		{Key: "max_uploads", Tab: TabConnection, Section: "connectionLimits", Control: ControlNumber},
		{Key: "max_uploads_per_torrent", Tab: TabConnection, Section: "connectionLimits", Control: ControlNumber},
		{Key: "ip_filter_enabled", Tab: TabConnection, Section: "ipFiltering", Control: ControlSwitch},
		{Key: "ip_filter_path", Tab: TabConnection, Section: "ipFiltering", Control: ControlPath, DependsOn: "ip_filter_enabled"},
		{Key: "ip_filter_trackers", Tab: TabConnection, Section: "ipFiltering", Control: ControlSwitch, DependsOn: "ip_filter_enabled"},
		{Key: "banned_IPs", Tab: TabConnection, Section: "ipFiltering", Control: ControlList},
	},
	notShown(TabConnection, "pending proxy and I2P PR",
		"i2p_enabled", "i2p_address", "i2p_port", "i2p_mixed_mode", "i2p_pex_enabled",
		"proxy_type", "proxy_ip", "proxy_port", "proxy_hostname_lookup", "proxy_auth_enabled",
		"proxy_username", "proxy_password", "proxy_bittorrent", "proxy_peer_connections",
		"proxy_rss", "proxy_misc", "proxy_torrents_only"),
	// qBittorrent 4.4.0 deprecates random_port. There, true sets the listening port to 0, the same as a
	// listen_port of 0. On 4.3.x it is a separate setting (a new port on each startup), which qui does not show.
	notShown(TabConnection, "deprecated by qBittorrent", "random_port"),
	notShown(TabConnection, "pending Connection PR", "ssl_enabled", "ssl_listen_port"),
)
