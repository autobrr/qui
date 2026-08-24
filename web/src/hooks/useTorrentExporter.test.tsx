/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { act, cleanup, renderHook, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

vi.mock("@/lib/api", () => ({
  api: {
    exportTorrent: vi.fn(),
    exportTorrentsArchive: vi.fn(),
    getTorrents: vi.fn(),
  },
}))

vi.mock("sonner", () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
    info: vi.fn(),
    loading: vi.fn(() => "archive-toast"),
  },
}))

vi.mock("react-i18next", () => ({
  useTranslation: () => ({
    t: (key: string, opts?: Record<string, unknown>) =>
      opts && "count" in opts ? `${key}/${opts.count}` : key,
  }),
}))

vi.mock("@/lib/incognito", () => ({
  getLinuxIsoName: vi.fn((hash: string) => `linux-${hash}.iso`),
  getLinuxCategory: vi.fn((hash: string) => `linux-category-${hash}`),
}))

import { api } from "@/lib/api"
import { getLinuxIsoName } from "@/lib/incognito"
import { useTorrentExporter } from "@/hooks/useTorrentExporter"
import { makeTorrent } from "@/test/mockTorrent"
import { makeFilters } from "@/test/mockFilters"
import { toast } from "sonner"

const mockedApi = vi.mocked(api, true)
const mockedToast = vi.mocked(toast, true)

const INSTANCE_ID = 7

// Records the `download` attribute of every anchor that gets clicked so we can
// assert the filenames produced by buildDownloadName / ensureUniqueFilename.
let clickedDownloads: string[]
let clickSpy: ReturnType<typeof vi.spyOn>

function makeExportResult(filename: string | null) {
  return { blob: new Blob(["torrent-bytes"]), filename }
}

beforeEach(() => {
  clickedDownloads = []

  // jsdom does not implement these — stub them so triggerBrowserDownload runs.
  vi.stubGlobal("URL", {
    createObjectURL: vi.fn(() => "blob:mock-url"),
    revokeObjectURL: vi.fn(),
  })

  clickSpy = vi
    .spyOn(HTMLAnchorElement.prototype, "click")
    .mockImplementation(function (this: HTMLAnchorElement) {
      clickedDownloads.push(this.download)
    })
})

afterEach(() => {
  clickSpy.mockRestore()
  vi.unstubAllGlobals()
  vi.clearAllMocks()
})

afterEach(cleanup)

function setupHook(incognitoMode = false) {
  return renderHook(() =>
    useTorrentExporter({ instanceId: INSTANCE_ID, incognitoMode }))
}

describe("useTorrentExporter", () => {
  describe("isExporting state transitions", () => {
    it("flips false -> true -> false across an exportTorrents() call", async () => {
      let resolveExport!: (value: ReturnType<typeof makeExportResult>) => void
      mockedApi.exportTorrent.mockReturnValue(
        new Promise((resolve) => {
          resolveExport = resolve
        })
      )

      const { result } = setupHook()
      expect(result.current.isExporting).toBe(false)

      let pending: Promise<void>
      act(() => {
        pending = result.current.exportTorrents({
          hashes: ["aaa"],
          torrents: [makeTorrent({ hash: "aaa", name: "alpha" })],
          isAllSelected: false,
          totalSelected: 1,
        })
      })

      // In-flight: the awaited export promise has not resolved yet.
      await waitFor(() => expect(result.current.isExporting).toBe(true))

      await act(async () => {
        resolveExport(makeExportResult("alpha.torrent"))
        await pending
      })

      expect(result.current.isExporting).toBe(false)
    })
  })

  describe("explicit selection (isAllSelected=false)", () => {
    it("shows a loading toast while the archive request is pending", async () => {
      let resolveArchive!: (value: ReturnType<typeof makeExportResult>) => void
      mockedApi.exportTorrentsArchive.mockReturnValue(new Promise((resolve) => {
        resolveArchive = resolve
      }))
      const torrents = Array.from({ length: 11 }, (_, index) =>
        makeTorrent({ hash: `hash-${index}` }))

      const { result } = setupHook()
      let pending: Promise<void>
      act(() => {
        pending = result.current.exportTorrents({
          hashes: torrents.map(torrent => torrent.hash),
          torrents,
          isAllSelected: false,
          totalSelected: torrents.length,
        })
      })

      await waitFor(() => expect(mockedToast.loading).toHaveBeenCalledWith(
        "contextMenu.exportTorrents/11"
      ))
      expect(clickedDownloads).toHaveLength(0)

      await act(async () => {
        resolveArchive(makeExportResult("qui-torrents.zip"))
        await pending
      })
    })

    it("downloads one category-grouped archive when more than 10 torrents are selected", async () => {
      mockedApi.exportTorrentsArchive.mockResolvedValue(makeExportResult("qui-torrents.zip"))
      const torrents = Array.from({ length: 11 }, (_, index) => ({
        ...makeTorrent({ hash: `hash-${index}`, category: index < 6 ? "Movies" : "TV" }),
        instanceId: 8,
        instanceName: "Seedbox",
      }))

      const { result } = setupHook()
      await act(async () => {
        await result.current.exportTorrents({
          hashes: torrents.map(torrent => torrent.hash),
          torrents,
          isAllSelected: false,
          totalSelected: torrents.length,
        })
      })

      expect(mockedApi.exportTorrentsArchive).toHaveBeenCalledOnce()
      expect(mockedApi.exportTorrentsArchive).toHaveBeenCalledWith(torrents.map(torrent => ({
        instanceId: 8,
        instanceName: "Seedbox",
        hash: torrent.hash,
        category: torrent.category,
      })))
      expect(mockedApi.exportTorrent).not.toHaveBeenCalled()
      expect(clickedDownloads).toEqual(["qui-torrents.zip"])
      expect(mockedToast.success).toHaveBeenCalledWith(
        "creationTasks.toast.downloadStarted",
        { id: "archive-toast" }
      )
    })

    it("keeps individual downloads for exactly 10 torrents", async () => {
      mockedApi.exportTorrent.mockResolvedValue(makeExportResult("file.torrent"))
      const torrents = Array.from({ length: 10 }, (_, index) =>
        makeTorrent({ hash: `hash-${index}` }))

      const { result } = setupHook()
      await act(async () => {
        await result.current.exportTorrents({
          hashes: torrents.map(torrent => torrent.hash),
          torrents,
          isAllSelected: false,
          totalSelected: torrents.length,
        })
      })

      expect(mockedApi.exportTorrent).toHaveBeenCalledTimes(10)
      expect(mockedApi.exportTorrentsArchive).not.toHaveBeenCalled()
    })

    it("disguises archive filenames and categories in incognito mode", async () => {
      mockedApi.exportTorrentsArchive.mockResolvedValue(makeExportResult("qui-torrents.zip"))
      const torrents = Array.from({ length: 11 }, (_, index) =>
        makeTorrent({ hash: `hash-${index}`, category: "Secret" }))

      const { result } = setupHook(true)
      await act(async () => {
        await result.current.exportTorrents({
          hashes: torrents.map(torrent => torrent.hash),
          torrents,
          isAllSelected: false,
          totalSelected: torrents.length,
        })
      })

      expect(mockedApi.exportTorrentsArchive).toHaveBeenCalledWith(torrents.map(torrent => ({
        instanceId: INSTANCE_ID,
        instanceName: "",
        hash: torrent.hash,
        category: `linux-category-${torrent.hash}`,
        filename: `linux-${torrent.hash}.torrent`,
      })))
    })

    it("dedupes torrents in hash order, exports each, downloads per blob, and toasts plural", async () => {
      mockedApi.exportTorrent.mockResolvedValue(makeExportResult("file.torrent"))

      const torrents = [
        makeTorrent({ hash: "bbb", name: "beta" }),
        makeTorrent({ hash: "aaa", name: "alpha" }),
        makeTorrent({ hash: "ccc", name: "gamma" }),
      ]

      const { result } = setupHook()
      await act(async () => {
        await result.current.exportTorrents({
          // hash order requested differs from array order -> assert ordering
          hashes: ["aaa", "ccc"],
          torrents,
          isAllSelected: false,
          totalSelected: 2,
        })
      })

      expect(mockedApi.exportTorrent).toHaveBeenCalledTimes(2)
      expect(mockedApi.exportTorrent).toHaveBeenNthCalledWith(1, INSTANCE_ID, "aaa")
      expect(mockedApi.exportTorrent).toHaveBeenNthCalledWith(2, INSTANCE_ID, "ccc")
      expect(clickedDownloads).toHaveLength(2)
      expect(mockedToast.success).toHaveBeenCalledWith(
        "contextMenu.toast.torrentsExported/2"
      )
    })

    it("toasts the singular key for exactly one exported torrent", async () => {
      mockedApi.exportTorrent.mockResolvedValue(makeExportResult("only.torrent"))

      const { result } = setupHook()
      await act(async () => {
        await result.current.exportTorrents({
          hashes: ["aaa"],
          torrents: [makeTorrent({ hash: "aaa", name: "alpha" })],
          isAllSelected: false,
          totalSelected: 1,
        })
      })

      expect(mockedToast.success).toHaveBeenCalledWith("contextMenu.toast.torrentExported")
    })
  })

  describe("hash sanitization + early return", () => {
    it("returns without exporting or setting isExporting when sanitized hashes are empty", async () => {
      const { result } = setupHook()

      await act(async () => {
        await result.current.exportTorrents({
          hashes: ["", "", ""],
          torrents: [],
          isAllSelected: false,
          totalSelected: 0,
        })
      })

      expect(result.current.isExporting).toBe(false)
      expect(mockedApi.exportTorrent).not.toHaveBeenCalled()
      expect(mockedToast.info).not.toHaveBeenCalled()
      expect(mockedToast.success).not.toHaveBeenCalled()
    })

    it("collapses duplicate hashes via Set so each torrent exports once", async () => {
      mockedApi.exportTorrent.mockResolvedValue(makeExportResult("dup.torrent"))

      const { result } = setupHook()
      await act(async () => {
        await result.current.exportTorrents({
          hashes: ["aaa", "aaa", "aaa"],
          torrents: [makeTorrent({ hash: "aaa", name: "alpha" })],
          isAllSelected: false,
          totalSelected: 1,
        })
      })

      expect(mockedApi.exportTorrent).toHaveBeenCalledTimes(1)
    })
  })

  describe("excludeHashes", () => {
    it("skips excluded targets and reflects only non-excluded in the count", async () => {
      mockedApi.exportTorrent.mockResolvedValue(makeExportResult("x.torrent"))

      const torrents = [
        makeTorrent({ hash: "aaa", name: "alpha" }),
        makeTorrent({ hash: "bbb", name: "beta" }),
      ]

      const { result } = setupHook()
      await act(async () => {
        await result.current.exportTorrents({
          hashes: ["aaa", "bbb"],
          torrents,
          isAllSelected: false,
          totalSelected: 2,
          excludeHashes: ["bbb"],
        })
      })

      expect(mockedApi.exportTorrent).toHaveBeenCalledTimes(1)
      expect(mockedApi.exportTorrent).toHaveBeenCalledWith(INSTANCE_ID, "aaa")
      expect(mockedToast.success).toHaveBeenCalledWith("contextMenu.toast.torrentExported")
    })

    it("emits noTorrentsExported info toast when every target is excluded", async () => {
      const torrents = [makeTorrent({ hash: "aaa", name: "alpha" })]

      const { result } = setupHook()
      await act(async () => {
        await result.current.exportTorrents({
          hashes: ["aaa"],
          torrents,
          isAllSelected: false,
          totalSelected: 1,
          excludeHashes: ["aaa"],
        })
      })

      expect(mockedApi.exportTorrent).not.toHaveBeenCalled()
      expect(mockedToast.info).toHaveBeenCalledWith("contextMenu.toast.noTorrentsExported")
      expect(mockedToast.success).not.toHaveBeenCalled()
    })
  })

  describe("buildDownloadName", () => {
    it("appends .torrent when the server filename lacks the extension", async () => {
      mockedApi.exportTorrent.mockResolvedValue(makeExportResult("plainname"))

      const { result } = setupHook()
      await act(async () => {
        await result.current.exportTorrents({
          hashes: ["aaa"],
          torrents: [makeTorrent({ hash: "aaa", name: "alpha" })],
          isAllSelected: false,
          totalSelected: 1,
        })
      })

      expect(clickedDownloads).toEqual(["plainname.torrent"])
    })

    it("preserves an existing .torrent extension case-insensitively", async () => {
      mockedApi.exportTorrent.mockResolvedValue(makeExportResult("Movie.TORRENT"))

      const { result } = setupHook()
      await act(async () => {
        await result.current.exportTorrents({
          hashes: ["aaa"],
          torrents: [makeTorrent({ hash: "aaa", name: "alpha" })],
          isAllSelected: false,
          totalSelected: 1,
        })
      })

      expect(clickedDownloads).toEqual(["Movie.TORRENT"])
    })

    it("falls back to torrent.name then hash when the server filename is null", async () => {
      mockedApi.exportTorrent
        .mockResolvedValueOnce(makeExportResult(null))
        .mockResolvedValueOnce(makeExportResult(null))

      const { result } = setupHook()
      await act(async () => {
        await result.current.exportTorrents({
          hashes: ["aaa", "bbb"],
          torrents: [
            makeTorrent({ hash: "aaa", name: "named-torrent" }),
            makeTorrent({ hash: "bbb", name: "" }),
          ],
          isAllSelected: false,
          totalSelected: 2,
        })
      })

      expect(clickedDownloads).toEqual(["named-torrent.torrent", "bbb.torrent"])
    })

    it("uses getLinuxIsoName (stripping .iso) in incognito mode", async () => {
      mockedApi.exportTorrent.mockResolvedValue(makeExportResult("real-name.torrent"))

      const { result } = setupHook(true)
      await act(async () => {
        await result.current.exportTorrents({
          hashes: ["aaa"],
          torrents: [makeTorrent({ hash: "aaa", name: "secret" })],
          isAllSelected: false,
          totalSelected: 1,
        })
      })

      expect(getLinuxIsoName).toHaveBeenCalledWith("aaa")
      // getLinuxIsoName mock returns "linux-aaa.iso"; .iso stripped, .torrent added.
      expect(clickedDownloads).toEqual(["linux-aaa.torrent"])
    })
  })

  describe("ensureUniqueFilename", () => {
    it("disambiguates two targets resolving to the same download name", async () => {
      mockedApi.exportTorrent.mockResolvedValue(makeExportResult("dupe.torrent"))

      const { result } = setupHook()
      await act(async () => {
        await result.current.exportTorrents({
          hashes: ["aaa", "bbb"],
          torrents: [
            makeTorrent({ hash: "aaa", name: "alpha" }),
            makeTorrent({ hash: "bbb", name: "beta" }),
          ],
          isAllSelected: false,
          totalSelected: 2,
        })
      })

      expect(clickedDownloads).toEqual(["dupe.torrent", "dupe (2).torrent"])
    })
  })

  describe("isAllSelected pagination", () => {
    it("paginates getTorrents (limit 300), dedupes by hash, and honors the totalSelected cap", async () => {
      const filters = makeFilters({ status: ["downloading"] })

      mockedApi.getTorrents
        .mockResolvedValueOnce({
          torrents: [
            makeTorrent({ hash: "h1", name: "one" }),
            makeTorrent({ hash: "h2", name: "two" }),
          ],
          hasMore: true,
        } as never)
        .mockResolvedValueOnce({
          torrents: [
            // h2 duplicate across pages -> deduped
            makeTorrent({ hash: "h2", name: "two" }),
            makeTorrent({ hash: "h3", name: "three" }),
          ],
          hasMore: true,
        } as never)
      mockedApi.exportTorrent.mockResolvedValue(makeExportResult("p.torrent"))

      const { result } = setupHook()
      await act(async () => {
        await result.current.exportTorrents({
          hashes: [],
          torrents: [],
          isAllSelected: true,
          totalSelected: 3, // cap reached after h1,h2,h3 -> stops before more pages
          filters,
          search: "linux",
          sortField: "name",
          sortOrder: "asc",
        })
      })

      expect(mockedApi.getTorrents).toHaveBeenNthCalledWith(1, INSTANCE_ID, {
        page: 0,
        limit: 300,
        filters,
        search: "linux",
        sort: "name",
        order: "asc",
      })
      expect(mockedApi.getTorrents).toHaveBeenNthCalledWith(2, INSTANCE_ID, {
        page: 1,
        limit: 300,
        filters,
        search: "linux",
        sort: "name",
        order: "asc",
      })
      // Cap hit on page 1; no third page fetched.
      expect(mockedApi.getTorrents).toHaveBeenCalledTimes(2)
      expect(mockedApi.exportTorrent).toHaveBeenCalledTimes(3)
      const exportedHashes = mockedApi.exportTorrent.mock.calls.map((c) => c[1])
      expect(exportedHashes).toEqual(["h1", "h2", "h3"])
    })

    it("falls back to length===limit when hasMore is undefined and skips excluded hashes", async () => {
      // First page exactly fills the limit but hasMore is omitted -> fetch again.
      const fullPage = Array.from({ length: 300 }, (_, i) =>
        makeTorrent({ hash: `g${i}`, name: `iso-${i}` }))
      // Mark one as excluded to verify excludeSet skip during fetch.
      fullPage[0] = makeTorrent({ hash: "excluded", name: "skip-me" })

      mockedApi.getTorrents
        .mockResolvedValueOnce({ torrents: fullPage } as never)
        .mockResolvedValueOnce({ torrents: [], hasMore: false } as never)
      mockedApi.exportTorrentsArchive.mockResolvedValue(makeExportResult("qui-torrents.zip"))

      const { result } = setupHook()
      await act(async () => {
        await result.current.exportTorrents({
          hashes: [],
          torrents: [],
          isAllSelected: true,
          totalSelected: 0, // no cap
          excludeHashes: ["excluded"],
        })
      })

      expect(mockedApi.getTorrents).toHaveBeenCalledTimes(2)
      expect(mockedApi.exportTorrentsArchive).toHaveBeenCalledOnce()
      const exported = mockedApi.exportTorrentsArchive.mock.calls[0][0]
      expect(exported).toHaveLength(299)
      expect(exported.map(target => target.hash)).not.toContain("excluded")
    })

    it("emits noTorrentsFoundToExport when the all-selected fetch returns nothing", async () => {
      mockedApi.getTorrents.mockResolvedValue({ torrents: [], hasMore: false } as never)

      const { result } = setupHook()
      await act(async () => {
        await result.current.exportTorrents({
          hashes: [],
          torrents: [],
          isAllSelected: true,
          totalSelected: 5,
        })
      })

      expect(mockedApi.exportTorrent).not.toHaveBeenCalled()
      expect(mockedToast.info).toHaveBeenCalledWith(
        "contextMenu.toast.noTorrentsFoundToExport"
      )
    })
  })

  describe("error paths", () => {
    it("replaces the archive loading toast when the request fails", async () => {
      mockedApi.exportTorrentsArchive.mockRejectedValue(new Error("archive failed"))
      const torrents = Array.from({ length: 11 }, (_, index) =>
        makeTorrent({ hash: `hash-${index}` }))

      const { result } = setupHook()
      await act(async () => {
        await result.current.exportTorrents({
          hashes: torrents.map(torrent => torrent.hash),
          torrents,
          isAllSelected: false,
          totalSelected: torrents.length,
        })
      })

      expect(mockedToast.error).toHaveBeenCalledWith(
        "archive failed",
        { id: "archive-toast" }
      )
    })

    it("toasts error.message, resets isExporting, and triggers no download when exportTorrent rejects", async () => {
      mockedApi.exportTorrent.mockRejectedValue(new Error("export failed"))

      const { result } = setupHook()
      await act(async () => {
        await result.current.exportTorrents({
          hashes: ["aaa"],
          torrents: [makeTorrent({ hash: "aaa", name: "alpha" })],
          isAllSelected: false,
          totalSelected: 1,
        })
      })

      expect(mockedToast.error).toHaveBeenCalledWith("export failed")
      expect(result.current.isExporting).toBe(false)
      expect(clickedDownloads).toHaveLength(0)
      expect(mockedToast.success).not.toHaveBeenCalled()
    })

    it("falls back to the exportFailed key when a non-Error is thrown", async () => {
      mockedApi.exportTorrent.mockRejectedValue("boom")

      const { result } = setupHook()
      await act(async () => {
        await result.current.exportTorrents({
          hashes: ["aaa"],
          torrents: [makeTorrent({ hash: "aaa", name: "alpha" })],
          isAllSelected: false,
          totalSelected: 1,
        })
      })

      expect(mockedToast.error).toHaveBeenCalledWith("contextMenu.toast.exportFailed")
      expect(result.current.isExporting).toBe(false)
    })

    it("bubbles a getTorrents rejection in the all-selected path to the error toast", async () => {
      mockedApi.getTorrents.mockRejectedValue(new Error("fetch boom"))

      const { result } = setupHook()
      await act(async () => {
        await result.current.exportTorrents({
          hashes: [],
          torrents: [],
          isAllSelected: true,
          totalSelected: 10,
        })
      })

      expect(mockedToast.error).toHaveBeenCalledWith("fetch boom")
      expect(result.current.isExporting).toBe(false)
      expect(mockedApi.exportTorrent).not.toHaveBeenCalled()
    })
  })
})
