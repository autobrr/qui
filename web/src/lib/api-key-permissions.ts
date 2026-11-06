/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { API_KEY_PERMISSION_AREAS } from "./api-key-permissions.generated"

// i18n-namespace: settings

export type PermissionArea = (typeof API_KEY_PERMISSION_AREAS)[number]["area"]
export type PermissionLevel = (typeof API_KEY_PERMISSION_AREAS)[number]["levels"][number]

export interface ApiKeyPermission {
  area: PermissionArea
  level: PermissionLevel
}

export const PERMISSION_AREAS: readonly { area: PermissionArea; levels: readonly PermissionLevel[] }[] = API_KEY_PERMISSION_AREAS

export const PERMISSION_LEVELS = ["read", "run", "write"] as const satisfies readonly PermissionLevel[]

export const PERMISSION_LEVEL_LABELS = {
  read: { labelKey: "apiKeys.permissions.levels.read" },
  run: { labelKey: "apiKeys.permissions.levels.run" },
  write: { labelKey: "apiKeys.permissions.levels.write" },
} as const satisfies Record<PermissionLevel, { labelKey: string }>

// The type check fails when the Go table adds an area.
export const PERMISSION_AREA_LABELS = {
  instances: { labelKey: "apiKeys.permissions.areas.instances" },
  torrents: { labelKey: "apiKeys.permissions.areas.torrents" },
  crossSeed: { labelKey: "apiKeys.permissions.areas.crossSeed" },
  dirScan: { labelKey: "apiKeys.permissions.areas.dirScan" },
  automations: { labelKey: "apiKeys.permissions.areas.automations" },
  rss: { labelKey: "apiKeys.permissions.areas.rss" },
  backups: { labelKey: "apiKeys.permissions.areas.backups" },
  orphanScan: { labelKey: "apiKeys.permissions.areas.orphanScan" },
  externalPrograms: { labelKey: "apiKeys.permissions.areas.externalPrograms" },
  indexers: { labelKey: "apiKeys.permissions.areas.indexers" },
  arr: { labelKey: "apiKeys.permissions.areas.arr" },
  notifications: { labelKey: "apiKeys.permissions.areas.notifications" },
  settings: { labelKey: "apiKeys.permissions.areas.settings" },
  logs: { labelKey: "apiKeys.permissions.areas.logs" },
  system: { labelKey: "apiKeys.permissions.areas.system" },
} as const satisfies Record<PermissionArea, { labelKey: string }>

/** The routes of these areas refuse a key with an Instance limit, so a limited key gets nothing from them. */
export function isAreaRefusedByInstanceLimit(area: PermissionArea, instanceIds: readonly number[]) {
  return instanceIds.length > 0 && (area === "crossSeed" || area === "dirScan")
}

export function holdsLevel(permissions: readonly ApiKeyPermission[], area: PermissionArea, level: PermissionLevel) {
  return permissions.some(p => p.area === area && p.level === level)
}

/** Write includes read and run, so a pick of write replaces the other levels of its area. */
export function togglePermission(permissions: readonly ApiKeyPermission[], area: PermissionArea, level: PermissionLevel) {
  if (holdsLevel(permissions, area, level)) {
    return permissions.filter(p => p.area !== area || p.level !== level)
  }
  return [...permissions.filter(p => level !== "write" || p.area !== area), { area, level }]
}

/** The list to send, in table order. A greyed-out row stays picked in the dialog but is not saved. */
export function permissionsToSave(permissions: readonly ApiKeyPermission[], instanceIds: readonly number[]) {
  return PERMISSION_AREAS
    .filter(({ area }) => !isAreaRefusedByInstanceLimit(area, instanceIds))
    .flatMap(({ area, levels }) => levels.filter(level => holdsLevel(permissions, area, level)).map(level => ({ area, level })))
}
