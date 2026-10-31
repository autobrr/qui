/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

const mocks = vi.hoisted(() => ({
  settings: {
    seasonPackEnabled: true,
    seasonPackAutomationEnabled: false,
    seasonPackSkipRepackCompare: true,
    seasonPackSimplifyHdrCompare: false,
    seasonPackSimplifyWebCompare: false,
    seasonPackSkipYearCompare: false,
    seasonPackCoverageThreshold: 0.75,
    seasonPackTags: ["cross-seed", "season-pack"],
    seasonPackCategory: "tv",
    seasonPackCategoryRules: [],
    seasonPackTvdbApiKey: "key",
    seasonPackTvdbPin: "",
    rssAutomationTags: ["rss"],
  },
  patchSettings: vi.fn(),
  listRuns: vi.fn(),
}))

vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  // Echo interpolation values, so a test can read the counts.
  useTranslation: () => ({
    t: (key: string, options?: unknown) =>
      options && typeof options === "object" ? `${key}:${Object.values(options).join(",")}` : key,
  }),
}))
vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }))
vi.mock("@/components/ui/field-help", () => ({
  FieldHelp: ({ children }: { children: ReactNode }) => <span>{children}</span>,
}))
vi.mock("@/components/cross-seed/SeasonPackCategoryRulesEditor", () => ({
  SeasonPackCategoryRulesEditor: () => <div data-testid="rules-editor" />,
}))
vi.mock("@/hooks/useDateTimeFormatters", () => ({
  useDateTimeFormatters: () => ({ formatDate: (date: Date) => date.toISOString() }),
}))
vi.mock("@/lib/api", () => ({
  api: {
    getInstances: () => Promise.resolve([]),
    listSeasonPackRuns: mocks.listRuns,
    patchCrossSeedSettings: mocks.patchSettings,
  },
}))

import type { CrossSeedAutomationSettings, SeasonPackRun } from "@/types"
import { SeasonPacksTab } from "../SeasonPacksTab"

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
  localStorage.clear()
})

function renderTab() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <SeasonPacksTab settings={mocks.settings as unknown as CrossSeedAutomationSettings} />
    </QueryClientProvider>
  )
}

function makeRun(id: number, status: SeasonPackRun["status"]): SeasonPackRun {
  return {
    id,
    torrentName: `Pack.${status}.${id}`,
    phase: status === "applied" || status === "failed" ? "apply" : "check",
    status,
    reason: "",
    message: "",
    matchedEpisodes: 0,
    totalEpisodes: 0,
    coverage: 0,
    createdAt: "2026-10-31T12:00:00Z",
  }
}

async function openRunsPanel() {
  fireEvent.click(await screen.findByText("seasonPackRuns.title"))
}

function chip(label: string) {
  return screen.getByRole("button", { name: new RegExp(`^seasonPackRuns\\.statusLabels\\.${label} `) })
}

describe("SeasonPacksTab season pack runs", () => {
  it("requests every run that qui keeps", async () => {
    mocks.listRuns.mockResolvedValue([])
    renderTab()
    await waitFor(() => expect(mocks.listRuns).toHaveBeenCalledWith({ limit: 400 }))
  })

  it("shows only applied runs by default, with a count on each chip", async () => {
    mocks.listRuns.mockResolvedValue([
      makeRun(1, "applied"),
      makeRun(2, "failed"),
      makeRun(3, "failed"),
      makeRun(4, "skipped"),
      makeRun(5, "ready_no_threshold"),
    ])
    renderTab()
    await openRunsPanel()

    await screen.findByText("Pack.applied.1")
    expect(screen.queryByText("Pack.failed.2")).toBeNull()
    expect(screen.queryByText("Pack.skipped.4")).toBeNull()
    expect(screen.queryByText("Pack.ready_no_threshold.5")).toBeNull()

    expect(chip("applied").textContent).toBe("seasonPackRuns.statusLabels.applied 1")
    expect(chip("applied").getAttribute("aria-pressed")).toBe("true")
    expect(chip("ready").textContent).toBe("seasonPackRuns.statusLabels.ready 1")
    expect(chip("ready").getAttribute("aria-pressed")).toBe("false")
    expect(chip("skipped").textContent).toBe("seasonPackRuns.statusLabels.skipped 1")
    expect(chip("failed").textContent).toBe("seasonPackRuns.statusLabels.failed 2")

    // The header counts all runs, not only the runs that the chips show.
    expect(screen.getByText(/seasonPackRuns\.runsCount:5/).textContent).toBe(
      "seasonPackRuns.runsCount:5 • seasonPackRuns.appliedCount:1 • seasonPackRuns.failedCount:2"
    )

    fireEvent.click(chip("failed"))
    await screen.findByText("Pack.failed.2")
    expect(screen.getByText("Pack.failed.3")).not.toBeNull()
    expect(screen.getByText("Pack.applied.1")).not.toBeNull()

    fireEvent.click(chip("ready"))
    await screen.findByText("Pack.ready_no_threshold.5")
  })

  it("offers a link for each hidden status with runs when no run matches", async () => {
    mocks.listRuns.mockResolvedValue([makeRun(1, "failed"), makeRun(2, "failed"), makeRun(3, "skipped")])
    renderTab()
    await openRunsPanel()

    await screen.findByText(/seasonPackRuns\.noMatches/)
    expect(screen.queryByRole("button", { name: "seasonPackRuns.showStatus.ready:0" })).toBeNull()
    expect(screen.getByRole("button", { name: "seasonPackRuns.showStatus.skipped:1" })).not.toBeNull()
    fireEvent.click(screen.getByRole("button", { name: "seasonPackRuns.showStatus.failed:2" }))

    await screen.findByText("Pack.failed.1")
    expect(chip("failed").getAttribute("aria-pressed")).toBe("true")
    expect(screen.queryByText("Pack.skipped.3")).toBeNull()
  })
})

describe("SeasonPacksTab save", () => {
  it("sends every season pack field and nothing else", async () => {
    mocks.patchSettings.mockResolvedValue(mocks.settings)
    mocks.listRuns.mockResolvedValue([])
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const { container } = render(
      <QueryClientProvider client={client}>
        <SeasonPacksTab settings={mocks.settings as unknown as CrossSeedAutomationSettings} />
      </QueryClientProvider>
    )

    const threshold = await waitFor(() => {
      const input = container.querySelector<HTMLInputElement>("#season-pack-threshold")
      if (!input) throw new Error("card not mounted yet")
      return input
    })
    fireEvent.change(threshold, { target: { value: "90" } })
    fireEvent.click(screen.getByRole("switch", { name: "rules.seasonPack.enableAutomation" }))
    fireEvent.click(screen.getByRole("button", { name: "rules.saveChanges" }))

    await waitFor(() => expect(mocks.patchSettings).toHaveBeenCalledTimes(1))
    expect(mocks.patchSettings).toHaveBeenCalledWith({
      seasonPackEnabled: true,
      seasonPackAutomationEnabled: true,
      seasonPackSkipRepackCompare: true,
      seasonPackSimplifyHdrCompare: false,
      seasonPackSimplifyWebCompare: false,
      seasonPackSkipYearCompare: false,
      seasonPackCoverageThreshold: 0.9,
      seasonPackTags: ["cross-seed", "season-pack"],
      seasonPackCategory: "tv",
      seasonPackCategoryRules: [],
      seasonPackTvdbApiKey: undefined,
      seasonPackTvdbPin: undefined,
    })
  })
})
