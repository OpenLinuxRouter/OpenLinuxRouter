import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'

import { api, ApiError, configPatch } from '@/lib/api'
import { t } from '@/lib/i18n'

import type { FDBQuery, FDBResult, DevicesConfig, Device, Snapshot } from './types'

export const deviceKeys = {
  config: ['network', 'config'] as const,
  snapshot: (id: string) => ['network', 'snapshot', id] as const,
}

/**
 * The inventory is configuration, not an observation: reading it never touches
 * the equipment. Both this page and the Wireless page read the same keys, so a
 * save in one place is what the other sees.
 */
export function useDevicesConfig() {
  return useQuery({
    queryKey: deviceKeys.config,
    queryFn: () => api.get<DevicesConfig>('/api/devices/config'),
    staleTime: 30_000,
  })
}

/**
 * One device's observations. The daemon reads them on demand and never keeps
 * them as current truth, so a page treats each answer as a dated snapshot — one
 * collection may have failed while the rest are fine.
 */
export function useNetworkSnapshot(id?: string) {
  return useQuery({
    queryKey: deviceKeys.snapshot(id ?? ''),
    queryFn: () => api.get<Snapshot>(`/api/devices/${encodeURIComponent(id ?? '')}/snapshot`),
    enabled: Boolean(id),
    staleTime: 10_000,
  })
}

/**
 * Where one MAC is reachable from. The search is a decision an operator makes
 * about a specific address, not a table the page fetches on arrival, and its
 * answer is a dated read like every other observation here.
 */
export function useFDBLookup(id: string) {
  return useMutation({
    mutationFn: ({ mac, vlan }: FDBQuery) => {
      const query = new URLSearchParams({ mac })
      if (vlan != null) query.set('vlan', String(vlan))
      return api.get<FDBResult>(`/api/devices/${encodeURIComponent(id)}/fdb?${query}`)
    },
  })
}

/**
 * Writing the inventory: one entry at a time.
 *
 * The request carries the revision of the configuration the form was built
 * from, so a second tab that saved first is reported as a conflict instead of
 * being overwritten.
 */
export function useDeviceEditor() {
  const client = useQueryClient()
  const config = useDevicesConfig()
  const invalidate = () => client.invalidateQueries({ queryKey: ['network'] })

  const save = useMutation({
    mutationFn: (device: Device) =>
      api.put<DevicesConfig>(`/api/devices/${encodeURIComponent(device.id)}`, configPatch(config.data ?? {}, device)),
    onSettled: invalidate,
  })
  const remove = useMutation({
    mutationFn: (id: string) => api.delete<DevicesConfig>(`/api/devices/${encodeURIComponent(id)}`),
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
    save: (device: Device) =>
      report(save.mutateAsync, device, t('{name} saved', { name: device.name || device.id })),
    remove: (id: string) => report(remove.mutateAsync, id, t('{id} forgotten', { id })),
  }
}
