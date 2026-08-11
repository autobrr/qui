/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import {
  checkFieldConsistency,
  LIMIT_UNLIMITED,
  LIMIT_USE_GLOBAL,
  shareLimitEnumFieldFromTorrents,
  type TorrentLimitSnapshot
} from "./torrentLimitDialogHelpers"

function numericShareLimitToMobile(
  value: number | undefined,
  defaultCustom: number
): { enabled: boolean; limit: number } {
  if (value === undefined || value === LIMIT_USE_GLOBAL || value === LIMIT_UNLIMITED) {
    return { enabled: false, limit: defaultCustom }
  }
  return { enabled: true, limit: value }
}

export interface MobileShareLimitFormState {
  ratioEnabled: boolean
  ratioLimit: number
  seedingTimeEnabled: boolean
  seedingTimeLimit: number
  inactiveSeedingTimeEnabled: boolean
  inactiveSeedingTimeLimit: number
  shareLimitAction: string
  shareLimitsMode: string
}

export function buildMobileShareLimitInitialState(
  torrents?: TorrentLimitSnapshot[]
): MobileShareLimitFormState {
  const defaults: MobileShareLimitFormState = {
    ratioEnabled: false,
    ratioLimit: 1.5,
    seedingTimeEnabled: false,
    seedingTimeLimit: 1440,
    inactiveSeedingTimeEnabled: false,
    inactiveSeedingTimeLimit: 10080,
    shareLimitAction: "default",
    shareLimitsMode: "default",
  }

  if (!torrents?.length) {
    return defaults
  }

  const ratioCheck = checkFieldConsistency(torrents, t => t.ratio_limit)
  const seedTimeCheck = checkFieldConsistency(torrents, t => t.seeding_time_limit)
  const inactiveTimeCheck = checkFieldConsistency(torrents, t => t.inactive_seeding_time_limit)
  const ratio = ratioCheck.isMixed? { enabled: defaults.ratioEnabled, limit: defaults.ratioLimit }: numericShareLimitToMobile(ratioCheck.commonValue, defaults.ratioLimit)
  const seedTime = seedTimeCheck.isMixed? { enabled: defaults.seedingTimeEnabled, limit: defaults.seedingTimeLimit }: numericShareLimitToMobile(seedTimeCheck.commonValue, defaults.seedingTimeLimit)
  const inactiveTime = inactiveTimeCheck.isMixed? { enabled: defaults.inactiveSeedingTimeEnabled, limit: defaults.inactiveSeedingTimeLimit }: numericShareLimitToMobile(inactiveTimeCheck.commonValue, defaults.inactiveSeedingTimeLimit)
  const action = shareLimitEnumFieldFromTorrents(torrents, t => t.share_limit_action)
  const mode = shareLimitEnumFieldFromTorrents(torrents, t => t.share_limits_mode)

  return {
    ratioEnabled: ratio.enabled,
    ratioLimit: ratio.limit,
    seedingTimeEnabled: seedTime.enabled,
    seedingTimeLimit: seedTime.limit,
    inactiveSeedingTimeEnabled: inactiveTime.enabled,
    inactiveSeedingTimeLimit: inactiveTime.limit,
    shareLimitAction: action.value,
    shareLimitsMode: mode.value,
  }
}
