/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"
import type { FilesystemCapabilities, Instance } from "@/types"

const mocks = vi.hoisted(() => ({
  instances: { instances: [] as unknown[], updateInstance: vi.fn(), isUpdating: false },
  toastError: vi.fn(),
  routeFormProps: [] as { localWrite: boolean }[],
  translation: { t: (key: string) => key },
  routes: { data: [] as unknown[] | undefined, isSuccess: true },
  getCrossSeedRoute: vi.fn(),
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
vi.mock("@/components/cross-seed/CrossSeedRouteForm", () => ({
  CrossSeedRouteForm: (props: { localWrite: boolean }) => {
    mocks.routeFormProps.push(props)
    return null
  },
}))
vi.mock("@/hooks/useCrossSeedRoutes", () => ({ useCrossSeedRoutes: () => mocks.routes }))
vi.mock("@/lib/api", () => ({ api: { getCrossSeedRoute: mocks.getCrossSeedRoute } }))

import { HardlinkModeSettings } from "../HardlinkModeSettings"

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
  mocks.routes.data = []
  mocks.routes.isSuccess = true
})

const noCapabilities: FilesystemCapabilities = { read: false, identity: false, trustedIdentity: false, write: false, content: false }

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
  { name: "a local instance", instance: makeInstance(true, { read: true, identity: true, trustedIdentity: true, write: true, content: true }), canWrite: true, canReflink: true },
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
    // The route form gets the same Write plus local mode check as reflink mode.
    expect(mocks.routeFormProps.at(-1)?.localWrite).toBe(canReflink)

    fireEvent.change(screen.getByPlaceholderText("rules.baseDirectoriesPlaceholder"), { target: { value: "/data/links2" } })
    fireEvent.click(screen.getByRole("button", { name: "rules.saveChanges" }))

    expect(mocks.instances.updateInstance).toHaveBeenCalledTimes(canWrite ? 1 : 0)
    if (!canWrite) {
      expect(mocks.toastError).toHaveBeenCalledWith("toast.cannotEnableMode", expect.anything())
    }
  })

  describe("on a Source instance", () => {
    const source = { ...cases[0].instance, hardlinkBaseDir: "/data/links, /data/more/" }
    const seedbox = { ...cases[0].instance, id: 2, name: "seedbox" }

    function saveBaseDirs(value: string) {
      mocks.instances.instances = [source, seedbox]
      mocks.routes.data = [{
        sourceInstanceId: 1,
        crossSeedInstanceId: 2,
        rows: [{ baseDir: "/data/links", mappedPath: "/mnt/links" }, { baseDir: "/data/more", mappedPath: "" }],
      }]
      render(<HardlinkModeSettings pooledPartialCompletionEnabled={false} onPooledPartialCompletionEnabledChange={() => {}} />)
      fireEvent.click(screen.getByText("qbit"))
      fireEvent.change(screen.getByPlaceholderText("rules.baseDirectoriesPlaceholder"), { target: { value } })
      fireEvent.click(screen.getByRole("button", { name: "rules.saveChanges" }))
    }

    it("asks before a save that removes a mapped base folder, with the routed torrents under it", async () => {
      mocks.translation.t = ((key: string, options?: { count?: number }) => options?.count === undefined ? key : `${key}:${options.count}`) as typeof mocks.translation.t
      mocks.getCrossSeedRoute.mockResolvedValue({
        sourceInstanceId: 1,
        crossSeedInstanceId: 2,
        rows: [{ baseDir: "/data/links", mappedPath: "/mnt/links", routedTorrents: 4 }, { baseDir: "/data/more", mappedPath: "", routedTorrents: 7 }],
      })
      try {
        saveBaseDirs("/data/more")

        await screen.findByText("routeConfirm.count:4")
        expect(screen.getByText("routeConfirm.orphanScan")).toBeTruthy()
        expect(mocks.instances.updateInstance).not.toHaveBeenCalled()

        fireEvent.click(screen.getByRole("button", { name: "routeConfirm.confirm" }))
        expect(mocks.instances.updateInstance).toHaveBeenCalledTimes(1)
        expect(mocks.instances.updateInstance.mock.calls[0][0].data.hardlinkBaseDir).toBe("/data/more")
      } finally {
        mocks.translation.t = (key: string) => key
      }
    })

    it("says the count is unknown when the route read gives no count", async () => {
      mocks.getCrossSeedRoute.mockResolvedValue({
        sourceInstanceId: 1,
        crossSeedInstanceId: 2,
        rows: [{ baseDir: "/data/links", mappedPath: "/mnt/links", routedTorrents: null }, { baseDir: "/data/more", mappedPath: "", routedTorrents: null }],
      })
      saveBaseDirs("/data/more")

      await screen.findByText("routeConfirm.countUnknown")
      expect(mocks.instances.updateInstance).not.toHaveBeenCalled()
    })

    it("keeps Save changes off until the route list is read", () => {
      mocks.instances.instances = [source, seedbox]
      mocks.routes.data = undefined
      mocks.routes.isSuccess = false
      render(<HardlinkModeSettings pooledPartialCompletionEnabled={false} onPooledPartialCompletionEnabledChange={() => {}} />)
      fireEvent.click(screen.getByText("qbit"))
      fireEvent.change(screen.getByPlaceholderText("rules.baseDirectoriesPlaceholder"), { target: { value: "/data/more" } })

      expect(screen.getByRole<HTMLButtonElement>("button", { name: "rules.saveChanges" }).disabled).toBe(true)
    })

    it("keeps the count of a cancelled confirm out of the next confirm", async () => {
      mocks.translation.t = ((key: string, options?: { count?: number }) => options?.count === undefined ? key : `${key}:${options.count}`) as typeof mocks.translation.t
      const read = (routedLinks: number, routedMore: number) => ({
        sourceInstanceId: 1,
        crossSeedInstanceId: 2,
        rows: [{ baseDir: "/data/links", mappedPath: "/mnt/links", routedTorrents: routedLinks }, { baseDir: "/data/more", mappedPath: "", routedTorrents: routedMore }],
      })
      const resolves: ((value: unknown) => void)[] = []
      mocks.getCrossSeedRoute.mockImplementation(() => new Promise(resolve => resolves.push(resolve)))
      try {
        saveBaseDirs("/data/more")
        fireEvent.click(await screen.findByRole("button", { name: "routeConfirm.cancel" }))
        fireEvent.change(screen.getByPlaceholderText("rules.baseDirectoriesPlaceholder"), { target: { value: "/data/links" } })
        fireEvent.click(screen.getByRole("button", { name: "rules.saveChanges" }))
        await screen.findByText("routeConfirm.counting")

        await act(async () => resolves[0](read(4, 7)))
        expect(screen.queryByText("routeConfirm.count:4")).toBeNull()
        expect(screen.getByRole<HTMLButtonElement>("button", { name: "routeConfirm.confirm" }).disabled).toBe(true)

        await act(async () => resolves[1](read(4, 7)))
        expect(await screen.findByText("routeConfirm.count:7")).toBeTruthy()
      } finally {
        mocks.translation.t = (key: string) => key
      }
    })

    it("matches a decomposed base folder to its saved route row", () => {
      const cafe = "/data/cafe\u0301"
      mocks.instances.instances = [{ ...source, hardlinkBaseDir: cafe }, seedbox]
      mocks.routes.data = [{ sourceInstanceId: 1, crossSeedInstanceId: 2, rows: [{ baseDir: cafe.normalize("NFC"), mappedPath: "/mnt/cafe" }] }]
      render(<HardlinkModeSettings pooledPartialCompletionEnabled={false} onPooledPartialCompletionEnabledChange={() => {}} />)
      fireEvent.click(screen.getByText("qbit"))
      fireEvent.change(screen.getByPlaceholderText("rules.baseDirectoriesPlaceholder"), { target: { value: `${cafe}, /data/new` } })
      fireEvent.click(screen.getByRole("button", { name: "rules.saveChanges" }))

      expect(mocks.getCrossSeedRoute).not.toHaveBeenCalled()
      expect(mocks.instances.updateInstance).toHaveBeenCalledTimes(1)
    })

    it("saves without a confirm when every mapped base folder stays", () => {
      saveBaseDirs("/data/links/, /data/more, /data/new")

      expect(mocks.getCrossSeedRoute).not.toHaveBeenCalled()
      expect(mocks.instances.updateInstance).toHaveBeenCalledTimes(1)
    })
  })
})
