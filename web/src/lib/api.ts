/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type {
  AddTorrentResponse,
  AppPreferences,
  AsyncIndexerFilteringState,
  AuthResponse,
  BackupManifest,
  BackupRun,
  BackupRunsResponse,
  BackupSettings,
  Category,
  CrossInstanceTorrent,
  CrossSeedApplyResponse,
  CrossSeedAutomationSettings,
  CrossSeedAutomationSettingsPatch,
  CrossSeedAutomationStatus,
  CrossSeedInstanceResult,
  CrossSeedRun,
  CrossSeedSearchRun,
  CrossSeedSearchSettings,
  CrossSeedSearchSettingsPatch,
  CrossSeedSearchStatus,
  CrossSeedTorrentInfo,
  CrossSeedTorrentSearchResponse,
  CrossSeedTorrentSearchSelection,
  DiscoverJackettResponse,
  DuplicateTorrentMatch,
  ExternalProgram,
  ExternalProgramCreate,
  ExternalProgramExecute,
  ExternalProgramExecuteResponse,
  ExternalProgramUpdate,
  IndexerActivityStatus,
  InstanceCapabilities,
  InstanceCrossSeedCompletionSettings,
  InstanceFormData,
  InstanceReannounceActivity,
  InstanceReannounceCandidate,
  InstanceResponse,
  QBittorrentAppInfo,
  RestoreMode,
  RestorePlan,
  RestoreResult,
  SearchHistoryResponse,
  SortedPeersResponse,
  WebSeed,
  TorrentCreationParams,
  TorrentCreationTask,
  TorrentCreationTaskResponse,
  TorrentFile,
  TorrentFilters,
  TorrentProperties,
  TorrentResponse,
  TorrentTracker,
  IndexerResponse,
  TorznabIndexer,
  TorznabIndexerError,
  TorznabIndexerFormData,
  TorznabIndexerHealth,
  TorznabIndexerLatencyStats,
  TorznabRecentSearch,
  TorznabSearchCacheMetadata,
  TorznabSearchCacheStats,
  TorznabSearchRequest,
  TorznabSearchResponse,
  TorznabSearchResult,
  TrackerCustomization,
  TrackerCustomizationInput,
  TrackerRule,
  TrackerRuleInput,
  User,
  DashboardSettings,
  DashboardSettingsInput
} from "@/types"
import { getApiBaseUrl, withBasePath } from "./base-url"

const API_BASE = getApiBaseUrl()

const normalizeExcludedIndexerMap = (excluded?: Record<string, string>): Record<number, string> | undefined => {
  if (!excluded) {
    return undefined
  }

  const normalizedEntries = Object.entries(excluded)
    .map(([key, value]) => {
      const numericKey = Number(key)
      if (Number.isNaN(numericKey)) {
        return null
      }
      return [numericKey, value] as const
    })
    .filter((entry): entry is readonly [number, string] => entry !== null)

  if (normalizedEntries.length === 0) {
    return undefined
  }

  return Object.fromEntries(normalizedEntries) as Record<number, string>
}

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
      const errorMessage = await this.extractErrorMessage(response)
      this.handleAuthError(response.status, endpoint, errorMessage)
      throw new Error(errorMessage)
    }

    // Handle empty responses (like 204 No Content)
    if (response.status === 204 || response.headers.get("content-length") === "0") {
      return undefined as T
    }

    return response.json()
  }

  private async extractErrorMessage(response: Response): Promise<string> {
    const fallbackMessage = `HTTP error! status: ${response.status}`

    try {
      const rawBody = await response.text()
      if (!rawBody) {
        return fallbackMessage
      }

      try {
        const errorData = JSON.parse(rawBody) as { error?: string; message?: string }
        const parsedMessage = errorData?.error ?? errorData?.message
        if (typeof parsedMessage === "string" && parsedMessage.trim().length > 0) {
          return parsedMessage
        }
      } catch {
        const trimmed = rawBody.trim()
        if (trimmed.length > 0) {
          return trimmed
        }
      }

      return fallbackMessage
    } catch {
      return fallbackMessage
    }
  }

  private handleAuthError(status: number, endpoint: string, errorMessage: string): void {
    if (!this.shouldForceLogout(status, endpoint, errorMessage)) {
      return
    }

    window.location.href = withBasePath("/login")
    throw new Error("Session expired")
  }

  private shouldForceLogout(status: number, endpoint: string, errorMessage: string): boolean {
    if (typeof window === "undefined") {
      return false
    }

    if (this.isAuthCheckEndpoint(endpoint)) {
      return false
    }

    const pathname = window.location.pathname
    if (pathname.startsWith(withBasePath("/login")) || pathname.startsWith(withBasePath("/setup"))) {
      return false
    }

    if (status === 401) {
      return true
    }

    if (status === 403) {
      const normalizedMessage = errorMessage.trim().toLowerCase()
      return normalizedMessage === "unauthorized"
    }

    return false
  }

  private isAuthCheckEndpoint(endpoint: string): boolean {
    return endpoint === "/auth/me" || endpoint === "/auth/validate"
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

  async validate(): Promise<{
    username: string
    auth_method?: string
    profile_picture?: string
  }> {
    return this.request("/auth/validate")
  }

  async getOIDCConfig(): Promise<{
    enabled: boolean
    authorizationUrl: string
    state: string
    disableBuiltInLogin: boolean
    issuerUrl: string
  }> {
    try {
      return await this.request("/auth/oidc/config")
    } catch {
      // Return default config if OIDC is not configured
      return {
        enabled: false,
        authorizationUrl: "",
        state: "",
        disableBuiltInLogin: false,
        issuerUrl: "",
      }
    }
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

  async updateInstanceStatus(
    id: number,
    isActive: boolean
  ): Promise<InstanceResponse> {
    return this.request<InstanceResponse>(`/instances/${id}/status`, {
      method: "PUT",
      body: JSON.stringify({ isActive }),
    })
  }

  async deleteInstance(id: number): Promise<void> {
    return this.request(`/instances/${id}`, { method: "DELETE" })
  }

  async testConnection(id: number): Promise<{ connected: boolean; message: string }> {
    return this.request(`/instances/${id}/test`, { method: "POST" })
  }

  async getInstanceCapabilities(id: number): Promise<InstanceCapabilities> {
    return this.request<InstanceCapabilities>(`/instances/${id}/capabilities`)
  }

  async getInstanceReannounceActivity(
    instanceId: number,
    limit?: number
  ): Promise<InstanceReannounceActivity[]> {
    const query = typeof limit === "number" ? `?limit=${limit}` : ""
    return this.request<InstanceReannounceActivity[]>(`/instances/${instanceId}/reannounce/activity${query}`)
  }

  async getInstanceReannounceCandidates(
    instanceId: number
  ): Promise<InstanceReannounceCandidate[]> {
    return this.request<InstanceReannounceCandidate[]>(`/instances/${instanceId}/reannounce/candidates`)
  }

  async reorderInstances(instanceIds: number[]): Promise<InstanceResponse[]> {
    return this.request<InstanceResponse[]>("/instances/order", {
      method: "PUT",
      body: JSON.stringify({ instanceIds }),
    })
  }

  async getBackupSettings(instanceId: number): Promise<BackupSettings> {
    return this.request<BackupSettings>(`/instances/${instanceId}/backups/settings`)
  }

  async updateBackupSettings(instanceId: number, payload: {
    enabled: boolean
    hourlyEnabled: boolean
    dailyEnabled: boolean
    weeklyEnabled: boolean
    monthlyEnabled: boolean
    keepHourly: number
    keepDaily: number
    keepWeekly: number
    keepMonthly: number
    includeCategories: boolean
    includeTags: boolean
  }): Promise<BackupSettings> {
    return this.request<BackupSettings>(`/instances/${instanceId}/backups/settings`, {
      method: "PUT",
      body: JSON.stringify(payload),
    })
  }

  async triggerBackup(instanceId: number, payload: { kind?: string; requestedBy?: string } = {}): Promise<BackupRun> {
    return this.request<BackupRun>(`/instances/${instanceId}/backups/run`, {
      method: "POST",
      body: JSON.stringify(payload),
    })
  }

  async listBackupRuns(instanceId: number, params?: { limit?: number; offset?: number }): Promise<BackupRunsResponse> {
    const search = new URLSearchParams()
    if (params?.limit !== undefined) search.set("limit", params.limit.toString())
    if (params?.offset !== undefined) search.set("offset", params.offset.toString())

    const query = search.toString()
    const suffix = query ? `?${query}` : ""
    return this.request<BackupRunsResponse>(`/instances/${instanceId}/backups/runs${suffix}`)
  }

  async getBackupManifest(instanceId: number, runId: number): Promise<BackupManifest> {
    return this.request<BackupManifest>(`/instances/${instanceId}/backups/runs/${runId}/manifest`)
  }

  async deleteBackupRun(instanceId: number, runId: number): Promise<{ deleted: boolean }> {
    return this.request<{ deleted: boolean }>(`/instances/${instanceId}/backups/runs/${runId}`, {
      method: "DELETE",
    })
  }

  async deleteAllBackups(instanceId: number): Promise<{ deleted: boolean }> {
    return this.request<{ deleted: boolean }>(`/instances/${instanceId}/backups/runs`, {
      method: "DELETE",
    })
  }

  async importBackupManifest(instanceId: number, manifestFile: File): Promise<BackupRun> {
    const formData = new FormData()
    formData.append("archive", manifestFile)

    const response = await fetch(`${API_BASE}/instances/${instanceId}/backups/import`, {
      method: "POST",
      body: formData,
      credentials: "include",
    })

    if (!response.ok) {
      const errorMessage = await this.extractErrorMessage(response)
      this.handleAuthError(response.status, `/instances/${instanceId}/backups/import`, errorMessage)
      throw new Error(errorMessage)
    }

    return response.json()
  }

  async previewRestore(
    instanceId: number,
    runId: number,
    payload: { mode?: RestoreMode; excludeHashes?: string[] } = {}
  ): Promise<RestorePlan> {
    return this.request<RestorePlan>(`/instances/${instanceId}/backups/runs/${runId}/restore/preview`, {
      method: "POST",
      body: JSON.stringify(payload),
    })
  }

  async executeRestore(
    instanceId: number,
    runId: number,
    payload: {
      mode: RestoreMode
      dryRun?: boolean
      excludeHashes?: string[]
      startPaused?: boolean
      skipHashCheck?: boolean
      autoResumeVerified?: boolean
    }
  ): Promise<RestoreResult> {
    return this.request<RestoreResult>(`/instances/${instanceId}/backups/runs/${runId}/restore`, {
      method: "POST",
      body: JSON.stringify(payload),
    })
  }

  getBackupDownloadUrl(instanceId: number, runId: number, format?: string): string {
    const url = new URL(withBasePath(`/api/instances/${instanceId}/backups/runs/${runId}/download`), window.location.origin)
    if (format && format !== 'zip') {
      url.searchParams.set('format', format)
    }
    return url.toString()
  }

  getBackupTorrentDownloadUrl(instanceId: number, runId: number, torrentHash: string): string {
    const encodedHash = encodeURIComponent(torrentHash)
    return withBasePath(`/api/instances/${instanceId}/backups/runs/${runId}/items/${encodedHash}/download`)
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
      filters?: TorrentFilters
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

  async getCrossInstanceTorrents(
    params: {
      page?: number
      limit?: number
      sort?: string
      order?: "asc" | "desc"
      search?: string
      filters?: TorrentFilters
    }
  ): Promise<TorrentResponse> {
    const searchParams = new URLSearchParams()
    if (params.page !== undefined) searchParams.set("page", params.page.toString())
    if (params.limit !== undefined) searchParams.set("limit", params.limit.toString())
    if (params.sort) searchParams.set("sort", params.sort)
    if (params.order) searchParams.set("order", params.order)
    if (params.search) searchParams.set("search", params.search)
    if (params.filters) searchParams.set("filters", JSON.stringify(params.filters))

    type RawCrossInstanceTorrent = Omit<CrossInstanceTorrent, "instanceId" | "instanceName"> & {
      instanceId?: number
      instanceName?: string
      instance_id?: number
      instance_name?: string
    }

    const normalizeCrossInstanceTorrents = (
      torrents?: RawCrossInstanceTorrent[] | null
    ): CrossInstanceTorrent[] | undefined => {
      if (!torrents) {
        return undefined
      }

      let needsNormalization = false

      for (const torrent of torrents) {
        if (torrent.instanceId === undefined || torrent.instanceName === undefined) {
          needsNormalization = true
          break
        }
      }

      if (!needsNormalization) {
        return torrents as CrossInstanceTorrent[]
      }

      const normalizedTorrents: CrossInstanceTorrent[] = []

      torrents.forEach(torrent => {
        const instanceId = torrent.instanceId ?? torrent.instance_id
        const instanceName = torrent.instanceName ?? torrent.instance_name

        if (instanceId === undefined || instanceName === undefined) {
          console.error("Missing instance fields in cross-instance torrent:", torrent)
          return
        }

        normalizedTorrents.push({
          ...torrent,
          instanceId,
          instanceName,
        })
      })

      return normalizedTorrents
    }

    const response = await this.request<TorrentResponse>(
      `/torrents/cross-instance?${searchParams}`
    )

    const normalizedCrossInstanceTorrents = normalizeCrossInstanceTorrents(
      (response.crossInstanceTorrents ?? response.cross_instance_torrents) as RawCrossInstanceTorrent[] | undefined
    )

    if (normalizedCrossInstanceTorrents) {
      response.crossInstanceTorrents = normalizedCrossInstanceTorrents
      response.cross_instance_torrents = normalizedCrossInstanceTorrents
    }

    return response
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
      useDownloadPath?: boolean
      downloadPath?: string
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
      indexerId?: number
    }
  ): Promise<AddTorrentResponse> {
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
    if (data.useDownloadPath !== undefined) formData.append("useDownloadPath", data.useDownloadPath.toString())
    if (data.downloadPath) formData.append("downloadPath", data.downloadPath)
    if (data.indexerId) formData.append("indexer_id", data.indexerId.toString())

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

  async checkTorrentDuplicates(instanceId: number, hashes: string[]): Promise<{ duplicates: DuplicateTorrentMatch[] }> {
    return this.request<{ duplicates: DuplicateTorrentMatch[] }>(`/instances/${instanceId}/torrents/check-duplicates`, {
      method: "POST",
      body: JSON.stringify({ hashes }),
    })
  }


  async bulkAction(
    instanceId: number,
    data: {
      hashes: string[]
      action: "pause" | "resume" | "delete" | "recheck" | "reannounce" | "increasePriority" | "decreasePriority" | "topPriority" | "bottomPriority" | "setCategory" | "addTags" | "removeTags" | "setTags" | "toggleAutoTMM" | "forceStart" | "setShareLimit" | "setUploadLimit" | "setDownloadLimit" | "setLocation" | "editTrackers" | "addTrackers" | "removeTrackers" | "toggleSequentialDownload"
      deleteFiles?: boolean
      category?: string
      tags?: string  // Comma-separated tags string
      enable?: boolean  // For toggleAutoTMM
      selectAll?: boolean  // When true, apply to all torrents matching filters
      filters?: TorrentFilters
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

  async analyzeTorrentForCrossSeedSearch(
    instanceId: number,
    hash: string
  ): Promise<CrossSeedTorrentInfo> {
    type RawTorrentInfo = {
      instance_id?: number
      instance_name?: string
      hash?: string
      name: string
      category?: string
      size?: number
      progress?: number
      total_files?: number
      matching_files?: number
      file_count?: number
      content_type?: string
      search_type?: string
      search_categories?: number[]
      required_caps?: string[]
      available_indexers?: number[]
      filtered_indexers?: number[]
      excluded_indexers?: Record<string, string>
      content_matches?: string[]
      content_filtering_completed?: boolean
    }

    const raw = await this.request<RawTorrentInfo>(
      `/cross-seed/torrents/${instanceId}/${hash}/analyze`,
      { method: "GET" }
    )

    return {
      instanceId: raw.instance_id,
      instanceName: raw.instance_name,
      hash: raw.hash,
      name: raw.name,
      category: raw.category,
      size: raw.size,
      progress: raw.progress,
      totalFiles: raw.total_files,
      matchingFiles: raw.matching_files,
      fileCount: raw.file_count,
      contentType: raw.content_type,
      searchType: raw.search_type,
      searchCategories: raw.search_categories,
      requiredCaps: raw.required_caps,
      availableIndexers: raw.available_indexers,
      filteredIndexers: raw.filtered_indexers,
      excludedIndexers: normalizeExcludedIndexerMap(raw.excluded_indexers),
      contentMatches: raw.content_matches,
      contentFilteringCompleted: raw.content_filtering_completed,
    }
  }

  async getAsyncFilteringStatus(
    instanceId: number,
    hash: string
  ): Promise<AsyncIndexerFilteringState> {
    type RawAsyncFilteringState = {
      capabilities_completed: boolean
      content_completed: boolean
      capability_indexers: number[]
      filtered_indexers: number[]
      excluded_indexers: Record<string, string>
      content_matches: string[]
    }

    const raw = await this.request<RawAsyncFilteringState>(
      `/cross-seed/torrents/${instanceId}/${hash}/async-status`,
      { method: "GET" }
    )

    return {
      capabilitiesCompleted: raw.capabilities_completed,
      contentCompleted: raw.content_completed,
      capabilityIndexers: raw.capability_indexers,
      filteredIndexers: raw.filtered_indexers,
      excludedIndexers: normalizeExcludedIndexerMap(raw.excluded_indexers) || {},
      contentMatches: raw.content_matches,
    }
  }

  async searchCrossSeedTorrent(
    instanceId: number,
    hash: string,
    options: {
      query?: string
      limit?: number
      indexerIds?: number[]
      findIndividualEpisodes?: boolean
      cacheMode?: "bypass"
    } = {}
  ): Promise<CrossSeedTorrentSearchResponse> {
    const body: Record<string, unknown> = {}
    const trimmedQuery = options.query?.trim()
    if (trimmedQuery) {
      body.query = trimmedQuery
    }
    if (options.limit !== undefined) {
      body.limit = options.limit
    }
    if (options.indexerIds && options.indexerIds.length > 0) {
      body.indexer_ids = options.indexerIds
    }
    if (options.findIndividualEpisodes !== undefined) {
      body.find_individual_episodes = options.findIndividualEpisodes
    }
    if (options.cacheMode) {
      body.cache_mode = options.cacheMode
    }

    type RawTorrentInfo = {
      instance_id?: number
      instance_name?: string
      hash?: string
      name?: string
      category?: string
      size?: number
      progress?: number
      total_files?: number
      matching_files?: number
      file_count?: number
      content_type?: string
      search_type?: string
      search_categories?: number[]
      required_caps?: string[]
      available_indexers?: number[]
      filtered_indexers?: number[]
      excluded_indexers?: Record<string, string>
      content_matches?: string[]
    } | null

    type RawSearchResult = {
      indexer: string
      indexer_id: number
      title: string
      download_url: string
      info_url?: string
      size: number
      seeders: number
      leechers: number
      category_id: number
      category_name: string
      publish_date: string
      download_volume_factor: number
      upload_volume_factor: number
      guid: string
      imdb_id?: string
      tvdb_id?: string
      match_reason?: string
      match_score?: number
    }

    type RawSearchResponse = {
      source_torrent: RawTorrentInfo
      results?: RawSearchResult[]
      cache?: TorznabSearchCacheMetadata
    }

    const response = await this.request<RawSearchResponse>(`/cross-seed/torrents/${instanceId}/${hash}/search`, {
      method: "POST",
      body: Object.keys(body).length > 0 ? JSON.stringify(body) : undefined,
    })

    const normalizeTorrentInfo = (torrent?: RawTorrentInfo): CrossSeedTorrentInfo => ({
      instanceId: torrent?.instance_id ?? undefined,
      instanceName: torrent?.instance_name ?? undefined,
      hash: torrent?.hash ?? undefined,
      name: torrent?.name ?? trimmedQuery ?? "",
      category: torrent?.category ?? undefined,
      size: torrent?.size ?? undefined,
      progress: torrent?.progress ?? undefined,
      totalFiles: torrent?.total_files ?? undefined,
      matchingFiles: torrent?.matching_files ?? undefined,
      fileCount: torrent?.file_count ?? undefined,
      contentType: torrent?.content_type ?? undefined,
      searchType: torrent?.search_type ?? undefined,
      searchCategories: torrent?.search_categories ?? undefined,
      requiredCaps: torrent?.required_caps ?? undefined,
      availableIndexers: torrent?.available_indexers ?? undefined,
      filteredIndexers: torrent?.filtered_indexers ?? undefined,
      excludedIndexers: normalizeExcludedIndexerMap(torrent?.excluded_indexers),
      contentMatches: torrent?.content_matches ?? undefined,
    })

    return {
      sourceTorrent: normalizeTorrentInfo(response.source_torrent ?? null),
      results: (response.results ?? []).map((result): CrossSeedTorrentSearchResponse["results"][number] => ({
        indexer: result.indexer,
        indexerId: result.indexer_id,
        title: result.title,
        downloadUrl: result.download_url,
        infoUrl: result.info_url,
        size: result.size,
        seeders: result.seeders,
        leechers: result.leechers,
        categoryId: result.category_id,
        categoryName: result.category_name,
        publishDate: result.publish_date,
        downloadVolumeFactor: result.download_volume_factor,
        uploadVolumeFactor: result.upload_volume_factor,
        guid: result.guid,
        imdbId: result.imdb_id ?? undefined,
        tvdbId: result.tvdb_id ?? undefined,
        matchReason: result.match_reason ?? undefined,
        matchScore: result.match_score ?? 0,
      })),
      cache: response.cache,
    }
  }

  async applyCrossSeedSearchResults(
    instanceId: number,
    hash: string,
    payload: {
      selections: CrossSeedTorrentSearchSelection[]
      useTag: boolean
      tagName?: string
      startPaused?: boolean
      findIndividualEpisodes?: boolean
    }
  ): Promise<CrossSeedApplyResponse> {
    const body: Record<string, unknown> = {
      selections: payload.selections.map(selection => {
        const item: Record<string, unknown> = {
          indexer_id: selection.indexerId,
          indexer: selection.indexer,
          download_url: selection.downloadUrl,
          title: selection.title,
        }
        if (selection.guid) {
          item.guid = selection.guid
        }
        return item
      }),
      use_tag: payload.useTag,
    }

    if (payload.tagName) {
      body.tag_name = payload.tagName
    }
    if (payload.startPaused !== undefined) {
      body.start_paused = payload.startPaused
    }
    if (payload.findIndividualEpisodes !== undefined) {
      body.find_individual_episodes = payload.findIndividualEpisodes
    }

    type RawMatchedTorrent = {
      hash?: string
      name?: string
      progress?: number
      size?: number
    }

    type RawInstanceResult = {
      instance_id: number
      instance_name: string
      success: boolean
      status: string
      message?: string
      matched_torrent?: RawMatchedTorrent
    }

    type RawApplyResult = {
      title: string
      indexer: string
      torrent_name?: string
      success: boolean
      instance_results?: RawInstanceResult[]
      error?: string
    }

    type RawApplyResponse = {
      results?: RawApplyResult[]
    }

    const response = await this.request<RawApplyResponse>(`/cross-seed/torrents/${instanceId}/${hash}/apply`, {
      method: "POST",
      body: JSON.stringify(body),
    })

    return {
      results: (response.results ?? []).map((result): CrossSeedApplyResponse["results"][number] => ({
        title: result.title,
        indexer: result.indexer,
        torrentName: result.torrent_name ?? undefined,
        success: result.success,
        instanceResults: (result.instance_results ?? []).map((instance): CrossSeedInstanceResult => ({
          instanceId: instance.instance_id,
          instanceName: instance.instance_name,
          success: instance.success,
          status: instance.status,
          message: instance.message,
          matchedTorrent: instance.matched_torrent
            ? {
                hash: instance.matched_torrent.hash ?? "",
                name: instance.matched_torrent.name ?? "",
                progress: instance.matched_torrent.progress ?? 0,
                size: instance.matched_torrent.size ?? 0,
              }
            : undefined,
        })),
        error: result.error ?? undefined,
      })),
    }
  }

  async getCrossSeedSettings(): Promise<CrossSeedAutomationSettings> {
    return this.request<CrossSeedAutomationSettings>("/cross-seed/settings")
  }

  async updateCrossSeedSettings(payload: CrossSeedAutomationSettings): Promise<CrossSeedAutomationSettings> {
    return this.request<CrossSeedAutomationSettings>("/cross-seed/settings", {
      method: "PUT",
      body: JSON.stringify(payload),
    })
  }

  async patchCrossSeedSettings(payload: CrossSeedAutomationSettingsPatch): Promise<CrossSeedAutomationSettings> {
    return this.request<CrossSeedAutomationSettings>("/cross-seed/settings", {
      method: "PATCH",
      body: JSON.stringify(payload),
    })
  }

  async getInstanceCompletionSettings(instanceId: number): Promise<InstanceCrossSeedCompletionSettings> {
    return this.request<InstanceCrossSeedCompletionSettings>(`/cross-seed/completion/${instanceId}`)
  }

  async updateInstanceCompletionSettings(
    instanceId: number,
    payload: Omit<InstanceCrossSeedCompletionSettings, "instanceId">
  ): Promise<InstanceCrossSeedCompletionSettings> {
    return this.request<InstanceCrossSeedCompletionSettings>(`/cross-seed/completion/${instanceId}`, {
      method: "PUT",
      body: JSON.stringify(payload),
    })
  }

  async getCrossSeedSearchSettings(): Promise<CrossSeedSearchSettings> {
    return this.request<CrossSeedSearchSettings>("/cross-seed/search/settings")
  }

  async patchCrossSeedSearchSettings(payload: CrossSeedSearchSettingsPatch): Promise<CrossSeedSearchSettings> {
    return this.request<CrossSeedSearchSettings>("/cross-seed/search/settings", {
      method: "PATCH",
      body: JSON.stringify(payload),
    })
  }

  async getCrossSeedStatus(): Promise<CrossSeedAutomationStatus> {
    return this.request<CrossSeedAutomationStatus>("/cross-seed/status")
  }

  async listCrossSeedRuns(params?: { limit?: number; offset?: number }): Promise<CrossSeedRun[]> {
    const search = new URLSearchParams()
    if (params?.limit !== undefined) search.set("limit", params.limit.toString())
    if (params?.offset !== undefined) search.set("offset", params.offset.toString())
    const query = search.toString()
    const suffix = query ? `?${query}` : ""
    return this.request<CrossSeedRun[]>(`/cross-seed/runs${suffix}`)
  }

  async getCrossSeedSearchStatus(): Promise<CrossSeedSearchStatus> {
    return this.request<CrossSeedSearchStatus>("/cross-seed/search/status")
  }

  async startCrossSeedSearchRun(payload: {
    instanceId: number
    categories: string[]
    tags: string[]
    intervalSeconds: number
    indexerIds: number[]
    cooldownMinutes: number
  }): Promise<CrossSeedSearchRun> {
    return this.request<CrossSeedSearchRun>("/cross-seed/search/run", {
      method: "POST",
      body: JSON.stringify(payload),
    })
  }

  async cancelCrossSeedSearchRun(): Promise<void> {
    await this.request("/cross-seed/search/run/cancel", { method: "POST" })
  }

  async listCrossSeedSearchRuns(instanceId: number, params?: { limit?: number; offset?: number }): Promise<CrossSeedSearchRun[]> {
    const search = new URLSearchParams({ instanceId: instanceId.toString() })
    if (params?.limit !== undefined) search.set("limit", params.limit.toString())
    if (params?.offset !== undefined) search.set("offset", params.offset.toString())
    return this.request<CrossSeedSearchRun[]>(`/cross-seed/search/runs?${search.toString()}`)
  }

  async triggerCrossSeedRun(payload: { dryRun?: boolean } = {}): Promise<CrossSeedRun> {
    return this.request<CrossSeedRun>("/cross-seed/run", {
      method: "POST",
      body: JSON.stringify(payload),
    })
  }

  // Torrent Details
  async getTorrentProperties(instanceId: number, hash: string): Promise<TorrentProperties> {
    return this.request<TorrentProperties>(`/instances/${instanceId}/torrents/${hash}/properties`)
  }

  async getTorrentTrackers(instanceId: number, hash: string): Promise<TorrentTracker[]> {
    return this.request<TorrentTracker[]>(`/instances/${instanceId}/torrents/${hash}/trackers`)
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

  async renameTorrent(instanceId: number, hash: string, name: string): Promise<void> {
    return this.request(`/instances/${instanceId}/torrents/${hash}/rename`, {
      method: "PUT",
      body: JSON.stringify({ name }),
    })
  }

  async renameTorrentFile(instanceId: number, hash: string, oldPath: string, newPath: string): Promise<void> {
    return this.request(`/instances/${instanceId}/torrents/${hash}/rename-file`, {
      method: "PUT",
      body: JSON.stringify({ oldPath, newPath }),
    })
  }

  async renameTorrentFolder(instanceId: number, hash: string, oldPath: string, newPath: string): Promise<void> {
    return this.request(`/instances/${instanceId}/torrents/${hash}/rename-folder`, {
      method: "PUT",
      body: JSON.stringify({ oldPath, newPath }),
    })
  }

  async getTorrentFiles(instanceId: number, hash: string, options?: { refresh?: boolean }): Promise<TorrentFile[]> {
    const query = options?.refresh ? "?refresh=1" : ""
    return this.request<TorrentFile[]>(`/instances/${instanceId}/torrents/${hash}/files${query}`)
  }

  async setTorrentFilePriority(instanceId: number, hash: string, indices: number[], priority: number): Promise<void> {
    return this.request(`/instances/${instanceId}/torrents/${hash}/files`, {
      method: "PUT",
      body: JSON.stringify({ indices, priority }),
    })
  }

  async exportTorrent(instanceId: number, hash: string): Promise<{ blob: Blob; filename: string | null }> {
    const encodedHash = encodeURIComponent(hash)
    const response = await fetch(`${API_BASE}/instances/${instanceId}/torrents/${encodedHash}/export`, {
      method: "GET",
      credentials: "include",
    })

    if (!response.ok) {
      const errorMessage = await this.extractErrorMessage(response)
      this.handleAuthError(response.status, `/instances/${instanceId}/torrents/${encodedHash}/export`, errorMessage)
      throw new Error(errorMessage)
    }

    const blob = await response.blob()
    const disposition = response.headers.get("content-disposition")
    const filename = parseContentDispositionFilename(disposition)

    return { blob, filename }
  }

  async getTorrentPeers(instanceId: number, hash: string): Promise<SortedPeersResponse> {
    return this.request<SortedPeersResponse>(`/instances/${instanceId}/torrents/${hash}/peers`)
  }

  async getTorrentWebSeeds(instanceId: number, hash: string): Promise<WebSeed[]> {
    return this.request<WebSeed[]>(`/instances/${instanceId}/torrents/${hash}/webseeds`)
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

  // Torrent Creator
  async createTorrent(instanceId: number, params: TorrentCreationParams): Promise<TorrentCreationTaskResponse> {
    return this.request(`/instances/${instanceId}/torrent-creator`, {
      method: "POST",
      body: JSON.stringify(params),
    })
  }

  async getTorrentCreationTasks(instanceId: number, taskID?: string): Promise<TorrentCreationTask[]> {
    const query = taskID ? `?taskID=${encodeURIComponent(taskID)}` : ""
    return this.request(`/instances/${instanceId}/torrent-creator/status${query}`)
  }

  async getActiveTaskCount(instanceId: number): Promise<number> {
    const response = await this.request<{ count: number }>(`/instances/${instanceId}/torrent-creator/count`)
    return response.count
  }

  async downloadTorrentFile(instanceId: number, taskID: string): Promise<void> {
    const response = await fetch(
      `${API_BASE}/instances/${instanceId}/torrent-creator/${encodeURIComponent(taskID)}/file`,
      {
        method: "GET",
        credentials: "include",
      }
    )

    if (!response.ok) {
      throw new Error(`Failed to download torrent file: ${response.statusText}`)
    }

    // Get filename from Content-Disposition header
    const contentDisposition = response.headers.get("Content-Disposition")
    let filename = `${taskID}.torrent`
    if (contentDisposition) {
      const filenameMatch = contentDisposition.match(/filename="?([^";]+)"?/)
      if (filenameMatch) {
        filename = filenameMatch[1]
      }
    }

    // Create blob and download
    const blob = await response.blob()
    const url = window.URL.createObjectURL(blob)
    const a = document.createElement("a")
    a.href = url
    a.download = filename
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    window.URL.revokeObjectURL(url)
  }

  async deleteTorrentCreationTask(instanceId: number, taskID: string): Promise<{ message: string }> {
    return this.request(`/instances/${instanceId}/torrent-creator/${encodeURIComponent(taskID)}`, {
      method: "DELETE",
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

  async getActiveTrackers(instanceId: number): Promise<Record<string, string>> {
    return this.request(`/instances/${instanceId}/trackers`)
  }

  async listTrackerRules(instanceId: number): Promise<TrackerRule[]> {
    return this.request(`/instances/${instanceId}/tracker-rules`)
  }

  async createTrackerRule(instanceId: number, payload: TrackerRuleInput): Promise<TrackerRule> {
    return this.request(`/instances/${instanceId}/tracker-rules`, {
      method: "POST",
      body: JSON.stringify(payload),
    })
  }

  async updateTrackerRule(instanceId: number, ruleId: number, payload: TrackerRuleInput): Promise<TrackerRule> {
    return this.request(`/instances/${instanceId}/tracker-rules/${ruleId}`, {
      method: "PUT",
      body: JSON.stringify(payload),
    })
  }

  async deleteTrackerRule(instanceId: number, ruleId: number): Promise<void> {
    return this.request(`/instances/${instanceId}/tracker-rules/${ruleId}`, {
      method: "DELETE",
    })
  }

  async reorderTrackerRules(instanceId: number, orderedIds: number[]): Promise<void> {
    return this.request(`/instances/${instanceId}/tracker-rules/order`, {
      method: "PUT",
      body: JSON.stringify({ orderedIds }),
    })
  }

  async applyTrackerRules(instanceId: number): Promise<void> {
    return this.request(`/instances/${instanceId}/tracker-rules/apply`, {
      method: "POST",
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
  }> {
    return this.request("/client-api-keys", {
      method: "POST",
      body: JSON.stringify(data),
    })
  }

  async deleteClientApiKey(id: number): Promise<void> {
    return this.request(`/client-api-keys/${id}`, { method: "DELETE" })
  }

  // License endpoints
  async activateLicense(licenseKey: string): Promise<{
    valid: boolean
    expiresAt?: string
    message?: string
    error?: string
  }> {
    return this.request("/license/activate", {
      method: "POST",
      body: JSON.stringify({ licenseKey }),
    })
  }

  async validateLicense(licenseKey: string): Promise<{
    valid: boolean
    productName?: string
    expiresAt?: string
    message?: string
    error?: string
  }> {
    return this.request("/license/validate", {
      method: "POST",
      body: JSON.stringify({ licenseKey }),
    })
  }

  async getLicensedThemes(): Promise<{ hasPremiumAccess: boolean }> {
    return this.request("/license/licensed")
  }

  async getAllLicenses(): Promise<Array<{
    licenseKey: string
    productName: string
    status: string
    createdAt: string
  }>> {
    return this.request("/license/licenses")
  }


  async deleteLicense(licenseKey: string): Promise<{ message: string }> {
    return this.request(`/license/${licenseKey}`, { method: "DELETE" })
  }

  async refreshLicenses(): Promise<{ message: string }> {
    return this.request("/license/refresh", { method: "POST" })
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

  async getQBittorrentAppInfo(instanceId: number): Promise<QBittorrentAppInfo> {
    return this.request<QBittorrentAppInfo>(`/instances/${instanceId}/app-info`)
  }

  async getLatestVersion(): Promise<{
    tag_name: string
    name?: string
    html_url: string
    published_at: string
  } | null> {
    try {
      const response = await this.request<{
        tag_name: string
        name?: string
        html_url: string
        published_at: string
      } | null>("/version/latest")

      // Treat empty responses as no update available
      return response ?? null
    } catch {
      // Return null if no update available (204 status) or any error
      return null
    }
  }

  async getTrackerIcons(): Promise<Record<string, string>> {
    return this.request<Record<string, string>>("/tracker-icons")
  }

  // External Programs endpoints
  async listExternalPrograms(): Promise<ExternalProgram[]> {
    return this.request<ExternalProgram[]>("/external-programs")
  }

  async createExternalProgram(program: ExternalProgramCreate): Promise<ExternalProgram> {
    return this.request<ExternalProgram>("/external-programs", {
      method: "POST",
      body: JSON.stringify(program),
    })
  }

  async updateExternalProgram(id: number, program: ExternalProgramUpdate): Promise<ExternalProgram> {
    return this.request<ExternalProgram>(`/external-programs/${id}`, {
      method: "PUT",
      body: JSON.stringify(program),
    })
  }

  async deleteExternalProgram(id: number): Promise<void> {
    return this.request(`/external-programs/${id}`, {
      method: "DELETE",
    })
  }

  async executeExternalProgram(request: ExternalProgramExecute): Promise<ExternalProgramExecuteResponse> {
    return this.request<ExternalProgramExecuteResponse>("/external-programs/execute", {
      method: "POST",
      body: JSON.stringify(request),
    })
  }

  // Tracker Customization endpoints
  async listTrackerCustomizations(): Promise<TrackerCustomization[]> {
    return this.request<TrackerCustomization[]>("/tracker-customizations")
  }

  async createTrackerCustomization(data: TrackerCustomizationInput): Promise<TrackerCustomization> {
    return this.request<TrackerCustomization>("/tracker-customizations", {
      method: "POST",
      body: JSON.stringify(data),
    })
  }

  async updateTrackerCustomization(id: number, data: TrackerCustomizationInput): Promise<TrackerCustomization> {
    return this.request<TrackerCustomization>(`/tracker-customizations/${id}`, {
      method: "PUT",
      body: JSON.stringify(data),
    })
  }

  async deleteTrackerCustomization(id: number): Promise<void> {
    return this.request(`/tracker-customizations/${id}`, {
      method: "DELETE",
    })
  }

  // Dashboard Settings endpoints
  async getDashboardSettings(): Promise<DashboardSettings> {
    return this.request<DashboardSettings>("/dashboard-settings")
  }

  async updateDashboardSettings(data: DashboardSettingsInput): Promise<DashboardSettings> {
    return this.request<DashboardSettings>("/dashboard-settings", {
      method: "PUT",
      body: JSON.stringify(data),
    })
  }

  // Torznab Indexer endpoints
  async listTorznabIndexers(): Promise<TorznabIndexer[]> {
    return this.request<TorznabIndexer[]>("/torznab/indexers")
  }

  async getTorznabIndexer(id: number): Promise<TorznabIndexer> {
    return this.request<TorznabIndexer>(`/torznab/indexers/${id}`)
  }

  async createTorznabIndexer(data: TorznabIndexerFormData): Promise<IndexerResponse> {
    return this.request<IndexerResponse>("/torznab/indexers", {
      method: "POST",
      body: JSON.stringify(data),
    })
  }

  async updateTorznabIndexer(id: number, data: Partial<TorznabIndexerFormData>): Promise<IndexerResponse> {
    return this.request<IndexerResponse>(`/torznab/indexers/${id}`, {
      method: "PUT",
      body: JSON.stringify(data),
    })
  }

  async syncTorznabCaps(id: number): Promise<TorznabIndexer> {
    return this.request<TorznabIndexer>(`/torznab/indexers/${id}/caps/sync`, {
      method: "POST",
    })
  }

  async deleteTorznabIndexer(id: number): Promise<void> {
    return this.request<void>(`/torznab/indexers/${id}`, {
      method: "DELETE",
    })
  }

  async testTorznabIndexer(id: number): Promise<{ status: string }> {
    return this.request<{ status: string }>(`/torznab/indexers/${id}/test`, {
      method: "POST",
    })
  }

  async getIndexerActivityStatus(): Promise<IndexerActivityStatus> {
    return this.request<IndexerActivityStatus>("/torznab/activity")
  }

  async getSearchHistory(limit?: number): Promise<SearchHistoryResponse> {
    const params = limit ? `?limit=${limit}` : ""
    return this.request<SearchHistoryResponse>(`/torznab/search/history${params}`)
  }

  async discoverJackettIndexers(baseUrl: string, apiKey: string): Promise<DiscoverJackettResponse> {
    return this.request<DiscoverJackettResponse>("/torznab/indexers/discover", {
      method: "POST",
      body: JSON.stringify({ base_url: baseUrl, api_key: apiKey }),
    })
  }

  async searchTorznab(request: TorznabSearchRequest): Promise<TorznabSearchResponse> {
    const response = await this.request<{
      results: Array<{
        indexer: string
        indexer_id: number
        title: string
        download_url: string
        info_url?: string
        size: number
        seeders: number
        leechers: number
        category_id: number
        category_name: string
        publish_date: string
        download_volume_factor: number
        upload_volume_factor: number
        guid: string
        imdb_id?: string
        tvdb_id?: string
        source?: string
        collection?: string
        group?: string
      }>
      total: number
      cache?: TorznabSearchCacheMetadata
    }>("/torznab/search", {
      method: "POST",
      body: JSON.stringify(request),
    })

    return {
      ...response,
      results: response.results.map((result): TorznabSearchResult => ({
        indexer: result.indexer,
        indexerId: result.indexer_id,
        title: result.title,
        downloadUrl: result.download_url,
        infoUrl: result.info_url,
        size: result.size,
        seeders: result.seeders,
        leechers: result.leechers,
        categoryId: result.category_id,
        categoryName: result.category_name,
        publishDate: result.publish_date,
        downloadVolumeFactor: result.download_volume_factor,
        uploadVolumeFactor: result.upload_volume_factor,
        guid: result.guid,
        imdbId: result.imdb_id,
        tvdbId: result.tvdb_id,
        source: result.source,
        collection: result.collection,
        group: result.group,
      }))
    }
  }

  async getRecentTorznabSearches(limit?: number, scope?: string): Promise<TorznabRecentSearch[]> {
    const params = new URLSearchParams()
    if (limit && limit > 0) {
      params.set("limit", String(limit))
    }
    if (scope) {
      params.set("scope", scope)
    }
    const query = params.toString() ? `?${params.toString()}` : ""
    return this.request<TorznabRecentSearch[]>(`/torznab/search/recent${query}`)
  }

  async getTorznabSearchCacheStats(): Promise<TorznabSearchCacheStats> {
    return this.request<TorznabSearchCacheStats>("/torznab/search/cache")
  }

  async updateTorznabSearchCacheSettings(ttlMinutes: number): Promise<TorznabSearchCacheStats> {
    return this.request<TorznabSearchCacheStats>("/torznab/search/cache/settings", {
      method: "PUT",
      body: JSON.stringify({ ttlMinutes }),
    })
  }

  async getAllIndexerHealth(): Promise<TorznabIndexerHealth[]> {
    return this.request<TorznabIndexerHealth[]>("/torznab/indexers/health")
  }

  async getIndexerHealth(id: number): Promise<TorznabIndexerHealth> {
    return this.request<TorznabIndexerHealth>(`/torznab/indexers/${id}/health`)
  }

  async getIndexerErrors(id: number, limit?: number): Promise<TorznabIndexerError[]> {
    const params = limit ? `?limit=${limit}` : ""
    return this.request<TorznabIndexerError[]>(`/torznab/indexers/${id}/errors${params}`)
  }

  async getIndexerStats(id: number): Promise<TorznabIndexerLatencyStats[]> {
    return this.request<TorznabIndexerLatencyStats[]>(`/torznab/indexers/${id}/stats`)
  }
}

export const api = new ApiClient()

function parseContentDispositionFilename(header: string | null): string | null {
  if (!header) {
    return null
  }

  const utf8Match = header.match(/filename\*=UTF-8''([^;]+)/i)
  if (utf8Match?.[1]) {
    try {
      return decodeURIComponent(utf8Match[1])
    } catch {
      return utf8Match[1]
    }
  }

  const quotedMatch = header.match(/filename="?([^";]+)"?/i)
  if (quotedMatch?.[1]) {
    return quotedMatch[1]
  }

  return null
}
