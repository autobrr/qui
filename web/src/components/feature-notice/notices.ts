/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { ComponentType } from "react"

import { GroupCopiesNoticePicture } from "@/components/torrents/table/GroupCopiesNoticePicture"
import { readGroupCopies } from "@/hooks/usePersistedGroupCopies"
import { getDisplayProfiles } from "@/lib/client-settings"

export interface FeatureNoticeEntry {
  id: string
  titleKey: string
  descriptionKey: string
  actionLabelKey: string
  Picture?: ComponentType
  isInUse: () => boolean
}

export const FEATURE_NOTICES = [
  {
    id: "group-copies",
    titleKey: "torrents:featureNotice.groupCopies.title",
    descriptionKey: "torrents:featureNotice.groupCopies.description",
    actionLabelKey: "torrents:featureNotice.groupCopies.action",
    Picture: GroupCopiesNoticePicture,
    isInUse: readGroupCopies,
  },
  {
    id: "display-profiles",
    titleKey: "torrents:featureNotice.displayProfiles.title",
    descriptionKey: "torrents:featureNotice.displayProfiles.description",
    actionLabelKey: "torrents:featureNotice.displayProfiles.action",
    isInUse: () => getDisplayProfiles().profiles.length > 1,
  },
] as const satisfies readonly FeatureNoticeEntry[]

export type FeatureNoticeId = (typeof FEATURE_NOTICES)[number]["id"]
