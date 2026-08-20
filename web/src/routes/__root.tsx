/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { createRootRoute, Outlet, redirect } from "@tanstack/react-router"
import { NotFound } from "@/pages/NotFound"

export const Route = createRootRoute({
  beforeLoad: ({ location }) => {
    // SSO proxies (Pangolin, Cloudflare Access) preserve /index.html as the
    // request path. The server redirects it to the SPA root, but a registered
    // service worker answers with the app shell before that redirect can run,
    // which strands the router on the not-found screen. Bounce it ourselves.
    if (location.pathname.endsWith("/index.html")) {
      throw redirect({ to: "/", search: location.search })
    }
  },
  component: () => (
    <>
      <Outlet />
    </>
  ),
  notFoundComponent: NotFound,
})