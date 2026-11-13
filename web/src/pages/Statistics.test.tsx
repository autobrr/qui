/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { HttpResponse, http } from "msw"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import "@/i18n"
import { Statistics } from "@/pages/Statistics"
import { server } from "@/test/msw/server"
import type { StatisticsBar } from "@/lib/statistics"
import type { TransferHistoryInstance, TransferHistoryRow } from "@/types"

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

const DAY = 86_400_000

interface TransferRequest {
  instanceId: number
  bucket: string
  from: string | null
  to: string | null
  timeZone: string | null
}

let statuses: TransferHistoryInstance[]
let configuredIds: number[]
let answer: (request: TransferRequest) => TransferHistoryRow[]
let requests: TransferRequest[]
// While set, the transfer route waits for it before it answers.
let hold: Promise<void> | undefined

function row(group: string, uploaded: number, downloaded: number, extra: Partial<TransferHistoryRow> = {}): TransferHistoryRow {
  return { group, start: new Date(Date.now() - DAY).toISOString(), width: "hour", gapSeconds: 0, uploaded, downloaded, statisticsCrossSeedUploaded: 0, ...extra }
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

beforeEach(() => {
  vi.stubGlobal("ResizeObserver", class {
    observe() {}
    unobserve() {}
    disconnect() {}
  })
  mocks.incognitoMode = [false, () => {}]
  requests = []
  hold = undefined
  configuredIds = [1, 2]
  statuses = [recordedDaysAgo(1, "Seedbox", 400), recordedDaysAgo(2, "Home", 400)]
  answer = () => []
  server.use(
    http.get("*/api/transfer-history/status", () => HttpResponse.json({ instances: statuses })),
    http.get("*/api/instances", () => HttpResponse.json(configuredIds.map(id => ({ id, name: `instance ${id}`, isActive: true })))),
    http.get("*/api/instances/:id/transfer-history", async ({ params, request }) => {
      const url = new URL(request.url)
      const transfer = { instanceId: Number(params.id), bucket: url.searchParams.get("bucket") ?? "", from: url.searchParams.get("from"), to: url.searchParams.get("to"), timeZone: url.searchParams.get("timeZone") }
      requests.push(transfer)
      await hold
      return HttpResponse.json({ rows: answer(transfer) })
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
    expect(requests.filter(request => request.bucket === "total")).toEqual([])
  })

  it("shows the error in the chart when one instance fails", async () => {
    server.use(http.get("*/api/instances/2/transfer-history", () => HttpResponse.json({ error: "database is locked" }, { status: 500 })))
    renderPage()

    expect(await screen.findByText("errors.load message=database is locked")).toBeTruthy()
    expect(screen.queryByText("chart.empty")).toBeNull()
  })

  it("says that a period with no transfer has none", async () => {
    renderPage()

    expect(await screen.findByText("chart.empty")).toBeTruthy()
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
})
