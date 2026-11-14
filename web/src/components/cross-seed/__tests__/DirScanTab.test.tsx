/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

const mocks = vi.hoisted(() => {
  const query = { data: undefined, isLoading: false, isError: false }
  const mutation = { mutate: () => undefined, mutateAsync: () => Promise.resolve(), isPending: false }
  return { query, mutation, directories: { data: [], isLoading: false } }
})

vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => ({ t: (key: string) => key }),
}))
vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }))
vi.mock("@/components/ui/field-help", () => ({
  FieldHelp: ({ children }: { children: ReactNode }) => <span>{children}</span>,
}))
vi.mock("@/hooks/useDateTimeFormatters", () => ({
  useDateTimeFormatters: () => ({ formatISOTimestamp: (value: string) => value }),
}))
vi.mock("@/hooks/useInstanceMetadata", () => ({ useInstanceMetadata: () => mocks.query }))
vi.mock("@/lib/api", () => ({ api: {} }))
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...await importOriginal<typeof import("@tanstack/react-query")>(),
  useQueries: () => [],
}))
vi.mock("@/hooks/useDirScan", () => ({
  useCancelDirScan: () => mocks.mutation,
  useCreateDirScanDirectory: () => mocks.mutation,
  useDeleteDirScanDirectory: () => mocks.mutation,
  useDirScanDirectories: () => mocks.directories,
  useDirScanRunInjections: () => mocks.query,
  useDirScanRuns: () => mocks.query,
  useDirScanSettings: () => mocks.query,
  useDirScanStatus: () => mocks.query,
  useRequeueDirScanNoMatch: () => mocks.mutation,
  useResetDirScanFiles: () => mocks.mutation,
  useTriggerDirScan: () => mocks.mutation,
  useUpdateDirScanDirectory: () => mocks.mutation,
  useUpdateDirScanSettings: () => mocks.mutation,
}))

import type { InstanceResponse } from "@/types"
import { DirScanTab } from "../DirScanTab"

afterEach(cleanup)

const caps = (read: boolean, identity: boolean) => ({ read, identity, trustedIdentity: false, write: read, content: false })
const instance = (id: number, name: string, read: boolean, identity: boolean, local = false) =>
  ({ id, name, hasLocalFilesystemAccess: local, capabilities: caps(read, identity) }) as unknown as InstanceResponse

const local = instance(1, "local", true, true, true)
const exec = instance(2, "remote exec", true, true)
const sftp = instance(3, "remote sftp", true, false)
const none = instance(4, "no access", false, false)

const note = "dirScan.directoryDialog.remoteNoIdentityNote"
const warning = "dirScan.noLocalAccessWarning"

function addButton() {
  return screen.getByRole("button", { name: /dirScan\.addDirectory/ })
}

describe("DirScanTab scan targets", () => {
  it.each([
    { name: "no instance qui can read", instances: [none], wantWarning: true },
    { name: "a local instance", instances: [local], wantWarning: false },
    { name: "only a remote instance", instances: [none, sftp], wantWarning: false },
  ])("warns for $name", ({ instances, wantWarning }) => {
    render(<DirScanTab instances={instances} />)

    expect(screen.queryByText(warning) !== null).toBe(wantWarning)
    expect((addButton() as HTMLButtonElement).disabled).toBe(wantWarning)
  })

  it.each([
    { name: "a local target", instances: [local], wantNote: false },
    { name: "a remote target with identity", instances: [exec], wantNote: false },
    { name: "a remote target without identity", instances: [sftp], wantNote: true },
    { name: "a list that leaves out an instance with no access", instances: [none, exec], wantNote: false },
  ])("the directory dialog on $name", ({ instances, wantNote }) => {
    render(<DirScanTab instances={instances} />)

    fireEvent.click(addButton())

    expect(screen.queryByText(note) !== null).toBe(wantNote)
  })
})
