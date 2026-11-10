import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'

import { api, ApiError, configPatch } from '@/lib/api'
import { t } from '@/lib/i18n'

import type { NetworkConfig, NetworkDevice, ScanResult, Snapshot } from './types'

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

/**
 * Writing the equipment inventory: one access point at a time.
 *
 * The request carries the revision of the configuration the form was built
 * from, so a second tab that saved first is reported as a conflict instead of
 * being overwritten.
 */
export function useAccessPointEditor() {
  const client = useQueryClient()
  const config = useNetworkConfig()
  const invalidate = () => client.invalidateQueries({ queryKey: ['network'] })

  const save = useMutation({
    mutationFn: (device: NetworkDevice) =>
      api.put<NetworkConfig>(`/api/network/devices/${encodeURIComponent(device.id)}`, configPatch(config.data ?? {}, device)),
    onSettled: invalidate,
  })
  const remove = useMutation({
    mutationFn: (id: string) => api.delete<NetworkConfig>(`/api/network/devices/${encodeURIComponent(id)}`),
    onSettled: invalidate,
  })

  async function report<T>(run: (value: T) => Promise<unknown>, value: T, success: string) {
    try {
      await run(value)
      toast.success(success)
      return true
    } catch (error) {
      if (error instanceof ApiError) {
        toast.error(error.message, { description: error.problems.map(problem => problem.message).join('\n') || undefined })
      } else {
        toast.error(String(error))
      }
      return false
    }
  }

  return {
    busy: save.isPending || remove.isPending,
    save: (device: NetworkDevice) =>
      report(save.mutateAsync, device, t('{name} saved', { name: device.name || device.id })),
    remove: (id: string) => report(remove.mutateAsync, id, t('{id} forgotten', { id })),
  }
}
