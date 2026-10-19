/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useTranslation } from "react-i18next"

/** The reason the torrent list is empty: filters, a search, or no torrents at all. */
export function useTorrentListEmptyMessage(hasFilters: boolean, hasSearch: boolean): string {
  const { t } = useTranslation("torrents")
  if (hasFilters) return t("emptyState.noFilterMatches")
  if (hasSearch) return t("emptyState.noSearchMatches")
  return t("emptyState.noTorrents")
}
