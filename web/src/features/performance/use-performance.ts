import { useDialStatus, useUplink } from '@/features/dial/queries'
import { useGatewayLatency } from '@/features/gateway/queries'
import { useInterfaceTraffic, type RateSample } from '@/features/link/interface-traffic'
import { useInterfaces } from '@/features/link/queries'

import { SAMPLE_MS, useCpuSamples, useNow, timeWindow, WINDOW_MS, type CpuSample } from './live'
import { useGatewayCapacity, type GatewayCapacity } from './queries'
import {
  coveredWindow,
  coverageCopy,
  latencyPoints,
  sampleCount,
  usableCapacity,
  usableCapacityData,
  utilisation,
  type LatencyPoint,
} from './samples'
import { verdict, type CapacityState, type Verdict } from './verdict'

export type CounterState = 'unknown' | 'absent' | 'present'
export type LinkState = 'unknown' | 'missing' | 'down' | 'up'

export interface PerformanceEvidence {
  start: number
  end: number
  ticks: number[]
  /** The interface the uplink leaves by, from capacity or the observed route. */
  uplinkName?: string
  capacity?: GatewayCapacity
  capacityPending: boolean
  downloadCapacityBps?: number
  uploadCapacityBps?: number
  baselineSamples?: number
  /** When the daemon published this baseline. */
  baselineAsOf?: string
  coverage?: { key: string; count: number }
  history: RateSample[]
  downloadRate?: number
  uploadRate?: number
  downloadUtil?: number
  uploadUtil?: number
  /**
   * Whether each RFC 2863 counter is readable at all. Three states, because
   * "the driver does not publish this" and "the kernel answered zero" are
   * different readings and only one of them is a clean interface.
   */
  discardCounter: CounterState
  errorCounter: CounterState
  missedCounter: CounterState
  byteCounters: CounterState
  /** The uplink name is in the interface list the daemon just returned. */
  interfaceKnown: boolean
  linkState: LinkState
  interfacesPending: boolean
  /** Input discards per second (ifInDiscards), receive direction only. */
  discardsPerSecond?: number
  /** Input errors per second (ifInErrors). */
  errorsPerSecond?: number
  /** Input missed per second: the device ran out of buffer space. */
  missedPerSecond?: number
  /** Discards as a fraction of received frames, the denominator 1.0/s lacks. */
  discardShare?: number
  cpuSamples: CpuSample[]
  cpuPercent?: number
  cpuCores?: number
  cpuError: boolean
  cpuPending: boolean
  latencyPoints: LatencyPoint[]
  latencyAvailable: boolean
  latencyError: boolean
  latencyPending: boolean
  interfacesError: boolean
  verdict: Verdict
}

/**
 * Everything both lanes draw, read once.
 *
 * The two plots must agree on the window and the tick positions, so the clock
 * and the derived utilisation live here rather than in each card. Every value
 * that can be missing stays `undefined`; nothing is defaulted to zero on the
 * way through.
 */
export function usePerformance(): PerformanceEvidence {
  const now = useNow()
  const { start, end, ticks } = timeWindow(now)

  const capacityQuery = useGatewayCapacity()
  const uplink = useUplink()
  // When OLR does not own the way out, the observed default route still names
  // the interface whose counters this page has to read.
  const dial = useDialStatus()
  // The interface endpoint is the uplink counter source. A one-second poll is
  // this page's need, not the rest of the app's; the hook takes the interval so
  // the Gateway screens keep their five-second cadence.
  const interfaces = useInterfaces(SAMPLE_MS)
  const traffic = useInterfaceTraffic(interfaces.data, interfaces.isError, WINDOW_MS)
  const cpu = useCpuSamples()
  const latency = useGatewayLatency()

  // A failed read means no baseline, even if an earlier response is still in
  // the cache. The page must not keep drawing a line the daemon just refused.
  const capacity = capacityQuery.isError ? undefined : usableCapacityData(capacityQuery.data)
  const uplinkName = capacity?.uplink
    ?? uplink.data?.uplink?.interface
    ?? dial.data?.route?.dev
  const reading = uplinkName ? traffic.get(uplinkName) : undefined
  const row = reading?.row ?? interfaces.data?.interfaces.find(item => item.name === uplinkName)
  const history = reading?.history ?? []
  const current = history.at(-1)

  const downloadCapacityBps = usableCapacity(capacity?.rx)
  const uploadCapacityBps = usableCapacity(capacity?.tx)
  const downloadRate = current?.rx
  const uploadRate = current?.tx
  const downloadUtil = utilisation(downloadRate, downloadCapacityBps)
  const uploadUtil = utilisation(uploadRate, uploadCapacityBps)
  const discardsPerSecond = current?.discards
  const errorsPerSecond = current?.errors
  const missedPerSecond = current?.missed
  const discardShare = current?.discards != null && current.packets
    ? current.discards / current.packets
    : undefined

  const capacityState: CapacityState = capacity?.state
    ?? (capacityQuery.isPending ? 'loading' : 'missing')

  const linkState: LinkState = !row
    ? 'unknown'
    : !row.present ? 'missing'
      : !row.up || !row.running ? 'down'
        : 'up'
  const counterState = (value: number | undefined): CounterState => !row
    ? 'unknown'
    : value === undefined ? 'absent' : 'present'
  const discardCounter = counterState(row?.rx_dropped)
  const errorCounter = counterState(row?.rx_errors)
  const missedCounter = counterState(row?.rx_missed_errors)
  const byteCounters: CounterState = !row
    ? 'unknown'
    : row.rx_bytes === undefined || row.tx_bytes === undefined ? 'absent' : 'present'

  return {
    start,
    end,
    ticks,
    uplinkName,
    capacity,
    capacityPending: capacityQuery.isPending,
    downloadCapacityBps,
    uploadCapacityBps,
    baselineSamples: sampleCount(capacity),
    baselineAsOf: capacity?.as_of,
    coverage: capacity ? coverageCopy(coveredWindow(capacity)) : undefined,
    history,
    downloadRate,
    uploadRate,
    downloadUtil,
    uploadUtil,
    discardCounter,
    errorCounter,
    missedCounter,
    byteCounters,
    interfaceKnown: Boolean(row),
    linkState,
    interfacesPending: interfaces.isPending,
    discardsPerSecond,
    errorsPerSecond,
    missedPerSecond,
    discardShare,
    cpuSamples: cpu.samples,
    cpuPercent: cpu.current,
    cpuCores: cpu.cores,
    cpuError: cpu.isError,
    cpuPending: cpu.isPending,
    latencyPoints: latency.isError ? [] : latencyPoints(latency.data, start, end),
    latencyAvailable: !latency.isError && Boolean(latency.data),
    latencyError: latency.isError,
    latencyPending: latency.isPending,
    interfacesError: interfaces.isError,
    verdict: verdict({
      capacity: capacityState,
      downloadUtil,
      uploadUtil,
      cpuPercent: cpu.current,
      discardsPerSecond,
      throughput: downloadRate != null || uploadRate != null,
    }),
  }
}
