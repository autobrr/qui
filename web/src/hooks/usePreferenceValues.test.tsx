/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, renderHook } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

vi.mock("@/lib/api", () => ({
  api: {
    // The tests fill the cache, so a fetch never answers.
    getInstancePreferenceView: vi.fn(() => new Promise(() => {})),
    updateInstancePreferences: vi.fn(),
  },
}))

import {
  useAddTorrentPreferences,
  useGlobalMaxRatio,
  useListenPort,
  useQueueLimits,
  useRSSPreferences,
  useSubcategoriesEnabled
} from "@/hooks/usePreferenceValues"
import shownKeys from "@/lib/preference-catalog-keys.json"
import type { AppPreferences, InstanceCapabilities } from "@/types"

import hookSource from "./usePreferenceValues.ts?raw"

function wrapperWith(preferences?: Partial<AppPreferences>) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  if (preferences) {
    client.setQueryData(["instance-preferences", 1], preferences)
  }
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  )
}

function capabilities(supportsSubcategories: boolean, subcategoriesAlwaysEnabled: boolean) {
  return { supportsSubcategories, subcategoriesAlwaysEnabled } as InstanceCapabilities
}

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

// The hooks read the preferences view, which holds only the keys that the catalog shows.
it("reads only keys that the preferences view holds", () => {
  const read = [...new Set([...hookSource.matchAll(/preferences\?\.(\w+)/g)].map(match => match[1]))]

  expect(read.length).toBeGreaterThan(0)
  expect(read.filter(key => !shownKeys.includes(key))).toEqual([])
})

describe("useListenPort", () => {
  it("returns the listen port", () => {
    const { result } = renderHook(() => useListenPort(1), { wrapper: wrapperWith({ listen_port: 51413 }) })
    expect(result.current).toBe(51413)
  })

  it("returns undefined before the preferences load", () => {
    const { result } = renderHook(() => useListenPort(1), { wrapper: wrapperWith() })
    expect(result.current).toBeUndefined()
  })
})

describe("useGlobalMaxRatio", () => {
  it("returns the global ratio limit", () => {
    const { result } = renderHook(() => useGlobalMaxRatio(1), { wrapper: wrapperWith({ max_ratio: 1.5 }) })
    expect(result.current).toBe(1.5)
  })
})

describe("useSubcategoriesEnabled", () => {
  it("follows the use_subcategories preference when qBittorrent supports subcategories", () => {
    const on = renderHook(() => useSubcategoriesEnabled(1, capabilities(true, false), undefined), {
      wrapper: wrapperWith({ use_subcategories: true }),
    })
    expect(on.result.current).toBe(true)

    const off = renderHook(() => useSubcategoriesEnabled(1, capabilities(true, false), true), {
      wrapper: wrapperWith({ use_subcategories: false }),
    })
    expect(off.result.current).toBe(false)
  })

  it("is on when qBittorrent always enables subcategories, whatever the preference", () => {
    const { result } = renderHook(() => useSubcategoriesEnabled(1, capabilities(true, true), undefined), {
      wrapper: wrapperWith({ use_subcategories: false }),
    })
    expect(result.current).toBe(true)
  })

  it("is off when qBittorrent does not support subcategories", () => {
    const { result } = renderHook(() => useSubcategoriesEnabled(1, capabilities(false, true), true), {
      wrapper: wrapperWith({ use_subcategories: true }),
    })
    expect(result.current).toBe(false)
  })

  it("uses the fallback when the preference is missing", () => {
    const fallbackOn = renderHook(() => useSubcategoriesEnabled(1, capabilities(true, false), true), {
      wrapper: wrapperWith(),
    })
    expect(fallbackOn.result.current).toBe(true)

    const noFallback = renderHook(() => useSubcategoriesEnabled(1, capabilities(true, false), undefined), {
      wrapper: wrapperWith(),
    })
    expect(noFallback.result.current).toBe(false)
  })

  it("is off before the capabilities load", () => {
    const { result } = renderHook(() => useSubcategoriesEnabled(1, undefined, true), {
      wrapper: wrapperWith({ use_subcategories: true }),
    })
    expect(result.current).toBe(false)
  })
})

describe("useQueueLimits", () => {
  it("returns the queue limits when queueing is on", () => {
    const { result } = renderHook(() => useQueueLimits(1), {
      wrapper: wrapperWith({ queueing_enabled: true, max_active_downloads: 3, max_active_uploads: 5, max_active_torrents: 8 }),
    })
    expect(result.current).toEqual({ enabled: true, maxActiveDownloads: 3, maxActiveUploads: 5, maxActiveTorrents: 8 })
  })

  it("is off with zero limits before the preferences load", () => {
    const { result } = renderHook(() => useQueueLimits(1), { wrapper: wrapperWith() })
    expect(result.current).toEqual({ enabled: false, maxActiveDownloads: 0, maxActiveUploads: 0, maxActiveTorrents: 0 })
  })
})

describe("useAddTorrentPreferences", () => {
  it("returns the add torrent defaults and the auto-applied settings", () => {
    const { result } = renderHook(() => useAddTorrentPreferences(1), {
      wrapper: wrapperWith({
        auto_tmm_enabled: false,
        save_path: "/downloads",
        torrent_content_layout: "Subfolder",
        temp_path_enabled: true,
        temp_path: "/incomplete",
        add_trackers_enabled: true,
        add_trackers: "udp://tracker.example.invalid:1337/announce",
        excluded_file_names_enabled: true,
        excluded_file_names: "*.nfo",
        add_stopped_enabled: true,
      }),
    })
    expect(result.current).toEqual({
      loaded: true,
      defaults: {
        autoTMM: false,
        savePath: "/downloads",
        contentLayout: "Subfolder",
        tempPathEnabled: true,
        tempPath: "/incomplete",
        startPaused: true,
      },
      autoAddTrackers: true,
      excludedFileNamesEnabled: true,
      excludedFileNames: "*.nfo",
    })
  })

  it("does not report auto-added trackers when the tracker list is empty", () => {
    const { result } = renderHook(() => useAddTorrentPreferences(1), {
      wrapper: wrapperWith({ add_trackers_enabled: true, add_trackers: "" }),
    })
    expect(result.current.autoAddTrackers).toBe(false)
  })

  it("reads start paused from the key that qBittorrent 4.x sends", () => {
    const { result } = renderHook(() => useAddTorrentPreferences(1), {
      wrapper: wrapperWith({ start_paused_enabled: true }),
    })
    expect(result.current.defaults.startPaused).toBe(true)
  })

  it("returns the built-in defaults before the preferences load", () => {
    const { result } = renderHook(() => useAddTorrentPreferences(1), { wrapper: wrapperWith() })
    expect(result.current).toEqual({
      loaded: false,
      defaults: { autoTMM: true, savePath: "", contentLayout: "", tempPathEnabled: false, tempPath: "", startPaused: false },
      autoAddTrackers: false,
      excludedFileNamesEnabled: false,
      excludedFileNames: "",
    })
  })

  it("does not carry the defaults of one instance to another", () => {
    const wrapper = wrapperWith({ save_path: "/data/a", auto_tmm_enabled: false })
    const { result, rerender } = renderHook(({ id }) => useAddTorrentPreferences(id), {
      wrapper,
      initialProps: { id: 1 },
    })
    expect(result.current.defaults.savePath).toBe("/data/a")

    rerender({ id: 2 })
    expect(result.current.loaded).toBe(false)
    expect(result.current.defaults).toEqual({ autoTMM: true, savePath: "", contentLayout: "", tempPathEnabled: false, tempPath: "", startPaused: false })
  })
})

describe("useRSSPreferences", () => {
  it("returns the RSS switches", () => {
    const { result } = renderHook(() => useRSSPreferences(1), {
      wrapper: wrapperWith({
        rss_processing_enabled: false,
        rss_auto_downloading_enabled: false,
      }),
    })
    expect(result.current).toEqual({
      processingEnabled: false,
      autoDownloadingEnabled: false,
    })
  })

  it("treats RSS as on before the preferences load, so no banner flashes", () => {
    const { result } = renderHook(() => useRSSPreferences(1), { wrapper: wrapperWith() })
    expect(result.current).toEqual({
      processingEnabled: true,
      autoDownloadingEnabled: true,
    })
  })
})
