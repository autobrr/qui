/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

export interface NotificationEventDefinition {
  type: string
  label: string
  description: string
}

export interface NotificationTarget {
  id: number
  name: string
  url: string
  enabled: boolean
  eventTypes: string[]
  createdAt: string
  updatedAt: string
}

export interface NotificationTargetRequest {
  name: string
  url: string
  enabled: boolean
  eventTypes: string[]
}

export interface NotificationTestRequest {
  title?: string
  message?: string
}
