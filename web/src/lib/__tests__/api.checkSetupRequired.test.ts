/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { HttpResponse, http } from "msw"
import { describe, expect, it } from "vitest"

import { APIError, api } from "@/lib/api"
import { server } from "@/test/msw/server"
import "@/i18n"

describe("checkSetupRequired", () => {
  it("reports a fresh install", async () => {
    server.use(http.get("*/api/auth/check-setup", () => HttpResponse.json({ setupRequired: true })))

    await expect(api.checkSetupRequired()).resolves.toBe(true)
  })

  it("reports a configured install", async () => {
    server.use(http.get("*/api/auth/check-setup", () => HttpResponse.json({ setupRequired: false })))

    await expect(api.checkSetupRequired()).resolves.toBe(false)
  })

  // regression: #4313
  it("throws when the server cannot check the setup status", async () => {
    server.use(http.get("*/api/auth/check-setup", () => HttpResponse.json({ error: "database unavailable" }, { status: 500 })))

    const result = api.checkSetupRequired()
    await expect(result).rejects.toBeInstanceOf(APIError)
    await expect(result).rejects.toMatchObject({ status: 500 })
  })

  // regression: #4313
  it("throws when the check gets no answer", async () => {
    server.use(http.get("*/api/auth/check-setup", () => HttpResponse.error()))

    await expect(api.checkSetupRequired()).rejects.toThrow()
  })
})
