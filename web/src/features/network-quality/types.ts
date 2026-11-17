export interface QualitySample {
  started_at: string
  completed_at: string
  state: 'ok' | 'failed' | 'inconclusive' | 'error' | 'cancelled'
  stage: string
  address?: string
  dns_ms: number | null
  tcp_ms: number | null
  response_ms: number | null
  status?: number
  error?: string
}

export interface QualityComponent {
  id: string
  label: string
  weight: number
  effective_weight: number
  score: number | null
  reason?: string
  target?: string
  kind?: string
}

export interface QualityCategory {
  id: string
  label: string
  budget: number
  state: 'ready' | 'partial' | 'stale' | 'unavailable'
  score: number | null
  earned_points: number
  available_points: number
  components: QualityComponent[]
}

export type QualityScope = 'all' | 'responses' | 'target' | 'speed' | 'nat' | 'wifi'

export interface QualitySnapshot {
  scoring_version: string
  catalog_version: string
  profile_id: string
  profile_version: string
  scope: string
  run_id: string
  state: 'idle' | 'running' | 'completed' | 'cancelled' | 'timed_out'
  phase: 'responses' | 'speed' | 'nat' | 'wifi' | ''
  running_scope?: QualityScope
  running_target?: string
  include_speed: boolean
  started_at: string | null
  completed_at: string | null
  expires_at: string | null
  targets: {
    id: string
    name: string
    url: string
    family: 'ipv4' | 'ipv6'
    samples: QualitySample[]
  }[]
  speed: { server: string; location: string; download_mbps: number; upload_mbps: number } | null
  speed_at: string | null
  speed_error?: string
  nat: {
    server: string
    nat_type: string
    mapped: string
    local?: string
    binding?: string
    mapping: string
    filtering: string
    error?: string
  } | null
  nat_error?: string
  wireless: {
    observed_at: string
    aps: {
      id: string
      name?: string
      state: 'ok' | 'error'
      error?: string
      links: number
    }[]
    links: {
      ap_id: string
      ap_name?: string
      mac: string
      radio_id?: string
      band?: string
      signal_dbm?: number
      noise_dbm?: number
      snr_db?: number
      rx_rate_mbps?: number
      tx_rate_mbps?: number
      connected_seconds?: number
    }[]
  } | null
  wireless_at: string | null
  wireless_error?: string
  categories: QualityCategory[]
  overall: {
    state: 'unavailable' | 'partial' | 'ready' | 'stale' | 'measuring'
    score: number | null
    earned_points: number
    available_points: number
    coverage_percent: number
  }
}
