import { t, useLocale } from '@/lib/i18n'
import { Trash2 } from 'lucide-react'
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
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { useInterfaces } from '@/features/link/queries'
import type { Pool, PoolIPv4, RouterAdvertisementMode } from '@/lib/config-types'

// The RA vocabulary comes from the published schema (RAMode.JSONSchema in Go),
// so these labels are the only thing added here. The values themselves are not
// restated — see config-types.ts.
//
// Split into a short label and a hint because the label has to survive in the
// closed trigger, where "SLAAC — advertise the prefix (recommended)" was
// truncated into uselessness. The protocol name is not what an operator is
// choosing between; what the devices end up doing is.
const RA_OPTIONS: { value: Exclude<RouterAdvertisementMode, ''>; label: string; hint: string }[] = [
  { value: 'off', get label() { return t('Off') }, get hint() { return t('No IPv6 on this network.') } },
  {
    value: 'slaac',
    get label() { return t('Automatic') },
    get hint() { return t('Clients choose their own IPv6 address. Recommended.') },
  },
  { value: 'stateful', get label() { return t('Managed') }, get hint() { return t('The router hands out IPv6 addresses too.') } },
]

const RA_LABEL = new Map(RA_OPTIONS.map((o) => [o.value as string, o.label]))

const EMPTY: Pool = { network: '', ipv4: {} }

export function PoolDialog({
  open,
  onOpenChange,
  initial,
  onSubmit,
  onRemove,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** Undefined when adding. */
  initial?: Pool
  onSubmit: (pool: Pool) => void
  /** Only supplied when editing. Removing is an edit to the thing you opened,
      which is why it lives here rather than as a second button on the row. */
  onRemove?: () => void
}) {
  useLocale()
  const [draft, setDraft] = useState<Pool>(initial ?? EMPTY)
  const editing = initial !== undefined

  // The networks this router serves. A pool is served on one of them, not on an
  // interface — which is what lets the range be checked against a subnet
  // somebody declared rather than one an interface happens to hold.
  const interfaces = useInterfaces()
  const networks = interfaces.data?.networks ?? []
  const chosen = networks.find((n) => n.name === draft.network)

  function field<K extends keyof Pool>(key: K, value: Pool[K]) {
    setDraft((d) => ({ ...d, [key]: value }))
  }
  function ipv4<K extends keyof PoolIPv4>(key: K, value: PoolIPv4[K]) {
    setDraft((d) => ({ ...d, ipv4: { ...d.ipv4, [key]: value } }))
  }

  // Only the field the server requires. Everything else — whether the range
  // sits inside the network's subnet, whether a reservation collides — is the
  // server's job (design.md §5.3.1) and is deliberately not duplicated: a second
  // validator in the browser would be a second source of truth that disagrees
  // with the first one the day either changes.
  //
  // The range is not required any more. Left blank it is derived from the
  // network's subnet, which is both less typing and the safer answer — see the
  // hint under the fields.
  const servesSomething = draft.ipv4 !== undefined || (draft.ipv6?.mode ?? 'off') !== 'off'
  const complete = draft.network.trim() !== '' && servesSomething

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        onOpenChange(next)
        if (next) setDraft(initial ?? EMPTY)
      }}
    >
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{editing ? `Edit ${initial.network}` : 'Add addresses'}</DialogTitle>
          <DialogDescription>{t('Configure IPv4 address allocation and IPv6 advertisements separately.')}</DialogDescription>
        </DialogHeader>

        <div className="grid gap-4">
          <div className="grid gap-2">
            <Label htmlFor="pool-network">{t('Network')}</Label>
            {editing ? (
              <>
                <Input id="pool-network" value={draft.network} disabled />
                <p className="text-xs text-muted-foreground">
                  The network identifies this pool and cannot be changed. Remove and re-add
                  to move it.
                </p>
              </>
            ) : networks.length === 0 ? (
              <p
                id="pool-network"
                className="rounded-lg border border-dashed px-3 py-4 text-center text-sm text-muted-foreground"
              >
                No network yet. Create one under Networks — it is where the subnet is
                declared, and a range needs one to sit in.
              </p>
            ) : (
              <>
                <Select value={draft.network} onValueChange={(name) => field('network', name ?? '')}>
                  <SelectTrigger id="pool-network" className="w-full">
                    <SelectValue placeholder={t('Choose a network')} />
                  </SelectTrigger>
                  <SelectContent>
                    {networks.map((n) => (
                      <SelectItem key={n.name} value={n.name}>
                        <span className="flex flex-col gap-0.5">
                          <span className="font-mono">{n.name}</span>
                          <span className="text-xs text-muted-foreground">
                            {n.subnet ?? 'no IPv4 subnet'}
                          </span>
                        </span>
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                {chosen && (
                  <p className="text-xs text-muted-foreground">
                    {chosen.subnet
                      ? `Network ${chosen.subnet}. This router is ${chosen.router} on it.`
                      : 'This network has no IPv4 subnet, so it can only advertise IPv6.'}
                  </p>
                )}
              </>
            )}
          </div>

          {/* IPv4 and IPv6 are two sections, not two fields in one, because
              they are two decisions. "Hand out 172.16.1.100-200" and "advertise
              the prefix so devices configure themselves" have nothing in common
              except the network they happen on — and the old form put the
              second one in a dropdown beside the lease time, which said
              otherwise. */}
          <fieldset className="grid gap-3 rounded-lg border p-3">
            <div className="flex items-center justify-between">
              <legend className="text-sm font-medium">{t('IPv4 addresses')}</legend>
              <Switch
                checked={draft.ipv4 !== undefined}
                onCheckedChange={(on) => field('ipv4', on ? {} : undefined)}
                aria-label={t('Hand out IPv4 addresses')}
              />
            </div>

            {draft.ipv4 === undefined ? (
              <p className="text-xs text-muted-foreground">
                No IPv4 addresses are handed out here. Clients get an address only if this
                network advertises IPv6 below.
              </p>
            ) : (
              <>
                <div className="grid grid-cols-2 gap-3">
                  <div className="grid gap-2">
                    <Label htmlFor="pool-start">{t('First address')}</Label>
                    <Input
                      id="pool-start"
                      placeholder={chosen?.suggested_start ?? '192.168.1.100'}
                      value={draft.ipv4.start ?? ''}
                      onChange={(e) => ipv4('start', e.target.value || undefined)}
                    />
                  </div>
                  <div className="grid gap-2">
                    <Label htmlFor="pool-end">{t('Last address')}</Label>
                    <Input
                      id="pool-end"
                      placeholder={chosen?.suggested_end ?? '192.168.1.200'}
                      value={draft.ipv4.end ?? ''}
                      onChange={(e) => ipv4('end', e.target.value || undefined)}
                    />
                  </div>
                </div>
                <p className="text-xs text-muted-foreground">
                  {chosen?.suggested_start
                    ? `Blank uses ${chosen.suggested_start}–${chosen.suggested_end}, derived from
                       the network's subnet. It leaves the low addresses free for devices you
                       configure by hand — DHCP cannot avoid an address it was never told about.`
                    : "Blank derives a range from the network's subnet, leaving the low addresses free for devices configured by hand."}
                </p>
              </>
            )}
          </fieldset>

          <fieldset className="grid gap-3 rounded-lg border p-3">
            <legend className="text-sm font-medium">IPv6</legend>
            <Select
              value={draft.ipv6?.mode ?? 'off'}
              onValueChange={(v) =>
                field(
                  'ipv6',
                  !v || v === 'off'
                    ? undefined
                    : { ...draft.ipv6, mode: v as RouterAdvertisementMode },
                )
              }
            >
              <SelectTrigger id="pool-ra" className="w-full">
                {/* Without the render function this shows the raw schema
                    value — the trigger read "slaac". */}
                <SelectValue>{(value: string) => RA_LABEL.get(value) ?? 'Off'}</SelectValue>
              </SelectTrigger>
              <SelectContent>
                {RA_OPTIONS.map((o) => (
                  <SelectItem key={o.value} value={o.value}>
                    <span className="flex flex-col gap-0.5">
                      <span>{o.label}</span>
                      <span className="text-xs text-muted-foreground">{o.hint}</span>
                    </span>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <p className="text-xs text-muted-foreground">
              There is no range to set: the prefix comes from the network&rsquo;s own IPv6
              address — your internet connection&rsquo;s, or the network&rsquo;s static IPv6
              subnet — and this router follows it when it changes.
            </p>
            {draft.ipv6 && (
              <div className="grid gap-2">
                <Label htmlFor="pool-ra-mtu">{t('MTU to advertise')}</Label>
                <Input
                  id="pool-ra-mtu"
                  inputMode="numeric"
                  value={draft.ipv6.mtu ?? ''}
                  placeholder={t('None — clients use their own')}
                  onChange={(e) => {
                    const n = Number(e.target.value.trim())
                    field('ipv6', {
                      ...draft.ipv6,
                      mtu: e.target.value.trim() && Number.isInteger(n) ? n : undefined,
                    })
                  }}
                />
                <p className="text-xs text-muted-foreground">
                  Set 1480 when IPv6 leaves this router through a 6in4 tunnel, so devices send
                  packets that fit it.
                </p>
              </div>
            )}
          </fieldset>

          <div className="grid gap-2">
            <Label htmlFor="pool-lease">{t('Lease time')}</Label>
            <Input
              id="pool-lease"
              placeholder="12h"
              value={draft.lease_time ?? ''}
              onChange={(e) => field('lease_time', e.target.value || undefined)}
            />
            <p className="text-xs text-muted-foreground">{t('Blank for the default. Units: s, m, h, d, w. Applies to both families.')}</p>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div className="grid gap-2">
              <Label htmlFor="pool-gateway">{t('Gateway')}</Label>
              <Input
                id="pool-gateway"
                placeholder={t('the router itself')}
                value={draft.gateway ?? ''}
                onChange={(e) => field('gateway', e.target.value || undefined)}
              />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="pool-dns">{t('DNS servers')}</Label>
              <Input
                id="pool-dns"
                placeholder={t('the router itself')}
                value={(draft.dns ?? []).join(', ')}
                onChange={(e) => field('dns', splitList(e.target.value))}
              />
            </div>
          </div>

          {/* The trap this note exists for.

              Both fields default to "this router", which is right when the box
              is the gateway and wrong in the deployment people actually start
              with: an olr box added to an existing network, serving addresses
              while the old router still carries the traffic. Left blank there,
              every device is handed a default route to a machine that is not
              gateway and a resolver that is not answering — and the symptom is
              "the new router broke my internet", which is the worst possible
              first impression to debug. */}
          <p className="text-xs text-muted-foreground">
            Blank means this router
            {chosen?.router ? ` (${chosen.router})` : ''}. If something else on
            this network still provides the internet connection, put its address
            in both — otherwise devices will send their traffic and their name
            lookups here.
            <br />{t('DNS servers are comma separated.')}</p>

          <div className="grid gap-2">
            <Label htmlFor="pool-domain">{t('Domain')}</Label>
            <Input
              id="pool-domain"
              placeholder="lan"
              value={draft.domain ?? ''}
              onChange={(e) => field('domain', e.target.value || undefined)}
            />
          </div>
        </div>

        <DialogFooter>
          {onRemove && (
            <Button
              variant="danger"
              className="mr-auto"
              onClick={() => {
                onRemove()
                onOpenChange(false)
              }}
            >
              <Trash2 className="size-4" aria-hidden />{t('Remove')}</Button>
          )}
          <Button variant="quiet" onClick={() => onOpenChange(false)}>{t('Cancel')}</Button>
          <Button
            disabled={!complete}
            onClick={() => {
              onSubmit(normalise(draft))
              onOpenChange(false)
            }}
          >
            {editing ? t('Save') : t('Add')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function splitList(value: string): string[] | undefined {
  const items = value
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
  return items.length ? items : undefined
}

/**
 * Trims, and drops empty optional fields so they are omitted rather than sent
 * blank.
 *
 * The IPv4 block matters here: an empty `{}` means "derive the range", while
 * `{start: ""}` is a range the server has to reject. Blank fields have to
 * disappear, not travel as empty strings.
 */
function normalise(pool: Pool): Pool {
  const out: Pool = {
    ...pool,
    network: pool.network.trim(),
    lease_time: pool.lease_time?.trim() || undefined,
    gateway: pool.gateway?.trim() || undefined,
    domain: pool.domain?.trim() || undefined,
  }
  if (pool.ipv4) {
    out.ipv4 = {
      start: pool.ipv4.start?.trim() || undefined,
      end: pool.ipv4.end?.trim() || undefined,
    }
  }
  if (!pool.ipv6?.mode || pool.ipv6.mode === 'off') out.ipv6 = undefined
  return out
}
