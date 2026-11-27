/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

export const LOAD_MORE_STEP = 100

export interface LoadMoreWindow {
  loadedRows: number
  listLength: number
  pageRequested: boolean
}

export type LoadMoreWindowEvent =
  | { type: "queryChanged" }
  | { type: "dataArrived"; length: number }
  | { type: "nearEnd"; hasMorePages: boolean }
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
      if (event.hasMorePages && !state.pageRequested) {
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
