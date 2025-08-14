/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type { SortingState, ColumnFiltersState } from '@tanstack/react-table'

interface TorrentQueryParams {
  page: number
  limit: number
  sorting: SortingState
  filters: ColumnFiltersState
  search?: string
}

export function useTorrents(instanceId: number, params: TorrentQueryParams) {
  return useQuery({
    queryKey: ['torrents', instanceId, params],
    queryFn: () => api.getTorrents(instanceId, {
      page: params.page,
      limit: params.limit,
      sort: params.sorting[0]?.id,
      order: params.sorting[0]?.desc ? 'desc' : 'asc',
      search: params.search,
      filters: params.filters.reduce((acc, filter) => {
        acc[filter.id] = filter.value
        return acc
      }, {} as Record<string, any>),
    }),
    staleTime: 5000,
    refetchInterval: 5000, // Poll every 5 seconds
    placeholderData: (previousData) => previousData, // Keep previous data while fetching
  })
}