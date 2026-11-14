/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { HttpResponse, http } from "msw"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import "@/i18n"
import { Statistics } from "@/pages/Statistics"
import { server } from "@/test/msw/server"
import type { StatisticsBar } from "@/lib/statistics"
import type { EventDay } from "@/lib/statistics-events"
import type { LibraryPoint } from "@/lib/statistics-library"
import type { LiveTrackerGroup, TransferHistoryInstance, TransferHistoryLibraryRow, TransferHistoryRow, TransferHistoryTorrent } from "@/types"

const mocks = vi.hoisted(() => {
  const t = (key: string, options?: Record<string, unknown>) => {
    const values = Object.entries(options ?? {}).filter(([name]) => name !== "count" || options?.count !== undefined)
    return values.length > 0 ? `${key} ${values.map(([name, value]) => `${name}=${String(value)}`).join(" ")}` : key
  }
  return {
    i18n: { t, i18n: { resolvedLanguage: "en", language: "en" } },
    incognitoMode: [false, () => {}] as [boolean, () => void],
  }
})

vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => mocks.i18n,
}))
vi.mock("@/lib/incognito", async (importOriginal) => ({
  ...await importOriginal<typeof import("@/lib/incognito")>(),
  useIncognitoMode: () => mocks.incognitoMode,
}))
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
// The answer of a query by torrent. The mock limits the rows to the group of a query for one torrent.
let torrentAnswer: (request: TransferRequest) => TransferHistoryRow[]
let requests: TransferRequest[]
let liveGroups: (instanceId: number) => LiveTrackerGroup[]
let libraryAnswer: (instanceId: number) => TransferHistoryLibraryRow[]
// While set, the transfer route waits for it before it answers: for each bucket, or only for holdBucket.
let hold: Promise<void> | undefined
let holdBucket: string | undefined
// While set, the route holds only the queries with this groupOf.
let holdGroupOf: string | undefined

function row(group: string, uploaded: number, downloaded: number, extra: Partial<TransferHistoryRow> = {}): TransferHistoryRow {
  return { group, start: new Date(Date.now() - DAY).toISOString(), width: "hour", gapSeconds: 0, uploaded, downloaded, statisticsCrossSeedUploaded: 0, ...extra }
}

function torrentRow(hash: string, name: string, uploaded: number, torrent: Partial<TransferHistoryTorrent> = {}, extra: Partial<TransferHistoryRow> = {}): TransferHistoryRow {
  return row(hash, uploaded, 0, {
    width: "total",
    torrent: { name, trackerGroup: "a.test", deleted: false, statisticsCrossSeed: false, uploadFirst24h: null, ...torrent },
    ...extra,
  })
}

/** A library row of the UTC day that is daysAgo days before today. */
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
  return render(
    <QueryClientProvider client={queryClient}>
      <Statistics />
    </QueryClientProvider>
  )
}

async function expectTile(label: string, expected: string) {
  const labelNode = await screen.findByText(label)
  await vi.waitFor(() => {
    const value = labelNode.nextElementSibling
    expect(`${value?.textContent} | ${value?.nextElementSibling?.textContent}`).toBe(expected)
  })
}

/** Returns the text of each cell of the table row whose first cell holds name. */
function cells(table: HTMLElement, name: string): string[] {
  const nameNode = within(table).getByText(name)
  return [...(nameNode.closest("tr")?.querySelectorAll("td") ?? [])].map(cell => cell.textContent ?? "")
}

function expandedRows(table: HTMLElement): string[] {
  return [...table.querySelectorAll("button[aria-expanded=true]")].map(button => button.textContent ?? "")
}

async function expectFact(label: string, expected: string) {
  await vi.waitFor(() => {
    const value = screen.getByText(label).nextElementSibling
    expect(`${value?.textContent} | ${value?.nextElementSibling?.textContent ?? ""}`).toBe(expected)
  })
}

beforeEach(() => {
  vi.stubGlobal("ResizeObserver", class {
    observe() {}
    unobserve() {}
    disconnect() {}
  })
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
  libraryAnswer = () => []
  server.use(
    http.get("*/api/instances/:id/transfer-history/library", ({ params }) => HttpResponse.json({ rows: libraryAnswer(Number(params.id)) })),
    http.get("*/api/instances/:id/transfer-history/deletes", () => HttpResponse.json({ deletes: [], counts: { automation: 0, user: 0, other: 0 } })),
    http.get("*/api/instances/:id/tracker-groups", ({ params }) => HttpResponse.json({ groups: liveGroups(Number(params.id)) })),
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

  // regression: #4132
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
    await screen.findAllByTitle("First.Range-GRP")

    applyRange("2026-01-07")
    await vi.waitFor(() => expect(screen.queryAllByTitle("First.Range-GRP")).toHaveLength(0))
    releaseSecond()
    await screen.findAllByTitle("Second.Range-GRP")
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

  // regression: #4084
  it("does not show the numbers of other instances when the period moves while a new selection loads", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true, toFake: ["setInterval", "clearInterval", "Date"] })
    try {
      answer = ({ bucket }) => bucket === "day" ? [row("a.test", 1024, 0, { width: "day" })] : []
      renderPage()
      fireEvent.pointerDown(await screen.findByText("instances.all"), { button: 0, pointerType: "mouse" })
      fireEvent.click(await screen.findByText("Seedbox"))
      await expectTile("tiles.uploaded", "1 KiB | ")
      // The period moves at the tick of minute 5, and next at the tick of minute 10.
      await act(async () => {
        await vi.advanceTimersByTimeAsync(9 * 60_000 + 30_000)
      })
      await expectTile("tiles.uploaded", "1 KiB | ")
      hold = new Promise(() => {})
      // Home has no rows for the moved period yet.
      fireEvent.click(await screen.findByText("Home"))

      await act(async () => {
        await vi.advanceTimersByTimeAsync(60_000)
      })

      await expectTile("tiles.uploaded", "– | ")
    } finally {
      vi.useRealTimers()
    }
  })

  // regression: #4084
  it("keeps the totals and the change of one period until each query of the moved period loads", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true, toFake: ["setInterval", "clearInterval", "Date"] })
    try {
      // The comparison ends at a whole hour, so only a move into the next hour gives it new queries.
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

  // regression: #4084
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

  it("shows the change against the period before only when qui recorded that period in full", async () => {
    const runningHour = new Date(Math.floor(Date.now() / 3_600_000) * 3_600_000).toISOString()
    answer = ({ bucket, to }) => {
      if (bucket === "day") {
        return [row("a.test", 4096, 1024, { width: "day" })]
      }
      // The period ends at the start of the hour that still runs. The period before ends 30 days back.
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

    // The chart and the table both read the rows of the chart. The top torrents read the instance too.
    await vi.waitFor(() => expect(screen.getAllByText("errors.load message=database is locked")).toHaveLength(3))
    expect(screen.queryByText("chart.empty")).toBeNull()
  })

  // regression: #4099
  it("shows the error in the table when the status fails", async () => {
    server.use(http.get("*/api/transfer-history/status", () => HttpResponse.json({ error: "database is locked" }, { status: 500 })))
    renderPage()

    // The line at the top of the page, the table, the top torrents, the library growth, the adds and deletes, and the deleted torrents show it.
    await vi.waitFor(() => expect(screen.getAllByText("errors.load message=database is locked")).toHaveLength(6))
  })

  it("says that a period with no transfer has none", async () => {
    renderPage()

    // The chart, the table, and the top torrents say so.
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

  // regression: #4099
  it("keeps the live numbers of the instances that loaded when one instance fails", async () => {
    answer = ({ bucket }) => bucket === "day" ? [row("a.test", 1024, 0, { width: "day" })] : []
    liveGroups = () => [{ group: "a.test", torrents: 2, size: 2048, uploaded: 4096 }]
    server.use(http.get("*/api/instances/2/tracker-groups", () => HttpResponse.json({ error: "Instance is disabled" }, { status: 409 })))
    renderPage()

    const table = await screen.findByRole("table")
    await vi.waitFor(() => expect(cells(table, "a.test")[5]).toBe("2"))
    expect(await screen.findByText(/^table\.liveError message=/)).toBeTruthy()
  })

  // regression: #4099
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
    await expectFact("detail.bestDay", `6 KiB | ${yesterday.toLocaleDateString("en", { day: "numeric", month: "short", year: "numeric" })}`)
    // Both instances recorded the whole 30 days, so the average divides by each day of the period.
    await expectFact("detail.averageDay", "204.8 B | ")
    expect(screen.getByTestId("group-chart")).toBeTruthy()

    fireEvent.click(within(table).getByText("b.test"))

    await vi.waitFor(() => expect(expandedRows(table)).toEqual(["b.test"]))
    expect(screen.getAllByText("detail.share")).toHaveLength(1)
    await expectFact("detail.share", "25% | detail.shareOf total=8 KiB")
  })

  // regression: #4099
  it("leaves the days before the first recording out of the average of the detail band", async () => {
    const now = new Date()
    const yesterday = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1)
    statuses = [{ instanceId: 1, name: "Seedbox", firstRecordedAt: new Date(now.getFullYear(), now.getMonth(), now.getDate() - 2, 1).toISOString() }]
    answer = ({ bucket }) => bucket === "day" ? [row("a.test", 3072, 0, { start: yesterday.toISOString(), width: "day" })] : []
    renderPage()
    fireEvent.click(await within(await screen.findByRole("table")).findByText("a.test"))

    // The first recording is at 01:00 two days ago, so the average counts that day, yesterday, and today.
    await expectFact("detail.averageDay", "1 KiB | ")
  })

  // regression: #4114
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
    renderPage()
    const table = await screen.findByRole("table")
    await vi.waitFor(() => expect(within(table).getAllByRole("row").length).toBeGreaterThan(2))
    expect(table.textContent).not.toContain("tracker.private.test")

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
    expect(names).toEqual(["Alpha", "CharlietopTorrents.crossSeed", "BravotopTorrents.deleted"])
    expect(cells(table, "Alpha").slice(1)).toEqual(["a.test", "3 KiB", "0 B", "∞", "1 KiB"])
    const asked = requests.filter(request => request.groupBy === "torrent")
    expect(asked.map(request => [request.instanceId, request.bucket, request.limit, request.group])).toEqual([[1, "total", "10", null], [2, "total", "10", null]])
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
    // The 30 days of the period start at a local midnight, so they touch 30 or 31 UTC days.
    expect(Number(screen.getByTestId("group-chart").getAttribute("data-bars"))).toBeGreaterThanOrEqual(30)
    expect(screen.getByText("topTorrents.utcNote")).toBeTruthy()
  })

  // regression: #4113
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
      // The chart of the new period still loads, so the card keeps the torrents of the old period.
      expect(within(torrentTable()).queryByText("Alpha")).toBeTruthy()
      expect(within(torrentTable()).queryByText("Bravo")).toBeNull()
      release()
      await vi.waitFor(() => expect(within(torrentTable()).queryByText("Bravo")).toBeTruthy())
    } finally {
      vi.useRealTimers()
    }
  })

  // regression: #4113
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

      // The page loads the moved period, then the open row asks for the torrents of its group, which wait.
      await vi.waitFor(() => expect(requests.some(request => request.groupOf === "tracker")).toBe(true))
      expect(screen.getByText("detail.topTorrents group=a.test").nextElementSibling?.textContent).toBe("Alpha2 KiB")
      release()
    } finally {
      vi.useRealTimers()
    }
  })

  // regression: #4113
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
      // The table still shows the rows of the old period, so the open chart stays on that period.
      expect(requests.filter(request => request.groupBy === "torrent" && request.bucket === "day")).toEqual([])
      expect(screen.getByTestId("group-chart")).toBeTruthy()
      // The day rows of the new period wait, and the open chart keeps its bars meanwhile.
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
      // Instance 1 has no build on this day, so the sum of the two instances would draw a drop.
      libraryRow(1, { torrents: 1, totalSize: 4096, uniqueSize: instanceId === 1 ? undefined : 1024 }),
      libraryRow(0, { torrents: 1, totalSize: 4096 }),
    ]
    renderPage()

    // qui stored nothing for the day 2 days ago, so both lines have a gap there.
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
    // Instance 1 has no row 2 days ago and none yet today. Instance 2 is removed after 3 days ago, so its last row ends it.
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
    // After a custom range, the button of the picker shows that range.
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
    // A Saturday early in a month at least 80 days ago, the Monday after it, and a Wednesday of the next month.
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

    await expectRecord("yearView.bestDay", `600 KiB | ${other.toLocaleDateString("en", { day: "numeric", month: "short", year: "numeric" })}`)
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

    await expectRecord("yearView.bestDay", `900 KiB | ${daysAgo(790).toLocaleDateString("en", { day: "numeric", month: "short", year: "numeric" })}`)
    const cells = calendarCells()
    expect(cells).toHaveLength(53 * 7)
    const date = (day: Date) => day.toLocaleDateString("en", { weekday: "short", day: "numeric", month: "short", year: "numeric" })
    expect(cells[0].getAttribute("aria-label")).toBe(`yearView.day date=${date(daysAgo(370))} uploaded=0 B`)
    expect(cells[cells.length - 2].getAttribute("aria-label")).toBe(`yearView.day date=${date(daysAgo(1))} uploaded=100 KiB`)
    // The monthly bars cover the whole period.
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
    // 365 days touch 53 or 54 weeks from Monday on.
    await vi.waitFor(() => expect([53, 54]).toContain(Number(screen.getByTestId("group-chart").getAttribute("data-bars"))))
  })
})
