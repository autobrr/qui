/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { api } from "@/lib/api"
import type { CrossSeedGazelleLookup, CrossSeedInstanceResult, CrossSeedSearchDecisionTrace } from "@/types"
import { cleanup, fireEvent, render, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { buildCrossSeedTraceReport, CrossSeedDialog, type CrossSeedDialogProps } from "./CrossSeedDialog"

const { toast } = vi.hoisted(() => ({ toast: { success: vi.fn(), error: vi.fn() } }))
vi.mock("react-i18next", async (importOriginal) => ({ ...(await importOriginal<object>()), useTranslation: () => ({ t: (key: string) => key, i18n: { language: "en" } }) }))
vi.mock("sonner", () => ({ toast }))
vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, search }: { children: React.ReactNode; search?: { tab?: string } }) => <a data-tab={search?.tab}>{children}</a>,
}))

let mobile = false
vi.mock("@/hooks/useMediaQuery", async importOriginal => ({
  ...await importOriginal<typeof import("@/hooks/useMediaQuery")>(),
  useIsPhone: () => mobile,
}))

afterEach(() => {
  mobile = false
  cleanup()
  vi.clearAllMocks()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  Reflect.deleteProperty(document, "execCommand")
})

const noop = () => {}
const baseProps = {
  open: true, onOpenChange: noop, torrent: null, sourceTorrent: { name: "src", hash: "h" }, results: [], selectedKeys: new Set<string>(), selectionCount: 0,
  isLoading: false, isSubmitting: false, error: null, applyOutcomes: new Map(), indexerOptions: [],
  alreadySeededIndexers: [], unsupportedIndexers: [],
  indexerMode: "all", selectedIndexerIds: [], indexerNameMap: {}, onToggleIndexer: noop, onSetAllIndexers: noop, outcomesStale: false, onScopeSearch: noop, getResultKey: () => "",
  onToggleSelection: noop, onSelectAll: noop, onClearSelection: noop, onRetry: noop, onClose: noop, onApply: noop,
  useTag: false, onUseTagChange: noop, tagName: "", onTagNameChange: noop, startPaused: false,
  onStartPausedChange: noop, hasSearched: false, gazelleEntries: [], selectableGazelleIds: [],
} satisfies CrossSeedDialogProps

const red = { site: "redacted.sh", name: "Redacted (API)", shortName: "Redacted", indexerId: -1001 }
const ops = { site: "orpheus.network", name: "Orpheus (API)", shortName: "Orpheus", indexerId: -1002 }
// The dialog renders in a portal, outside the render container.
const entryRow = (name: string) =>
  [...document.body.querySelectorAll("li[data-status]")].find(row => row.textContent?.includes(name))
const openScope = () =>
  fireEvent.click([...document.body.querySelectorAll("button")].find(button => button.textContent === "crossSeedDialog.scope.change")!)

describe("buildCrossSeedTraceReport", () => {
  it("renders counts, indexer outcomes, and capped rejection detail", () => {
    const trace: CrossSeedSearchDecisionTrace = {
      sourceSize: 1073741824,
      tolerancePercent: 2,
      totalResults: 9,
      sizeFiltered: 1,
      releaseFiltered: 7,
      lateContentFiltered: 0,
      duplicateFiltered: 0,
      finalMatches: 1,
      rejectionCounts: { "hdr mismatch": 7, "size mismatch": 1 },
      rejectedCandidates: [
        { indexer: "alpha", indexerId: 1, title: "Show.S01.2160p.HDR.WEB-DL-GROUP", size: 2147483648, reason: "hdr mismatch" },
        { indexer: "beta", indexerId: 2, title: "Show.S01.1080p.WEB-DL-OTHER", size: 999, reason: "size mismatch" },
      ],
      indexers: [
        { indexerId: 1, status: "searched", candidates: 9 },
        { indexerId: 2, status: "error", detail: "connection refused", candidates: 0 },
        { indexerId: 3, status: "not_covered", candidates: 0 },
      ],
    }

    const report = buildCrossSeedTraceReport(trace, "Show.S01.2160p.WEB-DL-GROUP", { 1: "alpha", 2: "beta" })

    expect(report).toContain("source: Show.S01.2160p.WEB-DL-GROUP (1 GiB)")
    expect(report).toContain("size tolerance: 2%")
    expect(report).toContain("results: 9 candidates, 7 release-filtered, 1 size-filtered, 0 late-content-filtered, 0 duplicates, 1 matches")
    expect(report).toContain("alpha: searched, 9 candidates")
    expect(report).toContain("beta: error - connection refused")
    expect(report).toContain("indexer 3: not_covered")
    expect(report).toContain("hdr mismatch: 7")
    expect(report).toContain("- Show.S01.2160p.HDR.WEB-DL-GROUP [alpha] 2 GiB")
    expect(report).toContain("(+6 more)")
  })

  it("adds a RED/OPS section after the indexer lines", () => {
    const trace: CrossSeedSearchDecisionTrace = {
      sourceSize: 0, tolerancePercent: 0, totalResults: 0, sizeFiltered: 0, releaseFiltered: 0,
      lateContentFiltered: 0, duplicateFiltered: 0, finalMatches: 0,
      indexers: [{ indexerId: 1, status: "searched", candidates: 0 }],
    }
    const lookups: CrossSeedGazelleLookup[] = [
      { ...red, status: "not_asked", reason: "source_site" },
      { ...ops, status: "failed", reason: "timed_out", detail: "context deadline exceeded" },
    ]

    const report = buildCrossSeedTraceReport(trace, "Some.Album", { 1: "alpha" }, lookups)

    expect(report.indexOf("RED/OPS:")).toBeGreaterThan(report.indexOf("alpha: searched"))
    expect(report).toContain("  Redacted (API): not_asked (source_site)")
    expect(report).toContain("  Orpheus (API): failed (timed_out) - context deadline exceeded")
  })

  it("builds a RED/OPS report for a search that asked no Torznab indexer", () => {
    const report = buildCrossSeedTraceReport(undefined, "Some.Album", {}, [{ ...ops, status: "no_match" }])

    expect(report).toBe("qui cross-seed search report\nsource: Some.Album\nRED/OPS:\n  Orpheus (API): no_match")
  })

  it("omits indexer and rejection sections when empty", () => {
    const trace: CrossSeedSearchDecisionTrace = {
      sourceSize: 0,
      tolerancePercent: 0,
      totalResults: 0,
      sizeFiltered: 0,
      releaseFiltered: 0,
      lateContentFiltered: 0,
      duplicateFiltered: 0,
      finalMatches: 0,
    }

    const report = buildCrossSeedTraceReport(trace, "Some.Release", {})

    expect(report).not.toContain("indexers:")
    expect(report).not.toContain("rejections:")
  })
})

describe("CrossSeedDialog copy report", () => {
  it("copies the report on plain HTTP, where navigator.clipboard does not exist", async () => {
    vi.stubGlobal("isSecureContext", false)
    vi.stubGlobal("navigator", { ...navigator, clipboard: undefined })
    // jsdom has no execCommand, so the test defines it and afterEach deletes it.
    const execCommand = vi.fn().mockReturnValue(true)
    document.execCommand = execCommand

    const props = {
      ...baseProps,
      hasSearched: true,
      decisionTrace: {
        sourceSize: 0, tolerancePercent: 0, totalResults: 0, sizeFiltered: 0, releaseFiltered: 0,
        lateContentFiltered: 0, duplicateFiltered: 0, finalMatches: 0,
      },
    } satisfies CrossSeedDialogProps
    const { getByText } = render(<CrossSeedDialog {...props} />)

    fireEvent.click(getByText("crossSeedDialog.trace.copyReport"))

    await waitFor(() => expect(toast.success).toHaveBeenCalledWith("crossSeedDialog.trace.copied", { position: undefined }))
    expect(execCommand).toHaveBeenCalledWith("copy")
    expect(toast.error).not.toHaveBeenCalled()
  })
})

describe("CrossSeedDialog search scope", () => {
  it("searches RED/OPS when no Torznab indexer is available", () => {
    const onScopeSearch = vi.fn()
    const plan: CrossSeedGazelleLookup[] = [{ ...red, status: "will_ask" }, { ...ops, status: "no_match" }]
    const { getByText, queryByText } = render(
      <CrossSeedDialog {...baseProps} gazelleEntries={plan} selectableGazelleIds={[red.indexerId]} onScopeSearch={onScopeSearch} />
    )

    expect(queryByText("crossSeedDialog.noIndexersHelp")).toBeNull()
    fireEvent.click(getByText("crossSeedDialog.scope.searchCount"))
    expect(onScopeSearch).toHaveBeenCalledTimes(1)
  })

  it("lists both RED/OPS entries with the plan, and greys out a site that the search does not ask", () => {
    const plan: CrossSeedGazelleLookup[] = [{ ...red, status: "not_asked", reason: "source_site" }, { ...ops, status: "will_ask" }]
    render(<CrossSeedDialog {...baseProps} gazelleEntries={plan} selectableGazelleIds={[ops.indexerId]} />)
    openScope()

    const redRow = entryRow("Redacted (API)")
    expect(redRow?.getAttribute("data-status")).toBe("not_asked")
    expect(redRow?.textContent).toContain("crossSeedDialog.gazelle.notAsked.sourceSite")
    expect(redRow?.querySelector("button")).toBeNull()
    expect(entryRow("Orpheus (API)")?.getAttribute("data-status")).toBe("will_ask")
    expect(entryRow("Orpheus (API)")?.querySelector("[role=checkbox]")?.getAttribute("aria-checked")).toBe("true")
  })

  it("lists the Torznab indexers by name beside the RED/OPS entries, in both modes", () => {
    const plan: CrossSeedGazelleLookup[] = [{ ...red, status: "will_ask" }, { ...ops, status: "will_ask" }]
    const options = [{ id: 1, name: "Alpha Music" }, { id: 2, name: "Beta Tracker" }]
    const { getByText, rerender } = render(
      <CrossSeedDialog {...baseProps} indexerOptions={options} gazelleEntries={plan} selectableGazelleIds={[red.indexerId, ops.indexerId]} />
    )
    openScope()

    expect(entryRow("Alpha Music")?.getAttribute("data-status")).toBe("will_ask")
    expect(entryRow("Beta Tracker")?.getAttribute("data-status")).toBe("will_ask")
    expect(entryRow("Orpheus (API)")?.getAttribute("data-status")).toBe("will_ask")

    rerender(
      <CrossSeedDialog
        {...baseProps}
        indexerMode="custom"
        selectedIndexerIds={[1, red.indexerId]}
        indexerOptions={options}
        gazelleEntries={plan}
        selectableGazelleIds={[red.indexerId, ops.indexerId]}
      />
    )

    expect(entryRow("Alpha Music")?.getAttribute("data-status")).toBe("will_ask")
    expect(entryRow("Beta Tracker")?.getAttribute("data-status")).toBe("not_asked")
    expect(entryRow("Beta Tracker")?.querySelector("[role=checkbox]")?.getAttribute("aria-checked")).toBe("false")
    expect(entryRow("Redacted (API)")?.getAttribute("data-status")).toBe("will_ask")
    expect(entryRow("Orpheus (API)")?.getAttribute("data-status")).toBe("not_asked")
    expect(getByText("crossSeedDialog.scope.searchCount")).toBeTruthy()
  })

  it("toggles a RED/OPS site when only RED/OPS sites can be asked", () => {
    const onToggleIndexer = vi.fn()
    const plan: CrossSeedGazelleLookup[] = [{ ...red, status: "will_ask" }, { ...ops, status: "will_ask" }]
    render(<CrossSeedDialog {...baseProps} gazelleEntries={plan} selectableGazelleIds={[red.indexerId, ops.indexerId]} onToggleIndexer={onToggleIndexer} />)
    openScope()

    fireEvent.click(entryRow("Orpheus (API)")!.querySelector("[role=checkbox]")!)
    expect(onToggleIndexer).toHaveBeenCalledWith(ops.indexerId)
  })

  it("shows the outcome of each picked Torznab indexer after a search", () => {
    const options = [{ id: 1, name: "Alpha Music" }, { id: 2, name: "Beta Tracker" }, { id: 3, name: "Gamma" }, { id: 4, name: "Delta" }]
    const trace: CrossSeedSearchDecisionTrace = {
      sourceSize: 0, tolerancePercent: 0, totalResults: 1, sizeFiltered: 0, releaseFiltered: 0,
      lateContentFiltered: 0, duplicateFiltered: 0, finalMatches: 1,
      indexers: [
        { indexerId: 1, status: "searched", candidates: 1 },
        { indexerId: 2, status: "error", detail: "connection refused", candidates: 0 },
        { indexerId: 3, status: "searched", candidates: 4 },
      ],
    }
    const result = { indexer: "Alpha Music", indexerId: 1, title: "Cedar.Harbor.FLAC", size: 1, seeders: 1, publishDate: "", downloadUrl: "u", guid: "g" }
    render(
      <CrossSeedDialog
        {...baseProps}
        hasSearched
        indexerOptions={options}
        indexerNameMap={{ 2: "Beta Tracker" }}
        decisionTrace={trace}
        results={[result] as CrossSeedDialogProps["results"]}
        getResultKey={() => "g"}
      />
    )

    expect(entryRow("Alpha Music")).toBeUndefined()
    openScope()
    expect(entryRow("Alpha Music")?.textContent).toContain("crossSeedDialog.scope.matchCount")
    expect(entryRow("Beta Tracker")?.getAttribute("data-status")).toBe("error")
    expect(entryRow("Gamma")?.getAttribute("data-status")).toBe("no_match")
    expect(entryRow("Gamma")?.textContent).toBe("Gamma")
    expect(entryRow("Delta")?.getAttribute("data-status")).toBe("will_ask")
    expect(document.body.textContent).toContain("crossSeedDialog.problemDetail")
  })

  it("shows the plan on each indexer row again once the pick changes after a search", () => {
    const options = [{ id: 1, name: "Alpha Music" }]
    const trace: CrossSeedSearchDecisionTrace = {
      sourceSize: 0, tolerancePercent: 0, totalResults: 0, sizeFiltered: 0, releaseFiltered: 0,
      lateContentFiltered: 0, duplicateFiltered: 0, finalMatches: 0,
      indexers: [{ indexerId: 1, status: "error", candidates: 0 }],
    }
    render(<CrossSeedDialog {...baseProps} hasSearched outcomesStale indexerOptions={options} decisionTrace={trace} />)
    openScope()

    expect(entryRow("Alpha Music")?.getAttribute("data-status")).toBe("will_ask")
  })

  it("lists the indexers that the user already seeds from below the indexer rows, without a toggle", () => {
    const { getByText, queryByText } = render(
      <CrossSeedDialog
        {...baseProps}
        indexerOptions={[{ id: 1, name: "Alpha Music" }]}
        alreadySeededIndexers={[{ id: 2, name: "Beta Tracker" }]}
        unsupportedIndexers={[{ id: 3, name: "Gamma Video" }]}
      />
    )
    openScope()

    const seeded = entryRow("Beta Tracker")
    expect(seeded?.textContent).toContain("crossSeedDialog.scope.alreadySeeded")
    expect(seeded?.querySelector("button")).toBeNull()
    expect(queryByText("Gamma Video")).toBeNull()
    fireEvent.click(getByText("crossSeedDialog.scope.unsupported"))
    expect(getByText("Gamma Video")).toBeTruthy()
  })

  it("folds the indexers into their names, and folds them again when a search starts", () => {
    const options = [{ id: 1, name: "Alpha Music" }, { id: 2, name: "Beta Tracker" }]
    const onScopeSearch = vi.fn()
    const { getByText, rerender } = render(<CrossSeedDialog {...baseProps} indexerOptions={options} onScopeSearch={onScopeSearch} />)

    expect(getByText("Alpha Music, Beta Tracker")).toBeTruthy()
    expect(entryRow("Alpha Music")).toBeUndefined()
    openScope()
    expect(entryRow("Alpha Music")).toBeTruthy()

    fireEvent.click(getByText("crossSeedDialog.scope.searchCount"))
    expect(onScopeSearch).toHaveBeenCalledTimes(1)
    rerender(<CrossSeedDialog {...baseProps} indexerOptions={options} onScopeSearch={onScopeSearch} isLoading />)
    expect(entryRow("Alpha Music")).toBeUndefined()
  })

  it("opens as a drawer on a phone, with the same footer action", () => {
    mobile = true
    const options = [{ id: 1, name: "Alpha Music" }]
    const onScopeSearch = vi.fn()
    const { getByRole, getByText } = render(<CrossSeedDialog {...baseProps} indexerOptions={options} onScopeSearch={onScopeSearch} />)

    expect(getByRole("dialog").hasAttribute("data-vaul-drawer")).toBe(true)
    expect(getByText("crossSeedDialog.title")).toBeTruthy()
    fireEvent.click(getByText("crossSeedDialog.scope.searchCount"))
    expect(onScopeSearch).toHaveBeenCalledTimes(1)
  })

  it("offers the add button only when the search found results", () => {
    const result = { indexer: "Alpha Music", indexerId: 1, title: "Cedar.Harbor.FLAC", size: 1, seeders: 1, publishDate: "", downloadUrl: "u", guid: "g" }
    const { queryByText, rerender } = render(<CrossSeedDialog {...baseProps} hasSearched />)
    expect(queryByText("crossSeedDialog.addCount")).toBeNull()

    rerender(<CrossSeedDialog {...baseProps} hasSearched results={[result] as CrossSeedDialogProps["results"]} selectionCount={1} />)
    expect(queryByText("crossSeedDialog.addCount")).toBeTruthy()

    rerender(<CrossSeedDialog {...baseProps} hasSearched results={[result] as CrossSeedDialogProps["results"]} selectionCount={0} />)
    expect(queryByText("crossSeedDialog.addCount")).toBeNull()
    expect((queryByText("crossSeedDialog.add") as HTMLButtonElement).disabled).toBe(true)

    const added = new Map([["", { indexer: "Alpha Music", title: "Cedar.Harbor.FLAC", success: true }]])
    rerender(<CrossSeedDialog {...baseProps} hasSearched results={[result] as CrossSeedDialogProps["results"]} selectionCount={0} applyOutcomes={added} />)
    expect(queryByText("crossSeedDialog.add")).toBeNull()
  })

  it("offers one footer action: Search before a search, a fresh search after an empty one, Search once the pick changes", () => {
    const options = [{ id: 1, name: "Alpha Music" }]
    const onForceRefresh = vi.fn()
    const onScopeSearch = vi.fn()
    const props = { ...baseProps, indexerOptions: options, onForceRefresh, onScopeSearch }
    const { getByText, queryByText, rerender } = render(<CrossSeedDialog {...props} />)
    expect(queryByText("crossSeedDialog.scope.searchCount")).toBeTruthy()

    rerender(<CrossSeedDialog {...props} hasSearched />)
    expect(queryByText("crossSeedDialog.scope.searchCount")).toBeNull()
    fireEvent.click(getByText("crossSeedDialog.scope.searchAgain"))
    expect(onForceRefresh).toHaveBeenCalledTimes(1)
    expect(onScopeSearch).not.toHaveBeenCalled()

    rerender(<CrossSeedDialog {...props} hasSearched onForceRefresh={undefined} refreshCooldownUntil={Date.now() + 30_000} />)
    expect((getByText("crossSeedDialog.scope.searchAgain").closest("button") as HTMLButtonElement).disabled).toBe(true)

    rerender(<CrossSeedDialog {...props} hasSearched outcomesStale />)
    expect(queryByText("crossSeedDialog.scope.searchCount")).toBeTruthy()
    expect(queryByText("crossSeedDialog.scope.searchAgain")).toBeNull()
  })

  it("locks the indexer pick and the refresh while an add runs", () => {
    const options = [{ id: 1, name: "Alpha Music" }, { id: 2, name: "Beta Tracker" }]
    const result = { indexer: "Alpha Music", indexerId: 1, title: "Cedar.Harbor.FLAC", size: 1, seeders: 1, publishDate: "", downloadUrl: "u", guid: "g" }
    const props = { ...baseProps, indexerOptions: options, hasSearched: true, results: [result] as CrossSeedDialogProps["results"], selectionCount: 1, onForceRefresh: vi.fn(), canForceRefresh: true }
    const { queryByText, rerender } = render(<CrossSeedDialog {...props} />)
    expect(queryByText("crossSeedDialog.scope.change")).toBeTruthy()
    expect(queryByText("crossSeedDialog.refresh")).toBeTruthy()

    rerender(<CrossSeedDialog {...props} isSubmitting />)
    expect(queryByText("crossSeedDialog.scope.change")).toBeNull()
    expect(queryByText("crossSeedDialog.refresh")).toBeNull()
  })

  it("names the tag field with Tag, so a tap on the word leaves tagging on", () => {
    const onUseTagChange = vi.fn()
    const result = { indexer: "Alpha Music", indexerId: 1, title: "Cedar.Harbor.FLAC", size: 1, seeders: 1, publishDate: "", downloadUrl: "u", guid: "g" }
    const { getByLabelText, getByText } = render(
      <CrossSeedDialog {...baseProps} hasSearched results={[result] as CrossSeedDialogProps["results"]} useTag onUseTagChange={onUseTagChange} />
    )

    fireEvent.click(getByText("crossSeedDialog.tag"))
    expect(onUseTagChange).not.toHaveBeenCalled()
    expect(getByLabelText("crossSeedDialog.tag").tagName).toBe("INPUT")
  })

  it("shows the summary of an add above the results", () => {
    const result = { indexer: "Alpha Music", indexerId: 1, title: "Cedar.Harbor.FLAC", size: 1, seeders: 1, publishDate: "", downloadUrl: "u", guid: "g" }
    const { getByRole } = render(
      <CrossSeedDialog {...baseProps} hasSearched results={[result] as CrossSeedDialogProps["results"]} applySummary={{ level: "warning", message: "Added 1, 1 failed" }} />
    )

    expect(getByRole("status").textContent).toBe("Added 1, 1 failed")
  })

  it("does not retry a failed search when the user picked no indexer", () => {
    const options = [{ id: 1, name: "Alpha Music" }, { id: 2, name: "Beta Tracker" }]
    const { getByText } = render(
      <CrossSeedDialog {...baseProps} indexerOptions={options} indexerMode="custom" selectedIndexerIds={[]} hasSearched error="429" />
    )

    expect((getByText("crossSeedDialog.retry") as HTMLButtonElement).disabled).toBe(true)
  })

  it("toggles a result from a click on its row, but not from its indexer link", () => {
    const onToggleSelection = vi.fn()
    const result = { indexer: "Alpha Music", indexerId: 1, title: "Cedar.Harbor.FLAC", size: 1, seeders: 1, publishDate: "", downloadUrl: "u", guid: "g", infoUrl: "http://127.0.0.1:9/i" }
    const { getByText, getByLabelText } = render(
      <CrossSeedDialog {...baseProps} hasSearched results={[result] as CrossSeedDialogProps["results"]} onToggleSelection={onToggleSelection} />
    )

    fireEvent.click(getByLabelText("crossSeedDialog.openOnIndexer"))
    expect(onToggleSelection).not.toHaveBeenCalled()
    fireEvent.click(getByText("Cedar.Harbor.FLAC"))
    expect(onToggleSelection).toHaveBeenCalledTimes(1)
  })

  // regression: #4126
  it("offers Retry when the analysis fails before any search", () => {
    const onRetry = vi.fn()
    const { getByText } = render(<CrossSeedDialog {...baseProps} error="analyze failed" onRetry={onRetry} />)

    expect(getByText("analyze failed")).toBeTruthy()
    fireEvent.click(getByText("crossSeedDialog.retry"))
    expect(onRetry).toHaveBeenCalledTimes(1)
  })

  it("says that a site rejected the key last time", () => {
    render(<CrossSeedDialog {...baseProps} gazelleEntries={[{ ...ops, status: "will_ask", reason: "key_rejected" }]} />)

    expect(entryRow("Orpheus (API)")?.textContent).toContain("crossSeedDialog.gazelle.willAskRejected")
  })

  it("links to the RED/OPS settings when RED/OPS is on and no site has an API key", () => {
    render(<CrossSeedDialog {...baseProps} gazelleKeysMissing />)

    const link = entryRow("crossSeedDialog.gazelle.noApiKeys")?.querySelector("a")
    expect(link?.getAttribute("data-tab")).toBe("indexers")
  })

  it("shows the outcome of each site after the search, and the RED/OPS section without a decision trace", () => {
    const lookups: CrossSeedGazelleLookup[] = [
      { ...red, status: "failed", reason: "key_rejected", detail: "rejected the API key or this IP" },
      { ...ops, status: "no_match" },
    ]
    const { getByText } = render(<CrossSeedDialog {...baseProps} hasSearched gazelleEntries={lookups} gazelleLookups={lookups} />)

    expect(entryRow("Redacted (API)")?.textContent).toContain("crossSeedDialog.gazelle.failed.keyRejected")
    expect(entryRow("Orpheus (API)")?.getAttribute("data-status")).toBe("no_match")
    expect(getByText("crossSeedDialog.trace.copyReport")).toBeTruthy()
  })

  it("shows no indexers and no search button without RED/OPS keys or Torznab indexers", () => {
    const { getByText, queryByText } = render(<CrossSeedDialog {...baseProps} />)

    expect(getByText("crossSeedDialog.noIndexers")).toBeTruthy()
    expect(getByText("crossSeedDialog.noIndexersHelp")).toBeTruthy()
    expect(queryByText("crossSeedDialog.scope.search")).toBeNull()
  })

  it("shows that Torznab failed next to the RED/OPS results", () => {
    const { getByText } = render(<CrossSeedDialog {...baseProps} hasSearched torznabFailed />)

    expect(getByText("crossSeedDialog.torznabFailed")).toBeTruthy()
  })
})

describe("CrossSeedDialog routed adds", () => {
  const applyOutcomesFor = (instance: CrossSeedInstanceResult): CrossSeedDialogProps["applyOutcomes"] => new Map([
    ["", { indexer: "alpha", title: "Cedar.Harbor.S01E01", success: instance.success, infoHash: "abc", instanceResults: [instance] }],
  ])
  const appliedRow = [{ indexer: "alpha", indexerId: 1, title: "Cedar.Harbor.S01E01", size: 1, seeders: 1, publishDate: "", downloadUrl: "u", guid: "g" }] as CrossSeedDialogProps["results"]
  const notRouted: CrossSeedInstanceResult = { instanceId: 1, instanceName: "lib", success: true, status: "added", class: "added" }
  const routed: CrossSeedInstanceResult = { ...notRouted, crossSeedInstanceId: 2, crossSeedInstanceName: "seeds" }

  it("says where the cross-seeds go before the apply", () => {
    const { getByText, rerender, queryByText } = render(<CrossSeedDialog {...baseProps} crossSeedInstanceName="seeds" />)
    expect(getByText("crossSeedDialog.routedTo")).toBeTruthy()

    rerender(<CrossSeedDialog {...baseProps} />)
    expect(queryByText("crossSeedDialog.routedTo")).toBeNull()
  })

  it("names both instances of a routed result and leaves out the name of the one instance of a result that is not routed", () => {
    const { getByText, rerender, queryByText } = render(<CrossSeedDialog {...baseProps} hasSearched results={appliedRow} applyOutcomes={applyOutcomesFor(routed)} />)
    expect(getByText("crossseed:route.foundOnAddedTo")).toBeTruthy()

    rerender(<CrossSeedDialog {...baseProps} hasSearched results={appliedRow} applyOutcomes={applyOutcomesFor(notRouted)} />)
    expect(queryByText("lib")).toBeNull()
    expect(getByText("crossSeedDialog.status.added")).toBeTruthy()
    expect(queryByText("crossseed:route.foundOnAddedTo")).toBeNull()
  })

  it("labels the status of a failed routed add", () => {
    const { getByText } = render(
      <CrossSeedDialog {...baseProps} hasSearched results={appliedRow} applyOutcomes={applyOutcomesFor({ ...routed, success: false, status: "crossseed_instance_unavailable", message: "backend detail" })} />
    )

    expect(getByText("crossseed:routeStatus.unavailable")).toBeTruthy()
    expect(getByText("crossseed:route.foundOnRoutedTo")).toBeTruthy()
  })

  it.each([
    ["the Cross-seed instance of a routed result", routed, 2, "seeds"],
    ["the instance of a result that is not routed", notRouted, 1, "lib"],
  ])("blocks the hash on %s", async (_, instance, instanceId, name) => {
    const block = vi.spyOn(api, "addCrossSeedBlocklist").mockResolvedValue({ instanceId, infoHash: "abc", createdAt: "" })
    const { getByRole } = render(<CrossSeedDialog {...baseProps} hasSearched results={appliedRow} applyOutcomes={applyOutcomesFor(instance)} />)

    fireEvent.click(getByRole("button", { name: `crossSeedDialog.blockFor ${name}` }))

    await waitFor(() => expect(block).toHaveBeenCalledWith({ instanceId, infoHash: "abc" }))
  })
})
