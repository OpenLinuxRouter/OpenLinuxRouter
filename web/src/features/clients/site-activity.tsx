import { t, useLocale } from '@/lib/i18n'
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { Button } from '@/components/ui/button'

import { api } from '@/lib/api'
import { formatBytes } from '@/lib/utils'

// Retained samples are keyed by device, independently of the live proxy viewer.
// The selected window is reported by the router; missing intervals are not visits.
interface SiteUsage { site: string; up: number; down: number }
interface Bucket { at: string; sites: SiteUsage[] }
interface Response {
  buckets: Bucket[]
  since?: string
  window_seconds: number
  bucket_seconds: number
  sample_seconds: number
  recording: boolean
}

const palette = ['#ff3d54', '#fb7299', '#7fb3d5', '#25f4ee', '#ff8a00', '#60a5fa', '#a78bfa']
const otherColor = 'var(--border)'
const unnamedColor = 'var(--input)'

const ranges = [{ value: '4h', label: '4 hours' }, { value: '24h', label: '24 hours' }, { value: '7d', label: '7 days' }, { value: '30d', label: '30 days' }] as const

export function SiteActivity({ mac }: { mac: string; addresses?: string[] }) {
  useLocale()
  const [range, setRange] = useState<(typeof ranges)[number]['value']>('24h')
  const query = useQuery({
    queryKey: ['inspection', 'activity', mac, range],
    queryFn: () => api.get<Response>(`/api/inspection/activity?${new URLSearchParams({ mac, range })}`),
    refetchInterval: 15000,
  })

  return <div className="space-y-3 px-4 pb-4 pt-4 sm:px-6">
    <div className="flex flex-wrap items-center justify-between gap-3">
      <h3 className="text-sm font-medium">{t('Activity')}</h3>
      <div className="inline-flex flex-wrap items-center gap-1" role="group" aria-label={t('Activity time range')}>
        {ranges.map((item) => <Button key={item.value} size="sm" variant={range === item.value ? 'secondary' : 'quiet'} aria-pressed={range === item.value} onClick={() => setRange(item.value)}>{t(item.label)}</Button>)}
      </div>
    </div>
    {query.isError ? <p role="alert" className="text-sm text-destructive">{t('The activity series is unavailable.')} <Button size="sm" variant="quiet" onClick={() => void query.refetch()}>{t('Retry')}</Button></p>
      : query.isPending ? <p className="text-sm text-muted-foreground">{t('Loading activity…')}</p>
        : <ActivityChart data={query.data} />}
  </div>
}

function ActivityChart({ data }: { data: Response }) {
  useLocale()
  const buckets = data.buckets ?? []
  if (buckets.length === 0) return <p className="text-sm text-muted-foreground">{t('No activity recorded in this range.')}</p>

  const bucketSeconds = Math.max(1, data.bucket_seconds)
  const slots = Math.max(1, Math.ceil(data.window_seconds / bucketSeconds) + 1)
  const started = Math.floor(Date.parse(data.since ?? buckets[0].at) / 1000 / bucketSeconds) * 1000 * bucketSeconds

  // Top sites over the selected retained window, with the rest combined.
  const totals = new Map<string, number>()
  for (const bucket of buckets) for (const row of bucket.sites) {
    totals.set(row.site, (totals.get(row.site) ?? 0) + row.up + row.down)
  }
  const ranked = [...totals.entries()].sort((a, b) => b[1] - a[1])
  const named = ranked.filter(([site]) => site !== '未命名' && site !== '其他')
  const kept = named.slice(0, 6).map(([site]) => site)
  const colorOf = (site: string) => {
    if (site === '未命名') return unnamedColor
    if (site === '其他') return otherColor
    const index = kept.indexOf(site)
    return index >= 0 ? palette[index % palette.length] : otherColor
  }

  const keys = [...kept, ...(named.length > kept.length || totals.has('其他') ? ['其他'] : []), '未命名']
  if (named.length > kept.length) totals.set('其他', (totals.get('其他') ?? 0) + named.slice(kept.length).reduce((sum, [, bytes]) => sum + bytes, 0))
  const values = keys.map(() => new Array(slots).fill(0))
  const columnTotals = new Array(slots).fill(0)
  const observed = new Set<number>()
  for (const bucket of buckets) {
    const slot = Math.min(slots - 1, Math.max(0, Math.round((Date.parse(bucket.at) - started) / 1000 / bucketSeconds)))
    observed.add(slot)
    for (const row of bucket.sites) {
      const site = row.site
      const key = keys.includes(site) ? site : '其他'
      const index = keys.indexOf(key)
      if (index < 0) continue
      values[index][slot] += row.up + row.down
      columnTotals[slot] += row.up + row.down
    }
  }
  const grand = columnTotals.reduce((a, b) => a + b, 0)
  // Draw only retained buckets. Gaps while sampling was stopped stay blank,
  // and a single recorded bucket still occupies a visible interval.
  const runs: number[][] = []
  for (const slot of [...observed].sort((a, b) => a - b)) {
    const previous = runs[runs.length - 1]
    if (previous && previous[previous.length - 1] === slot - 1) previous.push(slot)
    else runs.push([slot])
  }

  const width = 1000
  const height = 132
  const base = height - 4
  const max = Math.max(...columnTotals, 1)
  const x = (i: number) => (i / slots) * width
  const lower = new Array(slots).fill(0)
  const paths = keys.map((key, slot) => {
    const top: number[] = []
    const bottom: number[] = []
    for (let i = 0; i < slots; i++) {
      const total = columnTotals[i]
      const share = total > 0 ? values[slot][i] / total : 0
      const scaled = Math.sqrt(total / max) * (base - 8)
      const y0 = lower[i]
      const y1 = y0 + scaled * share
      bottom.push(y0)
      top.push(y1)
      lower[i] = y1
    }
    const d = runs.map((run) => {
      const last = run[run.length - 1]
      let shape = run.map((i, index) => `${index ? 'L' : 'M'}${x(i).toFixed(1)},${(base - top[i]).toFixed(1)}`).join('')
      shape += `L${x(last + 1).toFixed(1)},${(base - top[last]).toFixed(1)}L${x(last + 1).toFixed(1)},${(base - bottom[last]).toFixed(1)}`
      for (const i of [...run].reverse()) shape += `L${x(i).toFixed(1)},${(base - bottom[i]).toFixed(1)}`
      return `${shape}Z`
    }).join('')
    return <path key={key} d={d} fill={colorOf(key)} fillOpacity={0.95} />
  })

  return <div>
    <div className="mb-1 flex items-baseline gap-4">
      <span className="text-2xl font-semibold tabular-nums">{formatBytes(grand)}</span>
      <span className="text-sm text-muted-foreground">{t('in selected range')}</span>
    </div>
    <svg viewBox={`0 0 ${width} ${height}`} preserveAspectRatio="none" className="block h-28 w-full" role="img" aria-label={t('Retained traffic in the selected range, by site')}>
      {paths}
      <line x1="0" y1={base} x2={width} y2={base} stroke="var(--border)" strokeWidth="1" />
    </svg>
    <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
      {keys.filter((key) => grand > 0 && (totals.get(key) ?? 0) > 0).map((key) => (
        <span key={key} className="inline-flex items-center gap-1.5">
          <i className="size-2 rounded-[2px]" style={{ background: colorOf(key) }} />
          <span className="text-foreground">{key === '其他' ? t('Other') : key === '未命名' ? t('Unnamed') : key}</span>
          <b className="font-medium tabular-nums">{Math.round((100 * (totals.get(key) ?? 0)) / grand)}%</b>
        </span>
      ))}
      <span className="ml-auto">{data.recording ? t('Recording while inspection is on. Saved activity remains after it stops.') : t('Not recording now. Showing saved activity.')}</span>
    </div>
  </div>
}
