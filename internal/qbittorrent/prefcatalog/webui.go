// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package prefcatalog

import "slices"

var webUIEntries = slices.Concat(
	notShown(TabWebUI, "qui does not send the qBittorrent API key to the browser", "web_ui_api_key"),
	notShown(TabWebUI, reasonWebUIOnly,
		"alternative_webui_enabled", "alternative_webui_path",
		"web_ui_use_custom_http_headers_enabled", "web_ui_custom_http_headers",
		"dyndns_enabled", "dyndns_service", "dyndns_domain", "dyndns_username", "dyndns_password"),
	notShown(TabWebUI, "pending WebUI PR",
		"web_ui_address", "web_ui_port", "web_ui_upnp", "use_https", "web_ui_https_cert_path", "web_ui_https_key_path",
		"web_ui_username", "bypass_local_auth", "bypass_auth_subnet_whitelist_enabled", "bypass_auth_subnet_whitelist",
		"web_ui_max_auth_fail_count", "web_ui_ban_duration", "web_ui_session_timeout", "web_ui_sessions_count_limit",
		"web_ui_clickjacking_protection_enabled", "web_ui_csrf_protection_enabled", "web_ui_secure_cookie_enabled",
		"web_ui_host_header_validation_enabled", "web_ui_domain_list",
		"web_ui_reverse_proxy_enabled", "web_ui_reverse_proxies_list"),
)
