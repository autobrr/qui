/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { TooltipProvider } from "@/components/ui/tooltip"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

const defaultSearchSettings = vi.hoisted(() => ({
  instanceId: 1,
  categories: [] as string[],
  tags: [] as string[],
  excludeCategories: [] as string[],
  excludeTags: [] as string[],
  indexerIds: [] as number[],
  intervalSeconds: 60,
  cooldownMinutes: 720,
  maxAddedAgeDays: 0,
  skipIndividualEpisodes: false,
  torznabEnabled: true,
  scheduleEnabled: false,
  scheduleIntervalHours: 24,
  scheduleMaxSearches: 400,
  scheduleMaxAgeDays: 14,
}))

const mocks = vi.hoisted(() => ({
  settings: {
    seededSearchTags: ["cross-seed"],
    skipAutoResumeSeededSearch: false,
    gazelleEnabled: false,
    orpheusApiKey: "",
    redactedApiKey: "",
    seasonPackAutomationEnabled: false,
  },
  searchSettings: { ...defaultSearchSettings },
  searchStatus: { running: false } as Record<string, unknown>,
  instances: [{ id: 1, name: "main", isActive: true }],
  searchRuns: [] as unknown[],
  indexers: [] as { id: number; indexer_id: string; name: string; base_url: string; enabled: boolean }[],
  gazelleSites: [] as { site: string; name: string; shortName: string; indexerId: number; active: boolean; replacedIndexerIds: number[] }[],
  patchSearch: vi.fn(),
  patchSettings: vi.fn(),
  startRun: vi.fn(),
  runScheduleNow: vi.fn(),
  getSearchSettings: vi.fn(() => Promise.resolve(mocks.searchSettings)),
}))

vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => ({
    t: (key: string, opts?: Record<string, unknown>) => {
      if (opts && "time" in opts) return `${key}:${String(opts.time)}`
      if (opts && "limit" in opts) return `${key}:${String(opts.limit)}/${String(opts.count)}`
      if (opts && "filters" in opts) return `${key}:${String(opts.filters)}`
      if (opts && "list" in opts) return `${key}:${String(opts.list)}`
      if (opts && "crossSeed" in opts) return `${key}:${String(opts.source)}->${String(opts.crossSeed)}`
      if (opts && "sites" in opts) return `${key}:${String(opts.sites)}`
      if (opts && "asked" in opts) return `${key}:${String(opts.name)} ${String(opts.asked)}/${String(opts.matched)}/${String(opts.failed)}`
      if (opts && "counts" in opts) return `${key}:${String(opts.counts)} +${String(opts.notAsked)} (${String(opts.reasons)})`
      if (opts && "reason" in opts) return `${key}:${String(opts.count)} ${String(opts.reason)}`
      return key
    },
  }),
}))
vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }))
vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
}))
vi.mock("@/components/ui/field-help", () => ({
  FieldHelp: ({ children }: { children: ReactNode }) => <span>{children}</span>,
}))
vi.mock("@/hooks/useDateTimeFormatters", () => ({
  useDateTimeFormatters: () => ({ formatDate: (date: Date) => date.toISOString() }),
}))
vi.mock("@/lib/api", async (importOriginal) => ({
  APIError: (await importOriginal<typeof import("@/lib/api")>()).APIError,
  api: {
    getCrossSeedSearchSettings: mocks.getSearchSettings,
    getInstances: () => Promise.resolve(mocks.instances),
    listTorznabIndexers: () => Promise.resolve(mocks.indexers),
    getCrossSeedGazelleSites: () => Promise.resolve(mocks.gazelleSites),
    getCrossSeedSearchStatus: () => Promise.resolve(mocks.searchStatus),
    listCrossSeedSearchRuns: () => Promise.resolve(mocks.searchRuns),
    getCategories: () => Promise.resolve({}),
    getTags: () => Promise.resolve([]),
    patchCrossSeedSearchSettings: mocks.patchSearch,
    patchCrossSeedSettings: mocks.patchSettings,
    startCrossSeedSearchRun: mocks.startRun,
    runCrossSeedSearchScheduleNow: mocks.runScheduleNow,
  },
}))

import { APIError } from "@/lib/api"
import type { CrossSeedAutomationSettings } from "@/types"
import { toast } from "sonner"
import { LibraryTab } from "../LibraryTab"

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
  vi.unstubAllGlobals()
  Object.assign(mocks.settings, { gazelleEnabled: false, orpheusApiKey: "", redactedApiKey: "" })
  mocks.searchSettings = { ...defaultSearchSettings }
  mocks.searchStatus = { running: false }
  mocks.indexers = []
  mocks.gazelleSites = []
  mocks.searchRuns = []
})

const redSite = { site: "redacted.sh", name: "Redacted (API)", shortName: "Redacted", indexerId: -1001 }
const opsSite = { site: "orpheus.network", name: "Orpheus (API)", shortName: "Orpheus", indexerId: -1002 }

function renderTab() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <LibraryTab settings={mocks.settings as unknown as CrossSeedAutomationSettings} />
      </TooltipProvider>
    </QueryClientProvider>
  )
}

describe("LibraryTab save", () => {
  it("sends the search settings and only the library fields of the cross-seed settings", async () => {
    mocks.patchSearch.mockResolvedValue({ ...mocks.searchSettings, cooldownMinutes: 900 })
    mocks.patchSettings.mockResolvedValue({ ...mocks.settings, skipAutoResumeSeededSearch: true })
    const { container } = renderTab()

    const cooldown = await waitFor(() => {
      const input = container.querySelector<HTMLInputElement>("#search-cooldown")
      if (!input) throw new Error("card not mounted yet")
      return input
    })
    fireEvent.change(cooldown, { target: { value: "900" } })
    fireEvent.click(screen.getByRole("switch", { name: "sourceCard.autoResume" }))
    fireEvent.click(screen.getByRole("button", { name: "rules.saveChanges" }))

    await waitFor(() => expect(mocks.patchSearch).toHaveBeenCalledTimes(1))
    expect(mocks.patchSearch).toHaveBeenCalledWith({ ...defaultSearchSettings, cooldownMinutes: 900 })
    expect(mocks.patchSettings).toHaveBeenCalledWith({
      seededSearchTags: ["cross-seed"],
      skipAutoResumeSeededSearch: true,
    })
    // A scheduled run that the search settings start reads the saved auto-resume.
    expect(mocks.patchSettings.mock.invocationCallOrder[0]).toBeLessThan(mocks.patchSearch.mock.invocationCallOrder[0])
  })

  it("refetches the search settings when one of the two saves fails", async () => {
    mocks.patchSearch.mockRejectedValue(new Error("boom"))
    mocks.patchSettings.mockResolvedValue(mocks.settings)
    renderTab()
    await waitFor(() => expect(mocks.getSearchSettings).toHaveBeenCalledTimes(1))
    fireEvent.click(await screen.findByRole("button", { name: "rules.saveChanges" }))

    await waitFor(() => expect(mocks.getSearchSettings).toHaveBeenCalledTimes(2))
  })

  it("associates the tags label with the picker trigger", async () => {
    renderTab()
    const trigger = await screen.findByLabelText("sourceCard.crossSeedTags")
    expect(trigger.getAttribute("role")).toBe("combobox")
  })

  it("rejects a cooldown under the minimum before it sends anything", async () => {
    const { container } = renderTab()
    const cooldown = await waitFor(() => {
      const input = container.querySelector<HTMLInputElement>("#search-cooldown")
      if (!input) throw new Error("card not mounted yet")
      return input
    })
    fireEvent.change(cooldown, { target: { value: "10" } })
    fireEvent.click(screen.getByRole("button", { name: "rules.saveChanges" }))

    expect(screen.getByText("validation.minCooldown")).toBeTruthy()
    expect(mocks.patchSearch).not.toHaveBeenCalled()
    expect(mocks.patchSettings).not.toHaveBeenCalled()
  })

  it("does not start a Gazelle-only run from a stale OPS/RED-only pick", async () => {
    // Saved before both Gazelle keys existed; the picker hides OPS/RED now, so nothing is left to show or store.
    Object.assign(mocks.settings, { gazelleEnabled: true, orpheusApiKey: "ops", redactedApiKey: "red" })
    mocks.indexers = [
      { id: 1, indexer_id: "orpheus", name: "OPS", base_url: "https://orpheus.example.invalid", enabled: true },
      { id: 2, indexer_id: "redacted", name: "RED", base_url: "https://redacted.example.invalid", enabled: true },
      { id: 3, indexer_id: "other", name: "Other", base_url: "https://other.example.invalid", enabled: true },
    ]
    mocks.gazelleSites = [
      { ...redSite, active: true, replacedIndexerIds: [2] },
      { ...opsSite, active: true, replacedIndexerIds: [1] },
    ]
    mocks.searchSettings.indexerIds = [1, 2]
    mocks.startRun.mockResolvedValue({ id: 1 })
    renderTab()

    expect(await screen.findByText("scan.indexers.helpAllEnabledNonOpsRedQueried")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "scan.startRun" }))

    await waitFor(() => expect(mocks.startRun).toHaveBeenCalledTimes(1))
    expect(mocks.startRun.mock.calls[0][0]).toMatchObject({ indexerIds: [], disableTorznab: false })
  })

  it("hides the Torznab indexer only of the site that has a key", async () => {
    Object.assign(mocks.settings, { gazelleEnabled: true, redactedApiKey: "red" })
    mocks.indexers = [
      { id: 1, indexer_id: "orpheus", name: "OPS", base_url: "https://orpheus.example.invalid", enabled: true },
      { id: 2, indexer_id: "redacted", name: "RED", base_url: "https://redacted.example.invalid", enabled: true },
    ]
    mocks.gazelleSites = [
      { ...redSite, active: true, replacedIndexerIds: [2] },
      { ...opsSite, active: false, replacedIndexerIds: [] },
    ]
    mocks.searchSettings.indexerIds = [1, 2]
    mocks.startRun.mockResolvedValue({ id: 1 })
    renderTab()

    await waitFor(() => expect(screen.getByRole("button", { name: "scan.startRun" })).toBeTruthy())
    await waitFor(() => expect(screen.queryByText("scan.indexers.helpAllEnabledNonOpsRedQueried")).toBeNull())
    fireEvent.click(screen.getByRole("button", { name: "scan.startRun" }))

    await waitFor(() => expect(mocks.startRun).toHaveBeenCalledTimes(1))
    expect(mocks.startRun.mock.calls[0][0]).toMatchObject({ indexerIds: [1] })
  })
})

const enabledIndexer = { id: 3, indexer_id: "other", name: "Other", base_url: "https://other.example.invalid", enabled: true }

async function findSave() {
  return screen.findByRole("button", { name: "rules.saveChanges" })
}

function inputValue(label: string) {
  return (screen.getByLabelText(label) as HTMLInputElement).value
}

describe("LibraryTab saved fields", () => {
  const saved = {
    ...defaultSearchSettings,
    excludeCategories: ["no-cs"],
    excludeTags: ["keep-out"],
    intervalSeconds: 10,
    maxAddedAgeDays: 30,
    skipIndividualEpisodes: true,
    torznabEnabled: false,
    scheduleEnabled: true,
    scheduleIntervalHours: 6,
    scheduleMaxSearches: 50,
    scheduleMaxAgeDays: 7,
  }

  it("loads every saved field into the card and saves it back unchanged", async () => {
    mocks.searchSettings = saved
    mocks.patchSearch.mockResolvedValue(saved)
    mocks.patchSettings.mockResolvedValue(mocks.settings)
    renderTab()
    const save = await findSave()

    expect(screen.getByRole("switch", { name: "scan.torznab" }).getAttribute("aria-checked")).toBe("false")
    expect(screen.getByRole("switch", { name: "scan.skipIndividualEpisodesLabel" }).getAttribute("aria-checked")).toBe("true")
    expect(screen.getByRole("switch", { name: "scan.schedule.enableLabel" }).getAttribute("aria-checked")).toBe("true")
    expect(inputValue("scan.maxAddedAgeLabel")).toBe("30")
    expect(inputValue("scan.schedule.intervalLabel")).toBe("6")
    expect(inputValue("scan.schedule.maxSearchesLabel")).toBe("50")
    expect(inputValue("scan.schedule.maxAgeLabel")).toBe("7")

    fireEvent.click(save)
    await waitFor(() => expect(mocks.patchSearch).toHaveBeenCalledTimes(1))
    expect(mocks.patchSearch).toHaveBeenCalledWith(saved)
  })

  it("saves the edited switches, max age and schedule", async () => {
    mocks.patchSearch.mockResolvedValue(mocks.searchSettings)
    mocks.patchSettings.mockResolvedValue(mocks.settings)
    renderTab()
    const save = await findSave()

    fireEvent.click(screen.getByRole("switch", { name: "scan.torznab" }))
    fireEvent.click(screen.getByRole("switch", { name: "scan.skipIndividualEpisodesLabel" }))
    fireEvent.change(screen.getByLabelText("scan.maxAddedAgeLabel"), { target: { value: "45" } })
    fireEvent.click(screen.getByRole("switch", { name: "scan.schedule.enableLabel" }))
    fireEvent.change(screen.getByLabelText("scan.schedule.intervalLabel"), { target: { value: "12" } })
    fireEvent.change(screen.getByLabelText("scan.schedule.maxSearchesLabel"), { target: { value: "2" } })
    fireEvent.change(screen.getByLabelText("scan.schedule.maxAgeLabel"), { target: { value: "3" } })
    fireEvent.click(save)

    await waitFor(() => expect(mocks.patchSearch).toHaveBeenCalledTimes(1))
    expect(mocks.patchSearch).toHaveBeenCalledWith({
      ...defaultSearchSettings,
      torznabEnabled: false,
      skipIndividualEpisodes: true,
      maxAddedAgeDays: 45,
      scheduleEnabled: true,
      scheduleIntervalHours: 12,
      scheduleMaxSearches: 2,
      scheduleMaxAgeDays: 3,
    })
  })

  it("rejects a schedule value under 1 on Save, and still lets Start run", async () => {
    mocks.indexers = [enabledIndexer]
    mocks.startRun.mockResolvedValue({ id: 1 })
    renderTab()
    const save = await findSave()

    fireEvent.change(screen.getByLabelText("scan.schedule.maxSearchesLabel"), { target: { value: "0" } })
    fireEvent.click(save)
    expect(screen.getByText("validation.atLeast")).toBeTruthy()
    expect(mocks.patchSearch).not.toHaveBeenCalled()

    await waitFor(() => expect(screen.getByRole("button", { name: "scan.startRun" }).hasAttribute("disabled")).toBe(false))
    fireEvent.click(screen.getByRole("button", { name: "scan.startRun" }))
    await waitFor(() => expect(mocks.startRun).toHaveBeenCalledTimes(1))
  })

  it("sends the exclude filters and the Torznab switch with Start", async () => {
    mocks.searchSettings = { ...defaultSearchSettings, excludeCategories: ["no-cs"], excludeTags: ["keep-out"] }
    mocks.indexers = [enabledIndexer]
    mocks.startRun.mockResolvedValue({ id: 1 })
    renderTab()
    await findSave()

    await waitFor(() => expect(screen.getByRole("button", { name: "scan.startRun" }).hasAttribute("disabled")).toBe(false))
    fireEvent.click(screen.getByRole("button", { name: "scan.startRun" }))

    await waitFor(() => expect(mocks.startRun).toHaveBeenCalledTimes(1))
    expect(mocks.startRun.mock.calls[0][0]).toMatchObject({
      categories: [],
      tags: [],
      excludeCategories: ["no-cs"],
      excludeTags: ["keep-out"],
      disableTorznab: false,
    })
  })
})

describe("LibraryTab schedule controls", () => {
  it("shows the time of the next scheduled run", async () => {
    mocks.searchStatus = { running: false, nextScheduledRunAt: "2999-01-01T06:00:00Z" }
    renderTab()

    expect(await screen.findByText("scan.schedule.nextRun:2999-01-01T06:00:00.000Z")).toBeTruthy()
  })

  it("says that a scheduled run in the past is due now", async () => {
    mocks.searchStatus = { running: false, nextScheduledRunAt: "2001-01-01T06:00:00Z" }
    renderTab()

    expect(await screen.findByText("scan.schedule.dueNow")).toBeTruthy()
  })

  it("says that a due scheduled run waits for the active run", async () => {
    mocks.searchStatus = { running: true, nextScheduledRunAt: "2001-01-01T06:00:00Z" }
    renderTab()

    expect(await screen.findByText("scan.schedule.dueAfterCurrentRun")).toBeTruthy()
  })

  it("offers Run schedule now only when the saved schedule is on", async () => {
    renderTab()
    await findSave()
    fireEvent.click(screen.getByRole("switch", { name: "scan.schedule.enableLabel" }))

    expect(screen.queryByRole("button", { name: "scan.schedule.runNow" })).toBeNull()
  })

  it("starts a scheduled run from Run schedule now", async () => {
    mocks.searchSettings = { ...defaultSearchSettings, scheduleEnabled: true }
    mocks.runScheduleNow.mockResolvedValue({ id: 9 })
    renderTab()

    fireEvent.click(await screen.findByRole("button", { name: "scan.schedule.runNow" }))

    await waitFor(() => expect(mocks.runScheduleNow).toHaveBeenCalledTimes(1))
    await waitFor(() => expect(toast.success).toHaveBeenCalledWith("scan.schedule.runStarted"))
  })

  it("says that a run is already active when Run schedule now gets a conflict", async () => {
    mocks.searchSettings = { ...defaultSearchSettings, scheduleEnabled: true }
    mocks.runScheduleNow.mockRejectedValue(new APIError("search run already active", 409))
    renderTab()

    fireEvent.click(await screen.findByRole("button", { name: "scan.schedule.runNow" }))

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("scan.schedule.runAlreadyActive"))
  })

  it("turns Run schedule now off while a run is active", async () => {
    mocks.searchSettings = { ...defaultSearchSettings, scheduleEnabled: true }
    mocks.searchStatus = { running: true }
    renderTab()

    await waitFor(() => expect(screen.getByRole("button", { name: "scan.schedule.runNow" }).hasAttribute("disabled")).toBe(true))
  })

  it("says that the active run is a scheduled run and offers Cancel", async () => {
    mocks.searchStatus = { running: true, run: { ...runRecord([]), status: "running", kind: "scheduled" } }
    renderTab()

    expect(await screen.findByText("scan.schedule.scheduledRunActive")).toBeTruthy()
    expect(screen.getByRole("button", { name: "automation.cancel" })).toBeTruthy()
  })

  // regression: #4246
  it.each([
    { name: "a manual run", run: { kind: "manual" }, shown: true },
    { name: "a capped scheduled run", run: { kind: "scheduled", searchLimit: 400 }, shown: false },
  ])("shows a completion estimate for $name: $shown", async ({ run, shown }) => {
    mocks.searchStatus = { running: true, run: { ...runRecord([]), ...run, status: "running", totalTorrents: 1000, processed: 10, intervalSeconds: 60 } }
    renderTab()

    await screen.findByText("scan.status")
    expect(screen.queryByText("scan.estCompletion") !== null).toBe(shown)
  })
})

function runRecord(results: unknown[]) {
  return {
    id: 7, instanceId: 1, kind: "manual", status: "success", startedAt: "2026-01-01T00:00:00Z", totalTorrents: 1, processed: 1,
    torrentsWithCrossSeeds: 1, crossSeedsAdded: 1, torrentsFailed: 0, torrentsSkipped: 0, filters: { categories: [], tags: [] }, indexerIds: [],
    createdAt: "2026-01-01T00:00:00Z", results,
  }
}

describe("LibraryTab run results", () => {
  const run = runRecord
  const result = { torrentHash: "abc", torrentName: "Cedar.Harbor.S01E01", indexerName: "alpha", releaseTitle: "Cedar.Harbor.S01E01", processedAt: "2026-01-01T00:00:00Z" }

  async function openRun() {
    renderTab()
    fireEvent.click(await screen.findByText("scan.recentRuns"))
    fireEvent.click(await screen.findByText("scan.torrentsCount"))
  }

  it("shows one RED/OPS summary line on the run", async () => {
    mocks.searchRuns = [{
      ...run([]),
      gazelleSummary: [
        { site: "redacted.sh", name: "Redacted", asked: 180, matched: 12, failed: 3, notAsked: { not_music: 20 } },
        { site: "orpheus.network", name: "Orpheus", asked: 4, matched: 0, failed: 0 },
      ],
    }]
    renderTab()
    fireEvent.click(await screen.findByText("scan.recentRuns"))

    expect(await screen.findByText(
      "scan.gazelleSummary.line:scan.gazelleSummary.siteWithNotAsked:scan.gazelleSummary.site:Redacted 180/12/3 +20 " +
      "(scan.gazelleSummary.reasonCount:20 scan.gazelleSummary.reasons.notMusic); scan.gazelleSummary.site:Orpheus 4/0/0"
    )).toBeTruthy()
  })

  it("names the Cross-seed instance that received a routed add", async () => {
    mocks.searchRuns = [run([{ ...result, status: "added", crossSeedInstanceId: 2, crossSeedInstanceName: "seeds" }])]
    await openRun()

    expect(await screen.findByText("crossseed:route.foundOnAddedTo:main->seeds")).toBeTruthy()
  })

  it("shows no instance on an add that is not routed", async () => {
    mocks.searchRuns = [run([{ ...result, status: "added" }])]
    await openRun()

    expect(await screen.findByText("Cedar.Harbor.S01E01")).toBeTruthy()
    expect(screen.queryByText(/route\./)).toBeNull()
  })

  it("labels a scheduled run, its cap and the filters it used", async () => {
    mocks.searchRuns = [{
      ...run([]),
      kind: "scheduled",
      searchLimit: 400,
      waitingTorrents: 200,
      filters: { categories: ["tv"], tags: [], excludeCategories: ["no-cs"], excludeTags: ["keep-out"] },
    }]
    renderTab()
    fireEvent.click(await screen.findByText("scan.recentRuns"))

    expect(await screen.findByText("scan.runKind.scheduled")).toBeTruthy()
    expect(screen.getByText("scan.stoppedAtLimit:400/200")).toBeTruthy()
    expect(screen.getByText("scan.runFilters.include:scan.runFilters.categories:tv")).toBeTruthy()
    expect(screen.getByText("scan.runFilters.exclude:scan.runFilters.categories:no-cs; scan.runFilters.tags:keep-out")).toBeTruthy()
  })

  it("labels a manual run and shows no cap or filter line", async () => {
    mocks.searchRuns = [run([])]
    renderTab()
    fireEvent.click(await screen.findByText("scan.recentRuns"))

    expect(await screen.findByText("scan.runKind.manual")).toBeTruthy()
    expect(screen.queryByText(/scan\.stoppedAtLimit|scan\.runFilters/)).toBeNull()
  })
})
