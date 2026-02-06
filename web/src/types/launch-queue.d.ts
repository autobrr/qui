/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

/**
 * TypeScript declarations for the File Handling API's LaunchQueue
 * @see https://developer.chrome.com/docs/capabilities/web-apis/file-handling
 */

interface LaunchParams {
  /** The target URL that was launched (may contain query params for protocol handlers) */
  targetURL?: string
  /** File handles from file_handlers launches */
  files?: FileSystemFileHandle[]
}

interface LaunchQueue {
  /** Set a consumer function to handle launch events */
  setConsumer(consumer: (params: LaunchParams) => void | Promise<void>): void
}

declare global {
  interface Window {
    /** LaunchQueue API for PWA file/protocol handling (Chrome 102+) */
    launchQueue?: LaunchQueue
  }
}

export {}
