/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

/**
 * Whole-table mount smoke test — the cross-PR regression tripwire for the
 * TorrentTableOptimized decomposition (issue #1963).
 *
 * It mounts the REAL component with the network/router/stream boundaries
 * mocked and a deterministic virtualizer, then asserts the behaviours most
 * likely to break while extracting seams: rows render, and the header
 * select-all toggles row selection. The external data/action hooks mocked
 * here are NOT what the decomposition refactors, so this harness stays stable
 * across the 10 PRs it guards. Real virtualized scrolling and column-filter
 * UI are intentionally left to per-seam unit tests + the manual smoke per PR.
 *
 * IMPORTANT: every mocked hook returns a STABLE singleton reference. Returning
 * fresh objects per render makes the table's effects re-fire forever (OOM).
 */

import { TorrentTableOptimized } from "@/components/torrents/TorrentTableOptimized"
import { TooltipProvider } from "@/components/ui/tooltip"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, within } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { makeTorrent } from "@/test/mockTorrent"

const torrents = [
  makeTorrent({ hash: "hash-aaa", name: "Alpha Release", state: "downloading", progress: 0.5 }),
  makeTorrent({ hash: "hash-bbb", name: "Bravo Release", state: "uploading", progress: 1 }),
  makeTorrent({ hash: "hash-ccc", name: "Charlie Release", state: "stalledUP", progress: 1 }),
]

// i18n: keep the real exports (i18n bootstrap needs initReactI18next), but
// override useTranslation with a stable passthrough translator.
vi.mock("react-i18next", async (importOriginal) => {
  const translation = {
    t: (key: string, opts?: { defaultValue?: string }) => opts?.defaultValue ?? key,
    i18n: { resolvedLanguage: "en", language: "en" },
  }
  return {
    ...(await importOriginal<typeof import("react-i18next")>()),
    useTranslation: () => translation,
  }
})

// Router: avoid needing a real router tree.
vi.mock("@tanstack/react-router", async (importOriginal) => {
  const search = {}
  const navigate = vi.fn()
  return {
    ...(await importOriginal<typeof import("@tanstack/react-router")>()),
    useSearch: () => search,
    useNavigate: () => navigate,
  }
})

// SyncStream: keep the pure error-code helper, stub the live connection.
vi.mock("@/contexts/SyncStreamContext", async (importOriginal) => {
  const streamState = {
    connected: false,
    error: null,
    lastMeta: undefined,
    retrying: false,
    nextRetryAt: undefined,
    retryAttempt: 0,
  }
  return {
    ...(await importOriginal<typeof import("@/contexts/SyncStreamContext")>()),
    useSyncStream: () => streamState,
  }
})

// Deterministic virtualizer: render all rows regardless of jsdom layout.
// Built lazily (on first call) so it reads `torrents` after module init, while
// still returning a stable singleton across renders.
vi.mock("@tanstack/react-virtual", () => {
  let virtualizer: Record<string, unknown> | undefined
  return {
    useVirtualizer: () => {
      if (!virtualizer) {
        const items = torrents.map((_, index) => ({ index, key: index, start: index * 40, size: 40 }))
        virtualizer = {
          getVirtualItems: () => items,
          getTotalSize: () => torrents.length * 40,
          measure: vi.fn(),
          scrollToOffset: vi.fn(),
          scrollToIndex: vi.fn(),
        }
      }
      return virtualizer
    },
  }
})

// Network boundary: any api.* call resolves to undefined (real hooks degrade
// gracefully via their ?? fallbacks).
vi.mock("@/lib/api", () => ({
  api: new Proxy({}, { get: () => vi.fn(() => Promise.resolve(undefined)) }),
}))

// Tracker query hooks subscribe to the activity stream (needs a provider);
// the table only reads their `.data`, so stub that directly with stable refs.
vi.mock("@/hooks/useTrackerIcons", () => {
  const result = { data: {} as Record<string, string> }
  return { useTrackerIcons: () => result }
})
vi.mock("@/hooks/useTrackerCustomizations", () => {
  const result = { data: [] as unknown[] }
  return { useTrackerCustomizations: () => result }
})

// The data hook that drives the table body. Built lazily (reads `torrents`
// after init) and cached so the reference is stable across renders.
vi.mock("@/hooks/useTorrentsList", () => {
  let result: Record<string, unknown> | undefined
  return {
    TORRENT_STREAM_POLL_INTERVAL_SECONDS: 2,
    useTorrentsList: () => {
      result ??= {
        torrents,
        totalCount: torrents.length,
        stats: {
          total: torrents.length,
          downloading: 1,
          seeding: 2,
          paused: 0,
          error: 0,
          totalDownloadSpeed: 0,
          totalUploadSpeed: 0,
          totalSize: 0,
        },
        counts: { status: {}, categories: {}, tags: {}, trackers: {} },
        categories: {},
        tags: [] as string[],
        trackerHealthSupported: false,
        serverState: null,
        capabilities: undefined,
        useSubcategories: false,
        isLoading: false,
        isCachedData: false,
        isStaleData: false,
        isLoadingMore: false,
        hasLoadedAll: true,
        loadMore: vi.fn(),
        streamConnected: false,
        streamMeta: undefined,
        isStreaming: false,
        streamError: null,
        streamRetrying: false,
        streamNextRetryAt: undefined,
        streamRetryAttempt: 0,
        isCrossSeedFiltering: false,
        isCrossInstanceEndpoint: false,
      }
      return result
    },
  }
})

// The action hook that drives dialog state (all dialogs closed).
vi.mock("@/hooks/useTorrentActions", () => {
  const noop = vi.fn()
  const actions = new Proxy(
    {
      blockCrossSeeds: false,
      deleteCrossSeeds: false,
      deleteFiles: false,
      isDeleteFilesLocked: false,
      pendingTmmEnable: false,
      isPending: false,
      contextHashes: [],
      contextTorrents: [],
    } as Record<string, unknown>,
    {
      // Unknown dialog flags read as falsy; unknown handlers read as no-ops.
      get(target, prop: string) {
        if (prop in target) return target[prop]
        if (prop.startsWith("show")) return false
        return noop
      },
    }
  )
  return {
    TORRENT_ACTIONS: { DELETE: "delete" },
    useTorrentActions: () => actions,
  }
})

afterEach(cleanup)

function renderTable() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>{children}</TooltipProvider>
    </QueryClientProvider>
  )
  return render(<TorrentTableOptimized instanceId={1} />, { wrapper })
}

describe("TorrentTableOptimized smoke", () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it("renders a row for each torrent", () => {
    const { container } = renderTable()
    expect(container.textContent).toContain("Alpha Release")
    expect(container.textContent).toContain("Bravo Release")
    expect(container.textContent).toContain("Charlie Release")
  })

  it("toggles row selection via the header select-all checkbox", () => {
    const { container } = renderTable()
    const checkboxes = container.querySelectorAll("[role=\"checkbox\"]")
    expect(checkboxes.length).toBeGreaterThan(0)
    const header = checkboxes[0]
    expect(header.getAttribute("aria-checked")).not.toBe("true")
    fireEvent.click(header)
    // Exclude the header itself so this asserts ROW checkboxes became checked,
    // not just the select-all control reflecting its own click.
    const checkedRowsAfter = within(container).getAllByRole("checkbox").filter(
      (cb) => cb !== header && cb.getAttribute("aria-checked") === "true"
    )
    expect(checkedRowsAfter.length).toBeGreaterThan(0)
  })
})
