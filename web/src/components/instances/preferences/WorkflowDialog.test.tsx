/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { afterEach, describe, expect, it, vi } from "vitest"
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import type { Automation } from "@/types"

const mocks = vi.hoisted(() => {
  const t = (key: string) => key
  return {
    query: { data: undefined },
    translation: { t, i18n: { t, language: "en" } },
    error: vi.fn(),
    preview: vi.fn(),
    save: vi.fn(),
    dryRun: vi.fn().mockResolvedValue({ activities: [] }),
  }
})

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...await importOriginal<typeof import("@tanstack/react-query")>(),
  useQuery: () => mocks.query,
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
    dryRunAutomation: mocks.dryRun,
  },
}))

const scrollIntoView = Object.getOwnPropertyDescriptor(Element.prototype, "scrollIntoView")

afterEach(() => {
  if (scrollIntoView) Object.defineProperty(Element.prototype, "scrollIntoView", scrollIntoView)
  else Reflect.deleteProperty(Element.prototype, "scrollIntoView")
  cleanup()
  vi.clearAllMocks()
  vi.restoreAllMocks()
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
  it("rejects an unselected category for save, enable, and dry run, but accepts Uncategorized", async () => {
    // Radix scrolls focused select options; jsdom has no layout.
    Object.defineProperty(Element.prototype, "scrollIntoView", { configurable: true, value: vi.fn() })
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={client}>
        <WorkflowDialog open onOpenChange={() => {}} instanceId={1} rule={rule} />
      </QueryClientProvider>
    )

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
