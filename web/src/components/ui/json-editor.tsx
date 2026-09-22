/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { lazy, Suspense } from "react"
import { Textarea } from "./textarea"

export interface JsonEditorProps {
  value: string
  onChange: (value: string) => void
  placeholder?: string
  "aria-label": string
}

// CodeMirror is its own chunk; the plain textarea stands in while it loads.
const JsonEditorCodeMirror = lazy(() => import("./json-editor-codemirror"))

export function JsonEditor(props: JsonEditorProps) {
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
