import { t, useLocale } from '@/lib/i18n'
import { Heading } from '@/components/ui/heading'
import { AlertTriangle, Check, Plus, X } from 'lucide-react'
import { useState } from 'react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
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
import { List, ListEmpty, ListRow } from '@/components/ui/list'
import { Skeleton } from '@/components/ui/skeleton'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { useDhcpConfig } from '@/features/dhcp/queries'
import { UplinkCard } from '@/features/dial/uplink-card'
import { ApplyOutcome, useGatewayEditor } from '@/features/gateway/editor'
import { DIRECT } from '@/features/gateway/network-list'
import { gatewayChange } from '@/features/gateway/queries'
import { InterfacesCard } from '@/features/link/interfaces-card'
import { NetworkDialog } from '@/features/link/network-dialog'
import { useNetworkEditor } from '@/features/link/use-networks'
import type { NetworkRow } from '@/lib/api-types'
import type { Network } from '@/lib/config-types'

/** Interfaces and the router's own way out, independent of served networks. */
export function InterfacesContent() {
  useLocale()
  const dhcp = useDhcpConfig()
  const links = useNetworkEditor()
  const gateway = useGatewayEditor()
  const exits = gateway.config?.exits ?? []

  return (
    <div className="space-y-3">
      <InterfacesCard
        dhcp={dhcp.data}
        physicalOnly
        navigationOnly
        uplinkAction={<span className="text-xs text-muted-foreground">{t('Internet uplink')}</span>}
        footer={gateway.config && (
          <div className="flex flex-wrap items-center gap-3 text-sm">
            <span className="font-medium">{t('Internet via')}</span>
            <Select value={gateway.config.default || DIRECT} disabled={gateway.busy} onValueChange={(v) => gateway.change(gatewayChange.settings({ default: !v || v === DIRECT ? '' : v }))}>
              <SelectTrigger className="w-full sm:w-72" aria-label={t('Default internet route')}><SelectValue>{(v: string) => v === DIRECT ? t('This router’s own connection') : v}</SelectValue></SelectTrigger>
              <SelectContent><SelectItem value={DIRECT}>{t('This router&rsquo;s own connection')}</SelectItem>{exits.map((e) => <SelectItem key={e.name} value={e.name}>{e.name}</SelectItem>)}</SelectContent>
            </Select>
            <UplinkCard compact interfaces={links.interfaces} networks={links.networks} />
            {!gateway.config.enabled && <span className="text-xs text-muted-foreground">{t('Gateway routing is off; this selection is saved only.')}</span>}
          </div>
        )}
      />
      {gateway.gate}
      <ApplyOutcome applier={gateway.applier} />
    </div>
  )
}

/** A served subnet belongs beside the DHCP ranges that use it. */
export function ServedNetworksContent() {
  useLocale()
  const editor = useNetworkEditor()
  const [editing, setEditing] = useState<Network | undefined>(undefined)
  const [open, setOpen] = useState(false)

  if (editor.isPending) return <PageSkeleton />
  if (editor.error) {
    return (
      <Alert variant="destructive">
        <AlertTriangle />
        <AlertTitle>{t('Could not load the networks')}</AlertTitle>
        <AlertDescription>{editor.error.message}</AlertDescription>
      </Alert>
    )
  }

  const stored = editor.config?.networks ?? []
  const taken = new Set(stored.flatMap((n) => n.members))

  function upsert(network: Network) {
    const rest = stored.filter((n) => n.name !== network.name)
    editor.save([...rest, network])
    setOpen(false)
  }

  function remove(name: string) {
    editor.save(stored.filter((n) => n.name !== name))
    setOpen(false)
  }

  return (
    <div className="space-y-4">
      <PartialApply editor={editor} />
      <section id="networks" className="space-y-3 scroll-mt-20">
        <div className="flex items-start justify-between gap-4">
          <div className="space-y-1">
            <Heading as="h3">{t('Served networks')}</Heading>
            <p className="max-w-prose text-sm text-muted-foreground"> {t('Interface and subnet for each served network. Router addresses remain active when DHCP is off.')} </p>
          </div>
          <Button
            size="sm"
            disabled={editor.busy}
            onClick={() => {
              setEditing(undefined)
              setOpen(true)
            }}
          >
            <Plus className="size-4" aria-hidden />{t('Add')}</Button>
        </div>

        {editor.networks.length === 0 ? (
          <ListEmpty> {t('No networks yet. Add a network before configuring its DHCP range.')} </ListEmpty>
        ) : (
          <List>
            {editor.networks.map((n) => (
              <ListRow
                key={n.name}
                title={n.name}
                subtitle={subtitleOf(n)}
                trailing={n.members.join(', ') || 'no interface'}
                editLabel={`${t('Edit')} ${n.name}`}
                onSelect={() => {
                  setEditing(stored.find((s) => s.name === n.name))
                  setOpen(true)
                }}
              />
            ))}
          </List>
        )}
      </section>

      {open && (
        <NetworkDialog
          open={open}
          onOpenChange={setOpen}
          initial={editing}
          interfaces={editor.interfaces}
          taken={taken}
          onSubmit={upsert}
          onRemove={editing ? () => remove(editing.name) : undefined}
        />
      )}

      <ConfirmDisruptive editor={editor} />
    </div>
  )
}

/** A network's subnet, router address, and suggested DHCP range. */
function subtitleOf(n: NetworkRow): string {
  const parts = [n.subnet ? `${n.subnet}, this router at ${n.router}` : 'No IPv4']
  if (n.suggested_start) parts.push(`range ${n.suggested_start}–${n.suggested_end}`)
  if (n.subnet6) parts.push(`${n.subnet6}, this router at ${n.router6}`)
  else if (n.delegated !== undefined) parts.push(`IPv6 /64 number ${n.delegated} — no prefix delegated yet`)
  return parts.join(' · ')
}

/**
 * The confirmation a disruptive network change stops at.
 *
 * Every other module applies instantly and only stops for `disruptive`. This
 * one stops harder than the rest: §5.5's lockout guard is not built, so nothing
 * reverts a change that takes the operator's own route to this box away. The
 * server attaches a warning naming the interface and address when it can see
 * that is what is about to happen, and it is shown here verbatim.
 */
export function ConfirmDisruptive({ editor }: { editor: ReturnType<typeof useNetworkEditor> }) {
  useLocale()
  const pending = editor.pending
  const ipv6Addressing = pending?.plan.changes.some((change) => change.path.endsWith('.ipv6_auto'))
  return (
    <Dialog open={pending !== null} onOpenChange={(o) => !o && editor.cancel()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{ipv6Addressing ? 'Change automatic IPv6 addressing?' : 'This drops devices from the network'}</DialogTitle>
          <DialogDescription>
            {ipv6Addressing
              ? 'Changing how this interface obtains IPv6 addresses can interrupt IPv6 access to this router. Static addresses and IPv4 settings are kept.'
              : 'Every device on this network loses the address it is holding and has to ask for a new one. Clients that are already on keep the old address until they renew.'}
          </DialogDescription>
        </DialogHeader>

        {pending?.plan.warnings?.map((w) => (
          <Alert variant="destructive" key={w.path + w.message}>
            <AlertTriangle />
            <AlertDescription>{w.message}</AlertDescription>
          </Alert>
        ))}

        <Disclosure summary={t('What changes on the box')}>
          <pre className="overflow-x-auto pt-1 font-mono text-xs">
            {pending?.plan.changes.map((c) => c.diff).join('')}
          </pre>
        </Disclosure>

        <DialogFooter>
          <Button variant="quiet" onClick={editor.cancel}>{t('Cancel')}</Button>
          <Button disabled={editor.busy} onClick={editor.confirm}>{t('Apply anyway')}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/** A network change reaches the kernel and can land halfway; §5.2 gives it no
 * rollback, so what did happen has to be reported rather than swallowed. */
export function PartialApply({ editor }: { editor: ReturnType<typeof useNetworkEditor> }) {
  useLocale()
  if (!editor.failure) return null
  return (
    <Alert variant="destructive" role="alert">
      <AlertTriangle />
      <AlertTitle>{t('Only part of the change went through')}</AlertTitle>
      <AlertDescription className="space-y-3">
        <p>{t('The steps that finished have already taken effect and will not be undone. Fixing the cause and applying again is safe — it picks up where this left off.')}</p>
        {editor.failure.error && <p>{editor.failure.error.message}</p>}
        <Disclosure summary={t('Which steps ran')}>
          <ul className="space-y-1 pt-1 font-mono text-xs">
            {editor.failure.steps?.map((step) => (
              <li key={step.description} className="flex gap-2">
                {step.done ? (
                  <Check className="mt-0.5 size-3.5 shrink-0 text-success-foreground" aria-label={t('done')} />
                ) : (
                  <X className="mt-0.5 size-3.5 shrink-0 text-destructive" aria-label={t('failed')} />
                )}
                <span>
                  {step.description}
                  {step.error && <span className="block opacity-80">{step.error}</span>}
                </span>
              </li>
            ))}
          </ul>
        </Disclosure>
        <Button size="sm" variant="secondary" onClick={editor.dismissFailure}>{t('Dismiss')}</Button>
      </AlertDescription>
    </Alert>
  )
}

function PageSkeleton() {
  useLocale()
  return (
    <div className="space-y-6">
      <Skeleton className="h-8 w-40" />
      <Skeleton className="h-24 w-full" />
    </div>
  )
}
