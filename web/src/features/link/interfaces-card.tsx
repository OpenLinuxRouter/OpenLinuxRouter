import { t, useLocale } from '@/lib/i18n'
import { AlertTriangle, ChevronRight } from 'lucide-react'
import { useState } from 'react'
import { Link } from 'react-router'
import { toast } from 'sonner'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Label } from '@/components/ui/label'
import { ListEmpty } from '@/components/ui/list'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { useUplink } from '@/features/dial/queries'
import {
  useApplyLinkConfig,
  useInterfaces,
  useLinkConfig,
  useRemoveAddress,
} from '@/features/link/queries'
import { ApiError } from '@/lib/api'
import type { NetworkRow, InterfaceRow } from '@/lib/api-types'
import type { DhcpConfig, Pool } from '@/lib/config-types'
import { state, type Health } from '@/lib/state'
import { StatusInline } from '@/components/ui/status'

/**
 * Which interfaces this router has been given.
 *
 * olr refuses to serve anything on an interface nobody handed it (design.md
 * §3.4), so on a fresh install every range and every resolver is rejected until
 * one row here is switched on. Before this existed, that refusal arrived as a
 * validation error on a form field, naming a permission the UI gave you no way
 * to grant — and the DHCP and DNS landing pages now say so *before* the switch
 * is touched, linking to the page this card sits on.
 *
 * Adoption sits above the network form on Gateway, so an interface can be
 * handed over before a network is assigned. Detail links remain available for
 * link diagnostics and per-interface settings.
 */
export function InterfacesCard({
  dhcp,
  disabled,
  only,
  physicalOnly = false,
  /**
   * The rows lead to their own pages and carry no control of their own. The
   * adoption switch lives on the interface page, so a row is never a chevron
   * and a switch at the same time.
   */
  navigationOnly = false,
  uplinkAction,
  footer,
}: {
  /** The stored DHCP config, so a release can warn about the pool it breaks. */
  dhcp?: DhcpConfig
  disabled?: boolean
  only?: string
  physicalOnly?: boolean
  navigationOnly?: boolean
  uplinkAction?: React.ReactNode
  footer?: React.ReactNode
}) {
  useLocale()
  const interfaces = useInterfaces()
  const config = useLinkConfig()
  const apply = useApplyLinkConfig()
  // Read only to label the row that is the way out. This card owns adoption and
  // nothing else; the uplink is configured in its own section on the same page.
  const uplink = useUplink()

  /** A release held back because a pool still names the interface. */
  const [confirming, setConfirming] = useState<InterfaceRow | null>(null)

  /** An address removal waiting for its confirmation. */
  const [removing, setRemoving] = useState<{ iface: string; address: string } | null>(null)
  const addresses = useRemoveAddress()
  const uplinkName = uplink.data?.uplink?.interface

  // Locked until the stored config has been read, because write() needs it to
  // build a complete body and a switch that silently does nothing is worse than
  // one that is visibly not ready yet.
  const busy = disabled || apply.isPending || config.isPending
  const rows = (interfaces.data?.interfaces ?? []).filter((row) =>
    (!only || row.name === only) && (!physicalOnly || row.physical),
  )
  const adopted = config.data?.adopted ?? []

  /**
   * Writes the new adopted set, announcing only what actually landed.
   *
   * The whole stored document goes on the wire, not just the field this card
   * owns. `PUT /api/link/config` is `BodyFull` — it *replaces* the module's
   * config — so a body of `{adopted}` alone deleted every network, and deleted
   * them silently in the worst possible way: PlanAddrs walks the networks, so an
   * empty list produced no kernel steps at all, and each interface kept the
   * address whose reason for existing had just been thrown away. Flipping one
   * switch here showed a success toast and lost the network on a different NIC.
   *
   * Nothing is written before the config has been read, for the same reason: an
   * undefined `config.data` spread into the body is a body with no networks in
   * it, which is the bug again with a narrower window.
   *
   * `done` is awaited rather than fired alongside the call: a toast published
   * before the mutation resolves would sit on screen next to the error toast
   * from the same failed write, telling the operator two different things about
   * one click.
   */
  async function write(next: string[], done: string) {
    if (!config.isSuccess) return
    try {
      await apply.mutateAsync({
        ...config.data,
        adopted: next.length ? next : undefined,
        ipv6_auto: Object.fromEntries(Object.entries(config.data.ipv6_auto ?? {}).filter(([name]) => next.includes(name))),
      })
      toast.success(done)
    } catch (error) {
      if (error instanceof ApiError) {
        toast.error(error.message, {
          description: error.problems.map((p) => p.message).join('\n') || undefined,
        })
      } else {
        toast.error(String(error))
      }
    }
  }

  function adopt(row: InterfaceRow) {
    void write([...adopted, row.name], `${row.name} is now yours to configure`)
  }

  function release(row: InterfaceRow) {
    void write(
      adopted.filter((name) => name !== row.name),
      `${row.name} handed back`,
    )
  }

  function toggle(row: InterfaceRow, on: boolean) {
    if (on) {
      adopt(row)
      return
    }
    // Releasing an interface a pool still reaches does not undo the pool — it
    // makes the network invalid, and the pool on it with it, so the next apply
    // fails with a message about adoption. Asking first is cheaper than
    // explaining that.
    if (poolsOn(dhcp, interfaces.data?.networks, row.name).length > 0) {
      setConfirming(row)
      return
    }
    release(row)
  }

  return (
    <Card>
      <CardContent className="space-y-3">
        {interfaces.isPending && <Skeleton className="h-32 w-full rounded-xl" />}

        {interfaces.isError && (
          <Alert variant="destructive">
            <AlertTriangle />
            <AlertTitle>{t('Could not read this machine\'s interfaces')}</AlertTitle>
            <AlertDescription>{(interfaces.error as Error).message}</AlertDescription>
          </Alert>
        )}

        {interfaces.isSuccess && rows.length === 0 && (
          <>
            <ListEmpty>{physicalOnly ? 'No physical interfaces found. Virtual and missing interfaces are in Advanced interfaces and routing.' : 'This machine reports no network interfaces.'}</ListEmpty>
            {footer}
          </>
        )}

        {rows.length > 0 && (
          <ul className="divide-y divide-border overflow-hidden rounded-xl border">
            {rows.map((row) => (
              <InterfaceItem
                key={row.name}
                row={row}
                role={roleOf(row, uplinkName)}
                uplinkAction={row.name === uplinkName ? uplinkAction : undefined}
                busy={!!busy}
                showLink={!only}
                onToggle={navigationOnly ? undefined : (on) => toggle(row, on)}
                // Only where nothing in olr owns the addressing: a network's
                // member is its network's, and the uplink's leftovers are
                // offered on its own card, beside the address it does claim.
                onRemoveAddress={
                  row.adopted && !row.network && row.name !== uplinkName
                    ? (address) => setRemoving({ iface: row.name, address })
                    : undefined
                }
              />
            ))}
            {footer && <li className="bg-muted/20 px-4 py-3">{footer}</li>}
          </ul>
        )}

        {interfaces.isSuccess && rows.length > 0 && adopted.length === 0 && !only && (
          <p className="text-sm text-muted-foreground">
            Nothing is adopted yet. Switch on the interface facing your home
            network — usually the one with an address on it — and then add a
            network under DHCP. Switch on a second one if this router serves
            two subnets.
          </p>
        )}
      </CardContent>

      <RemoveAddressDialog
        target={removing}
        busy={addresses.busy}
        onCancel={() => setRemoving(null)}
        onConfirm={async () => {
          if (removing) await addresses.remove(removing.iface, removing.address)
          setRemoving(null)
        }}
      />

      <ReleaseDialog
        row={confirming}
        pools={poolsOn(dhcp, interfaces.data?.networks, confirming?.name)}
        onCancel={() => setConfirming(null)}
        onConfirm={() => {
          if (confirming) release(confirming)
          setConfirming(null)
        }}
      />
    </Card>
  )
}

/* -------------------------------------------------------------------------- */

/**
 * What an adopted interface is *for*, in one word.
 *
 * Two roles and they are mutually exclusive by construction: an interface
 * either carries a network this router serves or is the one way out, and the
 * server refuses an uplink on a network's member. Showing it here is what makes
 * that legible from the list rather than only from the two sections below it —
 * "which NIC is my WAN" was previously answerable only by reading addresses and
 * guessing.
 */
function roleOf(row: InterfaceRow, uplink: string | undefined): string | undefined {
  if (row.name === uplink) return 'uplink'
  return row.network
}

function InterfaceItem({
  row,
  role,
  uplinkAction,
  busy,
  showLink,
  onToggle,
  onRemoveAddress,
}: {
  row: InterfaceRow
  role?: string
  uplinkAction?: React.ReactNode
  busy: boolean
  /** False on the interface's own page, where the link would point at itself. */
  showLink: boolean
  /** Omitted where the row's only control is the link to its page. */
  onToggle?: (on: boolean) => void
  /** Offered only on an interface no network or uplink owns. */
  onRemoveAddress?: (address: string) => void
}) {
  useLocale()
  const id = `adopt-${row.name}`
  const state = describeState(row)
  // IPv4 only: olr writes no IPv6 address, so it has none to take away.
  const removable = onRemoveAddress ? (row.prefixes ?? []).filter((p) => !p.includes(':')) : []

  return (
    <li className="flex min-h-14 items-center gap-3 bg-card px-4 py-2.5">
      <div className="min-w-0 flex-1">
        <div className="flex min-w-0 items-center gap-2">
          <span className="truncate font-mono text-sm font-medium">{row.name}</span>
          {role && <Badge variant="secondary">{role}</Badge>}
          <StatusInline tone={state.tone}>{state.label}</StatusInline>
        </div>
        <div className="font-mono text-meta leading-5 text-foreground [overflow-wrap:anywhere]">
          {row.prefixes?.length ? row.prefixes.join(' · ') : state.detail}
        </div>
        {removable.length > 0 && (
          <div className="flex flex-wrap gap-x-3 text-meta">
            {removable.map((address) => (
              <button
                key={address}
                type="button"
                disabled={busy}
                className="text-muted-foreground underline underline-offset-4 hover:text-foreground disabled:opacity-50"
                onClick={() => onRemoveAddress?.(address)}
              >{t('Remove')}{address}
              </button>
            ))}
          </div>
        )}
      </div>

      {uplinkAction}
      <Label htmlFor={id} className="sr-only">
        Give {row.name} to this router
      </Label>
      {showLink && (
        <Link
          to={`/gateway/interfaces/${encodeURIComponent(row.name)}`}
          className="inline-flex size-9 shrink-0 items-center justify-center rounded-md text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:outline-2 focus-visible:outline-ring"
          aria-label={`View ${row.name} details`}
          title={`View ${row.name} details`}
        >
          <ChevronRight className="size-4" aria-hidden />
        </Link>
      )}
      <Switch
        id={id}
        checked={row.adopted}
        // Loopback has no clients on it and nothing olr can usefully serve
        // there, so the server refuses it. Disabling the control says the same
        // thing without making the operator find out by being rejected.
        busy={busy} disabled={row.loopback}
        onCheckedChange={onToggle}
      />
    </li>
  )
}

/**
 * The one sentence that distinguishes the states an operator has to act on
 * differently. "Up" alone cannot say "up with no cable in it", which is the most
 * common reason a freshly configured DHCP server appears to do nothing.
 */
function describeState(row: InterfaceRow): Health & { detail: string } {
  if (!row.present) {
    return { ...state('Not found'), detail: 'This machine has no interface with this name.' }
  }
  if (row.loopback) {
    return { ...state('Loopback'), detail: 'The machine talking to itself.' }
  }
  if (!row.up) {
    return { ...state('Down'), detail: 'Down.' }
  }
  if (!row.running) {
    return { ...state('No cable'), detail: 'Up, but nothing is plugged in.' }
  }
  if (!row.prefixes?.length) {
    return { ...state('No address'), detail: 'No address. Give it one before serving a range here.' }
  }
  return { ...state('Connected'), detail: 'Connected.' }
}

/**
 * Which pools would stop working if this interface were released.
 *
 * Two hops now rather than one: a pool names a network and the network names
 * the interface. The indirection is the point — it is what lets a network move
 * to a different NIC without every pool having to be re-pointed — but the
 * warning still has to reach all the way back to the interface the operator is
 * about to take away.
 */
function poolsOn(
  dhcp: DhcpConfig | undefined,
  networks: NetworkRow[] | undefined,
  name: string | undefined,
) {
  if (!dhcp || !name) return []
  const onThisInterface = new Set(
    (networks ?? []).filter((n) => n.members.includes(name)).map((n) => n.name),
  )
  return (dhcp.pools ?? []).filter((p) => onThisInterface.has(p.network))
}

function ReleaseDialog({
  row,
  pools,
  onCancel,
  onConfirm,
}: {
  row: InterfaceRow | null
  pools: Pool[]
  onCancel: () => void
  onConfirm: () => void
}) {
  useLocale()
  return (
    <Dialog open={row !== null} onOpenChange={(open) => !open && onCancel()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <AlertTriangle className="size-4 text-warning-foreground" aria-hidden />
            An address range uses {row?.name}
          </DialogTitle>
          <DialogDescription>
            Taking {row?.name} back does not remove the range below, and does not
            stop the devices already using it. What it does is make the range
            invalid, so the next change you save on this page will be refused
            until you either adopt {row?.name} again or remove the range.
          </DialogDescription>
        </DialogHeader>

        <ul className="space-y-1 font-mono text-xs text-muted-foreground">
          {pools.map((p) => (
            <li key={p.network}>
              {p.network}
            </li>
          ))}
        </ul>

        <DialogFooter>
          <Button variant="quiet" onClick={onCancel}>{t('Cancel')}</Button>
          <Button onClick={onConfirm}>{t('Take it back anyway')}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/**
 * The confirmation before an address comes off by hand.
 *
 * It says what olr knows and no more. Nothing in olr accounts for the address,
 * which is true of a network's leftover and equally true of the address a
 * distribution put on an interface that has been adopted but not yet given a
 * network — the ordinary first-run state. Which of the two it is, the operator
 * knows and olr does not.
 */
function RemoveAddressDialog({
  target,
  busy,
  onCancel,
  onConfirm,
}: {
  target: { iface: string; address: string } | null
  busy: boolean
  onCancel: () => void
  onConfirm: () => void
}) {
  useLocale()
  return (
    <Dialog open={target !== null} onOpenChange={(open) => !open && onCancel()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('Remove')}{target?.address} from {target?.iface}?
          </DialogTitle>
          <DialogDescription>
            No network and not the uplink accounts for this address, so nothing in olr will put
            it back. If a network you removed left it behind, this is how it goes. If your
            distribution configured it, it comes back the next time that configuration runs —
            and anything reaching this router at that address stops reaching it now.
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="quiet" onClick={onCancel}>{t('Cancel')}</Button>
          <Button variant="danger" disabled={busy} onClick={onConfirm}> {t('Remove it')} </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
