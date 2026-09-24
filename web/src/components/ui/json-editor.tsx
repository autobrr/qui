/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { lazy, Suspense, useState } from "react"
import { Textarea } from "./textarea"

export interface JsonEditorProps {
  value: string
  onChange: (value: string) => void
  placeholder?: string
  "aria-label": string
}

// CodeMirror is its own chunk; the plain textarea stands in while it loads.
let loadedCodeMirror: typeof import("./json-editor-codemirror") | undefined
// eslint-disable-next-line react-refresh/only-export-components
export const preloadJsonEditor = () => import("./json-editor-codemirror").then((m) => (loadedCodeMirror = m))
const JsonEditorCodeMirror = lazy(preloadJsonEditor)

export function JsonEditor(props: JsonEditorProps) {
  // lazy() suspends on first render even with the chunk cached; picked once per mount, as a switch would remount CodeMirror.
  const [PreloadedEditor] = useState(() => loadedCodeMirror?.default)
  if (PreloadedEditor) return <PreloadedEditor {...props} />

  return (
    <Suspense
      fallback={(
        <Textarea
          value={props.value}
          onChange={(e) => props.onChange(e.target.value)}
          placeholder={props.placeholder}
          aria-label={props["aria-label"]}
          className="min-h-[200px] max-h-[50dvh] font-mono text-sm"
        />
      )}
    >
      <JsonEditorCodeMirror {...props} />
    </Suspense>
  )
}
