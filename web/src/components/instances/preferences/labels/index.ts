/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { PreferenceField } from "@/types"

import { advancedLabels } from "./advanced"
import { behaviorLabels } from "./behavior"
import { bittorrentLabels } from "./bittorrent"
import { connectionLabels } from "./connection"
import { downloadsLabels } from "./downloads"
import { rssLabels } from "./rss"
import { speedLabels } from "./speed"
import type { TabLabels } from "./types"
import { webUILabels } from "./webui"

export type { FieldLabels, TabLabels } from "./types"

// The qBittorrent tabs in the order of the qBittorrent WebUI.
export const preferenceTabLabels: Record<PreferenceField["tab"], TabLabels> = {
  behavior: behaviorLabels,
  downloads: downloadsLabels,
  connection: connectionLabels,
  speed: speedLabels,
  bittorrent: bittorrentLabels,
  rss: rssLabels,
  webui: webUILabels,
  advanced: advancedLabels,
}
