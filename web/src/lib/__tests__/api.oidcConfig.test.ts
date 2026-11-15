/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { HttpResponse, http } from "msw"
import { describe, expect, it } from "vitest"

import { APIError, api } from "@/lib/api"
import { server } from "@/test/msw/server"
import "@/i18n"

describe("getOIDCConfig", () => {
  it("keeps the login policy while the provider is unavailable", async () => {
    server.use(
      http.get("*/api/auth/oidc/config", () => HttpResponse.json(
        { error: "OIDC provider unavailable", disableBuiltInLogin: true },
        { status: 503 }
      ))
    )

    await expect(api.getOIDCConfig()).resolves.toMatchObject({
      enabled: true,
      disableBuiltInLogin: true,
      providerUnavailable: true,
    })
  })

  it("reports OIDC as off when the routes are not registered", async () => {
    server.use(
      http.get("*/api/auth/oidc/config", () => new HttpResponse("404 page not found", {
        status: 404,
        headers: { "Content-Type": "text/plain" },
      }))
    )

    await expect(api.getOIDCConfig()).resolves.toMatchObject({ enabled: false, providerUnavailable: false })
  })

  // regression: #3404
  it("throws on a throttled request instead of reporting OIDC as off", async () => {
    server.use(
      http.get("*/api/auth/oidc/config", () => new HttpResponse("Server capacity exceeded.", {
        status: 429,
        headers: { "Content-Type": "text/plain" },
      }))
    )

    const result = api.getOIDCConfig()
    await expect(result).rejects.toBeInstanceOf(APIError)
    await expect(result).rejects.toMatchObject({ status: 429 })
  })
})
