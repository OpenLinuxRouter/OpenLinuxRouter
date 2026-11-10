import { useQuery } from '@tanstack/react-query'

import { api } from '@/lib/api'

import type { NetworkConfig, Snapshot } from './types'

export const wirelessKeys = {
  config: ['network', 'config'] as const,
  snapshot: (id: string) => ['network', 'snapshot', id] as const,
}

/** The equipment inventory is configuration, not an observation. */
export function useNetworkConfig() {
  return useQuery({
    queryKey: wirelessKeys.config,
    queryFn: () => api.get<NetworkConfig>('/api/network/config'),
    staleTime: 30_000,
  })
}

/**
 * One device's observations. The daemon reads them on demand and never keeps
 * them as current truth, so the page treats each answer as a dated snapshot.
 */
export function useNetworkSnapshot(id?: string) {
  return useQuery({
    queryKey: wirelessKeys.snapshot(id ?? ''),
    queryFn: () => api.get<Snapshot>(`/api/network/devices/${encodeURIComponent(id ?? '')}/snapshot`),
    enabled: Boolean(id),
    staleTime: 10_000,
  })
}
