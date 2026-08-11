/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useTorrentExporter } from "@/hooks/useTorrentExporter"
import { api } from "@/lib/api"
import { makeTorrent } from "@/test/mockTorrent"
import type { CrossInstanceTorrent, Torrent } from "@/types"
import { act, renderHook } from "@testing-library/react"
import { afterAll, beforeAll, beforeEach, describe, expect, it, vi } from "vitest"

vi.mock("react-i18next", () => ({
  useTranslation: () => ({
    t: (key: string) => key,
  }),
}))

vi.mock("sonner", () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
    info: vi.fn(),
  },
}))

vi.mock("@/lib/api", () => ({
  api: {
    exportTorrent: vi.fn(),
    getTorrents: vi.fn(),
    getCrossInstanceTorrents: vi.fn(),
  },
}))

const mockedApi = vi.mocked(api)

function makeCrossTorrent(overrides: Partial<CrossInstanceTorrent>): CrossInstanceTorrent {
  return {
    ...makeTorrent(overrides),
    instanceId: overrides.instanceId ?? 1,
    instanceName: overrides.instanceName ?? "instance",
  }
}

function exportSelection(torrents: Torrent[], overrides: Record<string, unknown> = {}) {
  return {
    hashes: torrents.map(t => t.hash),
    torrents,
    isAllSelected: false,
    totalSelected: torrents.length,
    ...overrides,
  }
}

let anchorClick: ReturnType<typeof vi.spyOn>

beforeAll(() => {
  URL.createObjectURL = vi.fn(() => "blob:mock")
  URL.revokeObjectURL = vi.fn()
  anchorClick = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {})
})

afterAll(() => {
  anchorClick.mockRestore()
})

beforeEach(() => {
  vi.clearAllMocks()
  mockedApi.exportTorrent.mockResolvedValue({ blob: new Blob(["data"]), filename: null })
  mockedApi.getTorrents.mockResolvedValue({ torrents: [], total: 0, hasMore: false })
  mockedApi.getCrossInstanceTorrents.mockResolvedValue({ torrents: [], total: 0, hasMore: false })
})

describe("useTorrentExporter — per-torrent instance routing", () => {
  it("routes each export to the torrent's own instance in unified scope", async () => {
    const { result } = renderHook(() => useTorrentExporter({ instanceId: 0, incognitoMode: false }))
    const torrents = [
      makeCrossTorrent({ hash: "aaa", instanceId: 2 }),
      makeCrossTorrent({ hash: "bbb", instanceId: 3 }),
    ]

    await act(async () => {
      await result.current.exportTorrents(exportSelection(torrents))
    })

    expect(mockedApi.exportTorrent).toHaveBeenCalledTimes(2)
    expect(mockedApi.exportTorrent).toHaveBeenCalledWith(2, "aaa")
    expect(mockedApi.exportTorrent).toHaveBeenCalledWith(3, "bbb")
    expect(mockedApi.exportTorrent).not.toHaveBeenCalledWith(0, expect.anything())
  })

  it("falls back to the view instance for plain torrents in a single-instance view", async () => {
    const { result } = renderHook(() => useTorrentExporter({ instanceId: 2, incognitoMode: false }))
    const torrents = [makeTorrent({ hash: "aaa" })]

    await act(async () => {
      await result.current.exportTorrents(exportSelection(torrents))
    })

    expect(mockedApi.exportTorrent).toHaveBeenCalledTimes(1)
    expect(mockedApi.exportTorrent).toHaveBeenCalledWith(2, "aaa")
  })

  it("exports both copies when the same hash exists on two instances", async () => {
    const { result } = renderHook(() => useTorrentExporter({ instanceId: 0, incognitoMode: false }))
    const torrents = [
      makeCrossTorrent({ hash: "same", instanceId: 2 }),
      makeCrossTorrent({ hash: "same", instanceId: 3 }),
    ]

    await act(async () => {
      await result.current.exportTorrents(exportSelection(torrents, { hashes: ["same"] }))
    })

    expect(mockedApi.exportTorrent).toHaveBeenCalledTimes(2)
    expect(mockedApi.exportTorrent).toHaveBeenCalledWith(2, "same")
    expect(mockedApi.exportTorrent).toHaveBeenCalledWith(3, "same")
  })
})

describe("useTorrentExporter — select-all", () => {
  it("pages the cross-instance endpoint in unified scope and routes per row", async () => {
    mockedApi.getCrossInstanceTorrents.mockResolvedValue({
      torrents: [],
      crossInstanceTorrents: [
        makeCrossTorrent({ hash: "aaa", instanceId: 2 }),
        makeCrossTorrent({ hash: "bbb", instanceId: 3 }),
      ],
      total: 2,
      hasMore: false,
    })

    const { result } = renderHook(() => useTorrentExporter({ instanceId: 0, incognitoMode: false }))

    await act(async () => {
      await result.current.exportTorrents(exportSelection([], {
        isAllSelected: true,
        totalSelected: 2,
        instanceIds: [2, 3],
      }))
    })

    expect(mockedApi.getCrossInstanceTorrents).toHaveBeenCalledWith(
      expect.objectContaining({ instanceIds: [2, 3] })
    )
    expect(mockedApi.getTorrents).not.toHaveBeenCalled()
    expect(mockedApi.exportTorrent).toHaveBeenCalledTimes(2)
    expect(mockedApi.exportTorrent).toHaveBeenCalledWith(2, "aaa")
    expect(mockedApi.exportTorrent).toHaveBeenCalledWith(3, "bbb")
  })

  it("keeps paging the per-instance endpoint in a single-instance view", async () => {
    mockedApi.getTorrents.mockResolvedValue({
      torrents: [makeTorrent({ hash: "aaa" })],
      total: 1,
      hasMore: false,
    })

    const { result } = renderHook(() => useTorrentExporter({ instanceId: 2, incognitoMode: false }))

    await act(async () => {
      await result.current.exportTorrents(exportSelection([], {
        isAllSelected: true,
        totalSelected: 1,
      }))
    })

    expect(mockedApi.getTorrents).toHaveBeenCalledWith(2, expect.any(Object))
    expect(mockedApi.getCrossInstanceTorrents).not.toHaveBeenCalled()
    expect(mockedApi.exportTorrent).toHaveBeenCalledWith(2, "aaa")
  })

  it("skips excluded instance:hash targets in unified scope", async () => {
    mockedApi.getCrossInstanceTorrents.mockResolvedValue({
      torrents: [],
      crossInstanceTorrents: [
        makeCrossTorrent({ hash: "same", instanceId: 2 }),
        makeCrossTorrent({ hash: "same", instanceId: 3 }),
      ],
      total: 2,
      hasMore: false,
    })

    const { result } = renderHook(() => useTorrentExporter({ instanceId: 0, incognitoMode: false }))

    await act(async () => {
      await result.current.exportTorrents(exportSelection([], {
        isAllSelected: true,
        totalSelected: 1,
        excludeTargets: [{ instanceId: 2, hash: "same" }],
      }))
    })

    expect(mockedApi.exportTorrent).toHaveBeenCalledTimes(1)
    expect(mockedApi.exportTorrent).toHaveBeenCalledWith(3, "same")
  })
})
