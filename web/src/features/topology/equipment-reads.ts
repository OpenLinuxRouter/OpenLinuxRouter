import { t } from '@/lib/i18n'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'

import { api } from '@/lib/api'
import { useClientList } from '@/features/clients/queries'
import { deviceKeys, useDevicesConfig, useReadDevice } from '@/features/devices/queries'
import type { Device, FDBResult } from '@/features/devices/types'
import { useInterfaces } from '@/features/link/queries'

import { farEnd, planReads, type FarEndSources } from './equipment'
import { hostOf } from './placement'
import { runSweep } from './sweep'

/**
 * What the topology page asks the hardware, on arrival and without a button.
 *
 * First every box this session has no answer from is read, one at a time; then
 * every switch that answered is asked which port each address is on. The map
 * draws from the query cache and the sweep store, so it fills in as answers
 * land. A box already read this session is not read again, and a switch already
 * swept is only asked again through `rescan`.
 */
export interface EquipmentReads {
  /** The box being read now, if any. */
  reading?: string
  /** Why each box that did not answer did not. */
  failed: Readonly<Record<string, string>>
  /** The read pass is over. */
  readsDone: boolean
  /** Ask every switch again. */
  rescan: () => void
}

export function useEquipmentReads(): EquipmentReads {
  const config = useDevicesConfig()
  const interfaces = useInterfaces()
  const clients = useClientList()
  const client = useQueryClient()
  const { mutateAsync: readDevice } = useReadDevice()
  const started = useRef(false)
  const [reading, setReading] = useState<string | undefined>(undefined)
  const [failed, setFailed] = useState<Record<string, string>>({})
  const [readsDone, setReadsDone] = useState(false)
  const devices = useMemo(() => config.data?.devices ?? [], [config.data])

  // One pass over the inventory, on arrival only. `started` is a ref rather
  // than state so a second render cannot start a second pass.
  useEffect(() => {
    if (started.current || config.data === undefined) return
    started.current = true
    let cancelled = false
    void (async () => {
      for (const device of planReads(config.data?.devices ?? [], id => client.getQueryData(deviceKeys.snapshot(id)) !== undefined)) {
        setReading(device.id)
        try {
          await readDevice(device.id)
        } catch (error) {
          if (!cancelled) {
            setFailed(current => ({ ...current, [device.id]: error instanceof Error ? error.message : t('no answer') }))
          }
        }
        if (cancelled) return
      }
      setReading(undefined)
      setReadsDone(true)
    })()
    return () => { cancelled = true }
    // The ref, not the dependencies, keeps this to one pass: the mutation
    // object is new on every render, and the inventory refetches.
  }, [client, config.data, readDevice])

  const sources: FarEndSources = useMemo(() => {
    const self = new Map<string, string>()
    // A bridge carries the MAC of one of its ports; the port's name is the one
    // the map can draw a cable from, so physical interfaces claim it first.
    const rows = [...(interfaces.data?.interfaces ?? [])].sort((a, b) => Number(b.physical) - Number(a.physical))
    for (const row of rows) {
      const mac = row.mac?.toLowerCase()
      if (mac !== undefined && mac !== '' && mac !== '00:00:00:00:00:00' && !self.has(mac)) self.set(mac, row.name)
    }
    return {
      self,
      clients: new Map((clients.data?.clients ?? []).map(row => [row.mac.toLowerCase(), row])),
      devices,
    }
  }, [clients.data, devices, interfaces.data])

  // Every address worth asking about: this router's own interfaces first (the
  // cable back here), then the boxes we manage, then whoever is online now. An
  // offline client has aged out of any forwarding table.
  const macs = useMemo(() => {
    const hosts = new Set(devices.map(device => hostOf(device.address)).filter(host => host !== undefined))
    const all = clients.data?.clients ?? []
    return [...new Set([
      ...sources.self.keys(),
      ...all.filter(row => (row.ips ?? []).some(ip => hosts.has(ip))).map(row => row.mac.toLowerCase()),
      ...all.filter(row => row.online).map(row => row.mac.toLowerCase()),
    ].filter(mac => mac !== ''))]
  }, [clients.data, devices, sources])

  const sweep = useCallback((device: Device, force: boolean) => runSweep({
    deviceId: device.id,
    macs,
    force,
    resolve: mac => farEnd(mac, sources),
    lookup: async mac => {
      const result = await api.get<FDBResult>(`/api/devices/${encodeURIComponent(device.id)}/fdb?${new URLSearchParams({ mac })}`)
      return result.entries.map(entry => ({ mac: entry.mac, port_id: entry.port_id }))
    },
  }), [macs, sources])

  const sweepAll = useCallback((force: boolean) => {
    for (const device of devices) {
      if (!isSweepable(device) || failed[device.id] !== undefined) continue
      if ((client.getQueryData<{ ports?: unknown[] }>(deviceKeys.snapshot(device.id))?.ports?.length ?? 0) === 0) continue
      void sweep(device, force)
    }
  }, [devices, failed, client, sweep])

  // Once every box has been read, ask each switch that answered. Switches run
  // side by side; each one is asked one address at a time, which is all its
  // firmware takes.
  const swept = useRef(false)
  useEffect(() => {
    if (swept.current || !readsDone || clients.data === undefined || interfaces.data === undefined) return
    swept.current = true
    sweepAll(false)
  }, [readsDone, clients.data, interfaces.data, sweepAll])

  const rescan = useCallback(() => sweepAll(true), [sweepAll])
  return { reading, failed, readsDone, rescan }
}

/** A switch is anything we might ask: an explicit switch role, or the adapter we know implements the lookup. */
export function isSweepable(device: Device): boolean {
  return device.roles?.includes('switch') === true || device.adapter === 'mercury'
}
