/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { readFileSync } from "node:fs"
import { resolve } from "node:path"
import i18next from "i18next"
import { HttpResponse, http } from "msw"
import { afterAll, beforeAll, describe, expect, it } from "vitest"

import { api } from "@/lib/api"
import { server } from "@/test/msw/server"

// AddTorrentDialog.tsx:550 suppresses this message by prefix, so api.ts must keep
// it in English no matter the active language. The i18next singleton is set to
// French here (not through "@/i18n") to stand in for a non-English user: if the
// message is ever translated again, the dialog shows a raw status code instead of
// its "check your input" hint, in all ten non-English locales.

const STATUS_PREFIX = "HTTP error! status:"

beforeAll(async () => {
  await i18next.init({
    lng: "fr",
    resources: {
      fr: { common: { errors: { httpStatus: "Erreur HTTP ! Statut : {{status}}" } } },
    },
    interpolation: { escapeValue: false },
  })
})

afterAll(() => {
  i18next.changeLanguage("en")
})

describe("api.addTorrent generic error message", () => {
  it("stays English under a non-English language so the dialog can suppress it", async () => {
    expect(i18next.t("errors.httpStatus", { ns: "common", status: 500 })).toBe("Erreur HTTP ! Statut : 500")

    server.use(
      http.post("*/api/instances/:instanceId/torrents", () => new HttpResponse(null, { status: 500 }))
    )

    const failure = await api.addTorrent(1, { urls: ["magnet:?xt=urn:btih:0".repeat(1)] }).catch((error: unknown) => error)

    expect(failure).toBeInstanceOf(Error)
    const message = (failure as Error).message
    expect(message).toBe("HTTP error! status: 500")
    // The exact predicate AddTorrentDialog.tsx:550 runs.
    expect(message.startsWith(STATUS_PREFIX)).toBe(true)
  })

  // The coupling breaks from either end, and mounting the dialog here would cost
  // more than it proves, so the dialog's half is pinned as source text.
  it("is the same prefix AddTorrentDialog still matches on", () => {
    // Vitest's cwd is web/, and import.meta.url is not a file URL under jsdom.
    const dialog = readFileSync(resolve(process.cwd(), "src/components/torrents/AddTorrentDialog.tsx"), "utf8")
    expect(dialog).toContain(`startsWith("${STATUS_PREFIX}")`)
  })
})
