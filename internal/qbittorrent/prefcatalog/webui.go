// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package prefcatalog

import "slices"

// A change in qui to one of these keys can stop the connection of qui to qBittorrent.
const reasonQuiConnection = "qui connects to qBittorrent with this setting"

var webUIEntries = slices.Concat(
	[]Entry{
		{Key: "web_ui_address", Tab: TabWebUI, Section: "webUI", Control: ControlText, Mode: ReadOnly, Reason: reasonQuiConnection},
		{Key: "web_ui_port", Tab: TabWebUI, Section: "webUI", Control: ControlNumber, Mode: ReadOnly, Reason: reasonQuiConnection},
		{Key: "web_ui_upnp", Tab: TabWebUI, Section: "webUI", Control: ControlSwitch},
		{Key: "use_https", Tab: TabWebUI, Section: "https", Control: ControlSwitch, Mode: ReadOnly, Reason: reasonQuiConnection},
		{Key: "web_ui_https_cert_path", Tab: TabWebUI, Section: "https", Control: ControlPath, DependsOn: "use_https", Mode: ReadOnly, Reason: reasonQuiConnection},
		{Key: "web_ui_https_key_path", Tab: TabWebUI, Section: "https", Control: ControlPath, DependsOn: "use_https", Mode: ReadOnly, Reason: reasonQuiConnection},
		{Key: "web_ui_username", Tab: TabWebUI, Section: "authentication", Control: ControlText, Mode: ReadOnly, Reason: reasonQuiConnection},
		{Key: "bypass_local_auth", Tab: TabWebUI, Section: "authentication", Control: ControlSwitch, Mode: ReadOnly, Reason: reasonQuiConnection},
		{Key: "bypass_auth_subnet_whitelist_enabled", Tab: TabWebUI, Section: "authentication", Control: ControlSwitch, Mode: ReadOnly, Reason: reasonQuiConnection},
		{Key: "bypass_auth_subnet_whitelist", Tab: TabWebUI, Section: "authentication", Control: ControlList, DependsOn: "bypass_auth_subnet_whitelist_enabled", Mode: ReadOnly, Reason: reasonQuiConnection},
		{Key: "web_ui_max_auth_fail_count", Tab: TabWebUI, Section: "authentication", Control: ControlNumber},
		{Key: "web_ui_ban_duration", Tab: TabWebUI, Section: "authentication", Control: ControlNumber, WireUnit: "s", DisplayUnit: "s"},
		{Key: "web_ui_session_timeout", Tab: TabWebUI, Section: "authentication", Control: ControlNumber, WireUnit: "s", DisplayUnit: "s"},
		{Key: "web_ui_sessions_count_limit", Tab: TabWebUI, Section: "authentication", Control: ControlNumber},
		{Key: "web_ui_clickjacking_protection_enabled", Tab: TabWebUI, Section: "security", Control: ControlSwitch},
		{Key: "web_ui_csrf_protection_enabled", Tab: TabWebUI, Section: "security", Control: ControlSwitch},
		{Key: "web_ui_secure_cookie_enabled", Tab: TabWebUI, Section: "security", Control: ControlSwitch},
		{Key: "web_ui_host_header_validation_enabled", Tab: TabWebUI, Section: "security", Control: ControlSwitch, Mode: ReadOnly, Reason: reasonQuiConnection},
		{Key: "web_ui_domain_list", Tab: TabWebUI, Section: "security", Control: ControlText, DependsOn: "web_ui_host_header_validation_enabled", Mode: ReadOnly, Reason: reasonQuiConnection},
		{Key: "web_ui_reverse_proxy_enabled", Tab: TabWebUI, Section: "reverseProxy", Control: ControlSwitch},
		{Key: "web_ui_reverse_proxies_list", Tab: TabWebUI, Section: "reverseProxy", Control: ControlText, DependsOn: "web_ui_reverse_proxy_enabled"},
	},
	notShown(TabWebUI, "qui does not send the qBittorrent API key to the browser", "web_ui_api_key"),
	notShown(TabWebUI, reasonWebUIOnly,
		"alternative_webui_enabled", "alternative_webui_path",
		"web_ui_use_custom_http_headers_enabled", "web_ui_custom_http_headers",
		"dyndns_enabled", "dyndns_service", "dyndns_domain", "dyndns_username", "dyndns_password"),
)
