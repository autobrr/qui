/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { cleanup, render } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import type { InstanceMark } from "@/lib/instance-marks"

import { InstanceMarkIcon } from "./InstanceMarkIcon"

const markState = vi.hoisted(() => ({ value: [{ icon: null, color: null, letters: "" }, () => {}] as [InstanceMark, () => void] }))

vi.mock("@/lib/instance-marks", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/instance-marks")>()
  return { ...actual, useInstanceMark: () => markState.value }
})

afterEach(cleanup)

describe("InstanceMarkIcon", () => {
  it.each([
    { name: "the letters of the mark over the automatic letters", mark: { icon: null, letters: "XZ" }, autoLetters: "MA", letters: "XZ" },
    { name: "the automatic letters when the mark has none", mark: { icon: null, letters: "" }, autoLetters: "MA", letters: "MA" },
    { name: "the letters for an icon name that the list does not know", mark: { icon: "gone", letters: "" }, autoLetters: "MA", letters: "MA" },
    { name: "an icon in place of letters without autoLetters", mark: { icon: null, letters: "XZ" }, autoLetters: undefined, letters: null },
    { name: "the icon of the mark over its letters", mark: { icon: "cat", letters: "XZ" }, autoLetters: "MA", letters: null },
  ])("shows $name", ({ mark, autoLetters, letters }) => {
    markState.value = [{ ...mark, color: null }, () => {}]

    const { container } = render(<InstanceMarkIcon instanceId={1} autoLetters={autoLetters} />)

    expect({ text: container.textContent, icon: container.querySelector("svg") !== null }).toEqual({ text: letters ?? "", icon: letters === null })
  })
})
