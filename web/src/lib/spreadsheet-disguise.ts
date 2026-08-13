/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// Spreadsheet disguise: everything that makes the Spreadsheet theme read as a
// spreadsheet app instead of a torrent client. Activation is purely the
// data-theme attribute that applyTheme() stamps on <html>.

import { useSyncExternalStore } from "react"

export const SPREADSHEET_THEME_ID = "spreadsheet"
export const SPREADSHEET_DOCUMENT_TITLE = "Book1.xlsx"

export function isSpreadsheetDisguiseActive(): boolean {
  return typeof document !== "undefined" &&
    document.documentElement.getAttribute("data-theme") === SPREADSHEET_THEME_ID
}

function subscribe(callback: () => void): () => void {
  window.addEventListener("themechange", callback)
  return () => window.removeEventListener("themechange", callback)
}

export function useSpreadsheetDisguise(): boolean {
  return useSyncExternalStore(subscribe, isSpreadsheetDisguiseActive)
}

// Passerby-visible strings only (nav, table headers, filter pane, status bar,
// page titles). Dialogs, settings, and toasts keep their real names: renaming
// mid-interaction text makes the app confusing without helping the disguise.
// Values are English on purpose regardless of UI locale.
const STRING_OVERRIDES: Record<string, string> = {
  "common:nav.dashboard": "Overview",
  "common:nav.search": "Lookup",
  "common:nav.crossSeed": "Reconcile",
  "common:nav.instances": "Workbooks",
  "common:sidebar.instances": "Workbooks",
  "common:header.searchTorrents": "Find in sheet ({{shortcutKey}})",
  "common:header.globPattern": "Find pattern",
  "dashboard:title": "Overview",
  "torrents:tableColumns.seeds": "Sources",
  "torrents:tableColumns.peers": "Links",
  "torrents:tableColumns.downSpeed": "Rate In",
  "torrents:tableColumns.upSpeed": "Rate Out",
  "torrents:tableColumns.eta": "Due",
  "torrents:tableColumns.ratio": "Yield",
  "torrents:tableColumns.tracker": "Source",
  "torrents:tableColumns.trackerUrl": "Source URL",
  "torrents:tableColumns.trackerIcon": "Source Icon",
  "torrents:tableColumns.addedOn": "Created",
  "torrents:tableColumns.completedOn": "Finalized",
  "torrents:statusBar.torrentsLoaded": "{{loaded}} of {{total}} records loaded",
  "torrents:filterSidebar.trackers": "Sources",
  "torrents:filterSidebar.states.downloading": "Receiving",
  "torrents:filterSidebar.states.uploading": "Sharing",
  "torrents:filterSidebar.states.stalled": "Idle",
  "torrents:filterSidebar.states.stalledUp": "Idle (Out)",
  "torrents:filterSidebar.states.stalledDown": "Idle (In)",
  "torrents:filterSidebar.states.unregistered": "Unlinked",
  "torrents:filterSidebar.states.trackerDown": "Source Offline",
  "torrents:filterSidebar.states.trackerError": "Source Error",
  "torrents:filterSidebar.states.crossSeeds": "Duplicates",
  "torrents:columnFilter.states.downloading": "Receiving",
  "torrents:columnFilter.states.uploading": "Sharing",
  "torrents:columnFilter.states.stalled": "Idle",
  "torrents:columnFilter.states.stalledUp": "Idle (Out)",
  "torrents:columnFilter.states.stalledDown": "Idle (In)",
  "torrents:stateLabels.downloading": "Receiving",
  "torrents:stateLabels.uploading": "Sharing",
  "torrents:stateLabels.stalledDL": "Idle",
  "torrents:stateLabels.stalledUP": "Sharing",
  "torrents:stateLabels.forcedDL": "(F) Receiving",
  "torrents:stateLabels.forcedUP": "(F) Sharing",
  "torrents:stateLabels.metaDL": "Fetching Info",
}

function interpolate(template: string, options: Record<string, unknown>): string {
  return template.replace(/\{\{(\w+)\}\}/g, (match, name: string) => {
    const value = options[name]
    return value === undefined || value === null ? match : String(value)
  })
}

interface PostProcessOptions {
  ns?: string | readonly string[]
  [key: string]: unknown
}

export const spreadsheetPostProcessor = {
  type: "postProcessor" as const,
  name: "spreadsheetDisguise",
  process(value: string, key: string | string[], options: PostProcessOptions): string {
    if (!isSpreadsheetDisguiseActive()) {
      return value
    }
    const keys = Array.isArray(key) ? key : [key]
    const namespaces = Array.isArray(options.ns) ? options.ns : [options.ns ?? "common"]
    for (const ns of namespaces) {
      for (const singleKey of keys) {
        const override = STRING_OVERRIDES[`${ns}:${singleKey}`]
        if (override !== undefined) {
          return interpolate(override, options)
        }
      }
    }
    return value
  },
}
