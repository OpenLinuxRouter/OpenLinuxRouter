import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { api } from '@/lib/api'

export type Priority = 'high' | 'normal' | 'low'
export interface QosClient { mac: string; priority?: Priority; download_mbps?: number; upload_mbps?: number }
export interface QosConfig { enabled?: boolean; download_mbps?: number; upload_mbps?: number; clients?: QosClient[] }
export interface QosStatus { enabled: boolean; active: boolean; reason?: string; pending?: string[] }
interface Result { config: QosConfig; status: QosStatus }

export function useQosConfig() {
  return useQuery({ queryKey: ['qos', 'config'], queryFn: () => api.get<QosConfig>('/api/qos/config') })
}
export function useQosClientApply(mac: string) {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (device: QosClient) => api.put<Result>(`/api/qos/clients/${encodeURIComponent(mac)}`, device),
    onSettled: () => client.invalidateQueries({ queryKey: ['qos'] }),
  })
}

/** One device at a time, with the MAC supplied per call for group actions. */
export function useQosClientsApply() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (device: QosClient) => api.put<Result>(`/api/qos/clients/${encodeURIComponent(device.mac)}`, device),
    onSettled: () => client.invalidateQueries({ queryKey: ['qos'] }),
  })
}
