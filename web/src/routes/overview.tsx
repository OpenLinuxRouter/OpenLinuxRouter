import { t, useLocale } from '@/lib/i18n'
import { Link } from 'react-router'

import { CircleAlert, CircleCheck, CircleDashed, TriangleAlert } from 'lucide-react'
import { StatusInline } from '@/components/ui/status'
import { useDhcpStatus } from '@/features/dhcp/queries'
import { useDnsStatus } from '@/features/dns/queries'
import { RELAY_UNIT, serviceOf } from '@/features/dns/units'
import { useGatewayStatus, useGatewayTraffic } from '@/features/gateway/queries'
import { DnsCard } from '@/features/overview/dns-card'
import { HealthCard } from '@/features/overview/health-card'
import { OverviewSection } from '@/features/overview/card'
import { ClientsTable } from '@/features/overview/clients-table'
import { KeyFigures } from '@/features/overview/key-figures'
import { LiveCard } from '@/features/overview/live-card'
import { NetworkCard } from '@/features/overview/network-card'
import { RhythmCard } from '@/features/overview/rhythm-card'
import { SystemCard } from '@/features/overview/system-card'
import { TrafficCard } from '@/features/overview/traffic-card'
import { useRemoteStatus } from '@/features/remote/queries'
import { FirstRun } from '@/features/setup/first-run'
import { useHostMetrics, usePublicAddresses, type HostMetrics, type PublicAddresses } from '@/features/system/queries'
import { useTrafficView } from '@/features/topology/traffic'
import type { DhcpStatus, DnsStatus, GatewayStatus } from '@/lib/api-types'
import { state, type Tone } from '@/lib/state'
import 'flag-icons/css/flag-icons.min.css'

/**
 * What the router is doing, on the page you land on.
 *
 * The page is a conventional read of the network, in order: is anything wrong,
 * what is happening now, what happened today, when in the week it happens, and
 * what the supporting systems are doing. The two questions that used to be
 * missing — how much (a stacked volume chart) and when (a seven-day rhythm) —
 * are the two charts; everything else is a readout or a small panel.
 *
 * The page is drawn in tiers so that order is also weight: the status band and
 * the live row carry the only large figures, Trends puts its charts first, the
 * Router tier steps back, and the page ends with the clients as one plain
 * table — who is on the network, busiest first — linking on to the topology.
 */
export function OverviewPage() {
  useLocale()
  const dhcp = useDhcpStatus()
  const dns = useDnsStatus()
  const gateway = useGatewayStatus()
  // Only the live rates need a fast sample; the other observed data stays at 5s.
  const traffic = useGatewayTraffic(1000)
  const flows = useTrafficView(traffic.data, traffic.isError)
  const host = useHostMetrics()
  const addresses = usePublicAddresses()
  // The tunnel's own list, not a second reading of the kernel: a peer is not a
  // client and does not belong in `clients` (docs/remote-access.md §3.3).
  const remote = useRemoteStatus()

  const faults = collectFaults(dhcp.data, dns.data, gateway.data)
  // Only a verdict once every module that can raise a fault has answered:
  // "running smoothly" said before DNS has reported is a guess. And none on a
  // box where nothing is switched on — there are no faults because nothing is
  // running, which is what the first-run panel is there to say.
  const known = Boolean(dhcp.data && dns.data && gateway.data)
  // A box whose only job is letting devices in is doing something, even though
  // it hands out no addresses, answers no names and routes nothing: the tunnel
  // is the point.
  const idle = known && !dhcp.data!.enabled && !dns.data!.enabled && !gateway.data!.enabled && !remote.data?.enabled
  const failed = dhcp.isError || dns.isError || gateway.isError

  // Two gaps, not one: 16px between the cards of a tier and 32px between tiers,
  // which is what lets the section headings group rather than merely label.
  return (
    <div className="space-y-8">
      <div className="space-y-4">
        {/* Above everything, on exactly the boxes where all three are empty. It
            renders nothing once the router is doing something. */}
        <FirstRun />

        <StatusBand
          faults={faults} known={known} idle={idle} failed={failed}
          host={host.isError ? undefined : host.data}
          addresses={addresses.data}
          modules={[
            { name: 'DHCP', to: '/gateway/dhcp', data: dhcp.data, failed: dhcp.isError, observed: Boolean(dhcp.data?.service) && !dhcp.data?.service_error },
            { name: 'DNS', to: '/gateway/dns', data: dns.data, failed: dns.isError, observed: Boolean(serviceOf(dns.data, RELAY_UNIT)?.status) },
            { get name() { return t('Gateway') }, to: '/gateway', data: gateway.data, failed: gateway.isError, observed: Boolean(gateway.data?.known) },
          ]} />

        <div className="grid gap-4 lg:grid-cols-12">
          <div className="min-w-0 lg:col-span-8">
            <LiveCard traffic={traffic.data} trafficFailed={traffic.isError} flows={flows} sampledAt={traffic.dataUpdatedAt} />
          </div>
          <div className="min-w-0 lg:col-span-4"><KeyFigures /></div>
        </div>
      </div>

      <OverviewSection title={t('Trends')}>
        <TrafficCard />
        <div className="grid gap-4 lg:grid-cols-12">
          <div className="min-w-0 lg:col-span-6"><RhythmCard /></div>
          <div className="min-w-0 lg:col-span-6"><HealthCard /></div>
        </div>
      </OverviewSection>

      <OverviewSection title={t('Router')}>
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          <SystemCard />
          <DnsCard />
          <div className="min-w-0 md:col-span-2 xl:col-span-1"><NetworkCard /></div>
        </div>
      </OverviewSection>

      <ClientsTable flows={flows} />
    </div>
  )
}

/* -------------------------------------------------------------------------- */

const verdictIcon: Record<Tone, typeof CircleCheck> = {
  ok: CircleCheck,
  warn: TriangleAlert,
  fault: CircleAlert,
  off: CircleDashed,
  unknown: CircleDashed,
}

const verdictTint: Record<Tone, string> = {
  ok: 'bg-success/10 text-success-foreground dark:bg-success/15',
  warn: 'bg-warning/10 text-warning-foreground dark:bg-warning/15',
  fault: 'bg-destructive/10 text-destructive dark:bg-destructive/20',
  off: 'bg-muted text-muted-foreground',
  unknown: 'bg-muted text-muted-foreground',
}

/**
 * The verdict, and the three modules it was drawn from.
 *
 * A dashboard's first line is the answer to "is anything wrong", so it is the
 * largest words on the page, with the facts that make it believable beside it:
 * how long it has been up, where it meets the internet, and the module states,
 * which used to wait behind a disclosure even though they are three words. A
 * fault is listed under the verdict inside the same band, so the answer and its
 * reasons are read as one thing.
 */
function StatusBand({ faults, known, idle, failed, host, addresses, modules }: {
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
  const Icon = verdictIcon[tone]

  const place = [addresses?.ipv4?.city || addresses?.ipv4?.region || addresses?.ipv4?.country,
    addresses?.ipv4?.isp || addresses?.ipv4?.organization].filter(Boolean).join(', ')

  // Quiet while there is nothing to say; tinted, and so the loudest surface on
  // the page, only when there is.
  const band = tone === 'fault' ? 'border-destructive/40 bg-destructive/5 dark:bg-destructive/10'
    : tone === 'warn' ? 'border-warning/50 bg-warning/5 dark:bg-warning/10'
      : ''
  return <section aria-label={t('Status')} className={`panel-surface p-4 sm:p-5 ${band}`}>
    <div className="flex flex-col gap-4 lg:flex-row lg:items-center">
      <div className="flex min-w-0 flex-1 items-center gap-4">
        <span aria-hidden className={`grid size-11 shrink-0 place-items-center rounded-full ${verdictTint[tone]}`}>
          <Icon className="size-5" strokeWidth={2} />
        </span>
        <div className="min-w-0">
          <p className="text-xl leading-7 font-semibold tracking-[-0.01em]">{headline}</p>
          <p className="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-1 text-meta text-muted-foreground">
            <span className="tabular-nums">{host ? t('Up {time}', { time: uptime(host.uptime_seconds) }) : t('Uptime unavailable')}</span>
            {place && <>
              <span aria-hidden className="text-border">·</span>
              <span className="inline-flex items-center gap-1.5"><CountryIcon code={addresses?.ipv4?.country_code} />{place}</span>
            </>}
          </p>
        </div>
      </div>

      <ul className="grid grid-cols-3 gap-2 lg:flex lg:shrink-0">
        {modules.map((module) => {
          const issues = faults.filter((fault) => fault.to === module.to)
          const health = module.failed ? state('Unavailable')
            : !module.data ? state('Checking…')
              : issues.length ? state('Needs attention')
                : !module.data.enabled ? state('Disabled')
                  : !module.observed ? state('Unavailable')
                    : state('OK')
          return <li key={module.name} className="min-w-0">
            <Link to={module.to} aria-label={`${module.name}: ${health.label}`}
              className="flex h-full min-h-11 flex-col justify-center rounded-lg bg-muted/60 px-3 py-1.5 transition-colors hover:bg-muted focus-visible:outline-2 focus-visible:outline-ring lg:min-w-28">
              <span className="truncate text-xs leading-4 text-muted-foreground">{module.name}</span>
              <StatusInline tone={health.tone}>{health.label}</StatusInline>
            </Link>
          </li>
        })}
      </ul>
    </div>

    {/* A fault is a verdict and a consequence, and the two read as a stack
        rather than as one sentence glued with a middot. The measure is
        constrained because a detail running the width of the band is 150
        characters on a single line. */}
    {faults.length > 0 && <ul className="mt-4 space-y-3 border-t border-border pt-4">
      {faults.map((fault) => {
        const FaultIcon = fault.tone === 'bad' ? CircleAlert : TriangleAlert
        return <li key={fault.key} className="flex gap-3">
          <FaultIcon aria-hidden className={`mt-0.5 size-4 shrink-0 ${fault.tone === 'bad' ? 'text-destructive' : 'text-warning-foreground'}`} />
          <div className="max-w-2xl min-w-0 space-y-0.5">
            <div className="text-sm font-medium">
              {fault.to
                ? <Link to={fault.to} className="underline-offset-4 hover:underline focus-visible:outline-2 focus-visible:outline-ring">{fault.title}</Link>
                : fault.title}
            </div>
            <p className="text-meta text-muted-foreground">{fault.detail}</p>
          </div>
        </li>
      })}
    </ul>}
  </section>
}

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
  // saved takes effect until what broke them is fixed.
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
  // consequence — nothing is running, and enabling rewrites them on the way.
  const drifted: { name: string; to: string }[] = []
  if (dhcp?.enabled && dhcp.drifted && !dhcp.drift_error) drifted.push({ name: 'DHCP', to: '/gateway/dhcp' })
  if (dns?.enabled && dns.drifted && !dns.drift_error) drifted.push({ name: 'DNS', to: '/gateway/dns' })
  if (gateway?.drifted) drifted.push({ name: 'The gateway', to: '/gateway' })
  for (const d of drifted) {
    out.push({
      key: `drift-${d.name}`,
      title: `${d.name} is not doing what its settings say`,
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
  return `${days}d ${hours}h ${minutes}m`
}

function CountryIcon({ code }: { code?: string }) {
  if (!code || !/^[a-z]{2}$/i.test(code)) return null
  return <span role="img" aria-label={code.toUpperCase()} className={`fi fi-${code.toLowerCase()} shrink-0 rounded-xs`} />
}
