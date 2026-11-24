/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import type { ExternalProgram, ExternalProgramPayload } from "@/types"

const mocks = vi.hoisted(() => ({
  listExternalPrograms: vi.fn(),
  updateExternalProgram: vi.fn(),
}))

vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => ({ t: (key: string) => key }),
}))
vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }))
vi.mock("@/lib/api", () => ({
  APIError: class extends Error {},
  api: {
    listExternalPrograms: mocks.listExternalPrograms,
    updateExternalProgram: mocks.updateExternalProgram,
  },
}))

import { TooltipProvider } from "@/components/ui/tooltip"
import { ExternalProgramsManager } from "./ExternalProgramsManager"

const PROGRAM: ExternalProgram = {
  id: 1,
  name: "Old name",
  path: "/opt/old",
  args_template: "{hash}",
  enabled: true,
  use_terminal: false,
  path_mappings: [],
  created_at: "",
  updated_at: "",
}

function renderManager() {
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <TooltipProvider>
        <ExternalProgramsManager />
      </TooltipProvider>
    </QueryClientProvider>
  )
}

/** Opens the editor of the program, changes its name, and saves. */
async function renameProgram(name: string) {
  fireEvent.click(await screen.findByRole("button", { name: "kit.edit" }))
  const dialog = await screen.findByRole("dialog")
  fireEvent.change(within(dialog).getByLabelText("externalPrograms.nameLabel"), { target: { value: name } })
  fireEvent.click(within(dialog).getByRole("button", { name: "externalPrograms.save" }))
}

const sentPayloads = () => mocks.updateExternalProgram.mock.calls.map(([, payload]) => payload as ExternalProgramPayload)

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
  vi.unstubAllGlobals()
})

describe("ExternalProgramsManager", () => {
  // regression: #4233
  it("keeps a saved edit when the switch is pressed before the list refetch answers", async () => {
    mocks.listExternalPrograms.mockResolvedValueOnce([PROGRAM]).mockReturnValue(new Promise(() => {}))
    mocks.updateExternalProgram.mockImplementation((id: number, payload: ExternalProgramPayload) => Promise.resolve({ ...PROGRAM, ...payload, id }))
    renderManager()

    await renameProgram("New name")
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
    fireEvent.click(screen.getByRole("switch", { name: "kit.enabled" }))

    await waitFor(() => expect(mocks.updateExternalProgram).toHaveBeenCalledTimes(2))
    expect(sentPayloads()[1]).toMatchObject({ name: "New name", enabled: false })
  })

  // regression: #4233
  it("sends an edit only after the switch save of the same program ends", async () => {
    mocks.listExternalPrograms.mockResolvedValue([PROGRAM])
    let finishSwitch: (program: ExternalProgram) => void = () => {}
    mocks.updateExternalProgram
      .mockImplementationOnce(() => new Promise((resolve) => { finishSwitch = resolve }))
      .mockImplementation((id: number, payload: ExternalProgramPayload) => Promise.resolve({ ...PROGRAM, ...payload, id }))
    renderManager()

    fireEvent.click(await screen.findByRole("switch", { name: "kit.enabled" }))
    await waitFor(() => expect(mocks.updateExternalProgram).toHaveBeenCalledTimes(1))
    await renameProgram("New name")
    expect(mocks.updateExternalProgram).toHaveBeenCalledTimes(1)

    finishSwitch({ ...PROGRAM, enabled: false })
    const save = within(screen.getByRole("dialog")).getByRole("button", { name: "externalPrograms.save" })
    await waitFor(() => expect(save.hasAttribute("disabled")).toBe(false))
    fireEvent.click(save)

    await waitFor(() => expect(mocks.updateExternalProgram).toHaveBeenCalledTimes(2))
    expect(sentPayloads()[1]).toMatchObject({ name: "New name", enabled: false })
  })
})
