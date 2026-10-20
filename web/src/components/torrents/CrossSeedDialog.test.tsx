/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { CrossSeedSearchDecisionTrace } from "@/types"
import { cleanup, fireEvent, render, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { buildCrossSeedTraceReport, CrossSeedDialog, type CrossSeedDialogProps } from "./CrossSeedDialog"

const { toast } = vi.hoisted(() => ({ toast: { success: vi.fn(), error: vi.fn() } }))
vi.mock("react-i18next", async (importOriginal) => ({ ...(await importOriginal<object>()), useTranslation: () => ({ t: (key: string) => key, i18n: { language: "en" } }) }))
vi.mock("sonner", () => ({ toast }))

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
  vi.unstubAllGlobals()
  Reflect.deleteProperty(document, "execCommand")
})

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

    const noop = () => {}
    const props = {
      open: true, onOpenChange: noop, torrent: null, results: [], selectedKeys: new Set<string>(), selectionCount: 0,
      isLoading: false, isSubmitting: false, error: null, applyResult: null, indexerOptions: [], indexerMode: "all",
      selectedIndexerIds: [], indexerNameMap: {}, onIndexerModeChange: noop, onToggleIndexer: noop,
      onSelectAllIndexers: noop, onClearIndexerSelection: noop, onScopeSearch: noop, getResultKey: () => "",
      onToggleSelection: noop, onSelectAll: noop, onClearSelection: noop, onRetry: noop, onClose: noop, onApply: noop,
      useTag: false, onUseTagChange: noop, tagName: "", onTagNameChange: noop, startPaused: false,
      onStartPausedChange: noop, hasSearched: true,
      decisionTrace: {
        sourceSize: 0, tolerancePercent: 0, totalResults: 0, sizeFiltered: 0, releaseFiltered: 0,
        lateContentFiltered: 0, duplicateFiltered: 0, finalMatches: 0,
      },
    } satisfies CrossSeedDialogProps
    const { getByText } = render(<CrossSeedDialog {...props} />)

    fireEvent.click(getByText("crossSeedDialog.trace.title"))
    fireEvent.click(getByText("crossSeedDialog.trace.copyReport"))

    await waitFor(() => expect(toast.success).toHaveBeenCalledWith("crossSeedDialog.trace.copied"))
    expect(execCommand).toHaveBeenCalledWith("copy")
    expect(toast.error).not.toHaveBeenCalled()
  })
})
