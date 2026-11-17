import { t, useLocale } from '@/lib/i18n'
import { LoaderCircle } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { Link } from 'react-router'

import { StatusStrip } from '@/components/layout/status-strip'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Heading } from '@/components/ui/heading'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { PrimaryMetric } from '@/components/ui/primary-metric'
import { StatusInline } from '@/components/ui/status'
import { useClientList } from '@/features/clients/queries'
import { useDevicesConfig } from '@/features/devices/queries'
import { useDhcpLeases, useDhcpStatus } from '@/features/dhcp/queries'
import { useDnsStatus } from '@/features/dns/queries'
import { RELAY_UNIT, serviceOf } from '@/features/dns/units'
import { SiteIcon } from '@/features/gateway/latency-site-icon'
import { useGatewayLatency, useGatewayStatus, useGatewayTraffic, useMeasureLatencySite } from '@/features/gateway/queries'
import { TrafficSeriesCard } from '@/features/gateway/traffic-series-card'
import { useNetworkQuality } from '@/features/network-quality/queries'
import { scoreText } from '@/features/network-quality/score'
import { useRemoteStatus } from '@/features/remote/queries'
import { FirstRun } from '@/features/setup/first-run'
import { useHostMetrics, usePublicAddresses, type HostMetrics, type PublicAddresses } from '@/features/system/queries'
import { NetworkMapPanel } from '@/features/topology/map-panel'
import { useTrafficView, type TrafficView } from '@/features/topology/traffic'
import type { DhcpStatus, DnsStatus, GatewayLatency, GatewayStatus } from '@/lib/api-types'
import { state, type Tone } from '@/lib/state'
import { formatBytes, formatRate } from '@/lib/utils'
import 'flag-icons/css/flag-icons.min.css'

/**
 * What the router is doing, on the page you land on.
 *
 * Three questions, in the order an operator asks them: is anything wrong, how
 * much is this network doing, and what is on it. The answer to the first is one
 * card — a verdict with its faults under it — then a row of readouts over the
 * day's chart, then the map.
 *
 * The readouts are readouts: one number, one qualifier, nothing to click except
 * the plan speed that makes the qualifier meaningful. Everything that used to
 * sit inside those tiles — the module checklist, the host's memory and disk,
 * the public addresses — moved behind the verdict's disclosure. None of it is
 * what the page is for; it is what you check after the verdict says something
 * is wrong, and a 92px tile that opens a dialog is a panel pretending to be a
 * readout.
 *
 * The order is also why the map is last: it is the one place three modules'
 * answers are joined into one picture, and the only place devices are listed,
 * including the ones that dialed in over the tunnel — they are on no network
 * this box serves and are drawn as what they are rather than as client rows
 * (docs/remote-access.md §3.3).
 *
 * It polls more endpoints than any other screen. That is the cost of being the
 * one screen that saves you visiting the other three.
 */
export function OverviewPage() {
  useLocale()
  const dhcp = useDhcpStatus()
  const dns = useDnsStatus()
  const gateway = useGatewayStatus()
  // Only the live rates need a fast sample; the other observed data stays at 5s.
  const traffic = useGatewayTraffic(1000)
  const latency = useGatewayLatency()
  const flows = useTrafficView(traffic.data, traffic.isError)
  const host = useHostMetrics()
  const publicAddresses = usePublicAddresses()
  // The tunnel's own list, not a second reading of the kernel: a peer is not a
  // device and does not belong in `devices` (docs/remote-access.md §3.3).
  const remote = useRemoteStatus()

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
  const failed = dhcp.isError || dns.isError || gateway.isError

  return (
    <div className="space-y-4">
      {/* Above everything, on exactly the boxes where all three are empty. It
          renders nothing once the router is doing something. */}
      <FirstRun />

      <Verdict
        faults={faults} known={known} idle={idle} failed={failed}
        host={host.isError ? undefined : host.data}
        addresses={publicAddresses.data}
        modules={[
          { name: 'DHCP', to: '/gateway/dhcp', data: dhcp.data, failed: dhcp.isError, observed: Boolean(dhcp.data?.service) && !dhcp.data?.service_error },
          { name: 'DNS', to: '/gateway/dns', data: dns.data, failed: dns.isError, observed: Boolean(serviceOf(dns.data, RELAY_UNIT)?.status) },
          { get name() { return t('Gateway') }, to: '/gateway', data: gateway.data, failed: gateway.isError, observed: Boolean(gateway.data?.known) },
        ]} />

      <Readouts flows={flows} trafficFailed={traffic.isError} latency={latency.data} latencyFailed={latency.isError} />

      <TrafficSeriesCard hours={24} />

      <NetworkPanel />
    </div>
  )
}

/* -------------------------------------------------------------------------- */

/**
 * The verdict, and everything it is a verdict about.
 *
 * This is the same strip the DHCP, DNS and Gateway landing pages use: a dot, a
 * sentence, and one line of context under it. What makes Overview different is
 * only what follows on a bad day — the faults sit inside this card, so the
 * answer to "is anything wrong" never moves and never appears twice. The old
 * page put the verdict in a tile and the faults in a stack below the cards,
 * which meant scrolling past the answer to find the reason for it.
 */
function Verdict({ faults, known, idle, failed, host, addresses, modules }: {
  faults: Fault[]
  known: boolean
  idle: boolean
  failed: boolean
  host?: HostMetrics
  addresses?: PublicAddresses
  modules: { name: string; to: string; data?: { enabled: boolean }; failed: boolean; observed: boolean }[]
}) {
  useLocale()
  const tone: Tone = failed ? 'fault'
    : !known ? 'unknown'
      : faults.some((fault) => fault.tone === 'bad') ? 'fault'
        : faults.length ? 'warn'
          : idle ? 'off'
            : 'ok'
  const headline = failed ? t('Status unavailable')
    : !known ? t('Checking…')
      : faults.length ? t('Needs attention')
        : idle ? t('Not set up')
          : t('All systems OK')

  const place = [addresses?.ipv4?.city || addresses?.ipv4?.region || addresses?.ipv4?.country,
    addresses?.ipv4?.isp || addresses?.ipv4?.organization].filter(Boolean).join(', ')

  return <StatusStrip
    tone={tone}
    headline={headline}
    detail={<span className="inline-flex flex-wrap items-center gap-x-2 gap-y-1">
      <span>{host ? `Up ${uptime(host.uptime_seconds)}` : t('Uptime unavailable')}</span>
      {place && <>
        <span aria-hidden className="text-border">·</span>
        <span className="inline-flex items-center gap-1.5"><CountryIcon code={addresses?.ipv4?.country_code} />{place}</span>
      </>}
    </span>}
    details={<>
      {modules.map((module) => {
        const issues = faults.filter((fault) => fault.to === module.to)
        const health = module.failed ? state('Unavailable')
          : !module.data ? state('Checking…')
            : issues.length ? state('Needs attention')
              : !module.data.enabled ? state('Disabled')
                : !module.observed ? state('Unavailable')
                  : state('OK')
        return <div key={module.name} className="flex items-baseline justify-between gap-2">
          <dt><Link to={module.to} className="underline-offset-4 hover:underline focus-visible:outline-2 focus-visible:outline-ring">{module.name}</Link></dt>
          <dd className="text-right"><StatusInline tone={health.tone} emphasis="dense">{health.label}</StatusInline></dd>
        </div>
      })}
      {host && <>
        <div className="flex justify-between gap-2"><dt>CPU</dt><dd className="tabular-nums">{host.cpu_used_cores == null ? '—' : `${host.cpu_used_cores.toFixed(1)} / ${host.cpu_cores}`}</dd></div>
        <div className="flex justify-between gap-2"><dt>{t('Memory')}</dt><dd className="tabular-nums">{formatBytes(host.memory_used_bytes)} / {formatBytes(host.memory_total_bytes)}</dd></div>
        {host.disk_total_bytes ? <div className="flex justify-between gap-2"><dt>{t('Disk')}</dt><dd className="tabular-nums">{formatBytes(host.disk_used_bytes ?? 0)} / {formatBytes(host.disk_total_bytes)}</dd></div> : null}
      </>}
      {addresses?.ipv4?.ip && <div className="flex justify-between gap-2"><dt>IPv4</dt><dd className="break-all text-right font-mono">{addresses.ipv4.ip}</dd></div>}
      {addresses?.ipv6?.ip && <div className="flex justify-between gap-2"><dt>IPv6</dt><dd className="break-all text-right font-mono">{addresses.ipv6.ip}</dd></div>}
      {addresses?.ipv4?.asn != null && addresses.ipv4.asn > 0 && <div className="flex justify-between gap-2"><dt>ASN</dt><dd className="text-right font-mono">AS{addresses.ipv4.asn}</dd></div>}
      {addresses?.ipv4?.asn_organization && <div className="flex justify-between gap-2"><dt>{t('Org')}</dt><dd className="break-words text-right">{addresses.ipv4.asn_organization}</dd></div>}
    </>}>
    {faults.map((fault) => <p key={fault.key} className="text-sm leading-5 text-muted-foreground">
      {fault.to
        ? <Link to={fault.to} className="font-medium text-foreground underline-offset-4 hover:underline focus-visible:outline-2 focus-visible:outline-ring">{fault.title}</Link>
        : <span className="font-medium text-foreground">{fault.title}</span>}
      {` · ${fault.detail}`}
    </p>)}
  </StatusStrip>
}

/* -------------------------------------------------------------------------- */

/**
 * Six numbers, one row, the same anatomy in each.
 *
 * A readout row only scans if every tile answers the same shape of question in
 * the same place, so the labels sit on one line, the figures on another, and
 * the qualifiers under them. The one tile that is wider than the rest —
 * Internet, which carries the pinned sites — is wider because it holds a second
 * kind of thing, not because its number matters more.
 */
function Readouts({ flows, trafficFailed, latency, latencyFailed }: {
  flows: TrafficView
  trafficFailed: boolean
  latency?: GatewayLatency
  latencyFailed: boolean
}) {
  useLocale()
  const clients = useClientList()
  const quality = useNetworkQuality()
  const measure = useMeasureLatencySite()
  const [limits, setLimits] = useState(readLimits)
  const [editing, setEditing] = useState<Direction | null>(null)

  const online = clients.data ? clients.data.clients.filter((client) => client.online).length : undefined
  const rate = (direction: 'down' | 'up') => {
    if (trafficFailed || !flows.rated) return undefined
    const value = formatRate(flows.total[direction === 'down' ? 'downRate' : 'upRate'] ?? 0)
    return splitUnit(value)
  }
  const measured = !latencyFailed && latency?.state === 'ok' && latency.milliseconds != null
  const dnsMeasured = !latencyFailed && latency?.dns_milliseconds != null
  const score = quality.isError ? undefined : quality.data?.overall.score ?? undefined
  const sites = latency?.custom?.filter((site) => site.pinned) ?? []
  const down = rate('down')
  const up = rate('up')

  return <div className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">
    <Tile label={t('Download')} value={down?.[0]} unit={down?.[1]}
      note={<LimitNote value={limits.down} onClick={() => setEditing('down')} />} />
    <Tile label={t('Upload')} value={up?.[0]} unit={up?.[1]}
      note={<LimitNote value={limits.up} onClick={() => setEditing('up')} />} />
    <Tile label={<Link to="/latency" className="underline-offset-4 hover:text-foreground hover:underline">{t('Internet')}</Link>} className="xl:col-span-2"
      value={measured ? formatLatency(latency.milliseconds!) : latency?.state === 'unreachable' ? t('No response') : undefined}
      note={dnsMeasured ? `${t('DNS lookup')} ${formatLatency(latency.dns_milliseconds!)}` : undefined}>
      {sites.length > 0 && <div className="mt-1.5 flex flex-wrap items-center gap-x-1 gap-y-1">
        {sites.map((site) => {
          // Only a probe started by clicking this row sets its flag; the
          // once-a-minute background round leaves every row quiet.
          const probing = Boolean(site.measuring)
          const hosting = new URL(site.url).hostname
          return <button key={site.name} type="button" onClick={() => measure.mutate(site.name)} aria-busy={probing}
            aria-label={`${site.name}: ${latencySiteValue(site)}. ${probing ? t('Measuring now…') : t('Click to measure again')}`}
            title={`${site.name} · ${hosting} · ${probing ? t('Measuring now…') : t('Click to measure again')}`}
            className="inline-flex min-h-8 min-w-0 items-center gap-1 rounded-sm px-1 text-xs font-medium tabular-nums text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-2 focus-visible:outline-ring">
            <SiteIcon name={site.name} url={site.url} icon={site.icon} />
            {probing ? <LoaderCircle aria-hidden className="size-3.5 shrink-0 animate-spin" /> : <span className="min-w-0 whitespace-nowrap">{latencySiteValue(site)}</span>}
          </button>
        })}
      </div>}
      {measure.isError && <p role="alert" className="mt-1 text-xs text-destructive">{t('Could not measure {name}. Try again.', { name: measure.variables ?? '' })}</p>}
    </Tile>
    <Tile label={t('Quality')} value={score == null ? undefined : scoreText(score)} to="/quality"
      note={score == null ? t('Not tested') : t('of 100')} />
    <Tile label={t('Clients')} value={online} note={t('online')} to="/topology" />

    <BandwidthLimitDialog key={editing ?? 'closed'} direction={editing} limits={limits} onClose={() => setEditing(null)}
      onSave={(next) => {
        setLimits(next)
        try { localStorage.setItem(LIMITS_KEY, JSON.stringify(next)) } catch { /* private mode can disable storage */ }
      }} />
  </div>
}

/**
 * One readout. Everything in it is text; the only controls are the plan speed
 * and, where a tile has a page behind it, the tile itself — a readout that
 * opens the detail is a link, not a panel with a button in it.
 */
function Tile({ label, value, unit, note, to, className, children }: {
  label: ReactNode
  value?: ReactNode
  unit?: string
  note?: ReactNode
  to?: string
  className?: string
  children?: ReactNode
}) {
  const card = <Card size="sm" className="flex h-full min-w-0 flex-col justify-between gap-2 py-3.5 transition-colors group-hover:bg-accent/40 group-focus-visible:bg-accent/40">
    <p className="truncate text-xs leading-4 font-medium text-muted-foreground">{label}</p>
    <div className="min-w-0">
      <PrimaryMetric value={value} unit={unit} note={note} />
      {children}
    </div>
  </Card>
  return <div className={`min-w-0 ${className ?? ''}`}>
    {to
      ? <Link to={to} className="group block h-full rounded-xl focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring">{card}</Link>
      : card}
  </div>
}

/** A rate's number and its unit, so the figure can be the big one. */
function splitUnit(value: string): [string, string?] {
  const at = value.lastIndexOf(' ')
  return at < 0 ? [value] : [value.slice(0, at), value.slice(at + 1)]
}

/**
 * The plan's speed, which is the only thing that makes "1.2 Mbps" mean
 * something. It opens the same dialog the meters did; a rate with no plan to
 * compare it to is a number an operator has to interpret, and this is the one
 * click that fixes that.
 */
function LimitNote({ value, onClick }: { value: number; onClick: () => void }) {
  useLocale()
  return <button type="button" onClick={onClick}
    className="rounded-sm text-start text-xs leading-4 text-muted-foreground underline-offset-4 transition-colors hover:text-foreground hover:underline focus-visible:outline-2 focus-visible:outline-ring">
    {value ? t('of {rate}', { rate: formatRate(value * 1_000_000 / 8) }) : t('Set a plan speed')}
  </button>
}

/* -------------------------------------------------------------------------- */

/**
 * The map, with the counts that describe it.
 *
 * The counts are the picture's legend, not a second summary: each one is a
 * thing the drawing below contains, and each is a link to the page that owns
 * the number. That is why they live in this card's header rather than in a row
 * of their own — a strip of four numbers between the chart and the map made
 * the reader sort out what they were counting.
 */
function NetworkPanel() {
  useLocale()
  const leases = useDhcpLeases()
  const equipment = useDevicesConfig()
  const remote = useRemoteStatus()
  const networks = leases.data?.usage?.length
  const managed = equipment.data?.devices?.length
  const peers = remote.data?.peers?.length

  return <Card className="gap-0 py-0">
    <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1 px-4 pt-3.5">
      <Heading as="h2" size="sm">{t('Your network')}</Heading>
      <p className="text-xs text-muted-foreground tabular-nums">
        <Link to="/gateway/dhcp" className="underline-offset-4 hover:text-foreground hover:underline">{countOf(networks, 'network', 'networks')}</Link>
        <span aria-hidden className="mx-1.5 opacity-45">·</span>
        <Link to="/devices" className="underline-offset-4 hover:text-foreground hover:underline">{countOf(managed, 'device', 'devices')}</Link>
        <span aria-hidden className="mx-1.5 opacity-45">·</span>
        <Link to="/access/wireguard" className="underline-offset-4 hover:text-foreground hover:underline">{countOf(peers, 'tunnel peer', 'tunnel peers')}</Link>
      </p>
      <Link to="/topology" className="ml-auto text-xs text-muted-foreground underline-offset-4 hover:text-foreground hover:underline">
        {t('Open topology')}
      </Link>
    </div>
    <div className="px-2 py-2">
      <NetworkMapPanel density="auto" />
    </div>
  </Card>
}

/** "1 network", "4 devices": the count and the noun inflect together. */
function countOf(count: number | undefined, one: string, many: string): string {
  if (count == null) return '—'
  return t(count === 1 ? `{count} ${one}` : `{count} ${many}`, { count })
}

/* -------------------------------------------------------------------------- */

interface Fault {
  key: string
  title: string
  detail: string
  tone: 'bad' | 'warn'
  to?: string
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

function CountryIcon({ code }: { code?: string }) {
  useLocale()
  if (!code || !/^[a-z]{2}$/i.test(code)) return null
  return <span role="img" aria-label={code.toUpperCase()} className={`fi fi-${code.toLowerCase()} shrink-0 rounded-xs`} />
}

function formatLatency(ms: number) {
  return ms < 1 ? '<1 ms' : `${ms.toFixed(0)} ms`
}

/** Whether the site has completed an attempt, as opposed to never being probed. */
function hasLatencyAttempt(site: GatewayLatency['custom'][number]) {
  return Boolean(site.checked_at && !site.checked_at.startsWith('0001-'))
}

/** The short outcome the tile shows for one pinned site. */
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
