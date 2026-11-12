/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { ComponentType } from "react"

import { GroupCopiesNoticePicture } from "@/components/torrents/table/GroupCopiesNoticePicture"
import { readGroupCopies } from "@/hooks/usePersistedGroupCopies"

export interface FeatureNoticeEntry {
  id: string
  titleKey: string
  descriptionKey: string
  actionLabelKey: string
  /** A small drawing of what the feature does, shown above the title. Hide it from screen readers. */
  Picture?: ComponentType
  /** True when the user already uses the feature. Reads Client settings; checked again after each change. */
  isInUse: () => boolean
}

/** In ship order, oldest first. Three entries at most: to add a fourth, delete the oldest. */
export const FEATURE_NOTICES = [
  {
    id: "group-copies",
    titleKey: "torrents:featureNotice.groupCopies.title",
    descriptionKey: "torrents:featureNotice.groupCopies.description",
    actionLabelKey: "torrents:featureNotice.groupCopies.action",
    Picture: GroupCopiesNoticePicture,
    isInUse: readGroupCopies,
  },
] as const satisfies readonly FeatureNoticeEntry[]

export type FeatureNoticeId = (typeof FEATURE_NOTICES)[number]["id"]
