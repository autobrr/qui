// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package prefcatalog

import "slices"

var connectionEntries = slices.Concat(
	[]Entry{
		{Key: "bittorrent_protocol", Tab: TabConnection, Section: "protocol", Control: ControlSelect, OptionSource: SourceFixed, Options: fixedOptions(0, 1, 2)},
		{Key: "listen_port", Tab: TabConnection, Section: "listeningPort", Control: ControlNumber},
		{Key: "upnp", Tab: TabConnection, Section: "listeningPort", Control: ControlSwitch},
		// qBittorrent 5.2 added SSL torrents with no control in its WebUI. The keys change what qBittorrent does, so qui shows them.
		{Key: "ssl_enabled", Tab: TabConnection, Section: "listeningPort", Control: ControlSwitch},
		{Key: "ssl_listen_port", Tab: TabConnection, Section: "listeningPort", Control: ControlNumber, DependsOn: "ssl_enabled"},
		{Key: "max_connec", Tab: TabConnection, Section: "connectionLimits", Control: ControlNumber},
		{Key: "max_connec_per_torrent", Tab: TabConnection, Section: "connectionLimits", Control: ControlNumber},
		{Key: "max_uploads", Tab: TabConnection, Section: "connectionLimits", Control: ControlNumber},
		{Key: "max_uploads_per_torrent", Tab: TabConnection, Section: "connectionLimits", Control: ControlNumber},
		{Key: "i2p_enabled", Tab: TabConnection, Section: "i2p", Control: ControlSwitch, Gate: libtorrent2Only},
		{Key: "i2p_address", Tab: TabConnection, Section: "i2p", Control: ControlText, Gate: libtorrent2Only, DependsOn: "i2p_enabled"},
		{Key: "i2p_port", Tab: TabConnection, Section: "i2p", Control: ControlNumber, Gate: libtorrent2Only, DependsOn: "i2p_enabled"},
		{Key: "i2p_mixed_mode", Tab: TabConnection, Section: "i2p", Control: ControlSwitch, Gate: libtorrent2Only, DependsOn: "i2p_enabled"},
		{Key: "i2p_pex_enabled", Tab: TabConnection, Section: "i2p", Control: ControlSwitch, Gate: Gate{MinLibtorrent: "2.1"}, DependsOn: "i2p_enabled"},
		// The strings are for qBittorrent 4.6 and later, and the numbers for earlier versions.
		{Key: "proxy_type", Tab: TabConnection, Section: "proxy", Control: ControlSelect, OptionSource: SourceFixed, Options: fixedOptions(
			"None", "SOCKS4", "SOCKS5", "HTTP",
			0, 5, 2, 4, 1, 3,
		)},
		{Key: "proxy_ip", Tab: TabConnection, Section: "proxy", Control: ControlText},
		{Key: "proxy_port", Tab: TabConnection, Section: "proxy", Control: ControlNumber},
		{Key: "proxy_hostname_lookup", Tab: TabConnection, Section: "proxy", Control: ControlSwitch},
		{Key: "proxy_auth_enabled", Tab: TabConnection, Section: "proxy", Control: ControlSwitch},
		{Key: "proxy_username", Tab: TabConnection, Section: "proxy", Control: ControlText, DependsOn: "proxy_auth_enabled"},
		{Key: "proxy_password", Tab: TabConnection, Section: "proxy", Control: ControlPassword, DependsOn: "proxy_auth_enabled"},
		{Key: "proxy_bittorrent", Tab: TabConnection, Section: "proxy", Control: ControlSwitch},
		{Key: "proxy_peer_connections", Tab: TabConnection, Section: "proxy", Control: ControlSwitch, DependsOn: "proxy_bittorrent"},
		{Key: "proxy_torrents_only", Tab: TabConnection, Section: "proxy", Control: ControlSwitch},
		{Key: "proxy_rss", Tab: TabConnection, Section: "proxy", Control: ControlSwitch},
		{Key: "proxy_misc", Tab: TabConnection, Section: "proxy", Control: ControlSwitch},
		{Key: "ip_filter_enabled", Tab: TabConnection, Section: "ipFiltering", Control: ControlSwitch},
		{Key: "ip_filter_path", Tab: TabConnection, Section: "ipFiltering", Control: ControlPath, DependsOn: "ip_filter_enabled"},
		{Key: "ip_filter_trackers", Tab: TabConnection, Section: "ipFiltering", Control: ControlSwitch, DependsOn: "ip_filter_enabled"},
		{Key: "banned_IPs", Tab: TabConnection, Section: "ipFiltering", Control: ControlList},
	},
	// qBittorrent 4.4.0 deprecates random_port. There, true sets the listening port to 0, the same as a
	// listen_port of 0. On 4.3.x it is a separate setting (a new port on each startup), which qui does not show.
	notShown(TabConnection, "deprecated by qBittorrent", "random_port"),
)
