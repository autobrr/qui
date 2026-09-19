/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

export interface PathTemplateEntry {
  snippet: string
  // Key under preferences.workflowDialog.move.templateHelp in the instances namespace.
  descriptionKey: string
}

// Mirrors the data map and FuncMap in resolveMovePath (internal/services/automations/processor.go).
export const MOVE_PATH_TEMPLATE_VARIABLES: PathTemplateEntry[] = [
  { snippet: "{{ .Name }}", descriptionKey: "variables.name" },
  { snippet: "{{ .Hash }}", descriptionKey: "variables.hash" },
  { snippet: "{{ .Category }}", descriptionKey: "variables.category" },
  { snippet: "{{ .IsolationFolderName }}", descriptionKey: "variables.isolationFolderName" },
  { snippet: "{{ .Tracker }}", descriptionKey: "variables.tracker" },
  { snippet: "{{ sanitize .Name }}", descriptionKey: "variables.sanitize" },
]
