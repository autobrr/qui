/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { useTorrentListEmptyMessage } from "@/hooks/useTorrentListEmptyMessage"
import { useTranslation } from "react-i18next"

interface TorrentListEmptyStateProps {
  hasFilters: boolean
  hasSearch: boolean
  onClearFilters?: () => void
}

/** Tells the user why the torrent list is empty, and offers to clear the filters when they hide every torrent. */
export function TorrentListEmptyState({ hasFilters, hasSearch, onClearFilters }: TorrentListEmptyStateProps) {
  const { t } = useTranslation("torrents")
  const message = useTorrentListEmptyMessage(hasFilters, hasSearch)

  return (
    <div className="text-center animate-in zoom-in-95 duration-300 text-muted-foreground space-y-3">
      <p>{message}</p>
      {hasFilters && onClearFilters && (
        <Button size="sm" variant="outline" onClick={onClearFilters}>
          {t("columnFilter.clearFilters")}
        </Button>
      )}
    </div>
  )
}
