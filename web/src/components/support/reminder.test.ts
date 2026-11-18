/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { TFunction } from "i18next"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { _resetClientSettingsForTests, readRaw } from "@/lib/client-settings"
import {
  firstDueTime,
  hideShortcut,
  isReminderDue,
  notNow,
  REMINDER_DUE_KEY,
  REMINDER_OFF_KEY,
  SHORTCUT_HIDDEN_KEY,
  type ReminderInputs
} from "./reminder"

const toast = vi.hoisted(() => vi.fn())
vi.mock("sonner", () => ({ toast }))

const DAY = 24 * 60 * 60 * 1000
const NOW = Date.UTC(2026, 10, 18, 12)
const t = ((key: string) => key) as unknown as TFunction

type ToastAction = { label: string; onClick: () => void }
const toastAt = (index: number) => toast.mock.calls[index] as [string, { action: ToastAction }]

beforeEach(() => {
  localStorage.clear()
  _resetClientSettingsForTests()
  vi.useFakeTimers({ toFake: ["Date"], now: NOW })
})

afterEach(() => {
  vi.useRealTimers()
  vi.clearAllMocks()
})

describe("isReminderDue", () => {
  const due: ReminderInputs = {
    now: NOW,
    dueAt: NOW,
    setupAt: NOW - 400 * DAY,
    isSupporter: false,
    shortcutHidden: false,
    reminderOff: false,
    appNotice: false,
  }

  it.each<[string, Partial<ReminderInputs>, boolean]>([
    ["at the due time", {}, true],
    ["before the due time", { dueAt: NOW + 1 }, false],
    ["without a stored due time", { dueAt: null }, false],
    ["without the setup time of the server", { setupAt: null }, false],
    ["for a supporter", { isSupporter: true }, false],
    ["while the shortcut is hidden", { shortcutHidden: true }, false],
    ["after Don't ask again", { reminderOff: true }, false],
    ["while an update banner or a system notice shows", { appNotice: true }, false],
  ])("%s", (_, change, expected) => {
    expect(isReminderDue({ ...due, ...change })).toBe(expected)
  })
})

describe("firstDueTime", () => {
  it.each([
    ["waits until a new server is 30 days old", NOW, 0, NOW + 30 * DAY],
    ["comes 14 days from now at the highest draw", NOW - 400 * DAY, 0.999, NOW + 14 * DAY],
  ])("%s", (_, setupAt, draw, expected) => {
    expect(firstDueTime(NOW, setupAt, () => draw)).toBe(expected)
  })
})

describe("answers", () => {
  it("Not now waits a year, and its toast turns the reminder off, with an undo", () => {
    notNow(t)

    expect(readRaw(REMINDER_DUE_KEY)).toBe(String(NOW + 365 * DAY))
    const [title, { action }] = toastAt(0)
    expect(title).toBe("support.reminder.notNowToast")

    action.onClick()
    expect(readRaw(REMINDER_OFF_KEY)).toBe("true")

    toastAt(1)[1].action.onClick()
    expect(readRaw(REMINDER_OFF_KEY)).toBe("false")
  })

  it("Hide the heart hides the shortcut, with an undo", () => {
    hideShortcut(t)

    expect(readRaw(SHORTCUT_HIDDEN_KEY)).toBe("true")

    toastAt(0)[1].action.onClick()
    expect(readRaw(SHORTCUT_HIDDEN_KEY)).toBe("false")
  })
})
