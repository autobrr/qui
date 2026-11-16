/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"

import { normalizeNotificationUrl } from "@/components/settings/notification-targets"

describe("normalizeNotificationUrl", () => {
  it.each([
    ["https://discord.com/api/webhooks/123/token", "discord://token@123"],
    ["https://canary.discord.com/api/webhooks/123/token?thread_id=9", "discord://token@123?thread_id=9"],
    ["https://discordapp.com/api/webhooks/123/token", "discord://token@123"],
    ["https://notdiscord.com/api/webhooks/123/token", "https://notdiscord.com/api/webhooks/123/token"],
    ["https://hooks.notdiscordapp.com/api/webhooks/123/token", "https://hooks.notdiscordapp.com/api/webhooks/123/token"],
  ])("turns %s into %s", (input, expected) => {
    expect(normalizeNotificationUrl(input)).toBe(expected)
  })
})
