/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type {
  AppPreferences,
  AuthResponse,
  Category,
  InstanceFormData,
  InstanceResponse,
  TorrentResponse,
  User
} from "@/types"
import { getApiBaseUrl, withBasePath } from "./base-url"

const API_BASE = getApiBaseUrl()

class ApiClient {
  private async request<T>(
    endpoint: string,
    options?: RequestInit
  ): Promise<T> {
    const response = await fetch(`${API_BASE}${endpoint}`, {
      ...options,
      headers: {
        "Content-Type": "application/json",
        ...options?.headers,
      },
      credentials: "include",
    })

    if (!response.ok) {
      if (response.status === 401 && !window.location.pathname.startsWith(withBasePath("/login")) && !window.location.pathname.startsWith(withBasePath("/setup"))) {
        window.location.href = withBasePath("/login")
        throw new Error("Session expired")
      }

      let errorMessage = `HTTP error! status: ${response.status}`
      try {
        const errorData = await response.json()
        errorMessage = errorData.error || errorData.message || errorMessage
      } catch {
        try {
          const errorText = await response.text()
          errorMessage = errorText || errorMessage
        } catch {
          // nothing to see here
        }
      }
      throw new Error(errorMessage)
    }

    // Handle empty responses (like 204 No Content)
    if (response.status === 204 || response.headers.get("content-length") === "0") {
      return undefined as T
    }

    return response.json()
  }

  // Auth endpoints
  async checkAuth(): Promise<User> {
    return this.request<User>("/auth/me")
  }

  async checkSetupRequired(): Promise<boolean> {
    try {
      const response = await fetch(`${API_BASE}/auth/check-setup`, {
        method: "GET",
        credentials: "include",
      })
      const data = await response.json()
      return data.setupRequired || false
    } catch {
      return false
    }
  }

  async setup(username: string, password: string): Promise<AuthResponse> {
    return this.request<AuthResponse>("/auth/setup", {
      method: "POST",
      body: JSON.stringify({ username, password }),
    })
  }

  async login(username: string, password: string, rememberMe = false): Promise<AuthResponse> {
    return this.request<AuthResponse>("/auth/login", {
      method: "POST",
      body: JSON.stringify({ username, password, remember_me: rememberMe }),
    })
  }

  async logout(): Promise<void> {
    return this.request("/auth/logout", { method: "POST" })
  }

  // Instance endpoints
  async getInstances(): Promise<InstanceResponse[]> {
    return this.request<InstanceResponse[]>("/instances")
  }

  async createInstance(data: InstanceFormData): Promise<InstanceResponse> {
    return this.request<InstanceResponse>("/instances", {
      method: "POST",
      body: JSON.stringify(data),
    })
  }

  async updateInstance(
    id: number,
    data: Partial<InstanceFormData>
  ): Promise<InstanceResponse> {
    return this.request<InstanceResponse>(`/instances/${id}`, {
      method: "PUT",
      body: JSON.stringify(data),
    })
  }

  async deleteInstance(id: number): Promise<void> {
    return this.request(`/instances/${id}`, { method: "DELETE" })
  }

  async testConnection(id: number): Promise<{ connected: boolean; message: string }> {
    return this.request(`/instances/${id}/test`, { method: "POST" })
  }


  // Torrent endpoints
  async getTorrents(
    instanceId: number,
    params: {
      page?: number
      limit?: number
      sort?: string
      order?: "asc" | "desc"
      search?: string
      filters?: any
    }
  ): Promise<TorrentResponse> {
    const searchParams = new URLSearchParams()
    if (params.page !== undefined) searchParams.set("page", params.page.toString())
    if (params.limit !== undefined) searchParams.set("limit", params.limit.toString())
    if (params.sort) searchParams.set("sort", params.sort)
    if (params.order) searchParams.set("order", params.order)
    if (params.search) searchParams.set("search", params.search)
    if (params.filters) searchParams.set("filters", JSON.stringify(params.filters))

    return this.request<TorrentResponse>(
      `/instances/${instanceId}/torrents?${searchParams}`
    )
  }

  async addTorrent(
    instanceId: number,
    data: {
      torrentFiles?: File[]
      urls?: string[]
      category?: string
      tags?: string[]
      startPaused?: boolean
      savePath?: string
      autoTMM?: boolean
      skipHashCheck?: boolean
      sequentialDownload?: boolean
      firstLastPiecePrio?: boolean
      limitUploadSpeed?: number
      limitDownloadSpeed?: number
      limitRatio?: number
      limitSeedTime?: number
      contentLayout?: string
      rename?: string
    }
  ): Promise<{ success: boolean; message?: string }> {
    const formData = new FormData()
    // Append each file with the same field name "torrent"
    if (data.torrentFiles) {
      data.torrentFiles.forEach(file => formData.append("torrent", file))
    }
    if (data.urls) formData.append("urls", data.urls.join("\n"))
    if (data.category) formData.append("category", data.category)
    if (data.tags) formData.append("tags", data.tags.join(","))
    if (data.startPaused !== undefined) formData.append("paused", data.startPaused.toString())
    if (data.autoTMM !== undefined) formData.append("autoTMM", data.autoTMM.toString())
    if (data.skipHashCheck !== undefined) formData.append("skip_checking", data.skipHashCheck.toString())
    if (data.sequentialDownload !== undefined) formData.append("sequentialDownload", data.sequentialDownload.toString())
    if (data.firstLastPiecePrio !== undefined) formData.append("firstLastPiecePrio", data.firstLastPiecePrio.toString())
    if (data.limitUploadSpeed !== undefined && data.limitUploadSpeed > 0) formData.append("upLimit", data.limitUploadSpeed.toString())
    if (data.limitDownloadSpeed !== undefined && data.limitDownloadSpeed > 0) formData.append("dlLimit", data.limitDownloadSpeed.toString())
    if (data.limitRatio !== undefined && data.limitRatio > 0) formData.append("ratioLimit", data.limitRatio.toString())
    if (data.limitSeedTime !== undefined && data.limitSeedTime > 0) formData.append("seedingTimeLimit", data.limitSeedTime.toString())
    if (data.contentLayout) formData.append("contentLayout", data.contentLayout)
    if (data.rename) formData.append("rename", data.rename)
    // Only send savePath if autoTMM is false or undefined
    if (data.savePath && !data.autoTMM) formData.append("savepath", data.savePath)

    const response = await fetch(`${API_BASE}/instances/${instanceId}/torrents`, {
      method: "POST",
      body: formData,
      credentials: "include",
    })

    if (!response.ok) {
      let errorMessage = `HTTP error! status: ${response.status}`
      try {
        const errorData = await response.json()
        errorMessage = errorData.error || errorData.message || errorMessage
      } catch {
        try {
          const errorText = await response.text()
          errorMessage = errorText || errorMessage
        } catch {
          // nothing to see here
        }
      }
      throw new Error(errorMessage)
    }

    return response.json()
  }


  async bulkAction(
    instanceId: number,
    data: {
      hashes: string[]
      action: "pause" | "resume" | "delete" | "recheck" | "reannounce" | "increasePriority" | "decreasePriority" | "topPriority" | "bottomPriority" | "setCategory" | "addTags" | "removeTags" | "setTags" | "toggleAutoTMM" | "setShareLimit" | "setUploadLimit" | "setDownloadLimit" | "setLocation" | "editTrackers" | "addTrackers" | "removeTrackers"
      deleteFiles?: boolean
      category?: string
      tags?: string  // Comma-separated tags string
      enable?: boolean  // For toggleAutoTMM
      selectAll?: boolean  // When true, apply to all torrents matching filters
      filters?: {
        status: string[]
        categories: string[]
        tags: string[]
        trackers: string[]
      }
      search?: string  // Search query when selectAll is true
      excludeHashes?: string[]  // Hashes to exclude when selectAll is true
      ratioLimit?: number  // For setShareLimit action
      seedingTimeLimit?: number  // For setShareLimit action (minutes)
      inactiveSeedingTimeLimit?: number  // For setShareLimit action (minutes)
      uploadLimit?: number  // For setUploadLimit action (KB/s)
      downloadLimit?: number  // For setDownloadLimit action (KB/s)
      location?: string  // For setLocation action
      trackerOldURL?: string  // For editTrackers action
      trackerNewURL?: string  // For editTrackers action
      trackerURLs?: string  // For addTrackers/removeTrackers actions (newline-separated)
    }
  ): Promise<void> {
    return this.request(`/instances/${instanceId}/torrents/bulk-action`, {
      method: "POST",
      body: JSON.stringify(data),
    })
  }

  // Torrent Details
  async getTorrentProperties(instanceId: number, hash: string): Promise<any> {
    return this.request(`/instances/${instanceId}/torrents/${hash}/properties`)
  }

  async getTorrentTrackers(instanceId: number, hash: string): Promise<any[]> {
    return this.request(`/instances/${instanceId}/torrents/${hash}/trackers`)
  }

  async editTorrentTracker(instanceId: number, hash: string, oldURL: string, newURL: string): Promise<void> {
    return this.request(`/instances/${instanceId}/torrents/${hash}/trackers`, {
      method: "PUT",
      body: JSON.stringify({ oldURL, newURL }),
    })
  }

  async addTorrentTrackers(instanceId: number, hash: string, urls: string): Promise<void> {
    return this.request(`/instances/${instanceId}/torrents/${hash}/trackers`, {
      method: "POST",
      body: JSON.stringify({ urls }),
    })
  }

  async removeTorrentTrackers(instanceId: number, hash: string, urls: string): Promise<void> {
    return this.request(`/instances/${instanceId}/torrents/${hash}/trackers`, {
      method: "DELETE",
      body: JSON.stringify({ urls }),
    })
  }

  async getTorrentFiles(instanceId: number, hash: string): Promise<any[]> {
    return this.request(`/instances/${instanceId}/torrents/${hash}/files`)
  }

  async getTorrentPeers(instanceId: number, hash: string): Promise<any> {
    return this.request(`/instances/${instanceId}/torrents/${hash}/peers`)
  }

  async addPeersToTorrents(instanceId: number, hashes: string[], peers: string[]): Promise<void> {
    return this.request(`/instances/${instanceId}/torrents/add-peers`, {
      method: "POST",
      body: JSON.stringify({ hashes, peers }),
    })
  }

  async banPeers(instanceId: number, peers: string[]): Promise<void> {
    return this.request(`/instances/${instanceId}/torrents/ban-peers`, {
      method: "POST",
      body: JSON.stringify({ peers }),
    })
  }

  // Categories & Tags
  async getCategories(instanceId: number): Promise<Record<string, Category>> {
    return this.request(`/instances/${instanceId}/categories`)
  }

  async createCategory(instanceId: number, name: string, savePath?: string): Promise<{ message: string }> {
    return this.request(`/instances/${instanceId}/categories`, {
      method: "POST",
      body: JSON.stringify({ name, savePath: savePath || "" }),
    })
  }

  async editCategory(instanceId: number, name: string, savePath: string): Promise<{ message: string }> {
    return this.request(`/instances/${instanceId}/categories`, {
      method: "PUT",
      body: JSON.stringify({ name, savePath }),
    })
  }

  async removeCategories(instanceId: number, categories: string[]): Promise<{ message: string }> {
    return this.request(`/instances/${instanceId}/categories`, {
      method: "DELETE",
      body: JSON.stringify({ categories }),
    })
  }

  async getTags(instanceId: number): Promise<string[]> {
    return this.request(`/instances/${instanceId}/tags`)
  }

  async createTags(instanceId: number, tags: string[]): Promise<{ message: string }> {
    return this.request(`/instances/${instanceId}/tags`, {
      method: "POST",
      body: JSON.stringify({ tags }),
    })
  }

  async deleteTags(instanceId: number, tags: string[]): Promise<{ message: string }> {
    return this.request(`/instances/${instanceId}/tags`, {
      method: "DELETE",
      body: JSON.stringify({ tags }),
    })
  }

  // User endpoints
  async changePassword(currentPassword: string, newPassword: string): Promise<void> {
    return this.request("/auth/change-password", {
      method: "PUT",
      body: JSON.stringify({ currentPassword, newPassword }),
    })
  }

  // API Key endpoints
  async getApiKeys(): Promise<{
    id: number
    name: string
    key?: string
    createdAt: string
    lastUsedAt?: string
  }[]> {
    return this.request("/api-keys")
  }

  async createApiKey(name: string): Promise<{ id: number; key: string; name: string }> {
    return this.request("/api-keys", {
      method: "POST",
      body: JSON.stringify({ name }),
    })
  }

  async deleteApiKey(id: number): Promise<void> {
    return this.request(`/api-keys/${id}`, { method: "DELETE" })
  }

  // Client API Keys for proxy authentication
  async getClientApiKeys(): Promise<{
    id: number
    clientName: string
    instanceId: number
    createdAt: string
    lastUsedAt?: string
    instance?: {
      id: number
      name: string
      host: string
    } | null
  }[]> {
    return this.request("/client-api-keys")
  }

  async createClientApiKey(data: {
    clientName: string
    instanceId: number
  }): Promise<{
    key: string
    clientApiKey: {
      id: number
      clientName: string
      instanceId: number
      createdAt: string
    }
    instance?: {
      id: number
      name: string
      host: string
    }
    proxyUrl: string
    instructions: string
  }> {
    return this.request("/client-api-keys", {
      method: "POST",
      body: JSON.stringify(data),
    })
  }

  async deleteClientApiKey(id: number): Promise<void> {
    return this.request(`/client-api-keys/${id}`, { method: "DELETE" })
  }

  // Theme License endpoints
  async validateThemeLicense(licenseKey: string): Promise<{
    valid: boolean
    themeName?: string
    expiresAt?: string
    message?: string
    error?: string
  }> {
    return this.request("/themes/license/validate", {
      method: "POST",
      body: JSON.stringify({ licenseKey }),
    })
  }

  async getLicensedThemes(): Promise<{ hasPremiumAccess: boolean }> {
    return this.request("/themes/licensed")
  }

  async getAllLicenses(): Promise<Array<{
    licenseKey: string
    themeName: string
    status: string
    createdAt: string
  }>> {
    return this.request("/themes/licenses")
  }


  async deleteThemeLicense(licenseKey: string): Promise<{ message: string }> {
    return this.request(`/themes/license/${licenseKey}`, { method: "DELETE" })
  }

  async refreshThemeLicenses(): Promise<{ message: string }> {
    return this.request("/themes/license/refresh", { method: "POST" })
  }

  // Preferences endpoints
  async getInstancePreferences(instanceId: number): Promise<AppPreferences> {
    return this.request<AppPreferences>(`/instances/${instanceId}/preferences`)
  }

  async updateInstancePreferences(
    instanceId: number,
    preferences: Partial<AppPreferences>
  ): Promise<AppPreferences> {
    return this.request<AppPreferences>(`/instances/${instanceId}/preferences`, {
      method: "PATCH",
      body: JSON.stringify(preferences),
    })
  }

  async getAlternativeSpeedLimitsMode(instanceId: number): Promise<{ enabled: boolean }> {
    return this.request<{ enabled: boolean }>(`/instances/${instanceId}/alternative-speed-limits`)
  }

  async toggleAlternativeSpeedLimits(instanceId: number): Promise<{ enabled: boolean }> {
    return this.request<{ enabled: boolean }>(`/instances/${instanceId}/alternative-speed-limits/toggle`, {
      method: "POST",
    })
  }
}

export const api = new ApiClient()
