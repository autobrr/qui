/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { renderHook, waitFor } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

vi.mock("@/lib/api", () => ({
  api: {
    getInstances: vi.fn(),
    getLocalCrossSeedMatches: vi.fn(),
  },
}))

import { api } from "@/lib/api"
import {
  getLocalMatchTypeInfo,
  hasBreakableLocalMatches,
  isHardlinkManaged,
  isInsideBase,
  normalizePath,
  parseNonNegativeInt,
  toCompatibleMatch,
  useLocalCrossSeedMatches
} from "@/lib/cross-seed-utils"
import { makeTorrent } from "@/test/mockTorrent"
import type { LocalCrossSeedMatch } from "@/types"

const mockedApi = vi.mocked(api, true)

function makeWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  )
}

function makeMatch(overrides: Partial<LocalCrossSeedMatch> = {}): LocalCrossSeedMatch {
  return {
    instanceId: 2,
    instanceName: "seedbox",
    hash: "deadbeef",
    name: "Some.Release.1080p",
    size: 12345,
    progress: 1,
    savePath: "/data/movies",
    contentPath: "/data/movies/Some.Release.1080p",
    category: "movies",
    tags: "linux,iso",
    state: "uploading",
    tracker: "https://tracker.example/announce",
    trackerHealth: "unregistered",
    matchType: "release",
    ...overrides,
  }
}

describe("normalizePath", () => {
  it("lowercases, collapses mixed slash runs to single '/', and strips a single trailing slash", () => {
    expect(normalizePath("C:\\Foo//Bar/")).toBe("c:/foo/bar")
  })

  it("collapses any mix of backslashes and forward slashes between segments", () => {
    expect(normalizePath("a\\\\b//c\\/d")).toBe("a/b/c/d")
  })

  it("collapses slash runs first, then strips the single trailing slash", () => {
    expect(normalizePath("/a//")).toBe("/a")
  })

  it("returns '' for empty or undefined input", () => {
    expect(normalizePath("")).toBe("")
    expect(normalizePath(undefined as unknown as string)).toBe("")
  })
})

describe("parseNonNegativeInt", () => {
  it("truncates decimals and passes whole numbers through", () => {
    expect(parseNonNegativeInt("50")).toBe(50)
    expect(parseNonNegativeInt("12.7")).toBe(12)
  })

  it("clamps negative input to 0", () => {
    expect(parseNonNegativeInt("-5")).toBe(0)
  })

  it("maps empty and non-numeric input to 0", () => {
    expect(parseNonNegativeInt("")).toBe(0)
    expect(parseNonNegativeInt("abc")).toBe(0)
  })
})

describe("isInsideBase", () => {
  it("is false when the base is empty", () => {
    expect(isInsideBase("/data/movies", "")).toBe(false)
  })

  it("is true when the path equals the base", () => {
    expect(isInsideBase("/data/movies", "/data/movies")).toBe(true)
  })

  it("is true when the path starts with base + '/'", () => {
    expect(isInsideBase("/data/movies/foo", "/data/movies")).toBe(true)
  })

  it("is false for a shared prefix without a slash boundary", () => {
    // The load-bearing edge: '/data/movies2' must NOT count as inside '/data/movies'.
    expect(isInsideBase("/data/movies2", "/data/movies")).toBe(false)
  })
})

describe("isHardlinkManaged", () => {
  const base = "/data/links"

  it("is false when the instance is undefined", () => {
    expect(isHardlinkManaged({ save_path: "/data/links/x" }, undefined)).toBe(false)
  })

  it("is false when useHardlinks is absent or false", () => {
    expect(
      isHardlinkManaged(
        { save_path: "/data/links/x" },
        { hasLocalFilesystemAccess: true, hardlinkBaseDir: base }
      )
    ).toBe(false)
  })

  it("is false when hasLocalFilesystemAccess is false", () => {
    expect(
      isHardlinkManaged(
        { save_path: "/data/links/x" },
        { useHardlinks: true, hasLocalFilesystemAccess: false, hardlinkBaseDir: base }
      )
    ).toBe(false)
  })

  it("is false when hardlinkBaseDir normalizes to empty", () => {
    expect(
      isHardlinkManaged(
        { save_path: "/data/links/x" },
        { useHardlinks: true, hasLocalFilesystemAccess: true, hardlinkBaseDir: "" }
      )
    ).toBe(false)
  })

  it("is true when only the save_path is inside the base", () => {
    expect(
      isHardlinkManaged(
        { save_path: "/data/links/show", content_path: "/elsewhere/show" },
        { useHardlinks: true, hasLocalFilesystemAccess: true, hardlinkBaseDir: base }
      )
    ).toBe(true)
  })

  it("is true when only the content_path is inside the base", () => {
    expect(
      isHardlinkManaged(
        { save_path: "/elsewhere/show", content_path: "/data/links/show/file.mkv" },
        { useHardlinks: true, hasLocalFilesystemAccess: true, hardlinkBaseDir: base }
      )
    ).toBe(true)
  })

  it("is false when neither path is inside the base", () => {
    expect(
      isHardlinkManaged(
        { save_path: "/elsewhere/a", content_path: "/elsewhere/b" },
        { useHardlinks: true, hasLocalFilesystemAccess: true, hardlinkBaseDir: base }
      )
    ).toBe(false)
  })
})

describe("local match type classification", () => {
  it("classifies content-path matches as delete-relevant and breakable", () => {
    expect(getLocalMatchTypeInfo("content_path")).toEqual({
      deleteRelevant: true,
      independentlyUsable: false,
      breaksWhenSourceDeleted: true,
    })
  })

  it.each(["hardlink", "reflink"] as const)(
    "classifies %s matches as delete-relevant and independently usable",
    (matchType) => {
      expect(getLocalMatchTypeInfo(matchType)).toEqual({
        deleteRelevant: true,
        independentlyUsable: true,
        breaksWhenSourceDeleted: false,
      })
    }
  )

  it.each(["name", "release"] as const)(
    "excludes %s matches from delete suggestions",
    (matchType) => {
      expect(getLocalMatchTypeInfo(matchType).deleteRelevant).toBe(false)
    }
  )

  it("makes mixed lists breakable only when a content-path match is present", () => {
    expect(hasBreakableLocalMatches([
      { matchType: "hardlink" },
      { matchType: "reflink" },
    ])).toBe(false)
    expect(hasBreakableLocalMatches([
      { matchType: "hardlink" },
      { matchType: "reflink" },
      { matchType: "content_path" },
    ])).toBe(true)
  })
})

describe("toCompatibleMatch", () => {
  it("maps camelCase match fields to the snake_case torrent shape", () => {
    const match = makeMatch()
    const result = toCompatibleMatch(match)

    expect(result.save_path).toBe(match.savePath)
    expect(result.content_path).toBe(match.contentPath)
    expect(result.tracker_health).toBe(match.trackerHealth)
    expect(result.instanceId).toBe(match.instanceId)
    expect(result.instanceName).toBe(match.instanceName)
    expect(result.matchType).toBe(match.matchType)
    expect(result.total_size).toBe(match.size)
  })

  it("fills in zeroed Torrent defaults for fields the match does not carry", () => {
    const result = toCompatibleMatch(makeMatch())
    expect(result.added_on).toBe(0)
    expect(result.ratio).toBe(0)
  })
})

describe("useLocalCrossSeedMatches", () => {
  beforeEach(() => {
    mockedApi.getInstances.mockResolvedValue([
      { id: 2, name: "seedbox" },
    ] as never)
    mockedApi.getLocalCrossSeedMatches.mockResolvedValue([makeMatch()])
  })

  afterEach(() => {
    vi.clearAllMocks()
  })

  it("fetches instances and matches, mapping matches through toCompatibleMatch (happy path)", async () => {
    const torrent = makeTorrent({ hash: "torrenthash" })
    const { result } = renderHook(
      () => useLocalCrossSeedMatches(2, torrent, true),
      { wrapper: makeWrapper() }
    )

    await waitFor(() => {
      expect(result.current.matchingTorrents).toHaveLength(1)
    })

    expect(mockedApi.getInstances).toHaveBeenCalled()
    expect(mockedApi.getLocalCrossSeedMatches).toHaveBeenCalledWith(2, "torrenthash")
    expect(result.current.matchingTorrents[0]).toEqual(toCompatibleMatch(makeMatch()))
    // Verify the hook->mapping wiring independently of the transform implementation.
    expect(result.current.matchingTorrents[0].save_path).toBe("/data/movies")
    expect(result.current.matchingTorrents[0].total_size).toBe(12345)
    expect(result.current.allInstances).toEqual([{ id: 2, name: "seedbox" }])
    expect(result.current.pendingQueryCount).toBe(0)
    expect(result.current.isLoadingMatches).toBe(false)
  })

  it("reports a pending matches query while loading and clears it once settled", async () => {
    let resolveMatches: (value: LocalCrossSeedMatch[]) => void = () => {}
    mockedApi.getLocalCrossSeedMatches.mockReturnValue(
      new Promise<LocalCrossSeedMatch[]>((resolve) => {
        resolveMatches = resolve
      })
    )

    const { result } = renderHook(
      () => useLocalCrossSeedMatches(2, makeTorrent({ hash: "h" }), true),
      { wrapper: makeWrapper() }
    )

    await waitFor(() => {
      expect(result.current.pendingQueryCount).toBe(1)
    })
    expect(result.current.isLoadingMatches).toBe(true)

    resolveMatches([makeMatch()])

    await waitFor(() => {
      expect(result.current.pendingQueryCount).toBe(0)
    })
    expect(result.current.isLoadingMatches).toBe(false)
  })

  it("disables the matches query when the torrent is null but still loads instances", async () => {
    const { result } = renderHook(
      () => useLocalCrossSeedMatches(2, null, true),
      { wrapper: makeWrapper() }
    )

    await waitFor(() => {
      expect(result.current.allInstances).toHaveLength(1)
    })

    expect(mockedApi.getLocalCrossSeedMatches).not.toHaveBeenCalled()
    expect(result.current.matchingTorrents).toEqual([])
    expect(result.current.pendingQueryCount).toBe(0)
  })

  it("disables the matches query when instanceId <= 0", async () => {
    const { result } = renderHook(
      () => useLocalCrossSeedMatches(0, makeTorrent({ hash: "h" }), true),
      { wrapper: makeWrapper() }
    )

    await waitFor(() => {
      expect(result.current.allInstances).toHaveLength(1)
    })

    expect(mockedApi.getLocalCrossSeedMatches).not.toHaveBeenCalled()
    expect(result.current.matchingTorrents).toEqual([])
    expect(result.current.pendingQueryCount).toBe(0)
  })

  it("calls neither endpoint and returns empty instances when disabled", async () => {
    const { result } = renderHook(
      () => useLocalCrossSeedMatches(2, makeTorrent({ hash: "h" }), false),
      { wrapper: makeWrapper() }
    )

    // Give both queries a chance to fire before asserting they stayed idle.
    await waitFor(() => {
      expect(result.current.isLoadingMatches).toBe(false)
    })

    expect(mockedApi.getInstances).not.toHaveBeenCalled()
    expect(mockedApi.getLocalCrossSeedMatches).not.toHaveBeenCalled()
    expect(result.current.allInstances).toEqual([])
    expect(result.current.matchingTorrents).toEqual([])
  })

  it("returns an empty match list when the backend resolves to no matches", async () => {
    mockedApi.getLocalCrossSeedMatches.mockResolvedValue([])

    const { result } = renderHook(
      () => useLocalCrossSeedMatches(2, makeTorrent({ hash: "h" }), true),
      { wrapper: makeWrapper() }
    )

    await waitFor(() => {
      expect(result.current.pendingQueryCount).toBe(0)
    })

    expect(mockedApi.getLocalCrossSeedMatches).toHaveBeenCalled()
    expect(result.current.matchingTorrents).toEqual([])
  })

  it("settles without throwing and keeps matches empty when the matches query rejects", async () => {
    mockedApi.getLocalCrossSeedMatches.mockRejectedValue(new Error("boom 500"))

    const { result } = renderHook(
      () => useLocalCrossSeedMatches(2, makeTorrent({ hash: "h" }), true),
      { wrapper: makeWrapper() }
    )

    await waitFor(() => {
      expect(result.current.pendingQueryCount).toBe(0)
    })

    expect(result.current.matchingTorrents).toEqual([])
    // Instances still load fine even though the matches query errored.
    expect(result.current.allInstances).toEqual([{ id: 2, name: "seedbox" }])
  })

  it("falls back to an empty instances list when getInstances rejects", async () => {
    mockedApi.getInstances.mockRejectedValue(new Error("instances down"))

    const { result } = renderHook(
      () => useLocalCrossSeedMatches(2, makeTorrent({ hash: "h" }), true),
      { wrapper: makeWrapper() }
    )

    await waitFor(() => {
      expect(result.current.isLoadingInstances).toBe(false)
    })

    expect(result.current.allInstances).toEqual([])
    // The matches query is independent and still resolves.
    await waitFor(() => {
      expect(result.current.matchingTorrents).toHaveLength(1)
    })
  })
})
