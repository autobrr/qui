/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, render, screen } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
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

beforeEach(() => {
  vi.stubGlobal("ResizeObserver", class {
    observe() {}
    unobserve() {}
    disconnect() {}
  })
})

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
    const { unmount } = render(<InstanceForm onSuccess={vi.fn()} onCancel={vi.fn()} />)
    expect(screen.getByText("form.sshAfterSave")).not.toBeNull()
    unmount()

    render(<InstanceForm instance={instance} onSuccess={vi.fn()} onCancel={vi.fn()} />)
    expect(screen.queryByText("form.sshAfterSave")).toBeNull()
  })
})
