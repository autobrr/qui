/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { TooltipProvider } from "@/components/ui/tooltip"
import "@/i18n"
import type { Automation, AutomationActivity, PendingDeleteList } from "@/types"

import { WorkflowsOverview } from "./WorkflowsOverview"

const mocks = vi.hoisted(() => ({
  instances: { instances: [{ id: 1, name: "seedbox", isActive: true }] },
  trackerData: { data: undefined },
  rules: [] as Automation[],
  activity: [] as AutomationActivity[],
  pendingDeletes: { rules: [] } as PendingDeleteList,
  incognitoMode: [false, () => {}] as [boolean, () => void],
}))

vi.mock("@/hooks/useInstances", () => ({ useInstances: () => mocks.instances }))
vi.mock("@/lib/incognito", async (importOriginal) => ({
  ...await importOriginal<typeof import("@/lib/incognito")>(),
  useIncognitoMode: () => mocks.incognitoMode,
}))
vi.mock("@/contexts/SyncStreamContext", () => ({ useActivityStream: () => {} }))
vi.mock("@/hooks/useTrackerCustomizations", () => ({ useTrackerCustomizations: () => mocks.trackerData }))
vi.mock("@/hooks/useTrackerIcons", () => ({ useTrackerIcons: () => mocks.trackerData }))
vi.mock("@/components/ui/json-editor", () => ({ JsonEditor: () => null, preloadJsonEditor: () => Promise.resolve() }))
vi.mock("@/lib/api", () => ({
  api: {
    listAutomations: () => Promise.resolve(mocks.rules),
    getAutomationActivity: () => Promise.resolve(mocks.activity),
    getPendingDeletes: () => Promise.resolve(mocks.pendingDeletes),
  },
}))

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  mocks.rules = []
  mocks.activity = []
  mocks.pendingDeletes = { rules: [] }
  mocks.incognitoMode = [false, () => {}]
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

async function renderOverview(queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })) {
  render(
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <WorkflowsOverview />
      </TooltipProvider>
    </QueryClientProvider>
  )
  fireEvent.click(await screen.findByRole("button", { name: /seedbox/ }))
}

function deleteRule(overrides: Partial<Automation>): Automation {
  return {
    id: 4, instanceId: 1, name: "Clean unregistered", trackerPattern: "*", enabled: true, dryRun: false, notify: false, sortOrder: 1,
    conditions: { schemaVersion: "1", delete: { enabled: true, mode: "delete", condition: { field: "IS_UNREGISTERED", operator: "EQUAL", value: "true" } } },
    ...overrides,
  }
}

describe("WorkflowsOverview", () => {
  it("shows the count of waiting torrents only on the delete rule that has them", async () => {
    mocks.rules = [deleteRule({ id: 4 }), deleteRule({ id: 5, name: "Clean ratio", sortOrder: 2 })]
    mocks.pendingDeletes = { rules: [{ ruleId: 4, ruleName: "Clean unregistered", torrents: [
      { hash: "aaaa1111", name: "Show.S01.1080p-GRP", tracker: "tracker.example.invalid", message: "Unregistered torrent", unregisteredSince: 1, deletesAt: 2, awaitsTracker: false },
      { hash: "bbbb2222", name: "Film.2020.1080p-GRP", tracker: "tracker.example.invalid", message: "Unregistered torrent", unregisteredSince: 1, deletesAt: 2, awaitsTracker: false },
    ] }] }
    await renderOverview()

    const badge = await screen.findByRole("button", { name: "2 waiting" })
    expect(badge.closest(".grid")?.textContent).toContain("Clean unregistered")
    expect(screen.getAllByText(/waiting$/)).toHaveLength(1)
  })

  it("lists the waiting torrents of a rule with what happens next", async () => {
    vi.spyOn(Date, "now").mockReturnValue(Date.UTC(2026, 10, 28, 12))
    const now = Date.UTC(2026, 10, 28, 12) / 1000
    mocks.rules = [deleteRule({})]
    mocks.pendingDeletes = { rules: [{ ruleId: 4, ruleName: "Clean unregistered", torrents: [
      { hash: "aaaa1111", name: "", tracker: "", message: "", unregisteredSince: now - 3 * 86400, deletesAt: now - 86400, awaitsTracker: true },
      { hash: "bbbb2222", name: "Film.2020.1080p-GRP", tracker: "tracker.example.invalid", message: "Unregistered torrent", unregisteredSince: now - 3600, deletesAt: now + 47 * 3600, awaitsTracker: false },
      { hash: "cccc3333", name: "", tracker: "", message: "", unregisteredSince: now - 3600, deletesAt: now + 47 * 3600, awaitsTracker: true },
    ] }] }
    await renderOverview()

    fireEvent.click(await screen.findByRole("button", { name: "3 waiting" }))

    const dialog = await screen.findByRole("dialog")
    expect(dialog.textContent).toContain("This rule deletes each torrent when its time is up, unless a tracker works again or the torrent is stopped.")
    const rows = Array.from(dialog.querySelectorAll("tbody tr")).map(row => Array.from(row.querySelectorAll("td")).map(cell => cell.textContent))
    expect(rows).toHaveLength(3)
    expect(rows[0][1]).toBe("aaaa1111")
    expect(rows[0][4]).toBe("When the tracker answers unregistered")
    expect(rows[1][0]).toContain("tracker.example.invalid")
    expect(rows[1].slice(1, 3)).toEqual(["Film.2020.1080p-GRP", "Unregistered torrent"])
    expect(rows[1][4]).toMatch(/^After /)
    expect(rows[2][4]).toBe(rows[1][4])
  })

  it("reads the waiting torrents again when a rule of the instance is saved", async () => {
    const waiting = (hash: string) => ({ hash, name: "", tracker: "", message: "", unregisteredSince: 1, deletesAt: 2, awaitsTracker: false })
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mocks.rules = [deleteRule({})]
    mocks.pendingDeletes = { rules: [{ ruleId: 4, ruleName: "Clean unregistered", torrents: [waiting("aaaa1111")] }] }
    await renderOverview(queryClient)
    await screen.findByRole("button", { name: "1 waiting" })

    mocks.pendingDeletes = { rules: [{ ruleId: 4, ruleName: "Clean unregistered", torrents: [waiting("aaaa1111"), waiting("bbbb2222")] }] }
    await act(() => queryClient.invalidateQueries({ queryKey: ["automations", 1] }))

    expect(await screen.findByRole("button", { name: "2 waiting" })).toBeTruthy()
  })

  it("hides the name and the tracker of each waiting torrent in incognito mode", async () => {
    mocks.incognitoMode = [true, () => {}]
    mocks.rules = [deleteRule({})]
    mocks.pendingDeletes = { rules: [{ ruleId: 4, ruleName: "Clean unregistered", torrents: [
      { hash: "bbbb2222", name: "Film.2020.1080p-GRP", tracker: "tracker.example.invalid", message: "Unregistered torrent", unregisteredSince: 1, deletesAt: 2, awaitsTracker: false },
      { hash: "cccc3333", name: "", tracker: "", message: "", unregisteredSince: 1, deletesAt: 3, awaitsTracker: false },
    ] }] }
    await renderOverview()

    fireEvent.click(await screen.findByRole("button", { name: "2 waiting" }))

    const dialog = await screen.findByRole("dialog")
    expect(dialog.querySelectorAll("tbody tr")).toHaveLength(2)
    expect(dialog.textContent).not.toContain("Film.2020.1080p-GRP")
    expect(dialog.textContent).not.toContain("cccc3333")
    expect(dialog.textContent).not.toContain("tracker.example.invalid")
    expect(dialog.querySelectorAll("tbody td")[2].textContent).toBe("Tracker message hidden in incognito mode")
  })

  it("shows whether each torrent of a live Transfer started", async () => {
    mocks.activity = [
      transferActivity({ id: 1, hash: "aaaa1111", torrentName: "Show.S01.1080p-GRP" }),
      transferActivity({ id: 2, hash: "bbbb2222", torrentName: "Film.2020.1080p-GRP", outcome: "failed", reason: "The target is offline." }),
    ]
    await renderOverview()

    await screen.findByText("Film.2020.1080p-GRP")
    const badges = screen.getAllByText("Transfer to instance").map(action => action.parentElement?.textContent)
    expect(badges).toEqual(["Transfer to instanceStarted", "Transfer to instanceFailed"])
  })

  it.each([
    ["external_program", "External program"],
    ["delete_failed", "Delete"],
  ] as const)("shows Failed as the outcome of a failed %s run", async (action, actionLabel) => {
    mocks.activity = [transferActivity({ action, outcome: "failed", reason: "The target is offline." })]
    await renderOverview()

    await screen.findByText("The target is offline.")
    expect(screen.getByText(actionLabel).parentElement?.textContent).toBe(`${actionLabel}Failed`)
  })

  it("shows the torrents that start and end the wait of a delete rule, with the rule and the cause", async () => {
    mocks.activity = [
      transferActivity({ id: 1, action: "unregistered_wait_started", ruleId: undefined, ruleName: undefined,
        details: { count: 3, rules: [{ id: 4, name: "Clean unregistered", count: 3 }] } }),
      transferActivity({ id: 2, action: "unregistered_wait_ended", ruleId: undefined, ruleName: undefined,
        details: { count: 2, causes: { stopped: 1, trackerWorks: 1 }, rules: [{ id: 4, name: "Clean unregistered", count: 2 }] } }),
    ]
    await renderOverview()

    const started = await screen.findByText("3 torrents started waiting to be deleted")
    const ended = screen.getByText("2 torrents no longer wait to be deleted")
    const startedRow = started.closest("[role=button]")
    const endedRow = ended.closest("[role=button]")
    expect(startedRow?.textContent).toContain("Wait started")
    expect(startedRow?.textContent).toContain("Clean unregistered (3)")
    expect(startedRow?.textContent).not.toContain("Removed")
    expect(endedRow?.textContent).toContain("Wait ended")
    expect(endedRow?.textContent).toContain("Stopped: 1")
    expect(endedRow?.textContent).toContain("Tracker works again or trackers edited: 1")
  })

  it("finds the wait lines of a rule when the search names the rule", async () => {
    mocks.activity = [
      transferActivity({ id: 1, action: "unregistered_wait_started", ruleId: undefined, ruleName: undefined,
        details: { count: 3, rules: [{ id: 4, name: "Clean unregistered", count: 3 }] } }),
      transferActivity({ id: 2, hash: "aaaa1111", torrentName: "Show.S01.1080p-GRP" }),
    ]
    await renderOverview()

    await screen.findByText("Show.S01.1080p-GRP")
    fireEvent.change(screen.getByPlaceholderText("Filter by name, hash, or rule..."), { target: { value: "clean unreg" } })
    expect(screen.getByText("3 torrents started waiting to be deleted")).toBeTruthy()
    expect(screen.queryByText("Show.S01.1080p-GRP")).toBeNull()
  })

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
