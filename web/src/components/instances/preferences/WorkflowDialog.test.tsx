/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { TooltipProvider } from "@/components/ui/tooltip"
import type { Automation, FilesystemCapabilities } from "@/types"

const mocks = vi.hoisted(() => {
  const t = (key: string) => key
  return {
    query: { data: undefined },
    instancesQuery: { data: [] as unknown[], isLoading: false, error: null },
    translation: { t, i18n: { t, language: "en" } },
    error: vi.fn(),
    preview: vi.fn(),
    save: vi.fn(),
    create: vi.fn(),
    validateRegex: vi.fn(),
    dryRun: vi.fn().mockResolvedValue({ activities: [] }),
  }
})

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...await importOriginal<typeof import("@tanstack/react-query")>(),
  useQuery: ({ queryKey }: { queryKey: string[] }) => queryKey[0] === "instances" ? mocks.instancesQuery : mocks.query,
}))
vi.mock("@/hooks/useInstanceMetadata", () => ({ useInstanceMetadata: () => mocks.query }))
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
    createAutomation: mocks.create,
    validateAutomationRegex: mocks.validateRegex,
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

describe("WorkflowDialog category validation", () => {
  it("rejects an unselected category for save and dry run, but accepts Uncategorized", async () => {
    // Radix scrolls focused select options; jsdom has no layout.
    Object.defineProperty(Element.prototype, "scrollIntoView", { configurable: true, value: vi.fn() })
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={client}>
        <TooltipProvider>
          <WorkflowDialog open onOpenChange={() => {}} instanceId={1} rule={rule} />
        </TooltipProvider>
      </QueryClientProvider>
    )

    fireEvent.keyDown(screen.getByText(key("actions.addAction")), { key: "ArrowDown" })
    fireEvent.click(await screen.findByRole("option", { name: key("actions.category") }))

    const enable = screen.getByRole("switch", { name: key("footer.enabled") })
    fireEvent.click(enable)
    expect(enable.getAttribute("aria-checked")).toBe("true")
    expect(mocks.error).not.toHaveBeenCalled()

    for (const control of [
      screen.getByRole("button", { name: key("save") }),
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
        <TooltipProvider>
          <WorkflowDialog open onOpenChange={() => {}} instanceId={1} rule={deleteRule} />
        </TooltipProvider>
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
        <TooltipProvider>
          <WorkflowDialog open onOpenChange={() => {}} instanceId={1} rule={{ ...rule, trackerPattern }} />
        </TooltipProvider>
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

    fireEvent.change(screen.getByLabelText(key("nameLabel")), { target: { value: "Renamed" } })
    fireEvent.click(screen.getByRole("button", { name: key("save") }))
    await waitFor(() => expect(mocks.save).toHaveBeenCalled())
    const payload = mocks.save.mock.calls[0][2]
    expect(payload.trackerPattern).toBe(trackerPattern)
    expect(payload).not.toHaveProperty("trackerDomains")
    client.clear()
  })
})

function renderDialog(dialogRule: Automation) {
  // Radix scrolls focused select options; jsdom has no layout.
  Object.defineProperty(Element.prototype, "scrollIntoView", { configurable: true, value: vi.fn() })
  mocks.instancesQuery.data = []
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

async function optionLabels(trigger: HTMLElement) {
  fireEvent.keyDown(trigger, { key: "ArrowDown" })
  await screen.findAllByRole("option")
  return screen.getAllByRole("option").map(option => option.textContent)
}

// Labels in actionRunOrder, without delete.
const combinableLabels = ["speedLimits", "shareLimits", "pause", "resume", "recheck", "reannounce", "autoManagement", "tag", "category", "move", "externalProgram", "exportToInstance"]
  .map(action => key(`actions.${action}`))

describe("WorkflowDialog action order", () => {
  it("renders the action sections in run order", () => {
    const client = renderDialog({
      ...rule,
      conditions: {
        schemaVersion: "1",
        speedLimits: { enabled: true, uploadKiB: 100 },
        shareLimits: { enabled: true, ratioLimit: 2 },
        pause: { enabled: true },
        resume: { enabled: true },
        recheck: { enabled: true },
        reannounce: { enabled: true },
        autoManagement: { enabled: true },
        tags: [{ enabled: true, tags: ["ordered"], mode: "add" }],
        category: { enabled: true, category: "sorted" },
        move: { enabled: true, path: "/moved" },
        externalProgram: { enabled: true, programId: 1 },
        exportToInstance: { enabled: true, targetInstanceId: 2, savePath: "" },
      },
    })

    const headings = ["actions.speedLimits", "actions.shareLimits", "actions.pause", "actions.resume", "actions.recheck", "actions.reannounce", "actions.autoManagement", "tag.title", "actions.category", "actions.move", "actions.externalProgram", "export.title"]
      .map(suffix => screen.getByText(key(suffix)))
    const inDomOrder = [...headings].sort((a, b) => a.compareDocumentPosition(b) & Node.DOCUMENT_POSITION_FOLLOWING ? -1 : 1)
    expect(inDomOrder.map(heading => heading.textContent)).toEqual(headings.map(heading => heading.textContent))
    client.clear()
  })

  it("lists actions in run order when a rule has none, with delete last", async () => {
    const client = renderDialog({ ...rule, conditions: { schemaVersion: "1" } })

    expect(await optionLabels(screen.getByText(key("actions.selectAction")))).toEqual([...combinableLabels, key("actions.deleteStandalone")])
    client.clear()
  })

  it("offers the remaining actions in run order", async () => {
    const client = renderDialog(rule)

    expect(await optionLabels(screen.getByText(key("actions.addAction")))).toEqual(combinableLabels.filter(label => label !== key("actions.pause")))
    client.clear()
  })

  it("explains the run order on the Actions label", () => {
    // The tooltip opens on tap only where "ontouchstart" exists; jsdom has no touch support.
    const touchWindow = window as Window & { ontouchstart?: unknown }
    touchWindow.ontouchstart = null
    const client = renderDialog(rule)
    try {
      const trigger = within(screen.getByText(key("actions.title"))).getByRole("button", { name: "fieldHelp.trigger" })
      fireEvent.pointerDown(trigger, { pointerType: "touch" })
      fireEvent.pointerUp(trigger, { pointerType: "touch" })
      fireEvent.click(trigger)

      expect(screen.getAllByText(key("actions.runOrderHelp")).length).toBeGreaterThan(0)
    } finally {
      delete touchWindow.ontouchstart
      client.clear()
    }
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

    fireEvent.change(screen.getByLabelText(key("nameLabel")), { target: { value: "Renamed" } })
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

// regression: #3859
describe("WorkflowDialog Save flow", () => {
  const actions: Record<string, Automation["conditions"]> = {
    delete: {
      schemaVersion: "1",
      delete: { enabled: true, mode: "deleteWithFilesPreserveCrossSeeds", condition: { field: "RATIO", operator: "GREATER_THAN", value: "2" } },
    },
    category: { schemaVersion: "1", category: { enabled: true, category: "sorted" } },
    tag: { schemaVersion: "1", tags: [{ enabled: true, tags: ["ordered"], mode: "add" }] },
    pause: { schemaVersion: "1", pause: { enabled: true } },
  }
  const previewActions = ["delete", "category", "tag"]
  const previewConfirm = key("saveRule")
  const saveWithoutPreview = "preferences.workflowPreview.saveWithoutPreview"
  const saveAndEnable = key("enableDryRunPrompt.saveAndEnable")

  beforeEach(() => {
    vi.useFakeTimers()
    Object.defineProperty(Element.prototype, "scrollIntoView", { configurable: true, value: vi.fn() })
    mocks.instancesQuery.data = []
    mocks.preview.mockResolvedValue({ totalMatches: 0, examples: [] })
    mocks.save.mockResolvedValue({})
    mocks.create.mockResolvedValue({})
    mocks.validateRegex.mockResolvedValue({ valid: true, errors: [] })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  // Past the 400 ms debounce of the live preview and the 1 s minimum of the preview dialog.
  const settle = () => act(() => vi.advanceTimersByTimeAsync(1100))

  async function openEditor(dialogRule?: Automation) {
    const onOpenChange = vi.fn()
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={client}>
        <TooltipProvider>
          <WorkflowDialog open onOpenChange={onOpenChange} instanceId={1} rule={dialogRule ?? null} />
        </TooltipProvider>
      </QueryClientProvider>
    )
    await settle()
    mocks.preview.mockClear()
    return onOpenChange
  }

  const enableSwitch = () => screen.getByRole("switch", { name: key("footer.enabled") })
  const dryRunSwitch = () => screen.getByRole("switch", { name: key("footer.dryRun") })
  const nameInput = () => screen.getByLabelText<HTMLInputElement>(key("nameLabel"))
  const rename = (name: string) => fireEvent.change(nameInput(), { target: { value: name } })
  const clickButton = (name: string) => fireEvent.click(screen.getByRole("button", { name }))
  const clickSave = async () => {
    clickButton(key("save"))
    await settle()
  }

  it.each(Object.keys(actions))("flips the Enable switch of a %s rule with no request and no dialog", async (action) => {
    await openEditor({ ...rule, conditions: actions[action] })

    fireEvent.click(enableSwitch())
    await settle()

    expect(enableSwitch().getAttribute("aria-checked")).toBe("true")
    expect(screen.queryByRole("alertdialog")).toBeNull()
    expect(mocks.save).not.toHaveBeenCalled()
    expect(mocks.preview).not.toHaveBeenCalled()
  })

  it.each([
    { action: "delete", enabled: false },
    { action: "category", enabled: false },
    { action: "tag", enabled: false },
    { action: "pause", enabled: false },
    { action: "pause", enabled: true },
  ])("closes the editor of a $action rule with enabled $enabled on Save with no changes, with no checks", async ({ action, enabled }) => {
    const onOpenChange = await openEditor({ ...rule, enabled, conditions: actions[action] })

    await clickSave()

    expect(onOpenChange).toHaveBeenCalledWith(false)
    expect(screen.queryByRole("alertdialog")).toBeNull()
    expect(mocks.validateRegex).not.toHaveBeenCalled()
    expect(mocks.preview).not.toHaveBeenCalled()
    expect(mocks.save).not.toHaveBeenCalled()
  })

  it("reports a missing category when the user adds only that action", async () => {
    const onOpenChange = await openEditor({ ...rule, conditions: actions.pause })

    fireEvent.keyDown(screen.getByText(key("actions.addAction")), { key: "ArrowDown" })
    await settle()
    fireEvent.click(screen.getByRole("option", { name: key("actions.category") }))
    await settle()
    await clickSave()

    expect(mocks.error).toHaveBeenCalledWith(key("toast.selectCategory"))
    expect(onOpenChange).not.toHaveBeenCalled()
  })

  it("reports a blank tag action that the payload leaves out", async () => {
    const onOpenChange = await openEditor({ ...rule, conditions: actions.tag })

    clickButton(key("tag.addAction"))
    await clickSave()

    expect(mocks.error).toHaveBeenCalledWith(key("toast.specifyTagOrTrackerName"))
    expect(onOpenChange).not.toHaveBeenCalled()
  })

  it("does not ask after the check when the user changed the form during the check", async () => {
    let finishCheck: (result: { valid: boolean; errors: never[] }) => void = () => {}
    mocks.validateRegex.mockReturnValueOnce(new Promise((resolve) => { finishCheck = resolve }))
    await openEditor({ ...rule, enabled: true, conditions: actions.pause })

    rename("First name")
    clickButton(key("save"))
    rename("Second name")
    await act(async () => finishCheck({ valid: true, errors: [] }))
    await settle()

    expect(screen.queryByRole("alertdialog")).toBeNull()
    expect(mocks.save).not.toHaveBeenCalled()

    await clickSave()
    expect(mocks.validateRegex).toHaveBeenCalledTimes(2)
    fireEvent.click(within(screen.getByRole("alertdialog")).getByRole("button", { name: key("save") }))
    await settle()
    expect(mocks.save).toHaveBeenCalledTimes(1)
    expect(mocks.save.mock.calls[0][2]).toMatchObject({ name: "Second name" })
  })

  it.each([
    { name: "stays closed", reopen: false },
    { name: "opens again", reopen: true },
  ])("asks nothing after the check when the editor closes during the check and $name", async ({ reopen }) => {
    let finishCheck: (result: { valid: boolean; errors: never[] }) => void = () => {}
    mocks.validateRegex.mockReturnValueOnce(new Promise((resolve) => { finishCheck = resolve }))
    const dialogRule = { ...rule, enabled: true, conditions: actions.pause }
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const editor = (open: boolean, editorRule = dialogRule) => (
      <QueryClientProvider client={client}>
        <TooltipProvider>
          <WorkflowDialog open={open} onOpenChange={vi.fn()} instanceId={1} rule={editorRule} />
        </TooltipProvider>
      </QueryClientProvider>
    )
    const { rerender } = render(editor(true))
    await settle()

    rename("Changed")
    clickButton(key("save"))
    rerender(editor(false))
    await settle()
    if (reopen) {
      rerender(editor(true, { ...dialogRule, name: "Changed" }))
      await settle()
    }
    await act(async () => finishCheck({ valid: true, errors: [] }))
    await settle()

    expect(screen.queryByRole("alertdialog")).toBeNull()
    expect(mocks.save).not.toHaveBeenCalled()
  })

  it("enables Save again when the user cancels a preview that still loads", async () => {
    mocks.preview.mockReturnValue(new Promise(() => {}))
    await openEditor({ ...rule, enabled: true, conditions: actions.tag })
    rename("Changed")
    clickButton(key("save"))
    await settle()

    fireEvent.click(within(screen.getByRole("alertdialog")).getByRole("button", { name: "preferences.workflowPreview.cancel" }))
    await settle()

    expect(screen.getByRole<HTMLButtonElement>("button", { name: key("save") }).disabled).toBe(false)
  })

  it("runs only the last check when the user clicks Save twice", async () => {
    const finishChecks: ((result: { valid: boolean; errors: never[] }) => void)[] = []
    mocks.validateRegex.mockImplementation(() => new Promise((resolve) => { finishChecks.push(resolve) }))
    await openEditor({ ...rule, enabled: true, conditions: actions.tag })

    rename("Changed")
    clickButton(key("save"))
    clickButton(key("save"))
    await act(async () => finishChecks[0]({ valid: true, errors: [] }))
    await settle()

    expect(screen.queryByRole("alertdialog")).toBeNull()

    await act(async () => finishChecks[1]({ valid: true, errors: [] }))
    await settle()

    expect(screen.queryByRole("alertdialog")).not.toBeNull()
  })

  it("counts a flip of the switch and a flip back as no change", async () => {
    const onOpenChange = await openEditor({ ...rule, conditions: actions.tag })

    fireEvent.click(enableSwitch())
    fireEvent.click(enableSwitch())
    await clickSave()

    expect(onOpenChange).toHaveBeenCalledWith(false)
    expect(screen.queryByRole("alertdialog")).toBeNull()
    expect(mocks.save).not.toHaveBeenCalled()
  })

  it.each(["tag", "pause"])("saves a changed %s rule with Enabled off and no dialog", async (action) => {
    const onOpenChange = await openEditor({ ...rule, conditions: actions[action] })

    rename("Renamed")
    await clickSave()

    expect(screen.queryByRole("alertdialog")).toBeNull()
    expect(mocks.validateRegex).not.toHaveBeenCalled()
    expect(mocks.save).toHaveBeenCalledWith(1, 1, expect.objectContaining({ name: "Renamed", enabled: false }))
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })

  it("saves with no question when the user only turns a live rule off", async () => {
    await openEditor({ ...rule, enabled: true, conditions: actions.tag })

    fireEvent.click(enableSwitch())
    await clickSave()

    expect(screen.queryByRole("alertdialog")).toBeNull()
    expect(mocks.save).toHaveBeenCalledWith(1, 1, expect.objectContaining({ enabled: false }))
  })

  it.each(previewActions)("opens the preview on Save of an enabled %s rule", async (action) => {
    await openEditor({ ...rule, conditions: actions[action] })

    fireEvent.click(enableSwitch())
    await clickSave()

    expect(mocks.validateRegex).toHaveBeenCalled()
    expect(mocks.preview).toHaveBeenCalledTimes(1)
    expect(screen.getByRole("button", { name: previewConfirm })).not.toBeNull()
    expect(screen.queryByText(key("enableDryRunPrompt.title"))).toBeNull()
    expect(mocks.save).not.toHaveBeenCalled()
  })

  it("asks about dry run on Save of an enabled rule with other actions", async () => {
    await openEditor({ ...rule, conditions: actions.pause })

    fireEvent.click(enableSwitch())
    await clickSave()

    expect(mocks.validateRegex).toHaveBeenCalled()
    expect(screen.getByText(key("enableDryRunPrompt.title"))).not.toBeNull()
    expect(mocks.preview).not.toHaveBeenCalled()
    expect(mocks.save).not.toHaveBeenCalled()
  })

  it("opens no dialog when the regex check fails", async () => {
    mocks.validateRegex.mockResolvedValue({ valid: false, errors: [{ path: "x", message: "bad", pattern: "(" }] })
    await openEditor({ ...rule, conditions: actions.pause })

    fireEvent.click(enableSwitch())
    await clickSave()

    expect(screen.queryByRole("alertdialog")).toBeNull()
    expect(mocks.error).toHaveBeenCalledWith(key("toast.invalidRegexPatternUnsupported"))
    expect(mocks.save).not.toHaveBeenCalled()
  })

  it.each([
    { name: "the dry-run question", action: "pause", cancel: key("cancel") },
    { name: "the preview", action: "tag", cancel: "preferences.workflowPreview.cancel" },
  ])("keeps the switch and the edits when the user cancels $name", async ({ action, cancel }) => {
    const onOpenChange = await openEditor({ ...rule, conditions: actions[action] })

    rename("Renamed")
    fireEvent.click(enableSwitch())
    await clickSave()
    clickButton(cancel)
    await settle()

    expect(screen.queryByRole("alertdialog")).toBeNull()
    expect(enableSwitch().getAttribute("aria-checked")).toBe("true")
    expect(nameInput().value).toBe("Renamed")
    expect(onOpenChange).not.toHaveBeenCalled()
    expect(mocks.save).not.toHaveBeenCalled()
  })

  it.each([
    { name: "the dry-run question", action: "pause", confirm: saveAndEnable },
    { name: "the preview", action: "tag", confirm: previewConfirm },
  ])("sends the toggle value of $name to the save request", async ({ action, confirm }) => {
    await openEditor({ ...rule, dryRun: false, conditions: actions[action] })

    fireEvent.click(enableSwitch())
    await clickSave()
    expect(dryRunSwitch().getAttribute("aria-checked")).toBe("false")
    fireEvent.click(dryRunSwitch())
    expect(dryRunSwitch().getAttribute("aria-checked")).toBe("true")
    clickButton(confirm)
    await settle()

    expect(mocks.save).toHaveBeenCalledWith(1, 1, expect.objectContaining({ enabled: true, dryRun: true }))
  })

  it("lets the user set dry run while the preview loads, and sends no second preview", async () => {
    await openEditor({ ...rule, dryRun: false, conditions: actions.tag })
    mocks.preview.mockReturnValue(new Promise(() => {}))

    fireEvent.click(enableSwitch())
    clickButton(key("save"))
    await act(() => vi.advanceTimersByTimeAsync(0))
    fireEvent.click(dryRunSwitch())
    await settle()
    expect(mocks.preview).toHaveBeenCalledTimes(1)
    clickButton(saveWithoutPreview)
    await settle()

    expect(mocks.save).toHaveBeenCalledWith(1, 1, expect.objectContaining({ enabled: true, dryRun: true }))
  })

  it("keeps the toggle value in the editor after Cancel", async () => {
    await openEditor({ ...rule, dryRun: false, conditions: actions.pause })

    fireEvent.click(enableSwitch())
    await clickSave()
    fireEvent.click(dryRunSwitch())
    clickButton(key("cancel"))
    await settle()

    expect(dryRunSwitch().getAttribute("aria-checked")).toBe("true")
  })

  it.each([
    { name: "a rule that turns on", enabled: false, label: saveAndEnable },
    { name: "a rule that is already enabled", enabled: true, label: key("save") },
  ])("names the save button for $name", async ({ enabled, label }) => {
    await openEditor({ ...rule, enabled, conditions: actions.pause })

    rename("Renamed")
    if (!enabled) fireEvent.click(enableSwitch())
    await clickSave()

    expect(within(screen.getByRole("alertdialog")).getByRole("button", { name: label })).not.toBeNull()
  })

  it("asks a new rule the same question, with the label for a rule that turns on", async () => {
    await openEditor()

    rename("New rule")
    fireEvent.click(screen.getByRole("switch", { name: key("allTrackers") }))
    fireEvent.keyDown(screen.getByText(key("actions.selectAction")), { key: "ArrowDown" })
    await settle()
    fireEvent.click(screen.getByRole("option", { name: key("actions.pause") }))
    await settle()
    fireEvent.click(enableSwitch())
    clickButton(key("create"))
    await settle()

    expect(mocks.create).not.toHaveBeenCalled()
    clickButton(saveAndEnable)
    await settle()

    expect(mocks.create).toHaveBeenCalledWith(1, expect.objectContaining({ name: "New rule", enabled: true }))
  })

  it("asks again on a second save, also after a reload of the dialog", async () => {
    const enabledRule = { ...rule, enabled: true, conditions: actions.pause }
    for (const name of ["First", "Second"]) {
      await openEditor(enabledRule)
      rename(name)
      await clickSave()

      expect(screen.getByText(key("enableDryRunPrompt.title"))).not.toBeNull()
      cleanup()
    }
  })
})
