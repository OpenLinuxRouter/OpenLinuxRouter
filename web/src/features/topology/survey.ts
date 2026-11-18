import { useQuery } from '@tanstack/react-query'

import { api } from '@/lib/api'
import type { FDBEntry, Snapshot } from '@/features/devices/types'

/**
 * The router's last survey of the equipment — internal/devices Survey.
 *
 * The router keeps it and refreshes it in the background, so the page draws
 * the stored answer at once. While a survey runs the page asks again every
 * few seconds and watches it fill in; otherwise every half minute, and a
 * request that finds the answer stale starts the next survey itself. Nothing
 * here asks the operator to refresh.
 */
export interface Survey {
  running: boolean
  started_at?: string
  finished_at?: string
  devices: Record<string, SurveyDevice>
}

export interface SurveyDevice {
  snapshot?: Snapshot
  read_at?: string
  read_error?: string
  fdb?: FDBEntry[]
  fdb_at?: string
  fdb_asked?: number
  fdb_total?: number
}

export const surveyKey = ['devices', 'survey'] as const

export function useSurvey() {
  return useQuery({
    queryKey: surveyKey,
    queryFn: () => api.get<Survey>('/api/devices/survey'),
    refetchInterval: query => (query.state.data?.running === true ? 2_000 : 30_000),
    // The answer is the router's cache, not the hardware: drawing it again
    // on return to the page costs nothing, and the next poll refreshes it.
    staleTime: 0,
    gcTime: Infinity,
  })
}
