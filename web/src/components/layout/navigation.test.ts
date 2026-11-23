/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { type AnyRoute, createRouter } from "@tanstack/react-router"
import { expect, it } from "vitest"

import { routeTree } from "@/routeTree.gen"
import { NAV_ITEMS } from "./navigation"

const routes: AnyRoute[] = Object.values(createRouter({ routeTree }).routesById)

it.each(NAV_ITEMS.filter(item => !item.search))("titles the $id tab with its sidebar label", ({ href, titleKey }) => {
  const route = routes.find(candidate => candidate.fullPath === href)
  expect(route?.options.staticData?.title).toEqual({ titleKey, titleNs: "common" })
})
