/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import type { SortedPeer, Torrent, TorrentFile, TorrentProperties, TorrentTracker, WebSeed } from "@/types"
import "@/i18n"
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
    trackers: { data: [{ url: "https://tracker.example.invalid/announce" }] as TorrentTracker[], isLoading: false, error: null },
    peers: {
      data: { peers: {}, sorted_peers: [{ key: "1" }, { key: "2" }, { key: "3" }] as SortedPeer[] },
      isLoading: false,
      error: null,
    },
    content: {
      data: [{ index: 0, name: "Show.S01/a.mkv", size: 1, progress: 0.5, priority: 1 }, { index: 1, name: "Show.S01/b.mkv", size: 1, progress: 1, priority: 1 }] as TorrentFile[],
      isLoading: false,
      error: null,
    },
    webseeds: { data: [{ url: "https://example.invalid/a" }, { url: "https://example.invalid/b" }] as WebSeed[], isLoading: false, error: null },
  } as Record<string, unknown>,
  // The real hook fetches nothing for a null hash, as before the 150 ms ready delay ends.
  noHash: { data: undefined, isLoading: false, error: null },
  capabilities: { data: {} },
  matches: { matchingTorrents: [], isLoadingMatches: false, allInstances: [] },
  liveHashes: [] as (string | null)[],
}))

vi.mock("@/hooks/useTorrentDetailTab", () => ({
  useTorrentDetailTab: (_instanceId: number, hash: string | null, tab: string, liveHash?: string | null) => {
    if (tab === "general" || tab === "trackers") {
      mocks.liveHashes.push(liveHash ?? null)
    }
    return hash === null ? mocks.noHash : mocks.tabs[tab]
  },
}))
vi.mock("@/hooks/useInstanceCapabilities", () => ({ useInstanceCapabilities: () => mocks.capabilities }))
vi.mock("@/lib/cross-seed-utils", async (importOriginal) => ({
  ...await importOriginal<typeof import("@/lib/cross-seed-utils")>(),
  useLocalCrossSeedMatches: () => mocks.matches,
  isHardlinkManaged: () => false,
}))
vi.mock("@/lib/api", () => ({
  api: {
    getInstances: async () => [],
  },
}))
vi.mock("./details", () => ({
  GeneralTabHorizontal: ({ torrent, properties }: { torrent: Torrent; properties: TorrentProperties }) => (
    <div data-testid="general">{`${torrent.progress} ${properties.seeds}/${properties.seeds_total}`}</div>
  ),
  CrossSeedTable: () => null,
  PeersTable: ({ peers }: { peers?: SortedPeer[] }) => <div data-testid="peers">{peers?.length}</div>,
  TorrentFileTable: ({ files }: { files: TorrentFile[] }) => <div data-testid="content">{files.map(file => file.name).join(",")}</div>,
  TrackerContextMenu: () => null,
  TrackersTable: ({ trackers }: { trackers?: TorrentTracker[] }) => <div data-testid="trackers">{trackers?.map(tracker => tracker.url).join(",")}</div>,
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

function renderPanel(props: { initialTab: string; layout: "horizontal" | "mobile"; onNavigateToTorrent?: (instanceId: number, hash: string) => void }) {
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

  it("shows the merged properties in the mobile General layout", async () => {
    const { findByText } = renderPanel({ initialTab: "general", layout: "mobile" })
    expect((await findByText("37")).textContent).toBe("37 / 91")
    expect((await findByText("12")).textContent).toBe("12 / 48")
  })

  it("shows the trackers from the hook", async () => {
    const { findByTestId } = renderPanel({ initialTab: "trackers", layout: "horizontal" })
    expect((await findByTestId("trackers")).textContent).toBe("https://tracker.example.invalid/announce")
  })

  it("shows the peers from the hook", async () => {
    const { findByTestId } = renderPanel({ initialTab: "peers", layout: "horizontal" })
    expect((await findByTestId("peers")).textContent).toBe("3")
  })

  it("shows the files from the hook", async () => {
    const { findByTestId } = renderPanel({ initialTab: "content", layout: "horizontal" })
    expect((await findByTestId("content")).textContent).toBe("Show.S01/a.mkv,Show.S01/b.mkv")
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

describe("TorrentDetailsPanel stream subscription", () => {
  it("moves the live row from one torrent to the next with no gap", async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const view = (hash: string) => (
      <QueryClientProvider client={client}>
        <TorrentDetailsPanel instanceId={1} torrent={makeTorrent({ hash })} layout="horizontal" onClose={vi.fn()} />
      </QueryClientProvider>
    )
    mocks.liveHashes.length = 0
    const { rerender } = render(view("a"))
    // The live row does not wait for the 150 ms ready delay.
    expect(mocks.liveHashes).toEqual(expect.arrayContaining(["a"]))
    expect(mocks.liveHashes).not.toContain(null)

    rerender(view("b"))
    await waitFor(() => expect(mocks.liveHashes).toContain("b"))
    expect(mocks.liveHashes).not.toContain(null)
    client.clear()
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
    const { findByText, queryByText } = renderPanel({ initialTab: "crossseed", layout: "mobile" })

    expect(await findByText(/crossSeedTable\.loadFailed|Could not check for cross-seed matches/)).toBeTruthy()
    expect(queryByText("Kept.Match.2024.1080p.WEB-GRP")).toBeNull()
    mocks.matches = kept
  })
})

describe("TorrentDetailsPanel mobile layout", () => {
  it("shows five sections in the bar and no HTTP Sources section", async () => {
    const { findAllByRole } = renderPanel({ initialTab: "general", layout: "mobile" })
    const tabs = (await findAllByRole("tab")).map(tab => tab.textContent)
    expect(tabs).toEqual(["General", "Trackers", "Peers", "Content", "Cross-Seed"])
  })

  it("opens Trackers when the last section was HTTP Sources", async () => {
    const { findByRole } = renderPanel({ initialTab: "webseeds", layout: "mobile" })
    expect((await findByRole("tab", { selected: true })).textContent).toBe("Trackers")
  })

  it("shows the web seeds in Trackers only when the torrent has some", async () => {
    const { findByText, unmount } = renderPanel({ initialTab: "trackers", layout: "mobile" })
    expect(await findByText("https://example.invalid/a")).toBeTruthy()
    unmount()

    const kept = mocks.tabs.webseeds
    mocks.tabs.webseeds = { data: [], isLoading: false, error: null }
    const { findByText: findAgain, queryByText } = renderPanel({ initialTab: "trackers", layout: "mobile" })
    expect(await findAgain("tracker.example.invalid")).toBeTruthy()
    expect(queryByText("https://example.invalid/a")).toBeNull()
    expect(queryByText(/HTTP source/)).toBeNull()
    mocks.tabs.webseeds = kept
  })

  it("selects a cross-seed match on a tap, and opens it only from its open button", async () => {
    const kept = mocks.matches
    mocks.matches = {
      matchingTorrents: [{ ...makeTorrent({ hash: "b", name: "Other.Match.2024.1080p.WEB-GRP" }), instanceId: 2, instanceName: "Second", matchType: "release" }],
      isLoadingMatches: false,
      allInstances: [],
    } as unknown as typeof mocks.matches
    const onNavigateToTorrent = vi.fn()
    const { findByRole, getByRole, getByText, queryByText } = renderPanel({ initialTab: "crossseed", layout: "mobile", onNavigateToTorrent })

    const deleteMatches = await findByRole("button", { name: "Delete Matches (0)" })
    expect(deleteMatches.hasAttribute("disabled")).toBe(true)
    expect(queryByText("Delete This Torrent")).toBeNull()

    fireEvent.click(getByText(/Other\.\u200BMatch/))
    expect(getByRole("checkbox").getAttribute("aria-checked")).toBe("true")
    expect(deleteMatches.hasAttribute("disabled")).toBe(false)
    expect(onNavigateToTorrent).not.toHaveBeenCalled()

    fireEvent.click(getByRole("button", { name: "Open Other.Match.2024.1080p.WEB-GRP" }))
    expect(onNavigateToTorrent).toHaveBeenCalledWith(2, "b")
    expect(getByRole("checkbox").getAttribute("aria-checked")).toBe("true")
    mocks.matches = kept
  })
})
