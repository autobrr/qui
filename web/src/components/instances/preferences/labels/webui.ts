/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TabLabels } from "./types"

// i18n-namespace: instances

export const webUILabels: TabLabels = {
  labelKey: "preferences.dialog.tabs.webui",
  sections: {
    webUI: { labelKey: "preferences.sections.webUI" },
    https: { labelKey: "preferences.sections.webUIHttps" },
    authentication: { labelKey: "preferences.sections.webUIAuthentication" },
    security: { labelKey: "preferences.sections.webUISecurity" },
    reverseProxy: { labelKey: "preferences.sections.webUIReverseProxy" },
  },
  fields: {
    web_ui_address: { labelKey: "preferences.fields.webUIAddress" },
    web_ui_port: { labelKey: "preferences.fields.webUIPort" },
    web_ui_upnp: { labelKey: "preferences.fields.webUIUpnp" },
    use_https: { labelKey: "preferences.fields.useHttps" },
    web_ui_https_cert_path: { labelKey: "preferences.fields.webUIHttpsCertPath" },
    web_ui_https_key_path: { labelKey: "preferences.fields.webUIHttpsKeyPath" },
    web_ui_username: {
      labelKey: "preferences.fields.webUIUsername",
      readOnlyNoteKey: "preferences.fields.webUIConnectionNote",
    },
    bypass_local_auth: { labelKey: "preferences.fields.bypassLocalAuth" },
    bypass_auth_subnet_whitelist_enabled: { labelKey: "preferences.fields.bypassAuthSubnetWhitelistEnabled" },
    bypass_auth_subnet_whitelist: { labelKey: "preferences.fields.bypassAuthSubnetWhitelist" },
    web_ui_max_auth_fail_count: {
      labelKey: "preferences.fields.webUIMaxAuthFailCount",
      descriptionKey: "preferences.fields.webUIMaxAuthFailCountDescription",
    },
    web_ui_ban_duration: { labelKey: "preferences.fields.webUIBanDuration" },
    web_ui_session_timeout: {
      labelKey: "preferences.fields.webUISessionTimeout",
      descriptionKey: "preferences.fields.webUISessionTimeoutDescription",
    },
    web_ui_sessions_count_limit: { labelKey: "preferences.fields.webUISessionsCountLimit", unlimited: 0 },
    web_ui_clickjacking_protection_enabled: { labelKey: "preferences.fields.webUIClickjackingProtection" },
    web_ui_csrf_protection_enabled: { labelKey: "preferences.fields.webUICsrfProtection" },
    web_ui_secure_cookie_enabled: { labelKey: "preferences.fields.webUISecureCookie" },
    web_ui_host_header_validation_enabled: { labelKey: "preferences.fields.webUIHostHeaderValidation" },
    web_ui_domain_list: {
      labelKey: "preferences.fields.webUIDomainList",
      descriptionKey: "preferences.fields.webUIDomainListDescription",
    },
    web_ui_reverse_proxy_enabled: { labelKey: "preferences.fields.webUIReverseProxyEnabled" },
    web_ui_reverse_proxies_list: {
      labelKey: "preferences.fields.webUIReverseProxiesList",
      descriptionKey: "preferences.fields.webUIReverseProxiesListDescription",
    },
  },
}
