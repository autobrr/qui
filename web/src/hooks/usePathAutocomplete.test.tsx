/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { act, cleanup, renderHook } from "@testing-library/react"
import type { KeyboardEvent } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { usePathAutocomplete } from "./usePathAutocomplete"

const { requestedPaths } = vi.hoisted(() => ({ requestedPaths: [] as string[] }))

vi.mock("./useDirectoryContent", () => {
  const listings: Record<string, { dirs: string[]; files: string[] }> = {
    "/data/": { dirs: ["/data/alpha", "/data/alps", "/data/beta"], files: ["/data/album.flac"] },
    "/data/alpha/": { dirs: ["/data/alpha/one"], files: [] },
    "/music/": { dirs: ["/music/ac\\dc", "/music/abba"], files: [] },
    "/flat/": { dirs: [], files: Array.from({ length: 250 }, (_, i) => `/flat/${String(i).padStart(3, "0")}.jpg`) },
    "C:\\data\\": { dirs: ["C:\\data\\alpha", "C:\\data\\alps"], files: ["C:\\data\\album.flac"] },
    "C:\\data\\alpha\\": { dirs: ["C:\\data\\alpha\\one"], files: [] },
  }
  const results = new Map<string, { data: string[] }>()
  return {
    useDirectoryContent: (_id: number, path: string, options: { mode?: string; enabled?: boolean }) => {
      // A disabled query has no data, as in React Query.
      if (options.enabled === false) return { data: undefined }
      const key = `${options.mode ?? "dirs"}:${path}`
      if (path) requestedPaths.push(path)
      let result = results.get(key)
      if (!result) {
        result = { data: listings[path]?.[options.mode === "files" ? "files" : "dirs"] ?? [] }
        results.set(key, result)
      }
      return result
    },
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
    requestedPaths.length = 0
  })

  it("lists directories only by default", () => {
    const { result } = renderHook(() => usePathAutocomplete(vi.fn(), 1))
    act(() => result.current.handleInputChange("/data/al"))
    expect(result.current.suggestions).toEqual(["/data/alpha", "/data/alps"])
  })

  it("matches the start of the last segment, not the parent path or the middle of a name", () => {
    const { result } = renderHook(() => usePathAutocomplete(vi.fn(), 1, { includeFiles: true }))
    act(() => result.current.handleInputChange("/data/a"))
    expect(result.current.suggestions).toEqual(["/data/alpha", "/data/alps", "/data/album.flac"])
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

  it("treats a backslash inside a POSIX path as part of the name, not a separator", () => {
    const onSelect = vi.fn()
    const { result } = renderHook(() => usePathAutocomplete(onSelect, 1, { includeFiles: true }))
    mountRefs(result)
    act(() => result.current.handleInputChange("/music/ac\\d"))
    expect(requestedPaths).toContain("/music/")
    expect(requestedPaths).not.toContain("/music/ac\\")
    expect(result.current.suggestions).toEqual(["/music/ac\\dc"])
    act(() => result.current.handleSelect("/music/ac\\dc"))
    expect(onSelect).toHaveBeenCalledWith("/music/ac\\dc/")
  })

  it("caps the list at 100 entries", () => {
    const { result } = renderHook(() => usePathAutocomplete(vi.fn(), 1, { includeFiles: true }))
    act(() => result.current.handleInputChange("/flat/"))
    expect(result.current.suggestions).toHaveLength(100)
    act(() => result.current.handleInputChange("/flat/2"))
    expect(result.current.suggestions).toHaveLength(50)
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

describe("usePathAutocomplete Windows paths", () => {
  afterEach(() => {
    cleanup()
    document.body.replaceChildren()
    requestedPaths.length = 0
  })

  it("asks for the backslash parent and filters on the last segment", () => {
    const { result } = renderHook(() => usePathAutocomplete(vi.fn(), 1, { includeFiles: true }))
    act(() => result.current.handleInputChange("C:\\data\\alp"))
    expect(requestedPaths).toContain("C:\\data\\")
    expect(requestedPaths).not.toContain("/")
    expect(result.current.suggestions).toEqual(["C:\\data\\alpha", "C:\\data\\alps"])
  })

  it("selecting a directory appends a backslash, selecting a file does not", () => {
    const onSelect = vi.fn()
    const { result } = renderHook(() => usePathAutocomplete(onSelect, 1, { includeFiles: true }))
    mountRefs(result)
    act(() => result.current.handleInputChange("C:\\data\\al"))
    act(() => result.current.handleSelect("C:\\data\\alpha"))
    expect(onSelect).toHaveBeenLastCalledWith("C:\\data\\alpha\\")
    expect(result.current.showSuggestions).toBe(true)
    act(() => result.current.handleInputChange("C:\\data\\al"))
    act(() => result.current.handleSelect("C:\\data\\album.flac"))
    expect(onSelect).toHaveBeenLastCalledWith("C:\\data\\album.flac")
    expect(result.current.showSuggestions).toBe(false)
  })
})

function press(result: { current: ReturnType<typeof usePathAutocomplete> }, key: string) {
  act(() => result.current.handleKeyDown({ key, preventDefault: vi.fn() } as unknown as KeyboardEvent<HTMLInputElement>))
}

describe("usePathAutocomplete keyboard navigation", () => {
  afterEach(() => {
    cleanup()
    document.body.replaceChildren()
    requestedPaths.length = 0
  })

  // regression: #3204
  it.each([false, true])("arrow keys move and wrap the highlight with includeFiles %s", (includeFiles) => {
    const onSelect = vi.fn()
    const { result } = renderHook(() => usePathAutocomplete(onSelect, 1, { includeFiles }))
    mountRefs(result)
    act(() => result.current.handleInputChange("/data/al"))
    expect(result.current.highlightedIndex).toBe(0)

    press(result, "ArrowDown")
    expect(result.current.highlightedIndex).toBe(1)

    press(result, "ArrowUp")
    press(result, "ArrowUp")
    expect(result.current.highlightedIndex).toBe(result.current.suggestions.length - 1)

    press(result, "ArrowDown")
    press(result, "ArrowDown")
    expect(result.current.highlightedIndex).toBe(1)
    press(result, "Enter")
    expect(onSelect).toHaveBeenCalledWith("/data/alps/")
  })

  // regression: #3204
  it("typing new input resets the highlight to the first suggestion", () => {
    const { result } = renderHook(() => usePathAutocomplete(vi.fn(), 1))
    mountRefs(result)
    act(() => result.current.handleInputChange("/data/"))
    press(result, "ArrowDown")
    expect(result.current.highlightedIndex).toBe(1)

    act(() => result.current.handleInputChange("/data/al"))
    expect(result.current.highlightedIndex).toBe(0)
  })

  it("a case-only input change keeps a suggestion highlighted for Enter", () => {
    const onSelect = vi.fn()
    const { result } = renderHook(() => usePathAutocomplete(onSelect, 1))
    mountRefs(result)
    act(() => result.current.handleInputChange("/data/al"))
    act(() => result.current.handleInputChange("/data/AL"))
    expect(result.current.highlightedIndex).toBe(0)
    press(result, "Enter")
    expect(onSelect).toHaveBeenCalledWith("/data/alpha/")
  })

  it("an edit that the deferred input skips keeps a suggestion highlighted", () => {
    const { result } = renderHook(() => usePathAutocomplete(vi.fn(), 1))
    mountRefs(result)
    act(() => result.current.handleInputChange("/data/al"))
    // Type and delete a character in one render, so the deferred input never changes.
    act(() => {
      result.current.handleInputChange("/data/alp")
      result.current.handleInputChange("/data/al")
    })
    expect(result.current.highlightedIndex).toBe(0)
  })
})
