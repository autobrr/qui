/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { HttpResponse, http } from "msw"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import "@/i18n"
import { _resetClientSettingsForTests, applyServerSettings, readRaw, seedAndMarkReady, writeRaw } from "@/lib/client-settings"
import { Statistics, StatisticsRoute } from "@/pages/Statistics"
import { TooltipProvider } from "@/components/ui/tooltip"
import { server } from "@/test/msw/server"
import { stubPhone } from "@/test/stubPhone"
import type { StatisticsBar } from "@/lib/statistics"
import type { EventDay } from "@/lib/statistics-events"
import type { LibraryPoint } from "@/lib/statistics-library"
import type { LiveTrackerGroup, TrackerCustomization, TransferHistoryInstance, TransferHistoryLibraryRow, TransferHistoryRow, TransferHistoryTorrent } from "@/types"

const mocks = vi.hoisted(() => {
  const t = (key: string, options?: Record<string, unknown>) => {
    const values = Object.entries(options ?? {}).filter(([name]) => name !== "count" || options?.count !== undefined)
    return values.length > 0 ? `${key} ${values.map(([name, value]) => `${name}=${String(value)}`).join(" ")}` : key
  }
  return {
    i18n: { t, i18n: { resolvedLanguage: "en", language: "en" } },
    incognitoMode: [false, () => {}] as [boolean, () => void],
    trackerIcons: { data: undefined } as { data?: Record<string, string> },
    search: {} as { period?: string },
    navigate: vi.fn(),
  }
})

vi.mock("@tanstack/react-router", async (importOriginal) => ({
  ...await importOriginal<typeof import("@tanstack/react-router")>(),
  getRouteApi: () => ({ useSearch: () => mocks.search, useNavigate: () => mocks.navigate }),
}))

vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => mocks.i18n,
}))
vi.mock("@/lib/incognito", async (importOriginal) => ({
  ...await importOriginal<typeof import("@/lib/incognito")>(),
  useIncognitoMode: () => mocks.incognitoMode,
}))
vi.mock("@/hooks/useTrackerIcons", () => ({ useTrackerIcons: () => mocks.trackerIcons }))
vi.mock("@/components/statistics/TransferChart", () => ({
  TransferChart: ({ bars, groups, displayName }: { bars: StatisticsBar[]; groups: string[]; displayName: (group: string) => string }) => (
    <ul data-testid="chart" data-uploaded={bars.reduce((sum, bar) => sum + [...bar.bytes.values()].reduce((bytes, value) => bytes + value.uploaded, 0), 0)}>
      {groups.map(group => <li key={group}>{displayName(group)}</li>)}
    </ul>
  ),
}))
vi.mock("@/components/statistics/LibraryGrowthChart", () => ({
  LibraryGrowthChart: ({ points }: { points: LibraryPoint[] }) => (
    <div data-testid="growth-chart" data-points={JSON.stringify(points.map(point => [point.torrents, point.totalSize, point.uniqueSize]))} />
  ),
}))
vi.mock("@/components/statistics/EventsChart", () => ({
  EventsChart: ({ days }: { days: EventDay[] }) => (
    <div data-testid="events-chart" data-events={JSON.stringify(days.map(day => [day.added, day.completed, day.deleted]))} />
  ),
}))
vi.mock("@/components/statistics/GroupChart", () => ({
  GroupChart: ({ bars }: { bars: StatisticsBar[] }) => <div data-testid="group-chart" data-bars={bars.length} />,
}))

const DAY = 86_400_000

interface TransferRequest {
  instanceId: number
  bucket: string
  groupBy: string | null
  group: string | null
  groupOf: string | null
  limit: string | null
  from: string | null
  to: string | null
  timeZone: string | null
}

let statuses: TransferHistoryInstance[]
let configuredIds: number[]
let answer: (request: TransferRequest) => TransferHistoryRow[]
let torrentAnswer: (request: TransferRequest) => TransferHistoryRow[]
let requests: TransferRequest[]
let liveGroups: (instanceId: number) => LiveTrackerGroup[]
let customizations: TrackerCustomization[]
let libraryAnswer: (instanceId: number) => TransferHistoryLibraryRow[]
let hold: Promise<void> | undefined
let holdBucket: string | undefined
let holdGroupOf: string | undefined

function row(group: string, uploaded: number, downloaded: number, extra: Partial<TransferHistoryRow> = {}): TransferHistoryRow {
  return { group, start: new Date(Date.now() - DAY).toISOString(), width: "hour", gapSeconds: 0, uploaded, downloaded, statisticsCrossSeedUploaded: 0, ...extra }
}

function torrentRow(hash: string, name: string, uploaded: number, torrent: Partial<TransferHistoryTorrent> = {}, extra: Partial<TransferHistoryRow> = {}): TransferHistoryRow {
  return row(hash, uploaded, 0, {
    width: "total",
    torrent: { name, trackerGroup: "a.test", size: 1024, deleted: false, statisticsCrossSeed: false, uploadFirst24h: null, ...torrent },
    ...extra,
  })
}

function libraryRow(daysAgo: number, sizes: Partial<TransferHistoryLibraryRow>): TransferHistoryLibraryRow {
  const now = new Date()
  return { day: new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate()) - daysAgo * DAY).toISOString(), added: 0, completed: 0, deleted: 0, ...sizes }
}

async function growthPoints(): Promise<unknown> {
  return JSON.parse((await screen.findByTestId("growth-chart")).getAttribute("data-points") ?? "")
}

function recordedDaysAgo(instanceId: number, name: string, days: number): TransferHistoryInstance {
  return { instanceId, name, firstRecordedAt: new Date(Date.now() - days * DAY).toISOString() }
}

function renderPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const view = render(
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <Statistics />
      </TooltipProvider>
    </QueryClientProvider>
  )
  return { ...view, queryClient }
}

async function expectTile(label: string, expected: string) {
  const labelNode = await screen.findByText(label)
  await vi.waitFor(() => {
    const value = labelNode.nextElementSibling?.firstElementChild
    expect(`${value?.textContent} | ${value?.nextElementSibling?.textContent}`).toBe(expected)
  })
}

function wideText(node: Element): string {
  const copy = node.cloneNode(true) as Element
  copy.querySelectorAll("[data-narrow], [aria-hidden=true]").forEach(narrow => narrow.remove())
  return copy.textContent ?? ""
}

function cells(table: HTMLElement, name: string): string[] {
  const nameNode = within(table).getByText(name)
  return [...(nameNode.closest("tr")?.querySelectorAll("td") ?? [])].map(wideText)
}

function expandedRows(table: HTMLElement): string[] {
  return [...table.querySelectorAll("button[aria-expanded=true]")].map(wideText)
}

function isoDay(date: Date): string {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, "0")}-${String(date.getDate()).padStart(2, "0")}`
}

function rowNames(table: HTMLElement): string[] {
  return [...table.querySelectorAll("tbody tr")].map(row => wideText(row.querySelector("td")!))
}

function columnHeader(table: HTMLElement, label: string): HTMLElement {
  return within(table.querySelector("thead")!).getByText(label)
}

function sortOf(table: HTMLElement, label: string): string | null {
  return columnHeader(table, label).closest("th")!.getAttribute("aria-sort")
}

async function expectFact(label: string, expected: string) {
  await vi.waitFor(() => {
    const value = screen.getByText(label).nextElementSibling
    expect(`${value?.textContent} | ${value?.nextElementSibling?.textContent ?? ""}`).toBe(expected)
  })
}

beforeEach(() => {
  mocks.incognitoMode = [false, () => {}]
  requests = []
  hold = undefined
  holdBucket = undefined
  holdGroupOf = undefined
  configuredIds = [1, 2]
  statuses = [recordedDaysAgo(1, "Seedbox", 400), recordedDaysAgo(2, "Home", 400)]
  answer = () => []
  torrentAnswer = () => []
  liveGroups = () => []
  customizations = []
  mocks.trackerIcons = { data: undefined }
  libraryAnswer = () => []
  server.use(
    http.get("*/api/instances/:id/transfer-history/library", ({ params }) => HttpResponse.json({ rows: libraryAnswer(Number(params.id)) })),
    http.get("*/api/instances/:id/transfer-history/deletes", () => HttpResponse.json({ deletes: [], counts: { automation: 0, user: 0, other: 0 } })),
    http.get("*/api/instances/:id/tracker-groups", ({ params }) => HttpResponse.json({ groups: liveGroups(Number(params.id)) })),
    http.get("*/api/tracker-customizations", () => HttpResponse.json(customizations)),
    http.get("*/api/transfer-history/status", () => HttpResponse.json({ instances: statuses })),
    http.get("*/api/instances", () => HttpResponse.json(configuredIds.map(id => ({ id, name: `instance ${id}`, isActive: true })))),
    http.get("*/api/instances/:id/transfer-history", async ({ params, request }) => {
      const url = new URL(request.url)
      const transfer = {
        instanceId: Number(params.id),
        bucket: url.searchParams.get("bucket") ?? "",
        groupBy: url.searchParams.get("groupBy"),
        group: url.searchParams.get("group"),
        groupOf: url.searchParams.get("groupOf"),
        limit: url.searchParams.get("limit"),
        from: url.searchParams.get("from"),
        to: url.searchParams.get("to"),
        timeZone: url.searchParams.get("timeZone"),
      }
      requests.push(transfer)
      if ((holdBucket === undefined || transfer.bucket === holdBucket) && (holdGroupOf === undefined || transfer.groupOf === holdGroupOf)) {
        await hold
      }
      const rows = transfer.groupBy === "torrent" ? torrentAnswer(transfer) : answer(transfer)
      return HttpResponse.json({ rows: transfer.group === null || transfer.groupOf !== null ? rows : rows.filter(row => row.group === transfer.group) })
    })
  )
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  delete (Element.prototype as { scrollIntoView?: unknown }).scrollIntoView
  localStorage.clear()
  _resetClientSettingsForTests()
  mocks.search = {}
  mocks.navigate.mockReset()
})

function renderRoute() {
  return render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <StatisticsRoute />
    </QueryClientProvider>
  )
}

function loadServerSnapshot(settings: Record<string, string> = {}) {
  act(() => {
    applyServerSettings(settings)
    seedAndMarkReady(settings)
  })
}

function selectedPeriod(): string | undefined {
  return screen.getAllByRole("tab").find(tab => tab.textContent?.startsWith("periods.") && tab.getAttribute("aria-selected") === "true")?.textContent ?? undefined
}

describe("Statistics period", () => {
  it("opens on the preset period that the user picked last", async () => {
    localStorage.setItem("qui-statistics-period", JSON.stringify("7d"))
    renderPage()

    await screen.findByText("periods.7d")
    expect(selectedPeriod()).toBe("periods.7d")
  })

  it("saves a picked preset, and keeps it when the user then applies a custom range", async () => {
    const { unmount } = renderPage()
    fireEvent.mouseDown(await screen.findByText("periods.24h"), { button: 0 })
    expect(readRaw("qui-statistics-period")).toBe(JSON.stringify("24h"))

    fireEvent.click(screen.getByText("periods.custom"))
    fireEvent.change(screen.getByLabelText("customRange.from"), { target: { value: "2026-01-05" } })
    fireEvent.change(screen.getByLabelText("customRange.to"), { target: { value: "2026-01-06" } })
    fireEvent.click(screen.getByText("customRange.apply"))
    expect(await screen.findByText(/^customRange\.range from=/)).toBeTruthy()
    expect(readRaw("qui-statistics-period")).toBe(JSON.stringify("24h"))
    unmount()

    renderPage()
    await screen.findByText("periods.24h")
    expect(selectedPeriod()).toBe("periods.24h")
  })

  it("moves to a preset that the server or another tab saves after the page opened", async () => {
    renderPage()
    await screen.findByText("periods.7d")
    expect(selectedPeriod()).toBe("periods.30d")

    act(() => writeRaw("qui-statistics-period", JSON.stringify("7d")))
    expect(selectedPeriod()).toBe("periods.7d")
  })

  it("waits for the saved period from the server before it shows the periods, so that a tap is not lost", async () => {
    renderRoute()
    expect(screen.queryByText("periods.30d")).toBeNull()

    loadServerSnapshot({ "qui-statistics-period": JSON.stringify("7d") })
    await screen.findByText("periods.7d")
    expect(selectedPeriod()).toBe("periods.7d")
  })

  it("keeps the period of the address over an older server value when the local copy already holds it", async () => {
    localStorage.setItem("qui-statistics-period", JSON.stringify("24h"))
    mocks.search = { period: "24h" }
    renderRoute()
    loadServerSnapshot({ "qui-statistics-period": JSON.stringify("7d") })

    await screen.findByText("periods.24h")
    expect(selectedPeriod()).toBe("periods.24h")
    expect(readRaw("qui-statistics-period")).toBe(JSON.stringify("24h"))
  })

  it("opens on the period of the address, saves it, and removes it from the address", async () => {
    localStorage.setItem("qui-statistics-period", JSON.stringify("7d"))
    mocks.search = { period: "24h" }
    renderRoute()
    loadServerSnapshot()

    await screen.findByText("periods.24h")
    expect(selectedPeriod()).toBe("periods.24h")
    expect(readRaw("qui-statistics-period")).toBe(JSON.stringify("24h"))
    expect(mocks.navigate).toHaveBeenCalledWith(expect.objectContaining({ replace: true }))
    const { search } = mocks.navigate.mock.calls[0][0] as { search: (previous: { period?: string }) => { period?: string } }
    expect(search({ period: "24h" }).period).toBeUndefined()
  })
})

describe("Statistics", () => {
  it("adds the instances together and takes the cross-seed upload from the same rows", async () => {
    answer = ({ instanceId, bucket }) => bucket === "day" ? [row("a.test", instanceId * 1024, 1024, { statisticsCrossSeedUploaded: 512, width: "day" })] : []
    renderPage()

    await expectTile("tiles.uploaded", "3 KiB | ")
    await expectTile("tiles.crossSeed", "1 KiB | tiles.crossSeedShare percent=33")
    await expectTile("tiles.ratio", "1.50 | tiles.buffer buffer=+1 KiB")
    expect(requests.find(request => request.bucket === "day")?.timeZone).toBe(Intl.DateTimeFormat().resolvedOptions().timeZone)
  })

  it("includes the Baseline in the numbers of all time", async () => {
    answer = ({ bucket }) => bucket === "total" ? [row("a.test", 1024, 0, { start: undefined, width: "total", baseline: { uploaded: 4096, downloaded: 0, statisticsCrossSeedUploaded: 0 } })] : [row("a.test", 1024, 0)]
    renderPage()
    fireEvent.mouseDown(await screen.findByText("periods.all"), { button: 0 })

    await vi.waitFor(() => expect(requests.some(request => request.bucket === "total" && request.from === null)).toBe(true))
    await expectTile("tiles.uploaded", "10 KiB | ")
    expect(screen.getByText("allTimeNote")).toBeTruthy()
    expect((await screen.findByTestId("chart")).getAttribute("data-uploaded")).toBe("2048")
  })

  it("asks for each hour of a custom range from the local midnight of its first day", async () => {
    renderPage()
    fireEvent.click(await screen.findByText("periods.custom"))
    fireEvent.change(screen.getByLabelText("customRange.from"), { target: { value: "2026-01-05" } })
    fireEvent.change(screen.getByLabelText("customRange.to"), { target: { value: "2026-01-06" } })
    requests = []
    fireEvent.click(screen.getByText("customRange.apply"))

    await vi.waitFor(() => expect(requests.filter(request => request.bucket === "hour").length).toBe(2))
    expect(requests.find(request => request.bucket === "hour")).toMatchObject({ from: new Date(2026, 0, 5).toISOString(), to: new Date(2026, 0, 7).toISOString(), timeZone: null })
    expect(await screen.findByText(/^customRange\.range from=/)).toBeTruthy()
  })

  it("does not keep the deleted torrents of a custom range when only its last day changes", async () => {
    let releaseSecond = () => {}
    const second = new Promise<void>(resolve => {
      releaseSecond = resolve
    })
    const firstTo = new Date(2026, 0, 7).toISOString()
    server.use(http.get("*/api/instances/:id/transfer-history/deletes", async ({ request }) => {
      const to = new URL(request.url).searchParams.get("to")
      if (to !== firstTo) {
        await second
      }
      const name = to === firstTo ? "First.Range-GRP" : "Second.Range-GRP"
      return HttpResponse.json({
        deletes: [{ time: new Date(2026, 0, 5, 12).toISOString(), hash: name, name, trackerGroup: "a.test", uploaded: 1, downloaded: 1, cause: { kind: "user" } }],
        counts: { automation: 0, user: 1, other: 0 },
      })
    }))
    const applyRange = (last: string) => {
      fireEvent.click(screen.getAllByText(/^(periods\.custom|customRange\.range from=)/)[0])
      fireEvent.change(screen.getByLabelText("customRange.from"), { target: { value: "2026-01-05" } })
      fireEvent.change(screen.getByLabelText("customRange.to"), { target: { value: last } })
      fireEvent.click(screen.getByText("customRange.apply"))
    }
    renderPage()
    await screen.findByText("periods.custom")
    applyRange("2026-01-06")
    await screen.findAllByText("First.Range-GRP")

    applyRange("2026-01-07")
    await vi.waitFor(() => expect(screen.queryAllByText("First.Range-GRP")).toHaveLength(0))
    releaseSecond()
    await screen.findAllByText("Second.Range-GRP")
  })

  it("moves a preset period forward while the page stays open, and keeps the old numbers until the new ones load", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true, toFake: ["setInterval", "clearInterval", "Date"] })
    try {
      answer = ({ bucket }) => bucket === "day" ? [row("a.test", 1024, 0, { width: "day" })] : []
      renderPage()
      await expectTile("tiles.uploaded", "2 KiB | ")
      const firstEnd = requests.find(request => request.bucket === "day")?.to
      requests = []
      let release = () => {}
      hold = new Promise(resolve => {
        release = resolve
      })

      await act(async () => {
        await vi.advanceTimersByTimeAsync(5 * 60_000 + 60_000)
      })

      await vi.waitFor(() => expect(requests.some(request => request.bucket === "day")).toBe(true))
      expect(Date.parse(requests.find(request => request.bucket === "day")?.to ?? "")).toBeGreaterThan(Date.parse(firstEnd ?? ""))
      await expectTile("tiles.uploaded", "2 KiB | ")
      release()
    } finally {
      vi.useRealTimers()
    }
  })

  it("does not show the numbers of other instances after a moved period", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true, toFake: ["setInterval", "clearInterval", "Date"] })
    try {
      answer = ({ bucket }) => bucket === "day" ? [row("a.test", 1024, 0, { width: "day" })] : []
      renderPage()
      fireEvent.pointerDown(await screen.findByText("instances.all"), { button: 0, pointerType: "mouse" })
      fireEvent.click(await screen.findByText("Seedbox"))
      await expectTile("tiles.uploaded", "1 KiB | ")
      await act(async () => {
        await vi.advanceTimersByTimeAsync(5 * 60_000 + 60_000)
      })
      await expectTile("tiles.uploaded", "1 KiB | ")
      hold = new Promise(() => {})

      fireEvent.click(await screen.findByText("Home"))

      await expectTile("tiles.uploaded", "– | ")
    } finally {
      vi.useRealTimers()
    }
  })

  it("drops the change of the old selection after a moved period", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true, toFake: ["setInterval", "clearInterval", "Date"] })
    try {
      statuses = [recordedDaysAgo(1, "Seedbox", 400), recordedDaysAgo(2, "Home", 10)]
      answer = ({ bucket, to }) => {
        if (bucket === "day") {
          return [row("a.test", 2048, 0, { width: "day" })]
        }
        return Date.parse(to ?? "") > Date.now() - DAY ? [row("a.test", 3072, 0, { width: "total" })] : [row("a.test", 2048, 0, { width: "total" })]
      }
      renderPage()
      fireEvent.pointerDown(await screen.findByText("instances.all"), { button: 0, pointerType: "mouse" })
      fireEvent.click(await screen.findByText("Seedbox"))
      await expectTile("tiles.uploaded", "2 KiB | tiles.change change=+50%")
      await act(async () => {
        await vi.advanceTimersByTimeAsync(5 * 60_000 + 60_000)
      })

      fireEvent.click(await screen.findByText("Home"))
      fireEvent.click(await screen.findByText("Seedbox"))

      await expectTile("tiles.uploaded", "2 KiB | ")
    } finally {
      vi.useRealTimers()
    }
  })

  it("does not show the numbers of other instances when the period moves while a new selection loads", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true, toFake: ["setInterval", "clearInterval", "Date"] })
    try {
      answer = ({ bucket }) => bucket === "day" ? [row("a.test", 1024, 0, { width: "day" })] : []
      renderPage()
      fireEvent.pointerDown(await screen.findByText("instances.all"), { button: 0, pointerType: "mouse" })
      fireEvent.click(await screen.findByText("Seedbox"))
      await expectTile("tiles.uploaded", "1 KiB | ")
      await act(async () => {
        await vi.advanceTimersByTimeAsync(9 * 60_000 + 30_000)
      })
      await expectTile("tiles.uploaded", "1 KiB | ")
      hold = new Promise(() => {})
      fireEvent.click(await screen.findByText("Home"))

      await act(async () => {
        await vi.advanceTimersByTimeAsync(60_000)
      })

      await expectTile("tiles.uploaded", "– | ")
    } finally {
      vi.useRealTimers()
    }
  })

  it("keeps the totals and the change of one period until each query of the moved period loads", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true, toFake: ["setInterval", "clearInterval", "Date"] })
    try {
      const nearTheHour = new Date()
      nearTheHour.setMinutes(57, 0, 0)
      vi.setSystemTime(nearTheHour)
      let moved = false
      answer = ({ bucket, to }) => {
        if (bucket === "day") {
          return [row("a.test", 2048, 0, { width: "day" })]
        }
        const compared = Date.parse(to ?? "") > Date.now() - DAY
        return [row("a.test", compared ? (moved ? 1024 : 3072) : 2048, 0, { width: "total" })]
      }
      renderPage()
      await expectTile("tiles.uploaded", "4 KiB | tiles.change change=+50%")
      moved = true
      requests = []
      holdBucket = "day"
      hold = new Promise(() => {})

      await act(async () => {
        await vi.advanceTimersByTimeAsync(5 * 60_000 + 60_000)
      })
      await vi.waitFor(() => expect(requests.filter(request => request.bucket === "total" && request.groupBy === "tracker")).toHaveLength(4))
      await act(async () => {
        await new Promise(resolve => setTimeout(resolve, 200))
      })

      await expectTile("tiles.uploaded", "4 KiB | tiles.change change=+50%")
    } finally {
      vi.useRealTimers()
    }
  })

  it("finds an instance that starts recording while the page is open", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true, toFake: ["setInterval", "clearInterval", "Date"] })
    try {
      statuses = []
      renderPage()
      await screen.findByText("notRecording")
      statuses = [recordedDaysAgo(1, "Seedbox", 0)]

      await act(async () => {
        await vi.advanceTimersByTimeAsync(5 * 60_000 + 60_000)
      })

      expect(await screen.findByText(/^recordingSince date=/)).toBeTruthy()
    } finally {
      vi.useRealTimers()
    }
  })

  it("does not apply a custom range that starts after today", async () => {
    renderPage()
    fireEvent.click(await screen.findByText("periods.custom"))
    const now = new Date()
    const tomorrow = new Date(now.getFullYear(), now.getMonth(), now.getDate() + 1)
    const day = `${tomorrow.getFullYear()}-${String(tomorrow.getMonth() + 1).padStart(2, "0")}-${String(tomorrow.getDate()).padStart(2, "0")}`
    fireEvent.change(screen.getByLabelText("customRange.from"), { target: { value: day } })
    fireEvent.change(screen.getByLabelText("customRange.to"), { target: { value: day } })

    expect((screen.getByText("customRange.apply") as HTMLButtonElement).disabled).toBe(true)
  })

  it("shows the days of a custom range in the date format of the settings on a desktop", async () => {
    localStorage.setItem("qui-datetime-preferences", JSON.stringify({ dateFormat: "eu" }))
    const year = new Date().getFullYear() - 1
    renderPage()
    fireEvent.click(await screen.findByText("periods.custom"))
    expect(screen.getAllByText("customRange.pick")).toHaveLength(2)

    fireEvent.change(screen.getByLabelText("customRange.from"), { target: { value: `${year}-02-03` } })
    fireEvent.change(screen.getByLabelText("customRange.to"), { target: { value: `${year}-02-04` } })

    expect(screen.getByText(`03/02/${year}`)).toBeTruthy()
    expect(screen.getByText(`04/02/${year}`)).toBeTruthy()

    fireEvent.click(screen.getByText("customRange.apply"))

    expect(await screen.findByText(`customRange.range from=03/02/${year} to=04/02/${year}`)).toBeTruthy()
  })

  it("shows the first day of the recording in the date format of the settings", async () => {
    localStorage.setItem("qui-datetime-preferences", JSON.stringify({ dateFormat: "us" }))
    const since = new Date(statuses[0].firstRecordedAt!)
    renderPage()

    const day = `${String(since.getMonth() + 1).padStart(2, "0")}/${String(since.getDate()).padStart(2, "0")}/${since.getFullYear()}`
    expect(await screen.findByText(`recordingSince date=${day}`)).toBeTruthy()
  })

  it("shows the change against the period before only when qui recorded that period in full", async () => {
    const runningHour = new Date(Math.floor(Date.now() / 3_600_000) * 3_600_000).toISOString()
    answer = ({ bucket, to }) => {
      if (bucket === "day") {
        return [row("a.test", 4096, 1024, { width: "day" })]
      }
      return to === runningHour ? [row("a.test", 3072, 1024, { width: "total" })] : [row("a.test", 2048, 2048, { width: "total" })]
    }
    renderPage()

    await expectTile("tiles.uploaded", "8 KiB | tiles.change change=+50%")
    await expectTile("tiles.downloaded", "2 KiB | tiles.change change=-50%")
    cleanup()

    requests = []
    statuses = [recordedDaysAgo(1, "Seedbox", 400), recordedDaysAgo(2, "Home", 40)]
    renderPage()

    await expectTile("tiles.uploaded", "8 KiB | ")
    expect(requests.filter(request => request.bucket === "total" && request.groupBy === "tracker")).toEqual([])
  })

  it("shows the error in the chart when one instance fails", async () => {
    server.use(http.get("*/api/instances/2/transfer-history", () => HttpResponse.json({ error: "database is locked" }, { status: 500 })))
    renderPage()

    await vi.waitFor(() => expect(screen.getAllByText("errors.load message=database is locked")).toHaveLength(3))
    expect(screen.queryByText("chart.empty")).toBeNull()
  })

  it("shows the error in the table when the status fails", async () => {
    server.use(http.get("*/api/transfer-history/status", () => HttpResponse.json({ error: "database is locked" }, { status: 500 })))
    renderPage()

    await vi.waitFor(() => expect(screen.getAllByText("errors.load message=database is locked")).toHaveLength(6))
  })

  it("says that a period with no transfer has none", async () => {
    renderPage()

    await vi.waitFor(() => expect(screen.getAllByText("chart.empty")).toHaveLength(3))
    expect(screen.queryByTestId("chart")).toBeNull()
  })

  it("labels a period that starts before the first recording as partial", async () => {
    statuses = [recordedDaysAgo(1, "Seedbox", 400), recordedDaysAgo(2, "Home", 10)]
    renderPage()

    expect(await screen.findByText("partial")).toBeTruthy()
    fireEvent.mouseDown(screen.getByText("periods.7d"), { button: 0 })
    await vi.waitFor(() => expect(screen.queryByText("partial")).toBeNull())
  })

  it("limits the queries to the selected instances and lists a removed instance", async () => {
    configuredIds = [1]
    renderPage()

    fireEvent.pointerDown(await screen.findByText("instances.all"), { button: 0, pointerType: "mouse" })
    expect((await screen.findByText("Home")).parentElement?.textContent).toContain("instances.removed")
    fireEvent.click(screen.getByText("Home"))

    await vi.waitFor(() => expect(requests.some(request => request.instanceId === 2 && request.bucket === "day")).toBe(true))
    requests = []
    await act(async () => {
      fireEvent.mouseDown(screen.getByText("periods.24h"), { button: 0 })
    })
    await vi.waitFor(() => expect(requests.length).toBeGreaterThan(0))
    expect(new Set(requests.map(request => request.instanceId))).toEqual(new Set([2]))
  })

  it("masks the tracker names in incognito mode", async () => {
    mocks.incognitoMode = [true, () => {}]
    answer = ({ bucket }) => bucket === "day" ? [row("tracker.private.test", 1024, 0, { width: "day" }), row("Public", 512, 0, { width: "day" })] : []
    renderPage()

    const chart = await screen.findByTestId("chart")
    await vi.waitFor(() => expect(chart.children.length).toBe(2))
    expect(chart.textContent).not.toContain("tracker.private.test")
    expect(chart.textContent).toContain("groups.public")
  })
  it("adds up the tracker rows of the table to the numbers at the top, with the live numbers of each group", async () => {
    answer = ({ instanceId, bucket }) => bucket === "day" ? [row("a.test", instanceId * 1024, 0, { width: "day" }), row("b.test", 512, 512, { width: "day" })] : []
    liveGroups = () => [{ group: "a.test", torrents: 2, size: 2048, uploaded: 4096 }]
    renderPage()

    await expectTile("tiles.uploaded", "4 KiB | ")
    const table = await screen.findByRole("table")
    await vi.waitFor(() => expect(cells(table, "a.test")).toEqual(["a.test", "3 KiB", "0 B", "∞", "+3 KiB", "4", "4 KiB", "table.seededValue value=2.00"]))
    expect(cells(table, "b.test").slice(0, 6)).toEqual(["b.test", "1 KiB", "1 KiB", "1.00", "+0 B", "0"])
    expect(cells(table, "table.total").slice(0, 2)).toEqual(["table.total", "4 KiB"])
  })

  it("shows the tracker icon of each Tracker group in the table, from a domain of the customizations that name the group", async () => {
    mocks.trackerIcons = { data: { "a.test": "data:image/png;base64,YQ==", "b.test": "data:image/png;base64,Yg==" } }
    customizations = [
      { id: 1, displayName: "Bee ", domains: ["b.test"], createdAt: "", updatedAt: "" },
      { id: 2, displayName: "Bee", domains: ["b-alt.test"], createdAt: "", updatedAt: "" },
    ]
    answer = ({ bucket }) => bucket === "day" ? ["a.test", "Bee", "c.test", "Public", "No tracker"].map(group => row(group, 1024, 0, { width: "day" })) : []
    renderPage()

    const table = await screen.findByRole("table")
    const nameButton = (name: string) => within(table).getByText(name).closest("button")!
    await vi.waitFor(() => expect(nameButton("Bee").querySelector("img")?.getAttribute("src")).toBe("data:image/png;base64,Yg=="))
    expect(nameButton("a.test").querySelector("img")?.getAttribute("src")).toBe("data:image/png;base64,YQ==")
    expect(nameButton("c.test").querySelector("img")).toBeNull()
    expect(nameButton("c.test").querySelector("span[aria-hidden=true]")?.textContent).toBe("C")
    expect(nameButton("groups.public").querySelector("img, span[aria-hidden=true]")).toBeNull()
    expect(nameButton("groups.noTracker").querySelector("img, span[aria-hidden=true]")).toBeNull()
  })

  it("keeps the live numbers of the instances that loaded when one instance fails", async () => {
    answer = ({ bucket }) => bucket === "day" ? [row("a.test", 1024, 0, { width: "day" })] : []
    liveGroups = () => [{ group: "a.test", torrents: 2, size: 2048, uploaded: 4096 }]
    server.use(http.get("*/api/instances/2/tracker-groups", () => HttpResponse.json({ error: "Instance is disabled" }, { status: 409 })))
    renderPage()

    const table = await screen.findByRole("table")
    await vi.waitFor(() => expect(cells(table, "a.test")[5]).toBe("2"))
    expect(await screen.findByText(/^table\.liveError message=/)).toBeTruthy()
  })

  it("does not ask a disabled instance for its live numbers", async () => {
    server.use(http.get("*/api/instances", () => HttpResponse.json([{ id: 1, name: "instance 1", isActive: true }, { id: 2, name: "instance 2", isActive: false }])))
    const liveRequests: string[] = []
    server.use(http.get("*/api/instances/:id/tracker-groups", ({ params }) => {
      liveRequests.push(String(params.id))
      return HttpResponse.json({ groups: [] })
    }))
    answer = ({ bucket }) => bucket === "day" ? [row("a.test", 1024, 0, { width: "day" })] : []
    renderPage()

    await screen.findByRole("table")
    await vi.waitFor(() => expect(liveRequests).toEqual(["1"]))
  })

  it("groups the table by category with the period columns only", async () => {
    answer = ({ bucket, groupBy }) => groupBy === "category" && bucket === "total" ? [row("tv", 1024, 0, { width: "total" }), row("", 512, 0, { width: "total" })] : []
    renderPage()
    fireEvent.mouseDown(await screen.findByText("table.groupings.category"), { button: 0 })

    await vi.waitFor(() => expect(requests.some(request => request.groupBy === "category" && request.bucket === "total")).toBe(true))
    const table = await screen.findByRole("table")
    await vi.waitFor(() => expect(cells(table, "tv")).toEqual(["tv", "2 KiB", "0 B", "∞", "+2 KiB"]))
    expect(cells(table, "table.noCategory")[1]).toBe("1 KiB")
    expect(within(table).queryByText("table.columns.torrents")).toBeNull()
  })

  it("sorts each row of the table by the column of a clicked header on a desktop, before it cuts the table to ten rows, and turns the order on the next click", async () => {
    answer = ({ instanceId, bucket }) => instanceId === 1 && bucket === "day"
      ? Array.from({ length: 12 }, (_, index) => row(`t${index + 1}.test`, (12 - index) * 1024, ((index * 5) % 12) * 1024, { width: "day" }))
      : []
    const names = (...rows: number[]) => rows.map(index => `t${index}.test`)
    renderPage()
    const table = await screen.findByRole("table")
    await vi.waitFor(() => expect(rowNames(table)).toEqual(names(1, 2, 3, 4, 5, 6, 7, 8, 9, 10)))
    expect(sortOf(table, "table.columns.uploaded")).toBe("descending")

    fireEvent.click(columnHeader(table, "table.columns.downloaded"))

    expect(rowNames(table)).toEqual(names(8, 3, 10, 5, 12, 7, 2, 9, 4, 11))
    expect(sortOf(table, "table.columns.downloaded")).toBe("descending")
    expect(sortOf(table, "table.columns.uploaded")).toBeNull()

    fireEvent.click(columnHeader(table, "table.columns.downloaded"))

    expect(rowNames(table)).toEqual(names(1, 6, 11, 4, 9, 2, 7, 12, 5, 10))
    expect(sortOf(table, "table.columns.downloaded")).toBe("ascending")

    fireEvent.click(columnHeader(table, "table.groupings.tracker"))

    expect(rowNames(table)).toEqual(names(1, 2, 3, 4, 5, 6, 7, 8, 9, 10))
    expect(sortOf(table, "table.groupings.tracker")).toBe("ascending")

    fireEvent.click(columnHeader(table, "table.groupings.tracker"))

    expect(rowNames(table)).toEqual(names(12, 11, 10, 9, 8, 7, 6, 5, 4, 3))
  })

  it("keeps a row with no value in the column of the sort below the other rows, in both orders", async () => {
    answer = ({ instanceId, bucket }) => instanceId === 1 && bucket === "day" ? [row("a.test", 2048, 1024, { width: "day" }), row("b.test", 1024, 1024, { width: "day" })] : []
    liveGroups = instanceId => instanceId === 1 ? [{ group: "idle.test", torrents: 1, size: 1024, uploaded: 0 }] : []
    renderPage()
    const table = await screen.findByRole("table")
    await vi.waitFor(() => expect(rowNames(table)).toEqual(["a.test", "b.test", "idle.test"]))
    expect(cells(table, "idle.test")[3]).toBe("–")

    fireEvent.click(columnHeader(table, "table.columns.ratio"))

    expect(rowNames(table)).toEqual(["a.test", "b.test", "idle.test"])

    fireEvent.click(columnHeader(table, "table.columns.ratio"))

    expect(rowNames(table)).toEqual(["b.test", "a.test", "idle.test"])
  })

  it("sorts the table by the upload again when the next grouping has no column of the picked sort", async () => {
    answer = ({ instanceId, bucket, groupBy }) => {
      if (instanceId !== 1) {
        return []
      }
      if (groupBy === "category") {
        return bucket === "total" ? [row("tv", 2048, 0, { width: "total" }), row("movies", 1024, 0, { width: "total" })] : []
      }
      return bucket === "day" ? [row("a.test", 2048, 0, { width: "day" }), row("b.test", 1024, 0, { width: "day" })] : []
    }
    liveGroups = instanceId => instanceId === 1 ? [{ group: "a.test", torrents: 1, size: 1024, uploaded: 0 }, { group: "b.test", torrents: 5, size: 1024, uploaded: 0 }] : []
    renderPage()
    const table = await screen.findByRole("table")
    await vi.waitFor(() => expect(cells(table, "b.test")[5]).toBe("5"))
    fireEvent.click(columnHeader(table, "table.columns.torrents"))
    expect(rowNames(table)).toEqual(["b.test", "a.test"])

    fireEvent.mouseDown(screen.getByText("table.groupings.category"), { button: 0 })

    await vi.waitFor(() => expect(rowNames(screen.getByRole("table"))).toEqual(["tv", "movies"]))
    expect(sortOf(screen.getByRole("table"), "table.columns.uploaded")).toBe("descending")
  })

  it("shows ten more rows of the table for each Show more on a desktop, and ten again after Show fewer", async () => {
    answer = ({ instanceId, bucket }) => instanceId === 1 && bucket === "day"
      ? Array.from({ length: 25 }, (_, index) => row(`t${index + 1}.test`, (25 - index) * 1024, 0, { width: "day" }))
      : []
    renderPage()
    const table = await screen.findByRole("table")
    await vi.waitFor(() => expect(rowNames(table)).toHaveLength(10))
    expect(screen.getByText("table.shown shown=10 total=25")).toBeTruthy()
    expect(screen.queryByText("table.showAll")).toBeNull()

    fireEvent.click(screen.getByText("table.showMore"))

    expect(rowNames(table)).toHaveLength(20)
    expect(screen.getByText("table.shown shown=20 total=25")).toBeTruthy()

    fireEvent.click(screen.getByText("table.showMore"))

    expect(rowNames(table)).toHaveLength(25)
    expect(screen.queryByText("table.showMore")).toBeNull()

    Element.prototype.scrollIntoView = vi.fn()
    fireEvent.click(screen.getByText("table.showFewer"))

    expect(rowNames(table)).toHaveLength(10)
    expect(screen.queryByText("table.showFewer")).toBeNull()
  })

  it("shows a spinner while the query of a release attribute runs, then its rows with None for no value", async () => {
    let release = () => {}
    answer = ({ bucket, groupBy }) => groupBy === "resolution" && bucket === "total" ? [row("1080p", 1024, 0, { width: "total" }), row("", 512, 0, { width: "total" })] : []
    renderPage()
    const tab = await screen.findByText("table.groupings.resolution")
    hold = new Promise(resolve => { release = resolve })
    holdBucket = "total"
    fireEvent.mouseDown(tab, { button: 0 })

    await vi.waitFor(() => expect(requests.some(request => request.groupBy === "resolution" && request.bucket === "total")).toBe(true))
    const tableCard = screen.getByText("table.title").closest("[data-slot=card]")
    expect(screen.queryByRole("table")).toBeNull()
    expect(tableCard?.querySelector(".animate-spin")).not.toBeNull()

    release()
    const table = await screen.findByRole("table")
    await vi.waitFor(() => expect(cells(table, "1080p")).toEqual(["1080p", "2 KiB", "0 B", "∞", "+2 KiB"]))
    expect(cells(table, "table.none")[1]).toBe("1 KiB")
    expect(within(table).queryByText("table.columns.torrents")).toBeNull()
  })

  it("opens the detail band of a release attribute row from the days of that value", async () => {
    answer = ({ bucket, groupBy }) => groupBy === "releaseGroup" && bucket === "total" ? [row("", 1024, 0, { width: "total" })] : []
    renderPage()
    fireEvent.mouseDown(await screen.findByText("table.groupings.releaseGroup"), { button: 0 })
    fireEvent.click(await within(await screen.findByRole("table")).findByText("table.none"))

    await vi.waitFor(() => expect(requests.find(request => request.groupBy === "releaseGroup" && request.bucket === "day")).toMatchObject({ group: "", timeZone: null }))
    expect(await screen.findByTestId("group-chart")).toBeTruthy()
  })

  it("includes the Baseline in a row of all time", async () => {
    answer = ({ bucket }) => bucket === "total" ? [row("a.test", 1024, 0, { start: undefined, width: "total", baseline: { uploaded: 4096, downloaded: 0, statisticsCrossSeedUploaded: 0 } })] : []
    renderPage()
    fireEvent.mouseDown(await screen.findByText("periods.all"), { button: 0 })

    await expectTile("tiles.uploaded", "10 KiB | ")
    const table = await screen.findByRole("table")
    await vi.waitFor(() => expect(cells(table, "a.test")[1]).toBe("10 KiB"))
  })

  it("opens the detail band of one row at a time, from the rows of that group", async () => {
    const now = new Date()
    const yesterday = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1)
    answer = ({ bucket }) => bucket === "day" ? [row("a.test", 3072, 1024, { start: yesterday.toISOString(), width: "day", statisticsCrossSeedUploaded: 1024 }), row("b.test", 1024, 0, { start: yesterday.toISOString(), width: "day" })] : []
    renderPage()
    const table = await screen.findByRole("table")
    fireEvent.click(await within(table).findByText("a.test"))

    await vi.waitFor(() => expect(requests.some(request => request.group === "a.test" && request.bucket === "day" && request.groupBy === "tracker")).toBe(true))
    await expectFact("detail.share", "75% | detail.shareOf total=8 KiB")
    await expectFact("detail.crossSeed", "2 KiB | detail.crossSeedShare percent=33.3%")
    await expectFact("detail.bestDay", `6 KiB | ${isoDay(yesterday)}`)
    await expectFact("detail.averageDay", "204.8 B | ")
    expect(screen.getByTestId("group-chart")).toBeTruthy()

    fireEvent.click(within(table).getByText("b.test"))

    await vi.waitFor(() => expect(expandedRows(table)).toEqual(["b.test"]))
    expect(screen.getAllByText("detail.share")).toHaveLength(1)
    await expectFact("detail.share", "25% | detail.shareOf total=8 KiB")
  })

  it("leaves the days before the first recording out of the average of the detail band", async () => {
    const now = new Date()
    const yesterday = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1)
    statuses = [{ instanceId: 1, name: "Seedbox", firstRecordedAt: new Date(now.getFullYear(), now.getMonth(), now.getDate() - 2, 1).toISOString() }]
    answer = ({ bucket }) => bucket === "day" ? [row("a.test", 3072, 0, { start: yesterday.toISOString(), width: "day" })] : []
    renderPage()
    fireEvent.click(await within(await screen.findByRole("table")).findByText("a.test"))

    await expectFact("detail.averageDay", "1 KiB | ")
  })

  it("masks the release groups of the table in incognito mode", async () => {
    mocks.incognitoMode = [true, () => {}]
    answer = ({ bucket, groupBy }) => groupBy === "releaseGroup" && bucket === "total" ? [row("PRIVATEGRP", 1024, 0, { width: "total" }), row("", 512, 0, { width: "total" })] : []
    renderPage()
    fireEvent.mouseDown(await screen.findByText("table.groupings.releaseGroup"), { button: 0 })

    const table = await screen.findByRole("table")
    await vi.waitFor(() => expect(within(table).getAllByRole("row").length).toBe(4))
    expect(table.textContent).not.toContain("PRIVATEGRP")
    expect(cells(table, "table.none")[1]).toBe("1 KiB")
  })

  it("masks the tracker names and the category names of the table in incognito mode", async () => {
    mocks.incognitoMode = [true, () => {}]
    answer = ({ bucket, groupBy }) => {
      if (groupBy === "category") {
        return bucket === "total" ? [row("private-category", 1024, 0, { width: "total" })] : []
      }
      return bucket === "day" ? [row("tracker.private.test", 1024, 0, { width: "day" })] : []
    }
    mocks.trackerIcons = { data: { "tracker.private.test": "data:image/png;base64,cA==" } }
    renderPage()
    const table = await screen.findByRole("table")
    await vi.waitFor(() => expect(within(table).getAllByRole("row").length).toBeGreaterThan(2))
    expect(table.textContent).not.toContain("tracker.private.test")
    expect(table.querySelector("img")).toBeNull()

    fireEvent.mouseDown(screen.getByText("table.groupings.category"), { button: 0 })

    await vi.waitFor(() => expect(within(screen.getByRole("table")).queryByText("table.groupings.category")).toBeTruthy())
    await vi.waitFor(() => expect(within(screen.getByRole("table")).getAllByRole("row").length).toBeGreaterThan(2))
    expect(screen.getByRole("table").textContent).not.toContain("private-category")
  })

  it("lists the top torrents of the instances in the order of their upload, with a deleted torrent and its name", async () => {
    torrentAnswer = ({ instanceId, bucket }) => {
      if (bucket !== "total") {
        return []
      }
      return instanceId === 1
        ? [torrentRow("h1", "Alpha", 3072, { uploadFirst24h: 1024 }), torrentRow("h2", "Bravo", 1024, { deleted: true })]
        : [torrentRow("h3", "Charlie", 2048, { statisticsCrossSeed: true })]
    }
    renderPage()

    const table = await screen.findByRole("table")
    await vi.waitFor(() => expect(within(table).queryByText("Bravo")).toBeTruthy())
    const names = within(table).getAllByRole("button").map(button => button.textContent)
    expect(names).toEqual(["Alpha", "Charlie", "Bravo"])
    const marks = (name: string) => within(table).getByText(name).closest("td")?.querySelector("[role=img]")?.getAttribute("aria-label")
    expect([marks("Alpha"), marks("Charlie"), marks("Bravo")]).toEqual([undefined, "topTorrents.crossSeed", "topTorrents.deleted"])
    expect(cells(table, "Alpha").slice(1)).toEqual(["a.test · Seedbox", "3 KiB", "0 B", "∞", "table.seededValue value=3.00", "1 KiB"])
    expect(within(table).getAllByText("a.test · Seedbox").length).toBeGreaterThan(0)
    const asked = requests.filter(request => request.groupBy === "torrent")
    expect(asked.map(request => [request.instanceId, request.bucket, request.limit, request.group])).toEqual([[1, "total", "10", null], [2, "total", "10", null]])
  })

  it("shows the tracker icon next to the tracker of each top torrent, the first letter for a tracker with no icon, and none for Public", async () => {
    mocks.trackerIcons = { data: { "a.test": "data:image/png;base64,YQ==" } }
    torrentAnswer = ({ instanceId, bucket }) => bucket === "total" && instanceId === 1
      ? [torrentRow("h1", "Alpha", 3072), torrentRow("h2", "Bravo", 2048, { trackerGroup: "c.test" }), torrentRow("h3", "Charlie", 1024, { trackerGroup: "Public" })]
      : []
    renderPage()

    const table = await screen.findByRole("table")
    await vi.waitFor(() => expect(within(table).queryByText("Charlie")).toBeTruthy())
    const trackerCells = [...table.querySelectorAll("tbody tr td:nth-child(2)")]
    expect(trackerCells.map(cell => cell.querySelector("img")?.getAttribute("src") ?? cell.querySelector("span[aria-hidden=true]")?.textContent ?? null)).toEqual(["data:image/png;base64,YQ==", "C", null])
  })

  it("shows the upload of an open torrent for each day, from the rows of that torrent on its instance", async () => {
    torrentAnswer = ({ instanceId, bucket, group }) => {
      if (bucket === "total") {
        return instanceId === 2 ? [torrentRow("h3", "Charlie", 2048)] : []
      }
      return group === "h3" ? [torrentRow("h3", "Charlie", 2048, {}, { width: "day", start: new Date(Date.now() - DAY).toISOString() })] : []
    }
    renderPage()

    fireEvent.click(await within(await screen.findByRole("table")).findByText("Charlie"))

    await vi.waitFor(() => expect(screen.getByTestId("group-chart")).toBeTruthy())
    expect(requests.filter(request => request.groupBy === "torrent" && request.bucket === "day").map(request => [request.instanceId, request.group])).toEqual([[2, "h3"]])
    expect(Number(screen.getByTestId("group-chart").getAttribute("data-bars"))).toBeGreaterThanOrEqual(30)
    expect(screen.getByText("topTorrents.utcNote")).toBeTruthy()
  })

  it("puts the numbers of a top torrent that a narrow table has no column for below its name, without an empty First 24 hours", async () => {
    torrentAnswer = ({ instanceId, bucket }) => bucket === "total" && instanceId === 1 ? [torrentRow("h1", "Alpha", 3072, { uploadFirst24h: 1024 }, { downloaded: 1024 }), torrentRow("h2", "Bravo", 2048)] : []
    renderPage()
    const facts = async (name: string) => {
      const node = await within(await screen.findByRole("table")).findByText(name)
      return [...node.closest("td")?.querySelector("[data-narrow]")?.children ?? []].map(fact => fact.textContent)
    }

    expect(await facts("Alpha")).toEqual(["table.columns.size 1 KiB", "table.columns.downloaded 1 KiB", "table.columns.ratio 3.00", "topTorrents.columns.first24h 1 KiB"])
    expect(await facts("Bravo")).toEqual(["table.columns.size 1 KiB", "table.columns.downloaded 0 B", "table.columns.ratio ∞"])
  })

  it("shows the upload of a top torrent over its size as Seeded, a value for a cross-seed and none for a size of 0", async () => {
    torrentAnswer = ({ instanceId, bucket }) => bucket === "total" && instanceId === 1
      ? [torrentRow("h1", "Alpha", 3072, {}, { downloaded: 1024 }), torrentRow("h2", "Bravo", 2048, { statisticsCrossSeed: true, size: 4096 }), torrentRow("h3", "Charlie", 1024, { size: 0 })]
      : []
    renderPage()
    const table = await screen.findByRole("table")
    await within(table).findByText("Charlie")
    const header = [...table.querySelectorAll("thead th")].map(cell => cell.textContent)
    const seeded = (name: string) => cells(table, name)[header.indexOf("table.columns.seeded")]
    const belowUpload = (name: string) => within(table).getByText(name).closest("tr")?.querySelectorAll("td")[header.indexOf("table.columns.uploaded")].querySelector("[data-narrow]")?.textContent

    expect(header).toEqual(["topTorrents.columns.torrent", "table.columns.tracker", "table.columns.uploaded", "table.columns.downloaded", "table.columns.ratio", "table.columns.seeded", "topTorrents.columns.first24h"])
    expect([seeded("Alpha"), seeded("Bravo"), seeded("Charlie")]).toEqual(["table.seededValue value=3.00", "table.seededValue value=0.50", "–"])
    expect(cells(table, "Bravo")[header.indexOf("table.columns.ratio")]).toBe("∞")
    expect([belowUpload("Alpha"), belowUpload("Bravo"), belowUpload("Charlie")]).toEqual(["table.seededValue value=3.00", "table.seededValue value=0.50", "–"])
    expect(screen.getByText("topTorrents.seededNote")).toBeTruthy()
  })

  it("opens the tooltip of a mark on a tap, without opening the row", async () => {
    const touchWindow = window as Window & { ontouchstart?: unknown }
    touchWindow.ontouchstart = null
    try {
      torrentAnswer = ({ instanceId, bucket }) => bucket === "total" && instanceId === 1 ? [torrentRow("h1", "Alpha", 1024, { deleted: true })] : []
      renderPage()
      const table = await screen.findByRole("table")
      const icon = (await within(table).findByText("Alpha")).closest("td")!.querySelector("[role=img]")!

      fireEvent.pointerDown(icon, { pointerType: "touch" })
      fireEvent.pointerUp(icon, { pointerType: "touch" })
      fireEvent(icon, new PointerEvent("click", { bubbles: true, cancelable: true, pointerType: "mouse" }))

      expect(screen.getByRole("tooltip").textContent).toContain("topTorrents.deleted")
      expect(within(table).getByText("Alpha").closest("button")?.getAttribute("aria-expanded")).toBe("false")
    } finally {
      vi.unstubAllGlobals()
      delete touchWindow.ontouchstart
    }
  })

  it("shows a cut-off tracker on a tap, and the tap still opens the row", async () => {
    const touchWindow = window as Window & { ontouchstart?: unknown }
    touchWindow.ontouchstart = null
    vi.spyOn(HTMLElement.prototype, "scrollWidth", "get").mockReturnValue(200)
    vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockReturnValue(50)
    try {
      torrentAnswer = ({ instanceId, bucket }) => bucket === "total" && instanceId === 1 ? [torrentRow("h1", "Alpha", 1024)] : []
      renderPage()
      const table = await screen.findByRole("table")
      const row = (await within(table).findByText("Alpha")).closest("tr")!
      const tracker = row.querySelectorAll("td")[1].querySelector<HTMLElement>("[data-slot=truncated-text]")!

      fireEvent.pointerDown(tracker, { pointerType: "touch" })
      fireEvent.pointerUp(tracker, { pointerType: "touch" })
      fireEvent.click(tracker)

      expect(screen.getByRole("tooltip").textContent).toContain(tracker.textContent)
      expect(within(table).getByText("Alpha").closest("button")?.getAttribute("aria-expanded")).toBe("true")
    } finally {
      vi.unstubAllGlobals()
      delete touchWindow.ontouchstart
    }
  })

  it("shows a cut-off tracker on a tap on a phone, where the tracker sits under the name, and the tap still opens the row", async () => {
    const touchWindow = window as Window & { ontouchstart?: unknown }
    touchWindow.ontouchstart = null
    stubPhone("upright")
    vi.spyOn(HTMLElement.prototype, "scrollWidth", "get").mockReturnValue(200)
    vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockReturnValue(50)
    try {
      torrentAnswer = ({ instanceId, bucket }) => bucket === "total" && instanceId === 1 ? [torrentRow("h1", "Alpha", 1024)] : []
      renderPage()
      const table = await screen.findByRole("table")
      const row = (await within(table).findByText("Alpha")).closest("tr")!
      const [nameCell, trackerCell] = row.querySelectorAll("td")
      const trackerText = trackerCell.querySelector("[data-slot=truncated-text]")!.textContent
      const tracker = [...nameCell.querySelectorAll<HTMLElement>("[data-slot=truncated-text]")].find(element => element.textContent === trackerText)

      expect(tracker).toBeDefined()
      fireEvent.pointerDown(tracker!, { pointerType: "touch" })
      fireEvent.pointerUp(tracker!, { pointerType: "touch" })
      fireEvent.click(tracker!)

      expect(screen.getByRole("tooltip").textContent).toContain(trackerText)
      expect(within(table).getByText("Alpha").closest("button")?.getAttribute("aria-expanded")).toBe("true")
    } finally {
      delete touchWindow.ontouchstart
    }
  })

  it("opens the tooltip of a mark when the keyboard focuses it, outside the button of the row", async () => {
    torrentAnswer = ({ instanceId, bucket }) => bucket === "total" && instanceId === 1 ? [torrentRow("h1", "Alpha", 1024, { statisticsCrossSeed: true })] : []
    renderPage()
    const table = await screen.findByRole("table")
    const icon = (await within(table).findByText("Alpha")).closest("td")!.querySelector<HTMLElement>("[role=img]")!

    act(() => icon.focus())

    expect(document.activeElement).toBe(icon)
    expect(icon.closest("button")).toBeNull()
    await vi.waitFor(() => expect(screen.getByRole("tooltip").textContent).toContain("topTorrents.crossSeed"))
  })

  it("shows five top torrents on a phone, each after Show all, and five again after Show fewer", async () => {
    stubPhone("upright")
    torrentAnswer = ({ instanceId, bucket }) => bucket === "total" && instanceId === 1 ? Array.from({ length: 7 }, (_, index) => torrentRow(`h${index + 1}`, `Torrent ${index + 1}`, (7 - index) * 1024)) : []
    renderPage()
    const table = await screen.findByRole("table")
    await within(table).findByText("Torrent 5")
    expect(within(table).queryByText("Torrent 6")).toBeNull()
    expect(screen.getByText("table.shown shown=5 total=7")).toBeTruthy()

    fireEvent.click(screen.getByText("table.showAll"))

    expect(within(table).getByText("Torrent 7")).toBeTruthy()
    const scrollIntoView = vi.fn()
    Element.prototype.scrollIntoView = scrollIntoView
    fireEvent.click(screen.getByText("table.showFewer"))

    expect(within(table).queryByText("Torrent 6")).toBeNull()
    expect(screen.getByText("table.shown shown=5 total=7")).toBeTruthy()
    expect(scrollIntoView).toHaveBeenCalledOnce()
  })

  it("shows five top torrents on a phone turned sideways, which is wider than the phone layout", async () => {
    stubPhone("sideways")
    torrentAnswer = ({ instanceId, bucket }) => bucket === "total" && instanceId === 1 ? Array.from({ length: 7 }, (_, index) => torrentRow(`h${index + 1}`, `Torrent ${index + 1}`, (7 - index) * 1024)) : []
    renderPage()

    await within(await screen.findByRole("table")).findByText("Torrent 5")
    expect(within(screen.getByRole("table")).queryByText("Torrent 6")).toBeNull()
  })

  it("shows ten group rows and five top torrents again on a phone after a return to an earlier period", async () => {
    stubPhone("upright")
    Element.prototype.scrollIntoView = vi.fn()
    const now = new Date()
    const yesterday = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1)
    answer = ({ bucket }) => bucket === "day" || bucket === "hour" ? Array.from({ length: 12 }, (_, index) => row(`t${index + 1}.test`, (12 - index) * 1024, 0, { start: yesterday.toISOString(), width: bucket })) : []
    torrentAnswer = ({ instanceId, bucket }) => bucket === "total" && instanceId === 1 ? Array.from({ length: 7 }, (_, index) => torrentRow(`h${index + 1}`, `Torrent ${index + 1}`, (7 - index) * 1024)) : []
    const pickPeriod = async (shown: string, next: string) => {
      fireEvent.keyDown(screen.getAllByRole("combobox").find(picker => picker.textContent === shown)!, { key: "Enter" })
      fireEvent.click(await screen.findByRole("option", { name: next }))
    }
    renderPage()
    await screen.findByText("table.shown shown=10 total=12")
    await screen.findByText("table.shown shown=5 total=7")
    for (const button of screen.getAllByText("table.showAll")) {
      fireEvent.click(button)
    }
    expect(screen.getByText("table.shown shown=12 total=12")).toBeTruthy()

    await pickPeriod("periods.30d", "periods.7d")
    await screen.findByText("table.shown shown=10 total=12")
    await pickPeriod("periods.7d", "periods.30d")

    await vi.waitFor(() => expect(screen.getAllByRole("combobox").some(picker => picker.textContent === "periods.30d")).toBe(true))
    await screen.findByText("table.shown shown=10 total=12")
    expect(screen.getByText("table.shown shown=5 total=7")).toBeTruthy()
  })

  it("shows five top torrents again on a phone after another period", async () => {
    stubPhone("upright")
    Element.prototype.scrollIntoView = vi.fn()
    torrentAnswer = ({ instanceId, bucket }) => bucket === "total" && instanceId === 1 ? Array.from({ length: 7 }, (_, index) => torrentRow(`h${index + 1}`, `Torrent ${index + 1}`, (7 - index) * 1024)) : []
    renderPage()
    await within(await screen.findByRole("table")).findByText("Torrent 5")
    fireEvent.click(screen.getByText("table.showAll"))
    expect(within(screen.getByRole("table")).getByText("Torrent 7")).toBeTruthy()

    fireEvent.keyDown(screen.getAllByRole("combobox").find(picker => picker.textContent === "periods.30d")!, { key: "Enter" })
    fireEvent.click(await screen.findByRole("option", { name: "periods.7d" }))

    await within(await screen.findByRole("table")).findByText("Torrent 5")
    expect(within(screen.getByRole("table")).queryByText("Torrent 6")).toBeNull()
    expect(screen.getByText("table.showAll")).toBeTruthy()
  })

  it("keeps the top torrents on the period of the other numbers while a moved period loads", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true, toFake: ["setInterval", "clearInterval", "Date"] })
    try {
      let moved = false
      answer = ({ bucket }) => bucket === "day" ? [row("a.test", 1024, 0, { width: "day" })] : []
      torrentAnswer = ({ instanceId, bucket }) => instanceId === 1 && bucket === "total" ? [torrentRow("h1", moved ? "Bravo" : "Alpha", 2048)] : []
      const torrentTable = () => screen.getAllByRole("table").at(-1)!
      renderPage()
      await vi.waitFor(() => expect(within(torrentTable()).queryByText("Alpha")).toBeTruthy())
      moved = true
      requests = []
      let release = () => {}
      holdBucket = "day"
      hold = new Promise(resolve => {
        release = resolve
      })

      await act(async () => {
        await vi.advanceTimersByTimeAsync(5 * 60_000 + 60_000)
      })

      await vi.waitFor(() => expect(requests.some(request => request.groupBy === "torrent" && request.bucket === "total")).toBe(true))
      await act(async () => {
        await new Promise(resolve => setTimeout(resolve, 200))
      })
      expect(within(torrentTable()).queryByText("Alpha")).toBeTruthy()
      expect(within(torrentTable()).queryByText("Bravo")).toBeNull()
      release()
      await vi.waitFor(() => expect(within(torrentTable()).queryByText("Bravo")).toBeTruthy())
    } finally {
      vi.useRealTimers()
    }
  })

  it("keeps the top torrents of an open row while a moved period loads", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true, toFake: ["setInterval", "clearInterval", "Date"] })
    try {
      answer = ({ bucket }) => bucket === "day" ? [row("a.test", 3072, 0, { width: "day" })] : []
      torrentAnswer = ({ instanceId, groupOf, group }) => groupOf === "tracker" && group === "a.test" && instanceId === 1 ? [torrentRow("h1", "Alpha", 2048)] : []
      renderPage()
      fireEvent.click(await within((await screen.findAllByRole("table"))[0]).findByText("a.test"))
      const heading = await screen.findByText("detail.topTorrents group=a.test")
      await vi.waitFor(() => expect(heading.nextElementSibling?.textContent).toBe("Alpha2 KiB"))
      requests = []
      let release = () => {}
      holdGroupOf = "tracker"
      hold = new Promise(resolve => {
        release = resolve
      })

      await act(async () => {
        await vi.advanceTimersByTimeAsync(5 * 60_000 + 60_000)
      })

      await vi.waitFor(() => expect(requests.some(request => request.groupOf === "tracker")).toBe(true))
      expect(screen.getByText("detail.topTorrents group=a.test").nextElementSibling?.textContent).toBe("Alpha2 KiB")
      release()
    } finally {
      vi.useRealTimers()
    }
  })

  it("keeps the daily chart of an open torrent on the period of the shown rows while a moved period loads", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true, toFake: ["setInterval", "clearInterval", "Date"] })
    try {
      torrentAnswer = ({ instanceId, bucket, group }) => {
        if (instanceId !== 1) {
          return []
        }
        return bucket === "total" ? [torrentRow("h1", "Alpha", 2048)] : group === "h1" ? [torrentRow("h1", "Alpha", 2048, {}, { width: "day", start: new Date(Date.now() - DAY).toISOString() })] : []
      }
      renderPage()
      fireEvent.click(await within(await screen.findByRole("table")).findByText("Alpha"))
      await vi.waitFor(() => expect(screen.getByTestId("group-chart")).toBeTruthy())
      const firstEnd = requests.find(request => request.groupBy === "torrent" && request.bucket === "day")?.to
      requests = []
      let release = () => {}
      holdBucket = "total"
      hold = new Promise(resolve => {
        release = resolve
      })

      await act(async () => {
        await vi.advanceTimersByTimeAsync(5 * 60_000 + 60_000)
      })

      await vi.waitFor(() => expect(requests.some(request => request.groupBy === "torrent" && request.bucket === "total")).toBe(true))
      expect(requests.filter(request => request.groupBy === "torrent" && request.bucket === "day")).toEqual([])
      expect(screen.getByTestId("group-chart")).toBeTruthy()
      const releaseTotals = release
      holdBucket = "day"
      hold = new Promise(resolve => {
        release = resolve
      })
      releaseTotals()
      await vi.waitFor(() => expect(requests.some(request => request.groupBy === "torrent" && request.bucket === "day" && Date.parse(request.to ?? "") > Date.parse(firstEnd ?? ""))).toBe(true))
      expect(screen.getByTestId("group-chart")).toBeTruthy()
      release()
    } finally {
      vi.useRealTimers()
    }
  })

  it("shows the top torrents of a group in the detail band of its row", async () => {
    answer = ({ bucket }) => bucket === "day" ? [row("a.test", 3072, 0, { width: "day" })] : []
    torrentAnswer = ({ instanceId, groupOf, group, limit }) => groupOf === "tracker" && group === "a.test" && limit === "5" && instanceId === 1 ? [torrentRow("h1", "Alpha", 2048)] : []
    renderPage()
    const tables = await screen.findAllByRole("table")
    fireEvent.click(await within(tables[0]).findByText("a.test"))

    const heading = await screen.findByText("detail.topTorrents group=a.test")
    await vi.waitFor(() => expect(heading.nextElementSibling?.textContent).toBe("Alpha2 KiB"))
  })

  it("masks the torrent names in incognito mode", async () => {
    mocks.incognitoMode = [true, () => {}]
    torrentAnswer = ({ instanceId, bucket }) => bucket === "total" && instanceId === 1 ? [torrentRow("h1", "Private.Release.Name-GRP", 1024)] : []
    renderPage()

    const table = await screen.findByRole("table")
    await vi.waitFor(() => expect(within(table).getAllByRole("row")).toHaveLength(2))
    expect(table.textContent).not.toContain("Private.Release.Name-GRP")
    expect(table.textContent).not.toContain("a.test")
  })
  it("adds the library of the instances together for each day, with the newest Unique size as the fifth number", async () => {
    libraryAnswer = instanceId => [
      libraryRow(1, { torrents: instanceId, totalSize: instanceId * 4096, uniqueSize: instanceId * 1024 }),
      libraryRow(0, { torrents: instanceId * 10, totalSize: instanceId * 8192, uniqueSize: instanceId * 2048 }),
    ]
    renderPage()

    expect(await growthPoints()).toEqual([[3, 12288, 3072], [30, 24576, 6144]])
    await expectTile("tiles.unique", "6 KiB | tiles.uniqueTotal size=24 KiB")
    expect(screen.getByText("growth.description torrents=30")).toBeTruthy()
    expect(screen.queryByText("growth.gapNote")).toBeNull()
  })

  it("leaves a gap in the Unique size of a day without a Copy groups build, and takes the fifth number from the day before", async () => {
    libraryAnswer = instanceId => [
      libraryRow(3, { torrents: 1, totalSize: 4096, uniqueSize: 1024 }),
      libraryRow(1, { torrents: 1, totalSize: 4096, uniqueSize: instanceId === 1 ? undefined : 1024 }),
      libraryRow(0, { torrents: 1, totalSize: 4096 }),
    ]
    renderPage()

    expect(await growthPoints()).toEqual([[2, 8192, 2048], [null, null, null], [2, 8192, null], [2, 8192, null]])
    await expectTile("tiles.unique", "2 KiB | tiles.uniqueTotal size=8 KiB")
    expect(screen.getByText("growth.gapNote")).toBeTruthy()
  })

  it("adds the adds, completions, and deletes of the instances together for each UTC day of the period", async () => {
    libraryAnswer = instanceId => [libraryRow(2, { added: instanceId, completed: 1, deleted: 0 }), libraryRow(0, { added: 0, completed: 0, deleted: instanceId * 2 })]
    renderPage()

    const chart = await screen.findByTestId("events-chart")
    const days: number[][] = JSON.parse(chart.getAttribute("data-events") ?? "[]")
    expect(days.slice(-3)).toEqual([[3, 2, 0], [0, 0, 0], [0, 0, 6]])
  })

  it("leaves a gap on a day on which an instance that qui records has no row, and ends a removed instance at its last row", async () => {
    statuses = [recordedDaysAgo(1, "Seedbox", 400), recordedDaysAgo(2, "Old box", 400), recordedDaysAgo(3, "Home", 400)]
    libraryAnswer = instanceId => [3, 2, 1, 0].filter(daysAgo => instanceId === 1 ? daysAgo === 3 || daysAgo === 1 : instanceId === 2 ? daysAgo === 3 : true)
      .map(daysAgo => libraryRow(daysAgo, { torrents: 1, totalSize: instanceId * 1024, uniqueSize: instanceId * 1024 }))
    configuredIds = [1, 3]
    renderPage()

    expect(await growthPoints()).toEqual([[3, 6144, 6144], [null, null, null], [2, 4096, 4096], [null, null, null]])
    await expectTile("tiles.unique", "4 KiB | tiles.uniqueTotal size=4 KiB")
  })

  it("leaves a gap, not a zero, after the last row of a removed instance", async () => {
    configuredIds = []
    statuses = [recordedDaysAgo(1, "Old box", 400)]
    libraryAnswer = () => [libraryRow(3, { torrents: 1, totalSize: 4096, uniqueSize: 2048 }), libraryRow(2, { torrents: 1, totalSize: 4096, uniqueSize: 2048 })]
    renderPage()

    expect(await growthPoints()).toEqual([[1, 4096, 2048], [1, 4096, 2048], [null, null, null], [null, null, null]])
    await expectTile("tiles.unique", "2 KiB | tiles.uniqueTotal size=4 KiB")
  })

  it("leaves a gap on each day when an instance that qui records has no row in the period", async () => {
    libraryAnswer = instanceId => instanceId === 1 ? [libraryRow(1, { torrents: 1, totalSize: 4096, uniqueSize: 2048 }), libraryRow(0, { torrents: 1, totalSize: 4096, uniqueSize: 2048 })] : []
    renderPage()

    expect(await growthPoints()).toEqual([[null, null, null], [null, null, null]])
  })

  it("says that the period has no Unique size before the first Copy groups build", async () => {
    libraryAnswer = () => [libraryRow(0, { torrents: 1, totalSize: 4096 })]
    renderPage()

    await expectTile("tiles.unique", "– | tiles.uniqueNone")
  })

  it("keeps the description of the last grouping until the rows of the next grouping load", async () => {
    const now = new Date()
    const yesterday = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1)
    let release = () => {}
    answer = ({ bucket, groupBy }) => groupBy === "resolution" ? [row("1080p", 1024, 0, { width: "total" })] : bucket === "day" ? [row("a.test", 1024, 0, { start: yesterday.toISOString(), width: "day" })] : []
    renderPage()
    await within(await screen.findByRole("table")).findByText("a.test")
    hold = new Promise(resolve => { release = resolve })
    holdBucket = "total"
    fireEvent.mouseDown(screen.getByText("table.groupings.resolution"), { button: 0 })

    await vi.waitFor(() => expect(requests.some(request => request.groupBy === "resolution" && request.bucket === "total")).toBe(true))
    expect(screen.getByText("table.descriptionTracker")).toBeTruthy()
    expect(screen.queryByText("table.descriptionRelease")).toBeNull()

    release()
    expect(await screen.findByText("table.descriptionRelease")).toBeTruthy()
  })

  it("shows ten rows of the table again on a phone after another custom range", async () => {
    stubPhone("upright")
    const now = new Date()
    const yesterday = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1)
    const day = (daysAgo: number) => {
      const date = new Date(now.getFullYear(), now.getMonth(), now.getDate() - daysAgo)
      return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, "0")}-${String(date.getDate()).padStart(2, "0")}`
    }
    const applyRange = async (firstDaysAgo: number) => {
      fireEvent.click(screen.getByRole("button", { name: "periods.custom" }))
      fireEvent.change(await screen.findByLabelText("customRange.from"), { target: { value: day(firstDaysAgo) } })
      fireEvent.change(screen.getByLabelText("customRange.to"), { target: { value: day(0) } })
      fireEvent.click(screen.getByText("customRange.apply"))
    }
    answer = ({ bucket }) => bucket === "day" || bucket === "hour" ? Array.from({ length: 12 }, (_, index) => row(`t${index + 1}.test`, (12 - index) * 1024, 0, { start: yesterday.toISOString(), width: bucket })) : []
    renderPage()
    await applyRange(10)
    await within(await screen.findByRole("table")).findByText("t10.test")
    fireEvent.click(screen.getByText("table.showAll"))
    expect(within(screen.getByRole("table")).getByText("t12.test")).toBeTruthy()

    await applyRange(5)

    await vi.waitFor(() => expect(screen.getByText("table.shown shown=10 total=12")).toBeTruthy())
    expect(within(screen.getByRole("table")).queryByText("t11.test")).toBeNull()
  })

  it("keeps the rows of the last grouping, dimmed, until the rows of the next grouping load", async () => {
    const now = new Date()
    const yesterday = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1)
    let release = () => {}
    answer = ({ bucket, groupBy }) => groupBy === "resolution" ? [row("1080p", 1024, 0, { width: "total" })] : bucket === "day" ? [row("a.test", 1024, 0, { start: yesterday.toISOString(), width: "day" })] : []
    renderPage()
    await within(await screen.findByRole("table")).findByText("a.test")
    hold = new Promise(resolve => { release = resolve })
    holdBucket = "total"
    fireEvent.mouseDown(screen.getByText("table.groupings.resolution"), { button: 0 })

    await vi.waitFor(() => expect(requests.some(request => request.groupBy === "resolution" && request.bucket === "total")).toBe(true))
    expect(within(screen.getByRole("table")).getByText("a.test")).toBeTruthy()
    expect(screen.getByRole("table").closest("[aria-busy]")?.getAttribute("aria-busy")).toBe("true")

    release()
    await vi.waitFor(() => expect(cells(screen.getByRole("table"), "1080p")[1]).toBe("2 KiB"))
    expect(screen.getByRole("table").closest("[aria-busy]")?.getAttribute("aria-busy")).toBe("false")
  })

  it("puts the live numbers of a Tracker group in its detail band, for a narrow table that has no column for them", async () => {
    const now = new Date()
    const yesterday = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1)
    answer = ({ bucket }) => bucket === "day" ? [row("a.test", 1024, 0, { start: yesterday.toISOString(), width: "day" })] : []
    liveGroups = () => [{ group: "a.test", torrents: 2, size: 2048, uploaded: 4096 }]
    renderPage()
    const table = await screen.findByRole("table")
    await vi.waitFor(() => expect(cells(table, "a.test")[5]).toBe("4"))
    fireEvent.click(within(table).getByText("a.test"))

    const facts = (await screen.findByText("detail.share")).closest("dl")!
    expect(within(facts).getByText("table.columns.torrents").nextElementSibling?.textContent).toBe("4")
    expect(within(facts).getByText("table.columns.size").nextElementSibling?.textContent).toBe("4 KiB")
    expect(within(facts).getByText("table.columns.seeded").nextElementSibling?.textContent).toBe("table.seededValue value=2.00")
  })

  it("puts the Buffer of a group in its detail band, for a phone row that has no room for it", async () => {
    const now = new Date()
    const yesterday = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1)
    answer = ({ bucket }) => bucket === "day" ? [row("a.test", 3072, 1024, { start: yesterday.toISOString(), width: "day" })] : []
    renderPage()
    fireEvent.click(await within(await screen.findByRole("table")).findByText("a.test"))

    const facts = (await screen.findByText("detail.share")).closest("dl")!
    expect(within(facts).getByText("table.columns.buffer").nextElementSibling?.textContent).toBe("+4 KiB")
  })

  it("shows no rows of the last period while the rows of another period load", async () => {
    const now = new Date()
    const yesterday = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1)
    let release = () => {}
    answer = ({ bucket }) => bucket === "day" || bucket === "hour" ? [row("a.test", 1024, 0, { start: yesterday.toISOString(), width: bucket })] : []
    renderPage()
    await within(await screen.findByRole("table")).findByText("a.test")
    hold = new Promise(resolve => { release = resolve })
    fireEvent.mouseDown(screen.getByText("periods.7d"), { button: 0 })

    await vi.waitFor(() => expect(screen.queryByRole("table")).toBeNull())
    release()
    await within(await screen.findByRole("table")).findByText("a.test")
  })

  it("leaves the year of this year out of the custom range on a phone", async () => {
    stubPhone("upright")
    const year = new Date().getFullYear()
    renderPage()
    fireEvent.click(await screen.findByRole("button", { name: "periods.custom" }))
    fireEvent.change(screen.getByLabelText("customRange.from"), { target: { value: `${year}-01-01` } })
    fireEvent.change(screen.getByLabelText("customRange.to"), { target: { value: `${year}-01-01` } })
    fireEvent.click(screen.getByText("customRange.apply"))

    const day = new Date(year, 0, 1).toLocaleDateString("en", { day: "numeric", month: "short" })
    expect(await screen.findByText(`customRange.range from=${day} to=${day}`)).toBeTruthy()
  })

  it("loads the rows of a grouping once when the pointer reaches its tab, and shows them at once on a click", async () => {
    const preloads = () => requests.filter(request => request.bucket === "total" && request.groupBy !== "tracker" && request.groupBy !== "torrent")
    answer = ({ bucket, groupBy }) => groupBy === "category" && bucket === "total" ? [row("tv", 1024, 0, { width: "total" })] : []
    const { queryClient } = renderPage()
    await vi.waitFor(() => expect(requests.some(request => request.bucket === "day")).toBe(true))
    expect(preloads()).toHaveLength(0)
    const category = screen.getByText("table.groupings.category")

    fireEvent.pointerEnter(category)

    await vi.waitFor(() => expect(preloads().map(request => request.groupBy)).toEqual(["category", "category"]))
    await vi.waitFor(() => expect(queryClient.isFetching()).toBe(0))

    fireEvent.pointerLeave(category)
    fireEvent.pointerEnter(category)
    fireEvent.pointerEnter(screen.getByText("table.groupings.source"))

    await vi.waitFor(() => expect(queryClient.isFetching()).toBe(0))
    expect(preloads().map(request => request.groupBy).sort()).toEqual(["category", "category", "source", "source"])

    fireEvent.mouseDown(category, { button: 0 })

    expect(cells(screen.getByRole("table"), "tv")[1]).toBe("2 KiB")
    await vi.waitFor(() => expect(queryClient.isFetching()).toBe(0))
  })

  it("loads the rows of each other grouping when a phone opens the grouping picker, and shows them at once on a pick", async () => {
    stubPhone("upright")
    Element.prototype.scrollIntoView = vi.fn()
    const preloads = () => new Set(requests.filter(request => request.bucket === "total" && request.groupBy !== "tracker" && request.groupBy !== "torrent").map(request => request.groupBy))
    answer = ({ bucket, groupBy }) => groupBy === "resolution" && bucket === "total" ? [row("1080p", 1024, 0, { width: "total" })] : []
    const { queryClient } = renderPage()
    await vi.waitFor(() => expect(requests.some(request => request.bucket === "day")).toBe(true))
    expect(preloads().size).toBe(0)

    const picker = screen.getByText("table.groupBy").parentElement!.querySelector("[role=combobox]")!
    fireEvent.keyDown(picker, { key: "Enter" })

    await vi.waitFor(() => expect(preloads()).toEqual(new Set(["category", "releaseGroup", "resolution", "source", "contentType"])))
    await vi.waitFor(() => expect(queryClient.isFetching()).toBe(0), { timeout: 5000 })

    fireEvent.click(await screen.findByRole("option", { name: "table.groupings.resolution" }))

    expect(within(screen.getByRole("table")).getByText("1080p")).toBeTruthy()
    await vi.waitFor(() => expect(queryClient.isFetching()).toBe(0))
  })

  it("shows ten rows of the table on a phone, each row after Show all, and ten again after Show fewer", async () => {
    stubPhone("upright")
    const now = new Date()
    const yesterday = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1)
    answer = ({ bucket }) => bucket === "day" ? Array.from({ length: 12 }, (_, index) => row(`t${index + 1}.test`, (12 - index) * 1024, 0, { start: yesterday.toISOString(), width: "day" })) : []
    renderPage()
    const table = await screen.findByRole("table")
    await within(table).findByText("t10.test")
    expect(within(table).queryByText("t11.test")).toBeNull()
    expect(screen.getByText("table.shown shown=10 total=12")).toBeTruthy()

    fireEvent.click(screen.getByText("table.showAll"))

    expect(within(table).getByText("t12.test")).toBeTruthy()
    expect(screen.queryByText("table.showAll")).toBeNull()

    const scrollIntoView = vi.fn()
    Element.prototype.scrollIntoView = scrollIntoView
    fireEvent.click(screen.getByText("table.showFewer"))

    expect(within(table).queryByText("t11.test")).toBeNull()
    expect(screen.getByText("table.shown shown=10 total=12")).toBeTruthy()
    expect(scrollIntoView).toHaveBeenCalledOnce()
  })

  it("gives the table no header that sorts it on a phone turned sideways, which shows the number columns", async () => {
    stubPhone("sideways")
    const now = new Date()
    const yesterday = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1)
    answer = ({ bucket }) => bucket === "day" ? [row("a.test", 2048, 1024, { start: yesterday.toISOString(), width: "day" })] : []
    renderPage()
    const table = await screen.findByRole("table")
    await within(table).findByText("a.test")

    expect(columnHeader(table, "table.columns.downloaded").closest("button")).toBeNull()
    expect(within(table.querySelector("thead")!).queryAllByRole("button")).toEqual([])
  })

  it("keeps each row of the last grouping on a phone after Show all until the next grouping loads, then shows ten of it", async () => {
    stubPhone("upright")
    Element.prototype.scrollIntoView = vi.fn()
    const now = new Date()
    const yesterday = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1)
    let release = () => {}
    answer = ({ bucket, groupBy }) => groupBy === "resolution"
      ? Array.from({ length: 12 }, (_, index) => row(`r${index + 1}`, (12 - index) * 1024, 0, { width: "total" }))
      : bucket === "day" ? Array.from({ length: 12 }, (_, index) => row(`t${index + 1}.test`, (12 - index) * 1024, 0, { start: yesterday.toISOString(), width: "day" })) : []
    renderPage()
    await within(await screen.findByRole("table")).findByText("t10.test")
    fireEvent.click(screen.getByText("table.showAll"))
    hold = new Promise(resolve => { release = resolve })
    holdBucket = "total"

    fireEvent.keyDown(screen.getByText("table.groupBy").parentElement!.querySelector("[role=combobox]")!, { key: "Enter" })
    fireEvent.click(await screen.findByRole("option", { name: "table.groupings.resolution" }))

    await vi.waitFor(() => expect(screen.getByRole("table").closest("[aria-busy]")?.getAttribute("aria-busy")).toBe("true"))
    expect(within(screen.getByRole("table")).getByText("t12.test")).toBeTruthy()

    release()
    await within(screen.getByRole("table")).findByText("r10")
    expect(within(screen.getByRole("table")).queryByText("r11")).toBeNull()
  })
})

describe("Statistics year view", () => {
  const KiB = 1024

  function daysAgo(days: number): Date {
    const now = new Date()
    return new Date(now.getFullYear(), now.getMonth(), now.getDate() - days)
  }

  function dayRow(group: string, day: Date, uploaded: number, gapSeconds = 0): TransferHistoryRow {
    return row(group, uploaded, 0, { start: day.toISOString(), width: "day", gapSeconds })
  }

  function inputDate(day: Date): string {
    return `${day.getFullYear()}-${String(day.getMonth() + 1).padStart(2, "0")}-${String(day.getDate()).padStart(2, "0")}`
  }

  async function applyRange(first: Date, last: Date) {
    fireEvent.click(await screen.findByText(/^(periods\.custom|customRange\.range from=.*)$/))
    fireEvent.change(screen.getByLabelText("customRange.from"), { target: { value: inputDate(first) } })
    fireEvent.change(screen.getByLabelText("customRange.to"), { target: { value: inputDate(last) } })
    fireEvent.click(screen.getByText("customRange.apply"))
  }

  function calendarCells(): HTMLElement[] {
    return [...screen.getByRole("list", { name: "yearView.calendar" }).querySelectorAll<HTMLElement>("li[aria-label]")]
  }

  async function expectRecord(label: string, expected: string) {
    await vi.waitFor(() => {
      const value = screen.getByText(label).nextElementSibling
      expect(`${value?.textContent} | ${value?.nextElementSibling?.textContent ?? ""}`).toBe(expected)
    })
  }

  it("shows the year view for a range of 121 days and the stacked bars for 120 days", async () => {
    answer = ({ instanceId, bucket }) => instanceId === 1 && bucket === "day" ? [dayRow("a.test", daysAgo(150), KiB)] : []
    renderPage()
    await applyRange(daysAgo(200), daysAgo(80))

    expect(await screen.findByRole("list", { name: "yearView.calendar" })).toBeTruthy()
    expect(screen.getByText("yearView.title")).toBeTruthy()
    expect(screen.queryByText("chart.download")).toBeNull()

    await applyRange(daysAgo(200), daysAgo(81))

    await vi.waitFor(() => expect(screen.getByText("chart.title")).toBeTruthy())
    expect(screen.queryByRole("list", { name: "yearView.calendar" })).toBeNull()
    expect(screen.getByText("chart.download")).toBeTruthy()
  })

  it("takes the best day, the best month, and the weekend share from the rows of the transfer query, and marks a Recording gap", async () => {
    let saturday = daysAgo(80)
    while (saturday.getDay() !== 6 || saturday.getDate() > 20) {
      saturday = new Date(saturday.getFullYear(), saturday.getMonth(), saturday.getDate() - 1)
    }
    const monday = new Date(saturday.getFullYear(), saturday.getMonth(), saturday.getDate() + 2)
    let other = new Date(saturday.getFullYear(), saturday.getMonth() + 1, 15)
    while (other.getDay() !== 3) {
      other = new Date(other.getFullYear(), other.getMonth(), other.getDate() + 1)
    }
    answer = ({ instanceId, bucket }) => instanceId === 1 && bucket === "day"
      ? [dayRow("a.test", saturday, 300 * KiB), dayRow("b.test", monday, 100 * KiB, 5400), dayRow("a.test", other, 600 * KiB)]
      : []
    renderPage()
    fireEvent.mouseDown(await screen.findByText("periods.1y"), { button: 0 })

    await expectRecord("yearView.bestDay", `600 KiB | ${isoDay(other)}`)
    await expectRecord("yearView.bestMonth", `${other.toLocaleDateString("en", { month: "long", year: "numeric" })} | 600 KiB`)
    await expectRecord("yearView.weekends", "30% | yearView.weekendsDetail")
    expect(screen.getByTestId("chart").getAttribute("data-uploaded")).toBe(String(1000 * KiB))

    const gaps = calendarCells().filter(cell => cell.dataset.gap)
    expect(gaps).toHaveLength(1)
    expect(gaps[0].getAttribute("aria-label")).toContain("uploaded=100 KiB")
    expect(gaps[0].getAttribute("aria-label")).toContain("chart.gap duration=1h 30m")
  })

  it("shows the last 53 weeks of all time on a history of more than two years", async () => {
    statuses = [recordedDaysAgo(1, "Seedbox", 800)]
    configuredIds = [1]
    answer = ({ bucket }) => bucket === "day" ? [dayRow("a.test", daysAgo(790), 900 * KiB), dayRow("a.test", daysAgo(1), 100 * KiB)] : []
    renderPage()
    fireEvent.mouseDown(await screen.findByText("periods.all"), { button: 0 })

    await expectRecord("yearView.bestDay", `900 KiB | ${isoDay(daysAgo(790))}`)
    const cells = calendarCells()
    expect(cells).toHaveLength(53 * 7)
    const date = (day: Date) => `${day.toLocaleDateString("en", { weekday: "short" })}, ${isoDay(day)}`
    expect(cells[0].getAttribute("aria-label")).toBe(`yearView.day date=${date(daysAgo(370))} uploaded=0 B`)
    expect(cells[cells.length - 2].getAttribute("aria-label")).toBe(`yearView.day date=${date(daysAgo(1))} uploaded=100 KiB`)
    expect(screen.getByTestId("chart").getAttribute("data-uploaded")).toBe(String(1000 * KiB))
  })

  it("shows the date and the upload of a tapped day above the calendar", async () => {
    answer = ({ instanceId, bucket }) => instanceId === 1 && bucket === "day" ? [dayRow("a.test", daysAgo(1), 100 * KiB)] : []
    renderPage()
    fireEvent.mouseDown(await screen.findByText("periods.1y"), { button: 0 })
    await screen.findByRole("list", { name: "yearView.calendar" })
    const cell = calendarCells().find(cell => cell.getAttribute("aria-label")?.includes("uploaded=100 KiB"))!

    fireEvent.click(cell)

    const readout = screen.getByText(cell.getAttribute("aria-label")!)
    expect(readout.tagName).toBe("SPAN")
  })

  it("shows weeks in the chart of the detail band of a long period", async () => {
    answer = ({ bucket }) => bucket === "day" ? [dayRow("a.test", daysAgo(1), KiB)] : []
    renderPage()
    fireEvent.mouseDown(await screen.findByText("periods.1y"), { button: 0 })
    await screen.findByRole("list", { name: "yearView.calendar" })
    fireEvent.click(await within(await screen.findByRole("table")).findByText("a.test"))

    expect(await screen.findByText("detail.chartWeek group=a.test")).toBeTruthy()
    await vi.waitFor(() => expect([53, 54]).toContain(Number(screen.getByTestId("group-chart").getAttribute("data-bars"))))
  })
})
