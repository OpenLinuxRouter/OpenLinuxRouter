import { t, useLocale } from '@/lib/i18n'
import { Heading } from '@/components/ui/heading'
import { Meter, MetricPill } from '@/components/traffic-metrics'
import { StatusInline } from '@/components/ui/status'
import { state } from '@/lib/state'
import { WorldTraffic } from '@/features/world-traffic/world-traffic'
import { NetworkQuality } from '@/features/network-quality/network-quality'
import { AlertTriangle, CircleAlert, CircleCheck, CircleHelp, ChevronRight, Info, LoaderCircle, Map } from 'lucide-react'
import { useMemo, useState, type MouseEvent } from 'react'
import { Link, useNavigate } from 'react-router'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { useGroupActions } from '@/features/clients/group-actions'
import { useClientList, useClientsConfig } from '@/features/clients/queries'
import { useDhcpConfig, useDhcpStatus } from '@/features/dhcp/queries'
import { useDnsStatus } from '@/features/dns/queries'
import { RELAY_UNIT, serviceOf } from '@/features/dns/units'
import { useIngressConfig, useIngressStatus } from '@/features/ingress/queries'
import { servicesByClient } from '@/features/topology/services'
import { useHostMetrics, usePublicAddresses, type HostMetrics, type PublicAddresses } from '@/features/system/queries'
import 'flag-icons/css/flag-icons.min.css'
import { SiteIcon } from '@/features/gateway/latency-site-icon'
import { useGatewayLatency, useMeasureLatencySite, useGatewayStatus, useGatewayTraffic } from '@/features/gateway/queries'
import { FirstRun } from '@/features/setup/first-run'
import { useRemoteStatus } from '@/features/remote/queries'
import { buildDialIn } from '@/features/topology/dial-in'
import { NetworkMap } from '@/features/topology/network-map'
import { useTrafficView, type TrafficView } from '@/features/topology/traffic'
import type { DhcpStatus, DnsStatus, GatewayLatency, GatewayStatus, GatewayTraffic } from '@/lib/api-types'
import { cn, formatBytes, formatRate } from '@/lib/utils'

/**
 * What the router is doing, on the page you land on.
 *
 * This used to be two cards and a placeholder saying the rest arrived with link
 * and dial — which stayed on screen after three more modules shipped, so an
 * operator's first view under-described their own router and the most urgent
 * fact it held, a way out that had stopped responding, appeared nowhere. The
 * faults below the cards are where that answer lives now.
 *
 * The order is what an operator asks in order: is anything wrong, how much is
 * this network doing, and what is on it. The answer to the first is the
 * first card; faults follow the cards in full. Then the map, which is both the
 * only place three modules' answers are joined into one picture and the only
 * place devices are listed — including the ones that dialed in over the
 * tunnel, which are on no network this box serves and are drawn as what they
 * are rather than as device rows (docs/remote-access.md §3.3).
 *
 * The map has no toolbar. A search box, a network filter, a density switch and
 * a New group button sat between the counters and the picture on every visit,
 * for things done now and then; the picture chooses its own density, a new
 * group is made from any device's menu, and the other two have their own pages.
 *
 * The tree is organised by the operator's own groups, and is where they are
 * made, renamed and removed: a group only changes this picture, so the picture
 * is the place to change it, and a separate Groups page would be a screen whose
 * only effect is on a different screen.
 *
 * One device surface, deliberately. A flat list under the tree would have shown
 * the same devices twice on one screen — the exact duplication that took the
 * connected-devices card off the DHCP page — so the tree carries the search box
 * and opens the detail sheet itself.
 *
 * It polls more endpoints than any other screen. That is the cost of being the
 * one screen that saves you visiting the other three.
 */
export function OverviewPage() {
  useLocale()
  const devices = useClientList()
  const dhcp = useDhcpStatus()
  const dhcpConfig = useDhcpConfig()
  const dns = useDnsStatus()
  const gateway = useGatewayStatus()
  // Only the live rates need a fast sample; the other observed data stays at 5s.
  const traffic = useGatewayTraffic(1000)
  const latency = useGatewayLatency()
  const identity = useClientsConfig()
  const ingress = useIngressConfig()
  const ingressStatus = useIngressStatus()
  const services = useMemo(() => servicesByClient(ingress.data, devices.data?.clients ?? []), [ingress.data, devices.data])
  const flows = useTrafficView(traffic.data, traffic.isError)
  const host = useHostMetrics()
  const publicAddresses = usePublicAddresses()
  // The tunnel's own list, not a second reading of the kernel: a peer is not a
  // device and does not belong in `devices` (docs/remote-access.md §3.3), so the
  // map takes it from the module that owns it.
  const remote = useRemoteStatus()
  const dialIn = useMemo(() => buildDialIn(remote.data), [remote.data])
  const navigate = useNavigate()
  const groupActions = useGroupActions()

  const faults = collectFaults(dhcp.data, dns.data, gateway.data)
  // Only a verdict once every module that can raise a fault has answered:
  // "running smoothly" said before DNS has reported is a guess. And none on a
  // box where nothing is switched on — there are no faults because nothing is
  // running, which is what the first-run panel is there to say.
  const known = Boolean(dhcp.data && dns.data && gateway.data)
  // A box whose only job is letting devices in is doing something, even though
  // it hands out no addresses, answers no names and routes nothing: the tunnel
  // is the point, and its devices are on the map below.
  const idle = known && !dhcp.data!.enabled && !dns.data!.enabled && !gateway.data!.enabled && !remote.data?.enabled

  return (
    <div className="space-y-6">
      {/* Above everything, on exactly the boxes where all three are empty. It
          renders nothing once the router is doing something. */}
      <FirstRun />

      <Stats flows={flows} host={host.isError ? undefined : host.data} publicAddresses={publicAddresses.data}
        modules={[
          { name: 'DHCP', to: '/gateway/dhcp', data: dhcp.data, failed: dhcp.isError, observed: Boolean(dhcp.data?.service) && !dhcp.data?.service_error },
          { name: 'DNS', to: '/gateway/dns', data: dns.data, failed: dns.isError, observed: Boolean(serviceOf(dns.data, RELAY_UNIT)?.status) },
          { get name() { return t('Gateway') }, to: '/gateway', data: gateway.data, failed: gateway.isError, observed: Boolean(gateway.data?.known) },
        ]}
        faults={faults} known={known} idle={idle} failed={dhcp.isError || dns.isError || gateway.isError}
        latency={latency.data} latencyFailed={latency.isError} trafficFailed={traffic.isError} />

      {faults.map((fault) => (
        <Alert key={fault.key} variant={fault.tone === 'bad' ? 'destructive' : 'default'}>
          <AlertTriangle />
          <AlertTitle>{fault.title}</AlertTitle>
          <AlertDescription className="space-y-2">
            <p>{fault.detail}</p>
            {fault.to && (
              <Link
                to={fault.to}
                className="inline-flex items-center gap-1 text-sm font-medium underline underline-offset-4"
              >
                {fault.action}
                <ChevronRight className="size-3.5" aria-hidden />
              </Link>
            )}
          </AlertDescription>
        </Alert>
      ))}

      <section aria-label={t('Your network')} className="space-y-4 pt-2">
        <NetworkMap
          devices={devices.data?.clients ?? []}
          services={services}
          serviceDomain={ingressStatus.data?.domain}
          serviceDomainKnown={!ingressStatus.isPending}
          groups={identity.data?.groups}
          traffic={flows}
          assignments={gateway.data?.assignments}
          exits={gateway.data?.exits}
          dialIn={dialIn}
          pools={dhcpConfig.data?.pools}
          pending={devices.isPending}
          density="auto"
          onSelect={(device) => navigate(`/clients/${device.mac}`)}
          onOpenDialIn={(peer) => navigate(`/clients/wireguard/${encodeURIComponent(peer.name)}`)}
          onCreateGroup={groupActions.create}
          onRenameGroup={groupActions.rename}
          onDeleteGroup={groupActions.remove}
          onMoveGroup={groupActions.moveGroup}
        />

        <TrafficNote traffic={traffic.data} failed={traffic.isError} flows={flows} />
      </section>

      {groupActions.dialogs}
    </div>
  )
}

/* -------------------------------------------------------------------------- */

interface Fault {
  key: string
  title: string
  detail: string
  tone: 'bad' | 'warn'
  to?: string
  action?: string
}

/**
 * Everything wrong right now.
 *
 * Each one names the consequence rather than the mechanism. An operator reading
 * "Office VPN is not responding" still has to be told that work0 is cut off by
 * it, because that is the part they will act on.
 */
function collectFaults(dhcp?: DhcpStatus, dns?: DnsStatus, gateway?: GatewayStatus): Fault[] {
  const out: Fault[] = []

  for (const exit of gateway?.exits ?? []) {
    // Never probed is not the same as up (§5.6), and an exit nobody checks must
    // not be reported as broken either.
    if (!exit.probed || exit.up) continue
    const affected = exit.used_by ?? []
    out.push({
      key: `exit-${exit.name}`,
      title: `${exit.name} is not responding`,
      detail: affected.length
        ? `${affected.join(', ')} ${affected.length === 1 ? 'goes' : 'go'} out this way, so traffic from ${affected.length === 1 ? 'it' : 'them'} is not getting through.`
        : 'Nothing is using it at the moment, so nothing is cut off yet.',
      tone: affected.length ? 'bad' : 'warn',
      to: '/gateway',
      action: 'Open Gateway',
    })
  }

  if (gateway && !gateway.known) {
    out.push({
      key: 'gateway-unknown',
      get title() { return t('The gateway settings are saved but not in force') },
      detail:
        'Nothing on the gateway screen is actually running. On Linux this usually means the daemon lacks permission to change routing.',
      tone: 'bad',
      to: '/gateway',
      action: 'Open Gateway',
    })
  }

  const relay = dns ? serviceOf(dns, RELAY_UNIT) : undefined
  if (dns?.enabled && relay?.status && !relay.status.active) {
    out.push({
      key: 'dns-down',
      get title() { return t('Nothing is answering DNS') },
      detail:
        'DNS is on and the server is stopped. To the people using this network it looks like the internet is down.',
      tone: 'bad',
      to: '/gateway/dns',
      action: 'Open DNS',
    })
  }

  if (dhcp?.enabled && dhcp.service && !dhcp.service.active) {
    out.push({
      key: 'dhcp-down',
      get title() { return t('Addresses are not being handed out') },
      detail:
        'DHCP is on and the server is stopped. Clients here keep their address until it expires; anything joining now gets none.',
      tone: 'bad',
      to: '/gateway/dhcp',
      action: 'Open DHCP',
    })
  }

  // A unit that runs but is not enabled costs nothing until the power goes out,
  // and then costs the whole network at once. Worth saying while it is cheap.
  //
  // `active` is half the condition and was missing: a unit that is stopped and
  // not enabled is not this warning at all. The dns-down card above already
  // says nothing is answering, and following it with "is running, but is not
  // set to start at boot" contradicted it on the same screen — which is what a
  // brand-new install saw, since nothing is enabled or running until the first
  // successful apply.
  for (const service of dns?.services ?? []) {
    if (!service.status || service.status.enabled || !service.status.active) continue
    out.push({
      key: `boot-${service.unit}`,
      get title() { return t('DNS will not come back after a reboot') },
      detail: `${service.unit} is running, but is not set to start at boot.`,
      tone: 'warn',
      to: '/gateway/dns',
      action: 'Open DNS',
    })
  }

  // Settings that no longer validate, which the drift check below leaves out
  // because it cannot run against them. A different and worse state than
  // drift: the backend keeps the last settings that did validate, and nothing
  // saved takes effect until what broke them is fixed. StuckSettings has the
  // rest, and the box that had it.
  const stuck: { name: string; to: string; error?: string }[] = [
    { name: 'DHCP', to: '/gateway/dhcp', error: dhcp?.enabled ? dhcp.drift_error : undefined },
    { name: 'DNS', to: '/gateway/dns', error: dns?.enabled ? dns.drift_error : undefined },
  ]
  for (const s of stuck) {
    if (!s.error) continue
    out.push({
      key: `stuck-${s.name}`,
      title: `${s.name} is running on settings it can no longer apply`,
      detail: 'Something those settings depend on was removed or changed. Its page says what.',
      tone: 'warn',
      to: s.to,
      action: 'Take a look',
    })
  }

  // Only for a module that is switched on, and that is not a way of hiding
  // drift. A disabled module's rendered files differing from its intent has no
  // consequence — nothing is running, and enabling rewrites them on the way —
  // whereas on a box that has never applied anything it is *always* true, which
  // is how a brand-new install came to greet its owner with two warnings that
  // something had changed the configuration behind their back. Nothing had.
  // The section's own page still shows it, where the claim is narrower.
  const drifted: { name: string; to: string }[] = []
  if (dhcp?.enabled && dhcp.drifted && !dhcp.drift_error) drifted.push({ name: 'DHCP', to: '/gateway/dhcp' })
  if (dns?.enabled && dns.drifted && !dns.drift_error) drifted.push({ name: 'DNS', to: '/gateway/dns' })
  if (gateway?.drifted) drifted.push({ name: 'The gateway', to: '/gateway' })
  for (const d of drifted) {
    out.push({
      key: `drift-${d.name}`,
      title: `${d.name} is not doing what its settings say`,
      // Not "something changed it outside olr", which this used to assert and
      // which is only one of the two ways a module gets here. The other is a
      // backend that is enabled and was never started — nothing changed
      // anything, there is no file to put back, and the old advice sent the
      // operator to a screen with no change left to save.
      detail: 'Some of what you have set is not in force yet. That screen has a button to run it.',
      tone: 'warn',
      to: d.to,
      action: 'Take a look',
    })
  }

  return out
}

/* -------------------------------------------------------------------------- */

function uptime(seconds: number): string {
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  return `${days}d${hours}h${minutes}m`
}

// Limits are personal display settings; no router configuration is changed.
const LIMITS_KEY = 'olr-overview-bandwidth-limits'

function readLimits(): { down: number; up: number } {
  try {
    const saved = JSON.parse(localStorage.getItem(LIMITS_KEY) ?? '{}')
    return {
      down: Number.isFinite(saved.down) && saved.down > 0 ? saved.down : 0,
      up: Number.isFinite(saved.up) && saved.up > 0 ? saved.up : 0,
    }
  } catch { return { down: 0, up: 0 } }
}

type Direction = 'down' | 'up'

function BandwidthLimitDialog({ direction, limits, onClose, onSave }: {
  direction: Direction | null
  limits: { down: number; up: number }
  onClose: () => void
  onSave: (limits: { down: number; up: number }) => void
}) {
  useLocale()
  const [value, setValue] = useState(direction && limits[direction] ? String(limits[direction]) : '')
  const valid = value.trim() === '' || (Number.isFinite(Number(value)) && Number(value) > 0)
  return <Dialog open={direction !== null} onOpenChange={(open) => { if (!open) onClose() }}>
    <DialogContent>
      <DialogHeader><DialogTitle>{direction === 'down' ? t('Download') : t('Upload')} limit</DialogTitle>
        <DialogDescription>{t('Enter your plan speed to see utilization. Saved in this browser only.')}</DialogDescription></DialogHeader>
      <form onSubmit={(event) => {
        event.preventDefault()
        if (!direction || !valid) return
        onSave({ ...limits, [direction]: Number(value) })
        onClose()
      }} className="space-y-4">
        <div className="space-y-2"><Label htmlFor="bandwidth-limit">{t('Limit (Mbps)')}</Label>
          <Input id="bandwidth-limit" autoFocus type="number" min="0.001" step="any" value={value} onChange={(event) => setValue(event.target.value)} />
        </div>
        <DialogFooter><Button type="submit" disabled={!valid}>{t('Save')}</Button></DialogFooter>
      </form>
    </DialogContent>
  </Dialog>
}


function StatCard({ title, children, className, action, back, flipped = false }: { title: React.ReactNode; children: React.ReactNode; className?: string; action?: React.ReactNode; back?: React.ReactNode; flipped?: boolean }) {
  useLocale()
  if (back) {
    const heading = <div className="flex h-6 shrink-0 items-center justify-between"><Heading size="sm">{title}</Heading>{action}</div>
    const face = 'col-start-1 row-start-1 flex min-w-0 flex-col gap-2 panel-surface p-3 sm:p-4 [backface-visibility:hidden] [-webkit-backface-visibility:hidden]'
    return <section aria-label={t('Traffic')} className="min-w-0 [perspective:1200px]">
      <div className={cn('grid min-h-60 w-full transition-transform duration-600 ease-[cubic-bezier(0.22,1,0.36,1)] [transform-style:preserve-3d] lg:aspect-[4/3] motion-reduce:transition-none', flipped && '[transform:rotateY(180deg)]')}>
        <div inert={flipped} aria-hidden={flipped} className={cn(face, flipped && 'pointer-events-none')}>{heading}<div className="flex-1 space-y-2 [&_button]:flex-nowrap [&_button]:whitespace-nowrap">{children}</div></div>
        <div inert={!flipped} aria-hidden={!flipped} className={cn(face, '[transform:rotateY(180deg)]', !flipped && 'pointer-events-none')}>{heading}<div className="-mx-1.5 -mb-1.5 flex min-h-40 flex-1 sm:-mx-2 sm:-mb-2">{back}</div></div>
      </div>
    </section>
  }
  return <section className={cn('flex w-full min-w-0 flex-col gap-2 panel-surface p-3 sm:p-4 lg:min-h-60 lg:aspect-[4/3] lg:h-fit lg:has-[details[open]]:aspect-auto lg:has-[[aria-expanded=true]]:aspect-auto', className)}>
    <div className="flex h-6 shrink-0 items-center justify-between"><Heading size="sm">{title}</Heading>{action}</div>{children}
  </section>
}


function CountryIcon({ code }: { code?: string }) {
  useLocale()
  if (!code || !/^[a-z]{2}$/i.test(code)) return null
  return <span role="img" aria-label={code.toUpperCase()} className={`fi fi-${code.toLowerCase()} shrink-0 rounded-xs`} />
}

function StatusDetail({ title, summary, children }: { title: string; summary: React.ReactNode; children: React.ReactNode }) {
  useLocale()
  const [open, setOpen] = useState(false)
  return <>
    <button type="button" aria-haspopup="dialog" onClick={() => setOpen(true)} className="flex min-h-11 w-full items-center gap-2 rounded-sm text-left text-sm font-semibold tabular-nums hover:bg-muted focus-visible:outline-2 focus-visible:outline-ring sm:min-h-9">{summary}</button>
    <Dialog open={open} onOpenChange={setOpen}><DialogContent className="max-h-[85dvh] overflow-y-auto">
      <DialogHeader><DialogTitle>{title}</DialogTitle></DialogHeader>
      {children}
    </DialogContent></Dialog>
  </>
}

function PublicAddressRow({ addresses }: { addresses?: PublicAddresses }) {
  useLocale()
  const [open, setOpen] = useState(false)
  const address = addresses?.ipv4
  const summary = [address?.city || address?.region || address?.country, address?.isp || address?.organization].filter(Boolean).join(', ')
  return <div className="min-w-0">
    <button type="button" aria-haspopup="dialog" onClick={() => setOpen(true)}
      className="flex min-h-11 sm:min-h-9 w-full min-w-0 flex-wrap items-center gap-2 rounded-lg py-1.5 text-left transition-colors hover:bg-muted focus-visible:outline-2 focus-visible:outline-ring">
      <span className="flex w-4 shrink-0 items-center justify-center"><CountryIcon code={address?.country_code} /></span>
      <span className="min-w-0 flex-1 truncate text-[0.8125rem] font-semibold" title={summary || undefined}>{summary || (address?.ip ? 'Location unavailable' : '—')}</span>
    </button>
    <Dialog open={open} onOpenChange={setOpen}><DialogContent className="max-h-[85dvh] overflow-y-auto"><DialogHeader><DialogTitle>{t('Public network')}</DialogTitle><DialogDescription>{summary || 'Location unavailable'}</DialogDescription></DialogHeader><div className="space-y-1 py-2 text-sm text-muted-foreground">
      <dl className="grid grid-cols-[max-content_minmax(0,1fr)] items-start gap-x-2 gap-y-2 [&_dd]:text-right">
        <div className="contents"><dt>IPv4</dt><dd className="break-all font-mono text-foreground">{address?.ip || 'Address unavailable'}</dd></div>
        {addresses?.ipv6?.ip && <div className="contents"><dt>IPv6</dt><dd className="break-all font-mono text-foreground">{addresses.ipv6.ip}</dd></div>}
        {address?.asn != null && address.asn > 0 && <div className="contents"><dt>ASN</dt><dd className="break-all font-mono text-foreground">AS{address.asn}</dd></div>}
        {address?.asn_organization && <div className="contents"><dt>{t('Org')}</dt><dd className="break-words text-foreground">{address.asn_organization}</dd></div>}
      </dl>
    </div></DialogContent></Dialog>
  </div>
}

function formatLatency(ms: number) {
  return ms < 1 ? '<1 ms' : `${ms.toFixed(0)} ms`
}

/** Whether the site has completed an attempt, as opposed to never being probed. */
function hasLatencyAttempt(site: GatewayLatency['custom'][number]) {
  return Boolean(site.checked_at && !site.checked_at.startsWith('0001-'))
}

/** The short outcome the card shows for one monitored site. */
function latencySiteValue(site: GatewayLatency['custom'][number]) {
  if (!hasLatencyAttempt(site)) return '—'
  const ms = site.milliseconds
  if (ms != null) return formatLatency(ms)
  const error = site.error ?? ''
  if (error.startsWith('HTTP ')) return error
  if (error === 'Timed out') return t('Timeout')
  if (error === 'DNS lookup failed') return 'DNS'
  if (error === 'Gateway exit unavailable') return 'Exit'
  return 'Failed'
}

function Stats({ flows, host, publicAddresses, modules, faults, known, idle, failed, latency, latencyFailed, trafficFailed }: {
  flows: TrafficView
  host?: HostMetrics
  publicAddresses?: PublicAddresses
  modules: { name: string; to: string; data?: { enabled: boolean }; failed: boolean; observed: boolean }[]
  faults: Fault[]
  known: boolean
  idle: boolean
  failed: boolean
  latency?: GatewayLatency
  latencyFailed: boolean
  trafficFailed: boolean
}) {
  useLocale()
  const [limits, setLimits] = useState(readLimits)
  const [editing, setEditing] = useState<Direction | null>(null)
  const [showMap, setShowMap] = useState(false)
  const flipTraffic = (event: MouseEvent<HTMLButtonElement>) => {
    const card = event.currentTarget.closest('section')
    setShowMap(value => !value)
    requestAnimationFrame(() => {
      card?.querySelector<HTMLButtonElement>('[aria-hidden="false"] button[aria-pressed]')?.focus({ preventScroll: true })
    })
  }
  const saveLimits = (next: typeof limits) => {
    setLimits(next)
    try { localStorage.setItem(LIMITS_KEY, JSON.stringify(next)) } catch { /* private mode can disable storage */ }
  }
  const title = failed ? t('Status unavailable') : !known ? t('Checking…') : faults.length ? t('Needs attention') : idle ? 'Not set up' : 'All systems OK'
  const measured = !latencyFailed && latency?.state === 'ok' && latency.milliseconds != null
  const dnsMeasured = !latencyFailed && latency?.dns_milliseconds != null
  const latencyValue = measured ? formatLatency(latency.milliseconds!) : latency?.state === 'unreachable' ? 'No response' : '—'
  const measure = useMeasureLatencySite()
  const sites = latency?.custom?.filter((site) => site.pinned) ?? []
  const rate = (direction: 'downRate' | 'upRate') => !trafficFailed && flows.rated ? formatRate(flows.total[direction] ?? 0) : trafficFailed ? t('Unavailable') : '—'
  // Keep four columns for wide screens so values and units fit on one line.
  return <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4 lg:gap-4">
    <StatCard title={t('Status')}>
      <div className="w-full space-y-1">
        <StatusDetail title={t('System')} summary={<>
            {host ? <CircleCheck aria-hidden className="size-4 shrink-0 text-success-foreground" /> : <CircleHelp aria-hidden className="size-4 shrink-0 text-muted-foreground" />}
            <span className="min-w-0 flex-1">{host ? `Up ${uptime(host.uptime_seconds)}` : 'Uptime unavailable'}</span>
          </>}>
          <dl className="grid grid-cols-[max-content_minmax(0,1fr)_max-content_max-content] gap-x-1 gap-y-2 py-2 pl-6 text-xs tabular-nums">
            {[
              { label: 'CPU', used: host?.cpu_used_cores == null ? null : host.cpu_used_cores.toFixed(1), total: host ? `${host.cpu_cores} cores` : null },
              { get label() { return t('Memory') }, used: host ? formatBytes(host.memory_used_bytes) : null, total: host ? formatBytes(host.memory_total_bytes) : null },
              { get label() { return t('Disk') }, used: host?.disk_total_bytes ? formatBytes(host.disk_used_bytes!) : null, total: host?.disk_total_bytes ? formatBytes(host.disk_total_bytes) : null },
            ].map(({ label, used, total }) => <div key={label} className="contents">
              <dt className="text-muted-foreground">{label}</dt>
              <dd className="col-span-3 grid grid-cols-subgrid text-right whitespace-nowrap">
                {used != null ? <><span>{used}</span><span>/</span><span>{total}</span></> : <span className="col-span-3">—</span>}
              </dd>
            </div>)}
          </dl>
        </StatusDetail>
        <StatusDetail title={t('System health')} summary={<>
            {faults.length || failed ? <CircleAlert aria-hidden className="size-4 shrink-0 text-destructive" /> : known && !idle ? <CircleCheck aria-hidden className="size-4 shrink-0 text-success-foreground" /> : <CircleHelp aria-hidden className="size-4 shrink-0 text-muted-foreground" />}
            <span className={cn('min-w-0 flex-1', (faults.length > 0 || failed) && 'text-destructive')}>{title}</span>
          </>}>
          <dl className="space-y-1 py-2 pl-6 text-xs">
            {modules.map((module) => {
              const issues = faults.filter((fault) => fault.to === module.to)
              const health = module.failed ? state('Unavailable') : !module.data ? state('Checking…') : issues.length ? state('Needs attention') : !module.data.enabled ? state('Disabled') : !module.observed ? state('Unavailable') : state('OK')
              return <div key={module.name} className="flex items-baseline justify-between gap-2 py-1">
                <dt><Link to={module.to} className="text-muted-foreground underline-offset-4 hover:underline focus-visible:outline-2 focus-visible:outline-ring">{module.name}</Link></dt>
                <dd className="text-right"><StatusInline tone={health.tone} emphasis="dense">{health.label}</StatusInline></dd>
              </div>
            })}
          </dl>
        </StatusDetail>
        <PublicAddressRow addresses={publicAddresses} />
      </div>
    </StatCard>
    <StatCard title={t('Traffic')} flipped={showMap} back={<WorldTraffic embedded origin={publicAddresses?.ipv4.ip ? publicAddresses.ipv4 : publicAddresses?.ipv6} />} action={<button type="button" onClick={flipTraffic} aria-label={showMap ? t('Show traffic statistics') : t('Show traffic map')} aria-pressed={showMap} title={showMap ? t('Show traffic statistics') : t('Show traffic map')} className={cn('relative inline-flex size-6 items-center justify-center rounded-sm text-muted-foreground before:absolute before:-inset-2.5 transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-2 focus-visible:outline-ring', showMap && 'bg-primary/10 text-primary')}><Map aria-hidden className="size-4" /></button>}>
      <div className="space-y-1">
        <Meter label={t('Download')} arrow="↓" onClick={() => setEditing('down')} current={rate('downRate')} maximum={limits.down ? formatRate(limits.down * 1_000_000 / 8) : undefined}
          percent={flows.rated && !trafficFailed && limits.down ? (flows.total.downRate ?? 0) * 8 / (limits.down * 1_000_000) * 100 : undefined} />
        <Meter label={t('Upload')} arrow="↑" onClick={() => setEditing('up')} current={rate('upRate')} maximum={limits.up ? formatRate(limits.up * 1_000_000 / 8) : undefined}
          percent={flows.rated && !trafficFailed && limits.up ? (flows.total.upRate ?? 0) * 8 / (limits.up * 1_000_000) * 100 : undefined} tone="amber" />
      </div>
      <div className="space-y-1 pt-1">
        <MetricPill label={t('Download')} value={flows.counting && !trafficFailed ? formatBytes(flows.total.down) : trafficFailed ? t('Unavailable') : '—'} valueRight />
        <MetricPill label={t('Upload')} value={flows.counting && !trafficFailed ? formatBytes(flows.total.up) : trafficFailed ? t('Unavailable') : '—'} valueRight />
      </div>
    </StatCard>
    <StatCard title={<Link to="/latency" aria-label={t('Manage latency pins')} className="relative rounded-sm before:absolute before:inset-x-0 before:-inset-y-2.5 hover:text-primary focus-visible:outline-2 focus-visible:outline-ring">{t('Latency')}</Link>} className={sites.length > 3 ? 'lg:aspect-auto' : undefined}>
      <div className="space-y-1">
        <div title={latency?.target ? `Internet latency to ${latency.target}` : undefined}><MetricPill label={t('Internet')} value={latencyValue} valueRight /></div>
        <MetricPill label={t('DNS lookup')} value={dnsMeasured ? formatLatency(latency.dns_milliseconds!) : '—'} valueRight />
      </div>
      {sites.length > 0 && <div className="pt-1">
        <div className="grid grid-cols-2 items-center gap-x-2 gap-y-1.5 sm:grid-cols-3">
          {sites.map((site) => {
            // Only a probe started by clicking this row sets its flag; the
            // once-a-minute background round leaves every row quiet.
            const probing = Boolean(site.measuring)
            const action = probing ? t('Measuring now…') : t('Click to measure again')
            const description = `${site.name} · HTTPS latency to ${new URL(site.url).hostname} via ${site.exit || 'router default'} · ${!hasLatencyAttempt(site) ? 'Waiting for first probe' : site.milliseconds == null ? site.error || 'Failed' : formatLatency(site.milliseconds)}`
            return <button key={site.name} type="button" onClick={() => measure.mutate(site.name)} aria-busy={probing}
              aria-label={`${site.name}: ${latencySiteValue(site)}. ${action}`} title={`${description} · ${action}`}
              className="inline-flex min-h-8 min-w-0 items-center gap-1.5 rounded-sm text-left text-xs font-semibold tabular-nums text-foreground focus-visible:outline-2 focus-visible:outline-ring">
              <SiteIcon name={site.name} url={site.url} icon={site.icon} />
              {probing
                ? <LoaderCircle aria-hidden className="size-3.5 shrink-0 animate-spin text-muted-foreground" />
                : <span className="min-w-0 whitespace-nowrap">{latencySiteValue(site)}</span>}
            </button>
          })}
        </div>
      </div>}
      {measure.isError && <p role="alert" className="pt-1 text-xs text-destructive">{t('Could not measure {name}. Try again.', { name: measure.variables ?? '' })}</p>}
    </StatCard>
    <NetworkQuality />
    <BandwidthLimitDialog key={editing ?? 'closed'} direction={editing} limits={limits} onClose={() => setEditing(null)} onSave={saveLimits} />
  </div>
}

/**
 * Why the lines carry no traffic, when they do not.
 *
 * Only then. With counting on, the caveat about what is measured — traffic
 * through the router, not between two devices on one network — is on the
 * Traffic tile, where the number it qualifies is; a sentence under the map on
 * every visit saying the numbers were fine was read once and then only
 * scrolled past.
 */
function TrafficNote({
  traffic,
  failed,
  flows,
}: {
  traffic?: GatewayTraffic
  failed: boolean
  flows: TrafficView
}) {
  useLocale()
  if ((!traffic && !failed) || flows.counting) return null
  return (
    <p className="flex items-start gap-2 text-xs text-muted-foreground">
      <Info className="mt-px size-3.5 shrink-0" aria-hidden />
      <span>
        {failed
          ? 'Traffic counts could not be read, so the lines do not show how busy each group is.'
          : 'Traffic is not being counted, so the lines do not show how busy each group is.'}{' '}
        <Link to="/gateway/interfaces#gateway-stats" className="font-medium text-foreground underline-offset-4 hover:underline"> {t('Traffic settings')} </Link>
      </span>
    </p>
  )
}
