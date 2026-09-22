/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { json, jsonParseLinter } from "@codemirror/lang-json"
import { HighlightStyle, syntaxHighlighting } from "@codemirror/language"
import { linter } from "@codemirror/lint"
import { tags } from "@lezer/highlight"
import CodeMirror, { EditorView } from "@uiw/react-codemirror"
import { useMemo } from "react"
import type { JsonEditorProps } from "./json-editor"

// Mapped to the qui CSS variables so the editor follows every theme; no stock theme package.
const quiTheme = EditorView.theme({
  "&": {
    backgroundColor: "var(--background)",
    color: "var(--foreground)",
    fontSize: "0.875rem",
    border: "1px solid var(--input)",
    borderRadius: "var(--radius)",
  },
  "&.cm-focused": { outline: "none" },
  ".cm-scroller": { fontFamily: "var(--font-mono)" },
  ".cm-content": { caretColor: "var(--foreground)" },
  ".cm-cursor, .cm-dropCursor": { borderLeftColor: "var(--foreground)" },
  ".cm-gutters": {
    backgroundColor: "var(--muted)",
    color: "var(--muted-foreground)",
    borderRight: "1px solid var(--border)",
  },
  ".cm-activeLine": { backgroundColor: "color-mix(in oklch, var(--muted) 50%, transparent)" },
  ".cm-activeLineGutter": { backgroundColor: "transparent", color: "var(--foreground)" },
  "&.cm-focused > .cm-scroller > .cm-selectionLayer .cm-selectionBackground, .cm-selectionBackground, .cm-content ::selection": {
    backgroundColor: "color-mix(in oklch, var(--primary) 25%, transparent)",
  },
  ".cm-matchingBracket": { backgroundColor: "color-mix(in oklch, var(--primary) 20%, transparent)" },
  ".cm-lintRange-error": { textDecoration: "underline wavy var(--destructive)", backgroundImage: "none" },
})

const quiHighlight = HighlightStyle.define([
  { tag: tags.propertyName, color: "var(--foreground)" },
  { tag: tags.string, color: "var(--chart-2)" },
  { tag: [tags.number, tags.bool, tags.null], color: "var(--chart-3)" },
])

// jsonParseLinter reports a zero-width diagnostic, which renders as a dot; stretch it to the line end so the error is underlined.
const parseLinter = jsonParseLinter()
const jsonLint = linter((view) => parseLinter(view).map((d) => ({ ...d, to: view.state.doc.lineAt(d.from).to })))

const baseExtensions = [json(), jsonLint, quiTheme, syntaxHighlighting(quiHighlight)]

export default function JsonEditorCodeMirror({ value, onChange, placeholder, "aria-label": ariaLabel }: JsonEditorProps) {
  // The wrapper div takes stray props; the label must sit on .cm-content, the role="textbox" element.
  const extensions = useMemo(
    () => [...baseExtensions, EditorView.contentAttributes.of({ "aria-label": ariaLabel })],
    [ariaLabel]
  )
  return (
    <CodeMirror
      value={value}
      onChange={onChange}
      placeholder={placeholder}
      theme="none"
      extensions={extensions}
      basicSetup={{ foldGutter: false, autocompletion: false }}
      minHeight="200px"
      maxHeight="50dvh"
    />
  )
}
