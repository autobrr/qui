/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { joinPath } from "@/lib/utils"
import type { Category } from "@/types/torrents"

/** Sentinel the add dialog uses for "no category". */
const NO_CATEGORY = "__none__"

export interface DestinationPathInput {
  autoTMM: boolean
  savePath: string
  category: string
  categories: Record<string, Category> | undefined
  defaultSavePath: string | undefined
}

/**
 * Resolves the path qBittorrent saves a new torrent to. With automatic torrent
 * management off it is the entered save path, otherwise the assigned category's
 * path, or the implicit <default save path>/<category> when the category has none.
 * Returns "" when the destination cannot be resolved.
 */
export function resolveDestinationPath({ autoTMM, savePath, category, categories, defaultSavePath }: DestinationPathInput): string {
  const fallback = (defaultSavePath ?? "").trim()

  if (!autoTMM) {
    return savePath.trim() || fallback
  }

  const name = category.trim()
  if (name === "" || name === NO_CATEGORY) {
    return fallback
  }

  const configured = (categories?.[name]?.savePath ?? "").trim()
  if (configured) {
    return configured
  }

  return fallback ? joinPath(fallback, name) : ""
}
