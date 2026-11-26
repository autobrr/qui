/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { extractMagnetFromTargetURL } from "./magnet"

export type LaunchQueuePayload =
  | { type: "file"; files: File[] }
  | { type: "url"; urls: string[] }

export type LaunchQueueEvent =
  | { kind: "payload"; payload: LaunchQueuePayload }
  | { kind: "invalid-files" }

type LaunchQueueListener = (event: LaunchQueueEvent) => void

let isLaunchQueueInitialized = false
let pendingEvent: LaunchQueueEvent | null = null
const listeners = new Set<LaunchQueueListener>()

function emitLaunchQueueEvent(event: LaunchQueueEvent): void {
  pendingEvent = event
  for (const listener of listeners) {
    listener(event)
  }
}

export function consumeLaunchQueueEvent(): LaunchQueueEvent | null {
  const event = pendingEvent
  pendingEvent = null
  return event
}

export function subscribeLaunchQueueEvents(listener: LaunchQueueListener): () => void {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

export function setupLaunchQueueConsumer(): void {
  if (isLaunchQueueInitialized) return
  if (typeof window === "undefined") return
  if (!window.launchQueue) return

  isLaunchQueueInitialized = true

  window.launchQueue.setConsumer(async (launchParams) => {
    const launchedMagnet = extractMagnetFromTargetURL(launchParams.targetURL)
    if (launchedMagnet) {
      emitLaunchQueueEvent({ kind: "payload", payload: { type: "url", urls: [launchedMagnet] } })
      return
    }

    if (!launchParams.files?.length) return

    const files: File[] = []
    for (const handle of launchParams.files) {
      try {
        const file = await handle.getFile()
        if (file.name.toLowerCase().endsWith(".torrent")) {
          files.push(file)
        }
      } catch (error) {
        console.error("Failed to get file from handle:", error)
      }
    }

    if (files.length > 0) {
      emitLaunchQueueEvent({ kind: "payload", payload: { type: "file", files } })
      return
    }

    emitLaunchQueueEvent({ kind: "invalid-files" })
  })
}
