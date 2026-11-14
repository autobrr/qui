/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useEffect, useId, useMemo, useReducer, useRef, useState } from "react"
import { useQueryClient } from "@tanstack/react-query"
import { AlertTriangle, Eye, Loader2 } from "lucide-react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog"
import { Button } from "@/components/ui/button"
import { FieldHelp } from "@/components/ui/field-help"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Sheet, SheetContent, SheetDescription, SheetFooter, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { Textarea } from "@/components/ui/textarea"
import { useDebounce } from "@/hooks/useDebounce"
import { useIsMobile } from "@/hooks/useMediaQuery"
import { refreshCustomThemes } from "@/hooks/useCustomThemes"
import { APIError, api } from "@/lib/api"
import { findOutsideFile } from "@/lib/css-outside-files"
import { readThemeName, themeIdFromName, writeThemeName } from "@/lib/custom-theme-editor"
import { CUSTOM_THEME_PREFIX } from "@/lib/custom-themes"
import { isDefaultThemeVisit } from "@/lib/default-theme-visit"
import type { Theme } from "@/config/themes"
import { endCustomThemePreview, previewCustomThemeCss, setTheme } from "@/utils/theme"
import { findThemeCssProblem, parseThemeCSS } from "@/utils/themeParser"

const PREVIEW_DELAY_MS = 300

export type CustomThemeEditorTarget =
  | { kind: "new" }
  | { kind: "edit"; id: string; filename: string; css: string }

type EditorPhase =
  | { kind: "editing"; error: string | null }
  | { kind: "saving" }
  | { kind: "confirm-replace" }

interface EditorState {
  name: string
  css: string
  phase: EditorPhase
}

type EditorAction =
  | { type: "name"; name: string }
  | { type: "css"; css: string }
  | { type: "save-start" }
  | { type: "save-failed"; error: string }
  | { type: "conflict" }
  | { type: "keep-editing" }

function editorReducer(state: EditorState, action: EditorAction): EditorState {
  switch (action.type) {
    case "name":
      return { ...state, name: action.name, phase: clearedError(state.phase) }
    case "css": {
      // Only a new @name fills the field, so an edit elsewhere in the CSS keeps a name that the user typed.
      const header = readThemeName(action.css)
      return {
        ...state,
        css: action.css,
        name: header !== null && header !== readThemeName(state.css) ? header : state.name,
        phase: clearedError(state.phase),
      }
    }
    case "save-start":
      return { ...state, phase: { kind: "saving" } }
    case "save-failed":
      return { ...state, phase: { kind: "editing", error: action.error } }
    case "conflict":
      return { ...state, phase: { kind: "confirm-replace" } }
    case "keep-editing":
      return state.phase.kind === "confirm-replace" ? { ...state, phase: { kind: "editing", error: null } } : state
  }
}

// An edit makes a refused save stale, so its message gives way to the live status.
function clearedError(phase: EditorPhase): EditorPhase {
  return phase.kind === "editing" ? { kind: "editing", error: null } : phase
}

function initialState(target: CustomThemeEditorTarget): EditorState {
  const css = target.kind === "edit" ? target.css : ""
  return { name: readThemeName(css) ?? "", css, phase: { kind: "editing", error: null } }
}

type CssProblem = "empty" | "missing-root" | "missing-dark"

function cssProblem(css: string): CssProblem | null {
  return css.trim() === "" ? "empty" : findThemeCssProblem(css)
}

type PreviewProblem =
  | { kind: CssProblem }
  | { kind: "outside-file" | "unreadable"; line: number }

// The preview refuses an outside file too, so the browser never loads one; Save leaves that refusal to the server.
function previewProblemOf(css: string): PreviewProblem | null {
  const problem = cssProblem(css)
  return problem ? { kind: problem } : findOutsideFile(css)
}

const PROBLEM_KEYS = {
  "missing-root": "themes.custom.editor.problems.missingRoot",
  "missing-dark": "themes.custom.editor.problems.missingDark",
  "outside-file": "themes.custom.editor.problems.outsideFile",
  "unreadable": "themes.custom.editor.problems.unreadable",
} as const

interface CustomThemeEditorProps {
  target: CustomThemeEditorTarget
  onClose: () => void
}

/**
 * Edits one custom theme with a live preview in this browser. Closing it in
 * any way paints the active theme again; a saved new theme becomes the active
 * theme of the slot that the picker changes.
 */
export function CustomThemeEditor({ target, onClose }: CustomThemeEditorProps) {
  const { t } = useTranslation("settings")
  const queryClient = useQueryClient()
  const isMobile = useIsMobile()
  const nameId = useId()
  const cssId = useId()
  const [state, dispatch] = useReducer(editorReducer, target, initialState)

  const previewCss = useDebounce(state.css, PREVIEW_DELAY_MS)
  const previewProblem = useMemo(() => previewProblemOf(previewCss), [previewCss])
  const shown = useRef<{ css: string; cssVars: Theme["cssVars"]; lightOnly: boolean } | null>(null)
  const [openedCss] = useState(state.css)
  // A default theme visit is for repair, and the saved CSS can hide the page, so only a change previews.
  const waitsForChange = isDefaultThemeVisit() && previewCss === openedCss

  useEffect(() => {
    if (previewProblem) return
    if (waitsForChange) {
      if (shown.current) {
        shown.current = null
        void endCustomThemePreview()
      }
      return
    }
    const parsed = parseThemeCSS(previewCss)
    if (!parsed) return
    shown.current = { css: previewCss, cssVars: parsed.cssVars, lightOnly: parsed.metadata.lightOnly === true }
    previewCustomThemeCss(previewCss, parsed.cssVars, parsed.metadata.lightOnly === true)
  }, [previewCss, previewProblem, waitsForChange])

  // A mode change or a re-applied theme repaints the page, so the preview paints itself again.
  useEffect(() => {
    const repaint = () => {
      const preview = shown.current
      if (preview) previewCustomThemeCss(preview.css, preview.cssVars, preview.lightOnly)
    }
    window.addEventListener("themechange", repaint)
    return () => {
      window.removeEventListener("themechange", repaint)
      if (shown.current) void endCustomThemePreview()
    }
  }, [])

  const name = state.name.trim()
  const id = target.kind === "edit" ? target.id : themeIdFromName(name)
  const filename = target.kind === "edit" ? target.filename : id && `${id}.css`
  const saving = state.phase.kind === "saving"
  const canSave = !saving && id !== "" && name !== "" && cssProblem(state.css) === null

  const save = async (replace: boolean) => {
    const css = writeThemeName(state.css, name)
    dispatch({ type: "save-start" })
    let savedId: string
    try {
      const saved = target.kind === "edit" || replace ? await api.replaceCustomTheme(id, css) : await api.createCustomTheme(id, css)
      savedId = saved.id
    } catch (error) {
      if (target.kind === "new" && !replace && error instanceof APIError && error.status === 409) {
        dispatch({ type: "conflict" })
        return
      }
      dispatch({ type: "save-failed", error: error instanceof Error ? error.message : String(error) })
      return
    }

    try {
      await refreshCustomThemes(queryClient)
    } catch {
      toast.error(t("themes.custom.editor.savedNotLoaded"))
      onClose()
      return
    }
    if (target.kind === "new") {
      await setTheme(`${CUSTOM_THEME_PREFIX}${savedId}`)
    }
    toast.success(t("themes.custom.editor.saved"))
    onClose()
  }

  return (
    <>
      <Sheet
        open
        modal={false}
        onOpenChange={(open) => {
          if (!open && !saving) onClose()
        }}
      >
        <SheetContent
          side={isMobile ? "bottom" : "right"}
          className={isMobile ? "h-[85dvh] gap-3" : "w-full gap-3 sm:max-w-xl"}
          onInteractOutside={(event) => event.preventDefault()}
        >
          <SheetHeader className="pb-0">
            <SheetTitle>
              {t(target.kind === "edit" ? "themes.custom.editor.editTitle" : "themes.custom.editor.newTitle")}
            </SheetTitle>
            <SheetDescription>{t("themes.custom.editor.description")}</SheetDescription>
          </SheetHeader>

          <div className="flex min-h-0 flex-1 flex-col gap-3 px-4">
            <div className="space-y-1.5">
              <Label htmlFor={nameId}>{t("themes.custom.editor.name")}</Label>
              <Input
                id={nameId}
                value={state.name}
                placeholder={t("themes.custom.editor.namePlaceholder")}
                onChange={(event) => dispatch({ type: "name", name: event.target.value })}
                disabled={saving}
              />
              {target.kind === "new" && name !== "" && id === "" ? (
                <p className="text-xs text-destructive">{t("themes.custom.editor.nameInvalid")}</p>
              ) : name === "" && state.css.trim() !== "" ? (
                <p className="text-xs text-muted-foreground">{t("themes.custom.editor.nameRequired")}</p>
              ) : filename && (
                <p className="text-xs text-muted-foreground">
                  {t("themes.custom.editor.savedAs", { filename })}
                </p>
              )}
            </div>

            <div className="flex min-h-0 flex-1 flex-col gap-1.5">
              <div className="flex items-center gap-1">
                <Label htmlFor={cssId}>{t("themes.custom.editor.css")}</Label>
                <FieldHelp>{t("themes.custom.editor.cssHelp")}</FieldHelp>
              </div>
              <Textarea
                id={cssId}
                value={state.css}
                onChange={(event) => dispatch({ type: "css", css: event.target.value })}
                disabled={saving}
                placeholder={t("themes.custom.editor.cssPlaceholder")}
                wrap="off"
                spellCheck={false}
                autoCapitalize="off"
                autoCorrect="off"
                className="min-h-40 flex-1 resize-none font-mono text-base field-sizing-fixed md:text-xs"
              />
              {state.phase.kind === "editing" && state.phase.error ? (
                <p role="alert" className="flex items-start gap-1.5 text-xs text-destructive">
                  <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
                  <span className="break-words">
                    <span className="font-medium">{t("themes.custom.editor.saveFailed")}</span> {state.phase.error}
                  </span>
                </p>
              ) : previewProblem && previewProblem.kind !== "empty" ? (
                <p className="flex items-start gap-1.5 text-xs text-yellow-700 dark:text-yellow-400">
                  <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
                  <span>{t(PROBLEM_KEYS[previewProblem.kind], "line" in previewProblem ? { line: previewProblem.line } : undefined)}</span>
                </p>
              ) : (
                <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
                  <Eye className="h-3.5 w-3.5 shrink-0" />
                  {t(
                    previewProblem
                      ? "themes.custom.editor.emptyStatus"
                      : waitsForChange
                        ? "themes.custom.editor.previewWaits"
                        : "themes.custom.editor.previewing"
                  )}
                </p>
              )}
            </div>
          </div>

          <SheetFooter className="flex-row justify-end gap-2 pt-0 pb-[max(1rem,env(safe-area-inset-bottom))]">
            <Button variant="outline" className="min-h-11 flex-1 sm:flex-none md:min-h-9" onClick={onClose} disabled={saving}>
              {t("common:actions.cancel")}
            </Button>
            <Button className="min-h-11 flex-1 sm:flex-none md:min-h-9" onClick={() => void save(false)} disabled={!canSave}>
              {saving && <Loader2 className="h-4 w-4 animate-spin" />}
              {t("themes.custom.editor.save")}
            </Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>

      <AlertDialog
        open={state.phase.kind === "confirm-replace"}
        onOpenChange={(open) => {
          if (!open) dispatch({ type: "keep-editing" })
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("themes.custom.editor.replaceTitle")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("themes.custom.editor.replaceDescription", { filename: `${id}.css` })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("common:actions.cancel")}</AlertDialogCancel>
            <AlertDialogAction onClick={() => void save(true)}>
              {t("themes.custom.editor.replace")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}
