/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { HttpResponse, http } from "msw"
import type { ChangeEvent, ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { StatisticsSettingsCard } from "@/components/settings/StatisticsSettingsCard"
import { TooltipProvider } from "@/components/ui/tooltip"
import "@/i18n"
import { server } from "@/test/msw/server"
import type { DatabaseShrinkStatus, TransferHistorySettings } from "@/types"

const i18n = vi.hoisted(() => ({
  t: (key: string, values?: object) => (values ? `${key} ${JSON.stringify(values)}` : key),
  i18n: { language: "en", resolvedLanguage: "en" },
}))
// The byte units come from the loaded locale, and the card text stays as keys.
vi.mock("react-i18next", async (importOriginal) => ({ ...(await importOriginal<typeof import("react-i18next")>()), useTranslation: () => i18n }))

// jsdom cannot open the Radix select, so the test drives a native select.
vi.mock("@/components/ui/select", () => ({
  Select: ({ name, value, onValueChange, disabled, children }: { name?: string; value: string; onValueChange: (value: string) => void; disabled?: boolean; children: ReactNode }) => (
    <select aria-label={name ?? "retention"} value={value} disabled={disabled} onChange={(e: ChangeEvent<HTMLSelectElement>) => onValueChange(e.target.value)}>
      {children}
    </select>
  ),
  SelectContent: ({ children }: { children: ReactNode }) => <>{children}</>,
  SelectItem: ({ value, children }: { value: string; children: ReactNode }) => <option value={value}>{children}</option>,
  SelectTrigger: () => null,
  SelectValue: () => null,
}))

const shrink = vi.hoisted(() => ({ requestShrink: vi.fn(async () => {}), pending: false }))
vi.mock("@/hooks/useDatabaseShrink", () => ({
  databaseShrinkQueryKey: ["database-shrink"],
  useDatabaseShrink: () => shrink,
}))

let stored: TransferHistorySettings
let shrinkStatus: DatabaseShrinkStatus
let saved: unknown[]
let engine: string
let deletes: URLSearchParams[]
const instances = [
  { instanceId: 1, name: "Seedbox", firstRecordedAt: "2026-01-02T10:00:00Z" },
  { instanceId: 7, name: "Old box", firstRecordedAt: "2025-06-01T10:00:00Z" },
]

function renderCard() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <StatisticsSettingsCard />
      </TooltipProvider>
    </QueryClientProvider>
  )
}

beforeEach(() => {
  stored = { recording: true, detailRetentionDays: 400 }
  saved = []
  engine = "sqlite"
  deletes = []
  shrinkStatus = { available: true, fileBytes: 100 * 1024 * 1024, freeBytes: 50 * 1024 * 1024 * 1024, needBytes: 200 * 1024 * 1024, restartAvailable: true, pending: false }
  server.use(
    http.get("*/api/database/shrink", () => HttpResponse.json(shrinkStatus)),
    http.get("*/api/transfer-history/status", () =>
      HttpResponse.json({ instances, historyBytes: 3 * 1024 * 1024, settings: stored })
    ),
    http.get("*/api/application/info", () => HttpResponse.json({ database: { engine, target: "" } })),
    http.delete("*/api/transfer-history", ({ request }) => {
      deletes.push(new URL(request.url).searchParams)
      return new HttpResponse(null, { status: 204 })
    }),
    http.put("*/api/transfer-history/settings", async ({ request }) => {
      const body = (await request.json()) as TransferHistorySettings
      saved.push(body)
      stored = body
      return HttpResponse.json(body)
    })
  )
})

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

describe("StatisticsSettingsCard", () => {
  it("turns recording off and keeps the retention", async () => {
    renderCard()
    const recording = await screen.findByRole("switch")
    expect(recording.getAttribute("aria-checked")).toBe("true")

    fireEvent.click(recording)

    await waitFor(() => expect(saved).toEqual([{ recording: false, detailRetentionDays: 400 }]))
    await waitFor(() => expect(screen.getByRole("switch").getAttribute("aria-checked")).toBe("false"))
  })

  it("sets the retention to forever and keeps the switch", async () => {
    stored = { recording: false, detailRetentionDays: 90 }
    renderCard()
    const retention = await screen.findByLabelText<HTMLSelectElement>("retention")
    expect(retention.value).toBe("90")

    fireEvent.change(retention, { target: { value: "0" } })

    await waitFor(() => expect(saved).toEqual([{ recording: false, detailRetentionDays: 0 }]))
    await waitFor(() => expect(screen.getByLabelText<HTMLSelectElement>("retention").value).toBe("0"))
  })

  it("shows the size of the history", async () => {
    renderCard()
    const size = await screen.findByTestId("statistics-history-size")
    expect(size.textContent).toContain("3")
    expect(size.textContent).toMatch(/MiB/)
  })
})

describe("Delete history", () => {
  it("deletes the history of one instance before a date after a confirmation that names both", async () => {
    renderCard()
    fireEvent.change(await screen.findByLabelText("statistics-delete-instance"), { target: { value: "7" } })
    fireEvent.click(screen.getByRole("radio", { name: "statistics.deleteHistory.before" }))
    fireEvent.change(screen.getByLabelText("statistics.deleteHistory.date"), { target: { value: "2026-03-01" } })
    fireEvent.click(screen.getByRole("button", { name: "statistics.deleteHistory.button" }))

    const dialog = await screen.findByRole("alertdialog")
    expect(dialog.textContent).toContain("statistics.deleteHistory.confirmBefore")
    expect(dialog.textContent).toContain("\"name\":\"Old box\"")
    expect(dialog.textContent).toContain("\"date\":\"Mar 1, 2026\"")
    await waitFor(() => expect(dialog.textContent).toContain("statistics.deleteHistory.sqliteSpace"))
    expect(deletes).toEqual([])

    fireEvent.click(screen.getByRole("button", { name: "statistics.deleteHistory.confirm" }))

    await waitFor(() => expect(deletes).toHaveLength(1))
    expect(Object.fromEntries(deletes[0])).toEqual({ instanceId: "7", before: new Date(2026, 2, 1).toISOString() })
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
  })

  it("deletes everything of all instances", async () => {
    engine = "postgres"
    renderCard()
    fireEvent.click(await screen.findByRole("button", { name: "statistics.deleteHistory.button" }))

    const dialog = await screen.findByRole("alertdialog")
    expect(dialog.textContent).toContain("statistics.deleteHistory.confirmEverything")
    expect(dialog.textContent).toContain("\"name\":\"statistics.deleteHistory.allInstances\"")
    await waitFor(() => expect(dialog.textContent).toContain("statistics.deleteHistory.postgresSpace"))
    fireEvent.click(screen.getByRole("button", { name: "statistics.deleteHistory.confirm" }))

    await waitFor(() => expect(deletes).toHaveLength(1))
    expect(Object.fromEntries(deletes[0])).toEqual({ instanceId: "all" })
  })

  it("asks for a date before it deletes before a date", async () => {
    renderCard()
    fireEvent.click(await screen.findByRole("radio", { name: "statistics.deleteHistory.before" }))

    expect(screen.getByRole<HTMLButtonElement>("button", { name: "statistics.deleteHistory.button" }).disabled).toBe(true)
  })
})

describe("Shrink tick box", () => {
  async function openDialog() {
    renderCard()
    fireEvent.click(await screen.findByRole("button", { name: "statistics.deleteHistory.button" }))
    return screen.findByRole("alertdialog")
  }

  it("deletes the history and then requests the shrink", async () => {
    const dialog = await openDialog()
    const box = await screen.findByRole<HTMLButtonElement>("checkbox", { name: "statistics.deleteHistory.shrink.label" })
    expect(box.disabled).toBe(false)
    expect(dialog.textContent).toContain("statistics.deleteHistory.shrink.restart")

    fireEvent.click(box)
    fireEvent.click(screen.getByRole("button", { name: "statistics.deleteHistory.confirm" }))

    await waitFor(() => expect(shrink.requestShrink).toHaveBeenCalledTimes(1))
    expect(deletes).toHaveLength(1)
  })

  it("is off with too little free disk space and gives the two numbers", async () => {
    shrinkStatus = { ...shrinkStatus, freeBytes: 150 * 1024 * 1024 }
    const dialog = await openDialog()
    const box = await screen.findByRole<HTMLButtonElement>("checkbox", { name: "statistics.deleteHistory.shrink.label" })

    expect(box.disabled).toBe(true)
    expect(dialog.textContent).toContain("statistics.deleteHistory.shrink.tooLittleSpace")
    expect(dialog.textContent).toMatch(/"free":"150(\.0)? MiB"/)
    expect(dialog.textContent).toMatch(/"need":"200(\.0)? MiB"/)
    expect(screen.getByRole("link", { name: "statistics.deleteHistory.shrink.manualDocs" })).toBeTruthy()

    fireEvent.click(screen.getByRole("button", { name: "statistics.deleteHistory.confirm" }))
    await waitFor(() => expect(deletes).toHaveLength(1))
    expect(shrink.requestShrink).not.toHaveBeenCalled()
  })

  it("is absent on Postgres", async () => {
    engine = "postgres"
    shrinkStatus = { ...shrinkStatus, available: false }
    const dialog = await openDialog()

    await waitFor(() => expect(dialog.textContent).toContain("statistics.deleteHistory.postgresSpace"))
    expect(screen.queryByRole("checkbox")).toBeNull()
  })
})
