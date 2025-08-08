export interface User {
  id: number
  username: string
  createdAt: string
  updatedAt: string
}

export interface AuthResponse {
  user: User
  message?: string
}

export interface Instance {
  id: number
  name: string
  host: string
  port: number
  username: string
  basicUsername?: string
  isActive: boolean
  lastConnectedAt?: string
  createdAt: string
  updatedAt: string
}

export interface Torrent {
  hash: string
  name: string
  size: number
  progress: number
  dlspeed: number
  upspeed: number
  priority: number
  num_seeds: number
  num_leechs: number
  ratio: number
  eta: number
  state: string
  category: string
  tags: string
  added_on: number
  completion_on: number
  tracker: string
  dl_limit: number
  up_limit: number
  downloaded: number
  uploaded: number
  downloaded_session: number
  uploaded_session: number
  amount_left: number
  save_path: string
  completed: number
  ratio_limit: number
  seen_complete: number
  last_activity: number
  time_active: number
  auto_tmm: boolean
  total_size: number
  max_ratio: number
  max_seeding_time: number
  seeding_time_limit: number
}

export interface TorrentStats {
  total: number
  downloading: number
  seeding: number
  paused: number
  error: number
  totalDownloadSpeed?: number
  totalUploadSpeed?: number
}

export interface CacheMetadata {
  source: 'cache' | 'fresh'
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
  cacheMetadata?: CacheMetadata
}

export interface MainData {
  rid: number
  fullUpdate: boolean
  torrents?: Record<string, Torrent>
  torrentsRemoved?: string[]
  categories?: Record<string, Category>
  categoriesRemoved?: string[]
  tags?: string[]
  tagsRemoved?: string[]
  serverState?: ServerState
}

export interface Category {
  name: string
  save_path: string
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
  // User statistics
  alltime_dl?: number
  alltime_ul?: number
  total_wasted_session?: number
  global_ratio?: string
  total_peer_connections?: number
}