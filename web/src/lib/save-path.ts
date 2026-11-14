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
  /** True when qBittorrent saves a category without a path under its parent's folder: 5.0+ with subcategories on. */
  parentCategoryPaths: boolean
}

/**
 * Resolves the path qBittorrent saves a new torrent to. With automatic torrent
 * management off it is the entered save path. Otherwise it follows qBittorrent's
 * category rule: a category's own path, relative paths counted from the default
 * save path, and a category without a path saved as <parent category path>/<leaf name>
 * (qBittorrent 5.0+ with subcategories on) or <default save path>/<category>.
 * Returns "" when the destination cannot be resolved.
 */
export function resolveDestinationPath({ autoTMM, savePath, category, categories, defaultSavePath, parentCategoryPaths }: DestinationPathInput): string {
  const fallback = (defaultSavePath ?? "").trim()

  if (!autoTMM) {
    return savePath.trim() || fallback
  }

  const name = category.trim()
  return name === NO_CATEGORY ? fallback : categorySavePath(name, categories, fallback, parentCategoryPaths)
}

// Mirrors SessionImpl::categorySavePath in qBittorrent.
function categorySavePath(name: string, categories: Record<string, Category> | undefined, fallback: string, parentCategoryPaths: boolean): string {
  if (name === "") {
    return fallback
  }

  const configured = (categories?.[name]?.savePath ?? "").trim()
  if (configured) {
    return isAbsolutePath(configured) || !fallback ? configured : joinPath(fallback, configured)
  }

  if (!parentCategoryPaths) {
    return fallback ? joinPath(fallback, name) : ""
  }

  const slash = name.lastIndexOf("/")
  const parentPath = categorySavePath(slash === -1 ? "" : name.slice(0, slash), categories, fallback, parentCategoryPaths)
  return parentPath ? joinPath(parentPath, name.slice(slash + 1)) : ""
}

// A POSIX root, a Windows drive root, or a UNC share.
function isAbsolutePath(path: string): boolean {
  return path.startsWith("/") || path.startsWith("\\\\") || /^[A-Za-z]:[\\/]/.test(path)
}
