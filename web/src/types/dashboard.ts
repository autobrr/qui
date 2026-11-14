/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

export interface TrackerCustomization {
  id: number
  displayName: string
  domains: string[]
  createdAt: string
  updatedAt: string
}

export interface TrackerCustomizationInput {
  displayName: string
  domains: string[]
}

export interface DashboardSettings {
  id: number
  userId: number
  sectionVisibility: Record<string, boolean>
  sectionOrder: string[]
  sectionCollapsed: Record<string, boolean>
  createdAt: string
  updatedAt: string
}

export interface DashboardSettingsInput {
  sectionVisibility?: Record<string, boolean>
  sectionOrder?: string[]
  sectionCollapsed?: Record<string, boolean>
}

export interface LogExclusions {
  id: number
  patterns: string[]
  createdAt: string
  updatedAt: string
}

export interface LogExclusionsInput {
  patterns: string[]
}
