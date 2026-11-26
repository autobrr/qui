/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { afterEach, describe, expect, it } from "vitest"
import { isSpreadsheetClassicActive, isSpreadsheetDisguiseActive, SPREADSHEET_CLASSIC_THEME_ID, SPREADSHEET_THEME_ID, spreadsheetDocumentTitle, spreadsheetPostProcessor } from "./spreadsheet-disguise"

afterEach(() => {
  document.documentElement.removeAttribute("data-theme")
})

function activate(themeId: string = SPREADSHEET_THEME_ID) {
  document.documentElement.setAttribute("data-theme", themeId)
}

describe("isSpreadsheetDisguiseActive", () => {
  it("is false without the theme attribute", () => {
    expect(isSpreadsheetDisguiseActive()).toBe(false)
  })
  it("is true when data-theme is spreadsheet", () => {
    activate()
    expect(isSpreadsheetDisguiseActive()).toBe(true)
  })
  it("is true for the classic variant", () => {
    activate(SPREADSHEET_CLASSIC_THEME_ID)
    expect(isSpreadsheetDisguiseActive()).toBe(true)
  })
})

describe("isSpreadsheetClassicActive", () => {
  it("is false for the modern variant", () => {
    activate()
    expect(isSpreadsheetClassicActive()).toBe(false)
  })
  it("is true for the classic variant", () => {
    activate(SPREADSHEET_CLASSIC_THEME_ID)
    expect(isSpreadsheetClassicActive()).toBe(true)
  })
})

describe("spreadsheetDocumentTitle", () => {
  it("uses the xlsx title for the modern variant", () => {
    activate()
    expect(spreadsheetDocumentTitle()).toBe("Book1.xlsx")
  })
  it("uses the xls title for the classic variant", () => {
    activate(SPREADSHEET_CLASSIC_THEME_ID)
    expect(spreadsheetDocumentTitle()).toBe("Book1.xls")
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
