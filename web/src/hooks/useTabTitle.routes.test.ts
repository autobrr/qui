/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { type AnyRoute, createRouter } from "@tanstack/react-router"
import { beforeAll, expect, it } from "vitest"

import i18n, { changeLanguage, initI18n } from "@/i18n"
import { routeTree } from "@/routeTree.gen"

const routes: AnyRoute[] = Object.values(createRouter({ routeTree }).routesById)

beforeAll(async () => {
  await initI18n()
  await changeLanguage("en")
})

it.each([
  ["/dashboard", "Dashboard"],
  ["/statistics", "Statistics"],
  ["/settings", "Settings"],
  ["/instances/", "Unified"],
  ["/instances/$instanceId", "Torrents"],
  ["/login", "Log in"],
  ["/setup", "Setup"],
  ["/add", "Add torrent"],
])("names the %s page %s", (fullPath, name) => {
  const title = routes.find(route => route.fullPath === fullPath)?.options.staticData?.title
  expect(title && i18n.t(title.titleKey, { ns: title.titleNs })).toBe(name)
})

it.each(["/", "/instances"])("gives the %s route no title of its own", (fullPath) => {
  expect(routes.find(route => route.fullPath === fullPath)?.options.staticData?.title).toBeNull()
})
