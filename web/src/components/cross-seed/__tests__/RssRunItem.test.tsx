// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

import { afterEach, describe, it, expect, vi } from "vitest"
import { cleanup, fireEvent, render } from "@testing-library/react"
import type { CrossSeedRun } from "@/types"

// RSSRunItem only calls useTranslation; override it with an interpolating
// identity translator so we can assert on the rendered error text and title
// attribute, while keeping the module's other exports (initReactI18next, used
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

afterEach(cleanup)

describe("RSSRunItem", () => {
  it("renders the run error message with a full-text title on a failed run", () => {
    const message = "all indexers unreachable"
    const { container } = render(
      <RSSRunItem run={makeRun({ errorMessage: message })} formatDateValue={() => "just now"} />
    )

    const errorSpan = container.querySelector(`span[title="${message}"]`)
    expect(errorSpan).not.toBeNull()
    expect(errorSpan?.textContent).toBe(`automation.runError:${message}`)
  })

  it("renders no error span when the run has no errorMessage", () => {
    const { container } = render(
      <RSSRunItem run={makeRun({ status: "success" })} formatDateValue={() => "just now"} />
    )

    // React drops title={undefined}, so a span[title] query cannot see the
    // guard; assert on the rendered text instead.
    expect(container.textContent).not.toContain("automation.runError")
  })

  it("shows only the instance name on a result that is not routed", () => {
    const { container } = render(
      <RSSRunItem
        run={makeRun({ status: "success", results: [{ instanceId: 1, instanceName: "lib", success: true, status: "added", matchedTorrentName: "Cedar.Harbor.S01E01" }] })}
        formatDateValue={() => "just now"}
      />
    )
    fireEvent.click(container.querySelector("[aria-expanded]")!)

    expect(container.querySelector("span[title=\"lib\"]")?.textContent).toBe("lib")
    expect(container.textContent).not.toContain("route.")
  })

  it("names the instance that held the match and the Cross-seed instance on a routed add", () => {
    const { container } = render(
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
    const { container } = render(
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
