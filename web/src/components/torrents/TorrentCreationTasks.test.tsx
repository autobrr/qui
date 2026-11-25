/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, render, screen } from "@testing-library/react"
import { afterEach, expect, it, vi } from "vitest"

import { TooltipProvider } from "@/components/ui/tooltip"
import { api } from "@/lib/api"
import type { TorrentCreationStatus, TorrentCreationTask } from "@/types"
import { TorrentCreationTasks } from "./TorrentCreationTasks"


vi.mock("@/lib/api", () => ({ api: { getTorrentCreationTasks: vi.fn() } }))
vi.mock("@/hooks/useDateTimeFormatters", () => {
  const formatters = { formatDate: () => "2024-06-15 14:30" }
  return { useDateTimeFormatters: () => formatters }
})
vi.mock("react-i18next", () => {
  const translation = { t: (key: string) => key }
  return { useTranslation: () => translation }
})

afterEach(() => {
  cleanup()
  vi.useRealTimers()
  vi.clearAllMocks()
})

it.each<[TorrentCreationStatus | undefined, number]>([
  ["Running", 2000],
  ["Queued", 2000],
  ["Finished", 30000],
  [undefined, 30000],
])("polls %s tasks after %i ms", async (status, interval) => {
  vi.useFakeTimers()
  const tasks: TorrentCreationTask[] = status ? [{
    taskID: "synthetic-task",
    sourcePath: "/synthetic/sample.iso",
    pieceSize: 16384,
    private: false,
    status,
    timeAdded: "2024-06-15T14:30:45Z",
  }] : []
  vi.mocked(api.getTorrentCreationTasks).mockResolvedValue(tasks)
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })

  render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <TorrentCreationTasks instanceId={1} />
      </TooltipProvider>
    </QueryClientProvider>
  )
  await act(() => vi.advanceTimersByTimeAsync(0))
  expect(api.getTorrentCreationTasks).toHaveBeenCalledTimes(1)

  await act(() => vi.advanceTimersByTimeAsync(interval - 1))
  expect(api.getTorrentCreationTasks).toHaveBeenCalledTimes(1)
  await act(() => vi.advanceTimersByTimeAsync(1))
  expect(api.getTorrentCreationTasks).toHaveBeenCalledTimes(2)
  cleanup()
  client.clear()
})

it.each<[TorrentCreationStatus, string]>([
  ["Running", "creationTasks.statusLabels.Running"],
  ["FutureStatus" as TorrentCreationStatus, "common:status.unknown"],
])("renders translated label key for %s without falling back to the raw status", async (status, expectedKey) => {
  vi.mocked(api.getTorrentCreationTasks).mockResolvedValue([{
    taskID: "synthetic-task",
    sourcePath: "/synthetic/sample.iso",
    pieceSize: 16384,
    private: false,
    status,
    timeAdded: "2024-06-15T14:30:45Z",
  }])
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })

  render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <TorrentCreationTasks instanceId={1} />
      </TooltipProvider>
    </QueryClientProvider>
  )

  expect(await screen.findAllByText(expectedKey)).toHaveLength(2)
  expect(screen.queryByText(status)).toBeNull()
  client.clear()
})
