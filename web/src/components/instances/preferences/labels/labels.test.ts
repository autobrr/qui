/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"

import shownKeys from "@/lib/preference-catalog-keys.json"

import catalogSource from "../../../../../../internal/qbittorrent/prefcatalog/catalog.go?raw"

import { preferenceWidgets } from "../PreferenceWidgets"
import { preferenceTabLabels } from "."

// The Go source of the catalog declares each control type as `ControlX Control = "x"`.
const catalogControls = [...catalogSource.matchAll(/^\s*Control\w+\s+Control\s*=\s*"(\w+)"/gm)].map(match => match[1])

describe("preference labels", () => {
  it("has a label entry for each key that the catalog shows", () => {
    const labelled = new Set(Object.values(preferenceTabLabels).flatMap(tab => Object.keys(tab.fields)))

    expect(shownKeys.filter(key => !labelled.has(key))).toEqual([])
  })

  it("has a widget for each control type of the catalog", () => {
    expect(catalogControls.length).toBeGreaterThan(0)
    expect(catalogControls.filter(control => !(control in preferenceWidgets))).toEqual([])
  })
})
