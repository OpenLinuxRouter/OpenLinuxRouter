import { useMutation } from '@tanstack/react-query'

import { api } from '@/lib/api'
import type { ScanResult } from '@/features/devices/types'

/**
 * A scan is a decision, not a read: it takes the radio off its channel for a
 * few seconds. Nothing runs it on a timer, and the answer is never cached as
 * if it were the current neighbourhood.
 *
 * The inventory and the snapshot behind this page belong to the devices module
 * feature, because they describe the box rather than its radios.
 */
export function useRadioScan(id?: string) {
  return useMutation({
    mutationFn: (radioID: string) => api.post<ScanResult>(`/api/devices/${encodeURIComponent(id ?? '')}/scan`, { radio_id: radioID }),
  })
}
