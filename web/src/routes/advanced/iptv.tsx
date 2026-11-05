import { t, useLocale } from '@/lib/i18n'
import { Heading } from '@/components/ui/heading'
import { AlertTriangle, ArrowRight, Tv } from 'lucide-react'
import { useState } from 'react'
import { Link } from 'react-router'
import { toast } from 'sonner'

import { SubPage } from '@/components/layout/sub-page'
import { StatusStrip } from '@/components/layout/status-strip'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Disclosure } from '@/components/ui/disclosure'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { Skeleton } from '@/components/ui/skeleton'
import { useInterfaces } from '@/features/link/queries'
import {
  useIptvApply,
  useIptvConfig,
  useIptvPlan,
  useIptvStatus,
  type IptvApplyResult,
  type IptvConfig,
  type IptvPlan,
} from '@/features/iptv/queries'
import { ApiError } from '@/lib/api'

export function IptvPage() {
  useLocale()
  const config = useIptvConfig()
  const status = useIptvStatus()
  const links = useInterfaces()
  const plan = useIptvPlan()
  const apply = useIptvApply()
  const [edited, setEdited] = useState<IptvConfig | null>(null)
  const [sourceEdit, setSourceEdit] = useState<string | null>(null)
  const [preview, setPreview] = useState<{ config: IptvConfig; plan: IptvPlan } | null>(null)

  const draft = edited ?? config.data
  const sources = sourceEdit ?? (config.data?.sources ?? []).join('\n')

  function edit(next: IptvConfig) {
    setEdited(next)
    setPreview(null)
  }

  async function review(next: IptvConfig) {
    try {
      setPreview({ config: next, plan: await plan.mutateAsync(next) })
    } catch (error) {
      toast.error(error instanceof Error ? error.message : String(error))
    }
  }

  async function save(next: IptvConfig) {
    try {
      const result = await apply.mutateAsync(next)
      if (result.error) throw new Error(result.error.message)
      setEdited(next)
      setSourceEdit((next.sources ?? []).join('\n'))
      setPreview(null)
      toast.success(next.enabled ? 'IPTV forwarding is on' : 'IPTV forwarding is off')
    } catch (error) {
      const partial = error instanceof ApiError ? error.body as IptvApplyResult | undefined : undefined
      toast.error(error instanceof Error ? error.message : String(error), {
        description: partial?.steps?.filter((step) => step.done).map((step) => step.description).join('; ') || undefined,
      })
      setPreview(null)
      void status.refetch()
    }
  }

  if (config.isPending) return <SubPage section="/advanced" slug="iptv"><Skeleton className="h-48 w-full" /></SubPage>
  if (config.isError) return <SubPage section="/advanced" slug="iptv"><Alert variant="destructive"><AlertTriangle /><AlertTitle>{t('Could not load IPTV')}</AlertTitle><AlertDescription>{config.error.message}</AlertDescription></Alert></SubPage>

  if (!draft) return <SubPage section="/advanced" slug="iptv"><Alert variant="destructive">{t('Configuration is unavailable.')}</Alert></SubPage>

  const interfaces = links.data?.interfaces ?? []
  const networks = links.data?.networks ?? []
  const available = interfaces.filter((iface) => iface.adopted && iface.present && iface.address && !iface.network && !iface.loopback)
  const selectedInterface = interfaces.find((iface) => iface.name === draft.upstream)
  const selectedNetworks = draft.networks ?? []
  const ready = !!draft.upstream && selectedNetworks.length > 0 &&
    !!selectedInterface?.adopted && !!selectedInterface.present && !!selectedInterface.address &&
    !selectedInterface.network && selectedNetworks.every((name) => networks.some((network) =>
      network.name === name && network.subnet && network.members.length === 1 &&
      network.members[0] !== draft.upstream &&
      interfaces.some((iface) => iface.name === network.members[0] && iface.present)))
  const next = { ...draft, sources: sources.split(/[\s,]+/).filter(Boolean) }
  const service = status.data?.service

  return (
    <SubPage section="/advanced" slug="iptv">
      <StatusStrip
        headline={status.isPending ? 'Checking IPTV status…' : status.isError ? 'Cannot read IPTV status' : status.data?.enabled ? service?.active ? 'IGMP proxy running' : 'IPTV needs attention' : 'IPTV is off'}
        detail={status.isPending ? 'Waiting for the router.' : status.isError ? status.error.message : status.data?.enabled ? service?.active ? 'The proxy is running; this does not confirm a viewer has joined or that playback works.' : 'The proxy is not running; check the problem below.' : 'Nothing is sent from the IPTV interface to your LAN.'}
        tone={status.isPending ? 'unknown' : status.isError ? 'fault' : status.data?.enabled ? service?.active ? 'ok' : 'warn' : 'off'}
      />
      {links.isError && <Alert variant="destructive"><AlertTriangle /><AlertTitle>{t('Cannot list interfaces')}</AlertTitle><AlertDescription>{links.error.message}</AlertDescription></Alert>}
      {status.data?.problem && <Alert variant="destructive"><AlertTriangle /><AlertTitle>{t('Cannot forward IPTV')}</AlertTitle><AlertDescription>{status.data.problem}</AlertDescription></Alert>}
      {status.data?.enabled && status.data.plan && !status.data.plan.empty && <Alert><AlertTriangle /><AlertTitle>{t('Settings need reapplying')}</AlertTitle><AlertDescription>{status.data.plan.changes?.join(', ')}</AlertDescription></Alert>}
      <Alert><Tv /><AlertTitle>{t('Before you turn this on')}</AlertTitle><AlertDescription>
        Requires igmpproxy, IPv4 forwarding, and an IPTV interface with IPv4 access to the provider's stream. An AP without IGMP snooping may slow Wi-Fi during playback.
      </AlertDescription></Alert>
      <Disclosure summary={t('IPTV scope')}><p className="text-sm text-muted-foreground">Routes IPv4 multicast only. Configure VLANs and provider DHCP or authentication outside this page; it does not bridge the modem to a set-top box.</p></Disclosure>

      <section className="panel-surface space-y-5 p-4 sm:p-6">
        <div className="space-y-1"><Heading>{t('Signal path')}</Heading><p className="text-sm text-muted-foreground">{t('Choose the upstream interface and subscribing LANs.')}</p></div>
        <div className="grid gap-5 sm:grid-cols-2">
          <label className="space-y-2 text-sm font-medium" htmlFor="iptv-upstream">{t('IPTV upstream interface')} <select data-slot="select-trigger" id="iptv-upstream" value={draft.upstream ?? ''} onChange={(event) => edit({ ...draft, upstream: event.target.value })} className="h-9 w-full rounded-md border border-input bg-card px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring">
              <option value="">{t('Choose an interface')}</option>
              {available.map((iface) => <option key={iface.name} value={iface.name}>{iface.name} · {iface.address}</option>)}
              {draft.upstream && !available.some((iface) => iface.name === draft.upstream) && <option value={draft.upstream}>{draft.upstream} {t('· unavailable')}</option>}
            </select>
          </label>
          <div className="space-y-2 text-sm font-medium">{t('Downstream networks')}<div className="space-y-2 rounded-lg border p-3">
              {networks.filter((network) => network.subnet && network.members.length === 1).map((network) => (
                <label key={network.name} className="flex min-h-11 cursor-pointer items-center gap-2 font-normal">
                  <input type="checkbox" checked={selectedNetworks.includes(network.name)} onChange={(event) => edit({ ...draft, networks: event.target.checked ? [...selectedNetworks, network.name] : selectedNetworks.filter((name) => name !== network.name) })} />
                  <span>{network.name} <span className="text-muted-foreground">({network.members[0]})</span></span>
                </label>
              ))}
              {networks.length === 0 && <p className="font-normal text-muted-foreground">{t('No LAN networks yet.')}</p>}
            </div>
          </div>
        </div>
        <label className="block space-y-2 text-sm font-medium" htmlFor="iptv-sources">{t('Additional source IPv4 prefixes')}<span className="font-normal text-muted-foreground">(optional, one per line)</span>
          <Textarea id="iptv-sources" rows={3} value={sources} onChange={(event) => { setSourceEdit(event.target.value); setPreview(null) }} placeholder="10.0.0.0/8" className="font-mono" />
        </label>
        <p className="text-sm text-muted-foreground">Missing your IPTV interface? Set up its VLAN and IPv4 address on the router first, then <Link className="underline underline-offset-4" to="/gateway/interfaces">adopt it under Interfaces <ArrowRight className="inline size-3" /></Link>.</p>
        <div className="flex flex-wrap gap-2 border-t pt-4">
          <Button variant="secondary" disabled={!ready || plan.isPending || apply.isPending} onClick={() => void review({ ...next, enabled: true })}>Preview changes</Button>
          {draft.enabled && <Button variant="secondary" disabled={plan.isPending || apply.isPending} onClick={() => void review({ ...next, enabled: false })}>{t('Turn off')}</Button>}
        </div>
        {!ready && !links.isPending && <p className="text-sm text-muted-foreground">Select an adopted IPTV interface with IPv4 and at least one present IPv4 LAN to continue.</p>}
      </section>
      {preview && <section className="space-y-4 rounded-xl border border-primary/30 bg-primary/5 p-5 sm:p-6">
        <div><Heading>{t('Review before applying')}</Heading><p className="text-sm text-muted-foreground">{preview.plan.empty ? 'No service changes; the selected settings will still be saved.' : `Impact: ${preview.plan.impact}`}</p></div>
        <ul className="list-inside list-disc space-y-1 text-sm">{(preview.plan.changes ?? []).map((change) => <li key={change}>{change}</li>)}</ul>
        <div className="flex gap-2"><Button disabled={apply.isPending} onClick={() => void save(preview.config)}>{preview.config.enabled ? 'Enable IPTV' : 'Turn off IPTV'}</Button><Button variant="quiet" onClick={() => setPreview(null)}>{t('Cancel')}</Button></div>
      </section>}
    </SubPage>
  )
}
