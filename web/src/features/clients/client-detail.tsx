import { t, useLocale } from '@/lib/i18n'
import { Tag, Trash2 } from 'lucide-react'
import { useState } from 'react'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Disclosure } from '@/components/ui/disclosure'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { CategoryPicker, type IconChoice } from '@/features/clients/category-picker'
import { namePlaceholder } from '@/features/clients/client-name'
import { DeviceIcon } from '@/features/clients/client-icon'
import { groupOptions } from '@/features/clients/group-tree'
import { categoryLabel } from '@/features/clients/icons'
import { lastHeardTitle } from '@/features/clients/last-heard'
import type { ClientRow } from '@/lib/api-types'
import type { Client, ClientsGroup, GatewayConfig } from '@/lib/config-types'

/**
 * The Select's value for "in no group". Base UI treats an empty string as a
 * real value, but a sentinel that cannot be a group name — names are trimmed
 * server-side, so none starts with a space — keeps "none" from ever colliding
 * with one.
 */
const NO_GROUP = ' none'
const INHERIT_EXIT = ' inherit'
const DIRECT_EXIT = ' direct'

/**
 * One device, and everything a human is allowed to say about it.
 *
 * The dialog holds the two halves of §4.4 apart on screen, because conflating
 * them is how an inventory starts lying: the top is *identity*, which is stored
 * and which the operator owns, and the bottom is *presence*, which is observed,
 * stamped, and read-only. Nothing observed is editable here and nothing stored
 * is presented as a fact about the network.
 */
export function ClientDetail({
  device,
  open,
  onOpenChange,
  onSave,
  onForget,
  onEditFixedAddress,
  groups = [],
  gateway,
  gatewayBusy,
  onExitChange,
  busy,
}: {
  device: ClientRow
  open: boolean
  onOpenChange: (open: boolean) => void
  onSave: (identity: Client) => void
  /** Only offered for a device that has stored identity to drop. */
  onForget?: () => void
  onEditFixedAddress: () => void
  /** Every group there is, to choose this device's from. */
  groups?: ClientsGroup[]
  gateway?: GatewayConfig
  gatewayBusy?: boolean
  onExitChange?: (exit: string | null) => Promise<boolean>
  busy?: boolean
}) {
  useLocale()
  const [name, setName] = useState(device.name_origin === 'operator' ? device.name : '')
  const [look, setLook] = useState<IconChoice>(() => storedLook(device))
  const [notes, setNotes] = useState(device.notes ?? '')
  const [group, setGroup] = useState(device.group ?? '')
  const override = gateway?.devices?.find((a) => a.mac === device.mac)
  const deviceExit = override ? override.exit || DIRECT_EXIT : INHERIT_EXIT

  function reset() {
    setName(device.name_origin === 'operator' ? device.name : '')
    setLook(storedLook(device))
    setNotes(device.notes ?? '')
    setGroup(device.group ?? '')
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        onOpenChange(next)
        if (next) reset()
      }}
    >
      <DialogContent className="max-h-[90svh] overflow-y-auto">
        <DialogHeader>
          <div className="flex items-center gap-3">
            <DeviceIcon
              icon={device.icon}
              category={device.category}
              vendor={device.vendor}
              vendorKey={device.vendor_key}
              size="lg"
            />
            <div className="min-w-0">
              <DialogTitle className="truncate">{device.name || device.mac}</DialogTitle>
              <DialogDescription className="truncate font-mono text-xs">
                {device.mac}
              </DialogDescription>
              {/* The same fact every other surface reports, in its long form:
                  a dialog has room for the sentence, and the sentence is what
                  makes a dash or "never seen" mean something. */}
              <p className="mt-1 text-xs text-muted-foreground">{lastHeardTitle(device)}</p>
            </div>
          </div>
        </DialogHeader>

        <div className="grid gap-5">
          <div className="grid gap-2">
            <Label htmlFor="device-name">{t('Name')}</Label>
            <Input
              id="device-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={
                // The hostname is shown as the placeholder rather than filled
                // in, so it is visibly the client's own claim and not something
                // the operator has agreed to. A remembered name says so for the
                // same reason: it is what the device called itself when it was
                // last here, not what it is saying now.
                namePlaceholder(device)
              }
            />
          </div>

          <div className="grid gap-2">
            <Label>{t('Icon')}</Label>
            <CategoryPicker value={look} onChange={setLook} />
          </div>

          {/* Part of the form, saved with the rest, rather than applied the
              moment it changes. A field that took effect on its own inside a
              dialog with a Cancel button would make Cancel a lie. The quick
              path is the map's own "Move to group", which is one click and
              says so. */}
          <div className="grid gap-2">
            <Label htmlFor="device-group">{t('Group')}</Label>
            <Select
              value={group || NO_GROUP}
              onValueChange={(v) => setGroup(!v || v === NO_GROUP ? '' : v)}
            >
              <SelectTrigger id="device-group" className="w-full">
                <SelectValue>
                  {(value: string) => (value === NO_GROUP || !value ? t('None') : value)}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={NO_GROUP}>{t('None')}</SelectItem>
                {/* In tree order and indented by depth: a flat alphabetical
                    list would put Lab nowhere near the VMs it sits inside. */}
                {groupOptions(groups).map((g) => (
                  <SelectItem key={g.name} value={g.name}>
                    <span style={{ paddingLeft: g.depth * 12 }}>{g.name}</span>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {groups.length === 0 && (
              <p className="text-xs text-muted-foreground">
                No groups yet. Create one from the overview, then put devices in it here.
              </p>
            )}
          </div>

          <div className="grid gap-2">
            <Label htmlFor="device-notes">{t('Notes')}</Label>
            <Textarea
              id="device-notes"
              rows={2}
              value={notes}
              onChange={(e) => setNotes(e.target.value)}
              placeholder={t('Where it lives, whose it is — anything you want to remember')}
            />
          </div>

          <FixedAddress device={device} onEdit={onEditFixedAddress} />

          {gateway && onExitChange && (
            <div className="grid gap-2">
              <Label htmlFor="device-exit">{t('Internet via')}</Label>
              <Select
                value={deviceExit}
                disabled={gatewayBusy}
                onValueChange={async (value) => {
                  if (!value) return
                  await onExitChange(
                    value === INHERIT_EXIT ? null : value === DIRECT_EXIT ? '' : value,
                  )
                }}
              >
                <SelectTrigger id="device-exit" className="w-full">
                  <SelectValue>
                    {(value: string) =>
                      value === INHERIT_EXIT
                        ? t('Follow the network setting')
                        : value === DIRECT_EXIT
                          ? t('This router’s own connection')
                          : value
                    }
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={INHERIT_EXIT}>{t('Follow the network setting')}</SelectItem>
                  <SelectItem value={DIRECT_EXIT}>{t('This router’s own connection')}</SelectItem>
                  {(gateway.exits ?? []).map((exit) => (
                    <SelectItem key={exit.name} value={exit.name}>
                      {exit.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <p className="text-xs text-muted-foreground">
                For devices directly on this router&apos;s Ethernet network. A device behind another
                router, or using a different private MAC, will not match. Changes apply immediately;
                established connections normally keep their path.
              </p>
              {deviceExit === INHERIT_EXIT && (
                <p className="text-xs text-muted-foreground"> {t('Currently follows')} {device.network || 'its network'}; check Internet via on the
                  Gateway page.
                </p>
              )}
            </div>
          )}

          <Observed device={device} />
        </div>

        <DialogFooter>
          {onForget && (
            <Button
              variant="danger"
              className="mr-auto"
              disabled={busy}
              onClick={() => {
                onForget()
                onOpenChange(false)
              }}
            >
              <Trash2 className="size-4" aria-hidden /> {t('Forget')} </Button>
          )}
          <Button variant="quiet" disabled={busy} onClick={() => onOpenChange(false)}> {t('Cancel')} </Button>
          <Button
            disabled={busy}
            onClick={() => {
              onSave({
                mac: device.mac,
                name: name.trim() || undefined,
                category: look.category || undefined,
                icon: look.icon || undefined,
                notes: notes.trim() || undefined,
                group: group || undefined,
                // Preserved rather than edited: there is no tier-2 image set to
                // choose from yet, and silently dropping the field on every
                // save would quietly erase an operator's earlier answer.
                model: device.model || undefined,
              })
              onOpenChange(false)
            }}
          > {t('Save')} </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/** What the operator has said about the look, with '' for anything left to detection. */
function storedLook(device: ClientRow): IconChoice {
  return {
    category: device.category_origin === 'operator' ? device.category : '',
    icon: device.icon ?? '',
  }
}

/**
 * The fixed address, owned by dhcp.
 *
 * §11.1: a fixed address is a property *of a device*, set from the device list
 * rather than by typing a MAC into a form. This is that surface. The fact still
 * lives in the dhcp module — the button hands off to dhcp's own editor, so the
 * change goes through dhcp's plan and its impact gate rather than sneaking a
 * network change through a screen that otherwise cannot cause one.
 */
function FixedAddress({ device, onEdit }: { device: ClientRow; onEdit: () => void }) {
  useLocale()
  return (
    <div className="rounded-lg border p-3">
      <div className="flex items-center justify-between gap-3">
        <div className="min-w-0">
          <div className="flex items-center gap-1.5 text-sm font-medium">
            <Tag className="size-3.5 shrink-0 text-muted-foreground" aria-hidden /> {t('Fixed address')} </div>
          <div className="mt-0.5 truncate text-[0.8rem] text-muted-foreground">
            {device.fixed_ip ? (
              <span className="font-mono">{device.fixed_ip}</span>
            ) : (
              'This device gets whichever address is free.'
            )}
          </div>
        </div>
        <Button variant="secondary" size="sm" onClick={onEdit}>
          {device.fixed_ip ? t('Change') : 'Reserve'}
        </Button>
      </div>
    </div>
  )
}

/** Everything read off the network. Read-only, and stamped by the caller. */
function Observed({ device }: { device: ClientRow }) {
  useLocale()
  const rows: [string, React.ReactNode][] = []

  if (device.ips?.length) {
    rows.push([
      device.ips.length > 1 ? t('Addresses') : t('Address'),
      <span key="ips" className="font-mono">
        {device.ips.join(', ')}
      </span>,
    ])
  }
  if (device.hostname) rows.push(['Calls itself', device.hostname])
  if (device.vendor) rows.push(['Vendor', device.vendor])
  if (device.sources?.length) {
    rows.push(['Seen by', device.sources.map(sourceLabel).join(', ')])
  }
  if (device.expires) {
    rows.push(['Lease expires', new Date(device.expires).toLocaleString()])
  }
  if (device.detected_category) {
    rows.push([
      'Detected as',
      <span key="detected">
        {categoryLabel(device.detected_category)}
        {device.detect_reason && (
          <span className="text-muted-foreground"> — {device.detect_reason}</span>
        )}
      </span>,
    ])
  }

  if (rows.length === 0) {
    return (
      <p className="text-[0.8rem] text-muted-foreground"> {t('Nothing has been observed about this client yet.')} </p>
    )
  }

  return (
    <Disclosure summary={t('What the network says')}>
      <dl className="grid gap-1.5 pt-1 text-[0.8rem]">
        {rows.map(([label, value], i) => (
          <div key={i} className="flex gap-3">
            <dt className="w-28 shrink-0 text-muted-foreground">{label}</dt>
            <dd className="min-w-0 break-words">{value}</dd>
          </div>
        ))}
      </dl>
    </Disclosure>
  )
}

function sourceLabel(source: string): string {
  switch (source) {
    case 'dhcp-lease':
      return 'DHCP lease'
    case 'arp':
      return 'network traffic'
    default:
      return source
  }
}
