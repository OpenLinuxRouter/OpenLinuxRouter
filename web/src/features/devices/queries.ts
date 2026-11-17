import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useCallback, useState, useSyncExternalStore } from 'react'
import { toast } from 'sonner'

import { linkKeys } from '@/features/link/queries'
import { api, ApiError, configPatch } from '@/lib/api'
import type { InterfaceList } from '@/lib/api-types'
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

/** One snapshot read, named once so the cache key and the request cannot drift. */
function snapshotQuery(id: string) {
  return {
    queryKey: deviceKeys.snapshot(id),
    queryFn: () => api.get<Snapshot>(`/api/devices/${encodeURIComponent(id)}/snapshot`),
    // The inventory's status is session-scoped, so a read has to survive as
    // long as the tab. React Query's five-minute GC would quietly turn a device
    // this session read back into "Not checked", which is a different claim.
    gcTime: Infinity,
  }
}

/**
 * One device's observations. The daemon reads them on demand and never keeps
 * them as current truth, so a page treats each answer as a dated snapshot — one
 * collection may have failed while the rest are fine.
 */
export function useNetworkSnapshot(id?: string) {
  return useQuery({
    ...snapshotQuery(id ?? ''),
    enabled: Boolean(id),
    staleTime: 10_000,
  })
}

/**
 * What this session has already learned about one device, read from React
 * Query's cache and never fetched.
 *
 * A cache subscription rather than a query observer, on purpose: an observer
 * would make the snapshot key *active* on the inventory page, and saving a
 * device invalidates the whole `network` family — which would then refetch
 * every device in the list. Reading the cache must not create that presence.
 * The only things that contact a device are opening it and "Read all".
 */
export function useDeviceRead(id: string) {
  const client = useQueryClient()
  const subscribe = useCallback(
    (onChange: () => void) => client.getQueryCache().subscribe(onChange),
    [client],
  )
  const read = useCallback(
    () => client.getQueryState<Snapshot>(deviceKeys.snapshot(id)),
    [client, id],
  )
  return useSyncExternalStore(subscribe, read, read)
}

/**
 * This machine's own addresses, for the one row that is this router.
 *
 * The inventory stores an address and a set of roles; the box serving this
 * console is the entry whose address is one of this machine's own. One answer
 * is enough, so this observer shares the cache entry with Gateway's interface
 * card but does not inherit that card's five-second poll.
 */
export function useLocalInterfaces() {
  return useQuery({
    queryKey: linkKeys.interfaces,
    queryFn: () => api.get<InterfaceList>('/api/link/interfaces'),
    staleTime: 5 * 60_000,
    refetchInterval: false,
    refetchOnWindowFocus: false,
  })
}

export interface ReadProgress {
  done: number
  total: number
}

/**
 * Reads every device in the list, one after another.
 *
 * Sequential because the daemon serializes reads anyway: six requests fired at
 * once would queue behind each other while the progress counter lied about
 * which one was being read. A device that fails does not stop the rest — its
 * row says Not answering, which is the answer the read produced.
 */
export function useReadDevices() {
  const client = useQueryClient()
  const [progress, setProgress] = useState<ReadProgress | null>(null)

  const run = async (devices: Device[]) => {
    if (progress) return 0
    let failed = 0
    setProgress({ done: 0, total: devices.length })
    for (const [index, device] of devices.entries()) {
      try {
        await client.fetchQuery({ ...snapshotQuery(device.id), staleTime: 0, retry: false })
      } catch {
        failed += 1
      }
      setProgress({ done: index + 1, total: devices.length })
    }
    setProgress(null)
    return failed
  }

  return { progress, run }
}

/**
 * One device, read now, into the cache entry the device page uses.
 *
 * `fetchQuery` rather than a query observer, for the reason `useDeviceRead`
 * gives: a page that draws a box from the cache must not make the snapshot key
 * active, or saving a device — which invalidates the whole `network` family —
 * would refetch every box the page happens to mention.
 */
export function useReadDevice() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => client.fetchQuery({ ...snapshotQuery(id), staleTime: 0, retry: false }),
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
