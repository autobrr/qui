/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// i18n-namespace: settings

/** Nav sections in display order. A group without a label renders its tabs without a heading. */
export const SETTINGS_NAV_GROUPS = [
  {
    labelKey: "nav.general",
    tabs: [
      { tab: "application", labelKey: "tabs.application" },
      { tab: "datetime", labelKey: "tabs.dateTime" },
      { tab: "themes", labelKey: "tabs.premiumThemes" },
    ],
  },
  {
    labelKey: "nav.connections",
    tabs: [
      { tab: "instances", labelKey: "tabs.instances" },
      { tab: "indexers", labelKey: "tabs.indexers" },
      { tab: "search-cache", labelKey: "tabs.searchCache" },
      { tab: "integrations", labelKey: "tabs.integrations" },
      { tab: "notifications", labelKey: "tabs.notifications" },
      { tab: "external-programs", labelKey: "tabs.externalPrograms" },
    ],
  },
  {
    labelKey: "nav.access",
    tabs: [
      { tab: "client-api", labelKey: "tabs.clientProxy" },
      { tab: "api", labelKey: "tabs.apiKeys" },
      { tab: "security", labelKey: "tabs.security" },
    ],
  },
  { tabs: [{ tab: "logs", labelKey: "tabs.logs" }] },
] as const

export type SettingsTab = (typeof SETTINGS_NAV_GROUPS)[number]["tabs"][number]["tab"]

/** Tab values as they appear in the URL search parameter. The cast gives z.enum the tuple it needs. */
export const SETTINGS_TABS = SETTINGS_NAV_GROUPS.flatMap(group => group.tabs.map(entry => entry.tab)) as [SettingsTab, ...SettingsTab[]]

export const DEFAULT_SETTINGS_TAB: SettingsTab = "application"
