/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { AUTOMATIONS_NAV_GROUPS, DEFAULT_AUTOMATIONS_TAB } from "@/components/automations/tabs"
import { CROSS_SEED_NAV_GROUPS, DEFAULT_CROSS_SEED_TAB } from "@/components/cross-seed/tabs"
import { NAV_ITEMS } from "@/components/layout/navigation"
import { DEFAULT_SETTINGS_TAB, SETTINGS_NAV_GROUPS } from "@/components/settings/tabs"
import { useLayoutRoute } from "@/contexts/LayoutRouteContext"
import { useInstances } from "@/hooks/useInstances"
import { isDemo } from "@/lib/demo"
import { useIncognitoMode } from "@/lib/incognito"
import { ALL_INSTANCES_ID } from "@/lib/instances"
import { navigateWithSearch } from "@/lib/router-search"
import { useLocation, useNavigate } from "@tanstack/react-router"
import { EyeOff, GitBranch, HardDrive, Plus, Rss, Settings, Zap } from "lucide-react"
import { useTranslation } from "react-i18next"
import type { PaletteEntry, PaletteGroup } from "./palette"
import { useThemeEntry } from "./useThemeEntry"

/** The instances, pages and actions that the palette opens. `close` shuts the palette. */
export function useDestinations(close: () => void): PaletteGroup[] {
  const { t } = useTranslation(["common", "settings", "crossseed", "rss", "dashboard", "search", "automations", "instances", "torrents"])
  const navigate = useNavigate()
  const location = useLocation()
  const { instances } = useInstances()
  const { state: layoutRoute } = useLayoutRoute()
  const [incognito, setIncognito] = useIncognitoMode()
  const themeEntry = useThemeEntry(close)

  const pathname = location.pathname
  const routeSearch = location.search as Record<string, unknown>
  const tab = typeof routeSearch.tab === "string" ? routeSearch.tab : undefined
  const go = (action: () => void) => () => {
    close()
    action()
  }

  // The header of a torrent list of one instance has the Add torrent button, and this id is that instance.
  const listInstanceId = layoutRoute.showInstanceControls && layoutRoute.instanceId !== null && layoutRoute.instanceId > 0 ? layoutRoute.instanceId : null
  // The list that is open keeps its search under the dialog.
  const addTorrent = (instanceId: number) => go(() => instanceId === listInstanceId
    ? navigateWithSearch({ navigate, search: { ...routeSearch, modal: "add-torrent" } })
    : navigate({ to: "/instances/$instanceId", params: { instanceId: String(instanceId) }, search: { modal: "add-torrent" } }))

  const activeInstances = (instances ?? []).filter(instance => instance.isActive)
  const instanceEntries: PaletteEntry[] = activeInstances.map((instance, index) => {
    const path = `/instances/${instance.id}`
    const params = { instanceId: String(instance.id) }
    const open = go(() => navigate({ to: "/instances/$instanceId", params }))
    return {
      id: `instance.${instance.id}`,
      label: instance.name,
      icon: { instanceId: instance.id, problem: !instance.connected },
      note: instance.connected ? t("common:header.connected") : t("common:header.disconnected"),
      current: pathname === path || pathname.startsWith(`${path}/`),
      shortcut: index < 9 ? String(index + 1) : undefined,
      sections: [{
        id: `instance.${instance.id}.actions`,
        entries: [
          { id: `instance.${instance.id}.open`, label: t("torrents:page.routeTitle"), icon: HardDrive, trail: instance.name, ownListOnly: true, run: open },
          {
            id: `instance.${instance.id}.add`,
            label: t("common:header.addTorrent"),
            icon: Plus,
            trail: instance.name,
            // A search already finds the Add torrent action of the list that is open.
            ownListOnly: instance.id === listInstanceId,
            run: addTorrent(instance.id),
          },
        ],
      }],
      run: open,
    }
  })
  if (activeInstances.length > 1) {
    instanceEntries.unshift({
      id: "instance.unified",
      label: t("common:unifiedScope.unified"),
      icon: { instanceId: ALL_INSTANCES_ID },
      current: pathname === "/instances" || pathname === "/instances/",
      shortcut: "0",
      run: go(() => navigate({ to: "/instances" })),
    })
  }

  const settingsSections: PaletteGroup[] = SETTINGS_NAV_GROUPS.map((group, index) => {
    const groupLabel = "labelKey" in group ? t(`settings:${group.labelKey}`) : undefined
    return {
      id: `settings.group.${index}`,
      label: groupLabel,
      entries: group.tabs.map(entry => ({
        id: `settings.${entry.tab}`,
        label: t(`settings:${entry.labelKey}`),
        icon: Settings,
        trail: [t("common:nav.settings"), groupLabel].filter(Boolean).join(" › "),
        current: pathname === "/settings" && (tab ?? DEFAULT_SETTINGS_TAB) === entry.tab,
        // The Logs page of the sidebar is this section.
        ownListOnly: entry.tab === "logs",
        run: go(() => navigate({ to: "/settings", search: { tab: entry.tab } })),
      })),
    }
  })

  const crossSeedSections: PaletteGroup[] = CROSS_SEED_NAV_GROUPS.map((group, index) => {
    const groupLabel = "labelKey" in group ? t(`crossseed:${group.labelKey}`) : undefined
    return {
      id: `cross-seed.group.${index}`,
      label: groupLabel,
      entries: group.tabs.map(section => ({
        id: `cross-seed.${section}`,
        label: t(`crossseed:tabs.${section}`),
        icon: GitBranch,
        trail: [t("common:nav.crossSeed"), groupLabel].filter(Boolean).join(" › "),
        current: pathname === "/cross-seed" && (tab ?? DEFAULT_CROSS_SEED_TAB) === section,
        run: go(() => navigate({ to: "/cross-seed", search: { tab: section } })),
      })),
    }
  })

  const automationsSections: PaletteGroup[] = AUTOMATIONS_NAV_GROUPS.map((group, index) => ({
    id: `automations.group.${index}`,
    entries: group.tabs.map(entry => ({
      id: `automations.${entry.tab}`,
      label: t(`automations:${entry.labelKey}`),
      icon: Zap,
      trail: t("common:nav.automations"),
      current: pathname === "/automations" && (tab ?? DEFAULT_AUTOMATIONS_TAB) === entry.tab,
      run: go(() => navigate({ to: "/automations", search: { tab: entry.tab } })),
    })),
  }))

  const rssSections: PaletteGroup[] = [{
    id: "rss.group",
    entries: (["feeds", "rules"] as const).map(section => ({
      id: `rss.${section}`,
      label: t(`rss:tabs.${section}`),
      icon: Rss,
      trail: t("common:nav.rss"),
      current: pathname === "/rss" && (tab ?? "feeds") === section,
      run: go(() => navigate({ to: "/rss", search: { tab: section } })),
    })),
  }]

  const pageNotes: Record<string, string> = {
    dashboard: t("dashboard:description"),
    statistics: t("common:commandPalette.statisticsNote"),
    search: t("search:pageDescription"),
    "cross-seed": t("crossseed:pageDescription"),
    automations: t("automations:page.description"),
    transfers: t("transfers:page.description"),
    backups: t("instances:backups.pageDescription"),
    rss: t("rss:pageDescription"),
    settings: t("settings:description"),
    logs: t("settings:logs.description"),
  }
  const pageSections: Record<string, PaletteGroup[]> = {
    "cross-seed": crossSeedSections,
    automations: automationsSections,
    rss: rssSections,
    settings: settingsSections,
  }
  // Only the torrent lists run in the demo.
  const pageEntries: PaletteEntry[] = isDemo ? [] : NAV_ITEMS.map(item => ({
    id: `page.${item.id}`,
    label: t(`common:${item.titleKey}`),
    icon: item.icon,
    note: pageNotes[item.id],
    current: item.isActive ? item.isActive(pathname, routeSearch) : pathname === item.href,
    sections: pageSections[item.id],
    run: go(() => navigateWithSearch({ navigate, to: item.href, search: item.search ?? {} })),
  }))

  const actionEntries: PaletteEntry[] = []
  const listInstance = activeInstances.find(instance => instance.id === listInstanceId)
  if (listInstance) {
    actionEntries.push({
      id: "action.add-torrent",
      label: t("common:header.addTorrent"),
      icon: Plus,
      note: listInstance.name,
      run: addTorrent(listInstance.id),
    })
  }
  actionEntries.push(
    {
      id: "action.incognito",
      label: incognito ? t("dashboard:incognito.turnOff") : t("dashboard:incognito.turnOn"),
      icon: EyeOff,
      run: go(() => setIncognito(!incognito)),
    },
    themeEntry
  )

  return [
    { id: "instances", label: t("common:sidebar.instances"), entries: instanceEntries },
    { id: "pages", label: t("common:commandPalette.groups.pages"), entries: pageEntries },
    { id: "actions", label: t("common:commandPalette.groups.actions"), entries: actionEntries },
  ].filter(group => group.entries.length > 0)
}
