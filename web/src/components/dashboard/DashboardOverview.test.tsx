/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { createMemoryHistory, createRootRoute, createRouter, RouterContextProvider } from "@tanstack/react-router"
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { HttpResponse, http } from "msw"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import "@/i18n"
import { DashboardOverview } from "@/components/dashboard/DashboardOverview"
import { TooltipProvider } from "@/components/ui/tooltip"
import { readRaw } from "@/lib/client-settings"
import type { DashboardInstanceStats } from "@/lib/dashboard-overview"
import { formatBytes } from "@/lib/utils"
import { server } from "@/test/msw/server"
import type { InstanceResponse, ServerState, TransferHistoryInstance, TransferHistoryRow } from "@/types"

const mocks = vi.hoisted(() => {
  const t = (key: string | string[], options?: Record<string, unknown>) => {
    const name = Array.isArray(key) ? key[0] : key
    const values = Object.entries(options ?? {})
    return values.length > 0 ? `${name} ${values.map(([field, value]) => `${field}=${String(value)}`).join(" ")}` : name
  }
  return {
    i18n: { t, i18n: { resolvedLanguage: "en", language: "en" } },
    incognitoMode: [false, () => {}] as [boolean, () => void],
    toastError: vi.fn(),
  }
})

vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => mocks.i18n,
}))
vi.mock("sonner", async (importOriginal) => ({
  ...await importOriginal<typeof import("sonner")>(),
  toast: { error: mocks.toastError },
}))
vi.mock("@/lib/incognito", async (importOriginal) => ({
  ...await importOriginal<typeof import("@/lib/incognito")>(),
  useIncognitoMode: () => mocks.incognitoMode,
}))

const HOUR = 3_600_000
const GiB = 1024 ** 3

let statuses: TransferHistoryInstance[]
let answer: (instanceId: number) => TransferHistoryRow[]
let transferRequests: number[]
let toggles: number[]
let toggleFails: boolean
let desktop: boolean

function hourRow(group: string, hoursAgo: number, uploaded: number, downloaded: number, extra: Partial<TransferHistoryRow> = {}): TransferHistoryRow {
  const start = Math.floor(Date.now() / HOUR) * HOUR - hoursAgo * HOUR
  return { group, start: new Date(start).toISOString(), width: "hour", gapSeconds: 0, uploaded, downloaded, statisticsCrossSeedUploaded: 0, ...extra }
}

function recorded(instanceId: number, hoursAgo: number): TransferHistoryInstance {
  return { instanceId, name: `instance ${instanceId}`, firstRecordedAt: new Date(Date.now() - hoursAgo * HOUR).toISOString() }
}

function stats(id: number, extra: Partial<DashboardInstanceStats> = {}, instance: Partial<InstanceResponse> = {}, serverState: Partial<ServerState> = {}): DashboardInstanceStats {
  return {
    instance: { id, name: `instance ${id}`, host: `http://qbit-${id}.example.test:8080`, isActive: true, connected: true, hasDecryptionError: false, connectionStatus: "connected", sortOrder: id, ...instance } as InstanceResponse,
    stats: { total: 10, downloading: 1, seeding: 9, paused: 0, error: 0, totalUploadSpeed: 2048, totalDownloadSpeed: 1024, totalSize: 100 * GiB, totalSeedingSize: 90 * GiB, uniqueSize: 80 * GiB, uniqueSeedingSize: 70 * GiB },
    serverState: { connection_status: "connected", free_space_on_disk: 500 * GiB, total_peer_connections: 12, last_external_address_v4: "203.0.113.7", ...serverState } as ServerState,
    torrentCounts: { status: { active: 3 }, categories: {}, tags: {}, trackers: {}, total: 10 },
    appInfo: { version: "v5.1.0" } as DashboardInstanceStats["appInfo"],
    altSpeedEnabled: false,
    isLoading: false,
    error: null,
    streamConnected: true,
    streamError: null,
    hasLiveDashboardStatsPayload: true,
    isDashboardStatsFallbackActive: false,
    cacheMetadata: null,
    instanceMeta: null,
    ...extra,
  }
}

function renderOverview(statsData: DashboardInstanceStats[]) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const router = createRouter({ routeTree: createRootRoute(), history: createMemoryHistory() })
  const view = render(
    <RouterContextProvider router={router}>
      <QueryClientProvider client={queryClient}>
        <TooltipProvider>
          <DashboardOverview statsData={statsData} />
        </TooltipProvider>
      </QueryClientProvider>
    </RouterContextProvider>
  )
  return { ...view, router }
}

beforeEach(() => {
  statuses = [recorded(1, 48), recorded(2, 48)]
  answer = () => []
  transferRequests = []
  toggles = []
  toggleFails = false
  mocks.toastError.mockReset()
  desktop = false
  mocks.incognitoMode = [false, () => {}]
  vi.stubGlobal("ResizeObserver", class {
    observe() {}
    unobserve() {}
    disconnect() {}
  })
  vi.stubGlobal("matchMedia", (query: string) => ({
    matches: desktop && query.includes("min-width: 1024px"),
    media: query,
    addEventListener: () => {},
    removeEventListener: () => {},
  }))
  server.use(
    http.get("*/api/transfer-history/status", () => HttpResponse.json({ instances: statuses })),
    http.get("*/api/instances/:id/transfer-history", ({ params }) => {
      transferRequests.push(Number(params.id))
      return HttpResponse.json({ rows: answer(Number(params.id)) })
    }),
    http.post("*/api/instances/:id/alternative-speed-limits/toggle", ({ params }) => {
      toggles.push(Number(params.id))
      return toggleFails ? HttpResponse.json({ error: "qBittorrent rejected the request" }, { status: 500 }) : HttpResponse.json({ enabled: true })
    })
  )
})

afterEach(() => {
  cleanup()
  vi.useRealTimers()
  vi.unstubAllGlobals()
  localStorage.clear()
})

describe("DashboardOverview, 24 h", () => {
  it("gives the upload and the download of all instances in the captions", async () => {
    answer = id => id === 1
      ? [hourRow("a.test", 3, 6 * GiB, 1 * GiB, { statisticsCrossSeedUploaded: 2 * GiB }), hourRow("b.test", 2, 2 * GiB, 0)]
      : [hourRow("a.test", 1, 2 * GiB, 3 * GiB, { statisticsCrossSeedUploaded: 1 * GiB })]
    renderOverview([stats(1), stats(2)])

    await vi.waitFor(() => expect(screen.getByTestId("upload-caption").textContent).toBe(`stream.dayUploaded size=${formatBytes(10 * GiB)} percent=30%`))
    expect(screen.getByTestId("download-caption").textContent).toBe(`stream.dayDownloaded size=${formatBytes(4 * GiB)}`)
  })

  it("lists the five largest Tracker groups and Other, which add up to the upload, with a link to Statistics for 24 hours", async () => {
    answer = id => id === 1
      ? ["a", "b", "c", "d", "e", "f", "g"].map((name, index) => hourRow(`${name}.test`, 2, (10 - index) * GiB, 0))
      : [hourRow("a.test", 1, 5 * GiB, 0)]
    renderOverview([stats(1), stats(2)])

    const section = await screen.findByRole("region", { name: "trackers.title" })
    const rows = within(section).getAllByTestId("tracker-row").map(row => row.textContent)
    expect(rows).toEqual([
      `a.test${formatBytes(15 * GiB)}28%`,
      `b.test${formatBytes(9 * GiB)}17%`,
      `c.test${formatBytes(8 * GiB)}15%`,
      `d.test${formatBytes(7 * GiB)}13%`,
      `e.test${formatBytes(6 * GiB)}11%`,
      `statistics:chart.otherCount groups=2${formatBytes(9 * GiB)}17%`,
    ])
    expect(within(section).getByText("trackers.statistics").closest("a")?.getAttribute("href")).toBe("/statistics?period=24h")
  })

  it("reads the transfer route once for each instance at open and again each 5 minutes", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true, toFake: ["setInterval", "clearInterval", "Date"] })
    renderOverview([stats(1), stats(2)])
    await vi.waitFor(() => expect(transferRequests.sort()).toEqual([1, 2]))

    await act(async () => {
      await vi.advanceTimersByTimeAsync(4 * 60_000)
    })
    expect(transferRequests).toHaveLength(2)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(60_000)
    })
    await vi.waitFor(() => expect(transferRequests.sort()).toEqual([1, 1, 2, 2]))
  })

  it("marks an hour that includes a Recording gap, and labels a recording younger than 24 hours as partial", async () => {
    statuses = [recorded(1, 5)]
    answer = () => [hourRow("a.test", 4, GiB, 0, { gapSeconds: 1800 }), hourRow("a.test", 2, GiB, 0)]
    renderOverview([stats(1)])

    await screen.findByText("statistics:partial")
    expect(screen.getAllByTestId("gap-mark")).toHaveLength(1)
    const gapTitles = [...document.querySelectorAll("rect > title")].map(title => title.textContent ?? "").filter(title => title.includes("statistics:chart.gap"))
    expect(gapTitles).toHaveLength(1)
  })

  // regression: #4141
  it("labels the hours in the timezone of the date and time preferences", async () => {
    localStorage.setItem("qui-datetime-preferences", JSON.stringify({ timezone: "Asia/Kolkata", timeFormat: "24h", dateFormat: "iso" }))
    answer = () => [hourRow("a.test", 2, GiB, 0)]
    renderOverview([stats(1)])

    await screen.findByRole("region", { name: "trackers.title" })
    const times = [...document.querySelectorAll("rect > title")].map(title => /time=(\S+)/.exec(title.textContent ?? "")?.[1])
    expect(times.length).toBeGreaterThan(0)
    expect(times.every(time => time?.endsWith(":30"))).toBe(true)
  })

  // regression: #4141
  it("opens with a saved timezone that the browser does not know, and labels the hours in the browser zone", async () => {
    localStorage.setItem("qui-datetime-preferences", JSON.stringify({ timezone: "Bogus/Zone", timeFormat: "24h", dateFormat: "iso" }))
    answer = () => [hourRow("a.test", 2, GiB, 0)]
    renderOverview([stats(1)])

    await screen.findByRole("region", { name: "trackers.title" })
    expect(document.querySelectorAll("rect > title").length).toBeGreaterThan(0)
  })

  it("has no partial label and no gap mark for a full day without a gap", async () => {
    answer = () => [hourRow("a.test", 4, GiB, 0)]
    renderOverview([stats(1)])

    await screen.findByRole("region", { name: "trackers.title" })
    expect(screen.queryByText("statistics:partial")).toBeNull()
    expect(screen.queryAllByTestId("gap-mark")).toHaveLength(0)
  })

  it("gives five instances a layer each and puts the other two in one Other layer", async () => {
    statuses = [1, 2, 3, 4, 5, 6, 7].map(id => recorded(id, 48))
    answer = () => [hourRow("a.test", 2, GiB, GiB)]
    renderOverview([1, 2, 3, 4, 5, 6, 7].map(id => stats(id)))

    await screen.findByRole("region", { name: "trackers.title" })
    expect(screen.getAllByTestId("chart-layer").map(layer => layer.getAttribute("data-layer"))).toEqual(["0", "1", "2", "3", "4", "5"])
  })
})

describe("DashboardOverview after a failed read", () => {
  it("shows the error in place of the numbers of the older period", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true, toFake: ["setInterval", "clearInterval", "Date"] })
    statuses = [recorded(1, 48)]
    answer = () => [hourRow("a.test", 2, GiB, 0)]
    renderOverview([stats(1)])
    await vi.waitFor(() => expect(screen.getByTestId("upload-caption").textContent).toContain(`size=${formatBytes(GiB)}`))

    server.use(http.get("*/api/instances/:id/transfer-history", () => HttpResponse.json({ error: "database is locked" }, { status: 500 })))
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5 * 60_000 + 60_000)
    })

    await screen.findByText(/^stream\.loadError/)
    expect(screen.getByTestId("upload-caption").textContent).toBe("")
    expect(screen.queryByText(/^stream\.peakHour/)).toBeNull()
    expect(screen.queryByRole("region", { name: "trackers.title" })).toBeNull()
  })
})

describe("DashboardOverview without history", () => {
  it("opens on Live, turns 24 h off with its reason, and shows no tracker list", async () => {
    statuses = []
    renderOverview([stats(1)])

    await vi.waitFor(() => expect(screen.getByRole("tab", { name: "stream.day" }).hasAttribute("disabled")).toBe(true))
    const day = screen.getByRole("tab", { name: "stream.day" })
    expect(screen.getByRole("tab", { name: "stream.live" }).getAttribute("aria-selected")).toBe("true")
    expect(screen.getByTestId("upload-caption").textContent).toBe("stream.liveSeeding seeding=9 peers=12")
    expect(screen.queryByText("trackers.title")).toBeNull()
    expect(transferRequests).toEqual([])

    fireEvent.click(day.parentElement!)
    expect((await screen.findAllByText("stream.noHistory")).length).toBeGreaterThan(0)
  })

  // regression: #4141
  it("opens on Live when no active instance recorded in the last 24 hours, also when a disabled one did", async () => {
    statuses = [recorded(1, 48), recorded(2, 48)]
    answer = id => id === 2 ? [hourRow("a.test", 2, GiB, 0)] : []
    renderOverview([stats(1)])

    await vi.waitFor(() => expect(transferRequests.sort()).toEqual([1, 2]))
    await vi.waitFor(() => expect(screen.getByRole("tab", { name: "stream.day" }).hasAttribute("disabled")).toBe(true))
    expect(screen.queryByRole("region", { name: "trackers.title" })).toBeNull()
  })

  it("opens on Live when the instances recorded nothing in the last 24 hours", async () => {
    statuses = [recorded(1, 24 * 30)]
    renderOverview([stats(1)])

    await vi.waitFor(() => expect(transferRequests).toEqual([1]))
    await vi.waitFor(() => expect(screen.getByRole("tab", { name: "stream.day" }).hasAttribute("disabled")).toBe(true))
    expect(screen.getByRole("tab", { name: "stream.live" }).getAttribute("aria-selected")).toBe("true")
    expect(screen.queryByRole("region", { name: "trackers.title" })).toBeNull()
  })
})

describe("DashboardOverview instances", () => {
  it("shows the cause of an offline instance in its line and lists it in Needs attention, with a firewalled instance", async () => {
    renderOverview([
      stats(1, { stats: null }, { connected: false, recentErrors: [{ id: 1, instanceId: 1, errorType: "connection", errorMessage: "connection refused", occurredAt: "" }] }),
      stats(2, {}, { connectionStatus: "firewalled" }),
      stats(3),
    ])

    const list = screen.getByRole("list", { name: "instances.title" })
    expect(within(list).getByText("offline.cause cause=Connection refused")).toBeTruthy()
    const attention = screen.getByTestId("attention-list")
    expect([...attention.querySelectorAll("li[data-tone]")].map(item => `${item.getAttribute("data-tone")} ${item.textContent}`)).toEqual([
      "red attention.offline name=instance 1",
      "yellow attention.firewalled name=instance 2",
    ])
  })

  it("lists a disk with less than 5% free of the free space and the Unique size as almost full", () => {
    renderOverview([
      stats(1, {}, {}, { free_space_on_disk: 4 * GiB }),
      stats(2, {}, {}, { free_space_on_disk: 5 * GiB }),
    ])

    const items = [...screen.getByTestId("attention-list").querySelectorAll("li[data-tone]")]
    expect(items.map(item => `${item.getAttribute("data-tone")} ${item.textContent}`)).toEqual([
      `red attention.lowDisk name=instance 1 free=${formatBytes(4 * GiB)}`,
    ])
  })

  // regression: #4141
  it("shows a free space that qBittorrent cannot read as unknown, not as an amount", async () => {
    renderOverview([stats(1, {}, {}, { free_space_on_disk: -1 })])

    expect(screen.queryByRole("region", { name: "storage.title" })).toBeNull()
    fireEvent.click(within(screen.getByRole("list", { name: "instances.title" })).getByText("instance 1"))
    const sheet = await screen.findByRole("dialog")
    expect(within(sheet).getByTestId("fact-free").textContent).toBe("details.unknowndetails.freeSpace")
  })

  // regression: #4141
  it("opens no bottom sheet for an instance that is still loading", () => {
    renderOverview([stats(1, { stats: null, isLoading: true })])

    fireEvent.click(within(screen.getByRole("list", { name: "instances.title" })).getByText("instance 1"))
    expect(screen.queryByRole("dialog")).toBeNull()
  })

  it("opens the bottom sheet with the details on a tap on a phone", async () => {
    renderOverview([stats(1), stats(2)])

    fireEvent.click(within(screen.getByRole("list", { name: "instances.title" })).getByText("instance 2"))
    const sheet = await screen.findByRole("dialog")
    expect(within(sheet).getByTestId("fact-host").textContent).toContain("http://qbit-2.example.test:8080")
    expect(within(sheet).getByTestId("fact-addresses").textContent).toContain("203.0.113.7")
    expect(within(sheet).getByText("details.openTorrents").closest("a")?.getAttribute("href")).toBe("/instances/2")
  })

  it("opens one detail band at a time on desktop and keeps the columns of the table", async () => {
    desktop = true
    answer = () => [hourRow("a.test", 2, GiB, 0)]
    renderOverview([stats(1), stats(2)])
    const table = await screen.findByRole("table", { name: "instances.title" })
    await within(table).findByText("instances.uploaded24h")

    fireEvent.click(within(table).getByText("instance 1"))
    expect(within(table).getAllByTestId("fact-host").map(fact => fact.textContent)).toEqual(["http://qbit-1.example.test:8080details.host"])
    fireEvent.click(within(table).getByText("instance 2"))
    expect(within(table).getAllByTestId("fact-host").map(fact => fact.textContent)).toEqual(["http://qbit-2.example.test:8080details.host"])
    expect(within(table).getAllByRole("columnheader")).toHaveLength(6)
  })

  // regression: #4141
  it("lets Enter on an issue link in a desktop row open the link, not the detail band", async () => {
    desktop = true
    renderOverview([stats(1, { torrentCounts: { status: { unregistered: 4 }, categories: {}, tags: {}, trackers: {}, total: 10 } })])
    const table = await screen.findByRole("table", { name: "instances.title" })
    const link = within(table).getByText("issues.unregistered count=4").closest("a")!

    expect(fireEvent.keyDown(link, { key: "Enter" })).toBe(true)
    expect(within(table).queryAllByTestId("fact-host")).toHaveLength(0)
  })

  it("gives an offline instance its cause and a link to the instance settings on desktop", async () => {
    desktop = true
    renderOverview([stats(1, { stats: null }, { connected: false })])

    fireEvent.click(await screen.findByText("instance 1"))
    expect(screen.getAllByText("offline.noAnswer")).toHaveLength(2)
    expect(screen.getByText("details.openSettings").closest("a")?.getAttribute("href")).toBe("/settings?tab=instances")
  })

  it("changes the alternative speed limits only after the confirmation", async () => {
    renderOverview([stats(1)])
    fireEvent.click(within(screen.getByRole("list", { name: "instances.title" })).getByText("instance 1"))
    const sheet = await screen.findByRole("dialog")

    fireEvent.click(within(sheet).getByRole("switch"))
    const confirm = await screen.findByRole("alertdialog")
    expect(toggles).toEqual([])
    fireEvent.click(within(confirm).getByText("altSpeedDialog.enable"))
    await vi.waitFor(() => expect(toggles).toEqual([1]))
  })

  // regression: #4141
  it("tells the user when qBittorrent rejects the change of the alternative speed limits", async () => {
    toggleFails = true
    renderOverview([stats(1)])
    fireEvent.click(within(screen.getByRole("list", { name: "instances.title" })).getByText("instance 1"))
    const sheet = await screen.findByRole("dialog")

    fireEvent.click(within(sheet).getByRole("switch"))
    fireEvent.click(within(await screen.findByRole("alertdialog")).getByText("altSpeedDialog.enable"))
    await vi.waitFor(() => expect(mocks.toastError).toHaveBeenCalledTimes(1))
    expect(String(mocks.toastError.mock.calls[0][0])).toMatch(/^common:errors\.altSpeedToggle message=/)
  })

  // regression: #4141
  it("leaves the cause out of the speed limit error in incognito mode", async () => {
    mocks.incognitoMode = [true, () => {}]
    toggleFails = true
    renderOverview([stats(1)])
    fireEvent.click(within(screen.getByRole("list", { name: "instances.title" })).getByText("instance 1"))
    const sheet = await screen.findByRole("dialog")

    fireEvent.click(within(sheet).getByRole("switch"))
    fireEvent.click(within(await screen.findByRole("alertdialog")).getByText("altSpeedDialog.enable"))
    await vi.waitFor(() => expect(mocks.toastError).toHaveBeenCalledTimes(1))
    expect(mocks.toastError.mock.calls[0][0]).toBe("common:errors.altSpeedToggleHidden")
  })

  it("opens the torrent list with the filter of an issue", async () => {
    const { router } = renderOverview([stats(1, { torrentCounts: { status: { unregistered: 4 }, categories: {}, tags: {}, trackers: {}, total: 10 } })])

    const link = within(screen.getByTestId("attention-list")).getByText("issues.unregistered count=4").closest("a")!
    fireEvent.click(link)
    expect(JSON.parse(readRaw("qui-filters-global") ?? "")).toEqual({ status: ["unregistered"], excludeStatus: [] })
    await vi.waitFor(() => expect(router.state.location.pathname).toBe("/instances/1"))
  })
})

describe("DashboardOverview library", () => {
  it("shows the Unique size as the total and the seeding size, with the calculating mark until the first build ends", () => {
    const building = stats(2)
    building.stats = { ...building.stats!, uniqueSize: undefined, uniqueSeedingSize: undefined }
    renderOverview([stats(1), building])

    expect(screen.getByTestId("library-total").textContent).toBe(`${formatBytes(180 * GiB)}library.totalSize uniqueSize.calculating`)
    expect(screen.getByTestId("library-seeding").textContent).toBe(`${formatBytes(160 * GiB)}library.seedingSize uniqueSize.calculating`)
    cleanup()

    renderOverview([stats(1), stats(2)])
    expect(screen.getByTestId("library-total").textContent).toBe(`${formatBytes(160 * GiB)}library.totalSize`)
    expect(screen.getByTestId("library-seeding").textContent).toBe(`${formatBytes(140 * GiB)}library.seedingSize`)
  })
})

describe("DashboardOverview in incognito mode", () => {
  it("shows no real tracker name, host, or external address", async () => {
    mocks.incognitoMode = [true, () => {}]
    answer = () => [hourRow("tracker-a.test", 2, GiB, 0)]
    renderOverview([
      stats(1),
      stats(2, { stats: null }, { connected: false, recentErrors: [{ id: 1, instanceId: 2, errorType: "connection", errorMessage: "dial tcp 198.51.100.9:8080: connection refused", occurredAt: "" }] }),
    ])

    await screen.findByRole("region", { name: "trackers.title" })
    fireEvent.click(within(screen.getByRole("list", { name: "instances.title" })).getByText("instance 1"))
    await screen.findByRole("dialog")
    const text = document.body.textContent ?? ""
    expect(text).not.toContain("tracker-a.test")
    expect(text).not.toContain("qbit-1.example.test")
    expect(text).not.toContain("qbit-2.example.test")
    expect(text).not.toContain("198.51.100.9")
    expect(text).not.toContain("203.0.113.7")
    expect(text).toContain("offline.hidden")
  })
})
