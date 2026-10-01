/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import type { FilesystemCapabilities, Torrent } from "@/types"

// Every mock returns a stable object: a fresh one per render loops the panel's effects.
const mocks = vi.hoisted(() => {
  const empty = { data: undefined, isLoading: false }
  return {
    empty,
    queries: {
      instances: { ...empty, data: [] as unknown[] },
      "torrent-files": { ...empty, data: [{ index: 0, name: "Show.S01/BDMV/index.bdmv", size: 1, progress: 1, priority: 1 }] },
      "torrent-properties": { ...empty, data: { save_path: "/data" } },
    } as Record<string, unknown>,
    tab: ["content", () => {}],
    stream: { connected: true, error: null },
    matches: { matchingTorrents: [], isLoadingMatches: false, allInstances: [] },
    discScansEnabled: vi.fn(),
    discScans: { runsByPath: new Map(), runFor: () => undefined, start: { mutate: () => {}, reset: () => {} }, cancel: {} },
    translation: { t: (key: string) => key, i18n: { language: "en" } },
  }
})

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...await importOriginal<typeof import("@tanstack/react-query")>(),
  useQuery: ({ queryKey }: { queryKey: string[] }) => mocks.queries[queryKey[0]] ?? mocks.empty,
}))
vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => mocks.translation,
}))
vi.mock("@/hooks/usePersistedTabState", () => ({ usePersistedTabState: () => mocks.tab }))
vi.mock("@/hooks/useInstanceMetadata", () => ({ useInstanceMetadata: () => mocks.empty }))
vi.mock("@/hooks/useInstanceCapabilities", () => ({ useInstanceCapabilities: () => mocks.empty }))
vi.mock("@/contexts/SyncStreamContext", () => ({ useSyncStream: () => mocks.stream }))
vi.mock("@/lib/cross-seed-utils", () => ({
  isHardlinkManaged: () => false,
  useLocalCrossSeedMatches: () => mocks.matches,
}))
vi.mock("@/hooks/useDiscScans", () => ({
  useDiscScans: (_instanceId: number, _hash: string, _files: unknown, enabled: boolean) => {
    mocks.discScansEnabled(enabled)
    return mocks.discScans
  },
}))
vi.mock("./TorrentFileTree", () => ({
  TorrentFileSortBar: () => null,
  TorrentFileTree: (props: Record<string, unknown>) => (
    <div data-testid="file-tree">
      {["onDownloadFile", "onShowMediaInfo", "onShowDiscReport"].filter(name => props[name]).join(",")}
    </div>
  ),
}))

import { TorrentDetailsPanel } from "./TorrentDetailsPanel"

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
  vi.unstubAllGlobals()
})

const noCapabilities: FilesystemCapabilities = { read: false, identity: false, write: false, content: false }
const torrent = { hash: "abc", name: "Show.S01", state: "uploading", progress: 1 } as Torrent

const cases = [
  { name: "a local instance", hasLocalFilesystemAccess: true, capabilities: { read: true, identity: true, write: true, content: true }, canReadContent: true },
  { name: "a remote instance", hasLocalFilesystemAccess: false, capabilities: { ...noCapabilities, read: true }, canReadContent: false },
  // The setting is off here, so only the capability can offer the actions.
  { name: "a remote instance with the content capability", hasLocalFilesystemAccess: false, capabilities: { ...noCapabilities, read: true, content: true }, canReadContent: true },
]

describe("TorrentDetailsPanel content actions", () => {
  it.each(cases)("offer download, MediaInfo, and Disc reports from the content capability for $name", async ({ hasLocalFilesystemAccess, capabilities, canReadContent }) => {
    vi.stubGlobal("ResizeObserver", class {
      observe() {}
      disconnect() {}
    })
    mocks.queries.instances = { ...mocks.empty, data: [{ id: 1, hasLocalFilesystemAccess, capabilities }] }
    render(
      <QueryClientProvider client={new QueryClient()}>
        <TorrentDetailsPanel instanceId={1} torrent={torrent} />
      </QueryClientProvider>
    )

    const tree = await screen.findByTestId("file-tree")
    expect(tree.textContent).toBe(canReadContent ? "onDownloadFile,onShowMediaInfo,onShowDiscReport" : "")
    expect(mocks.discScansEnabled).toHaveBeenLastCalledWith(canReadContent)
  })
})
