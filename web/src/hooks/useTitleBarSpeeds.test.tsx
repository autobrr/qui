/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, renderHook, waitFor } from "@testing-library/react"
import { afterEach, expect, it, vi } from "vitest"
import type { ReactNode } from "react"
import type { TorrentStreamPayload } from "@/types"

const mocks = vi.hoisted(() => ({
  streamState: {
    connected: true,
    initialized: true,
    dataStalled: false,
    error: null,
    retrying: false,
    retryAttempt: 0,
  },
  getTransferInfo: vi.fn(),
  onStreamMessage: undefined as ((payload: TorrentStreamPayload) => void) | undefined,
  visibility: { isHidden: false, isHiddenDelayed: false, isVisible: true },
}))

vi.mock("@/contexts/SyncStreamContext", () => ({
  useSyncStream: (_params: unknown, options: { onMessage: (payload: TorrentStreamPayload) => void }) => {
    mocks.onStreamMessage = options.onMessage
    return mocks.streamState
  },
}))
vi.mock("@/hooks/useDelayedVisibility", () => ({ useDelayedVisibility: () => mocks.visibility }))
vi.mock("@/hooks/useRouteTitle", () => ({ useRouteTitle: () => "Torrents" }))
vi.mock("@/lib/api", () => ({ api: { getTransferInfo: mocks.getTransferInfo } }))
vi.mock("@/lib/speedUnits", () => ({
  useSpeedUnits: () => ["bytes", vi.fn()],
  formatSpeedWithUnit: (speed: number) => `${speed} B/s`,
}))
vi.mock("@/lib/spreadsheet-disguise", () => ({
  isSpreadsheetDisguiseActive: () => false,
  spreadsheetDocumentTitle: () => "Spreadsheet",
  useSpreadsheetDisguise: () => false,
}))

import { useTitleBarSpeeds } from "@/hooks/useTitleBarSpeeds"

afterEach(() => {
  cleanup()
  mocks.streamState.connected = true
  mocks.streamState.error = null
  mocks.streamState.dataStalled = false
  mocks.onStreamMessage = undefined
  mocks.visibility.isHidden = false
  mocks.visibility.isHiddenDelayed = false
  mocks.visibility.isVisible = true
  mocks.getTransferInfo.mockReset()
})

it("keeps visible foreground speeds in the title while stalled REST polling starts", async () => {
  mocks.getTransferInfo.mockImplementation(() => new Promise(() => undefined))
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  )
  const { rerender, unmount } = renderHook(() => useTitleBarSpeeds({
    mode: "instance",
    instanceId: 1,
    instanceName: "Example",
    foregroundSpeeds: { dl: 5, up: 6 },
  }), { wrapper })

  expect(document.title).toBe("D: 5 B/s U: 6 B/s | Example")
  mocks.streamState.dataStalled = true
  rerender()

  expect(document.title).toBe("D: 5 B/s U: 6 B/s | Example")
  await waitFor(() => expect(mocks.getTransferInfo).toHaveBeenCalledWith(1))
  unmount()
  client.clear()
})

it("keeps newer foreground speeds until a post-stall REST response arrives", async () => {
  let resolveFetch: (value: { dl_info_speed: number; up_info_speed: number }) => void = () => {}
  mocks.getTransferInfo.mockImplementation(() => new Promise(resolve => { resolveFetch = resolve }))

  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  client.setQueryData(["transfer-info", 1], { dl_info_speed: 1, up_info_speed: 2 })
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  )
  const { rerender, unmount } = renderHook(() => useTitleBarSpeeds({
    mode: "instance",
    instanceId: 1,
    instanceName: "Example",
    foregroundSpeeds: { dl: 5, up: 6 },
  }), { wrapper })

  expect(document.title).toBe("D: 5 B/s U: 6 B/s | Example")
  mocks.streamState.dataStalled = true
  rerender()

  expect(document.title).toBe("D: 5 B/s U: 6 B/s | Example")
  await waitFor(() => expect(mocks.getTransferInfo).toHaveBeenCalledWith(1))

  resolveFetch({ dl_info_speed: 7, up_info_speed: 8 })
  await waitFor(() => expect(document.title).toBe("D: 7 B/s U: 8 B/s | Example"))
  unmount()
  client.clear()
})

it("clears cached speeds during a stall without foreground or fresh REST data", () => {
  mocks.getTransferInfo.mockImplementation(() => new Promise(() => undefined))
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  client.setQueryData(["transfer-info", 1], { dl_info_speed: 1, up_info_speed: 2 })
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  )
  const { rerender, unmount } = renderHook(() => useTitleBarSpeeds({
    mode: "instance",
    instanceId: 1,
    instanceName: "Example",
  }), { wrapper })

  expect(document.title).toBe("D: 1 B/s U: 2 B/s | Example")
  mocks.streamState.dataStalled = true
  rerender()

  expect(document.title).toBe("Torrents")
  unmount()
  client.clear()
})

it.each([
  { source: "foreground speeds", foregroundSpeeds: { dl: 5, up: 6 }, initialTitle: "D: 5 B/s U: 6 B/s | Example" },
  { source: "cached speeds", foregroundSpeeds: undefined, initialTitle: "D: 1 B/s U: 2 B/s | Example" },
])("uses fresh REST speeds after a visible disconnect with $source", async ({ foregroundSpeeds, initialTitle }) => {
  let resolveFetch: (value: { dl_info_speed: number; up_info_speed: number }) => void = () => {}
  mocks.getTransferInfo.mockImplementation(() => new Promise(resolve => { resolveFetch = resolve }))

  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  client.setQueryData(["transfer-info", 1], { dl_info_speed: 1, up_info_speed: 2 })
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  )
  const { rerender, unmount } = renderHook(() => useTitleBarSpeeds({
    mode: "instance",
    instanceId: 1,
    instanceName: "Example",
    foregroundSpeeds,
  }), { wrapper })

  expect(document.title).toBe(initialTitle)
  mocks.streamState.connected = false
  rerender()

  await waitFor(() => expect(mocks.getTransferInfo).toHaveBeenCalledWith(1))
  resolveFetch({ dl_info_speed: 7, up_info_speed: 8 })
  await waitFor(() => expect(document.title).toBe("D: 7 B/s U: 8 B/s | Example"))

  unmount()
  client.clear()
})

it.each([
  { visibility: "visible throughout", returnFromHidden: false },
  { visibility: "returned from a hidden tab", returnFromHidden: true },
])("uses a recovered stream speed after a stalled REST response when $visibility", async ({ returnFromHidden }) => {
  let resolveFetch: (value: { dl_info_speed: number; up_info_speed: number }) => void = () => {}
  mocks.getTransferInfo.mockImplementation(() => new Promise(resolve => { resolveFetch = resolve }))

  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  client.setQueryData(["transfer-info", 1], { dl_info_speed: 1, up_info_speed: 2 })
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  )
  const { rerender, unmount } = renderHook(() => useTitleBarSpeeds({
    mode: "instance",
    instanceId: 1,
    instanceName: "Example",
  }), { wrapper })

  expect(document.title).toBe("D: 1 B/s U: 2 B/s | Example")
  mocks.streamState.dataStalled = true
  rerender()
  await waitFor(() => expect(mocks.getTransferInfo).toHaveBeenCalledWith(1))

  resolveFetch({ dl_info_speed: 3, up_info_speed: 4 })
  await waitFor(() => expect(document.title).toBe("D: 3 B/s U: 4 B/s | Example"))

  if (returnFromHidden) {
    mocks.visibility.isHidden = true
    mocks.visibility.isHiddenDelayed = true
    mocks.visibility.isVisible = false
    rerender()

    mocks.visibility.isHidden = false
    mocks.visibility.isHiddenDelayed = false
    mocks.visibility.isVisible = true
    rerender()
  }

  mocks.streamState.dataStalled = false
  act(() => {
    mocks.onStreamMessage?.({ data: { serverState: { dl_info_speed: 7, up_info_speed: 8 } } } as TorrentStreamPayload)
    rerender()
  })
  await waitFor(() => expect(document.title).toBe("D: 7 B/s U: 8 B/s | Example"))

  unmount()
  client.clear()
})
