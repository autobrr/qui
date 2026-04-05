import test from "node:test"
import assert from "node:assert/strict"

import {
  getNextBlocklistInstanceId,
  getRunDiscoveredFiles,
  hasDirScanStatusStats,
  shouldShowRunFileDetails
} from "./crossSeedUiState.ts"

test("getNextBlocklistInstanceId falls back to the first active instance when selection disappears", () => {
  const nextInstanceId = getNextBlocklistInstanceId(
    [{ id: 11 }, { id: 22 }],
    99
  )

  assert.equal(nextInstanceId, 11)
})

test("getNextBlocklistInstanceId clears selection when no instances remain", () => {
  const nextInstanceId = getNextBlocklistInstanceId([], 99)

  assert.equal(nextInstanceId, null)
})

test("hasDirScanStatusStats treats skipped-only runs as meaningful work", () => {
  const hasStats = hasDirScanStatusStats({
    filesFound: 0,
    filesSkipped: 3,
    matchesFound: 0,
    torrentsAdded: 0,
  })

  assert.equal(hasStats, true)
})

test("run file helpers include skipped files in discovered counts and detail visibility", () => {
  const run = {
    filesFound: 2,
    filesSkipped: 3,
    matchesFound: 0,
    torrentsAdded: 0,
  }

  assert.equal(getRunDiscoveredFiles(run), 5)
  assert.equal(shouldShowRunFileDetails(run), true)
})
