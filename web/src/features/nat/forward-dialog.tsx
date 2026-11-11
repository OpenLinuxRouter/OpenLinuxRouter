import { t, useLocale } from '@/lib/i18n'
import { AlertTriangle, Trash2 } from 'lucide-react'
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
import { Switch } from '@/components/ui/switch'
import { useClientList } from '@/features/clients/queries'
import { useInterfaces } from '@/features/link/queries'
import type { ClientRow } from '@/lib/api-types'
import type { Forward, Protocol } from '@/lib/config-types'

const EMPTY: Forward = { name: '', in: '', slot: 0, port: '', to: '' }

/**
 * What each protocol is, said the way somebody choosing needs to hear it. The
 * schema words are fine here — `tcp` and `udp` are what a service's own
 * documentation says — so only the third needs translating.
 */
const PROTOCOLS: { value: Protocol; label: string; hint: string }[] = [
  { value: 'tcp', label: 'TCP', get hint() { return t('Web, SSH, mail, and most things.') } },
  { value: 'udp', label: 'UDP', hint: 'Games, voice, and VPNs like WireGuard.' },
  { value: 'both', get label() { return t('TCP and UDP') }, get hint() { return t('For a service that needs both, such as a game server.') } },
]

/** The sentinel for "type the address yourself", which is not a device MAC. */
const MANUAL = ' manual'

/**
 * Adds or edits a port forward.
 *
 * The form asks four things — a name, where connections arrive, on what port,
 * and where they go — and defaults everything else. Hairpin is behind the
 * disclosure with its cost spelled out rather than left as jargon, because it is
 * on by default and what it costs is invisible until somebody reads a log on the
 * other machine (docs/port-forwarding.md §4.1).
 *
 * The destination is an **address**, not a device reference, and the picker
 * below does not change that — it fills the field in. docs/port-forwarding.md §1.2 has
 * the argument: a forward is a kernel rule that has to exist at boot, before any
 * lease has been handed out and while the device is switched off. What the
 * picker adds is the warning that goes with it — a device with no fixed address
 * can be given a different one tomorrow, and then this rule points at whatever
 * took it.
 */
export function ForwardDialog({
  open,
  onOpenChange,
  initial,
  onSubmit,
  onRemove,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  initial?: Forward
  onSubmit: (forward: Forward) => void
  /** Only supplied when editing. Removing is an edit to the thing you opened. */
  onRemove?: () => void
}) {
  useLocale()
  const [draft, setDraft] = useState<Forward>(initial ?? EMPTY)
  const [picked, setPicked] = useState<string>(MANUAL)
  const editing = initial !== undefined

  const interfaces = useInterfaces()
  const devices = useClientList()

  function field<K extends keyof Forward>(key: K, value: Forward[K]) {
    setDraft((d) => ({ ...d, [key]: value }))
  }

  // Only adopted interfaces: the daemon refuses the rest, and offering a choice
  // that is about to be rejected is worse than not offering it (design.md §7 —
  // olr touches nothing it was not given).
  const adopted = (interfaces.data?.interfaces ?? []).filter((i) => i.adopted)

  // A device is only offerable if we know an address for it. One with none has
  // nothing to put in the field.
  const offerable = (devices.data?.clients ?? []).filter((d) => addressOf(d) !== undefined)
  const pickedDevice = offerable.find((d) => d.mac === picked)

  // The Select reports null for "nothing chosen", which is the same statement as
  // the manual sentinel here: the address field is whatever was typed into it.
  function pickDevice(value: string | null) {
    const mac = value ?? MANUAL
    setPicked(mac)
    if (mac === MANUAL) return
    const device = offerable.find((d) => d.mac === mac)
    const address = device && addressOf(device)
    if (!address) return
    // Keep whatever port was already typed, so choosing a device after filling
    // the rest in does not undo the rest.
    setDraft((d) => ({ ...d, to: `${address}:${portOf(d.to) || d.port || ''}` }))
  }

  const complete = draft.name.trim() && draft.in.trim() && draft.port.trim() && draft.to.trim()

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) {
          setDraft(initial ?? EMPTY)
          setPicked(MANUAL)
        }
        onOpenChange(next)
      }}
    >
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{editing ? `Edit ${initial.name}` : 'Forward a port'}</DialogTitle>
          <DialogDescription>
            Let something on the internet connect to one device on your network, through this
            router.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="fwd-name">{t('Name')}</Label>
            <Input
              id="fwd-name"
              value={draft.name}
              placeholder="web"
              onChange={(e) => field('name', e.target.value)}
            />
            <p className="text-xs text-muted-foreground"> {t('What you will call it in the list. Anything you like.')} </p>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="fwd-in">{t('Connections arrive on')}</Label>
            <Select value={draft.in || undefined} onValueChange={(v) => field('in', v ?? '')}>
              <SelectTrigger id="fwd-in">
                <SelectValue placeholder={t('Choose an interface')} />
              </SelectTrigger>
              <SelectContent>
                {adopted.map((i) => (
                  <SelectItem key={i.name} value={i.name}>
                    {i.name}
                    {i.address ? ` — ${i.address}` : ''}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <p className="text-xs text-muted-foreground">
              The connection to the outside world. Only interfaces you have given to this router
              are listed.
            </p>
          </div>

          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label htmlFor="fwd-protocol">{t('Kind of traffic')}</Label>
              <Select
                value={draft.protocol || 'tcp'}
                onValueChange={(v) => field('protocol', v as Protocol)}
              >
                <SelectTrigger id="fwd-protocol">
                  <SelectValue>
                    {(v: string) => PROTOCOLS.find((p) => p.value === v)?.label ?? 'TCP'}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {PROTOCOLS.map((p) => (
                    <SelectItem key={p.value} value={p.value}>
                      {p.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="fwd-port">{t('On port')}</Label>
              <Input
                id="fwd-port"
                value={draft.port}
                placeholder="8080"
                onChange={(e) => field('port', e.target.value)}
              />
            </div>
          </div>
          <p className="-mt-2 text-xs text-muted-foreground">
            {PROTOCOLS.find((p) => p.value === (draft.protocol || 'tcp'))?.hint} A range like{' '}
            <code>30000-30010</code> works too, but it can only go to the same range inside.
          </p>

          {offerable.length > 0 && (
            <div className="space-y-1.5">
              <Label htmlFor="fwd-device">{t('Go to client')}</Label>
              <Select value={picked} onValueChange={pickDevice}>
                <SelectTrigger id="fwd-device">
                  <SelectValue>
                    {(v: string) =>
                      v === MANUAL
                        ? t('Type an address instead')
                        : (offerable.find((d) => d.mac === v)?.name ?? v)
                    }
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={MANUAL}>{t('Type an address instead')}</SelectItem>
                  {offerable.map((d) => (
                    <SelectItem key={d.mac} value={d.mac}>
                      {d.name || d.mac} — {addressOf(d)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {pickedDevice && !pickedDevice.fixed_ip && (
                // The address is what gets stored either way, so a device whose
                // address can change tomorrow is a forward that will quietly
                // point at whatever took it.
                <p className="flex gap-2 text-xs text-warning-foreground">
                  <AlertTriangle className="mt-0.5 size-3.5 shrink-0" aria-hidden />
                  <span>
                    {pickedDevice.name || pickedDevice.mac} does not have a reserved address, so it
                    may be given a different one later — and this forward would keep pointing at
                    the old one. Reserve an address for it under Addresses.
                  </span>
                </p>
              )}
            </div>
          )}

          <div className="space-y-1.5">
            <Label htmlFor="fwd-to">{t('Deliver to')}</Label>
            <Input
              id="fwd-to"
              value={draft.to}
              placeholder="192.168.1.10:80"
              onChange={(e) => {
                field('to', e.target.value)
                setPicked(MANUAL)
              }}
            />
            <p className="text-xs text-muted-foreground">
              The address on your network and the port there — they do not have to match the port
              above. IPv6 is not forwarded.
            </p>
          </div>

          <Disclosure summary={t('Reaching it from inside your own network')}>
            <div className="flex items-start justify-between gap-4 pt-1">
              <div className="space-y-1">
                <Label htmlFor="fwd-hairpin">{t('Works from inside too')}</Label>
                <p className="text-xs text-muted-foreground">
                  On, so the same address works whether you are at home or away — otherwise the
                  forward appears broken from every machine in the house. The cost is that the
                  device sees this router as the source of those connections and cannot tell your
                  own machines apart. Turn it off if it needs to, and reach it by its internal
                  address from home.
                </p>
              </div>
              <Switch
                id="fwd-hairpin"
                checked={draft.hairpin ?? true}
                onCheckedChange={(v) => field('hairpin', v)}
              />
            </div>
          </Disclosure>
        </div>

        <DialogFooter className="sm:justify-between">
          {onRemove ? (
            <Button variant="danger" onClick={onRemove}>
              <Trash2 />{t('Remove')}</Button>
          ) : (
            <span />
          )}
          <div className="flex gap-2">
            <Button variant="quiet" onClick={() => onOpenChange(false)}>{t('Cancel')}</Button>
            <Button
              disabled={!complete}
              onClick={() => {
                onSubmit({
                  ...draft,
                  name: draft.name.trim(),
                  in: draft.in.trim(),
                  port: draft.port.trim(),
                  to: draft.to.trim(),
                })
                onOpenChange(false)
              }}
            >
              {editing ? t('Save') : t('Add')}
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/**
 * The address to forward to, preferring the reserved one.
 *
 * A fixed address is the one that will still be true next month, which is the
 * whole reason the warning above exists. IPv4 only: docs/port-forwarding.md §7 says
 * IPv6 is not forwarded, so offering a v6 address would fill the field with
 * something validation then rejects.
 */
function addressOf(d: ClientRow): string | undefined {
  if (d.fixed_ip) return d.fixed_ip
  return d.ips?.find((ip) => !ip.includes(':'))
}

/** The port already typed into `to`, so picking a device keeps it. */
function portOf(to: string): string {
  const at = to.lastIndexOf(':')
  return at < 0 ? '' : to.slice(at + 1)
}
