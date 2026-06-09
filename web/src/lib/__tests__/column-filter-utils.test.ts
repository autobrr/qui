/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import {
  type ColumnFilter,
  columnFilterToExpr,
  columnFiltersToExpr,
  convertSizeToBytes,
  filterSearchResult,
  getColumnType,
  getDefaultOperation,
  getOperations
} from "@/lib/column-filter-utils"
import type { TorznabSearchResult } from "@/types"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

let warnSpy: ReturnType<typeof vi.spyOn>

beforeEach(() => {
  warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {})
})

afterEach(() => {
  warnSpy.mockRestore()
})

// Intent: binary (1024-based) byte conversion. Catches anyone who switches
// to decimal (1000-based) units, which would silently misrepresent torrent
// sizes against qBittorrent's binary semantics.
describe("convertSizeToBytes", () => {
  it.each([
    ["B", 1, 1],
    ["KiB", 1, 1024],
    ["MiB", 1, 1048576],
    ["GiB", 1, 1073741824],
    ["TiB", 1, 1099511627776],
    ["B/s", 1, 1],
    ["KiB/s", 1, 1024],
    ["MiB/s", 1, 1048576],
    ["GiB/s", 1, 1073741824],
    ["TiB/s", 1, 1099511627776],
  ] as const)("converts 1 %s to %i bytes", (unit, value, expected) => {
    expect(convertSizeToBytes(value, unit)).toBe(expected)
  })

  it("floors fractional results", () => {
    expect(convertSizeToBytes(0.5, "KiB")).toBe(512)
    expect(convertSizeToBytes(1.7, "B")).toBe(1)
  })
})

// Intent: produce the exact qBittorrent expression syntax the backend
// expects. The spacing and operator characters are part of the wire
// contract — divergence breaks server-side filtering silently.
describe("columnFilterToExpr — numeric comparison ops", () => {
  it.each<[ColumnFilter, string]>([
    [{ columnId: "ratio", operation: "gt", value: "2" }, "Ratio > 2"],
    [{ columnId: "ratio", operation: "lt", value: "0.5" }, "Ratio < 0.5"],
    [{ columnId: "ratio", operation: "ge", value: "1" }, "Ratio >= 1"],
    [{ columnId: "ratio", operation: "le", value: "10" }, "Ratio <= 10"],
    [{ columnId: "ratio", operation: "eq", value: "1.5" }, "Ratio == 1.5"],
    [{ columnId: "ratio", operation: "ne", value: "0" }, "Ratio != 0"],
  ])("$operation produces $1", (filter, expected) => {
    expect(columnFilterToExpr(filter)).toBe(expected)
  })
})

// Intent: UI shows the user "10 GiB" but the backend wants bytes. Conversion
// must use the binary multipliers from convertSizeToBytes. Catches anyone
// who drops the unit conversion or passes the raw user input through.
describe("columnFilterToExpr — size unit conversion", () => {
  it.each<[ColumnFilter, string]>([
    [{ columnId: "size", operation: "gt", value: "10", sizeUnit: "GiB" }, "Size > 10737418240"],
    [{ columnId: "size", operation: "lt", value: "1", sizeUnit: "MiB" }, "Size < 1048576"],
    [{ columnId: "size", operation: "eq", value: "1024", sizeUnit: "B" }, "Size == 1024"],
    [{ columnId: "size", operation: "ge", value: "1", sizeUnit: "TiB" }, "Size >= 1099511627776"],
    [{ columnId: "total_size", operation: "gt", value: "5", sizeUnit: "GiB" }, "TotalSize > 5368709120"],
    [{ columnId: "downloaded", operation: "gt", value: "100", sizeUnit: "MiB" }, "Downloaded > 104857600"],
  ])("size column with $sizeUnit unit", (filter, expected) => {
    expect(columnFilterToExpr(filter)).toBe(expected)
  })

  it("returns null for NaN size value", () => {
    expect(columnFilterToExpr({ columnId: "size", operation: "gt", value: "abc", sizeUnit: "MiB" })).toBeNull()
  })
})

// Intent: same as size — UI displays "1 MiB/s", backend wants bytes/s.
describe("columnFilterToExpr — speed unit conversion", () => {
  it.each<[ColumnFilter, string]>([
    [{ columnId: "dlspeed", operation: "gt", value: "1", speedUnit: "MiB/s" }, "DlSpeed > 1048576"],
    [{ columnId: "upspeed", operation: "lt", value: "500", speedUnit: "KiB/s" }, "UpSpeed < 512000"],
    [{ columnId: "dl_limit", operation: "eq", value: "0", speedUnit: "B/s" }, "DlLimit == 0"],
  ])("speed column with $speedUnit unit", (filter, expected) => {
    expect(columnFilterToExpr(filter)).toBe(expected)
  })
})

// Intent: user enters "2 hours", backend wants seconds.
describe("columnFilterToExpr — duration unit conversion", () => {
  it.each<[ColumnFilter, string]>([
    [{ columnId: "eta", operation: "gt", value: "1", durationUnit: "seconds" }, "ETA > 1"],
    [{ columnId: "eta", operation: "gt", value: "5", durationUnit: "minutes" }, "ETA > 300"],
    [{ columnId: "time_active", operation: "gt", value: "2", durationUnit: "hours" }, "TimeActive > 7200"],
    [{ columnId: "seeding_time", operation: "gt", value: "7", durationUnit: "days" }, "SeedingTime > 604800"],
  ])("duration column with $durationUnit unit", (filter, expected) => {
    expect(columnFilterToExpr(filter)).toBe(expected)
  })
})

// Intent: dates → unix-seconds timestamps. The qBittorrent backend stores
// timestamps as unix-seconds; sending raw ISO strings or millisecond values
// would silently misfilter.
describe("columnFilterToExpr — date filters", () => {
  it("converts ISO date to UTC unix-seconds timestamp", () => {
    expect(columnFilterToExpr({ columnId: "added_on", operation: "gt", value: "2024-01-01" })).toBe("AddedOn > 1704067200")
  })

  it("handles all date columns", () => {
    expect(columnFilterToExpr({ columnId: "completion_on", operation: "lt", value: "2024-01-01" })).toBe("CompletionOn < 1704067200")
    expect(columnFilterToExpr({ columnId: "last_activity", operation: "ge", value: "2024-01-01" })).toBe("LastActivity >= 1704067200")
  })

  it("returns null for invalid date string", () => {
    expect(columnFilterToExpr({ columnId: "added_on", operation: "gt", value: "not-a-date" })).toBeNull()
  })

  it("respects timezone offsets in ISO dates", () => {
    // "2024-01-01T00:00:00+05:00" is 2023-12-31 19:00 UTC = 1704049200 unix-seconds.
    // Pins current behavior: new Date(...) parses the offset, the user's local
    // timezone does NOT affect the result.
    expect(columnFilterToExpr({ columnId: "added_on", operation: "gt", value: "2024-01-01T00:00:00+05:00" })).toBe("AddedOn > 1704049200")
  })
})

// Intent: range filters use `(A >= lo && A <= hi)` form, applying unit
// conversion to BOTH endpoints. Allows mixed units between endpoints
// (e.g. 1 MiB to 1 GiB).
describe("columnFilterToExpr — between operation", () => {
  it.each<[ColumnFilter, string]>([
    [
      { columnId: "size", operation: "between", value: "1", value2: "10", sizeUnit: "MiB" },
      "(Size >= 1048576 && Size <= 10485760)",
    ],
    [
      { columnId: "size", operation: "between", value: "1", value2: "1", sizeUnit: "MiB", sizeUnit2: "GiB" },
      "(Size >= 1048576 && Size <= 1073741824)",
    ],
    [
      { columnId: "dlspeed", operation: "between", value: "1", value2: "100", speedUnit: "KiB/s" },
      "(DlSpeed >= 1024 && DlSpeed <= 102400)",
    ],
    [
      { columnId: "eta", operation: "between", value: "1", value2: "5", durationUnit: "minutes" },
      "(ETA >= 60 && ETA <= 300)",
    ],
    [
      { columnId: "added_on", operation: "between", value: "2024-01-01", value2: "2024-12-31" },
      "(AddedOn >= 1704067200 && AddedOn <= 1735603200)",
    ],
    [
      { columnId: "ratio", operation: "between", value: "1", value2: "5" },
      "(Ratio >= 1 && Ratio <= 5)",
    ],
    [
      { columnId: "progress", operation: "between", value: "25", value2: "75" },
      "(Progress >= 0.25 && Progress <= 0.75)",
    ],
  ])("$columnId between", (filter, expected) => {
    expect(columnFilterToExpr(filter)).toBe(expected)
  })

  it("returns null when value2 is missing", () => {
    expect(columnFilterToExpr({ columnId: "ratio", operation: "between", value: "1" })).toBeNull()
  })

  it("returns null when between values are non-numeric", () => {
    expect(columnFilterToExpr({ columnId: "ratio", operation: "between", value: "a", value2: "b" })).toBeNull()
  })
})

// Intent: escape user input before embedding in the expression string.
// Without escaping, a torrent name containing `"` could break the expression
// parser or be exploited to inject extra clauses.
describe("columnFilterToExpr — string filters and escaping", () => {
  it.each<[ColumnFilter, string]>([
    [{ columnId: "name", operation: "contains", value: "linux" }, "Name contains \"linux\""],
    [{ columnId: "name", operation: "notContains", value: "windows" }, "Name not contains \"windows\""],
    [{ columnId: "name", operation: "startsWith", value: "ubuntu" }, "Name startsWith \"ubuntu\""],
    [{ columnId: "name", operation: "endsWith", value: ".iso" }, "Name endsWith \".iso\""],
    [{ columnId: "category", operation: "eq", value: "movies" }, "Category == \"movies\""],
    [{ columnId: "tracker", operation: "contains", value: "tracker.example.com" }, "Tracker contains \"tracker.example.com\""],
    [{ columnId: "save_path", operation: "contains", value: "/data" }, "SavePath contains \"/data\""],
  ])("$operation on $columnId", (filter, expected) => {
    expect(columnFilterToExpr(filter)).toBe(expected)
  })

  it("escapes embedded double quotes and backslashes", () => {
    expect(columnFilterToExpr({ columnId: "name", operation: "contains", value: "He said \"hi\"" })).toBe("Name contains \"He said \\\"hi\\\"\"")
    expect(columnFilterToExpr({ columnId: "name", operation: "contains", value: "C:\\path" })).toBe("Name contains \"C:\\\\path\"")
  })

  it("lowercases when caseSensitive is false", () => {
    expect(columnFilterToExpr({ columnId: "name", operation: "contains", value: "Linux", caseSensitive: false })).toBe("lower(Name) contains \"linux\"")
  })

  it("preserves case when caseSensitive is true or undefined", () => {
    expect(columnFilterToExpr({ columnId: "name", operation: "contains", value: "Linux", caseSensitive: true })).toBe("Name contains \"Linux\"")
    expect(columnFilterToExpr({ columnId: "name", operation: "contains", value: "Linux" })).toBe("Name contains \"Linux\"")
  })
})

// Intent: the FilterSidebar shows categories like "Seeding" / "Downloading",
// which map to MULTIPLE qBittorrent state strings. Equal-to filters expand
// into an OR chain so any state in the category matches. Order matches
// STATE_CATEGORY_MAP and the exact format is the wire contract.
describe("columnFilterToExpr — state expansion (eq → OR)", () => {
  it("expands 'uploading' to all seeding states", () => {
    expect(columnFilterToExpr({ columnId: "state", operation: "eq", value: "uploading" })).toBe(
      "(string(State) == \"uploading\" || string(State) == \"stalledUP\" || string(State) == \"queuedUP\" || string(State) == \"checkingUP\" || string(State) == \"forcedUP\")"
    )
  })

  it("expands 'downloading' to all downloading states", () => {
    expect(columnFilterToExpr({ columnId: "state", operation: "eq", value: "downloading" })).toBe(
      "(string(State) == \"downloading\" || string(State) == \"stalledDL\" || string(State) == \"metaDL\" || string(State) == \"queuedDL\" || string(State) == \"allocating\" || string(State) == \"checkingDL\" || string(State) == \"forcedDL\")"
    )
  })

  it("expands 'paused' / 'stopped' / 'errored' state categories", () => {
    expect(columnFilterToExpr({ columnId: "state", operation: "eq", value: "stopped" })).toBe(
      "(string(State) == \"stoppedDL\" || string(State) == \"stoppedUP\")"
    )
    expect(columnFilterToExpr({ columnId: "state", operation: "eq", value: "errored" })).toBe(
      "(string(State) == \"error\" || string(State) == \"missingFiles\")"
    )
    expect(columnFilterToExpr({ columnId: "state", operation: "eq", value: "moving" })).toBe(
      "(string(State) == \"moving\")"
    )
  })
})

// Intent (#1925-class): not-equal must use && to EXCLUDE every state in
// the category. If someone naively reuses the OR joiner from eq, ne
// becomes "matches if it's not state X OR not state Y" which is always
// true — silently breaking the filter.
describe("columnFilterToExpr — state expansion (ne → AND)", () => {
  it("uses && to exclude all states in the category", () => {
    expect(columnFilterToExpr({ columnId: "state", operation: "ne", value: "uploading" })).toBe(
      "(string(State) != \"uploading\" && string(State) != \"stalledUP\" && string(State) != \"queuedUP\" && string(State) != \"checkingUP\" && string(State) != \"forcedUP\")"
    )
  })
})

// Intent: "completed" isn't a qBittorrent state — it's progress == 1.
// Mapping it as if it were a state would produce a filter that matches
// nothing. Catches anyone who adds "completed" to STATE_CATEGORY_MAP.
describe("columnFilterToExpr — completed special case", () => {
  it("eq 'completed' uses Progress == 1, not state expansion", () => {
    expect(columnFilterToExpr({ columnId: "state", operation: "eq", value: "completed" })).toBe("Progress == 1")
  })

  it("ne 'completed' uses Progress != 1", () => {
    expect(columnFilterToExpr({ columnId: "state", operation: "ne", value: "completed" })).toBe("Progress != 1")
  })
})

// Intent: only eq/ne use category expansion. Other operators (contains,
// startsWith, etc.) treat the value literally so power users can match
// specific qBittorrent state strings.
describe("columnFilterToExpr — state with non-eq/ne falls through to string handling", () => {
  it("contains operation treats state value literally", () => {
    expect(columnFilterToExpr({ columnId: "state", operation: "contains", value: "down" })).toBe("string(State) contains \"down\"")
  })

  it("unknown state value with eq falls through to literal match", () => {
    expect(columnFilterToExpr({ columnId: "state", operation: "eq", value: "not_a_category" })).toBe("string(State) == \"not_a_category\"")
  })
})

// Intent: boolean values come from the UI as strings ("true" / "false"),
// possibly capitalized. They must serialize to JSON-style booleans
// (lowercase) and tolerate any input casing.
describe("columnFilterToExpr — boolean filters", () => {
  it.each<[ColumnFilter, string]>([
    [{ columnId: "private", operation: "eq", value: "true" }, "Private == true"],
    [{ columnId: "private", operation: "eq", value: "false" }, "Private == false"],
    [{ columnId: "private", operation: "ne", value: "true" }, "Private != true"],
    [{ columnId: "private", operation: "eq", value: "TRUE" }, "Private == true"],
  ])("$value", (filter, expected) => {
    expect(columnFilterToExpr(filter)).toBe(expected)
  })
})

// Intent: UI shows progress as a percentage (0–100) but the backend stores
// it as a fraction (0–1). Catches anyone who removes the /100 conversion.
describe("columnFilterToExpr — progress percent → fraction", () => {
  it.each<[ColumnFilter, string]>([
    [{ columnId: "progress", operation: "gt", value: "50" }, "Progress > 0.5"],
    [{ columnId: "progress", operation: "lt", value: "100" }, "Progress < 1"],
    [{ columnId: "progress", operation: "ge", value: "0" }, "Progress >= 0"],
    [{ columnId: "progress", operation: "eq", value: "5" }, "Progress == 0.05"],
  ])("$value%", (filter, expected) => {
    expect(columnFilterToExpr(filter)).toBe(expected)
  })

  it("returns null for NaN progress value", () => {
    expect(columnFilterToExpr({ columnId: "progress", operation: "gt", value: "abc" })).toBeNull()
  })
})

// Intent: the visible column "Seeds" is qBittorrent's connected-peer count.
// When *filtering* we want total seeders/leechers (NumComplete / NumIncomplete)
// to match the sorting behavior. Catches anyone who removes FILTER_COLUMN_REMAP
// and silently changes filter semantics.
describe("columnFilterToExpr — column remapping (num_seeds → num_complete)", () => {
  it("remaps num_seeds to NumComplete (total, not connected)", () => {
    expect(columnFilterToExpr({ columnId: "num_seeds", operation: "gt", value: "5" })).toBe("NumComplete > 5")
  })

  it("remaps num_leechs to NumIncomplete", () => {
    expect(columnFilterToExpr({ columnId: "num_leechs", operation: "gt", value: "5" })).toBe("NumIncomplete > 5")
  })
})

// Intent: bad input never reaches the backend as a partial / corrupt
// expression. Unknown columns / operations log a warning and return null
// so callers (e.g. columnFiltersToExpr) can drop the bad filter cleanly.
describe("columnFilterToExpr — edge cases", () => {
  it("returns null and warns on unknown columnId", () => {
    expect(columnFilterToExpr({ columnId: "nope", operation: "gt", value: "1" })).toBeNull()
    expect(warnSpy).toHaveBeenCalledWith(expect.stringContaining("Unknown column ID"))
  })

  it("returns null and warns on unknown operation", () => {
    expect(columnFilterToExpr({ columnId: "ratio", operation: "nope" as never, value: "1" })).toBeNull()
    expect(warnSpy).toHaveBeenCalledWith(expect.stringContaining("Unknown operation"))
  })

  it("treats empty value as a literal empty-string match on string columns", () => {
    // Pins current behavior: empty value isn't a UX-level "no filter" signal —
    // it produces a real expression matching torrents whose field equals "".
    // If the UI wants empty-as-no-filter, it must skip the filter at the call
    // site, not rely on this layer.
    expect(columnFilterToExpr({ columnId: "name", operation: "contains", value: "" })).toBe("Name contains \"\"")
  })
})

// Intent: combine multiple column filters into a single expression. The
// AND default mirrors the user's mental model ("I'm narrowing the list").
// Filters that fail individually (return null) are dropped silently rather
// than aborting the whole filter set.
describe("columnFiltersToExpr", () => {
  it("returns null for empty filter list", () => {
    expect(columnFiltersToExpr([])).toBeNull()
  })

  it("returns single expression unchanged for single filter", () => {
    expect(columnFiltersToExpr([{ columnId: "ratio", operation: "gt", value: "2" }])).toBe("Ratio > 2")
  })

  it("AND-combines multiple filters with default operator", () => {
    expect(
      columnFiltersToExpr([
        { columnId: "ratio", operation: "gt", value: "2" },
        { columnId: "state", operation: "eq", value: "completed" },
      ])
    ).toBe("Ratio > 2 and Progress == 1")
  })

  it("supports custom operator", () => {
    expect(
      columnFiltersToExpr(
        [
          { columnId: "ratio", operation: "gt", value: "2" },
          { columnId: "ratio", operation: "lt", value: "10" },
        ],
        "or"
      )
    ).toBe("Ratio > 2 or Ratio < 10")
  })

  it("drops filters that fail to convert and keeps the rest", () => {
    expect(
      columnFiltersToExpr([
        { columnId: "ratio", operation: "gt", value: "2" },
        { columnId: "unknown", operation: "gt", value: "1" },
      ])
    ).toBe("Ratio > 2")
  })

  it("returns null when all filters fail to convert", () => {
    expect(
      columnFiltersToExpr([
        { columnId: "unknown_a", operation: "gt", value: "1" },
        { columnId: "unknown_b", operation: "gt", value: "1" },
      ])
    ).toBeNull()
  })
})

// Intent: type classification drives which filter UI is shown (size picker,
// date picker, etc.) and which operations are valid. Default to "string" so
// unknown columns get the broadest UI rather than an empty operation list.
describe("getColumnType", () => {
  it.each([
    ["ratio", "number"],
    ["size", "size"],
    ["dlspeed", "speed"],
    ["eta", "duration"],
    ["progress", "percentage"],
    ["added_on", "date"],
    ["private", "boolean"],
    ["state", "enum"],
    ["name", "string"],
    ["unknown", "string"],
  ] as const)("classifies %s as %s", (columnId, expected) => {
    expect(getColumnType(columnId)).toBe(expected)
  })
})

// Intent: when a user opens the filter UI for a column, prefill the most
// common operation for that type. Numeric → "greater than", enum/bool →
// "equals", string → "contains".
describe("getDefaultOperation", () => {
  it.each([
    ["size", "gt"],
    ["speed", "gt"],
    ["duration", "gt"],
    ["percentage", "gt"],
    ["number", "gt"],
    ["date", "gt"],
    ["enum", "eq"],
    ["boolean", "eq"],
    ["string", "contains"],
  ] as const)("%s defaults to %s", (type, expected) => {
    expect(getDefaultOperation(type)).toBe(expected)
  })
})

// Intent: each column type exposes only the operations that make sense
// (e.g. dates don't support contains/startsWith). Catches anyone who
// fans out wider operation lists than the UI can handle.
describe("getOperations", () => {
  it("returns NUMERIC_OPERATIONS for numeric-family types", () => {
    expect(getOperations("size").map(o => o.value)).toContain("between")
    expect(getOperations("speed").map(o => o.value)).toContain("gt")
    expect(getOperations("number").map(o => o.value)).toContain("ne")
  })

  it("returns DATE_OPERATIONS for date type", () => {
    const ops = getOperations("date").map(o => o.value)
    expect(ops).toContain("between")
    expect(ops).not.toContain("contains")
  })

  it("returns BOOLEAN_OPERATIONS for enum and boolean", () => {
    expect(getOperations("enum").map(o => o.value)).toEqual(["eq", "ne"])
    expect(getOperations("boolean").map(o => o.value)).toEqual(["eq", "ne"])
  })

  it("returns STRING_OPERATIONS for string type", () => {
    expect(getOperations("string").map(o => o.value)).toContain("contains")
    expect(getOperations("string").map(o => o.value)).toContain("startsWith")
  })
})

// Intent: TorznabSearchResult filtering happens client-side (no backend
// expression involved). Unknown columns and missing data should never
// hide a result the user might want to see — except where the schema
// guarantees the field exists.
describe("filterSearchResult", () => {
  const baseResult: TorznabSearchResult = {
    title: "Ubuntu Linux ISO",
    indexer: "test",
    size: 1073741824,
    seeders: 100,
    leechers: 5,
    publishDate: "2024-06-15T00:00:00Z",
    downloadVolumeFactor: 0,
    uploadVolumeFactor: 1,
    categoryId: 1000,
    categoryName: "Movies",
    downloadUrl: "",
    guid: "test-1",
    source: "scene",
    collection: "linux-distros",
    group: "RARBG",
  } as TorznabSearchResult

  const categoryMap = new Map<number, string>([[1000, "Movies"]])

  it("returns true for unknown columnId", () => {
    expect(filterSearchResult(baseResult, { columnId: "unknown", operation: "eq", value: "x" }, categoryMap)).toBe(true)
  })

  it("filters by size with unit conversion", () => {
    expect(filterSearchResult(baseResult, { columnId: "size", operation: "gt", value: "500", sizeUnit: "MiB" }, categoryMap)).toBe(true)
    expect(filterSearchResult(baseResult, { columnId: "size", operation: "lt", value: "100", sizeUnit: "MiB" }, categoryMap)).toBe(false)
  })

  it("filters seeders by numeric comparison", () => {
    expect(filterSearchResult(baseResult, { columnId: "seeders", operation: "ge", value: "100" }, categoryMap)).toBe(true)
    expect(filterSearchResult(baseResult, { columnId: "seeders", operation: "gt", value: "100" }, categoryMap)).toBe(false)
  })

  it("filters seeders with between operation", () => {
    expect(filterSearchResult(baseResult, { columnId: "seeders", operation: "between", value: "50", value2: "200" }, categoryMap)).toBe(true)
    expect(filterSearchResult(baseResult, { columnId: "seeders", operation: "between", value: "200", value2: "500" }, categoryMap)).toBe(false)
  })

  it("filters by published date (gt, lt, eq, between)", () => {
    expect(filterSearchResult(baseResult, { columnId: "published", operation: "gt", value: "2024-01-01" }, categoryMap)).toBe(true)
    expect(filterSearchResult(baseResult, { columnId: "published", operation: "lt", value: "2024-01-01" }, categoryMap)).toBe(false)
    expect(filterSearchResult(baseResult, { columnId: "published", operation: "eq", value: "2024-06-15" }, categoryMap)).toBe(true)
    expect(filterSearchResult(baseResult, { columnId: "published", operation: "between", value: "2024-01-01", value2: "2024-12-31" }, categoryMap)).toBe(true)
  })

  it("filters freeleech via downloadVolumeFactor", () => {
    expect(filterSearchResult(baseResult, { columnId: "freeleech", operation: "eq", value: "true" }, categoryMap)).toBe(true)
    expect(filterSearchResult({ ...baseResult, downloadVolumeFactor: 1 }, { columnId: "freeleech", operation: "eq", value: "true" }, categoryMap)).toBe(false)
    expect(filterSearchResult({ ...baseResult, downloadVolumeFactor: 0.5 }, { columnId: "freeleech", operation: "eq", value: "0.5" }, categoryMap)).toBe(true)
  })

  it("returns true when freeleech filter has no selected values", () => {
    expect(filterSearchResult(baseResult, { columnId: "freeleech", operation: "eq", value: "" }, categoryMap)).toBe(true)
  })

  it("rejects freeleech rows when the filter value matches none of the known patterns", () => {
    // Intent: unknown freeleech values (not "true"/"false" and not a numeric
    // factor) hide the row. Catches anyone who flips the default to "match"
    // and silently shows all results when an unrecognized filter is selected.
    expect(filterSearchResult(baseResult, { columnId: "freeleech", operation: "eq", value: "unknown" }, categoryMap)).toBe(false)
  })

  it("string contains is case-insensitive by default", () => {
    expect(filterSearchResult(baseResult, { columnId: "title", operation: "contains", value: "UBUNTU" }, categoryMap)).toBe(true)
  })

  it("string contains respects caseSensitive flag", () => {
    expect(filterSearchResult(baseResult, { columnId: "title", operation: "contains", value: "UBUNTU", caseSensitive: true }, categoryMap)).toBe(false)
    expect(filterSearchResult(baseResult, { columnId: "title", operation: "contains", value: "Ubuntu", caseSensitive: true }, categoryMap)).toBe(true)
  })

  it("supports notContains / startsWith / endsWith", () => {
    expect(filterSearchResult(baseResult, { columnId: "title", operation: "notContains", value: "windows" }, categoryMap)).toBe(true)
    expect(filterSearchResult(baseResult, { columnId: "title", operation: "startsWith", value: "ubuntu" }, categoryMap)).toBe(true)
    expect(filterSearchResult(baseResult, { columnId: "title", operation: "endsWith", value: "iso" }, categoryMap)).toBe(true)
  })

  it("multi-value (CSV) filter matches any value with eq/contains", () => {
    expect(filterSearchResult(baseResult, { columnId: "title", operation: "contains", value: "windows,linux" }, categoryMap)).toBe(true)
    expect(filterSearchResult(baseResult, { columnId: "indexer", operation: "eq", value: "other,test" }, categoryMap)).toBe(true)
    expect(filterSearchResult(baseResult, { columnId: "indexer", operation: "eq", value: "other,nope" }, categoryMap)).toBe(false)
  })

  it("resolves category via categoryMap with fallback to categoryName", () => {
    expect(filterSearchResult(baseResult, { columnId: "category", operation: "eq", value: "Movies" }, categoryMap)).toBe(true)
    expect(filterSearchResult({ ...baseResult, categoryId: 999 }, { columnId: "category", operation: "eq", value: "Movies" }, new Map())).toBe(true)
    expect(
      filterSearchResult(
        { ...baseResult, categoryId: 999, categoryName: undefined as unknown as string },
        { columnId: "category", operation: "eq", value: "Movies" },
        new Map()
      )
    ).toBe(false)
  })

  it("returns false when itemValue is undefined", () => {
    expect(filterSearchResult({ ...baseResult, seeders: undefined as unknown as number }, { columnId: "seeders", operation: "gt", value: "1" }, categoryMap)).toBe(false)
    expect(filterSearchResult({ ...baseResult, title: undefined as unknown as string }, { columnId: "title", operation: "contains", value: "x" }, categoryMap)).toBe(false)
  })
})
