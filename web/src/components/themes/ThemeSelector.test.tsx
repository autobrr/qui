/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { Theme } from "@/config/themes"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, fireEvent, render as rtlRender, screen, waitFor, within } from "@testing-library/react"
import type { ReactElement } from "react"
import { http, HttpResponse } from "msw"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { _resetClientSettingsForTests, seedAndMarkReady } from "@/lib/client-settings"
import { server } from "@/test/msw/server"
import { _setDefaultThemeVisitForTests } from "@/lib/default-theme-visit"

const fixture = vi.hoisted(() => {
  const makeTheme = (
    id: string,
    name: string,
    options: Pick<Theme, "isPremium" | "isCustom" | "locked"> = {}
  ): Theme => ({
    id,
    name,
    description: `${name} description`,
    ...options,
    cssVars: {
      light: {},
      dark: {},
    },
  })

  return {
    builtInThemes: [
      makeTheme("nord", "Licensed theme", { isPremium: true }),
      makeTheme("locked", "Locked theme", { isPremium: true, locked: true }),
      makeTheme("free", "Free theme"),
      makeTheme("gruvbox", "Community theme", { isPremium: true }),
    ],
    customThemes: [
      {
        ...makeTheme("custom:local", "Custom theme", { isCustom: true }),
        rawCss: "/* @name: Custom theme */\n:root { --background: white; }\n.dark { --background: black; }\n",
      },
    ],
  }
})

const license = vi.hoisted(() => ({ hasPremiumAccess: true }))
const themeUtils = vi.hoisted(() => ({
  previewCustomThemeCss: vi.fn(),
  endCustomThemePreview: vi.fn(() => Promise.resolve()),
  setTheme: vi.fn(() => Promise.resolve()),
}))
const customThemesHook = vi.hoisted(() => ({
  refreshCustomThemes: vi.fn<() => Promise<Array<{ id: string; filename: string; css: string }>>>(() => Promise.resolve([])),
}))
const clipboard = vi.hoisted(() => ({ copyTextToClipboard: vi.fn<(text: string) => Promise<void>>(() => Promise.resolve()) }))

vi.mock("@/config/themes", () => ({
  themes: fixture.builtInThemes,
  getThemeById: (id: string) =>
    fixture.builtInThemes.find((theme) => theme.id === id),
}))

vi.mock("@/hooks/useLicense.ts", () => ({
  useHasPremiumAccess: () => ({
    hasPremiumAccess: license.hasPremiumAccess,
    isLoading: false,
    isError: false,
  }),
}))

const builtinsResult = vi.hoisted(() => ({ data: undefined, isSuccess: true, isError: false }))
vi.mock("@/hooks/useBuiltinThemes", () => ({
  useBuiltinThemes: () => builtinsResult,
}))

vi.mock("@/hooks/useCustomThemes", () => ({
  useCustomThemes: () => ({
    customThemes: fixture.customThemes,
    errors: [],
    directory: "/config/themes",
    isFetching: false,
    isError: false,
    refetch: vi.fn(),
  }),
  refreshCustomThemes: customThemesHook.refreshCustomThemes,
}))

vi.mock("@/hooks/useDebounce", () => ({ useDebounce: (value: unknown) => value }))
vi.mock("@/hooks/useMediaQuery", () => ({ useIsMobile: () => false }))
vi.mock("@/lib/utils", async (importOriginal) => ({
  ...await importOriginal<typeof import("@/lib/utils")>(),
  copyTextToClipboard: clipboard.copyTextToClipboard,
}))

vi.mock("@/hooks/useTheme", () => ({
  useTheme: () => ({
    theme: "free",
    setTheme: vi.fn(),
    setVariation: vi.fn(),
  }),
}))

vi.mock("@/utils/theme", () => ({
  getThemeColors: () => ({
    primary: "blue",
    secondary: "gray",
    accent: "green",
  }),
  getThemeVariation: () => null,
  ...themeUtils,
}))

vi.mock("@/components/themes/ThemeLayoutSettings", () => ({
  ThemeLayoutSettings: () => null,
}))

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}))

import { ThemeSelector } from "@/components/themes/ThemeSelector"

beforeEach(() => {
  localStorage.clear()
  _resetClientSettingsForTests()
  license.hasPremiumAccess = true
})

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
  _setDefaultThemeVisitForTests(false)
})

function render(ui: ReactElement) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  return rtlRender(<QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>)
}

function themeCards(): HTMLElement[] {
  return within(screen.getByRole("list", { name: "themes.selector.title" })).getAllByRole("listitem")
}

function card(name: string): HTMLElement {
  const match = themeCards().find((item) => item.textContent?.includes(name))
  if (!match) throw new Error(`Theme card not found: ${name}`)
  return match
}

// Opens the client settings push and resolves with the body of the next PUT.
function nextSettingsPush(): Promise<Record<string, string>> {
  seedAndMarkReady({})
  return new Promise((resolve) => {
    server.use(http.put("*/api/client-settings", async ({ request }) => {
      resolve(await request.json() as Record<string, string>)
      return HttpResponse.json({})
    }))
  })
}

describe("ThemeSelector", () => {
  it("presents built-in and custom themes as one ordered catalog", () => {
    render(<ThemeSelector />)

    const catalog = screen.getByRole("list", { name: "themes.selector.title" })
    const cards = within(catalog).getAllByRole("listitem")

    expect(cards).toHaveLength(5)
    expect(cards.map((card) => card.textContent)).toEqual([
      expect.stringContaining("Free theme"),
      expect.stringContaining("Licensed theme"),
      expect.stringContaining("Locked theme"),
      expect.stringContaining("Community theme"),
      expect.stringContaining("Custom theme"),
    ])
    expect(screen.getByText("themes.custom.title")).toBeTruthy()
  })

  it("offers Hide on a shown theme and Show on a hidden theme", () => {
    render(<ThemeSelector />)

    for (const name of ["Free theme", "Licensed theme", "Custom theme"]) {
      expect(within(card(name)).getByRole("button", { name: "themes.selector.hideTheme" })).toBeTruthy()
    }
    expect(within(card("Community theme")).getByRole("button", { name: "themes.selector.showTheme" })).toBeTruthy()
  })

  it("pushes a Hide choice through the client settings", async () => {
    const push = nextSettingsPush()
    render(<ThemeSelector />)

    fireEvent.click(within(card("Free theme")).getByRole("button", { name: "themes.selector.hideTheme" }))
    window.dispatchEvent(new Event("pagehide"))

    expect(await push).toEqual({ "qui-theme-visibility": JSON.stringify({ free: false }) })
    expect(within(card("Free theme")).getByRole("button", { name: "themes.selector.showTheme" })).toBeTruthy()
  })

  it("stores only the changes from the defaults", async () => {
    localStorage.setItem("qui-theme-visibility", JSON.stringify({ gruvbox: true, free: false }))
    const push = nextSettingsPush()
    render(<ThemeSelector />)

    fireEvent.click(within(card("Free theme")).getByRole("button", { name: "themes.selector.showTheme" }))
    window.dispatchEvent(new Event("pagehide"))

    expect(await push).toEqual({ "qui-theme-visibility": JSON.stringify({ gruvbox: true }) })
  })

  it("marks a locked theme with a lock, not a premium badge", () => {
    render(<ThemeSelector />)

    const catalog = screen.getByRole("list", { name: "themes.selector.title" })
    const [lockedCard] = within(catalog)
      .getAllByRole("listitem")
      .filter((card) => card.textContent?.includes("Locked theme"))

    expect(lockedCard.textContent).toContain("themes.selector.locked")
    for (const card of within(catalog).getAllByRole("listitem")) {
      expect(card.textContent).not.toMatch(/premium/i)
    }
  })
})

const RED_THEME = "/* @name: Red Alert */\n:root { --background: red; }\n.dark { --background: darkred; }\n"

interface RecordedRequest {
  method: string
  path: string
  body: unknown
}

function recordCustomThemeApi(respond: (req: RecordedRequest) => Response): RecordedRequest[] {
  const requests: RecordedRequest[] = []
  const handler = async ({ request }: { request: Request }) => {
    const text = await request.text()
    const recorded = { method: request.method, path: new URL(request.url).pathname, body: text ? JSON.parse(text) : null }
    requests.push(recorded)
    return respond(recorded)
  }
  server.use(
    http.post("*/api/themes/custom", handler),
    http.put("*/api/themes/custom/:id", handler),
    http.delete("*/api/themes/custom/:id", handler)
  )
  return requests
}

function cssField(): HTMLTextAreaElement {
  return screen.getByLabelText("themes.custom.editor.css") as HTMLTextAreaElement
}

function nameField(): HTMLInputElement {
  return screen.getByLabelText("themes.custom.editor.name") as HTMLInputElement
}

function openNewTheme() {
  fireEvent.click(screen.getByRole("button", { name: "themes.custom.newTheme" }))
}

function saveButton(): HTMLButtonElement {
  return screen.getByRole("button", { name: "themes.custom.editor.save" }) as HTMLButtonElement
}

describe("ThemeSelector custom theme editor", () => {
  it("keeps the box locked without a license, and the text names the editor", () => {
    license.hasPremiumAccess = false
    render(<ThemeSelector />)

    expect(screen.queryByRole("button", { name: "themes.custom.newTheme" })).toBeNull()
    expect(screen.queryByRole("button", { name: "themes.custom.copyPrompt" })).toBeNull()
    expect(screen.queryByRole("button", { name: "themes.custom.edit" })).toBeNull()
    expect(screen.getByText("themes.custom.lockedDescription")).toBeTruthy()
  })

  it("previews pasted CSS, fills the name, and saves a new theme as the selection", async () => {
    const requests = recordCustomThemeApi(() => HttpResponse.json({ id: "red-alert", filename: "red-alert.css", css: RED_THEME }, { status: 201 }))
    render(<ThemeSelector />)

    openNewTheme()
    fireEvent.change(cssField(), { target: { value: RED_THEME } })

    expect(nameField().value).toBe("Red Alert")
    expect(themeUtils.previewCustomThemeCss).toHaveBeenLastCalledWith(RED_THEME, {
      light: { "--background": "red" },
      dark: { "--background": "darkred" },
    }, false)

    fireEvent.click(saveButton())

    await waitFor(() => expect(themeUtils.setTheme).toHaveBeenCalledWith("custom:red-alert"))
    expect(requests).toEqual([{ method: "POST", path: "/api/themes/custom", body: { id: "red-alert", css: RED_THEME } }])
    expect(customThemesHook.refreshCustomThemes).toHaveBeenCalled()
    await waitFor(() => expect(screen.queryByLabelText("themes.custom.editor.css")).toBeNull())
  })

  it("writes an edited name into the @name line", async () => {
    const requests = recordCustomThemeApi(() => HttpResponse.json({ id: "blood-moon", filename: "blood-moon.css", css: "" }, { status: 201 }))
    render(<ThemeSelector />)

    openNewTheme()
    fireEvent.change(cssField(), { target: { value: RED_THEME } })
    fireEvent.change(nameField(), { target: { value: "Blood Moon" } })
    fireEvent.click(saveButton())

    await waitFor(() => expect(requests).toHaveLength(1))
    expect(requests[0].body).toEqual({ id: "blood-moon", css: RED_THEME.replace("Red Alert", "Blood Moon") })
  })

  // regression: #4124
  it("keeps a typed name through a later CSS edit", async () => {
    const requests = recordCustomThemeApi(() => HttpResponse.json({ id: "blood-moon", filename: "blood-moon.css", css: "" }, { status: 201 }))
    render(<ThemeSelector />)

    openNewTheme()
    fireEvent.change(cssField(), { target: { value: RED_THEME } })
    fireEvent.change(nameField(), { target: { value: "Blood Moon" } })
    fireEvent.change(cssField(), { target: { value: RED_THEME.replace("darkred", "maroon") } })

    expect(nameField().value).toBe("Blood Moon")
    fireEvent.click(saveButton())
    await waitFor(() => expect(requests).toHaveLength(1))
    expect(requests[0].body).toEqual({ id: "blood-moon", css: RED_THEME.replace("darkred", "maroon").replace("Red Alert", "Blood Moon") })
  })

  // regression: #4124
  it("loads the other theme when Edit is clicked while the editor is open", async () => {
    render(<ThemeSelector />)

    openNewTheme()
    fireEvent.change(cssField(), { target: { value: RED_THEME } })
    fireEvent.click(within(card("Custom theme")).getByRole("button", { name: "themes.custom.edit" }))

    await waitFor(() => expect(cssField().value).toBe(fixture.customThemes[0].rawCss))
    expect(nameField().value).toBe("Custom theme")
  })

  it("says why qui cannot use CSS without a .dark block and keeps the last good preview", () => {
    render(<ThemeSelector />)

    openNewTheme()
    fireEvent.change(cssField(), { target: { value: RED_THEME } })
    fireEvent.change(cssField(), { target: { value: ":root { --background: blue; }" } })

    expect(screen.getByText("themes.custom.editor.problems.missingDark")).toBeTruthy()
    expect(themeUtils.previewCustomThemeCss).toHaveBeenCalledTimes(1)
    expect(themeUtils.previewCustomThemeCss).toHaveBeenLastCalledWith(RED_THEME, expect.anything(), false)
    expect(saveButton().disabled).toBe(true)
  })

  // regression: #4124
  it("leaves CSS that loads an outside file out of the preview, and Save still asks the server", async () => {
    const refusal = "The theme can load a file from another website, line 4: url() can only use a data: address or a #fragment"
    const requests = recordCustomThemeApi(() => HttpResponse.json({ error: refusal }, { status: 400 }))
    const outside = `${RED_THEME}body { background: url(https://example.invalid/a.png); }\n`
    render(<ThemeSelector />)

    openNewTheme()
    fireEvent.change(cssField(), { target: { value: RED_THEME } })
    fireEvent.change(cssField(), { target: { value: outside } })

    expect(screen.getByText("themes.custom.editor.problems.outsideFile")).toBeTruthy()
    expect(themeUtils.previewCustomThemeCss).toHaveBeenCalledTimes(1)
    expect(themeUtils.previewCustomThemeCss).toHaveBeenLastCalledWith(RED_THEME, expect.anything(), false)

    fireEvent.click(saveButton())
    expect((await screen.findByRole("alert")).textContent).toBe(refusal)
    expect(requests).toHaveLength(1)
  })

  // regression: #4124
  it("asks before it replaces a theme with the same name, and a yes replaces and selects it", async () => {
    const requests = recordCustomThemeApi((req) =>
      req.method === "POST"
        ? HttpResponse.json({ error: "A custom theme with the id \"red-alert\" exists" }, { status: 409 })
        : HttpResponse.json({ id: "Red-Alert", filename: "Red-Alert.css", css: RED_THEME }))
    render(<ThemeSelector />)

    openNewTheme()
    fireEvent.change(cssField(), { target: { value: RED_THEME } })
    fireEvent.click(saveButton())

    const question = await screen.findByRole("alertdialog")
    expect(within(question).getByText("themes.custom.editor.replaceTitle")).toBeTruthy()
    expect(themeUtils.setTheme).not.toHaveBeenCalled()

    fireEvent.click(within(question).getByRole("button", { name: "themes.custom.editor.replace" }))

    await waitFor(() => expect(themeUtils.setTheme).toHaveBeenCalledWith("custom:Red-Alert"))
    expect(requests.map(({ method, path }) => `${method} ${path}`)).toEqual([
      "POST /api/themes/custom",
      "PUT /api/themes/custom/red-alert",
    ])
    expect(requests[1].body).toEqual({ css: RED_THEME })
  })

  // regression: #4124
  it("previews an edited theme only after a change during a default theme visit", async () => {
    _setDefaultThemeVisitForTests(true)
    render(<ThemeSelector />)

    fireEvent.click(within(card("Custom theme")).getByRole("button", { name: "themes.custom.edit" }))
    await waitFor(() => expect(cssField().value).toBe(fixture.customThemes[0].rawCss))
    expect(themeUtils.previewCustomThemeCss).not.toHaveBeenCalled()

    const repaired = fixture.customThemes[0].rawCss.replace("white", "ivory")
    fireEvent.change(cssField(), { target: { value: repaired } })
    expect(themeUtils.previewCustomThemeCss).toHaveBeenLastCalledWith(repaired, expect.anything(), false)

    fireEvent.change(cssField(), { target: { value: fixture.customThemes[0].rawCss } })
    expect(themeUtils.endCustomThemePreview).toHaveBeenCalledTimes(1)
    window.dispatchEvent(new CustomEvent("themechange", { detail: { mode: "dark", theme: { id: "minimal" }, isSystemChange: true } }))
    expect(themeUtils.previewCustomThemeCss).toHaveBeenCalledTimes(1)
  })

  // regression: #4124
  it("closes with a message when the list does not load after a save, and selects nothing", async () => {
    const { toast } = await import("sonner")
    recordCustomThemeApi(() => HttpResponse.json({ id: "red-alert", filename: "red-alert.css", css: RED_THEME }, { status: 201 }))
    customThemesHook.refreshCustomThemes.mockRejectedValueOnce(new Error("list failed"))
    render(<ThemeSelector />)

    openNewTheme()
    fireEvent.change(cssField(), { target: { value: RED_THEME } })
    fireEvent.click(saveButton())

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("themes.custom.editor.savedNotLoaded"))
    await waitFor(() => expect(screen.queryByLabelText("themes.custom.editor.css")).toBeNull())
    expect(themeUtils.setTheme).not.toHaveBeenCalled()
  })

  // regression: #4124
  it("keeps a New theme draft when an earlier Edit finishes loading later", async () => {
    let finishLoad: (files: Array<{ id: string; filename: string; css: string }>) => void = () => {}
    customThemesHook.refreshCustomThemes.mockReturnValueOnce(new Promise(resolve => { finishLoad = resolve }))
    render(<ThemeSelector />)

    fireEvent.click(within(card("Custom theme")).getByRole("button", { name: "themes.custom.edit" }))
    openNewTheme()
    fireEvent.change(cssField(), { target: { value: RED_THEME } })
    await act(async () => {
      finishLoad([{ id: "local", filename: "local.css", css: fixture.customThemes[0].rawCss }])
    })

    expect(cssField().value).toBe(RED_THEME)
    expect(screen.getByText("themes.custom.editor.newTitle")).toBeTruthy()
  })

  // regression: #4124
  it("keeps an editor that opened while an earlier save was still running", async () => {
    let finishSave: () => void = () => {}
    const saved = new Promise<void>(resolve => { finishSave = resolve })
    server.use(http.put("*/api/themes/custom/:id", async () => {
      await saved
      return HttpResponse.json({ id: "local", filename: "local.css", css: "" })
    }))
    render(<ThemeSelector />)

    fireEvent.click(within(card("Custom theme")).getByRole("button", { name: "themes.custom.edit" }))
    await waitFor(() => expect(cssField().value).toBe(fixture.customThemes[0].rawCss))
    fireEvent.click(saveButton())
    openNewTheme()
    fireEvent.change(cssField(), { target: { value: RED_THEME } })

    finishSave()
    await waitFor(() => expect(customThemesHook.refreshCustomThemes).toHaveBeenCalledTimes(2))
    await act(async () => {})

    expect(cssField().value).toBe(RED_THEME)
  })

  it("restores the active theme on Cancel and on Esc", async () => {
    render(<ThemeSelector />)

    openNewTheme()
    fireEvent.change(cssField(), { target: { value: RED_THEME } })
    fireEvent.click(screen.getByRole("button", { name: "common:actions.cancel" }))

    await waitFor(() => expect(themeUtils.endCustomThemePreview).toHaveBeenCalledTimes(1))
    expect(screen.queryByLabelText("themes.custom.editor.css")).toBeNull()

    openNewTheme()
    fireEvent.change(cssField(), { target: { value: RED_THEME } })
    fireEvent.keyDown(cssField(), { key: "Escape" })

    await waitFor(() => expect(themeUtils.endCustomThemePreview).toHaveBeenCalledTimes(2))
    expect(screen.queryByLabelText("themes.custom.editor.css")).toBeNull()
    expect(themeUtils.setTheme).not.toHaveBeenCalled()
  })

  it("shows the message of a refused save and keeps the editor open", async () => {
    const refusal = "The theme can load a file from another website, line 3: url() can only use a data: address or a #fragment"
    recordCustomThemeApi(() => HttpResponse.json({ error: refusal }, { status: 400 }))
    render(<ThemeSelector />)

    openNewTheme()
    fireEvent.change(cssField(), { target: { value: RED_THEME } })
    fireEvent.click(saveButton())

    expect((await screen.findByRole("alert")).textContent).toBe(refusal)
    expect(cssField().value).toBe(RED_THEME)
    expect(themeUtils.setTheme).not.toHaveBeenCalled()
  })

  // regression: #4124
  it("edits the current file of a custom theme with replace and keeps the selection", async () => {
    const requests = recordCustomThemeApi(() => HttpResponse.json({ id: "local", filename: "local.css", css: "" }))
    const onDisk = fixture.customThemes[0].rawCss.replace("white", "linen")
    customThemesHook.refreshCustomThemes.mockResolvedValueOnce([{ id: "local", filename: "local.css", css: onDisk }])
    render(<ThemeSelector />)

    fireEvent.click(within(card("Custom theme")).getByRole("button", { name: "themes.custom.edit" }))
    await waitFor(() => expect(cssField().value).toBe(onDisk))
    expect(nameField().value).toBe("Custom theme")

    const edited = onDisk.replace("linen", "ivory")
    fireEvent.change(cssField(), { target: { value: edited } })
    fireEvent.click(saveButton())

    await waitFor(() => expect(themeUtils.endCustomThemePreview).toHaveBeenCalled())
    expect(requests).toEqual([{ method: "PUT", path: "/api/themes/custom/local", body: { css: edited } }])
    expect(customThemesHook.refreshCustomThemes).toHaveBeenCalled()
    expect(themeUtils.setTheme).not.toHaveBeenCalled()
  })

  it("asks before it removes a custom theme, then removes it", async () => {
    const requests = recordCustomThemeApi(() => new HttpResponse(null, { status: 204 }))
    render(<ThemeSelector />)

    fireEvent.click(within(card("Custom theme")).getByRole("button", { name: "themes.custom.remove" }))
    const question = await screen.findByRole("alertdialog")
    expect(requests).toEqual([])

    fireEvent.click(within(question).getByRole("button", { name: "themes.custom.removeConfirm" }))

    await waitFor(() => expect(requests).toEqual([{ method: "DELETE", path: "/api/themes/custom/local", body: null }]))
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
  })

  it("copies the AI prompt with the theme areas", async () => {
    render(<ThemeSelector />)

    fireEvent.click(screen.getByRole("button", { name: "themes.custom.copyPrompt" }))

    await waitFor(() => expect(clipboard.copyTextToClipboard).toHaveBeenCalledTimes(1))
    expect(clipboard.copyTextToClipboard.mock.calls[0][0]).toContain("[data-qui-area=\"torrent-table\"]")
  })
})
