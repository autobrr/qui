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
		{Key: "resume_data_storage_type", Tab: TabAdvanced, Section: "qbittorrent", Control: ControlSelect, OptionSource: SourceFixed, Options: fixedOptions("Legacy", "SQLite")},
		{Key: "torrent_content_remove_option", Tab: TabAdvanced, Section: "qbittorrent", Control: ControlSelect, OptionSource: SourceFixed, Options: fixedOptions("Delete", "MoveToTrash")},
		// The memory working set limit has an effect only on libtorrent 2, and not on Linux or macOS.
		{Key: "memory_working_set_limit", Tab: TabAdvanced, Section: "qbittorrent", Control: ControlNumber, WireUnit: "MiB", DisplayUnit: "MiB", Gate: Gate{MinLibtorrent: "2", NotPlatforms: []string{"linux", "macos"}}},
		{Key: "current_network_interface", Tab: TabAdvanced, Section: "qbittorrent", Control: ControlSelect, OptionSource: SourceNetworkInterfaces},
		{Key: "current_interface_address", Tab: TabAdvanced, Section: "qbittorrent", Control: ControlSelect, OptionSource: SourceInterfaceAddresses},
		// qBittorrent 4.6 and later send the readable name of the saved interface.
		{Key: "current_interface_name", Tab: TabAdvanced, Section: "qbittorrent", Control: ControlText, Mode: ReadOnly, Reason: "qBittorrent sets it from the network interface"},
		{Key: "save_resume_data_interval", Tab: TabAdvanced, Section: "qbittorrent", Control: ControlNumber, WireUnit: "min", DisplayUnit: "min"},
		{Key: "save_statistics_interval", Tab: TabAdvanced, Section: "qbittorrent", Control: ControlNumber, WireUnit: "min", DisplayUnit: "min"},
		{Key: "torrent_file_size_limit", Tab: TabAdvanced, Section: "qbittorrent", Control: ControlNumber, WireUnit: "B", DisplayUnit: "MiB"},
		{Key: "recheck_completed_torrents", Tab: TabAdvanced, Section: "qbittorrent", Control: ControlSwitch},
		{Key: "resolve_peer_countries", Tab: TabAdvanced, Section: "qbittorrent", Control: ControlSwitch},
		{Key: "reannounce_when_address_changed", Tab: TabAdvanced, Section: "qbittorrent", Control: ControlSwitch},
		{Key: "enable_embedded_tracker", Tab: TabAdvanced, Section: "qbittorrent", Control: ControlSwitch},
		{Key: "embedded_tracker_port", Tab: TabAdvanced, Section: "qbittorrent", Control: ControlNumber},
		{Key: "embedded_tracker_port_forwarding", Tab: TabAdvanced, Section: "qbittorrent", Control: ControlSwitch},
		// qBittorrent sends the platform "unknown" on a platform that is not Linux, macOS, or Windows.
		{Key: "mark_of_the_web", Tab: TabAdvanced, Section: "qbittorrent", Control: ControlSwitch, Gate: Gate{NotPlatforms: []string{"linux", "unknown"}}},
		{Key: "ignore_ssl_errors", Tab: TabAdvanced, Section: "qbittorrent", Control: ControlSwitch},
		// start_paused pauses the whole session at startup. It is not the setting that adds new torrents stopped.
		{Key: "start_paused", Tab: TabAdvanced, Section: "qbittorrent", Control: ControlSwitch},
		{Key: "shutdown_timeout", Tab: TabAdvanced, Section: "qbittorrent", Control: ControlNumber, WireUnit: "s", DisplayUnit: "s"},
		{Key: "bdecode_depth_limit", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber},
		{Key: "bdecode_token_limit", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber},
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
		// qBittorrent 4.3 and 4.4 send enable_os_cache. Later versions replace it with the disk I/O read and write modes.
		{Key: "enable_os_cache", Tab: TabAdvanced, Section: "libtorrent", Control: ControlSwitch},
		{Key: "enable_coalesce_read_write", Tab: TabAdvanced, Section: "libtorrent", Control: ControlSwitch, Gate: libtorrent1Only},
		{Key: "enable_piece_extent_affinity", Tab: TabAdvanced, Section: "libtorrent", Control: ControlSwitch},
		{Key: "enable_upload_suggestions", Tab: TabAdvanced, Section: "libtorrent", Control: ControlSwitch},
		{Key: "send_buffer_watermark", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber, WireUnit: "KiB", DisplayUnit: "KiB"},
		{Key: "send_buffer_low_watermark", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber, WireUnit: "KiB", DisplayUnit: "KiB"},
		{Key: "send_buffer_watermark_factor", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber, WireUnit: "%", DisplayUnit: "%"},
		{Key: "connection_speed", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber},
		{Key: "seeding_outgoing_connections", Tab: TabAdvanced, Section: "libtorrent", Control: ControlSwitch},
		{Key: "socket_send_buffer_size", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber, WireUnit: "B", DisplayUnit: "KiB"},
		{Key: "socket_receive_buffer_size", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber, WireUnit: "B", DisplayUnit: "KiB"},
		{Key: "socket_backlog_size", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber},
		{Key: "outgoing_ports_min", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber},
		{Key: "outgoing_ports_max", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber},
		{Key: "upnp_lease_duration", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber, WireUnit: "s", DisplayUnit: "s"},
		{Key: "peer_tos", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber},
		{Key: "utp_tcp_mixed_mode", Tab: TabAdvanced, Section: "libtorrent", Control: ControlSelect, OptionSource: SourceFixed, Options: fixedOptions(0, 1)},
		{Key: "hostname_cache_ttl", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber, WireUnit: "s", DisplayUnit: "s"},
		{Key: "idn_support_enabled", Tab: TabAdvanced, Section: "libtorrent", Control: ControlSwitch},
		{Key: "enable_multi_connections_from_same_ip", Tab: TabAdvanced, Section: "libtorrent", Control: ControlSwitch},
		{Key: "enable_multi_connections_from_same_peer_id", Tab: TabAdvanced, Section: "libtorrent", Control: ControlSwitch, Gate: Gate{MinLibtorrent: "2.0.13"}},
		{Key: "validate_https_tracker_certificate", Tab: TabAdvanced, Section: "libtorrent", Control: ControlSwitch},
		{Key: "ssrf_mitigation", Tab: TabAdvanced, Section: "libtorrent", Control: ControlSwitch},
		{Key: "block_peers_on_privileged_ports", Tab: TabAdvanced, Section: "libtorrent", Control: ControlSwitch},
		{Key: "upload_slots_behavior", Tab: TabAdvanced, Section: "libtorrent", Control: ControlSelect, OptionSource: SourceFixed, Options: fixedOptions(0, 1)},
		{Key: "upload_choking_algorithm", Tab: TabAdvanced, Section: "libtorrent", Control: ControlSelect, OptionSource: SourceFixed, Options: fixedOptions(0, 1, 2)},
		{Key: "announce_to_all_trackers", Tab: TabAdvanced, Section: "libtorrent", Control: ControlSwitch},
		{Key: "announce_to_all_tiers", Tab: TabAdvanced, Section: "libtorrent", Control: ControlSwitch},
		{Key: "announce_ip", Tab: TabAdvanced, Section: "libtorrent", Control: ControlText},
		{Key: "announce_port", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber},
		{Key: "max_concurrent_http_announces", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber},
		{Key: "stop_tracker_timeout", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber, WireUnit: "s", DisplayUnit: "s"},
		{Key: "peer_turnover", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber, WireUnit: "%", DisplayUnit: "%"},
		{Key: "peer_turnover_cutoff", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber, WireUnit: "%", DisplayUnit: "%"},
		{Key: "peer_turnover_interval", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber, WireUnit: "s", DisplayUnit: "s"},
		{Key: "request_queue_size", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber},
		{Key: "max_outstanding_block_requests", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber},
		{Key: "dht_bootstrap_nodes", Tab: TabAdvanced, Section: "libtorrent", Control: ControlText},
		{Key: "webtorrent_stun_server", Tab: TabAdvanced, Section: "libtorrent", Control: ControlText, Gate: Gate{MinLibtorrent: "2.1"}},
		{Key: "i2p_inbound_quantity", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber, Gate: libtorrent2Only},
		{Key: "i2p_outbound_quantity", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber, Gate: libtorrent2Only},
		{Key: "i2p_inbound_length", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber, Gate: libtorrent2Only},
		{Key: "i2p_outbound_length", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber, Gate: libtorrent2Only},
		{Key: "i2p_inbound_length_variance", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber, Gate: Gate{MinLibtorrent: "2.0.12"}},
		{Key: "i2p_outbound_length_variance", Tab: TabAdvanced, Section: "libtorrent", Control: ControlNumber, Gate: Gate{MinLibtorrent: "2.0.12"}},
	},
	notShown(TabAdvanced, reasonWebUIOnly,
		"refresh_interval", "app_instance_name", "confirm_torrent_recheck", "python_executable_path", "resolve_peer_host_names"),
)
