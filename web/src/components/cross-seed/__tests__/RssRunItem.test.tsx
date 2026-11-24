// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

import { afterEach, beforeEach, describe, it, expect, vi } from "vitest"
import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import type { ReactElement } from "react"
import { TooltipProvider } from "@/components/ui/tooltip"
import type { CrossSeedRun } from "@/types"

// RSSRunItem only calls useTranslation; override it with an interpolating
// identity translator so we can assert on the rendered error text and its
// tooltip, while keeping the module's other exports (initReactI18next, used
// by the i18n bootstrap pulled in through the page's import graph).
vi.mock("react-i18next", async (importOriginal) => {
  const actual = await importOriginal<typeof import("react-i18next")>()
  return {
    ...actual,
    useTranslation: () => ({
      t: (key: string, opts?: Record<string, unknown>) =>
        opts && "message" in opts ? `${key}:${String(opts.message)}`
          : opts && "crossSeed" in opts ? `${key}:${String(opts.source)}->${String(opts.crossSeed)}` : key,
    }),
  }
})

import { RSSRunItem } from "../RssRunItem"

function makeRun(overrides: Partial<CrossSeedRun> = {}): CrossSeedRun {
  return {
    id: 1,
    triggeredBy: "auto",
    mode: "auto",
    status: "failed",
    startedAt: "2026-01-01T00:00:00Z",
    totalFeedItems: 0,
    candidatesFound: 0,
    crossSeedsAdded: 0,
    candidatesFailed: 0,
    candidatesSkipped: 0,
    createdAt: "2026-01-01T00:00:00Z",
    ...overrides,
  }
}

const touchWindow = window as Window & { ontouchstart?: unknown }

beforeEach(() => {
  delete touchWindow.ontouchstart
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  cleanup()
})

function renderWithTooltips(ui: ReactElement) {
  return render(<TooltipProvider>{ui}</TooltipProvider>)
}

describe("RSSRunItem", () => {
  it("shows the full run error in a tooltip when the error is cut off", () => {
    const message = "all indexers unreachable"
    vi.spyOn(HTMLElement.prototype, "scrollWidth", "get").mockReturnValue(200)
    vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockReturnValue(50)
    renderWithTooltips(
      <RSSRunItem run={makeRun({ errorMessage: message })} formatDateValue={() => "just now"} />
    )

    fireEvent.focus(screen.getByText(`automation.runError:${message}`))
    expect(screen.getByRole("tooltip").textContent).toContain(`automation.runError:${message}`)
  })

  // regression: #4358
  it("shows the full cut-off run error on a tap on a phone, also when the run has no results to open", () => {
    touchWindow.ontouchstart = null
    const message = "all indexers unreachable"
    vi.spyOn(HTMLElement.prototype, "scrollWidth", "get").mockReturnValue(200)
    vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockReturnValue(50)
    renderWithTooltips(
      <RSSRunItem run={makeRun({ errorMessage: message })} formatDateValue={() => "just now"} />
    )

    const error = screen.getByText(`automation.runError:${message}`)
    fireEvent.pointerDown(error, { pointerType: "touch" })
    fireEvent.pointerUp(error, { pointerType: "touch" })
    fireEvent.click(error)

    expect(screen.getByRole("tooltip").textContent).toContain(`automation.runError:${message}`)
  })

  it("renders no error span when the run has no errorMessage", () => {
    const { container } = renderWithTooltips(
      <RSSRunItem run={makeRun({ status: "success" })} formatDateValue={() => "just now"} />
    )

    expect(container.textContent).not.toContain("automation.runError")
  })

  it("shows only the instance name on a result that is not routed", () => {
    const { container } = renderWithTooltips(
      <RSSRunItem
        run={makeRun({ status: "success", results: [{ instanceId: 1, instanceName: "lib", success: true, status: "added", matchedTorrentName: "Cedar.Harbor.S01E01" }] })}
        formatDateValue={() => "just now"}
      />
    )
    fireEvent.click(container.querySelector("[aria-expanded]")!)

    expect(screen.getByText("lib").closest("[data-slot=badge]")?.textContent).toBe("lib")
    expect(container.textContent).not.toContain("route.")
  })

  it("names the instance that held the match and the Cross-seed instance on a routed add", () => {
    const { container } = renderWithTooltips(
      <RSSRunItem
        run={makeRun({
          status: "success",
          results: [{
            instanceId: 1, instanceName: "lib", success: true, status: "added_hardlink", matchedTorrentName: "Cedar.Harbor.S01E01",
            crossSeedInstanceId: 2, crossSeedInstanceName: "seeds",
          }],
        })}
        formatDateValue={() => "just now"}
      />
    )
    fireEvent.click(container.querySelector("[aria-expanded]")!)

    expect(container.textContent).toContain("crossseed:route.foundOnAddedTo:lib->seeds")
  })

  it("labels the status of a failed routed add", () => {
    const { container } = renderWithTooltips(
      <RSSRunItem
        run={makeRun({
          status: "partial",
          results: [{
            instanceId: 1, instanceName: "lib", success: false, status: "crossseed_instance_cannot_see_data", message: "backend detail",
            crossSeedInstanceId: 2, crossSeedInstanceName: "seeds",
          }],
        })}
        formatDateValue={() => "just now"}
      />
    )
    fireEvent.click(container.querySelector("[aria-expanded]")!)

    expect(container.textContent).toContain("crossseed:routeStatus.cannotSeeData")
    expect(container.textContent).toContain("backend detail")
    expect(container.textContent).toContain("crossseed:route.foundOnRoutedTo:lib->seeds")
  })
})
