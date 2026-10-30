/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// The load-more window of the torrent list: how many of the rows in memory the
// Layout shows. useTorrentListController holds it in a useReducer. Each event
// names who caused it, so a data update never resets the window.

export const LOAD_MORE_STEP = 100

export interface LoadMoreWindow {
  // The rows that the Layout shows. Never more than listLength.
  loadedRows: number
  // The rows in memory.
  listLength: number
  // The window asked for a server page whose rows have not arrived yet.
  pageRequested: boolean
}

export type LoadMoreWindowEvent =
  // The user changed the instance, the filters, the search or the sort.
  | { type: "queryChanged" }
  // A stream update, a poll or a server page changed the number of rows in memory.
  | { type: "dataArrived"; length: number }
  // The user reached the end of the loaded rows. canLoadPage: the server has
  // more rows and no page is loading.
  | { type: "nearEnd"; canLoadPage: boolean }
  // A server page finished loading and its rows are in memory.
  | { type: "pageLoaded" }

export const EMPTY_LOAD_MORE_WINDOW: LoadMoreWindow = {
  loadedRows: 0,
  listLength: 0,
  pageRequested: false,
}

function nextStep(state: LoadMoreWindow): number {
  return Math.min(state.loadedRows + LOAD_MORE_STEP, state.listLength)
}

export function loadMoreWindowReducer(state: LoadMoreWindow, event: LoadMoreWindowEvent): LoadMoreWindow {
  switch (event.type) {
    case "queryChanged":
      return { ...state, loadedRows: Math.min(LOAD_MORE_STEP, state.listLength), pageRequested: false }
    case "dataArrived":
      return {
        ...state,
        listLength: event.length,
        loadedRows: Math.min(Math.max(state.loadedRows, LOAD_MORE_STEP), event.length),
      }
    case "nearEnd":
      if (state.loadedRows < state.listLength) {
        return { ...state, loadedRows: nextStep(state) }
      }
      if (event.canLoadPage && !state.pageRequested) {
        return { ...state, pageRequested: true }
      }
      return state
    case "pageLoaded":
      if (!state.pageRequested || state.listLength <= state.loadedRows) {
        return state
      }
      return { ...state, pageRequested: false, loadedRows: nextStep(state) }
  }
}
