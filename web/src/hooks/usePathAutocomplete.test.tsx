/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { act, cleanup, renderHook } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { usePathAutocomplete } from "./usePathAutocomplete"

vi.mock("./useDirectoryContent", () => {
  const dirs = { data: ["/data/alpha", "/data/alps"] }
  const files = { data: ["/data/album.flac"] }
  return {
    useDirectoryContent: (_id: number, _path: string, options: { mode?: string }) =>
      options.mode === "files" ? files : dirs,
  }
})

function mountRefs(result: { current: ReturnType<typeof usePathAutocomplete> }) {
  const input = document.createElement("input")
  const list = document.createElement("div")
  document.body.append(input, list)
  result.current.inputRef.current = input
  result.current.listRef.current = list
  return { input, list }
}

function pointerDown(target: EventTarget) {
  act(() => {
    target.dispatchEvent(new Event("pointerdown", { bubbles: true }))
  })
}

describe("usePathAutocomplete outside dismissal", () => {
  afterEach(() => {
    cleanup()
    document.body.replaceChildren()
  })

  it("dismisses on a pointer interaction outside the input and list, keeps the value", () => {
    const onSelect = vi.fn()
    const { result } = renderHook(() => usePathAutocomplete(onSelect, 1))
    mountRefs(result)

    act(() => result.current.handleInputChange("/data/al"))
    expect(result.current.showSuggestions).toBe(true)

    pointerDown(document.body)
    expect(result.current.showSuggestions).toBe(false)
    expect(result.current.inputValue).toBe("/data/al")
    expect(onSelect).not.toHaveBeenCalled()
  })

  it("keeps the list open for pointer interactions inside the input or the list", () => {
    const { result } = renderHook(() => usePathAutocomplete(vi.fn(), 1))
    const { input, list } = mountRefs(result)
    const scrollBox = document.createElement("div")
    list.append(scrollBox)

    act(() => result.current.handleInputChange("/data/al"))
    pointerDown(scrollBox)
    expect(result.current.showSuggestions).toBe(true)
    pointerDown(input)
    expect(result.current.showSuggestions).toBe(true)
  })

  it("reopens on typing after an outside dismissal", () => {
    const { result } = renderHook(() => usePathAutocomplete(vi.fn(), 1))
    mountRefs(result)

    act(() => result.current.handleInputChange("/data/al"))
    pointerDown(document.body)
    expect(result.current.showSuggestions).toBe(false)

    act(() => result.current.handleInputChange("/data/alp"))
    expect(result.current.showSuggestions).toBe(true)
  })
})

describe("usePathAutocomplete file entries", () => {
  afterEach(() => {
    cleanup()
    document.body.replaceChildren()
  })

  it("lists directories only by default", () => {
    const { result } = renderHook(() => usePathAutocomplete(vi.fn(), 1))
    act(() => result.current.handleInputChange("/data/al"))
    expect(result.current.suggestions).toEqual(["/data/alpha", "/data/alps"])
  })

  it("lists files after the directories when includeFiles is set", () => {
    const { result } = renderHook(() => usePathAutocomplete(vi.fn(), 1, { includeFiles: true }))
    act(() => result.current.handleInputChange("/data/al"))
    expect(result.current.suggestions).toEqual(["/data/alpha", "/data/alps", "/data/album.flac"])
  })

  it("selecting a directory appends a separator and keeps the list open", () => {
    const onSelect = vi.fn()
    const { result } = renderHook(() => usePathAutocomplete(onSelect, 1, { includeFiles: true }))
    mountRefs(result)
    act(() => result.current.handleInputChange("/data/al"))
    act(() => result.current.handleSelect("/data/alpha"))
    expect(onSelect).toHaveBeenCalledWith("/data/alpha/")
    expect(result.current.inputValue).toBe("/data/alpha/")
    expect(result.current.showSuggestions).toBe(true)
  })

  it("selecting a file keeps the path as is and closes the list", () => {
    const onSelect = vi.fn()
    const { result } = renderHook(() => usePathAutocomplete(onSelect, 1, { includeFiles: true }))
    mountRefs(result)
    act(() => result.current.handleInputChange("/data/al"))
    act(() => result.current.handleSelect("/data/album.flac"))
    expect(onSelect).toHaveBeenCalledWith("/data/album.flac")
    expect(result.current.inputValue).toBe("/data/album.flac")
    expect(result.current.showSuggestions).toBe(false)
  })
})
