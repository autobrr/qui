/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { TooltipProvider } from "@/components/ui/tooltip"
import "@/i18n"

import { Facts } from "./Facts"
import { Section } from "./Section"

vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }))

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

type Query = Parameters<typeof Section>[0]["queries"] extends readonly (infer Q)[] | undefined ? Q : never

function query(state: Partial<Query> = {}): Query {
  return { isLoading: false, isLoadingError: false, error: null, refetch: vi.fn(), ...state }
}


describe("Section", () => {
  it("holds the content and Save back while a query loads for the first time", () => {
    const onSubmit = vi.fn()
    render(
      <Section title="Log settings" queries={[query({ isLoading: true })]} submit={{ label: "Save settings", onSubmit }}>
        <p>the fields</p>
      </Section>
    )

    expect(screen.queryByText("the fields")).toBeNull()
    const save = screen.getByRole("button", { name: "Save settings" }) as HTMLButtonElement
    expect(save.disabled).toBe(true)
  })

  it("shows the error of a failed first load and loads again on Try again", () => {
    const failed = query({ isLoadingError: true, error: new Error("connection refused") })
    render(
      <Section title="Log settings" queries={[failed, query()]}>
        <p>the fields</p>
      </Section>
    )

    expect(screen.queryByText("the fields")).toBeNull()
    expect(screen.getByRole("alert").textContent).toContain("connection refused")
    fireEvent.click(screen.getByRole("button", { name: "Try again" }))

    expect(failed.refetch).toHaveBeenCalledOnce()
  })

  it("saves on its submit button and not on a copy button in its facts", () => {
    const onSubmit = vi.fn()
    render(
      <Section title="Log settings" submit={{ label: "Save settings", onSubmit }}>
        <Facts items={[{ label: "Config file", value: "/config/config.toml", copy: "/config/config.toml" }]} />
      </Section>,
      { wrapper: TooltipProvider }
    )

    fireEvent.click(screen.getByRole("button", { name: "Copy Config file" }))
    expect(onSubmit).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole("button", { name: "Save settings" }))
    expect(onSubmit).toHaveBeenCalledOnce()
  })
})
