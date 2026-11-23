import { useQuery } from '@tanstack/react-query'

import { api } from '@/lib/api'

/**
 * One direction's measured capacity baseline.
 *
 * `p99_bps` is bits per second, which is how carriers quote capacity and how
 * the chart's byte rates have to be converted before they are compared. `peak`
 * is published for evidence, not for utilisation: a p99 is the repeatable
 * ceiling, and dividing by a single peak would understate every later reading.
 */
export interface CapacitySide {
  p99_bps: number
  peak_bps: number
  points: number
  since: string
}

/**
 * `GET /api/gateway/capacity`, which is being added alongside this page.
 *
 * Absent on a daemon that predates it. The page treats a failed read as "no
 * baseline" rather than as an error: there is nothing an operator can fix here,
 * and a guessed capacity would be worse than a dash.
 */
export interface GatewayCapacity {
  uplink: string
  rx: CapacitySide
  tx: CapacitySide
  state: 'measured' | 'warming'
  as_of: string
}

export const capacityKeys = {
  current: ['gateway', 'capacity'] as const,
}

export function useGatewayCapacity() {
  return useQuery({
    queryKey: capacityKeys.current,
    queryFn: () => api.get<GatewayCapacity>('/api/gateway/capacity'),
    // A 404 is the expected answer on a daemon without the endpoint, so it is
    // not retried. The interval still asks again: the endpoint can arrive with
    // a daemon upgrade while this page is open.
    retry: false,
    staleTime: 30_000,
    refetchInterval: 60_000,
  })
}
