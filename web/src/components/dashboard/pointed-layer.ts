/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { createContext, useContext } from "react"

export const PointedLayerContext = createContext<number | undefined>(undefined)
export const PointLayerContext = createContext<(layer: number | undefined) => void>(() => {})

export function usePointedLayer(): number | undefined {
  return useContext(PointedLayerContext)
}

export function usePointLayer(): (layer: number | undefined) => void {
  return useContext(PointLayerContext)
}
