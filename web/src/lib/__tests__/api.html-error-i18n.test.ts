/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { HttpResponse, http } from "msw"
import { afterEach, describe, expect, it } from "vitest"

import { changeLanguage } from "@/i18n"
import deCommon from "@/i18n/locales/de/common.json"
import { api } from "@/lib/api"
import { server } from "@/test/msw/server"

// api.export.test.ts covers the same path with i18next never initialised.
afterEach(async () => {
  await changeLanguage("en")
})

describe("HTML error body message", () => {
  it("is translated once i18n is initialised", async () => {
    await changeLanguage("de")
    server.use(
      http.get("*/api/instances/:instanceId/torrents/:encodedHash/export", () =>
        new HttpResponse("<html><body>Bad Gateway</body></html>", {
          status: 502,
          headers: { "Content-Type": "text/html" },
        })
      )
    )

    const thrown = await api.exportTorrent(1, "deadbeef").catch((err: unknown) => err)

    expect((thrown as Error).message).toBe(
      deCommon.apiErrors.htmlErrorPage.replace("{{message}}", "HTTP error! status: 502")
    )
  })
})
