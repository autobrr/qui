/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useQuery } from "@tanstack/react-query"
import type { TFunction } from "i18next"
import { useEffect } from "react"
import { toast } from "sonner"

import { useHasPremiumAccess } from "@/hooks/useLicense"
import { useSystemNotices } from "@/hooks/useSystemNotices"
import { api } from "@/lib/api"
import {
  parseJsonBoolean,
  parseJsonNumber,
  readClientSetting,
  useClientSetting,
  useClientSettingsLoaded,
  useClientSettingsSettled,
  writeClientSetting
} from "@/lib/client-settings"
import { isDemo } from "@/lib/demo"

// i18n-namespace: common

const DAY = 24 * 60 * 60 * 1000

export const SHORTCUT_HIDDEN_KEY = "qui-support-shortcut-hidden"
export const REMINDER_OFF_KEY = "qui-support-reminder-off"
/** The time, in milliseconds since the epoch, from which the yearly reminder shows. */
export const REMINDER_DUE_KEY = "qui-support-reminder-due"

const FLAG = { defaultValue: false, parse: parseJsonBoolean }
const DUE_TIME = { defaultValue: null as number | null, parse: parseJsonNumber }

/** The first due time: 7 to 14 days from now, and not before the server is 30 days old. */
export function firstDueTime(now: number, setupAt: number, random: () => number = Math.random): number {
  const days = 7 + Math.floor(random() * 8)
  return Math.max(now + days * DAY, setupAt + 30 * DAY)
}

export const daysOnServer = (now: number, setupAt: number) => Math.floor((now - setupAt) / DAY)

export interface ReminderInputs {
  now: number
  dueAt: number | null
  /** Null when the server did not send its setup time. */
  setupAt: number | null
  isSupporter: boolean
  shortcutHidden: boolean
  reminderOff: boolean
  /** The app shows an update banner or a system notice. */
  appNotice: boolean
}

export function isReminderDue({ now, dueAt, setupAt, isSupporter, shortcutHidden, reminderOff, appNotice }: ReminderInputs): boolean {
  if (dueAt === null || setupAt === null || now < dueAt) return false
  return !isSupporter && !shortcutHidden && !reminderOff && !appNotice
}

export function useSupportShortcutHidden(): boolean {
  return useClientSetting(SHORTCUT_HIDDEN_KEY, FLAG)[0]
}

/** The state of the yearly reminder. The first call after the first read of the server stores the first due time. */
export function useSupportReminder() {
  const settled = useClientSettingsSettled()
  const loaded = useClientSettingsLoaded()
  const shortcutHidden = useSupportShortcutHidden()
  const [reminderOff] = useClientSetting(REMINDER_OFF_KEY, FLAG)
  const [dueAt] = useClientSetting(REMINDER_DUE_KEY, DUE_TIME)
  const { isSupporter, setupAt: setupTime } = useHasPremiumAccess()
  const notices = useSystemNotices()
  // The header and the sidebar fetch the latest release. The reminder only reads it.
  const { data: update } = useQuery({ queryKey: ["latest-version"], queryFn: () => api.getLatestVersion(), enabled: false })
  const setupAt = setupTime ? Date.parse(setupTime) : null

  useEffect(() => {
    // Not on a failed read of the server: the queued time would later replace the due time on the server.
    // Read again: each component that uses the hook runs this effect, and the first one stores the time.
    if (isDemo || !loaded || setupAt === null || readClientSetting(REMINDER_DUE_KEY, DUE_TIME) !== null) return
    writeClientSetting(REMINDER_DUE_KEY, firstDueTime(Date.now(), setupAt))
  }, [loaded, setupAt])

  const now = Date.now()
  const due = !isDemo && settled && isReminderDue({
    now,
    dueAt,
    setupAt,
    isSupporter,
    shortcutHidden,
    reminderOff,
    appNotice: Boolean(update) || notices.length > 0,
  })

  return { due, days: setupAt === null ? 0 : daysOnServer(now, setupAt), settled, isSupporter, shortcutHidden, reminderOff }
}

/** Ends the reminder for one year. An open of the support dialog while the reminder is due calls it. */
export function answerReminder() {
  writeClientSetting(REMINDER_DUE_KEY, Date.now() + 365 * DAY)
}

export function setReminderOff(off: boolean) {
  writeClientSetting(REMINDER_OFF_KEY, off)
}

export function setShortcutHidden(hidden: boolean) {
  writeClientSetting(SHORTCUT_HIDDEN_KEY, hidden)
}

/** "Not now" on the reminder: no reminder for a year, and a toast that can stop the reminder for good. */
export function notNow(t: TFunction) {
  answerReminder()
  toast(t("support.reminder.notNowToast"), {
    action: { label: t("support.reminder.dontAskAgain"), onClick: () => turnOffReminder(t) },
  })
}

export function turnOffReminder(t: TFunction) {
  setReminderOff(true)
  toast(t("support.reminder.offToast"), {
    description: t("support.reminder.offToastDescription"),
    action: { label: t("support.reminder.undo"), onClick: () => setReminderOff(false) },
  })
}

export function hideShortcut(t: TFunction) {
  setShortcutHidden(true)
  toast(t("support.reminder.hiddenToast"), {
    description: t("support.reminder.hiddenToastDescription"),
    action: { label: t("support.reminder.undo"), onClick: () => setShortcutHidden(false) },
  })
}
