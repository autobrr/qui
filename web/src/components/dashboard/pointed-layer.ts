/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { createContext, useContext } from "react"

/** The layer that the pointer or the keyboard focus is on, in the chart or in the instance list of the Dashboard. */
export const PointedLayerContext = createContext<number | undefined>(undefined)
// A separate context, so that a row that only sets the layer does not render again when the layer changes.
export const PointLayerContext = createContext<(layer: number | undefined) => void>(() => {})

export function usePointedLayer(): number | undefined {
  return useContext(PointedLayerContext)
}

export function usePointLayer(): (layer: number | undefined) => void {
  return useContext(PointLayerContext)
}
