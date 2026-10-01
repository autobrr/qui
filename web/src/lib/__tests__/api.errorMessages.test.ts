/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { HttpResponse, http } from "msw"
import { afterEach, describe, expect, it } from "vitest"

import { api } from "@/lib/api"
import { server } from "@/test/msw/server"
import "@/i18n"

// api.ts reads these messages from the i18next singleton that "@/i18n" initializes,
// with no hardcoded English fallback: the English text lives in en/common.json only.

afterEach(() => {
  sessionStorage.clear()
})

describe("api error messages", () => {
  it("explains an SSO login page returned by an API endpoint", async () => {
    server.use(
      http.get("*/api/instances", () => new HttpResponse("<!doctype html><html><body>Sign in</body></html>", {
        status: 200,
        headers: { "Content-Type": "text/html" },
      }))
    )

    // Recovery navigation is attempted first and only throws once it is blocked;
    // the guard key is what a second attempt in the same tab would find.
    sessionStorage.setItem("qui_sso_recovery_attempted", "1")

    await expect(api.getInstances()).rejects.toThrow(
      "Received an HTML response instead of JSON from the API. If you are behind an SSO proxy (Cloudflare Access, Pangolin, etc.), try refreshing the page or re-opening the URL in a new tab."
    )
  })

  it("names the status when a torrent file download fails", async () => {
    server.use(
      http.get("*/api/instances/:instanceId/torrent-creator/:taskID/file", () => new HttpResponse("nope", {
        status: 503,
        statusText: "Service Unavailable",
        headers: { "Content-Type": "text/plain" },
      }))
    )

    await expect(api.downloadTorrentFile(3, "synthetic-task")).rejects.toThrow(
      "Failed to download torrent file: Service Unavailable"
    )
  })
})
