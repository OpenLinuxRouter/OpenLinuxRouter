import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { api, ApiError } from '@/lib/api'
import type { ClientList, ClientsApplyResult, Plan } from '@/lib/api-types'
import type { Client, ClientsConfig } from '@/lib/config-types'

// Same polling story as features/dhcp/queries.ts: the UI asks again rather than
// subscribing, because EventSource cannot carry an Authorization header. The
// device list is observed state that the daemon never caches (design.md §4.5),
// so asking again is the only way to stay current.
const OBSERVED_REFETCH_MS = 5000

export const deviceKeys = {
  config: ['devices', 'config'] as const,
  list: ['devices', 'list'] as const,
}

/** Stored identity only — what a human has said. */
export function useClientsConfig() {
  return useQuery({
    queryKey: deviceKeys.config,
    queryFn: () => api.get<ClientsConfig>('/api/clients/config'),
    structuralSharing: false,
  })
}

/** The join: identity, presence and fixed addresses in one answer. */
export function useClientList() {
  return useQuery({
    queryKey: deviceKeys.list,
    queryFn: () => api.get<ClientList>('/api/clients/list'),
    refetchInterval: OBSERVED_REFETCH_MS,
  })
}

export function useDevicesPlanPreview() {
  return useMutation({
    mutationFn: (config: ClientsConfig) => api.post<Plan>('/api/clients/plan', config),
  })
}

/**
 * Stores identity.
 *
 * No plan preview beforehand, unlike dhcp. This module has no backend, so its
 * impact is always `none` and there is nothing a rename could disconnect —
 * §5.1's instant apply with no confirmation is exactly right here. The one
 * action on this screen that *can* drop a client is setting a fixed address,
 * and that goes through dhcp's own apply and its impact gate.
 */
export function useApplyClientsConfig() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (config: ClientsConfig) =>
      api.put<ClientsApplyResult>('/api/clients/config', config),
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ['clients'] })
      queryClient.invalidateQueries({ queryKey: ['ingress'] })
    },
  })
}

/*
 * Groups, one item at a time.
 *
 * These go to the per-item routes rather than through useApplyClientsConfig,
 * for the reason internal/clients/http.go gives: a whole-document PUT is two
 * requests with the lock covering only the second, and a rename has to carry
 * every member along — the server does that under its lock, and a client
 * splicing the document itself would be a second copy of the cascade.
 *
 * Every one invalidates all of ['clients'], not just the config: the list rows
 * carry `group` too, and the map is drawn from the list.
 */

const groupPath = (name: string) => `/api/clients/groups/${encodeURIComponent(name)}`

function useDevicesMutation<T>(fn: (vars: T) => Promise<ClientsApplyResult>) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: fn,
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ['clients'] })
      queryClient.invalidateQueries({ queryKey: ['ingress'] })
    },
  })
}

export function useSaveDevice() {
  return useDevicesMutation(({ device, forget }: { device: Client; forget: boolean }) => {
    const path = `/api/clients/${encodeURIComponent(device.mac)}`
    return forget
      ? api.delete<ClientsApplyResult>(path)
      : api.put<ClientsApplyResult>(path, device)
  })
}

/**
 * Creates a group, inside another when a parent is given. Creating one that
 * already exists is a no-op, not an error.
 */
export function useCreateDeviceGroup() {
  return useDevicesMutation(({ name, parent }: { name: string; parent?: string }) =>
    api.put<ClientsApplyResult>(groupPath(name), parent ? { parent } : {}),
  )
}

/** Renames a group; its members follow on the server. */
export function useRenameDeviceGroup() {
  return useDevicesMutation(({ from, to }: { from: string; to: string }) =>
    api.put<ClientsApplyResult>(groupPath(from), { name: to }),
  )
}

/**
 * Moves a group inside another, or to the top with an empty parent. Its
 * members and subgroups travel with it; the server refuses a move that would
 * put a group inside itself or nest deeper than four levels.
 */
export function useMoveDeviceGroup() {
  return useDevicesMutation(({ name, parent }: { name: string; parent: string }) =>
    api.put<ClientsApplyResult>(groupPath(name), { parent }),
  )
}

/**
 * Deletes a group. What it held moves up one level — its devices and
 * subgroups go to its parent, or to the top — rather than the delete being
 * refused.
 */
export function useDeleteDeviceGroup() {
  return useDevicesMutation((name: string) => api.delete<ClientsApplyResult>(groupPath(name)))
}

/**
 * Puts one device in a group, or takes it out of the group it is in.
 *
 * Membership is addressed through the group, not through the client, because a
 * client has one group and the two routes would otherwise collide. Taking it out
 * names the group it is expected to be in, so a menu that went stale cannot
 * silently empty a membership it never showed.
 */
export function useSetDeviceGroup() {
  return useDevicesMutation(({ mac, group, from }: { mac: string; group: string; from?: string }) =>
    group
      ? api.put<ClientsApplyResult>(
          `/api/clients/groups/${encodeURIComponent(group)}/members/${encodeURIComponent(mac)}`,
          undefined,
        )
      : api.delete<ClientsApplyResult>(
          `/api/clients/groups/${encodeURIComponent(from ?? '')}/members/${encodeURIComponent(mac)}`,
        ),
  )
}

/**
 * The server's words, most specific first. A 422 carries the reason in its
 * problems and only "invalid devices configuration" in its message, which on
 * its own would tell the operator nothing about what to change.
 */
export function describeError(error: unknown): string {
  if (error instanceof ApiError) {
    return error.problems.length ? error.problems.map((p) => p.message).join('; ') : error.message
  }
  return String(error)
}
