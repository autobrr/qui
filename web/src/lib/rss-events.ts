/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { RSSItems } from "@/types"
import { getApiBaseUrl } from "./base-url"

// Event types from backend
export type RSSEventType = "connected" | "feeds_update"

export interface RSSEvent<T = unknown> {
  type: RSSEventType
  data: T
}

export interface ConnectedPayload {
  instanceId: number
  timestamp: number
}

export interface FeedsUpdatePayload {
  instanceId: number
  items: RSSItems
  timestamp: number
}

export interface RSSEventHandlers {
  onConnected?: (data: ConnectedPayload) => void
  onFeedsUpdate?: (data: FeedsUpdatePayload) => void
  onError?: (error: Event) => void
  onDisconnected?: () => void
  onReconnecting?: (info: { attempt: number; delayMs: number }) => void
  onMaxReconnectAttempts?: () => void
}

export class RSSEventSource {
  private eventSource: EventSource | null = null
  private instanceId: number
  private handlers: RSSEventHandlers
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null
  private reconnectAttempts = 0
  private maxReconnectAttempts = 5
  private baseReconnectDelay = 1000
  private isIntentionalClose = false

  constructor(instanceId: number, handlers: RSSEventHandlers) {
    this.instanceId = instanceId
    this.handlers = handlers
  }

  connect(): void {
    if (this.eventSource) {
      this.disconnect()
    }

    this.isIntentionalClose = false
    const url = `${getApiBaseUrl()}/instances/${this.instanceId}/rss/events`

    try {
      this.eventSource = new EventSource(url, { withCredentials: true })

      this.eventSource.addEventListener("connected", (event) => {
        this.reconnectAttempts = 0
        try {
          const parsed = JSON.parse(event.data) as RSSEvent<ConnectedPayload>
          this.handlers.onConnected?.(parsed.data)
        } catch (e) {
          console.error("Failed to parse connected event", e)
        }
      })

      this.eventSource.addEventListener("feeds_update", (event) => {
        try {
          const parsed = JSON.parse(event.data) as RSSEvent<FeedsUpdatePayload>
          this.handlers.onFeedsUpdate?.(parsed.data)
        } catch (e) {
          console.error("Failed to parse feeds_update event", e)
        }
      })

      this.eventSource.onerror = (error) => {
        this.handlers.onError?.(error)

        // Don't reconnect if we intentionally closed
        if (!this.isIntentionalClose) {
          this.scheduleReconnect()
        }
      }
    } catch (error) {
      console.error("Failed to create EventSource", error)
      this.scheduleReconnect()
    }
  }

  disconnect(): void {
    this.isIntentionalClose = true

    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer)
      this.reconnectTimer = null
    }

    if (this.eventSource) {
      this.eventSource.close()
      this.eventSource = null
      this.handlers.onDisconnected?.()
    }

    this.reconnectAttempts = 0
  }

  private scheduleReconnect(): void {
    if (this.reconnectAttempts >= this.maxReconnectAttempts) {
      console.error("RSS SSE max reconnection attempts reached")
      this.handlers.onMaxReconnectAttempts?.()
      return
    }

    // Prevent overlapping reconnect timers when multiple error events fire quickly.
    if (this.reconnectTimer) {
      return
    }

    // Close existing connection if any
    if (this.eventSource) {
      this.eventSource.close()
      this.eventSource = null
    }

    // Exponential backoff: 1s, 2s, 4s, 8s, 16s
    const attempt = this.reconnectAttempts + 1
    const delay = this.baseReconnectDelay * Math.pow(2, this.reconnectAttempts)
    this.reconnectAttempts = attempt

    this.handlers.onReconnecting?.({ attempt, delayMs: delay })

    console.debug(`RSS SSE reconnecting in ${delay}ms (attempt ${this.reconnectAttempts})`)

    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null
      this.connect()
    }, delay)
  }

  isConnected(): boolean {
    return this.eventSource?.readyState === EventSource.OPEN
  }
}
