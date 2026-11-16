/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import type { PreferenceField } from "@/types"

import { preferenceWidgets } from "./PreferenceWidgets"

const incognito = vi.hoisted(() => [false, () => {}] as const)
const translation = vi.hoisted(() => ({ t: (key: string) => key }))

vi.mock("react-i18next", () => ({ useTranslation: () => translation }))
vi.mock("@/lib/incognito", () => ({ useIncognitoMode: () => incognito }))

describe("the password widget of the qBittorrent preferences", () => {
  it("keeps the password managers away, because the password belongs to qBittorrent", () => {
    const field: PreferenceField = { key: "web_ui_password", value: "", tab: "webui", section: "auth", control: "password" }
    const Widget = preferenceWidgets.password
    render(<Widget id="pw" instanceId={1} field={field} labels={{ labelKey: "password" }} value="" onChange={() => {}} disabled={false} />)

    const input = screen.getByLabelText("password")
    expect(input.getAttribute("autocomplete")).toBe("new-password")
    expect(input.getAttribute("data-1p-ignore")).toBe("true")
    expect(input.getAttribute("data-lpignore")).toBe("true")
  })
})
