/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import type { Automation } from "@/types"

const mocks = vi.hoisted(() => ({
  updateAutomation: vi.fn(),
  toastSuccess: vi.fn(),
}))

vi.mock("react-i18next", async (importOriginal) => ({
  ...(await importOriginal<typeof import("react-i18next")>()),
  useTranslation: () => ({ t: (key: string) => key, i18n: { t: (key: string) => key } }),
}))
vi.mock("sonner", () => ({ toast: { success: mocks.toastSuccess } }))
vi.mock("@/lib/api", () => ({ api: { updateAutomation: mocks.updateAutomation } }))
vi.mock("@/components/ui/json-editor", () => ({
  JsonEditor: ({ value, onChange }: { value: string; onChange: (v: string) => void }) => (
    <textarea value={value} onChange={(e) => onChange(e.target.value)} />
  ),
}))

import { WorkflowJsonEditDialog } from "@/components/instances/preferences/WorkflowJsonEditDialog"

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

const rule: Automation = {
  id: 7,
  instanceId: 1,
  name: "Old name",
  trackerPattern: "a.com",
  trackerDomains: ["a.com"],
  conditions: { schemaVersion: "1" },
  enabled: true,
  dryRun: false,
  notify: true,
  sortOrder: 4,
  intervalSeconds: 120,
}

function renderDialog(onOpenChange = vi.fn()) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  render(
    <QueryClientProvider client={client}>
      <WorkflowJsonEditDialog rule={rule} onOpenChange={onOpenChange} />
    </QueryClientProvider>
  )
  return { editor: screen.getByRole("textbox") as HTMLTextAreaElement, onOpenChange }
}

describe("WorkflowJsonEditDialog", () => {
  it("opens with the export-shape JSON of the rule", () => {
    const { editor } = renderDialog()
    const parsed = JSON.parse(editor.value)
    expect(parsed).toEqual({
      name: "Old name",
      trackerPattern: "a.com",
      trackerDomains: ["a.com"],
      conditions: { schemaVersion: "1" },
      intervalSeconds: 120,
    })
    expect(parsed).not.toHaveProperty("id")
    expect(parsed).not.toHaveProperty("instanceId")
    expect(parsed).not.toHaveProperty("enabled")
    expect(parsed).not.toHaveProperty("sortOrder")
  })

  it("blocks the save on invalid JSON and keeps the text", () => {
    const { editor, onOpenChange } = renderDialog()
    fireEvent.change(editor, { target: { value: "{not json" } })
    fireEvent.click(screen.getByText("preferences.workflowsOverview.editJsonDialog.save"))

    expect(mocks.updateAutomation).not.toHaveBeenCalled()
    expect(screen.getByText("preferences.workflowsOverview.importDialog.errors.invalidJson")).toBeTruthy()
    expect(onOpenChange).not.toHaveBeenCalled()
    expect(editor.value).toBe("{not json")
  })

  it("sends the parsed fields over the rule's enabled state and sort order", async () => {
    mocks.updateAutomation.mockResolvedValue({ ...rule, name: "New name" })
    const { editor, onOpenChange } = renderDialog()
    fireEvent.change(editor, {
      target: { value: JSON.stringify({ name: "New name", trackerDomains: ["b.com"], conditions: { schemaVersion: "1" }, dryRun: true }) },
    })
    fireEvent.click(screen.getByText("preferences.workflowsOverview.editJsonDialog.save"))

    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false))
    expect(mocks.updateAutomation).toHaveBeenCalledWith(1, 7, {
      name: "New name",
      enabled: true,
      sortOrder: 4,
      trackerPattern: "b.com",
      trackerDomains: ["b.com"],
      conditions: { schemaVersion: "1" },
      freeSpaceSource: undefined,
      sortingConfig: undefined,
      dryRun: true,
      notify: true,
    })
    expect(mocks.toastSuccess).toHaveBeenCalled()
  })

  it("keeps the dialog open with the text when the backend rejects the save", async () => {
    mocks.updateAutomation.mockRejectedValue(new Error("rule name already exists"))
    const { editor, onOpenChange } = renderDialog()
    const text = JSON.stringify({ name: "Dup", trackerDomains: [], conditions: { schemaVersion: "1" } })
    fireEvent.change(editor, { target: { value: text } })
    fireEvent.click(screen.getByText("preferences.workflowsOverview.editJsonDialog.save"))

    await waitFor(() => expect(screen.getByText("rule name already exists")).toBeTruthy())
    expect(onOpenChange).not.toHaveBeenCalled()
    expect(editor.value).toBe(text)
  })
})
