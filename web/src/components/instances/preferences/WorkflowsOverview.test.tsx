/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { TooltipProvider } from "@/components/ui/tooltip"
import "@/i18n"
import type { Automation, AutomationActivity } from "@/types"

import { WorkflowsOverview } from "./WorkflowsOverview"

// Stable singletons: a fresh object per render would rerun effects forever.
const mocks = vi.hoisted(() => ({
  instances: { instances: [{ id: 1, name: "seedbox", isActive: true }] },
  trackerData: { data: undefined },
  rules: [] as Automation[],
  activity: [] as AutomationActivity[],
}))

vi.mock("@/hooks/useInstances", () => ({ useInstances: () => mocks.instances }))
vi.mock("@/contexts/SyncStreamContext", () => ({ useActivityStream: () => {} }))
vi.mock("@/hooks/useTrackerCustomizations", () => ({ useTrackerCustomizations: () => mocks.trackerData }))
vi.mock("@/hooks/useTrackerIcons", () => ({ useTrackerIcons: () => mocks.trackerData }))
vi.mock("@/components/ui/json-editor", () => ({ JsonEditor: () => null, preloadJsonEditor: () => Promise.resolve() }))
vi.mock("@/lib/api", () => ({
  api: {
    listAutomations: () => Promise.resolve(mocks.rules),
    getAutomationActivity: () => Promise.resolve(mocks.activity),
  },
}))

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  mocks.rules = []
  mocks.activity = []
})

function transferRule(overrides: Partial<Automation>): Automation {
  return {
    id: 1, instanceId: 1, name: "Transfer rule", trackerPattern: "*", enabled: true, dryRun: false, notify: false, sortOrder: 1,
    conditions: { schemaVersion: "1", transferToInstance: { enabled: true, targetInstanceId: 2, savePath: "" } },
    ...overrides,
  }
}

function transferActivity(overrides: Partial<AutomationActivity>): AutomationActivity {
  return {
    id: 1, instanceId: 1, hash: "", action: "transferred_to_instance", outcome: "success",
    ruleId: 1, ruleName: "Transfer rule", details: { count: 1 }, createdAt: "2026-01-02T10:00:00Z",
    ...overrides,
  }
}

async function renderOverview() {
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <TooltipProvider>
        <WorkflowsOverview />
      </TooltipProvider>
    </QueryClientProvider>
  )
  fireEvent.click(await screen.findByRole("button", { name: /seedbox/ }))
}

describe("WorkflowsOverview", () => {
  // regression: #4242
  it("shows whether each torrent of a live Transfer started", async () => {
    mocks.activity = [
      transferActivity({ id: 1, hash: "aaaa1111", torrentName: "Show.S01.1080p-GRP" }),
      transferActivity({ id: 2, hash: "bbbb2222", torrentName: "Film.2020.1080p-GRP", outcome: "failed", reason: "The target is offline." }),
    ]
    await renderOverview()

    await screen.findByText("Film.2020.1080p-GRP")
    const badges = screen.getAllByText("Transfer to instance").map(action => action.parentElement?.textContent)
    expect(badges).toEqual([expect.stringContaining("Started"), expect.stringContaining("failed")])
  })

  // regression: #4242
  it("shows no All torrents badge for a Transfer rule with a condition", async () => {
    mocks.rules = [transferRule({
      conditions: {
        schemaVersion: "1",
        transferToInstance: { enabled: true, targetInstanceId: 2, savePath: "", condition: { field: "NAME", operator: "CONTAINS", value: "1080p" } },
      },
    })]
    await renderOverview()

    await screen.findByText("Transfer rule")
    expect(screen.queryByText("All torrents")).toBeNull()
  })
})
