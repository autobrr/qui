/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, expect, it } from "vitest"
import type { TorznabIndexer } from "@/types"
import { deriveSavedConnections } from "./discovery-connections"

function indexer(overrides: Partial<TorznabIndexer>): TorznabIndexer {
  return {
    id: 1,
    name: "Indexer",
    base_url: "http://localhost:9696",
    indexer_id: "idx",
    backend: "prowlarr",
    enabled: true,
    priority: 0,
    timeout_seconds: 30,
    capabilities: [],
    categories: [],
    last_test_status: "",
    created_at: "",
    updated_at: "",
    ...overrides,
  }
}

describe("deriveSavedConnections", () => {
  it("groups by base url and backend, keeping the first id seen", () => {
    const connections = deriveSavedConnections([
      indexer({ id: 7, base_url: "http://localhost:9696", backend: "prowlarr" }),
      indexer({ id: 3, base_url: "http://localhost:9696", backend: "prowlarr" }),
      indexer({ id: 5, base_url: "http://localhost:9117", backend: "jackett" }),
    ])

    expect(connections).toEqual([
      { baseUrl: "http://localhost:9117", backend: "jackett", sourceIndexerId: 5, indexerCount: 1 },
      { baseUrl: "http://localhost:9696", backend: "prowlarr", sourceIndexerId: 7, indexerCount: 2 },
    ])
  })

  it("excludes native indexers", () => {
    expect(deriveSavedConnections([indexer({ backend: "native" })])).toEqual([])
  })
})
