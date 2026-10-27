/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"
import {
  buildShareLimitFormState,
  buildSpeedLimitFormState,
  shareLimitsUnresolved,
  shareLimitValues,
  speedLimitsUnresolved,
  speedLimitValues,
  type TorrentLimitSnapshot
} from "./torrentLimitDialogHelpers"

function torrent(fields: Partial<TorrentLimitSnapshot>): TorrentLimitSnapshot {
  return {
    ratio_limit: -2,
    seeding_time_limit: -2,
    inactive_seeding_time_limit: -2,
    max_ratio: -1,
    max_seeding_time: -1,
    max_inactive_seeding_time: -1,
    dl_limit: 0,
    up_limit: 0,
    share_limit_action: "Default",
    share_limits_mode: "Default",
    ...fields,
  }
}

describe("share limit form model", () => {
  it("keeps use global for a torrent that follows the global limits", () => {
    const form = buildShareLimitFormState([torrent({})])

    expect(form.ratio.mode).toBe("global")
    expect(shareLimitValues(form, true, true)).toEqual({
      ratioLimit: -2,
      seedingTimeLimit: -2,
      inactiveSeedingTimeLimit: -2,
      shareLimitAction: undefined,
      shareLimitsMode: undefined,
    })
  })

  it("keeps unlimited and custom values", () => {
    const form = buildShareLimitFormState([
      torrent({ ratio_limit: -1, seeding_time_limit: 720, inactive_seeding_time_limit: -1 }),
    ])

    expect(form.ratio.mode).toBe("unlimited")
    expect(form.seedTime).toMatchObject({ mode: "custom", customValue: 720 })
    expect(shareLimitValues(form, false, false)).toEqual({
      ratioLimit: -1,
      seedingTimeLimit: 720,
      inactiveSeedingTimeLimit: -1,
      shareLimitAction: undefined,
      shareLimitsMode: undefined,
    })
  })

  it.each([
    ["ratio", "ratio_limit", "ratioLimit", 2.5],
    ["seedTime", "seeding_time_limit", "seedingTimeLimit", 720],
    ["inactiveTime", "inactive_seeding_time_limit", "inactiveSeedingTimeLimit", 60],
  ] as const)("keeps a custom %s limit", (field, torrentKey, valueKey, value) => {
    const form = buildShareLimitFormState([torrent({ [torrentKey]: value })])

    expect(form[field]).toMatchObject({ mode: "custom", customValue: value, isMixed: false })
    expect(shareLimitValues(form, false, false)[valueKey]).toBe(value)
  })

  it.each([
    ["ratio", "ratio_limit"],
    ["seedTime", "seeding_time_limit"],
    ["inactiveTime", "inactive_seeding_time_limit"],
  ] as const)("marks the %s limit as mixed and keeps Confirm blocked until the user picks a value", (field, torrentKey) => {
    const form = buildShareLimitFormState([torrent({ [torrentKey]: -2 }), torrent({ [torrentKey]: -1 })])

    expect(form[field].isMixed).toBe(true)
    expect(shareLimitsUnresolved(form, false, false)).toBe(true)

    const picked = { ...form, [field]: { ...form[field], mode: "global", touched: true } }

    expect(shareLimitsUnresolved(picked, false, false)).toBe(false)
    expect(shareLimitValues(picked, false, false)).toMatchObject({
      ratioLimit: -2,
      seedingTimeLimit: -2,
      inactiveSeedingTimeLimit: -2,
    })
  })

  it("blocks Confirm on a mixed action or matching mode only when the instance supports it", () => {
    const form = buildShareLimitFormState([
      torrent({ share_limit_action: "Stop", share_limits_mode: "MatchAny" }),
      torrent({ share_limit_action: "Remove", share_limits_mode: "MatchAll" }),
    ])

    expect(shareLimitsUnresolved(form, true, false)).toBe(true)
    expect(shareLimitsUnresolved(form, false, true)).toBe(true)
    expect(shareLimitsUnresolved(form, false, false)).toBe(false)
  })

  it("rounds a custom ratio to two decimals", () => {
    const form = buildShareLimitFormState([torrent({ ratio_limit: 1.23456 })])

    expect(shareLimitValues(form, false, false).ratioLimit).toBe(1.23)
  })

  it("starts a custom ratio at 1.0 when the torrent has none", () => {
    const form = buildShareLimitFormState([torrent({})])

    expect(form.ratio.customValue).toBe(1)
  })

  it("keeps the action and the matching mode when the instance supports them", () => {
    const form = buildShareLimitFormState([
      torrent({ share_limit_action: "Stop", share_limits_mode: "MatchAll" }),
    ])

    expect(shareLimitValues(form, true, true)).toMatchObject({
      shareLimitAction: "Stop",
      shareLimitsMode: "MatchAll",
    })
    expect(shareLimitValues(form, false, false)).toMatchObject({
      shareLimitAction: undefined,
      shareLimitsMode: undefined,
    })
  })

  it("marks a limit as mixed when the torrents disagree", () => {
    const form = buildShareLimitFormState([
      torrent({ ratio_limit: -2, seeding_time_limit: 60 }),
      torrent({ ratio_limit: 2, seeding_time_limit: 60 }),
    ])

    expect(form.ratio).toMatchObject({ isMixed: true, touched: false })
    expect(form.seedTime).toMatchObject({ isMixed: false, mode: "custom", customValue: 60 })
  })

  it("marks the action and the matching mode as mixed when the torrents disagree", () => {
    const form = buildShareLimitFormState([
      torrent({ share_limit_action: "Stop", share_limits_mode: "MatchAny" }),
      torrent({ share_limit_action: "Remove", share_limits_mode: "" }),
    ])

    expect(form.action.isMixed).toBe(true)
    expect(form.matchingMode.isMixed).toBe(true)
  })

  it("sends the value the user picked for a mixed limit", () => {
    const form = buildShareLimitFormState([
      torrent({ ratio_limit: -1 }),
      torrent({ ratio_limit: 3 }),
    ])

    const picked = { ...form, ratio: { ...form.ratio, mode: "custom" as const, customValue: 2, touched: true } }

    expect(shareLimitsUnresolved(picked, false, false)).toBe(false)
    expect(shareLimitValues(picked, false, false).ratioLimit).toBe(2)
  })
})

describe("speed limit form model", () => {
  it("keeps no limit for a torrent without a speed limit", () => {
    const form = buildSpeedLimitFormState([torrent({ up_limit: 0, dl_limit: 0 })])

    expect(form.upload.enabled).toBe(false)
    expect(speedLimitValues(form)).toEqual({ uploadLimit: 0, downloadLimit: 0 })
  })

  it("keeps a custom limit in KiB/s", () => {
    const form = buildSpeedLimitFormState([torrent({ up_limit: 512 * 1024, dl_limit: 0 })])

    expect(form.upload).toMatchObject({ enabled: true, limit: 512 })
    expect(speedLimitValues(form)).toEqual({ uploadLimit: 512, downloadLimit: 0 })
  })

  it("marks a limit as mixed when the torrents disagree", () => {
    const form = buildSpeedLimitFormState([
      torrent({ up_limit: 1024, dl_limit: 2048 }),
      torrent({ up_limit: 0, dl_limit: 2048 }),
    ])

    expect(form.upload).toMatchObject({ isMixed: true, touched: false })
    expect(form.download).toMatchObject({ isMixed: false, enabled: true, limit: 2 })
    expect(speedLimitsUnresolved(form)).toBe(true)
  })

  it("sends the value the user picked for a mixed limit", () => {
    const form = buildSpeedLimitFormState([
      torrent({ up_limit: 1024 }),
      torrent({ up_limit: 0 }),
    ])

    const picked = { ...form, upload: { ...form.upload, enabled: true, limit: 300, touched: true } }

    expect(speedLimitsUnresolved(picked)).toBe(false)
    expect(speedLimitValues(picked).uploadLimit).toBe(300)
  })
})
