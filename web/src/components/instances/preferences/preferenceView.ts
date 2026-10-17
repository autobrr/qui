/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { api } from "@/lib/api"
import type { PreferenceField } from "@/types"
import { useQuery } from "@tanstack/react-query"

import { preferenceTabLabels } from "./labels"

type PreferenceTab = PreferenceField["tab"]

export const preferenceViewKey = (instanceId: number) => ["instance-preference-view", instanceId] as const

export function usePreferenceView(instanceId: number) {
  return useQuery({
    queryKey: preferenceViewKey(instanceId),
    queryFn: () => api.getInstancePreferenceView(instanceId),
    // The cache goes when the dialog closes, so a reopen never shows an old view, also when its fetch fails.
    gcTime: 0,
  })
}

export function labelsOf(field: PreferenceField) {
  return preferenceTabLabels[field.tab].fields[field.key]
}

// The qBittorrent tabs that have a field to show, in the order of the qBittorrent WebUI.
export function preferenceTabsOf(fields: PreferenceField[]): PreferenceTab[] {
  return (Object.keys(preferenceTabLabels) as PreferenceTab[]).filter(tab =>
    fields.some(field => field.tab === tab && labelsOf(field))
  )
}
