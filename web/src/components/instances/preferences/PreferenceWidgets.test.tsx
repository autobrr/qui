/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import type { PreferenceField } from "@/types"

import { preferenceWidgets } from "./PreferenceWidgets"

// i18n-namespace: instances

const incognito = vi.hoisted(() => [false, () => {}] as const)
const translation = vi.hoisted(() => ({ t: (key: string) => key }))

vi.mock("react-i18next", () => ({ useTranslation: () => translation }))
vi.mock("@/lib/incognito", () => ({ useIncognitoMode: () => incognito }))

describe("the password widget of the qBittorrent preferences", () => {
  // regression: #4237
  it("keeps the password managers away, because the password belongs to qBittorrent", () => {
    const field: PreferenceField = { key: "mail_notification_password", value: "", tab: "downloads", section: "mail", control: "password" }
    const Widget = preferenceWidgets.password
    render(<Widget id="pw" instanceId={1} field={field} labels={{ labelKey: "preferences.fields.mailNotificationPassword" }} value="" onChange={() => {}} disabled={false} />)

    const input = screen.getByLabelText("preferences.fields.mailNotificationPassword")
    expect(input.getAttribute("autocomplete")).toBe("new-password")
    expect(input.getAttribute("data-1p-ignore")).toBe("true")
    expect(input.getAttribute("data-lpignore")).toBe("true")
  })
})
