/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { afterEach, describe, expect, it } from "vitest"
import { http, HttpResponse } from "msw"
import { server } from "@/test/msw/server"
import "@/i18n"
import { api } from "@/lib/api"

const view = [
  { key: "dl_limit", value: 1024, tab: "speed", section: "globalRateLimits", control: "number", wireUnit: "B/s", displayUnit: "KiB/s" },
  { key: "save_path", value: "/data", tab: "downloads", section: "savingManagement", control: "path" },
  { key: "add_trackers_enabled", value: true, tab: "bittorrent", section: "autoAddTrackers", control: "switch", readOnly: true, reason: "pending" },
]

describe("api preferences", () => {
  afterEach(() => {
    server.resetHandlers()
  })

  it("turns the catalog view into the preferences object", async () => {
    server.use(http.get("*/api/instances/7/preferences", () => HttpResponse.json(view)))

    expect(await api.getInstancePreferences(7)).toEqual({ dl_limit: 1024, save_path: "/data", add_trackers_enabled: true })
  })

  it("sends the changed keys and returns the new view as a preferences object", async () => {
    let body: unknown
    server.use(http.patch("*/api/instances/7/preferences", async ({ request }) => {
      body = await request.json()
      return HttpResponse.json(view)
    }))

    const result = await api.updateInstancePreferences(7, { dl_limit: 1024 })

    expect(body).toEqual({ dl_limit: 1024 })
    expect(result.save_path).toBe("/data")
  })
})
