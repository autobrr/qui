/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, render } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import type { SortedPeer, Torrent, TorrentProperties, WebSeed } from "@/types"
import { makeTorrent } from "@/test/mockTorrent"
import { TorrentDetailsPanel } from "./TorrentDetailsPanel"

// Every mock returns a stable object: a fresh one per render loops the panel's effects.
const mocks = vi.hoisted(() => ({
  tabs: {
    general: {
      data: {
        row: { progress: 0.5 } as Torrent,
        properties: { seeds: 37, seeds_total: 91, peers: 12, peers_total: 48, save_path: "/data" } as TorrentProperties,
      },
      isLoading: false,
      error: null,
    },
    peers: {
      data: { peers: {}, sorted_peers: [{ key: "1" }, { key: "2" }, { key: "3" }] as SortedPeer[] },
      isLoading: false,
      error: null,
    },
    webseeds: { data: [{ url: "https://example.invalid/a" }, { url: "https://example.invalid/b" }] as WebSeed[], isLoading: false, error: null },
  } as Record<string, unknown>,
  // The real hook fetches nothing for a null hash, as before the 150 ms ready delay ends.
  noHash: { data: undefined, isLoading: false, error: null },
  capabilities: { data: {} },
  matches: { matchingTorrents: [], isLoadingMatches: false, allInstances: [] },
}))

vi.mock("@/hooks/useTorrentDetailTab", () => ({
  useTorrentDetailTab: (_instanceId: number, hash: string | null, tab: string) => hash === null ? mocks.noHash : mocks.tabs[tab],
}))
vi.mock("@/hooks/useInstanceCapabilities", () => ({ useInstanceCapabilities: () => mocks.capabilities }))
vi.mock("@/lib/cross-seed-utils", () => ({
  useLocalCrossSeedMatches: () => mocks.matches,
  isHardlinkManaged: () => false,
}))
vi.mock("@/lib/api", () => ({
  api: {
    getInstances: async () => [],
    getTorrentTrackers: async () => [],
  },
}))
vi.mock("./details", () => ({
  GeneralTabHorizontal: ({ torrent, properties }: { torrent: Torrent; properties: TorrentProperties }) => (
    <div data-testid="general">{`${torrent.progress} ${properties.seeds}/${properties.seeds_total}`}</div>
  ),
  CrossSeedTable: () => null,
  PeersTable: ({ peers }: { peers?: SortedPeer[] }) => <div data-testid="peers">{peers?.length}</div>,
  TorrentFileTable: () => null,
  TrackerContextMenu: () => null,
  TrackersTable: () => null,
  WebSeedsTable: ({ webseeds }: { webseeds?: WebSeed[] }) => <div data-testid="webseeds">{webseeds?.map(seed => seed.url).join(",")}</div>,
}))

beforeEach(() => {
  vi.stubGlobal("ResizeObserver", class {
    observe() {}
    unobserve() {}
    disconnect() {}
  })
})

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
  vi.unstubAllGlobals()
})

function renderPanel(props: { initialTab: string; layout?: "horizontal" | "vertical" }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <TorrentDetailsPanel instanceId={1} torrent={makeTorrent({ hash: "a", progress: 0 })} onClose={vi.fn()} {...props} />
    </QueryClientProvider>
  )
}

describe("TorrentDetailsPanel tabs", () => {
  it("shows the live row and the merged properties in the horizontal General layout", async () => {
    const { findByTestId } = renderPanel({ initialTab: "general", layout: "horizontal" })
    expect((await findByTestId("general")).textContent).toBe("0.5 37/91")
  })

  it("shows the merged properties in the inline General layout", async () => {
    const { findByText } = renderPanel({ initialTab: "general" })
    expect((await findByText("37")).textContent).toBe("37 / 91")
    expect((await findByText("12")).textContent).toBe("12 / 48")
  })

  it("shows the peers from the hook", async () => {
    const { findByTestId } = renderPanel({ initialTab: "peers", layout: "horizontal" })
    expect((await findByTestId("peers")).textContent).toBe("3")
  })

  it("shows the HTTP sources from the hook", async () => {
    const { findByTestId } = renderPanel({ initialTab: "webseeds", layout: "horizontal" })
    expect((await findByTestId("webseeds")).textContent).toBe("https://example.invalid/a,https://example.invalid/b")
  })

  it("moves to General when the torrent has no HTTP sources", async () => {
    const kept = mocks.tabs.webseeds
    mocks.tabs.webseeds = { data: [], isLoading: false, error: null }
    const { findByTestId } = renderPanel({ initialTab: "webseeds", layout: "horizontal" })
    expect((await findByTestId("general")).textContent).toBe("0.5 37/91")
    mocks.tabs.webseeds = kept
  })
})

describe("TorrentDetailsPanel cross-seed tab", () => {
  it("shows a failed refetch instead of the matches it kept", async () => {
    const kept = mocks.matches
    mocks.matches = {
      matchingTorrents: [makeTorrent({ hash: "b", name: "Kept.Match.2024.1080p.WEB-GRP" })],
      matchesError: new Error("overlap check failed"),
      isLoadingMatches: false,
      allInstances: [],
    } as unknown as typeof mocks.matches
    const { findByText, queryByText } = renderPanel({ initialTab: "crossseed" })

    expect(await findByText(/crossSeedTable\.loadFailed|Could not check for cross-seed matches/)).toBeTruthy()
    expect(queryByText("Kept.Match.2024.1080p.WEB-GRP")).toBeNull()
    mocks.matches = kept
  })
})
