import { t, useLocale } from '@/lib/i18n'
import { useState } from 'react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { GroupNameDialog } from '@/features/clients/group-name-dialog'
import {
  describeError,
  useCreateDeviceGroup,
  useDeleteDeviceGroup,
  useClientsConfig,
  useMoveDeviceGroup,
  useRenameDeviceGroup,
  useSetDeviceGroup,
} from '@/features/clients/queries'
import type { ClientRow } from '@/lib/api-types'

/** What a delete will move, so the confirmation can say where it goes. */
export interface GroupDeletion {
  name: string
  /** Clients directly in it. */
  devices: number
  /** Groups directly inside it. */
  groups: number
  /** Where both go: its parent, or empty for the top. */
  parent?: string
}

/**
 * Making, renaming and removing groups, and moving a device between them.
 *
 * Shaped like useDeviceActions: a hook that owns the dialogs and hands back
 * functions to open them, so the map can offer "Rename" from a card's menu
 * without owning any of the forms. None of this goes through a plan preview —
 * a group carries no policy today, so nothing here can disconnect anybody,
 * and §5.1's instant apply is the right shape. The one destructive action,
 * deleting a group, still asks, because it undoes work: the split it made is
 * gone, and there is no list anywhere to rebuild it from. What it held is not
 * lost — it moves up one level — and the confirmation says exactly where.
 */
export function useGroupActions() {
  useLocale()
  const config = useClientsConfig()
  const create = useCreateDeviceGroup()
  const rename = useRenameDeviceGroup()
  const reparent = useMoveDeviceGroup()
  const remove = useDeleteDeviceGroup()
  const setGroup = useSetDeviceGroup()

  const [naming, setNaming] = useState<{ from?: string; parent?: string } | null>(null)
  const [deleting, setDeleting] = useState<GroupDeletion | null>(null)

  /**
   * Moves one device, with a toast rather than a dialog: the menu item already
   * said where it was going, and asking again would be a confirmation of a
   * choice the operator made one click ago.
   */
  async function move(device: ClientRow, group: string) {
    try {
      await setGroup.mutateAsync({ mac: device.mac, group, from: device.group })
      const who = device.name || device.mac
      toast.success(group ? `${who} moved to ${group}` : `${who} is no longer in a group`)
    } catch (error) {
      toastError(error)
    }
  }

  /** Moves a group inside another, or to the top with an empty parent. */
  async function moveGroup(name: string, parent: string) {
    try {
      await reparent.mutateAsync({ name, parent })
      toast.success(parent ? `${name} is now inside ${parent}` : `${name} is now a top-level group`)
    } catch (error) {
      toastError(error)
    }
  }

  const dialogs = (
    <>
      {naming && (
        <GroupNameDialog
          // Keyed so reopening for a different group starts from its name
          // rather than the last draft.
          key={naming.from ?? ` new${naming.parent ?? ''}`}
          from={naming.from}
          parent={naming.parent}
          groups={config.data?.groups}
          busy={create.isPending || rename.isPending}
          onOpenChange={(open) => !open && setNaming(null)}
          onSubmit={async (name, parent) => {
            if (naming.from === undefined) {
              await create.mutateAsync({ name, parent })
              toast.success(parent ? `Group ${name} created inside ${parent}` : `Group ${name} created`)
            } else if (name !== naming.from) {
              await rename.mutateAsync({ from: naming.from, to: name })
              toast.success(`Renamed to ${name}`)
            }
            setNaming(null)
          }}
        />
      )}

      {deleting && (
        <Dialog open onOpenChange={(open) => !open && setDeleting(null)}>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>{t('Delete')}{deleting.name}?</DialogTitle>
              <DialogDescription>{describeDeletion(deleting)}</DialogDescription>
            </DialogHeader>
            <DialogFooter>
              <Button variant="quiet" onClick={() => setDeleting(null)}>{t('Cancel')}</Button>
              <Button
                variant="danger"
                disabled={remove.isPending}
                onClick={async () => {
                  try {
                    await remove.mutateAsync(deleting.name)
                    toast.success(`Group ${deleting.name} deleted`)
                    setDeleting(null)
                  } catch (error) {
                    toastError(error)
                  }
                }}
              >{t('Delete group')}</Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </>
  )

  return {
    /** Opens the new-group dialog, with a parent preselected when given. */
    create: (parent?: string) => setNaming({ parent }),
    rename: (name: string) => setNaming({ from: name }),
    remove: (deletion: GroupDeletion) => setDeleting(deletion),
    move,
    moveGroup,
    dialogs,
  }
}

/**
 * Where everything in a deleted group ends up, in one sentence.
 *
 * Up one level: devices to the parent (or out of every group, at the top) and
 * subgroups to the parent (or to the top). Said with the counts, because "its
 * contents move up" is a rule, and the operator is deciding about *these*
 * five devices.
 */
function describeDeletion(d: GroupDeletion): string {
  const devices = d.devices === 1 ? 'its device' : `its ${d.devices} devices`
  const groups = d.groups === 1 ? 'the group inside it' : `the ${d.groups} groups inside it`
  const unchanged = 'Names, addresses and how anything connects stay exactly as they are.'
  if (d.devices === 0 && d.groups === 0) return 'Nothing is in it, so nothing else changes.'
  if (d.parent) {
    const what = [d.devices ? devices : '', d.groups ? groups : ''].filter(Boolean).join(' and ')
    return `${capitalise(what)} will move up into ${d.parent}. ${unchanged}`
  }
  const parts = [
    d.devices ? `${devices} will no longer be in a group` : '',
    d.groups ? `${groups} will become ${d.groups === 1 ? 'a top-level group' : 'top-level groups'}` : '',
  ].filter(Boolean)
  return `${capitalise(parts.join(', and '))}. ${unchanged}`
}

function capitalise(s: string) {
  return s.charAt(0).toUpperCase() + s.slice(1)
}

function toastError(error: unknown) {
  toast.error(describeError(error))
}
