/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"
import { accessByArea, PERMISSION_AREAS, summarizePermissions, type ApiKeyPermission, type PermissionArea } from "../api-key-permissions"

const everything: ApiKeyPermission[] = PERMISSION_AREAS.flatMap(({ area, levels }) => levels.includes("write")
  ? [{ area, level: "write" as const }]
  : levels.map(level => ({ area, level })))

const without = (...areas: PermissionArea[]) => everything.filter(({ area }) => !areas.includes(area))

describe("summarizePermissions", () => {
  it("names no exception for a key at the top level of each area", () => {
    expect(summarizePermissions(everything)).toEqual({ except: [], rows: [] })
  })

  it("names the areas that a nearly full key lacks", () => {
    expect(summarizePermissions(without("crossSeed", "dirScan", "settings"))).toEqual({
      except: [
        { area: "crossSeed", levels: [] },
        { area: "dirScan", levels: [] },
        { area: "settings", levels: [] },
      ],
      rows: [],
    })
  })

  it("counts an area below its top level as an exception, and shows the access that it has", () => {
    expect(summarizePermissions([...without("settings"), { area: "settings", level: "read" }])).toEqual({
      except: [{ area: "settings", levels: ["read"] }],
      rows: [],
    })
    expect(summarizePermissions(everything.filter(p => p.area !== "externalPrograms" || p.level !== "run"))).toEqual({
      except: [{ area: "externalPrograms", levels: ["read"] }],
      rows: [],
    })
  })

  it("lists a key that lacks more than three areas by level, strongest first", () => {
    const summary = summarizePermissions(without("crossSeed", "dirScan", "settings", "logs"))
    expect(summary.except).toBeNull()
    expect(summary.rows.map(({ level }) => level)).toEqual(["write", "run", "read"])
    expect(summary.rows[0].areas).not.toContain("logs")
  })

  it("does not repeat an area at the levels that write includes", () => {
    const { rows } = summarizePermissions([
      { area: "torrents", level: "write" },
      { area: "torrents", level: "read" },
      { area: "automations", level: "run" },
    ])
    expect(rows).toEqual([
      { level: "write", areas: ["torrents"] },
      { level: "run", areas: ["automations"] },
    ])
  })
})

describe("accessByArea", () => {
  it("puts the areas that offer no write last, so a full key shows no gap in its write blocks", () => {
    const strongest = accessByArea(everything).map(({ levels }) => levels.at(-1))
    expect(strongest).toEqual([...Array<string>(13).fill("write"), "run", "read"])
  })

  it("gives an area no levels when the key holds none, and hides what write includes", () => {
    const access = accessByArea([
      { area: "torrents", level: "write" },
      { area: "torrents", level: "read" },
      { area: "rss", level: "read" },
      { area: "rss", level: "run" },
    ])
    expect(access.find(({ area }) => area === "torrents")?.levels).toEqual(["write"])
    expect(access.find(({ area }) => area === "rss")?.levels).toEqual(["read", "run"])
    expect(access.find(({ area }) => area === "logs")?.levels).toEqual([])
  })
})
