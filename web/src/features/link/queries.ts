import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'

import { ApiError, api } from '@/lib/api'
import type { InterfaceList, LinkApplyResult, LinkStep } from '@/lib/api-types'
import type { LinkConfig } from '@/lib/config-types'

// Polling, for the reason features/dhcp/queries.ts gives about /api/events.
const OBSERVED_REFETCH_MS = 5000

export const linkKeys = {
  config: ['link', 'config'] as const,
  interfaces: ['link', 'interfaces'] as const,
}

export function useLinkConfig() {
  return useQuery({
    queryKey: linkKeys.config,
    queryFn: () => api.get<LinkConfig>('/api/link/config'),
    structuralSharing: false,
  })
}

/**
 * The machine's interfaces, joined to what has been adopted.
 *
 * Refetched on an interval because the observed half is genuinely live: a cable
 * going in, an address arriving from an upstream DHCP server, a USB adapter
 * being plugged in. An operator standing at the box plugging things in is a
 * real way this screen gets used, and a stale list there is worse than useless.
 */
export function useInterfaces(refetchInterval = OBSERVED_REFETCH_MS) {
  return useQuery({
    queryKey: linkKeys.interfaces,
    queryFn: () => api.get<InterfaceList>('/api/link/interfaces'),
    refetchInterval,
  })
}

/**
 * Adopts or releases interfaces.
 *
 * No plan preview beforehand, unlike every dhcp write. This used to be
 * justified as "this module's plan is always `impact: none`", which stopped
 * being true the day `link` grew networks: the same endpoint writes addresses
 * to the kernel now, and its plan reports `disruptive` when it does.
 *
 * The narrower claim still holds and is the one to state: an *adoption-only*
 * change touches nothing on the box, so its plan is empty and the round trip
 * could only ever return "go ahead". That is a claim about the body rather than
 * about the module — it holds only while the caller sends the stored networks
 * back unchanged, which is exactly what features/link/interfaces-card failed to
 * do. Anything that changes a network belongs on useNetworkEditor's path, which
 * plans first and stops when the plan is disruptive.
 *
 * The one consequence worth warning about — a pool that will stop validating —
 * is not in this module's plan at all and is joined client-side by the card.
 */
export function useApplyLinkConfig() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (config: LinkConfig) => api.put<LinkApplyResult>('/api/link/config', config),
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ['link'] })
      // dhcp too: adopting is what makes its pool form stop rejecting an
      // interface, and releasing is what makes an existing pool invalid. The
      // screen shows both, so writing one without re-reading the other leaves
      // a page contradicting itself.
      queryClient.invalidateQueries({ queryKey: ['dhcp'] })
    },
  })
}

/**
 * Takes one address off an adopted interface that no network owns.
 *
 * For what nothing else here reaches: an address no network or uplink accounts
 * for, most often one a removed network left behind. The server refuses a
 * network's address, the uplink's own, and the one this browser is connected
 * to, so the button can be offered wherever such an address shows up and the
 * refusal explains itself.
 *
 * Reports its own outcome, because it is one click with nothing to confirm
 * and a silent failure would read as the click not registering.
 */
export function useRemoveAddress() {
  const queryClient = useQueryClient()

  const remove = useMutation({
    mutationFn: ({ iface, address }: { iface: string; address: string }) =>
      api.delete<{ steps: LinkStep[] }>(`/api/link/interfaces/${iface}/addresses/${address}`),
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ['link'] })
      // dial too: the uplink card lists every address on its interface.
      queryClient.invalidateQueries({ queryKey: ['dial'] })
    },
  })

  return {
    busy: remove.isPending,
    remove: async (iface: string, address: string) => {
      try {
        await remove.mutateAsync({ iface, address })
        toast.success(`Removed ${address} from ${iface}`)
      } catch (error) {
        if (error instanceof ApiError) {
          toast.error(error.message, {
            description: error.problems.map((p) => p.message).join('\n') || undefined,
          })
        } else {
          toast.error(String(error))
        }
      }
    },
  }
}
