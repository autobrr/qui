/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { joinPath } from "@/lib/utils"
import type { Category } from "@/types/torrents"

const NO_CATEGORY = "__none__"

export interface DestinationPathInput {
  autoTMM: boolean
  savePath: string
  category: string
  categories: Record<string, Category> | undefined
  defaultSavePath: string | undefined
  parentCategoryPaths: boolean
}

export function resolveDestinationPath({ autoTMM, savePath, category, categories, defaultSavePath, parentCategoryPaths }: DestinationPathInput): string {
  const fallback = (defaultSavePath ?? "").trim()

  if (!autoTMM) {
    return savePath.trim() || fallback
  }

  const name = category.trim()
  return name === NO_CATEGORY ? fallback : categorySavePath(name, categories, fallback, parentCategoryPaths)
}

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

function isAbsolutePath(path: string): boolean {
  return path.startsWith("/") || path.startsWith("\\\\") || /^[A-Za-z]:[\\/]/.test(path)
}
