/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useColumnDnd, type UseColumnDndParams } from "@/hooks/torrent-table/useColumnDnd"
import type { DragEndEvent } from "@dnd-kit/core"
import { act, renderHook } from "@testing-library/react"
import { beforeEach, describe, expect, it } from "vitest"

function dragEvent(activeId: string | null, overId: string | null): DragEndEvent {
  return {
    active: activeId === null ? null : { id: activeId },
    over: overId === null ? null : { id: overId },
  } as unknown as DragEndEvent
}

function params(over: Partial<UseColumnDndParams>): UseColumnDndParams {
  return {
    instanceId: 1,
    defaultColumnOrder: ["a", "b", "c"],
    getLeafColumnIds: () => ["a", "b", "c"],
    ...over,
  }
}

beforeEach(() => localStorage.clear())

describe("useColumnDnd", () => {
  it("configures the two sensors and keeps setColumnOrder identity stable", () => {
    const initial = params({ instanceId: 1 })
    const { result, rerender } = renderHook((p: UseColumnDndParams) => useColumnDnd(p), { initialProps: initial })
    const setColumnOrder = result.current.setColumnOrder
    expect(result.current.sensors).toHaveLength(2) // MouseSensor + TouchSensor

    rerender(params({ instanceId: 1 }))

    // setColumnOrder is a raw useState setter -> stable, which is what keeps onDragEnd stable.
    expect(result.current.setColumnOrder).toBe(setColumnOrder)
  })

  it("bails out when there is no over target or active equals over", () => {
    const { result } = renderHook(() => useColumnDnd(params({ instanceId: 2 })))
    const before = result.current.columnOrder

    act(() => result.current.onDragEnd(dragEvent("a", null)))
    act(() => result.current.onDragEnd(dragEvent("a", "a")))

    expect(result.current.columnOrder).toEqual(before)
  })

  it("reorders columnOrder via reorderColumns on a valid drop", () => {
    const { result } = renderHook(() => useColumnDnd(params({ instanceId: 3 })))

    act(() => result.current.onDragEnd(dragEvent("a", "c")))

    expect(result.current.columnOrder).toEqual(["b", "c", "a"])
  })

  it("reads the latest leaf-column ids at drag time (stable handler, latest-ref)", () => {
    const { result, rerender } = renderHook((p: UseColumnDndParams) => useColumnDnd(p), {
      initialProps: params({ instanceId: 4, defaultColumnOrder: ["a", "b"], getLeafColumnIds: () => ["a", "b"] }),
    })
    const onDragEnd = result.current.onDragEnd

    // A new column "c" becomes visible in the table on a later render.
    rerender(params({ instanceId: 4, defaultColumnOrder: ["a", "b"], getLeafColumnIds: () => ["a", "b", "c"] }))
    expect(result.current.onDragEnd).toBe(onDragEnd) // handler identity stays stable

    act(() => result.current.onDragEnd(dragEvent("c", "a")))

    // "c" is known only via the latest leaf ids; it is normalized in and moved to a's slot.
    expect(result.current.columnOrder).toEqual(["c", "a", "b"])
  })
})
