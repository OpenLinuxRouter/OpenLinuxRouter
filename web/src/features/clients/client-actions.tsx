import { t, useLocale } from '@/lib/i18n'
import { useState } from 'react'
import { toast } from 'sonner'

import { ClientDetail } from '@/features/clients/client-detail'
import { useSaveDevice, useClientsConfig } from '@/features/clients/queries'
import { useDhcpConfig } from '@/features/dhcp/queries'
import { ReservationDialog } from '@/features/dhcp/reservation-dialog'
import { useDhcpApply } from '@/features/dhcp/use-apply'
import { ApplyOutcome } from '@/features/gateway/editor'
import { gatewayChange, useGatewayConfig } from '@/features/gateway/queries'
import { useGatewayApply } from '@/features/gateway/use-apply'
import { ApiError } from '@/lib/api'
import type { ClientRow } from '@/lib/api-types'
import type { Client, Reservation } from '@/lib/config-types'

/**
 * Acting on one device, wherever it was clicked.
 *
 * Split out from the list it used to live in so the map can open the same two
 * dialogs. One owner of the write paths, rather than a second copy that could
 * drift — and the two paths stay deliberately different:
 *
 *   Naming and categorising go to `devices`, which has no backend. Impact is
 *   always none, so they apply instantly with no confirmation (§5.1).
 *
 *   Setting a fixed address goes to `dhcp`, which does. It runs through dhcp's
 *   plan and its impact gate, so a change that would disconnect somebody still
 *   asks first (§5.3.3) even though it was started from a device.
 */
export function useDeviceActions() {
  useLocale()
  const identity = useClientsConfig()
  const saveIdentity = useSaveDevice()
  const dhcpConfig = useDhcpConfig()
  const dhcpApplier = useDhcpApply()
  const gateway = useGatewayConfig()
  const gatewayApplier = useGatewayApply()

  const [editing, setEditing] = useState<ClientRow | null>(null)
  const [reserving, setReserving] = useState<ClientRow | null>(null)

  /** Stores only this device, using the server's current inventory. */
  async function save(device: Client) {
    // An entry with nothing but a MAC says nothing, so saving one is a request
    // to forget rather than to store an empty record.
    //
    // A group counts as something said. It is stored on the device, so an
    // entry holding a MAC and a group is exactly how a device that was only
    // ever seen joins one — and treating it as empty would forget the device
    // the moment it was put in a group.
    const empty =
      !device.name &&
      !device.category &&
      !device.icon &&
      !device.notes &&
      !device.model &&
      !device.group
    try {
      await saveIdentity.mutateAsync({ device, forget: empty })
      toast.success(empty ? 'Client forgotten' : t('Saved'))
    } catch (error) {
      if (error instanceof ApiError) {
        toast.error(error.message, {
          description:
            error.problems.map((p) => `${p.path ?? ''} ${p.message}`.trim()).join('\n') || undefined,
        })
      } else {
        toast.error(String(error))
      }
    }
  }

  function saveReservation(reservation: Reservation) {
    const current = dhcpConfig.data
    if (!current) return
    const rest = (current.reservations ?? []).filter(
      (r) => r.mac.toLowerCase() !== reservation.mac.toLowerCase(),
    )
    dhcpApplier.submit({ ...current, reservations: [...rest, reservation] })
  }

  function removeReservation(mac: string) {
    const current = dhcpConfig.data
    if (!current) return
    dhcpApplier.submit({
      ...current,
      reservations: (current.reservations ?? []).filter(
        (r) => r.mac.toLowerCase() !== mac.toLowerCase(),
      ),
    })
  }

  const dialogs = (
    <>
      <ApplyOutcome applier={gatewayApplier} />
      {editing && (
        <ClientDetail
          // Keyed by MAC so opening a different device rebuilds the form rather
          // than showing the previous one's draft in it.
          key={editing.mac}
          device={editing}
          open
          onOpenChange={(open) => !open && setEditing(null)}
          busy={saveIdentity.isPending}
          groups={identity.data?.groups}
          gateway={gateway.data}
          gatewayBusy={gatewayApplier.busy}
          onExitChange={(exit) =>
            gatewayApplier.submit(
              exit === null
                ? gatewayChange.removeClient(editing.mac)
                : gatewayChange.client(editing.mac, exit),
            )
          }
          onSave={save}
          onForget={editing.stored ? () => save({ mac: editing.mac }) : undefined}
          onEditFixedAddress={() => {
            setReserving(editing)
            setEditing(null)
          }}
        />
      )}

      {reserving && (
        <ReservationDialog
          key={reserving.mac}
          open
          onOpenChange={(open) => !open && setReserving(null)}
          initial={
            reserving.fixed_ip
              ? { mac: reserving.mac, ip: reserving.fixed_ip }
              : // Pre-filled with the device's MAC: this is the whole point of
                // §11.1 — the operator never goes and finds a hardware address.
                { mac: reserving.mac, ip: '' }
          }
          onSubmit={saveReservation}
          onRemove={reserving.fixed_ip ? () => removeReservation(reserving.mac) : undefined}
        />
      )}
    </>
  )

  return { select: setEditing, dialogs }
}
