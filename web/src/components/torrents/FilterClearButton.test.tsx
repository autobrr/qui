/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { FilterClearButton } from "@/components/torrents/FilterClearButton"
import { usePersistedColumnFilters } from "@/hooks/usePersistedColumnFilters"
import { _resetClientSettingsForTests } from "@/lib/client-settings"
import { act, cleanup, render, renderHook, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

const mocks = vi.hoisted(() => ({
  navigate: vi.fn(),
  search: { q: "Ubuntu", modal: "tasks" },
}))

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => mocks.navigate,
  useSearch: () => mocks.search,
}))

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

beforeEach(() => {
  localStorage.clear()
  _resetClientSettingsForTests()
  mocks.navigate.mockClear()
})

afterEach(() => {
  cleanup()
  _resetClientSettingsForTests()
})

describe("FilterClearButton", () => {
  it("clears sidebar and column filters while keeping the search", async () => {
    const columns = renderHook(() => usePersistedColumnFilters(1))
    act(() => columns.result.current[1]([{ columnId: "ratio", operation: "lt", value: "1" }]))
    const clearSidebar = vi.fn()
    render(<FilterClearButton instanceId={1} hasSidebarFilters onClearSidebar={clearSidebar} />)

    await userEvent.click(screen.getByRole("button", { name: "columnFilter.clearFilters" }))

    expect(clearSidebar).toHaveBeenCalledOnce()
    expect(columns.result.current[0]).toEqual([])
    expect(mocks.navigate).not.toHaveBeenCalled()
  })

  it.each([
    ["clearSidebarFilters", true, false, false],
    ["clearColumnFilters", false, true, false],
    ["clearFiltersAndSearch", true, true, true],
  ] as const)("applies the %s scope", async (action, clearsSidebar, clearsColumns, clearsSearch) => {
    const columns = renderHook(() => usePersistedColumnFilters(1))
    act(() => columns.result.current[1]([{ columnId: "ratio", operation: "lt", value: "1" }]))
    const clearSidebar = vi.fn()
    render(<FilterClearButton instanceId={1} hasSidebarFilters onClearSidebar={clearSidebar} />)

    await userEvent.click(screen.getByRole("button", { name: "filterSidebar.clearOptions" }))
    await userEvent.click(screen.getByRole("menuitem", { name: `filterSidebar.${action}` }))

    expect(clearSidebar).toHaveBeenCalledTimes(clearsSidebar ? 1 : 0)
    expect(columns.result.current[0]).toEqual(clearsColumns ? [] : [{ columnId: "ratio", operation: "lt", value: "1" }])
    if (clearsSearch) {
      expect(mocks.navigate).toHaveBeenCalledWith({ search: { modal: "tasks" }, replace: true })
    } else {
      expect(mocks.navigate).not.toHaveBeenCalled()
    }
  })

  it("appears for column-only filters and clears only the current instance", async () => {
    const columns = renderHook(() => usePersistedColumnFilters(1))
    const otherColumns = renderHook(() => usePersistedColumnFilters(2))
    act(() => {
      columns.result.current[1]([{ columnId: "ratio", operation: "lt", value: "1" }])
      otherColumns.result.current[1]([{ columnId: "ratio", operation: "gt", value: "2" }])
    })
    render(<FilterClearButton instanceId={1} hasSidebarFilters={false} onClearSidebar={vi.fn()} />)

    await userEvent.click(screen.getByRole("button", { name: "columnFilter.clearFilters" }))

    expect(screen.queryByRole("button", { name: "columnFilter.clearFilters" })).toBeNull()
    expect(columns.result.current[0]).toEqual([])
    expect(otherColumns.result.current[0]).toEqual([{ columnId: "ratio", operation: "gt", value: "2" }])
  })
})
