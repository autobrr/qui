/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { AuthResponse, InstanceResponse, TorrentResponse, MainData, User, } from "@/types"
import { getApiBaseUrl, } from "./base-url"

const API_BASE = getApiBaseUrl()

class ApiClient {
  private async request<T,>(
    endpoint: string,
    options?: RequestInit,
  ): Promise<T> {
    const response = await fetch(`${API_BASE}${endpoint}`, {
      ...options,
      headers: {
        "Content-Type": "application/json",
        ...options?.headers,
      },
      credentials: "include",
    },)

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
      throw new Error(errorMessage,)
    }

    return response.json()
  }

  // Auth endpoints
  async checkAuth(): Promise<User> {
    return this.request<User>("/auth/me",)
  }

  async checkSetupRequired(): Promise<boolean> {
    try {
      const response = await fetch(`${API_BASE}/auth/check-setup`, {
        method: "GET",
        credentials: "include",
      },)
      const data = await response.json()
      return data.setupRequired || false
    } catch {
      return false
    }
  }

  async setup(username: string, password: string,): Promise<AuthResponse> {
    return this.request<AuthResponse>("/auth/setup", {
      method: "POST",
      body: JSON.stringify({ username, password, },),
    },)
  }

  async login(username: string, password: string,): Promise<AuthResponse> {
    return this.request<AuthResponse>("/auth/login", {
      method: "POST",
      body: JSON.stringify({ username, password, },),
    },)
  }

  async logout(): Promise<void> {
    return this.request("/auth/logout", { method: "POST", },)
  }

  // Instance endpoints
  async getInstances(): Promise<InstanceResponse[]> {
    return this.request<InstanceResponse[]>("/instances",)
  }

  async createInstance(data: {
    name: string
    host: string
    username: string
    password: string
    basicUsername?: string
    basicPassword?: string
  },): Promise<InstanceResponse> {
    return this.request<InstanceResponse>("/instances", {
      method: "POST",
      body: JSON.stringify(data,),
    },)
  }

  async updateInstance(
    id: number,
    data: Partial<{
      name: string
      host: string
      username: string
      password: string
      basicUsername?: string
      basicPassword?: string
    }>,
  ): Promise<InstanceResponse> {
    return this.request<InstanceResponse>(`/instances/${id}`, {
      method: "PUT",
      body: JSON.stringify(data,),
    },)
  }

  async deleteInstance(id: number,): Promise<void> {
    return this.request(`/instances/${id}`, { method: "DELETE", },)
  }

  async testConnection(id: number,): Promise<{ connected: boolean; message: string }> {
    return this.request(`/instances/${id}/test`, { method: "POST", },)
  }

  async getInstanceStats(id: number,): Promise<{
    instanceId: number
    connected: boolean
    torrents: {
      total: number
      downloading: number
      seeding: number
      paused: number
      error: number
      completed: number
    }
    speeds: {
      download: number
      upload: number
    }
    serverState?: {
      downloadSpeed: number
      uploadSpeed: number
      downloaded: number
      uploaded: number
      freeSpace: number
    }
  }> {
    return this.request(`/instances/${id}/stats`,)
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
    },
  ): Promise<TorrentResponse> {
    const searchParams = new URLSearchParams()
    if (params.page !== undefined) searchParams.set("page", params.page.toString(),)
    if (params.limit !== undefined) searchParams.set("limit", params.limit.toString(),)
    if (params.sort) searchParams.set("sort", params.sort,)
    if (params.order) searchParams.set("order", params.order,)
    if (params.search) searchParams.set("search", params.search,)
    if (params.filters) searchParams.set("filters", JSON.stringify(params.filters,),)

    return this.request<TorrentResponse>(
      `/instances/${instanceId}/torrents?${searchParams}`,
    )
  }

  async syncMainData(instanceId: number, rid: number,): Promise<MainData> {
    return this.request<MainData>(`/instances/${instanceId}/torrents/sync?rid=${rid}`,)
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
    },
  ): Promise<{ success: boolean; message?: string }> {
    const formData = new FormData()
    // Append each file with the same field name "torrent"
    if (data.torrentFiles) {
      data.torrentFiles.forEach(file => formData.append("torrent", file,),)
    }
    if (data.urls) formData.append("urls", data.urls.join("\n",),)
    if (data.category) formData.append("category", data.category,)
    if (data.tags) formData.append("tags", data.tags.join(",",),)
    if (data.startPaused !== undefined) formData.append("paused", data.startPaused.toString(),)
    if (data.autoTMM !== undefined) formData.append("autoTMM", data.autoTMM.toString(),)
    if (data.skipHashCheck !== undefined) formData.append("skip_checking", data.skipHashCheck.toString(),)
    // Only send savePath if autoTMM is false or undefined
    if (data.savePath && !data.autoTMM) formData.append("savepath", data.savePath,)

    const response = await fetch(`${API_BASE}/instances/${instanceId}/torrents`, {
      method: "POST",
      body: formData,
      credentials: "include",
    },)

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
      throw new Error(errorMessage,)
    }

    return response.json()
  }

  async pauseTorrent(instanceId: number, hash: string,): Promise<void> {
    return this.request(`/instances/${instanceId}/torrents/${hash}/pause`, {
      method: "PUT",
    },)
  }

  async resumeTorrent(instanceId: number, hash: string,): Promise<void> {
    return this.request(`/instances/${instanceId}/torrents/${hash}/resume`, {
      method: "PUT",
    },)
  }

  async deleteTorrent(
    instanceId: number,
    hash: string,
    deleteFiles: boolean = false,
  ): Promise<void> {
    return this.request(
      `/instances/${instanceId}/torrents/${hash}?deleteFiles=${deleteFiles}`,
      { method: "DELETE", },
    )
  }

  async bulkAction(
    instanceId: number,
    data: {
      hashes: string[]
      action: "pause" | "resume" | "delete" | "recheck" | "reannounce" | "increasePriority" | "decreasePriority" | "topPriority" | "bottomPriority" | "setCategory" | "addTags" | "removeTags" | "setTags" | "toggleAutoTMM"
      deleteFiles?: boolean
      category?: string
      tags?: string  // Comma-separated tags string
      enable?: boolean  // For toggleAutoTMM
    },
  ): Promise<void> {
    return this.request(`/instances/${instanceId}/torrents/bulk-action`, {
      method: "POST",
      body: JSON.stringify(data,),
    },)
  }

  // Torrent Details
  async getTorrentProperties(instanceId: number, hash: string,): Promise<any> {
    return this.request(`/instances/${instanceId}/torrents/${hash}/properties`,)
  }

  async getTorrentTrackers(instanceId: number, hash: string,): Promise<any[]> {
    return this.request(`/instances/${instanceId}/torrents/${hash}/trackers`,)
  }

  async getTorrentFiles(instanceId: number, hash: string,): Promise<any[]> {
    return this.request(`/instances/${instanceId}/torrents/${hash}/files`,)
  }

  // Categories & Tags
  async getCategories(instanceId: number,): Promise<Record<string, { name: string; savePath: string }>> {
    return this.request(`/instances/${instanceId}/categories`,)
  }

  async createCategory(instanceId: number, name: string, savePath?: string,): Promise<{ message: string }> {
    return this.request(`/instances/${instanceId}/categories`, {
      method: "POST",
      body: JSON.stringify({ name, savePath: savePath || "", },),
    },)
  }

  async editCategory(instanceId: number, name: string, savePath: string,): Promise<{ message: string }> {
    return this.request(`/instances/${instanceId}/categories`, {
      method: "PUT",
      body: JSON.stringify({ name, savePath, },),
    },)
  }

  async removeCategories(instanceId: number, categories: string[],): Promise<{ message: string }> {
    return this.request(`/instances/${instanceId}/categories`, {
      method: "DELETE",
      body: JSON.stringify({ categories, },),
    },)
  }

  async getTags(instanceId: number,): Promise<string[]> {
    return this.request(`/instances/${instanceId}/tags`,)
  }

  async createTags(instanceId: number, tags: string[],): Promise<{ message: string }> {
    return this.request(`/instances/${instanceId}/tags`, {
      method: "POST",
      body: JSON.stringify({ tags, },),
    },)
  }

  async deleteTags(instanceId: number, tags: string[],): Promise<{ message: string }> {
    return this.request(`/instances/${instanceId}/tags`, {
      method: "DELETE",
      body: JSON.stringify({ tags, },),
    },)
  }

  // User endpoints
  async changePassword(currentPassword: string, newPassword: string,): Promise<void> {
    return this.request("/auth/change-password", {
      method: "PUT",
      body: JSON.stringify({ currentPassword, newPassword, },),
    },)
  }

  // API Key endpoints
  async getApiKeys(): Promise<{
    id: number
    name: string
    key?: string
    createdAt: string
    lastUsedAt?: string
  }[]> {
    return this.request("/api-keys",)
  }

  async createApiKey(name: string,): Promise<{ id: number; key: string; name: string }> {
    return this.request("/api-keys", {
      method: "POST",
      body: JSON.stringify({ name, },),
    },)
  }

  async deleteApiKey(id: number,): Promise<void> {
    return this.request(`/api-keys/${id}`, { method: "DELETE", },)
  }

  // Theme License endpoints
  async validateThemeLicense(licenseKey: string,): Promise<{
    valid: boolean
    themeName?: string
    expiresAt?: string
    message?: string
    error?: string
  }> {
    return this.request("/themes/license/validate", {
      method: "POST",
      body: JSON.stringify({ licenseKey, },),
    },)
  }

  async getLicensedThemes(): Promise<{ hasPremiumAccess: boolean }> {
    return this.request("/themes/licensed",)
  }

  async getAllLicenses(): Promise<Array<{
    licenseKey: string
    themeName: string
    status: string
    createdAt: string
  }>> {
    return this.request("/themes/licenses",)
  }


  async deleteThemeLicense(licenseKey: string,): Promise<{ message: string }> {
    return this.request(`/themes/license/${licenseKey}`, { method: "DELETE", },)
  }

  async refreshThemeLicenses(): Promise<{ message: string }> {
    return this.request("/themes/license/refresh", { method: "POST", },)
  }
}

export const api = new ApiClient()