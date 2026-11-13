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
import type { TransferHistorySettings } from "@/types"

const i18n = vi.hoisted(() => ({ t: (key: string) => key }))
// The byte units come from the loaded locale, and the card text stays as keys.
vi.mock("react-i18next", async (importOriginal) => ({ ...(await importOriginal<typeof import("react-i18next")>()), useTranslation: () => i18n }))

// jsdom cannot open the Radix select, so the test drives a native select.
vi.mock("@/components/ui/select", () => ({
  Select: ({ value, onValueChange, disabled, children }: { value: string; onValueChange: (value: string) => void; disabled?: boolean; children: ReactNode }) => (
    <select aria-label="retention" value={value} disabled={disabled} onChange={(e: ChangeEvent<HTMLSelectElement>) => onValueChange(e.target.value)}>
      {children}
    </select>
  ),
  SelectContent: ({ children }: { children: ReactNode }) => <>{children}</>,
  SelectItem: ({ value, children }: { value: string; children: ReactNode }) => <option value={value}>{children}</option>,
  SelectTrigger: () => null,
  SelectValue: () => null,
}))

let stored: TransferHistorySettings
let saved: unknown[]

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
  server.use(
    http.get("*/api/transfer-history/status", () =>
      HttpResponse.json({ instances: [], historyBytes: 3 * 1024 * 1024, settings: stored })
    ),
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
