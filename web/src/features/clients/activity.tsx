import { t, useLocale } from '@/lib/i18n'
import { useLayoutEffect, useRef, useState } from 'react'

import { MetricValue } from '@/components/ui/metric-value'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { activityLevel, activityThresholds } from '@/lib/activity-scale'
import { formatBytes } from '@/lib/utils'

import { activitySummary, useActivity, type ActivityTarget, type Bytes } from './activity-history'

/**
 * What the device has moved over the retained window.
 *
 * The figure is the title matrix added up, not the kernel's cumulative counter,
 * and it carries no caption of its own: the matrix above it is the only picture
 * of that window, so the window does not need to be restated here.
 */
export function ActivityTotal({ target }: { target: ActivityTarget }) {
  const { data } = useActivity(target)
  const summary = activitySummary(data)
  return <span className="inline-flex flex-wrap items-baseline justify-end gap-x-3 gap-y-1 tabular-nums">
    <span className="inline-flex items-center gap-0.5" aria-label="Download total"><span aria-hidden className="text-base font-normal text-muted-foreground">↓</span><MetricValue value={summary ? formatBytes(summary.down) : null} /></span>
    <span className="inline-flex items-center gap-0.5" aria-label="Upload total"><span aria-hidden className="text-base font-normal text-muted-foreground">↑</span><MetricValue value={summary ? formatBytes(summary.up) : null} /></span>
  </span>
}

const fills = ['bg-[var(--activity-empty)]', 'bg-[var(--activity-low)]', 'bg-[var(--activity-green-low)]', 'bg-[var(--activity-medium)]', 'bg-[var(--activity-high)]', 'bg-[var(--activity-peak)]']

export function ClientActivity({ target, downRate, upRate }: { target: ActivityTarget; downRate?: number; upRate?: number }) {
  useLocale()
  const rate = Math.max(0, downRate ?? 0) + Math.max(0, upRate ?? 0)
  // Keep the breathing shape readable even at high rates (100 MiB/s and above).
  const breathDuration = Number.isFinite(rate) && rate > 0
    ? 4000 - 2600 * Math.min(1, Math.log1p(rate / 1024) / Math.log1p(100 * 1024))
    : undefined
  const container = useRef<HTMLDivElement>(null)
  const [{ days, gap }, setLayout] = useState({ days: 1, gap: 2 })
  useLayoutEffect(() => {
    const element = container.current
    if (!element) return
    const update = (width: number, height: number) => {
      const gap = Math.max(0, (height - 2 - 6 * 14) / 5)
      setLayout({ days: Math.max(1, Math.floor((width + gap) / (14 + gap))), gap })
    }
    const bounds = element.getBoundingClientRect()
    update(bounds.width, bounds.height)
    const observer = new ResizeObserver(([entry]) => update(entry.contentRect.width, entry.contentRect.height))
    observer.observe(element)
    return () => observer.disconnect()
  }, [])
  const history = useActivity(target)
  const buckets = new Map(history.data?.buckets.map(bucket => [Date.parse(bucket.at), bucket.bytes]))
  const now = new Date()
  // Use retained history, independently of the number of columns fitting the
  // screen. The daemon prunes what it no longer keeps, so the only timestamp
  // worth rejecting here is one from the future.
  const periods = new Map<number, Bytes>()
  if (!history.isError) {
    for (const [timestamp, sample] of buckets) {
      if (timestamp > now.getTime()) continue
      const at = new Date(timestamp)
      at.setHours(Math.floor(at.getHours() / 4) * 4, 0, 0, 0)
      const bytes = periods.get(at.getTime()) ?? { up: 0, down: 0 }
      bytes.up += sample.up
      bytes.down += sample.down
      periods.set(at.getTime(), bytes)
    }
  }
  const thresholds = activityThresholds([...periods.values()].map(bytes => bytes.up + bytes.down))
  const currentPeriod = `${now.getFullYear()}-${now.getMonth()}-${now.getDate()}-${Math.floor(now.getHours() / 4)}`
  useLayoutEffect(() => {
    const animation = container.current?.querySelector('.device-activity-current')?.getAnimations()[0]
    if (!animation) return
    const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)')
    let frame = 0
    let previous = performance.now()
    const active = breathDuration !== undefined
    const targetSpeed = active ? 4000 / breathDuration : 1
    // Keep one fixed timeline. Updating playback rate preserves its current phase.
    const stopAt = (Math.floor(Number(animation.currentTime ?? 0) / 4000) + 1) * 4000
    const tick = (time: number) => {
      if (reducedMotion.matches) {
        animation.pause()
        animation.currentTime = 0
        return
      }
      if (!active && (animation.playState === 'paused' || Number(animation.currentTime) >= stopAt)) {
        animation.pause()
        animation.currentTime = 0
        return
      }
      const elapsed = Math.max(0, time - previous)
      previous = time
      const speed = animation.playbackRate + (targetSpeed - animation.playbackRate) * (1 - Math.exp(-elapsed / 350))
      animation.updatePlaybackRate(speed)
      animation.play()
      frame = requestAnimationFrame(tick)
    }
    const restart = () => {
      cancelAnimationFrame(frame)
      previous = performance.now()
      frame = requestAnimationFrame(tick)
    }
    // Initial idle state should stay fully visible rather than run a first breath.
    if (!active && Number(animation.currentTime ?? 0) === 0) animation.pause()
    restart()
    reducedMotion.addEventListener('change', restart)
    return () => {
      cancelAnimationFrame(frame)
      reducedMotion.removeEventListener('change', restart)
    }
  }, [breathDuration, days, currentPeriod])
  const cells = Array.from({ length: days * 6 }, (_, i) => {
    const day = now.getDate() - days + 1 + Math.floor(i / 6)
    const hour = (i % 6) * 4
    const at = new Date(now.getFullYear(), now.getMonth(), day, hour)
    const end = new Date(now.getFullYear(), now.getMonth(), day, hour + 4)
    if (at > now) return <span key={i} aria-hidden="true" />
    const bytes = periods.get(at.getTime())
    const total = bytes ? bytes.up + bytes.down : 0
    const level = activityLevel(total, thresholds)
    const time = `${String(at.getMonth() + 1).padStart(2, '0')}-${String(at.getDate()).padStart(2, '0')} ${hour}:00-${hour + 4}:00`
    const detail = history.isPending ? 'Loading' : history.isError ? t('Unavailable') : !bytes ? t('No data') : `↓ ${formatBytes(bytes.down)} · ↑ ${formatBytes(bytes.up)}`
    return <Tooltip key={i}>
      <TooltipTrigger delay={0} aria-label={`${time}: ${detail}`} className={`h-full w-full min-h-0 min-w-0 rounded-[3px] p-0 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring ${fills[level]} ${end > now ? 'device-activity-current' : ''}`} />
      <TooltipContent className="flex-col items-start animate-none!"><span>{time}</span><span>{detail}</span></TooltipContent>
    </Tooltip>
  })
  return <div ref={container} className="absolute right-0 -top-2 h-24 w-2/3 [mask-image:linear-gradient(to_right,transparent,black_20%)]" role="group" aria-label={`Traffic activity, last ${days} days, oldest to newest from left to right, six four-hour periods per day from top to bottom`}>
    <div className="grid grid-flow-col auto-cols-[14px] grid-rows-[repeat(6,14px)] justify-end py-px" style={{ gap }}>{cells}</div>
  </div>
}
