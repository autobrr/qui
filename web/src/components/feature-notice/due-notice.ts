/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { parseJsonBoolean, readClientSetting, writeClientSetting } from "@/lib/client-settings"
import { FEATURE_NOTICES } from "./notices"

export const SEEN_SETTING = { defaultValue: false, parse: parseJsonBoolean }

export const seenKey = (id: string) => `qui-feature-notice-seen:${id}`

export function markFeatureNoticeSeen(id: string): void {
  writeClientSetting(seenKey(id), true)
}

let dueNoticeId: string | null | undefined

export function getDueNoticeId(): string | null {
  if (dueNoticeId === undefined) {
    dueNoticeId = FEATURE_NOTICES.find((notice) => !readClientSetting(seenKey(notice.id), SEEN_SETTING) && !notice.isInUse())?.id ?? null
  }
  return dueNoticeId
}

export function _resetFeatureNoticesForTests(): void {
  dueNoticeId = undefined
}
