/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { HttpResponse, http } from "msw"
import type { ChangeEvent, ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

import i18n from "@/i18n"
import { server } from "@/test/msw/server"
import type { CrossSeedRoute, CrossSeedRouteStatus, CrossSeedRouteTestResult, FilesystemCapabilities, Instance } from "@/types"

const mocks = vi.hoisted(() => ({
  instances: { instances: [] as unknown[] },
  // Echoes the key and each interpolation value, so a test can assert both.
  translation: { t: (key: string, values?: Record<string, unknown>) => values ? [key, ...Object.values(values)].join("|") : key },
}))

vi.mock("react-i18next", async (importOriginal) => ({
  ...await importOriginal<typeof import("react-i18next")>(),
  useTranslation: () => mocks.translation,
}))
vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }))
vi.mock("@/hooks/useInstances", () => ({ useInstances: () => mocks.instances }))
vi.mock("@/components/ui/field-help", () => ({ FieldHelp: () => null }))
// jsdom cannot open the Radix Select, so a native select stands in for it.
vi.mock("@/components/ui/select", () => ({
  Select: ({ value, onValueChange, disabled, children }: { value: string; onValueChange: (value: string) => void; disabled?: boolean; children: ReactNode }) => (
    <select aria-label="picker" value={value} disabled={disabled} onChange={(e: ChangeEvent<HTMLSelectElement>) => onValueChange(e.target.value)}>
      <option value="" />
      {children}
    </select>
  ),
  SelectContent: ({ children }: { children: ReactNode }) => <>{children}</>,
  SelectItem: ({ value, children }: { value: string; children: ReactNode }) => <option value={value}>{children}</option>,
  SelectTrigger: () => null,
  SelectValue: () => null,
}))

import { CrossSeedRouteForm } from "../CrossSeedRouteForm"

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

const localCapabilities: FilesystemCapabilities = { read: true, identity: true, trustedIdentity: true, write: true, content: true }
const noCapabilities: FilesystemCapabilities = { read: false, identity: false, trustedIdentity: false, write: false, content: false }

function makeInstance(id: number, name: string, overrides: Partial<Instance> = {}): Instance {
  return {
    id,
    name,
    host: "http://127.0.0.1:8080",
    username: "admin",
    tlsSkipVerify: false,
    hasLocalFilesystemAccess: true,
    filesystemMode: "local",
    capabilities: localCapabilities,
    useHardlinks: true,
    hardlinkBaseDir: "/data/a, /data/b",
    hardlinkDirPreset: "flat",
    useReflinks: false,
    fallbackToRegularMode: false,
    sortOrder: 0,
    isActive: true,
    reannounceSettings: {} as Instance["reannounceSettings"],
    ...overrides,
  }
}

const source = makeInstance(1, "library")
const crossSeed = makeInstance(2, "seedbox", { useHardlinks: false, hardlinkBaseDir: "" })

// Serves the route list, each route read, and the capabilities of each instance.
// Instances in old answer with a qBittorrent older than 5.0.
function serve({ routes = [] as CrossSeedRoute[], read = null as CrossSeedRouteStatus | null, old = [] as number[] } = {}) {
  server.use(
    http.get("*/api/cross-seed/routes", () => HttpResponse.json(routes)),
    http.get("*/api/cross-seed/routes/:id", () => HttpResponse.json(read)),
    http.get("*/api/instances/:id/capabilities", ({ params }) => HttpResponse.json(
      old.includes(Number(params.id))
        ? { webAPIVersion: "2.9.3", supportsPathAutocomplete: false }
        : { webAPIVersion: "2.11.4", supportsPathAutocomplete: true }
    ))
  )
}

function renderForm(instance: Instance, canWriteLocally = instance.capabilities.write && instance.filesystemMode === "local") {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <CrossSeedRouteForm instance={instance} localWrite={canWriteLocally} />
    </QueryClientProvider>
  )
}

const picker = () => screen.getByLabelText<HTMLSelectElement>("picker")
const optionNames = () => Array.from(picker().options).map(option => option.textContent).filter(Boolean)

describe("CrossSeedRouteForm picker", () => {
  const sourceOfLibrary = makeInstance(3, "other-library")
  const cases = [
    {
      name: "an instance without filesystem access",
      instance: makeInstance(1, "library", { hasLocalFilesystemAccess: false, filesystemMode: "none", capabilities: noCapabilities, useHardlinks: false }),
      reason: "automations:queryBuilder.capabilityReasons.localFilesystemAccess",
    },
    {
      // A remote instance has Write and can use hardlink mode, but cannot route.
      name: "a remote instance in hardlink mode",
      instance: makeInstance(1, "library", { hasLocalFilesystemAccess: false, filesystemMode: "remote", capabilities: { ...noCapabilities, read: true, write: true } }),
      reason: "automations:queryBuilder.capabilityReasons.remoteInstance",
    },
    {
      // A Cross-seed instance needs no filesystem access, so this reason wins.
      name: "an instance without filesystem access that receives cross-seeds",
      instance: makeInstance(1, "library", { hasLocalFilesystemAccess: false, filesystemMode: "none", capabilities: noCapabilities, useHardlinks: false }),
      routes: [{ sourceInstanceId: 3, crossSeedInstanceId: 1, rows: [] }],
      reason: "routeForm.receivesCrossSeeds|other-library",
    },
    {
      name: "an instance in regular mode",
      instance: makeInstance(1, "library", { useHardlinks: false }),
      reason: "routeForm.needsLinkMode",
    },
  ]

  it.each(cases)("is turned off with the reason on $name", async ({ instance, routes, reason }) => {
    mocks.instances.instances = [instance, crossSeed, sourceOfLibrary]
    serve({ routes })
    renderForm(instance)

    expect(await screen.findByText(reason)).toBeTruthy()
    expect(i18n.exists(reason.split("|")[0], { ns: "crossseed" })).toBe(true)
    expect(picker().disabled).toBe(true)
    expect(screen.queryByRole("button", { name: "routeForm.testRoute" })).toBeNull()
  })

  it("is turned on for a local instance in hardlink mode", async () => {
    mocks.instances.instances = [source, crossSeed]
    serve()
    renderForm(source)

    await waitFor(() => expect(optionNames()).toEqual(["seedbox"]))
    expect(picker().disabled).toBe(false)
  })

  it("leaves out the instance itself, routed, old, remote, and turned-off instances", async () => {
    mocks.instances.instances = [
      source,
      crossSeed,
      makeInstance(3, "old"),
      makeInstance(4, "remote", { filesystemMode: "remote" }),
      makeInstance(5, "off", { isActive: false }),
      makeInstance(6, "routed"),
      // Several Source instances can share one Cross-seed instance.
      makeInstance(7, "shared"),
    ]
    serve({ routes: [{ sourceInstanceId: 6, crossSeedInstanceId: 7, rows: [] }], old: [3] })
    renderForm(source)

    await waitFor(() => expect(optionNames()).toEqual(["seedbox", "shared"]))
  })
})

describe("CrossSeedRouteForm rows", () => {
  it("shows one row for each base folder and saves the typed paths", async () => {
    mocks.instances.instances = [source, crossSeed]
    serve()
    const saves: unknown[] = []
    server.use(http.put("*/api/cross-seed/routes/1", async ({ request }) => {
      saves.push(await request.json())
      return HttpResponse.json({ sourceInstanceId: 1, crossSeedInstanceId: 2, rows: [] })
    }))
    renderForm(source)

    await waitFor(() => expect(optionNames()).toEqual(["seedbox"]))
    fireEvent.change(picker(), { target: { value: "2" } })
    fireEvent.change(screen.getByLabelText("/data/a"), { target: { value: " /mnt/a " } })
    expect(screen.getByLabelText<HTMLInputElement>("/data/b").value).toBe("")
    fireEvent.click(screen.getByRole("button", { name: "routeForm.saveRoute" }))

    await waitFor(() => expect(saves).toEqual([{
      crossSeedInstanceId: 2,
      rows: [{ baseDir: "/data/a", mappedPath: "/mnt/a" }, { baseDir: "/data/b", mappedPath: "" }],
    }]))
  })

  it.each([
    { name: "nested folders", a: "/mnt/data", b: "/mnt/data/b" },
    { name: "an empty row inside a mapped row", a: "/data", b: "" },
    { name: "drive letters in another case", a: "D:\\Data", b: "d:/data" },
    { name: "folders that nest after .. is resolved", a: "/mnt/x/../data", b: "/mnt/data/b" },
    { name: "a UNC share and a .. above it", a: "//nas/share/../a", b: "//nas/share/a/b" },
  ])("refuses two rows that map to $name", async ({ a, b }) => {
    mocks.instances.instances = [source, crossSeed]
    serve()
    renderForm(source)

    await waitFor(() => expect(optionNames()).toEqual(["seedbox"]))
    fireEvent.change(picker(), { target: { value: "2" } })
    fireEvent.change(screen.getByLabelText("/data/a"), { target: { value: a } })
    fireEvent.change(screen.getByLabelText("/data/b"), { target: { value: b } })

    expect(screen.getByText("routeForm.overlap|/data/a|/data/b")).toBeTruthy()
    expect(screen.getByRole<HTMLButtonElement>("button", { name: "routeForm.saveRoute" }).disabled).toBe(true)
    expect(screen.getByRole<HTMLButtonElement>("button", { name: "routeForm.testRoute" }).disabled).toBe(true)
  })

  it("resolves .. before it compares two rows", async () => {
    mocks.instances.instances = [source, crossSeed]
    serve()
    renderForm(source)

    await waitFor(() => expect(optionNames()).toEqual(["seedbox"]))
    fireEvent.change(picker(), { target: { value: "2" } })
    fireEvent.change(screen.getByLabelText("/data/a"), { target: { value: "/mnt/a" } })
    fireEvent.change(screen.getByLabelText("/data/b"), { target: { value: "/mnt/a/../b" } })

    expect(screen.queryByText("routeForm.overlap|/data/a|/data/b")).toBeNull()
    expect(screen.getByRole<HTMLButtonElement>("button", { name: "routeForm.saveRoute" }).disabled).toBe(false)
  })

  it("gives each form its own row inputs when two Source instances share a base folder", async () => {
    const other = makeInstance(3, "other-library")
    mocks.instances.instances = [source, crossSeed, other]
    serve()
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={queryClient}>
        <CrossSeedRouteForm instance={source} localWrite />
        <CrossSeedRouteForm instance={other} localWrite />
      </QueryClientProvider>
    )

    const pickers = screen.getAllByLabelText<HTMLSelectElement>("picker")
    await waitFor(() => expect(pickers[1].options.length).toBeGreaterThan(1))
    for (const each of pickers) fireEvent.change(each, { target: { value: "2" } })

    expect(new Set(screen.getAllByLabelText("/data/a")).size).toBe(2)
  })

  it("shows one row for two spellings of one base folder", async () => {
    const instance = makeInstance(1, "library", { hardlinkBaseDir: "/data/a, /data/a/, /data/b" })
    mocks.instances.instances = [instance, crossSeed]
    serve()
    renderForm(instance)

    await waitFor(() => expect(optionNames()).toEqual(["seedbox"]))
    fireEvent.change(picker(), { target: { value: "2" } })

    expect(screen.getAllByRole("textbox")).toEqual([screen.getByLabelText("/data/a"), screen.getByLabelText("/data/b")])
  })

  it("does not save while it could not read the route list", async () => {
    mocks.instances.instances = [source, crossSeed]
    serve()
    server.use(http.get("*/api/cross-seed/routes", () => HttpResponse.json({ error: "boom" }, { status: 500 })))
    renderForm(source)

    await waitFor(() => expect(optionNames()).toEqual(["seedbox"]))
    fireEvent.change(picker(), { target: { value: "2" } })

    expect(screen.getByRole<HTMLButtonElement>("button", { name: "routeForm.saveRoute" }).disabled).toBe(true)
    expect(screen.getByRole<HTMLButtonElement>("button", { name: "routeForm.testRoute" }).disabled).toBe(true)
  })

  it("keeps the saved paths and Save off until the route reads refresh", async () => {
    mocks.instances.instances = [source, crossSeed]
    const before: CrossSeedRouteStatus = { sourceInstanceId: 1, crossSeedInstanceId: 2, rows: [{ baseDir: "/data/a", mappedPath: "/mnt/a", routedTorrents: null }, { baseDir: "/data/b", mappedPath: "", routedTorrents: null }] }
    serve({ routes: [{ sourceInstanceId: 1, crossSeedInstanceId: 2, rows: before.rows }] })
    let reads = 0
    let release = () => {}
    const refreshed = new Promise<void>(resolve => { release = resolve })
    server.use(
      http.get("*/api/cross-seed/routes/:id", async () => {
        reads++
        if (reads > 1) await refreshed
        return HttpResponse.json(before)
      }),
      http.put("*/api/cross-seed/routes/1", () => HttpResponse.json(before))
    )
    renderForm(source)

    fireEvent.change(await screen.findByLabelText("/data/a"), { target: { value: "/mnt/new" } })
    fireEvent.click(screen.getByRole("button", { name: "routeForm.saveRoute" }))
    await waitFor(() => expect(reads).toBe(2))

    expect(screen.getByLabelText<HTMLInputElement>("/data/a").value).toBe("/mnt/new")
    expect(screen.getByRole<HTMLButtonElement>("button", { name: "routeForm.saveRoute" }).disabled).toBe(true)
    release()
  })

  it("shows a saved route, a new base folder as an empty row, and the broken rule", async () => {
    const instance = makeInstance(1, "library", { hardlinkBaseDir: "/data/a, /data/new" })
    mocks.instances.instances = [instance, crossSeed]
    serve({
      routes: [{ sourceInstanceId: 1, crossSeedInstanceId: 2, rows: [{ baseDir: "/data/a", mappedPath: "/mnt/a" }] }],
      read: {
        sourceInstanceId: 1,
        crossSeedInstanceId: 2,
        rows: [{ baseDir: "/data/a", mappedPath: "/mnt/a", routedTorrents: null }, { baseDir: "/data/new", mappedPath: "", routedTorrents: null }],
        brokenRule: "Cross-seed instance unavailable: seedbox: offline",
      },
    })
    renderForm(instance)

    expect(await screen.findByText("routeForm.brokenRule|Cross-seed instance unavailable: seedbox: offline")).toBeTruthy()
    expect(picker().value).toBe("2")
    expect(screen.getByLabelText<HTMLInputElement>("/data/a").value).toBe("/mnt/a")
    expect(screen.getByLabelText<HTMLInputElement>("/data/new").value).toBe("")
    expect(screen.getByRole("button", { name: "routeForm.clearRoute" })).toBeTruthy()
  })

  it("does not save or test a saved route that it could not read", async () => {
    mocks.instances.instances = [source, crossSeed]
    serve({ routes: [{ sourceInstanceId: 1, crossSeedInstanceId: 2, rows: [{ baseDir: "/data/a", mappedPath: "/mnt/a" }] }] })
    server.use(http.get("*/api/cross-seed/routes/:id", () => HttpResponse.json({ error: "boom" }, { status: 500 })))
    renderForm(source)

    expect(await screen.findByLabelText("/data/a")).toBeTruthy()
    expect(screen.getByRole<HTMLButtonElement>("button", { name: "routeForm.saveRoute" }).disabled).toBe(true)
    expect(screen.getByRole<HTMLButtonElement>("button", { name: "routeForm.testRoute" }).disabled).toBe(true)
  })

  it("suggests folders of the Cross-seed instance", async () => {
    mocks.instances.instances = [source, crossSeed]
    serve()
    const listed: string[] = []
    server.use(http.get("*/api/instances/:id/getDirectoryContent", ({ params, request }) => {
      listed.push(`${params.id}:${new URL(request.url).searchParams.get("dirPath")}`)
      return HttpResponse.json(["/mnt/movies", "/mnt/tv"])
    }))
    renderForm(source)

    await waitFor(() => expect(optionNames()).toEqual(["seedbox"]))
    fireEvent.change(picker(), { target: { value: "2" } })
    const row = await screen.findByLabelText("/data/a")
    await waitFor(() => {
      fireEvent.change(row, { target: { value: "/mnt/" } })
      expect(screen.getByRole("button", { name: "/mnt/tv" })).toBeTruthy()
    })
    expect(listed).toContain("2:/mnt/")
  })
})

describe("CrossSeedRouteForm Test route", () => {
  async function runTest(result: CrossSeedRouteTestResult) {
    mocks.instances.instances = [source, crossSeed]
    serve()
    const tests: unknown[] = []
    server.use(http.post("*/api/cross-seed/routes/1/test", async ({ request }) => {
      tests.push(await request.json())
      return HttpResponse.json(result)
    }))
    renderForm(source)

    await waitFor(() => expect(optionNames()).toEqual(["seedbox"]))
    fireEvent.change(picker(), { target: { value: "2" } })
    fireEvent.change(screen.getByLabelText("/data/a"), { target: { value: "/mnt/a" } })
    fireEvent.click(screen.getByRole("button", { name: "routeForm.testRoute" }))
    return tests
  }

  it("tests the rows in the form and shows the result of each folder", async () => {
    const tests = await runTest({
      rows: [
        { baseDir: "/data/a", listedPath: "/mnt/a", result: "seesFolderNotMarker" },
        { baseDir: "/data/b", listedPath: "/data/b", result: "cannotWrite", error: "permission denied" },
      ],
    })

    expect(await screen.findByText("routeForm.testResultRow|/data/a|/mnt/a|crossseed:routeForm.testResult.seesFolderNotMarker")).toBeTruthy()
    expect(screen.getByText("routeForm.testResultRow|/data/b|/data/b|crossseed:routeForm.testResult.cannotWrite")).toBeTruthy()
    expect(screen.getByText("routeForm.markerHint")).toBeTruthy()
    expect(screen.getByText("permission denied")).toBeTruthy()
    expect(tests).toEqual([{ crossSeedInstanceId: 2, rows: [{ baseDir: "/data/a", mappedPath: "/mnt/a" }, { baseDir: "/data/b", mappedPath: "" }] }])

    // An edit makes the result stale, so the form drops it.
    fireEvent.change(screen.getByLabelText("/data/b"), { target: { value: "/mnt/b" } })
    expect(screen.queryByText("routeForm.markerHint")).toBeNull()
  })

  it("shows Cross-seed instance unavailable once", async () => {
    await runTest({ crossSeedUnavailable: "Cross-seed instance unavailable: seedbox: offline", rows: [] })

    expect(await screen.findAllByText("routeStatus.unavailable")).toHaveLength(1)
    expect(screen.getByText("seedbox: offline")).toBeTruthy()
  })
})

describe("CrossSeedRouteForm confirm", () => {
  const otherCrossSeed = makeInstance(3, "seedbox2", { useHardlinks: false, hardlinkBaseDir: "" })

  // A saved route to seedbox: 3 routed torrents under /data/a, 4 under /data/b.
  function serveSaved(routedA: number | null = 3) {
    mocks.instances.instances = [source, crossSeed, otherCrossSeed]
    const writes: string[] = []
    serve({
      routes: [{ sourceInstanceId: 1, crossSeedInstanceId: 2, rows: [{ baseDir: "/data/a", mappedPath: "/mnt/a" }, { baseDir: "/data/b", mappedPath: "/mnt/b" }] }],
      read: {
        sourceInstanceId: 1,
        crossSeedInstanceId: 2,
        rows: [{ baseDir: "/data/a", mappedPath: "/mnt/a", routedTorrents: routedA }, { baseDir: "/data/b", mappedPath: "/mnt/b", routedTorrents: 4 }],
      },
    })
    server.use(
      http.put("*/api/cross-seed/routes/1", () => {
        writes.push("save")
        return HttpResponse.json({ sourceInstanceId: 1, crossSeedInstanceId: 2, rows: [] })
      }),
      http.delete("*/api/cross-seed/routes/1", () => {
        writes.push("clear")
        return new HttpResponse(null, { status: 204 })
      })
    )
    renderForm(source)
    return writes
  }

  const confirmButton = () => screen.findByRole("button", { name: "routeConfirm.confirm" })

  it("asks before a row edit, with the routed torrents under the edited row", async () => {
    const writes = serveSaved()
    fireEvent.change(await screen.findByDisplayValue("/mnt/a"), { target: { value: "/mnt/x" } })
    fireEvent.click(screen.getByRole("button", { name: "routeForm.saveRoute" }))

    expect(await screen.findByText("routeConfirm.count|library|seedbox|3")).toBeTruthy()
    expect(screen.getByText("routeConfirm.orphanScan|library|seedbox")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "routeConfirm.cancel" }))
    expect(writes).toEqual([])

    fireEvent.click(screen.getByRole("button", { name: "routeForm.saveRoute" }))
    fireEvent.click(await confirmButton())
    await waitFor(() => expect(writes).toEqual(["save"]))
  })

  it("asks before a change of Cross-seed instance, with the routed torrents under all rows", async () => {
    const writes = serveSaved()
    await screen.findByDisplayValue("/mnt/a")
    await waitFor(() => expect(optionNames()).toContain("seedbox2"))
    fireEvent.change(picker(), { target: { value: "3" } })
    fireEvent.click(screen.getByRole("button", { name: "routeForm.saveRoute" }))

    expect(await screen.findByText("routeConfirm.count|library|seedbox|7")).toBeTruthy()
    fireEvent.click(await confirmButton())
    await waitFor(() => expect(writes).toEqual(["save"]))
  })

  it("says the count is unknown when a row has no count", async () => {
    serveSaved(null)
    fireEvent.change(await screen.findByDisplayValue("/mnt/a"), { target: { value: "/mnt/x" } })
    fireEvent.click(screen.getByRole("button", { name: "routeForm.saveRoute" }))

    expect(await screen.findByText("routeConfirm.countUnknown|library|seedbox")).toBeTruthy()
  })

  it("saves with no confirm when no saved row changes", async () => {
    const writes = serveSaved()
    await screen.findByDisplayValue("/mnt/a")
    fireEvent.click(screen.getByRole("button", { name: "routeForm.saveRoute" }))

    await waitFor(() => expect(writes).toEqual(["save"]))
    expect(screen.queryByText("routeConfirm.title")).toBeNull()
  })

  it("asks before a route clear and says the Cross-seed instance keeps the routed torrents", async () => {
    const writes = serveSaved()
    await screen.findByDisplayValue("/mnt/a")
    fireEvent.click(screen.getByRole("button", { name: "routeForm.clearRoute" }))

    expect(await screen.findByText("routeConfirm.count|library|seedbox|7")).toBeTruthy()
    expect(screen.getByText("routeConfirm.clear|library|seedbox")).toBeTruthy()
    expect(writes).toEqual([])
    fireEvent.click(await confirmButton())
    await waitFor(() => expect(writes).toEqual(["clear"]))
  })
})
