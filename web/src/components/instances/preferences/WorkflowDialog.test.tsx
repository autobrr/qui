/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { TooltipProvider } from "@/components/ui/tooltip"
import type { Automation, FilesystemCapabilities } from "@/types"

const mocks = vi.hoisted(() => {
  const t = (key: string) => key
  return {
    query: { data: undefined },
    preferences: { data: undefined as { queueing_enabled?: boolean } | undefined },
    instancesQuery: { data: [] as unknown[], isLoading: false, error: null },
    translation: { t, i18n: { t, language: "en" } },
    error: vi.fn(),
    preview: vi.fn(),
    save: vi.fn(),
    dryRun: vi.fn().mockResolvedValue({ activities: [] }),
  }
})

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...await importOriginal<typeof import("@tanstack/react-query")>(),
  useQuery: ({ queryKey }: { queryKey: string[] }) => queryKey[0] === "instances" ? mocks.instancesQuery : mocks.query,
}))
vi.mock("@/hooks/useInstanceMetadata", () => ({ useInstanceMetadata: () => mocks.query }))
vi.mock("@/hooks/useInstancePreferences", () => ({ useInstancePreferences: () => ({ preferences: mocks.preferences.data }) }))
vi.mock("@/hooks/useTrackerIcons", () => ({ useTrackerIcons: () => mocks.query }))
vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => mocks.translation,
}))
vi.mock("sonner", () => ({ toast: { error: mocks.error, success: vi.fn() } }))
vi.mock("@/lib/api", () => ({
  api: {
    previewAutomation: mocks.preview,
    updateAutomation: mocks.save,
    dryRunAutomation: mocks.dryRun,
  },
}))

const scrollIntoView = Object.getOwnPropertyDescriptor(Element.prototype, "scrollIntoView")

beforeEach(() => {
  vi.stubGlobal("ResizeObserver", class {
    observe() {}
    unobserve() {}
    disconnect() {}
  })
})

afterEach(() => {
  if (scrollIntoView) Object.defineProperty(Element.prototype, "scrollIntoView", scrollIntoView)
  else Reflect.deleteProperty(Element.prototype, "scrollIntoView")
  cleanup()
  vi.clearAllMocks()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  mocks.preferences.data = undefined
})

import { WorkflowDialog } from "./WorkflowDialog"

const rule: Automation = {
  id: 1,
  instanceId: 1,
  name: "Category validation",
  trackerPattern: "*",
  conditions: { schemaVersion: "1", pause: { enabled: true } },
  enabled: false,
  dryRun: true,
  notify: false,
  sortOrder: 0,
}

const key = (suffix: string) => `preferences.workflowDialog.${suffix}`

// Radix also renders a hidden native <option> with the same text, so match the visible trigger.
const selectTrigger = (text: string) => {
  const trigger = screen.getAllByRole("combobox").find(el => el.textContent === text)
  if (!trigger) throw new Error(`no select showing ${text}`)
  return trigger
}

function renderDialog(dialogRule: Automation = rule) {
  vi.stubGlobal("ResizeObserver", class {
    observe() {}
    unobserve() {}
    disconnect() {}
  })
  // Radix scrolls focused select options; jsdom has no layout.
  Object.defineProperty(Element.prototype, "scrollIntoView", { configurable: true, value: vi.fn() })
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <WorkflowDialog open onOpenChange={() => {}} instanceId={1} rule={dialogRule} />
      </TooltipProvider>
    </QueryClientProvider>
  )
  return client
}

describe("WorkflowDialog category validation", () => {
  it("rejects an unselected category for save, enable, and dry run, but accepts Uncategorized", async () => {
    const client = renderDialog()

    fireEvent.keyDown(screen.getByText(key("actions.addAction")), { key: "ArrowDown" })
    fireEvent.click(await screen.findByRole("option", { name: key("actions.category") }))

    for (const control of [
      screen.getByRole("button", { name: key("save") }),
      screen.getByRole("switch", { name: key("footer.enabled") }),
      screen.getByRole("button", { name: key("runDryRunNow") }),
    ]) {
      mocks.error.mockClear()
      fireEvent.click(control)
      expect(mocks.error).toHaveBeenCalledWith(key("toast.selectCategory"))
      expect(mocks.save).not.toHaveBeenCalled()
      expect(mocks.preview).not.toHaveBeenCalled()
      expect(mocks.dryRun).not.toHaveBeenCalled()
    }

    fireEvent.keyDown(screen.getByText(key("category.selectCategory")), { key: "ArrowDown" })
    fireEvent.click(await screen.findByRole("option", { name: key("uncategorized") }))
    fireEvent.click(screen.getByRole("button", { name: key("runDryRunNow") }))
    await waitFor(() => expect(mocks.dryRun).toHaveBeenCalledWith(1, expect.objectContaining({
      conditions: expect.objectContaining({ category: expect.objectContaining({ enabled: true, category: "" }) }),
    })))
    client.clear()
  })
})

describe("WorkflowDialog queue position", () => {
  it("disables the action while the instance has queueing off", async () => {
    mocks.preferences.data = { queueing_enabled: false }
    const client = renderDialog()

    fireEvent.keyDown(screen.getByText(key("actions.addAction")), { key: "ArrowDown" })
    const option = await screen.findByRole("option", { name: key("actions.queuePositionNeedsQueueing") })
    expect(option.getAttribute("aria-disabled")).toBe("true")
    expect(screen.queryByRole("option", { name: key("actions.queuePosition") })).toBeNull()
    client.clear()
  })

  it("sends the chosen position when queueing is on", async () => {
    mocks.preferences.data = { queueing_enabled: true }
    const client = renderDialog()

    fireEvent.keyDown(screen.getByText(key("actions.addAction")), { key: "ArrowDown" })
    fireEvent.click(await screen.findByRole("option", { name: key("actions.queuePosition") }))
    fireEvent.keyDown(selectTrigger(key("queuePosition.top")), { key: "ArrowDown" })
    fireEvent.click(await screen.findByRole("option", { name: key("queuePosition.bottom") }))

    fireEvent.click(screen.getByRole("button", { name: key("runDryRunNow") }))
    await waitFor(() => expect(mocks.dryRun).toHaveBeenCalledWith(1, expect.objectContaining({
      conditions: expect.objectContaining({ queuePosition: { enabled: true, position: "bottom", condition: undefined } }),
    })))
    client.clear()
  })

  it("shows a saved action with a disabled control and a note once queueing is turned off", () => {
    mocks.preferences.data = { queueing_enabled: false }
    const client = renderDialog({
      ...rule,
      conditions: { schemaVersion: "1", queuePosition: { enabled: true, position: "bottom" } },
    })

    expect(screen.getByText(key("queuePosition.queueingDisabled"))).not.toBeNull()
    expect(selectTrigger(key("queuePosition.bottom")).hasAttribute("disabled")).toBe(true)
    client.clear()
  })

  it("keeps the action usable while preferences are unknown", () => {
    mocks.preferences.data = {}
    const client = renderDialog({
      ...rule,
      conditions: { schemaVersion: "1", queuePosition: { enabled: true, position: "bottom" } },
    })

    expect(screen.queryByText(key("queuePosition.queueingDisabled"))).toBeNull()
    expect(selectTrigger(key("queuePosition.bottom")).hasAttribute("disabled")).toBe(false)
    client.clear()
  })
})

const noCapabilities: FilesystemCapabilities = { read: false, identity: false, trustedIdentity: false, write: false, content: false }

describe("WorkflowDialog include hardlinks", () => {
  const deleteRule: Automation = {
    ...rule,
    conditions: { schemaVersion: "1", delete: { enabled: true, mode: "deleteWithFilesIncludeCrossSeeds" } },
  }

  it.each([
    { name: "a local instance", hasLocalFilesystemAccess: true, filesystemMode: "local", capabilities: { read: true, identity: true, trustedIdentity: true, write: true, content: true }, enabled: true },
    { name: "a remote instance", hasLocalFilesystemAccess: false, filesystemMode: "remote", capabilities: { ...noCapabilities, read: true }, enabled: false },
    // Identity a remote host reports never adds torrents to a delete.
    { name: "a remote instance with the identity capability", hasLocalFilesystemAccess: false, filesystemMode: "remote", capabilities: { ...noCapabilities, read: true, identity: true }, enabled: false },
    // The setting is off here, so only the capability can offer the option.
    { name: "an instance with the trusted identity capability", hasLocalFilesystemAccess: false, filesystemMode: "local", capabilities: { ...noCapabilities, read: true, identity: true, trustedIdentity: true }, enabled: true },
  ])("follows the trusted identity capability for $name", ({ hasLocalFilesystemAccess, filesystemMode, capabilities, enabled }) => {
    mocks.instancesQuery.data = [{ id: 1, hasLocalFilesystemAccess, filesystemMode, capabilities }]
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={client}>
        <WorkflowDialog open onOpenChange={() => {}} instanceId={1} rule={deleteRule} />
      </QueryClientProvider>
    )

    const checkbox = screen.getByRole("checkbox", { name: key("delete.includeHardlinkedCopies") })
    expect(checkbox.hasAttribute("disabled")).toBe(!enabled)
    client.clear()
  })
})

describe("WorkflowDialog tracker pattern", () => {
  it.each([
    { name: "include", trackerPattern: "a.example,b.example", chips: ["a.example", "b.example"], mode: "trackerMatchInclude" },
    { name: "exclude", trackerPattern: "!a.example,!b.example", chips: ["a.example", "b.example"], mode: "trackerMatchExclude" },
    { name: "mixed", trackerPattern: "a.example,!b.example", chips: ["a.example", "b.example"], mode: null },
    { name: "all trackers", trackerPattern: "*", chips: [], mode: null },
  ])("loads the chips and mode from a $name pattern and saves only the pattern", async ({ trackerPattern, chips, mode }) => {
    mocks.instancesQuery.data = []
    mocks.save.mockResolvedValue({ ...rule, trackerPattern })
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={client}>
        <WorkflowDialog open onOpenChange={() => {}} instanceId={1} rule={{ ...rule, trackerPattern }} />
      </QueryClientProvider>
    )

    expect(screen.getByRole("switch", { name: key("allTrackers") }).getAttribute("aria-checked")).toBe(String(trackerPattern === "*"))
    for (const chip of chips) {
      expect(screen.getAllByText(chip).length).toBeGreaterThan(0)
    }
    // The selected mode button has the secondary variant; mixed selects neither.
    const selectedModes = ["trackerMatchInclude", "trackerMatchExclude"].filter((m) =>
      screen.queryByRole("button", { name: key(m) })?.className.includes("bg-secondary"))
    expect(selectedModes).toEqual(mode ? [mode] : [])

    fireEvent.click(screen.getByRole("button", { name: key("save") }))
    await waitFor(() => expect(mocks.save).toHaveBeenCalled())
    const payload = mocks.save.mock.calls[0][2]
    expect(payload.trackerPattern).toBe(trackerPattern)
    expect(payload).not.toHaveProperty("trackerDomains")
    client.clear()
  })
})

describe("WorkflowDialog move path on a disabled rule", () => {
  const renderRule = (path: string) => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const moveRule: Automation = { ...rule, enabled: false, conditions: { schemaVersion: "1", move: { enabled: true, path } } }
    render(
      <QueryClientProvider client={client}>
        <TooltipProvider>
          <WorkflowDialog open onOpenChange={() => {}} instanceId={1} rule={moveRule} />
        </TooltipProvider>
      </QueryClientProvider>
    )
    return client
  }

  it("saves a rule saved before the check existed, since the server skips disabled rules", async () => {
    mocks.save.mockResolvedValue({})
    const client = renderRule("data/archive")

    fireEvent.click(screen.getByRole("button", { name: key("save") }))

    await waitFor(() => expect(mocks.save).toHaveBeenCalled())
    client.clear()
  })

  it("shows the dialog's own move path error when a dry run is blocked", () => {
    const client = renderRule("data/archive")
    expect(screen.queryByText(key("move.errors.mustBeAbsolute"))).toBeNull()

    fireEvent.click(screen.getByRole("button", { name: key("runDryRunNow") }))

    expect(mocks.dryRun).not.toHaveBeenCalled()
    expect(screen.getByText(key("move.errors.mustBeAbsolute"))).not.toBeNull()
    // The error explains the dry run; saving the disabled rule stays allowed.
    expect(screen.getByRole("button", { name: key("save") }).hasAttribute("disabled")).toBe(false)
    client.clear()
  })

  it("shows the server's move path error when a dry run is rejected", async () => {
    const message = "Move path must be absolute, for example /data/archive or D:\\Archive. For a sample torrent it renders \"sample/x\""
    mocks.dryRun.mockRejectedValueOnce(new Error(message))
    const client = renderRule("{{ .Category }}/x")

    fireEvent.click(screen.getByRole("button", { name: key("runDryRunNow") }))

    expect(await screen.findByText(message)).not.toBeNull()
    expect(mocks.error).not.toHaveBeenCalled()
    client.clear()
  })
})
