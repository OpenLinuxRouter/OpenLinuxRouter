import { t, useLocale } from '@/lib/i18n'
import { Fragment, useEffect, useMemo, useRef, useState } from 'react'
import { useSearchParams } from 'react-router'
import {
  CircleHelp,
  Radar,
  RefreshCw,
  TriangleAlert,
  WifiOff,
} from 'lucide-react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Heading } from '@/components/ui/heading'
import { Skeleton } from '@/components/ui/skeleton'
import { StatusDot, StatusInline, StatusPill } from '@/components/ui/status'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { DeviceIcon } from '@/features/devices/device-icon'
import { useDeviceList } from '@/features/devices/queries'
import type { DeviceRow } from '@/lib/api-types'
import { cn, formatBytes } from '@/lib/utils'

import { useNetworkConfig, useNetworkSnapshot, useRadioScan } from './queries'
import type { Association, Neighbour, NetworkDevice, Radio, ScanResult, Snapshot } from './types'
import {
  bandLabel,
  findingsFor,
  formatDuration,
  formatIdle,
  isPrivateAddress,
  linkTech,
  networkFor,
  occupancyByChannel,
  radioFor,
  scanSummary,
  signalBars,
  signalLevel,
  snr,
  sortClients,
  sortNeighbours,
  standardLabel,
} from './wireless'

type Tab = 'overview' | 'clients' | 'channels'

/**
 * The page a person opens when the Wi-Fi is the suspect.
 *
 * It answers two questions before offering any setting: which radios are up and
 * whether anything is visibly wrong. The client table is the second question,
 * because "the internet is slow" usually means "one device is negotiating at
 * 6 Mbps". Nothing here writes to the access point: a channel change or a scan
 * is a different, more disruptive operation.
 */
export function WirelessPage() {
  useLocale()
  const config = useNetworkConfig()
  const devices = config.data?.devices ?? []
  const directory = useDeviceList().data?.devices
  const byMAC = useMemo(() => new Map((directory ?? []).map(device => [device.mac.toUpperCase(), device])), [directory])
  const [params, setParams] = useSearchParams()
  const preferred = preferredDevice(devices)
  const requested = params.get('device')
  const selected = devices.find(device => device.id === requested) ?? preferred
  const snapshot = useNetworkSnapshot(selected?.id)
  const [tab, setTabState] = useState<Tab>(tabFrom(params.get('tab')))
  const setTab = (next: Tab) => {
    setTabState(next)
    const nextParams: Record<string, string> = {}
    if (selected) nextParams.device = selected.id
    if (next !== 'overview') nextParams.tab = next
    setParams(nextParams, { replace: true })
  }

  if (config.isPending) return <Loading />
  if (config.isError) return <PageError error={config.error} title={t('Could not read network equipment')} />
  if (!devices.length || !selected) return <NoEquipment />

  const identity = snapshot.data?.identity
  const clients = snapshot.data?.associations ?? []
  const observed = snapshot.data?.observed_at
  const heading = selected.name || identity?.model || selected.id

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0 space-y-1">
          <div className="flex flex-wrap items-center gap-2">
            {devices.length > 1 ? (
              <select
                aria-label={t('Access point')}
                value={selected.id}
                onChange={event => setParams({ device: event.target.value, ...(tab !== 'overview' ? { tab } : {}) }, { replace: true })}
                className="h-8 rounded-lg border border-border bg-card px-2 text-sm font-medium text-foreground focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/20"
              >
                {devices.map(device => <option key={device.id} value={device.id}>{device.name || device.id}</option>)}
              </select>
            ) : (
              <h2 className="text-sm font-medium">{heading}</h2>
            )}
            {identity?.model && !identity.model.toLowerCase().includes(heading.toLowerCase()) && (
              <Badge variant="secondary">{identity.model}</Badge>
            )}
          </div>
          <p className="text-sm text-muted-foreground">
            {[identity?.firmware, t('Clients: {count}', { count: clients.length }), observed && t('Updated {time}', { time: clock(observed) })].filter(Boolean).join(' · ')}
          </p>
        </div>
        <Button variant="secondary" size="sm" onClick={() => snapshot.refetch()} disabled={snapshot.isFetching}>
          <RefreshCw className={cn('size-4', snapshot.isFetching && 'animate-spin')} />
          {t('Refresh')}
        </Button>
      </div>

      <WirelessTabs tab={tab} onChange={setTab} clients={clients.length} />

      {snapshot.isPending ? <Loading /> : snapshot.isError ? <PageError error={snapshot.error} title={t('Could not read this access point')} /> : snapshot.data ? (
        <WirelessBody tab={tab} snapshot={snapshot.data} devices={byMAC} onChangeTab={setTab} />
      ) : null}
    </div>
  )
}

function WirelessTabs({ tab, onChange, clients }: { tab: Tab; onChange: (tab: Tab) => void; clients: number }) {
  const items: { id: Tab; label: string; count?: number }[] = [
    { id: 'overview', label: t('Overview') },
    { id: 'clients', label: t('Clients'), count: clients },
    { id: 'channels', label: t('Channels') },
  ]
  return (
    <div role="tablist" aria-label={t('Wireless views')} className="flex gap-1 border-b border-border">
      {items.map(item => (
        <button
          key={item.id}
          role="tab"
          type="button"
          aria-selected={tab === item.id}
          onClick={() => onChange(item.id)}
          className={cn(
            '-mb-px flex items-center gap-1.5 border-b-2 px-3 py-2 text-sm transition-colors duration-150 focus-visible:outline-2 focus-visible:outline-ring',
            tab === item.id
              ? 'border-primary font-medium text-primary'
              : 'border-transparent text-muted-foreground hover:text-foreground',
          )}
        >
          {item.label}
          {item.count != null && (
            <span className={cn('rounded-full px-1.5 text-xs tabular-nums', tab === item.id ? 'bg-primary/10 text-primary' : 'bg-muted text-muted-foreground')}>{item.count}</span>
          )}
        </button>
      ))}
    </div>
  )
}

function WirelessBody({ tab, snapshot, devices, onChangeTab }: {
  tab: Tab
  snapshot: Snapshot
  devices: Map<string, DeviceRow>
  onChangeTab: (tab: Tab) => void
}) {
  const radios = snapshot.radios ?? []
  const collections = snapshot.collections ?? {}
  const radioError = collections.radios
  const clientError = collections.associations
  const findings = findingsFor(snapshot)
  const sorted = useMemo(() => sortClients(snapshot.associations ?? [], snapshot), [snapshot])
  const [expanded, setExpanded] = useState<string | null>(null)
  const opened = useRef(false)

  useEffect(() => {
    if (opened.current || !sorted.length) return
    opened.current = true
    setExpanded(sorted[0].mac)
  }, [sorted])

  if (!radios.length) {
    return (
      <Card>
        <div className="space-y-2 p-6">
          <div className="flex items-center gap-2">
            <WifiOff className="size-4 text-muted-foreground" />
            <Heading as="h2" size="sm">{t('No wireless radios')}</Heading>
          </div>
          <p className="max-w-prose text-sm text-muted-foreground">
            {radioError?.state === 'error' || radioError?.state === 'unsupported'
              ? radioError.error || t('This access point did not report its radios.')
              : t('This device reports no wireless radios.')}
          </p>
        </div>
      </Card>
    )
  }

  return (
    <div className="space-y-6">
      {clientError?.state === 'error' && (
        <Alert variant="destructive">
          <TriangleAlert />
          <AlertTitle>{t('Clients could not be read')}</AlertTitle>
          <AlertDescription>{clientError.error || t('The access point did not report its associated clients.')}</AlertDescription>
        </Alert>
      )}
      {tab === 'overview' ? (
        <>
          <div className="grid gap-4 lg:grid-cols-2">
            {radios.map(radio => <BandCard key={radio.id} radio={radio} snapshot={snapshot} />)}
          </div>
          <FindingsCard findings={findings} />
          <ClientPreview snapshot={snapshot} clients={sorted} devices={devices} onOpen={mac => { setExpanded(mac); onChangeTab('clients') }} onChangeTab={onChangeTab} />
        </>
      ) : tab === 'clients' ? (
        <ClientsCard snapshot={snapshot} clients={sorted} devices={devices} expanded={expanded} onToggle={mac => setExpanded(current => current === mac ? null : mac)} />
      ) : (
        <ChannelsTab snapshot={snapshot} />
      )}
    </div>
  )
}

function BandCard({ radio, snapshot }: { radio: Radio; snapshot: Snapshot }) {
  const network = networkForRadio(radio, snapshot)
  const clients = (snapshot.associations ?? []).filter(client => client.radio_id === radio.id).length
  const standard = standardLabel(radio.protocols)
  const mismatch = radio.width_mhz != null && radio.configured_width_mhz != null && radio.width_mhz < radio.configured_width_mhz
  const enabled = radio.enabled !== false

  return (
    <Card className="gap-0 py-0">
      <div className="space-y-4 p-5">
        <div className="flex items-center justify-between gap-3">
          <div className="flex items-center gap-2">
            <Heading as="h2" size="sm">{bandLabel(radio.band)}</Heading>
            {standard && <Badge variant="secondary">{standard}</Badge>}
          </div>
          <StatusInline tone={enabled ? 'ok' : 'off'}>{enabled ? t('On') : t('Off')}</StatusInline>
        </div>
        <div className="min-w-0">
          <p className="truncate text-xl font-semibold tracking-tight">{network?.ssid || t('No SSID')}</p>
          <p className="mt-1 truncate font-mono text-xs text-muted-foreground">
            {[network?.bssid, network?.interface_id].filter(Boolean).join(' · ') || t('Interface unknown')}
          </p>
        </div>
        <dl className="grid grid-cols-2 gap-x-4 gap-y-4 border-t border-border pt-4 sm:grid-cols-3">
          <Metric label={t('Channel')} value={radio.channel != null ? String(radio.channel) : '—'} />
          <Metric
            label={t('Width')}
            value={radio.width_mhz != null ? t('{width} MHz', { width: radio.width_mhz }) : '—'}
            sub={radio.configured_width_mhz != null ? t('Configured {width} MHz', { width: radio.configured_width_mhz }) : undefined}
            subClassName={mismatch ? 'text-warning-foreground' : undefined}
          />
          <Metric label={t('Transmit power')} value={radio.txpower_dbm != null ? t('{power} dBm', { power: radio.txpower_dbm }) : '—'} sub={radio.country} />
          <Metric label={t('Clients')} value={String(clients)} />
          <Metric label={t('Noise')} value={radio.noise_dbm != null ? t('{noise} dBm', { noise: radio.noise_dbm }) : '—'} />
          <Metric
            label={t('Utilization')}
            value={radio.utilization_percent != null ? t('{percent}%', { percent: Math.round(radio.utilization_percent) }) : '—'}
            sub={radio.utilization_percent != null ? undefined : t('Not reported')}
            bar={radio.utilization_percent}
          />
        </dl>
        {mismatch && (
          <div className="flex items-start gap-2 rounded-lg bg-warning/10 px-3 py-2 text-xs leading-5 text-warning-foreground">
            <TriangleAlert className="mt-0.5 size-3.5 shrink-0" />
            <span>{t('Configured {configured} MHz, running {actual} MHz. The driver may have fallen back.', { configured: radio.configured_width_mhz ?? 0, actual: radio.width_mhz ?? 0 })}</span>
          </div>
        )}
      </div>
    </Card>
  )
}

function Metric({ label, value, sub, subClassName, bar }: {
  label: string
  value: string
  sub?: string
  subClassName?: string
  bar?: number
}) {
  return (
    <div className="min-w-0">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="mt-0.5 truncate text-lg font-semibold leading-7 tabular-nums">{value}</dd>
      {bar != null && (
        <dd className="mt-1.5 h-1 overflow-hidden rounded-full bg-muted" aria-hidden>
          <span className="block h-full rounded-full bg-primary" style={{ width: `${Math.min(100, Math.max(0, bar))}%` }} />
        </dd>
      )}
      {sub && <dd className={cn('truncate text-xs text-muted-foreground', subClassName)}>{sub}</dd>}
    </div>
  )
}

function FindingsCard({ findings }: { findings: ReturnType<typeof findingsFor> }) {
  return (
    <Card className="gap-0 py-0">
      <div className="space-y-3 p-5">
        <div className="flex items-center justify-between gap-3">
          <Heading as="h2" size="sm">{t('Worth a look')}</Heading>
          <span className="text-xs text-muted-foreground">{findings.length ? t('{count} to review', { count: findings.length }) : t('All clear')}</span>
        </div>
        {findings.length ? (
          <ul className="divide-y divide-border">
            {findings.map(finding => (
              <li key={finding.id} className="flex gap-3 py-3 first:pt-0 last:pb-0">
                <StatusPill tone={finding.tone === 'warning' ? 'warn' : 'unknown'} className="mt-0.5 h-5 shrink-0 px-1.5 text-[10px]">
                  {finding.tone === 'warning' ? t('Warning') : t('Notice')}
                </StatusPill>
                <div className="min-w-0">
                  <p className="text-sm font-medium">{t(finding.title, finding.values)}</p>
                  <p className="mt-0.5 text-sm leading-5 text-muted-foreground">{t(finding.detail, finding.values)}</p>
                </div>
              </li>
            ))}
          </ul>
        ) : (
          <p className="flex items-center gap-2 text-sm text-muted-foreground">
            <StatusDot tone="ok" />
            {t('No obvious radio problems in this reading.')}
          </p>
        )}
      </div>
    </Card>
  )
}

/**
 * What a scan adds to a snapshot: which other access points share the air.
 *
 * Nothing runs on its own. A scan moves the radio off its channel for a few
 * seconds, so a person asks for it band by band, and every answer keeps the
 * moment it was heard.
 */
function ChannelsTab({ snapshot }: { snapshot: Snapshot }) {
  const radios = snapshot.radios ?? []
  const scan = useRadioScan(snapshot.device_id)
  const [states, setStates] = useState<Record<string, ScanState>>({})
  const [busy, setBusy] = useState<string | null>(null)
  const [showAll, setShowAll] = useState(false)

  const run = async (radio: Radio) => {
    setBusy(radio.id)
    setStates(current => ({ ...current, [radio.id]: {} }))
    try {
      const result = await scan.mutateAsync(radio.id)
      setStates(current => ({ ...current, [radio.id]: { result } }))
    } catch (error) {
      setStates(current => ({ ...current, [radio.id]: { error: error instanceof Error ? error.message : '' } }))
    } finally {
      setBusy(null)
    }
  }

  // A scan belongs to the device it was taken from; switching devices must not
  // dress another access point's neighbours as this one's.
  const stateFor = (radio: Radio): ScanState | undefined => {
    const state = states[radio.id]
    return state?.result && state.result.device_id !== snapshot.device_id ? undefined : state
  }
  const heard = sortNeighbours(radios.flatMap(radio => {
    const result = stateFor(radio)?.result
    if (!result) return []
    return (result.neighbours ?? []).map(neighbour => ({ ...neighbour, band: radio.band }))
  }))
  const visible = showAll ? heard : heard.slice(0, 12)

  return (
    <div className="space-y-4">
      <div className="grid items-start gap-4 lg:grid-cols-2">
        {radios.map(radio => (
          <ChannelCard
            key={radio.id}
            radio={radio}
            state={stateFor(radio)}
            busy={busy === radio.id}
            onScan={() => run(radio)}
          />
        ))}
      </div>
      {heard.length > 0 && (
        <Card className="gap-0 py-0">
          <div className="space-y-3 p-5">
            <div className="flex items-center justify-between gap-3">
              <Heading as="h2" size="sm">{t('Nearby networks')}</Heading>
              <span className="text-xs text-muted-foreground">
                {showAll ? t('{count} in total', { count: heard.length }) : t('Strongest {shown} of {total}', { shown: visible.length, total: heard.length })}
              </span>
            </div>
            <NeighbourTable rows={visible} />
            {heard.length > visible.length && (
              <Button variant="quiet" size="sm" onClick={() => setShowAll(true)}>{t('Show all')}</Button>
            )}
          </div>
        </Card>
      )}
    </div>
  )
}

interface ScanState {
  result?: ScanResult
  error?: string
}

function ChannelCard({ radio, state, busy, onScan }: { radio: Radio; state?: ScanState; busy: boolean; onScan: () => void }) {
  const result = state?.result
  const summary = scanSummary(result?.neighbours ?? [], radio)
  const occupancy = useMemo(() => occupancyByChannel(result?.neighbours ?? [], radio.band), [result, radio.band])
  const peak = Math.max(1, ...occupancy.map(item => item.count))

  return (
    <Card className="gap-0 py-0">
      <div className="space-y-4 p-5">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="space-y-0.5">
            <div className="flex flex-wrap items-center gap-2">
              <Heading as="h2" size="sm">{bandLabel(radio.band)}</Heading>
              {result && (
                <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
                  <span className="size-2 rounded-[2px] bg-primary" aria-hidden />
                  {t('Your channel')}
                </span>
              )}
            </div>
            <p className="text-sm text-muted-foreground">
              {[radio.channel != null ? t('Channel {channel}', { channel: radio.channel }) : t('Channel unknown'), radio.width_mhz != null ? t('{width} MHz', { width: radio.width_mhz }) : ''].filter(Boolean).join(' · ')}
            </p>
          </div>
          <Button variant="secondary" size="sm" onClick={onScan} disabled={busy}>
            <Radar className={cn('size-4', busy && 'animate-pulse')} />
            {busy ? t('Scanning…') : result ? t('Scan again') : t('Scan for neighbours')}
          </Button>
        </div>

        {!result && !busy && !state?.error && (
          <p className="max-w-prose text-sm leading-5 text-muted-foreground">
            {t('A scan listens on every channel of this band, so the radio leaves its own channel for a few seconds. It runs only when you ask, and the result is a snapshot of that moment.')}
          </p>
        )}
        {busy && (
          <p className="flex items-center gap-2 text-sm text-muted-foreground">
            <RefreshCw className="size-3.5 animate-spin" />
            {t('Listening on every channel. Clients on this band wait until the scan finishes.')}
          </p>
        )}
        {state?.error && (
          <Alert variant="destructive">
            <TriangleAlert />
            <AlertTitle>{t('The scan failed')}</AlertTitle>
            <AlertDescription>{state.error || t('The radio did not answer.')}</AlertDescription>
          </Alert>
        )}

        {result && !busy && (
          <>
            <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-sm">
              <span className="text-muted-foreground">{t('Scanned {time}', { time: clock(result.observed_at) })}</span>
              <span className="tabular-nums">{t('Heard {count} access points', { count: summary.heard })}</span>
              {summary.overlapping > 0 && <StatusInline tone="warn">{t('{count} overlap this channel', { count: summary.overlapping })}</StatusInline>}
            </div>
            {summary.heard ? (
              <>
                <OccupancyStrip items={occupancy} own={radio.channel} peak={peak} />
                <p className="text-xs text-muted-foreground">{t('Bar height counts nearby access points per primary channel, not interference.')}</p>
              </>
            ) : (
              <p className="text-sm text-muted-foreground">{t('No other access point answered on this band.')}</p>
            )}
          </>
        )}
      </div>
    </Card>
  )
}

/** A column per channel, so a busy stretch of the band is visible before any table. */
function OccupancyStrip({ items, own, peak }: { items: { channel: number; count: number }[]; own?: number; peak: number }) {
  return (
    <div role="img" aria-label={t('Access points per channel')}>
      <div className="flex h-24 items-end gap-0.5 border-b border-border">
        {items.map(item => (
          <div
            key={item.channel}
            title={t('Channel {channel}: {count}', { channel: item.channel, count: item.count })}
            className="flex h-full min-w-0 flex-1 items-end"
          >
            <span
              className={cn('w-full rounded-t-[2px]', item.channel === own ? 'bg-primary' : item.count ? 'bg-primary/30' : 'bg-muted')}
              style={{ height: item.count ? `${Math.max(6, (item.count / peak) * 100)}%` : '2px' }}
            />
          </div>
        ))}
      </div>
      <div className="flex gap-0.5 pt-1 text-[10px] leading-4 text-muted-foreground tabular-nums">
        {items.map((item, index) => (
          <span key={item.channel} className="min-w-0 flex-1 text-center">
            {item.channel === own || index % 2 === 0 ? item.channel : ''}
          </span>
        ))}
      </div>
    </div>
  )
}

function NeighbourTable({ rows }: { rows: (Neighbour & { band?: string })[] }) {
  return (
    <div className="overflow-x-auto rounded-lg border border-border">
      <Table>
        <TableHeader>
          <TableRow className="hover:bg-transparent">
            <TableHead className="h-9">{t('Network')}</TableHead>
            <TableHead className="hidden h-9 sm:table-cell">{t('Band')}</TableHead>
            <TableHead className="h-9">{t('Channel')}</TableHead>
            <TableHead className="h-9">{t('Signal')}</TableHead>
            <TableHead className="h-9">{t('Security')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map(row => (
            <TableRow key={`${row.band ?? ''}-${row.bssid ?? row.ssid ?? ''}-${row.channel ?? 0}`}>
              <TableCell>
                <div className="max-w-48 truncate text-sm font-medium">{row.ssid || t('Hidden network')}</div>
                <div className="font-mono text-[11px] text-muted-foreground">{row.bssid ?? '—'}</div>
              </TableCell>
              <TableCell className="hidden sm:table-cell">
                <Badge variant="secondary" className="font-normal">{bandLabel(row.band)}</Badge>
              </TableCell>
              <TableCell>
                <div className="text-sm tabular-nums">{row.channel ?? '—'}</div>
                <div className="text-[11px] text-muted-foreground">{row.width_mhz != null ? t('{width} MHz', { width: row.width_mhz }) : '—'}</div>
              </TableCell>
              <TableCell>
                <div className="flex items-center gap-2">
                  <SignalBars signal={row.signal_dbm} />
                  <span className="text-sm tabular-nums">{signalText(row.signal_dbm)}</span>
                </div>
              </TableCell>
              <TableCell className="text-sm">
                {row.open === true ? t('Open') : row.encryption || '—'}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

function ClientPreview({ snapshot, clients, devices, onOpen, onChangeTab }: {
  snapshot: Snapshot
  clients: Association[]
  devices: Map<string, DeviceRow>
  onOpen: (mac: string) => void
  onChangeTab: (tab: Tab) => void
}) {
  const preview = clients.slice(0, 5)
  return (
    <Card className="gap-0 py-0">
      <div className="space-y-3 p-5">
        <div className="flex items-center justify-between gap-3">
          <div>
            <Heading as="h2" size="sm">{t('Clients')}</Heading>
            <p className="text-xs text-muted-foreground">{t('Sorted by signal, strongest first.')}</p>
          </div>
          <Button variant="quiet" size="sm" onClick={() => onChangeTab('clients')}>{t('View all')}</Button>
        </div>
        <ClientTable snapshot={snapshot} clients={preview} devices={devices} onSelect={onOpen} />
      </div>
    </Card>
  )
}

function ClientsCard({ snapshot, clients, devices, expanded, onToggle }: {
  snapshot: Snapshot
  clients: Association[]
  devices: Map<string, DeviceRow>
  expanded: string | null
  onToggle: (mac: string) => void
}) {
  const weak = clients.filter(client => client.signal_dbm != null && client.signal_dbm <= -75).length
  const fast = clients.filter(client => (client.tx_rate_mbps ?? 0) >= 500).length
  return (
    <Card className="gap-0 py-0">
      <div className="space-y-3 p-5">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <Heading as="h2" size="sm">{t('Associated clients')}</Heading>
            <p className="text-xs text-muted-foreground">{t('Select a row to see its negotiated link.')}</p>
          </div>
          <div className="flex flex-wrap gap-1.5">
            <Badge variant="secondary">{t('{count} online', { count: clients.length })}</Badge>
            {weak > 0 && <Badge variant="warning">{t('{count} weak', { count: weak })}</Badge>}
            {fast > 0 && <Badge variant="secondary">{t('{count} fast', { count: fast })}</Badge>}
          </div>
        </div>
        <ClientTable snapshot={snapshot} clients={clients} devices={devices} expanded={expanded} onToggle={onToggle} />
        <p className="text-xs text-muted-foreground">{t('A dash means the access point did not report that value.')}</p>
      </div>
    </Card>
  )
}

function ClientTable({ snapshot, clients, devices, expanded, onSelect, onToggle }: {
  snapshot: Snapshot
  clients: Association[]
  devices: Map<string, DeviceRow>
  expanded?: string | null
  onSelect?: (mac: string) => void
  onToggle?: (mac: string) => void
}) {
  if (!clients.length) {
    return <p className="py-6 text-center text-sm text-muted-foreground">{t('No clients are associated right now.')}</p>
  }
  return (
    <Table>
      <TableHeader>
        <TableRow className="hover:bg-transparent">
          <TableHead className="h-9">{t('Device')}</TableHead>
          <TableHead className="hidden h-9 sm:table-cell">{t('Band')}</TableHead>
          <TableHead className="h-9">{t('Signal')}</TableHead>
          <TableHead className="h-9">{t('Rate (Mbps)')}</TableHead>
          <TableHead className="hidden h-9 md:table-cell">{t('Connected')}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {clients.map(client => {
          const device = devices.get(client.mac.toUpperCase())
          const radio = radioFor(client, snapshot)
          const network = networkFor(client, snapshot)
          const band = bandLabel(radio?.band)
          const level = signalLevel(client.signal_dbm)
          const open = expanded === client.mac
          const rows = (
            <Fragment key={client.mac}>
              <TableRow
                tabIndex={0}
                aria-expanded={onToggle ? open : undefined}
                className={cn('cursor-pointer focus-visible:outline-2 focus-visible:outline-ring', open && 'bg-muted/40')}
                onClick={() => onSelect ? onSelect(client.mac) : onToggle?.(client.mac)}
                onKeyDown={event => {
                  if (event.key !== 'Enter' && event.key !== ' ') return
                  event.preventDefault()
                  if (onSelect) onSelect(client.mac)
                  else onToggle?.(client.mac)
                }}
              >
                <TableCell>
                  <div className="flex items-center gap-2.5">
                    {device ? (
                      <DeviceIcon icon={device.icon} category={device.category} vendor={device.vendor} vendorKey={device.vendor_key} size="sm" />
                    ) : (
                      <span className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground">
                        <CircleHelp className="size-4" />
                      </span>
                    )}
                    <div className="min-w-0">
                      <div className="flex items-center gap-1.5">
                        <span className="max-w-40 truncate text-sm font-medium">{clientName(client, device)}</span>
                        {level === 'weak' && <Badge variant="destructive" className="h-4 px-1 text-[10px]">{t('Weak')}</Badge>}
                      </div>
                      {clientName(client, device) !== client.mac.toUpperCase() && (
                        <div className="font-mono text-[11px] text-muted-foreground">{client.mac.toUpperCase()}</div>
                      )}
                    </div>
                  </div>
                </TableCell>
                <TableCell className="hidden sm:table-cell">
                  <Badge variant="secondary" className="font-normal">{band}</Badge>
                </TableCell>
                <TableCell>
                  <div className="flex items-center gap-2">
                    <SignalBars signal={client.signal_dbm} />
                    <div>
                      <div className="text-sm tabular-nums">{signalText(client.signal_dbm)}</div>
                      <div className="text-[11px] text-muted-foreground">{snrText(client)}</div>
                    </div>
                  </div>
                </TableCell>
                <TableCell>
                  <div className="flex gap-2 text-sm font-medium tabular-nums">
                    <span className="text-receive">↓ {rate(client.tx_rate_mbps)}</span>
                    <span className="text-transmit">↑ {rate(client.rx_rate_mbps)}</span>
                  </div>
                  <div className="text-[11px] text-muted-foreground">{linkTech(client, radio) || t('Rate unknown')}</div>
                </TableCell>
                <TableCell className="hidden md:table-cell">
                  <div className="text-sm tabular-nums">{formatDuration(client.connected_seconds)}</div>
                  <div className="text-[11px] text-muted-foreground">{t('Idle {value}', { value: formatIdle(client.inactive_ms) })}</div>
                </TableCell>
              </TableRow>
              {onToggle && open && (
                <TableRow className="hover:bg-transparent">
                  <TableCell colSpan={5} className="whitespace-normal bg-muted/20 p-4">
                    <ClientDetail client={client} snapshot={snapshot} network={network} band={band} />
                  </TableCell>
                </TableRow>
              )}
            </Fragment>
          )
          return rows
        })}
      </TableBody>
    </Table>
  )
}

function ClientDetail({ client, snapshot, network, band }: {
  client: Association
  snapshot: Snapshot
  network: ReturnType<typeof networkFor>
  band: string
}) {
  return (
    <div className="grid gap-5 sm:grid-cols-3">
      <section className="space-y-1.5">
        <DetailTitle>{t('Link')}</DetailTitle>
        <DetailRow label={t('Downlink rate')}>{mbps(client.tx_rate_mbps)}</DetailRow>
        <DetailRow label={t('Uplink rate')}>{mbps(client.rx_rate_mbps)}</DetailRow>
        <DetailRow label={t('Expected throughput')}>{mbps(client.expected_mbps)}</DetailRow>
        <DetailRow label={t('Technology')}>{linkTech(client, radioFor(client, snapshot)) || '—'}</DetailRow>
        <DetailRow label={t('Network')}>{network?.ssid || '—'}</DetailRow>
        <DetailRow label={t('Band')}>{band}</DetailRow>
      </section>
      <section className="space-y-1.5">
        <DetailTitle>{t('Air')}</DetailTitle>
        <DetailRow label={t('Signal')}>{signalText(client.signal_dbm)}</DetailRow>
        <DetailRow label={t('Average signal')}>{signalText(client.signal_avg_dbm)}</DetailRow>
        <DetailRow label={t('SNR')}>{snrText(client)}</DetailRow>
        <DetailRow label={t('Retries / failed')}>{countPair(client.retries, client.failed)}</DetailRow>
        <DetailRow label={t('Airtime')}>{airtime(client.tx_airtime_us)}</DetailRow>
        <DetailRow label={t('Idle')}>{formatIdle(client.inactive_ms)}</DetailRow>
      </section>
      <section className="space-y-1.5">
        <DetailTitle>{t('Connection')}</DetailTitle>
        <DetailRow label={t('Connected')}>{formatDuration(client.connected_seconds)}</DetailRow>
        <DetailRow label={t('Bytes (RX / TX)')}>{bytesPair(client.rx_bytes, client.tx_bytes)}</DetailRow>
        <DetailRow label={t('Packets (RX / TX)')}>{countPair(client.rx_packets, client.tx_packets)}</DetailRow>
        <DetailRow label={t('MFP')}>{boolText(client.mfp)}</DetailRow>
        <DetailRow label={t('WMM')}>{boolText(client.wmm)}</DetailRow>
      </section>
    </div>
  )
}

function DetailTitle({ children }: { children: React.ReactNode }) {
  return <h3 className="text-[11px] font-semibold tracking-wide text-muted-foreground uppercase">{children}</h3>
}

function DetailRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-baseline justify-between gap-4 text-sm leading-6">
      <span className="shrink-0 text-muted-foreground">{label}</span>
      <span className="text-right tabular-nums">{children}</span>
    </div>
  )
}

function SignalBars({ signal }: { signal?: number }) {
  const bars = signalBars(signal)
  const weak = signalLevel(signal) === 'weak'
  return (
    <span className="flex h-4 shrink-0 items-end gap-0.5" aria-hidden>
      {[0, 1, 2, 3].map(index => (
        <span
          key={index}
          className={cn('w-1 rounded-[1px]', index < bars ? (weak ? 'bg-destructive' : 'bg-foreground/60') : 'bg-border')}
          style={{ height: `${4 + index * 3}px` }}
        />
      ))}
    </span>
  )
}

function tabFrom(value: string | null): Tab {
  return value === 'clients' || value === 'channels' ? value : 'overview'
}

function preferredDevice(devices: NetworkDevice[]): NetworkDevice | undefined {
  return devices.find(device => device.roles?.includes('ap')) ?? devices.find(device => device.adapter === 'openwrt') ?? devices[0]
}

function networkForRadio(radio: Radio, snapshot: Snapshot) {
  const networks = snapshot.wireless ?? []
  return networks.find(network => network.radio_id === radio.id && network.mode === 'ap') ?? networks.find(network => network.radio_id === radio.id)
}

function clientName(client: Association, device?: DeviceRow): string {
  if (device?.name) return device.name
  return isPrivateAddress(client.mac) ? t('Private address') : client.mac.toUpperCase()
}

function signalText(signal?: number): string {
  return signal == null ? '—' : t('{signal} dBm', { signal })
}

function snrText(client: Association): string {
  const value = snr(client)
  return value == null ? t('SNR unknown') : t('SNR {value} dB', { value })
}

function rate(value?: number): string {
  return value == null ? '—' : value >= 100 ? String(Math.round(value)) : value.toFixed(1)
}

function mbps(value?: number): string {
  return value == null ? '—' : t('{rate} Mbps', { rate: rate(value) })
}

function countPair(first?: number, second?: number): string {
  return first == null && second == null ? '—' : `${first ?? '—'} / ${second ?? '—'}`
}

function bytesPair(rx?: number, tx?: number): string {
  return rx == null && tx == null ? '—' : `${rx == null ? '—' : formatBytes(rx)} / ${tx == null ? '—' : formatBytes(tx)}`
}

function boolText(value?: boolean): string {
  return value == null ? '—' : value ? t('On') : t('Off')
}

function airtime(microseconds?: number): string {
  return microseconds == null ? '—' : `${(microseconds / 1_000_000).toFixed(1)} s`
}

function clock(iso: string): string {
  const date = new Date(iso)
  return Number.isNaN(date.getTime()) ? '—' : date.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
}

function Loading() {
  return (
    <div className="space-y-6">
      <div className="space-y-2">
        <Skeleton className="h-5 w-48" />
        <Skeleton className="h-4 w-72" />
      </div>
      <div className="grid gap-4 lg:grid-cols-2">
        {[0, 1].map(index => (
          <Card key={index} className="gap-0 py-0">
            <div className="space-y-4 p-5">
              <Skeleton className="h-5 w-32" />
              <Skeleton className="h-7 w-56" />
              <Skeleton className="h-24 w-full" />
            </div>
          </Card>
        ))}
      </div>
      <Card className="gap-0 py-0"><div className="space-y-3 p-5"><Skeleton className="h-5 w-40" /><Skeleton className="h-16 w-full" /></div></Card>
    </div>
  )
}

function PageError({ error, title }: { error: unknown; title: string }) {
  return (
    <Alert variant="destructive">
      <TriangleAlert />
      <AlertTitle>{title}</AlertTitle>
      <AlertDescription>{error instanceof Error ? error.message : t('The access point did not answer.')}</AlertDescription>
    </Alert>
  )
}

function NoEquipment() {
  return (
    <Card className="gap-0 py-0">
      <div className="space-y-3 p-6">
        <div className="flex items-center gap-2">
          <WifiOff className="size-4 text-muted-foreground" />
          <Heading as="h2" size="sm">{t('No access point is connected')}</Heading>
        </div>
        <p className="max-w-prose text-sm leading-6 text-muted-foreground">
          {t('Wireless reads a managed access point through the network equipment inventory. Add one from the router shell, then set its password variable in the daemon environment.')}
        </p>
        <pre className="overflow-x-auto rounded-lg border border-border bg-muted/50 p-3 text-xs leading-5">
{`olr network add device living-room \\
  --adapter openwrt --address http://192.168.1.2 \\
  --username root --password-env OLR_AP_PASSWORD`}
        </pre>
      </div>
    </Card>
  )
}
