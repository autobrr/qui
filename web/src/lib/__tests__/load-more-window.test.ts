/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"

import { EMPTY_LOAD_MORE_WINDOW, loadMoreWindowReducer, type LoadMoreWindow, type LoadMoreWindowEvent } from "@/lib/load-more-window"

function run(events: LoadMoreWindowEvent[], state: LoadMoreWindow = EMPTY_LOAD_MORE_WINDOW): LoadMoreWindow {
  return events.reduce(loadMoreWindowReducer, state)
}

// 1,000 rows in memory, and the user scrolled to row 400.
const scrolled = run([
  { type: "dataArrived", length: 1000 },
  { type: "nearEnd", canLoadPage: true },
  { type: "nearEnd", canLoadPage: true },
  { type: "nearEnd", canLoadPage: true },
])

describe("loadMoreWindowReducer", () => {
  it("shows the first 100 rows when the first data arrives", () => {
    expect(run([{ type: "dataArrived", length: 1000 }]).loadedRows).toBe(100)
    expect(run([{ type: "dataArrived", length: 30 }]).loadedRows).toBe(30)
  })

  it("starts again at the first 100 rows when the user changes the filters, search or sort", () => {
    expect(scrolled.loadedRows).toBe(400)
    expect(loadMoreWindowReducer(scrolled, { type: "queryChanged" }).loadedRows).toBe(100)
  })

  it("keeps the loaded rows when a data update changes the length", () => {
    expect(loadMoreWindowReducer(scrolled, { type: "dataArrived", length: 1003 }).loadedRows).toBe(400)
  })

  it("never keeps more loaded rows than the list has", () => {
    const shrunk = loadMoreWindowReducer(scrolled, { type: "dataArrived", length: 250 })
    expect(shrunk.loadedRows).toBe(250)
    expect(loadMoreWindowReducer(scrolled, { type: "dataArrived", length: 0 }).loadedRows).toBe(0)
  })

  it("shows the next 100 rows from memory near the end, and asks for no server page", () => {
    const next = loadMoreWindowReducer(scrolled, { type: "nearEnd", canLoadPage: true })
    expect(next.loadedRows).toBe(500)
    expect(next.pageRequested).toBe(false)
  })

  it("shows the rest of the rows in memory", () => {
    const almost = run([{ type: "dataArrived", length: 150 }])
    expect(loadMoreWindowReducer(almost, { type: "nearEnd", canLoadPage: false }).loadedRows).toBe(150)
  })

  it("asks for a server page near the end of the rows in memory, and shows its rows when it loads", () => {
    const atEnd = run([{ type: "dataArrived", length: 100 }])
    const requested = loadMoreWindowReducer(atEnd, { type: "nearEnd", canLoadPage: true })
    expect(requested.pageRequested).toBe(true)
    expect(requested.loadedRows).toBe(100)

    const loaded = run([{ type: "dataArrived", length: 200 }, { type: "pageLoaded" }], requested)
    expect(loaded.loadedRows).toBe(200)
  })

  it("keeps the request when a page brings no rows", () => {
    const requested = run([{ type: "dataArrived", length: 100 }, { type: "nearEnd", canLoadPage: true }])
    expect(loadMoreWindowReducer(requested, { type: "pageLoaded" })).toBe(requested)
  })

  it("does nothing at the end of the data", () => {
    const atEnd = run([{ type: "dataArrived", length: 100 }])
    expect(loadMoreWindowReducer(atEnd, { type: "nearEnd", canLoadPage: false })).toBe(atEnd)
  })

  it("ignores a page that loads after the user changed the filters", () => {
    const atEnd = run([{ type: "dataArrived", length: 100 }])
    const reset = run([{ type: "nearEnd", canLoadPage: true }, { type: "queryChanged" }, { type: "dataArrived", length: 200 }, { type: "pageLoaded" }], atEnd)
    expect(reset.loadedRows).toBe(100)
  })

  it("ignores a page that the window did not ask for", () => {
    expect(loadMoreWindowReducer(scrolled, { type: "pageLoaded" })).toBe(scrolled)
  })
})
