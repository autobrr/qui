/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

export const THEME_AREAS = [
  { name: "app-header", purpose: "The bar at the top of each page, with the page title, the search field, and the menus." },
  { name: "app-sidebar", purpose: "The navigation sidebar at the left of the desktop layout." },
  { name: "mobile-nav", purpose: "The navigation bar at the bottom of the mobile layout." },
  { name: "filter-sidebar", purpose: "The torrent filter panel with status, category, tag, and tracker filters." },
  { name: "torrent-table", purpose: "The torrent list table of the desktop layout, with its header row." },
  { name: "torrent-row", purpose: "One row of the torrent table. Do not change its height." },
  { name: "torrent-card", purpose: "One torrent card in the torrent list of the mobile layout." },
  { name: "details-panel", purpose: "The panel that shows the details of the selected torrent." },
  { name: "dashboard-card", purpose: "One card on the dashboard, for example the chart or the list of instances." },
  { name: "login", purpose: "The login page." },
] as const

export type ThemeAreaName = (typeof THEME_AREAS)[number]["name"]

type ThemeAreaProps = Readonly<{ "data-qui-area": ThemeAreaName }>

const AREA_PROPS = new Map<ThemeAreaName, ThemeAreaProps>(THEME_AREAS.map(({ name }) => [name, { "data-qui-area": name }]))

export function themeArea(name: ThemeAreaName): ThemeAreaProps {
  return AREA_PROPS.get(name)!
}
