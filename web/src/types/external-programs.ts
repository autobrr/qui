/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// External Program Types
export interface PathMapping {
  from: string
  to: string
}

export interface ExternalProgram {
  id: number
  name: string
  path: string
  args_template: string
  enabled: boolean
  use_terminal: boolean
  path_mappings: PathMapping[]
  created_at: string
  updated_at: string
}

// ExternalProgramCreate and ExternalProgramUpdate share an identical payload
// shape; keep them as a single source of truth to avoid drift.
export interface ExternalProgramPayload {
  name: string
  path: string
  args_template: string
  enabled: boolean
  use_terminal: boolean
  path_mappings: PathMapping[]
}

export type ExternalProgramCreate = ExternalProgramPayload
export type ExternalProgramUpdate = ExternalProgramPayload

export interface ExternalProgramExecute {
  program_id: number
  instance_id: number
  hashes: string[]
}

export interface ExternalProgramExecuteResult {
  hash: string
  success: boolean
  stdout?: string
  stderr?: string
  error?: string
}

export interface ExternalProgramExecuteResponse {
  results: ExternalProgramExecuteResult[]
}
