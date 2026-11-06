/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import "@/i18n"
import type { ApiKey, ApiKeyRequest } from "@/types"

const stubs = vi.hoisted(() => ({
  keys: [] as ApiKey[],
  instances: [] as { id: number; name: string }[],
  created: [] as unknown[],
  updated: [] as unknown[],
  updateGate: null as Promise<void> | null,
}))

vi.mock("@/lib/api", () => ({
  APIError: class extends Error {},
  api: {
    getApiKeys: async () => stubs.keys,
    getInstances: async () => stubs.instances,
    createApiKey: async (req: ApiKeyRequest) => {
      stubs.created.push(req)
      return { id: 9, key: "the-raw-key", name: req.name }
    },
    updateApiKey: async (id: number, req: ApiKeyRequest) => {
      stubs.updated.push(req)
      await stubs.updateGate
      stubs.keys = stubs.keys.map(key => key.id === id ? { ...key, ...req } : key)
      return stubs.keys.find(key => key.id === id)
    },
  },
}))

// Radix Checkbox sizes its hidden form input through ResizeObserver, which jsdom lacks.
vi.stubGlobal("ResizeObserver", class {
  observe() {}
  unobserve() {}
  disconnect() {}
})

import { TooltipProvider } from "@/components/ui/tooltip"
import { ApiKeysManager } from "./ApiKeysManager"

const key: ApiKey = { id: 1, name: "dashboard", createdAt: "2026-01-01T00:00:00Z", permissions: [], instanceIds: [] }

function renderManager() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <ApiKeysManager authModeLoading={false} />
      </TooltipProvider>
    </QueryClientProvider>
  )
  return queryClient
}

async function openCreateDialog() {
  const user = userEvent.setup()
  renderManager()
  await user.click(screen.getByRole("button", { name: "Create API Key" }))
  return { user, dialog: await screen.findByRole("dialog") }
}

const box = (dialog: HTMLElement, name: string) => within(dialog).queryByRole("checkbox", { name })
const keyRow = async (name: string) => (await screen.findByText(name)).closest<HTMLElement>("[data-testid='api-key-row']")!
const isChecked = (el: HTMLElement | null) => el?.getAttribute("aria-checked") === "true"
const isDisabled = (el: HTMLElement | null) => el?.hasAttribute("disabled")

beforeEach(() => {
  stubs.keys = []
  stubs.instances = [{ id: 1, name: "seedbox" }, { id: 2, name: "archive" }]
  stubs.created = []
  stubs.updated = []
  stubs.updateGate = null
})

afterEach(cleanup)

describe("ApiKeysManager create dialog", () => {
  // regression: #3858
  it("shows a picked instance that another session deleted, so the user can untick it", async () => {
    const user = userEvent.setup()
    const queryClient = renderManager()
    await user.click(screen.getByRole("button", { name: "Create API Key" }))
    const dialog = await screen.findByRole("dialog")
    await user.click(await within(dialog).findByRole("checkbox", { name: "seedbox" }))

    stubs.instances = [{ id: 2, name: "archive" }]
    await queryClient.invalidateQueries({ queryKey: ["instances"] })

    const gone = await within(dialog).findByRole("checkbox", { name: "Deleted instance 1" })
    expect(isChecked(gone)).toBe(true)
    await user.click(gone)
    expect(within(dialog).queryByRole("checkbox", { name: "Deleted instance 1" })).toBeNull()
  })

  it("offers only the levels that each area allows", async () => {
    const { dialog } = await openCreateDialog()

    const offered: Record<string, string[]> = {
      Instances: ["read", "write"],
      Torrents: ["read", "write"],
      "Cross-seed": ["read", "run", "write"],
      "Dir Scan": ["read", "run", "write"],
      Automations: ["read", "run", "write"],
      RSS: ["read", "run", "write"],
      Backups: ["read", "run", "write"],
      "Orphan scan": ["read", "run", "write"],
      "External programs": ["read", "run"],
      Indexers: ["read", "write"],
      Arr: ["read", "write"],
      Notifications: ["read", "write"],
      Settings: ["read", "write"],
      Logs: ["read", "write"],
      System: ["read"],
    }
    for (const [area, levels] of Object.entries(offered)) {
      for (const level of ["read", "run", "write"]) {
        expect(box(dialog, `${area}: ${level}`) !== null, `${area}: ${level}`).toBe(levels.includes(level))
      }
    }
  })

  it("keeps save disabled until the key has a name and a Permission", async () => {
    const { user, dialog } = await openCreateDialog()
    const save = within(dialog).getByRole("button", { name: "Create API Key" })

    await user.type(within(dialog).getByLabelText("Name"), "dashboard")
    expect(isDisabled(save)).toBe(true)

    await user.click(box(dialog, "Torrents: read")!)
    expect(isDisabled(save)).toBe(false)

    await user.click(box(dialog, "Torrents: read")!)
    expect(isDisabled(save)).toBe(true)
  })

  it("treats write as read and run, and run as neither", async () => {
    const { user, dialog } = await openCreateDialog()

    await user.click(box(dialog, "Cross-seed: run")!)
    expect(isChecked(box(dialog, "Cross-seed: read"))).toBe(false)
    expect(isDisabled(box(dialog, "Cross-seed: read"))).toBe(false)

    await user.click(box(dialog, "Automations: write")!)
    for (const included of ["Automations: read", "Automations: run"]) {
      expect(isChecked(box(dialog, included))).toBe(true)
      expect(isDisabled(box(dialog, included))).toBe(true)
    }

    await user.type(within(dialog).getByLabelText("Name"), "k")
    await user.click(within(dialog).getByRole("button", { name: "Create API Key" }))
    await screen.findByText("the-raw-key")
    expect(stubs.created).toEqual([{
      name: "k",
      permissions: [{ area: "crossSeed", level: "run" }, { area: "automations", level: "write" }],
      instanceIds: [],
    }])
  })

  it("greys out Cross-seed and Dir Scan while the key has an Instance limit, and does not save them", async () => {
    const { user, dialog } = await openCreateDialog()
    const save = within(dialog).getByRole("button", { name: "Create API Key" })
    await user.type(within(dialog).getByLabelText("Name"), "k")

    await user.click(box(dialog, "Cross-seed: run")!)
    expect(isDisabled(save)).toBe(false)

    const seedbox = await within(dialog).findByRole("checkbox", { name: "seedbox" })
    await user.click(seedbox)
    for (const level of ["read", "run", "write"]) {
      for (const area of ["Cross-seed", "Dir Scan"]) {
        expect(isDisabled(box(dialog, `${area}: ${level}`)), `${area}: ${level}`).toBe(true)
        expect(isChecked(box(dialog, `${area}: ${level}`)), `${area}: ${level}`).toBe(false)
      }
    }
    expect(isDisabled(box(dialog, "Torrents: read"))).toBe(false)
    expect(isDisabled(save), "a key whose only Permission is greyed out has no Permission").toBe(true)

    await user.click(seedbox)
    expect(isChecked(box(dialog, "Cross-seed: run")), "the pick comes back with the limit gone").toBe(true)

    await user.click(seedbox)
    await user.click(box(dialog, "Torrents: read")!)
    await user.click(save)
    await screen.findByText("the-raw-key")
    expect(stubs.created).toEqual([{ name: "k", permissions: [{ area: "torrents", level: "read" }], instanceIds: [1] }])
  })
})

describe("ApiKeysManager key list", () => {
  it("shows the Permissions and the Instance limit of each key", async () => {
    stubs.keys = [
      { ...key, id: 1, name: "open", permissions: [{ area: "torrents", level: "read" }] },
      { ...key, id: 2, name: "limited", permissions: [{ area: "logs", level: "write" }], instanceIds: [2, 99] },
      { ...key, id: 3, name: "gone", permissions: [{ area: "system", level: "read" }], instanceIds: [99] },
    ]
    renderManager()

    expect((await keyRow("open")).textContent).toContain("Torrents: read")
    await waitFor(async () => expect((await keyRow("open")).textContent).toContain("Instance limit: All instances"))
    expect((await keyRow("limited")).textContent).toContain("Logs: write")
    expect((await keyRow("limited")).textContent).toContain("Instance limit: archive")
    expect((await keyRow("gone")).textContent).toContain("Instance limit: no instances")
  })

  it("does not call a limit empty before the instances load", async () => {
    stubs.keys = [{ ...key, permissions: [{ area: "system", level: "read" }], instanceIds: [1] }]
    stubs.instances = new Promise(() => {}) as never
    renderManager()

    const row = await keyRow("dashboard")
    expect(row.textContent).not.toContain("no instances")
  })

  // regression: #3858
  it("names the delete button after the key", async () => {
    stubs.keys = [key]
    renderManager()
    await keyRow("dashboard")
    expect(screen.getByRole("button", { name: "Delete dashboard" })).toBeTruthy()
  })
})

describe("ApiKeysManager edit dialog", () => {
  // regression: #3858
  it("keeps the dialog of another key open when an earlier save finishes", async () => {
    stubs.keys = [
      { ...key, permissions: [{ area: "logs", level: "read" }] },
      { ...key, id: 2, name: "wallboard", permissions: [{ area: "logs", level: "read" }] },
    ]
    let finishSave = () => {}
    stubs.updateGate = new Promise<void>((resolve) => { finishSave = resolve })
    const user = userEvent.setup()
    const queryClient = renderManager()

    await user.click(await screen.findByRole("button", { name: "Edit dashboard" }))
    await user.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Save changes" }))
    await waitFor(() => expect(stubs.updated).toHaveLength(1))
    await user.keyboard("{Escape}")
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())

    await user.click(screen.getByRole("button", { name: "Edit wallboard" }))
    const second = await screen.findByRole("dialog")
    expect((within(second).getByLabelText("Name") as HTMLInputElement).value).toBe("wallboard")

    finishSave()
    await waitFor(() => expect(queryClient.isMutating()).toBe(0))
    expect(screen.queryByRole("dialog")).not.toBeNull()
  })

  async function openEditDialog() {
    const user = userEvent.setup()
    renderManager()
    await user.click(await screen.findByRole("button", { name: "Edit dashboard" }))
    return { user, dialog: await screen.findByRole("dialog") }
  }

  it("changes the name, the Permissions, and the Instance limit, and the list shows the result", async () => {
    stubs.keys = [{ ...key, permissions: [{ area: "torrents", level: "read" }], instanceIds: [1] }]
    const { user, dialog } = await openEditDialog()
    const name = within(dialog).getByLabelText("Name")
    expect((name as HTMLInputElement).value).toBe("dashboard")
    expect(isChecked(box(dialog, "Torrents: read"))).toBe(true)
    expect(isChecked(await within(dialog).findByRole("checkbox", { name: "seedbox" }))).toBe(true)

    await user.clear(name)
    await user.type(name, "wallboard")
    await user.click(box(dialog, "Torrents: read")!)
    await user.click(box(dialog, "Logs: read")!)
    await user.click(within(dialog).getByRole("checkbox", { name: "seedbox" }))
    await user.click(within(dialog).getByRole("checkbox", { name: "archive" }))
    await user.click(within(dialog).getByRole("button", { name: "Save changes" }))

    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
    expect(stubs.updated).toEqual([{ name: "wallboard", permissions: [{ area: "logs", level: "read" }], instanceIds: [2] }])
    const row = await keyRow("wallboard")
    await waitFor(() => expect(row.textContent).toContain("Logs: read"))
    expect(row.textContent).toContain("Instance limit: archive")
    expect(row.textContent).not.toContain("the-raw-key")
  })

  it("keeps a deleted instance in the limit, so an edit does not open the key to all instances", async () => {
    stubs.keys = [{ ...key, permissions: [{ area: "torrents", level: "read" }], instanceIds: [99] }]
    const { user, dialog } = await openEditDialog()

    expect(isChecked(await within(dialog).findByRole("checkbox", { name: "Deleted instance 99" }))).toBe(true)
    expect(isDisabled(box(dialog, "Cross-seed: read"))).toBe(true)

    await user.click(within(dialog).getByRole("button", { name: "Save changes" }))
    await waitFor(() => expect(stubs.updated).toEqual([{
      name: "dashboard",
      permissions: [{ area: "torrents", level: "read" }],
      instanceIds: [99],
    }]))
  })
})
