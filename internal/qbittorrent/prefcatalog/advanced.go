// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package prefcatalog

import "slices"

var (
	libtorrent1Only = Gate{BelowLibtorrent: "2"}
	libtorrent2Only = Gate{MinLibtorrent: "2"}
)

var advancedEntries = slices.Concat(
	[]Entry{
		// The memory working set limit has an effect only on libtorrent 2, and not on Linux or macOS.
		{Key: "memory_working_set_limit", Tab: TabAdvanced, Section: "qbittorrent", Control: ControlNumber, WireUnit: "MiB", DisplayUnit: "MiB", Gate: Gate{MinLibtorrent: "2", NotPlatforms: []string{"linux", "macos"}}},
		{Key: "current_network_interface", Tab: TabAdvanced, Section: "qbittorrent", Control: ControlText, Mode: ReadOnly, Reason: "pending network binding PR"},
		{Key: "current_interface_address", Tab: TabAdvanced, Section: "qbittorrent", Control: ControlText, Mode: ReadOnly, Reason: "pending network binding PR"},
		{Key: "resolve_peer_countries", Tab: TabAdvanced, Section: "qbittorrent", Control: ControlSwitch},
		{Key: "reannounce_when_address_changed", Tab: TabAdvanced, Section: "qbittorrent", Control: ControlSwitch},
		{Key: "async_io_threads", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber},
		{Key: "hashing_threads", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber, Gate: libtorrent2Only},
		{Key: "file_pool_size", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber},
		{Key: "checking_memory_use", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber, WireUnit: "MiB", DisplayUnit: "MiB"},
		{Key: "disk_cache", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber, WireUnit: "MiB", DisplayUnit: "MiB", Gate: libtorrent1Only},
		{Key: "disk_cache_ttl", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber, WireUnit: "s", DisplayUnit: "s", Gate: libtorrent1Only},
		{Key: "disk_queue_size", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber, WireUnit: "B", DisplayUnit: "KiB"},
		{Key: "disk_io_type", Tab: TabAdvanced, Section: "libtorrent", Control: ControlSelect, OptionSource: SourceFixed, Gate: libtorrent2Only, Options: []Option{
			{Value: 0}, {Value: 1}, {Value: 2}, {Value: 3}, {Value: 4, Gate: Gate{MinLibtorrent: "2.1"}},
		}},
		{Key: "disk_io_read_mode", Tab: TabAdvanced, Section: "libtorrent", Control: ControlSelect, OptionSource: SourceFixed, Options: fixedOptions(0, 1)},
		{Key: "disk_io_write_mode", Tab: TabAdvanced, Section: "libtorrent", Control: ControlSelect, OptionSource: SourceFixed, Options: []Option{
			{Value: 0}, {Value: 1}, {Value: 2, Gate: Gate{MinLibtorrent: "2.0.6"}},
		}},
		{Key: "enable_coalesce_read_write", Tab: TabAdvanced, Section: "libtorrent", Control: ControlSwitch, Gate: libtorrent1Only},
		{Key: "send_buffer_watermark", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber, WireUnit: "KiB", DisplayUnit: "KiB"},
		{Key: "send_buffer_low_watermark", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber, WireUnit: "KiB", DisplayUnit: "KiB"},
		{Key: "send_buffer_watermark_factor", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber, WireUnit: "%", DisplayUnit: "%"},
		{Key: "socket_backlog_size", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber},
		{Key: "outgoing_ports_min", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber},
		{Key: "outgoing_ports_max", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber},
		{Key: "upnp_lease_duration", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber, WireUnit: "s", DisplayUnit: "s"},
		{Key: "peer_tos", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber},
		{Key: "utp_tcp_mixed_mode", Tab: TabAdvanced, Section: "libtorrent", Control: ControlSelect, OptionSource: SourceFixed, Options: fixedOptions(0, 1)},
		{Key: "enable_multi_connections_from_same_ip", Tab: TabAdvanced, Section: "libtorrent", Control: ControlSwitch},
		{Key: "block_peers_on_privileged_ports", Tab: TabAdvanced, Section: "libtorrent", Control: ControlSwitch},
		{Key: "announce_to_all_trackers", Tab: TabAdvanced, Section: "libtorrent", Control: ControlSwitch},
		{Key: "announce_to_all_tiers", Tab: TabAdvanced, Section: "libtorrent", Control: ControlSwitch},
		{Key: "announce_ip", Tab: TabAdvanced, Section: "libtorrent", Control: ControlText},
		{Key: "max_concurrent_http_announces", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber},
		{Key: "stop_tracker_timeout", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber, WireUnit: "s", DisplayUnit: "s"},
		{Key: "peer_turnover", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber, WireUnit: "%", DisplayUnit: "%"},
		{Key: "peer_turnover_cutoff", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber, WireUnit: "%", DisplayUnit: "%"},
		{Key: "peer_turnover_interval", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber, WireUnit: "s", DisplayUnit: "s"},
		{Key: "request_queue_size", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber},
	},
	notShown(TabAdvanced, reasonWebUIOnly,
		"refresh_interval", "app_instance_name", "confirm_torrent_recheck", "python_executable_path", "resolve_peer_host_names"),
	notShown(TabAdvanced, "pending network binding PR", "current_interface_name"),
	notShown(TabAdvanced, "pending Advanced PR",
		"resume_data_storage_type", "torrent_content_remove_option", "save_resume_data_interval", "save_statistics_interval",
		"torrent_file_size_limit", "recheck_completed_torrents", "enable_embedded_tracker", "embedded_tracker_port",
		"embedded_tracker_port_forwarding", "mark_of_the_web", "ignore_ssl_errors", "start_paused", "shutdown_timeout",
		"bdecode_depth_limit", "bdecode_token_limit", "enable_os_cache", "enable_piece_extent_affinity",
		"enable_upload_suggestions", "connection_speed", "seeding_outgoing_connections",
		"socket_send_buffer_size", "socket_receive_buffer_size", "hostname_cache_ttl", "idn_support_enabled",
		"enable_multi_connections_from_same_peer_id", "validate_https_tracker_certificate", "ssrf_mitigation",
		"upload_slots_behavior", "upload_choking_algorithm", "announce_port", "max_outstanding_block_requests",
		"dht_bootstrap_nodes", "webtorrent_stun_server",
		"i2p_inbound_quantity", "i2p_outbound_quantity", "i2p_inbound_length", "i2p_outbound_length",
		"i2p_inbound_length_variance", "i2p_outbound_length_variance"),
)
