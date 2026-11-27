/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"
import type { Instance } from "@/types"

const mocks = vi.hoisted(() => ({
  instances: { createInstance: vi.fn(), updateInstance: vi.fn(), isCreating: false, isUpdating: false },
  translation: { t: (key: string) => key },
}))

vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => mocks.translation,
}))
vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }))
vi.mock("@/components/ui/field-help", () => ({
  FieldHelp: ({ children }: { children: ReactNode }) => <span>{children}</span>,
}))
vi.mock("@/hooks/useInstances", () => ({ useInstances: () => mocks.instances }))

import { InstanceForm } from "./InstanceForm"

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const instance = {
  id: 7,
  name: "Seedbox",
  host: "http://seedbox.example:8080",
  username: "admin",
  tlsSkipVerify: false,
  hasLocalFilesystemAccess: false,
  capabilities: { read: false, identity: false, trustedIdentity: false, write: false, content: false },
  filesystemMode: "none",
  useHardlinks: false,
  hardlinkBaseDir: "",
  hardlinkDirPreset: "flat",
  useReflinks: false,
  fallbackToRegularMode: false,
  sortOrder: 0,
  isActive: true,
  reannounceSettings: {
    enabled: false, initialWaitSeconds: 0, reannounceIntervalSeconds: 0, maxAgeSeconds: 0, maxRetries: 0, aggressive: false, monitorAll: false,
  } as Instance["reannounceSettings"],
} as Instance

describe("InstanceForm", () => {
  it("points a new instance at the SSH tab, and an existing one not", () => {
    const { unmount } = render(<InstanceForm open onOpenChange={vi.fn()} onSuccess={vi.fn()} />)
    expect(screen.getByText("form.sshAfterSave")).not.toBeNull()
    unmount()

    render(<InstanceForm open onOpenChange={vi.fn()} instance={instance} onSuccess={vi.fn()} />)
    expect(screen.queryByText("form.sshAfterSave")).toBeNull()
  })

  it("shows the worked-out peer address as a placeholder and never saves it", async () => {
    const { unmount } = render(<InstanceForm open onOpenChange={vi.fn()} onSuccess={vi.fn()} />)
    expect(screen.queryByText("publicPeerAddress.label")).toBeNull()
    unmount()

    render(<InstanceForm open onOpenChange={vi.fn()} instance={{ ...instance, publicPeerAddress: "", resolvedPublicPeerAddress: "203.0.113.7:51413" }} onSuccess={vi.fn()} />)
    const field = screen.getByPlaceholderText("203.0.113.7:51413") as HTMLInputElement
    expect(field.value).toBe("")

    fireEvent.click(screen.getByText("form.buttons.updateInstance"))
    await waitFor(() => expect(mocks.instances.updateInstance).toHaveBeenLastCalledWith(
      { id: 7, data: expect.objectContaining({ publicPeerAddress: "" }) }, expect.anything()))

    fireEvent.change(field, { target: { value: "seedbox.example.org:6881" } })
    fireEvent.click(screen.getByText("form.buttons.updateInstance"))
    await waitFor(() => expect(mocks.instances.updateInstance).toHaveBeenLastCalledWith(
      { id: 7, data: expect.objectContaining({ publicPeerAddress: "seedbox.example.org:6881" }) }, expect.anything()))
  })
})
