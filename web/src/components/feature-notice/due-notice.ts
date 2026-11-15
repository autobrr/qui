/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { parseJsonBoolean, readClientSetting } from "@/lib/client-settings"
import { FEATURE_NOTICES } from "./notices"

export const SEEN_SETTING = { defaultValue: false, parse: parseJsonBoolean }

export const seenKey = (id: string) => `qui-feature-notice-seen:${id}`

let dueNoticeId: string | null | undefined

/**
 * The first notice that is not seen and not in use. Chosen on the first call
 * of the page load and fixed until the next one. Call it only after the first
 * server snapshot.
 */
export function getDueNoticeId(): string | null {
  if (dueNoticeId === undefined) {
    dueNoticeId = FEATURE_NOTICES.find((notice) => !readClientSetting(seenKey(notice.id), SEEN_SETTING) && !notice.isInUse())?.id ?? null
  }
  return dueNoticeId
}

// Test-only: stands for the next page load.
export function _resetFeatureNoticesForTests(): void {
  dueNoticeId = undefined
}
