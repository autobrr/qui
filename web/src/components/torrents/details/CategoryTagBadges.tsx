/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Badge } from "@/components/ui/badge"
import { getLinuxCategory, getLinuxTags, useIncognitoMode } from "@/lib/incognito"
import { parseTorrentTags } from "@/lib/torrent-utils"
import type { Torrent } from "@/types"
import { Folder, Tag } from "lucide-react"

export function CategoryTagBadges({ torrent, className }: { torrent: Torrent; className: string }) {
  const [incognitoMode] = useIncognitoMode()
  const category = incognitoMode ? getLinuxCategory(torrent.hash) : torrent.category
  const tagList = parseTorrentTags(incognitoMode ? getLinuxTags(torrent.hash) : torrent.tags)
  if (!category && tagList.length === 0) return null
  return (
    <div className={className}>
      {category && (
        <Badge variant="secondary">
          <Folder />{category}
        </Badge>
      )}
      {tagList.map(tag => (
        <Badge key={tag} variant="outline">
          <Tag />{tag}
        </Badge>
      ))}
    </div>
  )
}
