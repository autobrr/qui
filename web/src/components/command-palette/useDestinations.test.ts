/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, renderHook } from "@testing-library/react"
import { Circle } from "lucide-react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { findEntry } from "./palette"
import { useDestinations } from "./useDestinations"

const fixture = vi.hoisted(() => ({
  instances: { instances: [{ id: 1, name: "seedbox", isActive: true, connected: true }, { id: 2, name: "archive", isActive: true, connected: true }] },
  location: { pathname: "/instances/1", search: { q: "amber" } as Record<string, unknown> },
  layoutRoute: { state: { showInstanceControls: true, instanceId: 1 } },
  incognito: [false, () => {}] as [boolean, (value: boolean) => void],
  navigate: vi.fn(),
  navigateWithSearch: vi.fn(),
  translation: { t: (key: string) => key },
}))

vi.mock("react-i18next", () => ({ useTranslation: () => fixture.translation }))
vi.mock("@tanstack/react-router", () => ({ useNavigate: () => fixture.navigate, useLocation: () => fixture.location }))
vi.mock("@/hooks/useInstances", () => ({ useInstances: () => fixture.instances }))
vi.mock("@/contexts/LayoutRouteContext", () => ({ useLayoutRoute: () => fixture.layoutRoute }))
vi.mock("@/lib/incognito", () => ({ useIncognitoMode: () => fixture.incognito }))
vi.mock("@/lib/router-search", () => ({ navigateWithSearch: fixture.navigateWithSearch }))
vi.mock("./useThemeEntry", () => ({ useThemeEntry: () => ({ id: "action.theme", label: "Theme", icon: Circle }) }))

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

function runEntry(id: string) {
  const groups = renderHook(() => useDestinations(() => {})).result.current
  findEntry(groups, id)?.run?.()
}

describe("useDestinations", () => {
  // regression: #4229
  it("keeps the search of the open list when Add torrent of that instance opens the dialog", () => {
    runEntry("instance.1.add")

    expect(fixture.navigateWithSearch).toHaveBeenCalledWith(expect.objectContaining({ search: { q: "amber", modal: "add-torrent" } }))
    expect(fixture.navigate).not.toHaveBeenCalled()
  })

  it("opens Add torrent of another instance on that instance, without the search of the open list", () => {
    runEntry("instance.2.add")

    expect(fixture.navigate).toHaveBeenCalledWith({ to: "/instances/$instanceId", params: { instanceId: "2" }, search: { modal: "add-torrent" } })
  })

  // regression: #4260
  it("opens a section of Automations", () => {
    runEntry("automations.orphan-scan")

    expect(fixture.navigate).toHaveBeenCalledWith({ to: "/automations", search: { tab: "orphan-scan" } })
  })
})
