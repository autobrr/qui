/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { afterEach, describe, expect, it } from "vitest"
import { isSpreadsheetDisguiseActive, SPREADSHEET_THEME_ID, spreadsheetPostProcessor } from "./spreadsheet-disguise"

afterEach(() => {
  document.documentElement.removeAttribute("data-theme")
})

function activate() {
  document.documentElement.setAttribute("data-theme", SPREADSHEET_THEME_ID)
}

describe("isSpreadsheetDisguiseActive", () => {
  it("is false without the theme attribute", () => {
    expect(isSpreadsheetDisguiseActive()).toBe(false)
  })
  it("is true when data-theme is spreadsheet", () => {
    activate()
    expect(isSpreadsheetDisguiseActive()).toBe(true)
  })
})

describe("spreadsheetPostProcessor", () => {
  it("passes strings through when inactive", () => {
    expect(spreadsheetPostProcessor.process("Seeds", "tableColumns.seeds", { ns: "torrents" })).toBe("Seeds")
  })
  it("overrides mapped keys when active", () => {
    activate()
    expect(spreadsheetPostProcessor.process("Seeds", "tableColumns.seeds", { ns: "torrents" })).toBe("Sources")
  })
  it("leaves unmapped keys alone when active", () => {
    activate()
    expect(spreadsheetPostProcessor.process("Delete", "actions.delete", { ns: "torrents" })).toBe("Delete")
  })
  it("interpolates override placeholders from options", () => {
    activate()
    expect(
      spreadsheetPostProcessor.process("12 of 40 torrents loaded", "statusBar.torrentsLoaded", { ns: "torrents", loaded: 12, total: 40 })
    ).toBe("12 of 40 records loaded")
  })
  it("resolves key arrays and ns arrays", () => {
    activate()
    expect(spreadsheetPostProcessor.process("Instances", ["sidebar.instances"], { ns: ["common"] })).toBe("Workbooks")
  })
})
