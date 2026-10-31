/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { SeasonPackRun } from "@/types"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => ({
    t: (key: string, opts?: Record<string, unknown> | string) =>
      typeof opts === "object" && "crossSeed" in opts ? `${key}:${String(opts.source)}->${String(opts.crossSeed)}` : key,
  }),
}))
vi.mock("@/lib/api", () => ({
  api: { getInstances: () => Promise.resolve([{ id: 1, name: "lib" }, { id: 2, name: "seeds" }]) },
}))

import { SeasonPackRunsPanel } from "../SeasonPackRunsPanel"

afterEach(cleanup)

function makeRun(overrides: Partial<SeasonPackRun>): SeasonPackRun {
  return {
    id: 1, torrentName: "Cedar.Harbor.S01", phase: "apply", status: "applied", reason: "", message: "",
    matchedEpisodes: 4, totalEpisodes: 4, coverage: 1, createdAt: "2026-01-01T00:00:00Z", ...overrides,
  }
}

function renderPanel(run: SeasonPackRun) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={client}>
      <SeasonPackRunsPanel runs={[run]} isLoading={false} isFetching={false} error={null} onRefresh={() => {}} formatDateValue={() => "just now"} />
    </QueryClientProvider>
  )
  fireEvent.click(screen.getByText("seasonPackRuns.title"))
}

describe("SeasonPackRunsPanel instances", () => {
  it("shows the instance name of an apply that is not routed", async () => {
    renderPanel(makeRun({ instanceId: 1, addedInstanceId: 1 }))

    expect(await screen.findByText("lib")).toBeTruthy()
    expect(screen.queryByText(/route\./)).toBeNull()
  })

  it("names both instances of a routed apply", async () => {
    renderPanel(makeRun({ instanceId: 1, addedInstanceId: 2 }))

    expect(await screen.findByText("crossseed:route.foundOnAddedTo:lib->seeds")).toBeTruthy()
  })

  it("labels the reason of a failed routed apply", async () => {
    renderPanel(makeRun({ status: "failed", reason: "crossseed_instance_unavailable", instanceId: 1, addedInstanceId: 2 }))

    expect(screen.getByText("· crossseed:routeStatus.unavailable")).toBeTruthy()
    expect(await screen.findByText("crossseed:route.foundOnRoutedTo:lib->seeds")).toBeTruthy()
  })
})
