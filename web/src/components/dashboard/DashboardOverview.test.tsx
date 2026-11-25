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
import {
  _resetClientSettingsForTests,
  applyServerSettings,
  createDisplayProfile,
  markClientSettingsUnavailable,
  pickDisplayProfile,
  readClientSetting,
  readRaw,
  seedAndMarkReady,
  setDisplayProfileOwnSortAndFilters
} from "@/lib/client-settings"
import { DEFAULT_DISPLAY_PROFILE_ID } from "@/lib/display-profiles"
import type { DashboardInstanceStats } from "@/lib/dashboard-overview"
import { formatBytes } from "@/lib/utils"
import { server } from "@/test/msw/server"
import type { InstanceResponse, LiveSamples, ServerState, TrackerCustomization, TransferHistoryInstance, TransferHistoryRow } from "@/types"

const mocks = vi.hoisted(() => {
  const t = (key: string | string[], options?: Record<string, unknown>) => {
    const name = Array.isArray(key) ? key[0] : key
    const values = Object.entries(options ?? {})
    return values.length > 0 ? `${name} ${values.map(([field, value]) => `${field}=${String(value)}`).join(" ")}` : name
  }
  return {
    i18n: { t, i18n: { resolvedLanguage: "en", language: "en" } },
    incognitoMode: [false, () => {}] as [boolean, () => void],
    trackerIcons: { data: undefined } as { data?: Record<string, string> },
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
vi.mock("@/hooks/useTrackerIcons", () => ({ useTrackerIcons: () => mocks.trackerIcons }))
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
let liveSamples: LiveSamples
let customizations: TrackerCustomization[]

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
    disk: null,
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

function rowOf(table: HTMLElement, name: string): HTMLElement {
  return within(table).getByText(name).closest("tr")!
}

function visibleText(node: Element): string {
  const copy = node.cloneNode(true) as Element
  copy.querySelectorAll("[aria-hidden=true]").forEach(hidden => hidden.remove())
  return copy.textContent ?? ""
}

function openHosts(table: HTMLElement): string[] {
  return within(table).queryAllByTestId("fact-host").map(fact => fact.textContent ?? "")
}

function loadServerSnapshot(settings: Record<string, string> = {}) {
  act(() => {
    applyServerSettings(settings)
    seedAndMarkReady(settings)
  })
}

beforeEach(() => {
  _resetClientSettingsForTests()
  seedAndMarkReady({})
  statuses = [recorded(1, 48), recorded(2, 48)]
  answer = () => []
  transferRequests = []
  toggles = []
  toggleFails = false
  mocks.toastError.mockReset()
  desktop = false
  liveSamples = { now: new Date().toISOString(), instances: [] }
  mocks.incognitoMode = [false, () => {}]
  mocks.trackerIcons = { data: undefined }
  customizations = []
  vi.stubGlobal("matchMedia", (query: string) => ({
    matches: desktop && query.includes("min-width: 1024px"),
    media: query,
    addEventListener: () => {},
    removeEventListener: () => {},
  }))
  server.use(
    http.get("*/api/transfer-history/status", () => HttpResponse.json({ instances: statuses })),
    http.get("*/api/live-samples", () => HttpResponse.json(liveSamples)),
    http.get("*/api/tracker-customizations", () => HttpResponse.json(customizations)),
    http.put("*/api/client-settings", () => HttpResponse.json({})),
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
    const rows = within(section).getAllByTestId("tracker-row").map(visibleText)
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

  it("shows the tracker icon of each Tracker group, from a domain of the customization that names the group, and none for Other", async () => {
    mocks.trackerIcons = { data: { "a.test": "data:image/png;base64,YQ==", "b.test": "data:image/png;base64,Yg==" } }
    customizations = [{ id: 1, displayName: "Bee", domains: ["b-alt.test", "b.test"], createdAt: "", updatedAt: "" }]
    answer = id => id === 1
      ? ["a.test", "Bee", "c.test", "Public", "d.test", "e.test", "f.test"].map((group, index) => hourRow(group, 2, (10 - index) * GiB, 0))
      : []
    renderOverview([stats(1)])

    const section = await screen.findByRole("region", { name: "trackers.title" })
    const row = (name: string) => within(section).getByText(name).closest("li")!
    await vi.waitFor(() => expect(row("Bee").querySelector("img")?.getAttribute("src")).toBe("data:image/png;base64,Yg=="))
    expect(row("a.test").querySelector("img")?.getAttribute("src")).toBe("data:image/png;base64,YQ==")
    expect(row("c.test").querySelector("img")).toBeNull()
    expect(row("c.test").querySelector("span[aria-hidden=true]")?.textContent).toBe("C")
    expect(row("groups.public").querySelector("img, span[aria-hidden=true]")).toBeNull()
    expect(row("statistics:chart.otherCount groups=2").querySelector("img, span[aria-hidden=true]")).toBeNull()
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
    expect(screen.getByText("chart.gapLabel duration=30m")).toBeTruthy()
  })

  it("labels the hours in the timezone of the date and time preferences", async () => {
    localStorage.setItem("qui-datetime-preferences", JSON.stringify({ timezone: "Asia/Kolkata", timeFormat: "24h", dateFormat: "iso" }))
    answer = () => [hourRow("a.test", 2, GiB, 0)]
    renderOverview([stats(1)])

    await screen.findByRole("region", { name: "trackers.title" })
    const times = screen.getAllByText(/^\d{2}:\d{2}$/).map(time => time.textContent)
    expect(times).toHaveLength(3)
    expect(times.every(time => time?.endsWith(":30"))).toBe(true)
  })

  it("opens with a saved timezone that the browser does not know, and labels the hours in the browser zone", async () => {
    localStorage.setItem("qui-datetime-preferences", JSON.stringify({ timezone: "Bogus/Zone", timeFormat: "24h", dateFormat: "iso" }))
    answer = () => [hourRow("a.test", 2, GiB, 0)]
    renderOverview([stats(1)])

    await screen.findByRole("region", { name: "trackers.title" })
    expect(screen.getAllByText(/^\d{2}:\d{2}$/)).toHaveLength(3)
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

describe("DashboardOverview, Live", () => {
  it("opens on the view that the user picked last, after a remount", async () => {
    answer = () => [hourRow("a.test", 2, GiB, 0)]
    const { unmount } = renderOverview([stats(1)])
    await screen.findByTestId("tracker-row")
    expect(screen.getByRole("tab", { name: "stream.day" }).getAttribute("aria-selected")).toBe("true")

    fireEvent.mouseDown(screen.getByRole("tab", { name: "stream.live" }), { button: 0 })
    expect(readRaw("qui-dashboard-chart-view")).toBe(JSON.stringify("live"))
    unmount()

    renderOverview([stats(1)])
    expect(screen.getByRole("tab", { name: "stream.live" }).getAttribute("aria-selected")).toBe("true")
  })

  it("waits for the saved view from the server before it offers 24 h and Live, so that a tap is not lost", async () => {
    _resetClientSettingsForTests()
    answer = () => [hourRow("a.test", 2, GiB, 0)]
    renderOverview([stats(1)])
    await screen.findByTestId("tracker-row")
    expect(screen.getByRole("tab", { name: "stream.day" }).hasAttribute("disabled")).toBe(true)
    expect(screen.getByRole("tab", { name: "stream.live" }).hasAttribute("disabled")).toBe(true)

    loadServerSnapshot({ "qui-dashboard-chart-view": JSON.stringify("live") })
    const live = screen.getByRole("tab", { name: "stream.live" })
    expect(live.getAttribute("aria-selected")).toBe("true")
    expect(live.hasAttribute("disabled")).toBe(false)
    expect(screen.getByRole("tab", { name: "stream.day" }).hasAttribute("disabled")).toBe(false)
  })

  it("offers 24 h and Live with the local view when the saved settings cannot load", async () => {
    _resetClientSettingsForTests()
    localStorage.setItem("qui-dashboard-chart-view", JSON.stringify("live"))
    renderOverview([stats(1)])
    expect(screen.getByRole("tab", { name: "stream.live" }).hasAttribute("disabled")).toBe(true)

    act(() => markClientSettingsUnavailable())
    const live = screen.getByRole("tab", { name: "stream.live" })
    expect(live.hasAttribute("disabled")).toBe(false)
    expect(live.getAttribute("aria-selected")).toBe("true")
  })

  it("draws the samples of the server for the instances of the list", async () => {
    localStorage.setItem("qui-dashboard-chart-view", JSON.stringify("live"))
    const now = Date.now()
    const sample = (ago: number, up: number) => ({ time: new Date(now - ago * 1000).toISOString(), up, down: 0 })
    liveSamples = {
      now: new Date(now).toISOString(),
      instances: [
        { instanceId: 1, samples: [sample(5, 1024), sample(3, 4096), sample(1, 2048)] },
        { instanceId: 9, samples: [sample(3, 1024 ** 3), sample(1, 1024 ** 3)] },
      ],
    }
    renderOverview([stats(1)])

    await vi.waitFor(() => expect(screen.getAllByTestId("chart-layer").map(layer => layer.getAttribute("data-layer"))).toEqual(["0"]))
    expect(screen.getAllByText(/^stream.peakSpeed/).map(peak => peak.textContent)).toEqual(["stream.peakSpeed speed=4 KiB/s", "stream.peakSpeed speed=0 B/s"])
  })

  it("shows the error in place of the Live chart when a later read of the samples fails", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true, toFake: ["setInterval", "clearInterval", "Date"] })
    localStorage.setItem("qui-dashboard-chart-view", JSON.stringify("live"))
    const now = Date.now()
    liveSamples = { now: new Date(now).toISOString(), instances: [{ instanceId: 1, samples: [3, 1].map(ago => ({ time: new Date(now - ago * 1000).toISOString(), up: 1024, down: 0 })) }] }
    renderOverview([stats(1)])
    await vi.waitFor(() => expect(screen.getAllByTestId("chart-layer")).toHaveLength(1))

    server.use(http.get("*/api/live-samples", () => HttpResponse.json({ error: "qui is restarting" }, { status: 500 })))
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2000)
    })
    await vi.waitFor(() => expect(screen.getByText(/^stream.loadError/)).toBeTruthy())
    expect(screen.queryAllByTestId("chart-layer")).toEqual([])
    expect(screen.queryAllByText(/^stream.peakSpeed/)).toEqual([])
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
    expect(readRaw("qui-dashboard-chart-view")).toBeNull()
  })

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

  it("does not list a disk as almost full when qui only estimates its size", () => {
    renderOverview([
      stats(1, {}, {}, { free_space_on_disk: 4 * GiB }),
      stats(2, {}, {}, { free_space_on_disk: 5 * GiB }),
    ])

    expect(screen.queryByRole("region", { name: "attention.title" })).toBeNull()
  })

  it("warns only about the measured disk when a large library sits next to a mostly empty estimated disk", () => {
    const library = { ...stats(0).stats!, uniqueSize: 9 * 1024 * GiB }
    renderOverview([
      stats(1, { stats: library }, {}, { free_space_on_disk: 450 * GiB }),
      stats(2, { disk: { freeBytes: 40 * GiB, totalBytes: 1000 * GiB } }, {}, { free_space_on_disk: 40 * GiB }),
    ])

    const items = [...screen.getByTestId("attention-list").querySelectorAll("li[data-tone]")]
    expect(items.map(item => `${item.getAttribute("data-tone")} ${item.textContent}`)).toEqual([
      `red attention.lowDisk name=instance 2 free=${formatBytes(40 * GiB)}`,
    ])
    const storage = screen.getByRole("region", { name: "storage.title" })
    expect(within(storage).getByText("instance 1")).toBeTruthy()
    expect(within(storage).getByText("instance 2")).toBeTruthy()
    expect(within(storage).getByRole("button", { name: "fieldHelp.trigger" })).toBeTruthy()
  })

  it("judges two instances on one disk by the size of the disk, not by their own torrents", () => {
    const disk = { freeBytes: 60 * GiB, totalBytes: 2000 * GiB }
    const big = { ...stats(0).stats!, uniqueSize: 1900 * GiB }
    renderOverview([
      stats(1, { disk, stats: big }, {}, { free_space_on_disk: 60 * GiB }),
      stats(2, { disk }, {}, { free_space_on_disk: 60 * GiB }),
      stats(3, { disk: { freeBytes: 400 * GiB, totalBytes: 1000 * GiB } }, {}, { free_space_on_disk: 4 * GiB }),
    ])

    const items = [...screen.getByTestId("attention-list").querySelectorAll("li[data-tone]")]
    expect(items.map(item => item.textContent)).toEqual([
      `attention.lowDisk name=instance 1 free=${formatBytes(60 * GiB)}`,
      `attention.lowDisk name=instance 2 free=${formatBytes(60 * GiB)}`,
    ])
  })

  it("waits for the free space of qBittorrent before it warns about a measured disk", () => {
    const disk = { freeBytes: 10 * GiB, totalBytes: 1000 * GiB }
    renderOverview([
      stats(1, { disk }, {}, { free_space_on_disk: -1 }),
      stats(2, { disk }, {}, { free_space_on_disk: 10 * GiB }),
    ])

    const items = [...screen.getByTestId("attention-list").querySelectorAll("li[data-tone]")]
    expect(items.map(item => item.textContent)).toEqual([`attention.lowDisk name=instance 2 free=${formatBytes(10 * GiB)}`])
  })

  it("explains the Storage bars only while one of them is an estimate", () => {
    const disk = { freeBytes: 400 * GiB, totalBytes: 1000 * GiB }
    const measured = renderOverview([stats(1, { disk }), stats(2, { disk })])
    expect(within(screen.getByRole("region", { name: "storage.title" })).queryByRole("button", { name: "fieldHelp.trigger" })).toBeNull()
    measured.unmount()

    renderOverview([stats(1, { disk }), stats(2)])
    expect(within(screen.getByRole("region", { name: "storage.title" })).getByRole("button", { name: "fieldHelp.trigger" })).toBeTruthy()
  })

  it("leaves out Needs attention when nothing needs an action", () => {
    renderOverview([stats(1), stats(2)])

    expect(screen.getByRole("region", { name: "library.title" })).toBeTruthy()
    expect(screen.queryByRole("region", { name: "attention.title" })).toBeNull()
  })

  it("shows a free space that qBittorrent cannot read as unknown, not as an amount", async () => {
    renderOverview([stats(1, {}, {}, { free_space_on_disk: -1 })])

    expect(screen.queryByRole("region", { name: "storage.title" })).toBeNull()
    fireEvent.click(within(screen.getByRole("list", { name: "instances.title" })).getByText("instance 1"))
    const sheet = await screen.findByRole("dialog")
    expect(within(sheet).getByTestId("fact-free").textContent).toBe("details.unknowndetails.freeSpace")
  })

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

  it("keeps each detail band open on desktop until a click on its row, and keeps the columns of the table", async () => {
    desktop = true
    answer = () => [hourRow("a.test", 2, GiB, 0)]
    renderOverview([stats(1), stats(2)])
    const table = await screen.findByRole("table", { name: "instances.title" })
    await within(table).findByText("instances.uploaded24h")

    fireEvent.click(rowOf(table, "instance 1"))
    expect(openHosts(table)).toEqual(["http://qbit-1.example.test:8080details.host"])
    fireEvent.click(rowOf(table, "instance 2"))
    expect(openHosts(table)).toEqual(["http://qbit-1.example.test:8080details.host", "http://qbit-2.example.test:8080details.host"])
    fireEvent.click(rowOf(table, "instance 1"))
    expect(openHosts(table)).toEqual(["http://qbit-2.example.test:8080details.host"])
    expect(within(table).getAllByRole("columnheader")).toHaveLength(6)
  })

  it("opens the rows on desktop that the Display settings hold, and saves a row that the user opens", async () => {
    desktop = true
    loadServerSnapshot({ "qui-dashboard-open-instances": JSON.stringify([2]) })
    renderOverview([stats(1), stats(2)])
    const table = await screen.findByRole("table", { name: "instances.title" })
    expect(openHosts(table)).toEqual(["http://qbit-2.example.test:8080details.host"])

    fireEvent.click(rowOf(table, "instance 1"))
    expect(readRaw("qui-dashboard-open-instances")).toBe(JSON.stringify([2, 1]))
  })

  it("takes no click on a desktop row before the saved rows are read, so that the read does not close the row again", async () => {
    _resetClientSettingsForTests()
    desktop = true
    renderOverview([stats(1), stats(2)])
    const table = await screen.findByRole("table", { name: "instances.title" })

    fireEvent.click(rowOf(table, "instance 1"))
    expect(openHosts(table)).toEqual([])
    expect(within(table).getByRole("button", { name: "instances.expandAll" }).hasAttribute("disabled")).toBe(true)

    loadServerSnapshot({ "qui-dashboard-open-instances": JSON.stringify([2]) })
    expect(openHosts(table)).toEqual(["http://qbit-2.example.test:8080details.host"])
  })

  it("opens each row and closes each row with the button in the header of the desktop table", async () => {
    desktop = true
    renderOverview([stats(1), stats(2)])
    const table = await screen.findByRole("table", { name: "instances.title" })

    fireEvent.click(within(table).getByRole("button", { name: "instances.expandAll" }))
    expect(openHosts(table)).toHaveLength(2)
    fireEvent.click(within(table).getByRole("button", { name: "instances.collapseAll" }))
    expect(openHosts(table)).toEqual([])
  })

  it("links the name of a desktop row to the torrent list of the instance, and a click on it opens no detail band", async () => {
    desktop = true
    renderOverview([stats(1)])
    const table = await screen.findByRole("table", { name: "instances.title" })
    const name = within(table).getByText("instance 1")

    expect(name.closest("a")?.getAttribute("href")).toBe("/instances/1")
    fireEvent.click(name)
    expect(openHosts(table)).toEqual([])
  })

  it("shows the transfer of the last 24 hours and of all time in an open desktop row", async () => {
    desktop = true
    answer = () => [hourRow("a.test", 2, 3 * GiB, GiB)]
    renderOverview([stats(1, {}, {}, { alltime_ul: 10 * GiB, alltime_dl: 4 * GiB }), stats(2)])
    const table = await screen.findByRole("table", { name: "instances.title" })
    await within(table).findByText("instances.uploaded24h")
    fireEvent.click(within(table).getByRole("button", { name: "instances.expandAll" }))

    const [day, , emptyDay] = within(table).getAllByTestId("totals-day")
    expect(day.textContent).toBe(`details.last24hdetails.uploaded${formatBytes(3 * GiB)}details.downloaded${formatBytes(GiB)}details.ratio ratio=3.00`)
    expect(within(table).getAllByTestId("totals-all-time").map(row => row.textContent)).toEqual([
      `details.allTimedetails.uploaded${formatBytes(10 * GiB)}details.downloaded${formatBytes(4 * GiB)}details.ratio ratio=2.50`,
      "details.allTimedetails.uploaded0 Bdetails.downloaded0 Bdetails.ratio ratio=–",
    ])
    expect(emptyDay).toBeUndefined()
  })

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

  it("changes the alternative speed limits with one tap on the switch of the bottom sheet", async () => {
    renderOverview([stats(1)])
    fireEvent.click(within(screen.getByRole("list", { name: "instances.title" })).getByText("instance 1"))
    const sheet = await screen.findByRole("dialog")

    fireEvent.click(within(sheet).getByRole("switch"))
    await vi.waitFor(() => expect(toggles).toEqual([1]))
    expect(within(sheet).getByRole("switch").getAttribute("aria-checked")).toBe("true")
  })

  it("changes the alternative speed limits with one click on the turtle of a desktop row, and opens no detail band", async () => {
    desktop = true
    renderOverview([stats(1), stats(2, { altSpeedEnabled: true })])
    const table = await screen.findByRole("table", { name: "instances.title" })
    expect(within(table).getAllByRole("button", { name: "instances.altSpeedTurnOff" })).toHaveLength(1)

    fireEvent.click(within(table).getByRole("button", { name: "instances.altSpeedTurnOn" }))
    await vi.waitFor(() => expect(toggles).toEqual([1]))
    await vi.waitFor(() => expect(within(table).getAllByRole("button", { name: "instances.altSpeedTurnOff" })).toHaveLength(2))
    expect(openHosts(table)).toEqual([])
  })

  it("shows the limits as off again on a desktop row when qBittorrent rejects the change", async () => {
    desktop = true
    toggleFails = true
    renderOverview([stats(1)])
    const table = await screen.findByRole("table", { name: "instances.title" })

    fireEvent.click(within(table).getByRole("button", { name: "instances.altSpeedTurnOn" }))
    await vi.waitFor(() => expect(mocks.toastError).toHaveBeenCalledTimes(1))
    expect(within(table).getByRole("button", { name: "instances.altSpeedTurnOn" })).toBeTruthy()
    expect(within(table).queryByRole("button", { name: "instances.altSpeedTurnOff" })).toBeNull()
  })

  it("tells the user when qBittorrent rejects the change of the alternative speed limits", async () => {
    toggleFails = true
    renderOverview([stats(1)])
    fireEvent.click(within(screen.getByRole("list", { name: "instances.title" })).getByText("instance 1"))
    const sheet = await screen.findByRole("dialog")

    fireEvent.click(within(sheet).getByRole("switch"))
    await vi.waitFor(() => expect(mocks.toastError).toHaveBeenCalledTimes(1))
    expect(String(mocks.toastError.mock.calls[0][0])).toMatch(/^common:errors\.altSpeedToggle message=/)
  })

  it("leaves the cause out of the speed limit error in incognito mode", async () => {
    mocks.incognitoMode = [true, () => {}]
    toggleFails = true
    renderOverview([stats(1)])
    fireEvent.click(within(screen.getByRole("list", { name: "instances.title" })).getByText("instance 1"))
    const sheet = await screen.findByRole("dialog")

    fireEvent.click(within(sheet).getByRole("switch"))
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

  it("gives the filter of an issue to a profile that keeps its own sort and filters", () => {
    const phone = createDisplayProfile("Phone")
    setDisplayProfileOwnSortAndFilters(phone, true)
    renderOverview([stats(1, { torrentCounts: { status: { unregistered: 4 }, categories: {}, tags: {}, trackers: {}, total: 10 } })])

    fireEvent.click(within(screen.getByTestId("attention-list")).getByText("issues.unregistered count=4").closest("a")!)

    const filters = { defaultValue: null, parse: (raw: string) => JSON.parse(raw) as unknown }
    expect(readClientSetting("qui-filters-global", filters)).toEqual({ status: ["unregistered"], excludeStatus: [] })
    pickDisplayProfile(DEFAULT_DISPLAY_PROFILE_ID)
    expect(readClientSetting("qui-filters-global", filters)).toBeNull()
  })
})

describe("DashboardOverview pointed layer", () => {
  function marks() {
    const layers = screen.getAllByTestId("chart-layer")
    const withMark = (mark: string) => layers.filter(layer => layer.hasAttribute(mark)).map(layer => layer.getAttribute("data-layer"))
    return { pointed: withMark("data-pointed"), faded: withMark("data-faded") }
  }

  async function chartOfTwoLayers() {
    const chart = await screen.findByRole("img", { name: "stream.dayLabel" })
    await vi.waitFor(() => expect(screen.getAllByTestId("chart-layer")).toHaveLength(2))
    chart.getBoundingClientRect = () => ({ left: 0, top: 0, width: 600, height: 200 }) as DOMRect
    return { chart, x: ((24 - 1 - 2 + 0.5) / 24) * 600 }
  }

  it("marks the layer of a desktop row and fades the other layer while the pointer or the keyboard focus is on the row", async () => {
    desktop = true
    answer = () => [hourRow("a.test", 2, GiB, 0)]
    renderOverview([stats(1), stats(2)])
    const table = await screen.findByRole("table", { name: "instances.title" })
    await chartOfTwoLayers()
    const row = rowOf(table, "instance 2")

    fireEvent.mouseEnter(row)
    expect(marks()).toEqual({ pointed: ["1"], faded: ["0"] })
    fireEvent.mouseLeave(row)
    expect(marks()).toEqual({ pointed: [], faded: [] })
    fireEvent.focus(row)
    expect(marks()).toEqual({ pointed: ["1"], faded: ["0"] })
    fireEvent.blur(row)
    expect(marks()).toEqual({ pointed: [], faded: [] })
  })

  it("marks the layer under the pointer in the chart and the row of its instance, for upload and for download", async () => {
    desktop = true
    answer = id => [hourRow("a.test", 2, GiB, id === 2 ? GiB : 0)]
    renderOverview([stats(1), stats(2)])
    const table = await screen.findByRole("table", { name: "instances.title" })
    const { chart, x } = await chartOfTwoLayers()
    const pointAt = (y: number) => {
      fireEvent.mouseMove(chart, { clientX: x, clientY: y })
      return { ...marks(), rows: ["instance 1", "instance 2"].filter(name => rowOf(table, name).querySelector("[data-pointed]")) }
    }

    expect(pointAt(100)).toEqual({ pointed: ["0"], faded: ["1"], rows: ["instance 1"] })
    expect(pointAt(30)).toEqual({ pointed: ["1"], faded: ["0"], rows: ["instance 2"] })
    expect(pointAt(2)).toEqual({ pointed: [], faded: [], rows: [] })
    expect(pointAt(150)).toEqual({ pointed: ["1"], faded: ["0"], rows: ["instance 2"] })
    expect(pointAt(197)).toEqual({ pointed: [], faded: [], rows: [] })
    fireEvent.mouseMove(chart, { clientX: x, clientY: 150 })
    fireEvent.mouseLeave(chart)
    expect(marks()).toEqual({ pointed: [], faded: [] })
  })

  it("marks the line of the instance on a phone for a tap on its layer", async () => {
    answer = () => [hourRow("a.test", 2, GiB, 0)]
    renderOverview([stats(1), stats(2)])
    const { chart, x } = await chartOfTwoLayers()

    fireEvent.click(chart, { clientX: x, clientY: 150 })
    const lines = within(screen.getByRole("list", { name: "instances.title" })).getAllByRole("listitem")
    expect(lines.map(line => line.querySelector("[data-pointed]") !== null)).toEqual([true, false])
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

  it("adds up the all-time transfer of the instances, and shows no ratio when nothing was downloaded", () => {
    renderOverview([stats(1, {}, {}, { alltime_ul: 9 * GiB, alltime_dl: 2 * GiB }), stats(2, {}, {}, { alltime_ul: 3 * GiB, alltime_dl: 2 * GiB })])
    expect(screen.getByTestId("library-all-time").textContent).toBe(`library.allTime${formatBytes(12 * GiB)}library.uploaded${formatBytes(4 * GiB)}library.downloaded3.00library.ratio`)
    cleanup()

    renderOverview([stats(1)])
    expect(screen.getByTestId("library-all-time").textContent).toBe("library.allTime0 Blibrary.uploaded0 Blibrary.downloaded–library.ratio")
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
