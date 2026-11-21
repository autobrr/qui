/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, render, screen } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

import "@/i18n"
import type { InstanceResponse } from "@/types"

const themeState = vi.hoisted(() => ({ theme: "minimal" }))

vi.mock("@/hooks/useTheme", () => ({ useTheme: () => themeState }))
vi.mock("@tanstack/react-router", () => ({
  Link: ({ to, params, children, ...props }: { to: string; params?: Record<string, string>; children: ReactNode }) => (
    <a href={to.replace("$instanceId", params?.instanceId ?? "")} {...props}>{children}</a>
  ),
}))

import { HeaderInstanceTitle } from "./HeaderInstanceTitle"

function instance(id: number, name: string): InstanceResponse {
  return { id, name, isActive: true, connected: true } as InstanceResponse
}

function renderTitle(activeInstances: InstanceResponse[], instanceName: string) {
  return render(
    <HeaderInstanceTitle
      instanceName={instanceName}
      activeInstances={activeInstances}
      selectedInstanceId={-1}
      isAllInstancesRoute
      effectiveUnifiedInstanceIds={activeInstances.map(i => i.id)}
      onResetUnifiedScope={() => {}}
      onToggleUnifiedScopeInstance={() => {}}
    />
  )
}

afterEach(cleanup)

describe("HeaderInstanceTitle", () => {
  // regression: #4359
  it("shows the only enabled instance as a title, with no menu", () => {
    const { container } = renderTitle([instance(1, "main")], "main")

    expect(screen.getByRole("heading").textContent).toBe("main")
    expect(container.querySelector("[aria-haspopup]")).toBeNull()
  })

  it("opens a menu when more than one instance is enabled", () => {
    renderTitle([instance(1, "main"), instance(2, "spare")], "Unified")

    expect(screen.getByRole("button", { name: /Unified/ }).getAttribute("aria-haspopup")).toBe("menu")
    expect(screen.queryByRole("heading")).toBeNull()
  })
})
