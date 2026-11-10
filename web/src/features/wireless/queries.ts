import { useMutation, useQuery } from '@tanstack/react-query'

import { api } from '@/lib/api'

import type { NetworkConfig, ScanResult, Snapshot } from './types'

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

/**
 * A scan is a decision, not a read: it takes the radio off its channel for a
 * few seconds. Nothing runs it on a timer, and the answer is never cached as
 * if it were the current neighbourhood.
 */
export function useRadioScan(id?: string) {
  return useMutation({
    mutationFn: (radioID: string) => api.post<ScanResult>(`/api/network/devices/${encodeURIComponent(id ?? '')}/scan`, { radio_id: radioID }),
  })
}
