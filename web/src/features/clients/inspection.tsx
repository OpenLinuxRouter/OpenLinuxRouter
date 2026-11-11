import { t, useLocale } from '@/lib/i18n'
import { Heading } from '@/components/ui/heading'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import encodeQR from '@paulmillr/qr'
import { useEffect, useMemo, useState } from 'react'
import { toast } from 'sonner'
import { Settings } from 'lucide-react'
import { Switch } from '@/components/ui/switch'
import { ClientQueries } from '@/features/dns/client-queries'
import { SiteActivity } from '@/features/clients/site-activity'
import { InspectionHistory, InspectionRetention } from '@/features/clients/inspection-history'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { api } from '@/lib/api'

interface Status {
  active: boolean
  /* The switch was restored but the proxy is not running; error says why. */
  pending?: boolean
  error?: string
  history_error?: string
  mac?: string
  ips?: string[]
  ca_present: boolean
  ca_sha256?: string
  redirected_packets?: number
  proxy_accepted: number
  exit_routed: boolean
}

export function ClientInspection({ mac, addresses }: { mac: string; addresses: string[] }) {
  useLocale()
  const client = useQueryClient()
  const ipv4 = addresses.filter((address) => /^\d+\.\d+\.\d+\.\d+$/.test(address))
  const status = useQuery({
    queryKey: ['inspection', 'status'],
    queryFn: () => api.get<Status>('/api/inspection/status'),
    refetchInterval: 2000,
  })
  const statusState = JSON.stringify([status.data?.active, status.data?.pending ?? false, status.data?.mac])
  const [mutationError, setMutationError] = useState<{ message: string; state: string } | null>(null)
  const start = useMutation({
    mutationFn: (addresses?: string[]) => api.post<Status>('/api/inspection/session', { mac, ips: addresses?.length ? addresses : ipv4 }),
    onMutate: () => setMutationError(null),
    onError: (error) => { setMutationError({ message: error.message, state: statusState }); toast.error(String(error)) },
    onSettled: () => client.invalidateQueries({ queryKey: ['inspection'] }),
  })
  const stop = useMutation({
    mutationFn: () => api.send<Status>('DELETE', '/api/inspection/session'),
    onMutate: () => setMutationError(null),
    onError: (error) => { setMutationError({ message: error.message, state: statusState }); toast.error(String(error)) },
    onSettled: () => client.invalidateQueries({ queryKey: ['inspection'] }),
  })
  const busy = start.isPending || stop.isPending
  const [showSetup, setShowSetup] = useState(false)
  const [viewerReady, setViewerReady] = useState(false)
  const [requestView, setRequestView] = useState<'saved' | 'live'>('saved')
  const caURL = `${window.location.origin}/inspection-setup.html`
  const caQR = useMemo(() => {
    try { return encodeQR(caURL, 'svg') } catch { return null }
  }, [caURL])
  // The switch is a setting, not a session: it is on when this device is the
  // one recorded, whether or not the proxy behind it is running.
  const active = Boolean(status.data?.active)
  const pending = Boolean(status.data?.pending)
  const sameDevice = Boolean(status.data?.mac) && status.data?.mac?.toLowerCase() === mac.toLowerCase()
  const on = sameDevice && (active || pending)
  const live = sameDevice && active
  const other = Boolean(status.data?.mac) && !sameDevice && (active || pending)
  const error = status.data?.error || (mutationError?.state === statusState ? mutationError.message : null)
  // A reason the switch cannot be used, in the space the session would fill.
  const notice = status.isError || Boolean(error) || (sameDevice && Boolean(status.data?.history_error)) || other || (on && !active) || (!on && !other && (ipv4.length === 0 || ipv4.length > 8))

  useEffect(() => {
    if (!live) return
    let cancelled = false
    api.post('/api/inspection/viewer-access').then(() => {
      if (!cancelled) setViewerReady(true)
    }).catch((error) => {
      if (!cancelled) toast.error(`Could not open full inspection viewer: ${String(error)}`)
    })
    return () => { cancelled = true; setViewerReady(false) }
  }, [live])

  return <section className="overflow-hidden rounded-xl border bg-card shadow-xs [overflow-anchor:none]" aria-labelledby="inspection-title">
    <div className="border-b px-4 sm:px-6">
      <div className="flex h-18 items-center justify-between gap-3">
        <Heading id="inspection-title">{t('Inspection')}</Heading>
        {/* The title names the switch, so the title bar needs no second
            "Inspection": the gear and the pending note join the switch from the
            left without moving the control the operator came for. */}
        <div className="flex flex-wrap items-center justify-end gap-3">
          {status.isPending && <span className="text-xs text-muted-foreground">{t('Checking…')}</span>}
          <Button variant="quiet" size="icon" aria-label={t('Inspection setup')} title={t('Inspection setup')} onClick={() => setShowSetup(true)}><Settings className="size-5" /></Button>
          <Switch id="device-inspection" aria-labelledby="inspection-title" checked={on} busy={busy} disabled={status.isPending || status.isError || other || (!on && (ipv4.length === 0 || ipv4.length > 8))} onCheckedChange={(checked) => { if (busy) return; if (checked) start.mutate(undefined); else stop.mutate() }} />
        </div>
      </div>
    </div>
    <div>
      <Dialog open={showSetup} onOpenChange={setShowSetup}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t('Inspection setup')}</DialogTitle>
            <DialogDescription>{t('Scan on the client you are inspecting to open the five-step certificate setup guide. Connect it to a network that can reach this router.')}</DialogDescription>
          </DialogHeader>
          {caQR && <div className="mx-auto rounded-md bg-white p-3 [&>svg]:size-56" dangerouslySetInnerHTML={{ __html: caQR }} role="img" aria-label={t('Inspection setup guide QR code')} />}
          <a href={caURL} target="_blank" rel="noreferrer" className="break-all text-sm text-primary underline">{t('Open setup guide')}</a>
          <InspectionRetention />
        </DialogContent>
      </Dialog>
      {notice && <div className="space-y-3 px-4 py-4 sm:px-6 sm:py-6">
        {status.isError ? <p role="alert" className="text-sm text-destructive">{t('Inspection status is unavailable. Do not assume interception has stopped.')}</p> : null}
        {sameDevice && status.data?.history_error && <p role="alert" className="text-sm text-destructive [overflow-wrap:anywhere]">{t('Requests could not be saved.')} {status.data.history_error}</p>}
        {other ? <p className="text-sm text-muted-foreground">{t('Another client is set for inspection. Turn its switch off before starting this one.')}</p> : null}
        {on && !active ? <div className="flex flex-wrap items-center gap-3">
          <p role="alert" className="min-w-0 text-sm text-destructive [overflow-wrap:anywhere]">{error || t('The inspection proxy is not running.')}</p>
          <Button size="sm" variant="secondary" disabled={busy} aria-busy={start.isPending || undefined} onClick={() => { if (!busy) start.mutate(status.data?.ips) }}>{start.isPending ? t('Starting…') : t('Retry')}</Button>
        </div> : error ? <p role="alert" className="min-w-0 text-sm text-destructive [overflow-wrap:anywhere]">{error}</p> : null}
        {!on && !other && ipv4.length === 0 && !active && <p className="text-sm text-muted-foreground">{t('OLR has not observed an IPv4 address for this client yet. Connect it to the network and refresh this page to enable inspection.')}</p>}
        {!on && !other && ipv4.length > 8 && !active && <p className="text-sm text-muted-foreground">{t('Too many addresses are associated with this client to start inspection safely.')}</p>}
      </div>}
      <SiteActivity key={mac} mac={mac} addresses={ipv4} />
      {live && <ClientQueries addresses={ipv4} section />}
      <div className="space-y-3 px-4 pb-4 pt-4 sm:px-6 sm:pb-6">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <h3 className="text-sm font-medium">{t('Requests')}</h3>
          {live && <div className="inline-flex items-center gap-1" role="group" aria-label={t('Request view')}>
            <Button size="sm" variant={requestView === 'saved' ? 'secondary' : 'quiet'} aria-pressed={requestView === 'saved'} onClick={() => setRequestView('saved')}>{t('Saved')}</Button>
            <Button size="sm" variant={requestView === 'live' ? 'secondary' : 'quiet'} aria-pressed={requestView === 'live'} onClick={() => setRequestView('live')}>{t('Live')}</Button>
          </div>}
        </div>
        {(!live || requestView === 'saved') ? <InspectionHistory key={mac} mac={mac} /> : <>
          <p className="text-xs text-muted-foreground">{t('Live viewer keeps the latest 1000 flows in memory. Completed and failed requests are saved separately in history.')}</p>
          {viewerReady
            ? <iframe title={t('Full request and response inspection')} src="/inspection-web/" className="h-[75vh] min-h-[480px] w-full rounded border bg-white" />
            : <div className="text-sm text-muted-foreground">{t('Opening the inspection viewer…')}</div>}
        </>}
      </div>
    </div>
  </section>
}
