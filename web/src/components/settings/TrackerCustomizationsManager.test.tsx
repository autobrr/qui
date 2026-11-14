/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, fireEvent, render, renderHook, screen, waitFor } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

import type { TrackerCustomization } from "@/types"

const mocks = vi.hoisted(() => ({
  instances: [] as { id: number; isActive: boolean }[],
  activeTrackers: {} as Record<string, string>,
  customizations: [] as TrackerCustomization[],
  listTrackerCustomizations: vi.fn(),
  createTrackerCustomization: vi.fn(),
  updateTrackerCustomization: vi.fn(),
  deleteTrackerCustomization: vi.fn(),
}))

vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => ({ t: (key: string) => key }),
}))
vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn(), info: vi.fn() } }))
vi.mock("@/hooks/useInstances", () => ({ useInstances: () => ({ instances: mocks.instances }) }))
vi.mock("@/hooks/useTrackerIcons", () => ({ useTrackerIcons: () => ({ data: {} }) }))
vi.mock("@/lib/incognito", () => ({ useIncognitoMode: () => [false], getLinuxTrackerDomain: (d: string) => d }))
vi.mock("@/lib/api", () => ({
  api: {
    getActiveTrackers: () => Promise.resolve(mocks.activeTrackers),
    listTrackerCustomizations: mocks.listTrackerCustomizations,
    createTrackerCustomization: mocks.createTrackerCustomization,
    updateTrackerCustomization: mocks.updateTrackerCustomization,
    deleteTrackerCustomization: mocks.deleteTrackerCustomization,
  },
}))

import { TooltipProvider } from "@/components/ui/tooltip"
import { useTrackerCustomizations, useUpdateTrackerCustomization } from "@/hooks/useTrackerCustomizations"
import { TrackerCustomizationsManager } from "./TrackerCustomizationsManager"

function customization(id: number, displayName: string, domains: string[]): TrackerCustomization {
  return { id, displayName, domains, createdAt: "", updatedAt: "" }
}

function newClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } })
}

function renderManager() {
  // Radix measures the checkbox and the tooltip through ResizeObserver, which jsdom lacks.
  vi.stubGlobal("ResizeObserver", class {
    observe() {}
    unobserve() {}
    disconnect() {}
  })
  const client = newClient()
  render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <TrackerCustomizationsManager />
      </TooltipProvider>
    </QueryClientProvider>
  )
}

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
  mocks.instances = []
  mocks.activeTrackers = {}
  vi.unstubAllGlobals()
})

describe("TrackerCustomizationsManager", () => {
  // regression: #4108
  it("clears the group selection when the selected customization is deleted", async () => {
    mocks.customizations = [customization(1, "Alpha", ["a.example.invalid"]), customization(2, "Bravo", ["b.example.invalid"])]
    mocks.listTrackerCustomizations.mockImplementation(() => Promise.resolve(mocks.customizations))
    mocks.deleteTrackerCustomization.mockImplementation((id: number) => {
      mocks.customizations = mocks.customizations.filter(c => c.id !== id)
      return Promise.resolve()
    })
    renderManager()

    fireEvent.click(await screen.findByRole("checkbox", { name: "Alpha" }))
    expect(screen.queryByRole("checkbox", { name: "Bravo" })).toBeNull()

    fireEvent.click(screen.getAllByRole("button", { name: "trackerCustomizations.delete" })[0])

    await waitFor(() => expect(screen.queryByText("Alpha")).toBeNull())
    expect(screen.queryByRole("checkbox", { name: "Bravo" })).not.toBeNull()
  })

  // regression: #4108
  it("asks again about a conflict after the pasted text changes", async () => {
    mocks.customizations = [customization(1, "Alpha", ["a.example.invalid"]), customization(2, "Bravo", ["b.example.invalid"])]
    mocks.listTrackerCustomizations.mockImplementation(() => Promise.resolve(mocks.customizations))
    renderManager()
    await screen.findByText("Alpha")

    fireEvent.click(screen.getByRole("button", { name: "trackerCustomizations.importTooltip" }))
    const input = await screen.findByLabelText("trackerCustomizations.importDialog.jsonData")
    const importButton = () => screen.getByRole("button", { name: "trackerCustomizations.importDialog.import" })

    fireEvent.change(input, { target: { value: JSON.stringify({ trackerCustomizations: [{ displayName: "New", domains: ["a.example.invalid"] }] }) } })
    fireEvent.click(screen.getByRole("button", { name: "trackerCustomizations.importDialog.overwrite" }))
    expect(importButton().hasAttribute("disabled")).toBe(false)

    fireEvent.change(input, { target: { value: JSON.stringify({ trackerCustomizations: [{ displayName: "Other", domains: ["b.example.invalid"] }] }) } })
    expect(importButton().hasAttribute("disabled")).toBe(true)
  })

  // regression: #4108
  it("does not reopen the import dialog while an import still runs", async () => {
    mocks.customizations = []
    mocks.listTrackerCustomizations.mockImplementation(() => Promise.resolve(mocks.customizations))
    mocks.createTrackerCustomization.mockImplementation(() => new Promise(() => {}))
    renderManager()
    await screen.findByText("trackerCustomizations.empty")
    const text = JSON.stringify({ trackerCustomizations: [{ displayName: "New", domains: ["n.example.invalid"] }] })
    fireEvent.click(screen.getByRole("button", { name: "trackerCustomizations.importTooltip" }))
    fireEvent.change(await screen.findByLabelText("trackerCustomizations.importDialog.jsonData"), { target: { value: text } })
    fireEvent.click(screen.getByRole("button", { name: "trackerCustomizations.importDialog.import" }))
    await waitFor(() => expect(mocks.createTrackerCustomization).toHaveBeenCalledTimes(1))
    fireEvent.click(screen.getByRole("button", { name: "trackerCustomizations.importDialog.cancel" }))
    await waitFor(() => expect(screen.queryByLabelText("trackerCustomizations.importDialog.jsonData")).toBeNull())

    expect(screen.getByRole("button", { name: "trackerCustomizations.importTooltip" }).hasAttribute("disabled")).toBe(true)
  })

  // regression: #4108
  it("drops a domain from the selection when an import gives it a customization", async () => {
    mocks.deleteTrackerCustomization.mockImplementation((id: number) => {
      mocks.customizations = mocks.customizations.filter(c => c.id !== id)
      return Promise.resolve()
    })
    mocks.instances = [{ id: 1, isActive: true }]
    mocks.activeTrackers = { "a.example.invalid": "https://a.example.invalid/announce", "b.example.invalid": "https://b.example.invalid/announce" }
    mocks.customizations = []
    mocks.listTrackerCustomizations.mockImplementation(() => Promise.resolve(mocks.customizations))
    mocks.createTrackerCustomization.mockImplementation((data: { displayName: string; domains: string[] }) => {
      const created = customization(9, data.displayName, data.domains)
      mocks.customizations = [created]
      return Promise.resolve(created)
    })
    renderManager()

    fireEvent.click(await screen.findByRole("checkbox", { name: "a.example.invalid" }))
    fireEvent.click(screen.getByRole("button", { name: "trackerCustomizations.importTooltip" }))
    fireEvent.change(await screen.findByLabelText("trackerCustomizations.importDialog.jsonData"), {
      target: { value: JSON.stringify({ trackerCustomizations: [{ displayName: "Alpha", domains: ["a.example.invalid"] }] }) },
    })
    fireEvent.click(screen.getByRole("button", { name: "trackerCustomizations.importDialog.import" }))

    await screen.findByText("Alpha")
    expect(screen.queryByRole("button", { name: "trackerCustomizations.addToMerge" })).toBeNull()
    expect(screen.getByRole("button", { name: "trackerCustomizations.rename" })).not.toBeNull()

    // A delete of the imported customization gives the domain its row back, unselected.
    fireEvent.click(screen.getByRole("button", { name: "trackerCustomizations.delete" }))
    await waitFor(() => expect(screen.getAllByRole("button", { name: "trackerCustomizations.rename" })).toHaveLength(2))
    expect(screen.queryByRole("button", { name: "trackerCustomizations.addToMerge" })).toBeNull()
  })

  // regression: #4108
  it("keeps the selection of a rename that opens while a cancelled import still runs", async () => {
    mocks.instances = [{ id: 1, isActive: true }]
    mocks.activeTrackers = { "a.example.invalid": "https://a.example.invalid/announce", "b.example.invalid": "https://b.example.invalid/announce" }
    mocks.customizations = []
    mocks.listTrackerCustomizations.mockImplementation(() => Promise.resolve(mocks.customizations))
    let finishImport = () => {}
    mocks.createTrackerCustomization
      .mockImplementationOnce((data: { displayName: string; domains: string[] }) => new Promise(resolve => {
        finishImport = () => resolve(customization(9, data.displayName, data.domains))
      }))
      .mockImplementation((data: { displayName: string; domains: string[] }) => Promise.resolve(customization(10, data.displayName, data.domains)))
    renderManager()
    await screen.findByText("b.example.invalid")

    fireEvent.click(screen.getByRole("button", { name: "trackerCustomizations.importTooltip" }))
    fireEvent.change(await screen.findByLabelText("trackerCustomizations.importDialog.jsonData"), {
      target: { value: JSON.stringify({ trackerCustomizations: [{ displayName: "Other", domains: ["x.example.invalid"] }] }) },
    })
    fireEvent.click(screen.getByRole("button", { name: "trackerCustomizations.importDialog.import" }))
    fireEvent.click(screen.getByRole("button", { name: "trackerCustomizations.importDialog.cancel" }))
    await waitFor(() => expect(screen.queryByLabelText("trackerCustomizations.importDialog.jsonData")).toBeNull())

    fireEvent.click(screen.getAllByRole("button", { name: "trackerCustomizations.rename" })[1])
    fireEvent.change(await screen.findByLabelText("trackerCustomizations.customizeDialog.displayName"), { target: { value: "Bravo" } })
    await act(async () => finishImport())

    fireEvent.click(await screen.findByRole("button", { name: "trackerCustomizations.customizeDialog.renameAction" }))
    await waitFor(() => expect(mocks.createTrackerCustomization).toHaveBeenLastCalledWith({ displayName: "Bravo", domains: ["b.example.invalid"] }))
  })
})

describe("useUpdateTrackerCustomization", () => {
  // regression: #4108
  it("puts the saved customization in the cache before the refetch answers", async () => {
    const client = newClient()
    const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>
    const saved = customization(1, "Alpha", ["a.example.invalid", "c.example.invalid"])
    mocks.listTrackerCustomizations
      .mockResolvedValueOnce([customization(1, "Alpha", ["a.example.invalid"])])
      .mockImplementation(() => new Promise(() => {}))
    mocks.updateTrackerCustomization.mockResolvedValue(saved)

    const { result } = renderHook(() => ({ list: useTrackerCustomizations(), update: useUpdateTrackerCustomization() }), { wrapper })
    await waitFor(() => expect(result.current.list.data).toHaveLength(1))

    await act(() => result.current.update.mutateAsync({ id: 1, data: { displayName: "Alpha", domains: saved.domains } }))

    expect(client.getQueryData<TrackerCustomization[]>(["tracker-customizations"])?.[0].domains).toEqual(saved.domains)
  })
})
