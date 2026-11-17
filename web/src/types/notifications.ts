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
  /** Null when the database holds no list, which only a hand edit of the row makes: it gets each event except the system notice. */
  eventTypes: string[] | null
  createdAt: string
  updatedAt: string
}

export interface NotificationTargetRequest {
  name: string
  url: string
  enabled: boolean
  /** Left out, the server keeps the list that the target has. */
  eventTypes?: string[]
}

export interface NotificationTestRequest {
  title?: string
  message?: string
}
