import { useQuery } from '@tanstack/react-query'

import { api } from '@/lib/api'
import type { AccessView } from '@/lib/api'

export interface HostMetrics {
  uptime_seconds: number
  cpu_used_cores: number | null
  cpu_cores: number
  memory_used_bytes: number
  memory_total_bytes: number
  disk_used_bytes: number | null
  disk_total_bytes: number | null
}

// One read for the Advanced directory tile and the page that changes it.
export const adminAccessKey = ['system', 'access'] as const

export function useAdminAccess() {
  return useQuery({ queryKey: adminAccessKey, queryFn: () => api.get<AccessView>('/api/system/access') })
}

export function useHostMetrics(refetchInterval = 5000) {
  return useQuery({
    queryKey: ['system', 'metrics'],
    queryFn: () => api.get<HostMetrics>('/api/system/metrics'),
    refetchInterval,
    retry: false,
  })
}

export interface PublicAddresses {
  ipv4: PublicAddress
  ipv6: PublicAddress
}

export interface PublicAddress {
  ip: string
  country?: string
  country_code?: string
  latitude?: number
  longitude?: number
  region?: string
  city?: string
  isp?: string
  organization?: string
  asn?: number
  asn_organization?: string
}

/**
 * The last answer, kept for the paint that follows a reload.
 *
 * Two third-party HTTPS lookups stand behind /public-addresses, so a cold
 * answer waits for the slower of them and a refresh used to blank the ISP line
 * for that whole time. Showing the previous answer instead is not a claim that
 * the address has not changed: `initialDataUpdatedAt: 0` marks it stale on
 * arrival, so mounting still revalidates and a changed ISP replaces it as soon
 * as the daemon answers.
 */
const ADDRESSES_SNAPSHOT_KEY = 'olr.system.public-addresses-v1'

function savedAddresses(): PublicAddresses | undefined {
  try {
    const stored = sessionStorage.getItem(ADDRESSES_SNAPSHOT_KEY)
    if (!stored) return undefined
    const snapshot = JSON.parse(stored) as PublicAddresses
    return snapshot?.ipv4?.ip || snapshot?.ipv6?.ip ? snapshot : undefined
  } catch {
    return undefined
  }
}

export function usePublicAddresses() {
  return useQuery({
    queryKey: ['system', 'public-addresses'],
    initialData: savedAddresses,
    initialDataUpdatedAt: 0,
    queryFn: async () => {
      const next = await api.get<PublicAddresses>('/api/system/public-addresses')
      try {
        // Never overwrite a good answer with the empty one two failed lookups
        // produce; the next mount still has something to paint.
        if (next.ipv4?.ip || next.ipv6?.ip) sessionStorage.setItem(ADDRESSES_SNAPSHOT_KEY, JSON.stringify(next))
      } catch {
        /* Private mode can disable storage; the next refresh just waits again. */
      }
      return next
    },
    staleTime: 5 * 60 * 1000,
    gcTime: 30 * 60 * 1000,
    refetchInterval: 5 * 60 * 1000,
    retry: false,
  })
}
