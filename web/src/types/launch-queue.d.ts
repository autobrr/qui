/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

interface LaunchParams {
  targetURL?: string
  files?: FileSystemFileHandle[]
}

interface LaunchQueue {
  setConsumer(consumer: (params: LaunchParams) => void | Promise<void>): void
}

declare global {
  interface Window {
    launchQueue?: LaunchQueue
  }
}

export {}
