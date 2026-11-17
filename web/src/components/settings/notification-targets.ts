/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { NotificationEventDefinition } from "@/types"

export type NotificationEventGroupId = "torrent" | "maintenance" | "crossSeed" | "automations" | "other"

export interface NotificationEventGroup {
  id: NotificationEventGroupId
  events: NotificationEventDefinition[]
}

const GROUP_ORDER: readonly NotificationEventGroupId[] = ["torrent", "maintenance", "crossSeed", "automations", "other"]

const MAINTENANCE_EVENTS = new Set([
  "backup_succeeded",
  "backup_failed",
  "dir_scan_completed",
  "dir_scan_failed",
  "orphan_scan_completed",
  "orphan_scan_failed",
])

function groupOf(type: string): NotificationEventGroupId {
  if (type.startsWith("torrent_")) return "torrent"
  if (MAINTENANCE_EVENTS.has(type)) return "maintenance"
  if (type.startsWith("cross_seed_")) return "crossSeed"
  if (type.startsWith("automations_")) return "automations"
  return "other"
}

/** The events in their groups, in the order of the form. A group without events is left out. */
export function groupNotificationEvents(definitions: readonly NotificationEventDefinition[]): NotificationEventGroup[] {
  const groups = new Map<NotificationEventGroupId, NotificationEventDefinition[]>()
  for (const event of definitions) {
    const id = groupOf(event.type)
    groups.set(id, [...groups.get(id) ?? [], event])
  }
  return GROUP_ORDER.flatMap((id) => {
    const events = groups.get(id)
    return events ? [{ id, events }] : []
  })
}

const REDACTED = "••••"

/** The URL without its secrets: the scheme and the host stay, the credentials and the path become dots. */
export function maskNotificationUrl(rawUrl: string): string {
  const trimmed = rawUrl.trim()
  if (!trimmed) return ""
  try {
    const parsed = new URL(trimmed)
    const scheme = parsed.protocol.replace(":", "")
    if (!scheme) return REDACTED
    if (scheme === "notifiarr" || scheme === "notifiarrapi") return `${scheme}://${REDACTED}`

    const hasUserInfo = parsed.username !== "" || parsed.password !== ""
    const path = parsed.pathname && parsed.pathname !== "/" ? `/${REDACTED}` : ""
    if (hasUserInfo) return `${scheme}://${REDACTED}@${parsed.host}${path}`
    if (parsed.host) return `${scheme}://${parsed.host}${path}`
    return `${scheme}://${REDACTED}`
  } catch {
    return REDACTED
  }
}

const DISCORD_HOSTS = ["discord.com", "discordapp.com"]

function convertDiscordWebhookUrl(rawUrl: string): string | null {
  let parsed: URL
  try {
    parsed = new URL(rawUrl)
  } catch {
    return null
  }

  const protocol = parsed.protocol.toLowerCase()
  if (protocol !== "https:" && protocol !== "http:") return null

  const host = parsed.hostname.toLowerCase()
  if (!DISCORD_HOSTS.some((domain) => host === domain || host.endsWith(`.${domain}`))) return null

  const parts = parsed.pathname.split("/").filter(Boolean)
  const [apiPrefix, webhookPrefix, webhookId, token] = parts
  if (parts.length < 4 || apiPrefix !== "api" || webhookPrefix !== "webhooks" || !webhookId || !token) return null

  const threadId = parsed.searchParams.get("thread_id")
  const suffix = threadId ? `?thread_id=${encodeURIComponent(threadId)}` : ""
  return `discord://${token}@${webhookId}${suffix}`
}

/** A Discord webhook URL becomes the Shoutrrr discord:// URL. Each other URL stays as it is. */
export function normalizeNotificationUrl(rawUrl: string): string {
  const trimmed = rawUrl.trim()
  if (!trimmed || trimmed.startsWith("discord://")) return rawUrl
  return convertDiscordWebhookUrl(trimmed) ?? rawUrl
}
