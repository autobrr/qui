/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// i18n-namespace: settings

export const SETTINGS_NAV_GROUPS = [
  {
    labelKey: "nav.general",
    tabs: [
      { tab: "application", labelKey: "tabs.application" },
      { tab: "datetime", labelKey: "tabs.dateTime" },
      { tab: "display-profiles", labelKey: "tabs.displayProfiles" },
      { tab: "statistics", labelKey: "tabs.statistics" },
      { tab: "trackers", labelKey: "tabs.trackers" },
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
      { tab: "shares", labelKey: "tabs.shares" },
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

export const SETTINGS_TABS = SETTINGS_NAV_GROUPS.flatMap(group => group.tabs.map(entry => entry.tab)) as [SettingsTab, ...SettingsTab[]]

export const DEFAULT_SETTINGS_TAB: SettingsTab = "application"
