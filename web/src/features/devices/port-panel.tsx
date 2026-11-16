import { t, useLocale } from '@/lib/i18n'
import { Zap, ZapOff } from 'lucide-react'
import { Fragment, useId, useState } from 'react'
import type { ReactNode } from 'react'

import { Card } from '@/components/ui/card'
import { Heading } from '@/components/ui/heading'
import { StatusDot, StatusInline, StatusPill } from '@/components/ui/status'
import { healthOf, state } from '@/lib/state'
import type { Health, StateWord } from '@/lib/state'
import { cn } from '@/lib/utils'

import { duplexLabel, formatSpeed, sortPorts } from './device'
import { counters } from './device-format'
import type { Port, Snapshot } from './types'

/**
 * The switch as a front panel, above the table it already had.
 *
 * A tile carries exactly what console direction §6.3 allows: its number, a link
 * LED, and a caption with the negotiated speed and PoE watts. There is no VLAN
 * colour, because one hue would have to lie about either the PVID or one of the
 * tagged VLANs. Selecting a tile inspects that port in the column beside the
 * panel instead of navigating away, and the panel keeps its layout while the
 * inspector changes.
 *
 * The table in `switch-page` is the same snapshot and stays the phone layout;
 * below `lg` only the table is rendered. This component is the desktop reading.
 */
export function PortPanel({ snapshot }: { snapshot: Snapshot }) {
  useLocale()
  const inspectorId = useId()
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const ports = panelPorts(snapshot.ports ?? [])
  // Port 1 is selected until the operator picks another, so the inspector opens
  // with an answer rather than an empty card.
  const selected = ports.find(port => port.id === selectedId) ?? ports[0]
  if (!selected) return null

  const runs = groupRuns(ports)
  const counts = countWords(ports)

  return (
    <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_20.5rem] lg:items-start">
      <Card className="gap-0 py-0">
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 px-4 pt-3.5 pb-0">
          <Heading as="h2" size="sm">{t('Front panel')}</Heading>
          <span className="text-xs text-muted-foreground">{t('Click a port')}</span>
          <div className="ml-auto flex flex-wrap items-center gap-x-3 gap-y-1">
            {counts.map(({ word, count }) => {
              const health = state(word)
              return (
                <span key={word} className="inline-flex items-center gap-1.5 text-xs text-muted-foreground">
                  <StatusDot tone={health.tone} />
                  <span className="tabular-nums">{count}</span> {health.label}
                </span>
              )
            })}
          </div>
        </div>

        <div className="space-y-3.5 px-4 py-4">
          {runs.map((run, index) => {
            const label = runLabel(run)
            return (
              <Fragment key={run.medium}>
                {/* Copper and fiber are different sockets, so they are separate
                    runs rather than one number line. */}
                {index > 0 && <span aria-hidden className="block h-px bg-border" />}
                <div>
                  <p className="mb-2 text-[0.6875rem] leading-4 font-medium text-muted-foreground">{label}</p>
                  <div role="group" aria-label={label} className="flex flex-wrap gap-x-1.5 gap-y-2">
                    {run.ports.map(port => (
                      <PortTile
                        key={port.id}
                        port={port}
                        selected={selected.id === port.id}
                        controls={inspectorId}
                        onSelect={() => setSelectedId(port.id)}
                      />
                    ))}
                  </div>
                </div>
              </Fragment>
            )
          })}
          {!ports.some(port => port.poe) && (
            <p className="flex items-center gap-1.5 text-[0.6875rem] leading-4 text-muted-foreground">
              <StatusDot tone="unknown" />
              {t('This read did not report PoE')}
            </p>
          )}
        </div>
      </Card>

      <PortInspector id={inspectorId} port={selected} snapshot={snapshot} />
    </div>
  )
}

/**
 * One port tile. The LED carries the link word, the caption the two facts the
 * tile contract allows beside it, and the accessible name carries the word the
 * dot cannot say out loud.
 */
function PortTile({ port, selected, controls, onSelect }: {
  port: Port
  selected: boolean
  controls: string
  onSelect: () => void
}) {
  useLocale()
  const health = state(tileWord(port))
  const speed = port.speed_mbps != null ? tileSpeed(port.speed_mbps) : undefined
  const spokenSpeed = port.speed_mbps != null
    ? formatSpeed(port.speed_mbps)
    : port.link_up === false ? t('No link') : t('Speed not reported')
  const label = t('Port {number}. {state}. {speed}. {poe}', {
    number: port.id,
    state: health.label,
    speed: spokenSpeed,
    poe: poeLabel(port),
  })

  return (
    <button
      type="button"
      aria-pressed={selected}
      aria-controls={controls}
      aria-label={label}
      title={label}
      onClick={onSelect}
      className="group flex w-[3.875rem] shrink-0 cursor-pointer flex-col items-center gap-[5px] rounded-lg text-center focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
    >
      <span className={cn(
        'relative grid h-[2.875rem] w-full place-items-center rounded-lg border bg-background text-muted-foreground transition-colors duration-150',
        port.link_up === true && 'border-foreground/20 bg-card text-foreground',
        'group-hover:border-primary group-focus-visible:border-primary',
        selected && 'border-primary bg-card text-primary ring-2 ring-ring/25',
      )}>
        <span className="max-w-full truncate px-1 text-[0.90625rem] leading-none font-semibold tracking-[-0.01em] tabular-nums">
          {tileNumber(port)}
        </span>
        <StatusDot tone={health.tone} className="absolute top-1.5 right-1.5" />
      </span>
      <span className="flex h-3 items-center gap-0.5 text-[0.65625rem] leading-none text-muted-foreground tabular-nums">
        <span>{speed ?? '—'}</span>
        <PoeMark port={port} />
      </span>
    </button>
  )
}

/** The PoE half of the tile caption. Silence when the read said nothing, wattage
 * when it did; a port that reports a mark it could not finish stays a ring. */
function PoeMark({ port }: { port: Port }) {
  // No `poe` object is "this read did not report PoE", which is a fact about the
  // adapter, not about the port. Drawing the same ring on every tile turns one
  // sentence into nine glyphs, so the panel says it once, below.
  if (!port.poe) return null
  const mark = poeMark(port)
  if (mark.kind === 'watts') {
    return (
      <span className="inline-flex items-center gap-0.5 font-semibold text-warning-foreground">
        <Zap className="size-2" strokeWidth={2.5} aria-hidden />
        {formatWatts(mark.watts)}W
      </span>
    )
  }
  if (mark.kind === 'on') {
    return <Zap className="size-2 text-warning-foreground" strokeWidth={2.5} aria-hidden />
  }
  if (mark.kind === 'off' || mark.kind === 'not-powered') {
    return <ZapOff className="size-2 text-muted-foreground/60" strokeWidth={2.5} aria-hidden />
  }
  return (
    <span className="relative grid size-2.5 place-items-center">
      <StatusDot tone="unknown" className="absolute inset-0 size-2.5" />
      <Zap className="relative size-1.5 text-muted-foreground/70" strokeWidth={2.5} aria-hidden />
    </span>
  )
}

/**
 * The selected port, beside the panel rather than over it.
 *
 * Every missing field says so instead of borrowing a plausible zero: an absent
 * PoE reading is `Not reported`, a requested counter the device did not send is
 * a dash, and the RX/TX rows are explicitly counters, not a rate derived from a
 * single sample.
 */
function PortInspector({ id, port, snapshot }: { id: string; port: Port; snapshot: Snapshot }) {
  useLocale()
  const titleId = useId()
  const health = state(tileWord(port))
  const speed = port.speed_mbps != null ? formatSpeed(port.speed_mbps) : undefined
  const duplex = t(duplexLabel(port.duplex))
  const detail = speed
    ? duplex ? undefined : t('Duplex not reported')
    : port.link_up === false ? t('No link')
      : port.link_up == null ? t('Link state not reported')
        : t('Speed not reported')

  const vlansCollection = snapshot.collections.vlans
  const vlansRead = vlansCollection ? vlansCollection.state === 'ok' : snapshot.vlans !== undefined
  const vlans = vlansRead ? snapshot.vlans ?? [] : []
  const pvidVlan = port.pvid != null ? vlans.find(vlan => vlan.id === port.pvid) : undefined
  const tagged = vlans
    .filter(vlan => vlan.tagged_ports.includes(port.id))
    .map(vlan => vlan.id)
    .sort((a, b) => a - b)

  const fdbCollection = snapshot.collections.fdb
  const fdbRead = fdbCollection ? fdbCollection.state === 'ok' : snapshot.fdb !== undefined
  const learned = fdbRead ? (snapshot.fdb ?? []).filter(entry => entry.port_id === port.id).length : undefined

  const neighbours = neighboursOf(snapshot, port)
  const poe = poeMark(port)
  const poeHealth = poeState(port)
  const poeDetail = !port.poe
    ? t('This read did not report PoE for this port.')
    : poe.kind === 'on' ? t('Power not reported')
      : poe.kind === 'unknown' ? t('Delivery not reported')
        : undefined

  return (
    <Card id={id} role="region" aria-labelledby={titleId} className="gap-0 py-0">
      {/* The whole reading is the live region: a selection replaces every word
          below it, and a screen reader should hear the new port, not a diff. */}
      <div aria-live="polite" aria-atomic="true">
        <div className="flex flex-wrap items-center gap-2 px-4 pt-3.5 pb-3">
          <Heading as="h3" size="sm" id={titleId}>{t('Port {number}', { number: port.id })}</Heading>
          <StatusPill tone={health.tone}>{health.label}</StatusPill>
        </div>

        <div className="px-4 pb-3">
          <p className="text-[1.375rem] leading-tight font-semibold tracking-[-0.02em] tabular-nums">
            {speed ?? '—'}
            {duplex && <small className="ml-1 text-xs font-medium text-muted-foreground">{duplex}</small>}
          </p>
          {detail && <p className="mt-0.5 text-[0.8125rem] text-muted-foreground">{detail}</p>}
        </div>

        <dl className="px-4 pb-1">
          <InspectorRow label={t('VLAN')}>
            <span>
              {port.pvid != null
                ? pvidVlan?.name ? `${port.pvid} ${pvidVlan.name}` : String(port.pvid)
                : t('Not reported')}
            </span>
            <Detail>
              {tagged.length > 0 ? t('Tagged {vlans}', { vlans: tagged.join(', ') })
                : !vlansRead ? t('VLAN table not read')
                  : port.pvid == null ? t('PVID not reported')
                    : t('No tagged VLANs')}
            </Detail>
          </InspectorRow>

          <InspectorRow label={t('PoE')}>
            {poe.kind === 'watts' ? (
              <>
                <span className="tabular-nums">{formatWatts(poe.watts)} W</span>
                <Detail>{t('Delivering')}</Detail>
              </>
            ) : (
              <>
                <StatusInline tone={poeHealth.tone}>{poeHealth.label}</StatusInline>
                {poeDetail && <Detail>{poeDetail}</Detail>}
              </>
            )}
          </InspectorRow>

          {/* Only when this read carried the MAC table; otherwise the row is
              absent rather than a zero that claims the switch learned nothing. */}
          {learned !== undefined && (
            <InspectorRow label={t('Learned')}>
              <span className="tabular-nums">{learned}</span>
              <Detail>{t('MAC addresses')}</Detail>
            </InspectorRow>
          )}

          <InspectorRow label={t('Received')}>
            <span className="tabular-nums">{counters(port.rx_bytes, port.rx_packets)}</span>
          </InspectorRow>
          <InspectorRow label={t('Sent')}>
            <span className="tabular-nums">{counters(port.tx_bytes, port.tx_packets)}</span>
          </InspectorRow>

          {neighbours.length > 0 && (
            <InspectorRow label={t('Connected to')}>
              <span>{neighbours.map(neighbour => neighbour.name || neighbour.chassis_id).join(', ')}</span>
              <Detail>
                {neighbours.map(neighbour => [
                  t('Reported by {source}', { source: neighbour.source.toUpperCase() }),
                  neighbour.port,
                ].filter(Boolean).join(' · ')).join('; ')}
              </Detail>
            </InspectorRow>
          )}
        </dl>
      </div>

      <p className="px-4 pb-4 text-[0.6875rem] leading-4 text-muted-foreground">
        {t('Counters are the device’s own totals, not a rate.')}
      </p>
    </Card>
  )
}

function InspectorRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex items-start justify-between gap-3 border-t border-border/70 py-2 text-[0.8125rem] leading-5 first:border-t-0">
      <dt className="shrink-0 text-muted-foreground">{label}</dt>
      <dd className="min-w-0 text-right font-medium [overflow-wrap:anywhere]">{children}</dd>
    </div>
  )
}

function Detail({ children }: { children: ReactNode }) {
  return <small className="block text-[0.6875rem] leading-4 font-normal text-muted-foreground">{children}</small>
}

/**
 * The link state a tile may show, and why `No cable` is not one of them here.
 *
 * The snapshot reports one admin bit and one link bit per port. An enabled port
 * with no link cannot be told apart from an empty socket, so the tile says
 * `Down`; `No cable` belongs to an adapter that reports the difference, and
 * this read does not. A port the read never answered for stays a ring.
 */
function tileWord(port: Port): StateWord {
  if (port.enabled === false) return 'Disabled'
  if (port.link_up === true) return 'Connected'
  if (port.link_up === false) return 'Down'
  return 'Unknown'
}

/** Tiles are physical sockets in the order the hardware labels them. */
function panelPorts(ports: Port[]): Port[] {
  return sortPorts(ports.filter(port => port.kind === 'ethernet' || port.kind === 'fiber' || port.kind === 'other'))
}

interface Run {
  medium: 'copper' | 'fiber'
  ports: Port[]
}

function groupRuns(ports: Port[]): Run[] {
  const copper = ports.filter(port => port.kind !== 'fiber')
  const fiber = ports.filter(port => port.kind === 'fiber')
  const runs: Run[] = []
  if (copper.length > 0) runs.push({ medium: 'copper', ports: copper })
  if (fiber.length > 0) runs.push({ medium: 'fiber', ports: fiber })
  return runs
}

/**
 * "1–8 · 2.5 GbE copper". The speed class is the one speed the run's ports
 * negotiated, and it is omitted when they disagree or the read reported none;
 * a range label that guessed would be worse than a plain one.
 */
function runLabel(run: Run): string {
  const speeds = [...new Set(run.ports.map(port => port.speed_mbps).filter((speed): speed is number => speed != null))]
  const speed = speeds.length === 1 ? speedClass(speeds[0]) : undefined
  const medium = run.medium === 'fiber'
    ? run.ports.every(port => port.kind === 'fiber')
    : run.ports.every(port => port.kind === 'ethernet')
  return [portRanges(run.ports), speed, medium ? t(run.medium) : undefined].filter(Boolean).join(' · ')
}

/** Consecutive hardware numbers as ranges; non-numeric IDs keep their order. */
function portRanges(ports: Port[]): string {
  if (!ports.every(port => /^\d+$/.test(port.id))) return ports.map(port => port.id).join(', ')
  const ranges: string[] = []
  let start = Number(ports[0].id)
  let previous = start
  for (const port of ports.slice(1)) {
    const current = Number(port.id)
    if (current === previous + 1) {
      previous = current
      continue
    }
    ranges.push(start === previous ? String(start) : `${start}–${previous}`)
    start = current
    previous = current
  }
  ranges.push(start === previous ? String(start) : `${start}–${previous}`)
  return ranges.join(', ')
}

function countWords(ports: Port[]): { word: StateWord; count: number }[] {
  const order: StateWord[] = ['Connected', 'Down', 'Disabled', 'Unknown']
  return order
    .map(word => ({ word, count: ports.filter(port => tileWord(port) === word).length }))
    .filter(entry => entry.count > 0)
}

/** A tile says the port's number; a hardware label is the fallback. */
function tileNumber(port: Port): string {
  return /^\d+$/.test(port.id) ? port.id : port.name || port.id
}

function speedClass(mbps: number): string {
  if (mbps >= 1000) return `${mbps % 1000 === 0 ? mbps / 1000 : (mbps / 1000).toFixed(1)} GbE`
  return `${mbps} Mbps`
}

function tileSpeed(mbps: number): string {
  if (mbps >= 1000) return `${mbps % 1000 === 0 ? mbps / 1000 : (mbps / 1000).toFixed(1)}G`
  return `${mbps}M`
}

type PoeMark =
  | { kind: 'watts'; watts: number }
  | { kind: 'on' }
  | { kind: 'not-powered' }
  | { kind: 'off' }
  | { kind: 'unknown' }

/**
 * PoE as the snapshot reports it. `enabled` without `delivering` is unknown, not
 * "on": an adapter can know PoE is switched on without measuring delivery.
 */
function poeMark(port: Port): PoeMark {
  const poe = port.poe
  if (!poe) return { kind: 'unknown' }
  if (poe.enabled === false) return { kind: 'off' }
  if (poe.delivering === true) {
    return poe.power_watts != null ? { kind: 'watts', watts: poe.power_watts } : { kind: 'on' }
  }
  if (poe.delivering === false) return { kind: 'not-powered' }
  return { kind: 'unknown' }
}

function poeState(port: Port): Health {
  const mark = poeMark(port)
  if (mark.kind === 'watts' || mark.kind === 'on') return state('On')
  if (mark.kind === 'not-powered') return healthOf('off', t('Not powered'))
  if (mark.kind === 'off') return state('Off')
  return healthOf('unknown', t('Not reported'))
}

function poeLabel(port: Port): string {
  const mark = poeMark(port)
  if (mark.kind === 'watts') return t('PoE {watts} W', { watts: formatWatts(mark.watts) })
  if (mark.kind === 'on') return t('PoE on')
  if (mark.kind === 'off') return t('PoE off')
  if (mark.kind === 'not-powered') return t('PoE not powered')
  return t('PoE not reported')
}

function formatWatts(watts: number): string {
  return Number.isInteger(watts) ? String(watts) : watts.toFixed(1)
}

interface SnapshotNeighbour {
  port_id: string
  chassis_id: string
  port?: string
  name?: string
  source: string
}

/**
 * LLDP neighbours name what is plugged in, when an adapter can read them.
 *
 * The daemon's snapshot carries `neighbors`, but `types.ts` does not model the
 * collection yet; read it through this local shape rather than widening a file
 * this slice does not own. An adapter without LLDP reports none, and then no
 * peer line is drawn — the panel does not infer a cable from anything else.
 */
function neighboursOf(snapshot: Snapshot, port: Port): SnapshotNeighbour[] {
  const neighbours = (snapshot as Snapshot & { neighbors?: SnapshotNeighbour[] }).neighbors
  return (neighbours ?? []).filter(neighbour => neighbour.port_id === port.id)
}
