// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

import { describe, it, expect, vi } from "vitest"
import { render } from "@testing-library/react"
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
        opts && "message" in opts ? `${key}:${String(opts.message)}` : key,
    }),
  }
})

import { RSSRunItem } from "@/pages/CrossSeedPage"

function makeRun(overrides: Partial<CrossSeedRun> = {}): CrossSeedRun {
  return {
    id: 1,
    triggeredBy: "auto",
    mode: "auto",
    status: "failed",
    startedAt: "2026-01-01T00:00:00Z",
    totalFeedItems: 0,
    candidatesFound: 0,
    torrentsAdded: 0,
    torrentsFailed: 0,
    torrentsSkipped: 0,
    createdAt: "2026-01-01T00:00:00Z",
    ...overrides,
  }
}

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

    expect(container.querySelector("span[title]")).toBeNull()
  })
})
