import { t, useLocale } from '@/lib/i18n'
import { AlertTriangle, Plus, ShieldCheck } from 'lucide-react'
import { useState } from 'react'
import { useSearchParams } from 'react-router'
import { useClientList } from '@/features/clients/queries'

import { StatusStrip } from '@/components/layout/status-strip'
import { SubPage } from '@/components/layout/sub-page'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { StatusPill } from '@/components/ui/status'
import { state } from '@/lib/state'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
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
import { CertificateDialog } from '@/features/ingress/certificate-dialog'
import { ImpactBadge, PlanDiff, PlanReasons, impactHint } from '@/features/ingress/impact'
import {
  ingressChange,
  useIngressConfig,
  useIngressStatus,
  useReapplyIngress,
  type IngressChange,
} from '@/features/ingress/queries'
import { NoProxyAlert } from '@/features/ingress/no-proxy'
import { ServiceDialog } from '@/features/ingress/service-dialog'
import { useIngressApply } from '@/features/ingress/use-apply'
import type { IngressStatus } from '@/lib/api-types'
import type { Service } from '@/lib/config-types'

export function CaddyPage() {
  useLocale()
  const config = useIngressConfig()
  const status = useIngressStatus()
  const applier = useIngressApply()
  const reapply = useReapplyIngress()
  const [params, setParams] = useSearchParams()
  const devices = useClientList()
  const deviceMac = params.get('device')
  const device = devices.data?.clients.find((d) => d.mac.toLowerCase() === deviceMac?.toLowerCase())
  // `host` is the other half of the same shortcut, for a target that is not in
  // `devices` at all: a device that dialled in over the tunnel is published by
  // its address (docs/remote-access.md §3.3), so its page can prefill one and
  // has no MAC to prefill the other with.
  const host = params.get('host')?.trim()
  const initialUpstream = device ? device.fixed_ip && device.name
    ? { device: device.mac, port: 0 }
    : { host: device.fixed_ip || device.ips?.find((ip) => !ip.includes(':')) || device.ips?.[0] || '', port: 0 }
    : host ? { host, port: 0 } : undefined

  const [adding, setAdding] = useState(false)
  const [editing, setEditing] = useState<Service | null>(null)
  const [cert, setCert] = useState(false)

  if (config.isPending) return <SubPage section="/advanced" slug="caddy"><PageSkeleton /></SubPage>
  if (config.isError) {
    return (
      <SubPage section="/advanced" slug="caddy"><Alert variant="destructive">
        <AlertTriangle />
        <AlertTitle>{t('Could not load the published services')}</AlertTitle>
        <AlertDescription>{(config.error as Error).message}</AlertDescription>
      </Alert></SubPage>
    )
  }

  const current = config.data
  const services = current.services ?? []
  const domain = status.data?.domain
  const change = (c: IngressChange) => applier.submit(c)

  return (
    <SubPage section="/advanced" slug="caddy">
      {!!status.data?.problems?.length && (
        <Alert>
          <AlertTriangle />
          <AlertTitle>{t('Fix the remaining problems to apply your settings')}</AlertTitle>
          <AlertDescription>
            <p>{t('Repairs can be saved one at a time. The proxy keeps its current configuration until all problems are fixed.')}</p>
            <ul className="list-disc pl-5">
              {status.data.problems.map((problem, i) => {
                const index = /^services\[(\d+)\]/.exec(problem.path ?? '')?.[1]
                const service = index === undefined ? undefined : services[Number(index)]
                return <li key={i}>{service?.name || problem.path}: {problem.message}</li>
              })}
            </ul>
          </AlertDescription>
        </Alert>
      )}
      {applier.failure && (
        <Alert variant="destructive" role="alert">
          <AlertTriangle />
          <AlertTitle>{t('Only part of the change went through')}</AlertTitle>
          <AlertDescription className="space-y-3">
            <p> {t('The steps that finished have already taken effect and will not be undone. Fixing the cause and applying again is safe — it picks up where this left off.')} </p>
            <Disclosure summary={t('Which steps ran')}>
              <ul className="space-y-1 font-mono text-xs">
                {applier.failure.steps?.map((s, i) => (
                  <li key={i}>
                    {s.skipped ? 'skipped' : s.done ? 'done   ' : 'failed '} {s.description}
                    {s.error ? ` — ${s.error}` : ''}
                  </li>
                ))}
              </ul>
            </Disclosure>
            <Button size="sm" variant="secondary" onClick={applier.dismissFailure}> {t('Dismiss')} </Button>
          </AlertDescription>
        </Alert>
      )}

      {/* Ordered by what has to be true first. No proxy means nothing below can
          work, so it comes before the certificate, which comes before the list of
          things the certificate serves. */}
      {status.data?.binary_error && (
        <NoProxyAlert message={status.data.binary_error} binary={status.data.binary} />
      )}

      {status.data?.certificate_warnings?.map((warning) => (
        <Alert key={warning}>
          <AlertTriangle />
          <AlertTitle>{t('Certificate')}</AlertTitle>
          <AlertDescription>{warning}</AlertDescription>
        </Alert>
      ))}

      {status.data?.public_certificate_warnings?.map((warning) => (
        <Alert key={warning}>
          <AlertTriangle />
          <AlertTitle>{t('Public certificate')}</AlertTitle>
          <AlertDescription>{warning}</AlertDescription>
        </Alert>
      ))}

      {status.data?.drifted && (
        <Alert>
          <AlertTriangle />
          <AlertTitle>{t('The router is not doing what these settings say')}</AlertTitle>
          <AlertDescription className="space-y-3">
            <p>
              Something changed the proxy&rsquo;s configuration outside olr. Putting it back reloads
              the proxy without dropping connections.
            </p>
            <Button
              size="sm"
              variant="secondary"
              disabled={reapply.isPending}
              onClick={() => reapply.mutate()}
            > {t('Put it back')} </Button>
          </AlertDescription>
        </Alert>
      )}

      <StatusStrip
        headline={
          status.isError ? 'Cannot read service status' : status.isPending ? 'Checking service status…' : !current.enabled
            ? t('Off')
            : services.length === 0
              ? 'Nothing published yet'
              : `${services.length} configured address${services.length === 1 ? '' : 'es'}`
        }
        detail={
          status.isError ? status.error.message : current.enabled
            ? 'Services require their own authentication.'
            : 'These addresses are saved but switched off, so none of them answers.'
        }
        tone={
          status.isError ? 'fault' : status.isPending ? 'unknown' : !current.enabled
            ? 'off'
            : status.data?.problems?.length
              ? 'warn'
              : status.data?.binary_error
                ? 'fault'
                : 'ok'
        }
        control={{
          id: 'ingress-enabled',
          get label() { return t('Serve these addresses') },
          checked: current.enabled,
          busy: applier.busy,
          onChange: (enabled) => change(ingressChange.settings({ enabled })),
        }}
      />

      <CertificateCard
        status={status.data}
        enabled={current.enabled}
        onEdit={() => setCert(true)}
      />

      <Card>
        <CardHeader>
          <CardTitle>{t('Addresses')}</CardTitle>
          <CardDescription>
            {domain ? (
              <>
                <span className="font-mono">{domain}</span>
              </>
            ) : (
              'Set a domain to publish services.'
            )}
          </CardDescription>
          <CardAction>
            <Button size="sm" onClick={() => setAdding(true)}>
              <Plus /> {t('Add')} </Button>
          </CardAction>
        </CardHeader>
        <CardContent>
          <ServiceList services={services} status={status.data} onEdit={setEditing} />
        </CardContent>
      </Card>

      <ServiceDialog
        key={device?.mac ?? host ?? 'new'}
        open={adding || !!device || !!host}
        initialUpstream={initialUpstream}
        onOpenChange={(open) => {
          setAdding(open)
          // Both shortcuts are spent once the dialog closes: the address is in
          // the draft or it was abandoned, and a reload should not reopen it.
          if (!open && (deviceMac || host)) {
            const next = new URLSearchParams(params)
            next.delete('device')
            next.delete('host')
            setParams(next, { replace: true })
          }
        }}
        domain={domain}
        publicDomain={current.public_domain}
        onSubmit={(service) => change(ingressChange.saveService(service.name, service))}
      />
      {editing && (
        <ServiceDialog
          open
          onOpenChange={(open) => !open && setEditing(null)}
          initial={editing}
          domain={domain}
          publicDomain={current.public_domain}
          // The path carries the name this service has; the body carries what it
          // should become.
          onSubmit={(service) => change(ingressChange.saveService(editing.name, service))}
          onRemove={() => {
            setEditing(null)
            change(ingressChange.removeService(editing.name))
          }}
        />
      )}
      <CertificateDialog
        open={cert}
        onOpenChange={setCert}
        initial={current.certificate}
        domain={domain}
        publicDomain={current.public_domain}
        onSubmit={(fields) => change(ingressChange.settings(fields))}
      />

      <ConfirmDialog applier={applier} />
    </SubPage>
  )
}

/**
 * The certificate, which is the one thing on this screen that goes wrong on its
 * own.
 *
 * Shown whether or not anything is wrong, and that is deliberate: renewal
 * failures are silent for weeks, so a line that only appears once it is too late
 * is a line nobody has learned to read. The number of days is the whole point —
 * it is the only thing on the page that will be different tomorrow.
 */
function CertificateCard({
  status,
  enabled,
  onEdit,
}: {
  status?: IngressStatus
  enabled: boolean
  onEdit: () => void
}) {
  useLocale()
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <ShieldCheck className="size-4 text-muted-foreground" /> {t('Certificate')} {status && <CertState status={status} enabled={enabled} />}
        </CardTitle>
        <CardDescription> {t('Covers all published addresses.')} </CardDescription>
        <CardAction>
          <Button size="sm" variant="secondary" onClick={onEdit}> {t('Settings')} </Button>
        </CardAction>
      </CardHeader>
      {status?.certificate.certificate && (
        <CardContent>
          <p className="text-sm text-muted-foreground"> {t('Issued')} {new Date(status.certificate.certificate.issued_at).toLocaleDateString()}, renews
            itself automatically. Covers{' '}
            <span className="font-mono">{status.certificate.certificate.names.join(', ')}</span>.
          </p>
        </CardContent>
      )}
    </Card>
  )
}

/**
 * Four states, not two, and each points somewhere different.
 *
 * "Not needed yet" is a switched-off module and not a fault. "Waiting" is the
 * normal first minute after enabling — and, if it never changes, a wrong
 * credential. "Renewal overdue" is the one worth a colour: everything is working
 * and has been quietly failing to renew for weeks. Collapsing any of these into
 * the others would send somebody to look in the wrong place.
 */
function CertState({ status, enabled }: { status: IngressStatus; enabled: boolean }) {
  useLocale()
  const cert = status.certificate
  // Expired is broken; overdue is still serving and will stop serving, which is
  // a different colour and a different job for whoever reads it.
  const health = !enabled ? state('Not needed yet')
    : status.certificate_error ? state('Unknown')
      : !cert.found ? state('Waiting')
        : (cert.expires_in_days ?? 0) <= 0 ? state('Expired')
          : cert.renewal_overdue ? state('Renewal overdue')
            : state('Valid for {days} days', { days: cert.expires_in_days ?? 0 })
  return <StatusPill tone={health.tone}>{health.label}</StatusPill>
}

function ServiceList({
  services,
  status,
  onEdit,
}: {
  services: Service[]
  status?: IngressStatus
  onEdit: (service: Service) => void
}) {
  useLocale()
  const devices = useClientList()
  if (services.length === 0) {
    return (
      <ListEmpty> {t('No services published yet. Add a service and its target.')} </ListEmpty>
    )
  }

  const domain = status?.domain

  return (
    <List>
      {services.map((s) => {
        return (
          <ListRow
            key={s.name}
            title={domain ? `${s.name}.${domain}${s.link_path ?? ''}` : s.name}
            subtitle={describeService(s, devices.data?.clients.find((d) => d.mac === s.upstream.client)?.name, status?.public_domain)}
            wrap
            editLabel={`${t('Edit')} ${s.name}`}
            onSelect={() => onEdit(s)}
          />
        )
      })}
    </List>
  )
}

function describeService(s: Service, deviceName?: string, publicDomain?: string): string {
  const where = deviceName || s.upstream.client || s.upstream.host || '?'
  const scheme = s.upstream.scheme === 'https' ? ' over https' : ''
  const exposure =
    s.expose === 'public' ? 'public' : s.expose === 'restricted' ? 'restricted' : ''
  const exposed = exposure && publicDomain ? ` · ${exposure} at ${s.name}.${publicDomain}` : ''
  return `→ ${where}:${s.upstream.port}${scheme}${exposed}`
}

/**
 * The one thing that interrupts an operator (design.md §6.3).
 *
 * Everything else applies on the click. This appears only when the plan came back
 * `disruptive`, which here means an address somebody may have open, bookmarked,
 * or wired into another application is about to stop answering — and unlike a
 * dropped connection, reloading does not fix it.
 */
function ConfirmDialog({ applier }: { applier: ReturnType<typeof useIngressApply> }) {
  useLocale()
  const held = applier.confirming
  if (!held) return null

  return (
    <Dialog open onOpenChange={(open) => !open && applier.cancel()}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2"> {t('Apply this change?')} <ImpactBadge impact={held.plan.impact} />
          </DialogTitle>
          <DialogDescription>{impactHint(held.plan.impact)}</DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <PlanReasons plan={held.plan} />
          <Disclosure summary={t('What would change')}>
            <PlanDiff plan={held.plan} />
          </Disclosure>
        </div>

        <DialogFooter>
          <Button variant="quiet" onClick={applier.cancel}> {t('Cancel')}</Button>
          <Button onClick={applier.confirm} disabled={applier.busy}> {t('Apply anyway')} </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function PageSkeleton() {
  useLocale()
  return (
    <div className="space-y-6">
      <Skeleton className="h-8 w-40" />
      <Skeleton className="h-28 w-full" />
      <Skeleton className="h-28 w-full" />
      <Skeleton className="h-56 w-full" />
    </div>
  )
}
