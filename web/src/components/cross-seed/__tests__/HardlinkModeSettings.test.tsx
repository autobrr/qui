/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"
import type { FilesystemCapabilities, Instance } from "@/types"

const mocks = vi.hoisted(() => ({
  instances: { instances: [] as unknown[], updateInstance: vi.fn(), isUpdating: false },
  toastError: vi.fn(),
  translation: { t: (key: string) => key },
}))

vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => mocks.translation,
}))
vi.mock("sonner", () => ({ toast: { error: mocks.toastError, success: vi.fn() } }))
vi.mock("@/components/ui/field-help", () => ({
  FieldHelp: ({ children }: { children: ReactNode }) => <span>{children}</span>,
}))
vi.mock("@/components/cross-seed/PooledCompletionSetting", () => ({ PooledCompletionSetting: () => null }))
vi.mock("@/hooks/useInstances", () => ({ useInstances: () => mocks.instances }))

import { HardlinkModeSettings } from "../HardlinkModeSettings"

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

const noCapabilities: FilesystemCapabilities = { read: false, identity: false, write: false, content: false }

function makeInstance(hasLocalFilesystemAccess: boolean, capabilities: FilesystemCapabilities): Instance {
  return {
    id: 1,
    name: "qbit",
    host: "http://127.0.0.1:8080",
    username: "admin",
    tlsSkipVerify: false,
    hasLocalFilesystemAccess,
    filesystemMode: hasLocalFilesystemAccess ? "local" : "none",
    capabilities,
    useHardlinks: true,
    hardlinkBaseDir: "/data/links",
    hardlinkDirPreset: "flat",
    useReflinks: false,
    fallbackToRegularMode: false,
    sortOrder: 0,
    isActive: true,
    reannounceSettings: {} as Instance["reannounceSettings"],
  }
}

const remote = makeInstance(false, { ...noCapabilities, read: true, write: true })
remote.filesystemMode = "remote"

const cases = [
  { name: "a local instance", instance: makeInstance(true, { read: true, identity: true, write: true, content: true }), canWrite: true, canReflink: true },
  { name: "an instance without filesystem access", instance: makeInstance(false, noCapabilities), canWrite: false, canReflink: false },
  // A remote instance has Write (#2942) but nothing clones files over sftp, so
  // hardlink mode is offered and reflink mode is not.
  { name: "a remote instance", instance: remote, canWrite: true, canReflink: false },
]

describe("HardlinkModeSettings", () => {
  it.each(cases)("offers the link modes and saves them from the write capability for $name", ({ instance, canWrite, canReflink }) => {
    mocks.instances.instances = [instance]
    render(<HardlinkModeSettings pooledPartialCompletionEnabled={false} onPooledPartialCompletionEnabledChange={() => {}} />)
    fireEvent.click(screen.getByText("qbit"))

    expect(screen.getByRole("radio", { name: "rules.hardlink" }).hasAttribute("disabled")).toBe(!canWrite)
    expect(screen.getByRole("radio", { name: "rules.reflink" }).hasAttribute("disabled")).toBe(!canReflink)
    expect(screen.queryByText("rules.noLocalAccess") === null).toBe(canWrite)

    fireEvent.change(screen.getByPlaceholderText("rules.baseDirectoriesPlaceholder"), { target: { value: "/data/links2" } })
    fireEvent.click(screen.getByRole("button", { name: "rules.saveChanges" }))

    expect(mocks.instances.updateInstance).toHaveBeenCalledTimes(canWrite ? 1 : 0)
    if (!canWrite) {
      expect(mocks.toastError).toHaveBeenCalledWith("toast.cannotEnableMode", expect.anything())
    }
  })
})
