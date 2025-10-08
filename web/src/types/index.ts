/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

export interface User {
  id?: number
  username: string
  createdAt?: string
  updatedAt?: string
  auth_method?: string
}

export interface AuthResponse {
  user: User
  message?: string
}

export interface Instance {
  id: number
  name: string
  host: string
  username: string
  basicUsername?: string
  tlsSkipVerify: boolean
}

export interface InstanceFormData {
  name: string
  host: string
  username?: string
  password?: string
  basicUsername?: string
  basicPassword?: string
  tlsSkipVerify: boolean
}

export interface InstanceError {
  id: number
  instanceId: number
  errorType: string
  errorMessage: string
  occurredAt: string
}

export interface InstanceResponse extends Instance {
  connected: boolean
  hasDecryptionError: boolean
  recentErrors?: InstanceError[]
}

export interface InstanceCapabilities {
  supportsTorrentCreation: boolean
  supportsSetTags: boolean
  supportsTrackerHealth: boolean
  supportsTrackerEditing: boolean
  supportsRenameTorrent: boolean
  supportsRenameFile: boolean
  supportsRenameFolder: boolean
  webAPIVersion?: string
}

export interface TorrentTracker {
  url: string
  status: number
  num_peers: number
  num_seeds: number
  num_leechers: number
  num_downloaded: number
  msg: string
}

export interface Torrent {
  added_on: number
  amount_left: number
  auto_tmm: boolean
  availability: number
  category: string
  completed: number
  completion_on: number
  content_path: string
  dl_limit: number
  dlspeed: number
  download_path: string
  downloaded: number
  downloaded_session: number
  eta: number
  f_l_piece_prio: boolean
  force_start: boolean
  hash: string
  infohash_v1: string
  infohash_v2: string
  popularity: number
  private: boolean
  last_activity: number
  magnet_uri: string
  max_ratio: number
  max_seeding_time: number
  max_inactive_seeding_time?: number
  name: string
  num_complete: number
  num_incomplete: number
  num_leechs: number
  num_seeds: number
  priority: number
  progress: number
  ratio: number
  ratio_limit: number
  reannounce: number
  save_path: string
  seeding_time: number
  seeding_time_limit: number
  inactive_seeding_time_limit?: number
  seen_complete: number
  seq_dl: boolean
  size: number
  state: string
  super_seeding: boolean
  tags: string
  time_active: number
  total_size: number
  tracker: string
  trackers_count: number
  trackers?: TorrentTracker[]
  tracker_health?: "unregistered" | "tracker_down"
  up_limit: number
  uploaded: number
  uploaded_session: number
  upspeed: number
}

export interface TorrentStats {
  total: number
  downloading: number
  seeding: number
  paused: number
  error: number
  totalDownloadSpeed?: number
  totalUploadSpeed?: number
  totalSize?: number
}

export interface CacheMetadata {
  source: "cache" | "fresh"
  age: number
  isStale: boolean
  nextRefresh?: string
}

export interface TorrentCounts {
  status: Record<string, number>
  categories: Record<string, number>
  tags: Record<string, number>
  trackers: Record<string, number>
  total: number
}

export interface TorrentResponse {
  torrents: Torrent[]
  total: number
  stats?: TorrentStats
  counts?: TorrentCounts
  categories?: Record<string, Category>
  tags?: string[]
  serverState?: ServerState
  cacheMetadata?: CacheMetadata
  hasMore?: boolean
}

// Simplified MainData - only used for Dashboard server stats
export interface MainData {
  rid: number
  serverState?: ServerState
  server_state?: ServerState
}

export interface Category {
  name: string
  savePath: string
}

export interface ServerState {
  connection_status: string
  dht_nodes: number
  dl_info_data: number
  dl_info_speed: number
  dl_rate_limit: number
  up_info_data: number
  up_info_speed: number
  up_rate_limit: number
  queueing: boolean
  use_alt_speed_limits: boolean
  refresh_interval: number
  alltime_dl?: number
  alltime_ul?: number
  total_wasted_session?: number
  global_ratio?: string
  total_peer_connections?: number
  free_space_on_disk?: number
  average_time_queue?: number
  queued_io_jobs?: number
  read_cache_hits?: string
  read_cache_overload?: string
  total_buffers_size?: number
  total_queued_size?: number
  write_cache_overload?: string
  last_external_address_v4?: string
  last_external_address_v6?: string
}

export interface AppPreferences {
  // Core limits and speeds (fully supported)
  dl_limit: number
  up_limit: number
  alt_dl_limit: number
  alt_up_limit: number

  // Queue management (fully supported)
  queueing_enabled: boolean
  max_active_downloads: number
  max_active_torrents: number
  max_active_uploads: number
  max_active_checking_torrents: number

  // Network settings (fully supported)
  listen_port: number
  random_port: boolean // Deprecated in qBittorrent but functional
  upnp: boolean
  upnp_lease_duration: number

  // Connection protocol & interface (fully supported)
  bittorrent_protocol: number
  utp_tcp_mixed_mode: number

  // Network interface fields - displayed as read-only in UI
  // TODO: These fields are configurable in qBittorrent API but go-qbittorrent library
  // lacks the required endpoints for proper dropdown selection:
  // - /api/v2/app/networkInterfaceList
  // - /api/v2/app/networkInterfaceAddressList
  // Currently shown as read-only inputs displaying actual qBittorrent values
  current_network_interface: string // Shows current interface (empty = auto-detect)
  current_interface_address: string // Shows current interface IP address

  announce_ip: string
  reannounce_when_address_changed: boolean

  // Connection limits
  max_connec: number
  max_connec_per_torrent: number
  max_uploads: number
  max_uploads_per_torrent: number
  enable_multi_connections_from_same_ip: boolean

  // Advanced network
  outgoing_ports_min: number
  outgoing_ports_max: number
  limit_lan_peers: boolean
  limit_tcp_overhead: boolean
  limit_utp_rate: boolean
  peer_tos: number
  socket_backlog_size: number
  send_buffer_watermark: number
  send_buffer_low_watermark: number
  send_buffer_watermark_factor: number
  max_concurrent_http_announces: number
  request_queue_size: number
  stop_tracker_timeout: number

  // Seeding limits
  max_ratio_enabled: boolean
  max_ratio: number
  max_seeding_time_enabled: boolean
  max_seeding_time: number

  // Paths and file management
  save_path: string
  temp_path: string
  temp_path_enabled: boolean
  auto_tmm_enabled: boolean
  save_resume_data_interval: number

  // Startup behavior
  start_paused_enabled: boolean // NOTE: Not supported by qBittorrent API - handled via localStorage

  // BitTorrent protocol (fully supported)
  dht: boolean
  pex: boolean
  lsd: boolean
  encryption: number
  anonymous_mode: boolean

  // Proxy settings (fully supported)
  proxy_type: number | string // Note: number (pre-4.5.x), string (post-4.6.x)
  proxy_ip: string
  proxy_port: number
  proxy_username: string
  proxy_password: string
  proxy_auth_enabled: boolean
  proxy_peer_connections: boolean
  proxy_torrents_only: boolean
  proxy_hostname_lookup: boolean

  // Security & filtering
  ip_filter_enabled: boolean
  ip_filter_path: string
  ip_filter_trackers: boolean
  banned_IPs: string
  block_peers_on_privileged_ports: boolean
  resolve_peer_countries: boolean

  // Performance & disk I/O (mostly supported)
  async_io_threads: number
  hashing_threads: number
  file_pool_size: number
  disk_cache: number
  disk_cache_ttl: number
  disk_queue_size: number
  disk_io_type: number
  disk_io_read_mode: number // Limited API support
  disk_io_write_mode: number
  checking_memory_use: number
  memory_working_set_limit: number // May not be settable via API
  enable_coalesce_read_write: boolean

  // Upload behavior (partial support)
  upload_choking_algorithm: number
  upload_slots_behavior: number

  // Peer management
  peer_turnover: number
  peer_turnover_cutoff: number
  peer_turnover_interval: number

  // Embedded tracker
  enable_embedded_tracker: boolean
  embedded_tracker_port: number
  embedded_tracker_port_forwarding: boolean

  // Scheduler
  scheduler_enabled: boolean
  schedule_from_hour: number
  schedule_from_min: number
  schedule_to_hour: number
  schedule_to_min: number
  scheduler_days: number

  // Web UI (read-only reference)
  web_ui_port: number
  web_ui_username: string
  use_https: boolean
  web_ui_address: string
  web_ui_ban_duration: number
  web_ui_clickjacking_protection_enabled: boolean
  web_ui_csrf_protection_enabled: boolean
  web_ui_custom_http_headers: string
  web_ui_domain_list: string
  web_ui_host_header_validation_enabled: boolean
  web_ui_https_cert_path: string
  web_ui_https_key_path: string
  web_ui_max_auth_fail_count: number
  web_ui_reverse_proxies_list: string
  web_ui_reverse_proxy_enabled: boolean
  web_ui_secure_cookie_enabled: boolean
  web_ui_session_timeout: number
  web_ui_upnp: boolean
  web_ui_use_custom_http_headers_enabled: boolean

  // Additional commonly used fields
  add_trackers_enabled: boolean
  add_trackers: string
  announce_to_all_tiers: boolean
  announce_to_all_trackers: boolean

  // File management and content layout
  torrent_content_layout: string
  incomplete_files_ext: boolean
  preallocate_all: boolean
  excluded_file_names_enabled: boolean
  excluded_file_names: string

  // Category behavior
  category_changed_tmm_enabled: boolean
  save_path_changed_tmm_enabled: boolean
  use_category_paths_in_manual_mode: boolean

  // Torrent behavior
  torrent_changed_tmm_enabled: boolean
  torrent_stop_condition: string

  // Miscellaneous
  alternative_webui_enabled: boolean
  alternative_webui_path: string
  auto_delete_mode: number
  autorun_enabled: boolean
  autorun_on_torrent_added_enabled: boolean
  autorun_on_torrent_added_program: string
  autorun_program: string
  bypass_auth_subnet_whitelist: string
  bypass_auth_subnet_whitelist_enabled: boolean
  bypass_local_auth: boolean
  dont_count_slow_torrents: boolean
  export_dir: string
  export_dir_fin: string
  idn_support_enabled: boolean
  locale: string
  performance_warning: boolean
  recheck_completed_torrents: boolean
  refresh_interval: number
  resume_data_storage_type: string
  slow_torrent_dl_rate_threshold: number
  slow_torrent_inactive_timer: number
  slow_torrent_ul_rate_threshold: number
  ssrf_mitigation: boolean
  validate_https_tracker_certificate: boolean

  // RSS settings
  rss_auto_downloading_enabled: boolean
  rss_download_repack_proper_episodes: boolean
  rss_max_articles_per_feed: number
  rss_processing_enabled: boolean
  rss_refresh_interval: number
  rss_smart_episode_filters: string

  // Dynamic DNS
  dyndns_domain: string
  dyndns_enabled: boolean
  dyndns_password: string
  dyndns_service: number
  dyndns_username: string

  // Mail notifications
  mail_notification_auth_enabled: boolean
  mail_notification_email: string
  mail_notification_enabled: boolean
  mail_notification_password: string
  mail_notification_sender: string
  mail_notification_smtp: string
  mail_notification_ssl_enabled: boolean
  mail_notification_username: string

  // Scan directories (structured as empty object in go-qbittorrent)
  scan_dirs: Record<string, unknown>

  // Add catch-all for any additional fields from the API
  [key: string]: unknown
}

// qBittorrent application information
export interface QBittorrentBuildInfo {
  qt: string
  libtorrent: string
  boost: string
  openssl: string
  bitness: number
  platform?: string
}

export interface QBittorrentAppInfo {
  version: string
  webAPIVersion?: string
  buildInfo?: QBittorrentBuildInfo
}

// Torrent Creation Types
export type TorrentFormat = "v1" | "v2" | "hybrid"
export type TorrentCreationStatus = "Queued" | "Running" | "Finished" | "Failed"

export interface TorrentCreationParams {
  sourcePath: string
  torrentFilePath?: string
  private?: boolean
  format?: TorrentFormat
  optimizeAlignment?: boolean
  paddedFileSizeLimit?: number
  pieceSize?: number
  comment?: string
  source?: string
  trackers?: string[]
  urlSeeds?: string[]
  startSeeding?: boolean
}

export interface TorrentCreationTask {
  taskID: string
  sourcePath: string
  torrentFilePath?: string
  pieceSize: number
  private: boolean
  format?: TorrentFormat
  optimizeAlignment?: boolean
  paddedFileSizeLimit?: number
  status: TorrentCreationStatus
  comment?: string
  source?: string
  trackers?: string[]
  urlSeeds?: string[]
  timeAdded: string
  timeStarted?: string
  timeFinished?: string
  progress?: number
  errorMessage?: string
}

export interface TorrentCreationTaskResponse {
  taskID: string
}
